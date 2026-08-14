<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  Check,
  CheckCircle2,
  Clock3,
  CodeXml,
  Copy,
  KeyRound,
  PauseCircle,
  RefreshCw,
  RotateCw,
  ShieldCheck,
  Webhook,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";

const { t, locale } = useI18n();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("system.manage"));

type CredentialStatus = "active" | "suspended" | "pending" | "revoked";

interface APICredential {
  id: string;
  owner_type: string;
  owner_id?: string | null;
  name: string;
  key: string;
  permissions: string;
  status: CredentialStatus;
  last_used_at?: string | null;
  created_at: string;
}

interface APICallLog {
  id: string;
  credential_id: string;
  method: string;
  path: string;
  status_code: number;
  duration_ms: number;
  request_id?: string;
  ip?: string;
  created_at: string;
}

interface PageResult<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

const tab = ref<"overview" | "docs" | "logs">("overview");
const copied = ref("");
const credentials = ref<APICredential[]>([]);
const credentialTotal = ref(0);
const logs = ref<APICallLog[]>([]);
const logTotal = ref(0);
const logPage = ref(1);
const pageSize = 20;
const loadingCredentials = ref(false);
const loadingLogs = ref(false);
const updatingCredential = ref("");
const loadError = ref("");
const actionNotice = ref("");
const apiReachability = ref<"checking" | "online" | "offline">("checking");
const orderInputMode = ref<"parameters" | "input_values">("parameters");

const openAPIBaseURL = computed(() => {
  const adminBase = String(adminApi.defaults.baseURL || "/admin/v1").replace(
    /\/+$/,
    "",
  );
  return /\/admin\/v1$/i.test(adminBase)
    ? adminBase.replace(/\/admin\/v1$/i, "/openapi/v1")
    : `${adminBase}/openapi/v1`;
});

const productsURL = computed(() => `${openAPIBaseURL.value}/products`);
const openAPIPathBase = computed(() => {
  try {
    return new URL(
      openAPIBaseURL.value,
      window.location.origin,
    ).pathname.replace(/\/+$/, "");
  } catch {
    return "/openapi/v1";
  }
});

const curl = computed(
  () => `BASE_URL=${JSON.stringify(openAPIBaseURL.value)}
API_PATH=${JSON.stringify(`${openAPIPathBase.value}/products?currency=USD`)}
API_KEY='<YOUR_API_KEY>'
API_SECRET='<YOUR_API_SECRET>'
TIMESTAMP="$(date +%s)"
NONCE="$(openssl rand -hex 16)"
BODY=''
BODY_SHA="$(printf '%s' "$BODY" | openssl dgst -sha256 -binary | xxd -p -c 256)"
CANONICAL="${"${TIMESTAMP}"}.${"${NONCE}"}.GET.${"${API_PATH}"}.${"${BODY_SHA}"}"
SIGNATURE="$(printf '%s' "$CANONICAL" | openssl dgst -sha256 -hmac "$API_SECRET" -binary | xxd -p -c 256)"

curl --fail-with-body --silent --show-error "$BASE_URL/products?currency=USD" \\
  -H "X-API-Key: $API_KEY" \\
  -H "X-Timestamp: $TIMESTAMP" \\
  -H "X-Nonce: $NONCE" \\
  -H "X-Signature: $SIGNATURE"`,
);

const catalogExample = `{
  "code": 0,
  "message": "ok",
  "data": [{
    "external_id": "<PRODUCT_EXTERNAL_ID>",
    "external_sku": "game-credit",
    "name": "Game credit",
    "source_currency": "CNY",
    "currency": "USD",
    "fx": {
      "snapshot_id": "<FX_SNAPSHOT_UUID>",
      "source_currency": "CNY",
      "target_currency": "USD",
      "rate": "0.13945",
      "source_tier": "live",
      "expires_at": "2026-08-09T18:00:00Z"
    },
    "price": 9900,
    "stock": 128,
    "delivery": "auto",
    "variants": [{
      "external_id": "<VARIANT_EXTERNAL_ID>",
      "external_sku": "game-credit-100",
      "name": "100 credits",
      "price": 9900,
      "stock": 128,
      "purchase_limit": 20
    }],
    "input_fields": [{
      "id": "<FIELD_UUID>",
      "key": "account_id",
      "label": "Account UID",
      "input_type": "text",
      "required": true,
      "sensitive": true,
      "placeholder": "Enter UID",
      "help_text": "6–32 digits",
      "options": [],
      "validation_pattern": "[0-9]+",
      "min_length": 6,
      "max_length": 32,
      "sort": 10
    }]
  }]
}`;

const parametersOrderExample = `{
  "external_product_id": "<PRODUCT_OR_VARIANT_EXTERNAL_ID>",
  "quantity": 1,
  "email": "buyer@your-company.tld",
  "payment_method": "supplier_balance",
  "currency": "USD",
  "client_order_no": "YOUR-ORDER-20260809-0001",
  "callback_url": "https://<YOUR_PUBLIC_HOST>/callbacks/linlinqi",
  "parameters": {
    "account_id": "9384750291"
  }
}`;

