package messaging

import "time"

// Envelope is the shared message schema used across all three binaries.
// It is serialized as JSON on the NATS subject wire.
type Envelope struct {
	// ID uniquely identifies this message, set by the publisher.
	ID string `json:"id"`

	// Text is the raw payload provided by the publisher.
	Text string `json:"text"`

	// PublishedAt is the UTC timestamp set by the publisher.
	PublishedAt time.Time `json:"published_at"`

	// Metadata holds key-value pairs added by intermediate processors.
	Metadata map[string]string `json:"metadata,omitempty"`
}
