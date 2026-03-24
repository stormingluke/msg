package natsclient

import (
	"fmt"

	"github.com/nats-io/nats.go"
)

// Client wraps a *nats.Conn, providing a clean lifecycle interface.
type Client struct {
	Conn *nats.Conn
}

// New creates a connected NATS client using cfg.
// If cfg.CredsFile is non-empty, nats.UserCredentials is applied — this is
// the credential file produced by `nsc generate creds`.
// The caller must call Close() when done.
func New(cfg Config) (*Client, error) {
	opts := []nats.Option{
		nats.Name(cfg.ConnName),
	}
	if cfg.CredsFile != "" {
		opts = append(opts, nats.UserCredentials(cfg.CredsFile))
	}

	conn, err := nats.Connect(cfg.ServerURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats connect to %q: %w", cfg.ServerURL, err)
	}
	return &Client{Conn: conn}, nil
}

// Close drains the connection, flushing any pending messages before disconnecting.
func (c *Client) Close() {
	_ = c.Conn.Drain()
}
