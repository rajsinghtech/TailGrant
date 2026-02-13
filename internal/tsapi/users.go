package tsapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	tailscale "tailscale.com/client/tailscale/v2"
)

// UserOperations provides HTTP wrappers for Tailscale user mutation endpoints
// that are not yet exposed by the upstream Go client (suspend, restore, role).
type UserOperations struct {
	client *tailscale.Client
}

// NewUserOperations creates a UserOperations that reuses the authenticated
// HTTP transport from the given tailscale.Client. Calling Users() triggers
// the client's lazy init so that BaseURL and HTTP are populated.
func NewUserOperations(client *tailscale.Client) *UserOperations {
	_ = client.Users() // trigger init
	return &UserOperations{client: client}
}

// SuspendUser suspends a user by ID.
// POST /api/v2/users/{id}/suspend
func (u *UserOperations) SuspendUser(ctx context.Context, userID string) error {
	return u.post(ctx, fmt.Sprintf("/api/v2/users/%s/suspend", userID), nil)
}

// RestoreUser restores a suspended user by ID.
// POST /api/v2/users/{id}/restore
func (u *UserOperations) RestoreUser(ctx context.Context, userID string) error {
	return u.post(ctx, fmt.Sprintf("/api/v2/users/%s/restore", userID), nil)
}

// SetUserRole updates a user's role.
// POST /api/v2/users/{id}/role
func (u *UserOperations) SetUserRole(ctx context.Context, userID string, role string) error {
	return u.post(ctx, fmt.Sprintf("/api/v2/users/%s/role", userID), map[string]string{"role": role})
}

func (u *UserOperations) post(ctx context.Context, path string, body any) error {
	urlStr := u.client.BaseURL.String() + path

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, reqBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if u.client.UserAgent != "" {
		req.Header.Set("User-Agent", u.client.UserAgent)
	}

	resp, err := u.client.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("tailscale API %s returned %d: %s", path, resp.StatusCode, string(respBody))
}
