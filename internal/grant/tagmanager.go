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
	ActiveGrants map[string][]string
}

func DeviceTagManagerWorkflow(ctx workflow.Context, state DeviceTagManagerState) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("DeviceTagManagerWorkflow started", "nodeID", state.NodeID)

	if state.ActiveGrants == nil {
		state.ActiveGrants = make(map[string][]string)
	}

	if err := workflow.SetQueryHandler(ctx, "active-grants", func() (map[string][]string, error) {
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

			state.ActiveGrants[sig.GrantID] = sig.Tags

			if err := applyTags(ctx, actCtx, activities, state, nil, logger); err != nil {
				logger.Error("Failed to apply tags after add", "nodeID", state.NodeID, "grantID", sig.GrantID, "error", err)
			}
		})

		sel.AddReceive(removeCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig RemoveGrantSignal
			ch.Receive(ctx, &sig)
			signalCount++

			removedTags := state.ActiveGrants[sig.GrantID]
			delete(state.ActiveGrants, sig.GrantID)

			if err := applyTags(ctx, actCtx, activities, state, removedTags, logger); err != nil {
				logger.Error("Failed to apply tags after remove", "nodeID", state.NodeID, "grantID", sig.GrantID, "error", err)
			}
		})

		sel.AddReceive(syncCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig SyncSignal
			ch.Receive(ctx, &sig)
			signalCount++

			if err := applyTags(ctx, actCtx, activities, state, nil, logger); err != nil {
				logger.Error("Failed to apply tags after sync", "nodeID", state.NodeID, "error", err)
			}
			logger.Info("Tags resynced via reconciliation", "nodeID", state.NodeID)
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
func computeDesiredTags(currentTags []string, activeGrants map[string][]string, removedTags []string) []string {
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
	for _, tags := range activeGrants {
		for _, t := range tags {
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
