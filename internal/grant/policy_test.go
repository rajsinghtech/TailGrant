package grant

import (
	"strings"
	"testing"
	"time"

	"github.com/rajsinghtech/tailgrant/internal/config"
)

func TestNewYAMLGrantTypeStore(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "ssh-access",
			Description: "SSH access to production servers",
			Tags:        []string{"tag:ssh-prod"},
			MaxDuration: "4h",
			RiskLevel:   "high",
			Approvers:   []string{"admin1", "admin2"},
		},
		{
			Name:        "read-only",
			Description: "Read-only database access",
			Tags:        []string{"tag:db-read"},
			MaxDuration: "24h",
			RiskLevel:   "low",
			Approvers:   []string{},
		},
	}

	store, err := NewYAMLGrantTypeStore(configs)
	if err != nil {
		t.Fatalf("NewYAMLGrantTypeStore failed: %v", err)
	}

	t.Run("Get ssh-access", func(t *testing.T) {
		gt, err := store.Get("ssh-access")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if gt.Name != "ssh-access" {
			t.Errorf("Name = %q, want %q", gt.Name, "ssh-access")
		}
		if gt.Description != "SSH access to production servers" {
			t.Errorf("Description = %q, want %q", gt.Description, "SSH access to production servers")
		}
		if len(gt.Tags) != 1 || gt.Tags[0] != "tag:ssh-prod" {
			t.Errorf("Tags = %v, want [tag:ssh-prod]", gt.Tags)
		}
		if gt.MaxDuration != 4*time.Hour {
			t.Errorf("MaxDuration = %v, want 4h", gt.MaxDuration)
		}
		if gt.RiskLevel != RiskHigh {
			t.Errorf("RiskLevel = %v, want RiskHigh", gt.RiskLevel)
		}
		if len(gt.Approvers) != 2 || gt.Approvers[0] != "admin1" || gt.Approvers[1] != "admin2" {
			t.Errorf("Approvers = %v, want [admin1 admin2]", gt.Approvers)
		}
	})

	t.Run("Get read-only", func(t *testing.T) {
		gt, err := store.Get("read-only")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if gt.Name != "read-only" {
			t.Errorf("Name = %q, want %q", gt.Name, "read-only")
		}
		if gt.MaxDuration != 24*time.Hour {
			t.Errorf("MaxDuration = %v, want 24h", gt.MaxDuration)
		}
		if gt.RiskLevel != RiskLow {
			t.Errorf("RiskLevel = %v, want RiskLow", gt.RiskLevel)
		}
		if len(gt.Approvers) != 0 {
			t.Errorf("Approvers = %v, want []", gt.Approvers)
		}
	})

	t.Run("List returns all types in order", func(t *testing.T) {
		types, err := store.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(types) != 2 {
			t.Fatalf("List returned %d types, want 2", len(types))
		}
		if types[0].Name != "ssh-access" {
			t.Errorf("types[0].Name = %q, want %q", types[0].Name, "ssh-access")
		}
		if types[1].Name != "read-only" {
			t.Errorf("types[1].Name = %q, want %q", types[1].Name, "read-only")
		}
	})

	t.Run("List returns copy", func(t *testing.T) {
		types1, _ := store.List()
		types2, _ := store.List()
		if &types1[0] == &types2[0] {
			t.Error("List returned same slice, expected independent copies")
		}
	})
}

func TestNewYAMLGrantTypeStore_InvalidDuration(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "bad-duration",
			Description: "Invalid duration config",
			Tags:        []string{"tag:test"},
			MaxDuration: "not-a-duration",
			RiskLevel:   "low",
			Approvers:   []string{},
		},
	}

	store, err := NewYAMLGrantTypeStore(configs)
	if err == nil {
		t.Fatal("NewYAMLGrantTypeStore succeeded, expected error for invalid duration")
	}
	if store != nil {
		t.Error("store should be nil on error")
	}
	if !strings.Contains(err.Error(), "invalid maxDuration") {
		t.Errorf("error message = %q, want to contain 'invalid maxDuration'", err.Error())
	}
	if !strings.Contains(err.Error(), "bad-duration") {
		t.Errorf("error message = %q, want to contain grant type name 'bad-duration'", err.Error())
	}
}

