package proto

import (
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
)

func TestProxy_AToB(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()

	go Proxy(a2, b2)

	done := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(b1)
		done <- data
	}()

	a1.Write([]byte("hello from a"))
	a1.Close()

	got := <-done
	if string(got) != "hello from a" {
		t.Errorf("b got %q, want %q", got, "hello from a")
	}
}

func TestProxy_BToA(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()

	go Proxy(a2, b2)

	done := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(a1)
		done <- data
	}()

	b1.Write([]byte("hello from b"))
	b1.Close()

	got := <-done
	if string(got) != "hello from b" {
		t.Errorf("a got %q, want %q", got, "hello from b")
	}
}

func TestProxy_Bidirectional(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()

	go Proxy(a2, b2)

	// b1 acts as an echo server: reads, responds, closes.
	go func() {
		buf := make([]byte, 1024)
		n, _ := b1.Read(buf)
		b1.Write([]byte("echo:" + string(buf[:n])))
		b1.Close()
	}()

	a1.Write([]byte("ping"))
	// Don't close a1 — read the response first.
	got, _ := io.ReadAll(a1)
	if string(got) != "echo:ping" {
		t.Errorf("got %q, want %q", got, "echo:ping")
	}
}

func TestProxy_LargePayload(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()

	go Proxy(a2, b2)

	payload := strings.Repeat("x", 1<<20) // 1 MB

	done := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(b1)
		done <- data
	}()

	a1.Write([]byte(payload))
	a1.Close()

	got := <-done
	if len(got) != len(payload) {
		t.Errorf("got %d bytes, want %d", len(got), len(payload))
	}
}

func TestProxy_NilErrorOnCleanClose(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()

	done := make(chan error, 1)
	go func() {
		done <- Proxy(a2, b2)
	}()

	go io.ReadAll(b1)
	a1.Close()

	err := <-done
	if err != nil {
		t.Errorf("Proxy returned %v, want nil", err)
	}
}

func TestProxyHeader_VariousIDs(t *testing.T) {
	tests := []string{
		"t-client1-myapp",
		"t-client1-8080",
		"a",
		strings.Repeat("x", 255),
	}

	for _, id := range tests {
		var buf bytes.Buffer
		if err := WriteProxyHeader(&buf, ProxyHeader{TunnelID: id}); err != nil {
			t.Fatalf("WriteProxyHeader(%q): %v", id, err)
		}
		got, err := ReadProxyHeader(&buf)
		if err != nil {
			t.Fatalf("ReadProxyHeader: %v", err)
		}
		if got.TunnelID != id {
			t.Errorf("got TunnelID=%q, want %q", got.TunnelID, id)
		}
	}
}

func TestProxyHeader_TooLong(t *testing.T) {
	id := strings.Repeat("x", 256)
	var buf bytes.Buffer
	err := WriteProxyHeader(&buf, ProxyHeader{TunnelID: id})
	if err == nil {
		t.Fatal("expected error for 256-byte tunnel ID")
	}
}

func TestProxyHeader_BadVersion(t *testing.T) {
	buf := bytes.NewReader([]byte{0xFF, 5, 'h', 'e', 'l', 'l', 'o'})
	_, err := ReadProxyHeader(buf)
	if err == nil {
		t.Fatal("expected error for bad version")
	}
}

func TestProxyHeader_Truncated(t *testing.T) {
	buf := bytes.NewReader([]byte{Version})
	_, err := ReadProxyHeader(buf)
	if err == nil {
		t.Fatal("expected error for truncated header")
	}
}
