<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  AlertCircle,
  ArrowDownToLine,
  ArrowUpRight,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleDollarSign,
  Clipboard,
  CreditCard,
  Edit3,
  LoaderCircle,
  Plus,
  ReceiptText,
  RefreshCw,
  RotateCcw,
  Search,
  ShieldCheck,
  WalletCards,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import {
  type CurrencyDefinition,
  fetchPublicCurrencyDirectory,
  formatMoney as formatMinorMoney,
  majorInputStep,
  majorToMinor,
  minorToMajor,
  minorToSafeNumber,
} from "../utils/money";

const { t, locale } = useI18n();
const route = useRoute();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("payment.manage"));

type Tab = "channels" | "recharges" | "intents" | "transactions" | "refunds";

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface PaymentChannel {
  id: string;
  name: string;
  code: string;
  provider: string;
  fee_rate: number;
  enabled: boolean;
  sort: number;
  configured: boolean;
  supported_currencies: string[];
  settlement_currency: string;
  created_at: string;
  updated_at: string;
}

interface PaymentIntent {
  id: string;
  order_id: string;
  intent_no: string;
  channel_id: string;
  amount: number;
  currency: string;
  order_amount: number;
  order_currency: string;
  fx_snapshot_id?: string | null;
  status: string;
  provider_trade_no: string;
  expires_at: string;
  succeeded_at?: string | null;
  created_at: string;
}

interface RechargeOrder {
  id: string;
  recharge_no: string;
  user_id: string;
  user_email: string;
  amount: number;
  bonus: number;
  currency: string;
  credit_amount: number;
  credit_currency: string;
  fx_snapshot_id?: string | null;
  channel_code: string;
  channel_name: string;
  status: string;
  provider_trade_no: string;
  checkout_url?: string;
  expires_at: string;
  paid_at?: string | null;
  created_at: string;
}

interface PaymentTransaction {
  id: string;
  payment_intent_id: string;
  direction: string;
  provider_event_id: string;
  amount: number;
  fee: number;
  currency?: string;
  status: string;
  created_at: string;
}

interface Refund {
  id: string;
  refund_no: string;
  order_id: string;
  payment_intent_id?: string | null;
  amount: number;
  currency: string;
  order_amount: number;
  order_currency: string;
  reason: string;
  status: string;
  attempts: number;
  next_attempt_at?: string | null;
  requested_by: string;
  provider_refund_no: string;
  processed_at?: string | null;
  created_at: string;
}

interface OrderItem {
  id: string;
  product_name: string;
  variant_name?: string;
  quantity: number;
}

interface OrderOption {
  id: string;
  order_no: string;
  email: string;
  total: number;
  currency: string;
  payment_status: string;
  status: string;
  items: OrderItem[];
  created_at: string;
}

interface ChannelForm {
  name: string;
  code: string;
  provider: "signed_http" | "bepusdt";
  feePercent: number;
  enabled: boolean;
  sort: number;
  baseURL: string;
  merchantID: string;
  secret: string;
  apiToken: string;
  tradeType: string;
  fiat: string;
  timeout: number;
  supportedCurrencies: string[];
  settlementCurrency: string;
  reason: string;
}

const bepusdtTradeTypes: Array<{ value: string; label: string }> = [
  { value: "usdt.trc20", label: "USDT · TRC20" },
  { value: "usdc.trc20", label: "USDC · TRC20" },
  { value: "usdt.erc20", label: "USDT · ERC20" },
  { value: "usdc.erc20", label: "USDC · ERC20" },
  { value: "usdt.bep20", label: "USDT · BEP20" },
  { value: "usdc.bep20", label: "USDC · BEP20" },
  { value: "usdt.polygon", label: "USDT · Polygon" },
  { value: "usdc.polygon", label: "USDC · Polygon" },
  { value: "usdt.arbitrum", label: "USDT · Arbitrum" },
  { value: "usdc.arbitrum", label: "USDC · Arbitrum" },
  { value: "usdt.solana", label: "USDT · Solana" },
  { value: "usdc.solana", label: "USDC · Solana" },
  { value: "usdt.aptos", label: "USDT · Aptos" },
  { value: "usdc.aptos", label: "USDC · Aptos" },
  { value: "usdt.xlayer", label: "USDT · X Layer" },
  { value: "usdc.xlayer", label: "USDC · X Layer" },
  { value: "usdc.base", label: "USDC · Base" },
  { value: "usdt.plasma", label: "USDT · Plasma" },
  { value: "usdt.ton", label: "USDT · TON" },
  { value: "tron.trx", label: "TRX · Tron" },
  { value: "ethereum.eth", label: "ETH · Ethereum" },
  { value: "bsc.bnb", label: "BNB · BSC" },
  { value: "ton.gram", label: "GRAM · TON" },
];

const bepusdtFiats = ["CNY", "USD", "EUR", "GBP", "JPY"];

