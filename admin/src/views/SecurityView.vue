<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  Check,
  Clipboard,
  Download,
  Eye,
  History,
  KeyRound,
  RefreshCw,
  Search,
  ShieldCheck,
  ShieldX,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";

const { t, locale } = useI18n();

interface TOTPStatus {
  enabled: boolean;
  pending_reset: boolean;
  verified_at?: string | null;
  pending_expires_at?: string | null;
}

interface TOTPSetup {
  secret: string;
  provisioning_uri: string;
  recovery_codes: string[];
  pending_reset: boolean;
}

interface SecurityEvent {
  id: string;
  event_type: string;
  severity: string;
  realm: string;
  principal_id?: string | null;
  ip?: string;
  user_agent?: string;
  details: Record<string, unknown>;
  resolved: boolean;
  resolved_by?: string | null;
  resolved_by_name?: string;
  resolved_at?: string | null;
  created_at: string;
}

interface LoginEvent {
  id: string;
  realm: string;
  principal_id?: string | null;
  account: string;
  ip?: string;
  country?: string;
  city?: string;
  user_agent?: string;
  succeeded: boolean;
  reason?: string;
  created_at: string;
}

interface IPBlock {
  id: string;
  cidr: string;
  scope: "public" | "openapi" | "admin" | "all" | string;
  reason: string;
  source: string;
  enabled: boolean;
  expires_at?: string | null;
}

type ReauthenticationMethod = "current_code" | "password";

const router = useRouter();
const auth = useAuthStore();
const status = ref<TOTPStatus>({ enabled: false, pending_reset: false });
const setup = ref<TOTPSetup | null>(null);
const eventTab = ref<"security" | "login">("security");
const events = ref<SecurityEvent[]>([]);
const loginEvents = ref<LoginEvent[]>([]);
const eventLoading = ref(false);
const eventError = ref("");
const eventPage = ref(1);
const eventPageSize = ref(20);
const eventTotal = ref(0);
const eventSearch = ref("");
const eventRealm = ref("");
const eventSeverity = ref("");
const eventResult = ref("");
const eventFrom = ref("");
const eventTo = ref("");
const selectedEvent = ref<SecurityEvent | null>(null);
const dispositionOpen = ref(false);
const dispositionConclusion = ref("");
const dispositionEvidence = ref("");
const dispositionReason = ref("");
const blocks = ref<IPBlock[]>([]);
const loading = ref(true);
const blockLoading = ref(false);
const blockError = ref("");
const busy = ref(false);
const error = ref("");
const notice = ref("");
const reauthenticationMethod = ref<ReauthenticationMethod>("current_code");
const currentCode = ref("");
const password = ref("");
const verificationCode = ref("");
const setupVerified = ref(false);
const revokeConfirmation = ref("");
const blockCIDR = ref("");
const blockScope = ref("public");
const blockReason = ref("");
const blockExpiresAt = ref("");
const blockChangeReason = ref("");
const canViewSecurityEvents = computed(() => auth.hasPermission("system.view"));
const canManageSecurityOperations = computed(() =>
  auth.hasPermission("system.manage"),
);
const canViewIPBlocks = computed(() => auth.hasPermission("security.view"));
const revokePrompt = computed(() => {
  const translated = t("security.typeRevoke");
  const marker = "<code>REVOKE</code>";
  const index = translated.indexOf(marker);
  return index < 0
    ? { before: translated, after: "" }
    : {
        before: translated.slice(0, index),
        after: translated.slice(index + marker.length),
      };
});

function message(reason: unknown, fallback: string) {
  const failure = reason as { response?: { data?: { message?: string } } };
  const serverMessage = failure.response?.data?.message;
  return serverMessage && !serverMessage.startsWith("error.")
    ? serverMessage
    : fallback;
}

function clearSetupSecrets() {
  setup.value = null;
  verificationCode.value = "";
  setupVerified.value = false;
}

function selectReauthenticationMethod(method: ReauthenticationMethod) {
  reauthenticationMethod.value = method;
  currentCode.value = "";
  password.value = "";
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const statusResponse = await adminApi.get("/security/2fa");
    status.value = statusResponse.data.data;
  } catch (reason: unknown) {
    error.value = message(reason, t("security.errLoad"));
  } finally {
    loading.value = false;
  }

  const privilegedLoads: Promise<void>[] = [];
  if (canViewIPBlocks.value) privilegedLoads.push(loadIPBlocks());
  else {
    blocks.value = [];
    blockError.value = "";
  }
  if (canViewSecurityEvents.value) privilegedLoads.push(loadEventData());
  else {
    events.value = [];
    loginEvents.value = [];
    eventError.value = "";
    selectedEvent.value = null;
  }
  await Promise.all(privilegedLoads);
}

