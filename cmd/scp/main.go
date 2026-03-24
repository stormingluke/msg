package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"msg/infrastructure"
)

var (
	flagBaseURL string
	flagToken   string
)

func main() {
	root := &cobra.Command{
		Use:   "scp",
		Short: "Manage NATS users and credentials via the Synadia Control Plane API",
		Long: `scp replaces the nsc CLI workflow by talking directly to the
Synadia Control Plane REST API. It can create users, set subject
permissions, and download .creds files — everything needed to
onboard a new microservice.`,
	}

	root.PersistentFlags().StringVar(&flagBaseURL, "base-url", "", "SCP API base URL (e.g. https://cloud.synadia.com/api)")
	root.PersistentFlags().StringVar(&flagToken, "token", "", "SCP bearer token")
	root.MarkPersistentFlagRequired("base-url") //nolint:errcheck
	root.MarkPersistentFlagRequired("token")    //nolint:errcheck

	root.AddCommand(
		newCreateUserCmd(),
		newUpdatePermissionsCmd(),
		newDownloadCredsCmd(),
		newListUsersCmd(),
		newDescribeUserCmd(),
		newSetupCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newSCPClient() *infrastructure.Client {
	return infrastructure.NewClient(flagBaseURL, flagToken)
}

// --------------------------------------------------------------------------
// create-user
// --------------------------------------------------------------------------

func newCreateUserCmd() *cobra.Command {
	var (
		account string
		name    string
		skGroup string
	)
	cmd := &cobra.Command{
		Use:   "create-user",
		Short: "Create a new NATS user in an account",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newSCPClient()
			user, err := client.CreateUser(cmd.Context(), account, infrastructure.CreateUserRequest{
				Name:      name,
				SKGroupID: skGroup,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Created user %q (id: %s)\n", user.Name, user.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&account, "account", "", "Account ID")
	cmd.Flags().StringVar(&name, "name", "", "User name")
	cmd.Flags().StringVar(&skGroup, "sk-group", "", "Signing key group ID")
	cmd.MarkFlagRequired("account")  //nolint:errcheck
	cmd.MarkFlagRequired("name")     //nolint:errcheck
	cmd.MarkFlagRequired("sk-group") //nolint:errcheck
	return cmd
}

// --------------------------------------------------------------------------
// update-permissions
// --------------------------------------------------------------------------

func newUpdatePermissionsCmd() *cobra.Command {
	var (
		userID   string
		allowPub []string
		allowSub []string
		denyPub  []string
		denySub  []string
	)
	cmd := &cobra.Command{
		Use:   "update-permissions",
		Short: "Set publish/subscribe permissions for a NATS user",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newSCPClient()
			perms := infrastructure.Permissions{}
			if len(allowPub) > 0 || len(denyPub) > 0 {
				perms.Pub = &infrastructure.Permission{Allow: allowPub, Deny: denyPub}
			}
			if len(allowSub) > 0 || len(denySub) > 0 {
				perms.Sub = &infrastructure.Permission{Allow: allowSub, Deny: denySub}
			}
			if err := client.UpdateUserPermissions(cmd.Context(), userID, perms); err != nil {
				return err
			}
			fmt.Printf("Updated permissions for user %s\n", userID)
			return nil
		},
	}
	cmd.Flags().StringVar(&userID, "user", "", "User ID")
	cmd.Flags().StringSliceVar(&allowPub, "allow-pub", nil, "Subjects allowed to publish (comma-separated)")
	cmd.Flags().StringSliceVar(&allowSub, "allow-sub", nil, "Subjects allowed to subscribe (comma-separated)")
	cmd.Flags().StringSliceVar(&denyPub, "deny-pub", nil, "Subjects denied for publish (comma-separated)")
	cmd.Flags().StringSliceVar(&denySub, "deny-sub", nil, "Subjects denied for subscribe (comma-separated)")
	cmd.MarkFlagRequired("user") //nolint:errcheck
	return cmd
}

// --------------------------------------------------------------------------
// download-creds
// --------------------------------------------------------------------------

func newDownloadCredsCmd() *cobra.Command {
	var (
		userID string
		output string
	)
	cmd := &cobra.Command{
		Use:   "download-creds",
		Short: "Download a .creds file for a NATS user",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newSCPClient()
			creds, err := client.DownloadCreds(cmd.Context(), userID)
			if err != nil {
				return err
			}
			if output == "-" || output == "" {
				fmt.Print(creds)
				return nil
			}
			if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
				return fmt.Errorf("create output directory: %w", err)
			}
			if err := os.WriteFile(output, []byte(creds), 0o600); err != nil {
				return fmt.Errorf("write creds: %w", err)
			}
			fmt.Printf("Credentials written to %s\n", output)
			return nil
		},
	}
	cmd.Flags().StringVar(&userID, "user", "", "User ID")
	cmd.Flags().StringVarP(&output, "output", "o", "-", "Output file path (- for stdout)")
	cmd.MarkFlagRequired("user") //nolint:errcheck
	return cmd
}

// --------------------------------------------------------------------------
// list-users
// --------------------------------------------------------------------------

func newListUsersCmd() *cobra.Command {
	var account string
	cmd := &cobra.Command{
		Use:   "list-users",
		Short: "List NATS users in an account",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newSCPClient()
			users, err := client.ListUsers(cmd.Context(), account)
			if err != nil {
				return err
			}
			if len(users) == 0 {
				fmt.Println("No users found.")
				return nil
			}
			fmt.Printf("%-36s  %s\n", "ID", "NAME")
			for _, u := range users {
				fmt.Printf("%-36s  %s\n", u.ID, u.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&account, "account", "", "Account ID")
	cmd.MarkFlagRequired("account") //nolint:errcheck
	return cmd
}

// --------------------------------------------------------------------------
// describe-user
// --------------------------------------------------------------------------

func newDescribeUserCmd() *cobra.Command {
	var userID string
	cmd := &cobra.Command{
		Use:   "describe-user",
		Short: "Show details for a NATS user",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newSCPClient()
			user, err := client.GetUser(cmd.Context(), userID)
			if err != nil {
				return err
			}
			out, _ := json.MarshalIndent(user, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
	cmd.Flags().StringVar(&userID, "user", "", "User ID")
	cmd.MarkFlagRequired("user") //nolint:errcheck
	return cmd
}

// --------------------------------------------------------------------------
// setup — high-level orchestration
// --------------------------------------------------------------------------

// pipelineUser defines a user to create with its subject permissions.
type pipelineUser struct {
	Name     string
	AllowPub []string
	AllowSub []string
	DenyPub  []string
	DenySub  []string
}

// pipelineUsers is the hardcoded set of users for the msg pipeline,
// matching the permissions in the Makefile setup-nsc target.
var pipelineUsers = []pipelineUser{
	{
		Name:     "publisher",
		AllowPub: []string{"msg.raw", "_INBOX.>"},
		AllowSub: []string{"_INBOX.>"},
	},
	{
		Name:     "subscriber",
		AllowPub: []string{"_INBOX.>"},
		AllowSub: []string{"msg.raw", "msg.enhanced", "msg.final", "_INBOX.>"},
	},
	{
		Name:     "processor",
		AllowPub: []string{"msg.enhanced", "_INBOX.>"},
		AllowSub: []string{"msg.raw", "_INBOX.>"},
	},
	{
		Name:     "postprocessor",
		AllowPub: []string{"msg.final", "_INBOX.>"},
		AllowSub: []string{"msg.enhanced", "_INBOX.>"},
	},
}

func newSetupCmd() *cobra.Command {
	var (
		systemID  string
		accountID string
		skGroupID string
		credsDir  string
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Create all pipeline users with permissions and download creds",
		Long: `Setup creates the publisher, subscriber, processor, and postprocessor
users in the given account, configures their subject permissions, and
downloads .creds files to the specified directory.

This is the SCP equivalent of running 'make setup-nsc && make creds'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newSCPClient()
			ctx := cmd.Context()

			if err := os.MkdirAll(credsDir, 0o755); err != nil {
				return fmt.Errorf("create creds directory: %w", err)
			}

			for _, pu := range pipelineUsers {
				if err := setupUser(ctx, client, accountID, skGroupID, credsDir, pu); err != nil {
					return err
				}
			}

			slog.Info("setup complete",
				"users", len(pipelineUsers),
				"creds_dir", credsDir,
			)
			fmt.Println()
			fmt.Println("Pipeline users created. Run the pipeline with:")
			fmt.Println()
			fmt.Println("  T1: make run-server")
			fmt.Println("  T2: make run-subscriber")
			fmt.Println("  T3: make run-processor")
			fmt.Println("  T4: make run-postprocessor")
			fmt.Println("  T5: make run-publisher")
			return nil
		},
	}
	cmd.Flags().StringVar(&systemID, "system", "", "System ID")
	cmd.Flags().StringVar(&accountID, "account", "", "Account ID")
	cmd.Flags().StringVar(&skGroupID, "sk-group", "", "Signing key group ID")
	cmd.Flags().StringVar(&credsDir, "creds-dir", "creds", "Directory to write .creds files")
	cmd.MarkFlagRequired("system")   //nolint:errcheck
	cmd.MarkFlagRequired("account")  //nolint:errcheck
	cmd.MarkFlagRequired("sk-group") //nolint:errcheck
	return cmd
}

func setupUser(ctx context.Context, client *infrastructure.Client, accountID, skGroupID, credsDir string, pu pipelineUser) error {
	// Check if user already exists.
	existing, err := client.FindUserByName(ctx, accountID, pu.Name)
	if err != nil {
		return fmt.Errorf("check existing user %q: %w", pu.Name, err)
	}

	var user *infrastructure.NatsUser
	if existing != nil {
		slog.Info("user already exists, updating permissions", "name", pu.Name, "id", existing.ID)
		user = existing
	} else {
		slog.Info("creating user", "name", pu.Name)
		user, err = client.CreateUser(ctx, accountID, infrastructure.CreateUserRequest{
			Name:      pu.Name,
			SKGroupID: skGroupID,
		})
		if err != nil {
			return fmt.Errorf("create user %q: %w", pu.Name, err)
		}
		slog.Info("created user", "name", pu.Name, "id", user.ID)
	}

	// Set permissions.
	perms := infrastructure.Permissions{}
	if len(pu.AllowPub) > 0 || len(pu.DenyPub) > 0 {
		perms.Pub = &infrastructure.Permission{Allow: pu.AllowPub, Deny: pu.DenyPub}
	}
	if len(pu.AllowSub) > 0 || len(pu.DenySub) > 0 {
		perms.Sub = &infrastructure.Permission{Allow: pu.AllowSub, Deny: pu.DenySub}
	}
	if err := client.UpdateUserPermissions(ctx, user.ID, perms); err != nil {
		return fmt.Errorf("set permissions for %q: %w", pu.Name, err)
	}
	slog.Info("permissions set",
		"name", pu.Name,
		"allow_pub", strings.Join(pu.AllowPub, ","),
		"allow_sub", strings.Join(pu.AllowSub, ","),
	)

	// Download creds.
	creds, err := client.DownloadCreds(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("download creds for %q: %w", pu.Name, err)
	}
	credsPath := filepath.Join(credsDir, pu.Name+".creds")
	if err := os.WriteFile(credsPath, []byte(creds), 0o600); err != nil {
		return fmt.Errorf("write creds for %q: %w", pu.Name, err)
	}
	slog.Info("credentials saved", "name", pu.Name, "path", credsPath)

	return nil
}
