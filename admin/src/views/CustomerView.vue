<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  AlertCircle,
  BadgeCheck,
  Ban,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleDollarSign,
  Download,
  Eye,
  KeyRound,
  LoaderCircle,
  RefreshCw,
  Search,
  ShieldCheck,
  ShoppingBag,
  UserRound,
  UsersRound,
  WalletCards,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import {
  formatMoney,
  majorInputStep,
  majorToMinor,
  minorToSafeNumber,
  storeCurrency,
} from "../utils/money";

type ViewMode = "customers" | "wallets";
type DetailTab = "profile" | "orders" | "wallet" | "security";

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}
interface Customer {
  id: string;
  email: string;
  nickname: string;
  avatar_url?: string;
  status: string;
  last_login_at?: string;
  created_at: string;
  balance?: number;
  frozen?: number;
  order_count?: number;
  net_spend?: number;
  currency?: string;
}
interface UserRecord {
  id: string;
  email: string;
  nickname: string;
  avatar_url?: string;
  status: string;
  last_login_at: string;
  created_at: string;
  updated_at: string;
}
interface WalletAccount {
  id?: string;
  owner_id: string;
  currency: string;
  balance: number;
  frozen: number;
  version: number;
}
interface WalletEntry {
  id: string;
  entry_no: string;
  type: string;
  amount: number;
  balance_after: number;
  reference_type: string;
  reference_id?: string | null;
  description: string;
  created_at: string;
}
interface OrderItem {
  id: string;
  product_name: string;
  variant_name?: string;
  quantity: number;
}
interface Order {
  id: string;
  order_no: string;
  status: string;
  payment_status: string;
  total: number;
  currency: string;
  items: OrderItem[];
  created_at: string;
}
interface Session {
  id: string;
  device: string;
  ip: string;
  user_agent: string;
  last_active_at: string;
  expires_at: string;
  revoked_at?: string | null;
}
interface LoginEvent {
  id: string;
  ip: string;
  country: string;
  city: string;
  user_agent: string;
  succeeded: boolean;
  reason: string;
  created_at: string;
}
interface MemberLevel {
  id?: string;
  code?: string;
  name?: string;
  discount_basis_point?: number;
  minimum_spend?: number;
  priority?: number;
  currency?: string;
}
interface Membership {
  member_level_id?: string;
  granted_at?: string;
  expires_at?: string | null;
  source?: "automatic" | "manual" | string;
  granted_by?: string | null;
  evaluated_at?: string;
}
interface Affiliate {
  id?: string;
  referral_code?: string;
  status?: string;
  commission_basis_point?: number;
}
interface Reseller {
  id?: string;
  code?: string;
  name?: string;
  status?: string;
}
interface CustomerDetail {
  user: UserRecord;
  wallet?: WalletAccount;
  wallet_entries?: PagePayload<WalletEntry>;
  recent_orders: Order[];
  sessions: Session[];
  login_events: LoginEvent[];
  membership: Membership;
  member_level: MemberLevel;
  affiliate: Affiliate;
  reseller: Reseller;
  statistics: { order_count: number; ticket_count: number; net_spend: number };
}

const { t, locale } = useI18n();
const route = useRoute();
const auth = useAuthStore();
const mode = ref<ViewMode>("customers");
const customers = ref<Customer[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const searchInput = ref("");
const appliedSearch = ref("");
const statusFilter = ref("");
const loading = ref(false);
const loadError = ref("");
const notice = ref("");
const selectedCustomerIDs = ref<string[]>([]);
const batchSaving = ref(false);
const detailOpen = ref(false);
const detail = ref<CustomerDetail | null>(null);
const detailLoading = ref(false);
const detailError = ref("");
const detailTab = ref<DetailTab>("profile");
const ledgerPage = ref(1);
const actionError = ref("");
const actionSaving = ref(false);
const statusReason = ref("");
const walletAmount = ref("");
const walletDescription = ref("");
const walletReason = ref("");
const adjustmentKey = ref(crypto.randomUUID());
const membershipLevels = ref<MemberLevel[]>([]);
const membershipLevelID = ref("");
const membershipExpiresAt = ref("");
const membershipEvidence = ref("");
const membershipReason = ref("");
const membershipLevelsLoading = ref(false);
const exportOpen = ref(false);
const exportReason = ref("");
const exportError = ref("");
const exportSaving = ref(false);
let requestSequence = 0;

const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const pageBalance = computed(() =>
  customers.value.reduce((sum, customer) => sum + (customer.balance || 0), 0),
);
const pageSpend = computed(() =>
  customers.value.reduce((sum, customer) => sum + (customer.net_spend || 0), 0),
);
const disabledCount = computed(
  () =>
    customers.value.filter((customer) => customer.status === "disabled").length,
);
const canManageCustomers = computed(() =>
  auth.hasPermission("customer.manage"),
);
const canManageWallet = computed(() => auth.hasPermission("wallet.manage"));
const allPageCustomersSelected = computed(
  () =>
    customers.value.length > 0 &&
    customers.value.every((item) =>
      selectedCustomerIDs.value.includes(item.id),
    ),
);
const activeSessions = computed(
  () =>
    (detail.value?.sessions || []).filter((session) => !session.revoked_at)
      .length || 0,
);
const activeWallet = computed<WalletAccount>(() =>
  detail.value?.wallet
    ? detail.value.wallet
    : {
        owner_id: detail.value?.user.id || "",
        currency: storeCurrency.value,
        balance: 0,
        frozen: 0,
        version: 0,
      },
);
const activeWalletEntries = computed<PagePayload<WalletEntry>>(
  () =>
    detail.value?.wallet_entries || {
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
    },
);
const ledgerPages = computed(() =>
  Math.max(
    1,
    Math.ceil(
      (detail.value?.wallet_entries?.total || 0) /
        (detail.value?.wallet_entries?.page_size || 20),
    ),
  ),
);

function responseMessage(error: unknown, fallback: string) {
  const candidate = error as {
    response?: { data?: { message?: string } };
    message?: string;
  };
  return candidate.response?.data?.message || candidate.message || fallback;
}
function money(value: number, currency?: string) {
  return formatMoney(value, currency || storeCurrency.value, locale.value);
}
function dateTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "—"
    : date.toLocaleString("zh-CN", { hour12: false });
}
function shortID(value?: string | null) {
  if (!value) return "—";
  return value.length > 20 ? `${value.slice(0, 9)}…${value.slice(-6)}` : value;
}
function statusLabel(value?: string) {
  const key = (
    {
      active: "customer.statusActive",
      disabled: "customer.statusDisabled",
      paid: "customer.statusPaid",
      pending: "customer.statusPending",
      delivered: "customer.statusDelivered",
      completed: "customer.statusCompleted",
      failed: "customer.statusFailed",
      partially_refunded: "customer.statusPartiallyRefunded",
      refunded: "customer.statusRefunded",
      succeeded: "customer.statusSucceeded",
      rejected: "customer.statusRejected",
      suspended: "customer.statusSuspended",
    } as Record<string, string>
  )[value || ""];
  if (!key) return value || "—";
  const label = t(key);
  return label === key ? value || "—" : label;
}
function statusTone(value?: string) {
  if (
    ["active", "paid", "delivered", "completed", "succeeded"].includes(
      value || "",
    )
  )
    return "success";
  if (["disabled", "failed", "rejected"].includes(value || "")) return "danger";
  if (["partially_refunded", "suspended"].includes(value || ""))
    return "warning";
  return "neutral";
}
function membershipSourceLabel(value?: string) {
  if (value === "automatic") return t("customer.membershipAutomatic");
  if (value === "manual") return t("customer.membershipManual");
  return t("customer.membershipNone");
}