async function loadIPBlocks() {
  if (!canViewIPBlocks.value) return;
  blockLoading.value = true;
  blockError.value = "";
  try {
    const { data } = await adminApi.get("/operations/ip-blocklist", {
      params: { page_size: 100 },
    });
    const payload = data.data;
    blocks.value = Array.isArray(payload) ? payload : payload?.items || [];
  } catch (reason: unknown) {
    blocks.value = [];
    blockError.value = message(reason, t("security.errLoad"));
  } finally {
    blockLoading.value = false;
  }
}

function eventParams() {
  return {
    page: eventPage.value,
    page_size: eventPageSize.value,
    ...(eventSearch.value.trim() ? { q: eventSearch.value.trim() } : {}),
    ...(eventRealm.value ? { realm: eventRealm.value } : {}),
    ...(eventFrom.value ? { from: eventFrom.value } : {}),
    ...(eventTo.value ? { to: eventTo.value } : {}),
  };
}

async function loadEventData() {
  if (!canViewSecurityEvents.value) return;
  eventLoading.value = true;
  eventError.value = "";
  try {
    const params: Record<string, string | number> = eventParams();
    if (eventTab.value === "security") {
      if (eventSeverity.value) params.severity = eventSeverity.value;
      if (eventResult.value) params.resolved = eventResult.value;
      const { data } = await adminApi.get("/security/events", { params });
      const payload = data.data;
      events.value = Array.isArray(payload) ? payload : payload?.items || [];
      eventTotal.value = Number(payload?.total ?? events.value.length);
    } else {
      if (eventResult.value) params.succeeded = eventResult.value;
      const { data } = await adminApi.get("/security/login-events", { params });
      const payload = data.data;
      loginEvents.value = Array.isArray(payload)
        ? payload
        : payload?.items || [];
      eventTotal.value = Number(payload?.total ?? loginEvents.value.length);
    }
  } catch (reason: unknown) {
    eventError.value = message(reason, t("security.errEventLoad"));
  } finally {
    eventLoading.value = false;
  }
}

async function applyEventFilters() {
  eventPage.value = 1;
  await loadEventData();
}

async function switchEventTab(tab: "security" | "login") {
  if (eventTab.value === tab) return;
  eventTab.value = tab;
  eventPage.value = 1;
  eventResult.value = "";
  selectedEvent.value = null;
  await loadEventData();
}

async function changeEventPage(offset: number) {
  const target = eventPage.value + offset;
  const pages = Math.max(1, Math.ceil(eventTotal.value / eventPageSize.value));
  if (target < 1 || target > pages) return;
  eventPage.value = target;
  await loadEventData();
}

function inspectEvent(event: SecurityEvent) {
  selectedEvent.value = event;
  dispositionOpen.value = false;
}

function beginDisposition() {
  if (!selectedEvent.value) return;
  dispositionConclusion.value = "";
  dispositionEvidence.value = "";
  dispositionReason.value = "";
  dispositionOpen.value = true;
}

async function saveDisposition() {
  if (!selectedEvent.value || !canManageSecurityOperations.value) return;
  if (
    dispositionConclusion.value.trim().length < 4 ||
    dispositionEvidence.value.trim().length < 4 ||
    dispositionReason.value.trim().length < 4
  ) {
    eventError.value = t("security.errDispositionFields");
    return;
  }
  busy.value = true;
  eventError.value = "";
  try {
    const { data } = await adminApi.patch(
      `/security/events/${encodeURIComponent(selectedEvent.value.id)}`,
      {
        resolved: !selectedEvent.value.resolved,
        conclusion: dispositionConclusion.value.trim(),
        evidence: dispositionEvidence.value.trim(),
      },
      { headers: { "X-Change-Reason": dispositionReason.value.trim() } },
    );
    selectedEvent.value = data.data;
    dispositionOpen.value = false;
    notice.value = selectedEvent.value?.resolved
      ? t("security.eventResolved")
      : t("security.eventReopened");
    await loadEventData();
  } catch (reason: unknown) {
    eventError.value = message(reason, t("security.errDisposition"));
  } finally {
    busy.value = false;
  }
}

function detailJSON(value: Record<string, unknown>) {
  return JSON.stringify(value || {}, null, 2);
}

async function createIPBlock() {
  if (!canManageSecurityOperations.value) return;
  error.value = "";
  notice.value = "";
  if (!blockCIDR.value.trim() || blockReason.value.trim().length < 3) {
    error.value = t("security.errBlockFields");
    return;
  }
  busy.value = true;
  try {
    await adminApi.post(
      "/security/ip-blocklist",
      {
        cidr: blockCIDR.value.trim(),
        scope: blockScope.value,
        reason: blockReason.value.trim(),
        ...(blockExpiresAt.value
          ? { expires_at: new Date(blockExpiresAt.value).toISOString() }
          : {}),
      },
      { headers: { "X-Change-Reason": blockReason.value.trim() } },
    );
    blockCIDR.value = "";
    blockReason.value = "";
    blockExpiresAt.value = "";
    notice.value = t("security.blockSaved");
    await load();
  } catch (reason: unknown) {
    error.value = message(reason, t("security.errBlockSave"));
  } finally {
    busy.value = false;
  }
}

