<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import {
  BellRing,
  Cable,
  CircleAlert,
  ExternalLink,
  Plus,
  RefreshCw,
  Save,
  Trash2,
  X,
} from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { adminApi, useAuthStore } from "../stores/auth";

interface EventItem {
  code: string;
  group: string;
  name: string;
  severity: string;
  variables: string[];
}
interface TemplateItem {
  id: string;
  name: string;
  code: string;
  channel: string;
  audience: "admin" | "user";
  locale: string;
  enabled: boolean;
}
interface RuleItem {
  id: string;
  audience: "admin" | "user";
  event_code: string;
  channel: string;
  recipient: string;
  template_id: string;
  template_name: string;
  locale: string;
  enabled: boolean;
  updated_at: string;
}
interface ConnectorItem {
  id: string;
  name: string;
  channel: string;
  endpoint: string;
  username: string;
  sender: string;
  credentials_configured: boolean;
  enabled: boolean;
  updated_at: string;
}
const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("system.manage"));
type NotificationTab = "rules" | "connectors" | "events";

function routeTab(): NotificationTab {
  const value = String(route.meta.defaultTab || "rules");
  return value === "connectors" || value === "events" ? value : "rules";
}

const active = ref<NotificationTab>(routeTab());
const loading = ref(false);
const error = ref("");
const notice = ref("");
const events = ref<EventItem[]>([]);
const templates = ref<TemplateItem[]>([]);
const rules = ref<RuleItem[]>([]);
const connectors = ref<ConnectorItem[]>([]);
const modal = ref<"rule" | "connector" | null>(null);
const editingRule = ref<RuleItem | null>(null);
const saving = ref(false);
const ruleForm = reactive({
  audience: "admin" as "admin" | "user",
  event_code: "",
  channel: "email",
  recipient: "",
  template_id: "",
  locale: "zh-CN",
  enabled: true,
  reason: "",
});
const connectorForm = reactive({
  name: "",
  channel: "email",
  endpoint: "",
  username: "",
  sender: "",
  secret: "",
  enabled: true,
  reason: "",
});
const matchingTemplates = computed(() =>
  templates.value.filter(
    (item) =>
      item.enabled &&
      item.channel === ruleForm.channel &&
      item.audience === ruleForm.audience &&
      item.locale === ruleForm.locale,
  ),
);
const groupedEvents = computed(() => {
  const result: Record<string, EventItem[]> = {};
  for (const item of events.value) (result[item.group] ||= []).push(item);
  return result;
});
function message(e: unknown, fallback: string) {
  const value = (e as { response?: { data?: { message?: string } } }).response
    ?.data?.message;
  return value && !value.startsWith("error.") ? value : fallback;
}
function headers(reason: string) {
  return { headers: { "X-Change-Reason": reason.trim() } };
}
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [eventResponse, templateItems, ruleItems, connectorResponse] =
      await Promise.all([
        adminApi.get("/notifications/events"),
        fetchAllNotificationPages<TemplateItem>("/notifications/templates"),
        fetchAllNotificationPages<RuleItem>("/notifications/subscriptions"),
        adminApi.get("/notifications/connectors"),
      ]);
    events.value = eventResponse.data.data || [];
    templates.value = templateItems;
    rules.value = ruleItems;
    connectors.value = connectorResponse.data.data || [];
  } catch (e) {
    error.value = message(e, t("notification.errLoad"));
  } finally {
    loading.value = false;
  }
}
async function fetchAllNotificationPages<T>(path: string) {
  const result: T[] = [];
  let page = 1;
  let total = 0;
  do {
    const response = await adminApi.get(path, {
      params: { page, page_size: 100 },
    });
    const payload = response.data.data || {};
    const items = Array.isArray(payload) ? payload : payload.items || [];
    result.push(...items);
    total = Array.isArray(payload)
      ? result.length
      : Number(payload.total || result.length);
    page += 1;
  } while (result.length < total && page <= 20);
  return result;
}
function openRule(item?: RuleItem) {
  if (!canManage.value) return;
  editingRule.value = item || null;
  Object.assign(
    ruleForm,
    item
      ? {
          audience: item.audience || "admin",
          event_code: item.event_code,
          channel: item.channel,
          recipient: item.recipient,
          template_id: item.template_id,
          locale: item.locale,
          enabled: item.enabled,
          reason: "",
        }
      : {
          audience: "admin",
          event_code: events.value[0]?.code || "",
          channel: "email",
          recipient: "",
          template_id: "",
          locale: "zh-CN",
          enabled: true,
          reason: "",
        },
  );
  modal.value = "rule";
}
function openConnector(channel: string) {
  if (!canManage.value) return;
  const item = connectors.value.find((value) => value.channel === channel);
  Object.assign(
    connectorForm,
    item
      ? {
          name: item.name,
          channel: item.channel,
          endpoint: item.endpoint,
          username: item.username,
          sender: item.sender,
          secret: "",
          enabled: item.enabled,
          reason: "",
        }
      : {
          name:
            channel === "email"
              ? t("notification.connectorDefault.email")
              : channel === "telegram"
                ? t("notification.connectorDefault.telegram")
                : t("notification.connectorDefault.wecom"),
          channel,
          endpoint: channel === "email" ? "smtp.example.com:465" : "",
          username: "",
          sender: "",
          secret: "",
          enabled: true,
          reason: "",
        },
  );
  modal.value = "connector";
}
async function saveRule() {
  if (!canManage.value) return;
  if (
    ruleForm.reason.trim().length < 4 ||
    !ruleForm.template_id ||
    !ruleForm.recipient.trim()
  ) {
    error.value = t("notification.errRuleFields");
    return;
  }
  saving.value = true;
  error.value = "";
  try {
    const payload = {
      audience: ruleForm.audience,
      event_code: ruleForm.event_code,
      channel: ruleForm.channel,
      recipient: ruleForm.recipient.trim(),
      template_id: ruleForm.template_id,
      locale: ruleForm.locale,
      enabled: ruleForm.enabled,
    };
    if (editingRule.value)
      await adminApi.put(
        `/notifications/subscriptions/${editingRule.value.id}`,
        payload,
        headers(ruleForm.reason),
      );
    else
      await adminApi.post(
        "/notifications/subscriptions",
        payload,
        headers(ruleForm.reason),
      );
    notice.value = t("notification.ruleSaved");
    modal.value = null;
    await load();
  } catch (e) {
    error.value = message(e, t("notification.errRuleSave"));
  } finally {
    saving.value = false;
  }
}
async function removeRule(item: RuleItem) {
  if (!canManage.value) return;
  const reason = window.prompt(t("notification.deleteReason"));
  if (!reason || reason.trim().length < 4) return;
  try {
    await adminApi.delete(
      `/notifications/subscriptions/${item.id}`,
      headers(reason),
    );
    notice.value = t("notification.ruleDeleted");
    await load();
  } catch (e) {
    error.value = message(e, t("notification.errRuleDelete"));
  }
}
async function saveConnector() {
  if (!canManage.value) return;
  if (
    connectorForm.reason.trim().length < 4 ||
    (!connectorForm.secret &&
      !connectors.value.some(
        (item) =>
          item.channel === connectorForm.channel && item.credentials_configured,
      ))
  ) {
    error.value = t("notification.errConnectorFields");
    return;
  }
  saving.value = true;
  error.value = "";
  try {
    await adminApi.put(
      "/notifications/connectors",
      {
        name: connectorForm.name,
        channel: connectorForm.channel,
        endpoint: connectorForm.endpoint,
        username: connectorForm.username,
        sender: connectorForm.sender,
        secret: connectorForm.secret,
        enabled: connectorForm.enabled,
      },
      headers(connectorForm.reason),
    );
    connectorForm.secret = "";
    notice.value = t("notification.connectorSaved");
    modal.value = null;
    await load();
  } catch (e) {
    error.value = message(e, t("notification.errConnectorSave"));
  } finally {
    saving.value = false;
  }
}
function channelName(value: string) {
  return t(`notification.channel.${value}`);
}
function eventName(value: string) {
  const key = `notification.eventName.${value}`;
  const translated = t(key);
  return translated === key ? value : translated;
}
function severityName(value: string) {
  const key = `notification.severityName.${value}`;
  const translated = t(key);
  return translated === key ? value : translated;
}
watch(
  () => [route.path, route.meta.defaultTab] as const,
  () => {
    active.value = routeTab();
  },
);

