package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	configData := `
temporal:
  address: "localhost:7233"
  namespace: "tailgrant-prod"
  taskQueue: "tailgrant-queue"
tailscale:
  hostname: "tailgrant-server"
  stateDir: "/var/lib/tailscale"
  oauthClientID: "oauth-client-id"
  oauthClientSecret: "oauth-client-secret"
  tailnet: "example.com"
server:
  listenAddr: ":8080"
  useTLS: false
worker:
  ephemeral: true
  tags:
    - "tag:worker"
    - "tag:prod"
grants:
  - name: "ssh-access"
    description: "SSH access to production servers"
    tags:
      - "tag:ssh"
    maxDuration: "4h"
    riskLevel: "high"
    approvers:
      - "user1@example.com"
      - "user2@example.com"
  - name: "db-access"
    description: "Database read access"
    tags:
      - "tag:database"
    maxDuration: "1h"
    riskLevel: "medium"
    approvers:
      - "admin@example.com"
`

	if err := os.WriteFile(configPath, []byte(configData), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Temporal.Address != "localhost:7233" {
		t.Errorf("Temporal.Address = %q, want %q", cfg.Temporal.Address, "localhost:7233")
	}
	if cfg.Temporal.Namespace != "tailgrant-prod" {
		t.Errorf("Temporal.Namespace = %q, want %q", cfg.Temporal.Namespace, "tailgrant-prod")
	}
	if cfg.Temporal.TaskQueue != "tailgrant-queue" {
		t.Errorf("Temporal.TaskQueue = %q, want %q", cfg.Temporal.TaskQueue, "tailgrant-queue")
	}

	if cfg.Tailscale.Hostname != "tailgrant-server" {
		t.Errorf("Tailscale.Hostname = %q, want %q", cfg.Tailscale.Hostname, "tailgrant-server")
	}
	if cfg.Tailscale.StateDir != "/var/lib/tailscale" {
		t.Errorf("Tailscale.StateDir = %q, want %q", cfg.Tailscale.StateDir, "/var/lib/tailscale")
	}
	if cfg.Tailscale.OAuthClientID != "oauth-client-id" {
		t.Errorf("Tailscale.OAuthClientID = %q, want %q", cfg.Tailscale.OAuthClientID, "oauth-client-id")
	}
	if cfg.Tailscale.OAuthClientSecret != "oauth-client-secret" {
		t.Errorf("Tailscale.OAuthClientSecret = %q, want %q", cfg.Tailscale.OAuthClientSecret, "oauth-client-secret")
	}
	if cfg.Tailscale.Tailnet != "example.com" {
		t.Errorf("Tailscale.Tailnet = %q, want %q", cfg.Tailscale.Tailnet, "example.com")
	}

	if cfg.Server.ListenAddr != ":8080" {
		t.Errorf("Server.ListenAddr = %q, want %q", cfg.Server.ListenAddr, ":8080")
	}
	if cfg.Server.UseTLS == nil || *cfg.Server.UseTLS != false {
		t.Errorf("Server.UseTLS = %v, want false", cfg.Server.UseTLS)
	}

	if cfg.Worker.Ephemeral != true {
		t.Errorf("Worker.Ephemeral = %v, want %v", cfg.Worker.Ephemeral, true)
	}
	if len(cfg.Worker.Tags) != 2 {
		t.Errorf("len(Worker.Tags) = %d, want %d", len(cfg.Worker.Tags), 2)
	} else {
		if cfg.Worker.Tags[0] != "tag:worker" {
			t.Errorf("Worker.Tags[0] = %q, want %q", cfg.Worker.Tags[0], "tag:worker")
		}
		if cfg.Worker.Tags[1] != "tag:prod" {
			t.Errorf("Worker.Tags[1] = %q, want %q", cfg.Worker.Tags[1], "tag:prod")
		}
	}

	if len(cfg.Grants) != 2 {
		t.Fatalf("len(Grants) = %d, want %d", len(cfg.Grants), 2)
	}

	grant1 := cfg.Grants[0]
	if grant1.Name != "ssh-access" {
		t.Errorf("Grants[0].Name = %q, want %q", grant1.Name, "ssh-access")
	}
	if grant1.Description != "SSH access to production servers" {
		t.Errorf("Grants[0].Description = %q, want %q", grant1.Description, "SSH access to production servers")
	}
	if len(grant1.Tags) != 1 || grant1.Tags[0] != "tag:ssh" {
		t.Errorf("Grants[0].Tags = %v, want [tag:ssh]", grant1.Tags)
	}
	if grant1.MaxDuration != "4h" {
		t.Errorf("Grants[0].MaxDuration = %q, want %q", grant1.MaxDuration, "4h")
	}
	if grant1.RiskLevel != "high" {
		t.Errorf("Grants[0].RiskLevel = %q, want %q", grant1.RiskLevel, "high")
	}
	if len(grant1.Approvers) != 2 {
		t.Errorf("len(Grants[0].Approvers) = %d, want %d", len(grant1.Approvers), 2)
	}

	grant2 := cfg.Grants[1]
	if grant2.Name != "db-access" {
		t.Errorf("Grants[1].Name = %q, want %q", grant2.Name, "db-access")
	}
	if grant2.MaxDuration != "1h" {
		t.Errorf("Grants[1].MaxDuration = %q, want %q", grant2.MaxDuration, "1h")
	}
	if grant2.RiskLevel != "medium" {
		t.Errorf("Grants[1].RiskLevel = %q, want %q", grant2.RiskLevel, "medium")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	tempDir := t.TempDir()
	nonExistentPath := filepath.Join(tempDir, "nonexistent.yaml")

	_, err := Load(nonExistentPath)
	if err == nil {
		t.Fatal("Load() succeeded, want error for nonexistent file")
	}

	// Error is wrapped, just verify we got an error (already checked above)
	t.Logf("Load() correctly returned error: %v", err)
}

func TestLoad_InvalidYAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "invalid.yaml")

	invalidYAML := `
temporal:
  address: "localhost:7233"
  namespace: [invalid: yaml: structure
`

	if err := os.WriteFile(configPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Load() succeeded, want error for invalid YAML")
	}
}

