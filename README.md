<img width="3540" height="1690" alt="微信图片_20260815103656_45_5" src="https://github.com/user-attachments/assets/b276fb05-9630-4e84-b896-a1f54100745a" /># LinLinQi-FAKA

> ⚠️ **!!! THE PROJECT IS IN THE DEVELOPMENT STAGE — updates and fixes will be made as time permits !!!**

> **LinLinQi-FAKA** is an open-source reference implementation of an enterprise-grade digital goods auto-delivery platform, composed of a Go API, a Vue customer storefront, a Vue operations console, PostgreSQL, and Redis.

---

## ⚠️ Legal Disclaimer

> Please read and agree to the following terms carefully before using, downloading, cloning, or deploying this project. By continuing to use it, you acknowledge that you have read and agree to this disclaimer.

1. **For development / learning / research only**: This project is provided solely for programming education, technical research, and development testing. **Commercial operation is strictly prohibited.**
2. **No selling**: You **must not use this project** (including its source code, built artifacts, or derivatives) **to sell any goods, services, or virtual products** to anyone.
3. **Open source**: This project is released as open source. Everyone may learn, research, modify, and redistribute it in compliance with the license terms.
4. **Use at your own risk**: All consequences arising from the use of this project (including but not limited to operation, transactions, legal, and security risks) **are the sole responsibility of the user and have nothing to do with the author**. The author is not liable for any direct, indirect, or consequential damages.
5. This project ships with no real payment providers, supplier credentials, or production keys; integrating external systems for production requires your own compliance and security review.

---
<img width="3540" height="1690" alt="微信图片_20260815103656_45_5" src="https://github.com/user-attachments/assets/b276fb05-9630-4e84-b896-a1f54100745a" />
<img width="3538" height="1694" alt="微信图片_20260815103656_44_5" src="https://github.com/user-attachments/assets/1513fd70-d59a-4abb-b48c-e138aa762880" />
<img width="3542" height="1686" alt="微信图片_20260815103655_43_5" src="https://github.com/user-attachments/assets/cbb359f6-b2fc-48d3-9915-91a34b4794ee" />
<img width="3538" height="1682" alt="微信图片_20260815103655_42_5" src="https://github.com/user-attachments/assets/0503eb5e-00a7-48c4-8f0c-024defac63e4" />
<img width="3538" height="1686" alt="微信图片_20260815103655_41_5" src="https://github.com/user-attachments/assets/fcfd6911-5dd3-4548-a255-c5f8964f4f3e" />
<img width="1769" height="842" alt="image" src="https://github.com/user-attachments/assets/768b0b88-c11e-41c4-a961-5613de7e4af9" />
<img width="1767" height="839" alt="image" src="https://github.com/user-attachments/assets/e557bd09-a2e2-4224-ad92-2099f9ecc80d" />



## Overview

**LinLinQi** is an **independently implemented** enterprise-grade digital goods auto-delivery platform covering retail, open supply, distribution, payment, inventory, delivery, finance, risk control, and an operations console. It uses Dujiaoka and Dujiao Next only as public requirement/research material and **does not copy** their code, templates, components, page structure, or visual style. LinLinQi has its own domain model, API protocol, transaction state machine, and neutral black-and-white visual system.

## Features

- Product categories, SKUs, tiered pricing, member discounts, promotions, coupons, and channel restriction models
- Guest cart, multi-item checkout, transactional stock reservation, payment-timeout release, and encrypted auto-delivery
- Payment intents, signed payment connectors, callback signature verification, amount reconciliation, event idempotency, refunds, and reconciliation models
- AES-256-GCM application-layer encryption: card keys, OpenAPI secrets, payment configs, supplier credentials, and TOTP secrets
- Customer sign-up/sign-in, 15-minute access tokens, rotating refresh tokens, device sessions, and logout revocation
- Separate admin JWT, database RBAC, TOTP two-factor authentication, recovery codes, and audit logs
- HMAC-SHA256 OpenAPI, time windows, random nonces, Redis replay protection, and external order idempotency
- Independent supplier clients, product mapping, scheduled stock/price sync, and procurement domain models
- ISO 4217 multi-currency, 27 configurable FX sources, real-time/manual/trusted-cache conversion tiers, immutable FX snapshots, and atomic store re-pricing
- Double-entry wallet ledgers, gift cards, referral commissions, withdrawals, resellers, custom domains, and site rules
- Tickets, articles, announcements, banners, media, notifications, webhooks, risk control, blocklists, and security events
- Asynq multi-priority queues: order expiry, notification relay, webhook, supply sync, and reconciliation aggregation
- Prometheus metrics, liveness/readiness probes, request IDs, request body limits, CORS, security headers, and graceful shutdown
- Transactional migration ledger, migration checksums, and PostgreSQL advisory locks to prevent concurrent multi-instance migrations

