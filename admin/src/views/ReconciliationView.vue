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
import {
  AlertTriangle,
  BadgeCheck,
  ChevronLeft,
  ChevronRight,
  CopyCheck,
  Download,
  FileSearch,
  FileSpreadsheet,
  GitCompareArrows,
  LoaderCircle,
  RefreshCw,
  Scale,
  ShieldCheck,
  Upload,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import {
  currencyDirectory,
  formatMoney as formatMinorMoney,
  formatSignedMoney,
  storeCurrency,
} from "../utils/money";

const { t, locale } = useI18n();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("payment.manage"));

type BatchStatus =
  "pending" | "processing" | "completed" | "differences_found" | "failed";
type ItemStatus =
  | "matched"
  | "amount_mismatch"
  | "missing_system"
  | "missing_provider"
  | "resolved";
type ResolutionCode =
  | "accepted_provider"
  | "accepted_system"
  | "refund_created"
  | "adjusted"
  | "provider_dispute";

interface PaymentChannel {
  id: string;
  name: string;
  code: string;
  provider: string;
  fee_rate: number;
  enabled: boolean;
}

interface ReconciliationBatch {
  id: string;
  batch_no: string;
  channel_id: string;
  period_from: string;
  period_to: string;
  source_file: string;
  statement_hash: string;
  imported_by: string;
  status: BatchStatus | string;
  currency: string;
  total: number;
  matched: number;
  mismatched: number;
  resolved: number;
  completed_at?: string | null;
  created_at: string;
  updated_at: string;
}

interface ReconciliationItem {
  id: string;
  batch_id: string;
  order_id?: string | null;
  direction: "payment" | "refund" | string;
  provider_trade_no: string;
  provider_occurred_at?: string | null;
  system_amount: number;
  provider_amount: number;
  difference: number;
  currency: string;
  status: ItemStatus | string;
  resolution_code: string;
  resolution: string;
  resolved_by?: string | null;
  resolved_at?: string | null;
  created_at: string;
  updated_at: string;
}

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface DetailPayload extends PagePayload<ReconciliationItem> {
  batch: ReconciliationBatch;
}

interface ImportResult {
  batch: ReconciliationBatch;
  queued: boolean;
  notice?: string;
}

interface ResolutionOption {
  value: ResolutionCode;
  label: string;
  hint: string;
}

const batchStatuses: Array<{ value: "" | BatchStatus; label: string }> = [
  { value: "", label: "reconciliation.batchStatusOptions.all" },
  { value: "pending", label: "reconciliation.batchStatusOptions.pending" },
  {
    value: "processing",
    label: "reconciliation.batchStatusOptions.processing",
  },
  {
    value: "differences_found",
    label: "reconciliation.batchStatusOptions.differences_found",
  },
  { value: "completed", label: "reconciliation.batchStatusOptions.completed" },
  { value: "failed", label: "reconciliation.batchStatusOptions.failed" },
];
const itemStatuses: Array<{ value: "" | ItemStatus; label: string }> = [
  { value: "", label: "reconciliation.itemStatusOptions.all" },
  {
    value: "amount_mismatch",
    label: "reconciliation.itemStatusOptions.amount_mismatch",
  },
  {
    value: "missing_system",
    label: "reconciliation.itemStatusOptions.missing_system",
  },
  {
    value: "missing_provider",
    label: "reconciliation.itemStatusOptions.missing_provider",
  },
  { value: "resolved", label: "reconciliation.itemStatusOptions.resolved" },
  { value: "matched", label: "reconciliation.itemStatusOptions.matched" },
];
const resolutionOptions: ResolutionOption[] = [
  {
    value: "accepted_provider",
    label: "reconciliation.resolution.accepted_provider.label",
    hint: "reconciliation.resolution.accepted_provider.hint",
  },
  {
    value: "accepted_system",
    label: "reconciliation.resolution.accepted_system.label",
    hint: "reconciliation.resolution.accepted_system.hint",
  },
  {
    value: "refund_created",
    label: "reconciliation.resolution.refund_created.label",
    hint: "reconciliation.resolution.refund_created.hint",
  },
  {
    value: "adjusted",
    label: "reconciliation.resolution.adjusted.label",
    hint: "reconciliation.resolution.adjusted.hint",
  },
  {
    value: "provider_dispute",
    label: "reconciliation.resolution.provider_dispute.label",
    hint: "reconciliation.resolution.provider_dispute.hint",
  },
];

const channels = ref<PaymentChannel[]>([]);
const channelsLoading = ref(false);
const channelsError = ref("");

const batches = ref<ReconciliationBatch[]>([]);
const batchTotal = ref(0);
const batchPage = ref(1);
const batchPageSize = ref(20);
const batchStatusFilter = ref<"" | BatchStatus>("");
const batchLoading = ref(false);
const batchError = ref("");
const notice = ref("");
let batchRequest = 0;

const selectedBatchID = ref("");
const selectedBatch = ref<ReconciliationBatch | null>(null);
const detailItems = ref<ReconciliationItem[]>([]);
const detailTotal = ref(0);
const detailPage = ref(1);
const detailPageSize = ref(20);
const detailStatusFilter = ref<"" | ItemStatus>("");
const detailLoading = ref(false);
const detailError = ref("");
let detailRequest = 0;

const importOpen = ref(false);
const importBusy = ref(false);
const importError = ref("");
const statementFile = ref<File | null>(null);
const fileInput = ref<HTMLInputElement | null>(null);
const importForm = reactive({
  channelID: "",
  periodFrom: "",
  periodTo: "",
  currency: storeCurrency.value,
  reason: "",
});

const resolutionOpen = ref(false);
const resolvingItem = ref<ReconciliationItem | null>(null);
const resolutionBusy = ref(false);
const resolutionError = ref("");
const resolutionForm = reactive<{
  code: "" | ResolutionCode;
  evidence: string;
  reason: string;
}>({ code: "", evidence: "", reason: "" });

const channelLookup = computed(
  () => new Map(channels.value.map((channel) => [channel.id, channel])),
);
const enabledCurrencies = computed(() =>
  Object.values(currencyDirectory.value).filter(
    (item) => item.enabled !== false,
  ),
);
const batchPageCount = computed(() =>
  Math.max(1, Math.ceil(batchTotal.value / batchPageSize.value)),
);
const detailPageCount = computed(() =>
  Math.max(1, Math.ceil(detailTotal.value / detailPageSize.value)),
);
const batchPageNumbers = computed(() =>
  pageNumbers(batchPage.value, batchPageCount.value),
);
const detailPageNumbers = computed(() =>
  pageNumbers(detailPage.value, detailPageCount.value),
);
const pageMatched = computed(() =>
  batches.value.reduce((sum, batch) => sum + number(batch.matched), 0),
);
const pageDifferences = computed(() =>
  batches.value.reduce((sum, batch) => sum + number(batch.mismatched), 0),
);
const pageResolved = computed(() =>
  batches.value.reduce((sum, batch) => sum + number(batch.resolved), 0),
);
const selectedResolution = computed(() =>
  resolutionOptions.find((option) => option.value === resolutionForm.code),
);
const modalOpen = computed(() => importOpen.value || resolutionOpen.value);

