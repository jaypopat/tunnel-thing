package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"

	goauth "jaypopat/tunnel-thing/internal/auth"
	"jaypopat/tunnel-thing/internal/server"
)

func main() {
	listen := flag.String("listen", ":7000", "address to listen on for client connections")
	tokenFile := flag.String("tokens", "", "path to token file (token:label per line). If empty, all connections are allowed")
	httpAddr := flag.String("http", "", "address for shared HTTP listener (e.g. ':8080'). Required for subdomain routing")
	domain := flag.String("domain", "", "base domain for subdomain routing (e.g. 'tunnel.dev')")
	verbose := flag.Bool("v", false, "verbose logging (debug level)")
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})))

	var authenticator goauth.Authenticator
	if *tokenFile != "" {
		var err error
		authenticator, err = goauth.NewTokenFileAuth(*tokenFile)
		if err != nil {
			slog.Error("failed to load token file", "path", *tokenFile, "err", err)
			os.Exit(1)
		}
		slog.Info("loaded token file", "path", *tokenFile)
	} else {
		authenticator = goauth.AllowAll{}
		slog.Warn("no token file specified — all connections will be accepted")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	srv := server.New(server.Config{
		ListenAddr: *listen,
		HTTPAddr:   *httpAddr,
		Domain:     *domain,
		Auth:       authenticator,
	})

	if err := srv.Run(ctx); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}
