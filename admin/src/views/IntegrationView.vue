<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  BellRing,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  CircleAlert,
  Edit3,
  FlaskConical,
  LoaderCircle,
  Plus,
  RefreshCw,
  RotateCcw,
  Search,
  Send,
  ShieldCheck,
  Trash2,
  Webhook,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";

type Tab =
  "endpoints" | "webhook-deliveries" | "templates" | "notification-deliveries";
type PagePayload<T> = {
  items: T[];
  total: number;
  page: number;
  page_size: number;
};

interface IntegrationSummary {
  webhook_endpoints_active: number;
  webhook_endpoints_disabled: number;
  webhook_deliveries_pending: number;
  webhook_deliveries_failed: number;
  webhook_delivered_24_hours: number;
  notification_templates_enabled: number;
  notification_deliveries_pending: number;
  notification_deliveries_failed: number;
  notification_sent_24_hours: number;
}

interface WebhookEndpointItem {
  id: string;
  owner_type: string;
  owner_id: string;
  url: string;
  events: string[];
  enabled: boolean;
  failure_count: number;
  credentials_configured: boolean;
  disabled_at?: string | null;
  created_at: string;
  updated_at: string;
}

interface WebhookDeliveryItem {
  id: string;
  endpoint_id: string;
  endpoint_url: string;
  event_id: string;
  event_type: string;
  status: string;
  attempts: number;
  response_code: number;
  diagnostic: string;
  next_attempt_at?: string | null;
  delivered_at?: string | null;
  endpoint_enabled: boolean;
  created_at: string;
  updated_at: string;
}

interface NotificationTemplateItem {
  id: string;
  code: string;
  name: string;
  audience: "admin" | "user";
  channel: string;
  locale: string;
  subject: string;
  body: string;
  variables: string[];
  enabled: boolean;
  version: number;
  created_at: string;
  updated_at: string;
}

interface NotificationDeliveryItem {
  id: string;
  idempotency_key: string;
  template_id?: string | null;
  template_code: string;
  template_name: string;
  channel: string;
  recipient: string;
  subject: string;
  status: string;
  attempts: number;
  diagnostic: string;
  next_attempt_at?: string | null;
  sent_at?: string | null;
  created_at: string;
  updated_at: string;
}

const { t } = useI18n();
const route = useRoute();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("system.manage"));

function defaultRouteTab(): Tab {
  const candidate = String(route.meta.defaultTab || "");
  if (
    candidate === "endpoints" ||
    candidate === "webhook-deliveries" ||
    candidate === "templates" ||
    candidate === "notification-deliveries"
  ) {
    return candidate;
  }
  return "endpoints";
}

const activeTab = ref<Tab>(defaultRouteTab());
const summary = ref<IntegrationSummary | null>(null);
const items = ref<
  Array<
    | WebhookEndpointItem
    | WebhookDeliveryItem
    | NotificationTemplateItem
    | NotificationDeliveryItem
  >
>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const loading = ref(false);
const summaryLoading = ref(false);
const loadError = ref("");
const notice = ref("");
const searchInput = ref("");
const appliedSearch = ref("");
const statusFilter = ref("");
const channelFilter = ref("");
const ownerTypeFilter = ref("");
const eventFilter = ref("");
const localeFilter = ref("");
let requestSequence = 0;

const modal = ref<"template" | "test" | "action" | null>(null);
const saving = ref(false);
const modalError = ref("");
const editingTemplate = ref<NotificationTemplateItem | null>(null);
const testingTemplate = ref<NotificationTemplateItem | null>(null);
const actionTarget = ref<
  | WebhookEndpointItem
  | WebhookDeliveryItem
  | NotificationTemplateItem
  | NotificationDeliveryItem
  | null
>(null);
const actionType = ref<
  "endpoint" | "webhook-retry" | "notification-retry" | "template-delete"
>("endpoint");
const actionReason = ref("");

const templateForm = reactive({
  code: "",
  name: "",
  audience: "admin" as "admin" | "user",
  channel: "email",
  locale: "zh-CN",
  subject: "",
  body: "",
  variables: "",
  enabled: true,
  reason: "",
});

const testForm = reactive({
  recipient: "",
  idempotencyKey: "",
  values: {} as Record<string, string>,
  reason: "",
});

const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
function apiMessage(error: unknown, fallback: string) {
  const response = error as { response?: { data?: { message?: string } } };
  const serverMessage = response.response?.data?.message;
  return serverMessage && !serverMessage.startsWith("error.")
    ? serverMessage
    : fallback;
}

function validReason(value: string) {
  const length = [...value.trim()].length;
  return length >= 4 && length <= 500;
}

function unwrapPage<T>(value: unknown): PagePayload<T> {
  const source = (value || {}) as Partial<PagePayload<T>>;
  return {
    items: Array.isArray(source.items) ? source.items : [],
    total: Number(source.total || 0),
    page: Number(source.page || page.value),
    page_size: Number(source.page_size || pageSize.value),
  };
}

function shortID(value?: string | null) {
  return value ? `${value.slice(0, 8)}…${value.slice(-4)}` : "—";
}

function date(value?: string | null) {
  if (!value) return "—";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime())
    ? "—"
    : parsed.toLocaleString("zh-CN", { hour12: false });
}

function statusLabel(value: string) {
  const labels: Record<string, string> = {
    queued: t("integration.statusQueued"),
    sending: t("integration.statusSending"),
    retrying: t("integration.statusRetrying"),
    delivered: t("integration.statusDelivered"),
    sent: t("integration.statusSent"),
    failed: t("integration.statusFailed"),
  };
  return labels[value] || value;
}

function channelLabel(value: string) {
  return (
    (
      {
        email: t("integration.channelEmail"),
        telegram: t("integration.channelTelegram"),
        wecom: t("integration.channelWecom"),
        admin: t("integration.channelAdmin"),
      } as Record<string, string>
    )[value] || value
  );
}

