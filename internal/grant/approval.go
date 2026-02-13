package grant

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"
)

const approvalTimeout = 24 * time.Hour

func ApprovalWorkflow(ctx workflow.Context, grantID string, grantType GrantType) (ApprovalResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("ApprovalWorkflow started", "grantID", grantID)

	approvers := make(map[string]struct{}, len(grantType.Approvers))
	for _, a := range grantType.Approvers {
		approvers[a] = struct{}{}
	}

	approveCh := workflow.GetSignalChannel(ctx, "approve")
	denyCh := workflow.GetSignalChannel(ctx, "deny")

	timerCtx, timerCancel := workflow.WithCancel(ctx)
	timerFuture := workflow.NewTimer(timerCtx, approvalTimeout)

	var result ApprovalResult

	sel := workflow.NewSelector(ctx)

	sel.AddReceive(approveCh, func(ch workflow.ReceiveChannel, more bool) {
		var sig ApproveSignal
		ch.Receive(ctx, &sig)
		timerCancel()

		if _, ok := approvers[sig.ApprovedBy]; !ok && len(approvers) > 0 {
			result = ApprovalResult{
				Approved: false,
				Reason:   fmt.Sprintf("%s is not an authorized approver", sig.ApprovedBy),
			}
			logger.Warn("Unauthorized approval attempt", "grantID", grantID, "attemptedBy", sig.ApprovedBy)
			return
		}

		result = ApprovalResult{
			Approved:   true,
			ApprovedBy: sig.ApprovedBy,
		}
		logger.Info("Grant approved", "grantID", grantID, "approvedBy", sig.ApprovedBy)
	})

	sel.AddReceive(denyCh, func(ch workflow.ReceiveChannel, more bool) {
		var sig DenySignal
		ch.Receive(ctx, &sig)
		timerCancel()
		result = ApprovalResult{
			Approved: false,
			DeniedBy: sig.DeniedBy,
			Reason:   sig.Reason,
		}
		logger.Info("Grant denied", "grantID", grantID, "deniedBy", sig.DeniedBy)
	})

	sel.AddFuture(timerFuture, func(f workflow.Future) {
		if err := f.Get(ctx, nil); err == nil {
			result = ApprovalResult{
				Approved: false,
				Reason:   "approval timed out",
			}
			logger.Info("Approval timed out", "grantID", grantID)
		}
	})

	sel.Select(ctx)

	return result, nil
}