onMounted(load);
</script>

<template>
  <main class="notification-page">
    <header class="notification-hero">
      <div>
        <span><BellRing :size="16" />{{ t("notification.eyebrow") }}</span>
        <h1>{{ t("notification.title") }}</h1>
        <p>{{ t("notification.subtitle") }}</p>
      </div>
      <button :disabled="loading" @click="load">
        <RefreshCw :class="{ spin: loading }" :size="15" />{{
          t("notification.refresh")
        }}
      </button>
    </header>
    <div v-if="error" class="notice error">
      <CircleAlert :size="16" />{{ error }}
    </div>
    <div v-if="notice" class="notice success">{{ notice }}</div>
    <section v-if="active === 'rules'" class="panel">
      <header>
        <div>
          <h2>{{ t("notification.rulesTitle") }}</h2>
          <p>{{ t("notification.rulesHint") }}</p>
        </div>
        <div class="header-actions">
          <button @click="router.push('/notification-templates')">
            {{ t("notification.manageTemplates")
            }}<ExternalLink :size="14" /></button
          ><button v-if="canManage" class="primary" @click="openRule()">
            <Plus :size="14" />{{ t("notification.createRule") }}
          </button>
        </div>
      </header>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>{{ t("notification.event") }}</th>
              <th>{{ t("notification.audience") }}</th>
              <th>{{ t("notification.channelLabel") }}</th>
              <th>{{ t("notification.recipient") }}</th>
              <th>{{ t("notification.template") }}</th>
              <th>{{ t("notification.locale") }}</th>
              <th>{{ t("notification.status") }}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in rules" :key="item.id">
              <td>
                <strong>{{ eventName(item.event_code) }}</strong
                ><small>{{ item.event_code }}</small>
              </td>
              <td>{{ t(`notification.audienceName.${item.audience}`) }}</td>
              <td>{{ channelName(item.channel) }}</td>
              <td>{{ item.recipient }}</td>
              <td>{{ item.template_name }}</td>
              <td>{{ item.locale }}</td>
              <td>
                <span :class="['state', item.enabled ? 'ok' : 'muted']">{{
                  item.enabled
                    ? t("notification.enabled")
                    : t("notification.disabled")
                }}</span>
              </td>
              <td>
                <button v-if="canManage" @click="openRule(item)">
                  {{ t("notification.edit") }}</button
                ><button
                  v-if="canManage"
                  class="danger"
                  @click="removeRule(item)"
                >
                  <Trash2 :size="14" />
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="!loading && !rules.length" class="empty">
          {{ t("notification.noRules") }}
        </div>
      </div>
    </section>
    <section v-else-if="active === 'connectors'" class="connector-grid">
      <article v-for="channel in ['email', 'telegram', 'wecom']" :key="channel">
        <Cable :size="24" />
        <div>
          <h2>{{ channelName(channel) }}</h2>
          <p>{{ t(`notification.connectorHint.${channel}`) }}</p>
        </div>
        <span
          :class="[
            'state',
            connectors.find((v) => v.channel === channel)?.enabled
              ? 'ok'
              : 'muted',
          ]"
          >{{
            connectors.find((v) => v.channel === channel)?.enabled
              ? t("notification.enabled")
              : t("notification.notConfigured")
          }}</span
        ><button v-if="canManage" @click="openConnector(channel)">
          {{ t("notification.configure") }}
        </button>
      </article>
      <aside>{{ t("notification.connectorSecurity") }}</aside>
    </section>
    <section v-else class="event-groups">
      <article v-for="(items, group) in groupedEvents" :key="group">
        <header>
          <h2>{{ t(`notification.group.${group}`) }}</h2>
          <span>{{ items.length }}</span>
        </header>
        <div>
          <button
            v-for="item in items"
            :key="item.code"
            :disabled="!canManage"
            @click="
              openRule();
              ruleForm.event_code = item.code;
            "
          >
            <strong>{{ eventName(item.code) }}</strong
            ><small>{{ item.code }}</small
            ><em :class="item.severity">{{ severityName(item.severity) }}</em>
          </button>
        </div>
      </article>
    </section>
    <div
      v-if="modal && canManage"
      class="modal-backdrop"
      @click.self="modal = null"
    >
      <section class="modal">
        <header>
          <h2>
            {{
              modal === "rule"
                ? t("notification.ruleEditor")
                : t("notification.connectorEditor")
            }}
          </h2>
          <button @click="modal = null"><X /></button>
        </header>
        <form v-if="modal === 'rule'" @submit.prevent="saveRule">
          <label
            >{{ t("notification.event")
            }}<select v-model="ruleForm.event_code">
              <optgroup
                v-for="(items, group) in groupedEvents"
                :key="group"
                :label="t(`notification.group.${group}`)"
              >
                <option
                  v-for="item in items"
                  :key="item.code"
                  :value="item.code"
                >
                  {{ eventName(item.code) }} · {{ item.code }}
                </option>
              </optgroup>
            </select></label
          ><label
            >{{ t("notification.audience")
            }}<select
              v-model="ruleForm.audience"
              @change="
                ruleForm.channel =
                  ruleForm.audience === 'user' ? 'in_app' : 'email';
                ruleForm.recipient =
                  ruleForm.audience === 'user' ? 'event_user' : '';
                ruleForm.template_id = '';
              "
            >
              <option value="admin">
                {{ t("notification.audienceName.admin") }}
              </option>
              <option value="user">
                {{ t("notification.audienceName.user") }}
              </option>
            </select></label
          ><label
            >{{ t("notification.channelLabel")
            }}<select
              v-model="ruleForm.channel"
              @change="ruleForm.template_id = ''"
            >
              <option
                v-for="channel in ruleForm.audience === 'user'
                  ? ['in_app', 'email']
                  : ['email', 'telegram', 'wecom', 'admin']"
                :key="channel"
                :value="channel"
              >
                {{ channelName(channel) }}
              </option>
            </select></label
          ><label class="full"
            >{{ t("notification.recipient")
            }}<input
              v-model="ruleForm.recipient"
              :disabled="ruleForm.audience === 'user'"
              :placeholder="
                t(`notification.recipientHint.${ruleForm.channel}`)
              " /></label
          ><label class="full"
            >{{ t("notification.template")
            }}<select v-model="ruleForm.template_id">
              <option value="">{{ t("notification.selectTemplate") }}</option>
              <option
                v-for="item in matchingTemplates"
                :key="item.id"
                :value="item.id"
              >
                {{ item.name }} · {{ item.code }}
              </option></select
            ><small v-if="!matchingTemplates.length">{{
              t("notification.noChannelTemplate")
            }}</small></label
          ><label
            >{{ t("notification.locale")
            }}<select
              v-model="ruleForm.locale"
              @change="ruleForm.template_id = ''"
            >
              <option
                v-for="value in [
                  'zh-CN',
                  'zh-TW',
                  'en',
                  'vi',
                  'ru',
                  'ja',
                  'ko',
                  'th',
                ]"
                :key="value"
                :value="value"
              >
                {{ value }}
              </option>
            </select></label
          ><label class="check"
            ><input v-model="ruleForm.enabled" type="checkbox" />{{
              t("notification.enabled")
            }}</label
          ><label class="full"
            >{{ t("notification.reason")
            }}<textarea v-model="ruleForm.reason" maxlength="500" /></label
          ><button class="primary submit" :disabled="saving">
            <Save :size="15" />{{ t("notification.save") }}
          </button>
        </form>
        <form v-else @submit.prevent="saveConnector">
          <label
            >{{ t("notification.channelLabel")
            }}<input
              :value="channelName(connectorForm.channel)"
              disabled /></label
          ><label
            >{{ t("notification.connectorName")
            }}<input v-model="connectorForm.name" /></label
          ><label class="full"
            >{{
              connectorForm.channel === "email"
                ? t("notification.smtpEndpoint")
                : t("notification.apiEndpoint")
            }}<input
              v-model="connectorForm.endpoint"
              :placeholder="
                connectorForm.channel === 'email'
                  ? 'smtp.example.com:465'
                  : t('notification.optionalOfficialEndpoint')
              " /></label
          ><label v-if="connectorForm.channel === 'email'"
            >{{ t("notification.username")
            }}<input v-model="connectorForm.username" /></label
          ><label v-if="connectorForm.channel === 'email'"
            >{{ t("notification.sender")
            }}<input v-model="connectorForm.sender" type="email" /></label
          ><label class="full"
            >{{ t(`notification.secret.${connectorForm.channel}`)
            }}<input
              v-model="connectorForm.secret"
              type="password"
              autocomplete="new-password"
            /><small>{{ t("notification.secretNeverReturned") }}</small></label
          ><label class="check"
            ><input v-model="connectorForm.enabled" type="checkbox" />{{
              t("notification.enabled")
            }}</label
          ><label class="full"
            >{{ t("notification.reason")
            }}<textarea v-model="connectorForm.reason" maxlength="500" /></label
          ><button class="primary submit" :disabled="saving">
            <Save :size="15" />{{ t("notification.save") }}
          </button>
        </form>
      </section>
    </div>
  </main>
