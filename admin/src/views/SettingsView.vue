<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  AlertCircle,
  BadgePercent,
  Check,
  Clock3,
  Image,
  LoaderCircle,
  Mail,
  PackageSearch,
  RefreshCw,
  Save,
  SearchCheck,
  Settings2,
  Store,
  Upload,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import {
  formatMoney,
  majorInputStep,
  majorToMinor,
  minorToMajor,
  registerCurrencies,
} from "../utils/money";

const { t, locale } = useI18n();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("system.manage"));

interface SettingRecord {
  key: string;
  value: string;
  group: string;
  updated_at: string;
}

interface CurrencyOption {
  code: string;
  name: string;
  symbol: string;
  minor_unit: number;
  enabled: boolean;
}

type Section = "store" | "order" | "inventory" | "affiliate";

interface SettingsForm {
  store_name: string;
  store_tagline: string;
  store_currency: string;
  store_logo_url: string;
  store_support_email: string;
  store_seo_title: string;
  store_seo_description: string;
  order_timeout_minutes: string;
  inventory_warning_threshold: string;
  affiliate_default_basis_points: string;
  affiliate_hold_days: string;
  affiliate_withdrawal_minimum: string;
}

const defaults: SettingsForm = {
  store_name: "LinLinQi",
  store_tagline: t("settings.defaultTagline"),
  store_currency: "CNY",
  store_logo_url: "",
  store_support_email: "",
  store_seo_title: "",
  store_seo_description: "",
  order_timeout_minutes: "15",
  inventory_warning_threshold: "10",
  affiliate_default_basis_points: "500",
  affiliate_hold_days: "7",
  affiliate_withdrawal_minimum: "10000",
};

const activeSection = ref<Section>("store");
const form = ref<SettingsForm>({ ...defaults });
const original = ref<SettingsForm>({ ...defaults });
const records = ref<SettingRecord[]>([]);
const currencies = ref<CurrencyOption[]>([]);
const loading = ref(false);
const saving = ref(false);
const logoUploading = ref(false);
const loadError = ref("");
const saveError = ref("");
const notice = ref("");
const reason = ref("");

const sections: Array<{
  value: Section;
  label: string;
  hint: string;
  icon: typeof Store;
}> = [
  {
    value: "store",
    label: "settings.sectionStore",
    hint: "settings.sectionStoreHint",
    icon: Store,
  },
  {
    value: "order",
    label: "settings.sectionOrder",
    hint: "settings.sectionOrderHint",
    icon: Clock3,
  },
  {
    value: "inventory",
    label: "settings.sectionInventory",
    hint: "settings.sectionInventoryHint",
    icon: PackageSearch,
  },
  {
    value: "affiliate",
    label: "settings.sectionAffiliate",
    hint: "settings.sectionAffiliateHint",
    icon: BadgePercent,
  },
];

const sectionKeys: Record<Section, Array<keyof SettingsForm>> = {
  store: [
    "store_name",
    "store_tagline",
    "store_currency",
    "store_logo_url",
    "store_support_email",
    "store_seo_title",
    "store_seo_description",
  ],
  order: ["order_timeout_minutes"],
  inventory: ["inventory_warning_threshold"],
  affiliate: [
    "affiliate_default_basis_points",
    "affiliate_hold_days",
    "affiliate_withdrawal_minimum",
  ],
};

const changedKeys = computed(() =>
  sectionKeys[activeSection.value].filter(
    (key) => form.value[key] !== original.value[key],
  ),
);
const commissionPercent = computed(() =>
  (Number(form.value.affiliate_default_basis_points || 0) / 100).toFixed(2),
);
const minimumWithdrawal = computed(() =>
  formatMoney(
    form.value.affiliate_withdrawal_minimum || "0",
    form.value.store_currency,
    locale.value,
  ),
);
const withdrawalMinimumMajor = computed({
  get: () =>
    minorToMajor(
      form.value.affiliate_withdrawal_minimum || "0",
      form.value.store_currency,
    ),
  set: (value: string) => {
    try {
      form.value.affiliate_withdrawal_minimum = majorToMinor(
        value,
        form.value.store_currency,
      );
    } catch {
      form.value.affiliate_withdrawal_minimum = value;
    }
  },
});

