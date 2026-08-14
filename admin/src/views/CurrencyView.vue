<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  Activity,
  AlertCircle,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleDollarSign,
  Database,
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  ServerCog,
  SlidersHorizontal,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import { safeAdminHTTPURL } from "../utils/publicUrl";

type Tab = "currencies" | "providers" | "manual" | "snapshots";

interface CurrencyDefinition {
  id: string;
  code: string;
  numeric_code: string;
  name: string;
  symbol: string;
  minor_unit: number;
  enabled: boolean;
  settlement: boolean;
  display_sort: number;
  updated_at: string;
}

interface FXProvider {
  id: string;
  code: string;
  name: string;
  driver: string;
  provider_key?: string;
  base_url: string;
  priority: number;
  enabled: boolean;
  timeout_seconds: number;
  failure_count: number;
  last_success_at?: string;
  last_failure_at?: string;
  has_error: boolean;
}

interface ManualRate {
  id: string;
  base_code: string;
  quote_code: string;
  rate: string;
  enabled: boolean;
  valid_from: string;
  valid_to?: string | null;
  reason: string;
  updated_at: string;
}

interface FXSnapshot {
  id: string;
  base_code: string;
  quote_code: string;
  rate: string;
  source_tier: string;
  provider_code?: string;
  observed_at: string;
  selected_at: string;
  expires_at: string;
  stale_after: string;
  consensus_count: number;
  decision: string;
}

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface ManualRateForm {
  id: string;
  baseCode: string;
  quoteCode: string;
  rate: string;
  enabled: boolean;
  validFrom: string;
  validTo: string;
  reason: string;
  auditReason: string;
}

const { t, te } = useI18n();
const route = useRoute();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("system.manage"));
const activeTab = ref<Tab>("currencies");
const currencies = ref<CurrencyDefinition[]>([]);
const storeCurrency = ref("CNY");
const storeCurrencyDraft = ref("CNY");
const storeCurrencyReason = ref("");
const storeCurrencySaving = ref(false);
const providers = ref<FXProvider[]>([]);
const manualRates = ref<ManualRate[]>([]);
const snapshots = ref<FXSnapshot[]>([]);
const currencyDrafts = ref<
  Record<
    string,
    Pick<CurrencyDefinition, "enabled" | "minor_unit" | "display_sort">
  >
>({});
const providerDrafts = ref<
  Record<string, Pick<FXProvider, "enabled" | "priority" | "timeout_seconds">>
>({});
const loading = ref(false);
const savingID = ref("");
const notice = ref("");
const errorMessage = ref("");
const changeReason = ref("");
const page = ref(1);
const pageSize = ref(20);
const total = ref(0);
const manualOpen = ref(false);
const manualSaving = ref(false);
const filterBase = ref("");
const filterQuote = ref("");
const filterEnabled = ref("");
const filterTier = ref("");
const selectedFrom = ref("");
const selectedTo = ref("");
const refreshBase = ref("USD");
const refreshQuote = ref("CNY");
const refreshReason = ref("");
const refreshing = ref(false);
const exactDecimal = /^(?:0|[1-9][0-9]{0,19})(?:\.[0-9]{1,18})?$/;

const manualForm = ref<ManualRateForm>(emptyManualForm());
const enabledCurrencies = computed(() =>
  currencies.value.filter((item) => item.enabled),
);
const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const activeProviders = computed(
  () => providers.value.filter((item) => item.enabled).length,
);
const healthyProviders = computed(
  () =>
    providers.value.filter(
      (item) => item.enabled && !item.has_error && item.last_success_at,
    ).length,
);

function emptyManualForm(): ManualRateForm {
  const start = new Date(Date.now() - new Date().getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 16);
  return {
    id: "",
    baseCode: "USD",
    quoteCode: "CNY",
    rate: "",
    enabled: true,
    validFrom: start,
    validTo: "",
    reason: "",
    auditReason: "",
  };
}

function message(error: unknown, fallback: string) {
  const candidate = error as {
    response?: { data?: { message?: string } };
    message?: string;
  };
  const value = candidate.response?.data?.message || candidate.message || "";
  if (value && te(value)) return t(value);
  if (value.startsWith("error.")) return fallback;
  return value || fallback;
}

function validReason(value: string) {
  const length = Array.from(value.trim()).length;
  return length >= 4 && length <= 500;
}

function isoTime(value: string) {
  if (!value) return undefined;
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? undefined : parsed.toISOString();
}

function dateTime(value?: string | null) {
  if (!value) return "—";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime())
    ? "—"
    : parsed.toLocaleString(undefined, { hour12: false });
}

function toLocalInput(value?: string | null) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 16);
}

function pagePayload<T>(data: unknown): PagePayload<T> {
  const payload = (data as { data?: Partial<PagePayload<T>> })?.data || {};
  return {
    items: Array.isArray(payload.items) ? payload.items : [],
    total: Number(payload.total || 0),
    page: Number(payload.page || 1),
    page_size: Number(payload.page_size || pageSize.value),
  };
}

async function loadCurrencies() {
  const { data } = await adminApi.get("/currencies");
  const payload = data?.data || {};
  currencies.value = Array.isArray(payload.items) ? payload.items : [];
  storeCurrency.value = String(payload.store_currency || "CNY");
  storeCurrencyDraft.value = storeCurrency.value;
  currencyDrafts.value = Object.fromEntries(
    currencies.value.map((item) => [
      item.code,
      {
        enabled: item.enabled,
        minor_unit: item.minor_unit,
        display_sort: item.display_sort,
      },
    ]),
  );
}

