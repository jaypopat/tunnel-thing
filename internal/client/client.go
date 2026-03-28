package client

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/hashicorp/yamux"

	"jaypopat/tunnel-thing/internal/proto"
)

type Config struct {
	ServerAddr string
	Secret     string
	LocalAddr  string
	RemotePort uint16 // port-based tunnel (0 if using name)
	Name       string // subdomain name for HTTP routing (empty if using port)
}

type Client struct {
	cfg Config
}

func New(cfg Config) *Client {
	return &Client{cfg: cfg}
}

// Run connects to the server, authenticates, requests a tunnel, and proxies.
// Blocks until ctx is cancelled or the connection drops.
func (c *Client) Run(ctx context.Context) error {
	conn, err := net.Dial("tcp", c.cfg.ServerAddr)
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	defer conn.Close()

	slog.Info("connected to server", "server", c.cfg.ServerAddr)

	session, err := yamux.Client(conn, nil)
	if err != nil {
		return fmt.Errorf("yamux client setup: %w", err)
	}
	defer session.Close()

	// Close the session when context is cancelled, unblocking Accept.
	go func() {
		<-ctx.Done()
		session.Close()
	}()

	controlStream, err := session.Open()
	if err != nil {
		return fmt.Errorf("open control stream: %w", err)
	}
	defer controlStream.Close()

	if err := c.authenticate(controlStream); err != nil {
		return err
	}

	if err := proto.WriteMessage(controlStream, proto.MsgTunnelRequest, proto.TunnelRequest{
		RequestedPort: c.cfg.RemotePort,
		Name:          c.cfg.Name,
	}); err != nil {
		return fmt.Errorf("send tunnel request: %w", err)
	}

	msgType, raw, err := proto.ReadMessage(controlStream)
	if err != nil {
		return fmt.Errorf("read tunnel response: %w", err)
	}
	if msgType != proto.MsgTunnelResponse {
		return fmt.Errorf("unexpected message type: 0x%02x", msgType)
	}

	resp, err := proto.Decode[proto.TunnelResponse](raw)
	if err != nil {
		return fmt.Errorf("decode tunnel response: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("tunnel rejected: %s", resp.Error)
	}

	slog.Info("tunnel established",
		"tunnel_id", resp.TunnelID,
		"name", resp.Name,
		"remote_port", resp.AssignedPort,
		"local_addr", c.cfg.LocalAddr,
	)
	var wg sync.WaitGroup
	for {
		stream, err := session.Accept()
		if err != nil {
			if ctx.Err() != nil || session.IsClosed() {
				break
			}
			slog.Error("accept data stream failed", "err", err)
			continue
		}

		wg.Go(func() {
			c.handleStream(stream)
		})
	}

	wg.Wait()
	return nil
}

func (c *Client) authenticate(controlStream net.Conn) error {
	if err := proto.WriteMessage(controlStream, proto.MsgAuthRequest, proto.AuthRequest{
		Token: c.cfg.Secret,
	}); err != nil {
		return fmt.Errorf("send auth request: %w", err)
	}

	msgType, raw, err := proto.ReadMessage(controlStream)
	if err != nil {
		return fmt.Errorf("read auth response: %w", err)
	}
	if msgType != proto.MsgAuthResponse {
		return fmt.Errorf("unexpected message type: 0x%02x", msgType)
	}

	resp, err := proto.Decode[proto.AuthResponse](raw)
	if err != nil {
		return fmt.Errorf("decode auth response: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("authentication failed: %s", resp.Error)
	}

	slog.Info("authenticated", "client_id", resp.ClientID)
	return nil
}

func (c *Client) handleStream(stream net.Conn) {
	defer stream.Close()

	header, err := proto.ReadProxyHeader(stream)
	if err != nil {
		slog.Error("read proxy header failed", "err", err)
		return
	}

	localConn, err := net.Dial("tcp", c.cfg.LocalAddr)
	if err != nil {
		slog.Error("dial local service failed",
			"local_addr", c.cfg.LocalAddr,
			"tunnel_id", header.TunnelID,
			"err", err,
		)
		return
	}

	slog.Debug("proxying to local", "tunnel_id", header.TunnelID, "local_addr", c.cfg.LocalAddr)
	proto.Proxy(stream, localConn)
}
