<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  AlertCircle,
  ArrowRight,
  Check,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Copy,
  Download,
  Eye,
  FileClock,
  LoaderCircle,
  PackageCheck,
  Plus,
  ReceiptText,
  RefreshCw,
  RotateCcw,
  Search,
  ShieldAlert,
  ShoppingBag,
  WalletCards,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import {
  formatMoney,
  majorInputStep,
  majorToMinor,
  minorToMajor,
  minorToSafeNumber,
  storeCurrency,
} from "../utils/money";

const { t, locale } = useI18n();
const route = useRoute();
const auth = useAuthStore();
const canManageOrder = computed(() => auth.hasPermission("order.manage"));
const canManagePayment = computed(() => auth.hasPermission("payment.manage"));

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface OrderItem {
  id: string;
  product_id: string;
  variant_id?: string | null;
  variant_name?: string;
  product_name: string;
  unit_price: number;
  quantity: number;
  currency: string;
}

interface Order {
  id: string;
  order_no: string;
  external_order_no?: string | null;
  coupon_id?: string | null;
  reseller_id?: string | null;
  user_id?: string | null;
  email: string;
  status: string;
  payment_status: string;
  subtotal: number;
  discount: number;
  total: number;
  currency: string;
  payment_method: string;
  paid_at?: string | null;
  delivered_at?: string | null;
  client_ip: string;
  created_at: string;
  updated_at: string;
  items: OrderItem[];
}

interface OrderEvent {
  id: string;
  from_status: string;
  to_status: string;
  actor_type: string;
  actor_id?: string | null;
  reason: string;
  created_at: string;
}

interface PaymentIntent {
  id: string;
  intent_no: string;
  amount: number;
  currency: string;
  status: string;
  provider_trade_no: string;
  expires_at: string;
  succeeded_at?: string | null;
  created_at: string;
}

interface PaymentTransaction {
  id: string;
  payment_intent_id: string;
  direction: string;
  provider_event_id: string;
  amount: number;
  fee: number;
  status: string;
  created_at: string;
}

interface Refund {
  id: string;
  refund_no: string;
  amount: number;
  currency: string;
  reason: string;
  status: string;
  attempts: number;
  provider_refund_no: string;
  processed_at?: string | null;
  created_at: string;
}

interface FulfillmentAttempt {
  id: string;
  order_item_id: string;
  mode: string;
  attempt: number;
  status: string;
  supplier_id?: string | null;
  external_order: string;
  error_code: string;
  error_message: string;
  started_at: string;
  finished_at?: string | null;
}

interface Procurement {
  id: string;
  order_item_id: string;
  supplier_id: string;
  external_order_no: string;
  amount: number;
  cost_currency?: string;
  status: string;
  attempts: number;
  error_message: string;
  created_at: string;
}

interface RiskDecision {
  id: string;
  decision: string;
  score: number;
  reasons: string;
  reviewer_id?: string | null;
  reviewed_at?: string | null;
  created_at: string;
}

interface OrderInputValue {
  id: string;
  product_id: string;
  variant_id?: string | null;
  field_id?: string | null;
  key: string;
  label: string;
  input_type: string;
  sensitive: boolean;
  pass_to_supplier: boolean;
  value?: string;
  value_preview: string;
}

interface OrderDetail {
  order: Order;
  events: OrderEvent[];
  payment_intents: PaymentIntent[];
  payment_transactions: PaymentTransaction[];
  refunds: Refund[];
  fulfillment_attempts: FulfillmentAttempt[];
  procurements: Procurement[];
  risk_decisions: RiskDecision[];
  input_values: OrderInputValue[];
  allowed_transitions: string[];
  refundable_amount: number;
  can_refund: boolean;
}

interface ManualOrderProduct {
  id: string;
  name: string;
  slug: string;
  price: number;
  inventory_mode: string;
  status: string;
  currency: string;
}

interface ManualOrderVariant {
  id: string;
  product_id: string;
  sku: string;
  name: string;
  price: number;
  status: string;
  currency?: string;
}

interface ManualOrderInputField {
  id: string;
  key: string;
  label: string;
  input_type: "text" | "email" | "number" | "select" | "textarea";
  required: boolean;
  sensitive: boolean;
  placeholder: string;
  help_text: string;
  options: string[];
  validation_pattern?: string;
  min_length: number;
  max_length: number;
}

type DetailTab = "overview" | "timeline" | "payment" | "fulfillment";

const orders = ref<Order[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const searchInput = ref("");
const appliedSearch = ref("");
const statusFilter = ref("");
const paymentFilter = ref("");
const dateFrom = ref("");
const dateTo = ref("");
const loading = ref(false);
const loadError = ref("");
const notice = ref("");
const detail = ref<OrderDetail | null>(null);
const detailOpen = ref(false);
const detailLoading = ref(false);
const detailError = ref("");
const detailTab = ref<DetailTab>("overview");
const revealedInputValues = ref<OrderInputValue[] | null>(null);
const inputRevealReason = ref("");
const inputRevealError = ref("");
const inputRevealLoading = ref(false);
const transitionTarget = ref("");
const transitionReason = ref("");
const actionSaving = ref(false);
const actionError = ref("");
const refundAmount = ref("");
const refundReason = ref("");
const refundIdempotencyKey = ref("");
const exportOpen = ref(false);
const exportReason = ref("");
const exportSaving = ref(false);
const exportError = ref("");
const manualOpen = ref(false);
const manualLoading = ref(false);
const manualSaving = ref(false);
const manualError = ref("");
const manualProducts = ref<ManualOrderProduct[]>([]);
const manualVariants = ref<ManualOrderVariant[]>([]);
const manualProductSearch = ref("");
const manualProductID = ref("");
const manualVariantID = ref("");
const manualQuantity = ref(1);
const manualEmail = ref("");
const manualPaymentReference = ref("");
const manualInputFields = ref<ManualOrderInputField[]>([]);
const manualInputValues = reactive<Record<string, string>>({});
const manualReason = ref("");
const manualResult = ref<{
  order: Order;
  lookup_token: string;
  replayed: boolean;
} | null>(null);
const manualCopied = ref(false);
let requestSequence = 0;

const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const pageRevenue = computed(() => {
  const totals = new Map<string, bigint>();
  for (const order of orders.value) {
    const code = order.currency || storeCurrency.value;
    totals.set(code, (totals.get(code) || 0n) + BigInt(order.total || 0));
  }
  return [...totals.entries()]
    .map(([currency, total]) => formatMoney(total, currency, locale.value))
    .join(" · ");
});
const pagePaid = computed(
  () =>
    orders.value.filter((order) =>
      ["paid", "partially_refunded"].includes(order.payment_status),
    ).length,
);
const pageAttention = computed(
  () =>
    orders.value.filter((order) =>
      ["risk_review", "failed"].includes(order.status),
    ).length,
);
const productsSummary = computed(() => {
  if (!detail.value) return "";
  return detail.value.order.items
    .map(
      (item) =>
        `${item.product_name}${item.variant_name ? ` / ${item.variant_name}` : ""} ×${item.quantity}`,
    )
    .join("、");
});

const orderStatusOptions = [
  ["", "order.statusOptions.all"],
  ["pending_payment", "order.statusOptions.pending_payment"],
  ["pending", "order.statusOptions.pending"],
  ["risk_review", "order.statusOptions.risk_review"],
  ["paid", "order.statusOptions.paid"],
  ["processing", "order.statusOptions.processing"],
  ["delivered", "order.statusOptions.delivered"],
  ["completed", "order.statusOptions.completed"],
  ["failed", "order.statusOptions.failed"],
  ["refunded", "order.statusOptions.refunded"],
  ["cancelled", "order.statusOptions.cancelled"],
  ["expired", "order.statusOptions.expired"],
];
const paymentStatusOptions = [
  ["", "order.paymentStatusOptions.all"],
  ["pending", "order.paymentStatusOptions.pending"],
  ["paid", "order.paymentStatusOptions.paid"],
  ["partially_refunded", "order.paymentStatusOptions.partially_refunded"],
  ["refunded", "order.paymentStatusOptions.refunded"],
  ["failed", "order.paymentStatusOptions.failed"],
];

function responseMessage(error: unknown, fallback: string) {
  const candidate = error as {
    response?: { data?: { message?: string } };
    message?: string;
  };
  return candidate.response?.data?.message || candidate.message || fallback;
}

function statusLabel(value: string) {
  const key = `order.status.${value}`;
  return t(key) === key ? value || t("order.unknown") : t(key);
}
function statusTone(value: string) {
  if (
    [
      "paid",
      "succeeded",
      "delivered",
      "completed",
      "approved",
      "available",
    ].includes(value)
  )
    return "success";
  if (["failed", "rejected", "cancelled", "expired"].includes(value))
    return "danger";
  if (
    [
      "processing",
      "risk_review",
      "retrying",
      "partially_refunded",
      "refunding",
    ].includes(value)
  )
    return "warning";
  return "neutral";
}
function money(value: number, currency?: string) {
  return formatMoney(value, currency || storeCurrency.value, locale.value);
}
function dateTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "—"
    : date.toLocaleString(locale.value, { hour12: false });
}
function shortID(value?: string | null) {
  if (!value) return "—";
  return value.length > 20 ? `${value.slice(0, 9)}…${value.slice(-6)}` : value;
}
function filterParams() {
  const params: Record<string, string | number | undefined> = {
    page: page.value,
    page_size: pageSize.value,
    q: appliedSearch.value || undefined,
    status: statusFilter.value || undefined,
    payment_status: paymentFilter.value || undefined,
  };
  if (dateFrom.value)
    params.created_from = new Date(`${dateFrom.value}T00:00:00`).toISOString();
  if (dateTo.value) {
    const end = new Date(`${dateTo.value}T00:00:00`);
    end.setDate(end.getDate() + 1);
    params.created_to = end.toISOString();
  }
  return params;
}

