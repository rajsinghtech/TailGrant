package grant

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func setupIntegrationTestEnv() *testsuite.TestWorkflowEnvironment {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	activities := &Activities{}
	env.RegisterActivity(activities.SignalWithStartDeviceTagManager)
	env.RegisterActivity(activities.SetDeviceTags)
	env.RegisterWorkflow(ApprovalWorkflow)
	env.RegisterWorkflow(DeviceTagManagerWorkflow)

	// Allow the remove-grant signal to the DeviceTagManager.
	env.OnSignalExternalWorkflow(mock.Anything, mock.Anything, mock.Anything, "remove-grant", mock.Anything).Return(nil)

	return env
}

func TestIntegration_FullGrantLifecycle_AutoApprove(t *testing.T) {
	env := setupIntegrationTestEnv()

	env.OnActivity("SignalWithStartDeviceTagManager", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	grantType := GrantType{
		Name:        "ssh-access",
		Description: "SSH access to production servers",
		Tags:        []string{"tag:prod-ssh"},
		MaxDuration: time.Hour,
		RiskLevel:   RiskLow,
		Approvers:   []string{},
	}

	request := GrantRequest{
		ID:            "grant-123",
		Requester:     "user@example.com",
		RequesterNode: "requester-node",
		GrantTypeName: "ssh-access",
		TargetNodeID:  "target-node",
		Duration:      30 * time.Minute,
		Reason:        "Deploy hotfix",
		RequestedAt:   time.Now(),
	}

	env.ExecuteWorkflow(GrantWorkflow, request, grantType)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result GrantState
	require.NoError(t, env.GetWorkflowResult(&result))

	encodedValue, err := env.QueryWorkflow("status")
	require.NoError(t, err)

	var status GrantState
	require.NoError(t, encodedValue.Get(&status))

	require.Equal(t, StatusExpired, status.Status)
	require.Empty(t, status.ApprovedBy)
}

func TestIntegration_FullGrantLifecycle_WithApproval(t *testing.T) {
	env := setupIntegrationTestEnv()

	env.OnActivity("SignalWithStartDeviceTagManager", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	approvalResult := ApprovalResult{
		Approved:   true,
		ApprovedBy: "admin@example.com",
	}
	env.OnWorkflow("ApprovalWorkflow", mock.Anything, mock.Anything, mock.Anything).Return(approvalResult, nil)

	grantType := GrantType{
		Name:        "root-access",
		Description: "Root access to production servers",
		Tags:        []string{"tag:prod-root"},
		MaxDuration: time.Hour,
		RiskLevel:   RiskHigh,
		Approvers:   []string{"admin@example.com"},
	}

	request := GrantRequest{
		ID:            "grant-456",
		Requester:     "user@example.com",
		RequesterNode: "requester-node",
		GrantTypeName: "root-access",
		TargetNodeID:  "target-node",
		Duration:      30 * time.Minute,
		Reason:        "Emergency maintenance",
		RequestedAt:   time.Now(),
	}

	env.ExecuteWorkflow(GrantWorkflow, request, grantType)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result GrantState
	require.NoError(t, env.GetWorkflowResult(&result))

	encodedValue, err := env.QueryWorkflow("status")
	require.NoError(t, err)

	var status GrantState
	require.NoError(t, encodedValue.Get(&status))

	require.Equal(t, StatusExpired, status.Status)
	require.Equal(t, "admin@example.com", status.ApprovedBy)
}