function apiMessage(reason: unknown, fallback: string) {
  const error = reason as { response?: { data?: { message?: string } } };
  return error.response?.data?.message || fallback;
}

function number(value: unknown) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function normalizePage<T>(
  payload: unknown,
  fallbackPage: number,
  fallbackPageSize: number,
): PagePayload<T> {
  const source = (payload || {}) as Partial<PagePayload<T>>;
  const page = number(source.page);
  const pageSize = number(source.page_size);
  return {
    items: Array.isArray(source.items) ? source.items : [],
    total: Math.max(0, number(source.total)),
    page: Number.isInteger(page) && page > 0 ? page : fallbackPage,
    page_size:
      Number.isInteger(pageSize) && pageSize > 0 ? pageSize : fallbackPageSize,
  };
}

function pageNumbers(current: number, pages: number) {
  const start = Math.max(1, Math.min(current - 2, pages - 4));
  const end = Math.min(pages, start + 4);
  return Array.from({ length: end - start + 1 }, (_, index) => start + index);
}

function formatMoney(value: unknown, currency?: string) {
  return formatMinorMoney(
    String(value ?? 0),
    currency || selectedBatch.value?.currency || storeCurrency.value,
    locale.value,
  );
}

function formatDifference(value: unknown, currency?: string) {
  return formatSignedMoney(
    String(value ?? 0),
    currency || selectedBatch.value?.currency || storeCurrency.value,
    locale.value,
  );
}

