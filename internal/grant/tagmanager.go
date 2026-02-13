package grant

import (
	"fmt"
	"sort"
	"time"

	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const tagManagerContinueAsNewThreshold = 1000

// DeviceTagManagerState tracks active JIT grants for a single device.
// Current tags are fetched from the API before every mutation to avoid
// overwriting external changes.
type DeviceTagManagerState struct {
	NodeID       string
	ActiveGrants map[string]GrantAssets
}

func DeviceTagManagerWorkflow(ctx workflow.Context, state DeviceTagManagerState) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("DeviceTagManagerWorkflow started", "nodeID", state.NodeID)

	if state.ActiveGrants == nil {
		state.ActiveGrants = make(map[string]GrantAssets)
	}

	if err := workflow.SetQueryHandler(ctx, "active-grants", func() (map[string]GrantAssets, error) {
		return state.ActiveGrants, nil
	}); err != nil {
		return fmt.Errorf("register active-grants query: %w", err)
	}

	actCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 5,
		},
	})

	var activities *Activities
	signalCount := 0

	addCh := workflow.GetSignalChannel(ctx, "add-grant")
	removeCh := workflow.GetSignalChannel(ctx, "remove-grant")
	syncCh := workflow.GetSignalChannel(ctx, "sync")

	for {
		sel := workflow.NewSelector(ctx)

		sel.AddReceive(addCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig AddGrantSignal
			ch.Receive(ctx, &sig)
			signalCount++

			state.ActiveGrants[sig.GrantID] = GrantAssets{
				Tags:              sig.Tags,
				PostureAttributes: sig.PostureAttributes,
				RequesterNodeID:   sig.RequesterNodeID,
			}

			if err := applyTags(ctx, actCtx, activities, state, nil, logger); err != nil {
				logger.Error("Failed to apply tags after add", "nodeID", state.NodeID, "grantID", sig.GrantID, "error", err)
			}
			if err := applyPostureAttributes(ctx, actCtx, activities, state.NodeID, sig.PostureAttributes, sig.RequesterNodeID, logger); err != nil {
				logger.Error("Failed to set posture attributes after add", "nodeID", state.NodeID, "grantID", sig.GrantID, "error", err)
			}
		})

		sel.AddReceive(removeCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig RemoveGrantSignal
			ch.Receive(ctx, &sig)
			signalCount++

			assets := state.ActiveGrants[sig.GrantID]
			delete(state.ActiveGrants, sig.GrantID)

			if err := applyTags(ctx, actCtx, activities, state, assets.Tags, logger); err != nil {
				logger.Error("Failed to apply tags after remove", "nodeID", state.NodeID, "grantID", sig.GrantID, "error", err)
			}
			// Only delete posture attributes not claimed by another active grant.
			orphaned := orphanedPostureAttributes(assets, state.ActiveGrants, state.NodeID)
			if err := removePostureAttributes(ctx, actCtx, activities, state.NodeID, orphaned, assets.RequesterNodeID, logger); err != nil {
				logger.Error("Failed to delete posture attributes after remove", "nodeID", state.NodeID, "grantID", sig.GrantID, "error", err)
			}
		})

		sel.AddReceive(syncCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig SyncSignal
			ch.Receive(ctx, &sig)
			signalCount++

			if err := applyTags(ctx, actCtx, activities, state, nil, logger); err != nil {
				logger.Error("Failed to apply tags after sync", "nodeID", state.NodeID, "error", err)
			}
			if err := syncPostureAttributes(ctx, actCtx, activities, state, logger); err != nil {
				logger.Error("Failed to sync posture attributes", "nodeID", state.NodeID, "error", err)
			}
			logger.Info("Tags and posture attributes resynced via reconciliation", "nodeID", state.NodeID)
		})

		sel.Select(ctx)

		if len(state.ActiveGrants) == 0 {
			logger.Info("No active grants remaining, completing", "nodeID", state.NodeID)
			return nil
		}

		if signalCount >= tagManagerContinueAsNewThreshold {
			logger.Info("ContinueAsNew after processing signals", "nodeID", state.NodeID, "signalCount", signalCount)
			return workflow.NewContinueAsNewError(ctx, DeviceTagManagerWorkflow, state)
		}
	}
}

// applyTags fetches current device tags, strips removedTags (tags from a
// just-removed grant), and merges in all active grant tags. Active grant
// tags are always re-added, so shared tags between grants are preserved.
func applyTags(ctx workflow.Context, actCtx workflow.Context, activities *Activities, state DeviceTagManagerState, removedTags []string, logger log.Logger) error {
	// Collect all tags from active grants to check if any exist.
	hasTags := false
	for _, assets := range state.ActiveGrants {
		if len(assets.Tags) > 0 {
			hasTags = true
			break
		}
	}
	if !hasTags && len(removedTags) == 0 {
		return nil
	}

	var currentTags []string
	if err := workflow.ExecuteActivity(actCtx, activities.GetDeviceTags, state.NodeID).Get(ctx, &currentTags); err != nil {
		return fmt.Errorf("get current tags for %s: %w", state.NodeID, err)
	}

	desired := computeDesiredTags(currentTags, state.ActiveGrants, removedTags)

	if err := workflow.ExecuteActivity(actCtx, activities.SetDeviceTags, state.NodeID, desired).Get(ctx, nil); err != nil {
		return fmt.Errorf("set tags for %s: %w", state.NodeID, err)
	}
	return nil
}

