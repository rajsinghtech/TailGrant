package grant

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	tailscale "tailscale.com/client/tailscale/v2"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func setupReconcileTestEnv() (*testsuite.TestWorkflowEnvironment, *testsuite.WorkflowTestSuite) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	activities := &Activities{}
	env.RegisterActivity(activities.ListDevices)
	env.RegisterActivity(activities.SetDeviceTags)
	env.RegisterActivity(activities.CheckWorkflowExists)
	env.RegisterActivity(activities.QueryActiveGrants)
	env.RegisterActivity(activities.GetPostureAttributes)
	env.RegisterActivity(activities.DeletePostureAttribute)

	return env, testSuite
}

func TestReconciliationWorkflow_StaleOrphanedTags(t *testing.T) {
	env, _ := setupReconcileTestEnv()

	devices := []tailscale.Device{
		{NodeID: "node-1", Tags: []string{"tag:server", "tag:ssh-granted"}},
		{NodeID: "node-2", Tags: []string{"tag:server"}},
		{NodeID: "node-3", Tags: []string{"tag:admin-granted"}},
	}

	env.OnActivity("ListDevices", mock.Anything).Return(devices, nil)
	env.OnActivity("CheckWorkflowExists", mock.Anything, "device-tags-node-1").Return(false, nil)
	env.OnActivity("CheckWorkflowExists", mock.Anything, "device-tags-node-3").Return(true, nil)
	env.OnActivity("SetDeviceTags", mock.Anything, "node-1", []string{"tag:server"}).Return(nil)
	env.OnActivity("QueryActiveGrants", mock.Anything, "device-tags-node-3").Return(
		map[string]GrantAssets{"g1": {Tags: []string{"tag:admin-granted"}}}, nil)

	input := ReconciliationInput{GrantTags: []string{"tag:ssh-granted", "tag:admin-granted"}}
	env.ExecuteWorkflow(ReconciliationWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())

	err := env.GetWorkflowError()
	require.Error(t, err)
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr)

	env.AssertExpectations(t)
}

func TestReconciliationWorkflow_NoStaleGrantTags(t *testing.T) {
	env, _ := setupReconcileTestEnv()

	devices := []tailscale.Device{
		{NodeID: "node-1", Tags: []string{"tag:server", "tag:production"}},
		{NodeID: "node-2", Tags: []string{"tag:database"}},
	}

	env.OnActivity("ListDevices", mock.Anything).Return(devices, nil)

	input := ReconciliationInput{GrantTags: []string{"tag:ssh-granted", "tag:admin-granted"}}
	env.ExecuteWorkflow(ReconciliationWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())

	err := env.GetWorkflowError()
	require.Error(t, err)
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr)

	env.AssertExpectations(t)
}

func TestReconciliationWorkflow_GrantTagsWithActiveManager(t *testing.T) {
	env, _ := setupReconcileTestEnv()

	devices := []tailscale.Device{
		{NodeID: "node-managed", Tags: []string{"tag:server", "tag:ssh-granted", "tag:debug-granted"}},
	}

	env.OnActivity("ListDevices", mock.Anything).Return(devices, nil)
	env.OnActivity("CheckWorkflowExists", mock.Anything, "device-tags-node-managed").Return(true, nil)
	env.OnActivity("QueryActiveGrants", mock.Anything, "device-tags-node-managed").Return(
		map[string]GrantAssets{
			"g1": {Tags: []string{"tag:ssh-granted"}},
			"g2": {Tags: []string{"tag:debug-granted"}},
		}, nil)

	input := ReconciliationInput{GrantTags: []string{"tag:ssh-granted", "tag:debug-granted"}}
	env.ExecuteWorkflow(ReconciliationWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())

	err := env.GetWorkflowError()
	require.Error(t, err)
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr)

	env.AssertExpectations(t)
}

func TestReconciliationWorkflow_MultipleStaleDevices(t *testing.T) {
	env, _ := setupReconcileTestEnv()

	devices := []tailscale.Device{
		{NodeID: "node-stale-1", Tags: []string{"tag:server", "tag:admin-granted"}},
		{NodeID: "node-stale-2", Tags: []string{"tag:ssh-granted"}},
		{NodeID: "node-clean", Tags: []string{"tag:server"}},
	}

	env.OnActivity("ListDevices", mock.Anything).Return(devices, nil)
	env.OnActivity("CheckWorkflowExists", mock.Anything, "device-tags-node-stale-1").Return(false, nil)
	env.OnActivity("CheckWorkflowExists", mock.Anything, "device-tags-node-stale-2").Return(false, nil)
	env.OnActivity("SetDeviceTags", mock.Anything, "node-stale-1", []string{"tag:server"}).Return(nil)
	env.OnActivity("SetDeviceTags", mock.Anything, "node-stale-2", []string(nil)).Return(nil)

	input := ReconciliationInput{GrantTags: []string{"tag:admin-granted", "tag:ssh-granted"}}
	env.ExecuteWorkflow(ReconciliationWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())

	err := env.GetWorkflowError()
	require.Error(t, err)
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr)

	env.AssertExpectations(t)
}

