<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  AlertCircle,
  BadgePercent,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleDollarSign,
  Clock3,
  Edit3,
  LoaderCircle,
  Network,
  RefreshCw,
  Search,
  ShieldCheck,
  Wallet,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import { formatMoney as formatMinorMoney, storeCurrency } from "../utils/money";

const { t, locale } = useI18n();
const route = useRoute();
const authStore = useAuthStore();
const canManage = computed(() => authStore.hasPermission("marketing.manage"));

type AffiliateTab = "accounts" | "commissions" | "withdrawals";
type AffiliateStatus = "pending" | "active" | "suspended" | "rejected";
type CommissionStatus =
  "pending" | "available" | "partially_reversed" | "reversed";
type WithdrawalStatus = "pending" | "processing" | "completed" | "rejected";

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface AffiliateProfile {
  id: string;
  user_id: string;
  referral_code: string;
  commission_basis_point: number;
  status: AffiliateStatus | string;
  total_commission: number;
  available_commission: number;
  frozen_commission: number;
  currency: string;
  applied_at: string;
  approved_at?: string | null;
  rejected_at?: string | null;
  created_at: string;
  updated_at: string;
}

interface AffiliateCommission {
  id: string;
  affiliate_id: string;
  order_id: string;
  buyer_id?: string | null;
  order_amount: number;
  commission: number;
  reversed_amount: number;
  currency: string;
  status: CommissionStatus | string;
  settles_at: string;
  settled_at?: string | null;
  created_at: string;
  updated_at: string;
}

interface AffiliateWithdrawal {
  id: string;
  withdrawal_no: string;
  affiliate_id: string;
  referral_code?: string;
  user_email?: string;
  amount: number;
  fee: number;
  currency: string;
  method: string;
  account_preview: string;
  status: WithdrawalStatus | string;
  payout_reference: string;
  processed_by?: string | null;
  reason: string;
  processed_at?: string | null;
  created_at: string;
  updated_at: string;
}

interface AccountForm {
  targetStatus: AffiliateStatus | "";
  commissionPercent: number;
  reason: string;
}

interface WithdrawalForm {
  targetStatus: WithdrawalStatus | "";
  payoutReference: string;
  reason: string;
}

function accountStatusLabel(status: string) {
  const key = `affiliateadmin.accountStatus.${status}`;
  return t(key) === key ? status : t(key);
}
function commissionStatusLabel(status: string) {
  const key = `affiliateadmin.commissionStatus.${status}`;
  return t(key) === key ? status : t(key);
}
function withdrawalStatusLabel(status: string) {
  const key = `affiliateadmin.withdrawalStatus.${status}`;
  return t(key) === key ? status : t(key);
}
function payoutMethodLabel(method: string) {
  const key = `affiliateadmin.payoutMethod.${method}`;
  return t(key) === key ? method : t(key);
}
const accountStatusOptions = [
  { value: "", label: "affiliateadmin.accountStatusOptions.all" },
  { value: "pending", label: "affiliateadmin.accountStatusOptions.pending" },
  { value: "active", label: "affiliateadmin.accountStatusOptions.active" },
  {
    value: "suspended",
    label: "affiliateadmin.accountStatusOptions.suspended",
  },
  { value: "rejected", label: "affiliateadmin.accountStatusOptions.rejected" },
];
const commissionStatusOptions = [
  { value: "", label: "affiliateadmin.commissionStatusOptions.all" },
  { value: "pending", label: "affiliateadmin.commissionStatusOptions.pending" },
  {
    value: "available",
    label: "affiliateadmin.commissionStatusOptions.available",
  },
  {
    value: "partially_reversed",
    label: "affiliateadmin.commissionStatusOptions.partially_reversed",
  },
  {
    value: "reversed",
    label: "affiliateadmin.commissionStatusOptions.reversed",
  },
];
const withdrawalStatusOptions = [
  { value: "", label: "affiliateadmin.withdrawalStatusOptions.all" },
  { value: "pending", label: "affiliateadmin.withdrawalStatusOptions.pending" },
  {
    value: "processing",
    label: "affiliateadmin.withdrawalStatusOptions.processing",
  },
  {
    value: "completed",
    label: "affiliateadmin.withdrawalStatusOptions.completed",
  },
  {
    value: "rejected",
    label: "affiliateadmin.withdrawalStatusOptions.rejected",
  },
];
const accountTransitionMap: Record<AffiliateStatus, AffiliateStatus[]> = {
  pending: ["active", "rejected"],
  active: ["suspended"],
  suspended: ["active", "rejected"],
  rejected: ["pending"],
};
const withdrawalTransitionMap: Record<
  "pending" | "processing",
  WithdrawalStatus[]
> = {
  pending: ["processing", "rejected"],
  processing: ["completed", "rejected"],
};