// computeDesiredTags takes the device's current tags, strips any tags from a
// removed grant, then unions in all active grant tags. Tags shared between
// grants are preserved because active grants always re-add them.
func computeDesiredTags(currentTags []string, activeGrants map[string]GrantAssets, removedTags []string) []string {
	removeSet := make(map[string]struct{}, len(removedTags))
	for _, t := range removedTags {
		removeSet[t] = struct{}{}
	}

	seen := make(map[string]struct{})
	for _, t := range currentTags {
		if _, remove := removeSet[t]; !remove {
			seen[t] = struct{}{}
		}
	}
	for _, assets := range activeGrants {
		for _, t := range assets.Tags {
			seen[t] = struct{}{}
		}
	}

	result := make([]string, 0, len(seen))
	for t := range seen {
		result = append(result, t)
	}
	sort.Strings(result)
	return result
}

// resolvePostureTarget determines which device a posture attribute should be set on.
func resolvePostureTarget(targetNodeID string, pa PostureAttribute, requesterNodeID string) string {
	if pa.Target == "target" {
		return targetNodeID
	}
	return requesterNodeID
}

// orphanedPostureAttributes returns only those posture attributes from the
// removed grant that are not claimed by any remaining active grant on the same
// device. This mirrors how computeDesiredTags preserves shared tags.
func orphanedPostureAttributes(removed GrantAssets, active map[string]GrantAssets, targetNodeID string) []PostureAttribute {
	type deviceKey struct {
		deviceID string
		key      string
	}
	claimed := make(map[deviceKey]struct{})
	for _, assets := range active {
		for _, pa := range assets.PostureAttributes {
			did := resolvePostureTarget(targetNodeID, pa, assets.RequesterNodeID)
			claimed[deviceKey{did, pa.Key}] = struct{}{}
		}
	}

	var orphaned []PostureAttribute
	for _, pa := range removed.PostureAttributes {
		did := resolvePostureTarget(targetNodeID, pa, removed.RequesterNodeID)
		if _, ok := claimed[deviceKey{did, pa.Key}]; !ok {
			orphaned = append(orphaned, pa)
		}
	}
	return orphaned
}

// applyPostureAttributes sets posture attributes on the appropriate devices.
func applyPostureAttributes(ctx workflow.Context, actCtx workflow.Context, activities *Activities, targetNodeID string, attrs []PostureAttribute, requesterNodeID string, logger log.Logger) error {
	for _, pa := range attrs {
		deviceID := resolvePostureTarget(targetNodeID, pa, requesterNodeID)
		if deviceID == "" {
			logger.Warn("Skipping posture attribute: no device ID for target", "key", pa.Key, "target", pa.Target)
			continue
		}
		if err := workflow.ExecuteActivity(actCtx, activities.SetPostureAttribute, deviceID, pa.Key, pa.Value).Get(ctx, nil); err != nil {
			return fmt.Errorf("set posture attribute %s on %s: %w", pa.Key, deviceID, err)
		}
	}
	return nil
}

// removePostureAttributes deletes posture attributes from the appropriate devices.
func removePostureAttributes(ctx workflow.Context, actCtx workflow.Context, activities *Activities, targetNodeID string, attrs []PostureAttribute, requesterNodeID string, logger log.Logger) error {
	for _, pa := range attrs {
		deviceID := resolvePostureTarget(targetNodeID, pa, requesterNodeID)
		if deviceID == "" {
			logger.Warn("Skipping posture attribute removal: no device ID for target", "key", pa.Key, "target", pa.Target)
			continue
		}
		if err := workflow.ExecuteActivity(actCtx, activities.DeletePostureAttribute, deviceID, pa.Key).Get(ctx, nil); err != nil {
			return fmt.Errorf("delete posture attribute %s from %s: %w", pa.Key, deviceID, err)
		}
	}
	return nil
}

// syncPostureAttributes re-applies expected posture attributes and removes
// stale grant-managed keys from the target device. Requester-device posture
// attributes are re-applied but not diffed (the reconciliation loop only
// observes the target device).
func syncPostureAttributes(ctx workflow.Context, actCtx workflow.Context, activities *Activities, state DeviceTagManagerState, logger log.Logger) error {
	// Collect expected posture keys per device from active grants.
	type deviceKey struct {
		deviceID string
		key      string
	}
	expected := make(map[deviceKey]struct{})
	for _, assets := range state.ActiveGrants {
		for _, pa := range assets.PostureAttributes {
			did := resolvePostureTarget(state.NodeID, pa, assets.RequesterNodeID)
			expected[deviceKey{did, pa.Key}] = struct{}{}
		}
	}

	// Re-apply all expected posture attributes.
	for _, assets := range state.ActiveGrants {
		if err := applyPostureAttributes(ctx, actCtx, activities, state.NodeID, assets.PostureAttributes, assets.RequesterNodeID, logger); err != nil {
			return err
		}
	}

	// Fetch current posture attributes on the target device and remove stale ones.
	var currentAttrs map[string]any
	if err := workflow.ExecuteActivity(actCtx, activities.GetPostureAttributes, state.NodeID).Get(ctx, &currentAttrs); err != nil {
		logger.Warn("Failed to get target device posture attributes for sync diff", "nodeID", state.NodeID, "error", err)
		return nil
	}
	for key := range currentAttrs {
		dk := deviceKey{state.NodeID, key}
		if _, ok := expected[dk]; !ok {
			// Only remove keys that look grant-managed (custom: prefix).
			if len(key) > 7 && key[:7] == "custom:" {
				logger.Info("Removing stale posture attribute during sync", "nodeID", state.NodeID, "key", key)
				if err := workflow.ExecuteActivity(actCtx, activities.DeletePostureAttribute, state.NodeID, key).Get(ctx, nil); err != nil {
					logger.Error("Failed to remove stale posture attribute during sync", "nodeID", state.NodeID, "key", key, "error", err)
				}
			}
		}
	}
	return nil
}