const inputValuesOrderExample = `{
  "external_product_id": "<PRODUCT_OR_VARIANT_EXTERNAL_ID>",
  "quantity": 1,
  "email": "buyer@your-company.tld",
  "payment_method": "supplier_balance",
  "currency": "USD",
  "client_order_no": "YOUR-ORDER-20260809-0001",
  "input_values": [
    { "field_id": "<FIELD_UUID>", "value": "9384750291" }
  ]
}`;

const selectedOrderExample = computed(() =>
  orderInputMode.value === "parameters"
    ? parametersOrderExample
    : inputValuesOrderExample,
);

const createOrderCurl = computed(() => {
  const body = JSON.stringify(
    orderInputMode.value === "parameters"
      ? {
          external_product_id: "<PRODUCT_OR_VARIANT_EXTERNAL_ID>",
          quantity: 1,
          email: "buyer@your-company.tld",
          payment_method: "supplier_balance",
          currency: "USD",
          client_order_no: "YOUR-ORDER-20260809-0001",
          callback_url: "https://<YOUR_PUBLIC_HOST>/callbacks/linlinqi",
          parameters: { account_id: "9384750291" },
        }
      : {
          external_product_id: "<PRODUCT_OR_VARIANT_EXTERNAL_ID>",
          quantity: 1,
          email: "buyer@your-company.tld",
          payment_method: "supplier_balance",
          currency: "USD",
          client_order_no: "YOUR-ORDER-20260809-0001",
          input_values: [{ field_id: "<FIELD_UUID>", value: "9384750291" }],
        },
  );
  return `BASE_URL=${JSON.stringify(openAPIBaseURL.value)}
API_PATH=${JSON.stringify(`${openAPIPathBase.value}/orders`)}
API_KEY='<YOUR_API_KEY>'
API_SECRET='<YOUR_API_SECRET>'
TIMESTAMP="$(date +%s)"
NONCE="$(openssl rand -hex 16)"
BODY='${body}'
BODY_SHA="$(printf '%s' "$BODY" | openssl dgst -sha256 -binary | xxd -p -c 256)"
CANONICAL="${"${TIMESTAMP}"}.${"${NONCE}"}.POST.${"${API_PATH}"}.${"${BODY_SHA}"}"
SIGNATURE="$(printf '%s' "$CANONICAL" | openssl dgst -sha256 -hmac "$API_SECRET" -binary | xxd -p -c 256)"

curl --fail-with-body --silent --show-error "$BASE_URL/orders" \\
  -H 'Content-Type: application/json' \\
  -H "X-API-Key: $API_KEY" \\
  -H "X-Timestamp: $TIMESTAMP" \\
  -H "X-Nonce: $NONCE" \\
  -H "X-Signature: $SIGNATURE" \\
  --data-raw "$BODY"`;
});

const orderResponseExample = `{
  "code": 0,
  "message": "Created successfully",
  "data": {
    "client_order_no": "YOUR-ORDER-20260809-0001",
    "external_order_no": "LQ202608090001",
    "status": "delivered",
    "currency": "USD",
    "deliveries": ["CARD-CONTENT-1"],
    "cost": 9900
  }
}`;

const callbackExample = `{
  "event_id": "order.delivered:<ORDER_UUID>",
  "event": "order.delivered",
  "occurred_at": "2026-08-09T11:00:00Z",
  "data": {
    "client_order_no": "YOUR-ORDER-20260809-0001",
    "external_order_no": "LQ202608090001",
    "status": "delivered",
    "deliveries": ["CARD-CONTENT-1"],
    "cost": 9900
  }
}`;

const errorExample = `{
  "code": 42222,
  "message": "<LOCALIZED_ERROR_MESSAGE>"
}`;

const activeCredentialCount = computed(
  () => credentials.value.filter((item) => item.status === "active").length,
);
const sampleAverageLatency = computed(() => {
  if (!logs.value.length) return 0;
  return Math.round(
    logs.value.reduce((sum, item) => sum + item.duration_ms, 0) /
      logs.value.length,
  );
});
const sampleErrorRate = computed(() => {
  if (!logs.value.length) return 0;
  return (
    (logs.value.filter((item) => item.status_code >= 400).length /
      logs.value.length) *
    100
  );
});
const logPageCount = computed(() =>
  Math.max(1, Math.ceil(logTotal.value / pageSize)),
);

const endpoints = computed(() => [
  {
    method: "GET",
    path: "/account/balance",
    permission: "orders:write",
    description: t("openapi.endpointBalance"),
  },
  {
    method: "GET",
    path: "/products",
    permission: "products:read",
    description: t("openapi.endpointProducts"),
  },
  {
    method: "POST",
    path: "/orders",
    permission: "orders:write",
    description: t("openapi.endpointOrders"),
  },
  {
    method: "GET",
    path: "/orders/:order_no",
    permission: "orders:read",
    description: t("openapi.endpointOrderDetail"),
  },
]);

function parsePage<T>(payload: unknown, fallbackPage = 1): PageResult<T> {
  if (Array.isArray(payload)) {
    return {
      items: payload as T[],
      total: payload.length,
      page: fallbackPage,
      page_size: pageSize,
    };
  }
  const value = (payload || {}) as Partial<PageResult<T>>;
  return {
    items: Array.isArray(value.items) ? value.items : [],
    total: Number(value.total || 0),
    page: Number(value.page || fallbackPage),
    page_size: Number(value.page_size || pageSize),
  };
}

