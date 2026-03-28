package proto

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	Version    = 0x01
	HeaderSize = 6        // 1 version + 1 type + 4 length
	MaxPayload = 16 << 20 // 16 MB
)

const (
	MsgAuthRequest    uint8 = 0x01
	MsgAuthResponse   uint8 = 0x02
	MsgTunnelRequest  uint8 = 0x03
	MsgTunnelResponse uint8 = 0x04
	MsgTunnelClose    uint8 = 0x05
	MsgTunnelClosed   uint8 = 0x06
	MsgPing           uint8 = 0x07
	MsgPong           uint8 = 0x08
	MsgError          uint8 = 0x09
	MsgShutdown       uint8 = 0x0A
)

type AuthRequest struct {
	Token string `json:"token"`
}

type AuthResponse struct {
	OK       bool   `json:"ok"`
	ClientID string `json:"client_id,omitempty"`
	Error    string `json:"error,omitempty"`
}

type TunnelRequest struct {
	RequestedPort uint16 `json:"requested_port"`
	Name          string `json:"name,omitempty"`
	Description   string `json:"description,omitempty"`
}

type TunnelResponse struct {
	OK           bool   `json:"ok"`
	TunnelID     string `json:"tunnel_id,omitempty"`
	AssignedPort uint16 `json:"assigned_port,omitempty"`
	Name         string `json:"name,omitempty"`
	Error        string `json:"error,omitempty"`
}

type TunnelClose struct {
	TunnelID string `json:"tunnel_id"`
}

type TunnelClosed struct {
	TunnelID string `json:"tunnel_id"`
}

type Ping struct {
	Seq uint32 `json:"seq"`
}

type Pong struct {
	Seq uint32 `json:"seq"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Shutdown struct {
	Reason string `json:"reason"`
}

// WriteMessage writes a framed control message to w.
//
// Wire format: [version:1][type:1][length:4][json payload:N]
func WriteMessage(w io.Writer, msgType uint8, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	if len(data) > MaxPayload {
		return fmt.Errorf("payload too large: %d bytes (max %d)", len(data), MaxPayload)
	}

	var header [HeaderSize]byte
	header[0] = Version
	header[1] = msgType
	binary.BigEndian.PutUint32(header[2:6], uint32(len(data)))

	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}

// ReadMessage reads one framed control message from r.
func ReadMessage(r io.Reader) (uint8, json.RawMessage, error) {
	var header [HeaderSize]byte

	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, fmt.Errorf("read header: %w", err)
	}

	if header[0] != Version {
		return 0, nil, fmt.Errorf("unsupported protocol version: %d", header[0])
	}

	msgType := header[1]
	length := binary.BigEndian.Uint32(header[2:6])

	if length > MaxPayload {
		return 0, nil, fmt.Errorf("payload too large: %d bytes (max %d)", length, MaxPayload)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, fmt.Errorf("read payload: %w", err)
	}

	return msgType, json.RawMessage(payload), nil
}

func Decode[T any](raw json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(raw, &v)
	return v, err
}