async function loadOrders() {
  const sequence = ++requestSequence;
  loading.value = true;
  loadError.value = "";
  try {
    const { data } = await adminApi.get("/orders", { params: filterParams() });
    if (sequence !== requestSequence) return;
    const payload = data.data as PagePayload<Order>;
    orders.value = payload?.items || [];
    total.value = Number(payload?.total || 0);
  } catch (error) {
    loadError.value = responseMessage(error, t("order.errLoad"));
  } finally {
    if (sequence === requestSequence) loading.value = false;
  }
}

function applyFilters() {
  appliedSearch.value = searchInput.value.trim();
  page.value = 1;
  loadOrders();
}
function resetFilters() {
  searchInput.value = "";
  appliedSearch.value = "";
  statusFilter.value = "";
  paymentFilter.value = "";
  dateFrom.value = "";
  dateTo.value = "";
  page.value = 1;
  loadOrders();
}
function changePage(next: number) {
  if (next < 1 || next > totalPages.value || next === page.value) return;
  page.value = next;
  loadOrders();
}

async function openDetail(order: Order) {
  detailOpen.value = true;
  detail.value = null;
  detailError.value = "";
  detailTab.value = "overview";
  transitionTarget.value = "";
  transitionReason.value = "";
  refundAmount.value = "";
  refundReason.value = "";
  refundIdempotencyKey.value = crypto.randomUUID();
  revealedInputValues.value = null;
  inputRevealReason.value = "";
  inputRevealError.value = "";
  detailLoading.value = true;
  try {
    const { data } = await adminApi.get(`/orders/${order.id}`);
    detail.value = data.data as OrderDetail;
  } catch (error) {
    detailError.value = responseMessage(error, t("order.errDetail"));
  } finally {
    detailLoading.value = false;
  }
}

function closeDetail() {
  if (!actionSaving.value && !inputRevealLoading.value) {
    detailOpen.value = false;
    revealedInputValues.value = null;
    inputRevealReason.value = "";
  }
}

async function reloadDetail() {
  if (!detail.value) return;
  const id = detail.value.order.id;
  const { data } = await adminApi.get(`/orders/${id}`);
  detail.value = data.data as OrderDetail;
}

async function revealOrderInputValues() {
  if (!canManageOrder.value) return;
  if (!detail.value || inputRevealLoading.value) return;
  const reason = inputRevealReason.value.trim();
  if (reason.length < 4 || reason.length > 500) {
    inputRevealError.value = t("order.errInputRevealReason");
    return;
  }
  inputRevealLoading.value = true;
  inputRevealError.value = "";
  try {
    const { data } = await adminApi.post(
      `/orders/${detail.value.order.id}/input-values/reveal`,
      {},
      { headers: { "X-Change-Reason": reason } },
    );
    revealedInputValues.value = Array.isArray(data.data?.input_values)
      ? data.data.input_values
      : [];
    inputRevealReason.value = "";
  } catch (error) {
    inputRevealError.value = responseMessage(error, t("order.errInputReveal"));
  } finally {
    inputRevealLoading.value = false;
  }
}

async function submitTransition() {
  if (!canManageOrder.value) return;
  if (!detail.value || !transitionTarget.value) {
    actionError.value = t("order.errNoTarget");
    return;
  }
  const reason = transitionReason.value.trim();
  if (reason.length < 4 || reason.length > 500) {
    actionError.value = t("order.errReasonShort");
    return;
  }
  actionSaving.value = true;
  actionError.value = "";
  try {
    await adminApi.post(
      `/orders/${detail.value.order.id}/transition`,
      { status: transitionTarget.value, reason },
      { headers: { "X-Change-Reason": reason } },
    );
    notice.value = t("order.transitioned", {
      orderNo: detail.value.order.order_no,
      status: statusLabel(transitionTarget.value),
    });
    transitionTarget.value = "";
    transitionReason.value = "";
    await Promise.all([reloadDetail(), loadOrders()]);
  } catch (error) {
    actionError.value = responseMessage(error, t("order.errTransition"));
  } finally {
    actionSaving.value = false;
  }
}

async function submitRefund() {
  if (!canManagePayment.value) return;
  if (!detail.value?.can_refund) return;
  const reason = refundReason.value.trim();
  if (reason.length < 4 || reason.length > 500) {
    actionError.value = t("order.errRefundReason");
    return;
  }
  let amount = 0;
  if (refundAmount.value.trim()) {
    try {
      const exact = majorToMinor(
        refundAmount.value,
        detail.value.order.currency,
      );
      amount = minorToSafeNumber(exact);
      if (
        BigInt(exact) <= 0n ||
        BigInt(exact) > BigInt(detail.value.refundable_amount)
      )
        throw new Error("refund out of range");
    } catch {
      actionError.value = t("order.errRefundAmount");
      return;
    }
  }
  actionSaving.value = true;
  actionError.value = "";
  try {
    if (!refundIdempotencyKey.value)
      refundIdempotencyKey.value = crypto.randomUUID();
    const { data } = await adminApi.post(
      "/refunds",
      { order_id: detail.value.order.id, amount, reason },
      { headers: { "Idempotency-Key": refundIdempotencyKey.value } },
    );
    notice.value = data.data?.queued
      ? t("order.refundQueued")
      : t("order.refundRecovering");
    refundAmount.value = "";
    refundReason.value = "";
    refundIdempotencyKey.value = "";
    detailTab.value = "payment";
    await Promise.all([reloadDetail(), loadOrders()]);
  } catch (error) {
    actionError.value = responseMessage(error, t("order.errRefund"));
  } finally {
    actionSaving.value = false;
  }
}

function openExport() {
  exportReason.value = "";
  exportError.value = "";
  exportOpen.value = true;
}

async function exportOrders() {
  const reason = exportReason.value.trim();
  if (reason.length < 4 || reason.length > 500) {
    exportError.value = t("order.errExportReason");
    return;
  }
  exportSaving.value = true;
  exportError.value = "";
  try {
    const { page: _page, page_size: _pageSize, ...params } = filterParams();
    const response = await adminApi.get("/orders/export", {
      params,
      headers: { "X-Change-Reason": reason },
      responseType: "blob",
      timeout: 30000,
    });
    const blob = response.data as Blob;
    const disposition = String(response.headers["content-disposition"] || "");
    const match = disposition.match(/filename="?([^";]+)"?/i);
    const filename =
      match?.[1] ||
      `linlinqi-orders-${new Date().toISOString().slice(0, 10)}.csv`;
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = filename;
    anchor.click();
    URL.revokeObjectURL(url);
    exportOpen.value = false;
    notice.value =
      response.headers["x-export-truncated"] === "true"
        ? t("order.exportTruncated")
        : t("order.exportDone");
  } catch (error) {
    const candidate = error as { response?: { data?: Blob } };
    if (candidate.response?.data instanceof Blob) {
      try {
        const payload = JSON.parse(await candidate.response.data.text());
        exportError.value = payload.message || t("order.errExport");
      } catch {
        exportError.value = t("order.errExport");
      }
    } else exportError.value = responseMessage(error, t("order.errExport"));
  } finally {
    exportSaving.value = false;
  }
}

