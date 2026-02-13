package server

import (
	"context"
	"testing"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
)

func TestWhoIsFromContext_WithValue(t *testing.T) {
	whoIs := &apitype.WhoIsResponse{
		UserProfile: &tailcfg.UserProfile{
			LoginName: "user@example.com",
			ID:        123,
		},
		Node: &tailcfg.Node{
			StableID: tailcfg.StableNodeID("node-abc123"),
			Name:     "test-node",
		},
	}

	ctx := context.WithValue(context.Background(), whoIsContextKey, whoIs)

	result := WhoIsFromContext(ctx)
	if result == nil {
		t.Fatal("expected WhoIsResponse, got nil")
	}

	if result.UserProfile.LoginName != "user@example.com" {
		t.Errorf("expected LoginName %q, got %q", "user@example.com", result.UserProfile.LoginName)
	}

	if result.Node.StableID != tailcfg.StableNodeID("node-abc123") {
		t.Errorf("expected StableID %q, got %q", "node-abc123", result.Node.StableID)
	}
}

func TestWhoIsFromContext_NoValue(t *testing.T) {
	ctx := context.Background()

	result := WhoIsFromContext(ctx)
	if result != nil {
		t.Errorf("expected nil, got %+v", result)
	}
}

func TestWhoIsFromContext_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), whoIsContextKey, "not a WhoIsResponse")

	result := WhoIsFromContext(ctx)
	if result != nil {
		t.Errorf("expected nil for wrong type, got %+v", result)
	}
}

func TestWhoIsFromContext_DifferentKey(t *testing.T) {
	whoIs := &apitype.WhoIsResponse{
		UserProfile: &tailcfg.UserProfile{
			LoginName: "user@example.com",
		},
	}

	ctx := context.WithValue(context.Background(), contextKey("different-key"), whoIs)

	result := WhoIsFromContext(ctx)
	if result != nil {
		t.Errorf("expected nil for different context key, got %+v", result)
	}
}
