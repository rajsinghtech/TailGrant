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
		if time.Duration(gt.MaxDuration) != 4*time.Hour {
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
		if time.Duration(gt.MaxDuration) != 24*time.Hour {
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
		MaxDuration: JSONDuration(1 * time.Hour),
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
		MaxDuration: JSONDuration(1 * time.Hour),
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
		MaxDuration: JSONDuration(2 * time.Hour),
		RiskLevel:   RiskMedium,
		Approvers:   []string{"admin"},
	}

	autoApprove := EvaluatePolicy(grantType, "user@example.com")
	if autoApprove {
		t.Error("EvaluatePolicy returned true for medium risk grant, want false (require approval)")
	}
}

func TestNewYAMLGrantTypeStore_UserRoleAction(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "temp-admin",
			Description: "Temporarily elevate user to admin",
			Action:      "user_role",
			UserAction:  &config.UserActionConfig{Role: "admin"},
			MaxDuration: "2h",
			RiskLevel:   "high",
			Approvers:   []string{"admin@example.com"},
		},
	}

	store, err := NewYAMLGrantTypeStore(configs)
	if err != nil {
		t.Fatalf("NewYAMLGrantTypeStore failed: %v", err)
	}

	gt, err := store.Get("temp-admin")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if gt.Action != ActionUserRole {
		t.Errorf("Action = %q, want %q", gt.Action, ActionUserRole)
	}
	if gt.UserAction == nil || gt.UserAction.Role != "admin" {
		t.Errorf("UserAction.Role = %v, want admin", gt.UserAction)
	}
	if len(gt.Tags) != 0 {
		t.Errorf("Tags = %v, want empty for user_role action", gt.Tags)
	}
}

func TestNewYAMLGrantTypeStore_UserRestoreAction(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "temp-restore",
			Description: "Temporarily restore suspended user",
			Action:      "user_restore",
			MaxDuration: "4h",
			RiskLevel:   "medium",
			Approvers:   []string{"secops@example.com"},
		},
	}

	store, err := NewYAMLGrantTypeStore(configs)
	if err != nil {
		t.Fatalf("NewYAMLGrantTypeStore failed: %v", err)
	}

	gt, err := store.Get("temp-restore")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if gt.Action != ActionUserRestore {
		t.Errorf("Action = %q, want %q", gt.Action, ActionUserRestore)
	}
}

func TestNewYAMLGrantTypeStore_UserRoleMissingConfig(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "bad-role",
			Description: "Missing userAction",
			Action:      "user_role",
			MaxDuration: "1h",
			RiskLevel:   "low",
		},
	}

	_, err := NewYAMLGrantTypeStore(configs)
	if err == nil {
		t.Fatal("expected error for user_role action without userAction.role")
	}
	if !strings.Contains(err.Error(), "user_role action requires userAction.role") {
		t.Errorf("error = %q, want to contain 'user_role action requires userAction.role'", err.Error())
	}
}

func TestNewYAMLGrantTypeStore_UserRoleInvalidRole(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "bad-role-value",
			Description: "Invalid role value",
			Action:      "user_role",
			UserAction:  &config.UserActionConfig{Role: "superadmin"},
			MaxDuration: "1h",
			RiskLevel:   "low",
		},
	}

	_, err := NewYAMLGrantTypeStore(configs)
	if err == nil {
		t.Fatal("expected error for invalid role value")
	}
	if !strings.Contains(err.Error(), "invalid role") {
		t.Errorf("error = %q, want to contain 'invalid role'", err.Error())
	}
}

func TestNewYAMLGrantTypeStore_UnknownAction(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "bad-action",
			Description: "Unknown action",
			Action:      "delete_user",
			MaxDuration: "1h",
			RiskLevel:   "low",
		},
	}

	_, err := NewYAMLGrantTypeStore(configs)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("error = %q, want to contain 'unknown action'", err.Error())
	}
}

func TestNewYAMLGrantTypeStore_DefaultActionIsTag(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "default-action",
			Description: "No action specified",
			Tags:        []string{"tag:test"},
			MaxDuration: "1h",
			RiskLevel:   "low",
		},
	}

	store, err := NewYAMLGrantTypeStore(configs)
	if err != nil {
		t.Fatalf("NewYAMLGrantTypeStore failed: %v", err)
	}

	gt, _ := store.Get("default-action")
	if gt.Action != ActionTag {
		t.Errorf("Action = %q, want %q for default", gt.Action, ActionTag)
	}
}

func TestNewYAMLGrantTypeStore_PostureAttributesOnly(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "posture-only",
			Description: "Grant with only posture attributes, no tags",
			PostureAttributes: []config.PostureAttributeConfig{
				{Key: "custom:jit-ssh", Value: "granted", Target: "requester"},
			},
			MaxDuration: "4h",
			RiskLevel:   "low",
		},
	}

	store, err := NewYAMLGrantTypeStore(configs)
	if err != nil {
		t.Fatalf("NewYAMLGrantTypeStore failed: %v", err)
	}

	gt, err := store.Get("posture-only")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(gt.Tags) != 0 {
		t.Errorf("Tags = %v, want empty", gt.Tags)
	}
	if len(gt.PostureAttributes) != 1 {
		t.Fatalf("PostureAttributes = %v, want 1 attribute", gt.PostureAttributes)
	}
	pa := gt.PostureAttributes[0]
	if pa.Key != "custom:jit-ssh" {
		t.Errorf("PostureAttributes[0].Key = %q, want %q", pa.Key, "custom:jit-ssh")
	}
	if pa.Value != "granted" {
		t.Errorf("PostureAttributes[0].Value = %v, want %q", pa.Value, "granted")
	}
	if pa.Target != "requester" {
		t.Errorf("PostureAttributes[0].Target = %q, want %q", pa.Target, "requester")
	}
}

