package integration

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"jaypopat/tunnel-thing/internal/client"
	"jaypopat/tunnel-thing/internal/server"
)

// freeAddr returns a free TCP address by briefly listening and closing.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// TestEndToEnd starts a server and client, establishes a name-based tunnel,
// sends an HTTP request through it, and verifies the response.
func TestEndToEnd(t *testing.T) {
	// Start a local "backend" that the tunnel will expose.
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()

	go func() {
		for {
			conn, err := backend.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				// Read HTTP headers (until blank line), then respond.
				br := bufio.NewReader(conn)
				for {
					line, err := br.ReadString('\n')
					if err != nil || strings.TrimSpace(line) == "" {
						break
					}
				}
				fmt.Fprint(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
			}()
		}
	}()

	controlAddr := freeAddr(t)
	httpAddr := freeAddr(t)

	srv := server.New(server.Config{
		ListenAddr: controlAddr,
		HTTPAddr:   httpAddr,
		Domain:     "tunnel.test",
		Secret:     "s",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	// Start the tunnel client.
	cli := client.New(client.Config{
		ServerAddr: controlAddr,
		Secret:     "s",
		Tunnels: []client.TunnelSpec{
			{Name: "myapp", LocalAddr: backend.Addr().String()},
		},
	})

	go cli.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	// Send an HTTP request through the tunnel's HTTP listener.
	conn, err := net.DialTimeout("tcp", httpAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial http listener: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	req := "GET / HTTP/1.1\r\nHost: myapp.tunnel.test\r\nConnection: close\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write request: %v", err)
	}

	resp, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	want := "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"
	if string(resp) != want {
		t.Errorf("response = %q, want %q", resp, want)
	}
}

// TestAuthRejected verifies that a bad secret is rejected.
func TestAuthRejected(t *testing.T) {
	controlAddr := freeAddr(t)

	srv := server.New(server.Config{
		ListenAddr: controlAddr,
		Secret:     "correct-secret",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	cli := client.New(client.Config{
		ServerAddr: controlAddr,
		Secret:     "wrong-secret",
		Tunnels: []client.TunnelSpec{
			{Name: "test", LocalAddr: "localhost:1234"},
		},
	})

	err := cli.Run(ctx)
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
}

// TestPortTunnel verifies raw TCP port-based tunneling.
func TestPortTunnel(t *testing.T) {
	// Local backend: reads first chunk, echoes it back, closes.
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()

	go func() {
		for {
			conn, err := backend.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				buf := make([]byte, 1024)
				n, err := conn.Read(buf)
				if err != nil {
					return
				}
				conn.Write([]byte("echo:" + string(buf[:n])))
			}()
		}
	}()

	controlAddr := freeAddr(t)
	tunnelAddr := freeAddr(t)
	// Parse just the port number.
	_, tunnelPortStr, _ := net.SplitHostPort(tunnelAddr)
	var tunnelPort int
	fmt.Sscanf(tunnelPortStr, "%d", &tunnelPort)

	srv := server.New(server.Config{
		ListenAddr: controlAddr,
		Secret:     "s",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	cli := client.New(client.Config{
		ServerAddr: controlAddr,
		Secret:     "s",
		Tunnels: []client.TunnelSpec{
			{RemotePort: uint16(tunnelPort), LocalAddr: backend.Addr().String()},
		},
	})

	go cli.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	// Connect through the tunnel port.
	conn, err := net.DialTimeout("tcp", tunnelAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial tunnel port: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	conn.Write([]byte("ping"))

	// Read the echo response. Backend writes and closes, so ReadAll works.
	resp, _ := io.ReadAll(conn)
	if string(resp) != "echo:ping" {
		t.Errorf("got %q, want %q", resp, "echo:ping")
	}
}