async function copy(text: string, label: string) {
  try {
    await navigator.clipboard.writeText(text);
    copied.value = label;
    window.setTimeout(() => (copied.value = ""), 1200);
  } catch {
    loadError.value = t("openapi.errClipboard");
  }
}

function errorMessage(error: any, fallback: string) {
  return error?.response?.data?.message || fallback;
}

async function loadCredentials() {
  loadingCredentials.value = true;
  loadError.value = "";
  try {
    const { data } = await adminApi.get("/api-credentials", {
      params: { page: 1, page_size: 100 },
    });
    const result = parsePage<APICredential>(data.data);
    credentials.value = result.items;
    credentialTotal.value = result.total;
    apiReachability.value = "online";
  } catch (error: any) {
    apiReachability.value = "offline";
    loadError.value = errorMessage(error, t("openapi.errLoad"));
  } finally {
    loadingCredentials.value = false;
  }
}

async function loadLogs(page = logPage.value) {
  loadingLogs.value = true;
  loadError.value = "";
  try {
    const { data } = await adminApi.get("/api-call-logs", {
      params: { page, page_size: pageSize },
    });
    const result = parsePage<APICallLog>(data.data, page);
    logs.value = result.items;
    logTotal.value = result.total;
    logPage.value = result.page;
  } catch (error: any) {
    loadError.value = errorMessage(error, t("openapi.errLoadLogs"));
  } finally {
    loadingLogs.value = false;
  }
}

async function setCredentialStatus(
  credential: APICredential,
  status: CredentialStatus,
) {
  if (!canManage.value) return;
  if (credential.status === status || updatingCredential.value) return;
  const reason = window.prompt(
    t("openapi.promptStatus", {
      name: credential.name,
      status: statusLabel(status),
    }),
  );
  if (reason === null) return;
  if (!reason.trim()) {
    loadError.value = t("openapi.errReason");
    return;
  }
  updatingCredential.value = credential.id;
  loadError.value = "";
  actionNotice.value = "";
  try {
    await adminApi.patch(
      `/api-credentials/${credential.id}`,
      { status },
      {
        headers: {
          "X-Change-Reason": reason.trim(),
        },
      },
    );
    actionNotice.value = t("openapi.updated", {
      name: credential.name,
      status: statusLabel(status),
    });
    await loadCredentials();
  } catch (error: any) {
    loadError.value = errorMessage(error, t("openapi.errUpdate"));
  } finally {
    updatingCredential.value = "";
  }
}

function statusLabel(status: CredentialStatus) {
  return {
    active: t("openapi.credentialStatuses.active"),
    suspended: t("openapi.credentialStatuses.suspended"),
    pending: t("openapi.credentialStatuses.pending"),
    revoked: t("openapi.revoked"),
  }[status];
}

function formatDate(value?: string | null) {
  if (!value) return t("openapi.never");
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "—"
    : date.toLocaleString(locale.value, { hour12: false });
}

function shortID(value?: string) {
  if (!value) return "—";
  return value.length > 16 ? `${value.slice(0, 8)}…${value.slice(-5)}` : value;
}

function maskedCredentialKey(value: string) {
  if (!value) return "—";
  return value.length > 10
    ? `${value.slice(0, 6)}${"•".repeat(8)}${value.slice(-4)}`
    : "•".repeat(value.length);
}

function credentialName(credentialID: string) {
  const credential = credentials.value.find((item) => item.id === credentialID);
  return credential?.name || shortID(credentialID);
}

function switchTab(next: "overview" | "docs" | "logs") {
  tab.value = next;
  loadError.value = "";
  actionNotice.value = "";
  if (next === "logs" && !logs.value.length) void loadLogs(1);
}

function changeLogPage(page: number) {
  if (page < 1 || page > logPageCount.value || loadingLogs.value) return;
  void loadLogs(page);
}

onMounted(async () => {
  await Promise.all([loadCredentials(), loadLogs(1)]);
});
</script>