function responseMessage(error: unknown, fallback: string) {
  const candidate = error as {
    response?: { data?: { message?: string } };
    message?: string;
  };
  const value = candidate.response?.data?.message || candidate.message || "";
  return value.startsWith("error.") ? fallback : value || fallback;
}

function validLogoURL(value: string) {
  if (/[\u0000-\u001f\u007f]/.test(value)) return false;
  if (
    value.startsWith("/") &&
    !value.startsWith("//") &&
    !value.includes("\\") &&
    !value.includes("..")
  )
    return true;
  try {
    const parsed = new URL(value);
    if (parsed.username || parsed.password || parsed.hash) return false;
    if (parsed.protocol === "https:") return parsed.hostname !== "";
    if (parsed.protocol === "http:") {
      const host = parsed.hostname.toLowerCase().replace(/\.$/, "");
      return (
        host === "localhost" ||
        host === "127.0.0.1" ||
        host === "[::1]" ||
        host === "::1"
      );
    }
    return false;
  } catch {
    return false;
  }
}
function dateTime(value?: string) {
  if (!value) return t("settings.notSaved");
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? t("settings.notSaved")
    : date.toLocaleString("zh-CN", { hour12: false });
}
function sectionUpdatedAt(section: Section) {
  const keys = new Set(sectionKeys[section]);
  const timestamps = records.value
    .filter((item) => keys.has(item.key as keyof SettingsForm))
    .map((item) => item.updated_at)
    .filter(Boolean)
    .sort()
    .reverse();
  return dateTime(timestamps[0]);
}

async function loadSettings() {
  loading.value = true;
  loadError.value = "";
  try {
    const [settingsResponse, currenciesResponse] = await Promise.all([
      adminApi.get("/settings"),
      adminApi.get("/currencies"),
    ]);
    records.value = Array.isArray(settingsResponse.data.data)
      ? settingsResponse.data.data
      : [];
    const currencyPayload = currenciesResponse.data?.data || {};
    currencies.value = Array.isArray(currencyPayload.items)
      ? currencyPayload.items.filter((item: CurrencyOption) => item.enabled)
      : [];
    registerCurrencies(
      currencyPayload.items || [],
      currencyPayload.store_currency,
    );
    const next = { ...defaults };
    for (const record of records.value) {
      if (record.key in next)
        next[record.key as keyof SettingsForm] = String(record.value ?? "");
    }
    form.value = next;
    original.value = { ...next };
  } catch (error) {
    loadError.value = responseMessage(error, t("settings.errLoad"));
  } finally {
    loading.value = false;
  }
}

function validateSection() {
  const values = form.value;
  if (activeSection.value === "store") {
    if (
      values.store_name.trim().length < 2 ||
      values.store_name.trim().length > 80
    )
      return t("settings.errName");
    if (
      values.store_tagline.trim().length < 2 ||
      values.store_tagline.trim().length > 200
    )
      return t("settings.errTagline");
    if (values.store_logo_url && !validLogoURL(values.store_logo_url))
      return t("settings.errLogoUrl");
    if (
      values.store_support_email &&
      !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(values.store_support_email)
    )
      return t("settings.errEmail");
    if (
      values.store_seo_title &&
      (values.store_seo_title.length < 2 || values.store_seo_title.length > 100)
    )
      return t("settings.errSeoTitle");
    if (
      values.store_seo_description &&
      (values.store_seo_description.length < 2 ||
        values.store_seo_description.length > 300)
    )
      return t("settings.errSeoDescription");
  }
  if (activeSection.value === "order") {
    const value = Number(values.order_timeout_minutes);
    if (!Number.isInteger(value) || value < 5 || value > 1440)
      return t("settings.errOrderTimeout");
  }
  if (activeSection.value === "inventory") {
    const value = Number(values.inventory_warning_threshold);
    if (!Number.isInteger(value) || value < 1 || value > 100000)
      return t("settings.errInventoryThreshold");
  }
  if (activeSection.value === "affiliate") {
    const commission = Number(values.affiliate_default_basis_points);
    const hold = Number(values.affiliate_hold_days);
    if (!Number.isInteger(commission) || commission < 1 || commission > 3000)
      return t("settings.errCommission");
    if (!Number.isInteger(hold) || hold < 1 || hold > 90)
      return t("settings.errHoldDays");
    try {
      const minimum = BigInt(values.affiliate_withdrawal_minimum);
      const lower = BigInt(majorToMinor("1", values.store_currency));
      const upper = BigInt(majorToMinor("1000000", values.store_currency));
      if (minimum < lower || minimum > upper)
        return t("settings.errWithdrawalMinimum");
    } catch {
      return t("settings.errWithdrawalMinimum");
    }
  }
  return "";
}