See the [feature matrix](docs/FEATURES.md) for the full module list, [architecture](docs/ARCHITECTURE.md) for runtime design, [currency & FX](docs/CURRENCY.md) for money/FX boundaries, [brand assets](docs/BRAND_ASSETS.md) for default visuals, and the [Lobster manual](docs/LOBSTER.md) for the controlled ops-agent boundary.

## Tech Baseline

| Component | Version / Policy |
| --- | --- |
| Go | 1.26.5 |
| PostgreSQL | 18.4 |
| Redis | 8.8.1 stable (8.10 is still RC; not for production) |
| Node.js | 24.18.1 LTS |
| Alpine Linux | 3.24.1 |
| nginx | 1.30.4 stable |
| npm | 12.0.2 |
| Vite | 8.2.1 |
| TypeScript | 5.9.3 |
| vue-tsc | 3.3.9 |
| vue-i18n | 11.4.8 |
| @lucide/vue | 1.30.0 |
| GitHub Actions | checkout/setup-go/setup-node v7 |

## Repository Layout

```text
api/      Go 1.26.5 + Gin + GORM — API, migrations and async workers
user/     Vue 3 + Vite 8 + TypeScript customer storefront
admin/    Vue 3 + Vite 8 + TypeScript operations console
docs/     Architecture, security, OpenAPI, payments, operations and feature matrix
scripts/  Backup / restore / ops / toolchain verification scripts
deploy/   Deployment assets
```

## Quick Start (Setup Tutorial)

### Prerequisites

- Docker + Docker Compose (recommended — one command boots everything), or
- Local toolchain: Go 1.26.5, Node.js 24.18.1 LTS (npm 12.0.2), PostgreSQL 18.4, Redis 8.8.1

### Option A: Docker Compose (recommended)

```bash
git clone https://github.com/19910205/LinLinQi-FAKA.git && cd LinLinQi-FAKA
cp .env.example .env
docker compose --env-file .env -f docker-compose.yml -f docker-compose.dev.yml up --build
```

Access after boot:

- Storefront: <http://localhost:8080>
- Admin console: <http://localhost:8082>
- API: <http://localhost:8081>
- Readiness: <http://localhost:8081/ready>
- Prometheus: `GET http://localhost:8081/metrics` with header `Authorization: Bearer $METRICS_TOKEN`

> **Ports are configured in one place**: the `APP_PORT` (API), `API_PUBLISHED_PORT` (API published), `USER_PUBLISHED_PORT` (storefront) and `ADMIN_PUBLISHED_PORT` (admin console) values in `.env` (see `.env.example` for the annotated block). To change ports, edit only those values and restart the services — the API, Vite dev servers, Compose, nginx and frontend API endpoints all read from `.env`.

> **Default dev credentials are only `admin / LinLinQi@2026`** — never use them on the public internet or in production.

### Option B: Local Development on Host

Pin the global toolchain via `.tool-versions`, `.node-version`, `.nvmrc` and `.go-version`; after installing Node 24.18.1 run `npm install --global npm@12.0.2`. Then:

```bash
make dev-api     # API (Go, port 8081)
make dev-user    # storefront (Vite, port 8080)
make dev-admin   # admin console (Vite, port 8082)
make migrate     # run DB migrations manually
```

`make verify-toolchain` checks dev/CI/Docker/Compose and both frontend lock files against `toolchain.json` for version drift.

## Usage

- **Auth**: Customer sign-up/sign-in with 15-minute access tokens and rotating refresh tokens; admins use separate JWT + DB RBAC + TOTP 2FA.
- **Catalog & checkout**: categories / SKUs / tiered pricing / member discounts / promotions / coupons; guest cart, multi-item checkout, transactional stock reservation, timeout release, encrypted auto-delivery.
- **Payments**: create a `signed_http` channel in the admin console and configure callback signature verification; payment providers are external systems — complete your own integration testing.
- **Suppliers**: create supplier connections, product mapping, scheduled stock/price sync.
- **OpenAPI**: HMAC-SHA256 signing, time windows, random nonces, Redis replay protection and external order idempotency — see [docs/openapi.md](docs/openapi.md).
- **Backup & restore**:

  ```bash
  make backup-postgres
  export LINLINQI_RESTORE_CONFIRM="restore:linlinqi-postgres-20260809T120000Z.dump"
  make restore-postgres BACKUP=/absolute/path/to/linlinqi-postgres-20260809T120000Z.dump
  ```

  Restore is a destructive maintenance operation; run it only after an isolated restore drill. Script backups are not a substitute for PITR or off-site backups.

## Build & Release

This section covers building and packaging the three components: the **API** (`api/`), the **customer storefront** (`user/`), and the **operations console** (`admin/`).

### 1. API (Go)

Compile a static single binary:

```bash
cd api
CGO_ENABLED=0 go build -trimpath -o linlinqi ./cmd/linlinqi
```

Cross-compile for a target platform (e.g. `linux/amd64`):

