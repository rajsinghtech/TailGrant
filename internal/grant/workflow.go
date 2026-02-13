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
		if err := workflow.ExecuteChildWorkflow(childCtx, ApprovalWorkflow, request.ID, grantType, request.Requester).Get(ctx, &result); err != nil {
			return state, fmt.Errorf("approval workflow: %w", err)
		}
		if !result.Approved {
			state.Status = StatusDenied
			logger.Info("Grant denied", "grantID", request.ID, "deniedBy", result.DeniedBy, "reason", result.Reason)
			return state, nil
		}
		state.ApprovedBy = result.ApprovedBy
	}

	var activities *Activities
	taskQueue := workflow.GetInfo(ctx).TaskQueueName
	action := grantType.Action
	if action == "" {
		action = ActionTag
	}

	// Activate phase: apply the grant's effect based on action type.
	var tagMgrID string
	switch action {
	case ActionTag:
		if err := workflow.ExecuteActivity(actCtx, activities.SignalWithStartDeviceTagManager, request.TargetNodeID, taskQueue, AddGrantSignal{
			GrantID:           request.ID,
			Tags:              grantType.Tags,
			PostureAttributes: grantType.PostureAttributes,
			RequesterNodeID:   request.RequesterNode,
		}).Get(ctx, nil); err != nil {
			return state, fmt.Errorf("signal-with-start tag manager: %w", err)
		}
		tagMgrID = fmt.Sprintf("device-tags-%s", request.TargetNodeID)

	case ActionUserRole:
		if grantType.UserAction == nil {
			return state, fmt.Errorf("user_role grant type %q missing userAction config", grantType.Name)
		}
		var user UserInfo
		if err := workflow.ExecuteActivity(actCtx, activities.GetUser, request.TargetUserID).Get(ctx, &user); err != nil {
			return state, fmt.Errorf("get user for role elevation: %w", err)
		}
		state.OriginalRole = user.Role
		targetRole := grantType.UserAction.Role
		if err := workflow.ExecuteActivity(actCtx, activities.SetUserRole, request.TargetUserID, targetRole).Get(ctx, nil); err != nil {
			return state, fmt.Errorf("set user role to %s: %w", targetRole, err)
		}
		logger.Info("User role elevated", "userID", request.TargetUserID, "from", user.Role, "to", targetRole)

	case ActionUserRestore:
		if err := workflow.ExecuteActivity(actCtx, activities.RestoreUser, request.TargetUserID).Get(ctx, nil); err != nil {
			return state, fmt.Errorf("restore user: %w", err)
		}
		logger.Info("User restored", "userID", request.TargetUserID)
	}

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

			maxDur := time.Duration(grantType.MaxDuration)
			if maxDur > 0 && sig.Duration > maxDur {
				sig.Duration = maxDur
				logger.Info("Extend duration clamped to max", "grantID", request.ID, "maxDuration", maxDur)
			}

			remaining = sig.Duration
			state.ExpiresAt = workflow.Now(ctx).Add(sig.Duration)
			logger.Info("Grant extended", "grantID", request.ID, "newDuration", sig.Duration)
		})

		sel.Select(ctx)
	}

	// Deactivate phase: revert the grant's effect based on action type.
	switch action {
	case ActionTag:
		if err := workflow.SignalExternalWorkflow(ctx, tagMgrID, "", "remove-grant", RemoveGrantSignal{
			GrantID: request.ID,
		}).Get(ctx, nil); err != nil {
			logger.Error("Failed to signal tag manager remove", "grantID", request.ID, "error", err)
		}

	case ActionUserRole:
		if state.OriginalRole == "" {
			logger.Error("Cannot revert user role: originalRole is empty, skipping", "userID", request.TargetUserID)
		} else if err := workflow.ExecuteActivity(actCtx, activities.SetUserRole, request.TargetUserID, state.OriginalRole).Get(ctx, nil); err != nil {
			logger.Error("Failed to revert user role", "userID", request.TargetUserID, "role", state.OriginalRole, "error", err)
		} else {
			logger.Info("User role reverted", "userID", request.TargetUserID, "to", state.OriginalRole)
		}

	case ActionUserRestore:
		if err := workflow.ExecuteActivity(actCtx, activities.SuspendUser, request.TargetUserID).Get(ctx, nil); err != nil {
			logger.Error("Failed to re-suspend user", "userID", request.TargetUserID, "error", err)
		} else {
			logger.Info("User re-suspended", "userID", request.TargetUserID)
		}
	}

	logger.Info("GrantWorkflow completed", "grantID", request.ID, "status", state.Status)
	return state, nil
}
