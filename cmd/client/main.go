package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"

	"jaypopat/tunnel-thing/internal/client"
)

func main() {
	serverAddr := flag.String("server", "localhost:7000", "server address")
	secret := flag.String("secret", "", "shared secret for server auth")
	localAddr := flag.String("local", "localhost:8080", "local service address to tunnel to")
	remotePort := flag.Uint("remote", 0, "remote port to expose (port-based tunnel)")
	name := flag.String("name", "", "subdomain name for HTTP routing (e.g. 'myapp')")
	verbose := flag.Bool("v", false, "verbose logging (debug level)")
	flag.Parse()

	if *remotePort == 0 && *name == "" {
		slog.Error("specify either -remote (port) or -name (subdomain)")
		os.Exit(1)
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c := client.New(client.Config{
		ServerAddr: *serverAddr,
		Secret:     *secret,
		LocalAddr:  *localAddr,
		RemotePort: uint16(*remotePort),
		Name:       *name,
	})

	if err := c.Run(ctx); err != nil {
		slog.Error("client exited", "err", err)
		os.Exit(1)
	}
}
