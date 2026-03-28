package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/hashicorp/yamux"

	"jaypopat/tunnel-thing/internal/auth"
	"jaypopat/tunnel-thing/internal/proto"
)

type Config struct {
	ListenAddr string
	HTTPAddr   string // shared HTTP listener for subdomain routing (e.g. ":8080")
	Domain     string // base domain (e.g. "tunnel.dev")
	Auth       auth.Authenticator
}

type Server struct {
	cfg    Config
	router *Router
	wg     sync.WaitGroup
}

func New(cfg Config) *Server {
	return &Server{
		cfg:    cfg,
		router: NewRouter(),
	}
}

func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	slog.Info("server listening", "addr", ln.Addr())

	// Start the HTTP listener for subdomain routing if configured.
	if s.cfg.HTTPAddr != "" && s.cfg.Domain != "" {
		httpLn := NewHTTPListener(s.cfg.HTTPAddr, s.cfg.Domain, s.router)
		s.wg.Go(func() {
			if err := httpLn.Run(ctx); err != nil {
				slog.Error("http listener exited", "err", err)
			}
		})
	}

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			slog.Error("accept error", "err", err)
			continue
		}

		s.wg.Go(func() {
			s.handleClient(ctx, conn)
		})
	}

	s.wg.Wait()
	return nil
}

func (s *Server) handleClient(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	remote := conn.RemoteAddr()
	slog.Info("client connected", "remote", remote)

	ymux, err := yamux.Server(conn, nil)
	if err != nil {
		slog.Error("yamux setup failed", "remote", remote, "err", err)
		return
	}
	defer ymux.Close()

	controlStream, err := ymux.Accept()
	if err != nil {
		slog.Error("accept control stream failed", "remote", remote, "err", err)
		return
	}
	defer controlStream.Close()

	clientID, ok := s.authenticate(controlStream, remote)
	if !ok {
		return
	}

	sess := newSession(clientID, ymux, controlStream, s.router)
	sess.run(ctx)
}

func (s *Server) authenticate(controlStream net.Conn, remote net.Addr) (string, bool) {
	msgType, raw, err := proto.ReadMessage(controlStream)
	if err != nil {
		slog.Error("read auth request failed", "remote", remote, "err", err)
		return "", false
	}

	if msgType != proto.MsgAuthRequest {
		slog.Error("expected auth request", "remote", remote, "got", fmt.Sprintf("0x%02x", msgType))
		return "", false
	}

	req, err := proto.Decode[proto.AuthRequest](raw)
	if err != nil {
		slog.Error("decode auth request failed", "remote", remote, "err", err)
		return "", false
	}

	clientID, ok := s.cfg.Auth.Validate(req.Token)
	if !ok {
		slog.Warn("auth failed", "remote", remote)
		proto.WriteMessage(controlStream, proto.MsgAuthResponse, proto.AuthResponse{
			OK:    false,
			Error: "invalid token",
		})
		return "", false
	}

	slog.Info("client authenticated", "remote", remote, "client_id", clientID)
	proto.WriteMessage(controlStream, proto.MsgAuthResponse, proto.AuthResponse{
		OK:       true,
		ClientID: clientID,
	})

	return clientID, true
}
