package grant

import (
	"context"
	"fmt"

	tailscale "tailscale.com/client/tailscale/v2"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
)

// Activities holds dependencies for Tailscale API activity implementations.
type Activities struct {
	TS       *tailscale.Client
	Temporal client.Client
}

// GetDevice fetches a device by ID.
func (a *Activities) GetDevice(ctx context.Context, deviceID string) (*tailscale.Device, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("GetDevice", "deviceID", deviceID)

	device, err := a.TS.Devices().Get(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("get device %s: %w", deviceID, err)
	}
	return device, nil
}

// ListDevices lists all devices in the tailnet.
func (a *Activities) ListDevices(ctx context.Context) ([]tailscale.Device, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("ListDevices")

	devices, err := a.TS.Devices().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	return devices, nil
}

// GetDeviceTags fetches the current tags for a device.
func (a *Activities) GetDeviceTags(ctx context.Context, deviceID string) ([]string, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("GetDeviceTags", "deviceID", deviceID)

	device, err := a.TS.Devices().Get(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("get device tags %s: %w", deviceID, err)
	}
	return device.Tags, nil
}

// SetDeviceTags sets the full tag list on a device.
func (a *Activities) SetDeviceTags(ctx context.Context, deviceID string, tags []string) error {
	logger := activity.GetLogger(ctx)
	logger.Info("SetDeviceTags", "deviceID", deviceID, "tags", tags)

	if err := a.TS.Devices().SetTags(ctx, deviceID, tags); err != nil {
		return fmt.Errorf("set device tags %s: %w", deviceID, err)
	}
	return nil
}

// SignalWithStartDeviceTagManager atomically starts the DeviceTagManager workflow
// (if not already running) and sends it an add-grant signal. This avoids the race
// where SignalExternalWorkflow fails because no workflow exists yet.
func (a *Activities) SignalWithStartDeviceTagManager(ctx context.Context, nodeID string, taskQueue string, sig AddGrantSignal) error {
	logger := activity.GetLogger(ctx)
	logger.Info("SignalWithStartDeviceTagManager", "nodeID", nodeID, "grantID", sig.GrantID)

	workflowID := fmt.Sprintf("device-tags-%s", nodeID)
	_, err := a.Temporal.SignalWithStartWorkflow(
		ctx,
		workflowID,
		"add-grant",
		sig,
		client.StartWorkflowOptions{
			ID:        workflowID,
			TaskQueue: taskQueue,
		},
		DeviceTagManagerWorkflow,
		DeviceTagManagerState{NodeID: nodeID},
	)
	if err != nil {
		return fmt.Errorf("signal-with-start device tag manager %s: %w", nodeID, err)
	}
	return nil
}

// CheckWorkflowExists returns true if a workflow with the given ID is currently running.
func (a *Activities) CheckWorkflowExists(ctx context.Context, workflowID string) (bool, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("CheckWorkflowExists", "workflowID", workflowID)

	desc, err := a.Temporal.DescribeWorkflowExecution(ctx, workflowID, "")
	if err != nil {
		return false, nil
	}
	status := desc.WorkflowExecutionInfo.Status
	return status == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, nil
}

// QueryActiveGrants queries a DeviceTagManager workflow for its active grants.
// Returns nil map if the workflow can't be queried.
func (a *Activities) QueryActiveGrants(ctx context.Context, workflowID string) (map[string][]string, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("QueryActiveGrants", "workflowID", workflowID)

	resp, err := a.Temporal.QueryWorkflow(ctx, workflowID, "", "active-grants")
	if err != nil {
		return nil, fmt.Errorf("query active grants %s: %w", workflowID, err)
	}

	var activeGrants map[string][]string
	if err := resp.Get(&activeGrants); err != nil {
		return nil, fmt.Errorf("decode active grants %s: %w", workflowID, err)
	}
	return activeGrants, nil
}
