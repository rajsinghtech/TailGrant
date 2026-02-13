package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rajsinghtech/tailgrant/internal/grant"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	tailscale "tailscale.com/client/tailscale/v2"
)

type Handlers struct {
	TemporalClient client.Client
	TSClient       *tailscale.Client
	GrantTypes     grant.GrantTypeStore
	TaskQueue      string
}

type createGrantRequest struct {
	GrantTypeName string `json:"grantTypeName"`
	TargetNodeID  string `json:"targetNodeID"`
	TargetUserID  string `json:"targetUserID"`
	Duration      string `json:"duration"`
	Reason        string `json:"reason"`
}

func (h *Handlers) HandleCreateGrant(w http.ResponseWriter, r *http.Request) {
	who := WhoIsFromContext(r.Context())
	if who == nil {
		writeError(w, http.StatusUnauthorized, "missing identity")
		return
	}

	var req createGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	gt, err := h.GrantTypes.Get(req.GrantTypeName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	dur, err := time.ParseDuration(req.Duration)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid duration: "+err.Error())
		return
	}
	if dur <= 0 {
		writeError(w, http.StatusBadRequest, "duration must be positive")
		return
	}
	if dur > time.Duration(gt.MaxDuration) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("duration %s exceeds max %s for grant type %q", dur, time.Duration(gt.MaxDuration), gt.Name))
		return
	}

	action := gt.Action
	if action == "" {
		action = grant.ActionTag
	}

	switch action {
	case grant.ActionTag:
		if req.TargetNodeID == "" {
			writeError(w, http.StatusBadRequest, "targetNodeID is required for tag grants")
			return
		}
		if h.TSClient != nil {
			if _, err := h.TSClient.Devices().Get(r.Context(), req.TargetNodeID); err != nil {
				writeError(w, http.StatusBadRequest, "target device not found: "+err.Error())
				return
			}
		}
	case grant.ActionUserRole, grant.ActionUserRestore:
		if req.TargetUserID == "" {
			writeError(w, http.StatusBadRequest, "targetUserID is required for user grants")
			return
		}
		if h.TSClient != nil {
			if _, err := h.TSClient.Users().Get(r.Context(), req.TargetUserID); err != nil {
				writeError(w, http.StatusBadRequest, "target user not found: "+err.Error())
				return
			}
		}
	}

	id := uuid.New().String()
	grantReq := grant.GrantRequest{
		ID:            id,
		Requester:     who.UserProfile.LoginName,
		RequesterNode: string(who.Node.StableID),
		GrantTypeName: req.GrantTypeName,
		TargetNodeID:  req.TargetNodeID,
		TargetUserID:  req.TargetUserID,
		Duration:      dur,
		Reason:        req.Reason,
		RequestedAt:   time.Now(),
	}

	workflowID := fmt.Sprintf("grant-%s", id)
	opts := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: h.TaskQueue,
	}

	_, err = h.TemporalClient.ExecuteWorkflow(r.Context(), opts, grant.GrantWorkflow, grantReq, *gt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start workflow: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":         id,
		"workflowID": workflowID,
		"status":     "started",
	})
}

func (h *Handlers) HandleApproveGrant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	who := WhoIsFromContext(r.Context())
	if who == nil {
		writeError(w, http.StatusUnauthorized, "missing identity")
		return
	}

	// Query the grant to check for self-approval before signaling.
	resp, err := h.TemporalClient.QueryWorkflow(r.Context(), fmt.Sprintf("grant-%s", id), "", "status")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query grant: "+err.Error())
		return
	}
	var state grant.GrantState
	if err := resp.Get(&state); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode grant state: "+err.Error())
		return
	}
	if state.Status != grant.StatusPendingApproval {
		writeError(w, http.StatusConflict, fmt.Sprintf("grant is %s, not pending approval", state.Status))
		return
	}
	if state.Request.Requester == who.UserProfile.LoginName {
		writeError(w, http.StatusForbidden, "cannot approve your own grant request")
		return
	}

	err = h.TemporalClient.SignalWorkflow(r.Context(), fmt.Sprintf("approval-%s", id), "", "approve", grant.ApproveSignal{
		ApprovedBy: who.UserProfile.LoginName,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to signal approval: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"id":     id,
		"status": "approved",
	})
}

