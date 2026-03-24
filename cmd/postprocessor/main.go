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
		Use:   "postprocessor",
		Short: "Read from msg.enhanced, enrich further, publish to msg.final",
		Long: `Postprocessor is the second enrichment stage of the pipeline.

It subscribes to --in (msg.enhanced), applies PostprocessEnhancer to each
message (adding char_count, postprocessed_at, and postprocessor_version
metadata), then publishes the enriched Envelope to --out (msg.final).

Running multiple postprocessor instances with the same --queue forms a
load-balanced consumer group.`,
		RunE: run,
	}

	root.Flags().StringVarP(&flagServer, "server", "s", "nats://localhost:4222", "NATS server URL")
	root.Flags().StringVar(&flagCreds, "creds", "", "Path to .creds file (from `nsc generate creds`)")
	root.Flags().StringVar(&flagIn, "in", "msg.enhanced", "Input subject to consume from")
	root.Flags().StringVar(&flagOut, "out", "msg.final", "Output subject to publish enriched messages to")
	root.Flags().StringVar(&flagQueue, "queue", "postprocessors", "Queue group name (allows multiple postprocessor instances)")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	client, err := natsclient.New(natsclient.Config{
		ServerURL: flagServer,
		CredsFile: flagCreds,
		ConnName:  "postprocessor",
	})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer client.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("postprocessor running", "in", flagIn, "out", flagOut, "queue", flagQueue)
	slog.Info("press Ctrl+C to stop")

	return messaging.Process(ctx, client.Conn, flagIn, flagOut, flagQueue, messaging.PostprocessEnhancer)
}
