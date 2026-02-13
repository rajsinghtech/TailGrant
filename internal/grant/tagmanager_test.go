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
	env.RegisterActivity(activities.SetPostureAttribute)
	env.RegisterActivity(activities.DeletePostureAttribute)

	return env, testSuite
}

func TestDeviceTagManager_AddAndRemoveGrant(t *testing.T) {
	env, _ := setupTagManagerTestEnv()

	state := DeviceTagManagerState{
		NodeID:       "node-123",
		ActiveGrants: make(map[string]GrantAssets),
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
		ActiveGrants: make(map[string]GrantAssets),
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
		ActiveGrants: make(map[string]GrantAssets),
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
		ActiveGrants: make(map[string]GrantAssets),
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
	activeGrants := map[string]GrantAssets{
		"grant-1": {Tags: []string{"tag:jit-read", "tag:jit-logs"}},
		"grant-2": {Tags: []string{"tag:jit-write"}},
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
	activeGrants := map[string]GrantAssets{
		"grant-2": {Tags: []string{"tag:debug-granted"}},
	}
	removedTags := []string{"tag:ssh-granted"}

	result := computeDesiredTags(currentTags, activeGrants, removedTags)

	require.Len(t, result, 2)
	require.Contains(t, result, "tag:server")
	require.Contains(t, result, "tag:debug-granted")
	require.NotContains(t, result, "tag:ssh-granted")
}

func TestDeviceTagManager_ComputeDesiredTags_SharedTagPreserved(t *testing.T) {
	currentTags := []string{"tag:server", "tag:common", "tag:grant-a-only"}
	activeGrants := map[string]GrantAssets{
		"grant-b": {Tags: []string{"tag:common", "tag:grant-b-only"}},
	}
	removedTags := []string{"tag:common", "tag:grant-a-only"}

	result := computeDesiredTags(currentTags, activeGrants, removedTags)

	require.Contains(t, result, "tag:server")
	require.Contains(t, result, "tag:common")
	require.Contains(t, result, "tag:grant-b-only")
	require.NotContains(t, result, "tag:grant-a-only")
}

func TestDeviceTagManager_ComputeDesiredTags_EmptyGrants(t *testing.T) {
	currentTags := []string{"tag:server"}
	activeGrants := map[string]GrantAssets{}
	removedTags := []string{}

	result := computeDesiredTags(currentTags, activeGrants, removedTags)

	require.Len(t, result, 1)
	require.Contains(t, result, "tag:server")
}

func TestDeviceTagManager_WithPostureAttributes(t *testing.T) {
	env, _ := setupTagManagerTestEnv()

	state := DeviceTagManagerState{
		NodeID:       "node-posture",
		ActiveGrants: make(map[string]GrantAssets),
	}

	env.OnActivity("SetPostureAttribute", mock.Anything, "requester-node", "custom:jit-ssh", "granted").Return(nil)
	env.OnActivity("DeletePostureAttribute", mock.Anything, "requester-node", "custom:jit-ssh").Return(nil)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("add-grant", AddGrantSignal{
			GrantID: "grant-posture",
			PostureAttributes: []PostureAttribute{
				{Key: "custom:jit-ssh", Value: "granted", Target: "requester"},
			},
			RequesterNodeID: "requester-node",
		})
	}, 0)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("remove-grant", RemoveGrantSignal{
			GrantID: "grant-posture",
		})
	}, 0)

	env.ExecuteWorkflow(DeviceTagManagerWorkflow, state)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	env.AssertExpectations(t)
}

func TestDeviceTagManager_PostureAttributeTargetDevice(t *testing.T) {
	env, _ := setupTagManagerTestEnv()

	state := DeviceTagManagerState{
		NodeID:       "target-node",
		ActiveGrants: make(map[string]GrantAssets),
	}

	env.OnActivity("SetPostureAttribute", mock.Anything, "target-node", "custom:jit-access", "true").Return(nil)
	env.OnActivity("DeletePostureAttribute", mock.Anything, "target-node", "custom:jit-access").Return(nil)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("add-grant", AddGrantSignal{
			GrantID: "grant-target-posture",
			PostureAttributes: []PostureAttribute{
				{Key: "custom:jit-access", Value: "true", Target: "target"},
			},
			RequesterNodeID: "requester-node",
		})
	}, 0)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("remove-grant", RemoveGrantSignal{
			GrantID: "grant-target-posture",
		})
	}, 0)

	env.ExecuteWorkflow(DeviceTagManagerWorkflow, state)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	env.AssertExpectations(t)
}

