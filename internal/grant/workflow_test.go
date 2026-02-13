package grant

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func setupWorkflowTestEnv() (*testsuite.TestWorkflowEnvironment, *testsuite.WorkflowTestSuite) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	activities := &Activities{}
	env.RegisterActivity(activities.SignalWithStartDeviceTagManager)
	env.RegisterWorkflow(ApprovalWorkflow)
	env.RegisterWorkflow(DeviceTagManagerWorkflow)

	env.OnSignalExternalWorkflow(mock.Anything, mock.Anything, mock.Anything, "remove-grant", mock.Anything).Return(nil)

	return env, testSuite
}

func TestGrantWorkflow_AutoApprove(t *testing.T) {
	env, _ := setupWorkflowTestEnv()

	request := GrantRequest{
		ID:           "grant-123",
		Requester:    "user@example.com",
		TargetNodeID: "node-456",
		Duration:     1 * time.Minute,
	}

	grantType := GrantType{
		Name:      "low-risk-access",
		Tags:      []string{"tag:jit-read"},
		RiskLevel: RiskLow,
	}

	env.OnActivity("SignalWithStartDeviceTagManager", mock.Anything, "node-456", mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(GrantWorkflow, request, grantType)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result GrantState
	require.NoError(t, env.GetWorkflowResult(&result))

	require.Equal(t, StatusExpired, result.Status)
}

func TestGrantWorkflow_RequiresApproval_Approved(t *testing.T) {
	env, _ := setupWorkflowTestEnv()

	request := GrantRequest{
		ID:           "grant-789",
		Requester:    "user@example.com",
		TargetNodeID: "node-999",
		Duration:     30 * time.Minute,
	}

	grantType := GrantType{
		Name:      "high-risk-access",
		Tags:      []string{"tag:jit-admin"},
		RiskLevel: RiskHigh,
	}

	approvalResult := ApprovalResult{
		Approved:   true,
		ApprovedBy: "approver@example.com",
	}

	env.OnWorkflow("ApprovalWorkflow", mock.Anything, "grant-789", grantType, "user@example.com").Return(approvalResult, nil)
	env.OnActivity("SignalWithStartDeviceTagManager", mock.Anything, "node-999", mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(GrantWorkflow, request, grantType)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result GrantState
	require.NoError(t, env.GetWorkflowResult(&result))

	require.Equal(t, StatusExpired, result.Status)
	require.Equal(t, "approver@example.com", result.ApprovedBy)
}

func TestGrantWorkflow_RequiresApproval_Denied(t *testing.T) {
	env, _ := setupWorkflowTestEnv()

	request := GrantRequest{
		ID:           "grant-321",
		Requester:    "user@example.com",
		TargetNodeID: "node-654",
		Duration:     1 * time.Hour,
	}

	grantType := GrantType{
		Name:      "high-risk-access",
		Tags:      []string{"tag:jit-admin"},
		RiskLevel: RiskHigh,
	}

	approvalResult := ApprovalResult{
		Approved: false,
		DeniedBy: "approver@example.com",
		Reason:   "insufficient justification",
	}

	env.OnWorkflow("ApprovalWorkflow", mock.Anything, "grant-321", grantType, "user@example.com").Return(approvalResult, nil)

	env.ExecuteWorkflow(GrantWorkflow, request, grantType)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result GrantState
	require.NoError(t, env.GetWorkflowResult(&result))

	require.Equal(t, StatusDenied, result.Status)
}

func TestGrantWorkflow_Revoked(t *testing.T) {
	env, _ := setupWorkflowTestEnv()

	request := GrantRequest{
		ID:           "grant-555",
		Requester:    "user@example.com",
		TargetNodeID: "node-888",
		Duration:     10 * time.Minute,
	}

	grantType := GrantType{
		Name:      "low-risk-access",
		Tags:      []string{"tag:jit-read"},
		RiskLevel: RiskLow,
	}

	env.OnActivity("SignalWithStartDeviceTagManager", mock.Anything, "node-888", mock.Anything, mock.Anything).Return(nil)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("revoke", RevokeSignal{
			RevokedBy: "admin@example.com",
			Reason:    "security incident",
		})
	}, 30*time.Second)

	env.ExecuteWorkflow(GrantWorkflow, request, grantType)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result GrantState
	require.NoError(t, env.GetWorkflowResult(&result))

	require.Equal(t, StatusRevoked, result.Status)
	require.Equal(t, "admin@example.com", result.RevokedBy)
	require.NotZero(t, result.RevokedAt)
}

func TestGrantWorkflow_QueryStatus(t *testing.T) {
	env, _ := setupWorkflowTestEnv()

	request := GrantRequest{
		ID:           "grant-query",
		Requester:    "user@example.com",
		TargetNodeID: "node-123",
		Duration:     5 * time.Minute,
	}

	grantType := GrantType{
		Name:      "low-risk-access",
		Tags:      []string{"tag:jit-read"},
		RiskLevel: RiskLow,
	}

	env.OnActivity("SignalWithStartDeviceTagManager", mock.Anything, "node-123", mock.Anything, mock.Anything).Return(nil)

	env.RegisterDelayedCallback(func() {
		encoded, err := env.QueryWorkflow("status")
		require.NoError(t, err)

		var state GrantState
		require.NoError(t, encoded.Get(&state))
		require.Equal(t, StatusActive, state.Status)
	}, 1*time.Second)

	env.ExecuteWorkflow(GrantWorkflow, request, grantType)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestGrantWorkflow_Extend(t *testing.T) {
	env, _ := setupWorkflowTestEnv()

	request := GrantRequest{
		ID:           "grant-extend",
		Requester:    "user@example.com",
		TargetNodeID: "node-777",
		Duration:     1 * time.Minute,
	}

	grantType := GrantType{
		Name:      "low-risk-access",
		Tags:      []string{"tag:jit-read"},
		RiskLevel: RiskLow,
	}

	env.OnActivity("SignalWithStartDeviceTagManager", mock.Anything, "node-777", mock.Anything, mock.Anything).Return(nil)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("extend", ExtendSignal{
			ExtendedBy: "user@example.com",
			Duration:   5 * time.Minute,
		})
	}, 30*time.Second)

	env.ExecuteWorkflow(GrantWorkflow, request, grantType)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result GrantState
	require.NoError(t, env.GetWorkflowResult(&result))

	require.Equal(t, StatusExpired, result.Status)
}