async function saveSection() {
  if (!canManage.value) return;
  saveError.value = validateSection();
  if (saveError.value) return;
  if (!changedKeys.value.length) {
    saveError.value = t("settings.errNoChanges");
    return;
  }
  if (reason.value.trim().length < 4 || reason.value.trim().length > 500) {
    saveError.value = t("settings.errReason");
    return;
  }
  const payload: Record<string, string> = {};
  for (const key of changedKeys.value)
    payload[key] = String(form.value[key]).trim();
  saving.value = true;
  try {
    await adminApi.put("/settings", payload, {
      headers: { "X-Change-Reason": reason.value.trim() },
      timeout: 120_000,
    });
    const section = sections.find((item) => item.value === activeSection.value);
    notice.value = t("settings.savedNotice", {
      label: section ? t(section.label) : t("settings.settings"),
    });
    reason.value = "";
    await loadSettings();
  } catch (error) {
    saveError.value = responseMessage(error, t("settings.errSave"));
  } finally {
    saving.value = false;
  }
}

async function uploadLogo(event: Event) {
  if (!canManage.value) return;
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  if (!file.type.startsWith("image/")) {
    saveError.value = t("settings.errLogoImageType");
    return;
  }
  if (reason.value.trim().length < 4 || reason.value.trim().length > 500) {
    saveError.value = t("settings.errReasonBeforeUpload");
    return;
  }
  logoUploading.value = true;
  saveError.value = "";
  try {
    const body = new FormData();
    body.append("file", file, file.name);
    body.append("alt_text", form.value.store_name.trim() || file.name);
    const { data } = await adminApi.post("/settings/media/upload", body, {
      headers: { "X-Change-Reason": reason.value.trim() },
      timeout: 120_000,
    });
    form.value.store_logo_url = data.data.public_url;
    notice.value = t("settings.logoUploaded");
  } catch (error) {
    saveError.value = responseMessage(error, t("settings.errLogoUpload"));
  } finally {
    logoUploading.value = false;
  }
}

function discardSection() {
  if (!canManage.value) return;
  for (const key of sectionKeys[activeSection.value])
    form.value[key] = original.value[key];
  reason.value = "";
  saveError.value = "";
}

onMounted(loadSettings);
</script>

