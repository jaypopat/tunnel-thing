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

// TunnelSpec defines a single tunnel. Set either Name (subdomain) or RemotePort (TCP), not both.
type TunnelSpec struct {
	Name       string `toml:"name"`
	RemotePort uint16 `toml:"port"`
	LocalAddr  string `toml:"local"`
}

type Config struct {
	ServerAddr string       `toml:"server"`
	Secret     string       `toml:"secret"`
	Tunnels    []TunnelSpec `toml:"tunnels"`
}

type Client struct {
	cfg    Config
	routes map[string]string
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

	c.routes = make(map[string]string)

	for _, spec := range c.cfg.Tunnels {
		if err := proto.WriteMessage(controlStream, proto.MsgTunnelRequest, proto.TunnelRequest{
			RequestedPort: spec.RemotePort,
			Name:          spec.Name,
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

		c.routes[resp.TunnelID] = spec.LocalAddr

		slog.Info("tunnel established",
			"tunnel_id", resp.TunnelID,
			"name", resp.Name,
			"remote_port", resp.AssignedPort,
			"local_addr", spec.LocalAddr,
		)
	}

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

	localAddr, ok := c.routes[header.TunnelID]
	if !ok {
		slog.Error("unknown tunnel ID", "tunnel_id", header.TunnelID)
		return
	}

	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		slog.Error("dial local service failed",
			"local_addr", localAddr,
			"tunnel_id", header.TunnelID,
			"err", err,
		)
		return
	}

	slog.Debug("proxying to local", "tunnel_id", header.TunnelID, "local_addr", localAddr)
	proto.Proxy(stream, localConn)
}
