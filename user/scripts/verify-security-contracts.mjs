import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";
import ts from "typescript";

const root = new URL("../", import.meta.url);
const read = (path) => readFile(new URL(path, root), "utf8");
const sourceFiles = [
  "src/App.vue",
  "src/api.ts",
  "src/components/WalletCenter.vue",
  "src/components/UserNotificationCenter.vue",
  "src/views/AccountView.vue",
  "src/views/AccountCenterView.vue",
  "src/views/CheckoutView.vue",
  "src/views/ProductView.vue",
  "src/router.ts",
  "src/utils/sanitizeHTML.ts",
  "src/utils/publicUrl.ts",
  "../api/internal/handler/user_notifications.go",
  "../api/internal/router/router.go",
];
const sources = Object.fromEntries(
  await Promise.all(sourceFiles.map(async (path) => [path, await read(path)])),
);
const allSource = Object.values(sources).join("\n");

assert.doesNotMatch(
  sources["src/views/ProductView.vue"],
  /v-html\s*=\s*["']item\.product\.description/,
  "API product HTML must never reach v-html directly",
);
assert.match(
  sources["src/views/ProductView.vue"],
  /v-html="safeProductDescription"/,
);
for (const token of [
  '"script"',
  '"iframe"',
  '"object"',
  '"svg"',
  '"style"',
  "element.attributes",
  "safePublicHTTPURL",
  '"noopener noreferrer"',
])
  assert.ok(
    sources["src/utils/sanitizeHTML.ts"].includes(token),
    `sanitizer contract missing ${token}`,
  );

assert.doesNotMatch(
  allSource,
  /(?:window\.)?location\.assign\(\s*(?:payment|result)\.intent\.checkout_url\s*\)/,
  "API checkout URLs must pass the navigation allowlist",
);
for (const path of [
  "src/views/ProductView.vue",
  "src/views/CheckoutView.vue",
  "src/components/WalletCenter.vue",
])
  assert.match(sources[path], /safeNavigationURL/);
assert.match(sources["src/views/AccountView.vue"], /safeInternalPath/);
assert.match(
  sources["src/views/AccountCenterView.vue"],
  /\["notifications",\s*t\("account\.notifications"\)/,
);
assert.match(
  sources["src/views/AccountCenterView.vue"],
  /"wallet",\s*"notifications",/,
  "notifications must be component-owned and must not trigger a duplicate generic /me request",
);
for (const endpoint of [
  'user.GET("/notifications", h.MyNotifications)',
  'user.POST("/notifications/:id/read", h.MarkMyNotificationRead)',
])
  assert.ok(
    sources["../api/internal/router/router.go"].includes(endpoint),
    `missing authenticated user notification route ${endpoint}`,
  );
assert.match(
  sources["../api/internal/handler/user_notifications.go"],
  /Where\("user_id = \?", userID\)/,
  "notification list must be scoped to the authenticated user",
);
assert.match(
  sources["../api/internal/handler/user_notifications.go"],
  /Where\("id = \? AND user_id = \?", notificationID, userID\)/,
  "mark-read must not update another user's notification",
);
assert.match(
  sources["src/components/UserNotificationCenter.vue"],
  /@keydown\.enter\.prevent="read\(item\)"/,
  "notification actions must remain keyboard accessible",
);
const accountSectionBlock = sources["src/router.ts"].match(
  /const accountSections = new Set\(\[([\s\S]*?)\]\);/,
)?.[1];
assert.ok(accountSectionBlock?.includes('"notifications"'));
assert.match(sources["src/api.ts"], /targetsConfiguredAPI/);
assert.match(
  sources["src/api.ts"],
  /if \(!targetsConfiguredAPI\(config\)\) return config/,
);
assert.doesNotMatch(
  allSource,
  /\beval\s*\(|\bnew Function\b|document\.write\s*\(|\.postMessage\s*\(/,
);
const storefrontNginx = await read("nginx.conf");
for (const directive of [
  "style-src-elem 'self'",
  "style-src-attr 'unsafe-inline'",
  "frame-src 'none'",
  "frame-ancestors 'none'",
])
  assert.ok(storefrontNginx.includes(directive), `nginx missing ${directive}`);

async function importTypeScript(path) {
  const source = await read(path);
  const output = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText;
  return import(
    `data:text/javascript;base64,${Buffer.from(output).toString("base64")}#${Date.now()}-${path}`
  );
}

globalThis.window = { location: new URL("https://shop.example.test/checkout") };
const navigation = await importTypeScript("src/utils/publicUrl.ts");
assert.equal(navigation.safeNavigationURL("javascript:alert(1)"), "");
assert.equal(navigation.safeNavigationURL("data:text/html,x"), "");
assert.equal(navigation.safeNavigationURL("http://evil.example/pay"), "");
assert.equal(
  navigation.safeNavigationURL("https://pay.example.test/session"),
  "https://pay.example.test/session",
);
assert.equal(
  navigation.safeInternalPath("//evil.example/path", "/account/profile"),
  "/account/profile",
);
assert.equal(
  navigation.safeInternalPath("/\\evil.example/path", "/account/profile"),
  "/account/profile",
);
assert.equal(
  navigation.safeInternalPath(
    "/%255c%255cevil.example/path",
    "/account/profile",
  ),
  "/account/profile",
);
assert.equal(
  navigation.safeInternalPath(
    "/%252f%252fevil.example/path",
    "/account/profile",
  ),
  "/account/profile",
);
assert.equal(
  navigation.safeInternalPath("/account/orders?q=1", "/account/profile"),
  "/account/orders?q=1",
);

const patterns = await importTypeScript("src/utils/browserPattern.ts");
assert.equal(patterns.safeBrowserPattern("(a+)+"), null);
assert.equal(patterns.safeBrowserPattern("((a+))+"), null);
assert.equal(patterns.safeBrowserPattern("(a?)+"), null);
assert.equal(patterns.safeBrowserPattern("(a|aa)+"), null);
assert.equal(
  patterns.safeBrowserPattern("[A-Z0-9_-]{4,32}")?.test("AB_12"),
  true,
);

console.log("user security contracts: ok");