<template>
  <section class="settings-view">
    <div class="settings-toolbar">
      <div>
        <Settings2 :size="16" /><span>{{ t("settings.auditNote") }}</span>
      </div>
      <button type="button" :disabled="loading" @click="loadSettings">
        <RefreshCw :size="14" :class="{ spinning: loading }" />{{
          t("settings.refresh")
        }}
      </button>
    </div>

    <div v-if="notice" class="settings-alert success">
      <Check :size="14" /><span>{{ notice }}</span
      ><button type="button" @click="notice = ''"><X :size="13" /></button>
    </div>
    <div v-if="loadError" class="settings-alert danger">
      <AlertCircle :size="14" /><span>{{ loadError }}</span
      ><button type="button" @click="loadSettings">
        {{ t("settings.retry") }}
      </button>
    </div>

    <div class="settings-layout">
      <nav class="settings-nav" :aria-label="t('settings.navAria')">
        <button
          v-for="section in sections"
          :key="section.value"
          type="button"
          :class="{ active: activeSection === section.value }"
          @click="
            activeSection = section.value;
            saveError = '';
            reason = '';
          "
        >
          <component :is="section.icon" :size="16" />
          <span
            ><strong>{{ t(section.label) }}</strong
            ><small>{{ t(section.hint) }}</small></span
          >
          <i>{{ sectionUpdatedAt(section.value) }}</i>
        </button>
      </nav>

      <main class="settings-panel" :aria-busy="loading">
        <div v-if="loading" class="settings-empty">
          <LoaderCircle :size="25" class="spinning" />{{
            t("settings.loading")
          }}
        </div>
        <template v-else>
          <header>
            <div>
              <h2>
                {{
                  t(
                    sections.find((item) => item.value === activeSection)
                      ?.label ?? "",
                  )
                }}
              </h2>
              <p>
                {{
                  t(
                    sections.find((item) => item.value === activeSection)
                      ?.hint ?? "",
                  )
                }}
              </p>
            </div>
            <span v-if="changedKeys.length">{{
              t("settings.unsavedCount", { count: changedKeys.length })
            }}</span>
          </header>

          <div v-if="activeSection === 'store'" class="settings-content">
            <section class="store-preview">
              <div class="preview-logo">
                <img
                  v-if="form.store_logo_url"
                  :src="form.store_logo_url"
                  :alt="t('settings.logoPreviewAlt')"
                /><span v-else>LQ</span>
              </div>
              <div>
                <strong>{{ form.store_name || "LinLinQi" }}</strong>
                <p>{{ form.store_tagline }}</p>
                <small>{{
                  form.store_support_email || t("settings.noSupportEmail")
                }}</small>
              </div>
            </section>
            <div class="field-grid">
              <label
                ><span>{{ t("settings.labelStoreName") }}</span
                ><input
                  v-model="form.store_name"
                  :disabled="!canManage"
                  maxlength="80"
                /><small>{{ t("settings.hintStoreName") }}</small></label
              ><label
                ><span>{{ t("settings.labelCurrency") }}</span
                ><select v-model="form.store_currency" :disabled="!canManage">
                  <option
                    v-for="currency in currencies"
                    :key="currency.code"
                    :value="currency.code"
                  >
                    {{ currency.code }} · {{ currency.name }}
                    {{ currency.symbol ? `(${currency.symbol})` : "" }}
                  </option></select
                ><small
                  >{{ t("settings.hintCurrency") }}
                  <RouterLink to="/currencies">{{
                    t("currency.manageCurrencies")
                  }}</RouterLink></small
                ></label
              >
            </div>
            <label
              ><span>{{ t("settings.labelTagline") }}</span
              ><textarea
                v-model="form.store_tagline"
                :disabled="!canManage"
                maxlength="200"
                rows="3"
              ></textarea>
            </label>
            <fieldset>
              <legend>
                <Image :size="13" />{{ t("settings.legendBrandMedia") }}
              </legend>
              <div class="media-field">
                <label
                  ><span>{{ t("settings.labelLogoUrl") }}</span
                  ><input
                    v-model="form.store_logo_url"
                    :disabled="!canManage"
                    type="text"
                    inputmode="url"
                    placeholder="https://cdn.example.com/logo.svg"
                  /><small>{{ t("settings.hintLogoUrl") }}</small></label
                >
                <label
                  :class="[
                    'media-upload-button',
                    { disabled: !canManage || logoUploading },
                  ]"
                  ><LoaderCircle
                    v-if="logoUploading"
                    :size="14"
                    class="spinning" /><Upload v-else :size="14" />
                  {{
                    logoUploading
                      ? t("settings.uploadingLogo")
                      : t("settings.uploadLogo")
                  }}
                  <input
                    type="file"
                    accept="image/*"
                    :disabled="!canManage || logoUploading"
                    @change="uploadLogo"
                /></label>
              </div>
              <label
                ><span
                  ><Mail :size="12" />{{
                    t("settings.labelSupportEmail")
                  }}</span
                ><input
                  v-model="form.store_support_email"
                  :disabled="!canManage"
                  type="email"
                  placeholder="support@example.com"
                /><small>{{ t("settings.hintSupportEmail") }}</small></label
              >
            </fieldset>
            <fieldset>
              <legend>
                <SearchCheck :size="13" />{{ t("settings.legendSeo") }}
              </legend>
              <label
                ><span>{{ t("settings.labelSeoTitle") }}</span
                ><input
                  v-model="form.store_seo_title"
                  :disabled="!canManage"
                  maxlength="100"
                  :placeholder="t('settings.placeholderSeoTitle')" /></label
              ><label
                ><span>{{ t("settings.labelSeoDescription") }}</span
                ><textarea
                  v-model="form.store_seo_description"
                  :disabled="!canManage"
                  maxlength="300"
                  rows="3"
                  :placeholder="t('settings.placeholderSeoDescription')"
                ></textarea>
              </label>
            </fieldset>
          </div>

          <div v-else-if="activeSection === 'order'" class="settings-content">
            <article class="policy-card">
              <Clock3 :size="22" />
              <div>
                <strong>{{ t("settings.orderPolicyTitle") }}</strong>
                <p>{{ t("settings.orderPolicyText") }}</p>
              </div>
            </article>
            <label
              ><span>{{ t("settings.labelOrderTimeout") }}</span
              ><input
                v-model="form.order_timeout_minutes"
                :disabled="!canManage"
                type="number"
                min="5"
                max="1440"
                step="1"
              /><small>{{ t("settings.hintOrderTimeout") }}</small></label
            >
          </div>

          <div
            v-else-if="activeSection === 'inventory'"
            class="settings-content"
          >
            <article class="policy-card">
              <PackageSearch :size="22" />
              <div>
                <strong>{{ t("settings.inventoryPolicyTitle") }}</strong>
                <p>{{ t("settings.inventoryPolicyText") }}</p>
              </div>
            </article>
            <label
              ><span>{{ t("settings.labelInventoryThreshold") }}</span
              ><input
                v-model="form.inventory_warning_threshold"
                :disabled="!canManage"
                type="number"
                min="1"
                max="100000"
                step="1"
            /></label>
          </div>

          <div v-else class="settings-content">
            <div class="affiliate-preview">
              <article>
                <span>{{ t("settings.affiliateCommission") }}</span
                ><strong>{{ commissionPercent }}%</strong>
              </article>
              <article>
                <span>{{ t("settings.affiliateHold") }}</span
                ><strong>{{
                  t("settings.holdDays", { count: form.affiliate_hold_days })
                }}</strong>
              </article>
              <article>
                <span>{{ t("settings.affiliateMinimum") }}</span
                ><strong>{{ minimumWithdrawal }}</strong>
              </article>
            </div>
            <div class="field-grid">
              <label
                ><span>{{ t("settings.labelCommission") }}</span
                ><input
                  v-model="form.affiliate_default_basis_points"
                  :disabled="!canManage"
                  type="number"
                  min="1"
                  max="3000"
                  step="1"
                /><small>{{ t("settings.hintCommission") }}</small></label
              ><label
                ><span>{{ t("settings.labelHoldDays") }}</span
                ><input
                  v-model="form.affiliate_hold_days"
                  :disabled="!canManage"
                  type="number"
                  min="1"
                  max="90"
                  step="1"
                /><small>{{ t("settings.hintHoldDays") }}</small></label
              >
            </div>
            <label
              ><span>{{ t("settings.labelWithdrawalMinimum") }}</span
              ><input
                v-model="withdrawalMinimumMajor"
                :disabled="!canManage"
                inputmode="decimal"
                min="1"
                max="1000000"
                :step="majorInputStep(form.store_currency)"
              /><small>{{
                t("settings.hintWithdrawalMinimum", {
                  value: minimumWithdrawal,
                })
              }}</small></label
            >
          </div>

          <section v-if="canManage" class="save-area">
            <label
              ><span>{{ t("settings.labelReason") }}</span
              ><textarea
                v-model="reason"
                maxlength="500"
                rows="3"
                :placeholder="t('settings.placeholderReason')"
              ></textarea>
            </label>
            <div v-if="saveError" class="inline-error">
              <AlertCircle :size="14" />{{ saveError }}
            </div>
            <footer>
              <button
                type="button"
                class="secondary-button"
                :disabled="saving || !changedKeys.length"
                @click="discardSection"
              >
                {{ t("settings.discard") }}</button
              ><button
                type="button"
                class="primary-button"
                :disabled="saving || !changedKeys.length"
                @click="saveSection"
              >
                <LoaderCircle v-if="saving" :size="14" class="spinning" /><Save
                  v-else
                  :size="14"
                />{{ saving ? t("settings.saving") : t("settings.save") }}
              </button>
            </footer>
          </section>
        </template>
      </main>
    </div>
  </section>