async function loadManualProducts() {
  manualLoading.value = true;
  manualError.value = "";
  try {
    const { data } = await adminApi.get("/products", {
      params: {
        page: 1,
        page_size: 100,
        status: "on_sale",
        q: manualProductSearch.value.trim() || undefined,
      },
    });
    manualProducts.value = data.data?.items || [];
    if (
      manualProductID.value &&
      !manualProducts.value.some((item) => item.id === manualProductID.value)
    ) {
      manualProductID.value = "";
      manualVariantID.value = "";
      manualVariants.value = [];
    }
  } catch (error) {
    manualError.value = responseMessage(
      error,
      t("order.manual.productsLoadFailed"),
    );
  } finally {
    manualLoading.value = false;
  }
}

async function loadManualVariants() {
  manualVariantID.value = "";
  manualVariants.value = [];
  manualInputFields.value = [];
  Object.keys(manualInputValues).forEach(
    (key) => delete manualInputValues[key],
  );
  if (!manualProductID.value) return;
  manualLoading.value = true;
  manualError.value = "";
  try {
    const [variantResponse, inputResponse] = await Promise.all([
      adminApi.get("/operations/variants", {
        params: {
          page: 1,
          page_size: 100,
          product_id: manualProductID.value,
          status: "active",
        },
      }),
      adminApi.get(
        `/products/${encodeURIComponent(manualProductID.value)}/input-fields`,
      ),
    ]);
    manualVariants.value = variantResponse.data.data?.items || [];
    manualInputFields.value = Array.isArray(inputResponse.data.data)
      ? inputResponse.data.data.filter(
          (field: ManualOrderInputField & { enabled?: boolean }) =>
            field.enabled !== false,
        )
      : [];
    for (const field of manualInputFields.value)
      manualInputValues[field.id] = "";
  } catch (error) {
    manualError.value = responseMessage(
      error,
      t("order.manual.variantsLoadFailed"),
    );
  } finally {
    manualLoading.value = false;
  }
}

function resetManualOrder() {
  manualProductSearch.value = "";
  manualProductID.value = "";
  manualVariantID.value = "";
  manualQuantity.value = 1;
  manualEmail.value = "";
  manualPaymentReference.value = "";
  manualReason.value = "";
  manualVariants.value = [];
  manualInputFields.value = [];
  Object.keys(manualInputValues).forEach(
    (key) => delete manualInputValues[key],
  );
  manualError.value = "";
  manualResult.value = null;
  manualCopied.value = false;
}

function openManualOrder() {
  if (!canManageOrder.value) return;
  resetManualOrder();
  manualOpen.value = true;
  void loadManualProducts();
}

function closeManualOrder() {
  if (manualSaving.value) return;
  manualOpen.value = false;
  resetManualOrder();
}

async function createManualOrder() {
  if (!canManageOrder.value) return;
  manualError.value = "";
  const email = manualEmail.value.trim().toLowerCase();
  const reference = manualPaymentReference.value.trim();
  const reason = manualReason.value.trim();
  if (!manualProductID.value || !/^\S+@\S+\.\S+$/.test(email)) {
    manualError.value = t("order.manual.errSelectProduct");
    return;
  }
  if (
    !Number.isInteger(manualQuantity.value) ||
    manualQuantity.value < 1 ||
    manualQuantity.value > 20
  ) {
    manualError.value = t("order.manual.errQuantity");
    return;
  }
  if (reference.length < 4 || reference.length > 160) {
    manualError.value = t("order.manual.errPaymentRef");
    return;
  }
  if (reason.length < 4 || reason.length > 500) {
    manualError.value = t("order.manual.errEvidence");
    return;
  }
  const inputValues: Array<{ field_id: string; value: string }> = [];
  for (const field of manualInputFields.value) {
    const value = (manualInputValues[field.id] || "").trim();
    const length = [...value].length;
    let valid = Boolean(value) || !field.required;
    if (value)
      valid =
        length >= field.min_length &&
        length <= field.max_length &&
        (field.input_type !== "email" || /^\S+@\S+\.\S+$/.test(value)) &&
        (field.input_type !== "number" ||
          /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]{1,8})?$/.test(value)) &&
        (field.input_type !== "select" || field.options.includes(value));
    if (valid && value && field.validation_pattern) {
      try {
        valid = new RegExp(`^(?:${field.validation_pattern})$`).test(value);
      } catch {
        // The backend validates Go RE2 expressions authoritatively.
      }
    }
    if (!valid) {
      manualError.value =
        field.required && !value
          ? t("order.manual.errInputRequired", { field: field.label })
          : t("order.manual.errInputInvalid", { field: field.label });
      return;
    }
    if (value) inputValues.push({ field_id: field.id, value });
  }
  manualSaving.value = true;
  try {
    const { data } = await adminApi.post(
      "/orders/manual",
      {
        product_id: manualProductID.value,
        variant_id: manualVariantID.value,
        quantity: manualQuantity.value,
        email,
        payment_reference: reference,
        ...(inputValues.length ? { input_values: inputValues } : {}),
      },
      { headers: { "X-Change-Reason": reason } },
    );
    manualResult.value = data.data;
    notice.value = `${data.data.replayed ? t("order.manual.replayed") : t("order.manual.created")}${t("order.manual.createdNotice", { no: data.data.order.order_no })}`;
    await loadOrders();
  } catch (error) {
    manualError.value = responseMessage(error, t("order.manual.createFailed"));
  } finally {
    manualSaving.value = false;
  }
}

async function copyManualLookupToken() {
  const token = manualResult.value?.lookup_token || "";
  if (!token || !window.isSecureContext || !navigator.clipboard) {
    manualError.value = t("order.manual.errClipboard");
    return;
  }
  try {
    await navigator.clipboard.writeText(token);
    manualCopied.value = true;
  } catch {
    manualError.value = t("order.manual.errCopy");
  }
}

onMounted(async () => {
  await loadOrders();
  if (route.query.manual === "1") openManualOrder();
});
</script>

