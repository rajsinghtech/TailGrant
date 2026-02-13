# TailGrant Design

## Context

TailGrant is a programmable tailnet control plane built on Temporal + Tailscale API + tsnet. The immediate use case is durable, event-driven JIT admin access — but the architecture supports broader workflow types: incident response automation, onboarding/offboarding, compliance enforcement, key lifecycle management, and self-service provisioning.

The Temporal cluster is self-hosted and already running inside the tailnet. TailGrant uses the Tailscale control plane API (via `tailscale-client-go-v2`) for enforcement and tsnet for serving its own UI and connecting workers to Temporal — keeping the entire system inside the tailnet with zero public exposure.

## Architecture: Split Frontend/Worker

### Component Topology

```
[ tailgrant-server ]                    [ tailgrant-worker ]
  tsnet node "tailgrant"                  tsnet node "tailgrant-worker"
  :443 TLS web UI                         (outbound only)
  |                                       |
  |-- HTTP handlers                       |-- Temporal Worker
  |-- WhoIs middleware                    |     |-- GrantWorkflow
  |-- Temporal Client ---[tailnet]------> |     |-- ApprovalWorkflow (child)
  |     (start/signal/query)              |     |-- DeviceTagManagerWorkflow
  |                                       |     |-- ReconciliationWorkflow
  |                                       |     |-- Activities
  |                                       |
  |                                       |-- tailscale.Client (OAuth)
  |                                       |     |-- SetTags, GetDevice, ListDevices
  +-----[both connect to]-------> [ Temporal Cluster (self-hosted) ]
```

- **Server** never calls the Tailscale API directly — all mutations flow through Temporal
- **Worker** holds the OAuth secret and executes all Tailscale API interactions as activities
- Both connect to Temporal via tailnet hostnames
- WhoIs provides zero-config authentication — connecting user's Tailscale identity is the auth

### Enforcement Model (v0)

**Tags as capabilities.** Static ACLs, dynamic identity.

1. ACLs pre-define rules like: `tag:jit-ssh-prod -> tag:prod-servers:22`
2. TailGrant assigns `tag:jit-ssh-prod` to a user's device via `SetTags`
3. TTL expires -> TailGrant removes the tag
4. No ACL mutation required

### Data Model

**Grant Types** — config-driven (YAML), loaded at startup:

```go
type GrantType struct {
    Name        string
    Description string
    Tags        []string        // tags to assign: ["tag:jit-ssh-prod"]
    MaxDuration time.Duration   // max allowed TTL
    RiskLevel   RiskLevel       // low=auto-approve, medium/high=human approval
    Approvers   []string        // loginNames or groups who can approve
}
```

**Grant Requests** — workflow input, created by web UI:

```go
type GrantRequest struct {
    ID            string
    Requester     string        // from WhoIs
    RequesterNode string        // nodeID
    GrantTypeName string
    TargetNodeID  string        // device to receive tags
    Duration      time.Duration
    Reason        string
}
```

**No database.** Temporal workflow state = source of truth for grant instances.

### Workflow Design

#### GrantWorkflow (one per grant request)

```
Start(GrantRequest)
  -> EvaluatePolicy(grantType, requester)
  -> if high-risk: StartChild(ApprovalWorkflow) -> wait for result
  -> if approved/auto-approved:
      -> Signal DeviceTagManager: AddGrant{grantID, tags}
      -> Durable timer(duration)
      -> Signal DeviceTagManager: RemoveGrant{grantID}
      -> Verify removal
  -> Complete
```

**Signals:** `approve`, `deny`, `revoke`, `extend`
**Queries:** `status` -> returns GrantState for UI polling

#### DeviceTagManagerWorkflow (one per device)

Serializes all tag operations for a device. Eliminates SetTags read-modify-write race.

```
Receives signals: AddGrant{grantID, tags}, RemoveGrant{grantID}
On each signal:
  -> Recompute union of all active grant tags + base tags
  -> Call SetTags activity with computed set
  -> Idle (ContinueAsNew on cadence)
```

#### ApprovalWorkflow (child of GrantWorkflow)