</template>

<style scoped>
.settings-view {
  display: grid;
  gap: 13px;
  color: var(--text);
}
.settings-toolbar,
.settings-toolbar > div {
  display: flex;
  align-items: center;
  gap: 7px;
}
.settings-toolbar {
  justify-content: space-between;
  min-height: 36px;
  color: var(--muted);
  font-size: 9px;
}
.settings-toolbar button {
  display: inline-flex;
  min-height: 34px;
  align-items: center;
  gap: 5px;
  padding: 0 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 9px;
  cursor: pointer;
}
.settings-alert {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 9px 11px;
  border: 1px solid;
  border-radius: 7px;
  font-size: 9px;
}
.settings-alert span {
  flex: 1;
}
.settings-alert button {
  border: 0;
  color: inherit;
  background: transparent;
  font: inherit;
  cursor: pointer;
}
.settings-alert.success {
  color: #166534;
  border-color: #86efac;
  background: #f0fdf4;
}
.settings-alert.danger,
.inline-error {
  color: #991b1b;
  border-color: #fecaca;
  background: #fef2f2;
}
:global([data-theme="dark"]) .settings-alert.success {
  color: #bbf7d0;
  border-color: #166534;
  background: #052e16;
}
:global([data-theme="dark"]) .settings-alert.danger,
:global([data-theme="dark"]) .inline-error {
  color: #fecaca;
  border-color: #7f1d1d;
  background: #450a0a;
}
.settings-layout {
  display: grid;
  grid-template-columns: 235px minmax(0, 1fr);
  gap: 10px;
  align-items: start;
}
.settings-nav {
  display: grid;
  gap: 5px;
}
.settings-nav button {
  display: grid;
  grid-template-columns: 28px minmax(0, 1fr);
  gap: 6px 8px;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 8px;
  color: var(--muted);
  background: var(--surface);
  text-align: left;
  cursor: pointer;
}
.settings-nav button > svg {
  margin-top: 2px;
}
.settings-nav button > span {
  display: grid;
  gap: 4px;
}
.settings-nav strong {
  color: var(--text);
  font-size: 9px;
}
.settings-nav small {
  color: var(--muted);
  font-size: 8px;
}
.settings-nav i {
  grid-column: 2;
  color: var(--muted);
  font-size: 7px;
  font-style: normal;
}
.settings-nav button.active {
  color: var(--surface);
  border-color: var(--text);
  background: var(--text);
}
.settings-nav button.active strong,
.settings-nav button.active small,
.settings-nav button.active i {
  color: var(--surface);
}
.settings-panel {
  min-height: 620px;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
}
.settings-panel > header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  padding: 15px 17px;
  border-bottom: 1px solid var(--line);
}
.settings-panel h2 {
  margin: 0;
  font-size: 13px;
}
.settings-panel header p {
  margin: 5px 0 0;
  color: var(--muted);
  font-size: 8px;
}
.settings-panel header > span {
  padding: 4px 7px;
  border-radius: 999px;
  color: var(--surface);
  background: var(--text);
  font-size: 7px;
}
.settings-empty {
  min-height: 620px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--muted);
  font-size: 9px;
}
.settings-content {
  display: grid;
  gap: 13px;
  padding: 16px 17px;
}
.settings-content label,
.save-area label {
  display: grid;
  gap: 6px;
  color: var(--muted);
  font-size: 8px;
}
.settings-content label > span,
.save-area label > span {
  display: flex;
  align-items: center;
  gap: 4px;
  color: var(--text);
  font-size: 9px;
  font-weight: 650;
}
.settings-content input,
.settings-content select,
.settings-content textarea,
.save-area textarea {
  width: 100%;
  box-sizing: border-box;
  border: 1px solid var(--line);
  border-radius: 7px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 9px;
  outline: none;
}
.settings-content input,
.settings-content select {
  height: 37px;
  padding: 0 9px;
}
.settings-content textarea,
.save-area textarea {
  padding: 9px;
  resize: vertical;
  line-height: 1.6;
}
.settings-content input:focus,
.settings-content textarea:focus,
.save-area textarea:focus {
  border-color: var(--text);
}
.settings-content label small {
  color: var(--muted);
  font-size: 8px;
  line-height: 1.5;
}
.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 11px;
}
.media-field {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 9px;
  align-items: end;
}
.media-field > label:first-child {
  min-width: 0;
}
.media-upload-button {
  display: inline-flex;
  min-height: 37px;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 0 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
  color: var(--text) !important;
  background: var(--surface);
  white-space: nowrap;
  cursor: pointer;
  font-size: 9px;
  font-weight: 700;
}
.media-upload-button.disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.media-upload-button input {
  display: none;
}
.store-preview {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--soft);
}
.preview-logo {
  display: grid;
  width: 52px;
  height: 52px;
  place-items: center;
  flex: 0 0 auto;
  overflow: hidden;
  border-radius: 10px;
  color: var(--surface);
  background: var(--text);
  font-size: 15px;
  font-weight: 800;
}
.preview-logo img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  background: var(--surface);
}
.store-preview > div:last-child {
  display: grid;
  gap: 4px;
}
.store-preview strong {
  font-size: 12px;
}
.store-preview p {
  margin: 0;
  color: var(--text);
  font-size: 9px;
}
.store-preview small {
  color: var(--muted);
  font-size: 8px;
}
.settings-content fieldset {
  display: grid;
  gap: 11px;
  margin: 0;
  padding: 13px;
  border: 1px solid var(--line);
  border-radius: 8px;
}
.settings-content legend {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 0 6px;
  font-size: 9px;
  font-weight: 700;
}
.policy-card {
  display: flex;
  align-items: flex-start;
  gap: 11px;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--soft);
}
.policy-card svg {
  flex: 0 0 auto;
}
.policy-card strong {
  font-size: 10px;
}
.policy-card p {
  margin: 5px 0 0;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.6;
}
.affiliate-preview {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 7px;
}
.affiliate-preview article {
  display: grid;
  gap: 7px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--soft);
}
.affiliate-preview span {
  color: var(--muted);
  font-size: 8px;
}
.affiliate-preview strong {
  font-size: 13px;
}
.save-area {
  display: grid;
  gap: 10px;
  margin: 0 17px 17px;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--soft);
}
.inline-error {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  padding: 8px 9px;
  border: 1px solid;
  border-radius: 6px;
  font-size: 8px;
}
.save-area footer {
  display: flex;
  justify-content: flex-end;
  gap: 7px;
}
.primary-button,
.secondary-button {
  display: inline-flex;
  min-height: 35px;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 0 11px;
  border: 1px solid var(--text);
  border-radius: 6px;
  font: inherit;
  font-size: 9px;
  font-weight: 700;
  cursor: pointer;
}
.primary-button {
  color: var(--surface);
  background: var(--text);
}
.secondary-button {
  color: var(--text);
  border-color: var(--line);
  background: var(--surface);
}
.primary-button:disabled,
.secondary-button:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.spinning {
  animation: settings-spin 0.8s linear infinite;
}
@keyframes settings-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 820px) {
  .settings-layout {
    grid-template-columns: 1fr;
  }
  .settings-nav {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .settings-panel {
    min-height: 520px;
  }
}
@media (max-width: 560px) {
  .settings-toolbar {
    align-items: flex-start;
  }
  .settings-toolbar > div span {
    display: none;
  }
  .settings-nav {
    display: flex;
    overflow-x: auto;
  }
  .settings-nav button {
    min-width: 145px;
  }
  .field-grid,
  .media-field,
  .affiliate-preview {
    grid-template-columns: 1fr;
  }
  .save-area footer {
    flex-direction: column-reverse;
  }
  .save-area footer button {
    width: 100%;
  }
  .settings-panel > header,
  .settings-content {
    padding-right: 13px;
    padding-left: 13px;
  }
  .save-area {
    margin-right: 13px;
    margin-left: 13px;
  }
}
</style>
