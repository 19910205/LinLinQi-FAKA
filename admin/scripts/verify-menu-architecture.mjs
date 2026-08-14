#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const read = (relative) => fs.readFileSync(path.join(root, relative), "utf8");
const router = read("src/router.ts");
const layout = read("src/layouts/AdminLayout.vue");

function requireText(source, value, label) {
  if (!source.includes(value))
    throw new Error(`${label} is missing ${JSON.stringify(value)}`);
}

const routeTabs = {
  "product-categories": "categories",
  products: "products",
  variants: "variants",
  pricing: "pricing",
  "member-levels": "pricing",
  inventory: "stock",
  "card-secrets": "cards",
  "inventory-batches": "batches",
  suppliers: "supplier",
  mappings: "mapping",
  procurements: "procurement",
  customers: "customers",
  wallets: "wallets",
  "payment-channels": "channels",
  "wallet-recharges": "recharges",
  "payment-intents": "intents",
  "payment-transactions": "transactions",
  refunds: "refunds",
  promotions: "promotions",
  coupons: "coupons",
  "gift-card-batches": "batches",
  "gift-card-list": "cards",
  affiliates: "accounts",
  "affiliate-commissions": "commissions",
  "affiliate-withdrawals": "withdrawals",
  resellers: "profiles",
  "reseller-tiers": "tiers",
  "reseller-domains": "domains",
  "reseller-withdrawals": "withdrawals",
  posts: "posts",
  "post-categories": "categories",
  announcements: "announcements",
  banners: "banners",
  media: "media",
  "risk-rules": "rules",
  "risk-decisions": "decisions",
  "webhook-endpoints": "endpoints",
  "webhook-deliveries": "webhook-deliveries",
  "notification-templates": "templates",
  "notification-deliveries": "notification-deliveries",
  "notification-rules": "rules",
  "notification-connectors": "connectors",
  "notification-events": "events",
  admins: "admins",
  roles: "roles",
  "access-audit": "audit",
  currencies: "currencies",
  "fx-providers": "providers",
  "manual-rates": "manual",
  "fx-snapshots": "snapshots",
};

for (const [route, tab] of Object.entries(routeTabs)) {
  const routeIndex = router.indexOf(`path: "${route}"`);
  if (routeIndex < 0) throw new Error(`missing independent route /${route}`);
  const routeBlock = router.slice(routeIndex, routeIndex + 360);
  requireText(routeBlock, `defaultTab: "${tab}"`, `/${route} route`);
}

requireText(
  router,
  '{ path: "gift-cards", redirect: "/gift-card-batches" }',
  "legacy /gift-cards compatibility",
);

const menuModules = {
  catalog: [
    "/product-categories",
    "/products",
    "/variants",
    "/card-secrets",
    "/inventory-batches",
    "/inventory",
    "/pricing",
    "/member-levels",
  ],
  customers: ["/customers", "/wallets"],
  marketing: ["/promotions", "/coupons"],
  "gift-cards": ["/gift-card-batches", "/gift-card-list"],
  affiliates: [
    "/affiliates",
    "/affiliate-commissions",
    "/affiliate-withdrawals",
  ],
  resellers: [
    "/resellers",
    "/reseller-tiers",
    "/reseller-domains",
    "/reseller-withdrawals",
  ],
  payments: [
    "/payment-channels",
    "/wallet-recharges",
    "/payment-intents",
    "/payment-transactions",
    "/refunds",
    "/reconciliation",
  ],
  content: [
    "/posts",
    "/post-categories",
    "/announcements",
    "/banners",
    "/media",
  ],
  supply: ["/suppliers", "/category-bindings", "/mappings", "/procurements"],
  integration: [
    "/webhook-endpoints",
    "/webhook-deliveries",
    "/notification-templates",
    "/notification-deliveries",
    "/notification-rules",
    "/notification-connectors",
    "/notification-events",
    "/openapi",
  ],
  access: ["/admins", "/roles", "/access-audit"],
  risk: ["/risk-rules", "/risk-decisions"],
  currency: ["/currencies", "/fx-providers", "/manual-rates", "/fx-snapshots"],
};

