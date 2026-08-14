<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { onBeforeRouteLeave, useRoute, useRouter } from "vue-router";
import {
  AlertCircle,
  AlertTriangle,
  ArchiveX,
  Check,
  ChevronLeft,
  ChevronRight,
  ClipboardCopy,
  Download,
  Eye,
  EyeOff,
  Gift,
  Layers3,
  LoaderCircle,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import { safeCSVCell } from "../utils/csv";
import {
  currencyDirectory,
  formatMoney as formatMinorMoney,
  majorInputStep,
  majorToMinor,
  minorToSafeNumber,
  storeCurrency,
} from "../utils/money";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const authStore = useAuthStore();
const canManage = computed(() => authStore.hasPermission("marketing.manage"));

type GiftCardTab = "batches" | "cards";
type BatchStatus = "active" | "disabled";
type CardStatus = "active" | "disabled" | "redeemed" | "expired";
type ModalKind = "issue" | "disable-batch" | "card-status" | "result";

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface GiftCardBatch {
  id: string;
  batch_no: string;
  name: string;
  quantity: number;
  card_value: number;
  currency: string;
  status: BatchStatus | string;
  expires_at?: string | null;
  issued_by: string;
  disabled_at?: string | null;
  created_at: string;
  updated_at: string;
}

interface GiftCardItem {
  id: string;
  batch_id?: string | null;
  batch_no?: string;
  batch_name?: string;
  code_preview: string;
  initial_balance: number;
  balance: number;
  currency: string;
  status: CardStatus | string;
  redeemed_by?: string | null;
  redeemed_at?: string | null;
  expires_at?: string | null;
  created_at: string;
}

interface IssuedGiftCard {
  id: string;
  code: string;
  code_preview: string;
  card_value: number;
  expires_at?: string | null;
}

interface IssuedBatchResult {
  batch: GiftCardBatch;
  cards: IssuedGiftCard[];
  notice: string;
}

interface IssueForm {
  name: string;
  quantity: number;
  cardValueMajor: string;
  currency: string;
  hasExpiry: boolean;
  expiresAt: string;
  reason: string;
}

function batchStatusLabel(status: string) {
  const key = `giftcardadmin.batchStatus.${status}`;
  return t(key) === key ? status : t(key);
}
function cardStatusLabel(status: string) {
  const key = `giftcardadmin.cardStatus.${status}`;
  return t(key) === key ? status : t(key);
}
const batchStatusOptions = [
  { value: "", label: "giftcardadmin.batchStatusOptions.all" },
  { value: "active", label: "giftcardadmin.batchStatusOptions.active" },
  { value: "disabled", label: "giftcardadmin.batchStatusOptions.disabled" },
];
const cardStatusOptions = [
  { value: "", label: "giftcardadmin.cardStatusOptions.all" },
  { value: "active", label: "giftcardadmin.cardStatusOptions.active" },
  { value: "disabled", label: "giftcardadmin.cardStatusOptions.disabled" },
  { value: "redeemed", label: "giftcardadmin.cardStatusOptions.redeemed" },
  { value: "expired", label: "giftcardadmin.cardStatusOptions.expired" },
];

const activeTab = ref<GiftCardTab>("batches");
const batches = ref<GiftCardBatch[]>([]);
const cards = ref<GiftCardItem[]>([]);
const knownBatches = ref<GiftCardBatch[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const searchInput = ref("");
const appliedSearch = ref("");
const statusFilter = ref("");
const batchFilter = ref("");
const loading = ref(false);
const loadError = ref("");
const notice = ref("");
const modalKind = ref<ModalKind | null>(null);
const saving = ref(false);
const formError = ref("");
const editingBatch = ref<GiftCardBatch | null>(null);
const editingCard = ref<GiftCardItem | null>(null);
const changeReason = ref("");
const issuedResult = ref<IssuedBatchResult | null>(null);
const codesRevealed = ref(false);
const resultAcknowledged = ref(false);
const resultActionMessage = ref("");
const copying = ref(false);
const downloading = ref(false);
let listRequest = 0;
let directoryRequest = 0;

function localDateValue(date: Date) {
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function defaultIssueForm(): IssueForm {
  const expiry = new Date();
  expiry.setFullYear(expiry.getFullYear() + 1);
  expiry.setSeconds(0, 0);
  return {
    name: "",
    quantity: 10,
    cardValueMajor: "100.00",
    currency: storeCurrency.value,
    hasExpiry: true,
    expiresAt: localDateValue(expiry),
    reason: "",
  };
}

const issueForm = ref<IssueForm>(defaultIssueForm());
const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const pageNumbers = computed(() => {
  const first = Math.max(1, Math.min(page.value - 2, totalPages.value - 4));
  const last = Math.min(totalPages.value, first + 4);
  return Array.from({ length: last - first + 1 }, (_, index) => first + index);
});
const activeItemsCount = computed(() =>
  activeTab.value === "batches" ? batches.value.length : cards.value.length,
);
const batchLookup = computed(
  () => new Map(knownBatches.value.map((item) => [item.id, item])),
);
const batchDirectoryOptions = computed(() =>
  [...knownBatches.value].sort((left, right) =>
    right.created_at.localeCompare(left.created_at),
  ),
);
const enabledCurrencies = computed(() =>
  Object.values(currencyDirectory.value).filter(
    (item) => item.enabled !== false,
  ),
);
const issueLiability = computed(() => {
  try {
    return (
      BigInt(
        majorToMinor(issueForm.value.cardValueMajor, issueForm.value.currency),
      ) * BigInt(issueForm.value.quantity || 0)
    );
  } catch {
    return 0n;
  }
});
const targetCardStatus = computed<CardStatus | "">(() => {
  if (editingCard.value?.status === "active") return "disabled";
  if (editingCard.value?.status === "disabled") return "active";
  return "";
});

function apiMessage(error: unknown, fallback: string) {
  const failure = error as { response?: { data?: { message?: string } } };
  return failure.response?.data?.message || fallback;
}

function formatMoney(value?: number | bigint, currency?: string) {
  return formatMinorMoney(value, currency || storeCurrency.value, locale.value);
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

function isExpired(value?: string | null) {
  if (!value) return false;
  const time = new Date(value).getTime();
  return Number.isFinite(time) && time <= Date.now();
}

function cardDisplayStatus(item: GiftCardItem) {
  if (isExpired(item.expires_at) && item.status !== "redeemed")
    return "expired";
  return item.status;
}

function maskIssuedCode(code: string) {
  if (code.length <= 20) return "••••••••••••";
  return `${code.slice(0, 10)}••••••••${code.slice(-8)}`;
}

function validReason(value: string) {
  const length = [...value.trim()].length;
  return length >= 4 && length <= 500;
}

function mergeKnownBatches(items: GiftCardBatch[]) {
  const merged = new Map(knownBatches.value.map((item) => [item.id, item]));
  for (const item of items) merged.set(item.id, item);
  knownBatches.value = [...merged.values()];
}

async function loadBatchDirectory() {
  const request = ++directoryRequest;
  try {
    const { data } = await adminApi.get("/gift-card-batches", {
      params: { page: 1, page_size: 100 },
    });
    if (request !== directoryRequest) return;
    const payload = data.data as PagePayload<GiftCardBatch>;
    mergeKnownBatches(Array.isArray(payload.items) ? payload.items : []);
  } catch {
    // The primary card list remains usable; known batch names are optional UI context.
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
    if (appliedSearch.value) params.q = appliedSearch.value;
    if (statusFilter.value) params.status = statusFilter.value;
    if (tab === "cards" && batchFilter.value)
      params.batch_id = batchFilter.value;
    const { data } = await adminApi.get(
      tab === "batches" ? "/gift-card-batches" : "/gift-cards",
      { params },
    );
    if (request !== listRequest || tab !== activeTab.value) return;
    const payload = data.data as PagePayload<GiftCardBatch | GiftCardItem>;
    const items = Array.isArray(payload.items) ? payload.items : [];
    if (tab === "batches") {
      batches.value = items as GiftCardBatch[];
      mergeKnownBatches(batches.value);
    } else {
      cards.value = items as GiftCardItem[];
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
    if (tab === "batches") batches.value = [];
    else cards.value = [];
    total.value = 0;
    loadError.value = apiMessage(error, t("giftcardadmin.errLoad"));
  } finally {
    if (request === listRequest) loading.value = false;
  }
}

async function applySearch() {
  appliedSearch.value = searchInput.value.trim();
  page.value = 1;
  await loadList();
}

async function clearSearch() {
  searchInput.value = "";
  appliedSearch.value = "";
  page.value = 1;
  await loadList();
}

async function applyFilters() {
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

async function viewBatchCards(item: GiftCardBatch) {
  await router.push({
    path: "/gift-card-list",
    query: { batch_id: item.id, batch_no: item.batch_no },
  });
}

function openIssue() {
  if (!canManage.value) return;
  issueForm.value = defaultIssueForm();
  editingBatch.value = null;
  editingCard.value = null;
  formError.value = "";
  modalKind.value = "issue";
}

function openDisableBatch(item: GiftCardBatch) {
  if (!canManage.value) return;
  if (item.status !== "active") return;
  editingBatch.value = item;
  editingCard.value = null;
  changeReason.value = "";
  formError.value = "";
  modalKind.value = "disable-batch";
}

function cardUnavailableReason(item: GiftCardItem) {
  if (item.redeemed_at || item.redeemed_by || item.status === "redeemed")
    return t("giftcardadmin.unavailableRedeemed");
  if (item.balance <= 0) return t("giftcardadmin.unavailableZero");
  if (item.status === "expired" || isExpired(item.expires_at))
    return t("giftcardadmin.unavailableExpired");
  if (item.status !== "active" && item.status !== "disabled")
    return t("giftcardadmin.unavailableStatus");
  if (
    item.status === "disabled" &&
    item.batch_id &&
    batchLookup.value.get(item.batch_id)?.status === "disabled"
  )
    return t("giftcardadmin.unavailableBatchDisabled");
  return "";
}

function openCardStatus(item: GiftCardItem) {
  if (!canManage.value) return;
  if (cardUnavailableReason(item)) return;
  editingCard.value = item;
  editingBatch.value = null;
  changeReason.value = "";
  formError.value = "";
  modalKind.value = "card-status";
}

function clearIssuedResult() {
  issuedResult.value = null;
  codesRevealed.value = false;
  resultAcknowledged.value = false;
  resultActionMessage.value = "";
}

function closeModal() {
  if (saving.value || copying.value || downloading.value) return;
  if (modalKind.value === "result" && issuedResult.value) {
    if (!resultAcknowledged.value) {
      resultActionMessage.value = t("giftcardadmin.errAckFirst");
      return;
    }
    clearIssuedResult();
  }
  modalKind.value = null;
  editingBatch.value = null;
  editingCard.value = null;
  changeReason.value = "";
  formError.value = "";
}

function validateIssue() {
  const form = issueForm.value;
  const nameLength = [...form.name.trim()].length;
  if (nameLength < 2 || nameLength > 160)
    return t("giftcardadmin.errNameLength");
  if (
    !Number.isInteger(form.quantity) ||
    form.quantity < 1 ||
    form.quantity > 500
  )
    return t("giftcardadmin.errQtyRange");
  try {
    const value = BigInt(majorToMinor(form.cardValueMajor, form.currency));
    const maximum = BigInt(majorToMinor("1000000", form.currency));
    if (value <= 0n || value > maximum) throw new Error("out of range");
  } catch {
    return t("giftcardadmin.errValueRange");
  }
  if (form.hasExpiry) {
    const expiresAt = new Date(form.expiresAt);
    const minimum = Date.now() + 5 * 60_000;
    const maximum = new Date();
    maximum.setFullYear(maximum.getFullYear() + 10);
    if (
      Number.isNaN(expiresAt.getTime()) ||
      expiresAt.getTime() <= minimum ||
      expiresAt > maximum
    )
      return t("giftcardadmin.errExpiryRange");
  }
  if (!validReason(form.reason)) return t("giftcardadmin.errReason");
  return "";
}

async function submitIssue() {
  if (!canManage.value) return;
  const validation = validateIssue();
  if (validation) {
    formError.value = validation;
    return;
  }
  const form = issueForm.value;
  saving.value = true;
  formError.value = "";
  try {
    const { data } = await adminApi.post(
      "/gift-card-batches",
      {
        name: form.name.trim(),
        quantity: form.quantity,
        card_value: minorToSafeNumber(
          majorToMinor(form.cardValueMajor, form.currency),
        ),
        currency: form.currency,
        expires_at: form.hasExpiry
          ? new Date(form.expiresAt).toISOString()
          : null,
      },
      { headers: { "X-Change-Reason": form.reason.trim() } },
    );
    const result = data.data as IssuedBatchResult;
    if (!result?.batch || !Array.isArray(result.cards)) {
      throw new Error(t("giftcardadmin.errResultMissing"));
    }
    issuedResult.value = result;
    mergeKnownBatches([result.batch]);
    codesRevealed.value = false;
    resultAcknowledged.value = false;
    resultActionMessage.value = "";
    modalKind.value = "result";
    notice.value = t("giftcardadmin.issuedNotice", {
      batchNo: result.batch.batch_no,
      count: result.cards.length,
    });
    await loadList();
  } catch (error: unknown) {
    formError.value = apiMessage(error, t("giftcardadmin.errIssue"));
  } finally {
    saving.value = false;
  }
}

async function submitDisableBatch() {
  if (!canManage.value) return;
  const item = editingBatch.value;
  if (!item) return;
  if (!validReason(changeReason.value)) {
    formError.value = t("giftcardadmin.errDisableReason");
    return;
  }
  saving.value = true;
  formError.value = "";
  try {
    await adminApi.patch(
      `/gift-card-batches/${encodeURIComponent(item.id)}`,
      { status: "disabled" },
      { headers: { "X-Change-Reason": changeReason.value.trim() } },
    );
    modalKind.value = null;
    editingBatch.value = null;
    notice.value = t("giftcardadmin.disabledNotice", {
      batchNo: item.batch_no,
    });
    await loadList();
  } catch (error: unknown) {
    formError.value = apiMessage(error, t("giftcardadmin.errDisableBatch"));
  } finally {
    saving.value = false;
  }
}

async function submitCardStatus() {
  if (!canManage.value) return;
  const item = editingCard.value;
  const target = targetCardStatus.value;
  if (!item || !target) return;
  const unavailable = cardUnavailableReason(item);
  if (unavailable) {
    formError.value = unavailable;
    return;
  }
  if (!validReason(changeReason.value)) {
    formError.value = t("giftcardadmin.errChangeReason");
    return;
  }
  saving.value = true;
  formError.value = "";
  try {
    await adminApi.patch(
      `/gift-cards/${encodeURIComponent(item.id)}/status`,
      { status: target },
      { headers: { "X-Change-Reason": changeReason.value.trim() } },
    );
    modalKind.value = null;
    editingCard.value = null;
    notice.value = t("giftcardadmin.cardStatusNotice", {
      code: item.code_preview,
      action:
        target === "active"
          ? t("giftcardadmin.actionEnabled")
          : t("giftcardadmin.actionDisabled"),
    });
    await loadList();
  } catch (error: unknown) {
    formError.value = apiMessage(error, t("giftcardadmin.errCardStatus"));
  } finally {
    saving.value = false;
  }
}

function issuedCodesText() {
  return issuedResult.value?.cards.map((item) => item.code).join("\n") || "";
}

async function writeClipboard(value: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  const copied = document.execCommand("copy");
  textarea.remove();
  if (!copied) throw new Error("clipboard unavailable");
}

async function copyIssuedCodes() {
  if (!canManage.value) return;
  if (!issuedResult.value || copying.value) return;
  copying.value = true;
  resultActionMessage.value = "";
  try {
    await writeClipboard(issuedCodesText());
    resultActionMessage.value = t("giftcardadmin.copiedMessage");
  } catch {
    resultActionMessage.value = t("giftcardadmin.copyFallback");
  } finally {
    copying.value = false;
  }
}

function downloadIssuedCodes() {
  if (!canManage.value) return;
  const result = issuedResult.value;
  if (!result || downloading.value) return;
  downloading.value = true;
  resultActionMessage.value = "";
  try {
    const rows = [
      ["card_id", "code", "code_preview", "card_value_cents", "expires_at"],
      ...result.cards.map((item) => [
        item.id,
        item.code,
        item.code_preview,
        item.card_value,
        item.expires_at || "",
      ]),
    ];
    const csv = `\uFEFF${rows.map((row) => row.map(safeCSVCell).join(",")).join("\r\n")}`;
    const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    const safeBatchNo = result.batch.batch_no.replace(/[^A-Za-z0-9_-]/g, "_");
    anchor.href = url;
    anchor.download = `${safeBatchNo}-gift-cards.csv`;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 1_000);
    resultActionMessage.value = t("giftcardadmin.downloadedMessage");
  } catch {
    resultActionMessage.value = t("giftcardadmin.downloadFailed");
  } finally {
    downloading.value = false;
  }
}

function handleEscape(event: KeyboardEvent) {
  if (event.key === "Escape") closeModal();
}

function handleBeforeUnload(event: BeforeUnloadEvent) {
  if (!issuedResult.value) return;
  event.preventDefault();
  event.returnValue = "";
}

watch(modalKind, (value) => {
  document.body.style.overflow = value ? "hidden" : "";
});

watch(
  () => [route.path, route.meta.defaultTab] as const,
  async ([, defaultTab]) => {
    activeTab.value = defaultTab === "cards" ? "cards" : "batches";
    page.value = 1;
    searchInput.value = "";
    appliedSearch.value = "";
    statusFilter.value = "";
    batchFilter.value = String(route.query.batch_id || "");
    notice.value = route.query.batch_no
      ? t("giftcardadmin.viewingBatch", {
          batchNo: String(route.query.batch_no),
        })
      : "";
    if (activeTab.value === "cards") void loadBatchDirectory();
    await loadList();
  },
  { immediate: true },
);

onBeforeRouteLeave(() => {
  if (!issuedResult.value) return true;
  const shouldLeave = window.confirm(t("giftcardadmin.leaveConfirm"));
  if (shouldLeave) clearIssuedResult();
  return shouldLeave;
});

onMounted(() => {
  window.addEventListener("keydown", handleEscape);
  window.addEventListener("beforeunload", handleBeforeUnload);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", handleEscape);
  window.removeEventListener("beforeunload", handleBeforeUnload);
  document.body.style.overflow = "";
  clearIssuedResult();
});
</script>

<template>
  <section class="gift-shell">
    <div class="gift-nav panel">
      <button
        v-if="canManage"
        class="primary-button compact"
        type="button"
        @click="openIssue"
      >
        <Plus :size="15" />{{ t("giftcardadmin.issueNew") }}
      </button>
    </div>

    <div class="gift-panel panel">
      <header class="gift-toolbar">
        <form class="gift-search" @submit.prevent="applySearch">
          <Search :size="15" />
          <input
            v-model="searchInput"
            type="search"
            :placeholder="
              activeTab === 'batches'
                ? t('giftcardadmin.searchPlaceholderBatches')
                : t('giftcardadmin.searchPlaceholderCards')
            "
            :aria-label="t('giftcardadmin.searchAria')"
          />
          <button v-if="appliedSearch" type="button" @click="clearSearch">
            <X :size="13" />{{ t("giftcardadmin.clear") }}
          </button>
          <button type="submit">{{ t("giftcardadmin.search") }}</button>
        </form>
        <div class="gift-filters">
          <select
            v-model="statusFilter"
            :aria-label="t('giftcardadmin.statusFilterAria')"
            @change="applyFilters"
          >
            <option
              v-for="option in activeTab === 'batches'
                ? batchStatusOptions
                : cardStatusOptions"
              :key="option.value || 'all'"
              :value="option.value"
            >
              {{ t(option.label) }}
            </option>
          </select>
          <select
            v-if="activeTab === 'cards'"
            v-model="batchFilter"
            :aria-label="t('giftcardadmin.batchFilterAria')"
            @change="applyFilters"
          >
            <option value="">{{ t("giftcardadmin.allBatches") }}</option>
            <option
              v-for="batch in batchDirectoryOptions"
              :key="batch.id"
              :value="batch.id"
            >
              {{ batch.name }} · {{ batch.batch_no }}
            </option>
          </select>
          <button type="button" :disabled="loading" @click="loadList">
            <RefreshCw :size="14" :class="{ spinning: loading }" />{{
              t("giftcardadmin.refresh")
            }}
          </button>
        </div>
      </header>

      <div v-if="notice" class="gift-notice success-notice">
        <Check :size="15" />{{ notice }}
      </div>
      <div v-if="loadError" class="gift-notice error-notice">
        <AlertCircle :size="15" />{{ loadError }}
        <button type="button" @click="loadList">
          {{ t("giftcardadmin.retry") }}
        </button>
      </div>

      <div v-if="loading && !activeItemsCount" class="gift-state">
        <LoaderCircle class="spinning" :size="23" />
        <span>{{ t("giftcardadmin.loading") }}</span>
      </div>
      <div
        v-else-if="
          !loadError &&
          ((activeTab === 'batches' && !batches.length) ||
            (activeTab === 'cards' && !cards.length))
        "
        class="gift-state"
      >
        <Layers3 v-if="activeTab === 'batches'" :size="27" />
        <Gift v-else :size="27" />
        <strong>{{
          appliedSearch || statusFilter || batchFilter
            ? t("giftcardadmin.noMatch")
            : activeTab === "batches"
              ? t("giftcardadmin.noBatches")
              : t("giftcardadmin.noCards")
        }}</strong>
        <span>{{ t("giftcardadmin.noMatchHint") }}</span>
      </div>

      <div v-else-if="activeTab === 'batches'" class="gift-table-wrap">
        <table class="gift-table">
          <thead>
            <tr>
              <th>{{ t("giftcardadmin.colBatch") }}</th>
              <th>{{ t("giftcardadmin.colQtyValue") }}</th>
              <th>{{ t("giftcardadmin.colTotalValue") }}</th>
              <th>{{ t("giftcardadmin.colExpiry") }}</th>
              <th>{{ t("giftcardadmin.colIssue") }}</th>
              <th>{{ t("giftcardadmin.colStatus") }}</th>
              <th>
                <span class="sr-only">{{ t("giftcardadmin.colActions") }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in batches" :key="item.id">
              <td :data-label="t('giftcardadmin.colBatch')">
                <div class="record-primary">
                  <span><Layers3 :size="15" /></span>
                  <div>
                    <b>{{ item.name }}</b>
                    <code>{{ item.batch_no }}</code>
                  </div>
                </div>
              </td>
              <td :data-label="t('giftcardadmin.colQtyValue')">
                <b>{{
                  t("giftcardadmin.qtyValue", {
                    qty: item.quantity,
                    value: formatMoney(item.card_value, item.currency),
                  })
                }}</b>
                <small>{{ item.currency || storeCurrency }}</small>
              </td>
              <td :data-label="t('giftcardadmin.colTotalValue')">
                <b>{{
                  formatMoney(item.quantity * item.card_value, item.currency)
                }}</b>
                <small>{{ t("giftcardadmin.liabilityHint") }}</small>
              </td>
              <td :data-label="t('giftcardadmin.colExpiry')">
                <b>{{
                  item.expires_at
                    ? formatTime(item.expires_at)
                    : t("giftcardadmin.permanent")
                }}</b>
                <small v-if="item.expires_at">
                  {{
                    isExpired(item.expires_at)
                      ? t("giftcardadmin.expired")
                      : t("giftcardadmin.notExpired")
                  }}
                </small>
              </td>
              <td :data-label="t('giftcardadmin.colIssue')">
                <time>{{ formatTime(item.created_at) }}</time>
                <small :title="item.issued_by">{{
                  t("giftcardadmin.issuedBy", { id: shortID(item.issued_by) })
                }}</small>
              </td>
              <td :data-label="t('giftcardadmin.colStatus')">
                <span class="status-badge" :class="`status-${item.status}`">
                  {{ batchStatusLabel(item.status) }}
                </span>
                <small v-if="item.disabled_at">
                  {{ formatTime(item.disabled_at) }}
                </small>
              </td>
              <td
                :data-label="t('giftcardadmin.colActions')"
                class="record-actions"
              >
                <button type="button" @click="viewBatchCards(item)">
                  <Gift :size="13" />{{ t("giftcardadmin.viewCards") }}
                </button>
                <button
                  v-if="canManage && item.status === 'active'"
                  type="button"
                  class="danger-action"
                  @click="openDisableBatch(item)"
                >
                  <ArchiveX :size="13" />{{ t("giftcardadmin.disableBatch") }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="gift-table-wrap">
        <table class="gift-table card-table">
          <thead>
            <tr>
              <th>{{ t("giftcardadmin.colCard") }}</th>
              <th>{{ t("giftcardadmin.colBatchOf") }}</th>
              <th>{{ t("giftcardadmin.colBalance") }}</th>
              <th>{{ t("giftcardadmin.colRedeem") }}</th>
              <th>{{ t("giftcardadmin.colExpiry") }}</th>
              <th>{{ t("giftcardadmin.colStatus") }}</th>
              <th>
                <span class="sr-only">{{ t("giftcardadmin.colActions") }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in cards" :key="item.id">
              <td :data-label="t('giftcardadmin.colCard')">
                <div class="record-primary">
                  <span><Gift :size="15" /></span>
                  <div>
                    <b>{{ item.code_preview }}</b>
                    <code :title="item.id">{{ shortID(item.id) }}</code>
                  </div>
                </div>
              </td>
              <td :data-label="t('giftcardadmin.colBatchOf')">
                <b>{{ item.batch_name || t("giftcardadmin.noBatchName") }}</b>
                <small>{{ item.batch_no || shortID(item.batch_id) }}</small>
              </td>
              <td :data-label="t('giftcardadmin.colBalance')">
                <b>
                  {{ formatMoney(item.initial_balance, item.currency) }} /
                  {{ formatMoney(item.balance, item.currency) }}
                </b>
                <div class="balance-track">
                  <i
                    :style="{
                      width: `${Math.min(
                        100,
                        item.initial_balance > 0
                          ? (item.balance / item.initial_balance) * 100
                          : 0,
                      )}%`,
                    }"
                  ></i>
                </div>
              </td>
              <td :data-label="t('giftcardadmin.colRedeem')">
                <b>{{
                  item.redeemed_at
                    ? t("giftcardadmin.redeemed")
                    : t("giftcardadmin.notRedeemed")
                }}</b>
                <small v-if="item.redeemed_at">
                  {{ formatTime(item.redeemed_at) }} ·
                  {{ shortID(item.redeemed_by) }}
                </small>
              </td>
              <td :data-label="t('giftcardadmin.colExpiry')">
                <b>{{
                  item.expires_at
                    ? formatTime(item.expires_at)
                    : t("giftcardadmin.permanent")
                }}</b>
                <small v-if="item.expires_at && isExpired(item.expires_at)">{{
                  t("giftcardadmin.expired")
                }}</small>
              </td>
              <td :data-label="t('giftcardadmin.colStatus')">
                <span
                  class="status-badge"
                  :class="`status-${cardDisplayStatus(item)}`"
                >
                  {{ cardStatusLabel(cardDisplayStatus(item)) }}
                </span>
              </td>
              <td
                :data-label="t('giftcardadmin.colActions')"
                class="record-actions"
              >
                <button
                  v-if="canManage && !cardUnavailableReason(item)"
                  type="button"
                  @click="openCardStatus(item)"
                >
                  <ShieldCheck :size="13" />
                  {{
                    item.status === "active"
                      ? t("giftcardadmin.disableCardAction")
                      : t("giftcardadmin.enableCardAction")
                  }}
                </button>
                <span
                  v-else
                  class="no-action"
                  :title="cardUnavailableReason(item)"
                >
                  {{ cardUnavailableReason(item) }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <footer v-if="!loadError" class="gift-pagination">
        <span>{{
          t("giftcardadmin.pagination", { page, pages: totalPages, total })
        }}</span>
        <div>
          <button
            type="button"
            :aria-label="t('giftcardadmin.prevPage')"
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
            :aria-label="t('giftcardadmin.nextPage')"
            :disabled="page >= totalPages || loading"
            @click="changePage(page + 1)"
          >
            <ChevronRight :size="14" />
          </button>
          <select
            v-model.number="pageSize"
            :aria-label="t('giftcardadmin.pageSizeAria')"
            @change="changePageSize"
          >
            <option :value="10">
              {{ t("giftcardadmin.perPage", { size: 10 }) }}
            </option>
            <option :value="20">
              {{ t("giftcardadmin.perPage", { size: 20 }) }}
            </option>
            <option :value="50">
              {{ t("giftcardadmin.perPage", { size: 50 }) }}
            </option>
          </select>
        </div>
      </footer>
    </div>

    <div
      v-if="modalKind && canManage"
      class="gift-modal-backdrop"
      role="presentation"
      @mousedown.self="closeModal"
    >
      <section
        class="gift-modal"
        :class="{ 'result-modal': modalKind === 'result' }"
        role="dialog"
        aria-modal="true"
        :aria-label="
          modalKind === 'issue'
            ? t('giftcardadmin.modalIssueAria')
            : modalKind === 'result'
              ? t('giftcardadmin.modalResultAria')
              : modalKind === 'disable-batch'
                ? t('giftcardadmin.modalDisableAria')
                : t('giftcardadmin.modalCardStatusAria')
        "
      >
        <header>
          <div>
            <span class="kicker">{{ t("giftcardadmin.kicker") }}</span>
            <h2>
              {{
                modalKind === "issue"
                  ? t("giftcardadmin.titleIssue")
                  : modalKind === "result"
                    ? t("giftcardadmin.titleResult")
                    : modalKind === "disable-batch"
                      ? t("giftcardadmin.titleDisable")
                      : t("giftcardadmin.titleCardStatus")
              }}
            </h2>
            <p>
              {{
                modalKind === "result"
                  ? t("giftcardadmin.subtitleResult")
                  : t("giftcardadmin.subtitleAudit")
              }}
            </p>
          </div>
          <button
            type="button"
            :aria-label="t('giftcardadmin.close')"
            :disabled="saving || copying || downloading"
            @click="closeModal"
          >
            <X :size="18" />
          </button>
        </header>

        <form
          v-if="modalKind === 'issue'"
          class="gift-form"
          @submit.prevent="submitIssue"
        >
          <div v-if="formError" class="form-alert error-notice">
            <AlertCircle :size="15" />{{ formError }}
          </div>

          <fieldset>
            <legend>{{ t("giftcardadmin.legendIssueParams") }}</legend>
            <div class="form-grid two-columns">
              <label class="wide-field">
                {{ t("giftcardadmin.batchName") }}
                <input
                  v-model="issueForm.name"
                  maxlength="160"
                  :placeholder="t('giftcardadmin.batchNamePlaceholder')"
                  autofocus
                />
              </label>
              <label>
                {{ t("giftcardadmin.quantity") }}
                <input
                  v-model.number="issueForm.quantity"
                  type="number"
                  min="1"
                  max="500"
                  step="1"
                />
                <small>{{ t("giftcardadmin.quantityHint") }}</small>
              </label>
              <label>
                {{ t("giftcardadmin.cardValue") }}
                <input
                  v-model="issueForm.cardValueMajor"
                  inputmode="decimal"
                  max="1000000"
                  :step="majorInputStep(issueForm.currency)"
                />
                <small>{{ t("giftcardadmin.cardValueHint") }}</small>
              </label>
              <label>
                {{ t("currency.storeCurrency") }}
                <select v-model="issueForm.currency">
                  <option
                    v-for="currency in enabledCurrencies"
                    :key="currency.code"
                    :value="currency.code"
                  >
                    {{ currency.code }} · {{ currency.name }}
                  </option>
                </select>
              </label>
            </div>
            <div class="liability-summary">
              <span><Gift :size="17" /></span>
              <div>
                <small>{{ t("giftcardadmin.liabilityLabel") }}</small>
                <b>{{ formatMoney(issueLiability, issueForm.currency) }}</b>
              </div>
              <strong>{{
                t("giftcardadmin.qtySuffix", { qty: issueForm.quantity || 0 })
              }}</strong>
            </div>
          </fieldset>

          <fieldset>
            <legend>{{ t("giftcardadmin.legendExpiry") }}</legend>
            <label class="switch-row">
              <input v-model="issueForm.hasExpiry" type="checkbox" />
              <span>
                <b>{{ t("giftcardadmin.setExpiry") }}</b>
                <small>{{ t("giftcardadmin.setExpiryHint") }}</small>
              </span>
            </label>
            <label v-if="issueForm.hasExpiry" class="expiry-field">
              {{ t("giftcardadmin.expiryTime") }}
              <input v-model="issueForm.expiresAt" type="datetime-local" />
              <small>{{ t("giftcardadmin.expiryHint") }}</small>
            </label>
          </fieldset>

          <fieldset>
            <legend>{{ t("giftcardadmin.legendSecurity") }}</legend>
            <div class="security-warning">
              <AlertTriangle :size="18" />
              <div>
                <b>{{ t("giftcardadmin.oneTimeWarningTitle") }}</b>
                <span>{{ t("giftcardadmin.oneTimeWarningHint") }}</span>
              </div>
            </div>
            <label>
              {{ t("giftcardadmin.issueReason") }}
              <textarea
                v-model="issueForm.reason"
                maxlength="500"
                :placeholder="t('giftcardadmin.issueReasonPlaceholder')"
              ></textarea>
            </label>
          </fieldset>

          <footer>
            <button type="button" :disabled="saving" @click="closeModal">
              {{ t("giftcardadmin.cancel") }}
            </button>
            <button class="primary-button" type="submit" :disabled="saving">
              <LoaderCircle v-if="saving" class="spinning" :size="14" />
              <Gift v-else :size="14" />{{ t("giftcardadmin.confirmIssue") }}
            </button>
          </footer>
        </form>

        <div
          v-else-if="modalKind === 'result' && issuedResult"
          class="result-content"
        >
          <div class="one-time-warning">
            <AlertTriangle :size="21" />
            <div>
              <b>{{ issuedResult.notice }}</b>
              <span>{{ t("giftcardadmin.resultWarning") }}</span>
            </div>
          </div>

          <section class="result-summary">
            <div>
              <small>{{ t("giftcardadmin.batch") }}</small>
              <b>{{ issuedResult.batch.name }}</b>
              <code>{{ issuedResult.batch.batch_no }}</code>
            </div>
            <div>
              <small>{{ t("giftcardadmin.issuedQty") }}</small>
              <b>{{
                t("giftcardadmin.qtyCards", { qty: issuedResult.cards.length })
              }}</b>
            </div>
            <div>
              <small>{{ t("giftcardadmin.cardValueShort") }}</small>
              <b>{{
                formatMoney(
                  issuedResult.batch.card_value,
                  issuedResult.batch.currency,
                )
              }}</b>
            </div>
            <div>
              <small>{{ t("giftcardadmin.expiry") }}</small>
              <b>{{
                issuedResult.batch.expires_at
                  ? formatTime(issuedResult.batch.expires_at)
                  : t("giftcardadmin.permanent")
              }}</b>
            </div>
          </section>

          <div class="result-actions">
            <button type="button" @click="codesRevealed = !codesRevealed">
              <EyeOff v-if="codesRevealed" :size="14" />
              <Eye v-else :size="14" />
              {{
                codesRevealed
                  ? t("giftcardadmin.maskCodes")
                  : t("giftcardadmin.showCodes")
              }}
            </button>
            <button type="button" :disabled="copying" @click="copyIssuedCodes">
              <LoaderCircle v-if="copying" class="spinning" :size="14" />
              <ClipboardCopy v-else :size="14" />{{
                t("giftcardadmin.copyAll")
              }}
            </button>
            <button
              class="primary-button"
              type="button"
              :disabled="downloading"
              @click="downloadIssuedCodes"
            >
              <LoaderCircle v-if="downloading" class="spinning" :size="14" />
              <Download v-else :size="14" />{{ t("giftcardadmin.downloadCsv") }}
            </button>
          </div>

          <div
            v-if="resultActionMessage"
            class="result-message"
            :class="{ warning: !resultAcknowledged }"
          >
            <ShieldCheck :size="15" />{{ resultActionMessage }}
          </div>

          <div class="issued-table-wrap">
            <table class="issued-table">
              <thead>
                <tr>
                  <th>{{ t("giftcardadmin.colIndex") }}</th>
                  <th>{{ t("giftcardadmin.colFullCode") }}</th>
                  <th>{{ t("giftcardadmin.colPreview") }}</th>
                  <th>{{ t("giftcardadmin.colCardId") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(item, index) in issuedResult.cards" :key="item.id">
                  <td>{{ index + 1 }}</td>
                  <td>
                    <code :class="{ secret: !codesRevealed }">
                      {{
                        codesRevealed ? item.code : maskIssuedCode(item.code)
                      }}
                    </code>
                  </td>
                  <td>
                    <code>{{ item.code_preview }}</code>
                  </td>
                  <td>
                    <code :title="item.id">{{ shortID(item.id) }}</code>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <footer class="result-footer">
            <label>
              <input v-model="resultAcknowledged" type="checkbox" />
              <span>
                <b>{{ t("giftcardadmin.ackText") }}</b>
                <small>{{ t("giftcardadmin.ackHint") }}</small>
              </span>
            </label>
            <button
              class="primary-button"
              type="button"
              :disabled="!resultAcknowledged"
              @click="closeModal"
            >
              <Check :size="14" />{{ t("giftcardadmin.confirmHandoff") }}
            </button>
          </footer>
        </div>

        <form
          v-else-if="modalKind === 'disable-batch' && editingBatch"
          class="gift-form"
          @submit.prevent="submitDisableBatch"
        >
          <div v-if="formError" class="form-alert error-notice">
            <AlertCircle :size="15" />{{ formError }}
          </div>
          <section class="identity-card danger-card">
            <span><ArchiveX :size="18" /></span>
            <div>
              <b>{{ editingBatch.name }}</b>
              <code>{{ editingBatch.batch_no }} · {{ editingBatch.id }}</code>
            </div>
          </section>
          <fieldset>
            <legend>{{ t("giftcardadmin.legendIrreversible") }}</legend>
            <div class="security-warning danger-warning">
              <AlertTriangle :size="18" />
              <div>
                <b>{{ t("giftcardadmin.disableWarningTitle") }}</b>
                <span>{{ t("giftcardadmin.disableWarningHint") }}</span>
              </div>
            </div>
            <label>
              {{ t("giftcardadmin.disableReason") }}
              <textarea
                v-model="changeReason"
                maxlength="500"
                :placeholder="t('giftcardadmin.disableReasonPlaceholder')"
                autofocus
              ></textarea>
            </label>
          </fieldset>
          <footer>
            <button type="button" :disabled="saving" @click="closeModal">
              {{ t("giftcardadmin.cancel") }}
            </button>
            <button class="danger-button" type="submit" :disabled="saving">
              <LoaderCircle v-if="saving" class="spinning" :size="14" />
              <ArchiveX v-else :size="14" />{{
                t("giftcardadmin.confirmDisable")
              }}
            </button>
          </footer>
        </form>

        <form
          v-else-if="modalKind === 'card-status' && editingCard"
          class="gift-form"
          @submit.prevent="submitCardStatus"
        >
          <div v-if="formError" class="form-alert error-notice">
            <AlertCircle :size="15" />{{ formError }}
          </div>
          <section class="identity-card">
            <span><Gift :size="18" /></span>
            <div>
              <b>{{ editingCard.code_preview }}</b>
              <code>{{ editingCard.id }}</code>
            </div>
            <span class="status-badge" :class="`status-${editingCard.status}`">
              {{ cardStatusLabel(editingCard.status) }}
            </span>
          </section>
          <fieldset>
            <legend>{{ t("giftcardadmin.legendStatusChange") }}</legend>
            <div class="status-transition">
              <span>{{ cardStatusLabel(editingCard.status) }}</span>
              <strong>→</strong>
              <span :class="`status-${targetCardStatus}`">
                {{ cardStatusLabel(targetCardStatus) }}
              </span>
            </div>
            <p class="form-hint">
              {{ t("giftcardadmin.statusChangeHint") }}
            </p>
            <label>
              {{ t("giftcardadmin.changeReason") }}
              <textarea
                v-model="changeReason"
                maxlength="500"
                :placeholder="t('giftcardadmin.changeReasonPlaceholder')"
                autofocus
              ></textarea>
            </label>
          </fieldset>
          <footer>
            <button type="button" :disabled="saving" @click="closeModal">
              {{ t("giftcardadmin.cancel") }}
            </button>
            <button class="primary-button" type="submit" :disabled="saving">
              <LoaderCircle v-if="saving" class="spinning" :size="14" />
              <ShieldCheck v-else :size="14" />
              {{
                targetCardStatus === "active"
                  ? t("giftcardadmin.confirmEnable")
                  : t("giftcardadmin.confirmDisableCard")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>
  </section>
</template>

<style scoped>
.gift-shell {
  display: grid;
  gap: 12px;
}

.gift-nav {
  min-height: 58px;
  padding: 0 12px 0 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  overflow: hidden;
}

.gift-tabs {
  min-width: 0;
  align-self: stretch;
  display: flex;
  align-items: end;
  gap: 4px;
}

.gift-tabs button {
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

.gift-tabs button.active {
  border-bottom-color: var(--text);
  color: var(--text);
}

.gift-tabs button span {
  padding: 2px 5px;
  border-radius: 8px;
  background: var(--soft);
  font-size: 7px;
}

.gift-panel {
  min-width: 0;
  overflow: hidden;
}

.gift-toolbar {
  min-height: 58px;
  padding: 10px 13px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.gift-search {
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

.gift-search input {
  min-width: 0;
  flex: 1;
  border: 0;
  outline: none;
  background: transparent;
  font-size: 9px;
}

.gift-search button,
.gift-filters button,
.gift-filters select {
  height: 28px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}

.gift-search button {
  padding: 0 9px;
  border-top: 0;
  border-right: 0;
  border-bottom: 0;
  border-radius: 0;
  display: flex;
  align-items: center;
  gap: 4px;
}

.gift-filters {
  display: flex;
  align-items: center;
  gap: 6px;
}

.gift-filters select {
  max-width: 210px;
  min-width: 118px;
  padding: 0 8px;
}

.gift-filters button {
  padding: 0 9px;
  display: flex;
  align-items: center;
  gap: 5px;
}

.gift-filters button:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.gift-notice,
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

.gift-notice button {
  margin-left: auto;
  border: 0;
  background: transparent;
  color: inherit;
  font-size: 8px;
  font-weight: 700;
}

.gift-state {
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

.gift-state strong {
  color: var(--text);
  font-size: 11px;
}

.gift-table-wrap {
  width: 100%;
  min-height: 390px;
  overflow-x: auto;
}

.gift-table {
  width: 100%;
  min-width: 1050px;
  border-collapse: collapse;
}

.gift-table th,
.gift-table td {
  padding: 13px 14px;
  border-bottom: 1px solid var(--line);
  text-align: left;
  vertical-align: middle;
}

.gift-table th {
  background: var(--surface-2);
  color: var(--muted);
  font-size: 7px;
  font-weight: 600;
  letter-spacing: 0.04em;
}

.gift-table td {
  font-size: 8px;
}

.gift-table td > b,
.gift-table td > time,
.gift-table td > small {
  display: block;
}

.gift-table td > b,
.gift-table td > time {
  font-size: 8px;
  font-weight: 600;
}

.gift-table td > small,
.record-primary code {
  margin-top: 4px;
  color: var(--muted);
  font-size: 7px;
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

.record-primary code,
.issued-table code,
.identity-card code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
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

.status-active {
  background: color-mix(in srgb, var(--success) 11%, transparent);
  color: var(--success);
}

.status-disabled {
  opacity: 0.72;
}

.status-redeemed {
  background: color-mix(in srgb, var(--success) 7%, transparent);
  color: var(--success);
}

.status-expired {
  background: color-mix(in srgb, var(--warn) 10%, transparent);
  color: var(--warn);
}

.balance-track {
  width: 100px;
  height: 3px;
  margin-top: 6px;
  border-radius: 3px;
  overflow: hidden;
  background: var(--soft);
}

.balance-track i {
  height: 100%;
  display: block;
  border-radius: inherit;
  background: var(--text);
}

.record-actions {
  text-align: right !important;
  white-space: nowrap;
}

.record-actions button {
  height: 29px;
  margin-left: 4px;
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

.record-actions .danger-action {
  color: var(--danger);
}

.no-action {
  display: inline-block;
  max-width: 145px;
  color: var(--muted);
  font-size: 7px;
  white-space: normal;
}

.gift-pagination {
  min-height: 53px;
  padding: 9px 13px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: var(--muted);
  font-size: 8px;
}

.gift-pagination > div {
  display: flex;
  gap: 4px;
}

.gift-pagination button,
.gift-pagination select {
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

.gift-pagination button.active {
  background: var(--dark);
  color: var(--dark-text);
}

.gift-pagination button:disabled {
  cursor: not-allowed;
  opacity: 0.4;
}

.gift-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 120;
  padding: 24px;
  display: flex;
  justify-content: flex-end;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(2px);
}

.gift-modal {
  width: min(660px, 100%);
  height: 100%;
  border: 1px solid var(--line);
  border-radius: 10px;
  overflow-y: auto;
  background: var(--surface);
  color: var(--text);
  box-shadow: var(--shadow);
}

.gift-modal.result-modal {
  width: min(900px, 100%);
}

.gift-modal > header {
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

.gift-modal h2 {
  margin: 5px 0 3px;
  font-size: 17px;
  letter-spacing: -0.03em;
}

.gift-modal header p {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
}

.gift-modal > header > button {
  width: 31px;
  height: 31px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: grid;
  place-items: center;
  background: var(--surface);
}

.gift-form {
  padding: 5px 20px 20px;
}

.gift-form fieldset {
  margin: 0;
  padding: 18px 0;
  border: 0;
  border-bottom: 1px solid var(--line);
}

.gift-form legend {
  margin-bottom: 13px;
  padding: 0;
  font-size: 10px;
  font-weight: 700;
}

.gift-form label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: var(--muted);
  font-size: 8px;
  font-weight: 600;
}

.gift-form input:not([type="checkbox"]),
.gift-form select,
.gift-form textarea {
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

.gift-form input:focus,
.gift-form select:focus,
.gift-form textarea:focus {
  border-color: var(--text);
}

.gift-form textarea {
  min-height: 88px;
  resize: vertical;
  line-height: 1.55;
}

.gift-form small {
  color: var(--muted);
  font-size: 7px;
  font-weight: 400;
}

.form-grid {
  display: grid;
  gap: 12px;
}

.two-columns {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.wide-field {
  grid-column: 1 / -1;
}

.liability-summary,
.identity-card {
  margin-top: 14px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  background: var(--surface-2);
}

.liability-summary > span,
.identity-card > span:first-child {
  width: 34px;
  height: 34px;
  border-radius: 7px;
  display: grid;
  place-items: center;
  background: var(--soft);
}

.liability-summary div,
.identity-card div {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.liability-summary b {
  font-size: 14px;
}

.liability-summary strong {
  font-size: 9px;
}

.identity-card b {
  font-size: 9px;
}

.identity-card code {
  overflow: hidden;
  color: var(--muted);
  font-size: 7px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.danger-card {
  border-color: color-mix(in srgb, var(--danger) 30%, var(--line));
}

.switch-row {
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 6px;
  flex-direction: row !important;
  align-items: center;
  gap: 9px !important;
  background: var(--surface-2);
}

.switch-row input,
.result-footer input {
  width: 16px;
  height: 16px;
  accent-color: var(--text);
}

.switch-row span,
.result-footer label span {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.switch-row b,
.result-footer label b {
  color: var(--text);
  font-size: 8px;
}

.expiry-field {
  margin-top: 12px;
}

.security-warning,
.one-time-warning {
  margin-bottom: 13px;
  padding: 12px;
  border: 1px solid color-mix(in srgb, var(--warn) 25%, var(--line));
  border-radius: 7px;
  display: flex;
  align-items: flex-start;
  gap: 10px;
  background: color-mix(in srgb, var(--warn) 8%, var(--surface));
  color: var(--warn);
}

.security-warning div,
.one-time-warning div {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.security-warning b,
.one-time-warning b {
  color: var(--text);
  font-size: 9px;
}

.security-warning span,
.one-time-warning span {
  color: var(--muted);
  font-size: 8px;
  line-height: 1.55;
}

.danger-warning {
  border-color: color-mix(in srgb, var(--danger) 28%, var(--line));
  background: color-mix(in srgb, var(--danger) 8%, var(--surface));
  color: var(--danger);
}

.form-hint {
  margin: 0 0 13px;
  padding: 8px 9px;
  border-left: 2px solid var(--warn);
  background: color-mix(in srgb, var(--warn) 7%, transparent);
  color: var(--muted);
  font-size: 7px;
  line-height: 1.55;
}

.status-transition {
  margin-bottom: 12px;
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  gap: 10px;
}

.status-transition span {
  padding: 10px;
  border-radius: 6px;
  background: var(--soft);
  text-align: center;
  font-size: 9px;
  font-weight: 700;
}

.gift-form > footer {
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

.gift-form > footer > button:first-child,
.danger-button {
  height: 36px;
  padding: 0 14px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: var(--surface);
  font-size: 8px;
}

.danger-button {
  border-color: color-mix(in srgb, var(--danger) 35%, var(--line));
  background: var(--danger);
  color: white;
}

.gift-form > footer .primary-button {
  height: 36px;
  padding: 0 14px;
  font-size: 8px;
}

.result-content {
  padding: 18px 20px 20px;
}

.one-time-warning {
  margin-bottom: 14px;
}

.result-summary {
  margin-bottom: 13px;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: grid;
  grid-template-columns: 1.5fr repeat(3, 1fr);
  gap: 10px;
  background: var(--surface-2);
}

.result-summary div {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.result-summary small {
  color: var(--muted);
  font-size: 7px;
}

.result-summary b {
  overflow: hidden;
  font-size: 9px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.result-summary code {
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

.result-actions {
  margin-bottom: 10px;
  display: flex;
  justify-content: flex-end;
  gap: 6px;
}

.result-actions button {
  height: 32px;
  padding: 0 10px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}

.result-actions .primary-button {
  border: 0;
  background: var(--dark);
  color: var(--dark-text);
}

.result-message {
  margin-bottom: 10px;
  padding: 8px 9px;
  border-radius: 5px;
  display: flex;
  align-items: flex-start;
  gap: 7px;
  background: color-mix(in srgb, var(--success) 8%, transparent);
  color: var(--success);
  font-size: 8px;
  line-height: 1.5;
}

.result-message.warning {
  background: color-mix(in srgb, var(--warn) 8%, transparent);
  color: var(--warn);
}

.issued-table-wrap {
  max-height: 390px;
  border: 1px solid var(--line);
  border-radius: 7px;
  overflow: auto;
}

.issued-table {
  width: 100%;
  min-width: 730px;
  border-collapse: collapse;
}

.issued-table th,
.issued-table td {
  padding: 9px 10px;
  border-bottom: 1px solid var(--line);
  text-align: left;
  font-size: 8px;
}

.issued-table th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--surface-2);
  color: var(--muted);
  font-size: 7px;
}

.issued-table code {
  white-space: nowrap;
}

.issued-table code.secret {
  color: var(--muted);
  letter-spacing: 0.03em;
}

.result-footer {
  position: sticky;
  bottom: -20px;
  z-index: 3;
  margin: 14px -20px -20px;
  padding: 13px 20px;
  border-top: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  backdrop-filter: blur(12px);
}

.result-footer label {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 9px;
  color: var(--muted);
  font-size: 8px;
}

.result-footer .primary-button {
  height: 36px;
  flex: 0 0 auto;
  padding: 0 13px;
  font-size: 8px;
}

.spinning {
  animation: gift-spin 0.8s linear infinite;
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

@keyframes gift-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 850px) {
  .gift-toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .gift-search {
    width: 100%;
  }

  .gift-filters {
    justify-content: flex-end;
  }

  .result-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 660px) {
  .gift-nav {
    padding-left: 9px;
  }

  .gift-tabs button {
    padding: 0 8px;
  }

  .gift-nav > .primary-button {
    padding: 0 9px;
    white-space: nowrap;
  }

  .gift-filters {
    flex-wrap: wrap;
  }

  .gift-filters select {
    min-width: 0;
    flex: 1;
  }

  .gift-table-wrap {
    padding: 9px;
  }

  .gift-table {
    min-width: 0;
  }

  .gift-table thead {
    display: none;
  }

  .gift-table tbody,
  .gift-table tr,
  .gift-table td {
    display: block;
    width: 100%;
  }

  .gift-table tr {
    margin-bottom: 9px;
    padding: 7px 10px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--surface);
  }

  .gift-table td {
    min-height: 35px;
    padding: 8px 0 8px 98px;
    border-bottom: 1px solid var(--line);
    position: relative;
  }

  .gift-table td::before {
    content: attr(data-label);
    position: absolute;
    left: 0;
    top: 10px;
    color: var(--muted);
    font-size: 7px;
  }

  .gift-table td:last-child {
    border-bottom: 0;
  }

  .record-primary {
    min-width: 0;
  }

  .record-actions {
    text-align: left !important;
    white-space: normal;
  }

  .record-actions button {
    margin: 2px 3px 2px 0;
  }

  .gift-pagination {
    align-items: flex-start;
    flex-direction: column;
  }

  .gift-pagination > div {
    width: 100%;
    overflow-x: auto;
  }

  .gift-modal-backdrop {
    padding: 0;
  }

  .gift-modal {
    border-radius: 0;
  }

  .gift-modal > header,
  .gift-form,
  .result-content {
    padding-right: 14px;
    padding-left: 14px;
  }

  .two-columns,
  .result-summary {
    grid-template-columns: 1fr;
  }

  .identity-card {
    grid-template-columns: auto minmax(0, 1fr);
  }

  .identity-card > .status-badge {
    grid-column: 2;
  }

  .gift-form > footer,
  .result-footer {
    margin-right: -14px;
    margin-left: -14px;
    padding-right: 14px;
    padding-left: 14px;
  }

  .result-actions {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .result-actions .primary-button {
    grid-column: 1 / -1;
  }

  .result-footer {
    align-items: stretch;
    flex-direction: column;
  }

  .result-footer .primary-button {
    width: 100%;
  }
}
</style>
