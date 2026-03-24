package messaging

import (
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
)

// Publish serializes env as JSON and publishes it to subject.
func Publish(conn *nats.Conn, subject string, env Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	if err := conn.Publish(subject, data); err != nil {
		return fmt.Errorf("nats publish to %q: %w", subject, err)
	}
	return nil
}
