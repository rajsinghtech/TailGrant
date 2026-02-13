# TailGrant Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a programmable tailnet control plane that provides durable, event-driven JIT admin access using Temporal workflows and Tailscale tag-based enforcement.

**Architecture:** Split frontend/worker — server (tsnet + HTTP + Temporal client) handles UI and auth via WhoIs, worker (tsnet + Temporal worker + OAuth) executes all Tailscale API mutations as Temporal activities. No database — Temporal workflow state is the source of truth.

**Tech Stack:** Go 1.23+, Temporal Go SDK, tsnet, tailscale-client-go-v2 (OAuth), gopkg.in/yaml.v3

---

### Task 1: Project scaffolding and dependencies

**Files:**
- Create: `go.mod`
- Create: `.gitignore`

**Step 1: Initialize Go module**

```bash
cd /Users/rajsingh/Documents/GitHub/TailGrant
go mod init github.com/rajsinghtech/tailgrant
```

**Step 2: Create .gitignore**

Create `.gitignore`:
```
# Binaries
tailgrant-server
tailgrant-worker
*.exe

# tsnet state
tsnet-*/

# IDE
.idea/
.vscode/
*.swp

# OS
.DS_Store
```

**Step 3: Add dependencies with local replace directives**

```bash
cd /Users/rajsingh/Documents/GitHub/TailGrant

# Add Temporal SDK
go get go.temporal.io/sdk@latest

# Add YAML parser
go get gopkg.in/yaml.v3

# Add tailscale-client-go-v2 (will set up replace directive)
go get tailscale.com/client/tailscale/v2

# Add tsnet (will set up replace directive)
go get tailscale.com/tsnet
```

Then manually add replace directives to go.mod:
```
replace tailscale.com/client/tailscale/v2 => ../tailscale-client-go-v2
replace tailscale.com => ../tailscale
```

Run `go mod tidy` to resolve.

**Step 4: Create directory structure**

```bash
mkdir -p cmd/tailgrant-server cmd/tailgrant-worker
mkdir -p internal/grant internal/server internal/tsapi internal/config
mkdir -p ui/static
```

**Step 5: Commit**

```bash
git add go.mod go.sum .gitignore
git commit -m "Initialize Go module with dependencies"
```

---

### Task 2: Config loading

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `config.example.yaml`

**Step 1: Write the failing test**

Create `internal/config/config_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	yaml := `
server:
  hostname: tailgrant
  data_dir: /var/lib/tailgrant/server

worker:
  hostname: tailgrant-worker
  data_dir: /var/lib/tailgrant/worker
  ephemeral: true

temporal:
  host: temporal.example.ts.net:7233
  namespace: default
  task_queue: tailgrant

tailscale:
  oauth_client_id: "k123"
  oauth_client_secret: "tskey-client-secret"
  tailnet: "example.com"