func TestReconciliationWorkflow_StalePostureAttributes(t *testing.T) {
	env, _ := setupReconcileTestEnv()

	// Device has no grant tags but has a stale posture attribute and no tag manager.
	devices := []tailscale.Device{
		{NodeID: "node-posture", Tags: []string{"tag:server"}},
	}

	env.OnActivity("ListDevices", mock.Anything).Return(devices, nil)
	env.OnActivity("GetPostureAttributes", mock.Anything, "node-posture").Return(
		map[string]any{"custom:jit-ssh": "granted", "node:os": "linux"}, nil)
	env.OnActivity("CheckWorkflowExists", mock.Anything, "device-tags-node-posture").Return(false, nil)
	env.OnActivity("DeletePostureAttribute", mock.Anything, "node-posture", "custom:jit-ssh").Return(nil)

	input := ReconciliationInput{
		GrantPostureKeys: []string{"custom:jit-ssh"},
	}
	env.ExecuteWorkflow(ReconciliationWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr)

	env.AssertExpectations(t)
}

func TestReconciliationWorkflow_PostureDriftTriggersSync(t *testing.T) {
	env, _ := setupReconcileTestEnv()

	// Device has a stale posture attribute but the tag manager IS running.
	// The stale key is NOT in the expected set → should trigger sync.
	devices := []tailscale.Device{
		{NodeID: "node-drift", Tags: []string{"tag:server"}},
	}

	env.OnActivity("ListDevices", mock.Anything).Return(devices, nil)
	env.OnActivity("GetPostureAttributes", mock.Anything, "node-drift").Return(
		map[string]any{"custom:jit-ssh": "granted"}, nil)
	env.OnActivity("CheckWorkflowExists", mock.Anything, "device-tags-node-drift").Return(true, nil)
	// Active grants have no target-scoped posture attributes.
	env.OnActivity("QueryActiveGrants", mock.Anything, "device-tags-node-drift").Return(
		map[string]GrantAssets{
			"g1": {Tags: []string{}, PostureAttributes: []PostureAttribute{
				{Key: "custom:jit-ssh", Value: "granted", Target: "requester"},
			}},
		}, nil)
	env.OnSignalExternalWorkflow(mock.Anything, "device-tags-node-drift", "", "sync", mock.Anything).Return(nil)

	input := ReconciliationInput{
		GrantPostureKeys: []string{"custom:jit-ssh"},
	}
	env.ExecuteWorkflow(ReconciliationWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr)

	env.AssertExpectations(t)
}

func TestReconciliationWorkflow_PostureMatchNoSync(t *testing.T) {
	env, _ := setupReconcileTestEnv()

	// Device has a posture attribute that matches active grant's target-scoped posture → no sync.
	devices := []tailscale.Device{
		{NodeID: "node-ok", Tags: []string{"tag:server"}},
	}

	env.OnActivity("ListDevices", mock.Anything).Return(devices, nil)
	env.OnActivity("GetPostureAttributes", mock.Anything, "node-ok").Return(
		map[string]any{"custom:jit-access": "true"}, nil)
	env.OnActivity("CheckWorkflowExists", mock.Anything, "device-tags-node-ok").Return(true, nil)
	env.OnActivity("QueryActiveGrants", mock.Anything, "device-tags-node-ok").Return(
		map[string]GrantAssets{
			"g1": {PostureAttributes: []PostureAttribute{
				{Key: "custom:jit-access", Value: "true", Target: "target"},
			}},
		}, nil)

	input := ReconciliationInput{
		GrantPostureKeys: []string{"custom:jit-access"},
	}
	env.ExecuteWorkflow(ReconciliationWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr)

	env.AssertExpectations(t)
}

func TestPartitionTags(t *testing.T) {
	grantTagSet := map[string]struct{}{
		"tag:ssh-granted":   {},
		"tag:admin-granted": {},
	}

	tests := []struct {
		name        string
		tags        []string
		expectGrant []string
		expectOther []string
	}{
		{
			name:        "mixed tags",
			tags:        []string{"tag:server", "tag:ssh-granted", "tag:production", "tag:admin-granted"},
			expectGrant: []string{"tag:ssh-granted", "tag:admin-granted"},
			expectOther: []string{"tag:server", "tag:production"},
		},
		{
			name:        "only grant tags",
			tags:        []string{"tag:ssh-granted", "tag:admin-granted"},
			expectGrant: []string{"tag:ssh-granted", "tag:admin-granted"},
			expectOther: nil,
		},
		{
			name:        "no grant tags",
			tags:        []string{"tag:server", "tag:database"},
			expectGrant: nil,
			expectOther: []string{"tag:server", "tag:database"},
		},
		{
			name:        "empty tags",
			tags:        []string{},
			expectGrant: nil,
			expectOther: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grant, other := partitionTags(tt.tags, grantTagSet)
			require.Equal(t, tt.expectGrant, grant)
			require.Equal(t, tt.expectOther, other)
		})
	}
}

func TestTagsMatch(t *testing.T) {
	tests := []struct {
		name     string
		actual   []string
		expected map[string]struct{}
		match    bool
	}{
		{
			name:     "exact match",
			actual:   []string{"tag:a", "tag:b"},
			expected: map[string]struct{}{"tag:a": {}, "tag:b": {}},
			match:    true,
		},
		{
			name:     "missing expected",
			actual:   []string{"tag:a"},
			expected: map[string]struct{}{"tag:a": {}, "tag:b": {}},
			match:    false,
		},
		{
			name:     "extra actual",
			actual:   []string{"tag:a", "tag:b", "tag:c"},
			expected: map[string]struct{}{"tag:a": {}, "tag:b": {}},
			match:    false,
		},
		{
			name:     "wrong tag",
			actual:   []string{"tag:a", "tag:c"},
			expected: map[string]struct{}{"tag:a": {}, "tag:b": {}},
			match:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.match, tagsMatch(tt.actual, tt.expected))
		})
	}
}