function resetFilters() {
  searchInput.value = "";
  appliedSearch.value = "";
  statusFilter.value = "";
  channelFilter.value = "";
  ownerTypeFilter.value = "";
  eventFilter.value = "";
  localeFilter.value = "";
  page.value = 1;
}

async function loadSummary() {
  summaryLoading.value = true;
  try {
    const { data } = await adminApi.get("/integrations/summary");
    summary.value = data.data as IntegrationSummary;
  } catch (error) {
    if (!loadError.value)
      loadError.value = apiMessage(error, t("integration.errLoadSummary"));
  } finally {
    summaryLoading.value = false;
  }
}

function listRequest() {
  const params: Record<string, string | number | boolean> = {
    page: page.value,
    page_size: pageSize.value,
  };
  if (appliedSearch.value) params.q = appliedSearch.value;
  let endpoint = "/webhooks/endpoints";
  if (activeTab.value === "endpoints") {
    if (statusFilter.value) params.enabled = statusFilter.value === "enabled";
    if (ownerTypeFilter.value) params.owner_type = ownerTypeFilter.value;
    if (eventFilter.value) params.event_type = eventFilter.value;
  } else if (activeTab.value === "webhook-deliveries") {
    endpoint = "/webhooks/deliveries";
    if (statusFilter.value) params.status = statusFilter.value;
    if (eventFilter.value) params.event_type = eventFilter.value;
  } else if (activeTab.value === "templates") {
    endpoint = "/notifications/templates";
    if (statusFilter.value) params.enabled = statusFilter.value === "enabled";
    if (channelFilter.value) params.channel = channelFilter.value;
    if (localeFilter.value) params.locale = localeFilter.value;
  } else {
    endpoint = "/notifications/deliveries";
    if (statusFilter.value) params.status = statusFilter.value;
    if (channelFilter.value) params.channel = channelFilter.value;
  }
  return { endpoint, params };
}

async function loadList() {
  const sequence = ++requestSequence;
  loading.value = true;
  loadError.value = "";
  try {
    const request = listRequest();
    const { data } = await adminApi.get(request.endpoint, {
      params: request.params,
    });
    if (sequence !== requestSequence) return;
    const payload = unwrapPage<
      | WebhookEndpointItem
      | WebhookDeliveryItem
      | NotificationTemplateItem
      | NotificationDeliveryItem
    >(data.data);
    items.value = payload.items;
    total.value = payload.total;
    page.value = payload.page;
    pageSize.value = payload.page_size;
    if (page.value > totalPages.value && page.value > 1) {
      page.value = totalPages.value;
      await loadList();
    }
  } catch (error) {
    if (sequence !== requestSequence) return;
    items.value = [];
    total.value = 0;
    loadError.value = apiMessage(error, t("integration.errLoad"));
  } finally {
    if (sequence === requestSequence) loading.value = false;
  }
}

async function refreshAll() {
  notice.value = "";
  await Promise.all([loadSummary(), loadList()]);
}

async function applySearch() {
  appliedSearch.value = searchInput.value.trim();
  page.value = 1;
  await loadList();
}

async function changePage(target: number) {
  if (target < 1 || target > totalPages.value || target === page.value) return;
  page.value = target;
  await loadList();
}

function clearTemplateForm() {
  Object.assign(templateForm, {
    code: "",
    name: "",
    audience: "admin",
    channel: "email",
    locale: "zh-CN",
    subject: "",
    body: "",
    variables: "",
    enabled: true,
    reason: "",
  });
}

function resetModalState() {
  modal.value = null;
  modalError.value = "";
  editingTemplate.value = null;
  testingTemplate.value = null;
  actionTarget.value = null;
  actionReason.value = "";
  testForm.recipient = "";
  testForm.idempotencyKey = "";
  testForm.reason = "";
  testForm.values = {};
  clearTemplateForm();
}

function closeModal() {
  if (!saving.value) resetModalState();
}

function openTemplate(item?: NotificationTemplateItem) {
  if (!canManage.value) return;
  editingTemplate.value = item || null;
  clearTemplateForm();
  if (item) {
    Object.assign(templateForm, {
      code: item.code,
      name: item.name,
      audience: item.audience || "admin",
      channel: item.channel,
      locale: item.locale,
      subject: item.subject,
      body: item.body,
      variables: item.variables.join(", "),
      enabled: item.enabled,
    });
  }
  modalError.value = "";
  modal.value = "template";
}

function parsedVariables() {
  return [
    ...new Set(
      templateForm.variables
        .split(/[\s,，]+/)
        .map((value) => value.trim().toLowerCase())
        .filter(Boolean),
    ),
  ].sort();
}

function placeholderVariables() {
  const found = `${templateForm.subject}\n${templateForm.body}`.matchAll(
    /{{([a-z][a-z0-9_]*)}}/g,
  );
  return [...new Set([...found].map((entry) => entry[1]))].sort();
}