func TestNewYAMLGrantTypeStore_PostureAttributeInvalidKey(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "bad-posture-key",
			Description: "Posture attribute key missing custom: prefix",
			PostureAttributes: []config.PostureAttributeConfig{
				{Key: "jit-ssh", Value: "granted"},
			},
			MaxDuration: "1h",
			RiskLevel:   "low",
		},
	}

	_, err := NewYAMLGrantTypeStore(configs)
	if err == nil {
		t.Fatal("expected error for posture attribute key without custom: prefix")
	}
	if !strings.Contains(err.Error(), "must start with \"custom:\"") {
		t.Errorf("error = %q, want to contain 'must start with \"custom:\"'", err.Error())
	}
}

func TestNewYAMLGrantTypeStore_PostureAttributeInvalidTarget(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "bad-posture-target",
			Description: "Posture attribute with invalid target",
			PostureAttributes: []config.PostureAttributeConfig{
				{Key: "custom:test", Value: "v", Target: "both"},
			},
			MaxDuration: "1h",
			RiskLevel:   "low",
		},
	}

	_, err := NewYAMLGrantTypeStore(configs)
	if err == nil {
		t.Fatal("expected error for posture attribute with invalid target")
	}
	if !strings.Contains(err.Error(), "invalid target") {
		t.Errorf("error = %q, want to contain 'invalid target'", err.Error())
	}
}

func TestNewYAMLGrantTypeStore_PostureAttributeDefaultTarget(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "default-target",
			Description: "Posture attribute with no target specified",
			PostureAttributes: []config.PostureAttributeConfig{
				{Key: "custom:test", Value: "v"},
			},
			MaxDuration: "1h",
			RiskLevel:   "low",
		},
	}

	store, err := NewYAMLGrantTypeStore(configs)
	if err != nil {
		t.Fatalf("NewYAMLGrantTypeStore failed: %v", err)
	}

	gt, _ := store.Get("default-target")
	if gt.PostureAttributes[0].Target != "requester" {
		t.Errorf("Target = %q, want %q (should default to requester)", gt.PostureAttributes[0].Target, "requester")
	}
}

func TestNewYAMLGrantTypeStore_PostureAttributeNilValue(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "nil-value",
			Description: "Posture attribute with nil value",
			PostureAttributes: []config.PostureAttributeConfig{
				{Key: "custom:test"},
			},
			MaxDuration: "1h",
			RiskLevel:   "low",
		},
	}

	_, err := NewYAMLGrantTypeStore(configs)
	if err == nil {
		t.Fatal("expected error for posture attribute with nil value")
	}
	if !strings.Contains(err.Error(), "must have a value") {
		t.Errorf("error = %q, want to contain 'must have a value'", err.Error())
	}
}

func TestNewYAMLGrantTypeStore_TagsAndPostureAttributes(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "combined",
			Description: "Grant with both tags and posture attributes",
			Tags:        []string{"tag:ssh-granted"},
			PostureAttributes: []config.PostureAttributeConfig{
				{Key: "custom:jit-ssh", Value: "granted", Target: "requester"},
				{Key: "custom:jit-level", Value: "admin", Target: "target"},
			},
			MaxDuration: "4h",
			RiskLevel:   "low",
		},
	}

	store, err := NewYAMLGrantTypeStore(configs)
	if err != nil {
		t.Fatalf("NewYAMLGrantTypeStore failed: %v", err)
	}

	gt, _ := store.Get("combined")
	if len(gt.Tags) != 1 || gt.Tags[0] != "tag:ssh-granted" {
		t.Errorf("Tags = %v, want [tag:ssh-granted]", gt.Tags)
	}
	if len(gt.PostureAttributes) != 2 {
		t.Fatalf("PostureAttributes = %v, want 2 attributes", gt.PostureAttributes)
	}
	if gt.PostureAttributes[0].Target != "requester" {
		t.Errorf("PostureAttributes[0].Target = %q, want %q", gt.PostureAttributes[0].Target, "requester")
	}
	if gt.PostureAttributes[1].Target != "target" {
		t.Errorf("PostureAttributes[1].Target = %q, want %q", gt.PostureAttributes[1].Target, "target")
	}
}

func TestNewYAMLGrantTypeStore_NoTagsNoPostureAttributes(t *testing.T) {
	configs := []config.GrantTypeConfig{
		{
			Name:        "empty-grant",
			Description: "Grant with no tags and no posture attributes",
			MaxDuration: "1h",
			RiskLevel:   "low",
		},
	}

	_, err := NewYAMLGrantTypeStore(configs)
	if err == nil {
		t.Fatal("expected error for grant type with no tags and no posture attributes")
	}
	if !strings.Contains(err.Error(), "at least one tag or posture attribute") {
		t.Errorf("error = %q, want to contain 'at least one tag or posture attribute'", err.Error())
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