<template>
  <div class="openapi-grid">
    <section class="panel api-main">
      <div class="api-tabs" role="tablist" :aria-label="t('openapi.tabsAria')">
        <button
          :class="{ active: tab === 'overview' }"
          role="tab"
          :aria-selected="tab === 'overview'"
          @click="switchTab('overview')"
        >
          {{ t("openapi.credentials") }}
        </button>
        <button
          :class="{ active: tab === 'docs' }"
          role="tab"
          :aria-selected="tab === 'docs'"
          @click="switchTab('docs')"
        >
          {{ t("openapi.docs") }}
        </button>
        <button
          :class="{ active: tab === 'logs' }"
          role="tab"
          :aria-selected="tab === 'logs'"
          @click="switchTab('logs')"
        >
          {{ t("openapi.logs") }}
        </button>
      </div>

      <p v-if="loadError" class="module-state error">{{ loadError }}</p>
      <p v-if="actionNotice" class="module-state success">
        {{ actionNotice }}
      </p>

      <template v-if="tab === 'overview'">
        <div class="api-hero">
          <div>
            <span class="kicker">LINLINQI OPENAPI V1</span>
            <h2>{{ t("openapi.heroTitle") }}</h2>
            <p>
              {{ t("openapi.heroSub") }}
            </p>
            <div>
              <span><ShieldCheck :size="15" /> {{ t("openapi.hmac") }}</span>
              <span><KeyRound :size="15" /> {{ t("openapi.nonce") }}</span>
              <span
                ><Webhook :size="15" /> {{ t("openapi.reliableNotify") }}</span
              >
            </div>
          </div>
          <CodeXml />
        </div>

        <div class="api-section-head">
          <div>
            <h3>{{ t("openapi.credentialsTitle") }}</h3>
            <p>
              {{
                t("openapi.credentialsSub", {
                  total: credentialTotal,
                  active: activeCredentialCount,
                })
              }}
            </p>
          </div>
          <button :disabled="loadingCredentials" @click="loadCredentials">
            <RefreshCw :size="14" :class="{ spinning: loadingCredentials }" />
            {{ t("openapi.refresh") }}
          </button>
        </div>

        <div v-if="loadingCredentials && !credentials.length" class="api-empty">
          {{ t("openapi.loading") }}
        </div>
        <div v-else-if="!credentials.length" class="api-empty">
          {{ t("openapi.noCredentials") }}
        </div>
        <div v-else class="credential-list">
          <article
            v-for="credential in credentials"
            :key="credential.id"
            class="credential-row"
          >
            <div class="credential-identity">
              <span class="credential-icon"><KeyRound :size="16" /></span>
              <div>
                <strong>{{ credential.name }}</strong>
                <code>{{
                  canManage
                    ? credential.key
                    : maskedCredentialKey(credential.key)
                }}</code>
              </div>
            </div>
            <div class="credential-meta owner-meta">
              <span>{{ t("openapi.owner") }}</span>
              <b>{{ credential.owner_type || "system" }}</b>
              <small>{{ shortID(credential.owner_id || undefined) }}</small>
            </div>
            <div class="credential-meta permission-list">
              <span>{{ t("openapi.permissions") }}</span>
              <div>
                <i
                  v-for="permission in credential.permissions.split(',')"
                  :key="permission"
                  >{{ permission.trim() }}</i
                >
              </div>
            </div>
            <div class="credential-meta last-used-meta">
              <span>{{ t("openapi.lastUsed") }}</span>
              <b>{{ formatDate(credential.last_used_at) }}</b>
              <small>{{
                t("openapi.appliedAt", {
                  time: formatDate(credential.created_at),
                })
              }}</small>
            </div>
            <div class="credential-actions">
              <span :class="['api-status', credential.status]">
                <i></i>{{ statusLabel(credential.status) }}
              </span>
              <div v-if="canManage && credential.status !== 'revoked'">
                <button
                  v-if="credential.status !== 'active'"
                  :disabled="updatingCredential === credential.id"
                  :title="t('openapi.enableTitle')"
                  @click="setCredentialStatus(credential, 'active')"
                >
                  <CheckCircle2 :size="14" />{{ t("openapi.enable") }}
                </button>
                <button
                  v-if="credential.status !== 'suspended'"
                  :disabled="updatingCredential === credential.id"
                  :title="t('openapi.suspendTitle')"
                  @click="setCredentialStatus(credential, 'suspended')"
                >
                  <PauseCircle :size="14" />{{ t("openapi.suspend") }}
                </button>
                <button
                  v-if="credential.status !== 'pending'"
                  :disabled="updatingCredential === credential.id"
                  :title="t('openapi.pendingTitle')"
                  @click="setCredentialStatus(credential, 'pending')"
                >
                  <Clock3 :size="14" />{{ t("openapi.pending") }}
                </button>
              </div>
            </div>
          </article>
        </div>
      </template>

      <template v-else-if="tab === 'docs'">
        <div class="api-docs-intro">
          <span class="kicker">BASE URL</span>
          <div>
            <code>{{ openAPIBaseURL }}</code>
            <button @click="copy(openAPIBaseURL, 'base')">
              <Check v-if="copied === 'base'" :size="15" />
              <Copy v-else :size="15" />
              {{ copied === "base" ? t("openapi.copied") : t("openapi.copy") }}
            </button>
          </div>
          <p>{{ t("openapi.baseUrlHint") }}</p>
          <p class="docs-environment-note">
            <ShieldCheck :size="13" />
            {{ t("openapi.docEnvironmentHint") }}
          </p>
        </div>

        <div class="endpoint-section">
          <header>
            <div>
              <h3>{{ t("openapi.endpoints") }}</h3>
              <p>{{ t("openapi.endpointsSub") }}</p>
            </div>
            <code>LINLINQI OPENAPI V1</code>
          </header>
          <div class="api-endpoint-list">
            <article v-for="endpoint in endpoints" :key="endpoint.path">
              <span :class="endpoint.method.toLowerCase()">
                {{ endpoint.method }}
              </span>
              <code>{{ endpoint.path }}</code>
              <p>{{ endpoint.description }}</p>
              <b>{{ endpoint.permission }}</b>
            </article>
          </div>
        </div>

        <div class="endpoint-section signature-docs">
          <header>
            <div>
              <h3>{{ t("openapi.signature") }}</h3>
              <p>{{ t("openapi.signatureSub") }}</p>
            </div>
          </header>
          <div class="canonical-line">
            <span>Canonical String</span>
            <code
              >{timestamp}.{nonce}.{METHOD}.{path}.{sha256_hex(raw_body)}</code
            >
          </div>
          <ol class="doc-rule-list">
            <li>
              <b>01</b><span>{{ t("openapi.docSignatureTimestamp") }}</span>
            </li>
            <li>
              <b>02</b><span>{{ t("openapi.docSignatureNonce") }}</span>
            </li>
            <li>
              <b>03</b><span>{{ t("openapi.docSignaturePath") }}</span>
            </li>
            <li>
              <b>04</b><span>{{ t("openapi.docSignatureBody") }}</span>
            </li>
            <li>
              <b>05</b><span>{{ t("openapi.docSignatureOutput") }}</span>
            </li>
          </ol>
          <div class="code-block">
            <div>
              <i></i><i></i><i></i><span>{{ t("openapi.curlTitle") }}</span>
              <button @click="copy(curl, 'curl')">
                <Check v-if="copied === 'curl'" :size="14" />
                <Copy v-else :size="14" />
                {{
                  copied === "curl" ? t("openapi.copied") : t("openapi.copy")
                }}
              </button>
            </div>
            <pre>{{ curl }}</pre>
          </div>
          <p class="doc-callout warning">
            {{ t("openapi.docNonceRetryWarning") }}
          </p>
        </div>

        <div class="endpoint-section doc-contract-section">
          <header>
            <div>
              <h3>{{ t("openapi.docCatalogTitle") }}</h3>
              <p>{{ t("openapi.docCatalogSub") }}</p>
            </div>
            <code>GET {{ productsURL }}</code>
          </header>
          <div class="doc-copy-grid">
            <div class="doc-prose-card">
              <h4>{{ t("openapi.docInputFieldsTitle") }}</h4>
              <ul>
                <li>{{ t("openapi.docInputFieldsSource") }}</li>
                <li>{{ t("openapi.docInputFieldsTypes") }}</li>
                <li>{{ t("openapi.docInputFieldsValidation") }}</li>
                <li>{{ t("openapi.docInputFieldsSensitive") }}</li>
                <li>{{ t("openapi.docVariantIdentity") }}</li>
              </ul>
            </div>
            <div class="code-block doc-code-block">
              <div>
                <i></i><i></i><i></i
                ><span>{{ t("openapi.docCatalogResponse") }}</span>
                <button @click="copy(catalogExample, 'catalog')">
                  <Check v-if="copied === 'catalog'" :size="14" />
                  <Copy v-else :size="14" />
                  {{
                    copied === "catalog"
                      ? t("openapi.copied")
                      : t("openapi.copy")
                  }}
                </button>
              </div>
              <pre>{{ catalogExample }}</pre>
            </div>
          </div>
        </div>

        <div class="endpoint-section doc-contract-section">
          <header>
            <div>
              <h3>{{ t("openapi.docCreateTitle") }}</h3>
              <p>{{ t("openapi.docCreateSub") }}</p>
            </div>
            <code>POST /orders</code>
          </header>
          <div
            class="doc-mode-tabs"
            role="tablist"
            :aria-label="t('openapi.docInputModeAria')"
          >
            <button
              role="tab"
              :aria-selected="orderInputMode === 'parameters'"
              :class="{ active: orderInputMode === 'parameters' }"
              @click="orderInputMode = 'parameters'"
            >
              parameters · key
            </button>
            <button
              role="tab"
              :aria-selected="orderInputMode === 'input_values'"
              :class="{ active: orderInputMode === 'input_values' }"
              @click="orderInputMode = 'input_values'"
            >
              input_values · field_id
            </button>
          </div>
          <div class="doc-copy-grid order-doc-grid">
            <div class="code-block doc-code-block">
              <div>
                <i></i><i></i><i></i
                ><span>{{ t("openapi.docRequestBody") }}</span>
                <button @click="copy(selectedOrderExample, 'order-body')">
                  <Check v-if="copied === 'order-body'" :size="14" />
                  <Copy v-else :size="14" />
                  {{
                    copied === "order-body"
                      ? t("openapi.copied")
                      : t("openapi.copy")
                  }}
                </button>
              </div>
              <pre>{{ selectedOrderExample }}</pre>
            </div>
            <div class="doc-prose-card">
              <h4>{{ t("openapi.docOrderRulesTitle") }}</h4>
              <ul>
                <li>{{ t("openapi.docOrderProduct") }}</li>
                <li>{{ t("openapi.docOrderRequired") }}</li>
                <li>{{ t("openapi.docOrderInputs") }}</li>
                <li>{{ t("openapi.docOrderCallback") }}</li>
                <li>{{ t("openapi.docOrderStrict") }}</li>
              </ul>
            </div>
          </div>
          <div class="code-block doc-code-block create-order-shell">
            <div>
              <i></i><i></i><i></i><span>{{ t("openapi.docCreateCurl") }}</span>
              <button @click="copy(createOrderCurl, 'create-curl')">
                <Check v-if="copied === 'create-curl'" :size="14" />
                <Copy v-else :size="14" />
                {{
                  copied === "create-curl"
                    ? t("openapi.copied")
                    : t("openapi.copy")
                }}
              </button>
            </div>
            <pre>{{ createOrderCurl }}</pre>
          </div>
          <p class="doc-callout">
            {{ t("openapi.docInputModeRule") }}
          </p>
        </div>

        <div class="endpoint-section doc-contract-section">
          <header>
            <div>
              <h3>{{ t("openapi.docIdempotencyTitle") }}</h3>
              <p>{{ t("openapi.docIdempotencySub") }}</p>
            </div>
            <code>201 / 200</code>
          </header>
          <div class="doc-copy-grid">
            <div class="doc-prose-card">
              <h4>{{ t("openapi.docIdempotencyRulesTitle") }}</h4>
              <ul>
                <li>{{ t("openapi.docIdempotencyExact") }}</li>
                <li>{{ t("openapi.docIdempotencyConflict") }}</li>
                <li>{{ t("openapi.docIdempotencyTransport") }}</li>
                <li>{{ t("openapi.docDeliveryStatus") }}</li>
                <li>{{ t("openapi.docMoneyUnit") }}</li>
              </ul>
            </div>
            <div class="code-block doc-code-block">
              <div>
                <i></i><i></i><i></i
                ><span>{{ t("openapi.docOrderResponse") }}</span>
                <button @click="copy(orderResponseExample, 'order-response')">
                  <Check v-if="copied === 'order-response'" :size="14" />
                  <Copy v-else :size="14" />
                  {{
                    copied === "order-response"
                      ? t("openapi.copied")
                      : t("openapi.copy")
                  }}
                </button>
              </div>
              <pre>{{ orderResponseExample }}</pre>
            </div>
          </div>
        </div>

        <div class="endpoint-section doc-contract-section">
          <header>
            <div>
              <h3>{{ t("openapi.docCallbackTitle") }}</h3>
              <p>{{ t("openapi.docCallbackSub") }}</p>
            </div>
            <code>order.delivered</code>
          </header>
          <div class="canonical-line callback-canonical">
            <span>Callback Signature</span>
            <code>HMAC-SHA256(api_secret, timestamp + "." + raw_body)</code>
          </div>
          <div class="doc-copy-grid callback-doc-grid">
            <div class="code-block doc-code-block">
              <div>
                <i></i><i></i><i></i
                ><span>{{ t("openapi.docCallbackBody") }}</span>
                <button @click="copy(callbackExample, 'callback')">
                  <Check v-if="copied === 'callback'" :size="14" />
                  <Copy v-else :size="14" />
                  {{
                    copied === "callback"
                      ? t("openapi.copied")
                      : t("openapi.copy")
                  }}
                </button>
              </div>
              <pre>{{ callbackExample }}</pre>
            </div>
            <div class="doc-prose-card">
              <h4>{{ t("openapi.docCallbackRulesTitle") }}</h4>
              <ul>
                <li>{{ t("openapi.docCallbackHeaders") }}</li>
                <li>{{ t("openapi.docCallbackVerify") }}</li>
                <li>{{ t("openapi.docCallbackIdempotent") }}</li>
                <li>{{ t("openapi.docCallbackAck") }}</li>
                <li>{{ t("openapi.docCallbackRetry") }}</li>
                <li>{{ t("openapi.docCallbackFallback") }}</li>
              </ul>
            </div>
          </div>
        </div>

        <div class="endpoint-section doc-contract-section">
          <header>
            <div>
              <h3>{{ t("openapi.docErrorsTitle") }}</h3>
              <p>{{ t("openapi.docErrorsSub") }}</p>
            </div>
            <code>{ code, message, data? }</code>
          </header>
          <div class="doc-error-layout">
            <div class="api-table-wrap">
              <table class="api-table doc-error-table">
                <thead>
                  <tr>
                    <th>HTTP / code</th>
                    <th>{{ t("openapi.docErrorMeaning") }}</th>
                    <th>{{ t("openapi.docErrorAction") }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td><code>401 / 40120–40123</code></td>
                    <td>{{ t("openapi.docErrorAuth") }}</td>
                    <td>{{ t("openapi.docErrorAuthAction") }}</td>
                  </tr>
                  <tr>
                    <td><code>402 / 40201</code></td>
                    <td>{{ t("openapi.docErrorBalance") }}</td>
                    <td>{{ t("openapi.docErrorBalanceAction") }}</td>
                  </tr>
                  <tr>
                    <td><code>403 / 40320</code></td>
                    <td>{{ t("openapi.docErrorPermission") }}</td>
                    <td>{{ t("openapi.docErrorPermissionAction") }}</td>
                  </tr>
                  <tr>
                    <td><code>404 / 40420</code></td>
                    <td>{{ t("openapi.docErrorNotFound") }}</td>
                    <td>{{ t("openapi.docErrorNotFoundAction") }}</td>
                  </tr>
                  <tr>
                    <td><code>409 / 40901</code></td>
                    <td>{{ t("openapi.docErrorStock") }}</td>
                    <td>{{ t("openapi.docErrorStockAction") }}</td>
                  </tr>
                  <tr>
                    <td><code>409 / 40920</code></td>
                    <td>{{ t("openapi.docErrorNonce") }}</td>
                    <td>{{ t("openapi.docErrorNonceAction") }}</td>
                  </tr>
                  <tr>
                    <td><code>409 / 40921</code></td>
                    <td>{{ t("openapi.docErrorIdempotency") }}</td>
                    <td>{{ t("openapi.docErrorIdempotencyAction") }}</td>
                  </tr>
                  <tr>
                    <td><code>422 / 42220–42222</code></td>
                    <td>{{ t("openapi.docErrorValidation") }}</td>
                    <td>{{ t("openapi.docErrorValidationAction") }}</td>
                  </tr>
                  <tr>
                    <td><code>503 / 50320</code></td>
                    <td>{{ t("openapi.docErrorReplayService") }}</td>
                    <td>{{ t("openapi.docErrorRetryAction") }}</td>
                  </tr>
                  <tr>
                    <td><code>500 / 50020–50021</code></td>
                    <td>{{ t("openapi.docErrorServer") }}</td>
                    <td>{{ t("openapi.docErrorRetryAction") }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div class="code-block doc-code-block compact-error-code">
              <div>
                <i></i><i></i><i></i
                ><span>{{ t("openapi.docErrorEnvelope") }}</span>
                <button @click="copy(errorExample, 'error')">
                  <Check v-if="copied === 'error'" :size="14" />
                  <Copy v-else :size="14" />
                  {{
                    copied === "error" ? t("openapi.copied") : t("openapi.copy")
                  }}
                </button>
              </div>
              <pre>{{ errorExample }}</pre>
            </div>
          </div>
          <p class="doc-callout warning">
            {{ t("openapi.docRetryPolicy") }}
          </p>
        </div>

        <div class="api-features">
          <article>
            <KeyRound :size="19" />
            <div>
              <b>{{ t("openapi.auth") }}</b
              ><span>{{ t("openapi.authDesc") }}</span>
            </div>
          </article>
          <article>
            <RotateCw :size="19" />
            <div>
              <b>{{ t("openapi.idempotency") }}</b
              ><span>{{ t("openapi.idempotencyDesc") }}</span>
            </div>
          </article>
          <article>
            <Webhook :size="19" />
            <div>
              <b>{{ t("openapi.asyncDelivery") }}</b
              ><span>{{ t("openapi.asyncDeliveryDesc") }}</span>
            </div>
          </article>
        </div>
      </template>

      <template v-else>
        <div class="api-section-head">
          <div>
            <h3>{{ t("openapi.logsTitle") }}</h3>
            <p>{{ t("openapi.logsSub", { n: logTotal }) }}</p>
          </div>
          <button :disabled="loadingLogs" @click="loadLogs(logPage)">
            <RefreshCw :size="14" :class="{ spinning: loadingLogs }" />{{
              t("openapi.refresh")
            }}
          </button>
        </div>
        <div class="api-log-summary">
          <div>
            <span>{{ t("openapi.totalCalls") }}</span
            ><strong>{{ logTotal }}</strong>
          </div>
          <div>
            <span>{{ t("openapi.avgLatency") }}</span
            ><strong>{{ sampleAverageLatency }} ms</strong>
          </div>
          <div>
            <span>{{ t("openapi.errorRate") }}</span>
            <strong>{{ sampleErrorRate.toFixed(1) }}%</strong>
          </div>
        </div>
        <div class="api-table-wrap">
          <table class="api-table">
            <thead>
              <tr>
                <th>{{ t("openapi.time") }}</th>
                <th>{{ t("openapi.credential") }}</th>
                <th>{{ t("openapi.request") }}</th>
                <th>{{ t("openapi.status") }}</th>
                <th>{{ t("openapi.duration") }}</th>
                <th>{{ t("openapi.requestIdIp") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="loadingLogs && !logs.length">
                <td colspan="6" class="api-empty">
                  {{ t("openapi.loadingLogs") }}
                </td>
              </tr>
              <tr v-else-if="!logs.length">
                <td colspan="6" class="api-empty">{{ t("openapi.noLogs") }}</td>
              </tr>
              <tr v-for="log in logs" v-else :key="log.id">
                <td>{{ formatDate(log.created_at) }}</td>
                <td>
                  <b>{{ credentialName(log.credential_id) }}</b>
                  <small>{{ shortID(log.credential_id) }}</small>
                </td>
                <td>
                  <span :class="['method-badge', log.method.toLowerCase()]">
                    {{ log.method }}
                  </span>
                  <code>{{ log.path }}</code>
                </td>
                <td>
                  <span
                    :class="[
                      'http-status',
                      log.status_code < 400 ? 'good' : 'bad',
                    ]"
                    >{{ log.status_code }}</span
                  >
                </td>
                <td>{{ log.duration_ms }} ms</td>
                <td>
                  <code>{{ log.request_id || "—" }}</code>
                  <small>{{ log.ip || "—" }}</small>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="pagination api-pagination">
          <span>{{
            t("openapi.page", { page: logPage, total: logPageCount })
          }}</span>
          <div>
            <button
              :disabled="logPage <= 1 || loadingLogs"
              @click="changeLogPage(logPage - 1)"
            >
              {{ t("openapi.prev") }}
            </button>
            <button
              :disabled="logPage >= logPageCount || loadingLogs"
              @click="changeLogPage(logPage + 1)"
            >
              {{ t("openapi.next") }}
            </button>
          </div>
        </div>
      </template>
    </section>

    <aside class="api-side">
      <article class="panel">
        <header>
          <h3>{{ t("openapi.liveStatus") }}</h3>
          <span :class="['healthy', `api-status-${apiReachability}`]"
            ><i></i>
            {{
              apiReachability === "checking"
                ? t("openapi.loading")
                : apiReachability === "online"
                  ? t("openapi.apiConnected")
                  : t("openapi.errLoad")
            }}</span
          >
        </header>
        <div class="api-health">
          <strong>{{ activeCredentialCount }}</strong
          ><span>{{ t("openapi.activeCredentials") }}</span>
        </div>
        <div class="mini-metrics">
          <div>
            <span>{{ t("openapi.totalLogs") }}</span
            ><b>{{ logTotal }}</b>
          </div>
          <div>
            <span>{{ t("openapi.sampleLatency") }}</span
            ><b>{{ sampleAverageLatency }}ms</b>
          </div>
          <div>
            <span>{{ t("openapi.sampleErrors") }}</span
            ><b>{{ sampleErrorRate.toFixed(1) }}%</b>
          </div>
        </div>
      </article>
      <article class="panel">
        <header>
          <h3>{{ t("openapi.signatureParts") }}</h3>
        </header>
        <div class="request-list signature-list">
          <div>
            <span>01</span><code>Unix timestamp</code
            ><b>{{ t("openapi.timeWindow") }}</b>
          </div>
          <div>
            <span>02</span><code>Unique nonce</code
            ><b>{{ t("openapi.replayProtect") }}</b>
          </div>
          <div>
            <span>03</span><code>Method + path + body SHA</code
            ><b>{{ t("openapi.canonical") }}</b>
          </div>
          <div>
            <span>04</span><code>HMAC-SHA256</code
            ><b>{{ t("openapi.constCompare") }}</b>
          </div>
        </div>
      </article>
      <article class="panel api-policy-card">
        <header>
          <h3>{{ t("openapi.securityPolicy") }}</h3>
        </header>
        <p>{{ t("openapi.policyDesc") }}</p>
        <ul>
          <li><CheckCircle2 :size="13" /> {{ t("openapi.aesStorage") }}</li>
          <li><CheckCircle2 :size="13" /> {{ t("openapi.endpointPerms") }}</li>
          <li>
            <CheckCircle2 :size="13" /> {{ t("openapi.auditStatusAction") }}
          </li>
        </ul>
      </article>
    </aside>
  </div>
</template>

<style scoped>
.docs-environment-note {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--text) !important;
}

.doc-rule-list {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 10px 0 0;
  padding: 0;
  list-style: none;
}

.doc-rule-list li {
  min-height: 46px;
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface-2);
}

.doc-rule-list b {
  flex: 0 0 auto;
  font:
    700 8px ui-monospace,
    SFMono-Regular,
    Menlo,
    Monaco,
    Consolas,
    monospace;
  color: var(--muted);
}

.doc-rule-list span,
.doc-callout,
.doc-prose-card li {
  font-size: 9px;
  line-height: 1.65;
}

.doc-copy-grid {
  display: grid;
  grid-template-columns: minmax(240px, 0.75fr) minmax(0, 1.25fr);
  gap: 12px;
  align-items: stretch;
}

.order-doc-grid {
  grid-template-columns: minmax(0, 1.2fr) minmax(240px, 0.8fr);
}

.doc-prose-card {
  padding: 15px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-2);
}