const activeTab = ref<AffiliateTab>("accounts");
const accounts = ref<AffiliateProfile[]>([]);
const commissions = ref<AffiliateCommission[]>([]);
const withdrawals = ref<AffiliateWithdrawal[]>([]);
const knownAccounts = ref<AffiliateProfile[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const statusFilter = ref("");
const searchInput = ref("");
const appliedSearch = ref("");
const loading = ref(false);
const loadError = ref("");
const notice = ref("");
const modalKind = ref<"account" | "withdrawal" | null>(null);
const editingAccount = ref<AffiliateProfile | null>(null);
const editingWithdrawal = ref<AffiliateWithdrawal | null>(null);
const withdrawalDetail = ref<AffiliateWithdrawal | null>(null);
const maskedPayoutAccount = ref("");
const detailLoading = ref(false);
const detailError = ref("");
const saving = ref(false);
const formError = ref("");
const accountForm = ref<AccountForm>({
  targetStatus: "",
  commissionPercent: 5,
  reason: "",
});
const withdrawalForm = ref<WithdrawalForm>({
  targetStatus: "",
  payoutReference: "",
  reason: "",
});
let listRequest = 0;
let detailRequest = 0;

const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const pageNumbers = computed(() => {
  const first = Math.max(1, Math.min(page.value - 2, totalPages.value - 4));
  const last = Math.min(totalPages.value, first + 4);
  return Array.from({ length: last - first + 1 }, (_, index) => first + index);
});
const activeItemsCount = computed(() => {
  if (activeTab.value === "accounts") return accounts.value.length;
  if (activeTab.value === "commissions") return commissions.value.length;
  return withdrawals.value.length;
});
const accountLookup = computed(
  () => new Map(knownAccounts.value.map((item) => [item.id, item])),
);
const normalizedSearch = computed(() => appliedSearch.value.toLowerCase());
const visibleAccounts = computed(() => {
  if (!normalizedSearch.value) return accounts.value;
  return accounts.value.filter((item) =>
    [item.id, item.user_id, item.referral_code].some((value) =>
      String(value || "")
        .toLowerCase()
        .includes(normalizedSearch.value),
    ),
  );
});
const visibleCommissions = computed(() => {
  if (!normalizedSearch.value) return commissions.value;
  return commissions.value.filter((item) =>
    [item.id, item.affiliate_id, item.order_id, item.buyer_id].some((value) =>
      String(value || "")
        .toLowerCase()
        .includes(normalizedSearch.value),
    ),
  );
});
const visibleCount = computed(() => {
  if (activeTab.value === "accounts") return visibleAccounts.value.length;
  if (activeTab.value === "commissions") return visibleCommissions.value.length;
  return withdrawals.value.length;
});
const currentAccountTransitions = computed(() =>
  editingAccount.value
    ? accountTransitions(editingAccount.value.status)
    : ([] as AffiliateStatus[]),
);
const currentWithdrawal = computed(
  () => withdrawalDetail.value || editingWithdrawal.value,
);
const currentWithdrawalTransitions = computed(() =>
  currentWithdrawal.value
    ? withdrawalTransitions(currentWithdrawal.value.status)
    : ([] as WithdrawalStatus[]),
);

function apiMessage(error: unknown, fallback: string) {
  const failure = error as { response?: { data?: { message?: string } } };
  return failure.response?.data?.message || fallback;
}

function formatMoney(value?: number, currency?: string) {
  return formatMinorMoney(value, currency || storeCurrency.value, locale.value);
}

function formatPercent(basisPoints?: number) {
  return `${((Number(basisPoints) || 0) / 100)
    .toFixed(2)
    .replace(/\.00$/, "")}%`;
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

function shortID(value?: string | null) {
  if (!value) return "—";
  return value.length > 20 ? `${value.slice(0, 8)}…${value.slice(-6)}` : value;
}

function validReason(value: string) {
  const length = [...value.trim()].length;
  return length >= 4 && length <= 500;
}

function accountTransitions(status: string) {
  return accountTransitionMap[status as AffiliateStatus] || [];
}

function withdrawalTransitions(status: string) {
  if (status !== "pending" && status !== "processing") return [];
  return withdrawalTransitionMap[status];
}

function accountActionLabel(status: string) {
  switch (status) {
    case "pending":
      return "affiliateadmin.actionReviewAccount";
    case "active":
      return "affiliateadmin.actionSuspendAccount";
    case "suspended":
      return "affiliateadmin.actionRestoreReject";
    case "rejected":
      return "affiliateadmin.actionResubmit";
    default:
      return "affiliateadmin.actionNone";
  }
}

function withdrawalActionLabel(status: string) {
  return status === "pending" || status === "processing"
    ? "affiliateadmin.actionReviewWithdrawal"
    : "affiliateadmin.actionViewDetail";
}

function mergeKnownAccounts(items: AffiliateProfile[]) {
  const merged = new Map(knownAccounts.value.map((item) => [item.id, item]));
  for (const item of items) merged.set(item.id, item);
  knownAccounts.value = [...merged.values()];
}

function currentStatusOptions() {
  if (activeTab.value === "accounts") return accountStatusOptions;
  if (activeTab.value === "commissions") return commissionStatusOptions;
  return withdrawalStatusOptions;
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
    if (tab === "withdrawals" && appliedSearch.value)
      params.q = appliedSearch.value;
    const endpoint =
      tab === "accounts"
        ? "/operations/affiliates"
        : tab === "commissions"
          ? "/operations/affiliate-commissions"
          : "/affiliate-withdrawals";
    const { data } = await adminApi.get(endpoint, { params });
    if (request !== listRequest || tab !== activeTab.value) return;
    const payload = data.data as PagePayload<
      AffiliateProfile | AffiliateCommission | AffiliateWithdrawal
    >;
    const items = Array.isArray(payload.items) ? payload.items : [];
    if (tab === "accounts") {
      accounts.value = items as AffiliateProfile[];
      mergeKnownAccounts(accounts.value);
    } else if (tab === "commissions") {
      commissions.value = items as AffiliateCommission[];
    } else {
      withdrawals.value = items as AffiliateWithdrawal[];
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
    if (tab === "accounts") accounts.value = [];
    else if (tab === "commissions") commissions.value = [];
    else withdrawals.value = [];
    total.value = 0;
    loadError.value = apiMessage(error, t("affiliateadmin.errLoad"));
  } finally {
    if (request === listRequest) loading.value = false;
  }
}

async function applySearch() {
  appliedSearch.value = searchInput.value.trim();
  if (activeTab.value === "withdrawals") {
    page.value = 1;
    await loadList();
  }
}

async function clearSearch() {
  searchInput.value = "";
  appliedSearch.value = "";
  if (activeTab.value === "withdrawals") {
    page.value = 1;
    await loadList();
  }
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

function openAccount(item: AffiliateProfile) {
  if (!canManage.value) return;
  editingAccount.value = item;
  editingWithdrawal.value = null;
  accountForm.value = {
    targetStatus: "",
    commissionPercent: item.commission_basis_point / 100,
    reason: "",
  };
  formError.value = "";
  modalKind.value = "account";
}

function maskAccount(value: string) {
  const runes = [...value.trim()];
  if (!runes.length) return "••••";
  return `••••${runes.slice(-4).join("")}`;
}

async function loadWithdrawalDetail() {
  const item = editingWithdrawal.value;
  if (!item) return;
  const request = ++detailRequest;
  detailLoading.value = true;
  detailError.value = "";
  try {
    const { data } = await adminApi.get(
      `/affiliate-withdrawals/${encodeURIComponent(item.id)}`,
    );
    if (request !== detailRequest || editingWithdrawal.value?.id !== item.id)
      return;
    const payload = data.data as {
      withdrawal: AffiliateWithdrawal;
      account?: string;
    };
    if (!payload?.withdrawal) throw new Error("withdrawal detail unavailable");
    const rawAccount = String(payload.account || "");
    maskedPayoutAccount.value =
      payload.withdrawal.account_preview || maskAccount(rawAccount);
    payload.account = "";
    withdrawalDetail.value = { ...item, ...payload.withdrawal };
    withdrawalForm.value.payoutReference =
      payload.withdrawal.payout_reference || "";
  } catch (error: unknown) {
    if (request !== detailRequest) return;
    detailError.value = apiMessage(error, t("affiliateadmin.errDetail"));
  } finally {
    if (request === detailRequest) detailLoading.value = false;
  }
}

function openWithdrawal(item: AffiliateWithdrawal) {
  editingWithdrawal.value = item;
  editingAccount.value = null;
  withdrawalDetail.value = null;
  maskedPayoutAccount.value = item.account_preview || "••••";
  withdrawalForm.value = {
    targetStatus: "",
    payoutReference: item.payout_reference || "",
    reason: "",
  };
  formError.value = "";
  detailError.value = "";
  modalKind.value = "withdrawal";
  void loadWithdrawalDetail();
}

function closeModal() {
  if (saving.value) return;
  detailRequest += 1;
  modalKind.value = null;
  editingAccount.value = null;
  editingWithdrawal.value = null;
  withdrawalDetail.value = null;
  maskedPayoutAccount.value = "";
  detailError.value = "";
  formError.value = "";
}

function validateAccount() {
  const item = editingAccount.value;
  const form = accountForm.value;
  if (!item) return t("affiliateadmin.errAccountContext");
  if (
    !accountTransitions(item.status).includes(
      form.targetStatus as AffiliateStatus,
    )
  )
    return t("affiliateadmin.errTransition");
  if (
    typeof form.commissionPercent !== "number" ||
    !Number.isFinite(form.commissionPercent) ||
    form.commissionPercent < 0.01 ||
    form.commissionPercent > 30 ||
    Math.abs(
      form.commissionPercent * 100 - Math.round(form.commissionPercent * 100),
    ) > 0.000001
  )
    return t("affiliateadmin.errRateRange");
  if (!validReason(form.reason)) return t("affiliateadmin.errReason");
  return "";
}

async function submitAccount() {
  if (!canManage.value) return;
  const validation = validateAccount();
  if (validation) {
    formError.value = validation;
    return;
  }
  const item = editingAccount.value;
  if (!item) return;
  const form = accountForm.value;
  saving.value = true;
  formError.value = "";
  try {
    await adminApi.patch(
      `/affiliates/${encodeURIComponent(item.id)}`,
      {
        status: form.targetStatus,
        commission_basis_point: Math.round(form.commissionPercent * 100),
      },
      { headers: { "X-Change-Reason": form.reason.trim() } },
    );
    modalKind.value = null;
    editingAccount.value = null;
    notice.value = t("affiliateadmin.accountUpdated", {
      code: item.referral_code,
      status: accountStatusLabel(form.targetStatus),
    });
    await loadList();
  } catch (error: unknown) {
    formError.value = apiMessage(error, t("affiliateadmin.errAccountSave"));
  } finally {
    saving.value = false;
  }
}

function validateWithdrawal() {
  const item = currentWithdrawal.value;
  const form = withdrawalForm.value;
  if (!item || !withdrawalDetail.value)
    return t("affiliateadmin.errWaitDetail");
  if (
    !withdrawalTransitions(item.status).includes(
      form.targetStatus as WithdrawalStatus,
    )
  )
    return t("affiliateadmin.errWithdrawalTransition");
  const referenceLength = [...form.payoutReference.trim()].length;
  if (
    form.targetStatus === "completed" &&
    (referenceLength < 4 || referenceLength > 160)
  )
    return t("affiliateadmin.errRefRequired");
  if (referenceLength > 0 && referenceLength < 4)
    return t("affiliateadmin.errRefTooShort");
  if (referenceLength > 160) return t("affiliateadmin.errRefTooLong");
  if (!validReason(form.reason)) return t("affiliateadmin.errWithdrawalReason");
  return "";
}

async function submitWithdrawal() {
  if (!canManage.value) return;
  const validation = validateWithdrawal();
  if (validation) {
    formError.value = validation;
    return;
  }
  const item = currentWithdrawal.value;
  if (!item) return;
  const form = withdrawalForm.value;
  saving.value = true;
  formError.value = "";
  try {
    await adminApi.patch(
      `/affiliate-withdrawals/${encodeURIComponent(item.id)}`,
      {
        status: form.targetStatus,
        payout_reference: form.payoutReference.trim(),
      },
      { headers: { "X-Change-Reason": form.reason.trim() } },
    );
    modalKind.value = null;
    editingWithdrawal.value = null;
    withdrawalDetail.value = null;
    notice.value = t("affiliateadmin.withdrawalUpdated", {
      no: item.withdrawal_no,
      status: withdrawalStatusLabel(form.targetStatus),
    });
    await loadList();
  } catch (error: unknown) {
    formError.value = apiMessage(error, t("affiliateadmin.errWithdrawalSave"));
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
    const requested = String(defaultTab || "accounts") as AffiliateTab;
    activeTab.value = ["accounts", "commissions", "withdrawals"].includes(
      requested,
    )
      ? requested
      : "accounts";
    page.value = 1;
    statusFilter.value = "";
    searchInput.value = "";
    appliedSearch.value = "";
    notice.value = "";
    await loadList();
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
  <section class="affiliate-shell">
    <div class="affiliate-panel panel">
      <header class="affiliate-toolbar">
        <form class="affiliate-search" @submit.prevent="applySearch">
          <Search :size="15" />
          <input
            v-model="searchInput"
            type="search"
            :placeholder="
              activeTab === 'withdrawals'
                ? t('affiliateadmin.searchPlaceholderWithdrawals')
                : activeTab === 'accounts'
                  ? t('affiliateadmin.searchPlaceholderAccounts')
                  : t('affiliateadmin.searchPlaceholderCommissions')
            "
            :aria-label="t('affiliateadmin.searchAria')"
          />
          <button v-if="appliedSearch" type="button" @click="clearSearch">
            <X :size="13" />{{ t("affiliateadmin.clear") }}
          </button>
          <button type="submit">
            {{
              activeTab === "withdrawals"
                ? t("affiliateadmin.search")
                : t("affiliateadmin.filter")
            }}
          </button>
        </form>
        <div class="affiliate-filters">
          <select
            v-model="statusFilter"
            :aria-label="t('affiliateadmin.statusFilterAria')"
            @change="applyStatusFilter"
          >
            <option
              v-for="option in currentStatusOptions()"
              :key="option.value || 'all'"
              :value="option.value"
            >
              {{ t(option.label) }}
            </option>
          </select>
          <button type="button" :disabled="loading" @click="loadList">
            <RefreshCw :size="14" :class="{ spinning: loading }" />{{
              t("affiliateadmin.refresh")
            }}
          </button>
        </div>
      </header>

      <div v-if="notice" class="affiliate-notice success-notice">
        <Check :size="15" />{{ notice }}
      </div>
      <div v-if="loadError" class="affiliate-notice error-notice">
        <AlertCircle :size="15" />{{ loadError }}
        <button type="button" @click="loadList">
          {{ t("affiliateadmin.retry") }}
        </button>
      </div>

      <div v-if="loading && !activeItemsCount" class="affiliate-state">
        <LoaderCircle class="spinning" :size="23" />
        <span>{{ t("affiliateadmin.loading") }}</span>
      </div>
      <div v-else-if="!loadError && !visibleCount" class="affiliate-state">
        <Network v-if="activeTab === 'accounts'" :size="27" />
        <BadgePercent v-else-if="activeTab === 'commissions'" :size="27" />
        <Wallet v-else :size="27" />
        <strong>{{
          appliedSearch || statusFilter
            ? t("affiliateadmin.noMatch")
            : activeTab === "accounts"
              ? t("affiliateadmin.noAccounts")
              : activeTab === "commissions"
                ? t("affiliateadmin.noCommissions")
                : t("affiliateadmin.noWithdrawals")
        }}</strong>
        <span v-if="appliedSearch && activeTab !== 'withdrawals'">
          {{ t("affiliateadmin.noMatchHint") }}
        </span>
      </div>

      <div v-else-if="activeTab === 'accounts'" class="affiliate-table-wrap">
        <table class="affiliate-table">
          <thead>
            <tr>
              <th>{{ t("affiliateadmin.colAccount") }}</th>
              <th>{{ t("affiliateadmin.colRate") }}</th>
              <th>{{ t("affiliateadmin.colTotal") }}</th>
              <th>{{ t("affiliateadmin.colAvailable") }}</th>
              <th>{{ t("affiliateadmin.colApplied") }}</th>
              <th>{{ t("affiliateadmin.colStatus") }}</th>
              <th>
                <span class="sr-only">{{
                  t("affiliateadmin.colActions")
                }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in visibleAccounts" :key="item.id">
              <td :data-label="t('affiliateadmin.colAccount')">
                <div class="record-primary">
                  <span><Network :size="15" /></span>
                  <div>
                    <b>{{ item.referral_code }}</b>
                    <code :title="item.user_id">{{
                      t("affiliateadmin.user", { id: shortID(item.user_id) })
                    }}</code>
                  </div>
                </div>
              </td>
              <td :data-label="t('affiliateadmin.colRate')">
                <b>{{ formatPercent(item.commission_basis_point) }}</b>
                <small>{{ t("affiliateadmin.rateHint") }}</small>
              </td>
              <td :data-label="t('affiliateadmin.colTotal')">
                <b>{{ formatMoney(item.total_commission, item.currency) }}</b>
                <small>{{ t("affiliateadmin.totalHint") }}</small>
              </td>
              <td :data-label="t('affiliateadmin.colAvailable')">
                <b>{{
                  formatMoney(item.available_commission, item.currency)
                }}</b>
                <small>{{
                  t("affiliateadmin.frozen", {
                    amount: formatMoney(item.frozen_commission, item.currency),
                  })
                }}</small>
              </td>
              <td :data-label="t('affiliateadmin.colApplied')">
                <time>{{
                  formatTime(item.applied_at || item.created_at)
                }}</time>
                <small>{{
                  item.approved_at
                    ? t("affiliateadmin.approvedAt", {
                        time: formatTime(item.approved_at),
                      })
                    : item.rejected_at
                      ? t("affiliateadmin.rejectedAt", {
                          time: formatTime(item.rejected_at),
                        })
                      : t("affiliateadmin.notReviewed")
                }}</small>
              </td>
              <td :data-label="t('affiliateadmin.colStatus')">
                <span class="status-badge" :class="`status-${item.status}`">
                  {{ accountStatusLabel(item.status) }}
                </span>
              </td>
              <td
                :data-label="t('affiliateadmin.colActions')"
                class="record-actions"
              >
                <button
                  v-if="canManage && accountTransitions(item.status).length"
                  type="button"
                  @click="openAccount(item)"
                >
                  <Edit3 :size="13" />{{ t(accountActionLabel(item.status)) }}
                </button>
                <span
                  v-else-if="!accountTransitions(item.status).length"
                  class="no-action"
                  >{{ t("affiliateadmin.noTransition") }}</span
                >
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else-if="activeTab === 'commissions'" class="affiliate-table-wrap">
        <table class="affiliate-table commission-table">
          <thead>
            <tr>
              <th>{{ t("affiliateadmin.colRecord") }}</th>
              <th>{{ t("affiliateadmin.colOrder") }}</th>
              <th>{{ t("affiliateadmin.colOrderAmount") }}</th>
              <th>{{ t("affiliateadmin.colGross") }}</th>
              <th>{{ t("affiliateadmin.colNet") }}</th>
              <th>{{ t("affiliateadmin.colSettles") }}</th>
              <th>{{ t("affiliateadmin.colStatus") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in visibleCommissions" :key="item.id">
              <td :data-label="t('affiliateadmin.colRecord')">
                <div class="record-primary">
                  <span><BadgePercent :size="15" /></span>
                  <div>
                    <b>{{
                      accountLookup.get(item.affiliate_id)?.referral_code ||
                      t("affiliateadmin.accountFallback")
                    }}</b>
                    <code :title="item.affiliate_id">{{
                      shortID(item.affiliate_id)
                    }}</code>
                  </div>
                </div>
              </td>
              <td :data-label="t('affiliateadmin.colOrder')">
                <code :title="item.order_id">{{ shortID(item.order_id) }}</code>
                <small :title="item.buyer_id || ''">
                  {{
                    t("affiliateadmin.buyer", { id: shortID(item.buyer_id) })
                  }}
                </small>
              </td>
              <td :data-label="t('affiliateadmin.colOrderAmount')">
                <b>{{ formatMoney(item.order_amount, item.currency) }}</b>
              </td>
              <td :data-label="t('affiliateadmin.colGross')">
                <b>{{ formatMoney(item.commission, item.currency) }}</b>
                <small>{{
                  t("affiliateadmin.reversed", {
                    amount: formatMoney(item.reversed_amount, item.currency),
                  })
                }}</small>
              </td>
              <td :data-label="t('affiliateadmin.colNet')">
                <b>{{
                  formatMoney(
                    item.commission - item.reversed_amount,
                    item.currency,
                  )
                }}</b>
              </td>
              <td :data-label="t('affiliateadmin.colSettles')">
                <time>{{ formatTime(item.settles_at) }}</time>
                <small v-if="item.settled_at">
                  {{
                    t("affiliateadmin.actualSettled", {
                      time: formatTime(item.settled_at),
                    })
                  }}
                </small>
              </td>
              <td :data-label="t('affiliateadmin.colStatus')">
                <span class="status-badge" :class="`status-${item.status}`">
                  {{ commissionStatusLabel(item.status) }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="affiliate-table-wrap">
        <table class="affiliate-table withdrawal-table">
          <thead>
            <tr>
              <th>{{ t("affiliateadmin.colWithdrawal") }}</th>
              <th>{{ t("affiliateadmin.colUser") }}</th>
              <th>{{ t("affiliateadmin.colAmount") }}</th>
              <th>{{ t("affiliateadmin.colMethod") }}</th>
              <th>{{ t("affiliateadmin.colSubmitted") }}</th>
              <th>{{ t("affiliateadmin.colStatus") }}</th>
              <th>
                <span class="sr-only">{{
                  t("affiliateadmin.colActions")
                }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in withdrawals" :key="item.id">
              <td :data-label="t('affiliateadmin.colWithdrawal')">
                <div class="record-primary">
                  <span><Wallet :size="15" /></span>
                  <div>
                    <b>{{ item.withdrawal_no }}</b>
                    <code :title="item.id">{{ shortID(item.id) }}</code>
                  </div>
                </div>
              </td>
              <td :data-label="t('affiliateadmin.colUser')">
                <b>{{ item.referral_code || shortID(item.affiliate_id) }}</b>
                <small>{{
                  item.user_email || t("affiliateadmin.noEmail")
                }}</small>
              </td>
              <td :data-label="t('affiliateadmin.colAmount')">
                <b>{{ formatMoney(item.amount, item.currency) }}</b>
                <small>{{
                  t("affiliateadmin.fee", {
                    amount: formatMoney(item.fee, item.currency),
                  })
                }}</small>
              </td>
              <td :data-label="t('affiliateadmin.colMethod')">
                <b>{{ payoutMethodLabel(item.method) }}</b>
                <small>{{ item.account_preview || "••••" }}</small>
              </td>
              <td :data-label="t('affiliateadmin.colSubmitted')">
                <time>{{ formatTime(item.created_at) }}</time>
                <small v-if="item.processed_at">
                  {{
                    t("affiliateadmin.processedAt", {
                      time: formatTime(item.processed_at),
                    })
                  }}
                </small>
              </td>
              <td :data-label="t('affiliateadmin.colStatus')">
                <span class="status-badge" :class="`status-${item.status}`">
                  {{ withdrawalStatusLabel(item.status) }}
                </span>
              </td>
              <td
                :data-label="t('affiliateadmin.colActions')"
                class="record-actions"
              >
                <button type="button" @click="openWithdrawal(item)">
                  <Edit3 :size="13" />{{
                    t(withdrawalActionLabel(item.status))
                  }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <footer v-if="!loadError" class="affiliate-pagination">
        <span>
          {{ t("affiliateadmin.pageInfo", { page, pages: totalPages, total }) }}
          <template v-if="appliedSearch && activeTab !== 'withdrawals'">
            {{ t("affiliateadmin.pageMatch", { count: visibleCount }) }}
          </template>
        </span>
        <div>
          <button
            type="button"
            :aria-label="t('affiliateadmin.prevPage')"
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
            :aria-label="t('affiliateadmin.nextPage')"
            :disabled="page >= totalPages || loading"
            @click="changePage(page + 1)"
          >
            <ChevronRight :size="14" />
          </button>
          <select
            v-model.number="pageSize"
            :aria-label="t('affiliateadmin.pageSizeAria')"
            @change="changePageSize"
          >
            <option :value="10">
              {{ t("affiliateadmin.perPage", { size: 10 }) }}
            </option>
            <option :value="20">
              {{ t("affiliateadmin.perPage", { size: 20 }) }}
            </option>
            <option :value="50">
              {{ t("affiliateadmin.perPage", { size: 50 }) }}
            </option>
          </select>
        </div>
      </footer>
    </div>

    <div
      v-if="modalKind"
      class="affiliate-modal-backdrop"
      role="presentation"
      @mousedown.self="closeModal"
    >
      <section
        class="affiliate-modal"
        role="dialog"
        aria-modal="true"
        :aria-label="
          modalKind === 'account'
            ? t('affiliateadmin.modalAccountAria')
            : t('affiliateadmin.modalWithdrawalAria')
        "
      >
        <header>
          <div>
            <span class="kicker">{{ t("affiliateadmin.kicker") }}</span>
            <h2>
              {{
                modalKind === "account"
                  ? t("affiliateadmin.modalAccountTitle")
                  : t("affiliateadmin.modalWithdrawalTitle")
              }}
            </h2>
            <p>
              {{
                modalKind === "account"
                  ? editingAccount?.referral_code
                  : editingWithdrawal?.withdrawal_no
              }}
            </p>
          </div>
          <button
            type="button"
            :aria-label="t('affiliateadmin.close')"
            :disabled="saving"
            @click="closeModal"
          >
            <X :size="18" />
          </button>
        </header>

        <form
          v-if="modalKind === 'account' && editingAccount && canManage"
          class="affiliate-form"
          @submit.prevent="submitAccount"
        >
          <div v-if="formError" class="form-alert error-notice">
            <AlertCircle :size="15" />{{ formError }}
          </div>
          <section class="identity-card">
            <span><Network :size="18" /></span>
            <div>
              <b>{{ editingAccount.referral_code }}</b>
              <code>{{ editingAccount.id }}</code>
            </div>
            <span
              class="status-badge"
              :class="`status-${editingAccount.status}`"
            >
              {{ accountStatusLabel(editingAccount.status) }}
            </span>
          </section>

          <fieldset>
            <legend>{{ t("affiliateadmin.legendReview") }}</legend>
            <label>
              {{ t("affiliateadmin.resultLabel") }}
              <select v-model="accountForm.targetStatus" autofocus>
                <option value="">{{ t("affiliateadmin.selectResult") }}</option>
                <option
                  v-for="status in currentAccountTransitions"
                  :key="status"
                  :value="status"
                >
                  {{ accountStatusLabel(status) }}
                </option>
              </select>
            </label>
            <p class="form-hint">
              {{ t("affiliateadmin.reviewHint") }}
            </p>
          </fieldset>

          <fieldset>
            <legend>{{ t("affiliateadmin.legendRules") }}</legend>
            <div class="form-grid two-columns">
              <label>
                {{ t("affiliateadmin.rateLabel") }}
                <input
                  v-model.number="accountForm.commissionPercent"
                  type="number"
                  min="0.01"
                  max="30"
                  step="0.01"
                />
                <small>{{ t("affiliateadmin.rateRangeHint") }}</small>
              </label>
              <div class="balance-card">
                <span><CircleDollarSign :size="16" /></span>
                <div>
                  <small>{{ t("affiliateadmin.availableFrozen") }}</small>
                  <b>
                    {{
                      formatMoney(
                        editingAccount.available_commission,
                        editingAccount.currency,
                      )
                    }}
                    /
                    {{
                      formatMoney(
                        editingAccount.frozen_commission,
                        editingAccount.currency,
                      )
                    }}
                  </b>
                </div>
              </div>
            </div>
          </fieldset>

          <fieldset>
            <legend>{{ t("affiliateadmin.legendAudit") }}</legend>
            <label>
              {{ t("affiliateadmin.auditReason") }}
              <textarea
                v-model="accountForm.reason"
                maxlength="500"
                :placeholder="t('affiliateadmin.reasonPlaceholderAccount')"
              ></textarea>
            </label>
          </fieldset>

          <footer>
            <button type="button" :disabled="saving" @click="closeModal">
              {{ t("affiliateadmin.cancel") }}
            </button>
            <button class="primary-button" type="submit" :disabled="saving">
              <LoaderCircle v-if="saving" class="spinning" :size="14" />
              <Check v-else :size="14" />{{ t("affiliateadmin.confirmSubmit") }}
            </button>
          </footer>
        </form>

        <div v-else-if="editingWithdrawal" class="withdrawal-detail">
          <div v-if="formError" class="form-alert error-notice">
            <AlertCircle :size="15" />{{ formError }}
          </div>
          <div v-if="detailLoading" class="detail-state">
            <LoaderCircle class="spinning" :size="20" />{{
              t("affiliateadmin.loadingDetail")
            }}
          </div>
          <div v-else-if="detailError" class="detail-state error-detail">
            <AlertCircle :size="20" />
            <b>{{ detailError }}</b>
            <button type="button" @click="loadWithdrawalDetail">
              {{ t("affiliateadmin.retry") }}
            </button>
          </div>
          <template v-else-if="currentWithdrawal">
            <section class="withdrawal-summary">
              <div>
                <small>{{ t("affiliateadmin.amount") }}</small>
                <b>{{
                  formatMoney(
                    currentWithdrawal.amount,
                    currentWithdrawal.currency,
                  )
                }}</b>
              </div>
              <div>
                <small>{{ t("affiliateadmin.feeAmount") }}</small>
                <b>{{
                  formatMoney(currentWithdrawal.fee, currentWithdrawal.currency)
                }}</b>
              </div>
              <div>
                <small>{{ t("affiliateadmin.estimated") }}</small>
                <b>{{
                  formatMoney(
                    currentWithdrawal.amount - currentWithdrawal.fee,
                    currentWithdrawal.currency,
                  )
                }}</b>
              </div>
              <span
                class="status-badge"
                :class="`status-${currentWithdrawal.status}`"
              >
                {{ withdrawalStatusLabel(currentWithdrawal.status) }}
              </span>
            </section>

            <section class="payout-card">
              <span><Wallet :size="18" /></span>
              <div>
                <small>{{ t("affiliateadmin.method") }}</small>
                <b>{{ payoutMethodLabel(currentWithdrawal.method) }}</b>
                <code>{{ maskedPayoutAccount || "••••" }}</code>
              </div>
              <div class="privacy-note">
                <ShieldCheck :size="14" />{{ t("affiliateadmin.privacyNote") }}
              </div>
            </section>

            <section class="detail-grid">
              <div>
                <small>{{ t("affiliateadmin.codeUser") }}</small>
                <b>{{
                  currentWithdrawal.referral_code ||
                  accountLookup.get(currentWithdrawal.affiliate_id)
                    ?.referral_code ||
                  shortID(currentWithdrawal.affiliate_id)
                }}</b>
                <span>{{ currentWithdrawal.user_email || "—" }}</span>
              </div>
              <div>
                <small>{{ t("affiliateadmin.submittedAt") }}</small>
                <b>{{ formatTime(currentWithdrawal.created_at) }}</b>
              </div>
              <div>
                <small>{{ t("affiliateadmin.payoutRef") }}</small>
                <b>{{
                  currentWithdrawal.payout_reference ||
                  t("affiliateadmin.notFilled")
                }}</b>
              </div>
              <div>
                <small>{{ t("affiliateadmin.processedBy") }}</small>
                <b>{{ formatTime(currentWithdrawal.processed_at) }}</b>
                <span>{{ shortID(currentWithdrawal.processed_by) }}</span>
              </div>
              <div class="wide-detail">
                <small>{{ t("affiliateadmin.recordedReason") }}</small>
                <b>{{
                  currentWithdrawal.reason || t("affiliateadmin.notProcessed")
                }}</b>
              </div>
            </section>

            <form
              v-if="canManage && currentWithdrawalTransitions.length"
              class="withdrawal-form"
              @submit.prevent="submitWithdrawal"
            >
              <fieldset>
                <legend>{{ t("affiliateadmin.legendProcess") }}</legend>
                <div class="form-grid two-columns">
                  <label>
                    {{ t("affiliateadmin.nextStatus") }}
                    <select v-model="withdrawalForm.targetStatus" autofocus>
                      <option value="">
                        {{ t("affiliateadmin.selectResult") }}
                      </option>
                      <option
                        v-for="status in currentWithdrawalTransitions"
                        :key="status"
                        :value="status"
                      >
                        {{ withdrawalStatusLabel(status) }}
                      </option>
                    </select>
                  </label>
                  <label>
                    {{
                      withdrawalForm.targetStatus === "completed"
                        ? t("affiliateadmin.refCompleted")
                        : t("affiliateadmin.refOptional")
                    }}
                    <input
                      v-model="withdrawalForm.payoutReference"
                      maxlength="160"
                      :placeholder="
                        withdrawalForm.targetStatus === 'completed'
                          ? t('affiliateadmin.refPlaceholderCompleted')
                          : t('affiliateadmin.refPlaceholderOptional')
                      "
                    />
                  </label>
                </div>
                <label>
                  {{ t("affiliateadmin.reasonLabel") }}
                  <textarea
                    v-model="withdrawalForm.reason"
                    maxlength="500"
                    :placeholder="
                      t('affiliateadmin.reasonPlaceholderWithdrawal')
                    "
                  ></textarea>
                </label>
              </fieldset>
              <footer>
                <button type="button" :disabled="saving" @click="closeModal">
                  {{ t("affiliateadmin.cancel") }}
                </button>
                <button class="primary-button" type="submit" :disabled="saving">
                  <LoaderCircle v-if="saving" class="spinning" :size="14" />
                  <ShieldCheck v-else :size="14" />{{
                    t("affiliateadmin.confirmProcess")
                  }}
                </button>
              </footer>
            </form>
            <footer
              v-else-if="!currentWithdrawalTransitions.length"
              class="detail-footer"
            >
              <span
                ><Clock3 :size="14" />{{
                  t("affiliateadmin.terminalNote")
                }}</span
              >
              <button type="button" @click="closeModal">
                {{ t("affiliateadmin.close") }}
              </button>
            </footer>
          </template>
        </div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.affiliate-shell {
  display: grid;
  gap: 12px;
}

.affiliate-nav {
  min-height: 58px;
  padding: 0 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  overflow: hidden;
}

.affiliate-tabs {
  min-width: 0;
  align-self: stretch;
  display: flex;
  align-items: end;
  gap: 4px;
}

.affiliate-tabs button {
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

.affiliate-tabs button.active {
  border-bottom-color: var(--text);
  color: var(--text);
}

.affiliate-tabs button span {
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

.affiliate-panel {
  min-width: 0;
  overflow: hidden;
}

.affiliate-toolbar {
  min-height: 58px;
  padding: 10px 13px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.affiliate-search {
  width: min(460px, 100%);
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

.affiliate-search input {
  min-width: 0;
  flex: 1;
  border: 0;
  outline: none;
  background: transparent;
  font-size: 9px;
}

.affiliate-search button,
.affiliate-filters button,
.affiliate-filters select {
  height: 28px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}

.affiliate-search button {
  padding: 0 9px;
  border-top: 0;
  border-right: 0;
  border-bottom: 0;
  border-radius: 0;
  display: flex;
  align-items: center;
  gap: 4px;
}

.affiliate-filters {
  display: flex;
  align-items: center;
  gap: 6px;
}

.affiliate-filters select {
  min-width: 120px;
  padding: 0 8px;
}

.affiliate-filters button {
  padding: 0 9px;
  display: flex;
  align-items: center;
  gap: 5px;
}

.affiliate-filters button:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.affiliate-notice,
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

.affiliate-notice button {
  margin-left: auto;
  border: 0;
  background: transparent;
  color: inherit;
  font-size: 8px;
  font-weight: 700;
}

.affiliate-state {
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

.affiliate-state strong {
  color: var(--text);
  font-size: 11px;
}

.affiliate-table-wrap {
  width: 100%;
  min-height: 390px;
  overflow-x: auto;
}

.affiliate-table {
  width: 100%;
  min-width: 1050px;
  border-collapse: collapse;
}

.affiliate-table th,
.affiliate-table td {
  padding: 13px 14px;
  border-bottom: 1px solid var(--line);
  text-align: left;
  vertical-align: middle;
}

.affiliate-table th {
  background: var(--surface-2);
  color: var(--muted);
  font-size: 7px;
  font-weight: 600;
  letter-spacing: 0.04em;
}

.affiliate-table td {
  font-size: 8px;
}

.affiliate-table td > b,
.affiliate-table td > time,
.affiliate-table td > small,
.affiliate-table td > code {
  display: block;
}

.affiliate-table td > b,
.affiliate-table td > time {
  font-size: 8px;
  font-weight: 600;
}

.affiliate-table td > small,
.affiliate-table td > code,
.record-primary code {
  margin-top: 4px;
  color: var(--muted);
  font-size: 7px;
}

.affiliate-table code,
.identity-card code,
.payout-card code {
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
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.record-primary b {
  font-size: 9px;
}

.status-badge {
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
.status-available,
.status-completed {
  background: color-mix(in srgb, var(--success) 10%, transparent);
  color: var(--success);
}

.status-pending,
.status-processing,
.status-partially_reversed {
  background: color-mix(in srgb, var(--warn) 10%, transparent);
  color: var(--warn);
}

.status-rejected,
.status-reversed {
  background: color-mix(in srgb, var(--danger) 9%, transparent);
  color: var(--danger);
}

.status-suspended {
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

.affiliate-pagination {
  min-height: 53px;
  padding: 9px 13px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: var(--muted);
  font-size: 8px;
}

.affiliate-pagination > div {
  display: flex;
  gap: 4px;
}

.affiliate-pagination button,
.affiliate-pagination select {
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

.affiliate-pagination button.active {
  background: var(--dark);
  color: var(--dark-text);
}

.affiliate-pagination button:disabled {
  cursor: not-allowed;
  opacity: 0.4;
}

.affiliate-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 120;
  padding: 24px;
  display: flex;
  justify-content: flex-end;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(2px);
}

.affiliate-modal {
  width: min(680px, 100%);
  height: 100%;
  border: 1px solid var(--line);
  border-radius: 10px;
  overflow-y: auto;
  background: var(--surface);
  color: var(--text);
  box-shadow: var(--shadow);
}

.affiliate-modal > header {
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

.affiliate-modal h2 {
  margin: 5px 0 3px;
  font-size: 17px;
  letter-spacing: -0.03em;
}

.affiliate-modal header p {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
}

.affiliate-modal > header > button {
  width: 31px;
  height: 31px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: grid;
  place-items: center;
  background: var(--surface);
}

.affiliate-form,
.withdrawal-detail {
  padding: 5px 20px 20px;
}

.affiliate-form fieldset,
.withdrawal-form fieldset {
  margin: 0;
  padding: 18px 0;
  border: 0;
  border-bottom: 1px solid var(--line);
}

.affiliate-form legend,
.withdrawal-form legend {
  margin-bottom: 13px;
  padding: 0;
  font-size: 10px;
  font-weight: 700;
}

.affiliate-form label,
.withdrawal-form label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: var(--muted);
  font-size: 8px;
  font-weight: 600;
}

.affiliate-form input,
.affiliate-form select,
.affiliate-form textarea,
.withdrawal-form input,
.withdrawal-form select,
.withdrawal-form textarea {
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

.affiliate-form input:focus,
.affiliate-form select:focus,
.affiliate-form textarea:focus,
.withdrawal-form input:focus,
.withdrawal-form select:focus,
.withdrawal-form textarea:focus {
  border-color: var(--text);
}

.affiliate-form textarea,
.withdrawal-form textarea {
  min-height: 88px;
  resize: vertical;
  line-height: 1.55;
}

.affiliate-form small,
.withdrawal-form small {
  color: var(--muted);
  font-size: 7px;
  font-weight: 400;
}

.identity-card,
.payout-card {
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

.identity-card > span:first-child,
.payout-card > span:first-child {
  width: 34px;
  height: 34px;
  border-radius: 7px;
  display: grid;
  place-items: center;
  background: var(--soft);
}

.identity-card div,
.payout-card div {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.identity-card b,
.payout-card b {
  font-size: 9px;
}

.identity-card code,
.payout-card code {
  overflow: hidden;
  color: var(--muted);
  font-size: 7px;
  text-overflow: ellipsis;
  white-space: nowrap;
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

.balance-card {
  min-height: 64px;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: flex;
  align-items: center;
  gap: 9px;
  background: var(--surface-2);
}

.balance-card > span {
  width: 31px;
  height: 31px;
  border-radius: 6px;
  display: grid;
  place-items: center;
  background: var(--soft);
}

.balance-card div {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.balance-card small {
  color: var(--muted);
  font-size: 7px;
}

.balance-card b {
  font-size: 9px;
}

.affiliate-form > footer,
.withdrawal-form > footer {
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

.affiliate-form > footer > button:first-child,
.withdrawal-form > footer > button:first-child,
.detail-footer button {
  height: 36px;
  padding: 0 14px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  font-size: 8px;
}

.affiliate-form > footer .primary-button,
.withdrawal-form > footer .primary-button {
  height: 36px;
  padding: 0 14px;
  font-size: 8px;
}

.detail-state {
  min-height: 350px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 9px;
  color: var(--muted);
  font-size: 8px;
}

.error-detail {
  color: var(--danger);
}

.detail-state button {
  height: 29px;
  padding: 0 10px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  color: inherit;
  font-size: 8px;
}

.withdrawal-summary {
  margin-top: 15px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr)) auto;
  gap: 10px;
  background: var(--surface-2);
}

.withdrawal-summary div {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.withdrawal-summary small,
.detail-grid small,
.payout-card small {
  color: var(--muted);
  font-size: 7px;
}

.withdrawal-summary b {
  font-size: 11px;
}

.privacy-note {
  padding: 5px 7px;
  border-radius: 10px;
  display: flex !important;
  flex-direction: row !important;
  align-items: center;
  gap: 5px !important;
  background: color-mix(in srgb, var(--success) 9%, transparent);
  color: var(--success);
  font-size: 7px;
}

.detail-grid {
  margin-top: 12px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.detail-grid > div {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.detail-grid b {
  overflow-wrap: anywhere;
  font-size: 8px;
}

.detail-grid span {
  color: var(--muted);
  font-size: 7px;
}

.wide-detail {
  grid-column: 1 / -1;
}

.withdrawal-form {
  margin-top: 4px;
}

.withdrawal-form label + label,
.withdrawal-form .form-grid + label {
  margin-top: 12px;
}

.detail-footer {
  position: sticky;
  bottom: -20px;
  margin: 14px -20px -20px;
  padding: 13px 20px;
  border-top: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  color: var(--muted);
  font-size: 8px;
  backdrop-filter: blur(12px);
}

.detail-footer span {
  display: flex;
  align-items: center;
  gap: 6px;
}

.spinning {
  animation: affiliate-spin 0.8s linear infinite;
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

@keyframes affiliate-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 850px) {
  .affiliate-toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .affiliate-search {
    width: 100%;
  }

  .affiliate-filters {
    justify-content: flex-end;
  }

  .nav-context span {
    display: none;
  }
}

@media (max-width: 680px) {
  .affiliate-nav {
    padding: 0 8px;
  }

  .affiliate-tabs button {
    padding: 0 7px;
  }

  .affiliate-filters select {
    flex: 1;
  }

  .affiliate-table-wrap {
    padding: 9px;
  }

  .affiliate-table {
    min-width: 0;
  }

  .affiliate-table thead {
    display: none;
  }

  .affiliate-table tbody,
  .affiliate-table tr,
  .affiliate-table td {
    display: block;
    width: 100%;
  }

  .affiliate-table tr {
    margin-bottom: 9px;
    padding: 7px 10px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--surface);
  }

  .affiliate-table td {
    min-height: 35px;
    padding: 8px 0 8px 98px;
    border-bottom: 1px solid var(--line);
    position: relative;
  }

  .affiliate-table td::before {
    content: attr(data-label);
    position: absolute;
    left: 0;
    top: 10px;
    color: var(--muted);
    font-size: 7px;
  }

  .affiliate-table td:last-child {
    border-bottom: 0;
  }

  .record-primary {
    min-width: 0;
  }

  .record-actions {
    text-align: left !important;
  }

  .affiliate-pagination {
    align-items: flex-start;
    flex-direction: column;
  }

  .affiliate-pagination > div {
    width: 100%;
    overflow-x: auto;
  }

  .affiliate-modal-backdrop {
    padding: 0;
  }

  .affiliate-modal {
    border-radius: 0;
  }

  .affiliate-modal > header,
  .affiliate-form,
  .withdrawal-detail {
    padding-right: 14px;
    padding-left: 14px;
  }

  .two-columns,
  .detail-grid,
  .withdrawal-summary {
    grid-template-columns: 1fr;
  }

  .withdrawal-summary > .status-badge {
    grid-row: 1;
  }

  .identity-card,
  .payout-card {
    grid-template-columns: auto minmax(0, 1fr);
  }

  .identity-card > .status-badge,
  .payout-card > .privacy-note {
    grid-column: 2;
  }

  .affiliate-form > footer,
  .withdrawal-form > footer,
  .detail-footer {
    margin-right: -14px;
    margin-left: -14px;
    padding-right: 14px;
    padding-left: 14px;
  }

  .detail-footer {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