</template>

<style scoped>
.notification-page {
  display: grid;
  gap: 18px;
}
.notification-hero,
.panel,
.connector-grid article,
.event-groups article {
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface);
}
.notification-hero {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 24px;
}
.notification-hero span,
.notification-hero button,
.header-actions,
.tabs {
  display: flex;
  align-items: center;
  gap: 8px;
}
.notification-hero h1 {
  margin: 8px 0;
  font-size: 28px;
}
.notification-hero p,
.panel p,
.connector-grid p {
  margin: 0;
  color: var(--muted);
}
button {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  color: var(--text);
  padding: 9px 12px;
  cursor: pointer;
}
.primary {
  background: var(--text);
  color: var(--surface);
}
.notice {
  display: flex;
  gap: 8px;
  padding: 12px;
  border-radius: 8px;
}
.notice.error {
  color: #b42318;
  background: #fff1f0;
}
.notice.success {
  color: #087443;
  background: #ecfdf3;
}
.tabs {
  border-bottom: 1px solid var(--border);
}
.tabs button {
  border: 0;
  border-radius: 0;
  background: transparent;
}
.tabs button.active {
  border-bottom: 2px solid var(--text);
}
.tabs b {
  padding: 2px 7px;
  border-radius: 99px;
  background: var(--surface-2);
}
.panel > header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 20px;
}
.panel h2,
.connector-grid h2,
.event-groups h2 {
  margin: 0 0 5px;
}
.table-wrap {
  overflow: auto;
}
table {
  width: 100%;
  border-collapse: collapse;
}
th,
td {
  text-align: left;
  padding: 13px 16px;
  border-top: 1px solid var(--border);
  white-space: nowrap;
}
td strong,
td small {
  display: block;
}
td small {
  color: var(--muted);
  margin-top: 3px;
}
.state {
  padding: 4px 8px;
  border-radius: 99px;
}
.state.ok {
  background: #e9f8ef;
  color: #087443;
}
.state.muted {
  background: var(--surface-2);
  color: var(--muted);
}
td .danger {
  margin-left: 5px;
  color: #b42318;
}
.empty {
  padding: 40px;
  text-align: center;
  color: var(--muted);
}
.connector-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 14px;
}
.connector-grid article {
  display: grid;
  gap: 14px;
  padding: 20px;
}
.connector-grid aside {
  grid-column: 1/-1;
  padding: 14px;
  border: 1px dashed var(--border);
  color: var(--muted);
}
.event-groups {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 14px;
}
.event-groups article {
  padding: 18px;
}
.event-groups article header {
  display: flex;
  justify-content: space-between;
}
.event-groups article > div {
  display: grid;
  gap: 7px;
}
.event-groups article button {
  text-align: left;
  display: grid;
  grid-template-columns: 1fr auto;
}
.event-groups small {
  color: var(--muted);
}
.event-groups em {
  grid-row: 1/3;
  grid-column: 2;
  font-size: 10px;
}
.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 1200;
  display: grid;
  place-items: center;
  padding: 20px;
  background: #0008;
}
.modal {
  width: min(720px, 100%);
  max-height: 90vh;
  overflow: auto;
  border-radius: 12px;
  background: var(--surface);
  padding: 20px;
}
.modal > header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.modal form {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}
.modal label {
  display: grid;
  gap: 6px;
}
.modal .full,
.modal .submit {
  grid-column: 1/-1;
}
.modal input:not([type="checkbox"]):not([type="radio"]),
.modal select,
.modal textarea {
  width: 100%;
  box-sizing: border-box;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  color: var(--text);
  padding: 10px;
}
.modal textarea {
  min-height: 78px;
}
.modal .check {
  display: flex;
  align-items: center;
}
.modal .check input {
  width: 18px;
  height: 18px;
  padding: 0;
}
.spin {
  animation: spin 1s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 900px) {
  .connector-grid {
    grid-template-columns: 1fr;
  }
  .event-groups {
    grid-template-columns: 1fr;
  }
  .panel > header {
    align-items: flex-start;
    gap: 12px;
    flex-direction: column;
  }
}
@media (max-width: 620px) {
  .notification-hero {
    align-items: flex-start;
    gap: 15px;
    flex-direction: column;
  }
  .tabs {
    overflow: auto;
  }
  .modal form {
    grid-template-columns: 1fr;
  }
  .modal label,
  .modal .full,
  .modal .submit {
    grid-column: 1;
  }
  .header-actions {
    flex-wrap: wrap;
  }
}
</style>