func TestResolvePostureTarget(t *testing.T) {
	tests := []struct {
		name            string
		targetNodeID    string
		pa              PostureAttribute
		requesterNodeID string
		want            string
	}{
		{
			name:            "requester target",
			targetNodeID:    "target-1",
			pa:              PostureAttribute{Key: "custom:test", Value: "v", Target: "requester"},
			requesterNodeID: "requester-1",
			want:            "requester-1",
		},
		{
			name:            "target target",
			targetNodeID:    "target-1",
			pa:              PostureAttribute{Key: "custom:test", Value: "v", Target: "target"},
			requesterNodeID: "requester-1",
			want:            "target-1",
		},
		{
			name:            "empty target defaults to requester",
			targetNodeID:    "target-1",
			pa:              PostureAttribute{Key: "custom:test", Value: "v", Target: ""},
			requesterNodeID: "requester-1",
			want:            "requester-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePostureTarget(tt.targetNodeID, tt.pa, tt.requesterNodeID)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestOrphanedPostureAttributes(t *testing.T) {
	t.Run("shared key preserved", func(t *testing.T) {
		removed := GrantAssets{
			PostureAttributes: []PostureAttribute{
				{Key: "custom:shared", Value: "v", Target: "requester"},
			},
			RequesterNodeID: "req-1",
		}
		active := map[string]GrantAssets{
			"g2": {
				PostureAttributes: []PostureAttribute{
					{Key: "custom:shared", Value: "v", Target: "requester"},
				},
				RequesterNodeID: "req-1",
			},
		}
		orphaned := orphanedPostureAttributes(removed, active, "target-1")
		require.Empty(t, orphaned)
	})

	t.Run("unique key removed", func(t *testing.T) {
		removed := GrantAssets{
			PostureAttributes: []PostureAttribute{
				{Key: "custom:unique", Value: "v", Target: "requester"},
			},
			RequesterNodeID: "req-1",
		}
		active := map[string]GrantAssets{
			"g2": {
				PostureAttributes: []PostureAttribute{
					{Key: "custom:other", Value: "v", Target: "target"},
				},
				RequesterNodeID: "req-1",
			},
		}
		orphaned := orphanedPostureAttributes(removed, active, "target-1")
		require.Len(t, orphaned, 1)
		require.Equal(t, "custom:unique", orphaned[0].Key)
	})

	t.Run("same key different devices not shared", func(t *testing.T) {
		removed := GrantAssets{
			PostureAttributes: []PostureAttribute{
				{Key: "custom:jit", Value: "v", Target: "requester"},
			},
			RequesterNodeID: "req-1",
		}
		active := map[string]GrantAssets{
			"g2": {
				PostureAttributes: []PostureAttribute{
					{Key: "custom:jit", Value: "v", Target: "requester"},
				},
				RequesterNodeID: "req-2", // different requester device
			},
		}
		orphaned := orphanedPostureAttributes(removed, active, "target-1")
		require.Len(t, orphaned, 1)
		require.Equal(t, "custom:jit", orphaned[0].Key)
	})

	t.Run("no active grants all orphaned", func(t *testing.T) {
		removed := GrantAssets{
			PostureAttributes: []PostureAttribute{
				{Key: "custom:a", Value: "v", Target: "target"},
				{Key: "custom:b", Value: "v", Target: "requester"},
			},
			RequesterNodeID: "req-1",
		}
		active := map[string]GrantAssets{}
		orphaned := orphanedPostureAttributes(removed, active, "target-1")
		require.Len(t, orphaned, 2)
	})
}
