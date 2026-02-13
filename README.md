# TailGrant

<p align="center">
  <strong>JIT Access Control for Tailscale Networks</strong>
</p>

Programmable just-in-time access control for [Tailscale](https://tailscale.com). TailGrant uses [Temporal](https://temporal.io) workflows to manage durable, time-limited grants — device tags, user role elevations, and user restores — keeping your ACLs static while dynamically controlling who gets access and for how long.

Everything runs inside your tailnet. No public endpoints, no external databases.

## How It Works

```
User requests access via web UI
        |
        v
  GrantWorkflow evaluates policy
        |
   low risk ──> auto-approve
   med/high ──> ApprovalWorkflow (waits for human signal)
        |
        v
  Grant activated (tags assigned / role elevated / user restored)
        |
        v
  Durable timer counts down TTL
        |
        v
  Grant automatically reversed on expiry
```

ACLs are pre-defined (e.g. `tag:jit-ssh-prod -> tag:prod-servers:22`). TailGrant assigns and removes tags — no ACL mutations needed. For user-based grants, roles are elevated and reverted, or suspended users are restored and re-suspended.

## Architecture

```
[ tailgrant-server ]                  [ tailgrant-worker ]
  tsnet node "tailgrant"                tsnet node "tailgrant-worker"
  :443 TLS web UI                       (outbound only)
  - HTTP handlers                       - Temporal Worker
  - WhoIs auth middleware               - Tailscale API client (OAuth)
  - Temporal Client                     - Workflow execution
           \                             /
            [--- Temporal Cluster ---]
```

- **Server** handles the web UI and starts workflows. Never touches the Tailscale API directly.
- **Worker** holds OAuth credentials and executes all Tailscale API calls as Temporal activities.
- **Temporal** is the only state store — no database required.
- **WhoIs** provides zero-config authentication via Tailscale identity.

## Grant Types

Defined in YAML config. Three action types are supported:

| Action | Target | Effect |
|--------|--------|--------|
| `tag` (default) | Device | Add/remove tags on a device |
| `user_role` | User | Elevate user role, revert on expiry |
| `user_restore` | User | Restore suspended user, re-suspend on expiry |

Risk levels control the approval flow:

| Risk Level | Behavior |
|------------|----------|
| `low` | Auto-approved immediately |
| `medium` | Requires human approval |
| `high` | Requires human approval |

Grants can also set [posture attributes](https://tailscale.com/kb/1288/device-posture) on devices for fine-grained ACL conditions.

## Install

### Prerequisites

- Go 1.25+
- A self-hosted [Temporal](https://temporal.io) cluster accessible within your tailnet
- A Tailscale OAuth client with `devices:core` and `users:core` scopes
- `TS_AUTHKEY` for initial tsnet node registration

### Build

```sh
make build    # produces tailgrant-server and tailgrant-worker binaries
make test     # run tests with race detector
make lint     # golangci-lint
```

### Configuration

```sh
cp config.example.yaml config.yaml
```

```yaml
temporal:
  address: "temporal.your-tailnet.ts.net:7233"
  namespace: "default"
  taskQueue: "tailgrant"

tailscale:
  hostname: "tailgrant"
  stateDir: "/var/lib/tailgrant/tsnet"
  tailnet: "your-tailnet.com"

grants:
  # Tag-based grant with posture attributes
  - name: "ssh-access"
    description: "Temporary SSH access to a target node"
    tags: ["tag:ssh-granted"]
    postureAttributes:
      - key: "custom:jit-ssh"
        value: "granted"
        target: "requester"    # "requester" (default) or "target"
    maxDuration: "4h"
    riskLevel: "low"
    approvers: []

  # Tag-based grant requiring approval
  - name: "admin-access"
    description: "Full administrative access"
    tags: ["tag:admin-granted"]
    maxDuration: "2h"
    riskLevel: "high"
    approvers: ["admin@example.com"]

  # User role elevation
  - name: "temp-admin"
    description: "Temporarily elevate user to admin role"
    action: "user_role"
    userAction:
      role: "admin"
    maxDuration: "2h"
    riskLevel: "high"
    approvers: ["admin@example.com"]

  # User restore
  - name: "temp-restore"
    description: "Temporarily restore a suspended user"
    action: "user_restore"
    maxDuration: "4h"
    riskLevel: "medium"
    approvers: ["secops@example.com"]
```

OAuth credentials are set via environment variables:

```sh
export TS_OAUTH_CLIENT_ID="..."
export TS_OAUTH_CLIENT_SECRET="..."
```

### Run

Start both binaries with access to the same config:

```sh
./tailgrant-server -config config.yaml
./tailgrant-worker -config config.yaml
```

The web UI is available at `https://tailgrant.<your-tailnet>.ts.net`.

### Docker

```sh
make docker-build
```

Produces a distroless image with both binaries. Multi-arch builds (amd64/arm64) are handled by CI.

### Kubernetes

Kustomize manifests are in `kustomization/`:

```sh
kubectl apply -k kustomization/
```

Includes deployments for server and worker, RBAC, PVCs for tsnet state, and SOPS-encrypted secrets.

## API

All endpoints require WhoIs authentication (automatic via tailnet).

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/grants` | Request a new grant |
| `GET` | `/api/grants` | List all grants |
| `GET` | `/api/grants/{id}` | Query grant status |
| `POST` | `/api/grants/{id}/approve` | Approve a pending grant |
| `POST` | `/api/grants/{id}/deny` | Deny a pending grant |
| `POST` | `/api/grants/{id}/revoke` | Revoke an active grant |
| `POST` | `/api/grants/{id}/extend` | Extend an active grant |
| `GET` | `/api/grant-types` | List available grant types |
| `GET` | `/api/devices` | List tailnet devices |
| `GET` | `/api/users` | List tailnet users |
| `GET` | `/api/whoami` | Current user identity |

## Workflows

| Workflow | Purpose |
|----------|---------|
| **GrantWorkflow** | Full grant lifecycle: policy evaluation, approval, activation (tags/role/restore), TTL, deactivation |
| **ApprovalWorkflow** | Child workflow that waits for approve/deny signals (24h timeout) |
| **DeviceTagManagerWorkflow** | Serializes all tag and posture attribute mutations per device, preventing race conditions |
| **ReconciliationWorkflow** | Singleton loop (every 5min) that detects and corrects tag/posture drift |

## Project Structure

```
cmd/
  tailgrant-server/       Server entry point
  tailgrant-worker/       Worker entry point
internal/
  grant/                  Workflows, activities, types, policy
  server/                 HTTP router, handlers, WhoIs middleware
  tsapi/                  Tailscale API helpers (user operations)
  config/                 YAML config loading
ui/
  static/                 Embedded web UI
kustomization/            Kubernetes manifests
```