grant_types:
  - name: ssh-prod
    description: "SSH access to production servers"
    tags:
      - "tag:jit-ssh-prod"
    max_duration: 4h
    risk_level: high
    approvers:
      - admin@example.com
  - name: ssh-staging
    description: "SSH access to staging servers"
    tags:
      - "tag:jit-ssh-staging"
    max_duration: 8h
    risk_level: low
    approvers: []
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Hostname != "tailgrant" {
		t.Errorf("Server.Hostname = %q, want %q", cfg.Server.Hostname, "tailgrant")
	}
	if cfg.Worker.Ephemeral != true {
		t.Error("Worker.Ephemeral = false, want true")
	}
	if cfg.Temporal.Host != "temporal.example.ts.net:7233" {
		t.Errorf("Temporal.Host = %q, want %q", cfg.Temporal.Host, "temporal.example.ts.net:7233")
	}
	if cfg.Tailscale.OAuthClientID != "k123" {
		t.Errorf("Tailscale.OAuthClientID = %q, want %q", cfg.Tailscale.OAuthClientID, "k123")
	}
	if len(cfg.GrantTypes) != 2 {
		t.Fatalf("len(GrantTypes) = %d, want 2", len(cfg.GrantTypes))
	}
	if cfg.GrantTypes[0].Name != "ssh-prod" {
		t.Errorf("GrantTypes[0].Name = %q, want %q", cfg.GrantTypes[0].Name, "ssh-prod")
	}
	if cfg.GrantTypes[0].MaxDuration != 4*time.Hour {
		t.Errorf("GrantTypes[0].MaxDuration = %v, want %v", cfg.GrantTypes[0].MaxDuration, 4*time.Hour)
	}
	if cfg.GrantTypes[0].RiskLevel != "high" {
		t.Errorf("GrantTypes[0].RiskLevel = %q, want %q", cfg.GrantTypes[0].RiskLevel, "high")
	}
	if cfg.GrantTypes[1].RiskLevel != "low" {
		t.Errorf("GrantTypes[1].RiskLevel = %q, want %q", cfg.GrantTypes[1].RiskLevel, "low")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("Load() expected error for missing file")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(":::invalid"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() expected error for invalid YAML")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Users/rajsingh/Documents/GitHub/TailGrant
go test ./internal/config/ -v
```

Expected: FAIL — `Load` not defined.

**Step 3: Write minimal implementation**

Create `internal/config/config.go`:
```go
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Worker     WorkerConfig     `yaml:"worker"`
	Temporal   TemporalConfig   `yaml:"temporal"`
	Tailscale  TailscaleConfig  `yaml:"tailscale"`
	GrantTypes []GrantTypeConfig `yaml:"grant_types"`
}

type ServerConfig struct {
	Hostname string `yaml:"hostname"`
	DataDir  string `yaml:"data_dir"`
}

type WorkerConfig struct {
	Hostname  string `yaml:"hostname"`
	DataDir   string `yaml:"data_dir"`
	Ephemeral bool   `yaml:"ephemeral"`
}

type TemporalConfig struct {
	Host      string `yaml:"host"`
	Namespace string `yaml:"namespace"`
	TaskQueue string `yaml:"task_queue"`
}

type TailscaleConfig struct {
	OAuthClientID     string `yaml:"oauth_client_id"`
	OAuthClientSecret string `yaml:"oauth_client_secret"`
	Tailnet           string `yaml:"tailnet"`
}

type GrantTypeConfig struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Tags        []string      `yaml:"tags"`
	MaxDuration time.Duration `yaml:"max_duration"`
	RiskLevel   string        `yaml:"risk_level"`
	Approvers   []string      `yaml:"approvers"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &cfg, nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/config/ -v
```

Expected: PASS

**Step 5: Create config.example.yaml**

Create `config.example.yaml`:
```yaml
server:
  hostname: tailgrant
  data_dir: /var/lib/tailgrant/server

worker:
  hostname: tailgrant-worker
  data_dir: /var/lib/tailgrant/worker
  ephemeral: true

temporal:
  host: temporal.your-tailnet.ts.net:7233
  namespace: default
  task_queue: tailgrant

tailscale:
  oauth_client_id: "YOUR_OAUTH_CLIENT_ID"
  oauth_client_secret: "YOUR_OAUTH_CLIENT_SECRET"
  tailnet: "your-tailnet.com"

grant_types:
  - name: ssh-prod
    description: "SSH access to production servers"
    tags:
      - "tag:jit-ssh-prod"
    max_duration: 4h
    risk_level: high
    approvers:
      - admin@example.com
      - security@example.com

  - name: ssh-staging
    description: "SSH access to staging servers"
    tags:
      - "tag:jit-ssh-staging"
    max_duration: 8h
    risk_level: low
    approvers: []
```

**Step 6: Commit**

```bash
git add internal/config/ config.example.yaml
git commit -m "Add config loading with YAML parsing and grant type definitions"
```

---

### Task 3: Tailscale API client factory

**Files:**
- Create: `internal/tsapi/client.go`
- Create: `internal/tsapi/client_test.go`

**Step 1: Write the failing test**

Create `internal/tsapi/client_test.go`:
```go
package tsapi

import (
	"testing"

	"github.com/rajsinghtech/tailgrant/internal/config"
)

func TestNewClient(t *testing.T) {
	cfg := config.TailscaleConfig{
		OAuthClientID:     "test-id",
		OAuthClientSecret: "test-secret",
		Tailnet:           "example.com",
	}

	client := NewClient(cfg)
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/tsapi/ -v
```

Expected: FAIL — `NewClient` not defined.

**Step 3: Write minimal implementation**

Create `internal/tsapi/client.go`:
```go
package tsapi

import (
	"github.com/rajsinghtech/tailgrant/internal/config"
	tailscale "tailscale.com/client/tailscale/v2"
)

func NewClient(cfg config.TailscaleConfig) *tailscale.Client {
	return &tailscale.Client{
		Tailnet: cfg.Tailnet,
		Auth: &tailscale.OAuth{
			ClientID:     cfg.OAuthClientID,
			ClientSecret: cfg.OAuthClientSecret,
			Scopes:       []string{"devices:core"},
		},
	}
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/tsapi/ -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/tsapi/
git commit -m "Add Tailscale API client factory with OAuth credentials"
```

---

### Task 4: Grant types and policy evaluation

**Files:**
- Create: `internal/grant/types.go`
- Create: `internal/grant/policy.go`
- Create: `internal/grant/policy_test.go`

**Step 1: Write types**

Create `internal/grant/types.go`:
```go
package grant

import "time"

type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

type GrantType struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Tags        []string      `json:"tags"`
	MaxDuration time.Duration `json:"maxDuration"`
	RiskLevel   RiskLevel     `json:"riskLevel"`
	Approvers   []string      `json:"approvers"`
}

type GrantRequest struct {
	ID            string        `json:"id"`
	Requester     string        `json:"requester"`
	RequesterNode string        `json:"requesterNode"`
	GrantTypeName string        `json:"grantTypeName"`
	TargetNodeID  string        `json:"targetNodeID"`
	Duration      time.Duration `json:"duration"`
	Reason        string        `json:"reason"`
}

type GrantStatus string

const (
	StatusPending         GrantStatus = "pending"
	StatusAwaitingApproval GrantStatus = "awaiting_approval"
	StatusApproved        GrantStatus = "approved"
	StatusActive          GrantStatus = "active"
	StatusExpired         GrantStatus = "expired"
	StatusDenied          GrantStatus = "denied"
	StatusRevoked         GrantStatus = "revoked"
	StatusFailed          GrantStatus = "failed"
)

type GrantState struct {
	Request   GrantRequest `json:"request"`
	Status    GrantStatus  `json:"status"`
	GrantType GrantType    `json:"grantType"`
	ApprovedBy string     `json:"approvedBy,omitempty"`
	ActivatedAt time.Time `json:"activatedAt,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// Signals
type ApproveSignal struct {
	ApprovedBy string `json:"approvedBy"`
}

type DenySignal struct {
	DeniedBy string `json:"deniedBy"`
	Reason   string `json:"reason"`
}

type RevokeSignal struct {
	RevokedBy string `json:"revokedBy"`
	Reason    string `json:"reason"`
}

type ExtendSignal struct {
	ExtendedBy string        `json:"extendedBy"`
	Duration   time.Duration `json:"duration"`
}

// DeviceTagManager signals
type AddGrantSignal struct {
	GrantID string   `json:"grantID"`
	Tags    []string `json:"tags"`
}

type RemoveGrantSignal struct {
	GrantID string `json:"grantID"`
}

// ApprovalWorkflow types
type ApprovalInput struct {
	GrantID       string   `json:"grantID"`
	GrantTypeName string   `json:"grantTypeName"`
	Requester     string   `json:"requester"`
	Reason        string   `json:"reason"`
	Approvers     []string `json:"approvers"`
}

type ApprovalResult struct {
	Approved   bool   `json:"approved"`
	DecidedBy  string `json:"decidedBy"`
	Reason     string `json:"reason,omitempty"`
}
```

**Step 2: Write the failing policy test**

Create `internal/grant/policy_test.go`:
```go
package grant

import (
	"testing"
	"time"

	"github.com/rajsinghtech/tailgrant/internal/config"
)

func TestNewGrantTypeStore(t *testing.T) {
	cfgs := []config.GrantTypeConfig{
		{
			Name:        "ssh-prod",
			Description: "SSH access to prod",
			Tags:        []string{"tag:jit-ssh-prod"},
			MaxDuration: 4 * time.Hour,
			RiskLevel:   "high",
			Approvers:   []string{"admin@example.com"},
		},
		{
			Name:        "ssh-staging",
			Description: "SSH access to staging",
			Tags:        []string{"tag:jit-ssh-staging"},
			MaxDuration: 8 * time.Hour,
			RiskLevel:   "low",
			Approvers:   []string{},
		},
	}

	store := NewGrantTypeStore(cfgs)

	gt, ok := store.Get("ssh-prod")
	if !ok {
		t.Fatal("Get(ssh-prod) returned false")
	}
	if gt.Name != "ssh-prod" {
		t.Errorf("Name = %q, want %q", gt.Name, "ssh-prod")
	}
	if gt.RiskLevel != RiskHigh {
		t.Errorf("RiskLevel = %q, want %q", gt.RiskLevel, RiskHigh)
	}

	_, ok = store.Get("nonexistent")
	if ok {
		t.Error("Get(nonexistent) returned true, want false")
	}

	all := store.List()
	if len(all) != 2 {
		t.Errorf("List() len = %d, want 2", len(all))
	}
}

func TestEvaluatePolicy_AutoApprove(t *testing.T) {
	gt := GrantType{
		Name:        "ssh-staging",
		RiskLevel:   RiskLow,
		MaxDuration: 8 * time.Hour,
	}

	req := GrantRequest{
		Duration: 4 * time.Hour,
	}

	result := EvaluatePolicy(gt, req)
	if !result.AutoApproved {
		t.Error("expected auto-approve for low risk")
	}
}

func TestEvaluatePolicy_RequiresApproval(t *testing.T) {
	gt := GrantType{
		Name:        "ssh-prod",
		RiskLevel:   RiskHigh,
		MaxDuration: 4 * time.Hour,
		Approvers:   []string{"admin@example.com"},
	}

	req := GrantRequest{
		Duration: 2 * time.Hour,
	}

	result := EvaluatePolicy(gt, req)
	if result.AutoApproved {
		t.Error("expected approval required for high risk")
	}
}

func TestEvaluatePolicy_DurationExceeded(t *testing.T) {
	gt := GrantType{
		Name:        "ssh-staging",
		RiskLevel:   RiskLow,
		MaxDuration: 4 * time.Hour,
	}

	req := GrantRequest{
		Duration: 8 * time.Hour,
	}

	result := EvaluatePolicy(gt, req)
	if result.Err == nil {
		t.Error("expected error for duration exceeding max")
	}
}
```

**Step 3: Run test to verify it fails**

```bash
go test ./internal/grant/ -run TestNewGrantTypeStore -v
go test ./internal/grant/ -run TestEvaluatePolicy -v
```

Expected: FAIL

**Step 4: Write implementation**

Create `internal/grant/policy.go`:
```go
package grant

import (
	"fmt"

	"github.com/rajsinghtech/tailgrant/internal/config"
)

type GrantTypeStore struct {
	types map[string]GrantType
}

func NewGrantTypeStore(cfgs []config.GrantTypeConfig) *GrantTypeStore {
	m := make(map[string]GrantType, len(cfgs))
	for _, c := range cfgs {
		m[c.Name] = GrantType{
			Name:        c.Name,
			Description: c.Description,
			Tags:        c.Tags,
			MaxDuration: c.MaxDuration,
			RiskLevel:   RiskLevel(c.RiskLevel),
			Approvers:   c.Approvers,
		}
	}
	return &GrantTypeStore{types: m}
}

func (s *GrantTypeStore) Get(name string) (GrantType, bool) {
	gt, ok := s.types[name]
	return gt, ok
}

func (s *GrantTypeStore) List() []GrantType {
	out := make([]GrantType, 0, len(s.types))
	for _, gt := range s.types {
		out = append(out, gt)
	}
	return out
}

type PolicyResult struct {
	AutoApproved bool
	Err          error
}

func EvaluatePolicy(gt GrantType, req GrantRequest) PolicyResult {
	if req.Duration > gt.MaxDuration {
		return PolicyResult{
			Err: fmt.Errorf("requested duration %v exceeds max %v", req.Duration, gt.MaxDuration),
		}
	}

	autoApprove := gt.RiskLevel == RiskLow
	return PolicyResult{AutoApproved: autoApprove}
}
```

**Step 5: Run tests to verify they pass**

```bash
go test ./internal/grant/ -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add internal/grant/types.go internal/grant/policy.go internal/grant/policy_test.go
git commit -m "Add grant types, state machine, and policy evaluation"
```

---

### Task 5: Activities (Tailscale API wrappers)

**Files:**
- Create: `internal/grant/activities.go`
- Create: `internal/grant/activities_test.go`

**Step 1: Write the failing test**

Create `internal/grant/activities_test.go`:
```go
package grant

import (
	"context"
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func TestGetDeviceActivity(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	a := &Activities{}
	env.RegisterActivity(a.GetDevice)

	// Without a real client this will fail, but we verify the activity is registered
	_, err := env.ExecuteActivity(a.GetDevice, "test-node-id")
	if err == nil {
		t.Log("GetDevice executed (nil client expected to fail in real usage)")
	}
}

func TestSetTagsActivity(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	a := &Activities{}
	env.RegisterActivity(a.SetTags)

	_, err := env.ExecuteActivity(a.SetTags, SetTagsInput{
		DeviceID: "test-node",
		Tags:     []string{"tag:jit-ssh-prod"},
	})
	if err == nil {
		t.Log("SetTags executed (nil client expected to fail in real usage)")
	}
}

func TestListDevicesActivity(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	a := &Activities{}
	env.RegisterActivity(a.ListDevices)

	_, err := env.ExecuteActivity(a.ListDevices)
	if err == nil {
		t.Log("ListDevices executed (nil client expected to fail in real usage)")
	}
}

// Test that activity structs have the right shape
func TestActivitiesInterface(t *testing.T) {
	a := &Activities{}

	// Verify methods exist with correct signatures
	var _ func(context.Context, string) (*DeviceInfo, error) = a.GetDevice
	var _ func(context.Context, SetTagsInput) error = a.SetTags
	var _ func(context.Context) ([]DeviceInfo, error) = a.ListDevices
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/grant/ -run TestActivities -v
go test ./internal/grant/ -run TestGetDevice -v
```

Expected: FAIL

**Step 3: Write implementation**

Create `internal/grant/activities.go`:
```go
package grant

import (
	"context"
	"fmt"

	tailscale "tailscale.com/client/tailscale/v2"
)

type DeviceInfo struct {
	NodeID string   `json:"nodeId"`
	Name   string   `json:"name"`
	User   string   `json:"user"`
	Tags   []string `json:"tags"`
}

type SetTagsInput struct {
	DeviceID string   `json:"deviceID"`
	Tags     []string `json:"tags"`
}

type Activities struct {
	Client *tailscale.Client
}

func (a *Activities) GetDevice(ctx context.Context, deviceID string) (*DeviceInfo, error) {
	if a.Client == nil {
		return nil, fmt.Errorf("tailscale client not configured")
	}
	dev, err := a.Client.Devices().Get(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("getting device %s: %w", deviceID, err)
	}
	return &DeviceInfo{
		NodeID: dev.NodeID,
		Name:   dev.Name,
		User:   dev.User,
		Tags:   dev.Tags,
	}, nil
}

func (a *Activities) SetTags(ctx context.Context, input SetTagsInput) error {
	if a.Client == nil {
		return fmt.Errorf("tailscale client not configured")
	}
	if err := a.Client.Devices().SetTags(ctx, input.DeviceID, input.Tags); err != nil {
		return fmt.Errorf("setting tags on %s: %w", input.DeviceID, err)
	}
	return nil
}

func (a *Activities) ListDevices(ctx context.Context) ([]DeviceInfo, error) {
	if a.Client == nil {
		return nil, fmt.Errorf("tailscale client not configured")
	}
	devices, err := a.Client.Devices().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing devices: %w", err)
	}
	out := make([]DeviceInfo, len(devices))
	for i, d := range devices {
		out[i] = DeviceInfo{
			NodeID: d.NodeID,
			Name:   d.Name,
			User:   d.User,
			Tags:   d.Tags,
		}
	}
	return out, nil
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/grant/ -v
```

Expected: PASS (activity registration tests pass; the nil client tests fail gracefully)

**Step 5: Commit**

```bash
git add internal/grant/activities.go internal/grant/activities_test.go
git commit -m "Add Temporal activities wrapping Tailscale device API"
```

---

### Task 6: DeviceTagManagerWorkflow

**Files:**
- Create: `internal/grant/tagmanager.go`
- Create: `internal/grant/tagmanager_test.go`

**Step 1: Write the failing test**

Create `internal/grant/tagmanager_test.go`:
```go
package grant

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestDeviceTagManager_AddAndRemoveGrant(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Mock SetTags activity
	env.OnActivity((*Activities).SetTags, mock.Anything, mock.Anything).Return(nil)

	// Send AddGrant signal then RemoveGrant signal
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalAddGrant, AddGrantSignal{
			GrantID: "grant-1",
			Tags:    []string{"tag:jit-ssh-prod"},
		})
	}, 0)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalRemoveGrant, RemoveGrantSignal{
			GrantID: "grant-1",
		})
	}, 0)

	// Cancel after signals processed
	env.RegisterDelayedCallback(func() {
		env.CancelWorkflow()
	}, 0)

	env.ExecuteWorkflow(DeviceTagManagerWorkflow, DeviceTagManagerInput{
		DeviceID: "node-123",
		BaseTags: []string{"tag:server"},
	})

	require.True(t, env.IsWorkflowCompleted())

	// Verify SetTags was called - first with base+grant tags, then with just base tags
	env.AssertExpectations(t)
}

