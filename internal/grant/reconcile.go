package grant

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const reconcileInterval = 5 * time.Minute

// ReconciliationInput configures the reconciliation loop.
type ReconciliationInput struct {
	// GrantTags is the set of all tags that grant types may assign.
	// Devices with these tags but no active DeviceTagManager will have them removed.
	GrantTags []string
}

func ReconciliationWorkflow(ctx workflow.Context, input ReconciliationInput) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("ReconciliationWorkflow started")

	grantTagSet := make(map[string]struct{}, len(input.GrantTags))
	for _, t := range input.GrantTags {
		grantTagSet[t] = struct{}{}
	}

	actCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 5,
		},
	})

	var activities *Activities
	var devices []DeviceInfo
	if err := workflow.ExecuteActivity(actCtx, activities.ListDevices).Get(ctx, &devices); err != nil {
		logger.Error("Failed to list devices", "error", err)
		return sleepAndContinue(ctx, input)
	}

	cleanupCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	for _, device := range devices {
		grantTags, otherTags := partitionTags(device.Tags, grantTagSet)
		if len(grantTags) == 0 {
			continue
		}

		tagMgrID := fmt.Sprintf("device-tags-%s", device.NodeID)

		var exists bool
		if err := workflow.ExecuteActivity(actCtx, activities.CheckWorkflowExists, tagMgrID).Get(ctx, &exists); err != nil {
			logger.Error("Failed to check tag manager workflow", "nodeID", device.NodeID, "error", err)
			continue
		}

		if !exists {
			logger.Info("Removing stale grant tags", "nodeID", device.NodeID, "staleTags", grantTags)
			if err := workflow.ExecuteActivity(cleanupCtx, activities.SetDeviceTags, device.NodeID, otherTags).Get(ctx, nil); err != nil {
				logger.Error("Failed to remove stale tags", "nodeID", device.NodeID, "error", err)
			}
			continue
		}

		// Tag manager exists — query its state and check for drift.
		var activeGrants map[string][]string
		if err := workflow.ExecuteActivity(actCtx, activities.QueryActiveGrants, tagMgrID).Get(ctx, &activeGrants); err != nil {
			logger.Warn("Failed to query tag manager, triggering sync", "nodeID", device.NodeID, "error", err)
			_ = workflow.SignalExternalWorkflow(ctx, tagMgrID, "", "sync", SyncSignal{}).Get(ctx, nil)
			continue
		}

		expectedGrantTags := make(map[string]struct{})
		for _, tags := range activeGrants {
			for _, t := range tags {
				expectedGrantTags[t] = struct{}{}
			}
		}

		if !tagsMatch(grantTags, expectedGrantTags) {
			logger.Info("Tag drift detected, triggering sync",
				"nodeID", device.NodeID,
				"actualGrantTags", grantTags,
				"expectedGrantCount", len(expectedGrantTags))
			_ = workflow.SignalExternalWorkflow(ctx, tagMgrID, "", "sync", SyncSignal{}).Get(ctx, nil)
		}
	}

	return sleepAndContinue(ctx, input)
}

// DeviceInfo is a minimal projection of device data for reconciliation.
type DeviceInfo struct {
	NodeID string   `json:"nodeId"`
	Tags   []string `json:"tags"`
}

// partitionTags splits tags into those managed by grants and everything else.
func partitionTags(tags []string, grantTagSet map[string]struct{}) (grant, other []string) {
	for _, t := range tags {
		if _, ok := grantTagSet[t]; ok {
			grant = append(grant, t)
		} else {
			other = append(other, t)
		}
	}
	return
}

// tagsMatch checks if the actual grant tags on a device match the expected set.
func tagsMatch(actual []string, expected map[string]struct{}) bool {
	if len(actual) != len(expected) {
		return false
	}
	for _, t := range actual {
		if _, ok := expected[t]; !ok {
			return false
		}
	}
	return true
}

func sleepAndContinue(ctx workflow.Context, input ReconciliationInput) error {
	if err := workflow.Sleep(ctx, reconcileInterval); err != nil {
		return err
	}
	return workflow.NewContinueAsNewError(ctx, ReconciliationWorkflow, input)
}