async function loadCustomers() {
  const sequence = ++requestSequence;
  loading.value = true;
  loadError.value = "";
  try {
    const endpoint = mode.value === "wallets" ? "/wallets/users" : "/users";
    const { data } = await adminApi.get(endpoint, {
      params: {
        page: page.value,
        page_size: pageSize.value,
        q: appliedSearch.value || undefined,
        status: statusFilter.value || undefined,
      },
    });
    if (sequence !== requestSequence) return;
    const payload = data.data as PagePayload<Customer>;
    customers.value = payload?.items || [];
    selectedCustomerIDs.value = [];
    total.value = Number(payload?.total || 0);
  } catch (error) {
    loadError.value = responseMessage(error, t("customer.errLoadList"));
  } finally {
    if (sequence === requestSequence) loading.value = false;
  }
}
function toggleCustomerSelection(id: string) {
  selectedCustomerIDs.value = selectedCustomerIDs.value.includes(id)
    ? selectedCustomerIDs.value.filter((value) => value !== id)
    : [...selectedCustomerIDs.value, id];
}
function toggleAllPageCustomers() {
  selectedCustomerIDs.value = allPageCustomersSelected.value
    ? []
    : customers.value.map((item) => item.id);
}
async function batchCustomerStatus(status: "active" | "disabled") {
  if (
    !canManageCustomers.value ||
    !selectedCustomerIDs.value.length ||
    batchSaving.value
  )
    return;
  const reason = window.prompt(t("customer.batchReasonPrompt"), "")?.trim();
  if (!reason) return;
  if ([...reason].length < 4 || [...reason].length > 500) {
    loadError.value = t("customer.batchReasonInvalid");
    return;
  }
  batchSaving.value = true;
  loadError.value = "";
  try {
    const { data } = await adminApi.patch(
      "/users/batch-status",
      { ids: selectedCustomerIDs.value, status },
      { headers: { "X-Change-Reason": reason } },
    );
    notice.value = t("customer.batchUpdated", {
      count: Number(data.data?.changed || 0),
    });
    selectedCustomerIDs.value = [];
    await loadCustomers();
  } catch (error) {
    loadError.value = responseMessage(error, t("customer.batchFailed"));
  } finally {
    batchSaving.value = false;
  }
}
function search() {
  appliedSearch.value = searchInput.value.trim();
  page.value = 1;
  loadCustomers();
}
function reset() {
  searchInput.value = "";
  appliedSearch.value = "";
  statusFilter.value = "";
  page.value = 1;
  loadCustomers();
}
function changePage(next: number) {
  if (next < 1 || next > totalPages.value || next === page.value) return;
  page.value = next;
  loadCustomers();
}

async function openDetail(customer: Customer, tab: DetailTab = "profile") {
  detailOpen.value = true;
  detail.value = null;
  detailTab.value = tab;
  detailError.value = "";
  actionError.value = "";
  statusReason.value = "";
  walletAmount.value = "";
  walletDescription.value = "";
  walletReason.value = "";
  ledgerPage.value = 1;
  detailLoading.value = true;
  try {
    const tasks: Promise<void>[] = [loadDetail(customer.id)];
    if (mode.value === "customers") tasks.push(loadMembershipLevels());
    await Promise.all(tasks);
    if (mode.value === "customers") resetMembershipForm();
  } catch (error) {
    detailError.value = responseMessage(error, t("customer.errLoadDetail"));
  } finally {
    detailLoading.value = false;
  }
}
async function loadMembershipLevels() {
  membershipLevelsLoading.value = true;
  try {
    const { data } = await adminApi.get("/users/member-levels");
    membershipLevels.value = Array.isArray(data.data) ? data.data : [];
  } catch (error) {
    actionError.value = responseMessage(
      error,
      t("customer.errMembershipLevels"),
    );
  } finally {
    membershipLevelsLoading.value = false;
  }
}
function resetMembershipForm() {
  membershipLevelID.value = detail.value?.membership?.member_level_id || "";
  membershipExpiresAt.value = "";
  membershipEvidence.value = "";
  membershipReason.value = "";
}
function validMembershipAction() {
  const evidenceLength = Array.from(membershipEvidence.value.trim()).length;
  const reasonLength = Array.from(membershipReason.value.trim()).length;
  if (evidenceLength < 4 || evidenceLength > 1000) {
    actionError.value = t("customer.errMembershipEvidence");
    return false;
  }
  if (reasonLength < 4 || reasonLength > 500) {
    actionError.value = t("customer.errMembershipReason");
    return false;
  }
  return true;
}
async function grantMembership() {
  if (
    !canManageCustomers.value ||
    !detail.value ||
    !membershipLevelID.value ||
    !validMembershipAction()
  ) {
    if (!membershipLevelID.value)
      actionError.value = t("customer.errMembershipLevel");
    return;
  }
  let expiresAt: string | null = null;
  if (membershipExpiresAt.value) {
    const parsed = new Date(membershipExpiresAt.value);
    if (
      Number.isNaN(parsed.getTime()) ||
      parsed.getTime() <= Date.now() + 60_000
    ) {
      actionError.value = t("customer.errMembershipExpiry");
      return;
    }
    expiresAt = parsed.toISOString();
  }
  await performMembershipAction(async (userID, headers) => {
    await adminApi.put(
      `/users/${encodeURIComponent(userID)}/membership`,
      {
        member_level_id: membershipLevelID.value,
        expires_at: expiresAt,
        evidence: membershipEvidence.value.trim(),
      },
      { headers },
    );
    return t("customer.noticeMembershipGranted");
  });
}
async function recalculateMembership() {
  if (!canManageCustomers.value || !detail.value || !validMembershipAction())
    return;
  await performMembershipAction(async (userID, headers) => {
    await adminApi.post(
      `/users/${encodeURIComponent(userID)}/membership/recalculate`,
      { evidence: membershipEvidence.value.trim() },
      { headers },
    );
    return t("customer.noticeMembershipRecalculated");
  });
}
async function revokeMembership() {
  if (
    !canManageCustomers.value ||
    !detail.value?.membership.member_level_id ||
    !validMembershipAction()
  )
    return;
  await performMembershipAction(async (userID, headers) => {
    await adminApi.post(
      `/users/${encodeURIComponent(userID)}/membership/revoke`,
      { evidence: membershipEvidence.value.trim() },
      { headers },
    );
    return t("customer.noticeMembershipRevoked");
  });
}
async function performMembershipAction(
  action: (userID: string, headers: Record<string, string>) => Promise<string>,
) {
  if (!detail.value || actionSaving.value) return;
  const userID = detail.value.user.id;
  actionSaving.value = true;
  actionError.value = "";
  try {
    notice.value = await action(userID, {
      "X-Change-Reason": membershipReason.value.trim(),
    });
    await Promise.all([loadDetail(userID), loadCustomers()]);
    resetMembershipForm();
  } catch (error) {
    actionError.value = responseMessage(
      error,
      t("customer.errMembershipAction"),
    );
  } finally {
    actionSaving.value = false;
  }
}
async function loadDetail(id: string) {
  const endpoint =
    mode.value === "wallets"
      ? `/wallets/users/${encodeURIComponent(id)}`
      : `/users/${encodeURIComponent(id)}`;
  const { data } = await adminApi.get(endpoint, {
    params: { page: ledgerPage.value, page_size: 20 },
  });
  detail.value = data.data as CustomerDetail;
}
async function changeLedgerPage(next: number) {
  if (
    !detail.value ||
    next < 1 ||
    next > ledgerPages.value ||
    next === ledgerPage.value
  )
    return;
  ledgerPage.value = next;
  detailLoading.value = true;
  try {
    await loadDetail(detail.value.user.id);
  } catch (error) {
    detailError.value = responseMessage(error, t("customer.errLoadLedger"));
  } finally {
    detailLoading.value = false;
  }
}
function closeDetail() {
  if (!actionSaving.value) detailOpen.value = false;
}