async function saveTemplate() {
  if (!canManage.value) return;
  modalError.value = "";
  const variables = parsedVariables();
  if (
    !/^[a-z][a-z0-9_.-]{2,99}$/.test(templateForm.code.trim().toLowerCase())
  ) {
    modalError.value = t("integration.errTemplateCode");
    return;
  }
  if (templateForm.name.trim().length < 2) {
    modalError.value = t("integration.errTemplateName");
    return;
  }
  if (!validReason(templateForm.reason)) {
    modalError.value = t("integration.errReason");
    return;
  }
  if (!templateForm.subject.trim() || !templateForm.body.trim()) {
    modalError.value = t("integration.errTemplateEmpty");
    return;
  }
  if (JSON.stringify(variables) !== JSON.stringify(placeholderVariables())) {
    modalError.value = t("integration.errTemplateVariables");
    return;
  }
  saving.value = true;
  try {
    const payload = {
      code: templateForm.code.trim().toLowerCase(),
      name: templateForm.name.trim(),
      audience: templateForm.audience,
      channel: templateForm.channel,
      locale: templateForm.locale.trim(),
      subject: templateForm.subject.trim(),
      body: templateForm.body.trim(),
      variables,
      enabled: templateForm.enabled,
    };
    const options = {
      headers: { "X-Change-Reason": templateForm.reason.trim() },
    };
    if (editingTemplate.value)
      await adminApi.put(
        `/notifications/templates/${editingTemplate.value.id}`,
        payload,
        options,
      );
    else await adminApi.post("/notifications/templates", payload, options);
    notice.value = editingTemplate.value
      ? t("integration.noticeTemplateUpdated")
      : t("integration.noticeTemplateCreated");
    resetModalState();
    await refreshAll();
  } catch (error) {
    modalError.value = apiMessage(error, t("integration.errSaveTemplate"));
  } finally {
    saving.value = false;
  }
}

function randomKey() {
  const bytes = new Uint8Array(12);
  crypto.getRandomValues(bytes);
  return `test_${[...bytes].map((value) => value.toString(16).padStart(2, "0")).join("")}`;
}

function openTest(item: NotificationTemplateItem) {
  if (!canManage.value) return;
  testingTemplate.value = item;
  testForm.recipient = item.channel === "admin" ? "admin" : "";
  testForm.idempotencyKey = randomKey();
  testForm.reason = "";
  testForm.values = Object.fromEntries(
    item.variables.map((variable) => [variable, ""]),
  );
  modalError.value = "";
  modal.value = "test";
}

async function sendTest() {
  if (!canManage.value) return;
  if (!testingTemplate.value) return;
  modalError.value = "";
  if (!testForm.recipient.trim() || !validReason(testForm.reason)) {
    modalError.value = t("integration.errTestRecipient");
    return;
  }
  if (!/^[A-Za-z0-9_-]{8,64}$/.test(testForm.idempotencyKey.trim())) {
    modalError.value = t("integration.errTestIdempotency");
    return;
  }
  if (Object.values(testForm.values).some((value) => !value.trim())) {
    modalError.value = t("integration.errTestValues");
    return;
  }
  saving.value = true;
  try {
    const { data } = await adminApi.post(
      `/notifications/templates/${testingTemplate.value.id}/test`,
      {
        recipient: testForm.recipient.trim(),
        variables: Object.fromEntries(
          Object.entries(testForm.values).map(([key, value]) => [
            key,
            value.trim(),
          ]),
        ),
        idempotency_key: testForm.idempotencyKey,
      },
      { headers: { "X-Change-Reason": testForm.reason.trim() } },
    );
    const queueState = data.data?.queue_state || "queued";
    const dedup = data.data?.deduplicated
      ? t("integration.noticeTestDedup")
      : "";
    notice.value = t("integration.noticeTestAccepted", { queueState, dedup });
    resetModalState();
    await refreshAll();
  } catch (error) {
    modalError.value = apiMessage(error, t("integration.errSendTest"));
  } finally {
    saving.value = false;
  }
}

function openAction(
  type: typeof actionType.value,
  target: typeof actionTarget.value,
) {
  if (!canManage.value) return;
  actionType.value = type;
  actionTarget.value = target;
  actionReason.value = "";
  modalError.value = "";
  modal.value = "action";
}

const actionTitle = computed(() => {
  if (actionType.value === "endpoint")
    return (actionTarget.value as WebhookEndpointItem | null)?.enabled
      ? t("integration.actionDisableEndpoint")
      : t("integration.actionEnableEndpoint");
  if (actionType.value === "template-delete")
    return t("integration.actionDeleteTemplate");
  return actionType.value === "webhook-retry"
    ? t("integration.actionRetryWebhook")
    : t("integration.actionRetryNotification");
});

async function submitAction() {
  if (!canManage.value) return;
  if (!actionTarget.value || !validReason(actionReason.value)) {
    modalError.value = t("integration.errReason");
    return;
  }
  saving.value = true;
  modalError.value = "";
  try {
    const options = {
      headers: { "X-Change-Reason": actionReason.value.trim() },
    };
    if (actionType.value === "endpoint") {
      const target = actionTarget.value as WebhookEndpointItem;
      await adminApi.patch(
        `/webhooks/endpoints/${target.id}`,
        { enabled: !target.enabled },
        options,
      );
      notice.value = target.enabled
        ? t("integration.noticeEndpointDisabled")
        : t("integration.noticeEndpointEnabled");
    } else if (actionType.value === "webhook-retry") {
      const { data } = await adminApi.post(
        `/webhooks/deliveries/${actionTarget.value.id}/retry`,
        {},
        options,
      );
      notice.value = t("integration.noticeWebhookRequeued", {
        queueState: data.data?.queue_state || "queued",
      });
    } else if (actionType.value === "notification-retry") {
      const { data } = await adminApi.post(
        `/notifications/deliveries/${actionTarget.value.id}/retry`,
        {},
        options,
      );
      notice.value = t("integration.noticeNotificationRequeued", {
        queueState: data.data?.queue_state || "queued",
      });
    } else {
      await adminApi.delete(
        `/notifications/templates/${actionTarget.value.id}`,
        options,
      );
      notice.value = t("integration.noticeTemplateDeleted");
    }
    resetModalState();
    await refreshAll();
  } catch (error) {
    modalError.value = apiMessage(error, t("integration.errAction"));
  } finally {
    saving.value = false;
  }
}

watch(
  () => [route.path, route.meta.defaultTab] as const,
  async () => {
    activeTab.value = defaultRouteTab();
    resetFilters();
    await loadList();
  },
);

