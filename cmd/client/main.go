package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"jaypopat/tunnel-thing/internal/client"
)

type tunnelFlags []string

func (t *tunnelFlags) String() string { return fmt.Sprint(*t) }
func (t *tunnelFlags) Set(value string) error {
	*t = append(*t, value)
	return nil
}

func main() {
	configPath := flag.String("config", "", "path to TOML config file")
	serverAddr := flag.String("server", "localhost:7000", "server address")
	secret := flag.String("secret", "", "shared secret for server auth")
	verbose := flag.Bool("v", false, "verbose logging (debug level)")

	var tunnels tunnelFlags
	flag.Var(&tunnels, "tunnel", "tunnel spec: name=local or port=local (repeatable)")

	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})))

	cfg, err := loadConfig(*configPath, *serverAddr, *secret, tunnels)
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c := client.New(cfg)
	if err := c.Run(ctx); err != nil {
		slog.Error("client exited", "err", err)
		os.Exit(1)
	}
}

func loadConfig(configPath, serverAddr, secret string, tunnels tunnelFlags) (client.Config, error) {
	if configPath != "" {
		return loadTOML(configPath)
	}
	if len(tunnels) == 0 {
		cfg, err := loadTOML("config.toml")
		if err == nil {
			return cfg, nil
		}
	}
	return configFromFlags(serverAddr, secret, tunnels)
}

func loadTOML(path string) (client.Config, error) {
	var cfg client.Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	if len(cfg.Tunnels) == 0 {
		return cfg, fmt.Errorf("config %s: no tunnels defined", path)
	}
	for i, t := range cfg.Tunnels {
		if t.LocalAddr == "" {
			return cfg, fmt.Errorf("config %s: tunnel %d missing 'local' address", path, i+1)
		}
		if t.Name == "" && t.RemotePort == 0 {
			return cfg, fmt.Errorf("config %s: tunnel %d must have 'name' or 'port'", path, i+1)
		}
	}
	return cfg, nil
}

func configFromFlags(serverAddr, secret string, tunnels tunnelFlags) (client.Config, error) {
	if len(tunnels) == 0 {
		return client.Config{}, fmt.Errorf("specify -tunnel or -config")
	}

	specs := make([]client.TunnelSpec, 0, len(tunnels))
	for _, raw := range tunnels {
		left, right, ok := strings.Cut(raw, "=")
		if !ok || left == "" || right == "" {
			return client.Config{}, fmt.Errorf("invalid tunnel spec %q (use name=local or port=local)", raw)
		}

		var spec client.TunnelSpec
		spec.LocalAddr = right

		if port, err := strconv.ParseUint(left, 10, 16); err == nil && port > 0 {
			spec.RemotePort = uint16(port)
		} else {
			spec.Name = left
		}
		specs = append(specs, spec)
	}

	return client.Config{
		ServerAddr: serverAddr,
		Secret:     secret,
		Tunnels:    specs,
	}, nil
}
