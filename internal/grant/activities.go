package grant

import (
	"context"
	"errors"
	"fmt"

	"github.com/rajsinghtech/tailgrant/internal/tsapi"
	tailscale "tailscale.com/client/tailscale/v2"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
)

// Activities holds dependencies for Tailscale API activity implementations.
type Activities struct {
	TS       *tailscale.Client
	Temporal client.Client
	UserOps  *tsapi.UserOperations
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
		var notFound *serviceerror.NotFound
		if errors.As(err, &notFound) {
			return false, nil
		}
		return false, fmt.Errorf("describe workflow %s: %w", workflowID, err)
	}
	status := desc.WorkflowExecutionInfo.Status
	return status == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, nil
}

// QueryActiveGrants queries a DeviceTagManager workflow for its active grants.
// Returns nil map if the workflow can't be queried.
func (a *Activities) QueryActiveGrants(ctx context.Context, workflowID string) (map[string]GrantAssets, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("QueryActiveGrants", "workflowID", workflowID)

	resp, err := a.Temporal.QueryWorkflow(ctx, workflowID, "", "active-grants")
	if err != nil {
		return nil, fmt.Errorf("query active grants %s: %w", workflowID, err)
	}

	var activeGrants map[string]GrantAssets
	if err := resp.Get(&activeGrants); err != nil {
		return nil, fmt.Errorf("decode active grants %s: %w", workflowID, err)
	}
	return activeGrants, nil
}

// SetPostureAttribute sets a posture attribute on a device.
func (a *Activities) SetPostureAttribute(ctx context.Context, deviceID string, key string, value any) error {
	logger := activity.GetLogger(ctx)
	logger.Info("SetPostureAttribute", "deviceID", deviceID, "key", key, "value", value)

	if err := a.TS.Devices().SetPostureAttribute(ctx, deviceID, key, tailscale.DevicePostureAttributeRequest{
		Value: value,
	}); err != nil {
		return fmt.Errorf("set posture attribute %s on %s: %w", key, deviceID, err)
	}
	return nil
}

// DeletePostureAttribute deletes a posture attribute from a device.
func (a *Activities) DeletePostureAttribute(ctx context.Context, deviceID string, key string) error {
	logger := activity.GetLogger(ctx)
	logger.Info("DeletePostureAttribute", "deviceID", deviceID, "key", key)

	if err := a.TS.Devices().DeletePostureAttribute(ctx, deviceID, key); err != nil {
		return fmt.Errorf("delete posture attribute %s from %s: %w", key, deviceID, err)
	}
	return nil
}

// GetPostureAttributes fetches all posture attributes for a device.
func (a *Activities) GetPostureAttributes(ctx context.Context, deviceID string) (map[string]any, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("GetPostureAttributes", "deviceID", deviceID)

	attrs, err := a.TS.Devices().GetPostureAttributes(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("get posture attributes %s: %w", deviceID, err)
	}
	return attrs.Attributes, nil
}

// GetUser fetches a user by ID from the Tailscale API and returns a
// UserInfo projection. Mapping here avoids coupling the workflow's
// serialized state to the upstream tailscale.User struct.
func (a *Activities) GetUser(ctx context.Context, userID string) (*UserInfo, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("GetUser", "userID", userID)

	user, err := a.TS.Users().Get(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user %s: %w", userID, err)
	}
	return &UserInfo{
		ID:     user.ID,
		Role:   string(user.Role),
		Status: string(user.Status),
	}, nil
}

// SetUserRole updates a user's role via the Tailscale API.
func (a *Activities) SetUserRole(ctx context.Context, userID string, role string) error {
	if a.UserOps == nil {
		return fmt.Errorf("user operations not configured")
	}
	logger := activity.GetLogger(ctx)
	logger.Info("SetUserRole", "userID", userID, "role", role)

	if err := a.UserOps.SetUserRole(ctx, userID, role); err != nil {
		return fmt.Errorf("set user role %s to %s: %w", userID, role, err)
	}
	return nil
}

// SuspendUser suspends a user via the Tailscale API.
func (a *Activities) SuspendUser(ctx context.Context, userID string) error {
	if a.UserOps == nil {
		return fmt.Errorf("user operations not configured")
	}
	logger := activity.GetLogger(ctx)
	logger.Info("SuspendUser", "userID", userID)

	if err := a.UserOps.SuspendUser(ctx, userID); err != nil {
		return fmt.Errorf("suspend user %s: %w", userID, err)
	}
	return nil
}

// RestoreUser restores a suspended user via the Tailscale API.
func (a *Activities) RestoreUser(ctx context.Context, userID string) error {
	if a.UserOps == nil {
		return fmt.Errorf("user operations not configured")
	}
	logger := activity.GetLogger(ctx)
	logger.Info("RestoreUser", "userID", userID)

	if err := a.UserOps.RestoreUser(ctx, userID); err != nil {
		return fmt.Errorf("restore user %s: %w", userID, err)
	}
	return nil
}
