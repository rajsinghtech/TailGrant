package tsapi

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	clientID := "test-client-id"
	clientSecret := "test-client-secret"
	tailnet := "example.com"

	client := NewClient(clientID, clientSecret, tailnet)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.Tailnet != tailnet {
		t.Errorf("expected Tailnet to be %q, got %q", tailnet, client.Tailnet)
	}

	if client.UserAgent != "tailgrant" {
		t.Errorf("expected UserAgent to be %q, got %q", "tailgrant", client.UserAgent)
	}

	if client.Auth == nil {
		t.Error("expected Auth to be non-nil")
	}
}

func TestNewClientWithAPIKey(t *testing.T) {
	apiKey := "test-api-key"
	tailnet := "example.com"

	client := NewClientWithAPIKey(apiKey, tailnet)

	if client == nil {
		t.Fatal("NewClientWithAPIKey returned nil")
	}

	if client.Tailnet != tailnet {
		t.Errorf("expected Tailnet to be %q, got %q", tailnet, client.Tailnet)
	}

	if client.APIKey != apiKey {
		t.Errorf("expected APIKey to be %q, got %q", apiKey, client.APIKey)
	}

	if client.UserAgent != "tailgrant" {
		t.Errorf("expected UserAgent to be %q, got %q", "tailgrant", client.UserAgent)
	}
}
