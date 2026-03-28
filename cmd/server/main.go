package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"

	"jaypopat/tunnel-thing/internal/server"
)

func main() {
	listen := flag.String("listen", ":7000", "address to listen on for client connections")
	secret := flag.String("secret", "", "shared secret for client auth (empty = no auth)")
	httpAddr := flag.String("http", "", "address for shared HTTP listener (e.g. ':8080')")
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

	if *secret == "" {
		slog.Warn("no secret specified — all connections will be accepted")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	srv := server.New(server.Config{
		ListenAddr: *listen,
		HTTPAddr:   *httpAddr,
		Domain:     *domain,
		Secret:     *secret,
	})

	if err := srv.Run(ctx); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}
