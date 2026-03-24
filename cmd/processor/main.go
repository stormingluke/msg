package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"msg/internal/messaging"
	"msg/internal/natsclient"
)

var (
	flagServer string
	flagCreds  string
	flagIn     string
	flagOut    string
	flagQueue  string
)

func main() {
	root := &cobra.Command{
		Use:   "processor",
		Short: "Read from one NATS subject, enrich messages, publish to another",
		Long: `Processor is the middle layer of the pipeline.

It subscribes to --in, applies DefaultEnhancer to each message (adding
word_count, processed_at, and processor_version metadata), then publishes
the enriched Envelope to --out.

Running multiple processor instances with the same --queue forms a
load-balanced consumer group.`,
		RunE: run,
	}

	root.Flags().StringVarP(&flagServer, "server", "s", "nats://localhost:4222", "NATS server URL")
	root.Flags().StringVar(&flagCreds, "creds", "", "Path to .creds file (from `nsc generate creds`)")
	root.Flags().StringVar(&flagIn, "in", "msg.raw", "Input subject to consume from")
	root.Flags().StringVar(&flagOut, "out", "msg.enhanced", "Output subject to publish enriched messages to")
	root.Flags().StringVar(&flagQueue, "queue", "processors", "Queue group name (allows multiple processor instances)")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	client, err := natsclient.New(natsclient.Config{
		ServerURL: flagServer,
		CredsFile: flagCreds,
		ConnName:  "processor",
	})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer client.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("processor running", "in", flagIn, "out", flagOut, "queue", flagQueue)
	slog.Info("press Ctrl+C to stop")

	return messaging.Process(ctx, client.Conn, flagIn, flagOut, flagQueue, messaging.DefaultEnhancer)
}