func (h *Handlers) HandleDenyGrant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	who := WhoIsFromContext(r.Context())
	if who == nil {
		writeError(w, http.StatusUnauthorized, "missing identity")
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	err := h.TemporalClient.SignalWorkflow(r.Context(), fmt.Sprintf("approval-%s", id), "", "deny", grant.DenySignal{
		DeniedBy: who.UserProfile.LoginName,
		Reason:   body.Reason,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to signal denial: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"id":     id,
		"status": "denied",
	})
}

func (h *Handlers) HandleRevokeGrant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	who := WhoIsFromContext(r.Context())
	if who == nil {
		writeError(w, http.StatusUnauthorized, "missing identity")
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	err := h.TemporalClient.SignalWorkflow(r.Context(), fmt.Sprintf("grant-%s", id), "", "revoke", grant.RevokeSignal{
		RevokedBy: who.UserProfile.LoginName,
		Reason:    body.Reason,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to signal revocation: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"id":     id,
		"status": "revoked",
	})
}

func (h *Handlers) HandleGetGrant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	resp, err := h.TemporalClient.QueryWorkflow(r.Context(), fmt.Sprintf("grant-%s", id), "", "status")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query workflow: "+err.Error())
		return
	}

	var state grant.GrantState
	if err := resp.Get(&state); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode workflow state: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, state)
}

func (h *Handlers) HandleListGrantTypes(w http.ResponseWriter, r *http.Request) {
	types, err := h.GrantTypes.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, types)
}

func (h *Handlers) HandleListDevices(w http.ResponseWriter, r *http.Request) {
	if h.TSClient == nil {
		writeError(w, http.StatusInternalServerError, "tailscale API client not configured")
		return
	}
	devices, err := h.TSClient.Devices().List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list devices: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, devices)
}

func (h *Handlers) HandleListUsers(w http.ResponseWriter, r *http.Request) {
	if h.TSClient == nil {
		writeError(w, http.StatusInternalServerError, "tailscale API client not configured")
		return
	}
	users, err := h.TSClient.Users().List(r.Context(), nil, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handlers) HandleWhoAmI(w http.ResponseWriter, r *http.Request) {
	who := WhoIsFromContext(r.Context())
	if who == nil {
		writeError(w, http.StatusUnauthorized, "missing identity")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"login":  who.UserProfile.LoginName,
		"name":   who.UserProfile.DisplayName,
		"nodeID": string(who.Node.StableID),
	})
}

func (h *Handlers) HandleListGrants(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	resp, err := h.TemporalClient.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
		Namespace: "default",
		Query:     "WorkflowType = 'GrantWorkflow'",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflows: "+err.Error())
		return
	}

	var grants []grant.GrantState
	for _, exec := range resp.Executions {
		wfID := exec.Execution.WorkflowId
		status := exec.Status
		if status != enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING &&
			status != enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED {
			continue
		}

		qResp, err := h.TemporalClient.QueryWorkflow(ctx, wfID, "", "status")
		if err != nil {
			continue
		}

		var state grant.GrantState
		if err := qResp.Get(&state); err != nil {
			continue
		}
		grants = append(grants, state)
	}

	if grants == nil {
		grants = []grant.GrantState{}
	}
	writeJSON(w, http.StatusOK, grants)
}

func (h *Handlers) HandleExtendGrant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	who := WhoIsFromContext(r.Context())
	if who == nil {
		writeError(w, http.StatusUnauthorized, "missing identity")
		return
	}

	var body struct {
		Duration string `json:"duration"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	dur, err := time.ParseDuration(body.Duration)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid duration: "+err.Error())
		return
	}
	if dur <= 0 {
		writeError(w, http.StatusBadRequest, "duration must be positive")
		return
	}

	err = h.TemporalClient.SignalWorkflow(r.Context(), fmt.Sprintf("grant-%s", id), "", "extend", grant.ExtendSignal{
		ExtendedBy: who.UserProfile.LoginName,
		Duration:   dur,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to signal extend: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"id":     id,
		"status": "extended",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