<template>
  <section class="order-view">
    <div class="order-actions">
      <div class="order-search">
        <Search :size="15" />
        <input
          v-model="searchInput"
          :placeholder="t('order.searchPlaceholder')"
          @keydown.enter="applyFilters"
        />
        <button type="button" @click="applyFilters">
          {{ t("order.search") }}
        </button>
      </div>
      <div>
        <button
          v-if="canManageOrder"
          type="button"
          class="secondary-button"
          @click="openManualOrder"
        >
          <Plus :size="14" />{{ t("order.manual.title") }}
        </button>
        <button
          type="button"
          class="secondary-button"
          :disabled="loading"
          @click="loadOrders"
        >
          <RefreshCw :size="14" :class="{ spinning: loading }" />{{
            t("order.refresh")
          }}
        </button>
        <button type="button" class="primary-button" @click="openExport">
          <Download :size="14" />{{ t("order.export") }}
        </button>
      </div>
    </div>

    <div class="order-filters">
      <label
        ><span>{{ t("order.filterStatus") }}</span
        ><select v-model="statusFilter">
          <option
            v-for="option in orderStatusOptions"
            :key="option[0]"
            :value="option[0]"
          >
            {{ t(option[1]) }}
          </option>
        </select></label
      >
      <label
        ><span>{{ t("order.filterPayment") }}</span
        ><select v-model="paymentFilter">
          <option
            v-for="option in paymentStatusOptions"
            :key="option[0]"
            :value="option[0]"
          >
            {{ option[1] }}
          </option>
        </select></label
      >
      <label
        ><span>{{ t("order.filterDateFrom") }}</span
        ><input v-model="dateFrom" type="date"
      /></label>
      <label
        ><span>{{ t("order.filterDateTo") }}</span
        ><input v-model="dateTo" type="date"
      /></label>
      <button type="button" @click="applyFilters">
        {{ t("order.apply") }}
      </button>
      <button type="button" class="text-button" @click="resetFilters">
        {{ t("order.reset") }}
      </button>
    </div>

    <div class="order-metrics">
      <article>
        <span><ShoppingBag :size="14" />{{ t("order.metricMatched") }}</span
        ><strong>{{ total }}</strong
        ><small>{{ t("order.metricMatchedSub") }}</small>
      </article>
      <article>
        <span><WalletCards :size="14" />{{ t("order.metricPageAmount") }}</span
        ><strong>{{ pageRevenue || money(0) }}</strong
        ><small>{{
          t("order.metricPageAmountSub", { n: orders.length })
        }}</small>
      </article>
      <article>
        <span><PackageCheck :size="14" />{{ t("order.metricPagePaid") }}</span
        ><strong>{{ pagePaid }}</strong
        ><small>{{ t("order.metricPagePaidSub") }}</small>
      </article>
      <article>
        <span
          ><ShieldAlert :size="14" />{{ t("order.metricPageAttention") }}</span
        ><strong>{{ pageAttention }}</strong
        ><small>{{ t("order.metricPageAttentionSub") }}</small>
      </article>
    </div>

    <div v-if="notice" class="order-alert success">
      <Check :size="14" /><span>{{ notice }}</span
      ><button type="button" @click="notice = ''"><X :size="13" /></button>
    </div>
    <div v-if="loadError" class="order-alert danger">
      <AlertCircle :size="14" /><span>{{ loadError }}</span
      ><button type="button" @click="loadOrders">{{ t("order.retry") }}</button>
    </div>

    <div class="order-table-shell" :aria-busy="loading">
      <table v-if="orders.length" class="order-table">
        <thead>
          <tr>
            <th>{{ t("order.colOrder") }}</th>
            <th>{{ t("order.colCustomer") }}</th>
            <th>{{ t("order.colProduct") }}</th>
            <th>{{ t("order.colPaid") }}</th>
            <th>{{ t("order.colPaymentMethod") }}</th>
            <th>{{ t("order.colStatus") }}</th>
            <th>{{ t("order.colTime") }}</th>
            <th>{{ t("order.colActions") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="order in orders" :key="order.id">
            <td :data-label="t('order.colOrder')">
              <strong>{{ order.order_no }}</strong
              ><code v-if="order.external_order_no"
                >{{ t("order.external") }}
                {{ shortID(order.external_order_no) }}</code
              ><small v-if="order.reseller_id">{{
                t("order.resellerOrder")
              }}</small>
            </td>
            <td :data-label="t('order.colCustomer')">
              <span>{{ order.email }}</span
              ><small>{{
                order.user_id
                  ? t("order.registeredUser")
                  : t("order.guestOrder")
              }}</small>
            </td>
            <td :data-label="t('order.colProduct')">
              <span>{{ order.items[0]?.product_name || "—" }}</span
              ><small v-if="order.items.length > 1">{{
                t("order.moreItems", { n: order.items.length - 1 })
              }}</small
              ><small v-else-if="order.items[0]"
                >× {{ order.items[0].quantity }}</small
              >
            </td>
            <td :data-label="t('order.colPaid')">
              <strong>{{ money(order.total, order.currency) }}</strong
              ><small v-if="order.discount"
                >{{ t("order.discount") }}
                {{ money(order.discount, order.currency) }}</small
              >
            </td>
            <td :data-label="t('order.colPaymentMethod')">
              {{ order.payment_method || "—" }}
            </td>
            <td :data-label="t('order.colStatusShort')">
              <span class="status-chip" :class="statusTone(order.status)">{{
                statusLabel(order.status)
              }}</span
              ><span
                class="status-chip"
                :class="statusTone(order.payment_status)"
                >{{ statusLabel(order.payment_status) }}</span
              >
            </td>
            <td :data-label="t('order.colTime')">
              <span>{{ dateTime(order.created_at) }}</span
              ><small v-if="order.delivered_at"
                >{{ t("order.delivered") }}
                {{ dateTime(order.delivered_at) }}</small
              >
            </td>
            <td :data-label="t('order.colActions')">
              <button
                type="button"
                class="row-action"
                @click="openDetail(order)"
              >
                <Eye :size="13" />{{ t("order.detail") }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="order-empty">
        <LoaderCircle v-if="loading" :size="25" class="spinning" /><ShoppingBag
          v-else
          :size="28"
        /><strong>{{
          loading ? t("order.loading") : t("order.noOrders")
        }}</strong
        ><span v-if="!loading">{{ t("order.noOrdersHint") }}</span>
      </div>
    </div>

    <div class="order-pagination">
      <span>{{
        t("order.total", {
          total,
          page,
          pages: totalPages,
        })
      }}</span>
      <div>
        <button
          type="button"
          :disabled="page <= 1 || loading"
          @click="changePage(page - 1)"
        >
          <ChevronLeft :size="14" />{{ t("order.prev") }}</button
        ><button
          type="button"
          :disabled="page >= totalPages || loading"
          @click="changePage(page + 1)"
        >
          {{ t("order.next") }}<ChevronRight :size="14" />
        </button>
      </div>
    </div>

    <div
      v-if="detailOpen"
      class="drawer-backdrop"
      @mousedown.self="closeDetail"
    >
      <aside
        class="order-drawer"
        role="dialog"
        aria-modal="true"
        aria-labelledby="order-detail-title"
      >
        <header>
          <div>
            <span><ReceiptText :size="18" /></span>
            <div>
              <h2 id="order-detail-title">
                {{ detail?.order.order_no || t("order.detailTitle") }}
              </h2>
              <p>{{ t("order.detailSubtitle") }}</p>
            </div>
          </div>
          <button
            type="button"
            :aria-label="t('order.close')"
            :disabled="actionSaving"
            @click="closeDetail"
          >
            <X :size="17" />
          </button>
        </header>
        <div v-if="detailLoading" class="drawer-empty">
          <LoaderCircle :size="25" class="spinning" />{{
            t("order.loadingDetail")
          }}
        </div>
        <div v-else-if="detailError" class="drawer-empty danger-text">
          <AlertCircle :size="25" />{{ detailError }}
        </div>
        <template v-else-if="detail">
          <nav class="detail-tabs">
            <button
              type="button"
              :class="{ active: detailTab === 'overview' }"
              @click="detailTab = 'overview'"
            >
              {{ t("order.tabOverview") }}
            </button>
            <button
              type="button"
              :class="{ active: detailTab === 'timeline' }"
              @click="detailTab = 'timeline'"
            >
              {{ t("order.tabTimeline") }}
            </button>
            <button
              type="button"
              :class="{ active: detailTab === 'payment' }"
              @click="detailTab = 'payment'"
            >
              {{ t("order.tabPayment") }}
            </button>
            <button
              type="button"
              :class="{ active: detailTab === 'fulfillment' }"
              @click="detailTab = 'fulfillment'"
            >
              {{ t("order.tabFulfillment") }}
            </button>
          </nav>

          <div class="drawer-content">
            <template v-if="detailTab === 'overview'">
              <section class="detail-status">
                <div>
                  <span
                    class="status-chip"
                    :class="statusTone(detail.order.status)"
                    >{{ statusLabel(detail.order.status) }}</span
                  ><span
                    class="status-chip"
                    :class="statusTone(detail.order.payment_status)"
                    >{{ statusLabel(detail.order.payment_status) }}</span
                  >
                </div>
                <strong>{{
                  money(detail.order.total, detail.order.currency)
                }}</strong>
              </section>
              <dl class="detail-grid">
                <div>
                  <dt>{{ t("order.fieldCustomerEmail") }}</dt>
                  <dd>{{ detail.order.email }}</dd>
                </div>
                <div>
                  <dt>{{ t("order.fieldOrderIp") }}</dt>
                  <dd>{{ detail.order.client_ip || "—" }}</dd>
                </div>
                <div>
                  <dt>{{ t("order.colPaymentMethod") }}</dt>
                  <dd>{{ detail.order.payment_method || "—" }}</dd>
                </div>
                <div>
                  <dt>{{ t("order.fieldOrderTime") }}</dt>
                  <dd>{{ dateTime(detail.order.created_at) }}</dd>
                </div>
                <div>
                  <dt>{{ t("order.fieldPaidTime") }}</dt>
                  <dd>{{ dateTime(detail.order.paid_at) }}</dd>
                </div>
                <div>
                  <dt>{{ t("order.fieldDeliveredTime") }}</dt>
                  <dd>{{ dateTime(detail.order.delivered_at) }}</dd>
                </div>
              </dl>
              <section class="detail-section">
                <h3>{{ t("order.productLines") }}</h3>
                <article
                  v-for="item in detail.order.items"
                  :key="item.id"
                  class="product-line"
                >
                  <div>
                    <strong>{{ item.product_name }}</strong
                    ><small>{{
                      item.variant_name || t("order.defaultVariant")
                    }}</small>
                  </div>
                  <span
                    >{{
                      money(
                        item.unit_price,
                        item.currency || detail.order.currency,
                      )
                    }}
                    × {{ item.quantity }}</span
                  >
                </article>
                <p class="product-summary">{{ productsSummary }}</p>
              </section>
              <section
                v-if="detail.input_values?.length"
                class="detail-section order-input-section"
              >
                <div class="section-title-row">
                  <div>
                    <h3>{{ t("order.inputValuesTitle") }}</h3>
                    <p>{{ t("order.inputValuesHint") }}</p>
                  </div>
                  <span>{{ detail.input_values.length }}</span>
                </div>
                <article
                  v-for="value in revealedInputValues || detail.input_values"
                  :key="value.id"
                  class="order-input-value"
                >
                  <div>
                    <strong>{{ value.label }}</strong>
                    <code>{{ value.key }}</code>
                  </div>
                  <span>{{
                    revealedInputValues
                      ? value.value || "—"
                      : value.value_preview
                  }}</span>
                  <small>
                    <b v-if="value.sensitive">{{
                      t("order.sensitiveValue")
                    }}</b>
                    <b v-if="value.pass_to_supplier">{{
                      t("order.supplierParameter")
                    }}</b>
                  </small>
                </article>
                <div
                  v-if="canManageOrder && !revealedInputValues"
                  class="input-reveal-control"
                >
                  <label>
                    {{ t("order.inputRevealReason") }}
                    <textarea
                      v-model="inputRevealReason"
                      rows="2"
                      maxlength="500"
                      :placeholder="t('order.inputRevealReasonPlaceholder')"
                    ></textarea>
                  </label>
                  <button
                    type="button"
                    class="secondary-button"
                    :disabled="inputRevealLoading"
                    @click="revealOrderInputValues"
                  >
                    <LoaderCircle
                      v-if="inputRevealLoading"
                      :size="14"
                      class="spinning"
                    /><Eye v-else :size="14" />
                    {{
                      inputRevealLoading
                        ? t("order.revealingInputs")
                        : t("order.revealInputs")
                    }}
                  </button>
                </div>
                <p
                  v-else-if="canManageOrder && revealedInputValues"
                  class="input-reveal-warning"
                >
                  <ShieldAlert :size="14" />{{ t("order.inputRevealWarning") }}
                </p>
                <p v-if="inputRevealError" class="inline-error">
                  <AlertCircle :size="14" />{{ inputRevealError }}
                </p>
              </section>
              <section class="money-breakdown">
                <div>
                  <span>{{ t("order.subtotal") }}</span
                  ><strong>{{
                    money(detail.order.subtotal, detail.order.currency)
                  }}</strong>
                </div>
                <div>
                  <span>{{ t("order.discount") }}</span
                  ><strong
                    >-{{
                      money(detail.order.discount, detail.order.currency)
                    }}</strong
                  >
                </div>
                <div>
                  <span>{{ t("order.totalAmount") }}</span
                  ><strong>{{
                    money(detail.order.total, detail.order.currency)
                  }}</strong>
                </div>
              </section>
              <section
                v-if="canManageOrder && detail.allowed_transitions.length"
                class="detail-section action-section"
              >
                <h3>{{ t("order.manualTransition") }}</h3>
                <p>{{ t("order.manualTransitionHint") }}</p>
                <div class="transition-buttons">
                  <button
                    v-for="target in detail.allowed_transitions"
                    :key="target"
                    type="button"
                    :class="{ selected: transitionTarget === target }"
                    @click="transitionTarget = target"
                  >
                    {{ statusLabel(target) }}
                  </button>
                </div>
                <textarea
                  v-model="transitionReason"
                  maxlength="500"
                  rows="3"
                  :placeholder="t('order.transitionReasonPlaceholder')"
                ></textarea
                ><button
                  type="button"
                  class="primary-button"
                  :disabled="actionSaving || !transitionTarget"
                  @click="submitTransition"
                >
                  <LoaderCircle
                    v-if="actionSaving"
                    :size="14"
                    class="spinning"
                  /><ArrowRight v-else :size="14" />{{
                    t("order.confirmTransition")
                  }}
                </button>
              </section>
              <section
                v-if="canManagePayment && detail.can_refund"
                class="detail-section refund-section"
              >
                <h3>{{ t("order.refundTitle") }}</h3>
                <p>
                  {{
                    t("order.refundHint", {
                      amount: money(
                        detail.refundable_amount,
                        detail.order.currency,
                      ),
                    })
                  }}
                </p>
                <div>
                  <input
                    v-model="refundAmount"
                    inputmode="decimal"
                    :step="majorInputStep(detail.order.currency)"
                    :max="
                      minorToMajor(
                        detail.refundable_amount,
                        detail.order.currency,
                      )
                    "
                    :placeholder="t('order.refundAmountPlaceholder')"
                  /><textarea
                    v-model="refundReason"
                    maxlength="500"
                    rows="3"
                    :placeholder="t('order.refundReasonPlaceholder')"
                  ></textarea>
                </div>
                <button
                  type="button"
                  class="danger-button"
                  :disabled="actionSaving"
                  @click="submitRefund"
                >
                  <RotateCcw :size="14" />{{ t("order.submitRefund") }}
                </button>
              </section>
              <div v-if="actionError" class="inline-error">
                <AlertCircle :size="14" />{{ actionError }}
              </div>
            </template>

            <template v-else-if="detailTab === 'timeline'">
              <section v-if="detail.events.length" class="timeline">
                <article v-for="event in detail.events" :key="event.id">
                  <span><Clock3 :size="13" /></span>
                  <div>
                    <strong
                      >{{
                        event.from_status
                          ? statusLabel(event.from_status)
                          : t("order.createdEvent")
                      }}
                      <ArrowRight :size="12" />
                      {{ statusLabel(event.to_status) }}</strong
                    >
                    <p>{{ event.reason || t("order.systemTransition") }}</p>
                    <small
                      >{{ event.actor_type }} ·
                      {{ dateTime(event.created_at) }}</small
                    >
                  </div>
                </article>
              </section>
              <div v-else class="drawer-empty">
                <FileClock :size="26" />{{ t("order.noEvents") }}
              </div>
            </template>

            <template v-else-if="detailTab === 'payment'">
              <section class="detail-section">
                <h3>
                  {{
                    t("order.paymentIntents", {
                      n: detail.payment_intents.length,
                    })
                  }}
                </h3>
                <article
                  v-for="intent in detail.payment_intents"
                  :key="intent.id"
                  class="operation-card"
                >
                  <div>
                    <strong>{{ intent.intent_no }}</strong
                    ><code>{{ shortID(intent.provider_trade_no) }}</code>
                  </div>
                  <div>
                    <strong>{{ money(intent.amount, intent.currency) }}</strong
                    ><span
                      class="status-chip"
                      :class="statusTone(intent.status)"
                      >{{ statusLabel(intent.status) }}</span
                    >
                  </div>
                  <small>{{
                    dateTime(intent.succeeded_at || intent.created_at)
                  }}</small>
                </article>
                <p v-if="!detail.payment_intents.length" class="muted">
                  {{ t("order.noIntents") }}
                </p>
              </section>
              <section class="detail-section">
                <h3>
                  {{
                    t("order.channelTransactions", {
                      n: detail.payment_transactions.length,
                    })
                  }}
                </h3>
                <article
                  v-for="transaction in detail.payment_transactions"
                  :key="transaction.id"
                  class="operation-card"
                >
                  <div>
                    <strong>{{ statusLabel(transaction.direction) }}</strong
                    ><code>{{ shortID(transaction.provider_event_id) }}</code>
                  </div>
                  <div>
                    <strong>{{
                      money(transaction.amount, detail.order.currency)
                    }}</strong
                    ><span
                      class="status-chip"
                      :class="statusTone(transaction.status)"
                      >{{ statusLabel(transaction.status) }}</span
                    >
                  </div>
                  <small
                    >{{ t("order.fee") }}
                    {{ money(transaction.fee, detail.order.currency) }} ·
                    {{ dateTime(transaction.created_at) }}</small
                  >
                </article>
                <p v-if="!detail.payment_transactions.length" class="muted">
                  {{ t("order.noTransactions") }}
                </p>
              </section>
              <section class="detail-section">
                <h3>
                  {{ t("order.refunds", { n: detail.refunds.length }) }}
                </h3>
                <article
                  v-for="refund in detail.refunds"
                  :key="refund.id"
                  class="operation-card"
                >
                  <div>
                    <strong>{{ refund.refund_no }}</strong>
                    <p>{{ refund.reason }}</p>
                  </div>
                  <div>
                    <strong>{{
                      money(
                        refund.amount,
                        refund.currency || detail.order.currency,
                      )
                    }}</strong
                    ><span
                      class="status-chip"
                      :class="statusTone(refund.status)"
                      >{{ statusLabel(refund.status) }}</span
                    >
                  </div>
                  <small
                    >{{ t("order.attempts", { n: refund.attempts }) }} ·
                    {{
                      dateTime(refund.processed_at || refund.created_at)
                    }}</small
                  >
                </article>
                <p v-if="!detail.refunds.length" class="muted">
                  {{ t("order.noRefunds") }}
                </p>
              </section>
            </template>

            <template v-else>
              <section class="detail-section">
                <h3>
                  {{
                    t("order.fulfillmentAttempts", {
                      n: detail.fulfillment_attempts.length,
                    })
                  }}
                </h3>
                <article
                  v-for="attempt in detail.fulfillment_attempts"
                  :key="attempt.id"
                  class="operation-card"
                >
                  <div>
                    <strong
                      >{{ attempt.mode }} ·
                      {{ t("order.attemptNo", { n: attempt.attempt }) }}</strong
                    ><code>{{ shortID(attempt.external_order) }}</code>
                    <p v-if="attempt.error_message">
                      {{ attempt.error_code }} {{ attempt.error_message }}
                    </p>
                  </div>
                  <span
                    class="status-chip"
                    :class="statusTone(attempt.status)"
                    >{{ statusLabel(attempt.status) }}</span
                  ><small
                    >{{ dateTime(attempt.started_at) }} →
                    {{ dateTime(attempt.finished_at) }}</small
                  >
                </article>
                <p v-if="!detail.fulfillment_attempts.length" class="muted">
                  {{ t("order.noFulfillments") }}
                </p>
              </section>
              <section class="detail-section">
                <h3>
                  {{
                    t("order.procurements", { n: detail.procurements.length })
                  }}
                </h3>
                <article
                  v-for="procurement in detail.procurements"
                  :key="procurement.id"
                  class="operation-card"
                >
                  <div>
                    <strong>{{
                      shortID(procurement.external_order_no)
                    }}</strong>
                    <p v-if="procurement.error_message">
                      {{ procurement.error_message }}
                    </p>
                  </div>
                  <div>
                    <strong>{{
                      money(
                        procurement.amount,
                        procurement.cost_currency || detail.order.currency,
                      )
                    }}</strong
                    ><span
                      class="status-chip"
                      :class="statusTone(procurement.status)"
                      >{{ statusLabel(procurement.status) }}</span
                    >
                  </div>
                  <small
                    >{{ t("order.attempts", { n: procurement.attempts }) }} ·
                    {{ dateTime(procurement.created_at) }}</small
                  >
                </article>
                <p v-if="!detail.procurements.length" class="muted">
                  {{ t("order.noProcurements") }}
                </p>
              </section>
              <section class="detail-section">
                <h3>
                  {{
                    t("order.riskDecisions", {
                      n: detail.risk_decisions.length,
                    })
                  }}
                </h3>
                <article
                  v-for="risk in detail.risk_decisions"
                  :key="risk.id"
                  class="operation-card"
                >
                  <div>
                    <strong
                      >{{ statusLabel(risk.decision) }} ·
                      {{ t("order.score", { n: risk.score }) }}</strong
                    >
                    <p>{{ risk.reasons || t("order.noRiskReasons") }}</p>
                  </div>
                  <span
                    class="status-chip"
                    :class="statusTone(risk.decision)"
                    >{{ statusLabel(risk.decision) }}</span
                  ><small>{{
                    dateTime(risk.reviewed_at || risk.created_at)
                  }}</small>
                </article>
                <p v-if="!detail.risk_decisions.length" class="muted">
                  {{ t("order.noRiskDecisions") }}
                </p>
              </section>
            </template>
          </div>
        </template>
      </aside>
    </div>

    <div
      v-if="manualOpen && canManageOrder"
      class="modal-backdrop"
      @mousedown.self="closeManualOrder"
    >
      <section
        class="manual-order-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="manual-order-title"
      >
        <header>
          <div>
            <ReceiptText :size="18" />
            <div>
              <h2 id="manual-order-title">{{ t("order.manual.subtitle") }}</h2>
              <p>
                {{ t("order.manual.desc") }}
              </p>
            </div>
          </div>
          <button
            type="button"
            :disabled="manualSaving"
            @click="closeManualOrder"
          >
            <X :size="16" />
          </button>
        </header>
        <div v-if="manualResult" class="manual-order-result">
          <span>{{ t("order.manual.safeCreated") }}</span>
          <strong>{{ manualResult.order.order_no }}</strong>
          <small
            >{{ manualResult.order.email }} ·
            {{ money(manualResult.order.total, manualResult.order.currency) }} ·
            {{ statusLabel(manualResult.order.status) }}</small
          >
          <label>
            {{ t("order.manual.lookupKey") }}
            <div>
              <code>{{ manualResult.lookup_token }}</code>
              <button type="button" @click="copyManualLookupToken">
                <Copy :size="14" />{{
                  manualCopied
                    ? t("order.manual.copied")
                    : t("order.manual.copy")
                }}
              </button>
            </div>
          </label>
          <p>
            {{ t("order.manual.warnKeep") }}
          </p>
        </div>
        <form v-else @submit.prevent="createManualOrder">
          <div class="manual-product-search">
            <label
              >{{ t("order.manual.searchPlaceholderLabel") }}
              <div>
                <input
                  v-model="manualProductSearch"
                  maxlength="160"
                  :placeholder="t('order.manual.searchPlaceholder')"
                  @keydown.enter.prevent="loadManualProducts"
                />
                <button
                  type="button"
                  :disabled="manualLoading"
                  @click="loadManualProducts"
                >
                  <Search :size="14" />{{ t("order.manual.search") }}
                </button>
              </div>
            </label>
          </div>
          <div class="manual-order-grid">
            <label
              >{{ t("order.manual.product") }}
              <select
                v-model="manualProductID"
                required
                :disabled="manualLoading"
                @change="loadManualVariants"
              >
                <option value="">{{ t("order.manual.selectProduct") }}</option>
                <option
                  v-for="product in manualProducts"
                  :key="product.id"
                  :value="product.id"
                >
                  {{ product.name }} ·
                  {{ money(product.price, product.currency) }} ·
                  {{ product.inventory_mode }}
                </option>
              </select></label
            >
            <label
              >{{ t("order.manual.variant") }}
              <select
                v-model="manualVariantID"
                :disabled="manualLoading || !manualVariants.length"
              >
                <option value="">
                  {{
                    manualVariants.length
                      ? t("order.manual.selectVariant")
                      : t("order.manual.noVariants")
                  }}
                </option>
                <option
                  v-for="variant in manualVariants"
                  :key="variant.id"
                  :value="variant.id"
                >
                  {{ variant.name }} · {{ variant.sku }} ·
                  {{
                    money(
                      variant.price,
                      variant.currency ||
                        manualProducts.find(
                          (item) => item.id === manualProductID,
                        )?.currency,
                    )
                  }}
                </option>
              </select></label
            >
            <label
              >{{ t("order.manual.customerEmail") }}
              <input
                v-model="manualEmail"
                type="email"
                maxlength="190"
                autocomplete="off"
                placeholder="customer@example.com"
                required
            /></label>
            <label
              >{{ t("order.manual.quantity") }}
              <input
                v-model.number="manualQuantity"
                type="number"
                min="1"
                max="20"
                step="1"
                required
            /></label>
            <div v-if="manualInputFields.length" class="manual-input-fields">
              <div>
                <strong>{{ t("order.manual.productInputs") }}</strong>
                <small>{{ t("order.manual.productInputsHint") }}</small>
              </div>
              <label v-for="field in manualInputFields" :key="field.id"
                >{{ field.label }}<em v-if="field.required">*</em>
                <select
                  v-if="field.input_type === 'select'"
                  v-model="manualInputValues[field.id]"
                  :required="field.required"
                >
                  <option value="" disabled>
                    {{ field.placeholder || field.label }}
                  </option>
                  <option
                    v-for="option in field.options"
                    :key="option"
                    :value="option"
                  >
                    {{ option }}
                  </option>
                </select>
                <textarea
                  v-else-if="field.input_type === 'textarea'"
                  v-model="manualInputValues[field.id]"
                  :minlength="field.min_length"
                  :maxlength="field.max_length"
                  :placeholder="field.placeholder"
                  :required="field.required"
                  :autocomplete="field.sensitive ? 'off' : 'on'"
                  rows="3"
                ></textarea>
                <input
                  v-else
                  v-model="manualInputValues[field.id]"
                  :type="
                    field.sensitive
                      ? 'password'
                      : field.input_type === 'email'
                        ? 'email'
                        : 'text'
                  "
                  :inputmode="
                    field.input_type === 'number' ? 'decimal' : undefined
                  "
                  :minlength="field.min_length"
                  :maxlength="field.max_length"
                  :placeholder="field.placeholder"
                  :required="field.required"
                  :autocomplete="field.sensitive ? 'new-password' : 'off'"
                />
                <small v-if="field.help_text">{{ field.help_text }}</small>
              </label>
            </div>
          </div>
          <label
            >{{ t("order.manual.paymentRef") }}
            <input
              v-model="manualPaymentReference"
              maxlength="160"
              autocomplete="off"
              :placeholder="t('order.manual.paymentRefPlaceholder')"
              required
            /><small>{{ t("order.manual.idempotencyHint") }}</small></label
          >
          <label
            >{{ t("order.manual.evidence") }}
            <textarea
              v-model="manualReason"
              rows="4"
              maxlength="500"
              :placeholder="t('order.manual.evidencePlaceholder')"
              required
            ></textarea>
          </label>
          <div v-if="manualError" class="inline-error">
            <AlertCircle :size="14" />{{ manualError }}
          </div>
        </form>
        <footer>
          <button
            type="button"
            class="secondary-button"
            :disabled="manualSaving"
            @click="closeManualOrder"
          >
            {{
              manualResult ? t("order.manual.done") : t("order.cancel")
            }}</button
          ><button
            v-if="!manualResult"
            type="button"
            class="primary-button"
            :disabled="manualSaving || manualLoading"
            @click="createManualOrder"
          >
            <LoaderCircle v-if="manualSaving" :size="14" class="spinning" />
            <PackageCheck v-else :size="14" />
            {{
              manualSaving
                ? t("order.manual.saving")
                : t("order.manual.confirmCreate")
            }}
          </button>
        </footer>
      </section>
    </div>

    <div
      v-if="exportOpen"
      class="modal-backdrop"
      @mousedown.self="!exportSaving && (exportOpen = false)"
    >
      <section
        class="export-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="export-title"
      >
        <header>
          <div>
            <Download :size="18" />
            <div>
              <h2 id="export-title">{{ t("order.exportTitle") }}</h2>
              <p>{{ t("order.exportHint") }}</p>
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
            ><span>{{ t("order.exportPurpose") }}</span
            ><textarea
              v-model="exportReason"
              maxlength="500"
              rows="4"
              :placeholder="t('order.exportPurposePlaceholder')"
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
            {{ t("order.cancel") }}</button
          ><button
            type="button"
            class="primary-button"
            :disabled="exportSaving"
            @click="exportOrders"
          >
            <LoaderCircle
              v-if="exportSaving"
              :size="14"
              class="spinning"
            /><Download v-else :size="14" />{{
              exportSaving ? t("order.generating") : t("order.generateCsv")
            }}
          </button>
        </footer>
      </section>
    </div>
  </section>
</template>

<style scoped>
.order-view {
  display: grid;
  gap: 13px;
  color: var(--text);
}
.order-actions,
.order-actions > div,
.order-filters,
.order-pagination,
.order-pagination > div {
  display: flex;
  align-items: center;
  gap: 8px;
}
.order-actions,
.order-pagination {
  justify-content: space-between;
}
.order-search {
  min-width: 330px;
  height: 37px;
  box-sizing: border-box;
  padding-left: 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
  color: var(--muted);
  background: var(--surface);
}
.order-search input {
  min-width: 0;
  flex: 1;
  height: 100%;
  border: 0;
  outline: 0;
  color: var(--text);
  background: transparent;
  font: inherit;
  font-size: 10px;
}
.order-search button {
  height: 29px;
  margin-right: 4px;
  padding: 0 11px;
  border: 0;
  border-radius: 5px;
  color: var(--surface);
  background: var(--text);
  font: inherit;
  font-size: 9px;
  cursor: pointer;
}
.primary-button,
.secondary-button,
.danger-button {
  display: inline-flex;
  min-height: 36px;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 0 12px;
  border: 1px solid var(--text);
  border-radius: 7px;
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
.order-filters {
  flex-wrap: wrap;
  padding: 10px 11px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
}
.order-filters label {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  font-size: 8px;
}
.order-filters select,
.order-filters input {
  height: 31px;
  box-sizing: border-box;
  padding: 0 8px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 9px;
}
.order-filters > button {
  height: 31px;
  padding: 0 10px;
  border: 1px solid var(--text);
  border-radius: 6px;
  color: var(--surface);
  background: var(--text);
  font: inherit;
  font-size: 8px;
  cursor: pointer;
}
.order-filters .text-button {
  margin-left: auto;
  color: var(--muted);
  border-color: transparent;
  background: transparent;
}
.order-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}
.order-metrics article {
  min-height: 83px;
  padding: 13px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
}
.order-metrics span {
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: 8px;
}
.order-metrics strong {
  display: block;
  margin-top: 9px;
  font-size: 17px;
}
.order-metrics small {
  display: block;
  margin-top: 4px;
  color: var(--muted);
  font-size: 8px;
}
.order-alert {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 9px 11px;
  border: 1px solid;
  border-radius: 7px;
  font-size: 9px;
}
.order-alert span {
  flex: 1;
}
.order-alert button {
  border: 0;
  color: inherit;
  background: transparent;
  font: inherit;
  cursor: pointer;
}
.order-alert.success {
  color: #166534;
  border-color: #86efac;
  background: #f0fdf4;
}
.order-alert.danger,
.inline-error {
  color: #991b1b;
  border-color: #fecaca;
  background: #fef2f2;
}
:global([data-theme="dark"]) .order-alert.success {
  color: #bbf7d0;
  border-color: #166534;
  background: #052e16;
}
:global([data-theme="dark"]) .order-alert.danger,
:global([data-theme="dark"]) .inline-error {
  color: #fecaca;
  border-color: #7f1d1d;
  background: #450a0a;
}
.order-table-shell {
  min-height: 320px;
  overflow-x: auto;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
}
.order-table {
  width: 100%;
  min-width: 1040px;
  border-collapse: collapse;
}
.order-table th {
  padding: 11px 12px;
  color: var(--muted);
  border-bottom: 1px solid var(--line);
  background: var(--soft);
  font-size: 8px;
  font-weight: 600;
  text-align: left;
  white-space: nowrap;
}
.order-table td {
  padding: 12px;
  border-bottom: 1px solid var(--line);
  font-size: 9px;
  vertical-align: middle;
}
.order-table tr:last-child td {
  border-bottom: 0;
}
.order-table td > strong,
.order-table td > span,
.order-table td > small,
.order-table td > code {
  display: block;
}
.order-table td > strong {
  font-size: 10px;
}
.order-table td > small,
.order-table td > code {
  margin-top: 4px;
  color: var(--muted);
  font-size: 8px;
  background: transparent;
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
  white-space: nowrap;
}
.order-table .status-chip + .status-chip {
  margin-top: 4px;
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
.order-empty,
.drawer-empty {
  min-height: 300px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 30px;
  color: var(--muted);
  font-size: 9px;
  text-align: center;
}
.order-empty strong {
  color: var(--text);
  font-size: 11px;
}
.order-pagination {
  color: var(--muted);
  font-size: 9px;
}
.order-pagination button {
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
.order-drawer {
  width: min(760px, 94vw);
  height: 100%;
  overflow-y: auto;
  border-left: 1px solid var(--line);
  background: var(--surface);
  box-shadow: -20px 0 60px rgb(0 0 0 / 30%);
}
.order-drawer > header {
  position: sticky;
  top: 0;
  z-index: 4;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  padding: 16px 18px;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
}
.order-drawer > header > div {
  display: flex;
  gap: 10px;
}
.order-drawer > header > div > span {
  display: grid;
  width: 35px;
  height: 35px;
  place-items: center;
  border-radius: 8px;
  color: var(--surface);
  background: var(--text);
}
.order-drawer h2 {
  margin: 0;
  font-size: 14px;
}
.order-drawer header p {
  margin: 5px 0 0;
  color: var(--muted);
  font-size: 8px;
}
.order-drawer header > button {
  display: grid;
  width: 30px;
  height: 30px;
  place-items: center;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
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
  gap: 12px;
  padding: 16px 18px 40px;
}
.detail-status {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--soft);
}
.detail-status > div {
  display: flex;
  gap: 6px;
}
.detail-status > strong {
  font-size: 18px;
}
.detail-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
}
.detail-grid div {
  min-width: 0;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
}
.detail-grid dt {
  color: var(--muted);
  font-size: 8px;
}
.detail-grid dd {
  margin: 6px 0 0;
  overflow: hidden;
  font-size: 9px;
  text-overflow: ellipsis;
  white-space: nowrap;
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
.product-line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 9px 0;
  border-bottom: 1px solid var(--line);
  font-size: 9px;
}
.product-line div {
  display: grid;
  gap: 3px;
}
.product-line small {
  color: var(--muted);
  font-size: 8px;
}
.section-title-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.section-title-row h3 {
  margin-bottom: 3px;
}
.section-title-row > span {
  min-width: 24px;
  height: 24px;
  display: grid;
  place-items: center;
  border-radius: 999px;
  background: var(--soft);
  font-size: 9px;
}
.order-input-value {
  display: grid;
  grid-template-columns: minmax(120px, 0.8fr) minmax(0, 1.2fr) auto;
  gap: 10px;
  align-items: center;
  padding: 10px 0;
  border-bottom: 1px solid var(--line);
}
.order-input-value > div {
  display: grid;
  gap: 3px;
}
.order-input-value strong {
  font-size: 10px;
}
.order-input-value code {
  color: var(--muted);
  font-size: 8px;
}
.order-input-value > span {
  min-width: 0;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  font-size: 10px;
}
.order-input-value > small {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.order-input-value b {
  padding: 2px 5px;
  border-radius: 999px;
  background: var(--soft);
  font-size: 7px;
}
.input-reveal-control {
  display: grid;
  gap: 9px;
  margin-top: 13px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-2);
}
.input-reveal-control label {
  display: grid;
  gap: 6px;
  font-size: 9px;
  font-weight: 650;
}
.input-reveal-control button {
  justify-self: end;
}
.input-reveal-warning {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--warn) !important;
  margin-top: 10px !important;
}
.product-summary {
  display: none;
}
.money-breakdown {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 7px;
}
.money-breakdown div {
  display: grid;
  gap: 5px;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
}
.money-breakdown span {
  color: var(--muted);
  font-size: 8px;
}
.money-breakdown strong {
  font-size: 11px;
}
.transition-buttons {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.transition-buttons button {
  min-height: 29px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 8px;
  cursor: pointer;
}
.transition-buttons button.selected {
  color: var(--surface);
  border-color: var(--text);
  background: var(--text);
}
.detail-section textarea,
.detail-section input,
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
.detail-section textarea,
.export-modal textarea {
  padding: 9px;
  resize: vertical;
}
.detail-section input {
  height: 36px;
  padding: 0 9px;
}
.refund-section > div {
  display: grid;
  grid-template-columns: 180px minmax(0, 1fr);
  gap: 7px;
}
.inline-error {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  padding: 9px;
  border: 1px solid;
  border-radius: 6px;
  font-size: 8px;
}
.timeline {
  position: relative;
  display: grid;
  gap: 0;
}
.timeline::before {
  content: "";
  position: absolute;
  left: 15px;
  top: 20px;
  bottom: 20px;
  width: 1px;
  background: var(--line);
}
.timeline article {
  position: relative;
  display: grid;
  grid-template-columns: 31px minmax(0, 1fr);
  gap: 10px;
  padding: 9px 0;
}
.timeline article > span {
  z-index: 1;
  display: grid;
  width: 31px;
  height: 31px;
  place-items: center;
  border: 1px solid var(--line);
  border-radius: 50%;
  background: var(--surface);
}
.timeline strong {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 9px;
}
.timeline p {
  margin: 5px 0;
  color: var(--text);
  font-size: 8px;
}
.timeline small {
  color: var(--muted);
  font-size: 8px;
}
.operation-card {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 6px 12px;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--soft);
}
.operation-card > div {
  display: grid;
  gap: 4px;
}
.operation-card > div:nth-child(2) {
  justify-items: end;
}
.operation-card strong {
  font-size: 9px;
}
.operation-card code,
.operation-card p,
.operation-card > small {
  margin: 0;
  color: var(--muted);
  background: transparent;
  font-size: 8px;
}
.operation-card > small {
  grid-column: 1 / -1;
}
.danger-text {
  color: #b42318;
}
.modal-backdrop {
  display: grid;
  place-items: center;
  padding: 18px;
}
.export-modal,
.manual-order-modal {
  width: min(520px, 100%);
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: 0 25px 80px rgb(0 0 0 / 35%);
}
.export-modal > header,
.manual-order-modal > header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  padding: 15px 17px;
  border-bottom: 1px solid var(--line);
}
.export-modal header > div,
.manual-order-modal header > div {
  display: flex;
  gap: 9px;
}
.export-modal h2,
.manual-order-modal h2 {
  margin: 0;
  font-size: 13px;
}
.export-modal header p,
.manual-order-modal header p {
  margin: 5px 0 0;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.5;
}
.export-modal header > button,
.manual-order-modal header > button {
  border: 0;
  color: var(--text);
  background: transparent;
  cursor: pointer;
}
.export-modal > div,
.manual-order-modal > form {
  display: grid;
  gap: 9px;
  padding: 16px 17px;
}
.export-modal label,
.manual-order-modal label {
  display: grid;
  gap: 6px;
  font-size: 9px;
}
.manual-order-modal input,
.manual-order-modal select,
.manual-order-modal textarea {
  width: 100%;
  box-sizing: border-box;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 9px;
}
.manual-order-modal input,
.manual-order-modal select {
  height: 36px;
  padding: 0 9px;
}
.manual-order-modal textarea {
  resize: vertical;
  padding: 9px;
}
.manual-order-modal label > small {
  color: var(--muted);
  font-size: 8px;
  line-height: 1.6;
}
.manual-order-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 9px;
}
.manual-input-fields {
  grid-column: 1 / -1;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 9px;
  margin-top: 4px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-2);
}
.manual-input-fields > div {
  grid-column: 1 / -1;
  display: grid;
  gap: 3px;
}
.manual-input-fields > div strong {
  font-size: 10px;
}
.manual-input-fields > div small,
.manual-input-fields label > small {
  color: var(--muted);
  font-size: 8px;
  font-weight: 400;
}
.manual-input-fields em {
  color: var(--danger);
  font-style: normal;
}
.manual-product-search label > div {
  display: flex;
}
.manual-product-search input {
  border-radius: 6px 0 0 6px;
}
.manual-product-search button,
.manual-order-result label button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 0 11px;
  border: 1px solid var(--text);
  border-radius: 0 6px 6px 0;
  color: var(--surface);
  background: var(--text);
  font: inherit;
  font-size: 8px;
  white-space: nowrap;
}
.manual-order-result {
  display: grid;
  gap: 8px;
  padding: 18px;
}
.manual-order-result > span,
.manual-order-result > small,
.manual-order-result > p {
  color: var(--muted);
  font-size: 8px;
}
.manual-order-result > strong {
  font-size: 18px;
}
.manual-order-result label > div {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
}
.manual-order-result code {
  overflow-wrap: anywhere;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 6px 0 0 6px;
  background: var(--soft);
  font-size: 9px;
}
.export-modal footer,
.manual-order-modal footer {
  display: flex;
  justify-content: flex-end;
  gap: 7px;
  padding: 12px 17px;
  border-top: 1px solid var(--line);
}
.spinning {
  animation: order-spin 0.8s linear infinite;
}
@keyframes order-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 900px) {
  .order-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .detail-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 680px) {
  .order-actions {
    align-items: stretch;
    flex-direction: column;
  }
  .order-search {
    min-width: 0;
    width: 100%;
  }
  .order-actions > div:last-child {
    justify-content: flex-end;
  }
  .order-filters {
    align-items: stretch;
  }
  .order-filters label {
    width: calc(50% - 5px);
    flex-direction: column;
    align-items: stretch;
  }
  .order-filters select,
  .order-filters input {
    width: 100%;
  }
  .order-table-shell {
    overflow: visible;
    border: 0;
    background: transparent;
  }
  .order-table {
    min-width: 0;
    display: block;
  }
  .order-table thead {
    display: none;
  }
  .order-table tbody,
  .order-table tr,
  .order-table td {
    display: block;
    width: 100%;
    box-sizing: border-box;
  }
  .order-table tr {
    margin-bottom: 9px;
    padding: 7px 10px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--surface);
  }
  .order-table td {
    min-height: 35px;
    padding: 9px 0 9px 100px;
    position: relative;
    border-bottom: 1px solid var(--line);
  }
  .order-table td:last-child {
    border-bottom: 0;
  }
  .order-table td::before {
    content: attr(data-label);
    position: absolute;
    left: 0;
    top: 11px;
    color: var(--muted);
    font-size: 8px;
  }
  .order-drawer {
    width: 100%;
  }
  .detail-grid {
    grid-template-columns: 1fr;
  }
  .refund-section > div {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 470px) {
  .order-metrics {
    grid-template-columns: 1fr;
  }
  .order-filters label {
    width: 100%;
  }
  .order-pagination {
    align-items: flex-start;
    flex-direction: column;
  }
  .order-pagination > div {
    width: 100%;
  }
  .order-pagination button {
    flex: 1;
  }
  .order-actions > div:last-child button {
    flex: 1;
  }
  .detail-tabs {
    top: 67px;
    padding-right: 12px;
    padding-left: 12px;
  }
  .drawer-content {
    padding-right: 12px;
    padding-left: 12px;
  }
  .money-breakdown {
    grid-template-columns: 1fr;
  }
  .modal-backdrop {
    padding: 0;
  }
  .export-modal {
    min-height: 100vh;
    border-radius: 0;
  }
  .manual-order-modal {
    min-height: 100vh;
    border-radius: 0;
  }
  .manual-order-grid {
    grid-template-columns: 1fr;
  }
  .manual-input-fields {
    grid-template-columns: 1fr;
  }
  .order-input-value {
    grid-template-columns: 1fr;
  }
}
</style>
