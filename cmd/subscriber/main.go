package main

import (
	"context"
	"encoding/json"
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
	flagServer  string
	flagCreds   string
	flagSubject string
	flagQueue   string
)

func main() {
	root := &cobra.Command{
		Use:   "subscriber",
		Short: "Subscribe to a NATS subject and print received messages",
		RunE:  run,
	}

	root.Flags().StringVarP(&flagServer, "server", "s", "nats://localhost:4222", "NATS server URL")
	root.Flags().StringVar(&flagCreds, "creds", "", "Path to .creds file (from `nsc generate creds`)")
	root.Flags().StringVar(&flagSubject, "subject", "msg.raw", "Subject to subscribe to")
	root.Flags().StringVar(&flagQueue, "queue", "", "Queue group name for load-balanced subscriptions (optional)")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	client, err := natsclient.New(natsclient.Config{
		ServerURL: flagServer,
		CredsFile: flagCreds,
		ConnName:  "subscriber",
	})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer client.Close()

	sub, err := messaging.Subscribe(client.Conn, flagSubject, flagQueue, func(env messaging.Envelope) error {
		// Pretty-print the envelope so it's readable during development.
		out, _ := json.MarshalIndent(env, "", "  ")
		fmt.Println(string(out))
		return nil
	})
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	slog.Info("subscriber running", "subject", flagSubject, "queue", flagQueue)
	slog.Info("press Ctrl+C to stop")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	return nil
}
