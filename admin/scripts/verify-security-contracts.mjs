import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import ts from "typescript";

const root = new URL("../", import.meta.url);
const read = (path) => readFile(new URL(path, root), "utf8");
const sourceFiles = [
  "src/stores/auth.ts",
  "src/views/SecurityView.vue",
  "src/views/CustomerView.vue",
  "src/views/PaymentOperationsView.vue",
  "src/views/SupplyView.vue",
  "src/views/SupplierCategoryBindingView.vue",
  "src/views/ContentView.vue",
  "src/views/CurrencyView.vue",
  "src/views/AnalyticsView.vue",
  "src/views/GiftCardView.vue",
  "src/views/ModuleView.vue",
  "src/utils/csv.ts",
  "src/utils/money.ts",
  "src/utils/publicUrl.ts",
  "src/layouts/AdminLayout.vue",
];
const sources = Object.fromEntries(
  await Promise.all(sourceFiles.map(async (path) => [path, await read(path)])),
);
const allSource = Object.values(sources).join("\n");

assert.doesNotMatch(
  allSource,
  /\bv-html\s*=/,
  "admin must not use raw HTML sinks",
);
assert.match(sources["src/stores/auth.ts"], /targetsAdminAPI/);
assert.match(
  sources["src/stores/auth.ts"],
  /if \(!targetsAdminAPI\(config\)\) return config/,
);
assert.match(sources["src/stores/auth.ts"], /let profileRefreshed = false/);
assert.match(
  sources["src/stores/auth.ts"],
  /if \(!token\.value \|\| profileRefreshed\) return/,
);
assert.match(
  sources["src/stores/auth.ts"],
  /\.then\(\(\{ data \}\) => saveProfile\(data\.data, true\)\)/,
);
assert.match(
  sources["src/stores/auth.ts"],
  /saveProfile\(data\.data\.admin, true\)/,
  "a successful login response should be authoritative for the current page session",
);
assert.doesNotMatch(
  sources["src/stores/auth.ts"],
  /profileRefreshed\s*=\s*Array\.isArray\(parsed\?\.permissions\)/,
  "persisted permissions must never suppress the per-page session refresh",
);
assert.doesNotMatch(
  sources["src/stores/auth.ts"],
  /if\s*\(!profileRefreshed\)\s*return true/,
  "a failed session refresh must fall back to cached permissions, never grant every permission",
);
assert.match(sources["src/views/ContentView.vue"], /safeAdminHTTPURL/);
assert.match(sources["src/views/CurrencyView.vue"], /safeAdminHTTPURL/);
assert.match(
  sources["src/utils/money.ts"],
  /\/api\/v1\/currency-directory/,
  "business modules must use the public currency directory",
);
assert.doesNotMatch(
  sources["src/utils/money.ts"],
  /\/api\/v1\/currencies/,
  "the admin formatter must consume the directory envelope, not the legacy currency array",
);
assert.doesNotMatch(
  sources["src/utils/money.ts"] +
    sources["src/views/PaymentOperationsView.vue"] +
    sources["src/views/SupplyView.vue"],
  /adminApi\.get\("\/currencies"\)/,
  "fine-grained business roles must not need system.view for currency metadata",
);
for (const permission of ["dashboard.read", "system.view"])
  assert.ok(
    sources["src/layouts/AdminLayout.vue"].includes(
      `auth.hasPermission("${permission}")`,
    ),
    `runtime summary polling missing ${permission} guard`,
  );
assert.match(
  sources["src/views/SupplierCategoryBindingView.vue"],
  /\/supplier-category-mappings\/media\/upload/,
  "supplier category covers must use their supplier.manage-scoped upload route",
);
for (const permission of ["system.view", "system.manage", "security.view"])
  assert.ok(
    sources["src/views/SecurityView.vue"].includes(
      `auth.hasPermission("${permission}")`,
    ),
    `security center missing ${permission} partition`,
  );
