# Orchestrator

The orchestrator is the SaaS control plane for Cogitator. It manages the lifecycle of
tenant instances (provisioning, deprovisioning, health monitoring, rollouts) and exposes
an HTTP API for both tenant machines and operator tooling.

## Architecture

```
cmd/orchestrator/main.go    Entry point, loads config, starts server
internal/orchestrator/
  config.go                 Environment-based configuration
  server.go                 HTTP server, CORS middleware, router
  auth.go                   JWT auth, requireAuth, requireOperator, requireInternal
  handlers.go               All HTTP handlers (signup, login, tenants, fleet, rollouts, releases)
  database.go               SQLite schema, migrations, PromoteOperator
  tenant.go                 TenantProvisioner (Fly machines, volumes, DNS)
  rollout.go                RolloutManager (canary/fleet-wide, batches, health checks)
  billing.go                Stripe webhook handler (subscription lifecycle)
  waker.go                  Background goroutine that wakes sleeping tenants on schedule
  fly/client.go             Fly.io machine API client
  cloudflare/dns.go         Cloudflare DNS API client
```

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8485` | HTTP listen port |
| `ORCHESTRATOR_DB_PATH` | `/data/orchestrator.db` | SQLite database path |
| `FLY_API_TOKEN` | | Fly.io API token for machine management |
| `FLY_APP_NAME` | `cogitator-saas` | Fly app name for tenant machines |
| `CLOUDFLARE_API_TOKEN` | | Cloudflare API token for DNS records |
| `CLOUDFLARE_ZONE_ID` | | Cloudflare zone ID |
| `COGITATOR_INTERNAL_SECRET` | | Shared secret for tenant-to-orchestrator auth |
| `ORCHESTRATOR_JWT_SECRET` | (random) | JWT signing secret (random if unset, won't survive restarts) |
| `ORCHESTRATOR_OPERATOR_EMAIL` | | Email of account to promote to operator on startup |
| `STRIPE_SECRET_KEY` | | Stripe API key |
| `STRIPE_WEBHOOK_SECRET` | | Stripe webhook signing secret |

## Credentials

The orchestrator UI and operator API endpoints require an operator account. To create one:

1. **Sign up** via `POST /api/signup` with an email and password. This creates a regular
   (non-operator) account and returns a JWT.
2. **Promote to operator** by setting `ORCHESTRATOR_OPERATOR_EMAIL` to the account email
   and restarting the orchestrator. Promotion runs on startup and is idempotent.
3. **Log in** via `POST /api/login` (or the UI login screen) to get a JWT with operator
   privileges.

Non-operator accounts can only provision, delete, and check status of their own tenants.
Operator accounts can access fleet stats, tenant details, rollouts, releases, and all
other dashboard endpoints.

## API Endpoints

### Public

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/health` | Health check |
| POST | `/api/signup` | Create account, returns JWT |
| POST | `/api/login` | Authenticate, returns JWT |

### Authenticated (JWT)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/tenants` | Provision a new tenant |
| DELETE | `/api/tenants/{id}` | Delete a tenant (owner or operator) |
| GET | `/api/tenants/{id}/status` | Tenant status (owner or operator) |

### Operator (JWT + is_operator)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/tenants` | List all tenants with latest heartbeat |
| GET | `/api/tenants/{id}` | Tenant detail (infra, subscription, wake schedule) |
| GET | `/api/tenants/{id}/heartbeats` | Last 10 heartbeats for a tenant |
| GET | `/api/fleet/stats` | Aggregate fleet counts by status |
| GET | `/api/releases` | All releases |
| GET | `/api/rollouts` | All rollouts with progress |
| GET | `/api/rollouts/{id}` | Rollout detail with batches and per-tenant status |
| POST | `/api/rollouts/{id}/rollback` | Trigger rollback |

### Internal (X-Internal-Secret)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/internal/heartbeat` | Record tenant heartbeat |
| POST | `/api/internal/schedule-wake` | Schedule a machine wake |
| POST | `/api/internal/releases` | Register new release, trigger rollout |
| POST | `/api/internal/rollouts/{id}/rollback` | Trigger rollback |