async function saveStoreCurrency() {
  if (!canManage.value) return;
  if (
    storeCurrencyDraft.value === storeCurrency.value ||
    !validReason(storeCurrencyReason.value)
  ) {
    errorMessage.value =
      storeCurrencyDraft.value === storeCurrency.value
        ? t("settings.errNoChanges")
        : t("currency.errorReason");
    return;
  }
  storeCurrencySaving.value = true;
  errorMessage.value = "";
  notice.value = "";
  try {
    await adminApi.put(
      "/settings",
      { store_currency: storeCurrencyDraft.value },
      { headers: { "X-Change-Reason": storeCurrencyReason.value.trim() } },
    );
    notice.value = t("currency.noticeCurrencySaved", {
      code: storeCurrencyDraft.value,
    });
    storeCurrencyReason.value = "";
    await loadCurrencies();
  } catch (error) {
    errorMessage.value = message(error, t("currency.errorCurrencySave"));
  } finally {
    storeCurrencySaving.value = false;
  }
}

async function loadProviders() {
  const { data } = await adminApi.get("/fx/providers");
  providers.value = Array.isArray(data?.data?.items) ? data.data.items : [];
  providerDrafts.value = Object.fromEntries(
    providers.value.map((item) => [
      item.id,
      {
        enabled: item.enabled,
        priority: item.priority,
        timeout_seconds: item.timeout_seconds,
      },
    ]),
  );
}

async function loadPaged() {
  const params: Record<string, string | number> = {
    page: page.value,
    page_size: pageSize.value,
  };
  if (filterBase.value) params.base_code = filterBase.value;
  if (filterQuote.value) params.quote_code = filterQuote.value;
  if (activeTab.value === "manual" && filterEnabled.value)
    params.enabled = filterEnabled.value;
  if (activeTab.value === "snapshots") {
    if (filterTier.value) params.source_tier = filterTier.value;
    const from = isoTime(selectedFrom.value);
    const to = isoTime(selectedTo.value);
    if (from) params.selected_from = from;
    if (to) params.selected_to = to;
  }
  const endpoint =
    activeTab.value === "manual" ? "/fx/manual-rates" : "/fx/snapshots";
  const { data } = await adminApi.get(endpoint, { params });
  const payload = pagePayload<ManualRate | FXSnapshot>(data);
  page.value = payload.page;
  pageSize.value = payload.page_size;
  total.value = payload.total;
  if (activeTab.value === "manual")
    manualRates.value = payload.items as ManualRate[];
  else snapshots.value = payload.items as FXSnapshot[];
}

async function load() {
  loading.value = true;
  errorMessage.value = "";
  try {
    const directoryTasks: Promise<void>[] = [];
    if (!currencies.value.length || activeTab.value === "currencies")
      directoryTasks.push(loadCurrencies());
    if (!providers.value.length || activeTab.value === "providers")
      directoryTasks.push(loadProviders());
    await Promise.all(directoryTasks);
    if (activeTab.value === "manual" || activeTab.value === "snapshots")
      await loadPaged();
  } catch (error) {
    errorMessage.value = message(error, t("currency.errorLoad"));
  } finally {
    loading.value = false;
  }
}

async function saveCurrency(item: CurrencyDefinition) {
  if (!canManage.value) return;
  const draft = currencyDrafts.value[item.code];
  if (!draft || !validReason(changeReason.value)) {
    errorMessage.value = t("currency.errorReason");
    return;
  }
  savingID.value = item.code;
  errorMessage.value = "";
  try {
    await adminApi.patch(
      `/currencies/${encodeURIComponent(item.code)}`,
      draft,
      {
        headers: { "X-Change-Reason": changeReason.value.trim() },
      },
    );
    notice.value = t("currency.noticeCurrencySaved", { code: item.code });
    changeReason.value = "";
    await loadCurrencies();
  } catch (error) {
    errorMessage.value = message(error, t("currency.errorCurrencySave"));
  } finally {
    savingID.value = "";
  }
}

async function saveProvider(item: FXProvider) {
  if (!canManage.value) return;
  const draft = providerDrafts.value[item.id];
  if (!draft || !validReason(changeReason.value)) {
    errorMessage.value = t("currency.errorReason");
    return;
  }
  savingID.value = item.id;
  errorMessage.value = "";
  try {
    await adminApi.patch(
      `/fx/providers/${encodeURIComponent(item.id)}`,
      draft,
      {
        headers: { "X-Change-Reason": changeReason.value.trim() },
      },
    );
    notice.value = t("currency.noticeProviderSaved", { name: item.name });
    changeReason.value = "";
    await loadProviders();
  } catch (error) {
    errorMessage.value = message(error, t("currency.errorProviderSave"));
  } finally {
    savingID.value = "";
  }
}

function openManual(item?: ManualRate) {
  if (!canManage.value) return;
  manualForm.value = item
    ? {
        id: item.id,
        baseCode: item.base_code,
        quoteCode: item.quote_code,
        rate: item.rate,
        enabled: item.enabled,
        validFrom: toLocalInput(item.valid_from),
        validTo: toLocalInput(item.valid_to),
        reason: item.reason,
        auditReason: "",
      }
    : emptyManualForm();
  errorMessage.value = "";
  manualOpen.value = true;
}