```bash
cd api
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o linlinqi-linux-amd64 ./cmd/linlinqi
```

Run database migrations (once, before first boot):

```bash
cd api
CGO_ENABLED=0 BOOTSTRAP_ADMIN=false SEED_DATA=false go run ./cmd/linlinqi migrate
```

### 2. Customer Storefront (Vue 3 / Vite)

```bash
cd user
npm install
npm run build        # vue-tsc -b && vite build -> dist/
```

Optional verification scripts:

```bash
cd user
npm run test:responsive   # node scripts/verify-responsive-contracts.mjs
npm run test:security     # node scripts/verify-security-contracts.mjs
```

### 3. Operations Console (Vue 3 / Vite)

```bash
cd admin
npm install
npm run build        # prebuild (sync shared assets) + vue-tsc -b && vite build -> dist/
```

Optional verification scripts:

```bash
cd admin
npm run test:responsive
npm run test:menu
npm run test:security
```

### All-in-one build

```bash
make build          # verify-toolchain + API binary + storefront build + admin build
```

### Docker images

The Compose topology builds and runs all three services as containers:

```bash
docker compose --env-file .env -f docker-compose.yml up -d --build
```

Each service ships its own `Dockerfile` (`api/Dockerfile`, `user/Dockerfile`, `admin/Dockerfile`) and serves the static bundle through nginx (`user/nginx.conf`, `admin/nginx.conf`).

### Publishing a release

1. Build the artifacts as shown above.
2. Tag the release: `git tag v1.0.0 && git push origin v1.0.0`.
3. Create a GitHub Release and attach the API binary plus, optionally, tarballs of both frontend `dist/` folders.

## Production Deployment Notes

1. Create `.env` — either with the generator, or manually with `umask 077`:

   ```bash
   # Recommended: fill every secret with a strong random value and derive the
   # public HTTPS URLs (API/storefront/admin + the PUBLIC_*_API_URL endpoints
   # that frontend bundles compile in). Existing secrets are preserved.
   node scripts/generate-production-env.mjs \
     --api-url https://api.example.com \
     --user-url https://store.example.com \
     --admin-url https://admin.example.com \
     --support-email ops@example.com
   # Or for a host whose DNS names already point here:
   #   node scripts/generate-production-env.mjs --auto-host store.example.com
   ```

   Manually, set real domains and generate separate keys for PostgreSQL, Redis, JWT, data encryption, OpenAPI and metrics with `openssl rand -hex 32`. The PostgreSQL password must stay URL-safe; keep `.env` at `0600` and never commit it.
2. Set `BOOTSTRAP_ADMIN=true` with a strong `BOOTSTRAP_ADMIN_PASSWORD` only for the first boot, then flip it back to `false` and remove the bootstrap password. `SEED_DATA` is dev-only; production refuses to start with it enabled.
3. Create `signed_http` payment channels, supplier connections, notification relays and webhooks in the admin API.
4. Put a trusted TLS reverse proxy in front, forwarding public HTTPS domains to the three loopback ports in `.env`; the base Compose does not issue certificates.
5. Start with the base Compose file only:

   ```bash
   docker compose --env-file .env -f docker-compose.yml up -d --build
   ```

The base Compose binds API/storefront/admin to `127.0.0.1`; PostgreSQL/Redis join an isolated data network only; database, Redis and local media use persistent volumes. The one-shot `migrate` service must exit successfully before API/Worker start — migration failure blocks release. This Compose is a single-host reference topology, not a substitute for TLS certificate management, off-site backups, PITR, centralized logging, alerting, or HA orchestration. See the [Operations](docs/OPERATIONS.md) and [Security](docs/SECURITY.md) runbooks for the production checklist.

## Verification

```bash
make verify-toolchain
make test
cd api && CGO_ENABLED=0 go build -trimpath ./cmd/linlinqi
cd ../user && npm run build
cd ../admin && npm run build
```

## Documentation Index

- [Architecture](docs/ARCHITECTURE.md) · [Features](docs/FEATURES.md) · [Security](docs/SECURITY.md)
- [Operations](docs/OPERATIONS.md) · [Supply](docs/SUPPLY.md) · [Payments](docs/PAYMENTS.md)
- [Currency & FX](docs/CURRENCY.md) · [Brand Assets](docs/BRAND_ASSETS.md) · [Lobster](docs/LOBSTER.md)
- [OpenAPI Markdown](docs/openapi.md) · [OpenAPI 3.1 YAML](docs/openapi.yaml)

> Payment providers, notification relays, object storage, domains/TLS and real suppliers are external systems owned by the deployer — this repository does not fabricate these services or ship real credentials.

## Author & Community

- **Author**: **linlinqi** — Telegram: [t.me/Spkoik](https://t.me/Spkoik)
- **Telegram Channel**: [t.me/linlinqifaka](https://t.me/linlinqifaka)

## License

This project is released as open source with **non-commercial restrictions**. See [LICENSE](LICENSE).