function formatTime(value?: string | null) {
  if (!value) return "—";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return "—";
  return parsed.toLocaleString(locale.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

function formatPeriod(batch: ReconciliationBatch) {
  return `${formatTime(batch.period_from)} — ${formatTime(batch.period_to)}`;
}

function statusClass(status: string) {
  const allowed = [
    "pending",
    "processing",
    "completed",
    "differences_found",
    "failed",
    "matched",
    "amount_mismatch",
    "missing_system",
    "missing_provider",
    "resolved",
  ];
  return allowed.includes(status) ? `status-${status}` : "status-unknown";
}

function batchStatus(status: string) {
  const key = `reconciliation.batchStatus.${status}`;
  return t(key) === key ? status || t("reconciliation.unknown") : t(key);
}

function itemStatus(status: string) {
  const key = `reconciliation.itemStatus.${status}`;
  return t(key) === key ? status || t("reconciliation.unknown") : t(key);
}

function directionLabel(direction: string) {
  const key = `reconciliation.direction.${direction}`;
  return t(key) === key ? direction || t("reconciliation.unknown") : t(key);
}

function resolutionLabel(code: string) {
  const option = resolutionOptions.find((item) => item.value === code);
  return option ? t(option.label) : code || "—";
}

function channelName(id: string) {
  const channel = channelLookup.value.get(id);
  return channel
    ? `${channel.name} · ${channel.code}`
    : t("reconciliation.channel", { id: id.slice(0, 8) });
}

function progress(batch: ReconciliationBatch) {
  const total = Math.max(0, number(batch.total));
  if (!total) return 0;
  return Math.min(
    100,
    Math.round(
      ((number(batch.matched) + number(batch.resolved)) / total) * 100,
    ),
  );
}

function validReason(value: string) {
  const length = Array.from(value.trim()).length;
  return length >= 4 && length <= 500;
}

function localDateTimeInput(date: Date) {
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function initializePeriod() {
  const to = new Date();
  to.setSeconds(0, 0);
  const from = new Date(to.getTime() - 24 * 60 * 60 * 1000);
  importForm.periodFrom = localDateTimeInput(from);
  importForm.periodTo = localDateTimeInput(to);
}

async function loadChannels() {
  channelsLoading.value = true;
  channelsError.value = "";
  try {
    const { data } = await adminApi.get("/payments");
    const payload = data.data;
    channels.value = Array.isArray(payload)
      ? payload
      : Array.isArray(payload?.items)
        ? payload.items
        : [];
    if (
      !channels.value.some((channel) => channel.id === importForm.channelID)
    ) {
      importForm.channelID =
        channels.value.find((channel) => channel.enabled)?.id ||
        channels.value[0]?.id ||
        "";
    }
  } catch (reason: unknown) {
    channels.value = [];
    channelsError.value = apiMessage(reason, t("reconciliation.errChannels"));
  } finally {
    channelsLoading.value = false;
  }
}

async function loadBatches(targetPage = batchPage.value) {
  const request = ++batchRequest;
  batchLoading.value = true;
  batchError.value = "";
  try {
    const { data } = await adminApi.get("/operations/reconciliations", {
      params: {
        page: targetPage,
        page_size: batchPageSize.value,
        ...(batchStatusFilter.value ? { status: batchStatusFilter.value } : {}),
      },
    });
    if (request !== batchRequest) return;
    const result = normalizePage<ReconciliationBatch>(
      data.data,
      targetPage,
      batchPageSize.value,
    );
    batches.value = result.items;
    batchTotal.value = result.total;
    batchPage.value = result.page;
    batchPageSize.value = result.page_size;
  } catch (reason: unknown) {
    if (request !== batchRequest) return;
    batchError.value = apiMessage(reason, t("reconciliation.errBatches"));
  } finally {
    if (request === batchRequest) batchLoading.value = false;
  }
}

async function loadDetail(
  id = selectedBatchID.value,
  targetPage = detailPage.value,
) {
  if (!id) return;
  const request = ++detailRequest;
  detailLoading.value = true;
  detailError.value = "";
  try {
    const { data } = await adminApi.get(
      `/reconciliations/${encodeURIComponent(id)}`,
      {
        params: {
          page: targetPage,
          page_size: detailPageSize.value,
          ...(detailStatusFilter.value
            ? { status: detailStatusFilter.value }
            : {}),
        },
      },
    );
    if (request !== detailRequest) return;
    const payload = (data.data || {}) as Partial<DetailPayload>;
    const page = normalizePage<ReconciliationItem>(
      payload,
      targetPage,
      detailPageSize.value,
    );
    selectedBatch.value = payload.batch || null;
    detailItems.value = page.items;
    detailTotal.value = page.total;
    detailPage.value = page.page;
    detailPageSize.value = page.page_size;
  } catch (reason: unknown) {
    if (request !== detailRequest) return;
    detailError.value = apiMessage(reason, t("reconciliation.errDetail"));
  } finally {
    if (request === detailRequest) detailLoading.value = false;
  }
}

async function openBatch(batch: ReconciliationBatch | string) {
  selectedBatchID.value = typeof batch === "string" ? batch : batch.id;
  selectedBatch.value = typeof batch === "string" ? null : batch;
  detailPage.value = 1;
  detailStatusFilter.value = "";
  detailItems.value = [];
  detailError.value = "";
  await loadDetail(selectedBatchID.value, 1);
}

function closeDetail() {
  detailRequest += 1;
  selectedBatchID.value = "";
  selectedBatch.value = null;
  detailItems.value = [];
  detailTotal.value = 0;
  detailError.value = "";
}

async function refreshAll() {
  notice.value = "";
  await Promise.all([
    loadBatches(batchPage.value),
    selectedBatchID.value
      ? loadDetail(selectedBatchID.value, detailPage.value)
      : Promise.resolve(),
    channels.value.length ? Promise.resolve() : loadChannels(),
  ]);
}

function openImport() {
  if (!canManage.value) return;
  importError.value = "";
  if (!importForm.periodFrom || !importForm.periodTo) initializePeriod();
  importOpen.value = true;
}

function closeImport() {
  if (importBusy.value) return;
  importOpen.value = false;
  importError.value = "";
  importForm.reason = "";
  clearFile();
}

function handleFile(event: Event) {
  if (!canManage.value) return;
  importError.value = "";
  const target = event.target as HTMLInputElement;
  statementFile.value = target.files?.[0] || null;
}

function clearFile() {
  statementFile.value = null;
  if (fileInput.value) fileInput.value.value = "";
}

function validateImport() {
  if (!channels.value.some((channel) => channel.id === importForm.channelID))
    return t("reconciliation.errChannel");
  const from = new Date(importForm.periodFrom);
  const to = new Date(importForm.periodTo);
  if (
    Number.isNaN(from.getTime()) ||
    Number.isNaN(to.getTime()) ||
    to <= from ||
    to.getTime() - from.getTime() > 31 * 24 * 60 * 60 * 1000 ||
    to.getTime() > Date.now() + 5 * 60 * 1000
  ) {
    return t("reconciliation.errPeriod");
  }
  const file = statementFile.value;
  if (
    !file ||
    file.size < 1 ||
    file.size > 8 * 1024 * 1024 ||
    !file.name.toLowerCase().endsWith(".csv")
  ) {
    return t("reconciliation.errCsv");
  }
  if (!validReason(importForm.reason)) return t("reconciliation.errReason");
  return "";
}

async function submitImport() {
  if (!canManage.value) return;
  if (importBusy.value) return;
  const validation = validateImport();
  if (validation) {
    importError.value = validation;
    return;
  }
  const file = statementFile.value!;
  const from = new Date(importForm.periodFrom);
  const to = new Date(importForm.periodTo);
  const body = new FormData();
  body.append("channel_id", importForm.channelID);
  body.append("period_from", from.toISOString());
  body.append("period_to", to.toISOString());
  body.append("currency", importForm.currency || storeCurrency.value);
  body.append("file", file, file.name);
  importBusy.value = true;
  importError.value = "";
  notice.value = "";
  try {
    const { data } = await adminApi.post("/reconciliations/import", body, {
      headers: { "X-Change-Reason": importForm.reason.trim() },
      timeout: 60_000,
    });
    const result = data.data as ImportResult;
    if (!result?.batch?.id) throw new Error("missing reconciliation batch");
    importOpen.value = false;
    importForm.reason = "";
    clearFile();
    batchStatusFilter.value = "";
    await loadBatches(1);
    await openBatch(result.batch.id);
    notice.value = t("reconciliation.imported", {
      batchNo: result.batch.batch_no,
      status: result.queued
        ? t("reconciliation.importQueued")
        : t("reconciliation.importUnconfirmed"),
      extra: result.notice || "",
    }).trim();
  } catch (reason: unknown) {
    importError.value = apiMessage(reason, t("reconciliation.errImport"));
  } finally {
    importBusy.value = false;
  }
}

function downloadTemplate() {
  const content =
    "\ufeffprovider_trade_no,amount_minor,occurred_at,direction,status,currency\r\n";
  const blob = new Blob([content], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = "linlinqi-reconciliation-template.csv";
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

function openResolution(item: ReconciliationItem) {
  if (!canManage.value) return;
  if (item.status === "matched" || item.status === "resolved") return;
  resolvingItem.value = item;
  resolutionForm.code = "";
  resolutionForm.evidence = "";
  resolutionForm.reason = "";
  resolutionError.value = "";
  resolutionOpen.value = true;
}

function closeResolution() {
  if (resolutionBusy.value) return;
  resolutionOpen.value = false;
  resolvingItem.value = null;
  resolutionError.value = "";
  resolutionForm.code = "";
  resolutionForm.evidence = "";
  resolutionForm.reason = "";
}

function validateResolution() {
  if (!resolvingItem.value) return t("reconciliation.errContext");
  if (
    !resolutionOptions.some((option) => option.value === resolutionForm.code)
  ) {
    return t("reconciliation.errResolution");
  }
  const evidenceLength = Array.from(resolutionForm.evidence.trim()).length;
  if (evidenceLength < 4 || evidenceLength > 2000)
    return t("reconciliation.errEvidence");
  if (!validReason(resolutionForm.reason))
    return t("reconciliation.errAuditReason");
  return "";
}

async function submitResolution() {
  if (!canManage.value) return;
  if (resolutionBusy.value) return;
  const validation = validateResolution();
  if (validation) {
    resolutionError.value = validation;
    return;
  }
  const item = resolvingItem.value!;
  resolutionBusy.value = true;
  resolutionError.value = "";
  notice.value = "";
  try {
    await adminApi.patch(
      `/reconciliation-items/${encodeURIComponent(item.id)}`,
      {
        resolution_code: resolutionForm.code,
        resolution: resolutionForm.evidence.trim(),
      },
      { headers: { "X-Change-Reason": resolutionForm.reason.trim() } },
    );
    resolutionOpen.value = false;
    resolvingItem.value = null;
    await Promise.all([
      loadDetail(selectedBatchID.value, detailPage.value),
      loadBatches(batchPage.value),
    ]);
    notice.value = t("reconciliation.resolvedNotice", {
      tradeNo: item.provider_trade_no,
    });
  } catch (reason: unknown) {
    resolutionError.value = apiMessage(reason, t("reconciliation.errResolve"));
  } finally {
    resolutionBusy.value = false;
  }
}

function handleEscape(event: KeyboardEvent) {
  if (event.key !== "Escape") return;
  if (resolutionOpen.value) closeResolution();
  else if (importOpen.value) closeImport();
}

watch(batchStatusFilter, () => {
  notice.value = "";
  void loadBatches(1);
});
watch(detailStatusFilter, () => {
  if (selectedBatchID.value) void loadDetail(selectedBatchID.value, 1);
});
watch(modalOpen, (open) => {
  document.body.style.overflow = open ? "hidden" : "";
});

onMounted(() => {
  initializePeriod();
  window.addEventListener("keydown", handleEscape);
  void Promise.all([loadChannels(), loadBatches(1)]);
});
onBeforeUnmount(() => {
  batchRequest += 1;
  detailRequest += 1;
  document.body.style.overflow = "";
  window.removeEventListener("keydown", handleEscape);
});
</script>

<template>
  <div class="reconciliation-view">
    <p v-if="notice" class="reconciliation-feedback notice" role="status">
      <BadgeCheck />{{ notice }}
    </p>
    <p
      v-if="batchError || channelsError"
      class="reconciliation-feedback error"
      role="alert"
    >
      <AlertTriangle />{{ batchError || channelsError }}
    </p>

    <section class="reconciliation-toolbar">
      <div class="status-filter">
        <label>
          {{ t("reconciliation.filterBatchStatus") }}
          <select v-model="batchStatusFilter" :disabled="batchLoading">
            <option
              v-for="option in batchStatuses"
              :key="option.value"
              :value="option.value"
            >
              {{ t(option.label) }}
            </option>
          </select>
        </label>
        <span>{{ t("reconciliation.dataSourceHint") }}</span>
      </div>
      <div class="toolbar-actions">
        <button type="button" :disabled="batchLoading" @click="refreshAll">
          <RefreshCw />{{
            batchLoading
              ? t("reconciliation.refreshing")
              : t("reconciliation.refresh")
          }}
        </button>
        <button
          v-if="canManage"
          class="primary"
          type="button"
          @click="openImport"
        >
          <Upload />{{ t("reconciliation.importStatement") }}
        </button>
      </div>
    </section>

    <section class="reconciliation-metrics">
      <article>
        <FileSpreadsheet />
        <span>{{ t("reconciliation.metricBatches") }}</span>
        <strong>{{ batchTotal }}</strong>
        <small>{{ t("reconciliation.metricBatchesSub") }}</small>
      </article>
      <article>
        <BadgeCheck />
        <span>{{ t("reconciliation.metricMatched") }}</span>
        <strong>{{ pageMatched }}</strong>
        <small>{{ t("reconciliation.metricMatchedSub") }}</small>
      </article>
      <article>
        <AlertTriangle />
        <span>{{ t("reconciliation.metricDifferences") }}</span>
        <strong>{{ pageDifferences }}</strong>
        <small>{{ t("reconciliation.metricDifferencesSub") }}</small>
      </article>
      <article>
        <ShieldCheck />
        <span>{{ t("reconciliation.metricResolved") }}</span>
        <strong>{{ pageResolved }}</strong>
        <small>{{ t("reconciliation.metricResolvedSub") }}</small>
      </article>
    </section>

    <section class="reconciliation-panel batch-panel">
      <header>
        <div>
          <span>{{ t("reconciliation.kickerBatches") }}</span>
          <h2>{{ t("reconciliation.batchesTitle") }}</h2>
          <p>{{ t("reconciliation.batchesHint") }}</p>
        </div>
        <em v-if="batchLoading"
          ><LoaderCircle class="spinner" />{{ t("reconciliation.loading") }}</em
        >
      </header>
      <div class="table-scroll">
        <table class="batch-table">
          <thead>
            <tr>
              <th>{{ t("reconciliation.colBatchChannel") }}</th>
              <th>{{ t("reconciliation.colPeriod") }}</th>
              <th>{{ t("reconciliation.colSourceFile") }}</th>
              <th>{{ t("reconciliation.colMatchProgress") }}</th>
              <th>{{ t("reconciliation.colPendingResolved") }}</th>
              <th>{{ t("reconciliation.colStatus") }}</th>
              <th>{{ t("reconciliation.colActions") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="batch in batches"
              :key="batch.id"
              :class="{ selected: batch.id === selectedBatchID }"
            >
              <td>
                <b>{{ batch.batch_no }}</b>
                <small>{{ channelName(batch.channel_id) }}</small>
              </td>
              <td>
                <span>{{ formatPeriod(batch) }}</span>
                <small>{{
                  t("reconciliation.importedAt", {
                    time: formatTime(batch.created_at),
                  })
                }}</small>
              </td>
              <td>
                <span>{{ batch.source_file || "—" }}</span>
                <code :title="batch.statement_hash">
                  SHA-256 {{ batch.statement_hash?.slice(0, 12) || "—" }}
                </code>
              </td>
              <td>
                <div class="batch-progress">
                  <span
                    ><b>{{ number(batch.matched) }}</b> /
                    {{ number(batch.total) }}</span
                  >
                  <i><em :style="{ width: `${progress(batch)}%` }"></em></i>
                </div>
              </td>
              <td>
                <span class="difference-count">
                  {{ number(batch.mismatched) }} / {{ number(batch.resolved) }}
                </span>
              </td>
              <td>
                <span :class="['status-pill', statusClass(batch.status)]">
                  {{ batchStatus(batch.status) }}
                </span>
              </td>
              <td>
                <button class="detail-button" @click="openBatch(batch)">
                  <FileSearch />{{ t("reconciliation.viewDetail") }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!batchLoading && !batches.length" class="true-empty">
        <FileSpreadsheet />
        <b>{{ t("reconciliation.noBatches") }}</b>
        <span>{{ t("reconciliation.noBatchesHint") }}</span>
      </div>
      <footer class="reconciliation-pagination">
        <span>
          {{
            t("reconciliation.batchesTotal", {
              total: batchTotal,
              page: batchPage,
              pages: batchPageCount,
            })
          }}
        </span>
        <div>
          <button
            type="button"
            :disabled="batchLoading || batchPage <= 1"
            @click="loadBatches(batchPage - 1)"
          >
            <ChevronLeft />
          </button>
          <button
            v-for="pageNumber in batchPageNumbers"
            :key="pageNumber"
            type="button"
            :class="{ active: pageNumber === batchPage }"
            :disabled="batchLoading"
            @click="loadBatches(pageNumber)"
          >
            {{ pageNumber }}
          </button>
          <button
            type="button"
            :disabled="batchLoading || batchPage >= batchPageCount"
            @click="loadBatches(batchPage + 1)"
          >
            <ChevronRight />
          </button>
        </div>
      </footer>
    </section>

    <section v-if="selectedBatchID" class="reconciliation-panel detail-panel">
      <header class="detail-header">
        <div>
          <span>{{ t("reconciliation.kickerDifferences") }}</span>
          <h2>
            {{ selectedBatch?.batch_no || t("reconciliation.readingDetail") }}
          </h2>
          <p v-if="selectedBatch">
            {{ channelName(selectedBatch.channel_id) }} ·
            {{ formatPeriod(selectedBatch) }}
          </p>
        </div>
        <div>
          <button
            type="button"
            :disabled="detailLoading"
            @click="loadDetail(selectedBatchID, detailPage)"
          >
            <RefreshCw />{{
              detailLoading
                ? t("reconciliation.loading") + "…"
                : t("reconciliation.refreshDetail")
            }}
          </button>
          <button type="button" @click="closeDetail">
            <X />{{ t("reconciliation.close") }}
          </button>
        </div>
      </header>

      <p v-if="detailError" class="detail-error" role="alert">
        <AlertTriangle />{{ detailError }}
      </p>

      <div v-if="selectedBatch" class="detail-summary">
        <article>
          <span>{{ t("reconciliation.sumTotal") }}</span
          ><strong>{{ selectedBatch.total }}</strong>
        </article>
        <article>
          <span>{{ t("reconciliation.sumMatched") }}</span
          ><strong>{{ selectedBatch.matched }}</strong>
        </article>
        <article class="warning">
          <span>{{ t("reconciliation.sumPending") }}</span
          ><strong>{{ selectedBatch.mismatched }}</strong>
        </article>
        <article>
          <span>{{ t("reconciliation.sumResolved") }}</span
          ><strong>{{ selectedBatch.resolved }}</strong>
        </article>
        <article>
          <span>{{ t("reconciliation.sumBatchStatus") }}</span>
          <strong>{{ batchStatus(selectedBatch.status) }}</strong>
        </article>
      </div>

      <div class="detail-filter">
        <label>
          {{ t("reconciliation.filterItemStatus") }}
          <select v-model="detailStatusFilter" :disabled="detailLoading">
            <option
              v-for="option in itemStatuses"
              :key="option.value"
              :value="option.value"
            >
              {{ t(option.label) }}
            </option>
          </select>
        </label>
        <span>{{ t("reconciliation.differenceFormula") }}</span>
      </div>

      <div class="table-scroll">
        <table class="detail-table">
          <thead>
            <tr>
              <th>{{ t("reconciliation.colDirectionTrade") }}</th>
              <th>{{ t("reconciliation.colOrder") }}</th>
              <th>{{ t("reconciliation.colOccurredAt") }}</th>
              <th>{{ t("reconciliation.colSystemAmount") }}</th>
              <th>{{ t("reconciliation.colProviderAmount") }}</th>
              <th>{{ t("reconciliation.colDifference") }}</th>
              <th>{{ t("reconciliation.colStatusEvidence") }}</th>
              <th>{{ t("reconciliation.colActions") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in detailItems" :key="item.id">
              <td>
                <em class="direction">{{ directionLabel(item.direction) }}</em>
                <b>{{ item.provider_trade_no }}</b>
              </td>
              <td>
                <code :title="item.order_id || ''">
                  {{
                    item.order_id
                      ? item.order_id.slice(0, 12)
                      : t("reconciliation.channelOnly")
                  }}
                </code>
              </td>
              <td>{{ formatTime(item.provider_occurred_at) }}</td>
              <td>{{ formatMoney(item.system_amount, item.currency) }}</td>
              <td>{{ formatMoney(item.provider_amount, item.currency) }}</td>
              <td>
                <strong
                  :class="[
                    'difference-value',
                    {
                      positive: number(item.difference) > 0,
                      negative: number(item.difference) < 0,
                    },
                  ]"
                >
                  {{ formatDifference(item.difference, item.currency) }}
                </strong>
              </td>
              <td>
                <span :class="['status-pill', statusClass(item.status)]">
                  {{ itemStatus(item.status) }}
                </span>
                <details
                  v-if="item.status === 'resolved'"
                  class="resolution-evidence"
                >
                  <summary>{{ resolutionLabel(item.resolution_code) }}</summary>
                  <p>{{ item.resolution }}</p>
                  <small>
                    {{ formatTime(item.resolved_at) }} ·
                    {{
                      t("reconciliation.resolvedBy", {
                        id: item.resolved_by?.slice(0, 8) || "—",
                      })
                    }}
                  </small>
                </details>
              </td>
              <td>
                <button
                  v-if="
                    canManage &&
                    item.status !== 'matched' &&
                    item.status !== 'resolved'
                  "
                  class="resolve-button"
                  type="button"
                  @click="openResolution(item)"
                >
                  <Scale />{{ t("reconciliation.resolve") }}
                </button>
                <span v-else class="final-item">
                  <CopyCheck />{{
                    item.status === "matched"
                      ? t("reconciliation.autoFinal")
                      : t("reconciliation.auditDone")
                  }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div
        v-if="!detailLoading && !detailItems.length"
        class="true-empty compact"
      >
        <FileSearch />
        <b>{{ t("reconciliation.noDetail") }}</b>
        <span>{{ t("reconciliation.noDetailHint") }}</span>
      </div>
      <footer class="reconciliation-pagination">
        <span>
          {{
            t("reconciliation.detailTotal", {
              total: detailTotal,
              page: detailPage,
              pages: detailPageCount,
            })
          }}
        </span>
        <div>
          <button
            type="button"
            :disabled="detailLoading || detailPage <= 1"
            @click="loadDetail(selectedBatchID, detailPage - 1)"
          >
            <ChevronLeft />
          </button>
          <button
            v-for="pageNumber in detailPageNumbers"
            :key="pageNumber"
            type="button"
            :class="{ active: pageNumber === detailPage }"
            :disabled="detailLoading"
            @click="loadDetail(selectedBatchID, pageNumber)"
          >
            {{ pageNumber }}
          </button>
          <button
            type="button"
            :disabled="detailLoading || detailPage >= detailPageCount"
            @click="loadDetail(selectedBatchID, detailPage + 1)"
          >
            <ChevronRight />
          </button>
        </div>
      </footer>
    </section>

    <div
      v-if="importOpen && canManage"
      class="reconciliation-modal-backdrop"
      @click.self="closeImport"
    >
      <section
        class="reconciliation-modal import-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="import-title"
      >
        <header>
          <div>
            <span>{{ t("reconciliation.kickerImport") }}</span>
            <h2 id="import-title">{{ t("reconciliation.importTitle") }}</h2>
            <p>{{ t("reconciliation.importHint") }}</p>
          </div>
          <button type="button" :disabled="importBusy" @click="closeImport">
            <X />
          </button>
        </header>

        <form class="import-form" @submit.prevent="submitImport">
          <fieldset>
            <legend>{{ t("reconciliation.step1") }}</legend>
            <div class="form-grid three-columns">
              <label>
                {{ t("reconciliation.paymentChannel") }}
                <select
                  v-model="importForm.channelID"
                  :disabled="channelsLoading || importBusy"
                >
                  <option value="" disabled>
                    {{ t("reconciliation.selectChannel") }}
                  </option>
                  <option
                    v-for="channel in channels"
                    :key="channel.id"
                    :value="channel.id"
                  >
                    {{ channel.name }} · {{ channel.code
                    }}{{
                      channel.enabled ? "" : t("reconciliation.disabledSuffix")
                    }}
                  </option>
                </select>
              </label>
              <label>
                {{ t("reconciliation.periodFrom") }}
                <input
                  v-model="importForm.periodFrom"
                  type="datetime-local"
                  :disabled="importBusy"
                />
              </label>
              <label>
                {{ t("reconciliation.periodTo") }}
                <input
                  v-model="importForm.periodTo"
                  type="datetime-local"
                  :disabled="importBusy"
                />
              </label>
              <label>
                {{ t("currency.storeCurrency") }}
                <select v-model="importForm.currency" :disabled="importBusy">
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
            <p class="form-hint">
              {{ t("reconciliation.periodHint") }}
            </p>
          </fieldset>

          <fieldset>
            <legend>{{ t("reconciliation.step2") }}</legend>
            <div class="csv-specification">
              <div>
                <b>{{ t("reconciliation.requiredColumns") }}</b>
                <code>provider_trade_no, amount_minor, occurred_at</code>
                <span>
                  {{ t("reconciliation.requiredColumnsHint") }}
                </span>
              </div>
              <div>
                <b>{{ t("reconciliation.optionalColumns") }}</b>
                <code>direction, status, currency</code>
                <span>
                  {{ t("reconciliation.optionalColumnsHint") }}
                </span>
              </div>
              <div>
                <b>{{ t("reconciliation.importLimits") }}</b>
                <code>{{ t("reconciliation.importLimitsCode") }}</code>
                <span>
                  {{ t("reconciliation.importLimitsHint") }}
                </span>
              </div>
            </div>
            <button
              class="template-button"
              type="button"
              @click="downloadTemplate"
            >
              <Download />{{ t("reconciliation.downloadTemplate") }}
            </button>
          </fieldset>

          <fieldset>
            <legend>{{ t("reconciliation.step3") }}</legend>
            <label class="file-picker">
              <FileSpreadsheet />
              <div>
                <b>{{
                  statementFile?.name || t("reconciliation.selectCsv")
                }}</b>
                <span v-if="statementFile">
                  {{
                    t("reconciliation.fileSizeHint", {
                      size: (statementFile.size / 1024).toFixed(1),
                    })
                  }}
                </span>
                <span v-else>{{ t("reconciliation.fileClickHint") }}</span>
              </div>
              <input
                ref="fileInput"
                type="file"
                accept=".csv,text/csv"
                :disabled="importBusy"
                @change="handleFile"
              />
            </label>
            <label>
              {{ t("reconciliation.importReason") }}
              <textarea
                v-model="importForm.reason"
                rows="3"
                maxlength="500"
                :disabled="importBusy"
                :placeholder="t('reconciliation.importReasonPlaceholder')"
              ></textarea>
              <small
                >{{ Array.from(importForm.reason.trim()).length }} / 500</small
              >
            </label>
          </fieldset>

          <p
            v-if="importError || channelsError"
            class="modal-error"
            role="alert"
          >
            <AlertTriangle />{{ importError || channelsError }}
          </p>
          <footer>
            <button type="button" :disabled="importBusy" @click="closeImport">
              {{ t("reconciliation.cancel") }}
            </button>
            <button class="primary" :disabled="importBusy">
              <LoaderCircle v-if="importBusy" class="spinner" />
              <Upload v-else />
              {{
                importBusy
                  ? t("reconciliation.importing")
                  : t("reconciliation.confirmImport")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>

    <div
      v-if="resolutionOpen && resolvingItem && canManage"
      class="reconciliation-modal-backdrop"
      @click.self="closeResolution"
    >
      <section
        class="reconciliation-modal resolution-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="resolution-title"
      >
        <header>
          <div>
            <span>{{ t("reconciliation.kickerResolution") }}</span>
            <h2 id="resolution-title">
              {{ t("reconciliation.resolutionTitle") }}
            </h2>
            <p>{{ t("reconciliation.resolutionHint") }}</p>
          </div>
          <button
            type="button"
            :disabled="resolutionBusy"
            @click="closeResolution"
          >
            <X />
          </button>
        </header>

        <form class="resolution-form" @submit.prevent="submitResolution">
          <div class="difference-identity">
            <Scale />
            <div>
              <b>{{ resolvingItem.provider_trade_no }}</b>
              <span>
                {{ directionLabel(resolvingItem.direction) }} ·
                {{
                  t("reconciliation.orderNo", {
                    id:
                      resolvingItem.order_id ||
                      t("reconciliation.channelOnlyNoOrder"),
                  })
                }}
              </span>
            </div>
            <strong
              :class="{
                positive: number(resolvingItem.difference) > 0,
                negative: number(resolvingItem.difference) < 0,
              }"
            >
              {{
                formatDifference(
                  resolvingItem.difference,
                  resolvingItem.currency,
                )
              }}
            </strong>
          </div>

          <div class="amount-comparison">
            <article>
              <span>{{ t("reconciliation.systemAmount") }}</span>
              <strong>{{
                formatMoney(resolvingItem.system_amount, resolvingItem.currency)
              }}</strong>
            </article>
            <GitCompareArrows />
            <article>
              <span>{{ t("reconciliation.providerAmount") }}</span>
              <strong>{{
                formatMoney(
                  resolvingItem.provider_amount,
                  resolvingItem.currency,
                )
              }}</strong>
            </article>
          </div>

          <label>
            {{ t("reconciliation.resolutionCode") }}
            <select v-model="resolutionForm.code" :disabled="resolutionBusy">
              <option value="" disabled>
                {{ t("reconciliation.selectResolution") }}
              </option>
              <option
                v-for="option in resolutionOptions"
                :key="option.value"
                :value="option.value"
              >
                {{ t(option.label) }}
              </option>
            </select>
            <small v-if="selectedResolution">{{
              t(selectedResolution.hint)
            }}</small>
          </label>
          <label>
            {{ t("reconciliation.evidence") }}
            <textarea
              v-model="resolutionForm.evidence"
              rows="6"
              maxlength="2000"
              :disabled="resolutionBusy"
              :placeholder="t('reconciliation.evidencePlaceholder')"
            ></textarea>
            <small>
              {{ Array.from(resolutionForm.evidence.trim()).length }} / 2000
            </small>
          </label>
          <label>
            {{ t("reconciliation.auditReason") }}
            <textarea
              v-model="resolutionForm.reason"
              rows="3"
              maxlength="500"
              :disabled="resolutionBusy"
              :placeholder="t('reconciliation.auditReasonPlaceholder')"
            ></textarea>
            <small>
              {{ Array.from(resolutionForm.reason.trim()).length }} / 500
            </small>
          </label>

          <p v-if="resolutionError" class="modal-error" role="alert">
            <AlertTriangle />{{ resolutionError }}
          </p>
          <footer>
            <button
              type="button"
              :disabled="resolutionBusy"
              @click="closeResolution"
            >
              {{ t("reconciliation.cancel") }}
            </button>
            <button class="primary" :disabled="resolutionBusy">
              <LoaderCircle v-if="resolutionBusy" class="spinner" />
              <ShieldCheck v-else />
              {{
                resolutionBusy
                  ? t("reconciliation.submitting")
                  : t("reconciliation.confirmResolution")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>
  </div>
</template>

<style scoped>
.reconciliation-view {
  display: grid;
  gap: 12px;
}
.reconciliation-feedback {
  margin: 0;
  padding: 11px 13px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  background: var(--surface);
  font-size: 9px;
  line-height: 1.6;
}
.reconciliation-feedback svg,
.detail-error svg,
.modal-error svg {
  width: 15px;
  flex: 0 0 auto;
}
.reconciliation-feedback.notice {
  border-color: color-mix(in srgb, var(--success) 38%, var(--line));
  color: var(--success);
}
.reconciliation-feedback.error,
.detail-error,
.modal-error {
  color: var(--danger);
}
.reconciliation-toolbar {
  min-height: 64px;
  padding: 11px 13px;
  border: 1px solid var(--line);
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  background: var(--surface);
}
.status-filter {
  display: flex;
  align-items: flex-end;
  gap: 13px;
}
.status-filter label,
.detail-filter label,
.import-form label,
.resolution-form label {
  display: flex;
  flex-direction: column;
  gap: 5px;
  color: var(--muted);
  font-size: 8px;
  font-weight: 600;
}
.status-filter select,
.detail-filter select {
  min-width: 150px;
  height: 34px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 5px;
  outline: 0;
  background: var(--surface-2);
  color: var(--text);
  font-size: 9px;
}
.status-filter > span,
.detail-filter > span {
  padding-bottom: 5px;
  color: var(--muted);
  font-size: 8px;
}
.toolbar-actions,
.detail-header > div:last-child {
  display: flex;
  gap: 7px;
}
.toolbar-actions button,
.detail-header button,
.detail-button,
.resolve-button,
.reconciliation-modal button,
.template-button {
  min-height: 35px;
  padding: 0 11px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: var(--surface);
  color: var(--text);
  font-size: 9px;
}
.toolbar-actions button svg,
.detail-header button svg,
.detail-button svg,
.resolve-button svg,
.reconciliation-modal button svg,
.template-button svg {
  width: 14px;
}
.toolbar-actions .primary,
.reconciliation-modal .primary {
  border-color: var(--dark);
  background: var(--dark);
  color: var(--dark-text);
}
button:disabled,
input:disabled,
select:disabled,
textarea:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}
.reconciliation-metrics {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 10px;
}
.reconciliation-metrics article {
  min-width: 0;
  padding: 17px;
  border: 1px solid var(--line);
  border-radius: 8px;
  display: grid;
  grid-template-columns: auto 1fr;
  align-items: center;
  gap: 6px 9px;
  background: var(--surface);
}
.reconciliation-metrics svg {
  width: 18px;
  grid-row: 1 / 3;
}
.reconciliation-metrics span,
.reconciliation-metrics small {
  color: var(--muted);
  font-size: 8px;
}
.reconciliation-metrics strong {
  font-size: 21px;
}
.reconciliation-metrics small {
  grid-column: 1 / -1;
  margin-top: 4px;
}
.reconciliation-panel {
  border: 1px solid var(--line);
  border-radius: 8px;
  overflow: hidden;
  background: var(--surface);
}
.reconciliation-panel > header {
  min-height: 72px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 15px;
}
.reconciliation-panel > header > div:first-child > span,
.reconciliation-modal > header span {
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
  letter-spacing: 0.13em;
}
.reconciliation-panel h2,
.reconciliation-modal h2 {
  margin: 5px 0 3px;
  font-size: 16px;
  letter-spacing: -0.025em;
}
.reconciliation-panel header p,
.reconciliation-modal header p {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.55;
}
.reconciliation-panel > header > em {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  font-size: 8px;
  font-style: normal;
}
.spinner {
  animation: reconcile-spin 1s linear infinite;
}
@keyframes reconcile-spin {
  to {
    transform: rotate(360deg);
  }
}
.table-scroll {
  overflow-x: auto;
}
table {
  width: 100%;
  border-collapse: collapse;
}
.batch-table {
  min-width: 1050px;
}
.detail-table {
  min-width: 1240px;
}
th,
td {
  padding: 12px 13px;
  border-bottom: 1px solid var(--line);
  text-align: left;
  vertical-align: middle;
  font-size: 8px;
}
th {
  background: var(--surface-2);
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
  letter-spacing: 0.04em;
  white-space: nowrap;
}
tbody tr:last-child td {
  border-bottom: 0;
}
tbody tr.selected {
  background: color-mix(in srgb, var(--dark) 4%, var(--surface));
}
td > b,
td > span,
td > small,
td > code {
  display: block;
}
td > b {
  font-size: 9px;
}
td > small,
td > code {
  margin-top: 4px;
  color: var(--muted);
  font-size: 7px;
}
td > code {
  max-width: 180px;
  overflow: hidden;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.batch-progress {
  min-width: 100px;
}
.batch-progress > span {
  font-size: 8px;
}
.batch-progress > i {
  height: 3px;
  margin-top: 7px;
  display: block;
  border-radius: 5px;
  overflow: hidden;
  background: var(--soft);
}
.batch-progress > i > em {
  height: 100%;
  display: block;
  border-radius: inherit;
  background: var(--success);
}
.difference-count {
  color: var(--warn);
  font-weight: 700;
}
.status-pill {
  width: fit-content;
  padding: 5px 7px;
  border: 1px solid var(--line);
  border-radius: 99px;
  display: inline-flex;
  color: var(--muted);
  font-size: 7px;
  white-space: nowrap;
}
.status-completed,
.status-matched,
.status-resolved {
  border-color: color-mix(in srgb, var(--success) 35%, var(--line));
  color: var(--success);
}
.status-pending,
.status-processing {
  border-color: color-mix(in srgb, var(--warn) 35%, var(--line));
  color: var(--warn);
}
.status-differences_found,
.status-failed,
.status-amount_mismatch,
.status-missing_system,
.status-missing_provider {
  border-color: color-mix(in srgb, var(--danger) 32%, var(--line));
  color: var(--danger);
}
.detail-button,
.resolve-button {
  min-height: 30px;
  padding: 0 8px;
  white-space: nowrap;
}
.resolve-button {
  border-color: color-mix(in srgb, var(--warn) 40%, var(--line));
  color: var(--warn);
}
.true-empty {
  min-height: 180px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 7px;
  color: var(--muted);
  font-size: 8px;
  text-align: center;
}
.true-empty svg {
  width: 27px;
  height: 27px;
  color: var(--text);
}
.true-empty b {
  color: var(--text);
  font-size: 10px;
}
.true-empty.compact {
  min-height: 125px;
}
.reconciliation-pagination {
  min-height: 52px;
  padding: 9px 13px;
  border-top: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: var(--muted);
  font-size: 8px;
}
.reconciliation-pagination > div {
  display: flex;
  gap: 4px;
}
.reconciliation-pagination button {
  min-width: 28px;
  height: 28px;
  padding: 0 7px;
  border: 1px solid var(--line);
  border-radius: 4px;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--muted);
  font-size: 8px;
}
.reconciliation-pagination button svg {
  width: 13px;
}
.reconciliation-pagination button.active {
  background: var(--dark);
  color: var(--dark-text);
}
.detail-panel {
  margin-top: 2px;
}
.detail-header > div:last-child button {
  min-height: 32px;
}
.detail-error,
.modal-error {
  margin: 0;
  padding: 10px 13px;
  display: flex;
  align-items: flex-start;
  gap: 7px;
  background: color-mix(in srgb, var(--danger) 7%, transparent);
  font-size: 8px;
  line-height: 1.55;
}
.detail-summary {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  border-bottom: 1px solid var(--line);
}
.detail-summary article {
  padding: 13px 15px;
  border-right: 1px solid var(--line);
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.detail-summary article:last-child {
  border-right: 0;
}
.detail-summary span {
  color: var(--muted);
  font-size: 7px;
}
.detail-summary strong {
  font-size: 15px;
}
.detail-summary article.warning strong {
  color: var(--danger);
}
.detail-filter {
  padding: 10px 13px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: flex-end;
  gap: 14px;
  background: var(--surface-2);
}
.detail-table .direction {
  margin-right: 6px;
  padding: 3px 5px;
  border-radius: 4px;
  display: inline-flex;
  background: var(--soft);
  color: var(--muted);
  font-size: 7px;
  font-style: normal;
}
.detail-table td:first-child b {
  display: inline;
}
.difference-value.positive {
  color: var(--danger);
}
.difference-value.negative {
  color: var(--warn);
}
.resolution-evidence {
  max-width: 260px;
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.resolution-evidence summary {
  cursor: pointer;
  font-size: 8px;
  font-weight: 700;
}
.resolution-evidence p {
  margin: 1px 0;
  overflow-wrap: anywhere;
  color: var(--muted);
  font-size: 7px;
  line-height: 1.45;
}
.resolution-evidence small {
  color: var(--muted);
  font-size: 7px;
}
.final-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--muted);
  font-size: 7px;
  white-space: nowrap;
}
.final-item svg {
  width: 13px;
}
.reconciliation-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 140;
  padding: 22px;
  display: flex;
  justify-content: flex-end;
  background: rgba(0, 0, 0, 0.52);
  backdrop-filter: blur(2px);
}
.reconciliation-modal {
  width: min(760px, 100%);
  height: 100%;
  border: 1px solid var(--line);
  border-radius: 10px;
  overflow-y: auto;
  background: var(--surface);
  color: var(--text);
  box-shadow: var(--shadow);
}
.resolution-modal {
  width: min(650px, 100%);
}
.reconciliation-modal > header {
  position: sticky;
  top: 0;
  z-index: 3;
  min-height: 82px;
  padding: 16px 19px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 15px;
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  backdrop-filter: blur(12px);
}
.reconciliation-modal > header > button {
  width: 32px;
  min-height: 32px;
  padding: 0;
}
.import-form,
.resolution-form {
  padding: 0 19px 19px;
}
.import-form fieldset {
  margin: 0;
  padding: 18px 0;
  border: 0;
  border-bottom: 1px solid var(--line);
}
.import-form legend {
  margin-bottom: 13px;
  padding: 0;
  font-size: 10px;
  font-weight: 700;
}
.form-grid {
  display: grid;
  gap: 10px;
}
.three-columns {
  grid-template-columns: 1.2fr 1fr 1fr;
}
.import-form input,
.import-form select,
.import-form textarea,
.resolution-form select,
.resolution-form textarea {
  width: 100%;
  min-height: 37px;
  padding: 8px 9px;
  border: 1px solid var(--line);
  border-radius: 5px;
  outline: 0;
  background: var(--surface-2);
  color: var(--text);
  font-size: 9px;
}
.import-form textarea,
.resolution-form textarea {
  resize: vertical;
  line-height: 1.55;
}
.import-form input:focus,
.import-form select:focus,
.import-form textarea:focus,
.resolution-form select:focus,
.resolution-form textarea:focus {
  border-color: var(--text);
}
.form-hint {
  margin: 10px 0 0;
  padding: 8px 9px;
  border-left: 2px solid var(--warn);
  background: color-mix(in srgb, var(--warn) 7%, transparent);
  color: var(--muted);
  font-size: 7px;
  line-height: 1.6;
}
.csv-specification {
  display: grid;
  gap: 8px;
}
.csv-specification > div {
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: grid;
  grid-template-columns: 75px minmax(0, 1fr);
  gap: 5px 9px;
  background: var(--surface-2);
}
.csv-specification b {
  font-size: 8px;
}
.csv-specification code {
  overflow-wrap: anywhere;
  font-size: 8px;
}
.csv-specification span {
  grid-column: 2;
  color: var(--muted);
  font-size: 7px;
  line-height: 1.55;
}
.template-button {
  margin-top: 10px;
}
.file-picker {
  min-height: 75px;
  margin-bottom: 12px;
  padding: 12px;
  border: 1px dashed var(--line);
  border-radius: 7px;
  display: grid !important;
  grid-template-columns: auto 1fr;
  align-items: center;
  gap: 10px !important;
  background: var(--surface-2);
  cursor: pointer;
}
.file-picker > svg {
  width: 25px;
  color: var(--text);
}
.file-picker > div {
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.file-picker b {
  color: var(--text);
  font-size: 9px;
}
.file-picker span {
  color: var(--muted);
  font-size: 7px;
}
.file-picker input {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
.import-form label > small,
.resolution-form label > small {
  color: var(--muted);
  font-size: 7px;
  font-weight: 400;
  text-align: right;
}
.import-form > footer,
.resolution-form > footer {
  padding-top: 16px;
  display: flex;
  justify-content: flex-end;
  gap: 7px;
}
.modal-error {
  margin-top: 14px;
  border-radius: 5px;
}
.difference-identity {
  margin: 18px 0 12px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  background: var(--surface-2);
}
.difference-identity > svg {
  width: 22px;
}
.difference-identity > div {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.difference-identity b {
  overflow: hidden;
  font-size: 9px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.difference-identity span {
  overflow-wrap: anywhere;
  color: var(--muted);
  font-size: 7px;
}
.difference-identity > strong.positive {
  color: var(--danger);
}
.difference-identity > strong.negative {
  color: var(--warn);
}
.amount-comparison {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  gap: 8px;
}
.amount-comparison article {
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.amount-comparison span {
  color: var(--muted);
  font-size: 7px;
}
.amount-comparison strong {
  font-size: 15px;
}
.amount-comparison > svg {
  width: 17px;
  color: var(--muted);
}
.resolution-form > label {
  margin-top: 13px;
}

@media (max-width: 1050px) {
  .reconciliation-metrics {
    grid-template-columns: 1fr 1fr;
  }
  .three-columns {
    grid-template-columns: 1fr 1fr;
  }
  .three-columns label:first-child {
    grid-column: 1 / -1;
  }
}
@media (max-width: 720px) {
  .reconciliation-toolbar,
  .status-filter,
  .detail-header {
    align-items: stretch !important;
    flex-direction: column;
  }
  .status-filter > span {
    padding: 0;
  }
  .status-filter select,
  .toolbar-actions,
  .toolbar-actions button,
  .detail-header > div:last-child,
  .detail-header button {
    width: 100%;
  }
  .toolbar-actions button,
  .detail-header button {
    flex: 1;
  }
  .reconciliation-metrics {
    grid-template-columns: 1fr;
  }
  .detail-summary {
    grid-template-columns: 1fr 1fr;
  }
  .detail-summary article {
    border-bottom: 1px solid var(--line);
  }
  .detail-filter {
    align-items: stretch;
    flex-direction: column;
  }
  .detail-filter > span {
    padding: 0;
  }
  .reconciliation-pagination {
    align-items: flex-start;
    flex-direction: column;
  }
  .reconciliation-pagination > div {
    width: 100%;
    overflow-x: auto;
  }
  .reconciliation-modal-backdrop {
    padding: 0;
  }
  .reconciliation-modal {
    border: 0;
    border-radius: 0;
  }
  .three-columns {
    grid-template-columns: 1fr;
  }
  .three-columns label:first-child {
    grid-column: auto;
  }
  .csv-specification > div {
    grid-template-columns: 1fr;
  }
  .csv-specification span {
    grid-column: 1;
  }
  .difference-identity {
    grid-template-columns: auto 1fr;
  }
  .difference-identity > strong {
    grid-column: 2;
  }
}
</style>
