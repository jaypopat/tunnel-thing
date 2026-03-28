package proto

import (
	"bytes"
	"testing"
)

func TestWriteReadMessage_RoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		msgType uint8
		payload any
	}{
		{"tunnel request", MsgTunnelRequest, TunnelRequest{RequestedPort: 9090, Description: "my-app"}},
		{"tunnel response", MsgTunnelResponse, TunnelResponse{OK: true, TunnelID: "t-abc", AssignedPort: 9090}},
		{"auth request", MsgAuthRequest, AuthRequest{Token: "sk-secret"}},
		{"auth response ok", MsgAuthResponse, AuthResponse{OK: true, ClientID: "c-123"}},
		{"auth response fail", MsgAuthResponse, AuthResponse{OK: false, Error: "bad token"}},
		{"ping", MsgPing, Ping{Seq: 42}},
		{"error", MsgError, Error{Code: "PORT_UNAVAILABLE", Message: "port 9090 in use"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			if err := WriteMessage(&buf, tt.msgType, tt.payload); err != nil {
				t.Fatalf("WriteMessage: %v", err)
			}

			gotType, gotRaw, err := ReadMessage(&buf)
			if err != nil {
				t.Fatalf("ReadMessage: %v", err)
			}

			if gotType != tt.msgType {
				t.Errorf("type = 0x%02x, want 0x%02x", gotType, tt.msgType)
			}

			if len(gotRaw) == 0 {
				t.Fatal("payload is empty")
			}
		})
	}
}

func TestWriteReadMessage_DecodePayload(t *testing.T) {
	var buf bytes.Buffer
	want := TunnelRequest{RequestedPort: 8080, Description: "test"}

	if err := WriteMessage(&buf, MsgTunnelRequest, want); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	_, raw, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	got, err := Decode[TunnelRequest](raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.RequestedPort != want.RequestedPort || got.Description != want.Description {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestWriteReadMessage_BadVersion(t *testing.T) {
	// Manually craft a header with wrong version byte.
	header := []byte{0xFF, MsgPing, 0, 0, 0, 2, '{', '}'}
	_, _, err := ReadMessage(bytes.NewReader(header))
	if err == nil {
		t.Fatal("expected error for bad version")
	}
}

func TestWriteReadMessage_MultipleMessages(t *testing.T) {
	var buf bytes.Buffer

	// Write two messages back-to-back — verifies framing boundaries.
	WriteMessage(&buf, MsgPing, Ping{Seq: 1})
	WriteMessage(&buf, MsgPong, Pong{Seq: 1})

	typ1, _, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("first ReadMessage: %v", err)
	}
	if typ1 != MsgPing {
		t.Errorf("first type = 0x%02x, want 0x%02x", typ1, MsgPing)
	}

	typ2, _, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("second ReadMessage: %v", err)
	}
	if typ2 != MsgPong {
		t.Errorf("second type = 0x%02x, want 0x%02x", typ2, MsgPong)
	}
}

func TestProxyHeader_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := ProxyHeader{TunnelID: "t-abc123"}

	if err := WriteProxyHeader(&buf, want); err != nil {
		t.Fatalf("WriteProxyHeader: %v", err)
	}

	got, err := ReadProxyHeader(&buf)
	if err != nil {
		t.Fatalf("ReadProxyHeader: %v", err)
	}

	if got.TunnelID != want.TunnelID {
		t.Errorf("TunnelID = %q, want %q", got.TunnelID, want.TunnelID)
	}
}
