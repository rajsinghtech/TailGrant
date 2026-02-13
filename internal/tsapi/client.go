package tsapi

import (
	"context"
	"fmt"

	tailscale "tailscale.com/client/tailscale/v2"
)

// NewClient creates a Tailscale API client configured with OAuth credentials.
// The v2 client handles token refresh internally via the OAuth transport.
func NewClient(clientID, clientSecret, tailnet string) *tailscale.Client {
	return &tailscale.Client{
		Tailnet: tailnet,
		Auth: &tailscale.OAuth{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       []string{"devices", "auth_keys"},
		},
		UserAgent: "tailgrant",
	}
}

// CreateAuthKey mints a short-lived, preauthorized, single-use auth key
// with the given tags. Used for tsnet node registration.
func CreateAuthKey(ctx context.Context, client *tailscale.Client, tags []string, ephemeral bool) (string, error) {
	caps := tailscale.KeyCapabilities{}
	caps.Devices.Create.Preauthorized = true
	caps.Devices.Create.Ephemeral = ephemeral
	caps.Devices.Create.Tags = tags

	key, err := client.Keys().CreateAuthKey(ctx, tailscale.CreateKeyRequest{
		Capabilities:  caps,
		ExpirySeconds: 300,
		Description:   "tailgrant tsnet bootstrap",
	})
	if err != nil {
		return "", fmt.Errorf("creating auth key: %w", err)
	}
	return key.Key, nil
}

// NewClientWithAPIKey creates a Tailscale API client with a static API key.
func NewClientWithAPIKey(apiKey, tailnet string) *tailscale.Client {
	return &tailscale.Client{
		Tailnet:   tailnet,
		APIKey:    apiKey,
		UserAgent: "tailgrant",
	}
}
