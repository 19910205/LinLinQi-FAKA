# LinLinQi Admin Console

> ⚠️ **!!! THE PROJECT IS IN THE DEVELOPMENT STAGE — updates and fixes will be made as time permits !!!**

The operations management console of the LinLinQi digital goods auto-delivery platform, built with Vue 3, Vite and TypeScript.

## Local Development

```bash
npm ci
npm run dev
```

The dev server proxies `/api` to `http://localhost:8080` through Vite. Production build:

```bash
npm run build
```

## Scope

- Products, SKUs, categories, card-key inventory and batches
- Orders, payment channels, refunds, wallets and reconciliation
- Customers, tiers, resellers, promotions, campaigns and gift cards
- Suppliers, procurement, inventory sync and OpenAPI credentials
- Tickets, content, notifications, webhooks, audit and risk control
- Admin RBAC, TOTP two-factor authentication and recovery codes
- Runtime metrics, job queues, system configuration and maintenance status
- Light/dark themes and a responsive operations UI

The console has no offline login or production demo fallback; all permissions and data are validated and provided by the backend.
