import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const root = new URL("../", import.meta.url);
const read = (path) => readFile(new URL(path, root), "utf8");
const permissions = {
  "src/views/CatalogView.vue": "catalog.manage",
  "src/views/InventoryView.vue": "inventory.manage",
  "src/views/ContentView.vue": "marketing.manage",
  "src/views/GiftCardView.vue": "marketing.manage",
  "src/views/AffiliateView.vue": "marketing.manage",
  "src/views/ResellerView.vue": "reseller.manage",
  "src/components/ResellerWholesaleTierAdmin.vue": "reseller.manage",
  "src/components/ResellerWithdrawalAdmin.vue": "reseller.manage",
};
const sources = Object.fromEntries(
  await Promise.all(
    Object.keys(permissions).map(async (path) => [path, await read(path)]),
  ),
);

for (const [path, permission] of Object.entries(permissions)) {
  const source = sources[path];
  assert.match(
    source,
    /import \{ adminApi, useAuthStore \} from "\.\.\/stores\/auth";/,
  );
  assert.ok(
    source.includes(`authStore.hasPermission("${permission}")`),
    `${path} must derive its write capability from ${permission}`,
  );

  const functionStarts = [
    ...source.matchAll(/(?:async\s+)?function\s+([A-Za-z0-9_]+)\s*\(/g),
  ];
  for (const writeCall of source.matchAll(
    /adminApi\.(?:post|put|patch|delete)\s*\(/g,
  )) {
    const owner = functionStarts
      .filter((candidate) => candidate.index < writeCall.index)
      .at(-1);
    assert.ok(owner, `${path} has a write call outside a named function`);
    const opening = source.slice(
      owner.index,
      Math.min(writeCall.index, owner.index + 500),
    );
    assert.match(
      opening,
      /\{\s*if \(!canManage\.value\) return(?: false)?;/,
      `${path}:${owner[1]} must guard its write call with canManage`,
    );
  }
}

const catalog = sources["src/views/CatalogView.vue"];
assert.match(
  catalog,
  /v-if="canManage" class="primary-action" @click="openProduct\(\)"/,
);
assert.ok(
  (catalog.match(/<div v-if="canManage" class="row-actions">/g) || []).length >=
    5,
  "catalog write-only row action groups must be permission gated",
);
assert.match(catalog, /v-if="editor && canManage"/);
assert.match(catalog, /v-if="deleteTarget && canManage"/);

const inventory = sources["src/views/InventoryView.vue"];
assert.match(
  inventory,
  /v-if="canManage" class="primary-action" @click="openImport"/,
);
assert.match(inventory, /v-if="canManage && selectedCardIDs\.length"/);
assert.match(inventory, /v-if="importOpen && canManage"/);
assert.match(inventory, /v-if="statusTarget && canManage"/);

const content = sources["src/views/ContentView.vue"];
assert.match(content, /v-if="canManage"[\s\S]{0,100}@click="openCreate"/);
assert.match(content, /v-if="editorOpen && canManage"/);
assert.match(content, /v-if="deleteTarget && canManage"/);
assert.match(
  content,
  /<button\s+type="button"\s+class="row-action"\s+@click="copyMediaURL\(item\)"/,
  "view-only users must retain media URL copy",
);

const giftCards = sources["src/views/GiftCardView.vue"];
assert.match(giftCards, /v-if="canManage"[\s\S]{0,100}@click="openIssue"/);
assert.match(giftCards, /v-if="modalKind && canManage"/);
assert.match(
  giftCards,
  /<button type="button" @click="viewBatchCards\(item\)">/,
  "view-only users must retain batch detail navigation",
);

const affiliates = sources["src/views/AffiliateView.vue"];
assert.match(
  affiliates,
  /v-if="canManage && accountTransitions\(item\.status\)\.length"/,
);
assert.match(
  affiliates,
  /<button type="button" @click="openWithdrawal\(item\)">/,
  "view-only users must retain withdrawal detail",
);
assert.match(
  affiliates,
  /v-if="canManage && currentWithdrawalTransitions\.length"/,
);

const resellers = sources["src/views/ResellerView.vue"];
assert.match(
  resellers,
  /v-if="canManage && profileTransitions\(item\.status\)\.length"/,
);
assert.match(
  resellers,
  /<button\s+v-if="canManage"\s+type="button"\s+@click="openDomain\(item\)"/,
);
assert.match(resellers, /v-if="modalKind && canManage"/);

const tiers = sources["src/components/ResellerWholesaleTierAdmin.vue"];
assert.match(
  tiers,
  /<button v-if="canManage" type="button" @click="openEdit\(item\)">/,
);
assert.match(tiers, /v-if="modalOpen && canManage"/);

const withdrawals = sources["src/components/ResellerWithdrawalAdmin.vue"];
assert.match(withdrawals, /<label\s+v-if="canManage"/);
assert.match(withdrawals, /<div v-if="canManage" class="reveal-box">/);
assert.match(withdrawals, /<form v-if="canManage" @submit\.prevent="submit">/);
assert.doesNotMatch(
  withdrawals,
  /:disabled="!\['pending', 'processing'\]\.includes\(item\.status\)"/,
  "view-only and terminal records must retain detail access",
);

const dashboard = await read("src/views/DashboardView.vue");
for (const [permission, capability] of [
  ["catalog.manage", "canManageCatalog"],
  ["inventory.manage", "canManageInventory"],
  ["order.manage", "canManageOrders"],
]) {
  assert.ok(dashboard.includes(`authStore.hasPermission("${permission}")`));
  assert.ok(dashboard.includes(`v-if="${capability}"`));
}
assert.match(
  dashboard,
  /<RouterLink to="\/orders"/,
  "the dashboard's read-only order link must remain visible",
);

console.log("admin business UI RBAC contracts: ok");
