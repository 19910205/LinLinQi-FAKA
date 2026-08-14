import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const root = new URL("../", import.meta.url);
const read = (path) => readFile(new URL(path, root), "utf8");

function assertGuarded(source, path, action, guard) {
  const pattern = new RegExp(
    `(?:async\\s+)?function\\s+${action}\\s*\\([\\s\\S]{0,180}?\\)\\s*\\{\\s*if\\s*\\(!${guard.replace(".", "\\.")}\\.value\\)\\s*return;`,
  );
  assert.match(source, pattern, `${path}:${action} must enforce ${guard}`);
}

const orderPath = "src/views/OrderView.vue";
const order = await read(orderPath);
assert.match(order, /adminApi, useAuthStore/);
assert.match(
  order,
  /const canManageOrder = computed\(\(\) => auth\.hasPermission\("order\.manage"\)\);/,
);
assert.match(
  order,
  /const canManagePayment = computed\(\(\) =>\s*auth\.hasPermission\("payment\.manage"\),?\s*\);/,
);
for (const action of [
  "revealOrderInputValues",
  "submitTransition",
  "openManualOrder",
  "createManualOrder",
])
  assertGuarded(order, orderPath, action, "canManageOrder");
assertGuarded(order, orderPath, "submitRefund", "canManagePayment");
assert.match(
  order,
  /v-if="canManageOrder"[\s\S]{0,120}@click="openManualOrder"/,
);
assert.match(
  order,
  /v-if="canManageOrder && detail\.allowed_transitions\.length"/,
);
assert.match(order, /v-if="canManagePayment && detail\.can_refund"/);
assert.match(order, /v-if="manualOpen && canManageOrder"/);
assert.match(
  order,
  /function openExport\(\) \{\s+exportReason\.value = "";/,
  "order.view must retain CSV export",
);
assert.match(
  order,
  /@click="openDetail\(order\)"/,
  "order.view must retain detail",
);

const singlePermissionContracts = [
  {
    path: "src/views/MarketingView.vue",
    permission: "marketing.manage",
    actions: [
      "openCreate",
      "openPromotion",
      "openCoupon",
      "submitPromotion",
      "submitCoupon",
    ],
    templates: [
      /v-if="canManage"[\s\S]{0,100}@click="openCreate"/,
      /v-if="canManage"[\s\S]{0,100}@click="openPromotion\(item\)"/,
      /v-if="modalKind && canManage"/,
    ],
    readBoundaries: [/@click="loadList"/, /@click="changePage\(page - 1\)"/],
  },
  {
    path: "src/views/PaymentOperationsView.vue",
    permission: "payment.manage",
    actions: [
      "openCreateChannel",
      "openEditChannel",
      "saveChannel",
      "openRefund",
      "submitRefund",
    ],
    templates: [
      /v-if="activeTab === 'channels' && canManage"/,
      /v-if="activeTab === 'refunds' && canManage"/,
      /v-if="channelModal && canManage"/,
      /v-if="refundModal && canManage"/,
    ],
    readBoundaries: [/@click="loadData"/, /@click="changePage\(page - 1\)"/],
  },
  {
    path: "src/views/SupplyView.vue",
    permission: "supplier.manage",
    actions: [
      "openSupplier",
      "openSupplierStatus",
      "openSupplierSync",
      "openSupplierSyncAll",
      "openSupplierDelete",
      "openMapping",
      "openMappingDelete",
      "submitProcurementRecovery",
      "submitSupplier",
      "submitSupplierAction",
      "submitSupplierDelete",
      "probeSupplier",
      "submitMapping",
      "submitMappingDelete",
    ],
    templates: [
      /v-if="activeTab === 'supplier' && canManage"/,
      /<template v-if="canManage">[\s\S]{0,500}@click="openSupplier\(item\)"/,
      /v-if="modalKind && \(modalKind === 'procurement' \|\| canManage\)"/,
      /v-if="canManage && procurementRecoverable"/,
    ],
    readBoundaries: [
      /@click="openSupplierCatalog\(item\)"/,
      /@click="openProcurement\(item\)"/,
      /@click="loadList"/,
      /@click="changePage\(page - 1\)"/,
    ],
  },
  {
    path: "src/views/SupplierCategoryBindingView.vue",
    permission: "supplier.manage",
    actions: [
      "toggleAll",
      "toggleOne",
      "openCreate",
      "openEdit",
      "uploadDefaultCover",
      "saveEditor",
      "openDelete",
      "confirmDelete",
      "openBatch",
      "confirmBatch",
    ],
    templates: [
      /v-if="canManage"[\s\S]{0,100}class="primary-action"/,
      /v-if="canManage && selectedIDs\.length"/,
      /:disabled="!canManage"[\s\S]{0,100}@change="toggleAll"/,
      /v-if="editorOpen && canManage"/,
      /v-if="canManage && \(deleting \|\| batchAction\)"/,
    ],
    readBoundaries: [/@click="refreshPage"/, /@click="changePage\(page - 1\)"/],
  },
  {
    path: "src/components/SupplierCatalogManager.vue",
    permission: "supplier.manage",
    actions: [
      "toggleProduct",
      "toggleProductCategorySelection",
      "toggleCurrentPage",
      "savePolicy",
      "importSelected",
      "beginRetryImport",
      "retryImportJob",
    ],
    templates: [
      /:disabled="\s*!canManage \|\|\s*Boolean\(categorySelectionLoadingID\)\s*"/,
      /v-if="canManage" class="catalog-reason"/,
      /v-if="canManage" class="catalog-form-actions"/,
      /v-if="canManage"\s+class="supplier-import-dock"/,
      /canManage &&\s+job\.can_retry/,
    ],
    readBoundaries: [
      /@click\.stop="\s*toggleProductCategoryExpanded/,
      /@click="refreshAll"/,
      /@click="goProductPage\(productPage - 1\)"/,
      /@click="openRun\(run\)"/,
    ],
  },
];

for (const contract of singlePermissionContracts) {
  const source = await read(contract.path);
  assert.match(source, /adminApi, useAuthStore/);
  assert.ok(
    source.includes(
      `const canManage = computed(() => auth.hasPermission("${contract.permission}"));`,
    ),
    `${contract.path} must derive canManage from ${contract.permission}`,
  );
  for (const action of contract.actions)
    assertGuarded(source, contract.path, action, "canManage");
  for (const boundary of contract.templates)
    assert.match(source, boundary, `${contract.path} misses a write boundary`);
  for (const boundary of contract.readBoundaries)
    assert.match(source, boundary, `${contract.path} lost a read-only control`);
}

console.log("admin commerce RBAC contracts: ok");