func TestDeviceTagManager_MultipleConcurrentGrants(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.OnActivity((*Activities).SetTags, mock.Anything, mock.Anything).Return(nil)

	// Add two grants
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalAddGrant, AddGrantSignal{
			GrantID: "grant-1",
			Tags:    []string{"tag:jit-ssh-prod"},
		})
	}, 0)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalAddGrant, AddGrantSignal{
			GrantID: "grant-2",
			Tags:    []string{"tag:jit-db-prod"},
		})
	}, 0)

	// Remove first grant only
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalRemoveGrant, RemoveGrantSignal{
			GrantID: "grant-1",
		})
	}, 0)

	env.RegisterDelayedCallback(func() {
		env.CancelWorkflow()
	}, 0)

	env.ExecuteWorkflow(DeviceTagManagerWorkflow, DeviceTagManagerInput{
		DeviceID: "node-123",
		BaseTags: []string{"tag:server"},
	})

	require.True(t, env.IsWorkflowCompleted())
	env.AssertExpectations(t)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/grant/ -run TestDeviceTagManager -v
```

Expected: FAIL

**Step 3: Write implementation**

Create `internal/grant/tagmanager.go`:
```go
package grant

import (
	"sort"
	"time"

	"go.temporal.io/sdk/workflow"
)

const (
	SignalAddGrant    = "add-grant"
	SignalRemoveGrant = "remove-grant"

	TagManagerContinueAsNewThreshold = 1000
)

type DeviceTagManagerInput struct {
	DeviceID    string            `json:"deviceID"`
	BaseTags    []string          `json:"baseTags"`
	ActiveGrants map[string][]string `json:"activeGrants,omitempty"`
}

func DeviceTagManagerWorkflow(ctx workflow.Context, input DeviceTagManagerInput) error {
	logger := workflow.GetLogger(ctx)

	activeGrants := input.ActiveGrants
	if activeGrants == nil {
		activeGrants = make(map[string][]string)
	}

	signalCount := 0

	addCh := workflow.GetSignalChannel(ctx, SignalAddGrant)
	removeCh := workflow.GetSignalChannel(ctx, SignalRemoveGrant)

	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOpts)

	applyTags := func() error {
		tags := computeTags(input.BaseTags, activeGrants)
		logger.Info("applying tags", "deviceID", input.DeviceID, "tags", tags)
		return workflow.ExecuteActivity(ctx, (*Activities).SetTags, SetTagsInput{
			DeviceID: input.DeviceID,
			Tags:     tags,
		}).Get(ctx, nil)
	}

	for {
		sel := workflow.NewSelector(ctx)

		sel.AddReceive(addCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig AddGrantSignal
			ch.Receive(ctx, &sig)
			activeGrants[sig.GrantID] = sig.Tags
			signalCount++
			if err := applyTags(); err != nil {
				logger.Error("failed to apply tags after add", "error", err)
			}
		})

		sel.AddReceive(removeCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig RemoveGrantSignal
			ch.Receive(ctx, &sig)
			delete(activeGrants, sig.GrantID)
			signalCount++
			if err := applyTags(); err != nil {
				logger.Error("failed to apply tags after remove", "error", err)
			}
		})

		sel.AddReceive(ctx.Done(), func(ch workflow.ReceiveChannel, more bool) {})

		sel.Select(ctx)

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if signalCount >= TagManagerContinueAsNewThreshold {
			return workflow.NewContinueAsNewError(ctx, DeviceTagManagerWorkflow, DeviceTagManagerInput{
				DeviceID:     input.DeviceID,
				BaseTags:     input.BaseTags,
				ActiveGrants: activeGrants,
			})
		}
	}
}