const activeTab = ref<Tab>("channels");
const channels = ref<PaymentChannel[]>([]);
const recharges = ref<RechargeOrder[]>([]);
const intents = ref<PaymentIntent[]>([]);
const transactions = ref<PaymentTransaction[]>([]);
const refunds = ref<Refund[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const statusFilter = ref("");
const searchQuery = ref("");
const loading = ref(false);
const loadError = ref("");
const notice = ref("");
const copied = ref("");
const channelModal = ref(false);
const editingChannel = ref<PaymentChannel | null>(null);
const channelSaving = ref(false);
const channelError = ref("");
const refundModal = ref(false);
const refundSaving = ref(false);
const refundError = ref("");
const orderSearch = ref("");
const orderSearching = ref(false);
const orderOptions = ref<OrderOption[]>([]);
const selectedOrder = ref<OrderOption | null>(null);
const refundAmount = ref("");
const refundReason = ref("");
const refundIdempotencyKey = ref("");
const currencyDirectory = ref<CurrencyDefinition[]>([]);
const storeCurrency = ref("CNY");
let requestSequence = 0;

function blankChannelForm(): ChannelForm {
  return {
    name: "",
    code: "",
    provider: "signed_http",
    feePercent: 0,
    enabled: true,
    sort: 0,
    baseURL: "",
    merchantID: "",
    secret: "",
    apiToken: "",
    tradeType: "usdt.trc20",
    fiat: bepusdtFiats.includes(storeCurrency.value)
      ? storeCurrency.value
      : "CNY",
    timeout: 900,
    supportedCurrencies: [storeCurrency.value],
    settlementCurrency: storeCurrency.value,
    reason: "",
  };
}

const channelForm = ref<ChannelForm>(blankChannelForm());

const tabOptions: Array<{
  value: Tab;
  label: string;
  icon: typeof CreditCard;
}> = [
  { value: "channels", label: "paymentops.tabChannels", icon: CreditCard },
  { value: "recharges", label: "paymentops.recharges", icon: WalletCards },
  { value: "intents", label: "paymentops.tabIntents", icon: ReceiptText },
  {
    value: "transactions",
    label: "paymentops.tabTransactions",
    icon: ArrowDownToLine,
  },
  { value: "refunds", label: "paymentops.tabRefunds", icon: RotateCcw },
];

const statusOptions = computed(() => {
  if (activeTab.value === "recharges") {
    return [
      ["", t("paymentops.allStatus")],
      ["creating", t("paymentops.stCreating")],
      ["pending", t("paymentops.stPending")],
      ["succeeded", t("paymentops.stSucceeded")],
      ["failed", t("paymentops.stFailed")],
      ["expired", t("paymentops.stExpired")],
      ["cancelled", t("paymentops.stCancelled")],
    ];
  }
  if (activeTab.value === "intents") {
    return [
      ["", "paymentops.statusOptions.intents.all"],
      ["creating", "paymentops.statusOptions.intents.creating"],
      ["pending", "paymentops.statusOptions.intents.pending"],
      ["succeeded", "paymentops.statusOptions.intents.succeeded"],
      ["failed", "paymentops.statusOptions.intents.failed"],
      ["expired", "paymentops.statusOptions.intents.expired"],
      ["refunded", "paymentops.statusOptions.intents.refunded"],
    ];
  }
  if (activeTab.value === "transactions") {
    return [
      ["", "paymentops.statusOptions.transactions.all"],
      ["succeeded", "paymentops.statusOptions.transactions.succeeded"],
      ["pending", "paymentops.statusOptions.transactions.pending"],
      ["failed", "paymentops.statusOptions.transactions.failed"],
    ];
  }
  if (activeTab.value === "refunds") {
    return [
      ["", "paymentops.statusOptions.refunds.all"],
      ["pending", "paymentops.statusOptions.refunds.pending"],
      ["processing", "paymentops.statusOptions.refunds.processing"],
      ["retrying", "paymentops.statusOptions.refunds.retrying"],
      ["succeeded", "paymentops.statusOptions.refunds.succeeded"],
      ["failed", "paymentops.statusOptions.refunds.failed"],
    ];
  }
  return [];
});

const currentRows = computed(() => {
  if (activeTab.value === "recharges") return recharges.value;
  if (activeTab.value === "intents") return intents.value;
  if (activeTab.value === "transactions") return transactions.value;
  if (activeTab.value === "refunds") return refunds.value;
  return channels.value;
});

const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const enabledChannels = computed(
  () => channels.value.filter((item) => item.enabled).length,
);
const selectedProducts = computed(() => {
  if (!selectedOrder.value) return "";
  return selectedOrder.value.items
    .map(
      (item) =>
        `${item.product_name}${item.variant_name ? ` / ${item.variant_name}` : ""} ×${item.quantity}`,
    )
    .join("、");
});

function responseMessage(error: unknown, fallback: string) {
  const candidate = error as {
    response?: { data?: { message?: string } };
    message?: string;
  };
  const value = candidate.response?.data?.message || candidate.message || "";
  return value.startsWith("error.") ? fallback : value || fallback;
}

function money(value: number, currency?: string) {
  return formatMinorMoney(value, currency || storeCurrency.value, locale.value);
}

function recordMoney(value: number, currency?: string) {
  return currency ? money(value, currency) : `${value} amount_minor`;
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
  return value.length > 18 ? `${value.slice(0, 8)}…${value.slice(-6)}` : value;
}

function statusLabel(value: string) {
  const key = `paymentops.status.${value}`;
  return t(key) === key ? value || t("paymentops.unknown") : t(key);
}

function statusTone(value: string) {
  if (["succeeded", "paid", "active"].includes(value)) return "success";
  if (["failed", "expired"].includes(value)) return "danger";
  if (
    ["processing", "retrying", "creating", "partially_refunded"].includes(value)
  )
    return "warning";
  return "neutral";
}

function translatedOrLiteral(value: string) {
  return value.includes(".") ? t(value) : value;
}

function callbackURL(channel: PaymentChannel) {
  const base = String(
    adminApi.defaults.baseURL || window.location.origin,
  ).replace(/\/admin\/v1\/?$/, "");
  return `${base}/api/v1/payments/${encodeURIComponent(channel.code)}/callback`;
}

async function copyText(value: string, key: string) {
  try {
    await navigator.clipboard.writeText(value);
    copied.value = key;
    window.setTimeout(() => {
      if (copied.value === key) copied.value = "";
    }, 1800);
  } catch {
    notice.value = t("paymentops.errClipboard");
  }
}

async function loadData() {
  const sequence = ++requestSequence;
  loading.value = true;
  loadError.value = "";
  try {
    if (activeTab.value === "channels") {
      const { data } = await adminApi.get("/payments");
      if (sequence !== requestSequence) return;
      channels.value = Array.isArray(data.data) ? data.data : [];
      total.value = channels.value.length;
    } else if (activeTab.value === "recharges") {
      const { data } = await adminApi.get("/recharges", {
        params: {
          page: page.value,
          page_size: pageSize.value,
          status: statusFilter.value || undefined,
          q: searchQuery.value.trim() || undefined,
        },
      });
      if (sequence !== requestSequence) return;
      const payload = data.data as PagePayload<RechargeOrder>;
      recharges.value = payload?.items || [];
      total.value = Number(payload?.total || 0);
    } else {
      const resource =
        activeTab.value === "intents"
          ? "payment-intents"
          : activeTab.value === "transactions"
            ? "payment-transactions"
            : "refunds";
      const { data } = await adminApi.get(`/operations/${resource}`, {
        params: {
          page: page.value,
          page_size: pageSize.value,
          status: statusFilter.value || undefined,
        },
      });
      if (sequence !== requestSequence) return;
      const payload = data.data as PagePayload<
        PaymentIntent | PaymentTransaction | Refund
      >;
      total.value = Number(payload?.total || 0);
      if (activeTab.value === "intents")
        intents.value = (payload?.items || []) as PaymentIntent[];
      if (activeTab.value === "transactions")
        transactions.value = (payload?.items || []) as PaymentTransaction[];
      if (activeTab.value === "refunds")
        refunds.value = (payload?.items || []) as Refund[];
    }
  } catch (error) {
    loadError.value = responseMessage(error, t("paymentops.errLoad"));
  } finally {
    if (sequence === requestSequence) loading.value = false;
  }
}

async function loadCurrencies() {
  try {
    const payload = await fetchPublicCurrencyDirectory();
    storeCurrency.value = String(payload.store_currency || "CNY");
    currencyDirectory.value = Array.isArray(payload.items)
      ? payload.items.filter((item) => item.enabled !== false)
      : [];
  } catch (error) {
    loadError.value = responseMessage(
      error,
      t("currency.payment.errorLoadCurrencies"),
    );
  }
}

function openCreateChannel() {
  if (!canManage.value) return;
  editingChannel.value = null;
  channelForm.value = blankChannelForm();
  channelError.value = "";
  channelModal.value = true;
}

function onChannelProviderChange() {
  if (channelForm.value.provider !== "bepusdt") return;
  const fiat = bepusdtFiats.includes(storeCurrency.value)
    ? storeCurrency.value
    : "CNY";
  channelForm.value.fiat = fiat;
  channelForm.value.tradeType = "usdt.trc20";
  channelForm.value.timeout = 900;
  channelForm.value.supportedCurrencies = [fiat];
  channelForm.value.settlementCurrency = fiat;
}

function onBepusdtFiatChange() {
  const fiat = channelForm.value.fiat;
  if (!fiat) return;
  channelForm.value.supportedCurrencies = [fiat];
  channelForm.value.settlementCurrency = fiat;
}

function openEditChannel(channel: PaymentChannel) {
  if (!canManage.value) return;
  editingChannel.value = channel;
  const isBepusdt = channel.provider === "bepusdt";
  channelForm.value = {
    name: channel.name,
    code: channel.code,
    provider: isBepusdt ? "bepusdt" : "signed_http",
    feePercent: channel.fee_rate / 100,
    enabled: channel.enabled,
    sort: channel.sort,
    // Connector credentials are never echoed back; empty means "keep current".
    baseURL: "",
    merchantID: "",
    secret: "",
    apiToken: "",
    tradeType: "",
    fiat: "",
    timeout: 0,
    supportedCurrencies: Array.isArray(channel.supported_currencies)
      ? [...channel.supported_currencies]
      : [storeCurrency.value],
    settlementCurrency: isBepusdt
      ? channel.settlement_currency || storeCurrency.value
      : channel.settlement_currency ||
        channel.supported_currencies?.[0] ||
        storeCurrency.value,
    reason: "",
  };
  channelError.value = "";
  channelModal.value = true;
}

function closeChannelModal() {
  if (channelSaving.value) return;
  channelModal.value = false;
  channelForm.value.secret = "";
}

async function saveChannel() {
  if (!canManage.value) return;
  channelError.value = "";
  const form = channelForm.value;
  const feeRate = Math.round(Number(form.feePercent) * 100);
  if (form.name.trim().length < 2 || form.name.trim().length > 100) {
    channelError.value = t("paymentops.errChannelName");
    return;
  }
  if (!Number.isFinite(feeRate) || feeRate < 0 || feeRate > 10000) {
    channelError.value = t("paymentops.errFeeRate");
    return;
  }
  if (form.reason.trim().length < 4 || form.reason.trim().length > 500) {
    channelError.value = t("paymentops.errReason");
    return;
  }
  if (
    !form.supportedCurrencies.length ||
    form.supportedCurrencies.some(
      (code) => !currencyDirectory.value.some((item) => item.code === code),
    )
  ) {
    channelError.value = t("currency.payment.errorSupportedCurrencies");
    return;
  }
  if (!form.supportedCurrencies.includes(form.settlementCurrency)) {
    channelError.value = t("currency.payment.errorSettlementCurrency");
    return;
  }
  const isEditing = Boolean(editingChannel.value);
  if (!isEditing) {
    if (!/^[a-z0-9][a-z0-9_-]{1,49}$/.test(form.code.trim().toLowerCase())) {
      channelError.value = t("paymentops.errChannelCode");
      return;
    }
    if (form.provider === "signed_http") {
      if (
        !form.baseURL.trim() ||
        !form.merchantID.trim() ||
        form.secret.length < 24
      ) {
        channelError.value = t("paymentops.errChannelConfig");
        return;
      }
    } else if (form.provider === "bepusdt") {
      if (
        !form.baseURL.trim() ||
        !form.apiToken.trim() ||
        !bepusdtTradeTypes.some((item) => item.value === form.tradeType) ||
        !bepusdtFiats.includes(form.fiat) ||
        form.fiat !== form.settlementCurrency
      ) {
        channelError.value = t("paymentops.errChannelConfigBepusdt");
        return;
      }
      if (
        !Number.isFinite(Number(form.timeout)) ||
        Number(form.timeout) < 120 ||
        Number(form.timeout) > 86400
      ) {
        channelError.value = t("paymentops.errTimeout");
        return;
      }
    }
  } else if (form.provider === "bepusdt") {
    // On edit, empty fields keep the current encrypted identity. Validate only
    // the values the operator actually provided this time.
    if (form.tradeType && !bepusdtTradeTypes.some((item) => item.value === form.tradeType)) {
      channelError.value = t("paymentops.errChannelConfigBepusdt");
      return;
    }
    if (
      form.fiat &&
      (!bepusdtFiats.includes(form.fiat) ||
        form.fiat !== form.settlementCurrency)
    ) {
      channelError.value = t("paymentops.errChannelConfigBepusdt");
      return;
    }
    if (form.timeout > 0 && (form.timeout < 120 || form.timeout > 86400)) {
      channelError.value = t("paymentops.errTimeout");
      return;
    }
  }
  const configFor = (): Record<string, unknown> => {
    if (form.provider === "bepusdt") {
      return {
        base_url: form.baseURL.trim(),
        api_token: form.apiToken.trim(),
        trade_type: form.tradeType,
        fiat: form.fiat,
        // 0 on edit means "keep current"; the backend merge skips zero.
        timeout: Math.trunc(Number(form.timeout) || 0),
      };
    }
    return {
      base_url: form.baseURL.trim(),
      merchant_id: form.merchantID.trim(),
      secret: form.secret,
    };
  };
  const configChangedFor = (): boolean => {
    if (form.provider === "bepusdt") {
      return Boolean(
        form.baseURL.trim() ||
          form.apiToken.trim() ||
          form.tradeType.trim() ||
          form.fiat.trim() ||
          form.timeout > 0,
      );
    }
    return Boolean(form.baseURL.trim() || form.merchantID.trim() || form.secret);
  };
  channelSaving.value = true;
  try {
    const headers = { "X-Change-Reason": form.reason.trim() };
    if (editingChannel.value) {
      await adminApi.patch(
        `/payments/${editingChannel.value.id}`,
        {
          name: form.name.trim(),
          fee_rate: feeRate,
          enabled: form.enabled,
          sort: Math.trunc(Number(form.sort) || 0),
          supported_currencies: [...form.supportedCurrencies].sort(),
          settlement_currency: form.settlementCurrency,
          ...(configChangedFor()
            ? { config: configFor() }
            : {}),
        },
        { headers },
      );
      notice.value = t("paymentops.channelUpdated", {
        name: form.name.trim(),
      });
    } else {
      await adminApi.post(
        "/payments",
        {
          name: form.name.trim(),
          code: form.code.trim().toLowerCase(),
          provider: form.provider,
          fee_rate: feeRate,
          enabled: form.enabled,
          sort: Math.trunc(Number(form.sort) || 0),
          supported_currencies: [...form.supportedCurrencies].sort(),
          settlement_currency: form.settlementCurrency,
          config: configFor(),
        },
        { headers },
      );
      notice.value = t("paymentops.channelCreated", {
        name: form.name.trim(),
      });
    }
    channelForm.value.secret = "";
    channelModal.value = false;
    await loadData();
  } catch (error) {
    channelError.value = responseMessage(error, t("paymentops.errChannelSave"));
  } finally {
    channelSaving.value = false;
  }
}

function openRefund() {
  if (!canManage.value) return;
  refundModal.value = true;
  refundError.value = "";
  orderSearch.value = "";
  orderOptions.value = [];
  selectedOrder.value = null;
  refundAmount.value = "";
  refundReason.value = "";
  refundIdempotencyKey.value = crypto.randomUUID();
}

function closeRefundModal() {
  if (!refundSaving.value) refundModal.value = false;
}

async function searchOrders() {
  const keyword = orderSearch.value.trim();
  if (keyword.length < 2) {
    refundError.value = t("paymentops.errSearchKeyword");
    return;
  }
  refundError.value = "";
  orderSearching.value = true;
  try {
    const { data } = await adminApi.get("/orders", {
      params: { q: keyword, page: 1, page_size: 30 },
    });
    const payload = data.data as PagePayload<OrderOption>;
    orderOptions.value = (payload?.items || []).filter((item) =>
      ["paid", "partially_refunded"].includes(item.payment_status),
    );
    if (!orderOptions.value.length)
      refundError.value = t("paymentops.noRefundableOrders");
  } catch (error) {
    refundError.value = responseMessage(error, t("paymentops.errOrderSearch"));
  } finally {
    orderSearching.value = false;
  }
}

function selectOrder(order: OrderOption) {
  selectedOrder.value = order;
  refundAmount.value = "";
  refundError.value = "";
}

async function submitRefund() {
  if (!canManage.value) return;
  if (!selectedOrder.value) {
    refundError.value = t("paymentops.errSelectOrder");
    return;
  }
  const amountText = refundAmount.value.trim();
  let amount = 0;
  if (amountText) {
    try {
      const exact = majorToMinor(amountText, selectedOrder.value.currency);
      amount = minorToSafeNumber(exact);
      if (
        BigInt(exact) <= 0n ||
        BigInt(exact) > BigInt(selectedOrder.value.total)
      )
        throw new Error("refund out of range");
    } catch {
      refundError.value = t("paymentops.errRefundAmount");
      return;
    }
  }
  if (
    refundReason.value.trim().length < 4 ||
    refundReason.value.trim().length > 500
  ) {
    refundError.value = t("paymentops.errRefundReason");
    return;
  }
  refundSaving.value = true;
  refundError.value = "";
  try {
    if (!refundIdempotencyKey.value)
      refundIdempotencyKey.value = crypto.randomUUID();
    const { data } = await adminApi.post(
      "/refunds",
      {
        order_id: selectedOrder.value.id,
        amount,
        reason: refundReason.value.trim(),
      },
      { headers: { "Idempotency-Key": refundIdempotencyKey.value } },
    );
    const queued = Boolean(data.data?.queued);
    notice.value = queued
      ? t("paymentops.refundQueued")
      : t("paymentops.refundRecovering");
    refundModal.value = false;
    refundIdempotencyKey.value = "";
    activeTab.value = "refunds";
    page.value = 1;
    statusFilter.value = "";
    await loadData();
  } catch (error) {
    refundError.value = responseMessage(error, t("paymentops.errRefundCreate"));
  } finally {
    refundSaving.value = false;
  }
}

function changePage(next: number) {
  if (next < 1 || next > totalPages.value || next === page.value) return;
  page.value = next;
  loadData();
}

watch(activeTab, () => {
  statusFilter.value = "";
  page.value = 1;
  notice.value = "";
  searchQuery.value = "";
  loadData();
});

watch(statusFilter, () => {
  page.value = 1;
  loadData();
});

watch(
  () => route.meta.defaultTab,
  (value) => {
    const requested = String(value || "channels") as Tab;
    if (tabOptions.some((item) => item.value === requested))
      activeTab.value = requested;
  },
  { immediate: true },
);

onMounted(() => {
  void Promise.all([loadData(), loadCurrencies()]);
});
</script>

<template>
  <section class="payment-operations">
    <div class="payment-topbar">
      <div class="payment-primary-actions">
        <button
          v-if="activeTab === 'channels' && canManage"
          type="button"
          class="primary-button"
          @click="openCreateChannel"
        >
          <Plus :size="15" /> {{ t("paymentops.addChannel") }}
        </button>
        <button
          v-if="activeTab === 'refunds' && canManage"
          type="button"
          class="primary-button"
          @click="openRefund"
        >
          <RotateCcw :size="15" /> {{ t("paymentops.refundAction") }}
        </button>
        <button
          type="button"
          class="icon-button"
          :disabled="loading"
          :aria-label="t('paymentops.refresh')"
          @click="loadData"
        >
          <RefreshCw :size="15" :class="{ spinning: loading }" />
        </button>
      </div>
    </div>

    <div class="payment-summary-grid">
      <article>
        <span
          ><CreditCard :size="14" />
          {{ t("paymentops.connectedChannels") }}</span
        >
        <strong>{{
          channels.length || (activeTab === "channels" ? total : "—")
        }}</strong>
        <small v-if="activeTab === 'channels'">{{
          t("paymentops.connectedChannelsSub", { n: enabledChannels })
        }}</small>
        <small v-else>{{ t("paymentops.channelsServerValidated") }}</small>
      </article>
      <article>
        <span
          ><ShieldCheck :size="14" /> {{ t("paymentops.configSecurity") }}</span
        >
        <strong>{{ t("paymentops.encryptedStorage") }}</strong>
        <small>{{ t("paymentops.secretNotExposed") }}</small>
      </article>
      <article>
        <span
          ><CircleDollarSign :size="14" />
          {{ t("paymentops.currentView") }}</span
        >
        <strong>{{
          activeTab === "channels"
            ? total
            : t("paymentops.countItems", { n: total })
        }}</strong>
        <small>{{ t("paymentops.serverPaged") }}</small>
      </article>
    </div>

    <div v-if="notice" class="payment-alert success-alert">
      <Check :size="15" />
      <span>{{ notice }}</span>
      <button
        type="button"
        :aria-label="t('paymentops.closeNotice')"
        @click="notice = ''"
      >
        <X :size="14" />
      </button>
    </div>
    <div v-if="loadError" class="payment-alert error-alert">
      <AlertCircle :size="15" />
      <span>{{ loadError }}</span>
      <button type="button" @click="loadData">
        {{ t("paymentops.retry") }}
      </button>
    </div>

    <div v-if="activeTab !== 'channels'" class="payment-toolbar">
      <label>
        <span>{{ t("paymentops.filterStatus") }}</span>
        <select v-model="statusFilter">
          <option
            v-for="option in statusOptions"
            :key="option[0]"
            :value="option[0]"
          >
            {{ translatedOrLiteral(option[1]) }}
          </option>
        </select>
      </label>
      <label v-if="activeTab === 'recharges'">
        <span
          >{{ t("paymentops.colRecharge") }} /
          {{ t("paymentops.colChannelRef") }}</span
        >
        <input
          v-model="searchQuery"
          type="search"
          maxlength="190"
          :placeholder="t('paymentops.searchPlaceholder')"
          @keyup.enter="loadData"
        />
      </label>
      <p>{{ t("paymentops.totalRecords", { n: total }) }}</p>
    </div>

    <div class="payment-table-shell" :aria-busy="loading">
      <div v-if="loading && !currentRows.length" class="payment-empty">
        <LoaderCircle :size="24" class="spinning" />
        <span>{{ t("paymentops.loading") }}</span>
      </div>

      <table
        v-else-if="activeTab === 'channels' && channels.length"
        class="payment-table channel-table"
      >
        <thead>
          <tr>
            <th>{{ t("paymentops.colChannel") }}</th>
            <th>{{ t("paymentops.colConnector") }}</th>
            <th>{{ t("paymentops.colFeeRate") }}</th>
            <th>{{ t("paymentops.colConfig") }}</th>
            <th>{{ t("paymentops.colCallbackUrl") }}</th>
            <th>{{ t("paymentops.colStatus") }}</th>
            <th>{{ t("paymentops.colActions") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="channel in channels" :key="channel.id">
            <td :data-label="t('paymentops.colChannel')">
              <strong>{{ channel.name }}</strong>
              <code>{{ channel.code }}</code>
            </td>
            <td :data-label="t('paymentops.colConnector')">
              {{
                channel.provider === "signed_http"
                  ? "Signed HTTP"
                  : channel.provider === "bepusdt"
                    ? "BEpusdt"
                    : channel.provider
              }}
              <div class="channel-currencies">
                <span
                  v-for="code in channel.supported_currencies || []"
                  :key="code"
                  >{{ code }}</span
                >
              </div>
              <small
                >{{ t("currency.payment.settlementCurrency") }}:
                {{ channel.settlement_currency }}</small
              >
            </td>
            <td :data-label="t('paymentops.colFeeRate')">
              {{ (channel.fee_rate / 100).toFixed(2) }}%
            </td>
            <td :data-label="t('paymentops.colConfig')">
              <span
                class="status-chip"
                :class="channel.configured ? 'success' : 'danger'"
              >
                {{
                  channel.configured
                    ? t("paymentops.configured")
                    : t("paymentops.notConfigured")
                }}
              </span>
            </td>
            <td :data-label="t('paymentops.colCallbackUrl')">
              <button
                type="button"
                class="copy-address"
                @click="
                  copyText(callbackURL(channel), `callback-${channel.id}`)
                "
              >
                <Clipboard :size="13" />
                {{
                  copied === `callback-${channel.id}`
                    ? t("paymentops.copied")
                    : t("paymentops.copyAddress")
                }}
              </button>
            </td>
            <td :data-label="t('paymentops.colStatus')">
              <span
                class="status-chip"
                :class="channel.enabled ? 'success' : 'neutral'"
              >
                {{
                  channel.enabled
                    ? t("paymentops.enabled")
                    : t("paymentops.disabled")
                }}
              </span>
            </td>
            <td :data-label="t('paymentops.colActions')">
              <button
                v-if="canManage"
                type="button"
                class="row-action"
                @click="openEditChannel(channel)"
              >
                <Edit3 :size="13" />{{ t("paymentops.edit") }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>

      <table
        v-else-if="activeTab === 'recharges' && recharges.length"
        class="payment-table"
      >
        <thead>
          <tr>
            <th>{{ t("paymentops.colRecharge") }}</th>
            <th>{{ t("paymentops.colChannel") }}</th>
            <th>{{ t("paymentops.colAmount") }}</th>
            <th>{{ t("paymentops.colChannelRef") }}</th>
            <th>{{ t("paymentops.colTime") }}</th>
            <th>{{ t("paymentops.colStatus") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in recharges" :key="item.id">
            <td :data-label="t('paymentops.colRecharge')">
              <strong>{{ item.recharge_no }}</strong>
              <small>{{ item.user_email }}</small>
            </td>
            <td :data-label="t('paymentops.colChannel')">
              <strong>{{ item.channel_name || item.channel_code }}</strong>
              <code>{{ item.channel_code }}</code>
            </td>
            <td :data-label="t('paymentops.colAmount')">
              <strong>{{ money(item.amount, item.currency) }}</strong>
              <small>
                → {{ money(item.credit_amount, item.credit_currency) }}
              </small>
              <small v-if="item.bonus">{{
                t("paymentops.includesBonus", {
                  amount: money(item.bonus, item.credit_currency),
                })
              }}</small>
            </td>
            <td :data-label="t('paymentops.colChannelRef')">
              <code :title="item.provider_trade_no">{{
                shortID(item.provider_trade_no)
              }}</code>
            </td>
            <td :data-label="t('paymentops.colTime')">
              <strong>{{ dateTime(item.created_at) }}</strong>
              <small>{{
                item.paid_at
                  ? t("paymentops.paidAt", { time: dateTime(item.paid_at) })
                  : t("paymentops.expiredAt", {
                      time: dateTime(item.expires_at),
                    })
              }}</small>
            </td>
            <td :data-label="t('paymentops.colStatus')">
              <span class="status-chip" :class="statusTone(item.status)">{{
                statusLabel(item.status)
              }}</span>
            </td>
          </tr>
        </tbody>
      </table>

      <table
        v-else-if="activeTab === 'intents' && intents.length"
        class="payment-table"
      >
        <thead>
          <tr>
            <th>{{ t("paymentops.colIntent") }}</th>
            <th>{{ t("paymentops.colOrderId") }}</th>
            <th>{{ t("paymentops.colAmount") }}</th>
            <th>{{ t("paymentops.colProviderTradeNo") }}</th>
            <th>{{ t("paymentops.colStatus") }}</th>
            <th>{{ t("paymentops.colExpireSuccess") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="intent in intents" :key="intent.id">
            <td :data-label="t('paymentops.colIntent')">
              <strong>{{ intent.intent_no }}</strong
              ><code>{{ shortID(intent.id) }}</code>
            </td>
            <td :data-label="t('paymentops.colOrderId')">
              <code>{{ shortID(intent.order_id) }}</code>
            </td>
            <td :data-label="t('paymentops.colAmount')">
              <strong>{{ money(intent.amount, intent.currency) }}</strong>
              <small>
                {{ money(intent.order_amount, intent.order_currency) }} →
                {{ money(intent.amount, intent.currency) }}
              </small>
            </td>
            <td :data-label="t('paymentops.colProviderTradeNo')">
              <code>{{ shortID(intent.provider_trade_no) }}</code>
            </td>
            <td :data-label="t('paymentops.colStatus')">
              <span class="status-chip" :class="statusTone(intent.status)">{{
                statusLabel(intent.status)
              }}</span>
            </td>
            <td :data-label="t('paymentops.colExpireSuccess')">
              <span>{{
                intent.succeeded_at
                  ? dateTime(intent.succeeded_at)
                  : dateTime(intent.expires_at)
              }}</span
              ><small>{{
                intent.succeeded_at
                  ? t("paymentops.succeeded")
                  : t("paymentops.expired")
              }}</small>
            </td>
          </tr>
        </tbody>
      </table>

      <table
        v-else-if="activeTab === 'transactions' && transactions.length"
        class="payment-table"
      >
        <thead>
          <tr>
            <th>{{ t("paymentops.colChannelEvent") }}</th>
            <th>{{ t("paymentops.colIntent") }}</th>
            <th>{{ t("paymentops.colDirection") }}</th>
            <th>{{ t("paymentops.colAmount") }}</th>
            <th>{{ t("paymentops.colFee") }}</th>
            <th>{{ t("paymentops.colStatusTime") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="transaction in transactions" :key="transaction.id">
            <td :data-label="t('paymentops.colChannelEvent')">
              <code>{{ shortID(transaction.provider_event_id) }}</code>
            </td>
            <td :data-label="t('paymentops.colIntent')">
              <code>{{ shortID(transaction.payment_intent_id) }}</code>
            </td>
            <td :data-label="t('paymentops.colDirection')">
              <span class="direction"
                ><ArrowUpRight
                  v-if="transaction.direction === 'payment'"
                  :size="13"
                /><ArrowDownToLine v-else :size="13" />{{
                  statusLabel(transaction.direction)
                }}</span
              >
            </td>
            <td :data-label="t('paymentops.colAmount')">
              <strong>{{
                recordMoney(transaction.amount, transaction.currency)
              }}</strong>
            </td>
            <td :data-label="t('paymentops.colFee')">
              {{ recordMoney(transaction.fee, transaction.currency) }}
            </td>
            <td :data-label="t('paymentops.colStatusTime')">
              <span
                class="status-chip"
                :class="statusTone(transaction.status)"
                >{{ statusLabel(transaction.status) }}</span
              ><small>{{ dateTime(transaction.created_at) }}</small>
            </td>
          </tr>
        </tbody>
      </table>

      <table
        v-else-if="activeTab === 'refunds' && refunds.length"
        class="payment-table refund-table"
      >
        <thead>
          <tr>
            <th>{{ t("paymentops.colRefund") }}</th>
            <th>{{ t("paymentops.colOrderId") }}</th>
            <th>{{ t("paymentops.colAmountReason") }}</th>
            <th>{{ t("paymentops.colProviderRefundNo") }}</th>
            <th>{{ t("paymentops.colAttempts") }}</th>
            <th>{{ t("paymentops.colStatus") }}</th>
            <th>{{ t("paymentops.colProcessedAt") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="refund in refunds" :key="refund.id">
            <td :data-label="t('paymentops.colRefund')">
              <strong>{{ refund.refund_no }}</strong
              ><small>{{ refund.requested_by }}</small>
            </td>
            <td :data-label="t('paymentops.colOrderId')">
              <code>{{ shortID(refund.order_id) }}</code>
            </td>
            <td :data-label="t('paymentops.colAmountReason')">
              <strong>{{ money(refund.amount, refund.currency) }}</strong>
              <small>
                {{ money(refund.order_amount, refund.order_currency) }} →
                {{ money(refund.amount, refund.currency) }}
              </small>
              <small :title="refund.reason">{{ refund.reason }}</small>
            </td>
            <td :data-label="t('paymentops.colProviderRefundNo')">
              <code>{{ shortID(refund.provider_refund_no) }}</code>
            </td>
            <td :data-label="t('paymentops.colAttempts')">
              {{ refund.attempts
              }}<small v-if="refund.next_attempt_at">{{
                t("paymentops.nextAttempt", {
                  time: dateTime(refund.next_attempt_at),
                })
              }}</small>
            </td>
            <td :data-label="t('paymentops.colStatus')">
              <span class="status-chip" :class="statusTone(refund.status)">{{
                statusLabel(refund.status)
              }}</span>
            </td>
            <td :data-label="t('paymentops.colProcessedAt')">
              {{ dateTime(refund.processed_at || refund.created_at) }}
            </td>
          </tr>
        </tbody>
      </table>

      <div v-else class="payment-empty">
        <WalletCards :size="28" />
        <strong>{{ t("paymentops.noRecords") }}</strong>
        <span v-if="activeTab === 'channels'">{{
          t("paymentops.noRecordsChannelsHint")
        }}</span>
        <span v-else>{{ t("paymentops.noRecordsHint") }}</span>
      </div>
    </div>

    <div
      v-if="activeTab !== 'channels' && totalPages > 1"
      class="payment-pagination"
    >
      <span>{{ t("paymentops.page", { page, pages: totalPages }) }}</span>
      <div>
        <button
          type="button"
          :disabled="page <= 1 || loading"
          @click="changePage(page - 1)"
        >
          <ChevronLeft :size="14" />{{ t("paymentops.prev") }}
        </button>
        <button
          type="button"
          :disabled="page >= totalPages || loading"
          @click="changePage(page + 1)"
        >
          {{ t("paymentops.next") }}<ChevronRight :size="14" />
        </button>
      </div>
    </div>

    <div
      v-if="channelModal && canManage"
      class="payment-modal-backdrop"
      role="presentation"
      @mousedown.self="closeChannelModal"
    >
      <section
        class="payment-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="channel-modal-title"
      >
        <header>
          <div>
            <span class="modal-icon"><CreditCard :size="18" /></span>
            <div>
              <h2 id="channel-modal-title">
                {{
                  editingChannel
                    ? t("paymentops.editChannelTitle")
                    : t("paymentops.createChannelTitle")
                }}
              </h2>
              <p>{{ t("paymentops.channelModalHint") }}</p>
            </div>
          </div>
          <button
            type="button"
            :disabled="channelSaving"
            :aria-label="t('paymentops.close')"
            @click="closeChannelModal"
          >
            <X :size="17" />
          </button>
        </header>
        <form class="payment-form" @submit.prevent="saveChannel">
          <div class="form-grid">
            <label
              ><span>{{ t("paymentops.provider") }}</span
              ><select
                v-model="channelForm.provider"
                :disabled="Boolean(editingChannel)"
                @change="onChannelProviderChange"
              >
                <option value="signed_http">{{
                  t("paymentops.connectorSignedHttp")
                }}</option>
                <option value="bepusdt">{{
                  t("paymentops.connectorBepusdt")
                }}</option>
              </select></label
            >
            <label
              ><span>{{ t("paymentops.channelName") }}</span
              ><input
                v-model="channelForm.name"
                maxlength="100"
                autocomplete="off"
                :placeholder="t('paymentops.channelNamePlaceholder')"
            /></label>
            <label
              ><span>{{ t("paymentops.channelCode") }}</span
              ><input
                v-model="channelForm.code"
                :disabled="Boolean(editingChannel)"
                maxlength="50"
                autocomplete="off"
                placeholder="alipay_main"
              /><small>{{ t("paymentops.channelCodeHint") }}</small></label
            >
            <label
              ><span>{{ t("paymentops.feeRate") }}</span
              ><input
                v-model.number="channelForm.feePercent"
                type="number"
                min="0"
                max="100"
                step="0.01"
            /></label>
            <label
              ><span>{{ t("paymentops.sort") }}</span
              ><input v-model.number="channelForm.sort" type="number" step="1"
            /></label>
          </div>
          <label class="switch-line"
            ><input v-model="channelForm.enabled" type="checkbox" /><span
              ><strong>{{ t("paymentops.enableChannel") }}</strong
              ><small>{{ t("paymentops.enableChannelHint") }}</small></span
            ></label
          >
          <fieldset>
            <legend>{{ t("currency.payment.supportedCurrencies") }}</legend>
            <p class="field-hint">
              {{ t("currency.payment.supportedCurrenciesHint") }}
            </p>
            <div class="currency-checkbox-grid">
              <label
                v-for="currency in currencyDirectory"
                :key="currency.code"
                :class="{
                  selected: channelForm.supportedCurrencies.includes(
                    currency.code,
                  ),
                }"
              >
                <input
                  v-model="channelForm.supportedCurrencies"
                  type="checkbox"
                  :value="currency.code"
                />
                <span>
                  <b>{{ currency.code }} · {{ currency.symbol }}</b>
                  <small>{{ currency.name }}</small>
                </span>
              </label>
            </div>
          </fieldset>
          <label>
            <span>{{ t("currency.payment.settlementCurrency") }}</span>
            <select v-model="channelForm.settlementCurrency">
              <option
                v-for="code in channelForm.supportedCurrencies"
                :key="code"
                :value="code"
              >
                {{ code }}
              </option>
            </select>
            <small class="field-hint">{{
              t("currency.payment.settlementCurrencyHint")
            }}</small>
          </label>
          <fieldset>
            <legend>
              {{
                editingChannel
                  ? t("paymentops.configEditLegend")
                  : channelForm.provider === "bepusdt"
                    ? t("paymentops.configCreateLegendBepusdt")
                    : t("paymentops.configCreateLegend")
              }}
            </legend>
            <label
              ><span>{{ t("paymentops.baseUrl") }}</span
              ><input
                v-model="channelForm.baseURL"
                type="url"
                autocomplete="url"
                :placeholder="
                  channelForm.provider === 'bepusdt'
                    ? 'https://bepusdt.example.com'
                    : 'https://payments.example.com'
                "
            /></label>
            <div v-if="channelForm.provider === 'bepusdt'" class="form-grid">
              <label
                ><span>{{ t("paymentops.apiToken") }}</span
                ><input
                  v-model="channelForm.apiToken"
                  type="password"
                  autocomplete="new-password"
                  :placeholder="t('paymentops.apiTokenPlaceholder')"
                /><small>{{ t("paymentops.apiTokenHint") }}</small></label
              >
              <label
                ><span>{{ t("paymentops.tradeType") }}</span
                ><select v-model="channelForm.tradeType">
                  <option
                    v-for="item in bepusdtTradeTypes"
                    :key="item.value"
                    :value="item.value"
                  >
                    {{ item.label }}
                  </option>
                </select></label
              >
              <label
                ><span>{{ t("paymentops.fiat") }}</span
                ><select
                  v-model="channelForm.fiat"
                  @change="onBepusdtFiatChange"
                >
                  <option
                    v-for="code in bepusdtFiats"
                    :key="code"
                    :value="code"
                  >
                    {{ code }}
                  </option>
                </select></label
              >
              <label
                ><span>{{ t("paymentops.timeout") }}</span
                ><input
                  v-model.number="channelForm.timeout"
                  type="number"
                  min="120"
                  max="86400"
                  step="60"
                /><small>{{ t("paymentops.timeoutHint") }}</small></label
              >
            </div>
            <div v-else class="form-grid">
              <label
                ><span>{{ t("paymentops.merchantId") }}</span
                ><input
                  v-model="channelForm.merchantID"
                  autocomplete="off"
                  placeholder="merchant_..."
              /></label>
              <label
                ><span>{{
                  editingChannel
                    ? t("paymentops.newSecret")
                    : t("paymentops.secret")
                }}</span
                ><input
                  v-model="channelForm.secret"
                  type="password"
                  autocomplete="new-password"
                  :placeholder="
                    editingChannel
                      ? t('paymentops.newSecretPlaceholder')
                      : t('paymentops.secretPlaceholder')
                  "
                /><small>{{ t("paymentops.secretSubmitHint") }}</small></label
              >
            </div>
          </fieldset>
          <label
            ><span>{{ t("paymentops.operationReason") }}</span
            ><textarea
              v-model="channelForm.reason"
              maxlength="500"
              rows="3"
              :placeholder="t('paymentops.operationReasonPlaceholder')"
            ></textarea>
          </label>
          <div v-if="channelError" class="inline-error">
            <AlertCircle :size="14" />{{ channelError }}
          </div>
          <footer>
            <button
              type="button"
              class="secondary-button"
              :disabled="channelSaving"
              @click="closeChannelModal"
            >
              {{ t("paymentops.cancel") }}</button
            ><button
              type="submit"
              class="primary-button"
              :disabled="channelSaving"
            >
              <LoaderCircle
                v-if="channelSaving"
                :size="14"
                class="spinning"
              /><Check v-else :size="14" />{{
                channelSaving
                  ? t("paymentops.saving")
                  : t("paymentops.confirmSave")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>

    <div
      v-if="refundModal && canManage"
      class="payment-modal-backdrop"
      role="presentation"
      @mousedown.self="closeRefundModal"
    >
      <section
        class="payment-modal refund-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="refund-modal-title"
      >
        <header>
          <div>
            <span class="modal-icon"><RotateCcw :size="18" /></span>
            <div>
              <h2 id="refund-modal-title">{{ t("paymentops.refundTitle") }}</h2>
              <p>{{ t("paymentops.refundModalHint") }}</p>
            </div>
          </div>
          <button
            type="button"
            :disabled="refundSaving"
            :aria-label="t('paymentops.close')"
            @click="closeRefundModal"
          >
            <X :size="17" />
          </button>
        </header>
        <form class="payment-form" @submit.prevent="submitRefund">
          <label
            ><span>{{ t("paymentops.searchOrder") }}</span>
            <div class="search-control">
              <Search :size="14" /><input
                v-model="orderSearch"
                autocomplete="off"
                :placeholder="t('paymentops.searchOrderPlaceholder')"
                @keydown.enter.prevent="searchOrders"
              /><button
                type="button"
                :disabled="orderSearching"
                @click="searchOrders"
              >
                {{
                  orderSearching
                    ? t("paymentops.searching")
                    : t("paymentops.search")
                }}
              </button>
            </div></label
          >
          <div
            v-if="orderOptions.length && !selectedOrder"
            class="order-options"
          >
            <button
              v-for="order in orderOptions"
              :key="order.id"
              type="button"
              @click="selectOrder(order)"
            >
              <span
                ><strong>{{ order.order_no }}</strong
                ><small
                  >{{ order.email }} · {{ dateTime(order.created_at) }}</small
                ></span
              >
              <span
                ><strong>{{ money(order.total, order.currency) }}</strong
                ><small>{{ statusLabel(order.payment_status) }}</small></span
              >
            </button>
          </div>
          <article v-if="selectedOrder" class="selected-order">
            <div>
              <span>{{ t("paymentops.selectedOrder") }}</span
              ><strong>{{ selectedOrder.order_no }}</strong
              ><small>{{ selectedOrder.email }}</small>
            </div>
            <div>
              <span>{{ t("paymentops.paidAmount") }}</span
              ><strong>{{
                money(selectedOrder.total, selectedOrder.currency)
              }}</strong
              ><small>{{ statusLabel(selectedOrder.payment_status) }}</small>
            </div>
            <p>{{ selectedProducts }}</p>
            <button type="button" @click="selectedOrder = null">
              {{ t("paymentops.reselect") }}
            </button>
          </article>
          <div class="form-grid">
            <label
              ><span>{{ t("paymentops.refundAmount") }}</span
              ><input
                v-model="refundAmount"
                inputmode="decimal"
                :step="
                  selectedOrder
                    ? majorInputStep(selectedOrder.currency)
                    : undefined
                "
                :max="
                  selectedOrder
                    ? minorToMajor(selectedOrder.total, selectedOrder.currency)
                    : undefined
                "
                :placeholder="t('paymentops.refundAmountPlaceholder')"
              /><small>{{ t("paymentops.refundAmountHint") }}</small></label
            >
            <label
              ><span>{{ t("paymentops.refundMethod") }}</span>
              <div class="readonly-field">
                <ArrowDownToLine :size="14" />{{
                  t("paymentops.refundMethodValue")
                }}
              </div></label
            >
          </div>
          <label
            ><span>{{ t("paymentops.refundReason") }}</span
            ><textarea
              v-model="refundReason"
              maxlength="500"
              rows="4"
              :placeholder="t('paymentops.refundReasonPlaceholder')"
            ></textarea>
          </label>
          <div v-if="refundError" class="inline-error">
            <AlertCircle :size="14" />{{ refundError }}
          </div>
          <footer>
            <button
              type="button"
              class="secondary-button"
              :disabled="refundSaving"
              @click="closeRefundModal"
            >
              {{ t("paymentops.cancel") }}</button
            ><button
              type="submit"
              class="danger-button"
              :disabled="refundSaving || !selectedOrder"
            >
              <LoaderCircle
                v-if="refundSaving"
                :size="14"
                class="spinning"
              /><RotateCcw v-else :size="14" />{{
                refundSaving
                  ? t("paymentops.submitting")
                  : t("paymentops.confirmRefund")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>
  </section>
</template>

<style scoped>
.channel-currencies {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 6px;
}

.channel-currencies span {
  padding: 2px 6px;
  border: 1px solid var(--line);
  border-radius: 999px;
  color: var(--muted);
  font-size: 9px;
  font-weight: 700;
}

.field-hint {
  margin: 0 0 10px;
  color: var(--muted);
  font-size: 11px;
  line-height: 1.6;
}

.currency-checkbox-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}

.currency-checkbox-grid label {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: 8px;
  padding: 9px;
  border: 1px solid var(--line);
  border-radius: 7px;
  cursor: pointer;
}

.currency-checkbox-grid label.selected {
  border-color: var(--dark);
  background: var(--surface-2);
}

.currency-checkbox-grid label span {
  display: grid;
  gap: 2px;
}

.currency-checkbox-grid label small {
  color: var(--muted);
  font-size: 10px;
}

.payment-operations {
  display: grid;
  gap: 14px;
  color: var(--text);
}
.payment-topbar,
.payment-toolbar,
.payment-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.payment-topbar {
  min-height: 42px;
}
.payment-tabs {
  display: flex;
  align-items: center;
  gap: 3px;
  padding: 3px;
  overflow-x: auto;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
}
.payment-tabs button,
.icon-button,
.row-action,
.copy-address,
.payment-pagination button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  border: 0;
  color: var(--muted);
  background: transparent;
  cursor: pointer;
}
.payment-tabs button {
  height: 32px;
  padding: 0 11px;
  border-radius: 6px;
  font: inherit;
  font-size: 10px;
  white-space: nowrap;
}
.payment-tabs button.active {
  color: var(--surface);
  background: var(--text);
}
.payment-primary-actions {
  display: flex;
  align-items: center;
  gap: 7px;
}
.primary-button,
.secondary-button,
.danger-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 36px;
  padding: 0 13px;
  border: 1px solid var(--text);
  border-radius: 7px;
  font: inherit;
  font-size: 10px;
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
.primary-button:disabled,
.secondary-button:disabled,
.danger-button:disabled,
.icon-button:disabled,
.payment-pagination button:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.icon-button {
  width: 36px;
  height: 36px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface);
}
.payment-summary-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 9px;
}
.payment-summary-grid article {
  min-height: 96px;
  padding: 15px;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
}
.payment-summary-grid span {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  font-size: 9px;
}
.payment-summary-grid strong {
  display: block;
  margin-top: 10px;
  font-size: 18px;
  letter-spacing: -0.02em;
}
.payment-summary-grid small {
  display: block;
  margin-top: 5px;
  color: var(--muted);
  font-size: 8px;
}
.payment-alert {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 10px 12px;
  border: 1px solid;
  border-radius: 7px;
  font-size: 9px;
}
.payment-alert span {
  flex: 1;
}
.payment-alert button {
  border: 0;
  color: inherit;
  background: transparent;
  font: inherit;
  cursor: pointer;
}
.success-alert {
  color: #166534;
  border-color: #86efac;
  background: #f0fdf4;
}
.error-alert {
  color: #991b1b;
  border-color: #fecaca;
  background: #fef2f2;
}
:global([data-theme="dark"]) .success-alert {
  color: #bbf7d0;
  border-color: #166534;
  background: #052e16;
}
:global([data-theme="dark"]) .error-alert {
  color: #fecaca;
  border-color: #7f1d1d;
  background: #450a0a;
}
.payment-toolbar {
  padding: 10px 12px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
}
.payment-toolbar label {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  font-size: 9px;
}
.payment-toolbar select,
.payment-toolbar input {
  height: 31px;
  padding: 0 28px 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 9px;
}
.payment-toolbar input {
  width: min(310px, 50vw);
  padding: 0 9px;
}
.payment-toolbar p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
}
.payment-table-shell {
  min-height: 270px;
  overflow-x: auto;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
}
.payment-table {
  width: 100%;
  min-width: 900px;
  border-collapse: collapse;
}
.payment-table th {
  padding: 11px 13px;
  color: var(--muted);
  border-bottom: 1px solid var(--line);
  background: var(--soft);
  font-size: 8px;
  font-weight: 600;
  text-align: left;
  white-space: nowrap;
}
.payment-table td {
  padding: 12px 13px;
  border-bottom: 1px solid var(--line);
  font-size: 9px;
  vertical-align: middle;
}
.payment-table tr:last-child td {
  border-bottom: 0;
}
.payment-table td > strong,
.payment-table td > code,
.payment-table td > small {
  display: block;
}
.payment-table td > strong {
  font-size: 10px;
}
.payment-table td > code {
  margin-top: 4px;
  color: var(--muted);
  font-size: 8px;
  background: transparent;
}
.payment-table td > small {
  max-width: 220px;
  margin-top: 4px;
  overflow: hidden;
  color: var(--muted);
  font-size: 8px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.status-chip {
  display: inline-flex;
  align-items: center;
  min-height: 23px;
  padding: 0 7px;
  border: 1px solid var(--line);
  border-radius: 999px;
  font-size: 8px;
  white-space: nowrap;
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
.row-action,
.copy-address {
  min-height: 28px;
  padding: 0 7px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  font-size: 8px;
  background: var(--surface);
}
.direction {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.payment-empty {
  min-height: 270px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 30px;
  color: var(--muted);
  text-align: center;
}
.payment-empty strong {
  color: var(--text);
  font-size: 11px;
}
.payment-empty span {
  font-size: 9px;
}
.payment-pagination {
  color: var(--muted);
  font-size: 9px;
}
.payment-pagination > div {
  display: flex;
  gap: 6px;
}
.payment-pagination button {
  min-height: 31px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 9px;
}
.payment-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 80;
  display: grid;
  place-items: center;
  padding: 20px;
  background: rgb(0 0 0 / 55%);
  backdrop-filter: blur(2px);
}
.payment-modal {
  width: min(720px, 100%);
  max-height: min(850px, calc(100vh - 40px));
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: var(--surface);
  box-shadow: 0 25px 80px rgb(0 0 0 / 35%);
}
.refund-modal {
  width: min(760px, 100%);
}
.payment-modal > header {
  position: sticky;
  top: 0;
  z-index: 2;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 17px 19px;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
}
.payment-modal > header > div {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}
.modal-icon {
  display: grid;
  width: 35px;
  height: 35px;
  place-items: center;
  flex: 0 0 auto;
  border-radius: 8px;
  color: var(--surface);
  background: var(--text);
}
.payment-modal h2 {
  margin: 0;
  font-size: 14px;
}
.payment-modal header p {
  margin: 5px 0 0;
  color: var(--muted);
  font-size: 8px;
}
.payment-modal header > button {
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
.payment-form {
  display: grid;
  gap: 14px;
  padding: 18px 19px;
}
.payment-form label {
  display: grid;
  gap: 6px;
  color: var(--muted);
  font-size: 9px;
}
.payment-form label > span,
.payment-form legend {
  color: var(--text);
  font-weight: 650;
}
.payment-form input,
.payment-form textarea,
.readonly-field {
  width: 100%;
  box-sizing: border-box;
  border: 1px solid var(--line);
  border-radius: 7px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 10px;
  outline: none;
}
.payment-form input {
  height: 38px;
  padding: 0 10px;
}
.payment-form textarea {
  resize: vertical;
  padding: 10px;
  line-height: 1.6;
}
.payment-form input:focus,
.payment-form textarea:focus {
  border-color: var(--text);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--text) 10%, transparent);
}
.payment-form input:disabled {
  color: var(--muted);
  background: var(--soft);
}
.payment-form label small {
  color: var(--muted);
  font-size: 8px;
  line-height: 1.5;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.switch-line {
  grid-template-columns: auto minmax(0, 1fr);
  align-items: flex-start;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--soft);
  cursor: pointer;
}
.switch-line input {
  width: 15px;
  height: 15px;
  margin: 1px 0 0;
}
.switch-line span {
  display: grid;
  gap: 3px;
}
.payment-form fieldset {
  display: grid;
  gap: 12px;
  margin: 0;
  padding: 13px;
  border: 1px solid var(--line);
  border-radius: 8px;
}
.payment-form legend {
  padding: 0 6px;
  font-size: 9px;
}
.inline-error {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  padding: 9px 10px;
  border: 1px solid #fecaca;
  border-radius: 6px;
  color: #991b1b;
  background: #fef2f2;
  font-size: 9px;
}
:global([data-theme="dark"]) .inline-error {
  color: #fecaca;
  border-color: #7f1d1d;
  background: #450a0a;
}
.payment-form footer {
  position: sticky;
  bottom: -18px;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin: 2px -19px -18px;
  padding: 13px 19px;
  border-top: 1px solid var(--line);
  background: var(--surface);
}
.search-control {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  border: 1px solid var(--line);
  border-radius: 7px;
  padding-left: 10px;
}
.search-control input {
  border: 0;
}
.search-control button {
  height: 30px;
  margin-right: 4px;
  padding: 0 10px;
  border: 0;
  border-radius: 5px;
  color: var(--surface);
  background: var(--text);
  font: inherit;
  font-size: 9px;
  cursor: pointer;
}
.order-options {
  display: grid;
  gap: 6px;
  max-height: 220px;
  overflow-y: auto;
}
.order-options button {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
  color: var(--text);
  background: var(--surface);
  text-align: left;
  cursor: pointer;
}
.order-options button:hover {
  border-color: var(--text);
}
.order-options span {
  display: grid;
  gap: 4px;
}
.order-options span:last-child {
  text-align: right;
}
.order-options small {
  color: var(--muted);
  font-size: 8px;
}
.selected-order {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px 24px;
  padding: 13px;
  border: 1px solid var(--text);
  border-radius: 8px;
  background: var(--soft);
}
.selected-order div {
  display: grid;
  gap: 4px;
}
.selected-order div:nth-child(2) {
  text-align: right;
}
.selected-order span,
.selected-order small,
.selected-order p {
  color: var(--muted);
  font-size: 8px;
}
.selected-order strong {
  font-size: 11px;
}
.selected-order p {
  grid-column: 1 / -1;
  margin: 0;
  padding-right: 80px;
  line-height: 1.6;
}
.selected-order > button {
  position: absolute;
  right: 12px;
  bottom: 11px;
  border: 0;
  color: var(--text);
  background: transparent;
  font: inherit;
  font-size: 8px;
  text-decoration: underline;
  cursor: pointer;
}
.readonly-field {
  display: flex;
  align-items: center;
  gap: 7px;
  height: 38px;
  padding: 0 10px;
  background: var(--soft);
}
.spinning {
  animation: payment-spin 0.8s linear infinite;
}
@keyframes payment-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 760px) {
  .currency-checkbox-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .payment-topbar {
    align-items: stretch;
    flex-direction: column;
  }
  .payment-tabs {
    width: 100%;
    box-sizing: border-box;
  }
  .payment-tabs button {
    flex: 1 0 auto;
    min-height: 40px;
  }
  .payment-primary-actions {
    justify-content: flex-end;
  }
  .payment-summary-grid {
    grid-template-columns: 1fr;
  }
  .payment-summary-grid article {
    min-height: 78px;
  }
  .payment-modal-backdrop {
    padding: 8px;
  }
  .payment-modal {
    max-height: calc(100dvh - 16px);
  }
}

@media (max-width: 560px) {
  .payment-primary-actions .primary-button {
    flex: 1;
  }
  .payment-summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .payment-summary-grid article:last-child {
    grid-column: 1 / -1;
  }
  .payment-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .payment-toolbar label {
    justify-content: space-between;
  }
  .payment-table-shell {
    overflow: visible;
    border: 0;
    background: transparent;
  }
  .payment-table {
    min-width: 0;
    display: block;
  }
  .payment-table thead {
    display: none;
  }
  .payment-table tbody,
  .payment-table tr,
  .payment-table td {
    display: block;
    width: 100%;
    box-sizing: border-box;
  }
  .payment-table tr {
    margin-bottom: 9px;
    padding: 8px 11px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--surface);
  }
  .payment-table td {
    min-height: 35px;
    padding: 9px 0 9px 104px;
    position: relative;
    border-bottom: 1px solid var(--line);
  }
  .payment-table td:last-child {
    border-bottom: 0;
  }
  .payment-table td::before {
    content: attr(data-label);
    position: absolute;
    left: 0;
    top: 11px;
    width: 94px;
    color: var(--muted);
    font-size: 8px;
  }
  .row-action,
  .copy-address {
    min-height: 40px;
  }
  .payment-pagination {
    align-items: flex-start;
    flex-direction: column;
  }
  .payment-pagination > div {
    width: 100%;
  }
  .payment-pagination button {
    flex: 1;
  }
  .payment-modal-backdrop {
    padding: 0;
  }
  .payment-modal {
    max-height: 100dvh;
    height: 100dvh;
    border-radius: 0;
  }
  .payment-modal > header,
  .payment-form {
    padding-right: 14px;
    padding-left: 14px;
  }
  .form-grid {
    grid-template-columns: 1fr;
  }
  .payment-form input,
  .primary-button,
  .secondary-button,
  .danger-button,
  .icon-button {
    min-height: 42px;
  }
  .payment-form footer {
    margin-right: -14px;
    margin-left: -14px;
    padding-right: 14px;
    padding-left: 14px;
  }
  .selected-order {
    grid-template-columns: 1fr;
  }
  .selected-order div:nth-child(2) {
    text-align: left;
  }
  .selected-order p {
    padding-right: 0;
  }
  .selected-order > button {
    position: static;
    justify-self: start;
  }
}
</style>
