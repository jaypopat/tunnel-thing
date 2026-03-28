package proto

import (
	"fmt"
	"io"
)

// ProxyHeader is written once at the start of a data stream to tell the
// client which tunnel it belongs to.
// Wire format: [version:1][tunnel_id_len:1][tunnel_id:N]
type ProxyHeader struct {
	TunnelID string
}

func WriteProxyHeader(w io.Writer, h ProxyHeader) error {
	id := []byte(h.TunnelID)
	if len(id) > 255 {
		return fmt.Errorf("tunnel ID too long: %d bytes (max 255)", len(id))
	}

	buf := make([]byte, 2+len(id))
	buf[0] = Version
	buf[1] = byte(len(id))
	copy(buf[2:], id)

	_, err := w.Write(buf)
	return err
}

func ReadProxyHeader(r io.Reader) (ProxyHeader, error) {
	var prefix [2]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return ProxyHeader{}, fmt.Errorf("read proxy header: %w", err)
	}

	if prefix[0] != Version {
		return ProxyHeader{}, fmt.Errorf("unsupported proxy version: %d", prefix[0])
	}

	idLen := prefix[1]
	id := make([]byte, idLen)
	if _, err := io.ReadFull(r, id); err != nil {
		return ProxyHeader{}, fmt.Errorf("read tunnel ID: %w", err)
	}

	return ProxyHeader{TunnelID: string(id)}, nil
}
