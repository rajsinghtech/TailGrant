package tsapi

import (
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
			Scopes:       []string{"devices"},
		},
		UserAgent: "tailgrant",
	}
}

// NewClientWithAPIKey creates a Tailscale API client with a static API key.
func NewClientWithAPIKey(apiKey, tailnet string) *tailscale.Client {
	return &tailscale.Client{
		Tailnet:   tailnet,
		APIKey:    apiKey,
		UserAgent: "tailgrant",
	}
}
