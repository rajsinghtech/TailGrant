package grant

import (
	"encoding/json"
	"strings"
	"time"
)

type RiskLevel int

const (
	RiskLow RiskLevel = iota
	RiskMedium
	RiskHigh
)

func (r RiskLevel) String() string {
	switch r {
	case RiskLow:
		return "low"
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	default:
		return "unknown"
	}
}

func ParseRiskLevel(s string) RiskLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "medium":
		return RiskMedium
	case "high":
		return RiskHigh
	default:
		return RiskLow
	}
}

func (r RiskLevel) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

func (r *RiskLevel) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*r = ParseRiskLevel(s)
	return nil
}

type JSONDuration time.Duration

func (d JSONDuration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *JSONDuration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = JSONDuration(dur)
	return nil
}

type GrantStatus string

const (
	StatusPendingApproval GrantStatus = "pending_approval"
	StatusActive          GrantStatus = "active"
	StatusExpired         GrantStatus = "expired"
	StatusRevoked         GrantStatus = "revoked"
	StatusDenied          GrantStatus = "denied"
)

type GrantType struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Tags        []string     `json:"tags"`
	MaxDuration JSONDuration `json:"maxDuration"`
	RiskLevel   RiskLevel    `json:"riskLevel"`
	Approvers   []string     `json:"approvers"`
}

type GrantRequest struct {
	ID            string        `json:"id"`
	Requester     string        `json:"requester"`
	RequesterNode string        `json:"requesterNode"`
	GrantTypeName string        `json:"grantTypeName"`
	TargetNodeID  string        `json:"targetNodeID"`
	Duration      time.Duration `json:"duration"`
	Reason        string        `json:"reason"`
	RequestedAt   time.Time     `json:"requestedAt"`
}

type GrantState struct {
	Request      GrantRequest `json:"request"`
	Status       GrantStatus  `json:"status"`
	ApprovedBy   string       `json:"approvedBy"`
	ActivatedAt  time.Time    `json:"activatedAt"`
	ExpiresAt    time.Time    `json:"expiresAt"`
	RevokedBy    string       `json:"revokedBy"`
	RevokedAt    time.Time    `json:"revokedAt"`
	OriginalTags []string     `json:"originalTags"`
}

// Workflow signal types

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

// DeviceTagManager signal types

type AddGrantSignal struct {
	GrantID string   `json:"grantID"`
	Tags    []string `json:"tags"`
}

type RemoveGrantSignal struct {
	GrantID string `json:"grantID"`
}

// SyncSignal triggers the tag manager to re-read current device tags
// and reapply the desired state. Used by reconciliation to fix drift.
type SyncSignal struct{}

type ApprovalResult struct {
	Approved   bool   `json:"approved"`
	ApprovedBy string `json:"approvedBy"`
	DeniedBy   string `json:"deniedBy"`
	Reason     string `json:"reason"`
}