async function changeStatus() {
  if (!canManageCustomers.value || !detail.value) return;
  const reason = statusReason.value.trim();
  if (reason.length < 4 || reason.length > 500) {
    actionError.value = t("customer.errStatusReasonLength");
    return;
  }
  const target = detail.value.user.status === "active" ? "disabled" : "active";
  actionSaving.value = true;
  actionError.value = "";
  try {
    await adminApi.patch(
      `/users/${detail.value.user.id}/status`,
      { status: target },
      { headers: { "X-Change-Reason": reason } },
    );
    notice.value =
      target === "disabled"
        ? t("customer.noticeDisabled")
        : t("customer.noticeEnabled");
    statusReason.value = "";
    await Promise.all([loadDetail(detail.value.user.id), loadCustomers()]);
  } catch (error) {
    actionError.value = responseMessage(error, t("customer.errChangeStatus"));
  } finally {
    actionSaving.value = false;
  }
}

async function adjustWallet() {
  if (!canManageWallet.value || !detail.value?.wallet) return;
  const currency = detail.value.wallet.currency || storeCurrency.value;
  let amount = 0;
  try {
    const exactAmount = majorToMinor(walletAmount.value, currency);
    const maximum = BigInt(majorToMinor("1000000", currency));
    const candidate = BigInt(exactAmount);
    if (candidate === 0n || candidate > maximum || candidate < -maximum)
      throw new Error("amount out of range");
    amount = minorToSafeNumber(exactAmount);
  } catch {
    actionError.value = t("customer.errAdjustAmount");
    return;
  }
  if (
    walletDescription.value.trim().length < 4 ||
    walletDescription.value.trim().length > 500
  ) {
    actionError.value = t("customer.errDescriptionLength");
    return;
  }
  if (
    walletReason.value.trim().length < 4 ||
    walletReason.value.trim().length > 500
  ) {
    actionError.value = t("customer.errAdjustReasonLength");
    return;
  }
  actionSaving.value = true;
  actionError.value = "";
  try {
    await adminApi.post(
      `/users/${detail.value.user.id}/wallet-adjustments`,
      {
        amount,
        description: walletDescription.value.trim(),
        idempotency_key: adjustmentKey.value,
      },
      { headers: { "X-Change-Reason": walletReason.value.trim() } },
    );
    notice.value = t("customer.noticeLedgerWritten", {
      action: t(amount > 0 ? "customer.credit" : "customer.debit"),
      amount: money(Math.abs(amount), currency),
    });
    walletAmount.value = "";
    walletDescription.value = "";
    walletReason.value = "";
    adjustmentKey.value = crypto.randomUUID();
    ledgerPage.value = 1;
    await Promise.all([loadDetail(detail.value.user.id), loadCustomers()]);
  } catch (error) {
    actionError.value = responseMessage(error, t("customer.errAdjustWallet"));
  } finally {
    actionSaving.value = false;
  }
}

function openExport() {
  exportReason.value = "";
  exportError.value = "";
  exportOpen.value = true;
}
async function exportCustomers() {
  const reason = exportReason.value.trim();
  if (reason.length < 4 || reason.length > 500) {
    exportError.value = t("customer.errExportReasonLength");
    return;
  }
  exportSaving.value = true;
  exportError.value = "";
  try {
    const endpoint =
      mode.value === "wallets" ? "/wallets/users/export" : "/users/export";
    const response = await adminApi.get(endpoint, {
      params: {
        q: appliedSearch.value || undefined,
        status: statusFilter.value || undefined,
      },
      headers: { "X-Change-Reason": reason },
      responseType: "blob",
      timeout: 30000,
    });
    const blob = response.data as Blob;
    const disposition = String(response.headers["content-disposition"] || "");
    const match = disposition.match(/filename="?([^";]+)"?/i);
    const filename =
      match?.[1] ||
      `linlinqi-${mode.value}-${new Date().toISOString().slice(0, 10)}.csv`;
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = filename;
    anchor.click();
    URL.revokeObjectURL(url);
    exportOpen.value = false;
    notice.value =
      response.headers["x-export-truncated"] === "true"
        ? t("customer.noticeExportTruncated")
        : t("customer.noticeExportDone");
  } catch (error) {
    const candidate = error as { response?: { data?: Blob } };
    if (candidate.response?.data instanceof Blob) {
      try {
        const payload = JSON.parse(await candidate.response.data.text());
        exportError.value = payload.message || t("customer.errExport");
      } catch {
        exportError.value = t("customer.errExport");
      }
    } else exportError.value = responseMessage(error, t("customer.errExport"));
  } finally {
    exportSaving.value = false;
  }
}

watch(
  () => route.meta.defaultTab,
  (value) => {
    const nextMode: ViewMode =
      String(value || "customers") === "wallets" ? "wallets" : "customers";
    if (nextMode !== mode.value) {
      requestSequence += 1;
      mode.value = nextMode;
      page.value = 1;
      searchInput.value = "";
      appliedSearch.value = "";
      statusFilter.value = "";
      selectedCustomerIDs.value = [];
      exportOpen.value = false;
      closeDetail();
    }
    void loadCustomers();
  },
  { immediate: true },
);
</script>

