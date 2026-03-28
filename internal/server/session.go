package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/hashicorp/yamux"

	"jaypopat/tunnel-thing/internal/proto"
)

type tunnel struct {
	id       string
	port     uint16
	name     string
	listener net.Listener
	cancel   context.CancelFunc
}

type session struct {
	clientID      string
	yamux         *yamux.Session
	controlStream net.Conn
	router        *Router
	log           *slog.Logger

	mu      sync.Mutex
	tunnels map[string]*tunnel
}

func newSession(clientID string, ys *yamux.Session, control net.Conn, router *Router) *session {
	return &session{
		clientID:      clientID,
		yamux:         ys,
		controlStream: control,
		router:        router,
		log:           slog.With("client_id", clientID),
		tunnels:       make(map[string]*tunnel),
	}
}

func (s *session) run(ctx context.Context) {
	defer s.cleanup()

	for {
		msgType, raw, err := proto.ReadMessage(s.controlStream)
		if err != nil {
			if ctx.Err() != nil {
				s.log.Info("session ending (server shutdown)")
			} else {
				s.log.Info("client disconnected", "err", err)
			}
			return
		}

		switch msgType {
		case proto.MsgTunnelRequest:
			s.handleTunnelRequest(ctx, raw)
		case proto.MsgTunnelClose:
			s.handleTunnelClose(raw)
		default:
			s.log.Warn("unexpected message type", "type", fmt.Sprintf("0x%02x", msgType))
		}
	}
}

func (s *session) handleTunnelRequest(ctx context.Context, raw []byte) {
	req, err := proto.Decode[proto.TunnelRequest](raw)
	if err != nil {
		s.log.Error("decode tunnel request failed", "err", err)
		return
	}

	if req.Name != "" {
		s.handleNameTunnel(req)
		return
	}

	s.handlePortTunnel(ctx, req)
}

func (s *session) handleNameTunnel(req proto.TunnelRequest) {
	tunnelID := fmt.Sprintf("t-%s-%s", s.clientID, req.Name)

	if err := s.router.Register(req.Name, s); err != nil {
		s.log.Warn("subdomain registration failed", "name", req.Name, "err", err)
		proto.WriteMessage(s.controlStream, proto.MsgTunnelResponse, proto.TunnelResponse{
			OK:    false,
			Error: err.Error(),
		})
		return
	}

	tun := &tunnel{
		id:   tunnelID,
		name: req.Name,
		cancel: func() {
			s.router.Unregister(req.Name)
		},
	}

	s.mu.Lock()
	s.tunnels[tunnelID] = tun
	s.mu.Unlock()

	s.log.Info("tunnel open (subdomain)", "tunnel_id", tunnelID, "name", req.Name)

	proto.WriteMessage(s.controlStream, proto.MsgTunnelResponse, proto.TunnelResponse{
		OK:       true,
		TunnelID: tunnelID,
		Name:     req.Name,
	})
}

func (s *session) handlePortTunnel(ctx context.Context, req proto.TunnelRequest) {
	port := req.RequestedPort
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		s.log.Error("listen on public port failed", "port", port, "err", err)
		proto.WriteMessage(s.controlStream, proto.MsgTunnelResponse, proto.TunnelResponse{
			OK:    false,
			Error: fmt.Sprintf("cannot listen on port %d: %v", port, err),
		})
		return
	}

	tunnelID := fmt.Sprintf("t-%s-%d", s.clientID, port)
	tunnelCtx, tunnelCancel := context.WithCancel(ctx)

	tun := &tunnel{
		id:       tunnelID,
		port:     port,
		listener: ln,
		cancel:   tunnelCancel,
	}

	s.mu.Lock()
	s.tunnels[tunnelID] = tun
	s.mu.Unlock()

	s.log.Info("tunnel open (port)", "tunnel_id", tunnelID, "port", port)

	if err := proto.WriteMessage(s.controlStream, proto.MsgTunnelResponse, proto.TunnelResponse{
		OK:           true,
		TunnelID:     tunnelID,
		AssignedPort: port,
	}); err != nil {
		s.log.Error("write tunnel response failed", "err", err)
		tunnelCancel()
		ln.Close()
		return
	}

	go s.runTunnel(tunnelCtx, tun)
}

func (s *session) runTunnel(ctx context.Context, tun *tunnel) {
	defer func() {
		tun.listener.Close()
		s.mu.Lock()
		delete(s.tunnels, tun.id)
		s.mu.Unlock()
		s.log.Info("tunnel closed", "tunnel_id", tun.id, "port", tun.port)
	}()

	go func() {
		<-ctx.Done()
		tun.listener.Close()
	}()

	var wg sync.WaitGroup
	for {
		extConn, err := tun.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || s.yamux.IsClosed() {
				break
			}
			s.log.Error("accept on public port failed", "tunnel_id", tun.id, "err", err)
			continue
		}

		wg.Go(func() {
			s.proxyConnection(tun.id, extConn)
		})
	}

	wg.Wait()
}

func (s *session) handleTunnelClose(raw []byte) {
	req, err := proto.Decode[proto.TunnelClose](raw)
	if err != nil {
		s.log.Error("decode tunnel close failed", "err", err)
		return
	}

	s.mu.Lock()
	tun, ok := s.tunnels[req.TunnelID]
	if ok {
		delete(s.tunnels, req.TunnelID)
	}
	s.mu.Unlock()

	if !ok {
		return
	}

	tun.cancel()
	proto.WriteMessage(s.controlStream, proto.MsgTunnelClosed, proto.TunnelClosed{
		TunnelID: req.TunnelID,
	})
}

func (s *session) cleanup() {
	s.mu.Lock()
	tunnels := make([]*tunnel, 0, len(s.tunnels))
	for _, tun := range s.tunnels {
		tunnels = append(tunnels, tun)
	}
	s.mu.Unlock()

	for _, tun := range tunnels {
		tun.cancel()
	}
	s.log.Info("session closed")
}

func (s *session) proxyConnection(tunnelID string, extConn net.Conn) {
	defer extConn.Close()

	stream, err := s.yamux.Open()
	if err != nil {
		s.log.Error("open yamux stream failed", "tunnel_id", tunnelID, "err", err)
		return
	}

	if err := proto.WriteProxyHeader(stream, proto.ProxyHeader{TunnelID: tunnelID}); err != nil {
		s.log.Error("write proxy header failed", "tunnel_id", tunnelID, "err", err)
		stream.Close()
		return
	}

	s.log.Debug("proxying connection", "tunnel_id", tunnelID, "external", extConn.RemoteAddr())
	proto.Proxy(stream, extConn)
}