assert.doesNotMatch(
  sources["src/views/SecurityView.vue"],
  /Promise\.all\(\[\s*adminApi\.get\("\/security\/2fa"\)[\s\S]{0,240}ip-blocklist/,
  "personal 2FA status must not share a failure boundary with privileged operations",
);
assert.match(
  sources["src/views/SecurityView.vue"],
  /v-if="canViewSecurityEvents" class="panel security-events"/,
);
assert.match(
  sources["src/views/SecurityView.vue"],
  /v-if="canViewIPBlocks" class="panel blocklist-panel"/,
);
assert.match(
  sources["src/views/CustomerView.vue"],
  /mode\.value === "wallets" \? "\/wallets\/users" : "\/users"/,
  "wallet mode must use its wallet.view-scoped list endpoint",
);
assert.match(
  sources["src/views/CustomerView.vue"],
  /`\/wallets\/users\/\$\{encodeURIComponent\(id\)\}`/,
  "wallet mode must use its wallet.view-scoped detail endpoint",
);
assert.match(
  sources["src/views/CustomerView.vue"],
  /mode\.value === "wallets" \? "\/wallets\/users\/export" : "\/users\/export"/,
  "customer and wallet exports must remain permission-scoped",
);
assert.match(
  sources["src/views/CustomerView.vue"],
  /v-if="mode === 'wallets'"[^>]*>\{\{ t\("customer\.colBalance"\) \}\}/,
  "customer.view tables must not render wallet balances",
);
for (const permission of ["customer.manage", "wallet.manage"])
  assert.ok(
    sources["src/views/CustomerView.vue"].includes(
      `auth.hasPermission("${permission}")`,
    ),
    `customer and wallet actions missing ${permission} partition`,
  );
for (const path of [
  "src/views/AnalyticsView.vue",
  "src/views/GiftCardView.vue",
  "src/views/ModuleView.vue",
])
  assert.match(sources[path], /safeCSVCell/);
assert.doesNotMatch(
  allSource,
  /\beval\s*\(|\bnew Function\b|document\.write\s*\(|\.postMessage\s*\(/,
);

const csvOutput = ts.transpileModule(sources["src/utils/csv.ts"], {
  compilerOptions: {
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
const csv = await import(
  `data:text/javascript;base64,${Buffer.from(csvOutput).toString("base64")}#${Date.now()}`
);
assert.equal(
  csv.safeCSVCell('=HYPERLINK("https://evil")'),
  '"\'=HYPERLINK(""https://evil"")"',
);
assert.equal(csv.safeCSVCell("  @SUM(1,1)"), '"\'  @SUM(1,1)"');
assert.equal(csv.safeCSVCell(-125), '"-125"');

for (const locale of ["en", "ja", "ko", "ru", "th", "vi", "zh-CN", "zh-TW"]) {
  const messages = JSON.parse(await read(`src/locales/${locale}.json`));
  assert.ok(messages.security?.typeRevoke?.includes("<code>REVOKE</code>"));
}

for (const nginx of ["nginx.conf", "../deploy/macos/nginx.conf"]) {
  const config = await read(nginx);
  const expectedAppServers = nginx.startsWith("../") ? 2 : 1;
  for (const header of [
    "Content-Security-Policy",
    "X-Content-Type-Options",
    "X-Frame-Options",
    "Referrer-Policy",
    "Permissions-Policy",
  ])
    assert.ok(config.includes(header), `${nginx} missing ${header}`);
  for (const directive of [
    "style-src-elem 'self'",
    "style-src-attr 'unsafe-inline'",
    "frame-src 'none'",
    "frame-ancestors 'none'",
  ])
    assert.ok(config.includes(directive), `${nginx} missing ${directive}`);
  assert.equal(
    config.match(/style-src-elem 'self'/g)?.length,
    expectedAppServers,
    `${nginx} must harden every app server block`,
  );
}

console.log("admin security contracts: ok");