### Webhook

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/billing/webhook` | Stripe webhook (subscription events) |

## Auth Model

Three auth layers:

1. **JWT (requireAuth)**: Validates Bearer token, injects `account_id` and `is_operator`
   into request context.
2. **Operator (requireOperator)**: Wraps requireAuth, rejects non-operator accounts with 403.
3. **Internal (requireInternal)**: Validates `X-Internal-Secret` header via constant-time
   comparison. Used for tenant-to-orchestrator communication.

Operator accounts bypass ownership checks in tenant handlers.

## Operator Promotion

Set `ORCHESTRATOR_OPERATOR_EMAIL` to promote an account on startup. The account must
already exist (created via signup). The promotion is idempotent.

## Running

```bash
go build -o orchestrator ./cmd/orchestrator
./orchestrator
```

Or with Docker:

```bash
docker build -f cmd/orchestrator/Dockerfile -t orchestrator .
docker run -p 8485:8485 orchestrator
```

## Fly.io Setup Guide

Step-by-step guide to deploy the orchestrator and prepare the tenant app on Fly.io.

### Prerequisites

- [flyctl](https://fly.io/docs/hands-on/install-flyctl/) installed and authenticated
- A Cloudflare account with a zone for `cogitator.cloud` (or your domain)
- A Stripe account with a webhook endpoint configured

### 1. Create the orchestrator app

```bash
fly apps create cogitator-orchestrator
```

### 2. Create a persistent volume for the SQLite database

```bash
fly volumes create orchestrator_data --size 1 --region cdg --app cogitator-orchestrator
```

One volume is sufficient. The orchestrator runs as a single instance.

### 3. Create the tenant app (shared Fly app for all tenant machines)

```bash
fly apps create cogitator-saas
```

Tenant machines are created programmatically via the Fly Machines API, not via
`fly deploy`. This app acts as the container for all tenant machines.

### 4. Build and push the tenant Docker image

```bash
fly deploy --app cogitator-saas --image-only --dockerfile Dockerfile --build-arg BUILD_TAGS=saas
```

Or build locally and push to the Fly registry:

```bash
docker build --build-arg BUILD_TAGS=saas -t registry.fly.io/cogitator-saas:latest .
fly auth docker
docker push registry.fly.io/cogitator-saas:latest
```

### 5. Generate secrets

```bash
# Internal secret shared between orchestrator and tenant machines
INTERNAL_SECRET=$(openssl rand -hex 32)

# JWT secret for orchestrator account tokens
JWT_SECRET=$(openssl rand -hex 32)
```

### 6. Set orchestrator secrets

```bash
fly secrets set \
  FLY_API_TOKEN="$(fly auth token)" \
  FLY_APP_NAME="cogitator-saas" \
  CLOUDFLARE_API_TOKEN="your-cloudflare-token" \
  CLOUDFLARE_ZONE_ID="your-zone-id" \
  COGITATOR_INTERNAL_SECRET="$INTERNAL_SECRET" \
  ORCHESTRATOR_JWT_SECRET="$JWT_SECRET" \
  ORCHESTRATOR_OPERATOR_EMAIL="you@example.com" \
  STRIPE_SECRET_KEY="sk_live_..." \
  STRIPE_WEBHOOK_SECRET="whsec_..." \
  --app cogitator-orchestrator
```

### 7. Deploy the orchestrator

```bash
fly deploy --config fly.orchestrator.toml
```

This builds the orchestrator Docker image and deploys it. The `fly.orchestrator.toml`
configures port 8485, the SQLite volume mount, and HTTPS.

### 8. Verify the deployment

```bash
curl https://cogitator-orchestrator.fly.dev/api/health
# {"status":"ok"}
```

### 9. Create your operator account

```bash
# Sign up (creates the account)
curl -X POST https://cogitator-orchestrator.fly.dev/api/signup \
  -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","password":"your-secure-password"}'

# The account is auto-promoted to operator on the next deploy (or restart)
# because ORCHESTRATOR_OPERATOR_EMAIL is set. To promote immediately:
fly machines restart --app cogitator-orchestrator
```

### 10. Connect the orchestrator UI

```bash
cd orchestrator-ui
npm run dev
```

Open `http://localhost:5174`, enter:
- URL: `https://cogitator-orchestrator.fly.dev`
- Email and password from step 9

### 11. Configure Stripe webhook

In the Stripe dashboard, create a webhook endpoint pointing to:

```
https://cogitator-orchestrator.fly.dev/api/billing/webhook
```

Subscribe to these events:
- `customer.subscription.created`
- `customer.subscription.updated`
- `customer.subscription.deleted`
- `invoice.payment_failed`

### 12. Configure Cloudflare DNS

Create a wildcard CNAME record (or let the orchestrator manage individual records):

```
*.cogitator.cloud -> cogitator-saas.fly.dev
```

The orchestrator creates per-tenant CNAME records (`{slug}.cogitator.cloud`) when
provisioning. The wildcard is optional but simplifies initial setup.

### 13. Verify tenant provisioning

From the orchestrator UI (Fleet page), or via API:

```bash
TOKEN="your-jwt-from-login"
curl -X POST https://cogitator-orchestrator.fly.dev/api/tenants \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"slug":"test-tenant","tier":"free","admin_email":"admin@test.com","admin_password":"test1234"}'
```

This creates a Fly machine, volume, and DNS record. Check the UI for status.

## TODO

- [ ] Pagination on tenant list endpoint (`?offset=&limit=` query parameters)
- [ ] Review the full signup-to-provisioning-to-billing flow
- [ ] Integration tests against a running orchestrator
- [ ] LLM routing configuration per tier (free/starter/pro model access)
- [ ] Usage metering (track LLM token consumption per tenant)
- [ ] Automated database backup cron for the orchestrator SQLite volume
- [ ] CI pipeline: GitHub Actions workflow to build tenant image and notify orchestrator
- [ ] Custom domain support for tenants (Fly.io certificate API)
- [ ] Rate limiting on operator endpoints
- [ ] Stable/early release channels (tenants opt into early updates)

## Testing

```bash
go test ./internal/orchestrator/ -v
```
