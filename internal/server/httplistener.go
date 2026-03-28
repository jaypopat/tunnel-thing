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

	header, host, err := peekHTTPHost(conn)
	if err != nil {
		slog.Debug("failed to parse Host header", "remote", conn.RemoteAddr(), "err", err)
		fmt.Fprintf(conn, "HTTP/1.1 400 Bad Request\r\nContent-Length: 16\r\n\r\nBad Request.\r\n\r\n")
		return
	}

	// Clear deadline for the proxy phase — data flows at its own pace.
	conn.SetReadDeadline(time.Time{})

	subdomain, err := h.extractSubdomain(host)
	if err != nil {
		slog.Debug("bad subdomain", "host", host, "err", err)
		fmt.Fprintf(conn, "HTTP/1.1 404 Not Found\r\nContent-Length: 24\r\n\r\nTunnel not found.\r\n\r\n")
		return
	}

	sess, ok := h.router.Lookup(subdomain)
	if !ok {
		slog.Debug("no tunnel for subdomain", "subdomain", subdomain)
		fmt.Fprintf(conn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 32\r\n\r\nNo tunnel for %s.\r\n\r\n", subdomain)
		return
	}

	tunnelID := nameTunnelID(sess.clientID, subdomain)
	replayConn := &prefixConn{
		Reader: io.MultiReader(bytes.NewReader(header), conn),
		Conn:   conn,
	}

	slog.Debug("routing http", "subdomain", subdomain, "client", sess.clientID, "remote", conn.RemoteAddr())
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

// peekHTTPHost reads HTTP headers from conn, extracts the Host value,
// and returns all bytes read for replay to the tunnel client.
func peekHTTPHost(conn net.Conn) (headerBytes []byte, host string, err error) {
	br := bufio.NewReaderSize(conn, 4096)

	// Scan lines until we find the blank line ending the headers.
	var buf bytes.Buffer
	var hostVal string

	for range 128 {
		line, err := br.ReadSlice('\n')
		buf.Write(line)
		if err != nil {
			return buf.Bytes(), "", fmt.Errorf("read header: %w", err)
		}

		trimmed := bytes.TrimRight(line, "\r\n")
		if len(trimmed) == 0 {
			break
		}

		// Case-insensitive check for "Host:" prefix.
		if len(trimmed) > 5 && (trimmed[0] == 'H' || trimmed[0] == 'h') &&
			bytes.EqualFold(trimmed[:5], []byte("Host:")) {
			hostVal = strings.TrimSpace(string(trimmed[5:]))
		}
	}

	if hostVal == "" {
		return buf.Bytes(), "", fmt.Errorf("no Host header found")
	}

	// buf has the bytes consumed by ReadSlice. br may have buffered
	// additional bytes beyond the headers (start of body). We need to
	// include those for replay, so prepend any buffered remainder.
	remaining := br.Buffered()
	if remaining > 0 {
		extra, _ := br.Peek(remaining)
		buf.Write(extra)
	}

	return buf.Bytes(), hostVal, nil
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