.doc-prose-card h4 {
  margin: 0 0 10px;
  font-size: 10px;
}

.doc-prose-card ul {
  display: grid;
  gap: 7px;
  margin: 0;
  padding-left: 17px;
  color: var(--muted);
}

.doc-code-block {
  min-width: 0;
  margin-top: 0;
}

.doc-code-block pre {
  max-height: 430px;
  font-size: 9px;
  line-height: 1.65;
}

.create-order-shell {
  margin-top: 12px;
}

.doc-mode-tabs {
  display: inline-flex;
  gap: 4px;
  margin: 0 0 12px;
  padding: 3px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-2);
}

.doc-mode-tabs button {
  padding: 7px 10px;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--muted);
  font:
    8px ui-monospace,
    SFMono-Regular,
    Menlo,
    Monaco,
    Consolas,
    monospace;
}

.doc-mode-tabs button.active {
  background: var(--text);
  color: var(--surface);
}

.doc-callout {
  margin: 12px 0 0;
  padding: 10px 12px;
  border-left: 3px solid var(--text);
  border-radius: 0 5px 5px 0;
  background: var(--surface-2);
}

.doc-callout.warning {
  border-left-color: var(--warn);
}

.callback-canonical {
  margin-bottom: 12px;
}

.callback-doc-grid {
  grid-template-columns: minmax(0, 1.15fr) minmax(250px, 0.85fr);
}

.doc-error-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px;
}

.doc-error-table {
  min-width: 680px;
}

.doc-error-table td {
  vertical-align: top;
  line-height: 1.55;
}

.doc-error-table td:first-child {
  white-space: nowrap;
}

.compact-error-code pre {
  max-height: none;
}

@media (max-width: 960px) {
  .doc-copy-grid,
  .order-doc-grid,
  .callback-doc-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 560px) {
  .doc-rule-list {
    grid-template-columns: 1fr;
  }

  .doc-mode-tabs {
    width: 100%;
    flex-direction: column;
  }

  .doc-mode-tabs button {
    text-align: left;
  }

  .doc-contract-section {
    padding-right: 12px;
    padding-left: 12px;
  }
}
</style>
