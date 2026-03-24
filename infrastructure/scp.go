package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client is a thin wrapper around the Synadia Control Plane REST API.
// It covers the subset of endpoints needed to manage NATS users,
// permissions, and credentials — replacing the nsc CLI workflow.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// NewClient creates an SCP API client. BaseURL should include the /api
// prefix, e.g. "https://cloud.synadia.com/api".
func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL:    baseURL,
		Token:      token,
		HTTPClient: http.DefaultClient,
	}
}

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// Permission defines allow/deny subject lists for publish or subscribe.
type Permission struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// Permissions groups publish and subscribe permissions for a NATS user.
type Permissions struct {
	Pub *Permission `json:"pub,omitempty"`
	Sub *Permission `json:"sub,omitempty"`
}

// CreateUserRequest is the body for POST /core/beta/accounts/{accountId}/nats-users.
type CreateUserRequest struct {
	Name      string `json:"name"`
	SKGroupID string `json:"sk_group_id"`
}

// UpdateUserRequest is the body for PATCH /core/beta/nats-users/{userId}.
type UpdateUserRequest struct {
	JWTSettings *JWTSettingsPatch `json:"jwt_settings,omitempty"`
}

// JWTSettingsPatch carries the permission fields for a user update.
type JWTSettingsPatch struct {
	Pub *Permission `json:"pub,omitempty"`
	Sub *Permission `json:"sub,omitempty"`
}

// NatsUser is the relevant subset of NatsUserViewResponse.
type NatsUser struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	UserPublicKey string `json:"user_public_key"`
}

type natsUserListResponse struct {
	Items []NatsUser `json:"items"`
}

// AccountInfo is the relevant subset of AccountViewResponse.
type AccountInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type accountListResponse struct {
	Items []AccountInfo `json:"items"`
}

// --------------------------------------------------------------------------
// API methods
// --------------------------------------------------------------------------

// ListAccounts returns the accounts belonging to a system.
func (c *Client) ListAccounts(ctx context.Context, systemID string) ([]AccountInfo, error) {
	path := fmt.Sprintf("/core/beta/systems/%s/accounts", systemID)
	var resp accountListResponse
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	return resp.Items, nil
}

// ListUsers returns the NATS users belonging to an account.
func (c *Client) ListUsers(ctx context.Context, accountID string) ([]NatsUser, error) {
	path := fmt.Sprintf("/core/beta/accounts/%s/nats-users", accountID)
	var resp natsUserListResponse
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return resp.Items, nil
}

// GetUser returns a single NATS user by ID.
func (c *Client) GetUser(ctx context.Context, userID string) (*NatsUser, error) {
	path := fmt.Sprintf("/core/beta/nats-users/%s", userID)
	var user NatsUser
	if err := c.get(ctx, path, &user); err != nil {
		return nil, fmt.Errorf("get user %q: %w", userID, err)
	}
	return &user, nil
}

// CreateUser creates a new NATS user under the given account.
func (c *Client) CreateUser(ctx context.Context, accountID string, req CreateUserRequest) (*NatsUser, error) {
	path := fmt.Sprintf("/core/beta/accounts/%s/nats-users", accountID)
	var user NatsUser
	if err := c.post(ctx, path, req, &user); err != nil {
		return nil, fmt.Errorf("create user %q: %w", req.Name, err)
	}
	return &user, nil
}

// UpdateUserPermissions sets the publish and subscribe permissions for a user.
func (c *Client) UpdateUserPermissions(ctx context.Context, userID string, perms Permissions) error {
	path := fmt.Sprintf("/core/beta/nats-users/%s", userID)
	body := UpdateUserRequest{
		JWTSettings: &JWTSettingsPatch{
			Pub: perms.Pub,
			Sub: perms.Sub,
		},
	}
	if err := c.patch(ctx, path, body); err != nil {
		return fmt.Errorf("update permissions for %q: %w", userID, err)
	}
	return nil
}

// DownloadCreds downloads the .creds file content for a NATS user.
// The returned string is the full contents of the credentials file.
func (c *Client) DownloadCreds(ctx context.Context, userID string) (string, error) {
	path := fmt.Sprintf("/core/beta/nats-users/%s/creds", userID)
	url := c.BaseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download creds: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read creds body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download creds: HTTP %d: %s", resp.StatusCode, body)
	}
	return string(body), nil
}

// FindUserByName searches for a user by name within an account.
// Returns nil (not an error) if no user matches.
func (c *Client) FindUserByName(ctx context.Context, accountID, name string) (*NatsUser, error) {
	users, err := c.ListUsers(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if u.Name == name {
			return &u, nil
		}
	}
	return nil, nil
}

// --------------------------------------------------------------------------
// HTTP helpers
// --------------------------------------------------------------------------

func (c *Client) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.Token)
}

func (c *Client) get(ctx context.Context, path string, dest interface{}) error {
	url := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)
	return c.doJSON(req, dest)
}

func (c *Client) post(ctx context.Context, path string, body, dest interface{}) error {
	url := c.BaseURL + path
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")
	return c.doJSON(req, dest)
}

func (c *Client) patch(ctx context.Context, path string, body interface{}) error {
	url := c.BaseURL + path
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
	}
	return nil
}

func (c *Client) doJSON(req *http.Request, dest interface{}) error {
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	if dest != nil {
		if err := json.Unmarshal(body, dest); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
