# LinLinQi Customer Storefront

> ⚠️ **!!! THE PROJECT IS IN THE DEVELOPMENT STAGE — updates and fixes will be made as time permits !!!**

The customer-facing frontend of the LinLinQi digital goods auto-delivery platform, built with Vue 3, Vite and TypeScript.

## Local Development

```bash
npm ci
npm run dev
```

The dev server proxies `/api` to `http://localhost:8081` through Vite. Production build:

```bash
npm run build
```

## Main Pages

- Product categories, search, SKU selection, cart and checkout
- Guest orders, member orders, payments and card-key delivery
- Sign-up, sign-in, password recovery and session security
- Wallet, gift cards, tickets, referral commissions and OpenAPI keys
- Reseller application, tiers, domains and pricing rules
- Announcements, blog, terms of service and privacy policy
- Light/dark themes and mobile adaptation

The API, payment callbacks, email and external redirect URLs in production are configured centrally by the backend; the frontend stores no server-side secrets.