onMounted(refreshAll);
onBeforeUnmount(resetModalState);
</script>

<template>
  <section class="integration-console">
    <div class="integration-summary">
      <article>
        <Webhook :size="18" />
        <span>{{ t("integration.summaryEndpointsEnabled") }}</span>
        <strong>{{
          summaryLoading ? "…" : (summary?.webhook_endpoints_active ?? 0)
        }}</strong>
        <small>{{
          t("integration.summaryDisabledCount", {
            count: summary?.webhook_endpoints_disabled ?? 0,
          })
        }}</small>
      </article>
      <article>
        <Send :size="18" />
        <span>{{ t("integration.summaryWebhook24h") }}</span>
        <strong>{{
          summaryLoading ? "…" : (summary?.webhook_delivered_24_hours ?? 0)
        }}</strong>
        <small
          :class="{ danger: (summary?.webhook_deliveries_failed || 0) > 0 }"
          >{{
            t("integration.summaryFailedCount", {
              count: summary?.webhook_deliveries_failed ?? 0,
            })
          }}</small
        >
      </article>
      <article>
        <BellRing :size="18" />
        <span>{{ t("integration.summaryTemplatesEnabled") }}</span>
        <strong>{{
          summaryLoading ? "…" : (summary?.notification_templates_enabled ?? 0)
        }}</strong>
        <small>{{
          t("integration.summaryPendingCount", {
            count: summary?.notification_deliveries_pending ?? 0,
          })
        }}</small>
      </article>
      <article>
        <CheckCircle2 :size="18" />
        <span>{{ t("integration.summaryNotification24h") }}</span>
        <strong>{{
          summaryLoading ? "…" : (summary?.notification_sent_24_hours ?? 0)
        }}</strong>
        <small
          :class="{
            danger: (summary?.notification_deliveries_failed || 0) > 0,
          }"
          >{{
            t("integration.summaryFailedCount", {
              count: summary?.notification_deliveries_failed ?? 0,
            })
          }}</small
        >
      </article>
    </div>

    <div class="integration-card">
      <div class="integration-toolbar">
        <form @submit.prevent="applySearch">
          <Search :size="15" />
          <input
            v-model="searchInput"
            :placeholder="
              activeTab === 'endpoints'
                ? t('integration.searchPlaceholderEndpoints')
                : activeTab === 'templates'
                  ? t('integration.searchPlaceholderTemplates')
                  : t('integration.searchPlaceholderDeliveries')
            "
          />
        </form>
        <select
          v-if="activeTab === 'endpoints'"
          v-model="ownerTypeFilter"
          @change="
            page = 1;
            loadList();
          "
        >
          <option value="">{{ t("integration.filterAllOwners") }}</option>
          <option value="user">{{ t("integration.ownerUser") }}</option>
          <option value="api_credential">
            {{ t("integration.ownerApiCredential") }}
          </option>
        </select>
        <select
          v-if="
            activeTab === 'templates' || activeTab === 'notification-deliveries'
          "
          v-model="channelFilter"
          @change="
            page = 1;
            loadList();
          "
        >
          <option value="">{{ t("integration.filterAllChannels") }}</option>
          <option value="email">{{ t("integration.channelEmail") }}</option>
          <option value="telegram">
            {{ t("integration.channelTelegram") }}
          </option>
          <option value="wecom">{{ t("integration.channelWecom") }}</option>
          <option value="admin">{{ t("integration.channelAdmin") }}</option>
        </select>
        <input
          v-if="activeTab === 'templates'"
          v-model="localeFilter"
          class="compact-input"
          :placeholder="t('integration.filterLocalePlaceholder')"
          @keyup.enter="
            page = 1;
            loadList();
          "
        />
        <input
          v-if="activeTab === 'endpoints' || activeTab === 'webhook-deliveries'"
          v-model="eventFilter"
          class="compact-input"
          :placeholder="t('integration.filterEventPlaceholder')"
          @keyup.enter="
            page = 1;
            loadList();
          "
        />
        <select
          v-model="statusFilter"
          @change="
            page = 1;
            loadList();
          "
        >
          <option value="">{{ t("integration.filterAllStatuses") }}</option>
          <template
            v-if="activeTab === 'endpoints' || activeTab === 'templates'"
            ><option value="enabled">{{ t("integration.enabled") }}</option>
            <option value="disabled">
              {{ t("integration.disabled") }}
            </option></template
          >
          <template v-else
            ><option value="queued">{{ t("integration.statusQueued") }}</option>
            <option value="sending">
              {{ t("integration.statusSending") }}
            </option>
            <option value="retrying">
              {{ t("integration.statusRetrying") }}
            </option>
            <option
              :value="activeTab === 'webhook-deliveries' ? 'delivered' : 'sent'"
            >
              {{ t("integration.statusSucceeded") }}
            </option>
            <option value="failed">
              {{ t("integration.statusFailed") }}
            </option></template
          >
        </select>
        <button class="tool-button" :disabled="loading" @click="refreshAll">
          <RefreshCw :class="{ spin: loading }" :size="15" />{{
            t("integration.refresh")
          }}
        </button>
        <button
          v-if="activeTab === 'templates' && canManage"
          class="primary-action"
          @click="openTemplate()"
        >
          <Plus :size="15" />{{ t("integration.createTemplate") }}
        </button>
      </div>

      <p v-if="loadError" class="integration-message error">
        <CircleAlert :size="15" />{{ loadError }}
      </p>
      <p v-if="notice" class="integration-message success">
        <ShieldCheck :size="15" />{{ notice }}
      </p>

      <div class="integration-table-wrap">
        <table v-if="activeTab === 'endpoints'">
          <thead>
            <tr>
              <th>{{ t("integration.colEndpoint") }}</th>
              <th>{{ t("integration.colOwner") }}</th>
              <th>{{ t("integration.colEvents") }}</th>
              <th>{{ t("integration.colSignature") }}</th>
              <th>{{ t("integration.colFailures") }}</th>
              <th>{{ t("integration.colStatus") }}</th>
              <th>{{ t("integration.colUpdatedAt") }}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="entry in items as WebhookEndpointItem[]" :key="entry.id">
              <td>
                <strong>{{ entry.url }}</strong
                ><small>{{ shortID(entry.id) }}</small>
              </td>
              <td>
                {{ entry.owner_type
                }}<small>{{ shortID(entry.owner_id) }}</small>
              </td>
              <td>
                <div class="tag-list">
                  <span v-for="event in entry.events" :key="event">{{
                    event
                  }}</span
                  ><em v-if="!entry.events.length">{{
                    t("integration.none")
                  }}</em>
                </div>
              </td>
              <td>
                <span
                  :class="[
                    'state',
                    entry.credentials_configured ? 'ok' : 'bad',
                  ]"
                  >{{
                    entry.credentials_configured
                      ? t("integration.credentialsConfigured")
                      : t("integration.credentialsMissing")
                  }}</span
                >
              </td>
              <td>{{ entry.failure_count }}</td>
              <td>
                <span :class="['state', entry.enabled ? 'ok' : 'muted']">{{
                  entry.enabled
                    ? t("integration.enabled")
                    : t("integration.disabled")
                }}</span>
              </td>
              <td>{{ date(entry.updated_at) }}</td>
              <td>
                <button
                  v-if="canManage"
                  class="row-action"
                  @click="openAction('endpoint', entry)"
                >
                  {{
                    entry.enabled
                      ? t("integration.disabled")
                      : t("integration.enabled")
                  }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>

        <table v-else-if="activeTab === 'webhook-deliveries'">
          <thead>
            <tr>
              <th>{{ t("integration.colEvent") }}</th>
              <th>{{ t("integration.colEndpoint") }}</th>
              <th>{{ t("integration.colStatus") }}</th>
              <th>{{ t("integration.colAttempts") }}</th>
              <th>HTTP</th>
              <th>{{ t("integration.colTime") }}</th>
              <th>{{ t("integration.colDiagnostic") }}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="entry in items as WebhookDeliveryItem[]" :key="entry.id">
              <td>
                <strong>{{ entry.event_type }}</strong
                ><small>{{ shortID(entry.event_id) }}</small>
              </td>
              <td>
                {{ entry.endpoint_url
                }}<small>{{
                  entry.endpoint_enabled
                    ? t("integration.endpointEnabled")
                    : t("integration.endpointDisabled")
                }}</small>
              </td>
              <td>
                <span :class="['state', entry.status]">{{
                  statusLabel(entry.status)
                }}</span>
              </td>
              <td>{{ entry.attempts }}</td>
              <td>{{ entry.response_code || "—" }}</td>
              <td>
                {{
                  date(
                    entry.delivered_at ||
                      entry.next_attempt_at ||
                      entry.created_at,
                  )
                }}
              </td>
              <td class="diagnostic">{{ entry.diagnostic || "—" }}</td>
              <td>
                <button
                  v-if="canManage && entry.status === 'failed'"
                  class="row-action"
                  :disabled="!entry.endpoint_enabled"
                  @click="openAction('webhook-retry', entry)"
                >
                  <RotateCcw :size="13" />{{ t("integration.retry") }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>

        <table v-else-if="activeTab === 'templates'">
          <thead>
            <tr>
              <th>{{ t("integration.colTemplate") }}</th>
              <th>{{ t("integration.audience") }}</th>
              <th>{{ t("integration.colChannelLocale") }}</th>
              <th>{{ t("integration.colSubject") }}</th>
              <th>{{ t("integration.colVariables") }}</th>
              <th>{{ t("integration.colVersion") }}</th>
              <th>{{ t("integration.colStatus") }}</th>
              <th>{{ t("integration.colUpdatedAt") }}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="entry in items as NotificationTemplateItem[]"
              :key="entry.id"
            >
              <td>
                <strong>{{ entry.name }}</strong
                ><small>{{ entry.code }}</small>
              </td>
              <td>{{ t(`integration.audienceName.${entry.audience}`) }}</td>
              <td>
                {{ channelLabel(entry.channel)
                }}<small>{{ entry.locale }}</small>
              </td>
              <td class="subject-cell">
                {{ entry.subject }}
                <details>
                  <summary>{{ t("integration.viewBody") }}</summary>
                  <pre>{{ entry.body }}</pre>
                </details>
              </td>
              <td>
                <div class="tag-list">
                  <span v-for="variable in entry.variables" :key="variable">{{
                    variable
                  }}</span
                  ><em v-if="!entry.variables.length">{{
                    t("integration.noVariables")
                  }}</em>
                </div>
              </td>
              <td>v{{ entry.version }}</td>
              <td>
                <span :class="['state', entry.enabled ? 'ok' : 'muted']">{{
                  entry.enabled
                    ? t("integration.enabled")
                    : t("integration.disabled")
                }}</span>
              </td>
              <td>{{ date(entry.updated_at) }}</td>
              <td>
                <div v-if="canManage" class="row-actions">
                  <button
                    :title="t('integration.edit')"
                    @click="openTemplate(entry)"
                  >
                    <Edit3 :size="14" /></button
                  ><button
                    :title="t('integration.sendRealTest')"
                    @click="openTest(entry)"
                  >
                    <FlaskConical :size="14" /></button
                  ><button
                    :title="t('integration.deleteNoHistory')"
                    @click="openAction('template-delete', entry)"
                  >
                    <Trash2 :size="14" />
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>

        <table v-else>
          <thead>
            <tr>
              <th>{{ t("integration.colTemplateIdempotency") }}</th>
              <th>{{ t("integration.colChannel") }}</th>
              <th>{{ t("integration.colRecipient") }}</th>
              <th>{{ t("integration.colSubject") }}</th>
              <th>{{ t("integration.colStatus") }}</th>
              <th>{{ t("integration.colAttempts") }}</th>
              <th>{{ t("integration.colTime") }}</th>
              <th>{{ t("integration.colDiagnostic") }}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="entry in items as NotificationDeliveryItem[]"
              :key="entry.id"
            >
              <td>
                <strong>{{
                  entry.template_name || t("integration.systemDirect")
                }}</strong
                ><small>{{
                  entry.template_code || shortID(entry.idempotency_key)
                }}</small>
              </td>
              <td>{{ channelLabel(entry.channel) }}</td>
              <td>{{ entry.recipient }}</td>
              <td class="subject-cell">{{ entry.subject }}</td>
              <td>
                <span :class="['state', entry.status]">{{
                  statusLabel(entry.status)
                }}</span>
              </td>
              <td>{{ entry.attempts }}</td>
              <td>
                {{
                  date(
                    entry.sent_at || entry.next_attempt_at || entry.created_at,
                  )
                }}
              </td>
              <td class="diagnostic">{{ entry.diagnostic || "—" }}</td>
              <td>
                <button
                  v-if="canManage && entry.status === 'failed'"
                  class="row-action"
                  @click="openAction('notification-retry', entry)"
                >
                  <RotateCcw :size="13" />{{ t("integration.retry") }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="loading" class="table-state">
          <LoaderCircle class="spin" />{{ t("integration.loadingDeliveries") }}
        </div>
        <div v-else-if="!items.length" class="table-state">
          {{ t("integration.noRecords") }}
        </div>
      </div>

      <footer class="integration-pagination">
        <span>{{
          t("integration.pagination", { total, page, pages: totalPages })
        }}</span>
        <div>
          <button
            :disabled="page <= 1 || loading"
            @click="changePage(page - 1)"
          >
            <ChevronLeft :size="15" /></button
          ><button
            :disabled="page >= totalPages || loading"
            @click="changePage(page + 1)"
          >
            <ChevronRight :size="15" />
          </button>
        </div>
      </footer>
    </div>

    <div
      v-if="modal && canManage"
      class="integration-modal-backdrop"
      @click.self="closeModal"
    >
      <section v-if="modal === 'template'" class="integration-modal wide">
        <header>
          <div>
            <span>{{
              editingTemplate
                ? t("integration.templateEditTitle")
                : t("integration.templateCreateTitle")
            }}</span
            ><small>{{ t("integration.templateHint") }}</small>
          </div>
          <button type="button" :disabled="saving" @click="closeModal">
            <X />
          </button>
        </header>
        <div class="modal-grid">
          <label
            >{{ t("integration.templateCode")
            }}<input
              v-model="templateForm.code"
              maxlength="100"
              placeholder="order.delivered"
          /></label>
          <label
            >{{ t("integration.templateName")
            }}<input
              v-model="templateForm.name"
              maxlength="160"
              :placeholder="t('integration.templateNamePlaceholder')"
          /></label>
          <label
            >{{ t("integration.channel")
            }}<select v-model="templateForm.channel">
              <option value="email">{{ t("integration.channelEmail") }}</option>
              <option value="telegram">
                {{ t("integration.channelTelegram") }}
              </option>
              <option value="wecom">{{ t("integration.channelWecom") }}</option>
              <option value="admin">{{ t("integration.channelAdmin") }}</option>
              <option v-if="templateForm.audience === 'user'" value="in_app">
                {{ t("integration.channelInApp") }}
              </option>
            </select></label
          >
          <label
            >{{ t("integration.audience")
            }}<select
              v-model="templateForm.audience"
              @change="
                templateForm.channel =
                  templateForm.audience === 'user' ? 'in_app' : 'email'
              "
            >
              <option value="admin">
                {{ t("integration.audienceName.admin") }}
              </option>
              <option value="user">
                {{ t("integration.audienceName.user") }}
              </option>
            </select></label
          >
          <label
            >{{ t("integration.language")
            }}<input
              v-model="templateForm.locale"
              maxlength="16"
              placeholder="zh-CN"
          /></label>
          <label class="full"
            >{{ t("integration.subject")
            }}<input
              v-model="templateForm.subject"
              maxlength="255"
              :placeholder="t('integration.subjectPlaceholder')"
          /></label>
          <label class="full"
            >{{ t("integration.body")
            }}<textarea
              v-model="templateForm.body"
              rows="8"
              maxlength="20000"
              :placeholder="t('integration.bodyPlaceholder')"
            ></textarea>
          </label>
          <label class="full"
            >{{ t("integration.variablesLabel")
            }}<input
              v-model="templateForm.variables"
              placeholder="order_no, product_name"
            /><small>{{
              t("integration.currentVariables", {
                list: parsedVariables().join("、") || t("integration.none"),
              })
            }}</small></label
          >
          <label class="check"
            ><input v-model="templateForm.enabled" type="checkbox" />{{
              t("integration.enableImmediately")
            }}</label
          >
          <label class="full"
            >{{ t("integration.auditReason")
            }}<textarea
              v-model="templateForm.reason"
              rows="2"
              maxlength="500"
              :placeholder="t('integration.templateReasonPlaceholder')"
            ></textarea>
          </label>
        </div>
        <p v-if="modalError" class="integration-message error">
          {{ modalError }}
        </p>
        <footer>
          <button
            type="button"
            class="tool-button"
            :disabled="saving"
            @click="closeModal"
          >
            {{ t("integration.cancel") }}</button
          ><button
            class="primary-action"
            :disabled="saving"
            @click="saveTemplate"
          >
            <LoaderCircle v-if="saving" class="spin" :size="15" />{{
              t("integration.saveTemplate")
            }}
          </button>
        </footer>
      </section>

      <section
        v-else-if="modal === 'test' && testingTemplate"
        class="integration-modal"
      >
        <header>
          <div>
            <span>{{ t("integration.testTitle") }}</span
            ><small
              >{{ testingTemplate.name }} ·
              {{ channelLabel(testingTemplate.channel) }}</small
            >
          </div>
          <button type="button" :disabled="saving" @click="closeModal">
            <X />
          </button>
        </header>
        <div class="modal-grid one">
          <label
            >{{ t("integration.recipient")
            }}<input
              v-model="testForm.recipient"
              maxlength="255"
              :placeholder="
                testingTemplate.channel === 'email'
                  ? 'ops@example.com'
                  : testingTemplate.channel === 'admin'
                    ? 'admin'
                    : t('integration.connectorRecipientPlaceholder')
              "
          /></label>
          <label v-for="variable in testingTemplate.variables" :key="variable"
            >{{ t("integration.variable", { name: variable })
            }}<input v-model="testForm.values[variable]" maxlength="2000"
          /></label>
          <label
            >{{ t("integration.idempotencyKey")
            }}<input
              v-model="testForm.idempotencyKey"
              maxlength="64"
            /><small>{{ t("integration.idempotencyHint") }}</small></label
          >
          <label
            >{{ t("integration.auditReason")
            }}<textarea
              v-model="testForm.reason"
              rows="3"
              maxlength="500"
              :placeholder="t('integration.testReasonPlaceholder')"
            ></textarea>
          </label>
        </div>
        <p v-if="modalError" class="integration-message error">
          {{ modalError }}
        </p>
        <footer>
          <button
            type="button"
            class="tool-button"
            :disabled="saving"
            @click="closeModal"
          >
            {{ t("integration.cancel") }}</button
          ><button class="primary-action" :disabled="saving" @click="sendTest">
            <Send :size="15" />{{ t("integration.sendTest") }}
          </button>
        </footer>
      </section>

      <section v-else class="integration-modal compact-modal">
        <header>
          <div>
            <span>{{ actionTitle }}</span
            ><small>{{ t("integration.auditLogHint") }}</small>
          </div>
          <button type="button" :disabled="saving" @click="closeModal">
            <X />
          </button>
        </header>
        <p v-if="actionType === 'template-delete'">
          {{ t("integration.actionTemplateDeleteHint") }}
        </p>
        <p v-else-if="actionType.includes('retry')">
          {{ t("integration.actionRetryHint") }}
        </p>
        <p v-else>{{ t("integration.actionEndpointHint") }}</p>
        <label
          >{{ t("integration.auditReason")
          }}<textarea
            v-model="actionReason"
            rows="4"
            maxlength="500"
            :placeholder="t('integration.actionReasonPlaceholder')"
          ></textarea>
        </label>
        <p v-if="modalError" class="integration-message error">
          {{ modalError }}
        </p>
        <footer>
          <button
            type="button"
            class="tool-button"
            :disabled="saving"
            @click="closeModal"
          >
            {{ t("integration.cancel") }}</button
          ><button
            class="primary-action"
            :disabled="saving"
            @click="submitAction"
          >
            {{ t("integration.confirmAction") }}
          </button>
        </footer>
      </section>
    </div>
  </section>
</template>

<style scoped>
.integration-console {
  display: grid;
  gap: 18px;
}
.integration-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.integration-summary article {
  position: relative;
  min-height: 128px;
  padding: 18px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.integration-summary svg {
  position: absolute;
  right: 17px;
  top: 17px;
  color: var(--muted);
}
.integration-summary span,
.integration-summary small {
  display: block;
  color: var(--muted);
  font-size: 11px;
}
.integration-summary strong {
  display: block;
  margin: 17px 0 5px;
  font-size: 27px;
  letter-spacing: -0.04em;
}
.integration-summary small.danger {
  color: var(--danger);
}
.integration-card {
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.integration-tabs {
  display: flex;
  overflow-x: auto;
  border-bottom: 1px solid var(--line);
}
.integration-tabs button {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: max-content;
  padding: 15px 18px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--muted);
  font-size: 12px;
}
.integration-tabs button.active {
  border-bottom-color: var(--dark);
  color: var(--text);
  font-weight: 700;
}
.integration-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 9px;
  align-items: center;
  padding: 13px 15px;
  border-bottom: 1px solid var(--line);
  background: var(--surface-2);
}
.integration-toolbar form {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 230px;
  flex: 1;
  height: 36px;
  padding: 0 11px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--muted);
}
.integration-toolbar form input {
  min-width: 0;
  flex: 1;
  border: 0;
  outline: 0;
  background: transparent;
  font-size: 11px;
}
.integration-toolbar select,
.compact-input,
.integration-modal input,
.integration-modal select,
.integration-modal textarea {
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  outline: none;
}
.integration-toolbar select,
.compact-input {
  height: 36px;
  padding: 0 9px;
  font-size: 11px;
}
.compact-input {
  width: 150px;
}
.tool-button,
.primary-action,
.row-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
}
.tool-button {
  height: 36px;
  padding: 0 11px;
  border: 1px solid var(--line);
  background: var(--surface);
}
.primary-action {
  min-height: 36px;
  padding: 0 13px;
  border: 1px solid var(--dark);
  background: var(--dark);
  color: var(--dark-text);
}
button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.integration-message {
  display: flex;
  align-items: center;
  gap: 7px;
  margin: 12px 15px 0;
  padding: 10px 12px;
  border-radius: 6px;
  font-size: 11px;
}
.integration-message.error {
  color: var(--danger);
  background: color-mix(in srgb, var(--danger) 9%, transparent);
}
.integration-message.success {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 9%, transparent);
}
.integration-table-wrap {
  position: relative;
  overflow-x: auto;
  min-height: 260px;
}
table {
  width: 100%;
  border-collapse: collapse;
  min-width: 1050px;
}
th {
  padding: 11px 13px;
  background: var(--surface-2);
  color: var(--muted);
  font-size: 9px;
  letter-spacing: 0.04em;
  text-align: left;
  white-space: nowrap;
}
td {
  padding: 13px;
  border-top: 1px solid var(--line);
  font-size: 11px;
  vertical-align: top;
}
td strong,
td small {
  display: block;
}
td strong {
  max-width: 300px;
  overflow-wrap: anywhere;
}
td small {
  margin-top: 4px;
  color: var(--muted);
  font-size: 9px;
}
.tag-list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  max-width: 280px;
}
.tag-list span {
  padding: 3px 6px;
  border-radius: 4px;
  background: var(--soft);
  font-size: 9px;
}
.tag-list em {
  color: var(--muted);
  font-style: normal;
}
.state {
  display: inline-flex;
  padding: 4px 7px;
  border-radius: 999px;
  background: var(--soft);
  color: var(--muted);
  font-size: 9px;
  white-space: nowrap;
}
.state.ok,
.state.delivered,
.state.sent {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 10%, transparent);
}
.state.failed,
.state.bad {
  color: var(--danger);
  background: color-mix(in srgb, var(--danger) 10%, transparent);
}
.state.queued,
.state.sending,
.state.retrying {
  color: var(--warn);
  background: color-mix(in srgb, var(--warn) 10%, transparent);
}
.diagnostic {
  max-width: 260px;
  color: var(--muted);
}
.subject-cell {
  max-width: 300px;
  overflow-wrap: anywhere;
}
.subject-cell details {
  margin-top: 6px;
  color: var(--muted);
}
.subject-cell summary {
  cursor: pointer;
  font-size: 9px;
}
.subject-cell pre {
  max-width: 310px;
  max-height: 150px;
  overflow: auto;
  white-space: pre-wrap;
  font: inherit;
  color: var(--text);
}
.row-action {
  min-height: 29px;
  padding: 0 8px;
  border: 1px solid var(--line);
  background: var(--surface);
}
.row-actions {
  display: flex;
  gap: 5px;
}
.row-actions button {
  display: grid;
  place-items: center;
  width: 29px;
  height: 29px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
}
.table-state {
  position: absolute;
  inset: 42px 0 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  background: color-mix(in srgb, var(--surface) 90%, transparent);
  color: var(--muted);
  font-size: 11px;
}
.integration-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 15px;
  border-top: 1px solid var(--line);
  color: var(--muted);
  font-size: 10px;
}
.integration-pagination div {
  display: flex;
  gap: 5px;
}
.integration-pagination button {
  display: grid;
  place-items: center;
  width: 30px;
  height: 29px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
}
.integration-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: grid;
  place-items: center;
  padding: 20px;
  background: rgba(0, 0, 0, 0.55);
}
.integration-modal {
  width: min(560px, 100%);
  max-height: calc(100vh - 40px);
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: 0 24px 90px rgba(0, 0, 0, 0.3);
}
.integration-modal.wide {
  width: min(820px, 100%);
}
.integration-modal header {
  position: sticky;
  top: 0;
  z-index: 2;
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 18px;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
}
.integration-modal header span,
.integration-modal header small {
  display: block;
}
.integration-modal header span {
  font-size: 14px;
  font-weight: 700;
}
.integration-modal header small {
  margin-top: 3px;
  color: var(--muted);
  font-size: 9px;
}
.integration-modal header button {
  border: 0;
  background: transparent;
  color: var(--muted);
}
.modal-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 13px;
  padding: 18px;
}
.modal-grid.one {
  grid-template-columns: 1fr;
}
.modal-grid label,
.compact-modal label {
  display: grid;
  gap: 6px;
  font-size: 10px;
  font-weight: 600;
}
.modal-grid label.full {
  grid-column: 1 / -1;
}
.modal-grid label.check {
  display: flex;
  align-items: center;
}
.modal-grid input,
.modal-grid select {
  height: 38px;
  padding: 0 10px;
}
.modal-grid input[type="checkbox"],
.modal-grid input[type="radio"] {
  width: 18px;
  height: 18px;
  padding: 0;
}
.modal-grid textarea,
.compact-modal textarea {
  padding: 9px 10px;
  resize: vertical;
}
.modal-grid small {
  color: var(--muted);
  font-size: 9px;
  font-weight: 400;
}
.integration-modal footer {
  position: sticky;
  bottom: 0;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 13px 18px;
  border-top: 1px solid var(--line);
  background: var(--surface);
}
.compact-modal > p,
.compact-modal > label {
  margin: 16px 18px 0;
}
.compact-modal > p {
  color: var(--muted);
  font-size: 11px;
  line-height: 1.6;
}
.compact-modal textarea {
  width: 100%;
}
.spin {
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 1100px) {
  .integration-summary {
    grid-template-columns: 1fr 1fr;
  }
}
@media (max-width: 680px) {
  .integration-summary {
    grid-template-columns: 1fr;
  }
  .integration-summary article {
    min-height: 105px;
  }
  .integration-toolbar {
    align-items: stretch;
  }
  .integration-toolbar form,
  .integration-toolbar select,
  .compact-input,
  .tool-button,
  .primary-action {
    width: 100%;
  }
  .modal-grid {
    grid-template-columns: 1fr;
  }
  .modal-grid label.full {
    grid-column: auto;
  }
  .integration-modal-backdrop {
    padding: 0;
    align-items: end;
  }
  .integration-modal,
  .integration-modal.wide {
    width: 100%;
    max-height: 92vh;
    border-radius: 12px 12px 0 0;
  }
}
</style>
