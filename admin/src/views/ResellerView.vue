<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  AlertCircle,
  BadgeCheck,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleDollarSign,
  Edit3,
  Globe2,
  LoaderCircle,
  RefreshCw,
  Search,
  ShieldCheck,
  Store,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import {
  formatMoney as formatMinorMoney,
  majorInputStep,
  majorToMinor,
  minorToMajor,
  minorToSafeNumber,
  storeCurrency,
} from "../utils/money";
import ResellerWithdrawalAdmin from "../components/ResellerWithdrawalAdmin.vue";
import ResellerWholesaleTierAdmin from "../components/ResellerWholesaleTierAdmin.vue";

const { t, locale } = useI18n();
const route = useRoute();
const authStore = useAuthStore();
const canManage = computed(() => authStore.hasPermission("reseller.manage"));

type ResellerTab = "profiles" | "tiers" | "domains" | "withdrawals";
type ProfileStatus = "pending" | "active" | "suspended" | "rejected";
type DomainStatus =
  "pending" | "verified" | "active" | "suspended" | "rejected";
type TLSStatus = "pending" | "provisioning" | "active" | "failed" | "disabled";

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface ResellerProfile {
  id: string;
  user_id: string;
  name: string;
  code: string;
  status: ProfileStatus | string;
  credit_limit: number;
  wholesale_level: number;
  wallet_balance: number;
  wallet_frozen: number;
  credit_exposure: number;
  credit_remaining: number;
  credit_breached: boolean;
  currency: string;
  wholesale_tier_name: string;
  wholesale_discount_basis_point: number;
  wholesale_configured: boolean;
  applied_at: string;
  verified_at?: string | null;
  rejected_at?: string | null;
  created_at: string;
  updated_at: string;
}

interface ResellerWholesaleTier {
  id: string;
  level: number;
  name: string;
  discount_basis_point: number;
  enabled: boolean;
  active_reseller_count: number;
  total_reseller_count: number;
  created_at: string;
  updated_at: string;
}

interface ResellerDomain {
  id: string;
  reseller_id: string;
  domain: string;
  status: DomainStatus | string;
  tls_status: TLSStatus | string;
  verification_token: string;
  verified_at?: string | null;
  created_at: string;
  updated_at: string;
}

interface ProfileForm {
  targetStatus: ProfileStatus | "";
  creditLimitYuan: string;
  wholesaleLevel: number;
  reason: string;
}

interface DomainForm {
  status: Exclude<DomainStatus, "pending"> | "";
  tlsStatus: TLSStatus | "";
  reason: string;
}

function profileStatusLabel(status: string) {
  const key = `reselleradmin.profileStatus.${status}`;
  return t(key) === key ? status : t(key);
}
function domainStatusLabel(status: string) {
  const key = `reselleradmin.domainStatus.${status}`;
  return t(key) === key ? status : t(key);
}
function tlsStatusLabel(status: string) {
  const key = `reselleradmin.tlsStatus.${status}`;
  return t(key) === key ? status : t(key);
}
const profileStatusOptions = [
  { value: "", label: "reselleradmin.profileStatusOptions.all" },
  { value: "pending", label: "reselleradmin.profileStatusOptions.pending" },
  { value: "active", label: "reselleradmin.profileStatusOptions.active" },
  { value: "suspended", label: "reselleradmin.profileStatusOptions.suspended" },
  { value: "rejected", label: "reselleradmin.profileStatusOptions.rejected" },
];
const domainStatusOptions = [
  { value: "", label: "reselleradmin.domainStatusOptions.all" },
  { value: "pending", label: "reselleradmin.domainStatusOptions.pending" },
  { value: "verified", label: "reselleradmin.domainStatusOptions.verified" },
  { value: "active", label: "reselleradmin.domainStatusOptions.active" },
  { value: "suspended", label: "reselleradmin.domainStatusOptions.suspended" },
  { value: "rejected", label: "reselleradmin.domainStatusOptions.rejected" },
];
const tlsFilterOptions = [
  { value: "", label: "reselleradmin.tlsFilterOptions.all" },
  { value: "pending", label: "reselleradmin.tlsFilterOptions.pending" },
  {
    value: "provisioning",
    label: "reselleradmin.tlsFilterOptions.provisioning",
  },
  { value: "active", label: "reselleradmin.tlsFilterOptions.active" },
  { value: "failed", label: "reselleradmin.tlsFilterOptions.failed" },
  { value: "disabled", label: "reselleradmin.tlsFilterOptions.disabled" },
];
const tlsEditOptions: Array<{ value: TLSStatus; label: string }> =
  tlsFilterOptions.slice(1) as Array<{ value: TLSStatus; label: string }>;
const transitionMap: Record<ProfileStatus, ProfileStatus[]> = {
  pending: ["active", "rejected"],
  active: ["suspended"],
  suspended: ["active", "rejected"],
  rejected: ["pending"],
};

const activeTab = ref<ResellerTab>("profiles");
const profiles = ref<ResellerProfile[]>([]);
const domains = ref<ResellerDomain[]>([]);
const knownProfiles = ref<ResellerProfile[]>([]);
const wholesaleTiers = ref<ResellerWholesaleTier[]>([]);
const wholesaleTiersLoading = ref(false);
const wholesaleTiersError = ref("");
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const statusFilter = ref("");
const tlsFilter = ref("");
const searchInput = ref("");
const appliedSearch = ref("");
const loading = ref(false);
const loadError = ref("");
const notice = ref("");
const modalKind = ref<"profile" | "domain" | null>(null);
const editingProfile = ref<ResellerProfile | null>(null);
const editingDomain = ref<ResellerDomain | null>(null);
const saving = ref(false);
const formError = ref("");
const profileForm = ref<ProfileForm>({
  targetStatus: "",
  creditLimitYuan: "0.00",
  wholesaleLevel: 0,
  reason: "",
});
const domainForm = ref<DomainForm>({
  status: "",
  tlsStatus: "pending",
  reason: "",
});
let listRequest = 0;