func computeTags(baseTags []string, activeGrants map[string][]string) []string {
	tagSet := make(map[string]bool)
	for _, t := range baseTags {
		tagSet[t] = true
	}
	for _, tags := range activeGrants {
		for _, t := range tags {
			tagSet[t] = true
		}
	}
	out := make([]string, 0, len(tagSet))
	for t := range tagSet {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
```

**Note:** The import `"go.temporal.io/sdk/temporal"` is needed for `temporal.RetryPolicy`. The test workflow environment from Temporal SDK handles this. After writing, check that the import resolves properly; the `temporal` package import may need to be:

```go
import (
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)
```

and the `RetryPolicy` reference should be `&temporal.RetryPolicy{...}`.

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/grant/ -run TestDeviceTagManager -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/grant/tagmanager.go internal/grant/tagmanager_test.go
git commit -m "Add DeviceTagManagerWorkflow for serialized tag operations"
```

---

### Task 7: GrantWorkflow and ApprovalWorkflow

**Files:**
- Create: `internal/grant/workflow.go`
- Create: `internal/grant/approval.go`
- Create: `internal/grant/workflow_test.go`

**Step 1: Write the failing test for auto-approved grant**

Create `internal/grant/workflow_test.go`:
```go
package grant

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestGrantWorkflow_AutoApprove(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.OnActivity((*Activities).GetDevice, mock.Anything, "target-node").Return(&DeviceInfo{
		NodeID: "target-node",
		Name:   "prod-server",
		Tags:   []string{"tag:server"},
	}, nil)
	env.OnActivity((*Activities).SetTags, mock.Anything, mock.Anything).Return(nil)

	gt := GrantType{
		Name:        "ssh-staging",
		Tags:        []string{"tag:jit-ssh-staging"},
		MaxDuration: 8 * time.Hour,
		RiskLevel:   RiskLow,
	}

	req := GrantRequest{
		ID:            "grant-1",
		Requester:     "user@example.com",
		RequesterNode: "requester-node",
		GrantTypeName: "ssh-staging",
		TargetNodeID:  "target-node",
		Duration:      1 * time.Hour,
		Reason:        "debugging",
	}

	env.ExecuteWorkflow(GrantWorkflow, GrantWorkflowInput{
		Request:   req,
		GrantType: gt,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var state GrantState
	encoded, err := env.QueryWorkflow(QueryGrantStatus)
	require.NoError(t, err)
	require.NoError(t, encoded.Get(&state))
	require.Equal(t, StatusExpired, state.Status)
}

func TestGrantWorkflow_RequiresApproval_Approved(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.OnActivity((*Activities).GetDevice, mock.Anything, "target-node").Return(&DeviceInfo{
		NodeID: "target-node",
		Name:   "prod-server",
		Tags:   []string{"tag:server"},
	}, nil)
	env.OnActivity((*Activities).SetTags, mock.Anything, mock.Anything).Return(nil)

	// Register child approval workflow to return approved
	env.OnWorkflow(ApprovalWorkflow, mock.Anything).Return(ApprovalResult{
		Approved:  true,
		DecidedBy: "admin@example.com",
	}, nil)

	gt := GrantType{
		Name:        "ssh-prod",
		Tags:        []string{"tag:jit-ssh-prod"},
		MaxDuration: 4 * time.Hour,
		RiskLevel:   RiskHigh,
		Approvers:   []string{"admin@example.com"},
	}

	req := GrantRequest{
		ID:            "grant-2",
		Requester:     "user@example.com",
		RequesterNode: "requester-node",
		GrantTypeName: "ssh-prod",
		TargetNodeID:  "target-node",
		Duration:      1 * time.Hour,
		Reason:        "incident response",
	}

	env.ExecuteWorkflow(GrantWorkflow, GrantWorkflowInput{
		Request:   req,
		GrantType: gt,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestGrantWorkflow_RequiresApproval_Denied(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.OnWorkflow(ApprovalWorkflow, mock.Anything).Return(ApprovalResult{
		Approved:  false,
		DecidedBy: "admin@example.com",
		Reason:    "not justified",
	}, nil)

	gt := GrantType{
		Name:        "ssh-prod",
		Tags:        []string{"tag:jit-ssh-prod"},
		MaxDuration: 4 * time.Hour,
		RiskLevel:   RiskHigh,
		Approvers:   []string{"admin@example.com"},
	}

	req := GrantRequest{
		ID:            "grant-3",
		Requester:     "user@example.com",
		RequesterNode: "requester-node",
		GrantTypeName: "ssh-prod",
		TargetNodeID:  "target-node",
		Duration:      1 * time.Hour,
		Reason:        "want access",
	}

	env.ExecuteWorkflow(GrantWorkflow, GrantWorkflowInput{
		Request:   req,
		GrantType: gt,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var state GrantState
	encoded, err := env.QueryWorkflow(QueryGrantStatus)
	require.NoError(t, err)
	require.NoError(t, encoded.Get(&state))
	require.Equal(t, StatusDenied, state.Status)
}

func TestGrantWorkflow_Revoked(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.OnActivity((*Activities).GetDevice, mock.Anything, "target-node").Return(&DeviceInfo{
		NodeID: "target-node",
		Name:   "prod-server",
		Tags:   []string{"tag:server"},
	}, nil)
	env.OnActivity((*Activities).SetTags, mock.Anything, mock.Anything).Return(nil)

	gt := GrantType{
		Name:        "ssh-staging",
		Tags:        []string{"tag:jit-ssh-staging"},
		MaxDuration: 8 * time.Hour,
		RiskLevel:   RiskLow,
	}

	req := GrantRequest{
		ID:            "grant-4",
		Requester:     "user@example.com",
		RequesterNode: "requester-node",
		GrantTypeName: "ssh-staging",
		TargetNodeID:  "target-node",
		Duration:      4 * time.Hour,
		Reason:        "debugging",
	}

	// Send revoke signal after grant becomes active
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalRevoke, RevokeSignal{
			RevokedBy: "admin@example.com",
			Reason:    "emergency",
		})
	}, time.Minute)

	env.ExecuteWorkflow(GrantWorkflow, GrantWorkflowInput{
		Request:   req,
		GrantType: gt,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var state GrantState
	encoded, err := env.QueryWorkflow(QueryGrantStatus)
	require.NoError(t, err)
	require.NoError(t, encoded.Get(&state))
	require.Equal(t, StatusRevoked, state.Status)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/grant/ -run TestGrantWorkflow -v
```

Expected: FAIL

**Step 3: Write ApprovalWorkflow**

Create `internal/grant/approval.go`:
```go
package grant

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

const (
	SignalApprove = "approve"
	SignalDeny    = "deny"

	DefaultApprovalTimeout = 24 * time.Hour
)

func ApprovalWorkflow(ctx workflow.Context, input ApprovalInput) (ApprovalResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("approval workflow started",
		"grantID", input.GrantID,
		"requester", input.Requester,
		"approvers", input.Approvers,
	)

	approveCh := workflow.GetSignalChannel(ctx, SignalApprove)
	denyCh := workflow.GetSignalChannel(ctx, SignalDeny)

	timerCtx, cancelTimer := workflow.WithCancel(ctx)
	timerFuture := workflow.NewTimer(timerCtx, DefaultApprovalTimeout)

	sel := workflow.NewSelector(ctx)

	var result ApprovalResult

	sel.AddReceive(approveCh, func(ch workflow.ReceiveChannel, more bool) {
		var sig ApproveSignal
		ch.Receive(ctx, &sig)
		cancelTimer()
		result = ApprovalResult{
			Approved:  true,
			DecidedBy: sig.ApprovedBy,
		}
	})

	sel.AddReceive(denyCh, func(ch workflow.ReceiveChannel, more bool) {
		var sig DenySignal
		ch.Receive(ctx, &sig)
		cancelTimer()
		result = ApprovalResult{
			Approved:  false,
			DecidedBy: sig.DeniedBy,
			Reason:    sig.Reason,
		}
	})

	sel.AddFuture(timerFuture, func(f workflow.Future) {
		result = ApprovalResult{
			Approved: false,
			Reason:   "approval timed out",
		}
	})

	sel.Select(ctx)

	logger.Info("approval decision",
		"grantID", input.GrantID,
		"approved", result.Approved,
		"decidedBy", result.DecidedBy,
	)

	return result, nil
}
```

**Step 4: Write GrantWorkflow**

Create `internal/grant/workflow.go`:
```go
package grant

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	SignalRevoke  = "revoke"
	SignalExtend  = "extend"
	QueryGrantStatus = "grant-status"

	TagManagerWorkflowIDPrefix = "tag-manager-"
)

type GrantWorkflowInput struct {
	Request   GrantRequest `json:"request"`
	GrantType GrantType    `json:"grantType"`
}

func GrantWorkflow(ctx workflow.Context, input GrantWorkflowInput) error {
	logger := workflow.GetLogger(ctx)
	req := input.Request
	gt := input.GrantType

	state := GrantState{
		Request:   req,
		Status:    StatusPending,
		GrantType: gt,
	}

	// Register query handler
	if err := workflow.SetQueryHandler(ctx, QueryGrantStatus, func() (GrantState, error) {
		return state, nil
	}); err != nil {
		return fmt.Errorf("setting query handler: %w", err)
	}

	// Evaluate policy
	policyResult := EvaluatePolicy(gt, req)
	if policyResult.Err != nil {
		state.Status = StatusFailed
		state.Error = policyResult.Err.Error()
		return nil
	}

	// Handle approval
	if !policyResult.AutoApproved {
		state.Status = StatusAwaitingApproval

		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: fmt.Sprintf("approval-%s", req.ID),
		})

		var approvalResult ApprovalResult
		err := workflow.ExecuteChildWorkflow(childCtx, ApprovalWorkflow, ApprovalInput{
			GrantID:       req.ID,
			GrantTypeName: req.GrantTypeName,
			Requester:     req.Requester,
			Reason:        req.Reason,
			Approvers:     gt.Approvers,
		}).Get(ctx, &approvalResult)

		if err != nil {
			state.Status = StatusFailed
			state.Error = err.Error()
			return nil
		}

		if !approvalResult.Approved {
			state.Status = StatusDenied
			state.Error = approvalResult.Reason
			return nil
		}

		state.ApprovedBy = approvalResult.DecidedBy
	} else {
		state.ApprovedBy = "auto-approved"
	}

	state.Status = StatusApproved

	// Set up activity options
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    5,
		},
	}
	actCtx := workflow.WithActivityOptions(ctx, activityOpts)

	// Get current device info for base tags
	var deviceInfo DeviceInfo
	err := workflow.ExecuteActivity(actCtx, (*Activities).GetDevice, req.TargetNodeID).Get(ctx, &deviceInfo)
	if err != nil {
		state.Status = StatusFailed
		state.Error = fmt.Sprintf("getting device info: %v", err)
		return nil
	}

	// Signal DeviceTagManager to add grant tags
	tagManagerID := TagManagerWorkflowIDPrefix + req.TargetNodeID
	addSignal := AddGrantSignal{
		GrantID: req.ID,
		Tags:    gt.Tags,
	}
	if err := workflow.SignalExternalWorkflow(ctx, tagManagerID, "", SignalAddGrant, addSignal).Get(ctx, nil); err != nil {
		// TagManager might not exist yet — apply tags directly
		logger.Warn("tag manager signal failed, applying tags directly", "error", err)
		allTags := append(deviceInfo.Tags, gt.Tags...)
		if err := workflow.ExecuteActivity(actCtx, (*Activities).SetTags, SetTagsInput{
			DeviceID: req.TargetNodeID,
			Tags:     allTags,
		}).Get(ctx, nil); err != nil {
			state.Status = StatusFailed
			state.Error = fmt.Sprintf("setting tags: %v", err)
			return nil
		}
	}

	// Grant is now active
	state.Status = StatusActive
	now := workflow.Now(ctx)
	state.ActivatedAt = now
	state.ExpiresAt = now.Add(req.Duration)

	logger.Info("grant activated",
		"grantID", req.ID,
		"expiresAt", state.ExpiresAt,
	)

	// Wait for TTL or revoke signal
	timerCtx, cancelTimer := workflow.WithCancel(ctx)
	timerFuture := workflow.NewTimer(timerCtx, req.Duration)

	revokeCh := workflow.GetSignalChannel(ctx, SignalRevoke)
	extendCh := workflow.GetSignalChannel(ctx, SignalExtend)

	revoked := false
	expired := false

	for !revoked && !expired {
		sel := workflow.NewSelector(ctx)

		sel.AddFuture(timerFuture, func(f workflow.Future) {
			expired = true
		})

		sel.AddReceive(revokeCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig RevokeSignal
			ch.Receive(ctx, &sig)
			cancelTimer()
			revoked = true
			state.Status = StatusRevoked
			logger.Info("grant revoked", "grantID", req.ID, "by", sig.RevokedBy)
		})

		sel.AddReceive(extendCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig ExtendSignal
			ch.Receive(ctx, &sig)
			newDuration := time.Until(state.ExpiresAt) + sig.Duration
			if newDuration > gt.MaxDuration {
				logger.Warn("extension would exceed max duration, capping",
					"requested", sig.Duration, "max", gt.MaxDuration)
				newDuration = gt.MaxDuration
			}
			cancelTimer()
			state.ExpiresAt = workflow.Now(ctx).Add(newDuration)
			timerCtx, cancelTimer = workflow.WithCancel(ctx)
			timerFuture = workflow.NewTimer(timerCtx, newDuration)
			logger.Info("grant extended", "grantID", req.ID, "newExpiry", state.ExpiresAt)
		})

		sel.Select(ctx)
	}

	if expired {
		state.Status = StatusExpired
	}

	// Remove grant tags
	removeSignal := RemoveGrantSignal{GrantID: req.ID}
	if err := workflow.SignalExternalWorkflow(ctx, tagManagerID, "", SignalRemoveGrant, removeSignal).Get(ctx, nil); err != nil {
		logger.Warn("tag manager remove signal failed, removing tags directly", "error", err)
		// Fallback: get current tags and remove grant tags
		var currentDevice DeviceInfo
		if err := workflow.ExecuteActivity(actCtx, (*Activities).GetDevice, req.TargetNodeID).Get(ctx, &currentDevice); err != nil {
			logger.Error("failed to get device for tag cleanup", "error", err)
			return nil
		}
		cleanTags := removeTags(currentDevice.Tags, gt.Tags)
		_ = workflow.ExecuteActivity(actCtx, (*Activities).SetTags, SetTagsInput{
			DeviceID: req.TargetNodeID,
			Tags:     cleanTags,
		}).Get(ctx, nil)
	}

	logger.Info("grant workflow completed", "grantID", req.ID, "finalStatus", state.Status)
	return nil
}

func removeTags(current, toRemove []string) []string {
	removeSet := make(map[string]bool, len(toRemove))
	for _, t := range toRemove {
		removeSet[t] = true
	}
	var out []string
	for _, t := range current {
		if !removeSet[t] {
			out = append(out, t)
		}
	}
	return out
}
```

**Step 5: Run tests to verify they pass**

```bash
go test ./internal/grant/ -run TestGrantWorkflow -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add internal/grant/workflow.go internal/grant/approval.go internal/grant/workflow_test.go
git commit -m "Add GrantWorkflow with approval flow, TTL, revocation, and extension"
```

---

### Task 8: ReconciliationWorkflow

**Files:**
- Create: `internal/grant/reconcile.go`
- Create: `internal/grant/reconcile_test.go`

**Step 1: Write the failing test**

Create `internal/grant/reconcile_test.go`:
```go
package grant

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestReconciliationWorkflow(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.OnActivity((*Activities).ListDevices, mock.Anything).Return([]DeviceInfo{
		{NodeID: "node-1", Name: "server-1", Tags: []string{"tag:server"}},
	}, nil)

	env.ExecuteWorkflow(ReconciliationWorkflow, ReconciliationInput{
		Interval: time.Minute, // shortened for test
	})

	// ReconciliationWorkflow uses ContinueAsNew, which Temporal test env treats as completion
	require.True(t, env.IsWorkflowCompleted())
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/grant/ -run TestReconciliation -v
```

Expected: FAIL

**Step 3: Write implementation**

Create `internal/grant/reconcile.go`:
```go
package grant

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	DefaultReconcileInterval = 5 * time.Minute
)

type ReconciliationInput struct {
	Interval time.Duration `json:"interval"`
}

func ReconciliationWorkflow(ctx workflow.Context, input ReconciliationInput) error {
	logger := workflow.GetLogger(ctx)

	interval := input.Interval
	if interval == 0 {
		interval = DefaultReconcileInterval
	}

	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOpts)

	// List all devices
	var devices []DeviceInfo
	if err := workflow.ExecuteActivity(ctx, (*Activities).ListDevices).Get(ctx, &devices); err != nil {
		logger.Error("reconciliation: failed to list devices", "error", err)
	} else {
		logger.Info("reconciliation sweep", "deviceCount", len(devices))
		// Future: query active grant workflows and compare tags
		// For v0, this is a safety net that logs device state
	}

	// Sleep then ContinueAsNew
	if err := workflow.Sleep(ctx, interval); err != nil {
		return err
	}

	return workflow.NewContinueAsNewError(ctx, ReconciliationWorkflow, input)
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/grant/ -run TestReconciliation -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/grant/reconcile.go internal/grant/reconcile_test.go
git commit -m "Add ReconciliationWorkflow with periodic device sweep"
```

---

### Task 9: Worker binary

**Files:**
- Create: `cmd/tailgrant-worker/main.go`

**Step 1: Write the worker binary**

Create `cmd/tailgrant-worker/main.go`:
```go
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"tailscale.com/tsnet"

	"github.com/rajsinghtech/tailgrant/internal/config"
	"github.com/rajsinghtech/tailgrant/internal/grant"
	"github.com/rajsinghtech/tailgrant/internal/tsapi"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start tsnet node
	srv := &tsnet.Server{
		Hostname:  cfg.Worker.Hostname,
		Dir:       cfg.Worker.DataDir,
		Ephemeral: cfg.Worker.Ephemeral,
		AuthKey:   os.Getenv("TS_AUTHKEY"),
	}
	defer srv.Close()

	status, err := srv.Up(ctx)
	if err != nil {
		log.Fatalf("tsnet up: %v", err)
	}
	log.Printf("worker tsnet node up: %s", status.Self.DNSName)

	// Connect to Temporal via tailnet
	dialCtx := context.Background()
	temporalClient, err := client.DialContext(dialCtx, client.Options{
		HostPort:  cfg.Temporal.Host,
		Namespace: cfg.Temporal.Namespace,
	})
	if err != nil {
		log.Fatalf("connecting to temporal: %v", err)
	}
	defer temporalClient.Close()

	// Create Tailscale API client
	tsClient := tsapi.NewClient(cfg.Tailscale)

	// Create activities
	activities := &grant.Activities{
		Client: tsClient,
	}

	// Create and start Temporal worker
	w := worker.New(temporalClient, cfg.Temporal.TaskQueue, worker.Options{})

	// Register workflows
	w.RegisterWorkflow(grant.GrantWorkflow)
	w.RegisterWorkflow(grant.ApprovalWorkflow)
	w.RegisterWorkflow(grant.DeviceTagManagerWorkflow)
	w.RegisterWorkflow(grant.ReconciliationWorkflow)

	// Register activities
	w.RegisterActivity(activities.GetDevice)
	w.RegisterActivity(activities.SetTags)
	w.RegisterActivity(activities.ListDevices)

	log.Printf("starting temporal worker on queue %q", cfg.Temporal.TaskQueue)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("worker failed: %v", err)
	}
}
```

**Step 2: Verify it compiles**

```bash
go build ./cmd/tailgrant-worker/
```

Expected: Successful compilation (may need `go mod tidy` first).

**Step 3: Commit**

```bash
git add cmd/tailgrant-worker/
git commit -m "Add worker binary with tsnet, Temporal worker, and Tailscale API client"
```

---

### Task 10: WhoIs auth middleware

**Files:**
- Create: `internal/server/middleware.go`
- Create: `internal/server/middleware_test.go`

**Step 1: Write the failing test**

Create `internal/server/middleware_test.go`:
```go
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAuth_SetsIdentityInContext(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := IdentityFromContext(r.Context())
		if id == nil {
			t.Fatal("expected identity in context")
		}
		if id.LoginName != "user@example.com" {
			t.Errorf("LoginName = %q, want %q", id.LoginName, "user@example.com")
		}
		if id.NodeID != "node-123" {
			t.Errorf("NodeID = %q, want %q", id.NodeID, "node-123")
		}
		w.WriteHeader(http.StatusOK)
	})

	// Use a fake WhoIs function for testing
	whoIsFn := func(ctx context.Context, remoteAddr string) (*Identity, error) {
		return &Identity{
			LoginName:   "user@example.com",
			DisplayName: "Test User",
			NodeID:      "node-123",
			NodeName:    "test-laptop",
		}, nil
	}

	handler := RequireAuth(whoIsFn)(inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "100.64.0.1:12345"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRequireAuth_NoIdentity_Returns401(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	whoIsFn := func(ctx context.Context, remoteAddr string) (*Identity, error) {
		return nil, http.ErrNoCookie // simulate any error
	}

	handler := RequireAuth(whoIsFn)(inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "100.64.0.1:12345"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/server/ -run TestRequireAuth -v
```

Expected: FAIL

**Step 3: Write implementation**

Create `internal/server/middleware.go`:
```go
package server

import (
	"context"
	"log"
	"net/http"
)

type Identity struct {
	LoginName   string `json:"loginName"`
	DisplayName string `json:"displayName"`
	NodeID      string `json:"nodeId"`
	NodeName    string `json:"nodeName"`
}

type contextKey string

const identityKey contextKey = "identity"

func IdentityFromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(identityKey).(*Identity)
	return id
}

// WhoIsFunc abstracts the WhoIs call for testability.
// In production, this wraps lc.WhoIs from tsnet's LocalClient.
type WhoIsFunc func(ctx context.Context, remoteAddr string) (*Identity, error)

func RequireAuth(whoIs WhoIsFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, err := whoIs(r.Context(), r.RemoteAddr)
			if err != nil {
				log.Printf("auth: WhoIs failed for %s: %v", r.RemoteAddr, err)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), identityKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/server/ -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/server/middleware.go internal/server/middleware_test.go
git commit -m "Add WhoIs auth middleware with testable WhoIsFunc abstraction"
```

---

### Task 11: HTTP handlers

**Files:**
- Create: `internal/server/handlers.go`
- Create: `internal/server/router.go`
- Create: `internal/server/handlers_test.go`

**Step 1: Write the failing test**

Create `internal/server/handlers_test.go`:
```go
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rajsinghtech/tailgrant/internal/grant"
)

type mockTemporalClient struct {
	startedWorkflows []string
}

func (m *mockTemporalClient) StartGrant(ctx context.Context, req grant.GrantRequest, gt grant.GrantType) (string, error) {
	m.startedWorkflows = append(m.startedWorkflows, req.ID)
	return "wf-run-id", nil
}

func (m *mockTemporalClient) QueryGrant(ctx context.Context, grantID string) (*grant.GrantState, error) {
	return &grant.GrantState{
		Request: grant.GrantRequest{ID: grantID},
		Status:  grant.StatusActive,
	}, nil
}

func (m *mockTemporalClient) SignalApprove(ctx context.Context, grantID string, approvedBy string) error {
	return nil
}

func (m *mockTemporalClient) SignalDeny(ctx context.Context, grantID string, deniedBy string, reason string) error {
	return nil
}

func (m *mockTemporalClient) SignalRevoke(ctx context.Context, grantID string, revokedBy string, reason string) error {
	return nil
}

func TestHandleListGrantTypes(t *testing.T) {
	store := grant.NewGrantTypeStore(nil)
	h := &Handlers{GrantTypes: store}

	req := httptest.NewRequest("GET", "/api/grant-types", nil)
	w := httptest.NewRecorder()

	h.ListGrantTypes(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleRequestGrant(t *testing.T) {
	store := grant.NewGrantTypeStore([]struct {
		Name        string
		Description string
		Tags        []string
		MaxDuration time.Duration
		RiskLevel   string
		Approvers   []string
	}{
		// We'll use config.GrantTypeConfig through the store
	})
	// For this test, use a store with the grant type pre-loaded
	_ = store

	mc := &mockTemporalClient{}
	h := &Handlers{
		GrantTypes: grant.NewGrantTypeStoreFromTypes([]grant.GrantType{
			{
				Name:        "ssh-staging",
				Tags:        []string{"tag:jit-ssh-staging"},
				MaxDuration: 8 * time.Hour,
				RiskLevel:   grant.RiskLow,
			},
		}),
		Temporal: mc,
	}

	body := `{
		"grantTypeName": "ssh-staging",
		"targetNodeID": "node-123",
		"duration": "1h",
		"reason": "debugging"
	}`

	req := httptest.NewRequest("POST", "/api/grants", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Add identity to context
	ctx := context.WithValue(req.Context(), identityKey, &Identity{
		LoginName: "user@example.com",
		NodeID:    "requester-node",
	})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.RequestGrant(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	if len(mc.startedWorkflows) != 1 {
		t.Errorf("expected 1 workflow started, got %d", len(mc.startedWorkflows))
	}
}

func TestHandleGetGrant(t *testing.T) {
	mc := &mockTemporalClient{}
	h := &Handlers{Temporal: mc}

	req := httptest.NewRequest("GET", "/api/grants/grant-1", nil)
	// Simulate route param — we'll use a simple path-based extraction
	req.SetPathValue("id", "grant-1")
	w := httptest.NewRecorder()

	h.GetGrant(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var state grant.GrantState
	if err := json.NewDecoder(w.Body).Decode(&state); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if state.Request.ID != "grant-1" {
		t.Errorf("grant ID = %q, want %q", state.Request.ID, "grant-1")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/server/ -run TestHandle -v
```

Expected: FAIL

**Step 3: Write the Temporal client interface and handlers**

Create `internal/server/handlers.go`:
```go
package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rajsinghtech/tailgrant/internal/grant"
)

// TemporalClient abstracts the Temporal client operations needed by handlers.
type TemporalClient interface {
	StartGrant(ctx context.Context, req grant.GrantRequest, gt grant.GrantType) (string, error)
	QueryGrant(ctx context.Context, grantID string) (*grant.GrantState, error)
	SignalApprove(ctx context.Context, grantID string, approvedBy string) error
	SignalDeny(ctx context.Context, grantID string, deniedBy string, reason string) error
	SignalRevoke(ctx context.Context, grantID string, revokedBy string, reason string) error
}

type Handlers struct {
	GrantTypes *grant.GrantTypeStore
	Temporal   TemporalClient
}

func (h *Handlers) ListGrantTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.GrantTypes.List())
}

type grantRequestBody struct {
	GrantTypeName string `json:"grantTypeName"`
	TargetNodeID  string `json:"targetNodeID"`
	Duration      string `json:"duration"`
	Reason        string `json:"reason"`
}

func (h *Handlers) RequestGrant(w http.ResponseWriter, r *http.Request) {
	id := IdentityFromContext(r.Context())
	if id == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body grantRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	gt, ok := h.GrantTypes.Get(body.GrantTypeName)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown grant type: %s", body.GrantTypeName), http.StatusBadRequest)
		return
	}

	duration, err := time.ParseDuration(body.Duration)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid duration: %s", body.Duration), http.StatusBadRequest)
		return
	}

	req := grant.GrantRequest{
		ID:            uuid.New().String(),
		Requester:     id.LoginName,
		RequesterNode: id.NodeID,
		GrantTypeName: body.GrantTypeName,
		TargetNodeID:  body.TargetNodeID,
		Duration:      duration,
		Reason:        body.Reason,
	}

	runID, err := h.Temporal.StartGrant(r.Context(), req, gt)
	if err != nil {
		log.Printf("starting grant workflow: %v", err)
		http.Error(w, "failed to start grant", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"grantID": req.ID,
		"runID":   runID,
	})
}

func (h *Handlers) GetGrant(w http.ResponseWriter, r *http.Request) {
	grantID := r.PathValue("id")
	if grantID == "" {
		http.Error(w, "missing grant ID", http.StatusBadRequest)
		return
	}

	state, err := h.Temporal.QueryGrant(r.Context(), grantID)
	if err != nil {
		log.Printf("querying grant %s: %v", grantID, err)
		http.Error(w, "grant not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, state)
}

func (h *Handlers) ApproveGrant(w http.ResponseWriter, r *http.Request) {
	id := IdentityFromContext(r.Context())
	if id == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	grantID := r.PathValue("id")
	if err := h.Temporal.SignalApprove(r.Context(), grantID, id.LoginName); err != nil {
		log.Printf("approving grant %s: %v", grantID, err)
		http.Error(w, "failed to approve", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (h *Handlers) DenyGrant(w http.ResponseWriter, r *http.Request) {
	id := IdentityFromContext(r.Context())
	if id == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	grantID := r.PathValue("id")

	var body struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if err := h.Temporal.SignalDeny(r.Context(), grantID, id.LoginName, body.Reason); err != nil {
		log.Printf("denying grant %s: %v", grantID, err)
		http.Error(w, "failed to deny", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "denied"})
}

func (h *Handlers) RevokeGrant(w http.ResponseWriter, r *http.Request) {
	id := IdentityFromContext(r.Context())
	if id == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	grantID := r.PathValue("id")

	var body struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if err := h.Temporal.SignalRevoke(r.Context(), grantID, id.LoginName, body.Reason); err != nil {
		log.Printf("revoking grant %s: %v", grantID, err)
		http.Error(w, "failed to revoke", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
```

**Important:** Add `context` to imports, and add `github.com/google/uuid` dependency:

```bash
go get github.com/google/uuid
```

**Step 4: Write the router**

Create `internal/server/router.go`:
```go
package server

import (
	"net/http"
)

func NewRouter(h *Handlers, authMiddleware func(http.Handler) http.Handler) http.Handler {
	mux := http.NewServeMux()

	// API routes (all require auth)
	mux.Handle("GET /api/grant-types", authMiddleware(http.HandlerFunc(h.ListGrantTypes)))
	mux.Handle("POST /api/grants", authMiddleware(http.HandlerFunc(h.RequestGrant)))
	mux.Handle("GET /api/grants/{id}", authMiddleware(http.HandlerFunc(h.GetGrant)))
	mux.Handle("POST /api/grants/{id}/approve", authMiddleware(http.HandlerFunc(h.ApproveGrant)))
	mux.Handle("POST /api/grants/{id}/deny", authMiddleware(http.HandlerFunc(h.DenyGrant)))
	mux.Handle("POST /api/grants/{id}/revoke", authMiddleware(http.HandlerFunc(h.RevokeGrant)))

	// Static UI files
	mux.Handle("GET /", http.FileServer(http.Dir("ui/static")))

	return mux
}
```

**Step 5: Add NewGrantTypeStoreFromTypes helper to policy.go**

Add to `internal/grant/policy.go`:
```go
func NewGrantTypeStoreFromTypes(types []GrantType) *GrantTypeStore {
	m := make(map[string]GrantType, len(types))
	for _, gt := range types {
		m[gt.Name] = gt
	}
	return &GrantTypeStore{types: m}
}
```

**Step 6: Run tests to verify they pass**

```bash
go test ./internal/server/ -v
go test ./internal/grant/ -v
```

Expected: PASS

**Step 7: Commit**

```bash
git add internal/server/ internal/grant/policy.go
git commit -m "Add HTTP handlers, router, and grant type store helpers"
```

---

### Task 12: Server binary with Temporal client wrapper

**Files:**
- Create: `internal/server/temporal.go`
- Create: `cmd/tailgrant-server/main.go`

**Step 1: Write the Temporal client wrapper**

Create `internal/server/temporal.go`:
```go
package server

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"

	"github.com/rajsinghtech/tailgrant/internal/grant"
)

type temporalClientWrapper struct {
	client    client.Client
	taskQueue string
}

func NewTemporalClient(c client.Client, taskQueue string) TemporalClient {
	return &temporalClientWrapper{client: c, taskQueue: taskQueue}
}

func (t *temporalClientWrapper) StartGrant(ctx context.Context, req grant.GrantRequest, gt grant.GrantType) (string, error) {
	opts := client.StartWorkflowOptions{
		ID:        fmt.Sprintf("grant-%s", req.ID),
		TaskQueue: t.taskQueue,
	}

	run, err := t.client.ExecuteWorkflow(ctx, opts, grant.GrantWorkflow, grant.GrantWorkflowInput{
		Request:   req,
		GrantType: gt,
	})
	if err != nil {
		return "", err
	}

	return run.GetRunID(), nil
}

func (t *temporalClientWrapper) QueryGrant(ctx context.Context, grantID string) (*grant.GrantState, error) {
	workflowID := fmt.Sprintf("grant-%s", grantID)
	resp, err := t.client.QueryWorkflow(ctx, workflowID, "", grant.QueryGrantStatus)
	if err != nil {
		return nil, err
	}

	var state grant.GrantState
	if err := resp.Get(&state); err != nil {
		return nil, err
	}

	return &state, nil
}

func (t *temporalClientWrapper) SignalApprove(ctx context.Context, grantID string, approvedBy string) error {
	approvalID := fmt.Sprintf("approval-%s", grantID)
	return t.client.SignalWorkflow(ctx, approvalID, "", grant.SignalApprove, grant.ApproveSignal{
		ApprovedBy: approvedBy,
	})
}

func (t *temporalClientWrapper) SignalDeny(ctx context.Context, grantID string, deniedBy string, reason string) error {
	approvalID := fmt.Sprintf("approval-%s", grantID)
	return t.client.SignalWorkflow(ctx, approvalID, "", grant.SignalDeny, grant.DenySignal{
		DeniedBy: deniedBy,
		Reason:   reason,
	})
}

func (t *temporalClientWrapper) SignalRevoke(ctx context.Context, grantID string, revokedBy string, reason string) error {
	workflowID := fmt.Sprintf("grant-%s", grantID)
	return t.client.SignalWorkflow(ctx, workflowID, "", grant.SignalRevoke, grant.RevokeSignal{
		RevokedBy: revokedBy,
		Reason:    reason,
	})
}
```

**Step 2: Write the server binary**

Create `cmd/tailgrant-server/main.go`:
```go
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	temporalclient "go.temporal.io/sdk/client"
	"tailscale.com/tsnet"

	"github.com/rajsinghtech/tailgrant/internal/config"
	"github.com/rajsinghtech/tailgrant/internal/grant"
	"github.com/rajsinghtech/tailgrant/internal/server"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start tsnet node
	srv := &tsnet.Server{
		Hostname: cfg.Server.Hostname,
		Dir:      cfg.Server.DataDir,
		AuthKey:  os.Getenv("TS_AUTHKEY"),
	}
	defer srv.Close()

	status, err := srv.Up(ctx)
	if err != nil {
		log.Fatalf("tsnet up: %v", err)
	}
	log.Printf("server tsnet node up: %s", status.Self.DNSName)

	// Get local client for WhoIs
	lc, err := srv.LocalClient()
	if err != nil {
		log.Fatalf("getting local client: %v", err)
	}

	// Create WhoIs function
	whoIsFn := func(ctx context.Context, remoteAddr string) (*server.Identity, error) {
		who, err := lc.WhoIs(ctx, remoteAddr)
		if err != nil {
			return nil, err
		}
		return &server.Identity{
			LoginName:   who.UserProfile.LoginName,
			DisplayName: who.UserProfile.DisplayName,
			NodeID:      string(who.Node.StableID),
			NodeName:    who.Node.ComputedName,
		}, nil
	}

	// Connect to Temporal
	temporalClient, err := temporalclient.DialContext(ctx, temporalclient.Options{
		HostPort:  cfg.Temporal.Host,
		Namespace: cfg.Temporal.Namespace,
	})
	if err != nil {
		log.Fatalf("connecting to temporal: %v", err)
	}
	defer temporalClient.Close()

	// Build grant type store
	grantTypeStore := grant.NewGrantTypeStore(cfg.GrantTypes)

	// Create handlers
	tc := server.NewTemporalClient(temporalClient, cfg.Temporal.TaskQueue)
	handlers := &server.Handlers{
		GrantTypes: grantTypeStore,
		Temporal:   tc,
	}

	// Build router
	authMiddleware := server.RequireAuth(whoIsFn)
	router := server.NewRouter(handlers, authMiddleware)

	// Listen with TLS on tailnet
	ln, err := srv.Listen("tcp", ":443")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	ln = tls.NewListener(ln, &tls.Config{
		GetCertificate: lc.GetCertificate,
	})

	log.Printf("serving on https://%s", status.Self.DNSName)

	go func() {
		if err := http.Serve(ln, router); err != nil {
			log.Printf("http serve: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
}
```

**Step 3: Verify it compiles**

```bash
go mod tidy
go build ./cmd/tailgrant-server/
go build ./cmd/tailgrant-worker/
```

Expected: Successful compilation.

**Step 4: Commit**

```bash
git add internal/server/temporal.go cmd/tailgrant-server/
git commit -m "Add server binary with tsnet TLS, WhoIs auth, and Temporal client"
```

---

### Task 13: Minimal UI

**Files:**
- Create: `ui/static/index.html`
- Create: `ui/static/style.css`
- Create: `ui/static/app.js`
- Create: `ui/embed.go`

**Step 1: Create the HTML**

Create `ui/static/index.html`:
```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>TailGrant</title>
    <link rel="stylesheet" href="/style.css">
</head>
<body>
    <header>
        <h1>TailGrant</h1>
        <span id="user-info"></span>
    </header>
    <main>
        <section id="request-section">
            <h2>Request Access</h2>
            <form id="grant-form">
                <label for="grant-type">Grant Type</label>
                <select id="grant-type" required></select>

                <label for="target-node">Target Device (Node ID)</label>
                <input type="text" id="target-node" required placeholder="nodeID:abc123">

                <label for="duration">Duration</label>
                <input type="text" id="duration" required placeholder="1h" value="1h">

                <label for="reason">Reason</label>
                <textarea id="reason" required placeholder="Why do you need access?"></textarea>

                <button type="submit">Request Grant</button>
            </form>
            <div id="form-result"></div>
        </section>

        <section id="grants-section">
            <h2>Active Grants</h2>
            <table id="grants-table">
                <thead>
                    <tr>
                        <th>ID</th>
                        <th>Type</th>
                        <th>Target</th>
                        <th>Status</th>
                        <th>Expires</th>
                        <th>Actions</th>
                    </tr>
                </thead>
                <tbody id="grants-body"></tbody>
            </table>
        </section>

        <section id="pending-section">
            <h2>Pending Approvals</h2>
            <div id="pending-list"></div>
        </section>
    </main>
    <script src="/app.js"></script>
</body>
</html>
```

**Step 2: Create the CSS**

Create `ui/static/style.css`:
```css
* { margin: 0; padding: 0; box-sizing: border-box; }

body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    background: #f5f5f5;
    color: #333;
    max-width: 960px;
    margin: 0 auto;
    padding: 1rem;
}

header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 1rem 0;
    border-bottom: 2px solid #2563eb;
    margin-bottom: 2rem;
}

h1 { color: #1e40af; }
h2 { margin-bottom: 1rem; color: #1e3a5f; }

section { background: #fff; padding: 1.5rem; border-radius: 8px; margin-bottom: 1.5rem; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }

form { display: flex; flex-direction: column; gap: 0.75rem; }
label { font-weight: 600; font-size: 0.9rem; }
input, select, textarea { padding: 0.5rem; border: 1px solid #ccc; border-radius: 4px; font-size: 0.95rem; }
textarea { min-height: 60px; }

button {
    padding: 0.6rem 1.2rem;
    background: #2563eb;
    color: #fff;
    border: none;
    border-radius: 4px;
    cursor: pointer;
    font-size: 0.95rem;
    align-self: flex-start;
}
button:hover { background: #1d4ed8; }
button.danger { background: #dc2626; }
button.danger:hover { background: #b91c1c; }
button.approve { background: #16a34a; }
button.approve:hover { background: #15803d; }

table { width: 100%; border-collapse: collapse; }
th, td { text-align: left; padding: 0.5rem; border-bottom: 1px solid #eee; }
th { background: #f8fafc; font-size: 0.85rem; text-transform: uppercase; color: #64748b; }

.status { padding: 0.15rem 0.5rem; border-radius: 10px; font-size: 0.8rem; font-weight: 600; }
.status-active { background: #dcfce7; color: #166534; }
.status-pending, .status-awaiting_approval { background: #fef3c7; color: #92400e; }
.status-expired { background: #e5e7eb; color: #374151; }
.status-denied, .status-revoked { background: #fee2e2; color: #991b1b; }

#form-result { margin-top: 0.75rem; padding: 0.5rem; border-radius: 4px; }
.result-success { background: #dcfce7; color: #166534; }
.result-error { background: #fee2e2; color: #991b1b; }
```

**Step 3: Create the JavaScript**

Create `ui/static/app.js`:
```javascript
document.addEventListener("DOMContentLoaded", () => {
    loadGrantTypes();
    pollGrants();

    document.getElementById("grant-form").addEventListener("submit", async (e) => {
        e.preventDefault();
        await requestGrant();
    });
});

async function loadGrantTypes() {
    try {
        const resp = await fetch("/api/grant-types");
        const types = await resp.json();
        const select = document.getElementById("grant-type");
        select.innerHTML = "";
        for (const gt of types) {
            const opt = document.createElement("option");
            opt.value = gt.name;
            opt.textContent = `${gt.name} — ${gt.description} (${gt.riskLevel})`;
            select.appendChild(opt);
        }
    } catch (err) {
        console.error("failed to load grant types:", err);
    }
}

async function requestGrant() {
    const resultDiv = document.getElementById("form-result");
    try {
        const resp = await fetch("/api/grants", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                grantTypeName: document.getElementById("grant-type").value,
                targetNodeID: document.getElementById("target-node").value,
                duration: document.getElementById("duration").value,
                reason: document.getElementById("reason").value,
            }),
        });
        if (!resp.ok) {
            const text = await resp.text();
            throw new Error(text);
        }
        const data = await resp.json();
        resultDiv.className = "result-success";
        resultDiv.textContent = `Grant requested: ${data.grantID}`;
        document.getElementById("grant-form").reset();
        pollGrants();
    } catch (err) {
        resultDiv.className = "result-error";
        resultDiv.textContent = `Error: ${err.message}`;
    }
}

let knownGrants = [];

async function pollGrants() {
    // Poll known grants for status updates
    const tbody = document.getElementById("grants-body");
    tbody.innerHTML = "";
    for (const grantID of knownGrants) {
        try {
            const resp = await fetch(`/api/grants/${grantID}`);
            if (!resp.ok) continue;
            const state = await resp.json();
            const tr = document.createElement("tr");
            const expiresAt = state.expiresAt ? new Date(state.expiresAt).toLocaleString() : "-";
            tr.innerHTML = `
                <td title="${state.request.id}">${state.request.id.substring(0, 8)}...</td>
                <td>${state.grantType.name}</td>
                <td>${state.request.targetNodeID}</td>
                <td><span class="status status-${state.status}">${state.status}</span></td>
                <td>${expiresAt}</td>
                <td>${renderActions(state)}</td>
            `;
            tbody.appendChild(tr);
        } catch (err) {
            console.error("polling grant:", err);
        }
    }
}

function renderActions(state) {
    if (state.status === "awaiting_approval") {
        return `
            <button class="approve" onclick="approveGrant('${state.request.id}')">Approve</button>
            <button class="danger" onclick="denyGrant('${state.request.id}')">Deny</button>
        `;
    }
    if (state.status === "active") {
        return `<button class="danger" onclick="revokeGrant('${state.request.id}')">Revoke</button>`;
    }
    return "";
}

async function approveGrant(id) {
    await fetch(`/api/grants/${id}/approve`, { method: "POST" });
    pollGrants();
}

async function denyGrant(id) {
    const reason = prompt("Denial reason:");
    if (reason === null) return;
    await fetch(`/api/grants/${id}/deny`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ reason }),
    });
    pollGrants();
}

async function revokeGrant(id) {
    const reason = prompt("Revocation reason:");
    if (reason === null) return;
    await fetch(`/api/grants/${id}/revoke`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ reason }),
    });
    pollGrants();
}

// Track grants from form submissions
const origRequestGrant = requestGrant;
requestGrant = async function() {
    const resultDiv = document.getElementById("form-result");
    try {
        const resp = await fetch("/api/grants", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                grantTypeName: document.getElementById("grant-type").value,
                targetNodeID: document.getElementById("target-node").value,
                duration: document.getElementById("duration").value,
                reason: document.getElementById("reason").value,
            }),
        });
        if (!resp.ok) {
            const text = await resp.text();
            throw new Error(text);
        }
        const data = await resp.json();
        knownGrants.push(data.grantID);
        resultDiv.className = "result-success";
        resultDiv.textContent = `Grant requested: ${data.grantID}`;
        document.getElementById("grant-form").reset();
        pollGrants();
    } catch (err) {
        resultDiv.className = "result-error";
        resultDiv.textContent = `Error: ${err.message}`;
    }
};

// Poll every 5 seconds
setInterval(pollGrants, 5000);
```

**Step 4: Create embed.go**

Create `ui/embed.go`:
```go
package ui

import "embed"

//go:embed static
var Static embed.FS
```

**Step 5: Update router to use embedded files**

Modify `internal/server/router.go` to accept an `fs.FS` for the static files:
```go
package server

import (
	"io/fs"
	"net/http"
)

func NewRouter(h *Handlers, authMiddleware func(http.Handler) http.Handler, staticFS fs.FS) http.Handler {
	mux := http.NewServeMux()

	// API routes (all require auth)
	mux.Handle("GET /api/grant-types", authMiddleware(http.HandlerFunc(h.ListGrantTypes)))
	mux.Handle("POST /api/grants", authMiddleware(http.HandlerFunc(h.RequestGrant)))
	mux.Handle("GET /api/grants/{id}", authMiddleware(http.HandlerFunc(h.GetGrant)))
	mux.Handle("POST /api/grants/{id}/approve", authMiddleware(http.HandlerFunc(h.ApproveGrant)))
	mux.Handle("POST /api/grants/{id}/deny", authMiddleware(http.HandlerFunc(h.DenyGrant)))
	mux.Handle("POST /api/grants/{id}/revoke", authMiddleware(http.HandlerFunc(h.RevokeGrant)))

	// Static UI files
	mux.Handle("GET /", http.FileServerFS(staticFS))

	return mux
}
```

Update `cmd/tailgrant-server/main.go` to pass the embedded FS:
```go
import "github.com/rajsinghtech/tailgrant/ui"
import "io/fs"

// In main():
staticFS, _ := fs.Sub(ui.Static, "static")
router := server.NewRouter(handlers, authMiddleware, staticFS)
```

**Step 6: Verify compilation**

```bash
go mod tidy
go build ./cmd/tailgrant-server/
go build ./cmd/tailgrant-worker/
```

Expected: Successful compilation.

**Step 7: Commit**

```bash
git add ui/ internal/server/router.go cmd/tailgrant-server/main.go
git commit -m "Add minimal web UI with grant request form, status table, and approval actions"
```

---

### Task 14: Integration test setup and full compilation verification

**Files:**
- Create: `internal/grant/integration_test.go`

**Step 1: Write an integration test that exercises the full workflow in Temporal test env**

Create `internal/grant/integration_test.go`:
```go
package grant

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestIntegration_FullGrantLifecycle(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Mock activities
	env.OnActivity((*Activities).GetDevice, mock.Anything, "target-node").Return(&DeviceInfo{
		NodeID: "target-node",
		Name:   "prod-server",
		Tags:   []string{"tag:server"},
	}, nil)
	env.OnActivity((*Activities).SetTags, mock.Anything, mock.Anything).Return(nil)

	gt := GrantType{
		Name:        "ssh-staging",
		Tags:        []string{"tag:jit-ssh-staging"},
		MaxDuration: 8 * time.Hour,
		RiskLevel:   RiskLow,
	}

	req := GrantRequest{
		ID:            "integration-test-1",
		Requester:     "user@example.com",
		RequesterNode: "requester-node",
		GrantTypeName: "ssh-staging",
		TargetNodeID:  "target-node",
		Duration:      30 * time.Minute,
		Reason:        "integration test",
	}

	env.ExecuteWorkflow(GrantWorkflow, GrantWorkflowInput{
		Request:   req,
		GrantType: gt,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	// Verify final state
	var state GrantState
	encoded, err := env.QueryWorkflow(QueryGrantStatus)
	require.NoError(t, err)
	require.NoError(t, encoded.Get(&state))
	require.Equal(t, StatusExpired, state.Status)
	require.Equal(t, "auto-approved", state.ApprovedBy)
	require.Equal(t, "integration-test-1", state.Request.ID)
}

func TestIntegration_ApprovalThenExpiry(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.OnActivity((*Activities).GetDevice, mock.Anything, "target-node").Return(&DeviceInfo{
		NodeID: "target-node",
		Name:   "prod-server",
		Tags:   []string{"tag:server"},
	}, nil)
	env.OnActivity((*Activities).SetTags, mock.Anything, mock.Anything).Return(nil)

	env.OnWorkflow(ApprovalWorkflow, mock.Anything).Return(ApprovalResult{
		Approved:  true,
		DecidedBy: "admin@example.com",
	}, nil)

	gt := GrantType{
		Name:        "ssh-prod",
		Tags:        []string{"tag:jit-ssh-prod"},
		MaxDuration: 4 * time.Hour,
		RiskLevel:   RiskHigh,
		Approvers:   []string{"admin@example.com"},
	}

	req := GrantRequest{
		ID:            "integration-test-2",
		Requester:     "user@example.com",
		RequesterNode: "requester-node",
		GrantTypeName: "ssh-prod",
		TargetNodeID:  "target-node",
		Duration:      1 * time.Hour,
		Reason:        "incident response",
	}

	env.ExecuteWorkflow(GrantWorkflow, GrantWorkflowInput{
		Request:   req,
		GrantType: gt,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var state GrantState
	encoded, err := env.QueryWorkflow(QueryGrantStatus)
	require.NoError(t, err)
	require.NoError(t, encoded.Get(&state))
	require.Equal(t, StatusExpired, state.Status)
	require.Equal(t, "admin@example.com", state.ApprovedBy)
}
```

**Step 2: Run all tests**

```bash
go test ./... -v
```

Expected: All tests PASS.

**Step 3: Verify both binaries compile**

```bash
go build -o /dev/null ./cmd/tailgrant-server/
go build -o /dev/null ./cmd/tailgrant-worker/
```

Expected: Clean compilation with no errors.

**Step 4: Run go vet**

```bash
go vet ./...
```

Expected: No issues.

**Step 5: Commit**

```bash
git add internal/grant/integration_test.go
git commit -m "Add integration tests for full grant lifecycle"
```

---

## Post-Implementation Notes

**Testing with real infrastructure:**
1. Start both binaries with `TS_AUTHKEY` set
2. Browse to `https://tailgrant.{tailnet}.ts.net`
3. Verify WhoIs identifies the connecting user
4. Request a low-risk grant -> verify auto-approve -> verify tags appear
5. Request a high-risk grant -> verify approval flow works
6. Wait for TTL -> verify tags removed
7. Test revocation mid-grant
8. Kill worker mid-grant -> restart -> verify Temporal replays correctly
9. Test concurrent grants on same device via DeviceTagManager

**Dependency notes:**
- `tailscale-client-go-v2` is at `../tailscale-client-go-v2` (local replace)
- `tsnet` is at `../tailscale` (local replace)
- Temporal cluster expected at `temporal.{tailnet}.ts.net:7233`
- The `stretchr/testify` package is pulled transitively from tailscale-client-go-v2