func TestLoad_Defaults(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "minimal.yaml")

	minimalConfig := `
temporal:
  address: "localhost:7233"
tailscale:
  hostname: "tailgrant-server"
`

	if err := os.WriteFile(configPath, []byte(minimalConfig), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Temporal.Namespace != "default" {
		t.Errorf("default Temporal.Namespace = %q, want %q", cfg.Temporal.Namespace, "default")
	}
	if cfg.Temporal.TaskQueue != "tailgrant" {
		t.Errorf("default Temporal.TaskQueue = %q, want %q", cfg.Temporal.TaskQueue, "tailgrant")
	}
	if cfg.Server.ListenAddr != ":443" {
		t.Errorf("default Server.ListenAddr = %q, want %q", cfg.Server.ListenAddr, ":443")
	}
	if cfg.Server.UseTLS == nil || *cfg.Server.UseTLS != true {
		t.Errorf("default Server.UseTLS = %v, want true", cfg.Server.UseTLS)
	}
}

func TestLoad_EnvOverrideOAuth(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	configData := `
temporal:
  address: "localhost:7233"
tailscale:
  hostname: "tailgrant-server"
  oauthClientID: "yaml-client-id"
  oauthClientSecret: "yaml-client-secret"
  tailnet: "yaml-tailnet"
`

	if err := os.WriteFile(configPath, []byte(configData), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	envVars := map[string]string{
		"TS_OAUTH_CLIENT_ID":     "env-client-id",
		"TS_OAUTH_CLIENT_SECRET": "env-client-secret",
		"TS_TAILNET":             "env-tailnet",
	}
	for k, v := range envVars {
		if err := os.Setenv(k, v); err != nil {
			t.Fatalf("failed to set env var %s: %v", k, err)
		}
	}
	t.Cleanup(func() {
		for k := range envVars {
			os.Unsetenv(k)
		}
	})

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Tailscale.OAuthClientID != "env-client-id" {
		t.Errorf("OAuthClientID = %q, want %q", cfg.Tailscale.OAuthClientID, "env-client-id")
	}
	if cfg.Tailscale.OAuthClientSecret != "env-client-secret" {
		t.Errorf("OAuthClientSecret = %q, want %q", cfg.Tailscale.OAuthClientSecret, "env-client-secret")
	}
	if cfg.Tailscale.Tailnet != "env-tailnet" {
		t.Errorf("Tailnet = %q, want %q", cfg.Tailscale.Tailnet, "env-tailnet")
	}
}

func TestLoad_EnvOverrideEmpty(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	configData := `
temporal:
  address: "localhost:7233"
tailscale:
  hostname: "tailgrant-server"
  oauthClientID: "yaml-client-id"
  tailnet: "yaml-tailnet"
`

	if err := os.WriteFile(configPath, []byte(configData), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Ensure env vars are unset so YAML values are preserved
	os.Unsetenv("TS_OAUTH_CLIENT_ID")
	os.Unsetenv("TS_OAUTH_CLIENT_SECRET")
	os.Unsetenv("TS_TAILNET")
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Tailscale.OAuthClientID != "yaml-client-id" {
		t.Errorf("OAuthClientID = %q, want %q (YAML value should be preserved)", cfg.Tailscale.OAuthClientID, "yaml-client-id")
	}
	if cfg.Tailscale.Tailnet != "yaml-tailnet" {
		t.Errorf("Tailnet = %q, want %q (YAML value should be preserved)", cfg.Tailscale.Tailnet, "yaml-tailnet")
	}
}
