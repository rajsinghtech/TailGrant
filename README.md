# TailGrant

Programmable JIT access control for Tailscale networks. TailGrant uses [Temporal](https://temporal.io) workflows to manage durable, time-limited tag grants on devices — keeping your ACLs static while dynamically controlling who gets access and for how long.

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
  DeviceTagManager assigns tags to target device
        |
        v
  Durable timer counts down TTL
        |
        v
  Tags automatically removed on expiry
```

ACLs are pre-defined (e.g. `tag:jit-ssh-prod -> tag:prod-servers:22`). TailGrant assigns and removes tags — no ACL mutations needed.

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

Defined in YAML config with tiered risk levels:

| Risk Level | Behavior |
|------------|----------|
| `low` | Auto-approved immediately |
| `medium` | Requires human approval |
| `high` | Requires human approval |

Each grant type specifies tags, max duration, and approvers.

## Prerequisites

- Go 1.25+
- A self-hosted [Temporal](https://temporal.io) cluster accessible within your tailnet
- A Tailscale OAuth client with `devices:core` scope
- `TS_AUTHKEY` for initial tsnet node registration

## Configuration

Copy the example config and fill in your values:

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
  - name: "ssh-access"
    description: "Temporary SSH access to a target node"
    tags: ["tag:ssh-granted"]
    maxDuration: "4h"
    riskLevel: "low"
    approvers: []

  - name: "admin-access"
    description: "Full administrative access"
    tags: ["tag:admin-granted"]
    maxDuration: "2h"
    riskLevel: "high"
    approvers: ["admin@example.com"]
```

OAuth credentials are set via environment variables:

```sh
export TS_OAUTH_CLIENT_ID="..."
export TS_OAUTH_CLIENT_SECRET="..."
```

## Build

```sh
make build    # produces tailgrant-server and tailgrant-worker binaries
make test     # run tests with race detector
make lint     # golangci-lint
```

## Run

Start both binaries with access to the same config:

```sh
./tailgrant-server -config config.yaml
./tailgrant-worker -config config.yaml
```

The web UI is available at `https://tailgrant.<your-tailnet>.ts.net`.

## Docker

```sh
make docker-build
```

Produces a distroless image with both binaries. Multi-arch builds (amd64/arm64) are handled by CI.

## Kubernetes

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
| `GET` | `/api/grants/{id}` | Query grant status |
| `POST` | `/api/grants/{id}/approve` | Approve a pending grant |
| `POST` | `/api/grants/{id}/deny` | Deny a pending grant |
| `POST` | `/api/grants/{id}/revoke` | Revoke an active grant |
| `GET` | `/api/grant-types` | List available grant types |
| `GET` | `/api/devices` | List tailnet devices |
| `GET` | `/api/whoami` | Current user identity |

## Workflows

| Workflow | Purpose |
|----------|---------|
| **GrantWorkflow** | Full grant lifecycle: policy evaluation, approval, tag assignment, TTL, revocation |
| **ApprovalWorkflow** | Child workflow that waits for approve/deny signals (24h timeout) |
| **DeviceTagManagerWorkflow** | Serializes all tag mutations per device, preventing SetTags race conditions |
| **ReconciliationWorkflow** | Singleton loop (every 5min) that detects and corrects tag drift |

## Project Structure

```
cmd/
  tailgrant-server/       Server entry point
  tailgrant-worker/       Worker entry point
internal/
  grant/                  Workflows, activities, types, policy
  server/                 HTTP router, handlers, WhoIs middleware
  tsapi/                  Tailscale API client factory
  config/                 YAML config loading
ui/
  static/                 Embedded web UI
kustomization/            Kubernetes manifests
```

## License

See [LICENSE](LICENSE) for details.