async function saveManual() {
  if (!canManage.value) return;
  const form = manualForm.value;
  if (
    form.baseCode === form.quoteCode ||
    !exactDecimal.test(form.rate.trim()) ||
    !isoTime(form.validFrom) ||
    (form.validTo && !isoTime(form.validTo)) ||
    !validReason(form.reason) ||
    !validReason(form.auditReason)
  ) {
    errorMessage.value = t("currency.errorManualValidation");
    return;
  }
  manualSaving.value = true;
  errorMessage.value = "";
  const payload = {
    rate: form.rate.trim(),
    enabled: form.enabled,
    valid_from: isoTime(form.validFrom),
    valid_to: form.validTo ? isoTime(form.validTo) : null,
    reason: form.reason.trim(),
  };
  try {
    const config = { headers: { "X-Change-Reason": form.auditReason.trim() } };
    if (form.id) {
      await adminApi.patch(
        `/fx/manual-rates/${encodeURIComponent(form.id)}`,
        payload,
        config,
      );
    } else {
      await adminApi.post(
        "/fx/manual-rates",
        { ...payload, base_code: form.baseCode, quote_code: form.quoteCode },
        config,
      );
    }
    notice.value = t(
      form.id ? "currency.noticeManualUpdated" : "currency.noticeManualCreated",
    );
    manualOpen.value = false;
    await loadPaged();
  } catch (error) {
    errorMessage.value = message(error, t("currency.errorManualSave"));
  } finally {
    manualSaving.value = false;
  }
}

async function refreshRate() {
  if (!canManage.value) return;
  if (
    refreshBase.value === refreshQuote.value ||
    !validReason(refreshReason.value)
  ) {
    errorMessage.value = t("currency.errorRefreshValidation");
    return;
  }
  refreshing.value = true;
  errorMessage.value = "";
  try {
    const { data } = await adminApi.post(
      "/fx/refresh",
      { base_code: refreshBase.value, quote_code: refreshQuote.value },
      { headers: { "X-Change-Reason": refreshReason.value.trim() } },
    );
    const snapshot = data?.data as FXSnapshot;
    notice.value = t("currency.noticeRefreshed", {
      pair: `${snapshot.base_code}/${snapshot.quote_code}`,
      rate: snapshot.rate,
      tier: t(`currency.tier.${snapshot.source_tier}`),
    });
    refreshReason.value = "";
    if (activeTab.value === "snapshots") await loadPaged();
  } catch (error) {
    errorMessage.value = message(error, t("currency.errorRefresh"));
  } finally {
    refreshing.value = false;
  }
}

function applyFilters() {
  page.value = 1;
  void loadPaged();
}

function changePage(next: number) {
  if (next < 1 || next > totalPages.value) return;
  page.value = next;
  void loadPaged();
}

watch(
  () => [route.path, route.meta.defaultTab] as const,
  async ([, defaultTab]) => {
    const requested = String(defaultTab || "currencies") as Tab;
    activeTab.value = [
      "currencies",
      "providers",
      "manual",
      "snapshots",
    ].includes(requested)
      ? requested
      : "currencies";
    page.value = 1;
    total.value = 0;
    errorMessage.value = "";
    notice.value = "";
    await load();
  },
  { immediate: true },
);
</script>

