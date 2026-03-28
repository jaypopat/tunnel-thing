package server

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

var (
	hostPrefix    = []byte("Host:")
	headerTermSeq = []byte("\r\n\r\n")
)

// HTTPListener accepts HTTP connections on a shared port, peeks at the
// Host header to route to the correct tunnel session, then proxies raw
// bytes through. L7 routing decision, L4 data path.
type HTTPListener struct {
	addr         string
	domain       string
	domainSuffix string // "." + domain, precomputed
	router       *Router
}

func NewHTTPListener(addr, domain string, router *Router) *HTTPListener {
	return &HTTPListener{
		addr:         addr,
		domain:       domain,
		domainSuffix: "." + domain,
		router:       router,
	}
}

func (h *HTTPListener) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", h.addr)
	if err != nil {
		return fmt.Errorf("http listen: %w", err)
	}
	slog.Info("http listener ready", "addr", ln.Addr(), "domain", "*."+h.domain)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			slog.Error("http accept error", "err", err)
			continue
		}

		wg.Go(func() {
			h.handleConn(conn)
		})
	}

	wg.Wait()
	return nil
}

func (h *HTTPListener) handleConn(conn net.Conn) {
	defer conn.Close()

	// Deadline prevents slow/malicious clients from holding goroutines.
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	info, err := peekHTTPHost(conn)
	if err != nil {
		slog.Debug("failed to parse Host header", "remote", conn.RemoteAddr(), "err", err)
		writeHTTPError(conn, "400 Bad Request", "Bad request.\n")
		return
	}

	// Clear deadline for the proxy phase — data flows at its own pace.
	conn.SetReadDeadline(time.Time{})

	subdomain, err := h.extractSubdomain(info.host)
	if err != nil {
		slog.Debug("bad subdomain", "host", info.host, "err", err)
		writeHTTPError(conn, "404 Not Found", "Tunnel not found.\n")
		return
	}

	sess, ok := h.router.Lookup(subdomain)
	if !ok {
		slog.Debug("no tunnel for subdomain", "subdomain", subdomain)
		writeHTTPError(conn, "502 Bad Gateway", "No tunnel for "+subdomain+".\n")
		return
	}

	tunnelID := nameTunnelID(sess.clientID, subdomain)
	header := injectProxyHeaders(info.headerBytes, conn.RemoteAddr().String(), info.host)

	replayConn := &prefixConn{
		Reader: io.MultiReader(bytes.NewReader(header), conn),
		Conn:   conn,
	}

	slog.Debug("http request", "method", info.method, "path", info.path, "subdomain", subdomain, "remote", conn.RemoteAddr())
	sess.proxyConnection(tunnelID, replayConn)
}

// extractSubdomain: "myapp.tunnel.dev" → "myapp", "myapp.tunnel.dev:8080" → "myapp"
func (h *HTTPListener) extractSubdomain(host string) (string, error) {
	if hostname, _, err := net.SplitHostPort(host); err == nil {
		host = hostname
	}

	if !strings.HasSuffix(host, h.domainSuffix) {
		return "", fmt.Errorf("%q is not a subdomain of %q", host, h.domain)
	}

	subdomain := strings.TrimSuffix(host, h.domainSuffix)
	if subdomain == "" {
		return "", fmt.Errorf("empty subdomain in %q", host)
	}

	return subdomain, nil
}

type httpRequestInfo struct {
	headerBytes []byte
	host        string
	method      string
	path        string
}

// peekHTTPHost reads HTTP headers from conn, extracts the Host value
// and request method/path, and returns all bytes read for replay to
// the tunnel client.
func peekHTTPHost(conn net.Conn) (info httpRequestInfo, err error) {
	br := bufio.NewReaderSize(conn, 4096)

	var buf bytes.Buffer
	buf.Grow(512)
	var hostVal string
	firstLine := true

	for range 128 {
		line, err := br.ReadSlice('\n')
		buf.Write(line)
		if err != nil {
			return httpRequestInfo{headerBytes: buf.Bytes()}, fmt.Errorf("read header: %w", err)
		}

		trimmed := bytes.TrimRight(line, "\r\n")
		if len(trimmed) == 0 {
			break
		}

		if firstLine {
			firstLine = false
			if i := bytes.IndexByte(trimmed, ' '); i > 0 {
				info.method = string(trimmed[:i])
				rest := trimmed[i+1:]
				if j := bytes.IndexByte(rest, ' '); j > 0 {
					info.path = string(rest[:j])
				} else {
					info.path = string(rest)
				}
			}
		}

		// Case-insensitive check for "Host:" prefix (skip once found).
		if hostVal == "" && len(trimmed) >= 5 && (trimmed[0] == 'H' || trimmed[0] == 'h') &&
			bytes.EqualFold(trimmed[:5], hostPrefix) {
			hostVal = strings.TrimSpace(string(trimmed[5:]))
		}
	}

	if hostVal == "" {
		return httpRequestInfo{headerBytes: buf.Bytes()}, fmt.Errorf("no Host header found")
	}

	// buf has the bytes consumed by ReadSlice. br may have buffered
	// additional bytes beyond the headers (start of body). We need to
	// include those for replay, so prepend any buffered remainder.
	remaining := br.Buffered()
	if remaining > 0 {
		extra, _ := br.Peek(remaining)
		buf.Write(extra)
	}

	info.headerBytes = buf.Bytes()
	info.host = hostVal
	return info, nil
}

// injectProxyHeaders splices X-Forwarded-* headers into raw HTTP header
// bytes, right before the terminal \r\n that ends the header block.
func injectProxyHeaders(headerBytes []byte, remoteAddr, host string) []byte {
	clientIP, _, _ := net.SplitHostPort(remoteAddr)
	if clientIP == "" {
		clientIP = remoteAddr
	}

	// Headers end with \r\n\r\n. Insert before the final \r\n.
	idx := bytes.Index(headerBytes, headerTermSeq)
	if idx < 0 {
		return headerBytes
	}

	var buf bytes.Buffer
	buf.Grow(len(headerBytes) + len(clientIP) + len(host) + 80)
	buf.Write(headerBytes[:idx+2])
	buf.WriteString("X-Forwarded-For: ")
	buf.WriteString(clientIP)
	buf.WriteString("\r\nX-Forwarded-Host: ")
	buf.WriteString(host)
	buf.WriteString("\r\nX-Forwarded-Proto: http\r\n")
	buf.Write(headerBytes[idx+2:])
	return buf.Bytes()
}

func writeHTTPError(w io.Writer, status, body string) {
	fmt.Fprintf(w, "HTTP/1.1 %s\r\nContent-Type: text/plain\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", status, len(body), body)
}

// prefixConn wraps a net.Conn with a different Reader that replays
// buffered bytes before reading from the underlying connection.
// Write and Close delegate to the original Conn.
type prefixConn struct {
	io.Reader
	net.Conn
}

func (c *prefixConn) Read(p []byte) (int, error) {
	return c.Reader.Read(p)
}
