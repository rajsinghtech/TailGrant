package grant

import (
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

type GrantStatus string

const (
	StatusPendingApproval GrantStatus = "pending_approval"
	StatusActive          GrantStatus = "active"
	StatusExpired         GrantStatus = "expired"
	StatusRevoked         GrantStatus = "revoked"
	StatusDenied          GrantStatus = "denied"
)

type GrantType struct {
	Name        string
	Description string
	Tags        []string
	MaxDuration time.Duration
	RiskLevel   RiskLevel
	Approvers   []string
}

type GrantRequest struct {
	ID            string
	Requester     string // loginName from WhoIs
	RequesterNode string // nodeID
	GrantTypeName string
	TargetNodeID  string
	Duration      time.Duration
	Reason        string
	RequestedAt   time.Time
}

type GrantState struct {
	Request      GrantRequest
	Status       GrantStatus
	ApprovedBy   string
	ActivatedAt  time.Time
	ExpiresAt    time.Time
	RevokedBy    string
	RevokedAt    time.Time
	OriginalTags []string // tags on device before grant
}

// Workflow signal types

type ApproveSignal struct {
	ApprovedBy string
}

type DenySignal struct {
	DeniedBy string
	Reason   string
}

type RevokeSignal struct {
	RevokedBy string
	Reason    string
}

type ExtendSignal struct {
	ExtendedBy string
	Duration   time.Duration
}

// DeviceTagManager signal types

type AddGrantSignal struct {
	GrantID string
	Tags    []string
}

type RemoveGrantSignal struct {
	GrantID string
}

// SyncSignal triggers the tag manager to re-read current device tags
// and reapply the desired state. Used by reconciliation to fix drift.
type SyncSignal struct{}

type ApprovalResult struct {
	Approved   bool
	ApprovedBy string
	DeniedBy   string
	Reason     string
}