async function toggleIPBlock(item: IPBlock) {
  if (!canManageSecurityOperations.value) return;
  error.value = "";
  notice.value = "";
  if (blockChangeReason.value.trim().length < 3) {
    error.value = t("security.errChangeReason");
    return;
  }
  busy.value = true;
  try {
    await adminApi.patch(
      `/security/ip-blocklist/${encodeURIComponent(item.id)}`,
      { enabled: !item.enabled },
      { headers: { "X-Change-Reason": blockChangeReason.value.trim() } },
    );
    notice.value = item.enabled
      ? t("security.blockDisabled")
      : t("security.blockEnabled");
    blockChangeReason.value = "";
    await load();
  } catch (reason: unknown) {
    error.value = message(reason, t("security.errBlockUpdate"));
  } finally {
    busy.value = false;
  }
}

async function beginSetup() {
  error.value = "";
  notice.value = "";
  const normalizedCode = currentCode.value.trim().toUpperCase();
  if (status.value.enabled) {
    if (
      reauthenticationMethod.value === "current_code" &&
      !/^\d{6}$|^[A-F0-9]{6}-[A-F0-9]{6}$/.test(normalizedCode)
    ) {
      error.value = t("security.errRebindInfo");
      return;
    }
    if (
      reauthenticationMethod.value === "password" &&
      (!password.value || [...password.value].length > 72)
    ) {
      error.value = t("security.errRebindInfo");
      return;
    }
  }
  busy.value = true;
  try {
    const body: Record<string, string> = {};
    // Send exactly one reauthentication factor. In particular, do not consume
    // a recovery code when the administrator intended to use their password.
    if (status.value.enabled) {
      if (reauthenticationMethod.value === "current_code") {
        body.current_code = normalizedCode;
      } else {
        body.password = password.value;
      }
    }
    const { data } = await adminApi.post("/security/2fa/setup", body);
    setup.value = data.data;
    setupVerified.value = false;
    status.value.pending_reset = Boolean(setup.value?.pending_reset);
    notice.value = setup.value?.pending_reset
      ? t("security.setupPending")
      : t("security.setupReady");
  } catch (reason: unknown) {
    error.value = message(reason, t("security.errSetup"));
  } finally {
    currentCode.value = "";
    password.value = "";
    busy.value = false;
  }
}

async function verifySetup() {
  error.value = "";
  notice.value = "";
  if (!/^\d{6}$/.test(verificationCode.value.trim())) {
    error.value = t("security.errCodeFormat");
    return;
  }
  busy.value = true;
  try {
    const { data } = await adminApi.post("/security/2fa/verify", {
      code: verificationCode.value.trim(),
    });
    verificationCode.value = "";
    status.value.enabled = Boolean(data.data?.enabled);
    status.value.pending_reset = false;
    status.value.verified_at = new Date().toISOString();
    setupVerified.value = true;
    notice.value = Boolean(data.data?.reset)
      ? t("security.rebindDone")
      : t("security.enabledDone");
  } catch (reason: unknown) {
    error.value = message(reason, t("security.errVerify"));
  } finally {
    busy.value = false;
  }
}

async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    const field = document.createElement("textarea");
    field.value = value;
    field.style.position = "fixed";
    field.style.opacity = "0";
    document.body.appendChild(field);
    field.select();
    document.execCommand("copy");
    field.remove();
  }
  notice.value = t("security.copied");
}

function downloadRecoveryCodes() {
  if (!setup.value?.recovery_codes.length) return;
  const blob = new Blob(
    [
      `${t("security.downloadHeader")}\n`,
      `${t("security.downloadHint")}\n\n`,
      setup.value.recovery_codes.join("\n"),
    ],
    { type: "text/plain;charset=utf-8" },
  );
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `linlinqi-recovery-codes-${new Date().toISOString().slice(0, 10)}.txt`;
  link.click();
  URL.revokeObjectURL(url);
}

async function revokeAllSessions() {
  error.value = "";
  notice.value = "";
  if (revokeConfirmation.value !== "REVOKE") {
    error.value = t("security.errRevokeConfirm");
    return;
  }
  busy.value = true;
  try {
    const { data } = await adminApi.post("/security/sessions/revoke-all");
    if (data.data?.revoked !== true) {
      throw new Error("admin session revocation was not acknowledged");
    }
    clearSetupSecrets();
    currentCode.value = "";
    password.value = "";
    revokeConfirmation.value = "";
    auth.logout();
    delete adminApi.defaults.headers.common.Authorization;
    await router.replace("/login");
  } catch (reason: unknown) {
    error.value = message(reason, t("security.errRevokeAll"));
  } finally {
    busy.value = false;
  }
}

