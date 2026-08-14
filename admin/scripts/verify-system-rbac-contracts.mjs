import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const root = new URL("../", import.meta.url);
const read = (path) => readFile(new URL(path, root), "utf8");

const contracts = {
  "src/views/IntegrationView.vue": {
    permission: "system.manage",
    actions: [
      "openTemplate",
      "saveTemplate",
      "openTest",
      "sendTest",
      "openAction",
      "submitAction",
    ],
    templateChecks: [
      /v-if="activeTab === 'templates' && canManage"/,
      /v-if="canManage && entry\.status === 'failed'"/,
      /v-if="modal && canManage"/,
    ],
  },
  "src/views/NotificationAutomationView.vue": {
    permission: "system.manage",
    actions: [
      "openRule",
      "openConnector",
      "saveRule",
      "removeRule",
      "saveConnector",
    ],
    templateChecks: [
      /v-if="canManage" class="primary" @click="openRule\(\)"/,
      /v-if="canManage" @click="openConnector\(channel\)"/,
      /v-if="modal && canManage"/,
    ],
  },
  "src/views/AccessView.vue": {
    permission: "system.manage",
    actions: [
      "openCreateAdmin",
      "openEditAdmin",
      "saveAdmin",
      "openPassword",
      "savePassword",
      "openCreateRole",
      "openEditRole",
      "saveRole",
      "openDeleteRole",
      "confirmDeleteRole",
    ],
    templateChecks: [
      /v-if="activeTab === 'admins' && canManage"/,
      /v-if="activeTab === 'roles' && canManage"/,
      /v-if="canManage" class="row-actions"/,
      /v-if="passwordModal && passwordAdmin && canManage"/,
    ],
  },
  "src/views/RiskView.vue": {
    permission: "security.manage",
    actions: [
      "openCreateRule",
      "openEditRule",
      "saveRule",
      "openReview",
      "submitReview",
    ],
    templateChecks: [
      /v-if="activeTab === 'rules' && canManage"/,
      /v-if="ruleModal && canManage"/,
      /v-if="reviewModal && reviewingDecision && canManage"/,
    ],
  },
  "src/views/CurrencyView.vue": {
    permission: "system.manage",
    actions: [
      "saveStoreCurrency",
      "saveCurrency",
      "saveProvider",
      "openManual",
      "saveManual",
      "refreshRate",
    ],
    templateChecks: [
      /:disabled="storeCurrencySaving \|\| !canManage"/,
      /v-if="canManage" class="reason-field"/,
      /v-if="canManage"[\s\S]{0,80}class="refresh-rate"/,
      /v-if="manualOpen && canManage"/,
    ],
  },
  "src/views/SettingsView.vue": {
    permission: "system.manage",
    actions: ["saveSection", "discardSection"],
    templateChecks: [
      /v-model="form\.store_name"\s+:disabled="!canManage"/,
      /v-model="form\.store_currency" :disabled="!canManage"/,
      /v-if="canManage" class="save-area"/,
    ],
  },
  "src/views/JobsView.vue": {
    permission: "system.manage",
    actions: [
      "toggleJobSelection",
      "toggleAllRetryableJobs",
      "batchRetryJobs",
      "openRetry",
      "submitRetry",
    ],
    templateChecks: [
      /v-if="canManage && selectedJobIDs\.length"/,
      /v-if="canManage && job\.retryable"/,
      /v-if="retryJob && canManage"/,
    ],
  },
  "src/views/OpenAPIView.vue": {
    permission: "system.manage",
    actions: ["setCredentialStatus"],
    templateChecks: [
      /canManage\s+\? credential\.key\s+: maskedCredentialKey\(credential\.key\)/,
      /v-if="canManage && credential\.status !== 'revoked'"/,
    ],
  },
  "src/views/ReconciliationView.vue": {
    permission: "payment.manage",
    actions: [
      "openImport",
      "handleFile",
      "submitImport",
      "openResolution",
      "submitResolution",
    ],
    templateChecks: [
      /v-if="importOpen && canManage"/,
      /v-if="resolutionOpen && resolvingItem && canManage"/,
      /canManage &&\s+item\.status !== 'matched'/,
    ],
  },
  "src/views/TicketView.vue": {
    permission: "order.manage",
    actions: ["sendReply", "updateTicket"],
    templateChecks: [
      /canManage\.value &&\s+Boolean\(selectedTicket\.value\)/,
      /v-if="canManage"\s+class="ticket-composer"/,
      /v-if="canManage"\s+class="ticket-update-form"/,
    ],
  },
};

for (const [path, contract] of Object.entries(contracts)) {
  const source = await read(path);
  assert.match(
    source,
    /import \{ adminApi, useAuthStore \} from "\.\.\/stores\/auth";/,
    `${path} must use the authoritative auth store`,
  );
  assert.ok(
    source.includes(
      `const canManage = computed(() => auth.hasPermission("${contract.permission}"));`,
    ),
    `${path} must derive canManage from ${contract.permission}`,
  );
  for (const action of contract.actions) {
    const guardedAction = new RegExp(
      `(?:async\\s+)?function\\s+${action}\\s*\\([\\s\\S]{0,180}?\\)\\s*\\{\\s*if\\s*\\(!canManage\\.value\\)\\s*return;`,
    );
    assert.match(
      source,
      guardedAction,
      `${path}:${action} must enforce permission inside the handler`,
    );
  }
  for (const check of contract.templateChecks)
    assert.match(source, check, `${path} is missing a write-control boundary`);
}

console.log("admin system RBAC contracts: ok");
