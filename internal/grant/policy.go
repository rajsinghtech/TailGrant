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

		if len(c.Tags) == 0 {
			return nil, fmt.Errorf("grant type %q: must have at least one tag", c.Name)
		}
		for _, tag := range c.Tags {
			if err := validateTag(tag); err != nil {
				return nil, fmt.Errorf("grant type %q: %w", c.Name, err)
			}
		}

		if ParseRiskLevel(c.RiskLevel) > RiskLow && len(c.Approvers) == 0 {
			return nil, fmt.Errorf("grant type %q: medium/high risk requires at least one approver", c.Name)
		}

		gt := &GrantType{
			Name:        c.Name,
			Description: c.Description,
			Tags:        c.Tags,
			MaxDuration: JSONDuration(dur),
			RiskLevel:   ParseRiskLevel(c.RiskLevel),
			Approvers:   c.Approvers,
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