```
Start -> Notify approvers (activity)
      -> Wait for signal: approve/deny (with configurable timeout)
      -> Return ApprovalResult
```

Separating approval into a child workflow allows the mechanism to evolve (Slack, multi-approver, etc.) without changing the grant lifecycle.

#### ReconciliationWorkflow (singleton, ContinueAsNew loop)

```
Loop:
  -> ListDevices()
  -> For each active GrantWorkflow: Query(status)
  -> If expired grants still have tags: force-remove
  -> Sleep(5 minutes)
  -> ContinueAsNew
```

### API Auth

OAuth client (`tailscale-client-go-v2/oauth.go`):
- Scopes: `devices:core` (read/write devices, tags), `auth_keys` (future)
- 1-hour auto-refreshing tokens via `clientcredentials` flow
- Secret lives only in worker config

### tsnet Integration

**Server:** `tsnet.Server{Hostname: "tailgrant", AdvertiseTags: ["tag:tailgrant"]}` -> `ListenTLS(":443")` -> `LocalClient().WhoIs()` in middleware

**Worker:** `tsnet.Server{Hostname: "tailgrant-worker", Ephemeral: true, AdvertiseTags: ["tag:tailgrant-worker"]}` -> `Up(ctx)` -> connect to Temporal at `temporal.{tailnet}.ts.net:7233`

### Project Structure

```
tailgrant/
  cmd/
    tailgrant-server/main.go        # tsnet + HTTP + Temporal client
    tailgrant-worker/main.go        # tsnet + Temporal worker + TS API client
  internal/
    grant/
      types.go                      # GrantType, GrantRequest, GrantState, RiskLevel
      workflow.go                    # GrantWorkflow
      approval.go                   # ApprovalWorkflow (child)
      tagmanager.go                 # DeviceTagManagerWorkflow
      reconcile.go                  # ReconciliationWorkflow
      activities.go                 # Tailscale API activities
      policy.go                     # GrantTypeStore interface + YAML implementation
    server/
      router.go                     # HTTP router
      middleware.go                 # WhoIs auth middleware
      handlers.go                   # Grant CRUD, approval, device list
    tsapi/
      client.go                     # Tailscale API client factory (OAuth)
    config/
      config.go                     # Config struct, YAML parsing
  ui/
    embed.go                        # go:embed
    static/                         # HTML/CSS/JS (minimal v0)
  config.example.yaml
  go.mod
```

### Key Dependencies

- `go.temporal.io/sdk` — workflow/activity SDK, client SDK
- `github.com/tailscale/tailscale-client-go/v2` — Tailscale API v2 client
- `tailscale.com/tsnet` — embedded tailnet node
- `gopkg.in/yaml.v3` — config parsing

## v0 Scope

**In:**
- Split server/worker architecture
- Tag-based grants via DeviceTagManager
- YAML-driven grant type config
- tsnet web UI with WhoIs auth
- Tiered approval (auto-approve + signal-based human approval)
- ReconciliationWorkflow (5min sweep)
- Minimal HTML UI (request form, approval, status table)
- OAuth client for Tailscale API

**Out (v1+):**
- Posture attribute grants
- Slack/webhook notifications
- Multi-approver quorum
- Dynamic grant type management (API-driven)
- Onboarding/offboarding workflows
- Webhook-triggered workflows (Tailscale events)
- HA deployment
- Audit log export
- External IdP integration

## Verification

1. Start both binaries (server + worker) with `TS_AUTHKEY` set
2. Browse to `https://tailgrant.{tailnet}.ts.net`
3. Verify WhoIs identifies the connecting user
4. Request a low-risk grant -> verify auto-approve -> verify tags appear on target device
5. Request a high-risk grant -> verify it waits -> approve via UI -> verify tags appear
6. Wait for TTL -> verify tags are removed
7. Manually remove a tag mid-grant -> verify reconciliation loop detects and corrects
8. Kill the worker mid-grant -> restart -> verify Temporal replays and tags are still revoked on schedule
9. Request two concurrent grants on the same device -> verify DeviceTagManager serializes correctly
