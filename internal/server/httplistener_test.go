package server

import (
	"net"
	"testing"
)

func TestExtractSubdomain(t *testing.T) {
	h := NewHTTPListener(":0", "tunnel.dev", NewRouter())

	tests := []struct {
		host    string
		want    string
		wantErr bool
	}{
		{"myapp.tunnel.dev", "myapp", false},
		{"myapp.tunnel.dev:8080", "myapp", false},
		{"deep.nested.tunnel.dev", "deep.nested", false},
		{"deep.nested.tunnel.dev:443", "deep.nested", false},

		// bare domain, no subdomain
		{"tunnel.dev", "", true},
		{"tunnel.dev:8080", "", true},

		// wrong domain entirely
		{"myapp.other.dev", "", true},
		{"other.dev:8080", "", true},

		// empty
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got, err := h.extractSubdomain(tt.host)
			if (err != nil) != tt.wantErr {
				t.Fatalf("extractSubdomain(%q): err=%v, wantErr=%v", tt.host, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("extractSubdomain(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestPeekHTTPHost(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHost string
		wantErr  bool
	}{
		{
			name:     "standard GET",
			input:    "GET / HTTP/1.1\r\nHost: myapp.tunnel.dev\r\n\r\n",
			wantHost: "myapp.tunnel.dev",
		},
		{
			name:     "host with port",
			input:    "GET / HTTP/1.1\r\nHost: myapp.tunnel.dev:8080\r\n\r\n",
			wantHost: "myapp.tunnel.dev:8080",
		},
		{
			name:     "host among other headers",
			input:    "GET /path HTTP/1.1\r\nUser-Agent: test\r\nHost: api.tunnel.dev\r\nAccept: */*\r\n\r\n",
			wantHost: "api.tunnel.dev",
		},
		{
			name:     "lowercase host",
			input:    "GET / HTTP/1.1\r\nhost: lower.tunnel.dev\r\n\r\n",
			wantHost: "lower.tunnel.dev",
		},
		{
			name:    "no host header",
			input:   "GET / HTTP/1.1\r\nAccept: */*\r\n\r\n",
			wantErr: true,
		},
		{
			name:    "empty request",
			input:   "\r\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()

			go func() {
				b.Write([]byte(tt.input))
			}()

			info, err := peekHTTPHost(a)
			if (err != nil) != tt.wantErr {
				t.Fatalf("peekHTTPHost: err=%v, wantErr=%v", err, tt.wantErr)
			}
			if info.host != tt.wantHost {
				t.Errorf("peekHTTPHost host = %q, want %q", info.host, tt.wantHost)
			}
		})
	}
}

func TestInjectProxyHeaders(t *testing.T) {
	input := "GET / HTTP/1.1\r\nHost: myapp.tunnel.dev\r\n\r\n"
	got := string(injectProxyHeaders([]byte(input), "192.168.1.5:54321", "myapp.tunnel.dev"))

	want := "GET / HTTP/1.1\r\nHost: myapp.tunnel.dev\r\n" +
		"X-Forwarded-For: 192.168.1.5\r\n" +
		"X-Forwarded-Host: myapp.tunnel.dev\r\n" +
		"X-Forwarded-Proto: http\r\n" +
		"\r\n"

	if got != want {
		t.Errorf("injectProxyHeaders:\n got: %q\nwant: %q", got, want)
	}
}

func TestInjectProxyHeaders_WithBody(t *testing.T) {
	input := "POST /api HTTP/1.1\r\nHost: api.tunnel.dev\r\nContent-Length: 5\r\n\r\nhello"
	got := string(injectProxyHeaders([]byte(input), "10.0.0.1:9999", "api.tunnel.dev"))

	want := "POST /api HTTP/1.1\r\nHost: api.tunnel.dev\r\nContent-Length: 5\r\n" +
		"X-Forwarded-For: 10.0.0.1\r\n" +
		"X-Forwarded-Host: api.tunnel.dev\r\n" +
		"X-Forwarded-Proto: http\r\n" +
		"\r\nhello"

	if got != want {
		t.Errorf("injectProxyHeaders with body:\n got: %q\nwant: %q", got, want)
	}
}

func TestPeekHTTPHost_ReturnsAllBytes(t *testing.T) {
	// The header bytes returned must include everything read from the conn,
	// so the tunnel client can see the full original request.
	input := "GET /hello HTTP/1.1\r\nHost: myapp.tunnel.dev\r\n\r\n"

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	go func() {
		b.Write([]byte(input))
	}()

	info, err := peekHTTPHost(a)
	if err != nil {
		t.Fatal(err)
	}

	if string(info.headerBytes) != input {
		t.Errorf("header bytes = %q, want %q", info.headerBytes, input)
	}
	if info.method != "GET" {
		t.Errorf("method = %q, want GET", info.method)
	}
	if info.path != "/hello" {
		t.Errorf("path = %q, want /hello", info.path)
	}
}