const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const pageNumbers = computed(() => {
  const first = Math.max(1, Math.min(page.value - 2, totalPages.value - 4));
  const last = Math.min(totalPages.value, first + 4);
  return Array.from({ length: last - first + 1 }, (_, index) => first + index);
});
const profileLookup = computed(
  () => new Map(knownProfiles.value.map((item) => [item.id, item])),
);
const normalizedSearch = computed(() => appliedSearch.value.toLowerCase());
const visibleProfiles = computed(() => {
  if (!normalizedSearch.value) return profiles.value;
  return profiles.value.filter((item) =>
    [item.name, item.code, item.user_id, item.id].some((value) =>
      String(value || "")
        .toLowerCase()
        .includes(normalizedSearch.value),
    ),
  );
});
const visibleDomains = computed(() =>
  domains.value.filter((item) => {
    if (tlsFilter.value && item.tls_status !== tlsFilter.value) return false;
    if (!normalizedSearch.value) return true;
    const profile = profileLookup.value.get(item.reseller_id);
    return [
      item.domain,
      item.id,
      item.reseller_id,
      profile?.name,
      profile?.code,
    ].some((value) =>
      String(value || "")
        .toLowerCase()
        .includes(normalizedSearch.value),
    );
  }),
);
const visibleCount = computed(() =>
  activeTab.value === "profiles"
    ? visibleProfiles.value.length
    : visibleDomains.value.length,
);
const activeItemsCount = computed(() =>
  activeTab.value === "profiles" ? profiles.value.length : domains.value.length,
);
const currentProfileTransitions = computed(() =>
  editingProfile.value
    ? profileTransitions(editingProfile.value.status)
    : ([] as ProfileStatus[]),
);
const enabledWholesaleTiers = computed(() =>
  wholesaleTiers.value.filter((item) => item.enabled),
);
const editingWholesaleTier = computed(() =>
  enabledWholesaleTiers.value.find(
    (item) => item.level === profileForm.value.wholesaleLevel,
  ),
);
const currentDomainStatusOptions = computed(() => {
  if (!editingDomain.value) return [];
  if (!editingDomain.value.verified_at) {
    return [
      { value: "rejected" as const, label: "reselleradmin.rejectDomain" },
    ];
  }
  return (["verified", "active", "suspended", "rejected"] as const).map(
    (value) => ({ value, label: `reselleradmin.domainStatus.${value}` }),
  );
});

function apiMessage(error: unknown, fallback: string) {
  const failure = error as {
    response?: { status?: number; data?: { message?: string } };
  };
  return failure.response?.data?.message || fallback;
}

function apiStatus(error: unknown) {
  return (error as { response?: { status?: number } }).response?.status;
}

function centsToYuan(value: number, currency?: string) {
  return minorToMajor(value, currency || storeCurrency.value);
}

function yuanToCents(value: string, currency?: string) {
  return minorToSafeNumber(
    majorToMinor(value, currency || storeCurrency.value),
  );
}

function formatMoney(value: number, currency?: string) {
  return formatMinorMoney(value, currency || storeCurrency.value, locale.value);
}

function formatPercent(basisPoints: number) {
  return `${(Number(basisPoints || 0) / 100).toLocaleString(locale.value, {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  })}%`;
}

function formatTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString(locale.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

function shortID(value: string) {
  return value.length > 18 ? `${value.slice(0, 8)}…${value.slice(-6)}` : value;
}

function mergeKnownProfiles(items: ResellerProfile[]) {
  const merged = new Map(knownProfiles.value.map((item) => [item.id, item]));
  for (const item of items) merged.set(item.id, item);
  knownProfiles.value = [...merged.values()];
}

function profileTransitions(status: string) {
  return transitionMap[status as ProfileStatus] || [];
}

function profileActionLabel(status: string) {
  switch (status) {
    case "pending":
      return "reselleradmin.actionApprove";
    case "active":
      return "reselleradmin.actionSuspend";
    case "suspended":
      return "reselleradmin.actionRestore";
    case "rejected":
      return "reselleradmin.actionResubmit";
    default:
      return "reselleradmin.actionNone";
  }
}

async function getProfiles(params: Record<string, string | number>) {
  try {
    const response = await adminApi.get("/operations/reseller-profiles", {
      params,
    });
    return response;
  } catch (error: unknown) {
    if (apiStatus(error) !== 404) throw error;
    return adminApi.get("/operations/resellers", { params });
  }
}

async function loadWholesaleTiers() {
  wholesaleTiersLoading.value = true;
  wholesaleTiersError.value = "";
  try {
    const response = await adminApi.get("/operations/reseller-wholesale-tiers");
    const payload = response.data?.data;
    wholesaleTiers.value = (Array.isArray(payload) ? payload : []).sort(
      (left, right) => left.level - right.level,
    );
  } catch (error: unknown) {
    wholesaleTiers.value = [];
    wholesaleTiersError.value = apiMessage(
      error,
      t("reselleradmin.tiers.errLoad"),
    );
  } finally {
    wholesaleTiersLoading.value = false;
  }
}

async function loadList() {
  const request = ++listRequest;
  const tab = activeTab.value;
  loading.value = true;
  loadError.value = "";
  try {
    const params: Record<string, string | number> = {
      page: page.value,
      page_size: pageSize.value,
    };
    if (statusFilter.value) params.status = statusFilter.value;
    const response =
      tab === "profiles"
        ? await getProfiles(params)
        : await adminApi.get("/operations/reseller-domains", { params });
    if (request !== listRequest || tab !== activeTab.value) return;
    const payload = response.data.data as PagePayload<
      ResellerProfile | ResellerDomain
    >;
    const items = Array.isArray(payload.items) ? payload.items : [];
    if (tab === "profiles") {
      profiles.value = items as ResellerProfile[];
      mergeKnownProfiles(profiles.value);
    } else {
      domains.value = items as ResellerDomain[];
    }
    total.value = Number(payload.total || 0);
    page.value = Number(payload.page || page.value);
    pageSize.value = Number(payload.page_size || pageSize.value);
    if (page.value > totalPages.value && page.value > 1) {
      page.value = totalPages.value;
      await loadList();
    }
  } catch (error: unknown) {
    if (request !== listRequest) return;
    if (tab === "profiles") profiles.value = [];
    else domains.value = [];
    total.value = 0;
    loadError.value = apiMessage(error, t("reselleradmin.errLoad"));
  } finally {
    if (request === listRequest) loading.value = false;
  }
}

function applySearch() {
  appliedSearch.value = searchInput.value.trim();
}

function clearSearch() {
  searchInput.value = "";
  appliedSearch.value = "";
}

async function applyStatusFilter() {
  page.value = 1;
  await loadList();
}

async function changePage(target: number) {
  if (target < 1 || target > totalPages.value || target === page.value) return;
  page.value = target;
  await loadList();
}

async function changePageSize() {
  page.value = 1;
  await loadList();
}

function openProfile(item: ResellerProfile) {
  if (!canManage.value) return;
  editingProfile.value = item;
  editingDomain.value = null;
  profileForm.value = {
    targetStatus: "",
    creditLimitYuan: centsToYuan(item.credit_limit, item.currency),
    wholesaleLevel: item.wholesale_level,
    reason: "",
  };
  formError.value = "";
  modalKind.value = "profile";
  if (!wholesaleTiersLoading.value && !wholesaleTiers.value.length)
    void loadWholesaleTiers();
}

function openDomain(item: ResellerDomain) {
  if (!canManage.value) return;
  editingDomain.value = item;
  editingProfile.value = null;
  const editableStatus = [
    "verified",
    "active",
    "suspended",
    "rejected",
  ].includes(item.status)
    ? (item.status as Exclude<DomainStatus, "pending">)
    : "";
  const editableTLS = tlsEditOptions.some(
    (option) => option.value === item.tls_status,
  )
    ? (item.tls_status as TLSStatus)
    : "pending";
  domainForm.value = {
    status: item.verified_at ? editableStatus || "verified" : "",
    tlsStatus: editableTLS,
    reason: "",
  };
  formError.value = "";
  modalKind.value = "domain";
}

function closeModal() {
  if (saving.value) return;
  modalKind.value = null;
  editingProfile.value = null;
  editingDomain.value = null;
  formError.value = "";
}

function validReason(value: string) {
  const length = [...value.trim()].length;
  return length >= 4 && length <= 500;
}

function validCredit(value: string, currency: string) {
  try {
    const amount = BigInt(majorToMinor(value, currency));
    const maximum = BigInt(majorToMinor("10000000", currency));
    return amount >= 0n && amount <= maximum;
  } catch {
    return false;
  }
}

function validateProfile() {
  const item = editingProfile.value;
  const form = profileForm.value;
  if (!item) return t("reselleradmin.errProfileContext");
  if (
    form.targetStatus &&
    !profileTransitions(item.status).includes(
      form.targetStatus as ProfileStatus,
    )
  )
    return t("reselleradmin.errTransition");
  if (!validCredit(form.creditLimitYuan, item.currency))
    return t("reselleradmin.errCreditRange");
  if (
    !Number.isInteger(form.wholesaleLevel) ||
    !enabledWholesaleTiers.value.some(
      (tier) => tier.level === form.wholesaleLevel,
    )
  )
    return t("reselleradmin.errTierUnavailable");
  const candidateLimit = yuanToCents(form.creditLimitYuan, item.currency);
  const exposure = Number(item.credit_exposure || 0);
  if (
    candidateLimit < exposure &&
    (candidateLimit < item.credit_limit || form.targetStatus === "active")
  )
    return t("reselleradmin.errCreditBelowExposure", {
      exposure: formatMoney(exposure, item.currency),
    });
  if (
    !form.targetStatus &&
    yuanToCents(form.creditLimitYuan, item.currency) === item.credit_limit &&
    form.wholesaleLevel === item.wholesale_level
  )
    return t("reselleradmin.errNoChange");
  if (!validReason(form.reason)) return t("reselleradmin.errReason");
  return "";
}

async function submitProfile() {
  if (!canManage.value) return;
  const validation = validateProfile();
  if (validation) {
    formError.value = validation;
    return;
  }
  const item = editingProfile.value;
  if (!item) return;
  saving.value = true;
  formError.value = "";
  const form = profileForm.value;
  try {
    await adminApi.patch(
      `/resellers/${encodeURIComponent(item.id)}`,
      {
        status: form.targetStatus || item.status,
        credit_limit: yuanToCents(form.creditLimitYuan, item.currency),
        wholesale_level: form.wholesaleLevel,
      },
      { headers: { "X-Change-Reason": form.reason.trim() } },
    );
    modalKind.value = null;
    editingProfile.value = null;
    notice.value = form.targetStatus
      ? t("reselleradmin.profileUpdatedStatus", {
          status: profileStatusLabel(form.targetStatus),
        })
      : t("reselleradmin.profileUpdatedLimit");
    await loadList();
  } catch (error: unknown) {
    formError.value = apiMessage(error, t("reselleradmin.errProfileSave"));
  } finally {
    saving.value = false;
  }
}

function syncDomainTLS() {
  if (!canManage.value) return;
  if (domainForm.value.status === "active") {
    domainForm.value.tlsStatus = "active";
  }
}

function validateDomain() {
  const item = editingDomain.value;
  const form = domainForm.value;
  if (!item) return t("reselleradmin.errDomainContext");
  const allowedStatuses = currentDomainStatusOptions.value.map(
    (option) => option.value,
  );
  if (!allowedStatuses.includes(form.status as never))
    return t("reselleradmin.errDomainStatus");
  if (!tlsEditOptions.some((option) => option.value === form.tlsStatus))
    return t("reselleradmin.errTlsStatus");
  if (!item.verified_at && form.status !== "rejected")
    return t("reselleradmin.errNotVerified");
  if (form.status === "active" && form.tlsStatus !== "active")
    return t("reselleradmin.errTlsMustActive");
  if (form.status === item.status && form.tlsStatus === item.tls_status)
    return t("reselleradmin.errDomainNoChange");
  if (!validReason(form.reason)) return t("reselleradmin.errReason");
  return "";
}

async function submitDomain() {
  if (!canManage.value) return;
  const validation = validateDomain();
  if (validation) {
    formError.value = validation;
    return;
  }
  const item = editingDomain.value;
  if (!item) return;
  saving.value = true;
  formError.value = "";
  const form = domainForm.value;
  try {
    await adminApi.patch(
      `/reseller-domains/${encodeURIComponent(item.id)}`,
      { status: form.status, tls_status: form.tlsStatus },
      { headers: { "X-Change-Reason": form.reason.trim() } },
    );
    modalKind.value = null;
    editingDomain.value = null;
    notice.value = t("reselleradmin.domainUpdated", { domain: item.domain });
    await loadList();
  } catch (error: unknown) {
    formError.value = apiMessage(error, t("reselleradmin.errDomainSave"));
  } finally {
    saving.value = false;
  }
}

function handleEscape(event: KeyboardEvent) {
  if (event.key === "Escape") closeModal();
}

watch(modalKind, (value) => {
  document.body.style.overflow = value ? "hidden" : "";
});

watch(
  () => [route.path, route.meta.defaultTab] as const,
  async ([, defaultTab]) => {
    const requested = String(defaultTab || "profiles") as ResellerTab;
    activeTab.value = ["profiles", "tiers", "domains", "withdrawals"].includes(
      requested,
    )
      ? requested
      : "profiles";
    page.value = 1;
    statusFilter.value = "";
    tlsFilter.value = "";
    searchInput.value = "";
    appliedSearch.value = "";
    notice.value = "";
    if (activeTab.value === "tiers") await loadWholesaleTiers();
    else if (activeTab.value !== "withdrawals") await loadList();
  },
  { immediate: true },
);

onMounted(() => {
  window.addEventListener("keydown", handleEscape);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", handleEscape);
  document.body.style.overflow = "";
});
</script>

<template>
  <section class="reseller-shell">
    <ResellerWholesaleTierAdmin
      v-if="activeTab === 'tiers'"
      @updated="loadWholesaleTiers"
    />
    <ResellerWithdrawalAdmin v-else-if="activeTab === 'withdrawals'" />
    <div v-else class="reseller-panel panel">
      <header class="reseller-toolbar">
        <form class="reseller-search" @submit.prevent="applySearch">
          <Search :size="15" />
          <input
            v-model="searchInput"
            type="search"
            :placeholder="
              activeTab === 'profiles'
                ? t('reselleradmin.searchPlaceholderProfiles')
                : t('reselleradmin.searchPlaceholderDomains')
            "
            :aria-label="t('reselleradmin.searchAria')"
          />
          <button v-if="appliedSearch" type="button" @click="clearSearch">
            <X :size="13" />{{ t("reselleradmin.clear") }}
          </button>
          <button type="submit">{{ t("reselleradmin.filter") }}</button>
        </form>
        <div class="reseller-filters">
          <select
            v-model="statusFilter"
            :aria-label="t('reselleradmin.statusFilterAria')"
            @change="applyStatusFilter"
          >
            <option
              v-for="option in activeTab === 'profiles'
                ? profileStatusOptions
                : domainStatusOptions"
              :key="option.value || 'all'"
              :value="option.value"
            >
              {{ t(option.label) }}
            </option>
          </select>
          <select
            v-if="activeTab === 'domains'"
            v-model="tlsFilter"
            :aria-label="t('reselleradmin.tlsFilterAria')"
          >
            <option
              v-for="option in tlsFilterOptions"
              :key="option.value || 'all-tls'"
              :value="option.value"
            >
              {{ t(option.label) }}
            </option>
          </select>
          <button type="button" :disabled="loading" @click="loadList">
            <RefreshCw :size="14" :class="{ spinning: loading }" />{{
              t("reselleradmin.refresh")
            }}
          </button>
        </div>
      </header>

      <div v-if="notice" class="reseller-notice success-notice">
        <Check :size="15" />{{ notice }}
      </div>
      <div v-if="loadError" class="reseller-notice error-notice">
        <AlertCircle :size="15" />{{ loadError }}
        <button type="button" @click="loadList">
          {{ t("reselleradmin.retry") }}
        </button>
      </div>

      <div v-if="loading && !activeItemsCount" class="reseller-state">
        <LoaderCircle class="spinning" :size="23" />
        <span>{{ t("reselleradmin.loading") }}</span>
      </div>
      <div v-else-if="!loadError && !visibleCount" class="reseller-state">
        <Store v-if="activeTab === 'profiles'" :size="27" />
        <Globe2 v-else :size="27" />
        <strong>{{
          appliedSearch || tlsFilter || statusFilter
            ? t("reselleradmin.noMatch")
            : activeTab === "profiles"
              ? t("reselleradmin.noProfiles")
              : t("reselleradmin.noDomains")
        }}</strong>
        <span v-if="appliedSearch || tlsFilter">{{
          t("reselleradmin.noMatchHint")
        }}</span>
      </div>

      <div v-else-if="activeTab === 'profiles'" class="reseller-table-wrap">
        <table class="reseller-table">
          <thead>
            <tr>
              <th>{{ t("reselleradmin.colProfile") }}</th>
              <th>{{ t("reselleradmin.colUser") }}</th>
              <th>{{ t("reselleradmin.colCredit") }}</th>
              <th>{{ t("reselleradmin.colWallet") }}</th>
              <th>{{ t("reselleradmin.colLevel") }}</th>
              <th>{{ t("reselleradmin.colApplied") }}</th>
              <th>{{ t("reselleradmin.colStatus") }}</th>
              <th>
                <span class="sr-only">{{ t("reselleradmin.colActions") }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in visibleProfiles" :key="item.id">
              <td :data-label="t('reselleradmin.colProfile')">
                <div class="record-primary">
                  <span><Store :size="15" /></span>
                  <div>
                    <b>{{ item.name }}</b>
                    <code>{{ item.code }}</code>
                  </div>
                </div>
              </td>
              <td :data-label="t('reselleradmin.colUser')">
                <code :title="item.user_id">{{ shortID(item.user_id) }}</code>
                <small :title="item.id">{{
                  t("reselleradmin.accountId", { id: shortID(item.id) })
                }}</small>
              </td>
              <td :data-label="t('reselleradmin.colCredit')">
                <div
                  class="credit-cell"
                  :class="{ breached: item.credit_breached }"
                >
                  <b>{{ formatMoney(item.credit_limit, item.currency) }}</b>
                  <small>
                    {{
                      t("reselleradmin.creditUsage", {
                        exposure: formatMoney(
                          item.credit_exposure,
                          item.currency,
                        ),
                        remaining: formatMoney(
                          item.credit_remaining,
                          item.currency,
                        ),
                      })
                    }}
                  </small>
                  <em v-if="item.credit_breached">
                    {{ t("reselleradmin.creditBreached") }}
                  </em>
                </div>
              </td>
              <td :data-label="t('reselleradmin.colWallet')">
                <b>{{ formatMoney(item.wallet_balance, item.currency) }}</b>
                <small>{{
                  t("reselleradmin.walletFrozen", {
                    amount: formatMoney(item.wallet_frozen, item.currency),
                  })
                }}</small>
              </td>
              <td :data-label="t('reselleradmin.colLevel')">
                <div
                  class="tier-cell"
                  :class="{ unavailable: !item.wholesale_configured }"
                >
                  <b>
                    L{{ item.wholesale_level }} ·
                    {{
                      item.wholesale_tier_name ||
                      t("reselleradmin.tierNameUnavailable")
                    }}
                  </b>
                  <small v-if="item.wholesale_configured">
                    {{
                      t("reselleradmin.tierDiscount", {
                        discount: formatPercent(
                          item.wholesale_discount_basis_point,
                        ),
                      })
                    }}
                  </small>
                  <em v-else>{{ t("reselleradmin.tierNotConfigured") }}</em>
                </div>
              </td>
              <td :data-label="t('reselleradmin.colApplied')">
                <time>{{
                  formatTime(item.applied_at || item.created_at)
                }}</time>
                <small>{{
                  item.verified_at
                    ? t("reselleradmin.verifiedAt", {
                        time: formatTime(item.verified_at),
                      })
                    : item.rejected_at
                      ? t("reselleradmin.rejectedAt", {
                          time: formatTime(item.rejected_at),
                        })
                      : t("reselleradmin.notReviewed")
                }}</small>
              </td>
              <td :data-label="t('reselleradmin.colStatus')">
                <span class="status-badge" :class="`status-${item.status}`">
                  {{ profileStatusLabel(item.status) }}
                </span>
              </td>
              <td
                :data-label="t('reselleradmin.colActions')"
                class="record-actions"
              >
                <button
                  v-if="canManage && profileTransitions(item.status).length"
                  type="button"
                  @click="openProfile(item)"
                >
                  <Edit3 :size="13" />{{ t(profileActionLabel(item.status)) }}
                </button>
                <span
                  v-else-if="!profileTransitions(item.status).length"
                  class="no-action"
                  >{{ t("reselleradmin.noTransition") }}</span
                >
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="reseller-table-wrap">
        <table class="reseller-table domain-table">
          <thead>
            <tr>
              <th>{{ t("reselleradmin.colDomain") }}</th>
              <th>{{ t("reselleradmin.colReseller") }}</th>
              <th>{{ t("reselleradmin.colOwnership") }}</th>
              <th>{{ t("reselleradmin.colTls") }}</th>
              <th>{{ t("reselleradmin.colRunStatus") }}</th>
              <th>{{ t("reselleradmin.colUpdated") }}</th>
              <th>
                <span class="sr-only">{{ t("reselleradmin.colActions") }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in visibleDomains" :key="item.id">
              <td :data-label="t('reselleradmin.colDomain')">
                <div class="record-primary">
                  <span><Globe2 :size="15" /></span>
                  <div>
                    <b>{{ item.domain }}</b>
                    <code :title="item.id">{{ shortID(item.id) }}</code>
                  </div>
                </div>
              </td>
              <td :data-label="t('reselleradmin.colReseller')">
                <b>{{
                  profileLookup.get(item.reseller_id)?.name ||
                  t("reselleradmin.nameNotLoaded")
                }}</b>
                <small :title="item.reseller_id">{{
                  shortID(item.reseller_id)
                }}</small>
              </td>
              <td :data-label="t('reselleradmin.colOwnership')">
                <b :class="item.verified_at ? 'verified-text' : ''">
                  {{
                    item.verified_at
                      ? t("reselleradmin.verified")
                      : t("reselleradmin.pendingUserVerify")
                  }}
                </b>
                <small v-if="item.verified_at">{{
                  formatTime(item.verified_at)
                }}</small>
                <code v-else :title="item.verification_token">
                  {{
                    item.verification_token
                      ? shortID(item.verification_token)
                      : t("reselleradmin.noToken")
                  }}
                </code>
              </td>
              <td :data-label="t('reselleradmin.colTls')">
                <span class="tls-badge" :class="`tls-${item.tls_status}`">
                  {{ tlsStatusLabel(item.tls_status) }}
                </span>
              </td>
              <td :data-label="t('reselleradmin.colRunStatus')">
                <span class="status-badge" :class="`status-${item.status}`">
                  {{ domainStatusLabel(item.status) }}
                </span>
              </td>
              <td :data-label="t('reselleradmin.colUpdated')">
                <time>{{ formatTime(item.updated_at) }}</time>
                <small>{{
                  t("reselleradmin.createdAt", {
                    time: formatTime(item.created_at),
                  })
                }}</small>
              </td>
              <td
                :data-label="t('reselleradmin.colActions')"
                class="record-actions"
              >
                <button
                  v-if="canManage"
                  type="button"
                  @click="openDomain(item)"
                >
                  <Edit3 :size="13" />{{ t("reselleradmin.domainOps") }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <footer v-if="!loadError" class="reseller-pagination">
        <span>
          {{ t("reselleradmin.pageInfo", { page, pages: totalPages, total }) }}
          <template v-if="appliedSearch || tlsFilter">
            {{ t("reselleradmin.pageMatch", { count: visibleCount }) }}
          </template>
        </span>
        <div>
          <button
            type="button"
            :aria-label="t('reselleradmin.prevPage')"
            :disabled="page <= 1 || loading"
            @click="changePage(page - 1)"
          >
            <ChevronLeft :size="14" />
          </button>
          <button
            v-for="number in pageNumbers"
            :key="number"
            type="button"
            :class="{ active: number === page }"
            :disabled="loading"
            @click="changePage(number)"
          >
            {{ number }}
          </button>
          <button
            type="button"
            :aria-label="t('reselleradmin.nextPage')"
            :disabled="page >= totalPages || loading"
            @click="changePage(page + 1)"
          >
            <ChevronRight :size="14" />
          </button>
          <select
            v-model.number="pageSize"
            :aria-label="t('reselleradmin.pageSizeAria')"
            @change="changePageSize"
          >
            <option :value="10">
              {{ t("reselleradmin.perPage", { size: 10 }) }}
            </option>
            <option :value="20">
              {{ t("reselleradmin.perPage", { size: 20 }) }}
            </option>
            <option :value="50">
              {{ t("reselleradmin.perPage", { size: 50 }) }}
            </option>
          </select>
        </div>
      </footer>
    </div>

    <div
      v-if="modalKind && canManage"
      class="reseller-modal-backdrop"
      role="presentation"
      @mousedown.self="closeModal"
    >
      <section
        class="reseller-modal"
        role="dialog"
        aria-modal="true"
        :aria-label="
          modalKind === 'profile'
            ? t('reselleradmin.modalProfileAria')
            : t('reselleradmin.modalDomainAria')
        "
      >
        <header>
          <div>
            <span class="kicker">{{ t("reselleradmin.kicker") }}</span>
            <h2>
              {{
                modalKind === "profile"
                  ? t("reselleradmin.modalProfileTitle")
                  : t("reselleradmin.modalDomainTitle")
              }}
            </h2>
            <p>
              {{
                modalKind === "profile"
                  ? editingProfile?.name
                  : editingDomain?.domain
              }}
            </p>
          </div>
          <button
            type="button"
            :aria-label="t('reselleradmin.close')"
            :disabled="saving"
            @click="closeModal"
          >
            <X :size="18" />
          </button>
        </header>

        <form
          v-if="modalKind === 'profile' && editingProfile"
          class="reseller-form"
          @submit.prevent="submitProfile"
        >
          <div v-if="formError" class="form-alert error-notice">
            <AlertCircle :size="15" />{{ formError }}
          </div>

          <section class="identity-card">
            <span><Store :size="18" /></span>
            <div>
              <b>{{ editingProfile.name }}</b>
              <code>{{ editingProfile.code }} · {{ editingProfile.id }}</code>
            </div>
            <span
              class="status-badge"
              :class="`status-${editingProfile.status}`"
            >
              {{ profileStatusLabel(editingProfile.status) }}
            </span>
          </section>

          <section
            class="credit-snapshot"
            :class="{ breached: editingProfile.credit_breached }"
          >
            <div>
              <span>{{ t("reselleradmin.creditExposure") }}</span>
              <strong>{{
                formatMoney(
                  editingProfile.credit_exposure,
                  editingProfile.currency,
                )
              }}</strong>
            </div>
            <div>
              <span>{{ t("reselleradmin.creditRemaining") }}</span>
              <strong>{{
                formatMoney(
                  editingProfile.credit_remaining,
                  editingProfile.currency,
                )
              }}</strong>
            </div>
            <div>
              <span>{{ t("reselleradmin.walletBalance") }}</span>
              <strong>{{
                formatMoney(
                  editingProfile.wallet_balance,
                  editingProfile.currency,
                )
              }}</strong>
            </div>
            <div>
              <span>{{ t("reselleradmin.walletFrozenLabel") }}</span>
              <strong>{{
                formatMoney(
                  editingProfile.wallet_frozen,
                  editingProfile.currency,
                )
              }}</strong>
            </div>
            <p>
              <AlertCircle v-if="editingProfile.credit_breached" />
              <ShieldCheck v-else />
              {{
                editingProfile.credit_breached
                  ? t("reselleradmin.creditBreachDetail")
                  : t("reselleradmin.creditSemanticHint")
              }}
            </p>
          </section>

          <fieldset>
            <legend>{{ t("reselleradmin.legendReview") }}</legend>
            <label>
              {{ t("reselleradmin.resultLabel") }}
              <select v-model="profileForm.targetStatus" autofocus>
                <option value="">{{ t("reselleradmin.keepCurrent") }}</option>
                <option
                  v-for="status in currentProfileTransitions"
                  :key="status"
                  :value="status"
                >
                  {{ profileStatusLabel(status) }}
                </option>
              </select>
            </label>
            <p class="form-hint">
              {{ t("reselleradmin.reviewHint") }}
            </p>
          </fieldset>

          <fieldset>
            <legend>{{ t("reselleradmin.legendCredit") }}</legend>
            <div class="form-grid two-columns">
              <label>
                {{ t("reselleradmin.creditLabel") }}
                <div class="money-input">
                  <CircleDollarSign :size="15" />
                  <input
                    v-model="profileForm.creditLimitYuan"
                    inputmode="decimal"
                    min="0"
                    max="10000000"
                    :step="majorInputStep(editingProfile.currency)"
                  />
                </div>
                <small>{{ t("reselleradmin.creditRangeHint") }}</small>
              </label>
              <label>
                {{ t("reselleradmin.levelLabel") }}
                <select
                  v-model.number="profileForm.wholesaleLevel"
                  :disabled="
                    wholesaleTiersLoading || Boolean(wholesaleTiersError)
                  "
                >
                  <option disabled value="">
                    {{ t("reselleradmin.selectEnabledTier") }}
                  </option>
                  <option
                    v-for="tier in enabledWholesaleTiers"
                    :key="tier.id || tier.level"
                    :value="tier.level"
                  >
                    L{{ tier.level }} · {{ tier.name }} ·
                    {{ formatPercent(tier.discount_basis_point) }}
                  </option>
                </select>
                <small v-if="wholesaleTiersLoading">
                  {{ t("reselleradmin.tiers.loading") }}
                </small>
                <span v-else-if="wholesaleTiersError" class="field-load-error">
                  <small>{{ wholesaleTiersError }}</small>
                  <button type="button" @click="loadWholesaleTiers">
                    {{ t("reselleradmin.retry") }}
                  </button>
                </span>
                <small v-else-if="editingWholesaleTier">
                  {{
                    t("reselleradmin.selectedTierHint", {
                      name: editingWholesaleTier.name,
                      discount: formatPercent(
                        editingWholesaleTier.discount_basis_point,
                      ),
                    })
                  }}
                </small>
                <small v-else>{{ t("reselleradmin.levelRangeHint") }}</small>
              </label>
            </div>
            <p class="form-hint credit-policy-hint">
              {{ t("reselleradmin.settlementPolicyHint") }}
            </p>
          </fieldset>

          <fieldset>
            <legend>{{ t("reselleradmin.legendAudit") }}</legend>
            <label>
              {{ t("reselleradmin.auditReason") }}
              <textarea
                v-model="profileForm.reason"
                maxlength="500"
                :placeholder="t('reselleradmin.reasonPlaceholderProfile')"
              ></textarea>
            </label>
          </fieldset>

          <footer>
            <button type="button" :disabled="saving" @click="closeModal">
              {{ t("reselleradmin.cancel") }}
            </button>
            <button class="primary-button" type="submit" :disabled="saving">
              <LoaderCircle v-if="saving" class="spinning" :size="14" />
              <Check v-else :size="14" />{{ t("reselleradmin.confirmSubmit") }}
            </button>
          </footer>
        </form>

        <form
          v-else-if="editingDomain"
          class="reseller-form"
          @submit.prevent="submitDomain"
        >
          <div v-if="formError" class="form-alert error-notice">
            <AlertCircle :size="15" />{{ formError }}
          </div>

          <section class="identity-card">
            <span><Globe2 :size="18" /></span>
            <div>
              <b>{{ editingDomain.domain }}</b>
              <code>{{ editingDomain.id }}</code>
            </div>
            <span
              class="status-badge"
              :class="`status-${editingDomain.status}`"
            >
              {{ domainStatusLabel(editingDomain.status) }}
            </span>
          </section>

          <fieldset>
            <legend>{{ t("reselleradmin.legendOwnership") }}</legend>
            <div
              class="verification-card"
              :class="{ verified: editingDomain.verified_at }"
            >
              <BadgeCheck :size="18" />
              <div>
                <b>{{
                  editingDomain.verified_at
                    ? t("reselleradmin.userVerified")
                    : t("reselleradmin.waitingDns")
                }}</b>
                <span v-if="editingDomain.verified_at">
                  {{
                    t("reselleradmin.verifiedTime", {
                      time: formatTime(editingDomain.verified_at),
                    })
                  }}
                </span>
                <template v-else>
                  <span>{{ t("reselleradmin.cannotBypass") }}</span>
                  <code>{{
                    editingDomain.verification_token ||
                    t("reselleradmin.noToken")
                  }}</code>
                </template>
              </div>
            </div>
          </fieldset>

          <fieldset>
            <legend>{{ t("reselleradmin.legendRunTls") }}</legend>
            <div class="form-grid two-columns">
              <label>
                {{ t("reselleradmin.domainStatusLabel") }}
                <select
                  v-model="domainForm.status"
                  autofocus
                  @change="syncDomainTLS"
                >
                  <option value="">
                    {{ t("reselleradmin.selectDomainStatus") }}
                  </option>
                  <option
                    v-for="option in currentDomainStatusOptions"
                    :key="option.value"
                    :value="option.value"
                  >
                    {{ t(option.label) }}
                  </option>
                </select>
              </label>
              <label>
                {{ t("reselleradmin.tlsStatusLabel") }}
                <select v-model="domainForm.tlsStatus">
                  <option
                    v-for="option in tlsEditOptions"
                    :key="option.value"
                    :value="option.value"
                  >
                    {{ t(option.label) }}
                  </option>
                </select>
                <small>{{ t("reselleradmin.tlsMustActive") }}</small>
              </label>
            </div>
          </fieldset>

          <fieldset>
            <legend>{{ t("reselleradmin.legendAudit") }}</legend>
            <label>
              {{ t("reselleradmin.auditReason") }}
              <textarea
                v-model="domainForm.reason"
                maxlength="500"
                :placeholder="t('reselleradmin.reasonPlaceholderDomain')"
              ></textarea>
            </label>
          </fieldset>

          <footer>
            <button type="button" :disabled="saving" @click="closeModal">
              {{ t("reselleradmin.cancel") }}
            </button>
            <button class="primary-button" type="submit" :disabled="saving">
              <LoaderCircle v-if="saving" class="spinning" :size="14" />
              <ShieldCheck v-else :size="14" />{{
                t("reselleradmin.saveDomain")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>
  </section>
</template>

<style scoped>
.reseller-shell {
  display: grid;
  gap: 12px;
}

.reseller-nav {
  min-height: 58px;
  padding: 0 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  overflow: hidden;
}

.reseller-tabs {
  min-width: 0;
  align-self: stretch;
  display: flex;
  align-items: end;
  gap: 4px;
  overflow-x: auto;
  scrollbar-width: none;
}

.reseller-tabs::-webkit-scrollbar {
  display: none;
}

.reseller-tabs button {
  height: 43px;
  padding: 0 12px;
  border: 0;
  border-bottom: 2px solid transparent;
  display: flex;
  align-items: center;
  gap: 6px;
  background: transparent;
  color: var(--muted);
  font-size: 9px;
}

.reseller-tabs button.active {
  border-bottom-color: var(--text);
  color: var(--text);
}

.reseller-tabs button span {
  padding: 2px 5px;
  border-radius: 8px;
  background: var(--soft);
  font-size: 7px;
}

.nav-context {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--muted);
  font-size: 8px;
}

.reseller-panel {
  min-width: 0;
  overflow: hidden;
}

.reseller-toolbar {
  min-height: 58px;
  padding: 10px 13px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.reseller-search {
  width: min(440px, 100%);
  height: 34px;
  padding-left: 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: flex;
  align-items: center;
  gap: 7px;
  background: var(--surface-2);
  color: var(--muted);
}

.reseller-search input {
  min-width: 0;
  flex: 1;
  border: 0;
  outline: 0;
  background: transparent;
  font-size: 9px;
}

.reseller-search button,
.reseller-filters button,
.reseller-filters select {
  height: 28px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}

.reseller-search button {
  padding: 0 9px;
  border-top: 0;
  border-right: 0;
  border-bottom: 0;
  border-radius: 0;
  display: flex;
  align-items: center;
  gap: 4px;
}

.reseller-filters {
  display: flex;
  align-items: center;
  gap: 6px;
}

.reseller-filters select {
  min-width: 118px;
  padding: 0 8px;
}

.reseller-filters button {
  padding: 0 9px;
  display: flex;
  align-items: center;
  gap: 5px;
}

.reseller-filters button:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.reseller-notice,
.form-alert {
  margin: 11px 13px 0;
  padding: 9px 10px;
  border-radius: 5px;
  display: flex;
  align-items: flex-start;
  gap: 7px;
  font-size: 8px;
  line-height: 1.5;
}

.form-alert {
  position: sticky;
  top: 84px;
  z-index: 3;
}

.success-notice {
  background: color-mix(in srgb, var(--success) 9%, transparent);
  color: var(--success);
}

.error-notice {
  background: color-mix(in srgb, var(--danger) 9%, transparent);
  color: var(--danger);
}

.reseller-notice button {
  margin-left: auto;
  border: 0;
  background: transparent;
  color: inherit;
  font-size: 8px;
  font-weight: 700;
}

.reseller-state {
  min-height: 390px;
  padding: 30px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 9px;
  color: var(--muted);
  text-align: center;
  font-size: 9px;
}

.reseller-state strong {
  color: var(--text);
  font-size: 11px;
}

.reseller-table-wrap {
  width: 100%;
  min-height: 390px;
  overflow-x: auto;
}

.reseller-table {
  width: 100%;
  min-width: 1220px;
  border-collapse: collapse;
}

.reseller-table th,
.reseller-table td {
  padding: 13px 14px;
  border-bottom: 1px solid var(--line);
  text-align: left;
  vertical-align: middle;
}

.reseller-table th {
  background: var(--surface-2);
  color: var(--muted);
  font-size: 7px;
  font-weight: 600;
  letter-spacing: 0.04em;
}

.reseller-table td {
  font-size: 8px;
}

.reseller-table td > b,
.reseller-table td > time,
.reseller-table td > small,
.reseller-table td > code {
  display: block;
}

.credit-cell,
.tier-cell {
  min-width: 145px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.credit-cell b,
.tier-cell b {
  font-size: 8px;
}

.credit-cell small,
.tier-cell small {
  color: var(--muted);
  font-size: 7px;
  line-height: 1.45;
}

.credit-cell em,
.tier-cell em {
  color: var(--danger);
  font-size: 7px;
  font-style: normal;
  font-weight: 700;
}

.credit-cell.breached b,
.tier-cell.unavailable b {
  color: var(--danger);
}

.reseller-table td > b,
.reseller-table td > time {
  font-size: 8px;
  font-weight: 600;
}

.reseller-table td > small,
.reseller-table td > code,
.record-primary code {
  margin-top: 4px;
  color: var(--muted);
  font-size: 7px;
}

.reseller-table code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

.record-primary {
  min-width: 180px;
  display: flex;
  align-items: center;
  gap: 9px;
}

.record-primary > span {
  width: 30px;
  height: 30px;
  flex: 0 0 auto;
  border-radius: 6px;
  display: grid;
  place-items: center;
  background: var(--soft);
}

.record-primary div {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.record-primary b,
.record-primary code {
  max-width: 210px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.record-primary b {
  font-size: 9px;
}

.status-badge,
.tls-badge {
  width: fit-content;
  padding: 3px 7px;
  border-radius: 10px;
  display: block;
  background: var(--soft);
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
}

.status-active,
.status-verified,
.tls-active,
.verified-text {
  color: var(--success) !important;
}

.status-active,
.status-verified,
.tls-active {
  background: color-mix(in srgb, var(--success) 11%, transparent);
}

.status-pending,
.tls-pending,
.tls-provisioning {
  background: color-mix(in srgb, var(--warn) 11%, transparent);
  color: var(--warn);
}

.status-rejected,
.tls-failed {
  background: color-mix(in srgb, var(--danger) 10%, transparent);
  color: var(--danger);
}

.status-suspended,
.tls-disabled {
  opacity: 0.72;
}

.record-actions {
  text-align: right !important;
}

.record-actions button {
  height: 29px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}

.no-action {
  color: var(--muted);
  font-size: 7px;
}

.reseller-pagination {
  min-height: 53px;
  padding: 9px 13px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: var(--muted);
  font-size: 8px;
}

.reseller-pagination > div {
  display: flex;
  gap: 4px;
}

.reseller-pagination button,
.reseller-pagination select {
  min-width: 27px;
  height: 27px;
  padding: 0 7px;
  border: 1px solid var(--line);
  border-radius: 4px;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--muted);
  font-size: 8px;
}

.reseller-pagination button.active {
  background: var(--dark);
  color: var(--dark-text);
}

.reseller-pagination button:disabled {
  cursor: not-allowed;
  opacity: 0.4;
}

.reseller-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 120;
  padding: 24px;
  display: flex;
  justify-content: flex-end;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(2px);
}

.reseller-modal {
  width: min(650px, 100%);
  height: 100%;
  border: 1px solid var(--line);
  border-radius: 10px;
  overflow-y: auto;
  background: var(--surface);
  color: var(--text);
  box-shadow: var(--shadow);
}

.reseller-modal > header {
  position: sticky;
  top: 0;
  z-index: 4;
  min-height: 83px;
  padding: 17px 20px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 15px;
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  backdrop-filter: blur(12px);
}

.reseller-modal h2 {
  margin: 5px 0 3px;
  font-size: 17px;
  letter-spacing: -0.03em;
}

.reseller-modal header p {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
}

.reseller-modal > header > button {
  width: 31px;
  height: 31px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: grid;
  place-items: center;
  background: var(--surface);
}

.reseller-form {
  padding: 5px 20px 20px;
}

.reseller-form fieldset {
  margin: 0;
  padding: 18px 0;
  border: 0;
  border-bottom: 1px solid var(--line);
}

.reseller-form legend {
  margin-bottom: 13px;
  padding: 0;
  font-size: 10px;
  font-weight: 700;
}

.reseller-form label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: var(--muted);
  font-size: 8px;
  font-weight: 600;
}

.reseller-form input,
.reseller-form select,
.reseller-form textarea {
  width: 100%;
  min-height: 36px;
  padding: 8px 9px;
  border: 1px solid var(--line);
  border-radius: 5px;
  outline: none;
  background: var(--surface-2);
  color: var(--text);
  font-size: 9px;
}

.reseller-form input:focus,
.reseller-form select:focus,
.reseller-form textarea:focus {
  border-color: var(--text);
}

.reseller-form textarea {
  min-height: 88px;
  resize: vertical;
  line-height: 1.55;
}

.reseller-form small {
  color: var(--muted);
  font-size: 7px;
  font-weight: 400;
}

.identity-card {
  margin-top: 15px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  background: var(--surface-2);
}

.identity-card > span:first-child {
  width: 34px;
  height: 34px;
  border-radius: 7px;
  display: grid;
  place-items: center;
  background: var(--soft);
}

.identity-card div {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.identity-card b {
  font-size: 9px;
}

.identity-card code {
  overflow: hidden;
  color: var(--muted);
  font:
    500 7px ui-monospace,
    SFMono-Regular,
    Menlo,
    monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.credit-snapshot {
  margin-top: 10px;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 9px;
  background: var(--surface-2);
}

.credit-snapshot > div {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.credit-snapshot > div span {
  color: var(--muted);
  font-size: 7px;
}

.credit-snapshot > div strong {
  overflow: hidden;
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.credit-snapshot > p {
  grid-column: 1 / -1;
  margin: 2px 0 0;
  padding-top: 9px;
  border-top: 1px solid var(--line);
  display: flex;
  align-items: flex-start;
  gap: 6px;
  color: var(--muted);
  font-size: 7px;
  line-height: 1.55;
}

.credit-snapshot > p svg {
  width: 13px;
  flex: 0 0 auto;
}

.credit-snapshot.breached {
  border-color: color-mix(in srgb, var(--danger) 45%, var(--line));
}

.credit-snapshot.breached > p {
  color: var(--danger);
}

.credit-policy-hint {
  border-left-color: var(--text);
}

.field-load-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.field-load-error small {
  color: var(--danger) !important;
}

.field-load-error button {
  min-height: 24px;
  padding: 0 7px;
  border: 1px solid var(--line);
  border-radius: 4px;
  background: var(--surface);
  color: var(--text);
  font-size: 7px;
}

.form-grid {
  display: grid;
  gap: 12px;
}

.two-columns {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.form-hint {
  margin: 9px 0 0;
  padding: 8px 9px;
  border-left: 2px solid var(--warn);
  background: color-mix(in srgb, var(--warn) 7%, transparent);
  color: var(--muted);
  font-size: 7px;
  line-height: 1.55;
}

.money-input {
  position: relative;
}

.money-input svg {
  position: absolute;
  left: 9px;
  top: 10px;
  color: var(--muted);
  pointer-events: none;
}

.money-input input {
  padding-left: 31px;
}

.verification-card {
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: flex;
  align-items: flex-start;
  gap: 10px;
  background: color-mix(in srgb, var(--warn) 7%, var(--surface));
  color: var(--warn);
}

.verification-card.verified {
  background: color-mix(in srgb, var(--success) 7%, var(--surface));
  color: var(--success);
}

.verification-card div {
  min-width: 0;
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 5px;
}

.verification-card b {
  color: var(--text);
  font-size: 9px;
}

.verification-card span {
  color: var(--muted);
  font-size: 8px;
}

.verification-card code {
  padding: 7px 8px;
  border-radius: 4px;
  overflow-wrap: anywhere;
  background: var(--surface);
  color: var(--text);
  font:
    500 8px ui-monospace,
    SFMono-Regular,
    Menlo,
    monospace;
}

.reseller-form > footer {
  position: sticky;
  bottom: -20px;
  z-index: 3;
  margin: 0 -20px -20px;
  padding: 13px 20px;
  display: flex;
  justify-content: flex-end;
  gap: 7px;
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  backdrop-filter: blur(12px);
}

.reseller-form > footer > button:first-child {
  height: 36px;
  padding: 0 14px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  font-size: 8px;
}

.reseller-form > footer .primary-button {
  height: 36px;
  padding: 0 14px;
  font-size: 8px;
}

.spinning {
  animation: reseller-spin 0.8s linear infinite;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

@keyframes reseller-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 850px) {
  .reseller-toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .reseller-search {
    width: 100%;
  }

  .reseller-filters {
    justify-content: flex-end;
  }

  .nav-context span {
    display: none;
  }
}

@media (max-width: 660px) {
  .reseller-nav {
    padding: 0 9px;
  }

  .reseller-tabs button {
    padding: 0 8px;
  }

  .reseller-filters {
    flex-wrap: wrap;
  }

  .reseller-filters select {
    min-width: 0;
    flex: 1;
  }

  .reseller-table-wrap {
    padding: 9px;
  }

  .reseller-table {
    min-width: 0;
  }

  .reseller-table thead {
    display: none;
  }

  .reseller-table tbody,
  .reseller-table tr,
  .reseller-table td {
    display: block;
    width: 100%;
  }

  .reseller-table tr {
    margin-bottom: 9px;
    padding: 7px 10px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--surface);
  }

  .reseller-table td {
    min-height: 35px;
    padding: 8px 0 8px 99px;
    border-bottom: 1px solid var(--line);
    position: relative;
  }

  .reseller-table td::before {
    content: attr(data-label);
    position: absolute;
    left: 0;
    top: 10px;
    color: var(--muted);
    font-size: 7px;
  }

  .reseller-table td:last-child {
    border-bottom: 0;
  }

  .record-primary {
    min-width: 0;
  }

  .record-actions {
    text-align: left !important;
  }

  .reseller-pagination {
    align-items: flex-start;
    flex-direction: column;
  }

  .reseller-pagination > div {
    width: 100%;
    overflow-x: auto;
  }

  .reseller-modal-backdrop {
    padding: 0;
  }

  .reseller-modal {
    border-radius: 0;
  }

  .reseller-modal > header,
  .reseller-form {
    padding-right: 14px;
    padding-left: 14px;
  }

  .two-columns {
    grid-template-columns: 1fr;
  }

  .credit-snapshot {
    grid-template-columns: 1fr 1fr;
  }

  .identity-card {
    grid-template-columns: auto minmax(0, 1fr);
  }

  .identity-card > .status-badge {
    grid-column: 2;
  }

  .reseller-form > footer {
    margin-right: -14px;
    margin-left: -14px;
    padding-right: 14px;
    padding-left: 14px;
  }
}
</style>