<template>
  <section class="currency-view">
    <header class="currency-summary">
      <article>
        <CircleDollarSign :size="20" />
        <div>
          <span>{{ t("currency.storeCurrency") }}</span
          ><strong>{{ storeCurrency }}</strong>
        </div>
      </article>
      <article>
        <ServerCog :size="20" />
        <div>
          <span>{{ t("currency.providerHealth") }}</span
          ><strong>{{ healthyProviders }} / {{ activeProviders }}</strong>
        </div>
      </article>
      <article class="tier-card">
        <Database :size="20" />
        <div>
          <span>{{ t("currency.failoverTitle") }}</span
          ><strong>{{ t("currency.failoverPath") }}</strong>
        </div>
      </article>
      <button type="button" :disabled="loading" @click="load">
        <RefreshCw :size="15" :class="{ spinning: loading }" />{{
          t("currency.refreshData")
        }}
      </button>
    </header>

    <section class="store-currency-switch">
      <div>
        <CircleDollarSign :size="20" />
        <span
          ><b>{{ t("currency.storeCurrency") }}</b
          ><small>{{ t("currency.currencyHint") }}</small></span
        >
      </div>
      <select
        v-model="storeCurrencyDraft"
        :disabled="storeCurrencySaving || !canManage"
      >
        <option
          v-for="currency in enabledCurrencies"
          :key="currency.code"
          :value="currency.code"
        >
          {{ currency.code }} · {{ currency.name }}
        </option>
      </select>
      <input
        v-model="storeCurrencyReason"
        :disabled="!canManage"
        maxlength="500"
        :placeholder="t('currency.changeReasonPlaceholder')"
      />
      <button
        v-if="canManage"
        type="button"
        :disabled="storeCurrencySaving || storeCurrencyDraft === storeCurrency"
        @click="saveStoreCurrency"
      >
        <LoaderCircle v-if="storeCurrencySaving" :size="15" class="spinning" />
        <Save v-else :size="15" />{{ t("currency.save") }}
      </button>
    </section>

    <div v-if="notice" class="currency-alert success">
      <Check :size="15" />{{ notice
      }}<button @click="notice = ''"><X :size="14" /></button>
    </div>
    <div v-if="errorMessage" class="currency-alert danger">
      <AlertCircle :size="15" />{{ errorMessage
      }}<button @click="errorMessage = ''"><X :size="14" /></button>
    </div>

    <section v-if="activeTab === 'currencies'" class="currency-panel">
      <header class="panel-head">
        <div>
          <h2>{{ t("currency.currencyTitle") }}</h2>
          <p>{{ t("currency.currencyHint") }}</p>
        </div>
        <label v-if="canManage" class="reason-field"
          ><span>{{ t("currency.changeReason") }}</span
          ><input
            v-model="changeReason"
            maxlength="500"
            :placeholder="t('currency.changeReasonPlaceholder')"
        /></label>
      </header>
      <div class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>{{ t("currency.codeName") }}</th>
              <th>{{ t("currency.minorUnit") }}</th>
              <th>{{ t("currency.displaySort") }}</th>
              <th>{{ t("currency.status") }}</th>
              <th>{{ t("currency.updatedAt") }}</th>
              <th>{{ t("currency.actions") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in currencies" :key="item.code">
              <td data-label="Currency">
                <b>{{ item.code }} · {{ item.symbol }}</b
                ><small
                  >{{ item.name }} · ISO {{ item.numeric_code || "—" }}</small
                ><em v-if="item.code === storeCurrency">{{
                  t("currency.currentStore")
                }}</em>
              </td>
              <td :data-label="t('currency.minorUnit')">
                <input
                  v-model.number="currencyDrafts[item.code].minor_unit"
                  :disabled="!canManage"
                  class="number-input"
                  type="number"
                  min="0"
                  max="6"
                />
              </td>
              <td :data-label="t('currency.displaySort')">
                <input
                  v-model.number="currencyDrafts[item.code].display_sort"
                  :disabled="!canManage"
                  class="number-input"
                  type="number"
                  min="-100000"
                  max="100000"
                />
              </td>
              <td :data-label="t('currency.status')">
                <label class="switch"
                  ><input
                    v-model="currencyDrafts[item.code].enabled"
                    type="checkbox"
                    :disabled="!canManage || item.code === storeCurrency"
                  /><span></span
                  >{{
                    currencyDrafts[item.code].enabled
                      ? t("currency.enabled")
                      : t("currency.disabled")
                  }}</label
                >
              </td>
              <td :data-label="t('currency.updatedAt')">
                <time>{{ dateTime(item.updated_at) }}</time>
              </td>
              <td :data-label="t('currency.actions')">
                <button
                  v-if="canManage"
                  type="button"
                  class="row-action"
                  :disabled="savingID === item.code"
                  @click="saveCurrency(item)"
                >
                  <LoaderCircle
                    v-if="savingID === item.code"
                    class="spinning"
                    :size="14"
                  /><Save v-else :size="14" />{{ t("currency.save") }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section v-else-if="activeTab === 'providers'" class="currency-panel">
      <header class="panel-head">
        <div>
          <h2>{{ t("currency.providerTitle") }}</h2>
          <p>{{ t("currency.providerHint") }}</p>
        </div>
        <label v-if="canManage" class="reason-field"
          ><span>{{ t("currency.changeReason") }}</span
          ><input
            v-model="changeReason"
            maxlength="500"
            :placeholder="t('currency.changeReasonPlaceholder')"
        /></label>
      </header>
      <div class="provider-grid">
        <article
          v-for="item in providers"
          :key="item.id"
          class="provider-card"
          :class="{ unhealthy: item.has_error }"
        >
          <header>
            <div>
              <i :class="{ online: item.enabled && !item.has_error }"></i
              ><strong>{{ item.name }}</strong
              ><code>{{ item.code }}</code>
            </div>
            <label class="switch"
              ><input
                v-model="providerDrafts[item.id].enabled"
                type="checkbox"
                :disabled="!canManage" /><span></span
            ></label>
          </header>
          <p>
            {{ item.driver
            }}<template v-if="item.provider_key">
              · {{ item.provider_key }}</template
            >
          </p>
          <a
            :href="safeAdminHTTPURL(item.base_url) || undefined"
            target="_blank"
            rel="noopener noreferrer"
            >{{ item.base_url }}</a
          >
          <div class="provider-fields">
            <label
              >{{ t("currency.priority")
              }}<input
                v-model.number="providerDrafts[item.id].priority"
                type="number"
                :disabled="!canManage"
                min="0"
                max="100000"
            /></label>
            <label
              >{{ t("currency.timeoutSeconds")
              }}<input
                v-model.number="providerDrafts[item.id].timeout_seconds"
                type="number"
                :disabled="!canManage"
                min="1"
                max="30"
            /></label>
          </div>
          <dl>
            <div>
              <dt>{{ t("currency.lastSuccess") }}</dt>
              <dd>{{ dateTime(item.last_success_at) }}</dd>
            </div>
            <div>
              <dt>{{ t("currency.lastFailure") }}</dt>
              <dd>{{ dateTime(item.last_failure_at) }}</dd>
            </div>
            <div>
              <dt>{{ t("currency.failureCount") }}</dt>
              <dd>{{ item.failure_count }}</dd>
            </div>
          </dl>
          <button
            v-if="canManage"
            type="button"
            :disabled="savingID === item.id"
            @click="saveProvider(item)"
          >
            <Save :size="14" />{{ t("currency.saveProvider") }}
          </button>
        </article>
      </div>
    </section>

    <section v-else-if="activeTab === 'manual'" class="currency-panel">
      <header class="panel-head">
        <div>
          <h2>{{ t("currency.manualTitle") }}</h2>
          <p>{{ t("currency.manualHint") }}</p>
        </div>
        <button
          v-if="canManage"
          class="primary"
          type="button"
          @click="openManual()"
        >
          <Plus :size="15" />{{ t("currency.addManual") }}
        </button>
      </header>
      <div class="filter-bar">
        <select v-model="filterBase">
          <option value="">{{ t("currency.allBase") }}</option>
          <option
            v-for="item in enabledCurrencies"
            :key="item.code"
            :value="item.code"
          >
            {{ item.code }}
          </option>
        </select>
        <select v-model="filterQuote">
          <option value="">{{ t("currency.allQuote") }}</option>
          <option
            v-for="item in enabledCurrencies"
            :key="item.code"
            :value="item.code"
          >
            {{ item.code }}
          </option>
        </select>
        <select v-model="filterEnabled">
          <option value="">{{ t("currency.allStatus") }}</option>
          <option value="true">{{ t("currency.enabled") }}</option>
          <option value="false">{{ t("currency.disabled") }}</option>
        </select>
        <button type="button" @click="applyFilters">
          <SlidersHorizontal :size="14" />{{ t("currency.applyFilter") }}
        </button>
      </div>
      <div class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>{{ t("currency.pair") }}</th>
              <th>{{ t("currency.exactRate") }}</th>
              <th>{{ t("currency.validity") }}</th>
              <th>{{ t("currency.status") }}</th>
              <th>{{ t("currency.domainReason") }}</th>
              <th>{{ t("currency.actions") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in manualRates" :key="item.id">
              <td :data-label="t('currency.pair')">
                <b>{{ item.base_code }}/{{ item.quote_code }}</b>
              </td>
              <td :data-label="t('currency.exactRate')">
                <code class="rate">{{ item.rate }}</code>
              </td>
              <td :data-label="t('currency.validity')">
                <time>{{ dateTime(item.valid_from) }}</time
                ><small>→ {{ dateTime(item.valid_to) }}</small>
              </td>
              <td :data-label="t('currency.status')">
                <span class="status" :class="{ enabled: item.enabled }">{{
                  item.enabled ? t("currency.enabled") : t("currency.disabled")
                }}</span>
              </td>
              <td :data-label="t('currency.domainReason')">
                <span :title="item.reason">{{ item.reason }}</span>
              </td>
              <td :data-label="t('currency.actions')">
                <button
                  v-if="canManage"
                  class="row-action"
                  type="button"
                  @click="openManual(item)"
                >
                  <Pencil :size="14" />{{ t("currency.edit") }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!manualRates.length && !loading" class="empty-state">
        {{ t("currency.noManual") }}
      </div>
    </section>

    <section v-else class="currency-panel">
      <header class="panel-head">
        <div>
          <h2>{{ t("currency.snapshotTitle") }}</h2>
          <p>{{ t("currency.snapshotHint") }}</p>
        </div>
      </header>
      <form v-if="canManage" class="refresh-rate" @submit.prevent="refreshRate">
        <Activity :size="18" />
        <select v-model="refreshBase">
          <option
            v-for="item in enabledCurrencies"
            :key="item.code"
            :value="item.code"
          >
            {{ item.code }}
          </option></select
        ><span>→</span
        ><select v-model="refreshQuote">
          <option
            v-for="item in enabledCurrencies"
            :key="item.code"
            :value="item.code"
          >
            {{ item.code }}
          </option>
        </select>
        <input
          v-model="refreshReason"
          maxlength="500"
          :placeholder="t('currency.refreshReasonPlaceholder')"
        />
        <button class="primary" type="submit" :disabled="refreshing">
          <LoaderCircle
            v-if="refreshing"
            class="spinning"
            :size="14"
          /><RefreshCw v-else :size="14" />{{ t("currency.refreshNow") }}
        </button>
      </form>
      <div class="filter-bar">
        <select v-model="filterBase">
          <option value="">{{ t("currency.allBase") }}</option>
          <option
            v-for="item in enabledCurrencies"
            :key="item.code"
            :value="item.code"
          >
            {{ item.code }}
          </option></select
        ><select v-model="filterQuote">
          <option value="">{{ t("currency.allQuote") }}</option>
          <option
            v-for="item in enabledCurrencies"
            :key="item.code"
            :value="item.code"
          >
            {{ item.code }}
          </option>
        </select>
        <select v-model="filterTier">
          <option value="">{{ t("currency.allTiers") }}</option>
          <option
            v-for="tier in ['live', 'manual', 'cached', 'system']"
            :key="tier"
            :value="tier"
          >
            {{ t(`currency.tier.${tier}`) }}
          </option>
        </select>
        <input
          v-model="selectedFrom"
          type="datetime-local"
          :title="t('currency.selectedFrom')"
        /><input
          v-model="selectedTo"
          type="datetime-local"
          :title="t('currency.selectedTo')"
        />
        <button type="button" @click="applyFilters">
          <SlidersHorizontal :size="14" />{{ t("currency.applyFilter") }}
        </button>
      </div>
      <div class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>{{ t("currency.pair") }}</th>
              <th>{{ t("currency.exactRate") }}</th>
              <th>{{ t("currency.sourceTier") }}</th>
              <th>{{ t("currency.provider") }}</th>
              <th>{{ t("currency.consensus") }}</th>
              <th>{{ t("currency.selectedAt") }}</th>
              <th>{{ t("currency.decision") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in snapshots" :key="item.id">
              <td :data-label="t('currency.pair')">
                <b>{{ item.base_code }}/{{ item.quote_code }}</b>
              </td>
              <td :data-label="t('currency.exactRate')">
                <code class="rate">{{ item.rate }}</code>
              </td>
              <td :data-label="t('currency.sourceTier')">
                <span class="tier" :class="`tier-${item.source_tier}`">{{
                  t(`currency.tier.${item.source_tier}`)
                }}</span>
              </td>
              <td :data-label="t('currency.provider')">
                {{ item.provider_code || "—" }}
              </td>
              <td :data-label="t('currency.consensus')">
                {{ item.consensus_count }}
              </td>
              <td :data-label="t('currency.selectedAt')">
                <time>{{ dateTime(item.selected_at) }}</time
                ><small>{{
                  t("currency.expiresAt", { time: dateTime(item.expires_at) })
                }}</small>
              </td>
              <td :data-label="t('currency.decision')">
                <span :title="item.decision">{{ item.decision }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!snapshots.length && !loading" class="empty-state">
        {{ t("currency.noSnapshots") }}
      </div>
    </section>

    <footer
      v-if="activeTab === 'manual' || activeTab === 'snapshots'"
      class="pagination"
    >
      <span>{{ t("currency.page", { page, pages: totalPages, total }) }}</span>
      <div>
        <button
          type="button"
          :disabled="page <= 1"
          @click="changePage(page - 1)"
        >
          <ChevronLeft :size="15" />{{ t("currency.previous") }}</button
        ><button
          type="button"
          :disabled="page >= totalPages"
          @click="changePage(page + 1)"
        >
          {{ t("currency.next") }}<ChevronRight :size="15" />
        </button>
      </div>
    </footer>

    <div
      v-if="manualOpen && canManage"
      class="modal-backdrop"
      @mousedown.self="manualOpen = false"
    >
      <form class="manual-modal" @submit.prevent="saveManual">
        <header>
          <div>
            <span>{{ t("adminKicker.fxManualOverride") }}</span>
            <h2>
              {{
                t(manualForm.id ? "currency.editManual" : "currency.addManual")
              }}
            </h2>
          </div>
          <button
            type="button"
            :disabled="manualSaving"
            @click="manualOpen = false"
          >
            <X :size="18" />
          </button>
        </header>
        <div class="modal-grid">
          <label
            >{{ t("currency.baseCurrency")
            }}<select
              v-model="manualForm.baseCode"
              :disabled="Boolean(manualForm.id)"
            >
              <option
                v-for="item in enabledCurrencies"
                :key="item.code"
                :value="item.code"
              >
                {{ item.code }} · {{ item.name }}
              </option>
            </select></label
          ><label
            >{{ t("currency.quoteCurrency")
            }}<select
              v-model="manualForm.quoteCode"
              :disabled="Boolean(manualForm.id)"
            >
              <option
                v-for="item in enabledCurrencies"
                :key="item.code"
                :value="item.code"
              >
                {{ item.code }} · {{ item.name }}
              </option>
            </select></label
          >
        </div>
        <label
          >{{ t("currency.exactRate")
          }}<input
            v-model.trim="manualForm.rate"
            inputmode="decimal"
            autocomplete="off"
            maxlength="39"
            placeholder="7.0267"
          /><small>{{ t("currency.exactRateHint") }}</small></label
        >
        <div class="modal-grid">
          <label
            >{{ t("currency.validFrom")
            }}<input
              v-model="manualForm.validFrom"
              type="datetime-local" /></label
          ><label
            >{{ t("currency.validTo")
            }}<input v-model="manualForm.validTo" type="datetime-local"
          /></label>
        </div>
        <label class="switch-row"
          ><span
            ><b>{{ t("currency.manualEnabled") }}</b
            ><small>{{ t("currency.manualEnabledHint") }}</small></span
          ><span class="switch"
            ><input
              v-model="manualForm.enabled"
              type="checkbox" /><span></span></span
        ></label>
        <label
          >{{ t("currency.domainReason")
          }}<textarea
            v-model="manualForm.reason"
            maxlength="500"
            :placeholder="t('currency.domainReasonPlaceholder')"
          ></textarea>
        </label>
        <label
          >{{ t("currency.changeReason")
          }}<textarea
            v-model="manualForm.auditReason"
            maxlength="500"
            :placeholder="t('currency.changeReasonPlaceholder')"
          ></textarea>
        </label>
        <footer>
          <button
            type="button"
            :disabled="manualSaving"
            @click="manualOpen = false"
          >
            {{ t("currency.cancel") }}</button
          ><button class="primary" type="submit" :disabled="manualSaving">
            <LoaderCircle
              v-if="manualSaving"
              class="spinning"
              :size="14"
            /><Save v-else :size="14" />{{ t("currency.confirmSave") }}
          </button>
        </footer>
      </form>
    </div>
  </section>
</template>

<style scoped>
.currency-view {
  --panel: var(--surface);
}
.currency-view {
  display: grid;
  gap: 16px;
  color: var(--text, #171717);
}
.currency-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr)) auto;
  gap: 12px;
}

.store-currency-switch {
  display: grid;
  grid-template-columns:
    minmax(240px, 1fr) minmax(150px, 0.45fr) minmax(260px, 1fr)
    auto;
  gap: 12px;
  align-items: center;
  margin-bottom: 18px;
  padding: 16px;
  border: 1px solid var(--line);
  border-radius: 18px;
  background: var(--surface);
}

.store-currency-switch > div,
.store-currency-switch > div span {
  display: flex;
  gap: 10px;
}

.store-currency-switch > div span {
  flex-direction: column;
  gap: 2px;
}

.store-currency-switch small {
  color: var(--muted);
}

.store-currency-switch select,
.store-currency-switch input,
.store-currency-switch button {
  min-height: 42px;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: var(--surface-raised);
  color: inherit;
  padding: 0 12px;
}

.store-currency-switch button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  cursor: pointer;
}
.currency-summary article,
.currency-panel,
.currency-summary > button {
  border: 1px solid var(--line, #e5e5e5);
  background: var(--panel, #fff);
  border-radius: 14px;
}
.currency-summary article {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 15px;
}
.currency-summary article > svg {
  color: var(--muted, #737373);
}
.currency-summary article div {
  display: grid;
  gap: 3px;
}
.currency-summary span,
.panel-head p,
.provider-card p,
.provider-card dl,
.data-table small {
  color: var(--muted, #737373);
  font-size: 12px;
}
.currency-summary strong {
  font-size: 15px;
}
.currency-summary > button,
.currency-tabs button,
.filter-bar button,
.row-action,
.provider-card > button,
.pagination button,
.primary,
.manual-modal button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border: 1px solid var(--line, #e5e5e5);
  background: var(--panel, #fff);
  color: inherit;
  border-radius: 10px;
  padding: 9px 12px;
  cursor: pointer;
}
.currency-tabs {
  display: flex;
  gap: 4px;
  padding: 4px;
  background: var(--soft, #f5f5f5);
  border-radius: 12px;
  width: max-content;
}
.currency-tabs button {
  border: 0;
  background: transparent;
}
.currency-tabs button.active {
  background: var(--panel, #fff);
  box-shadow: 0 1px 5px rgb(0 0 0/0.08);
}
.currency-alert {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 12px 14px;
  border-radius: 10px;
  font-size: 13px;
}
.currency-alert.success {
  background: #ecfdf3;
  color: #087443;
}
.currency-alert.danger {
  background: #fff1f1;
  color: #b42318;
}
.currency-alert button {
  margin-left: auto;
  border: 0;
  background: none;
  color: inherit;
}
.currency-panel {
  overflow: hidden;
}
.panel-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 18px;
  padding: 18px 20px;
  border-bottom: 1px solid var(--line, #e5e5e5);
}
.panel-head h2 {
  font-size: 17px;
  margin: 0 0 5px;
}
.panel-head p {
  margin: 0;
}
.reason-field {
  display: grid;
  gap: 5px;
  min-width: min(390px, 46vw);
  font-size: 12px;
}
.reason-field input,
.filter-bar input,
.filter-bar select,
.provider-fields input,
.number-input,
.refresh-rate input,
.refresh-rate select,
.manual-modal input,
.manual-modal select,
.manual-modal textarea {
  border: 1px solid var(--line, #ddd);
  background: var(--panel, #fff);
  color: inherit;
  border-radius: 8px;
  padding: 9px 10px;
  outline: none;
}
.reason-field input:focus,
.manual-modal input:focus,
.manual-modal textarea:focus {
  border-color: #171717;
}
.data-table-wrap {
  overflow: auto;
}
.data-table {
  border-collapse: collapse;
  width: 100%;
  min-width: 850px;
}
.data-table th,
.data-table td {
  text-align: left;
  padding: 13px 15px;
  border-bottom: 1px solid var(--line, #ededed);
  font-size: 13px;
  vertical-align: middle;
}
.data-table th {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--muted, #737373);
  background: var(--soft, #fafafa);
}
.data-table td:first-child {
  display: table-cell;
}
.data-table td:first-child b,
.data-table td:first-child small {
  display: block;
}
.data-table em {
  display: inline-block;
  margin-top: 5px;
  background: #171717;
  color: white;
  border-radius: 20px;
  padding: 3px 7px;
  font-size: 10px;
  font-style: normal;
}
.number-input {
  width: 88px;
}
.switch {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 12px;
}
.switch input {
  position: absolute;
  opacity: 0;
}
.switch > span {
  width: 34px;
  height: 19px;
  border-radius: 20px;
  background: #bbb;
  position: relative;
}
.switch > span:after {
  content: "";
  position: absolute;
  width: 15px;
  height: 15px;
  top: 2px;
  left: 2px;
  border-radius: 50%;
  background: #fff;
  transition: 0.15s;
}
.switch input:checked + span {
  background: #171717;
}
.switch input:checked + span:after {
  transform: translateX(15px);
}
.switch input:disabled + span {
  opacity: 0.45;
}
.provider-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  padding: 18px;
}
.provider-card {
  border: 1px solid var(--line, #e5e5e5);
  border-radius: 12px;
  padding: 15px;
  display: grid;
  gap: 12px;
}
.provider-card.unhealthy {
  border-color: #f7b4ae;
}
.provider-card header {
  display: flex;
  justify-content: space-between;
}
.provider-card header > div {
  display: grid;
  grid-template-columns: auto 1fr;
  align-items: center;
  gap: 3px 8px;
}
.provider-card header i {
  width: 8px;
  height: 8px;
  background: #c4c4c4;
  border-radius: 50%;
  grid-row: 1/3;
}
.provider-card header i.online {
  background: #12b76a;
}
.provider-card header code {
  font-size: 11px;
  color: var(--muted, #737373);
}
.provider-card p {
  margin: 0;
}
.provider-card a {
  font-size: 11px;
  color: inherit;
  overflow: hidden;
  text-overflow: ellipsis;
}
.provider-fields {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 9px;
}
.provider-fields label {
  display: grid;
  gap: 5px;
  font-size: 11px;
}
.provider-card dl {
  display: grid;
  gap: 5px;
  margin: 0;
}
.provider-card dl div {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}
.provider-card dd {
  margin: 0;
  text-align: right;
}
.filter-bar,
.refresh-rate {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 12px 15px;
  border-bottom: 1px solid var(--line, #e5e5e5);
  background: var(--soft, #fafafa);
}
.filter-bar select,
.filter-bar input {
  min-width: 130px;
}
.primary {
  background: #171717 !important;
  color: #fff !important;
  border-color: #171717 !important;
}
.status,
.tier {
  display: inline-flex;
  border-radius: 20px;
  padding: 4px 8px;
  background: #f0f0f0;
  font-size: 11px;
}
.status.enabled,
.tier-live {
  background: #dcfae6;
  color: #087443;
}
.tier-manual {
  background: #fff4d6;
  color: #8a5a00;
}
.tier-cached {
  background: #e9efff;
  color: #2846a3;
}
.rate {
  font-size: 13px;
  font-weight: 700;
}
.empty-state {
  text-align: center;
  padding: 40px;
  color: var(--muted, #737373);
}
.pagination {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  border-top: 1px solid var(--line, #e5e5e5);
  font-size: 12px;
}
.pagination div {
  display: flex;
  gap: 8px;
}
.refresh-rate input {
  flex: 1;
  min-width: 220px;
}
.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 80;
  background: rgb(0 0 0/0.55);
  display: grid;
  place-items: center;
  padding: 20px;
}
.manual-modal {
  width: min(620px, 100%);
  max-height: calc(100vh - 40px);
  overflow: auto;
  background: var(--panel, #fff);
  border-radius: 16px;
  padding: 20px;
  display: grid;
  gap: 15px;
}
.manual-modal > header,
.manual-modal > footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.manual-modal header span {
  font-size: 10px;
  letter-spacing: 0.12em;
  color: var(--muted, #737373);
}
.manual-modal h2 {
  margin: 4px 0 0;
  font-size: 20px;
}
.manual-modal > label,
.modal-grid label {
  display: grid;
  gap: 6px;
  font-size: 12px;
}
.modal-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.manual-modal textarea {
  min-height: 74px;
  resize: vertical;
}
.manual-modal small {
  color: var(--muted, #737373);
}
.switch-row {
  grid-template-columns: 1fr auto !important;
  align-items: center;
  border: 1px solid var(--line, #e5e5e5);
  border-radius: 10px;
  padding: 11px;
}
.switch-row > span:first-child {
  display: grid;
  gap: 3px;
}
.manual-modal footer {
  border-top: 1px solid var(--line, #e5e5e5);
  padding-top: 15px;
  justify-content: flex-end;
  gap: 9px;
}
.spinning {
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
@media (max-width: 1100px) {
  .store-currency-switch {
    grid-template-columns: 1fr 1fr;
  }
  .currency-summary {
    grid-template-columns: repeat(3, 1fr);
  }
  .currency-summary > button {
    grid-column: 1/-1;
  }
  .provider-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
@media (max-width: 720px) {
  .store-currency-switch {
    grid-template-columns: 1fr;
  }
  .currency-summary {
    grid-template-columns: 1fr;
  }
  .currency-summary > button {
    grid-column: auto;
  }
  .currency-tabs {
    width: 100%;
    overflow: auto;
  }
  .currency-tabs button {
    white-space: nowrap;
    flex: 1;
  }
  .panel-head {
    align-items: stretch;
    flex-direction: column;
  }
  .reason-field {
    min-width: 0;
  }
  .provider-grid {
    grid-template-columns: 1fr;
    padding: 12px;
  }
  .data-table {
    min-width: 0;
  }
  .data-table thead {
    display: none;
  }
  .data-table,
  .data-table tbody,
  .data-table tr,
  .data-table td {
    display: block;
  }
  .data-table tr {
    padding: 10px 13px;
    border-bottom: 1px solid var(--line, #e5e5e5);
  }
  .data-table td {
    border: 0;
    padding: 6px 0;
    display: grid !important;
    grid-template-columns: 120px 1fr;
    gap: 8px;
  }
  .data-table td:before {
    content: attr(data-label);
    font-size: 11px;
    color: var(--muted, #737373);
  }
  .data-table td:first-child {
    grid-template-columns: 1fr;
  }
  .data-table td:first-child:before {
    display: none;
  }
  .refresh-rate {
    align-items: stretch;
  }
  .refresh-rate input {
    min-width: 100%;
    order: 5;
  }
  .modal-grid {
    grid-template-columns: 1fr;
  }
  .pagination {
    align-items: flex-start;
    gap: 10px;
    flex-direction: column;
  }
  .tier-card strong {
    font-size: 12px;
  }
}
</style>
