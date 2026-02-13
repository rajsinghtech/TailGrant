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
	// node-3 has an active manager; query returns matching grants.
	env.OnActivity("QueryActiveGrants", mock.Anything, "device-tags-node-3").Return(
		map[string][]string{"g1": {"tag:admin-granted"}}, nil)

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
	// Query returns grants that produce exactly the expected tags — no drift.
	env.OnActivity("QueryActiveGrants", mock.Anything, "device-tags-node-managed").Return(
		map[string][]string{
			"g1": {"tag:ssh-granted"},
			"g2": {"tag:debug-granted"},
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