const time = (value?: string | null) =>
  value ? new Date(value).toLocaleString(locale.value, { hour12: false }) : "—";

onMounted(load);
onBeforeUnmount(clearSetupSecrets);
</script>

<template>
  <div class="security-console">
    <p v-if="error" class="security-message error">{{ error }}</p>
    <p v-if="notice" class="security-message success">{{ notice }}</p>
    <p v-if="loading" class="security-message">{{ t("security.loading") }}</p>

    <div v-else class="security-grid">
      <section class="panel security-card">
        <header>
          <span class="security-icon"><ShieldCheck /></span>
          <div>
            <p>ADMIN 2FA</p>
            <h2>{{ t("security.twofa") }}</h2>
          </div>
          <b :class="status.enabled ? 'enabled' : 'warning'">
            {{
              status.enabled
                ? t("security.twofaStatus.enabled")
                : t("security.twofaStatus.disabled")
            }}
          </b>
        </header>
        <p class="security-description">
          {{ t("security.twofaDesc") }}
        </p>
        <div v-if="status.enabled" class="security-meta">
          <span>{{ t("security.lastVerified") }}</span
          ><b>{{ time(status.verified_at) }}</b>
          <span>{{ t("security.pendingRebind") }}</span
          ><b>{{
            status.pending_reset
              ? t("security.pendingYes")
              : t("security.pendingNo")
          }}</b>
        </div>

        <template v-if="status.enabled && !setup">
          <div
            class="reauth-methods"
            role="group"
            :aria-label="t('security.errRebindInfo')"
          >
            <button
              type="button"
              :class="{
                active: reauthenticationMethod === 'current_code',
              }"
              :disabled="busy"
              @click="selectReauthenticationMethod('current_code')"
            >
              {{ t("security.currentCode") }}
            </button>
            <button
              type="button"
              :class="{ active: reauthenticationMethod === 'password' }"
              :disabled="busy"
              @click="selectReauthenticationMethod('password')"
            >
              {{ t("security.orPassword") }}
            </button>
          </div>
          <div class="reauth-field">
            <label v-if="reauthenticationMethod === 'current_code'">
              {{ t("security.currentCode") }}
              <input
                v-model="currentCode"
                autocomplete="one-time-code"
                autocapitalize="characters"
                maxlength="13"
                spellcheck="false"
                :placeholder="t('security.currentCodePlaceholder')"
                @keyup.enter="beginSetup"
              />
            </label>
            <label v-else>
              {{ t("security.orPassword") }}
              <input
                v-model="password"
                type="password"
                autocomplete="current-password"
                maxlength="72"
                :placeholder="t('security.passwordPlaceholder')"
                @keyup.enter="beginSetup"
              />
            </label>
          </div>
        </template>

        <button
          v-if="!setup"
          type="button"
          class="primary-button"
          :disabled="busy"
          @click="beginSetup"
        >
          <KeyRound :size="16" />
          {{
            status.enabled
              ? t("security.rebindAuthenticator")
              : t("security.enable2fa")
          }}
        </button>

        <div v-else class="totp-setup">
          <div class="secret-field">
            <span>{{ t("security.manualSecret") }}</span
            ><code>{{ setup.secret }}</code>
            <button
              type="button"
              :aria-label="t('common.copy')"
              @click="copy(setup.secret)"
            >
              <Clipboard :size="15" />
            </button>
          </div>
          <details>
            <summary>{{ t("security.viewUri") }}</summary>
            <code>{{ setup.provisioning_uri }}</code>
          </details>
          <label v-if="!setupVerified">
            {{ t("security.verifyNewSecret") }}
            <div class="verify-row">
              <input
                v-model="verificationCode"
                inputmode="numeric"
                autocomplete="one-time-code"
                maxlength="6"
                :placeholder="t('security.codePlaceholder')"
                @keyup.enter="verifySetup"
              />
              <button
                type="button"
                class="primary-button"
                :disabled="busy"
                @click="verifySetup"
              >
                <Check :size="15" />{{ t("security.completeVerification") }}
              </button>
            </div>
          </label>
          <div class="recovery-panel">
            <div>
              <b>{{ t("security.recoveryCodes") }}</b>
              <span>{{ t("security.recoveryHint") }}</span>
            </div>
            <div class="recovery-codes">
              <code v-for="code in setup.recovery_codes" :key="code">{{
                code
              }}</code>
            </div>
            <button
              type="button"
              class="secondary-button"
              @click="downloadRecoveryCodes"
            >
              <Download :size="15" />{{ t("security.downloadCodes") }}
            </button>
          </div>
          <button
            v-if="setupVerified"
            type="button"
            class="secondary-button setup-close"
            @click="clearSetupSecrets"
          >
            <X :size="15" />{{ t("security.close") }}
          </button>
        </div>
      </section>

      <section class="panel security-card danger-card">
        <header>
          <span class="security-icon danger"><ShieldX /></span>
          <div>
            <p>{{ t("adminKicker.sessionControl") }}</p>
            <h2>{{ t("security.sessionControl") }}</h2>
          </div>
        </header>
        <p class="security-description">
          {{ t("security.sessionDesc") }}
        </p>
        <label
          ><span
            >{{ revokePrompt.before }}<code>REVOKE</code
            >{{ revokePrompt.after }}</span
          >
          <input v-model="revokeConfirmation" autocomplete="off" />
        </label>
        <button
          type="button"
          class="danger-button"
          :disabled="busy || revokeConfirmation !== 'REVOKE'"
          @click="revokeAllSessions"
        >
          <RefreshCw :size="15" />{{ t("security.revokeAll") }}
        </button>
      </section>
    </div>

    <section v-if="canViewSecurityEvents" class="panel security-events">
      <header class="event-header">
        <div>
          <p>{{ t("adminKicker.securityOperations") }}</p>
          <h2>{{ t("security.eventOperations") }}</h2>
        </div>
        <nav class="event-tabs" :aria-label="t('security.eventTabs')">
          <button
            type="button"
            :class="{ active: eventTab === 'security' }"
            @click="switchEventTab('security')"
          >
            <ShieldCheck :size="14" />{{ t("security.securityEvents") }}
          </button>
          <button
            type="button"
            :class="{ active: eventTab === 'login' }"
            @click="switchEventTab('login')"
          >
            <History :size="14" />{{ t("security.loginAudit") }}
          </button>
        </nav>
        <button
          type="button"
          class="secondary-button"
          :disabled="eventLoading"
          @click="loadEventData"
        >
          <RefreshCw :size="14" />{{ t("security.refresh") }}
        </button>
      </header>

      <form class="event-filters" @submit.prevent="applyEventFilters">
        <label class="event-search">
          <Search :size="15" />
          <input
            v-model="eventSearch"
            maxlength="100"
            :placeholder="t('security.eventSearchPlaceholder')"
          />
        </label>
        <select v-model="eventRealm" :aria-label="t('security.realm')">
          <option value="">{{ t("security.allRealms") }}</option>
          <option value="admin">{{ t("security.realmAdmin") }}</option>
          <option value="user">{{ t("security.realmUser") }}</option>
          <option v-if="eventTab === 'security'" value="openapi">
            OPENAPI
          </option>
          <option v-if="eventTab === 'security'" value="system">
            {{ t("security.realmSystem") }}
          </option>
        </select>
        <select
          v-if="eventTab === 'security'"
          v-model="eventSeverity"
          :aria-label="t('security.severity')"
        >
          <option value="">{{ t("security.allSeverity") }}</option>
          <option value="critical">{{ t("security.severityCritical") }}</option>
          <option value="high">{{ t("security.severityHigh") }}</option>
          <option value="warning">{{ t("security.severityWarning") }}</option>
          <option value="medium">{{ t("security.severityMedium") }}</option>
          <option value="low">{{ t("security.severityLow") }}</option>
          <option value="info">{{ t("security.severityInfo") }}</option>
        </select>
        <select v-model="eventResult" :aria-label="t('security.status')">
          <option value="">{{ t("security.allResults") }}</option>
          <template v-if="eventTab === 'security'">
            <option value="false">{{ t("security.pending") }}</option>
            <option value="true">{{ t("security.resolved") }}</option>
          </template>
          <template v-else>
            <option value="true">{{ t("security.loginSucceeded") }}</option>
            <option value="false">{{ t("security.loginFailed") }}</option>
          </template>
        </select>
        <input
          v-model="eventFrom"
          type="date"
          :aria-label="t('security.from')"
        />
        <input v-model="eventTo" type="date" :aria-label="t('security.to')" />
        <button class="primary-button" type="submit" :disabled="eventLoading">
          {{ t("security.search") }}
        </button>
      </form>

      <p v-if="eventError" class="event-error">{{ eventError }}</p>
      <div class="event-layout">
        <div class="table-wrap">
          <table class="data-table">
            <thead>
              <tr v-if="eventTab === 'security'">
                <th>{{ t("security.event") }}</th>
                <th>{{ t("security.severity") }}</th>
                <th>{{ t("security.realm") }}</th>
                <th>{{ t("security.ip") }}</th>
                <th>{{ t("security.time") }}</th>
                <th>{{ t("security.status") }}</th>
                <th>{{ t("security.action") }}</th>
              </tr>
              <tr v-else>
                <th>{{ t("security.account") }}</th>
                <th>{{ t("security.realm") }}</th>
                <th>{{ t("security.ipLocation") }}</th>
                <th>{{ t("security.agent") }}</th>
                <th>{{ t("security.time") }}</th>
                <th>{{ t("security.result") }}</th>
              </tr>
            </thead>
            <tbody v-if="eventTab === 'security'">
              <tr v-for="event in events" :key="event.id">
                <td>
                  <b>{{ event.event_type }}</b>
                </td>
                <td>
                  <span class="event-badge">{{ event.severity }}</span>
                </td>
                <td>{{ event.realm }}</td>
                <td>
                  <code>{{ event.ip || "—" }}</code>
                </td>
                <td>{{ time(event.created_at) }}</td>
                <td>
                  {{
                    event.resolved
                      ? t("security.resolved")
                      : t("security.pending")
                  }}
                </td>
                <td>
                  <button
                    type="button"
                    class="row-action"
                    @click="inspectEvent(event)"
                  >
                    <Eye :size="14" />{{ t("security.inspect") }}
                  </button>
                </td>
              </tr>
              <tr v-if="!events.length && !eventLoading">
                <td colspan="7" class="empty-cell">
                  {{ t("security.noEvents") }}
                </td>
              </tr>
            </tbody>
            <tbody v-else>
              <tr v-for="event in loginEvents" :key="event.id">
                <td>
                  <b>{{ event.account || "—" }}</b
                  ><small>{{ event.reason || "—" }}</small>
                </td>
                <td>{{ event.realm }}</td>
                <td>
                  <code>{{ event.ip || "—" }}</code
                  ><small>{{
                    [event.country, event.city].filter(Boolean).join(" · ") ||
                    "—"
                  }}</small>
                </td>
                <td class="event-agent">{{ event.user_agent || "—" }}</td>
                <td>{{ time(event.created_at) }}</td>
                <td :class="event.succeeded ? 'login-ok' : 'login-failed'">
                  {{
                    event.succeeded
                      ? t("security.loginSucceeded")
                      : t("security.loginFailed")
                  }}
                </td>
              </tr>
              <tr v-if="!loginEvents.length && !eventLoading">
                <td colspan="6" class="empty-cell">
                  {{ t("security.noLoginEvents") }}
                </td>
              </tr>
            </tbody>
          </table>
          <p v-if="eventLoading" class="empty-cell">
            {{ t("security.loadingEvents") }}
          </p>
        </div>

        <aside v-if="selectedEvent" class="event-inspector">
          <header>
            <div>
              <span>{{ selectedEvent.severity }}</span>
              <h3>{{ selectedEvent.event_type }}</h3>
            </div>
            <button
              type="button"
              :aria-label="t('security.close')"
              @click="selectedEvent = null"
            >
              <X :size="16" />
            </button>
          </header>
          <dl>
            <dt>{{ t("security.realm") }}</dt>
            <dd>{{ selectedEvent.realm }}</dd>
            <dt>{{ t("security.principal") }}</dt>
            <dd>
              <code>{{ selectedEvent.principal_id || "—" }}</code>
            </dd>
            <dt>{{ t("security.ip") }}</dt>
            <dd>
              <code>{{ selectedEvent.ip || "—" }}</code>
            </dd>
            <dt>{{ t("security.agent") }}</dt>
            <dd>{{ selectedEvent.user_agent || "—" }}</dd>
            <dt>{{ t("security.time") }}</dt>
            <dd>{{ time(selectedEvent.created_at) }}</dd>
            <dt>{{ t("security.resolvedBy") }}</dt>
            <dd>
              {{
                selectedEvent.resolved_by_name ||
                selectedEvent.resolved_by ||
                "—"
              }}
            </dd>
            <dt>{{ t("security.resolvedAt") }}</dt>
            <dd>{{ time(selectedEvent.resolved_at) }}</dd>
          </dl>
          <details open>
            <summary>{{ t("security.eventDetails") }}</summary>
            <pre>{{ detailJSON(selectedEvent.details) }}</pre>
          </details>
          <button
            v-if="canManageSecurityOperations"
            type="button"
            class="primary-button disposition-button"
            @click="beginDisposition"
          >
            {{
              selectedEvent.resolved
                ? t("security.reopenEvent")
                : t("security.resolveEvent")
            }}
          </button>
          <form
            v-if="dispositionOpen && canManageSecurityOperations"
            class="disposition-form"
            @submit.prevent="saveDisposition"
          >
            <label
              >{{ t("security.conclusion")
              }}<input v-model="dispositionConclusion" maxlength="500"
            /></label>
            <label
              >{{ t("security.evidence")
              }}<textarea
                v-model="dispositionEvidence"
                rows="4"
                maxlength="2000"
              ></textarea>
            </label>
            <label
              >{{ t("security.auditReason")
              }}<input v-model="dispositionReason" maxlength="500"
            /></label>
            <div>
              <button type="button" @click="dispositionOpen = false">
                {{ t("common.cancel") }}</button
              ><button class="primary-button" :disabled="busy" type="submit">
                {{ t("common.save") }}
              </button>
            </div>
          </form>
        </aside>
      </div>
      <footer class="event-pager">
        <span>{{ t("security.eventTotal", { total: eventTotal }) }}</span>
        <div>
          <button
            :disabled="eventPage <= 1 || eventLoading"
            @click="changeEventPage(-1)"
          >
            {{ t("common.prev") }}</button
          ><b
            >{{ eventPage }} /
            {{ Math.max(1, Math.ceil(eventTotal / eventPageSize)) }}</b
          ><button
            :disabled="
              eventPage >= Math.ceil(eventTotal / eventPageSize) || eventLoading
            "
            @click="changeEventPage(1)"
          >
            {{ t("common.next") }}
          </button>
        </div>
      </footer>
    </section>

    <section v-if="canViewIPBlocks" class="panel blocklist-panel">
      <header>
        <div>
          <p>{{ t("adminKicker.networkPolicy") }}</p>
          <h2>{{ t("security.blocklist") }}</h2>
        </div>
        <span>{{ t("security.cacheHint") }}</span>
      </header>
      <div v-if="canManageSecurityOperations" class="blocklist-form">
        <label
          >{{ t("security.cidr") }}
          <input
            v-model="blockCIDR"
            :placeholder="t('security.cidrPlaceholder')"
          />
        </label>
        <label
          >{{ t("security.scope") }}
          <select v-model="blockScope">
            <option value="public">{{ t("security.scopePublic") }}</option>
            <option value="openapi">{{ t("security.scopeOpenapi") }}</option>
            <option value="admin">{{ t("security.scopeAdmin") }}</option>
            <option value="all">{{ t("security.scopeAll") }}</option>
          </select>
        </label>
        <label
          >{{ t("security.expiresAt") }}
          <input v-model="blockExpiresAt" type="datetime-local" />
        </label>
        <label class="block-reason"
          >{{ t("security.reason") }}
          <input
            v-model="blockReason"
            maxlength="500"
            :placeholder="t('security.reasonPlaceholder')"
          />
        </label>
        <button
          type="button"
          class="primary-button"
          :disabled="busy"
          @click="createIPBlock"
        >
          {{ t("security.saveBlock") }}
        </button>
      </div>
      <div v-if="canManageSecurityOperations" class="blocklist-change-reason">
        <span>{{ t("security.changeReason") }}</span>
        <input
          v-model="blockChangeReason"
          maxlength="500"
          :placeholder="t('security.changeReasonPlaceholder')"
        />
      </div>
      <p v-if="blockError" class="event-error">{{ blockError }}</p>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>{{ t("security.network") }}</th>
              <th>{{ t("security.colScope") }}</th>
              <th>{{ t("security.colReason") }}</th>
              <th>{{ t("security.colExpiresAt") }}</th>
              <th>{{ t("security.source") }}</th>
              <th>{{ t("security.status") }}</th>
              <th v-if="canManageSecurityOperations">
                {{ t("security.action") }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in blocks" :key="item.id">
              <td>
                <code>{{ item.cidr }}</code>
              </td>
              <td>{{ item.scope }}</td>
              <td class="block-reason-cell">{{ item.reason }}</td>
              <td>{{ time(item.expires_at) }}</td>
              <td>{{ item.source }}</td>
              <td>
                {{
                  item.enabled ? t("security.enabled") : t("security.disabled")
                }}
              </td>
              <td v-if="canManageSecurityOperations">
                <button
                  type="button"
                  class="secondary-button"
                  :disabled="busy"
                  @click="toggleIPBlock(item)"
                >
                  {{
                    item.enabled
                      ? t("security.disabled")
                      : t("security.enabled")
                  }}
                </button>
              </td>
            </tr>
            <tr v-if="!blocks.length">
              <td
                :colspan="canManageSecurityOperations ? 7 : 6"
                class="empty-cell"
              >
                {{
                  blockLoading ? t("security.loading") : t("security.noBlocks")
                }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>

<style scoped>
.reauth-methods {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 4px;
  margin-bottom: 10px;
  padding: 3px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-2);
}
.reauth-methods button {
  min-height: 34px;
  border: 1px solid transparent;
  border-radius: 5px;
  background: transparent;
  color: var(--muted);
  font-size: 9px;
}
.reauth-methods button.active {
  border-color: var(--line);
  background: var(--surface);
  color: var(--text);
  box-shadow: var(--shadow-sm);
}
.reauth-field {
  margin-bottom: 14px;
}
.setup-close {
  width: 100%;
}
.event-header {
  gap: 12px;
  flex-wrap: wrap;
}
.event-tabs {
  display: flex;
  gap: 4px;
  margin-left: auto;
  padding: 3px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface-2);
}
.event-tabs button,
.row-action,
.event-pager button,
.event-inspector header button,
.disposition-form button {
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--text);
}
.event-tabs button {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 10px;
  border-color: transparent;
  font-size: 9px;
}
.event-tabs button.active {
  border-color: var(--line);
  box-shadow: var(--shadow-sm);
}
.event-filters {
  display: grid;
  grid-template-columns:
    minmax(190px, 1.4fr) repeat(5, minmax(100px, 0.7fr))
    auto;
  gap: 8px;
  padding: 12px 14px;
  border-block: 1px solid var(--line);
}
.event-filters input,
.event-filters select {
  min-width: 0;
  height: 36px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface-2);
  color: var(--text);
  padding: 0 9px;
  font-size: 9px;
}
.event-search {
  position: relative;
}
.event-search svg {
  position: absolute;
  z-index: 1;
  left: 10px;
  top: 10px;
  color: var(--muted);
}
.event-search input {
  width: 100%;
  padding-left: 31px;
}
.event-error {
  margin: 0;
  padding: 9px 14px;
  border-bottom: 1px solid var(--line);
  color: var(--danger);
  font-size: 9px;
}
.event-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
}
.event-layout td small {
  display: block;
  max-width: 260px;
  margin-top: 4px;
  color: var(--muted);
  font-size: 8px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.event-badge {
  padding: 3px 6px;
  border: 1px solid var(--line);
  border-radius: 999px;
  font-size: 8px;
  text-transform: uppercase;
}
.row-action {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 6px 8px;
  font-size: 9px;
  white-space: nowrap;
}
.login-ok {
  color: var(--success);
}
.login-failed {
  color: var(--danger);
}
.event-inspector {
  width: 360px;
  padding: 14px;
  border-left: 1px solid var(--line);
  background: var(--surface-2);
}
.event-inspector header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}
.event-inspector header span {
  color: var(--muted);
  font-size: 8px;
  text-transform: uppercase;
}
.event-inspector h3 {
  margin: 3px 0 0;
  overflow-wrap: anywhere;
  font-size: 14px;
}
.event-inspector header button {
  display: grid;
  width: 30px;
  height: 30px;
  place-items: center;
}
.event-inspector dl {
  display: grid;
  grid-template-columns: 90px minmax(0, 1fr);
  gap: 8px;
  margin: 14px 0;
  font-size: 9px;
}
.event-inspector dt {
  color: var(--muted);
}
.event-inspector dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}
.event-inspector details {
  border: 1px solid var(--line);
  border-radius: 7px;
  padding: 9px;
  font-size: 9px;
}
.event-inspector pre {
  max-height: 260px;
  margin: 9px 0 0;
  padding: 9px;
  overflow: auto;
  background: var(--surface);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 8px;
}
.disposition-button {
  width: 100%;
  margin-top: 12px;
}
.disposition-form {
  display: grid;
  gap: 9px;
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px solid var(--line);
}
.disposition-form label {
  display: grid;
  gap: 5px;
  color: var(--muted);
  font-size: 9px;
}
.disposition-form input,
.disposition-form textarea {
  width: 100%;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--text);
  padding: 8px;
}
.disposition-form > div,
.event-pager,
.event-pager > div {
  display: flex;
  align-items: center;
  gap: 8px;
}
.disposition-form > div {
  justify-content: flex-end;
}
.disposition-form button,
.event-pager button {
  min-height: 32px;
  padding: 0 10px;
  font-size: 9px;
}
.event-pager {
  justify-content: space-between;
  padding: 10px 14px;
  border-top: 1px solid var(--line);
  color: var(--muted);
  font-size: 9px;
}
@media (max-width: 1180px) {
  .event-filters {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  .event-search {
    grid-column: span 2;
  }
}
@media (max-width: 800px) {
  .event-tabs {
    width: 100%;
    order: 3;
    margin-left: 0;
  }
  .event-tabs button {
    flex: 1;
    justify-content: center;
  }
  .event-filters {
    grid-template-columns: 1fr 1fr;
  }
  .event-search {
    grid-column: 1 / -1;
  }
  .event-layout {
    grid-template-columns: 1fr;
  }
  .event-inspector {
    width: auto;
    border-top: 1px solid var(--line);
    border-left: 0;
  }
}
@media (max-width: 520px) {
  .event-filters {
    grid-template-columns: 1fr;
  }
  .event-search {
    grid-column: auto;
  }
  .event-pager {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