<template>
  <section class="customer-view">
    <div class="customer-toolbar">
      <div>
        <button
          type="button"
          class="secondary-button"
          :disabled="loading"
          @click="loadCustomers"
        >
          <RefreshCw :size="14" :class="{ spinning: loading }" />{{
            t("customer.refresh")
          }}</button
        ><button type="button" class="primary-button" @click="openExport">
          <Download :size="14" />{{ t("customer.controlledExport") }}
        </button>
      </div>
    </div>
    <div class="customer-search">
      <div>
        <Search :size="14" /><input
          v-model="searchInput"
          :placeholder="t('customer.searchPlaceholder')"
          @keydown.enter="search"
        /><button type="button" @click="search">
          {{ t("customer.search") }}
        </button>
      </div>
      <select
        v-model="statusFilter"
        @change="
          page = 1;
          loadCustomers();
        "
      >
        <option value="">{{ t("customer.allStatuses") }}</option>
        <option value="active">{{ t("customer.statusActive") }}</option>
        <option value="disabled">
          {{ t("customer.statusDisabled") }}
        </option></select
      ><button type="button" class="text-button" @click="reset">
        {{ t("customer.reset") }}</button
      ><span>{{ t("customer.totalCount", { count: total }) }}</span>
    </div>
    <div class="customer-metrics">
      <article>
        <span><UsersRound :size="14" />{{ t("customer.metricMatched") }}</span
        ><strong>{{ total }}</strong
        ><small>{{ t("customer.metricMatchedSub") }}</small>
      </article>
      <article v-if="mode === 'wallets'">
        <span
          ><WalletCards :size="14" />{{ t("customer.metricPageBalance") }}</span
        ><strong>{{ money(pageBalance, customers[0]?.currency) }}</strong
        ><small>{{ t("customer.metricPageBalanceSub") }}</small>
      </article>
      <article v-if="mode === 'customers'">
        <span
          ><ShoppingBag :size="14" />{{ t("customer.metricPageSpend") }}</span
        ><strong>{{ money(pageSpend, customers[0]?.currency) }}</strong
        ><small>{{ t("customer.metricPageSpendSub") }}</small>
      </article>
      <article v-if="mode === 'customers'">
        <span><Ban :size="14" />{{ t("customer.metricPageDisabled") }}</span
        ><strong>{{ disabledCount }}</strong
        ><small>{{ t("customer.metricPageDisabledSub") }}</small>
      </article>
    </div>
    <div v-if="notice" class="customer-alert success">
      <Check :size="14" /><span>{{ notice }}</span
      ><button type="button" @click="notice = ''"><X :size="13" /></button>
    </div>
    <div v-if="loadError" class="customer-alert danger">
      <AlertCircle :size="14" /><span>{{ loadError }}</span
      ><button type="button" @click="loadCustomers">
        {{ t("customer.retry") }}
      </button>
    </div>
    <div class="customer-table-shell" :aria-busy="loading">
      <div
        v-if="
          mode === 'customers' &&
          canManageCustomers &&
          selectedCustomerIDs.length
        "
        class="customer-batch-toolbar"
      >
        <strong>{{
          t("customer.batchSelected", { count: selectedCustomerIDs.length })
        }}</strong>
        <span>{{ t("customer.batchScopeHint") }}</span>
        <div>
          <button
            type="button"
            :disabled="batchSaving"
            @click="batchCustomerStatus('active')"
          >
            <Check :size="13" />{{ t("customer.batchEnable") }}
          </button>
          <button
            type="button"
            class="danger"
            :disabled="batchSaving"
            @click="batchCustomerStatus('disabled')"
          >
            <Ban :size="13" />{{ t("customer.batchDisable") }}
          </button>
          <button
            type="button"
            :disabled="batchSaving"
            @click="selectedCustomerIDs = []"
          >
            {{ t("customer.batchClear") }}
          </button>
        </div>
      </div>
      <table v-if="customers.length" class="customer-table">
        <thead>
          <tr>
            <th
              v-if="mode === 'customers' && canManageCustomers"
              class="selection-cell"
            >
              <input
                type="checkbox"
                :checked="allPageCustomersSelected"
                :aria-label="t('customer.batchSelectPage')"
                @change="toggleAllPageCustomers"
              />
            </th>
            <th>{{ t("customer.colCustomer") }}</th>
            <th v-if="mode === 'customers'">
              {{ t("customer.colOrdersSpend") }}
            </th>
            <th v-if="mode === 'wallets'">{{ t("customer.colBalance") }}</th>
            <th v-if="mode === 'wallets'">{{ t("customer.colFrozen") }}</th>
            <th v-if="mode === 'customers'">
              {{ t("customer.colLastLogin") }}
            </th>
            <th>{{ t("customer.colStatus") }}</th>
            <th>{{ t("customer.colActions") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="customer in customers" :key="customer.id">
            <td
              v-if="mode === 'customers' && canManageCustomers"
              class="selection-cell"
              data-label=""
            >
              <input
                type="checkbox"
                :checked="selectedCustomerIDs.includes(customer.id)"
                :aria-label="
                  t('customer.batchSelectCustomer', { email: customer.email })
                "
                @change="toggleCustomerSelection(customer.id)"
              />
            </td>
            <td :data-label="t('customer.colCustomer')">
              <div class="customer-identity">
                <img
                  v-if="customer.avatar_url"
                  :src="customer.avatar_url"
                  alt=""
                /><strong>{{
                  customer.nickname || t("customer.noNickname")
                }}</strong>
              </div>
              <span>{{ customer.email }}</span
              ><small>{{
                t("customer.registeredAt", {
                  time: dateTime(customer.created_at),
                })
              }}</small>
            </td>
            <td
              v-if="mode === 'customers'"
              :data-label="t('customer.colOrdersSpend')"
            >
              <strong>{{
                t("customer.orderCount", { count: customer.order_count })
              }}</strong
              ><small>{{
                money(customer.net_spend || 0, customer.currency)
              }}</small>
            </td>
            <td
              v-if="mode === 'wallets'"
              :data-label="t('customer.colBalance')"
            >
              <strong>{{
                money(customer.balance || 0, customer.currency)
              }}</strong>
            </td>
            <td v-if="mode === 'wallets'" :data-label="t('customer.colFrozen')">
              {{ money(customer.frozen || 0, customer.currency) }}
            </td>
            <td
              v-if="mode === 'customers'"
              :data-label="t('customer.colLastLogin')"
            >
              {{ dateTime(customer.last_login_at) }}
            </td>
            <td :data-label="t('customer.colStatus')">
              <span class="status-chip" :class="statusTone(customer.status)">{{
                statusLabel(customer.status)
              }}</span>
            </td>
            <td :data-label="t('customer.colActions')">
              <button
                type="button"
                class="row-action"
                @click="
                  openDetail(
                    customer,
                    mode === 'wallets' ? 'wallet' : 'profile',
                  )
                "
              >
                <Eye :size="13" />{{
                  mode === "wallets"
                    ? t("customer.ledger")
                    : t("customer.detail")
                }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="customer-empty">
        <LoaderCircle v-if="loading" :size="25" class="spinning" /><UsersRound
          v-else
          :size="28"
        /><strong>{{
          loading ? t("customer.loadingCustomers") : t("customer.noCustomers")
        }}</strong>
      </div>
    </div>
    <div class="customer-pagination">
      <span>{{
        t("customer.pageInfo", { current: page, total: totalPages })
      }}</span>
      <div>
        <button
          type="button"
          :disabled="page <= 1 || loading"
          @click="changePage(page - 1)"
        >
          <ChevronLeft :size="14" />{{ t("customer.prevPage") }}</button
        ><button
          type="button"
          :disabled="page >= totalPages || loading"
          @click="changePage(page + 1)"
        >
          {{ t("customer.nextPage") }}<ChevronRight :size="14" />
        </button>
      </div>
    </div>

    <div
      v-if="detailOpen"
      class="drawer-backdrop"
      @mousedown.self="closeDetail"
    >
      <aside class="customer-drawer" role="dialog" aria-modal="true">
        <header>
          <div>
            <span><UserRound :size="18" /></span>
            <div>
              <h2>
                {{
                  detail?.user.nickname ||
                  detail?.user.email ||
                  t("customer.drawerTitle")
                }}
              </h2>
              <p>{{ detail?.user.email || t("customer.drawerSubtitle") }}</p>
            </div>
          </div>
          <button type="button" :disabled="actionSaving" @click="closeDetail">
            <X :size="17" />
          </button>
        </header>
        <div v-if="detailLoading && !detail" class="drawer-empty">
          <LoaderCircle :size="25" class="spinning" />{{
            t("customer.loadingDetail")
          }}
        </div>
        <div v-else-if="detailError" class="drawer-empty danger-text">
          <AlertCircle :size="25" />{{ detailError }}
        </div>
        <template v-else-if="detail"
          ><nav class="detail-tabs">
            <button
              v-if="mode === 'customers'"
              type="button"
              :class="{ active: detailTab === 'profile' }"
              @click="detailTab = 'profile'"
            >
              {{ t("customer.tabProfile") }}</button
            ><button
              v-if="mode === 'customers'"
              type="button"
              :class="{ active: detailTab === 'orders' }"
              @click="detailTab = 'orders'"
            >
              {{ t("customer.tabOrders") }}</button
            ><button
              v-if="mode === 'wallets'"
              type="button"
              :class="{ active: detailTab === 'wallet' }"
              @click="detailTab = 'wallet'"
            >
              {{ t("customer.tabWallets") }}</button
            ><button
              v-if="mode === 'customers'"
              type="button"
              :class="{ active: detailTab === 'security' }"
              @click="detailTab = 'security'"
            >
              {{ t("customer.tabSecurity") }}
            </button>
          </nav>
          <div class="drawer-content">
            <template v-if="detailTab === 'profile'"
              ><section class="identity-card">
                <div>
                  <img
                    v-if="detail.user.avatar_url"
                    class="avatar avatar-image"
                    :src="detail.user.avatar_url"
                    alt=""
                  /><span v-else class="avatar">{{
                    (detail.user.nickname || detail.user.email)
                      .slice(0, 1)
                      .toUpperCase()
                  }}</span>
                  <div>
                    <strong>{{
                      detail.user.nickname || t("customer.noNickname")
                    }}</strong>
                    <p>{{ detail.user.email }}</p>
                    <small>ID {{ shortID(detail.user.id) }}</small>
                  </div>
                </div>
                <span
                  class="status-chip"
                  :class="statusTone(detail.user.status)"
                  >{{ statusLabel(detail.user.status) }}</span
                >
              </section>
              <div class="detail-metrics">
                <article>
                  <span>{{ t("customer.netSpend") }}</span
                  ><strong>{{
                    money(detail.statistics.net_spend, storeCurrency)
                  }}</strong>
                </article>
                <article>
                  <span>{{ t("customer.orders") }}</span
                  ><strong>{{ detail.statistics.order_count }}</strong>
                </article>
                <article>
                  <span>{{ t("customer.tickets") }}</span
                  ><strong>{{ detail.statistics.ticket_count }}</strong>
                </article>
              </div>
              <section class="detail-section">
                <h3>{{ t("customer.levelIdentity") }}</h3>
                <div class="identity-lines">
                  <p>
                    <BadgeCheck :size="14" /><span>{{
                      t("customer.memberLevel")
                    }}</span
                    ><strong
                      >{{
                        detail.member_level.name ||
                        t("customer.memberLevelDefault")
                      }}<small v-if="detail.member_level.discount_basis_point">
                        ·
                        {{
                          t("customer.discountPercent", {
                            percent: (
                              detail.member_level.discount_basis_point / 100
                            ).toFixed(2),
                          })
                        }}</small
                      ></strong
                    >
                  </p>
                  <p>
                    <CircleDollarSign :size="14" /><span>{{
                      t("customer.affiliateAccount")
                    }}</span
                    ><strong>{{
                      detail.affiliate.id
                        ? `${detail.affiliate.referral_code} · ${statusLabel(detail.affiliate.status)}`
                        : t("customer.notApplied")
                    }}</strong>
                  </p>
                  <p>
                    <ShoppingBag :size="14" /><span>{{
                      t("customer.resellerAccount")
                    }}</span
                    ><strong>{{
                      detail.reseller.id
                        ? `${detail.reseller.code} · ${statusLabel(detail.reseller.status)}`
                        : t("customer.notApplied")
                    }}</strong>
                  </p>
                </div>
                <div class="membership-meta">
                  <span>{{ t("customer.membershipSource") }}</span>
                  <b>{{ membershipSourceLabel(detail.membership.source) }}</b>
                  <span>{{ t("customer.membershipGrantedAt") }}</span>
                  <b>{{ dateTime(detail.membership.granted_at) }}</b>
                  <span>{{ t("customer.membershipExpiresAt") }}</span>
                  <b>{{
                    detail.membership.expires_at
                      ? dateTime(detail.membership.expires_at)
                      : t("customer.membershipNeverExpires")
                  }}</b>
                  <span>{{ t("customer.membershipEvaluatedAt") }}</span>
                  <b>{{ dateTime(detail.membership.evaluated_at) }}</b>
                </div>
                <form
                  v-if="canManageCustomers"
                  class="membership-form"
                  @submit.prevent="grantMembership"
                >
                  <h4>{{ t("customer.membershipOperations") }}</h4>
                  <p>{{ t("customer.membershipOperationsHint") }}</p>
                  <div>
                    <label
                      ><span>{{ t("customer.membershipLevel") }}</span
                      ><select
                        v-model="membershipLevelID"
                        :disabled="membershipLevelsLoading || actionSaving"
                      >
                        <option value="" disabled>
                          {{ t("customer.membershipSelectLevel") }}
                        </option>
                        <option
                          v-for="level in membershipLevels"
                          :key="level.id"
                          :value="level.id"
                        >
                          {{ level.name }} ·
                          {{
                            t("customer.membershipThreshold", {
                              amount: money(
                                level.minimum_spend || 0,
                                level.currency || storeCurrency,
                              ),
                            })
                          }}
                        </option>
                      </select></label
                    ><label
                      ><span>{{ t("customer.membershipExpiryOptional") }}</span
                      ><input
                        v-model="membershipExpiresAt"
                        type="datetime-local"
                        :disabled="actionSaving"
                    /></label>
                  </div>
                  <label
                    ><span>{{ t("customer.membershipEvidence") }}</span
                    ><textarea
                      v-model="membershipEvidence"
                      rows="3"
                      maxlength="1000"
                      :placeholder="t('customer.membershipEvidencePlaceholder')"
                    ></textarea>
                    <small>{{ t("customer.membershipEvidenceHint") }}</small>
                  </label>
                  <label
                    ><span>{{ t("customer.membershipAuditReason") }}</span
                    ><input
                      v-model="membershipReason"
                      maxlength="500"
                      :placeholder="t('customer.membershipReasonPlaceholder')"
                  /></label>
                  <div class="membership-actions">
                    <button
                      type="button"
                      class="secondary-button"
                      :disabled="actionSaving"
                      @click="recalculateMembership"
                    >
                      <RefreshCw :size="14" />{{
                        t("customer.membershipRecalculate")
                      }}
                    </button>
                    <button
                      v-if="detail.membership.source === 'manual'"
                      type="button"
                      class="danger-button"
                      :disabled="actionSaving"
                      @click="revokeMembership"
                    >
                      <Ban :size="14" />{{ t("customer.membershipRevoke") }}
                    </button>
                    <button
                      type="submit"
                      class="primary-button"
                      :disabled="actionSaving || membershipLevelsLoading"
                    >
                      <BadgeCheck :size="14" />{{
                        t("customer.membershipGrant")
                      }}
                    </button>
                  </div>
                </form>
              </section>
              <section
                v-if="canManageCustomers"
                class="detail-section danger-zone"
              >
                <h3>
                  {{
                    detail.user.status === "active"
                      ? t("customer.disableAccount")
                      : t("customer.enableAccount")
                  }}
                </h3>
                <p>
                  {{
                    detail.user.status === "active"
                      ? t("customer.disableHint")
                      : t("customer.enableHint")
                  }}
                </p>
                <textarea
                  v-model="statusReason"
                  maxlength="500"
                  rows="3"
                  :placeholder="t('customer.reasonPlaceholder')"
                ></textarea
                ><button
                  type="button"
                  :class="
                    detail.user.status === 'active'
                      ? 'danger-button'
                      : 'primary-button'
                  "
                  :disabled="actionSaving"
                  @click="changeStatus"
                >
                  <Ban
                    v-if="detail.user.status === 'active'"
                    :size="14"
                  /><ShieldCheck v-else :size="14" />{{
                    detail.user.status === "active"
                      ? t("customer.confirmDisable")
                      : t("customer.confirmEnable")
                  }}
                </button>
              </section></template
            >
            <template v-else-if="detailTab === 'orders'"
              ><section v-if="detail.recent_orders?.length" class="order-list">
                <article v-for="order in detail.recent_orders" :key="order.id">
                  <div>
                    <strong>{{ order.order_no }}</strong>
                    <p>
                      {{
                        order.items
                          .map(
                            (item) => `${item.product_name} ×${item.quantity}`,
                          )
                          .join(t("customer.listSeparator"))
                      }}
                    </p>
                    <small>{{ dateTime(order.created_at) }}</small>
                  </div>
                  <div>
                    <strong>{{ money(order.total, order.currency) }}</strong
                    ><span
                      class="status-chip"
                      :class="statusTone(order.status)"
                      >{{ statusLabel(order.status) }}</span
                    ><small>{{ statusLabel(order.payment_status) }}</small>
                  </div>
                </article>
              </section>
              <div v-else class="drawer-empty">
                <ShoppingBag :size="26" />{{ t("customer.noOrders") }}
              </div></template
            >
            <template v-else-if="detailTab === 'wallet'"
              ><section class="wallet-summary">
                <article>
                  <span>{{ t("customer.availableBalance") }}</span
                  ><strong>{{
                    money(activeWallet.balance, activeWallet.currency)
                  }}</strong>
                </article>
                <article>
                  <span>{{ t("customer.frozenAmount") }}</span
                  ><strong>{{
                    money(activeWallet.frozen, activeWallet.currency)
                  }}</strong>
                </article>
                <article>
                  <span>{{ t("customer.ledgerVersion") }}</span
                  ><strong>{{ activeWallet.version }}</strong>
                </article>
              </section>
              <section v-if="canManageWallet" class="detail-section adjustment">
                <h3>{{ t("customer.manualAdjustment") }}</h3>
                <p>{{ t("customer.adjustmentHint") }}</p>
                <div>
                  <label
                    ><span
                      >{{ t("customer.amountYuan") }} ({{
                        activeWallet.currency
                      }})</span
                    ><input
                      v-model="walletAmount"
                      inputmode="decimal"
                      :step="majorInputStep(activeWallet.currency)"
                      :placeholder="t('customer.amountPlaceholder')" /></label
                  ><label
                    ><span>{{ t("customer.ledgerSummary") }}</span
                    ><input
                      v-model="walletDescription"
                      maxlength="500"
                      :placeholder="t('customer.summaryPlaceholder')"
                  /></label>
                </div>
                <label
                  ><span>{{ t("customer.reasonLabel") }}</span
                  ><textarea
                    v-model="walletReason"
                    maxlength="500"
                    rows="3"
                    :placeholder="t('customer.walletReasonPlaceholder')"
                  ></textarea></label
                ><code>{{
                  t("customer.idempotencyKey", { key: adjustmentKey })
                }}</code
                ><button
                  type="button"
                  class="primary-button"
                  :disabled="actionSaving"
                  @click="adjustWallet"
                >
                  <WalletCards :size="14" />{{ t("customer.writeLedger") }}
                </button>
              </section>
              <section class="detail-section">
                <h3>
                  {{
                    t("customer.ledgerEntries", {
                      count: activeWalletEntries.total,
                    })
                  }}
                </h3>
                <article
                  v-for="entry in activeWalletEntries.items"
                  :key="entry.id"
                  class="ledger-entry"
                >
                  <div>
                    <strong>{{ entry.description }}</strong
                    ><code>{{ entry.entry_no }}</code
                    ><small
                      >{{ entry.type }} ·
                      {{ dateTime(entry.created_at) }}</small
                    >
                  </div>
                  <div>
                    <strong :class="entry.amount >= 0 ? 'positive' : 'negative'"
                      >{{ entry.amount >= 0 ? "+" : ""
                      }}{{ money(entry.amount, activeWallet.currency) }}</strong
                    ><small>{{
                      t("customer.balanceAfter", {
                        amount: money(
                          entry.balance_after,
                          activeWallet.currency,
                        ),
                      })
                    }}</small>
                  </div>
                </article>
                <p v-if="!activeWalletEntries.items.length" class="muted">
                  {{ t("customer.noLedgerEntries") }}
                </p>
                <div v-if="ledgerPages > 1" class="ledger-pagination">
                  <button
                    type="button"
                    :disabled="ledgerPage <= 1 || detailLoading"
                    @click="changeLedgerPage(ledgerPage - 1)"
                  >
                    <ChevronLeft :size="13" /></button
                  ><span>{{ ledgerPage }} / {{ ledgerPages }}</span
                  ><button
                    type="button"
                    :disabled="ledgerPage >= ledgerPages || detailLoading"
                    @click="changeLedgerPage(ledgerPage + 1)"
                  >
                    <ChevronRight :size="13" />
                  </button>
                </div></section
            ></template>
            <template v-else
              ><section class="security-summary">
                <KeyRound :size="20" />
                <div>
                  <strong>{{
                    t("customer.activeSessions", { count: activeSessions })
                  }}</strong>
                  <p>
                    {{
                      t("customer.lastLogin", {
                        time: dateTime(detail.user.last_login_at),
                      })
                    }}
                  </p>
                </div>
              </section>
              <section class="detail-section">
                <h3>{{ t("customer.loginSessions") }}</h3>
                <article
                  v-for="session in detail.sessions"
                  :key="session.id"
                  class="security-entry"
                >
                  <div>
                    <strong>{{
                      session.device || t("customer.unknownDevice")
                    }}</strong>
                    <p>
                      {{ session.ip }} ·
                      {{ session.user_agent || t("customer.unknownBrowser") }}
                    </p>
                    <small
                      >{{
                        t("customer.sessionActive", {
                          time: dateTime(session.last_active_at),
                        })
                      }}
                      ·
                      {{
                        t("customer.sessionExpires", {
                          time: dateTime(session.expires_at),
                        })
                      }}</small
                    >
                  </div>
                  <span
                    class="status-chip"
                    :class="session.revoked_at ? 'neutral' : 'success'"
                    >{{
                      session.revoked_at
                        ? t("customer.revoked")
                        : t("customer.active")
                    }}</span
                  >
                </article>
                <p v-if="!detail.sessions?.length" class="muted">
                  {{ t("customer.noSessions") }}
                </p>
              </section>
              <section class="detail-section">
                <h3>{{ t("customer.loginEvents") }}</h3>
                <article
                  v-for="event in detail.login_events"
                  :key="event.id"
                  class="security-entry"
                >
                  <div>
                    <strong
                      >{{ event.ip }} · {{ event.country }}
                      {{ event.city }}</strong
                    >
                    <p>{{ event.user_agent || t("customer.unknownClient") }}</p>
                    <small
                      >{{
                        event.reason ||
                        (event.succeeded
                          ? t("customer.authSuccess")
                          : t("customer.authFailed"))
                      }}
                      · {{ dateTime(event.created_at) }}</small
                    >
                  </div>
                  <span
                    class="status-chip"
                    :class="event.succeeded ? 'success' : 'danger'"
                    >{{
                      event.succeeded
                        ? t("customer.success")
                        : t("customer.failed")
                    }}</span
                  >
                </article>
              </section></template
            >
            <div v-if="actionError" class="inline-error">
              <AlertCircle :size="14" />{{ actionError }}
            </div>
          </div></template
        >
      </aside>
    </div>

    <div
      v-if="exportOpen"
      class="modal-backdrop"
      @mousedown.self="!exportSaving && (exportOpen = false)"
    >
      <section class="export-modal">
        <header>
          <div>
            <Download :size="18" />
            <div>
              <h2>{{ t("customer.exportTitle") }}</h2>
              <p>{{ t("customer.exportHint") }}</p>
            </div>
          </div>
          <button
            type="button"
            :disabled="exportSaving"
            @click="exportOpen = false"
          >
            <X :size="16" />
          </button>
        </header>
        <div>
          <label
            ><span>{{ t("customer.exportPurpose") }}</span
            ><textarea
              v-model="exportReason"
              maxlength="500"
              rows="4"
              :placeholder="t('customer.exportPurposePlaceholder')"
            ></textarea>
          </label>
          <div v-if="exportError" class="inline-error">
            <AlertCircle :size="14" />{{ exportError }}
          </div>
        </div>
        <footer>
          <button
            type="button"
            class="secondary-button"
            :disabled="exportSaving"
            @click="exportOpen = false"
          >
            {{ t("customer.cancel") }}</button
          ><button
            type="button"
            class="primary-button"
            :disabled="exportSaving"
            @click="exportCustomers"
          >
            <LoaderCircle
              v-if="exportSaving"
              :size="14"
              class="spinning"
            /><Download v-else :size="14" />{{
              exportSaving
                ? t("customer.generating")
                : t("customer.generateCsv")
            }}
          </button>
        </footer>
      </section>
    </div>
  </section>
</template>

<style scoped>
.customer-view {
  display: grid;
  gap: 13px;
  color: var(--text);
}
.customer-toolbar,
.customer-toolbar > div,
.customer-search,
.customer-pagination,
.customer-pagination > div {
  display: flex;
  align-items: center;
  gap: 7px;
}
.customer-toolbar,
.customer-pagination {
  justify-content: space-between;
}
.view-tabs {
  padding: 3px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface);
}
.view-tabs button {
  display: inline-flex;
  min-height: 31px;
  align-items: center;
  gap: 5px;
  padding: 0 10px;
  border: 0;
  border-radius: 5px;
  color: var(--muted);
  background: transparent;
  font: inherit;
  font-size: 9px;
  cursor: pointer;
}
.view-tabs button.active {
  color: var(--surface);
  background: var(--text);
}
.primary-button,
.secondary-button,
.danger-button {
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
.danger-button {
  color: white;
  border-color: #b42318;
  background: #b42318;
}
button:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.customer-search {
  padding: 9px 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
}
.customer-search > div {
  display: flex;
  min-width: 300px;
  height: 32px;
  align-items: center;
  gap: 6px;
  padding-left: 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--muted);
}
.customer-search input {
  min-width: 0;
  flex: 1;
  border: 0;
  outline: 0;
  color: var(--text);
  background: transparent;
  font: inherit;
  font-size: 9px;
}
.customer-search > div button {
  height: 26px;
  margin-right: 3px;
  padding: 0 9px;
  border: 0;
  border-radius: 4px;
  color: var(--surface);
  background: var(--text);
  font: inherit;
  font-size: 8px;
  cursor: pointer;
}
.customer-search select {
  height: 32px;
  padding: 0 25px 0 8px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 9px;
}
.customer-search > span {
  margin-left: auto;
  color: var(--muted);
  font-size: 8px;
}
.text-button {
  border: 0;
  color: var(--muted);
  background: transparent;
  font: inherit;
  font-size: 8px;
  cursor: pointer;
}
.customer-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}
.customer-metrics article {
  min-height: 82px;
  padding: 13px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
}
.customer-metrics span {
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: 8px;
}
.customer-metrics strong {
  display: block;
  margin-top: 9px;
  font-size: 16px;
}
.customer-metrics small {
  display: block;
  margin-top: 4px;
  color: var(--muted);
  font-size: 8px;
}
.customer-alert {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 9px 10px;
  border: 1px solid;
  border-radius: 7px;
  font-size: 9px;
}
.customer-alert span {
  flex: 1;
}
.customer-alert button {
  border: 0;
  color: inherit;
  background: transparent;
  font: inherit;
  cursor: pointer;
}
.customer-alert.success {
  color: #166534;
  border-color: #86efac;
  background: #f0fdf4;
}
.customer-alert.danger,
.inline-error {
  color: #991b1b;
  border-color: #fecaca;
  background: #fef2f2;
}
:global([data-theme="dark"]) .customer-alert.success {
  color: #bbf7d0;
  border-color: #166534;
  background: #052e16;
}
:global([data-theme="dark"]) .customer-alert.danger,
:global([data-theme="dark"]) .inline-error {
  color: #fecaca;
  border-color: #7f1d1d;
  background: #450a0a;
}
.customer-table-shell {
  min-height: 320px;
  overflow-x: auto;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
}
.customer-table {
  width: 100%;
  min-width: 850px;
  border-collapse: collapse;
}
.customer-table th {
  padding: 11px 12px;
  color: var(--muted);
  border-bottom: 1px solid var(--line);
  background: var(--soft);
  font-size: 8px;
  font-weight: 600;
  text-align: left;
}
.customer-table td {
  padding: 12px;
  border-bottom: 1px solid var(--line);
  font-size: 9px;
}
.customer-table tr:last-child td {
  border-bottom: 0;
}
.customer-table td > strong,
.customer-table td > span,
.customer-table td > small {
  display: block;
}
.customer-table td > strong {
  font-size: 10px;
}
.customer-table td > span,
.customer-table td > small {
  margin-top: 4px;
  color: var(--muted);
  font-size: 8px;
}
.status-chip {
  display: inline-flex !important;
  width: fit-content;
  min-height: 22px;
  align-items: center;
  padding: 0 6px;
  border: 1px solid var(--line);
  border-radius: 999px;
  font-size: 8px;
}
.status-chip.success {
  color: #166534;
  border-color: #bbf7d0;
  background: #f0fdf4;
}
.status-chip.warning {
  color: #92400e;
  border-color: #fde68a;
  background: #fffbeb;
}
.status-chip.danger {
  color: #991b1b;
  border-color: #fecaca;
  background: #fef2f2;
}
.status-chip.neutral {
  color: var(--muted);
  background: var(--soft);
}
:global([data-theme="dark"]) .status-chip.success {
  color: #bbf7d0;
  border-color: #166534;
  background: #052e16;
}
:global([data-theme="dark"]) .status-chip.warning {
  color: #fde68a;
  border-color: #92400e;
  background: #451a03;
}
:global([data-theme="dark"]) .status-chip.danger {
  color: #fecaca;
  border-color: #991b1b;
  background: #450a0a;
}
.row-action {
  display: inline-flex;
  min-height: 28px;
  align-items: center;
  gap: 5px;
  padding: 0 8px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 8px;
  cursor: pointer;
}
.customer-empty,
.drawer-empty {
  min-height: 300px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--muted);
  font-size: 9px;
}
.customer-empty strong {
  color: var(--text);
  font-size: 11px;
}
.customer-pagination {
  color: var(--muted);
  font-size: 8px;
}
.customer-pagination button {
  display: inline-flex;
  min-height: 31px;
  align-items: center;
  gap: 4px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 8px;
  cursor: pointer;
}
.drawer-backdrop,
.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 80;
  background: rgb(0 0 0 / 55%);
  backdrop-filter: blur(2px);
}
.drawer-backdrop {
  display: flex;
  justify-content: flex-end;
}
.customer-drawer {
  width: min(760px, 95vw);
  height: 100%;
  overflow-y: auto;
  border-left: 1px solid var(--line);
  background: var(--surface);
  box-shadow: -20px 0 60px rgb(0 0 0 / 30%);
}
.customer-drawer > header {
  position: sticky;
  top: 0;
  z-index: 4;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  padding: 16px 18px;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
}
.customer-drawer header > div {
  display: flex;
  gap: 9px;
}
.customer-drawer header > div > span {
  display: grid;
  width: 35px;
  height: 35px;
  place-items: center;
  border-radius: 8px;
  color: var(--surface);
  background: var(--text);
}
.customer-drawer h2 {
  margin: 0;
  font-size: 13px;
}
.customer-drawer header p {
  margin: 5px 0 0;
  color: var(--muted);
  font-size: 8px;
}
.customer-drawer header > button {
  border: 0;
  color: var(--text);
  background: transparent;
  cursor: pointer;
}
.detail-tabs {
  position: sticky;
  top: 68px;
  z-index: 3;
  display: flex;
  overflow-x: auto;
  padding: 7px 18px;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
}
.detail-tabs button {
  min-height: 31px;
  padding: 0 10px;
  border: 0;
  border-radius: 6px;
  color: var(--muted);
  background: transparent;
  font: inherit;
  font-size: 9px;
  white-space: nowrap;
  cursor: pointer;
}
.detail-tabs button.active {
  color: var(--surface);
  background: var(--text);
}
.drawer-content {
  display: grid;
  gap: 11px;
  padding: 16px 18px 40px;
}
.identity-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 13px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--soft);
}
.identity-card > div {
  display: flex;
  align-items: center;
  gap: 9px;
}
.customer-identity {
  display: flex;
  align-items: center;
  gap: 8px;
}
.customer-identity img {
  width: 28px;
  height: 28px;
  border-radius: 9px;
  object-fit: cover;
}
.avatar-image {
  object-fit: cover;
}
.avatar {
  display: grid;
  width: 42px;
  height: 42px;
  place-items: center;
  border-radius: 50%;
  color: var(--surface);
  background: var(--text);
  font-weight: 800;
}
.identity-card strong {
  font-size: 10px;
}
.identity-card p,
.identity-card small {
  margin: 4px 0 0;
  color: var(--muted);
  font-size: 8px;
}
.detail-metrics,
.wallet-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 7px;
}
.wallet-summary {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}
.detail-metrics article,
.wallet-summary article {
  display: grid;
  gap: 6px;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
}
.detail-metrics span,
.wallet-summary span {
  color: var(--muted);
  font-size: 8px;
}
.detail-metrics strong,
.wallet-summary strong {
  font-size: 10px;
}
.detail-section {
  display: grid;
  gap: 8px;
  padding: 13px;
  border: 1px solid var(--line);
  border-radius: 8px;
}
.detail-section h3 {
  margin: 0;
  font-size: 10px;
}
.detail-section > p,
.muted {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.6;
}
.identity-lines {
  display: grid;
  gap: 0;
}
.identity-lines p {
  display: grid;
  grid-template-columns: 20px 100px minmax(0, 1fr);
  align-items: center;
  margin: 0;
  padding: 9px 0;
  border-bottom: 1px solid var(--line);
  font-size: 8px;
}
.identity-lines p:last-child {
  border-bottom: 0;
}
.identity-lines span {
  color: var(--muted);
}
.identity-lines small {
  color: var(--muted);
}
.membership-meta {
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr);
  gap: 7px 10px;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-2);
  font-size: 8px;
}
.membership-meta span {
  color: var(--muted);
}
.membership-meta b {
  text-align: right;
  overflow-wrap: anywhere;
}
.membership-form {
  display: grid;
  gap: 9px;
  margin-top: 4px;
  padding-top: 12px;
  border-top: 1px solid var(--line);
}
.membership-form h4,
.membership-form p {
  margin: 0;
}
.membership-form h4 {
  font-size: 9px;
}
.membership-form p,
.membership-form small {
  color: var(--muted);
  font-size: 7px;
  line-height: 1.55;
}
.membership-form > div:not(.membership-actions) {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}
.membership-form label {
  display: grid;
  gap: 5px;
  color: var(--muted);
  font-size: 8px;
}
.membership-form select {
  width: 100%;
  height: 35px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--text);
  padding: 0 8px;
  font-size: 8px;
}
.membership-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 7px;
}
.membership-actions button {
  min-height: 34px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
}
.detail-section input,
.detail-section textarea,
.export-modal textarea {
  width: 100%;
  box-sizing: border-box;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 9px;
  outline: none;
}
.detail-section input {
  height: 35px;
  padding: 0 9px;
}
.detail-section textarea,
.export-modal textarea {
  padding: 9px;
  resize: vertical;
}
.danger-zone {
  border-color: #fecaca;
}
.adjustment > div {
  display: grid;
  grid-template-columns: 150px minmax(0, 1fr);
  gap: 8px;
}
.adjustment label {
  display: grid;
  gap: 5px;
  font-size: 8px;
}
.adjustment code {
  color: var(--muted);
  font-size: 7px;
  background: transparent;
}
.inline-error {
  display: flex;
  align-items: flex-start;
  gap: 5px;
  padding: 8px 9px;
  border: 1px solid;
  border-radius: 6px;
  font-size: 8px;
}
.order-list {
  display: grid;
  gap: 7px;
}
.order-list article,
.ledger-entry,
.security-entry {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 7px 12px;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--soft);
}
.order-list article > div,
.ledger-entry > div,
.security-entry > div {
  display: grid;
  gap: 4px;
}
.order-list article > div:last-child,
.ledger-entry > div:last-child {
  justify-items: end;
}
.order-list strong,
.ledger-entry strong,
.security-entry strong {
  font-size: 9px;
}
.order-list p,
.order-list small,
.ledger-entry code,
.ledger-entry small,
.security-entry p,
.security-entry small {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
}
.ledger-entry .positive {
  color: #166534;
}
.ledger-entry .negative {
  color: #b42318;
}
.ledger-pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}
.ledger-pagination button {
  display: grid;
  width: 27px;
  height: 27px;
  place-items: center;
  border: 1px solid var(--line);
  border-radius: 5px;
  color: var(--text);
  background: var(--surface);
  cursor: pointer;
}
.ledger-pagination span {
  color: var(--muted);
  font-size: 8px;
}
.security-summary {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 13px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--soft);
}
.security-summary strong {
  font-size: 10px;
}
.security-summary p {
  margin: 4px 0 0;
  color: var(--muted);
  font-size: 8px;
}
.danger-text {
  color: #b42318;
}
.modal-backdrop {
  display: grid;
  place-items: center;
  padding: 18px;
}
.export-modal {
  width: min(520px, 100%);
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
}
.export-modal > header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  padding: 15px 17px;
  border-bottom: 1px solid var(--line);
}
.export-modal header > div {
  display: flex;
  gap: 9px;
}
.export-modal h2 {
  margin: 0;
  font-size: 13px;
}
.export-modal header p {
  margin: 5px 0 0;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.5;
}
.export-modal header > button {
  border: 0;
  color: var(--text);
  background: transparent;
  cursor: pointer;
}
.export-modal > div {
  display: grid;
  gap: 8px;
  padding: 16px 17px;
}
.export-modal label {
  display: grid;
  gap: 6px;
  font-size: 9px;
}
.export-modal footer {
  display: flex;
  justify-content: flex-end;
  gap: 7px;
  padding: 12px 17px;
  border-top: 1px solid var(--line);
}
.spinning {
  animation: customer-spin 0.8s linear infinite;
}
@keyframes customer-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 850px) {
  .customer-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .detail-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 650px) {
  .customer-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .customer-toolbar > div:last-child {
    justify-content: flex-end;
  }
  .customer-search {
    flex-wrap: wrap;
  }
  .customer-search > div {
    min-width: 0;
    width: 100%;
  }
  .customer-search > span {
    margin-left: 0;
  }
  .customer-table-shell {
    overflow: visible;
    border: 0;
    background: transparent;
  }
  .customer-table {
    min-width: 0;
    display: block;
  }
  .customer-table thead {
    display: none;
  }
  .customer-table tbody,
  .customer-table tr,
  .customer-table td {
    display: block;
    width: 100%;
    box-sizing: border-box;
  }
  .customer-table tr {
    margin-bottom: 8px;
    padding: 7px 10px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--surface);
  }
  .customer-table td {
    min-height: 35px;
    padding: 9px 0 9px 100px;
    position: relative;
    border-bottom: 1px solid var(--line);
  }
  .customer-table td:last-child {
    border-bottom: 0;
  }
  .customer-table td::before {
    content: attr(data-label);
    position: absolute;
    left: 0;
    top: 11px;
    color: var(--muted);
    font-size: 8px;
  }
  .customer-drawer {
    width: 100%;
  }
  .adjustment > div {
    grid-template-columns: 1fr;
  }
  .membership-form > div:not(.membership-actions) {
    grid-template-columns: 1fr;
  }
  .membership-actions button {
    flex: 1 1 140px;
  }
}
.customer-batch-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--line);
  background: var(--soft);
}
.customer-batch-toolbar > span {
  color: var(--muted);
  font-size: 9px;
}
.customer-batch-toolbar > div {
  display: flex;
  gap: 7px;
  margin-left: auto;
}
.customer-batch-toolbar button {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  min-height: 32px;
  padding: 0 10px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface);
  cursor: pointer;
}
.customer-batch-toolbar button.danger {
  color: var(--danger);
}
.selection-cell {
  width: 42px;
  text-align: center;
}
.selection-cell input {
  width: 16px;
  height: 16px;
  accent-color: var(--text);
}
@media (max-width: 470px) {
  .customer-metrics {
    grid-template-columns: 1fr;
  }
  .customer-pagination {
    align-items: flex-start;
    flex-direction: column;
  }
  .customer-pagination > div {
    width: 100%;
  }
  .customer-pagination button {
    flex: 1;
  }
  .detail-tabs {
    padding-right: 12px;
    padding-left: 12px;
  }
  .drawer-content {
    padding-right: 12px;
    padding-left: 12px;
  }
  .wallet-summary {
    grid-template-columns: 1fr;
  }
  .modal-backdrop {
    padding: 0;
  }
  .export-modal {
    min-height: 100vh;
    border-radius: 0;
  }
  .customer-batch-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }
  .customer-batch-toolbar > div {
    width: 100%;
    margin-left: 0;
    flex-wrap: wrap;
  }
}
</style>
