package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rajsinghtech/tailgrant/internal/grant"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
)

type mockGrantTypeStore struct {
	types map[string]*grant.GrantType
	err   error
}

func (m *mockGrantTypeStore) Get(name string) (*grant.GrantType, error) {
	if m.err != nil {
		return nil, m.err
	}
	gt, ok := m.types[name]
	if !ok {
		return nil, errors.New("grant type not found: " + name)
	}
	return gt, nil
}

func (m *mockGrantTypeStore) List() ([]*grant.GrantType, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []*grant.GrantType
	for _, gt := range m.types {
		result = append(result, gt)
	}
	return result, nil
}

func newMockGrantTypeStore() *mockGrantTypeStore {
	return &mockGrantTypeStore{
		types: map[string]*grant.GrantType{
			"ssh-access": {
				Name:        "ssh-access",
				Description: "SSH access to production servers",
				Tags:        []string{"tag:ssh", "tag:prod"},
				MaxDuration: grant.JSONDuration(2 * time.Hour),
				RiskLevel:   grant.RiskMedium,
				Approvers:   []string{"admin@example.com"},
			},
			"db-access": {
				Name:        "db-access",
				Description: "Database read access",
				Tags:        []string{"tag:db", "tag:read"},
				MaxDuration: grant.JSONDuration(1 * time.Hour),
				RiskLevel:   grant.RiskHigh,
				Approvers:   []string{"admin@example.com", "dba@example.com"},
			},
		},
	}
}

func withWhoIs(req *http.Request, loginName, nodeID string) *http.Request {
	whoIs := &apitype.WhoIsResponse{
		UserProfile: &tailcfg.UserProfile{
			LoginName: loginName,
		},
		Node: &tailcfg.Node{
			StableID: tailcfg.StableNodeID(nodeID),
		},
	}
	ctx := context.WithValue(req.Context(), whoIsContextKey, whoIs)
	return req.WithContext(ctx)
}

