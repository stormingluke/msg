package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

// EnhancerFunc transforms an inbound Envelope into an enriched one.
type EnhancerFunc func(in Envelope) (Envelope, error)

// DefaultEnhancer adds computed metadata fields that the publisher did not produce:
//   - word_count:         number of whitespace-separated words in Text
//   - processed_at:       UTC RFC3339 timestamp of when enhancement ran
//   - processor_version:  semver tag for the enrichment logic
func DefaultEnhancer(in Envelope) (Envelope, error) {
	out := in
	if out.Metadata == nil {
		out.Metadata = make(map[string]string)
	}
	out.Metadata["word_count"] = fmt.Sprintf("%d", len(strings.Fields(in.Text)))
	out.Metadata["processed_at"] = time.Now().UTC().Format(time.RFC3339)
	out.Metadata["processor_version"] = "1.0.0"
	return out, nil
}

// Process subscribes to inSubject (within queueGroup), applies enhancer to each
// received Envelope, and publishes the result to outSubject.
// It blocks until ctx is cancelled, then unsubscribes cleanly.
func Process(ctx context.Context, conn *nats.Conn, inSubject, outSubject, queueGroup string, enhancer EnhancerFunc) error {
	sub, err := Subscribe(conn, inSubject, queueGroup, func(env Envelope) error {
		enhanced, err := enhancer(env)
		if err != nil {
			return fmt.Errorf("enhance %q: %w", env.ID, err)
		}
		if err := Publish(conn, outSubject, enhanced); err != nil {
			return fmt.Errorf("publish enhanced %q: %w", env.ID, err)
		}
		slog.Info("processed", "id", env.ID, "in", inSubject, "out", outSubject)
		return nil
	})
	if err != nil {
		return fmt.Errorf("process: %w", err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	<-ctx.Done()
	return nil
}
