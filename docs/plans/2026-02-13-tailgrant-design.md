# TailGrant Design

Programmable tailnet control plane built on Temporal + Tailscale API + tsnet.
Immediate use case: durable, event-driven JIT admin access with tag-based enforcement.

## Architecture: Split Frontend/Worker

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
  |                                       |     SetTags, GetDevice, ListDevices
  +-----[both connect to]-------> [ Temporal Cluster (self-hosted) ]
```

- Server never calls Tailscale API directly -- all mutations flow through Temporal
- Worker holds the OAuth secret and executes all Tailscale API interactions as activities
- Both connect to Temporal via tailnet hostnames (temporal.{tailnet}.ts.net:7233)
- WhoIs provides zero-config authentication

## Enforcement Model

Tags as capabilities. Static ACLs, dynamic identity.

1. ACLs pre-define rules like: `tag:jit-ssh-prod` -> `tag:prod-servers:22`
2. TailGrant assigns `tag:jit-ssh-prod` to a user's device via SetTags
3. TTL expires -> TailGrant removes the tag
4. No ACL mutation required

## Data Model

### Grant Types (config-driven YAML)

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

### Grant Requests (workflow input)

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

No database. Temporal workflow state = source of truth.

## Workflows

### GrantWorkflow (one per grant request)

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

Signals: approve, deny, revoke, extend
Queries: status -> returns GrantState for UI polling

### DeviceTagManagerWorkflow (one per device)

Serializes all tag operations for a device. Eliminates SetTags read-modify-write race.

Receives signals: AddGrant{grantID, tags}, RemoveGrant{grantID}
On each signal: recompute union of all active grant tags + base tags -> SetTags activity.
Idles with ContinueAsNew on cadence.

### ApprovalWorkflow (child of GrantWorkflow)

```
Start -> Notify approvers (activity)
       -> Wait for signal: approve/deny (with configurable timeout)
       -> Return ApprovalResult
```

### ReconciliationWorkflow (singleton, ContinueAsNew loop)

```
Loop:
  -> ListDevices()
  -> For each active GrantWorkflow: Query(status)
  -> If expired grants still have tags: force-remove
  -> Sleep(5 minutes)
  -> ContinueAsNew
```

## API Auth

OAuth client (tailscale-client-go-v2):
- Scopes: devices:core (read/write devices, tags)
- 1-hour auto-refreshing tokens via clientcredentials flow
- Credentials only in worker config

## tsnet Integration

- Server: `tsnet.Server{Hostname: "tailgrant"}` -> ListenTLS(":443") -> LocalClient().WhoIs()
- Worker: `tsnet.Server{Hostname: "tailgrant-worker", Ephemeral: true}` -> Up(ctx) -> connect to Temporal

## Project Structure

```
tailgrant/
  cmd/
    tailgrant-server/main.go
    tailgrant-worker/main.go
  internal/
    grant/
      types.go          # GrantType, GrantRequest, GrantState, RiskLevel
      workflow.go        # GrantWorkflow
      approval.go        # ApprovalWorkflow (child)
      tagmanager.go      # DeviceTagManagerWorkflow
      reconcile.go       # ReconciliationWorkflow
      activities.go      # Tailscale API activities
      policy.go          # GrantTypeStore interface + YAML implementation
    server/
      router.go          # HTTP router
      middleware.go       # WhoIs auth middleware
      handlers.go        # Grant CRUD, approval, device list
    tsapi/
      client.go          # Tailscale API client factory (OAuth)
    config/
      config.go          # Config struct, YAML parsing
  ui/
    embed.go             # go:embed
    static/              # HTML/CSS/JS (minimal v0)
  config.example.yaml
  go.mod
```

## Key Dependencies

- go.temporal.io/sdk (workflow/activity SDK, client SDK)
- github.com/tailscale/tailscale-client-go/v2 (local: ../tailscale-client-go-v2)
- tailscale.com/tsnet (local: ../tailscale)
- gopkg.in/yaml.v3

## Target

- Go 1.23+
- Temporal cluster at temporal.{tailnet}.ts.net:7233
- Full v0 scope (14 implementation steps)
