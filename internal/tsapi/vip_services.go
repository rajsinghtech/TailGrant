package tsapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	tailscale "tailscale.com/client/tailscale/v2"
)

// VIPService represents a Tailscale VIP Service.
type VIPService struct {
	Name        string            `json:"name,omitempty"`
	Addrs       []string          `json:"addrs,omitempty"`
	Comment     string            `json:"comment,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Ports       []string          `json:"ports,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
}

type vipServiceList struct {
	VIPServices []VIPService `json:"vipServices"`
}

// VIPServiceOperations provides HTTP wrappers for Tailscale VIP service endpoints.
type VIPServiceOperations struct {
	client *tailscale.Client
}

// NewVIPServiceOperations creates a VIPServiceOperations that reuses the
// authenticated HTTP transport from the given tailscale.Client.
func NewVIPServiceOperations(client *tailscale.Client) *VIPServiceOperations {
	_ = client.Users() // trigger init
	return &VIPServiceOperations{client: client}
}

// Get retrieves a VIP service by name.
// GET /api/v2/tailnet/{tailnet}/vip-services/{name}
func (v *VIPServiceOperations) Get(ctx context.Context, name string) (*VIPService, error) {
	path := fmt.Sprintf("/api/v2/tailnet/%s/vip-services/%s", v.client.Tailnet, name)
	var svc VIPService
	found, err := v.get(ctx, path, &svc)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &svc, nil
}

// List retrieves all VIP services for the tailnet.
// GET /api/v2/tailnet/{tailnet}/vip-services
func (v *VIPServiceOperations) List(ctx context.Context) ([]VIPService, error) {
	path := fmt.Sprintf("/api/v2/tailnet/%s/vip-services", v.client.Tailnet)
	var list vipServiceList
	found, err := v.get(ctx, path, &list)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return list.VIPServices, nil
}

// CreateOrUpdate creates or updates a VIP service. Fetch first to preserve
// auto-allocated addresses on update.
// PUT /api/v2/tailnet/{tailnet}/vip-services/{name}
func (v *VIPServiceOperations) CreateOrUpdate(ctx context.Context, svc VIPService) error {
	path := fmt.Sprintf("/api/v2/tailnet/%s/vip-services/%s", v.client.Tailnet, svc.Name)
	return v.put(ctx, path, svc)
}

// Delete removes a VIP service by name.
// DELETE /api/v2/tailnet/{tailnet}/vip-services/{name}
func (v *VIPServiceOperations) Delete(ctx context.Context, name string) error {
	path := fmt.Sprintf("/api/v2/tailnet/%s/vip-services/%s", v.client.Tailnet, name)
	return v.doRequest(ctx, http.MethodDelete, path, nil, nil)
}

func (v *VIPServiceOperations) get(ctx context.Context, path string, out any) (bool, error) {
	urlStr := v.client.BaseURL.String() + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return false, fmt.Errorf("create request: %w", err)
	}
	if v.client.UserAgent != "" {
		req.Header.Set("User-Agent", v.client.UserAgent)
	}

	resp, err := v.client.HTTP.Do(req)
	if err != nil {
		return false, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("tailscale API %s returned %d: %s", path, resp.StatusCode, string(body))
	}

	if err := json.Unmarshal(body, out); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}
	return true, nil
}

func (v *VIPServiceOperations) put(ctx context.Context, path string, body any) error {
	return v.doRequest(ctx, http.MethodPut, path, body, nil)
}

func (v *VIPServiceOperations) doRequest(ctx context.Context, method, path string, body any, out any) error {
	urlStr := v.client.BaseURL.String() + path

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, reqBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if v.client.UserAgent != "" {
		req.Header.Set("User-Agent", v.client.UserAgent)
	}

	resp, err := v.client.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tailscale API %s returned %d: %s", path, resp.StatusCode, string(respBody))
	}

	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
