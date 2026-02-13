package grant

import (
	"fmt"
	"strings"
	"time"

	"github.com/rajsinghtech/tailgrant/internal/config"
)

type GrantTypeStore interface {
	Get(name string) (*GrantType, error)
	List() ([]*GrantType, error)
}

type YAMLGrantTypeStore struct {
	types map[string]*GrantType
	order []*GrantType
}

func NewYAMLGrantTypeStore(configs []config.GrantTypeConfig) (*YAMLGrantTypeStore, error) {
	store := &YAMLGrantTypeStore{
		types: make(map[string]*GrantType, len(configs)),
		order: make([]*GrantType, 0, len(configs)),
	}

	for _, c := range configs {
		dur, err := time.ParseDuration(c.MaxDuration)
		if err != nil {
			return nil, fmt.Errorf("grant type %q: invalid maxDuration %q: %w", c.Name, c.MaxDuration, err)
		}

		action := ActionType(c.Action)
		if action == "" {
			action = ActionTag
		}

		var userAction *UserAction
		switch action {
		case ActionTag:
			if len(c.Tags) == 0 && len(c.PostureAttributes) == 0 {
				return nil, fmt.Errorf("grant type %q: tag action must have at least one tag or posture attribute", c.Name)
			}
			for _, tag := range c.Tags {
				if err := validateTag(tag); err != nil {
					return nil, fmt.Errorf("grant type %q: %w", c.Name, err)
				}
			}
			for _, pa := range c.PostureAttributes {
				if err := validatePostureAttribute(pa); err != nil {
					return nil, fmt.Errorf("grant type %q: %w", c.Name, err)
				}
			}
		case ActionUserRole:
			if c.UserAction == nil || c.UserAction.Role == "" {
				return nil, fmt.Errorf("grant type %q: user_role action requires userAction.role", c.Name)
			}
			validRoles := map[string]bool{
				"owner": true, "member": true, "admin": true,
				"it-admin": true, "network-admin": true,
				"billing-admin": true, "auditor": true,
			}
			if !validRoles[c.UserAction.Role] {
				return nil, fmt.Errorf("grant type %q: invalid role %q", c.Name, c.UserAction.Role)
			}
			userAction = &UserAction{Role: c.UserAction.Role}
		case ActionUserRestore:
			// no extra config needed
		default:
			return nil, fmt.Errorf("grant type %q: unknown action %q", c.Name, action)
		}

		if ParseRiskLevel(c.RiskLevel) > RiskLow && len(c.Approvers) == 0 {
			return nil, fmt.Errorf("grant type %q: medium/high risk requires at least one approver", c.Name)
		}

		postureAttrs := convertPostureAttributes(c.PostureAttributes)

		gt := &GrantType{
			Name:              c.Name,
			Description:       c.Description,
			Tags:              c.Tags,
			PostureAttributes: postureAttrs,
			MaxDuration:       JSONDuration(dur),
			RiskLevel:         ParseRiskLevel(c.RiskLevel),
			Approvers:         c.Approvers,
			Action:            action,
			UserAction:        userAction,
		}

		if _, exists := store.types[gt.Name]; exists {
			return nil, fmt.Errorf("duplicate grant type: %q", gt.Name)
		}

		store.types[gt.Name] = gt
		store.order = append(store.order, gt)
	}

	return store, nil
}

// validateTag checks that a tag follows Tailscale's format:
// must start with "tag:", followed by a letter, then alphanumeric or dashes.
func validateTag(tag string) error {
	name, ok := strings.CutPrefix(tag, "tag:")
	if !ok {
		return fmt.Errorf("tag %q must start with \"tag:\"", tag)
	}
	if name == "" {
		return fmt.Errorf("tag %q has empty name", tag)
	}
	if !isAlpha(name[0]) {
		return fmt.Errorf("tag %q name must start with a letter", tag)
	}
	for _, b := range []byte(name) {
		if !isAlpha(b) && !isNum(b) && b != '-' {
			return fmt.Errorf("tag %q contains invalid character %q (only letters, numbers, dashes allowed)", tag, string(b))
		}
	}
	return nil
}

func isAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isNum(b byte) bool   { return b >= '0' && b <= '9' }

func validatePostureAttribute(pa config.PostureAttributeConfig) error {
	if !strings.HasPrefix(pa.Key, "custom:") {
		return fmt.Errorf("posture attribute key %q must start with \"custom:\"", pa.Key)
	}
	if pa.Value == nil {
		return fmt.Errorf("posture attribute %q must have a value", pa.Key)
	}
	switch pa.Target {
	case "", "requester", "target":
		// valid
	default:
		return fmt.Errorf("posture attribute %q has invalid target %q (must be \"requester\" or \"target\")", pa.Key, pa.Target)
	}
	return nil
}

func convertPostureAttributes(configs []config.PostureAttributeConfig) []PostureAttribute {
	if len(configs) == 0 {
		return nil
	}
	attrs := make([]PostureAttribute, len(configs))
	for i, c := range configs {
		target := c.Target
		if target == "" {
			target = "requester"
		}
		attrs[i] = PostureAttribute{
			Key:    c.Key,
			Value:  c.Value,
			Target: target,
		}
	}
	return attrs
}

func (s *YAMLGrantTypeStore) Get(name string) (*GrantType, error) {
	gt, ok := s.types[name]
	if !ok {
		return nil, fmt.Errorf("unknown grant type: %q", name)
	}
	return gt, nil
}

func (s *YAMLGrantTypeStore) List() ([]*GrantType, error) {
	out := make([]*GrantType, len(s.order))
	copy(out, s.order)
	return out, nil
}

// EvaluatePolicy returns true if the grant type can be auto-approved
// (i.e., risk level is Low and no explicit approvers are required).
func EvaluatePolicy(grantType *GrantType, requesterLogin string) (autoApprove bool) {
	return grantType.RiskLevel == RiskLow
}
