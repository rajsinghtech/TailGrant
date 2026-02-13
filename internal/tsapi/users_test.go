package tsapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	tailscale "tailscale.com/client/tailscale/v2"
)

func setupTestServer(t *testing.T, handler http.HandlerFunc) (*tailscale.Client, *UserOperations) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	baseURL, _ := url.Parse(server.URL)
	client := &tailscale.Client{
		BaseURL:   baseURL,
		HTTP:      server.Client(),
		Tailnet:   "test-tailnet",
		UserAgent: "tailgrant-test",
	}

	ops := &UserOperations{client: client}
	return client, ops
}

func TestSuspendUser(t *testing.T) {
	var gotPath, gotMethod string
	_, ops := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	})

	err := ops.SuspendUser(context.Background(), "user-123")
	if err != nil {
		t.Fatalf("SuspendUser() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/api/v2/users/user-123/suspend" {
		t.Errorf("path = %s, want /api/v2/users/user-123/suspend", gotPath)
	}
}

func TestRestoreUser(t *testing.T) {
	var gotPath, gotMethod string
	_, ops := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	})

	err := ops.RestoreUser(context.Background(), "user-456")
	if err != nil {
		t.Fatalf("RestoreUser() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/api/v2/users/user-456/restore" {
		t.Errorf("path = %s, want /api/v2/users/user-456/restore", gotPath)
	}
}

func TestSetUserRole(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]string

	_, ops := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	})

	err := ops.SetUserRole(context.Background(), "user-789", "admin")
	if err != nil {
		t.Fatalf("SetUserRole() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/api/v2/users/user-789/role" {
		t.Errorf("path = %s, want /api/v2/users/user-789/role", gotPath)
	}
	if gotBody["role"] != "admin" {
		t.Errorf("body role = %q, want %q", gotBody["role"], "admin")
	}
}

func TestSetUserRole_ContentType(t *testing.T) {
	var gotContentType string
	_, ops := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	})

	_ = ops.SetUserRole(context.Background(), "user-1", "member")
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}
}

func TestSuspendUser_APIError(t *testing.T) {
	_, ops := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	})

	err := ops.SuspendUser(context.Background(), "user-bad")
	if err == nil {
		t.Fatal("SuspendUser() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want to contain 403", err)
	}
}

func TestUserAgent(t *testing.T) {
	var gotUA string
	_, ops := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	})

	_ = ops.RestoreUser(context.Background(), "user-1")
	if gotUA != "tailgrant-test" {
		t.Errorf("User-Agent = %q, want %q", gotUA, "tailgrant-test")
	}
}
