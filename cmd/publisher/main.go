package main

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
	"msg/internal/messaging"
	"msg/internal/natsclient"
)

var (
	flagServer   string
	flagCreds    string
	flagSubject  string
	flagMessage  string
	flagCount    int
	flagInterval time.Duration
)

func main() {
	root := &cobra.Command{
		Use:   "publisher",
		Short: "Publish messages to a NATS subject",
		RunE:  run,
	}

	root.Flags().StringVarP(&flagServer, "server", "s", "nats://localhost:4222", "NATS server URL")
	root.Flags().StringVar(&flagCreds, "creds", "", "Path to .creds file (from `nsc generate creds`)")
	root.Flags().StringVar(&flagSubject, "subject", "msg.raw", "Subject to publish to")
	root.Flags().StringVarP(&flagMessage, "message", "m", "hello from publisher", "Text payload to send")
	root.Flags().IntVarP(&flagCount, "count", "n", 1, "Number of messages to publish")
	root.Flags().DurationVar(&flagInterval, "interval", 0, "Delay between messages when count > 1 (e.g. 500ms)")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	client, err := natsclient.New(natsclient.Config{
		ServerURL: flagServer,
		CredsFile: flagCreds,
		ConnName:  "publisher",
	})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer client.Close()

	for i := range flagCount {
		env := messaging.Envelope{
			ID:          newID(),
			Text:        flagMessage,
			PublishedAt: time.Now().UTC(),
		}
		if err := messaging.Publish(client.Conn, flagSubject, env); err != nil {
			return fmt.Errorf("publish #%d: %w", i+1, err)
		}
		slog.Info("published", "id", env.ID, "subject", flagSubject, "n", i+1)

		if flagInterval > 0 && i < flagCount-1 {
			time.Sleep(flagInterval)
		}
	}
	return nil
}

// newID returns a random 8-byte hex string suitable for message IDs.
func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