func TestHandleListGrantTypes(t *testing.T) {
	store := newMockGrantTypeStore()
	handlers := &Handlers{
		GrantTypes: store,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/grant-types", nil)
	w := httptest.NewRecorder()

	handlers.HandleListGrantTypes(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var types []*grant.GrantType
	if err := json.NewDecoder(w.Body).Decode(&types); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(types) != 2 {
		t.Errorf("expected 2 grant types, got %d", len(types))
	}
}

func TestHandleListGrantTypes_StoreError(t *testing.T) {
	store := &mockGrantTypeStore{
		err: errors.New("database error"),
	}
	handlers := &Handlers{
		GrantTypes: store,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/grant-types", nil)
	w := httptest.NewRecorder()

	handlers.HandleListGrantTypes(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"] != "database error" {
		t.Errorf("expected error %q, got %q", "database error", resp["error"])
	}
}

func TestHandleListDevices_NilClient(t *testing.T) {
	handlers := &Handlers{}

	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	w := httptest.NewRecorder()

	handlers.HandleListDevices(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"] != "tailscale API client not configured" {
		t.Errorf("expected error about unconfigured client, got %q", resp["error"])
	}
}

func TestHandleCreateGrant_MissingIdentity(t *testing.T) {
	handlers := &Handlers{
		GrantTypes: newMockGrantTypeStore(),
	}

	body := map[string]string{
		"grantTypeName": "ssh-access",
		"targetNodeID":  "node-456",
		"duration":      "1h",
		"reason":        "Deploy hotfix",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/grants", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	handlers.HandleCreateGrant(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"] != "missing identity" {
		t.Errorf("expected error %q, got %q", "missing identity", resp["error"])
	}
}

func TestHandleCreateGrant_InvalidBody(t *testing.T) {
	handlers := &Handlers{
		GrantTypes: newMockGrantTypeStore(),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/grants", bytes.NewReader([]byte("invalid json")))
	req = withWhoIs(req, "user@example.com", "node-123")
	w := httptest.NewRecorder()

	handlers.HandleCreateGrant(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"] == "" {
		t.Error("expected error message about invalid request body")
	}
}

func TestHandleCreateGrant_UnknownGrantType(t *testing.T) {
	handlers := &Handlers{
		GrantTypes: newMockGrantTypeStore(),
	}

	body := map[string]string{
		"grantTypeName": "unknown-grant",
		"targetNodeID":  "node-456",
		"duration":      "1h",
		"reason":        "Test reason",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/grants", bytes.NewReader(bodyBytes))
	req = withWhoIs(req, "user@example.com", "node-123")
	w := httptest.NewRecorder()

	handlers.HandleCreateGrant(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"] != "grant type not found: unknown-grant" {
		t.Errorf("expected error about unknown grant type, got %q", resp["error"])
	}
}

func TestHandleCreateGrant_InvalidDuration(t *testing.T) {
	handlers := &Handlers{
		GrantTypes: newMockGrantTypeStore(),
	}

	body := map[string]string{
		"grantTypeName": "ssh-access",
		"targetNodeID":  "node-456",
		"duration":      "invalid-duration",
		"reason":        "Test reason",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/grants", bytes.NewReader(bodyBytes))
	req = withWhoIs(req, "user@example.com", "node-123")
	w := httptest.NewRecorder()

	handlers.HandleCreateGrant(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"] == "" {
		t.Error("expected error message about invalid duration")
	}
}

func TestHandleCreateGrant_DurationExceedsMax(t *testing.T) {
	handlers := &Handlers{
		GrantTypes: newMockGrantTypeStore(),
	}

	body := map[string]string{
		"grantTypeName": "ssh-access",
		"targetNodeID":  "node-456",
		"duration":      "5h",
		"reason":        "Test reason",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/grants", bytes.NewReader(bodyBytes))
	req = withWhoIs(req, "user@example.com", "node-123")
	w := httptest.NewRecorder()

	handlers.HandleCreateGrant(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"] == "" {
		t.Error("expected error message about duration exceeding max")
	}
}

func TestHandleApproveGrant_MissingIdentity(t *testing.T) {
	handlers := &Handlers{}

	req := httptest.NewRequest(http.MethodPost, "/api/grants/test-id/approve", nil)
	req.SetPathValue("id", "test-id")
	w := httptest.NewRecorder()

	handlers.HandleApproveGrant(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"] != "missing identity" {
		t.Errorf("expected error %q, got %q", "missing identity", resp["error"])
	}
}

func TestHandleDenyGrant_MissingIdentity(t *testing.T) {
	handlers := &Handlers{}

	body := map[string]string{"reason": "Security concern"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/grants/test-id/deny", bytes.NewReader(bodyBytes))
	req.SetPathValue("id", "test-id")
	w := httptest.NewRecorder()

	handlers.HandleDenyGrant(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"] != "missing identity" {
		t.Errorf("expected error %q, got %q", "missing identity", resp["error"])
	}
}

func TestHandleRevokeGrant_MissingIdentity(t *testing.T) {
	handlers := &Handlers{}

	body := map[string]string{"reason": "No longer needed"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/grants/test-id/revoke", bytes.NewReader(bodyBytes))
	req.SetPathValue("id", "test-id")
	w := httptest.NewRecorder()

	handlers.HandleRevokeGrant(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["error"] != "missing identity" {
		t.Errorf("expected error %q, got %q", "missing identity", resp["error"])
	}
}

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		data     any
		wantCode int
	}{
		{
			name:     "map data",
			status:   http.StatusOK,
			data:     map[string]string{"key": "value"},
			wantCode: http.StatusOK,
		},
		{
			name:     "struct data",
			status:   http.StatusCreated,
			data:     struct{ Name string }{Name: "test"},
			wantCode: http.StatusCreated,
		},
		{
			name:     "error status",
			status:   http.StatusBadRequest,
			data:     map[string]string{"error": "bad request"},
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeJSON(w, tt.status, tt.data)

			if w.Code != tt.wantCode {
				t.Errorf("expected status %d, got %d", tt.wantCode, w.Code)
			}

			contentType := w.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("expected Content-Type %q, got %q", "application/json", contentType)
			}

			var decoded map[string]any
			if err := json.NewDecoder(w.Body).Decode(&decoded); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
		})
	}
}

func TestWriteError(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
	}{
		{
			name:    "bad request",
			status:  http.StatusBadRequest,
			message: "invalid input",
		},
		{
			name:    "unauthorized",
			status:  http.StatusUnauthorized,
			message: "missing identity",
		},
		{
			name:    "internal error",
			status:  http.StatusInternalServerError,
			message: "something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeError(w, tt.status, tt.message)

			if w.Code != tt.status {
				t.Errorf("expected status %d, got %d", tt.status, w.Code)
			}

			var resp map[string]string
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if resp["error"] != tt.message {
				t.Errorf("expected error %q, got %q", tt.message, resp["error"])
			}
		})
	}
}
