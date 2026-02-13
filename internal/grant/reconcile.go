package grant

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const reconcileInterval = 5 * time.Minute

// ReconciliationInput configures the reconciliation loop.
//
// Limitation: posture attribute reconciliation only covers target-device-scoped
// attributes. Attributes with target="requester" live on the requester's device,
// which this per-device loop cannot associate back to a tag manager. The
// DeviceTagManagerWorkflow itself handles requester-scoped attributes correctly
// during normal grant add/remove lifecycle.
type ReconciliationInput struct {
	// GrantTags is the set of all tags that grant types may assign.
	// Devices with these tags but no active DeviceTagManager will have them removed.
	GrantTags []string
	// GrantPostureKeys is the set of all posture attribute keys that grant types may set.
	// Only target-device-scoped attributes are reconciled; see limitation note above.
	GrantPostureKeys []string
}

func ReconciliationWorkflow(ctx workflow.Context, input ReconciliationInput) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("ReconciliationWorkflow started")

	grantTagSet := make(map[string]struct{}, len(input.GrantTags))
	for _, t := range input.GrantTags {
		grantTagSet[t] = struct{}{}
	}

	grantPostureKeySet := make(map[string]struct{}, len(input.GrantPostureKeys))
	for _, k := range input.GrantPostureKeys {
		grantPostureKeySet[k] = struct{}{}
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
		hasGrantTags := len(grantTags) > 0

		// Check for grant-managed posture attributes on this device.
		hasGrantPosture := false
		var stalePostureKeys []string
		if len(grantPostureKeySet) > 0 {
			var deviceAttrs map[string]any
			if err := workflow.ExecuteActivity(actCtx, activities.GetPostureAttributes, device.NodeID).Get(ctx, &deviceAttrs); err != nil {
				logger.Warn("Failed to get posture attributes", "nodeID", device.NodeID, "error", err)
			} else {
				for key := range deviceAttrs {
					if _, ok := grantPostureKeySet[key]; ok {
						hasGrantPosture = true
						stalePostureKeys = append(stalePostureKeys, key)
					}
				}
			}
		}

		if !hasGrantTags && !hasGrantPosture {
			continue
		}

		tagMgrID := fmt.Sprintf("device-tags-%s", device.NodeID)

		var exists bool
		if err := workflow.ExecuteActivity(actCtx, activities.CheckWorkflowExists, tagMgrID).Get(ctx, &exists); err != nil {
			logger.Error("Failed to check tag manager workflow", "nodeID", device.NodeID, "error", err)
			continue
		}

		if !exists {
			var existsNow bool
			if err := workflow.ExecuteActivity(actCtx, activities.CheckWorkflowExists, tagMgrID).Get(ctx, &existsNow); err != nil {
				logger.Error("Failed to re-check tag manager workflow", "nodeID", device.NodeID, "error", err)
				continue
			}
			if existsNow {
				logger.Info("Tag manager appeared on re-check, skipping cleanup", "nodeID", device.NodeID)
				continue
			}

			// Clean up stale tags.
			if hasGrantTags {
				logger.Info("Removing stale grant tags", "nodeID", device.NodeID, "staleTags", grantTags)
				if err := workflow.ExecuteActivity(cleanupCtx, activities.SetDeviceTags, device.NodeID, otherTags).Get(ctx, nil); err != nil {
					logger.Error("Failed to remove stale tags", "nodeID", device.NodeID, "error", err)
				}
			}

			// Clean up stale posture attributes.
			for _, key := range stalePostureKeys {
				logger.Info("Removing stale posture attribute", "nodeID", device.NodeID, "key", key)
				if err := workflow.ExecuteActivity(cleanupCtx, activities.DeletePostureAttribute, device.NodeID, key).Get(ctx, nil); err != nil {
					logger.Error("Failed to remove stale posture attribute", "nodeID", device.NodeID, "key", key, "error", err)
				}
			}
			continue
		}

		// Tag manager exists — query its state and check for drift.
		var activeGrants map[string]GrantAssets
		if err := workflow.ExecuteActivity(actCtx, activities.QueryActiveGrants, tagMgrID).Get(ctx, &activeGrants); err != nil {
			logger.Warn("Failed to query tag manager, triggering sync", "nodeID", device.NodeID, "error", err)
			_ = workflow.SignalExternalWorkflow(ctx, tagMgrID, "", "sync", SyncSignal{}).Get(ctx, nil)
			continue
		}

		// Check tag drift.
		expectedGrantTags := make(map[string]struct{})
		for _, assets := range activeGrants {
			for _, t := range assets.Tags {
				expectedGrantTags[t] = struct{}{}
			}
		}

		tagDrift := hasGrantTags && !tagsMatch(grantTags, expectedGrantTags)

		// Check posture attribute drift on the target device only.
		// Requester-scoped attributes live on a different device and
		// cannot be associated back to this tag manager from the device
		// list alone; they are managed by the tag manager lifecycle.
		expectedPostureKeys := make(map[string]struct{})
		for _, assets := range activeGrants {
			for _, pa := range assets.PostureAttributes {
				if pa.Target == "target" {
					expectedPostureKeys[pa.Key] = struct{}{}
				}
			}
		}
		postureDrift := false
		for _, key := range stalePostureKeys {
			if _, ok := expectedPostureKeys[key]; !ok {
				postureDrift = true
				break
			}
		}

		if tagDrift || postureDrift {
			logger.Info("Drift detected, triggering sync",
				"nodeID", device.NodeID,
				"tagDrift", tagDrift,
				"postureDrift", postureDrift)
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