for (const [module, children] of Object.entries(menuModules)) {
  requireText(layout, `id: "${module}"`, `${module} parent menu`);
  for (const child of children)
    requireText(layout, `to: "${child}"`, `${module} child menu`);
}

for (const value of [
  ':aria-expanded="moduleIsOpen(link)"',
  '@click="toggleModule(link)"',
  "link.children?.some((child) => child.to === route.path)",
]) {
  requireText(layout, value, "accessible expanding parent menu");
}

const viewRouteContracts = {
  "src/views/CatalogView.vue": [
    "/product-categories",
    "/products",
    "/variants",
    "/pricing",
  ],
  "src/views/InventoryView.vue": [
    "/inventory",
    "/card-secrets",
    "/inventory-batches",
  ],
  "src/views/SupplyView.vue": ["/suppliers", "/mappings", "/procurements"],
  "src/views/CustomerView.vue": ["/customers", "/wallets"],
  "src/views/PaymentOperationsView.vue": [
    "/payment-channels",
    "/wallet-recharges",
    "/payment-intents",
    "/payment-transactions",
    "/refunds",
  ],
  "src/views/MarketingView.vue": ["/promotions", "/coupons"],
  "src/views/GiftCardView.vue": ["/gift-card-batches", "/gift-card-list"],
  "src/views/AffiliateView.vue": [
    "/affiliates",
    "/affiliate-commissions",
    "/affiliate-withdrawals",
  ],
  "src/views/ResellerView.vue": [
    "/resellers",
    "/reseller-tiers",
    "/reseller-domains",
    "/reseller-withdrawals",
  ],
  "src/views/ContentView.vue": [
    "/posts",
    "/post-categories",
    "/announcements",
    "/banners",
    "/media",
  ],
  "src/views/IntegrationView.vue": [
    "/webhook-endpoints",
    "/webhook-deliveries",
    "/notification-templates",
    "/notification-deliveries",
  ],
  "src/views/NotificationAutomationView.vue": [
    "/notification-rules",
    "/notification-connectors",
    "/notification-events",
  ],
  "src/views/AccessView.vue": ["/admins", "/roles", "/access-audit"],
  "src/views/RiskView.vue": ["/risk-rules", "/risk-decisions"],
  "src/views/CurrencyView.vue": [
    "/currencies",
    "/fx-providers",
    "/manual-rates",
    "/fx-snapshots",
  ],
};

for (const [file, routes] of Object.entries(viewRouteContracts)) {
  const source = read(file);
  for (const route of routes)
    requireText(source, route, `${file} route-driven tab`);
}

const localeFiles = fs
  .readdirSync(path.join(root, "src", "locales"))
  .filter((file) => file.endsWith(".json"));
for (const file of localeFiles) {
  const locale = JSON.parse(read(`src/locales/${file}`));
  const parentLocaleKeys = {
    catalog: "catalogModule",
    customers: "customerModule",
    marketing: "marketingModule",
    "gift-cards": "giftCardModule",
    affiliates: "affiliateModule",
    resellers: "resellerModule",
    payments: "paymentModule",
    content: "contentModule",
    supply: "supplyModule",
    integration: "integrationModule",
    access: "accessModule",
    risk: "riskModule",
    currency: "currencyModule",
  };
  for (const module of Object.keys(menuModules)) {
    const parentKey = parentLocaleKeys[module];
    if (!locale.layout?.nav?.[parentKey])
      throw new Error(`${file} missing layout.nav.${parentKey}`);
  }
  for (const route of Object.keys(routeTabs)) {
    if (!locale.page?.[route]?.title || !locale.page?.[route]?.subtitle)
      throw new Error(`${file} missing translated page.${route}`);
  }
}

console.log(
  `Menu architecture verified: ${Object.keys(routeTabs).length} child routes, ${Object.keys(menuModules).length} expandable parents, ${localeFiles.length} locales.`,
);