func TestNewYAMLGrantTypeStore_Duplicate(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "duplicate-name",
			Description: "First config",
			Tags:        []string{"tag:first"},
			MaxDuration: "1h",
			RiskLevel:   "low",
			Approvers:   []string{},
		},
		{
			Name:        "duplicate-name",
			Description: "Second config with same name",
			Tags:        []string{"tag:second"},
			MaxDuration: "2h",
			RiskLevel:   "high",
			Approvers:   []string{"admin"},
		},
	}

	store, err := NewYAMLGrantTypeStore(configs)
	if err == nil {
		t.Fatal("NewYAMLGrantTypeStore succeeded, expected error for duplicate grant type")
	}
	if store != nil {
		t.Error("store should be nil on error")
	}
	if !strings.Contains(err.Error(), "duplicate grant type") {
		t.Errorf("error message = %q, want to contain 'duplicate grant type'", err.Error())
	}
	if !strings.Contains(err.Error(), "duplicate-name") {
		t.Errorf("error message = %q, want to contain grant type name 'duplicate-name'", err.Error())
	}
}

func TestYAMLGrantTypeStore_Get_NotFound(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "existing-type",
			Description: "A valid grant type",
			Tags:        []string{"tag:test"},
			MaxDuration: "1h",
			RiskLevel:   "low",
			Approvers:   []string{},
		},
	}

	store, err := NewYAMLGrantTypeStore(configs)
	if err != nil {
		t.Fatalf("NewYAMLGrantTypeStore failed: %v", err)
	}

	gt, err := store.Get("nonexistent-type")
	if err == nil {
		t.Fatal("Get succeeded for nonexistent type, expected error")
	}
	if gt != nil {
		t.Error("Get should return nil grant type on error")
	}
	if !strings.Contains(err.Error(), "unknown grant type") {
		t.Errorf("error message = %q, want to contain 'unknown grant type'", err.Error())
	}
	if !strings.Contains(err.Error(), "nonexistent-type") {
		t.Errorf("error message = %q, want to contain requested name 'nonexistent-type'", err.Error())
	}
}

func TestEvaluatePolicy_LowRisk(t *testing.T) {
	grantType := &GrantType{
		Name:        "low-risk-grant",
		Description: "Low risk grant type",
		Tags:        []string{"tag:safe"},
		MaxDuration: 1 * time.Hour,
		RiskLevel:   RiskLow,
		Approvers:   []string{},
	}

	autoApprove := EvaluatePolicy(grantType, "user@example.com")
	if !autoApprove {
		t.Error("EvaluatePolicy returned false for low risk grant, want true (auto-approve)")
	}
}

func TestEvaluatePolicy_HighRisk(t *testing.T) {
	grantType := &GrantType{
		Name:        "high-risk-grant",
		Description: "High risk grant type",
		Tags:        []string{"tag:dangerous"},
		MaxDuration: 1 * time.Hour,
		RiskLevel:   RiskHigh,
		Approvers:   []string{"admin1", "admin2"},
	}

	autoApprove := EvaluatePolicy(grantType, "user@example.com")
	if autoApprove {
		t.Error("EvaluatePolicy returned true for high risk grant, want false (require approval)")
	}
}

func TestEvaluatePolicy_MediumRisk(t *testing.T) {
	grantType := &GrantType{
		Name:        "medium-risk-grant",
		Description: "Medium risk grant type",
		Tags:        []string{"tag:moderate"},
		MaxDuration: 2 * time.Hour,
		RiskLevel:   RiskMedium,
		Approvers:   []string{"admin"},
	}

	autoApprove := EvaluatePolicy(grantType, "user@example.com")
	if autoApprove {
		t.Error("EvaluatePolicy returned true for medium risk grant, want false (require approval)")
	}
}

func TestParseRiskLevel(t *testing.T) {
	tests := []struct {
		input string
		want  RiskLevel
	}{
		{"low", RiskLow},
		{"Low", RiskLow},
		{"LOW", RiskLow},
		{"  low  ", RiskLow},
		{"medium", RiskMedium},
		{"Medium", RiskMedium},
		{"MEDIUM", RiskMedium},
		{"  medium  ", RiskMedium},
		{"high", RiskHigh},
		{"High", RiskHigh},
		{"HIGH", RiskHigh},
		{"  high  ", RiskHigh},
		{"", RiskLow},
		{"unknown", RiskLow},
		{"invalid", RiskLow},
		{"critical", RiskLow},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseRiskLevel(tt.input)
			if got != tt.want {
				t.Errorf("ParseRiskLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
