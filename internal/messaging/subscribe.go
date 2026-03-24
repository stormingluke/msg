package messaging

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
)

// HandlerFunc is called for each received Envelope.
type HandlerFunc func(env Envelope) error

// Subscribe registers a subscription on subject, optionally within queueGroup.
// Each message is decoded from JSON and passed to handler.
// When queueGroup is non-empty, NATS distributes messages across all subscribers
// in the group (useful for horizontal scaling).
// Returns the subscription so the caller can Unsubscribe.
func Subscribe(conn *nats.Conn, subject, queueGroup string, handler HandlerFunc) (*nats.Subscription, error) {
	cb := func(m *nats.Msg) {
		var env Envelope
		if err := json.Unmarshal(m.Data, &env); err != nil {
			slog.Error("decode envelope", "subject", subject, "err", err)
			return
		}
		if err := handler(env); err != nil {
			slog.Error("handler error", "id", env.ID, "err", err)
		}
	}

	var (
		sub *nats.Subscription
		err error
	)
	if queueGroup != "" {
		sub, err = conn.QueueSubscribe(subject, queueGroup, cb)
	} else {
		sub, err = conn.Subscribe(subject, cb)
	}
	if err != nil {
		return nil, fmt.Errorf("subscribe to %q: %w", subject, err)
	}
	return sub, nil
}
