package grant

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

func GrantWorkflow(ctx workflow.Context, request GrantRequest, grantType GrantType) (GrantState, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("GrantWorkflow started", "grantID", request.ID, "grantType", grantType.Name)

	state := GrantState{
		Request: request,
		Status:  StatusPendingApproval,
	}

	if err := workflow.SetQueryHandler(ctx, "status", func() (GrantState, error) {
		return state, nil
	}); err != nil {
		return state, fmt.Errorf("register status query: %w", err)
	}

	actCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 5,
		},
	})

	// Approval gate for non-low-risk grants
	if grantType.RiskLevel > RiskLow {
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: fmt.Sprintf("approval-%s", request.ID),
		})
		var result ApprovalResult
		if err := workflow.ExecuteChildWorkflow(childCtx, ApprovalWorkflow, request.ID, grantType).Get(ctx, &result); err != nil {
			return state, fmt.Errorf("approval workflow: %w", err)
		}
		if !result.Approved {
			state.Status = StatusDenied
			logger.Info("Grant denied", "grantID", request.ID, "deniedBy", result.DeniedBy, "reason", result.Reason)
			return state, nil
		}
		state.ApprovedBy = result.ApprovedBy
	}

	// Start (if needed) and signal the DeviceTagManager to add this grant's tags.
	// Uses SignalWithStartWorkflow to atomically create the workflow if it
	// doesn't exist yet, avoiding the race where SignalExternalWorkflow
	// fails because no DeviceTagManager is running for this device.
	var activities *Activities
	taskQueue := workflow.GetInfo(ctx).TaskQueueName
	if err := workflow.ExecuteActivity(actCtx, activities.SignalWithStartDeviceTagManager, request.TargetNodeID, taskQueue, AddGrantSignal{
		GrantID: request.ID,
		Tags:    grantType.Tags,
	}).Get(ctx, nil); err != nil {
		return state, fmt.Errorf("signal-with-start tag manager: %w", err)
	}
	tagMgrID := fmt.Sprintf("device-tags-%s", request.TargetNodeID)

	// Activate the grant
	now := workflow.Now(ctx)
	state.Status = StatusActive
	state.ActivatedAt = now
	state.ExpiresAt = now.Add(request.Duration)

	remaining := request.Duration

	for state.Status == StatusActive {
		timerCtx, timerCancel := workflow.WithCancel(ctx)
		timerFuture := workflow.NewTimer(timerCtx, remaining)

		revokeCh := workflow.GetSignalChannel(ctx, "revoke")
		extendCh := workflow.GetSignalChannel(ctx, "extend")

		sel := workflow.NewSelector(ctx)

		sel.AddFuture(timerFuture, func(f workflow.Future) {
			if err := f.Get(ctx, nil); err == nil {
				state.Status = StatusExpired
			}
		})

		sel.AddReceive(revokeCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig RevokeSignal
			ch.Receive(ctx, &sig)
			timerCancel()
			state.Status = StatusRevoked
			state.RevokedBy = sig.RevokedBy
			state.RevokedAt = workflow.Now(ctx)
			logger.Info("Grant revoked", "grantID", request.ID, "revokedBy", sig.RevokedBy)
		})

		sel.AddReceive(extendCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig ExtendSignal
			ch.Receive(ctx, &sig)
			timerCancel()

			remaining = sig.Duration
			state.ExpiresAt = workflow.Now(ctx).Add(sig.Duration)
			logger.Info("Grant extended", "grantID", request.ID, "newDuration", sig.Duration)
		})

		sel.Select(ctx)
	}

	// Signal the DeviceTagManager to remove this grant's tags.
	// The workflow is guaranteed to be running since we just added a grant to it.
	if err := workflow.SignalExternalWorkflow(ctx, tagMgrID, "", "remove-grant", RemoveGrantSignal{
		GrantID: request.ID,
	}).Get(ctx, nil); err != nil {
		logger.Error("Failed to signal tag manager remove", "grantID", request.ID, "error", err)
	}

	logger.Info("GrantWorkflow completed", "grantID", request.ID, "status", state.Status)
	return state, nil
}
