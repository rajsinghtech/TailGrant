package grant

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func setupTagManagerTestEnv() (*testsuite.TestWorkflowEnvironment, *testsuite.WorkflowTestSuite) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	activities := &Activities{}
	env.RegisterActivity(activities.GetDeviceTags)
	env.RegisterActivity(activities.SetDeviceTags)

	return env, testSuite
}

func TestDeviceTagManager_AddAndRemoveGrant(t *testing.T) {
	env, _ := setupTagManagerTestEnv()

	state := DeviceTagManagerState{
		NodeID:       "node-123",
		ActiveGrants: make(map[string][]string),
	}

	env.OnActivity("GetDeviceTags", mock.Anything, "node-123").Return([]string{"tag:server"}, nil)
	env.OnActivity("SetDeviceTags", mock.Anything, "node-123", mock.Anything).Return(nil)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("add-grant", AddGrantSignal{
			GrantID: "grant-1",
			Tags:    []string{"tag:jit-read"},
		})
	}, 0)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("remove-grant", RemoveGrantSignal{
			GrantID: "grant-1",
		})
	}, 0)

	env.ExecuteWorkflow(DeviceTagManagerWorkflow, state)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	env.AssertExpectations(t)
}

func TestDeviceTagManager_MultipleConcurrentGrants(t *testing.T) {
	env, _ := setupTagManagerTestEnv()

	state := DeviceTagManagerState{
		NodeID:       "node-456",
		ActiveGrants: make(map[string][]string),
	}

	env.OnActivity("GetDeviceTags", mock.Anything, "node-456").Return([]string{"tag:server"}, nil)
	env.OnActivity("SetDeviceTags", mock.Anything, "node-456", mock.Anything).Return(nil)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("add-grant", AddGrantSignal{
			GrantID: "grant-1",
			Tags:    []string{"tag:jit-read"},
		})
	}, 0)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("add-grant", AddGrantSignal{
			GrantID: "grant-2",
			Tags:    []string{"tag:jit-write"},
		})
	}, 0)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("remove-grant", RemoveGrantSignal{
			GrantID: "grant-1",
		})
	}, 0)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("remove-grant", RemoveGrantSignal{
			GrantID: "grant-2",
		})
	}, 0)

	env.ExecuteWorkflow(DeviceTagManagerWorkflow, state)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	env.AssertExpectations(t)
}

func TestDeviceTagManager_CapturesBaseTags(t *testing.T) {
	env, _ := setupTagManagerTestEnv()

	state := DeviceTagManagerState{
		NodeID:       "node-789",
		ActiveGrants: make(map[string][]string),
	}

	baseTags := []string{"tag:server", "tag:production"}
	env.OnActivity("GetDeviceTags", mock.Anything, "node-789").Return(baseTags, nil)
	env.OnActivity("SetDeviceTags", mock.Anything, "node-789", mock.Anything).Return(nil)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("add-grant", AddGrantSignal{
			GrantID: "grant-1",
			Tags:    []string{"tag:jit-admin"},
		})
	}, 0)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("remove-grant", RemoveGrantSignal{
			GrantID: "grant-1",
		})
	}, 0)

	env.ExecuteWorkflow(DeviceTagManagerWorkflow, state)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	env.AssertExpectations(t)
}

func TestDeviceTagManager_ExitsWhenNoActiveGrants(t *testing.T) {
	env, _ := setupTagManagerTestEnv()

	state := DeviceTagManagerState{
		NodeID:       "node-empty",
		ActiveGrants: make(map[string][]string),
	}

	env.OnActivity("GetDeviceTags", mock.Anything, "node-empty").Return([]string{"tag:server"}, nil)
	env.OnActivity("SetDeviceTags", mock.Anything, "node-empty", mock.Anything).Return(nil)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("add-grant", AddGrantSignal{
			GrantID: "temp-grant",
			Tags:    []string{"tag:jit-temp"},
		})
	}, 0)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("remove-grant", RemoveGrantSignal{
			GrantID: "temp-grant",
		})
	}, 0)

	env.ExecuteWorkflow(DeviceTagManagerWorkflow, state)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestDeviceTagManager_ComputeDesiredTags(t *testing.T) {
	currentTags := []string{"tag:server", "tag:production"}
	activeGrants := map[string][]string{
		"grant-1": {"tag:jit-read", "tag:jit-logs"},
		"grant-2": {"tag:jit-write"},
	}
	removedTags := []string{}

	result := computeDesiredTags(currentTags, activeGrants, removedTags)

	require.Len(t, result, 5)
	require.Contains(t, result, "tag:server")
	require.Contains(t, result, "tag:production")
	require.Contains(t, result, "tag:jit-read")
	require.Contains(t, result, "tag:jit-logs")
	require.Contains(t, result, "tag:jit-write")
}

func TestDeviceTagManager_ComputeDesiredTags_WithRemovedTags(t *testing.T) {
	currentTags := []string{"tag:server", "tag:ssh-granted", "tag:debug-granted"}
	activeGrants := map[string][]string{
		"grant-2": {"tag:debug-granted"},
	}
	removedTags := []string{"tag:ssh-granted"}

	result := computeDesiredTags(currentTags, activeGrants, removedTags)

	require.Len(t, result, 2)
	require.Contains(t, result, "tag:server")
	require.Contains(t, result, "tag:debug-granted")
	require.NotContains(t, result, "tag:ssh-granted")
}

func TestDeviceTagManager_ComputeDesiredTags_SharedTagPreserved(t *testing.T) {
	// Two grants share "tag:common". Removing one should keep the tag.
	currentTags := []string{"tag:server", "tag:common", "tag:grant-a-only"}
	activeGrants := map[string][]string{
		"grant-b": {"tag:common", "tag:grant-b-only"},
	}
	removedTags := []string{"tag:common", "tag:grant-a-only"}

	result := computeDesiredTags(currentTags, activeGrants, removedTags)

	require.Contains(t, result, "tag:server")
	require.Contains(t, result, "tag:common")          // re-added by grant-b
	require.Contains(t, result, "tag:grant-b-only")
	require.NotContains(t, result, "tag:grant-a-only") // removed, not in any active grant
}

func TestDeviceTagManager_ComputeDesiredTags_EmptyGrants(t *testing.T) {
	currentTags := []string{"tag:server"}
	activeGrants := map[string][]string{}
	removedTags := []string{}

	result := computeDesiredTags(currentTags, activeGrants, removedTags)

	require.Len(t, result, 1)
	require.Contains(t, result, "tag:server")
}
