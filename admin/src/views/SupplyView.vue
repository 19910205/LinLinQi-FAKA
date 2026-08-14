<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  AlertCircle,
  ArrowRightLeft,
  Boxes,
  Check,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Edit3,
  HeartPulse,
  KeyRound,
  Link2,
  LoaderCircle,
  PackageSearch,
  Plus,
  Power,
  RefreshCw,
  Search,
  ServerCog,
  ShieldCheck,
  Trash2,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import SupplierCatalogManager from "../components/SupplierCatalogManager.vue";
import {
  type CurrencyDefinition,
  formatMoney as formatMinorMoney,
  fetchPublicCurrencyDirectory,
  majorInputStep,
  majorToMinor,
  minorToMajor,
  minorToSafeNumber,
  registerCurrencies,
  storeCurrency,
} from "../utils/money";
import { validSupplierExternalID } from "../utils/supplierIdentity";

type SupplyTab = "supplier" | "mapping" | "procurement";
type ModalKind =
  | "supplier"
  | "supplier-status"
  | "supplier-sync"
  | "supplier-sync-all"
  | "supplier-delete"
  | "mapping"
  | "mapping-delete"
  | "procurement";

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface Supplier {
  id: string;
  name: string;
  code: string;
  base_url: string;
  protocol: string;
  status: string;
  balance: number;
  balance_currency: string;
  price_currency: string;
  price_minor_unit: number;
  currency_mode: "auto" | "manual";
  balance_synced_at?: string | null;
  health_status: "unknown" | "healthy" | "degraded" | "unreachable";
  last_probe_at?: string | null;
  last_probe_latency_ms: number;
  last_probe_error?: string;
  credentials_configured: boolean;
  credential_fields: string[];
  callback_url?: string;
  last_sync_at?: string | null;
  sync_interval_minutes: number;
  sync_price: boolean;
  sync_stock: boolean;
  created_at: string;
  updated_at: string;
}

interface ProtocolCredentialField {
  key: string;
  label: string;
  type: "text" | "password";
  required: boolean;
  secret: boolean;
  min_length: number;
  max_length: number;
  placeholder?: string;
  help?: string;
}

interface SupplierProtocol {
  code: string;
  name: string;
  family: string;
  auth_mode: string;
  availability: "supported" | "limited" | "unavailable" | "reference_only";
  capabilities: string[];
  credential_fields: ProtocolCredentialField[];
  supports_discovery: boolean;
  notes?: string;
}

interface CatalogVariant {
  id: string;
  product_id: string;
  sku: string;
  name: string;
  price: number;
  status: string;
  currency?: string;
}

interface CatalogInputField {
  id: string;
  key: string;
  label: string;
  input_type: string;
  required: boolean;
  pass_to_supplier: boolean;
}

interface CatalogProduct {
  id: string;
  name: string;
  slug: string;
  price: number;
  status: string;
  currency: string;
  variants: CatalogVariant[];
  input_fields?: CatalogInputField[];
}

interface ProductMapping {
  id: string;
  supplier_id: string;
  supplier_name: string;
  supplier_code: string;
  product_id: string;
  product_name: string;
  product_status: string;
  variant_id?: string | null;
  variant_name: string;
  variant_sku: string;
  external_product_id: string;
  supplier_category_mapping_id?: string | null;
  inherit_category_policy: boolean;
  parameter_mapping?: Record<string, string> | null;
  price_mode: string;
  markup_basis_point: number;
  markup_amount: number;
  markup_currency: string;
  fixed_price: number;
  fixed_price_currency: string;
  auto_sync_price: boolean;
  auto_sync_stock: boolean;
  auto_sync_title: boolean;
  auto_sync_summary: boolean;
  auto_sync_description: boolean;
  auto_sync_media: boolean;
  mirror_remote_media: boolean;
  auto_sync_category: boolean;
  auto_sync_variants: boolean;
  auto_sync_status: boolean;
  auto_sync_limits: boolean;
  last_synced_at?: string | null;
  last_error: string;
  latest_external_price?: number | null;
  latest_external_currency?: string;
  latest_external_stock?: number | null;
  created_at: string;
  updated_at: string;
}

interface ParameterMappingRow {
  id: number;
  localKey: string;
  upstreamKey: string;
}

interface Procurement {
  id: string;
  procurement_no: string;
  supplier_id: string;
  supplier_name: string;
  supplier_code: string;
  order_id: string;
  order_no: string;
  order_item_id: string;
  product_name: string;
  variant_name: string;
  external_order_no: string;
  external_product_id: string;
  quantity: number;
  cost_amount: number;
  cost_currency: string;
  upstream_currency?: string;
  status: string;
  attempts: number;
  next_poll_at?: string | null;
  completed_at?: string | null;
  retry_message: string;
  callback_status?: string;
  callback_received_at?: string | null;
  callback_processed_at?: string | null;
  created_at: string;
  updated_at: string;
}

interface OrderSummary {
  id: string;
  order_no: string;
  status: string;
  payment_status: string;
  subtotal: number;
  discount: number;
  total: number;
  currency: string;
  paid_at?: string | null;
  delivered_at?: string | null;
  created_at: string;
}

interface OrderItemSummary {
  id: string;
  product_id: string;
  variant_id?: string | null;
  product_name: string;
  variant_name: string;
  unit_price: number;
  quantity: number;
  currency: string;
}

interface ProcurementDetail {
  procurement: Procurement;
  order: OrderSummary;
  item: OrderItemSummary;
}

interface SupplierForm {
  name: string;
  code: string;
  baseURL: string;
  protocol: string;
  rotateCredentials: boolean;
  credentials: Record<string, string>;
  priceCurrency: string;
  priceMinorUnit: number;
  balanceCurrency: string;
  currencyMode: "auto" | "manual";
  syncIntervalMinutes: number;
  reason: string;
}

interface MappingForm {
  supplierID: string;
  productID: string;
  variantID: string;
  externalProductID: string;
  parameterMappings: ParameterMappingRow[];
  priceMode: "fixed_markup" | "fixed_amount" | "fixed_price";
  markupPercent: number;
  markupAmountMajor: string;
  fixedPriceMajor: string;
  autoSyncPrice: boolean;
  autoSyncStock: boolean;
  autoSyncTitle: boolean;
  autoSyncSummary: boolean;
  autoSyncDescription: boolean;
  autoSyncMedia: boolean;
  mirrorRemoteMedia: boolean;
  autoSyncCategory: boolean;
  autoSyncVariants: boolean;
  autoSyncStatus: boolean;
  autoSyncLimits: boolean;
  reason: string;
}

const parameterMappingLimit = 20;
const localParameterKeyPattern = /^[a-z][a-z0-9_]{0,63}$/;
const upstreamParameterKeyPattern = /^[A-Za-z][A-Za-z0-9_.:-]{0,63}$/;
let nextParameterMappingRowID = 0;

function createParameterMappingRow(
  localKey = "",
  upstreamKey = "",
): ParameterMappingRow {
  nextParameterMappingRowID += 1;
  return { id: nextParameterMappingRowID, localKey, upstreamKey };
}

function parameterMappingRows(
  mapping?: Record<string, string> | null,
): ParameterMappingRow[] {
  if (!mapping || typeof mapping !== "object" || Array.isArray(mapping)) {
    return [];
  }
  return Object.entries(mapping).map(([localKey, upstreamKey]) =>
    createParameterMappingRow(localKey, String(upstreamKey ?? "")),
  );
}

const route = useRoute();
const { t, te, locale } = useI18n();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("supplier.manage"));

function supplyText(key: string, fallback: string) {
  return te(key) ? t(key) : fallback;
}

const supplierStatusLabels: Record<string, string> = {
  active: "supply.statusActive",
  disabled: "supply.statusDisabled",
};
const productStatusLabels: Record<string, string> = {
  draft: "supply.statusDraft",
  on_sale: "supply.statusOnSale",
  off_sale: "supply.statusOffSale",
};
const procurementStatusLabels: Record<string, string> = {
  creating: "supply.statusCreating",
  dispatching: "supply.statusDispatching",
  processing: "supply.statusUpstreamProcessing",
  retrying: "supply.statusRetrying",
  completed: "supply.statusProcurementCompleted",
  failed: "supply.statusProcurementFailed",
  cancelled: "supply.statusCancelled",
};
const orderStatusLabels: Record<string, string> = {
  pending: "supply.statusPending",
  pending_payment: "supply.statusPendingPayment",
  processing: "supply.statusProcessing",
  paid: "supply.statusPaid",
  completed: "supply.statusCompleted",
  delivered: "supply.statusDelivered",
  failed: "supply.statusFailed",
  cancelled: "supply.statusCancelled",
  refunded: "supply.statusRefunded",
};
const paymentStatusLabels: Record<string, string> = {
  unpaid: "supply.statusUnpaid",
  paid: "supply.statusPaid",
  refunded: "supply.statusRefunded",
  partially_refunded: "supply.statusPartiallyRefunded",
};
const supplierStatusOptions = [
  { value: "", label: "supply.supplierStatusAll" },
  { value: "active", label: "supply.statusActive" },
  { value: "disabled", label: "supply.statusDisabled" },
];
const procurementStatusOptions = [
  { value: "", label: "supply.procurementStatusAll" },
  ...Object.entries(procurementStatusLabels).map(([value, key]) => ({
    value,
    label: key,
  })),
];

function statusLabel(map: Record<string, string>, value: string) {
  const key = map[value];
  return key ? t(key) : value;
}

function resolveTab(value: unknown): SupplyTab {
  return value === "mapping" || value === "procurement" ? value : "supplier";
}

function emptySupplierForm(): SupplierForm {
  return {
    name: "",
    code: "",
    baseURL: "",
    protocol: "linlinqi-standard",
    rotateCredentials: true,
    credentials: {},
    priceCurrency: storeCurrency.value,
    priceMinorUnit: 2,
    balanceCurrency: storeCurrency.value,
    currencyMode: "auto",
    syncIntervalMinutes: 15,
    reason: "",
  };
}

function emptyMappingForm(): MappingForm {
  return {
    supplierID: "",
    productID: "",
    variantID: "",
    externalProductID: "",
    parameterMappings: [],
    priceMode: "fixed_markup",
    markupPercent: 10,
    markupAmountMajor: "0.00",
    fixedPriceMajor: "1.00",
    autoSyncPrice: true,
    autoSyncStock: true,
    autoSyncTitle: false,
    autoSyncSummary: false,
    autoSyncDescription: false,
    autoSyncMedia: false,
    mirrorRemoteMedia: true,
    autoSyncCategory: false,
    autoSyncVariants: false,
    autoSyncStatus: false,
    autoSyncLimits: false,
    reason: "",
  };
}

const activeTab = ref<SupplyTab>(resolveTab(route.meta.defaultTab));
const suppliers = ref<Supplier[]>([]);
const supplierProtocols = ref<SupplierProtocol[]>([]);
const protocolsLoading = ref(false);
const protocolsError = ref("");
const mappings = ref<ProductMapping[]>([]);
const procurements = ref<Procurement[]>([]);
const supplierDirectory = ref<Supplier[]>([]);
const catalogDirectory = ref<CatalogProduct[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const searchInput = ref("");
const appliedSearch = ref("");
const statusFilter = ref("");
const supplierFilter = ref("");
const priceModeFilter = ref("");
const loading = ref(false);
const loadError = ref("");
const directoryLoading = ref(false);
const directoryError = ref("");
const notice = ref("");
const noticeTone = ref<"success" | "error">("success");
const modalKind = ref<ModalKind | null>(null);
const editingSupplier = ref<Supplier | null>(null);
const catalogSupplier = ref<Supplier | null>(null);
const editingMapping = ref<ProductMapping | null>(null);
const selectedProcurement = ref<Procurement | null>(null);
const procurementDetail = ref<ProcurementDetail | null>(null);
const detailLoading = ref(false);
const detailError = ref("");
const saving = ref(false);
const probingSupplierID = ref("");
const formError = ref("");
const actionReason = ref("");
const recoveryMode = ref<"" | "retry" | "manual">("");
const recoveryEvidence = ref("");
const recoveryReason = ref("");
const manualDeliveries = ref("");
const manualCostYuan = ref("0.00");
const supplierForm = ref<SupplierForm>(emptySupplierForm());
const currencyDirectory = ref<CurrencyDefinition[]>([]);
const mappingForm = ref<MappingForm>(emptyMappingForm());
let listRequest = 0;
let detailRequest = 0;
let noticeTimer: ReturnType<typeof setTimeout> | undefined;

const selectedProtocol = computed(() =>
  supplierProtocols.value.find(
    (protocol) => protocol.code === supplierForm.value.protocol,
  ),
);
const mappingCurrency = computed(
  () =>
    catalogDirectory.value.find(
      (product) => product.id === mappingForm.value.productID,
    )?.currency ||
    editingMapping.value?.fixed_price_currency ||
    storeCurrency.value,
);
function supplierPriceCurrency(supplierID: string) {
  return (
    suppliers.value.find((supplier) => supplier.id === supplierID)
      ?.price_currency ||
    supplierDirectory.value.find((supplier) => supplier.id === supplierID)
      ?.price_currency ||
    storeCurrency.value
  );
}

function openSupplierCatalog(item: Supplier) {
  if (!supplierCatalogAvailable(item)) {
    showNotice(supplierCatalogGateReason(item));
    return;
  }
  catalogSupplier.value = item;
}

function supplierCatalogProtocol(item: Supplier) {
  return supplierProtocols.value.find(
    (protocol) => protocol.code === item.protocol,
  );
}

function supplierCatalogAvailable(item: Supplier) {
  const protocol = supplierCatalogProtocol(item);
  return Boolean(
    item.status === "active" &&
    item.credentials_configured &&
    protocol &&
    protocol.supports_discovery &&
    protocol.availability === "supported",
  );
}

function supplierCatalogGateReason(item: Supplier) {
  if (item.status !== "active") {
    return supplyText("supply.catalogRequiresActive", "请先启用供应商");
  }
  if (!item.credentials_configured) {
    return supplyText(
      "supply.catalogRequiresCredentials",
      "请先配置供应商凭证",
    );
  }
  const protocol = supplierCatalogProtocol(item);
  if (!protocol) {
    return protocolsLoading.value
      ? supplyText("supply.catalogProtocolsLoading", "正在读取协议能力")
      : supplyText("supply.catalogProtocolUnknown", "未找到该供应商的协议定义");
  }
  if (protocol.availability !== "supported") {
    return supplyText(
      "supply.catalogProtocolUnavailable",
      "该协议未达到可执行支持级别，不能进行生产目录接入",
    );
  }
  if (!protocol.supports_discovery) {
    return supplyText(
      "supply.catalogDiscoveryUnavailable",
      "该协议不支持远端分类与商品发现",
    );
  }
  return "";
}

async function loadProtocols() {
  if (protocolsLoading.value || supplierProtocols.value.length) return;
  protocolsLoading.value = true;
  protocolsError.value = "";
  try {
    const { data } = await adminApi.get("/supplier-protocols");
    const payload = data?.data ?? data;
    supplierProtocols.value = Array.isArray(payload) ? payload : [];
    if (!supplierProtocols.value.length) {
      throw new Error(t("supply.protocolErrProtocolsEmpty"));
    }
  } catch (error) {
    protocolsError.value = apiMessage(
      error,
      t("supply.protocolErrProtocolsLoad"),
    );
  } finally {
    protocolsLoading.value = false;
  }
}

async function loadCurrencies() {
  if (currencyDirectory.value.length) return;
  try {
    const payload = await fetchPublicCurrencyDirectory();
    currencyDirectory.value = Array.isArray(payload.items)
      ? payload.items.filter((item) => item.enabled !== false)
      : [];
    registerCurrencies(payload.items || [], payload.store_currency);
  } catch (error) {
    protocolsError.value = apiMessage(
      error,
      t("currency.supply.errorLoadCurrencies"),
    );
  }
}

const activeItemsCount = computed(() => {
  if (activeTab.value === "supplier") return suppliers.value.length;
  if (activeTab.value === "mapping") return mappings.value.length;
  return procurements.value.length;
});
const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const pageNumbers = computed(() => {
  const first = Math.max(1, Math.min(page.value - 2, totalPages.value - 4));
  const last = Math.min(totalPages.value, first + 4);
  return Array.from({ length: last - first + 1 }, (_, index) => first + index);
});
const selectedProduct = computed(() =>
  catalogDirectory.value.find(
    (product) => product.id === mappingForm.value.productID,
  ),
);
const variantOptions = computed(() => selectedProduct.value?.variants || []);
const supplierInputFields = computed(() =>
  (selectedProduct.value?.input_fields || []).filter(
    (field) =>
      field.pass_to_supplier && localParameterKeyPattern.test(field.key.trim()),
  ),
);
const mappedLocalKeys = computed(
  () =>
    new Set(
      mappingForm.value.parameterMappings.map((row) => row.localKey.trim()),
    ),
);
const selectedProductRequiresVariant = computed(() =>
  variantOptions.value.some((variant) => variant.status === "active"),
);
const targetSupplierStatus = computed(() =>
  editingSupplier.value?.status === "active" ? "disabled" : "active",
);
const procurementRecoverable = computed(() =>
  ["failed", "cancelled"].includes(
    procurementDetail.value?.procurement.status || "",
  ),
);
const modalTitle = computed(() => {
  switch (modalKind.value) {
    case "supplier":
      return editingSupplier.value
        ? t("supply.modalTitleEditSupplier")
        : t("supply.modalTitleAddSupplier");
    case "supplier-status":
      return targetSupplierStatus.value === "active"
        ? t("supply.modalTitleEnableSupplier")
        : t("supply.modalTitleDisableSupplier");
    case "supplier-sync":
      return t("supply.modalTitleManualSync");
    case "supplier-sync-all":
      return `${t("supply.fullCatalogSync", "全量目录同步")} · ${t("supply.allSuppliers")}`;
    case "supplier-delete":
      return `${t("supply.delete")} · ${
        editingSupplier.value?.name || t("supply.supplier")
      }`;
    case "mapping":
      return editingMapping.value
        ? t("supply.modalTitleEditMapping")
        : t("supply.modalTitleCreateMapping");
    case "mapping-delete":
      return t("supply.modalTitleDeleteMapping");
    case "procurement":
      return t("supply.modalTitleProcurement");
    default:
      return t("supply.modalTitleDefault");
  }
});

function apiMessage(error: unknown, fallback: string) {
  const failure = error as { response?: { data?: { message?: string } } };
  const value = failure.response?.data?.message || "";
  return value.startsWith("error.") ? fallback : value || fallback;
}

function extractPage<T>(responseData: unknown): PagePayload<T> {
  const envelope = responseData as { data?: Partial<PagePayload<T>> };
  const payload = envelope?.data || {};
  return {
    items: Array.isArray(payload.items) ? payload.items : [],
    total: Number(payload.total || 0),
    page: Number(payload.page || 1),
    page_size: Number(payload.page_size || pageSize.value),
  };
}

function formatMoney(value?: number | bigint | null, currency?: string) {
  return formatMinorMoney(value, currency || storeCurrency.value, locale.value);
}

function formatTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString("zh-CN", {
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
  return value.length > 16 ? `${value.slice(0, 8)}…${value.slice(-4)}` : value;
}

function formatMarkup(value: number) {
  return `${(Number(value || 0) / 100).toLocaleString("zh-CN", {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  })}%`;
}

function reasonLength(value: string) {
  return Array.from(value.trim()).length;
}

function validReason(value: string) {
  const length = reasonLength(value);
  return length >= 4 && length <= 500;
}

function normalizeBaseURL(value: string) {
  return value.trim().replace(/\/+$/, "");
}

function validSupplierURL(value: string) {
  try {
    const parsed = new URL(normalizeBaseURL(value));
    return (
      parsed.protocol === "https:" &&
      !parsed.username &&
      !parsed.password &&
      !parsed.search &&
      !parsed.hash &&
      Boolean(parsed.hostname)
    );
  } catch {
    return false;
  }
}

function supplierReadyForActivation(item: Supplier) {
  return Boolean(
    item.credentials_configured &&
    item.last_probe_at &&
    item.health_status === "healthy",
  );
}

function supplierFormMutatesConnection(form: SupplierForm) {
  const current = editingSupplier.value;
  if (!current) return true;
  return (
    form.rotateCredentials ||
    normalizeBaseURL(form.baseURL) !== current.base_url ||
    form.protocol !== current.protocol ||
    form.priceCurrency !== current.price_currency ||
    form.priceMinorUnit !== current.price_minor_unit ||
    form.balanceCurrency !== current.balance_currency ||
    form.currencyMode !== current.currency_mode
  );
}

function validCredentialInput(value: string, minimum: number, maximum: number) {
  const length = Array.from(value).length;
  return (
    length >= minimum &&
    length <= maximum &&
    value.trim() === value &&
    !/[\u0000-\u001f\u007f]/.test(value)
  );
}

function showNotice(message: string, tone: "success" | "error" = "success") {
  notice.value = message;
  noticeTone.value = tone;
  if (noticeTimer) clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => {
    notice.value = "";
  }, 6000);
}

function currentItems<T>() {
  if (activeTab.value === "supplier") return suppliers.value as T[];
  if (activeTab.value === "mapping") return mappings.value as T[];
  return procurements.value as T[];
}

async function loadList() {
  const request = ++listRequest;
  loading.value = true;
  loadError.value = "";
  try {
    const params: Record<string, string | number> = {
      page: page.value,
      page_size: pageSize.value,
    };
    if (appliedSearch.value) params.q = appliedSearch.value;
    let endpoint = "/suppliers";
    if (activeTab.value === "supplier") {
      if (statusFilter.value) params.status = statusFilter.value;
    } else if (activeTab.value === "mapping") {
      endpoint = "/operations/mappings";
      if (supplierFilter.value) params.supplier_id = supplierFilter.value;
      if (priceModeFilter.value) params.price_mode = priceModeFilter.value;
    } else {
      endpoint = "/operations/procurements";
      if (supplierFilter.value) params.supplier_id = supplierFilter.value;
      if (statusFilter.value) params.status = statusFilter.value;
    }
    const { data } = await adminApi.get(endpoint, { params });
    if (request !== listRequest) return;
    const payload = extractPage<Supplier | ProductMapping | Procurement>(data);
    total.value = payload.total;
    page.value = payload.page;
    pageSize.value = payload.page_size;
    if (activeTab.value === "supplier")
      suppliers.value = payload.items as Supplier[];
    if (activeTab.value === "mapping")
      mappings.value = payload.items as ProductMapping[];
    if (activeTab.value === "procurement")
      procurements.value = payload.items as Procurement[];
  } catch (error) {
    if (request !== listRequest) return;
    loadError.value = apiMessage(error, t("supply.errLoadList"));
  } finally {
    if (request === listRequest) loading.value = false;
  }
}

async function fetchAllPages<T>(endpoint: string) {
  const result: T[] = [];
  let directoryPage = 1;
  const pageLimit = 50;
  for (;;) {
    const { data } = await adminApi.get(endpoint, {
      params: { page: directoryPage, page_size: 100 },
    });
    const payload = extractPage<T>(data);
    result.push(...payload.items);
    if (!payload.items.length || result.length >= payload.total) break;
    if (directoryPage >= pageLimit) {
      throw new Error(t("supply.errDirectoryTooLarge"));
    }
    directoryPage += 1;
  }
  return result;
}

async function loadDirectories(force = false) {
  if (directoryLoading.value) return;
  const needsCatalog =
    activeTab.value === "mapping" || modalKind.value === "mapping";
  const needsSuppliers =
    activeTab.value !== "supplier" || modalKind.value === "mapping";
  if (
    !force &&
    (!needsSuppliers || supplierDirectory.value.length) &&
    (!needsCatalog || catalogDirectory.value.length)
  ) {
    return;
  }
  directoryLoading.value = true;
  directoryError.value = "";
  try {
    const tasks: Promise<void>[] = [];
    if (needsSuppliers && (force || !supplierDirectory.value.length)) {
      tasks.push(
        fetchAllPages<Supplier>("/suppliers").then((items) => {
          supplierDirectory.value = items;
        }),
      );
    }
    if (needsCatalog && (force || !catalogDirectory.value.length)) {
      tasks.push(
        fetchAllPages<CatalogProduct>("/supply/catalog").then((items) => {
          catalogDirectory.value = items;
        }),
      );
    }
    await Promise.all(tasks);
  } catch (error) {
    directoryError.value =
      error instanceof Error &&
      error.message === t("supply.errDirectoryTooLarge")
        ? error.message
        : apiMessage(error, t("supply.errLoadDirectories"));
  } finally {
    directoryLoading.value = false;
  }
}

function resetListState() {
  listRequest += 1;
  page.value = 1;
  total.value = 0;
  searchInput.value = "";
  appliedSearch.value = "";
  statusFilter.value = "";
  supplierFilter.value = "";
  priceModeFilter.value = "";
  loadError.value = "";
  notice.value = "";
}

function applySearch() {
  appliedSearch.value = searchInput.value.trim();
  page.value = 1;
  void loadList();
}

function clearSearch() {
  searchInput.value = "";
  appliedSearch.value = "";
  page.value = 1;
  void loadList();
}

function applyFilters() {
  page.value = 1;
  void loadList();
}

function changePage(next: number) {
  if (next < 1 || next > totalPages.value || next === page.value) return;
  page.value = next;
  void loadList();
}

function changePageSize() {
  page.value = 1;
  void loadList();
}

function openSupplier(item?: Supplier) {
  if (!canManage.value) return;
  editingSupplier.value = item || null;
  supplierForm.value = item
    ? {
        name: item.name,
        code: item.code,
        baseURL: item.base_url,
        protocol: item.protocol,
        rotateCredentials: false,
        credentials: {},
        priceCurrency: item.price_currency || storeCurrency.value,
        priceMinorUnit: Number.isInteger(item.price_minor_unit)
          ? item.price_minor_unit
          : 2,
        balanceCurrency:
          item.balance_currency || item.price_currency || storeCurrency.value,
        currencyMode: item.currency_mode === "manual" ? "manual" : "auto",
        syncIntervalMinutes: item.sync_interval_minutes || 15,
        reason: "",
      }
    : emptySupplierForm();
  formError.value = "";
  modalKind.value = "supplier";
  void Promise.all([loadProtocols(), loadCurrencies()]);
}

function handlePriceCurrencyChange() {
  const definition = currencyDirectory.value.find(
    (item) => item.code === supplierForm.value.priceCurrency,
  );
  if (definition) supplierForm.value.priceMinorUnit = definition.minor_unit;
}

function handleProtocolChange() {
  supplierForm.value.credentials = {};
  if (editingSupplier.value) {
    supplierForm.value.rotateCredentials =
      supplierForm.value.protocol !== editingSupplier.value.protocol;
  }
  formError.value = "";
}

function openSupplierStatus(item: Supplier) {
  if (!canManage.value) return;
  if (item.status !== "active" && !supplierReadyForActivation(item)) {
    showNotice(
      supplyText(
        "supply.enableRequiresHealthyProbe",
        "启用前必须先完成只读连接检查并达到健康状态",
      ),
      "error",
    );
    return;
  }
  editingSupplier.value = item;
  actionReason.value = "";
  formError.value = "";
  modalKind.value = "supplier-status";
}

function openSupplierSync(item: Supplier) {
  if (!canManage.value) return;
  editingSupplier.value = item;
  actionReason.value = "";
  formError.value = "";
  modalKind.value = "supplier-sync";
}

function openSupplierSyncAll() {
  if (!canManage.value) return;
  editingSupplier.value = null;
  actionReason.value = "";
  formError.value = "";
  modalKind.value = "supplier-sync-all";
}

function openSupplierDelete(item: Supplier) {
  if (!canManage.value) return;
  if (item.status !== "disabled") {
    showNotice(
      t(
        "supply.deleteSupplierRequiresDisabled",
        "请先停用共享店铺，再执行删除",
      ),
    );
    return;
  }
  editingSupplier.value = item;
  actionReason.value = "";
  formError.value = "";
  modalKind.value = "supplier-delete";
}

async function openMapping(item?: ProductMapping) {
  if (!canManage.value) return;
  editingMapping.value = item || null;
  mappingForm.value = item
    ? {
        supplierID: item.supplier_id,
        productID: item.product_id,
        variantID: item.variant_id || "",
        externalProductID: item.external_product_id,
        parameterMappings: parameterMappingRows(item.parameter_mapping),
        priceMode:
          item.price_mode === "fixed_price"
            ? "fixed_price"
            : item.price_mode === "fixed_amount"
              ? "fixed_amount"
              : "fixed_markup",
        markupPercent: Number(item.markup_basis_point || 0) / 100,
        markupAmountMajor: minorToMajor(
          item.markup_amount || 0,
          item.markup_currency || item.fixed_price_currency,
        ),
        fixedPriceMajor: minorToMajor(
          item.fixed_price || 0,
          item.fixed_price_currency,
        ),
        autoSyncPrice: item.auto_sync_price,
        autoSyncStock: item.auto_sync_stock,
        autoSyncTitle: item.auto_sync_title,
        autoSyncSummary: item.auto_sync_summary,
        autoSyncDescription: item.auto_sync_description,
        autoSyncMedia: item.auto_sync_media,
        mirrorRemoteMedia: item.mirror_remote_media,
        autoSyncCategory: item.auto_sync_category,
        autoSyncVariants: item.auto_sync_variants,
        autoSyncStatus: item.auto_sync_status,
        autoSyncLimits: item.auto_sync_limits,
        reason: "",
      }
    : emptyMappingForm();
  formError.value = "";
  modalKind.value = "mapping";
  await loadDirectories();
}

function openMappingDelete(item: ProductMapping) {
  if (!canManage.value) return;
  editingMapping.value = item;
  actionReason.value = "";
  formError.value = "";
  modalKind.value = "mapping-delete";
}

async function openProcurement(item: Procurement) {
  selectedProcurement.value = item;
  procurementDetail.value = null;
  detailError.value = "";
  modalKind.value = "procurement";
  resetProcurementRecovery();
  await loadProcurementDetail();
}

function resetProcurementRecovery() {
  recoveryMode.value = "";
  recoveryEvidence.value = "";
  recoveryReason.value = "";
  manualDeliveries.value = "";
  manualCostYuan.value = "0.00";
}

function recoveryEvidenceValid(value: string) {
  const length = Array.from(value.trim()).length;
  return length >= 4 && length <= 1000;
}

async function submitProcurementRecovery() {
  if (!canManage.value) return;
  const detail = procurementDetail.value;
  if (!detail || !procurementRecoverable.value || !recoveryMode.value) return;
  if (!recoveryEvidenceValid(recoveryEvidence.value)) {
    formError.value = t("supply.errRecoveryEvidence");
    return;
  }
  if (!validReason(recoveryReason.value)) {
    formError.value = t("supply.errReasonLength");
    return;
  }
  const headers = { "X-Change-Reason": recoveryReason.value.trim() };
  const evidence = recoveryEvidence.value.trim();
  let payload: Record<string, unknown> = { evidence };
  let path = `/operations/procurements/${encodeURIComponent(detail.procurement.id)}/retry`;
  if (recoveryMode.value === "manual") {
    const deliveries = manualDeliveries.value
      .split(/\r?\n/)
      .map((value) => value.trim())
      .filter(Boolean);
    let costAmount = 0;
    try {
      const exactCost = majorToMinor(
        manualCostYuan.value,
        detail.procurement.cost_currency,
      );
      const maximum = BigInt(
        majorToMinor("10000000", detail.procurement.cost_currency),
      );
      if (BigInt(exactCost) < 0n || BigInt(exactCost) > maximum)
        throw new Error("cost out of range");
      costAmount = minorToSafeNumber(exactCost);
    } catch {
      formError.value = t("supply.errManualDelivery", {
        count: detail.procurement.quantity,
      });
      return;
    }
    if (
      deliveries.length !== detail.procurement.quantity ||
      deliveries.some((value) => value.length > 64 * 1024)
    ) {
      formError.value = t("supply.errManualDelivery", {
        count: detail.procurement.quantity,
      });
      return;
    }
    payload = {
      evidence,
      deliveries,
      cost_amount: costAmount,
    };
    path = `/operations/procurements/${encodeURIComponent(detail.procurement.id)}/manual-complete`;
    // Full delivery values are held only long enough to build this request.
    manualDeliveries.value = "";
  }
  saving.value = true;
  formError.value = "";
  try {
    await adminApi.post(path, payload, { headers });
    const wasManual = recoveryMode.value === "manual";
    resetProcurementRecovery();
    showNotice(
      wasManual
        ? t("supply.noticeManualDelivery")
        : t("supply.noticeProcurementRetry"),
    );
    await Promise.all([loadProcurementDetail(), loadList()]);
  } catch (error) {
    formError.value = apiMessage(error, t("supply.errProcurementRecovery"));
  } finally {
    manualDeliveries.value = "";
    saving.value = false;
  }
}

async function loadProcurementDetail() {
  if (!selectedProcurement.value) return;
  const request = ++detailRequest;
  detailLoading.value = true;
  detailError.value = "";
  try {
    const { data } = await adminApi.get(
      `/operations/procurements/${encodeURIComponent(selectedProcurement.value.id)}`,
    );
    if (request !== detailRequest) return;
    procurementDetail.value = data?.data as ProcurementDetail;
  } catch (error) {
    if (request !== detailRequest) return;
    detailError.value = apiMessage(error, t("supply.errLoadDetail"));
  } finally {
    if (request === detailRequest) detailLoading.value = false;
  }
}

function closeModal() {
  if (saving.value) return;
  detailRequest += 1;
  modalKind.value = null;
  editingSupplier.value = null;
  editingMapping.value = null;
  selectedProcurement.value = null;
  procurementDetail.value = null;
  detailError.value = "";
  formError.value = "";
  actionReason.value = "";
  resetProcurementRecovery();
  supplierForm.value = emptySupplierForm();
  mappingForm.value = emptyMappingForm();
}

function validateSupplierForm() {
  const form = supplierForm.value;
  const nameLength = Array.from(form.name.trim()).length;
  if (nameLength < 2 || nameLength > 120) return t("supply.errNameLength");
  if (!/^[a-z0-9][a-z0-9_-]{1,59}$/.test(form.code.trim().toLowerCase())) {
    return t("supply.errCodeFormat");
  }
  if (!validSupplierURL(form.baseURL)) {
    return t("supply.errBaseUrl");
  }
  if (!selectedProtocol.value) return t("supply.protocolErrSelectProtocol");
  // Only protocols backed by an executable runtime can be saved or enabled.
  // "limited" and reference definitions remain visible for documentation,
  // but must never create a supplier that fails later in a sync worker.
  if (selectedProtocol.value.availability !== "supported") {
    return t("supply.protocolProtocolReferenceOnly");
  }
  const credentialsRequired = !editingSupplier.value || form.rotateCredentials;
  if (credentialsRequired) {
    for (const field of selectedProtocol.value.credential_fields) {
      const value = form.credentials[field.key] || "";
      if (
        field.required &&
        !validCredentialInput(value, field.min_length, field.max_length)
      ) {
        return t("supply.protocolErrCredentialFormat", { field: field.label });
      }
    }
  }
  if (
    !Number.isInteger(form.syncIntervalMinutes) ||
    form.syncIntervalMinutes < 5 ||
    form.syncIntervalMinutes > 10080
  ) {
    return t("supply.protocolErrSyncInterval");
  }
  const priceCurrency = currencyDirectory.value.find(
    (item) => item.code === form.priceCurrency,
  );
  const balanceCurrency = currencyDirectory.value.find(
    (item) => item.code === form.balanceCurrency,
  );
  if (!priceCurrency || !balanceCurrency)
    return t("currency.supply.errorCurrencyInvalid");
  if (form.priceMinorUnit !== priceCurrency.minor_unit) {
    return t("currency.supply.errorMinorUnitMismatch");
  }
  if (form.currencyMode !== "auto" && form.currencyMode !== "manual") {
    return t("currency.supply.errorCurrencyMode");
  }
  if (!validReason(form.reason)) return t("supply.errReasonLength");
  return "";
}

async function submitSupplier() {
  if (!canManage.value) return;
  const validation = validateSupplierForm();
  if (validation) {
    formError.value = validation;
    return;
  }
  saving.value = true;
  formError.value = "";
  const form = supplierForm.value;
  const payload: Record<string, unknown> = {
    name: form.name.trim(),
    code: form.code.trim().toLowerCase(),
    base_url: normalizeBaseURL(form.baseURL),
    protocol: form.protocol,
    price_currency: form.priceCurrency,
    price_minor_unit: form.priceMinorUnit,
    balance_currency: form.balanceCurrency,
    currency_mode: form.currencyMode,
    sync_interval_minutes: form.syncIntervalMinutes,
  };
  if (!editingSupplier.value || form.rotateCredentials) {
    payload.credentials = Object.fromEntries(
      (selectedProtocol.value?.credential_fields || []).map((field) => [
        field.key,
        form.credentials[field.key] || "",
      ]),
    );
  }
  try {
    const wasEditing = Boolean(editingSupplier.value);
    const rotated = Boolean(editingSupplier.value && form.rotateCredentials);
    const connectionMutation = supplierFormMutatesConnection(form);
    const headers = { "X-Change-Reason": form.reason.trim() };
    let savedSupplier: Supplier | undefined;
    if (editingSupplier.value) {
      const { data } = await adminApi.patch(
        `/suppliers/${encodeURIComponent(editingSupplier.value.id)}`,
        payload,
        { headers },
      );
      savedSupplier = (data?.data || data) as Supplier;
    } else {
      const { data } = await adminApi.post("/suppliers", payload, { headers });
      savedSupplier = (data?.data || data) as Supplier;
    }

    let activationError = "";
    if (connectionMutation) {
      try {
        if (!savedSupplier?.id) throw new Error("supplier id missing");
        const { data } = await adminApi.post(
          `/suppliers/${encodeURIComponent(savedSupplier.id)}/probe`,
          {},
          { headers },
        );
        const probe = data?.data || data;
        if (probe?.health_status !== "healthy") {
          throw new Error(
            supplyText(
              "supply.autoProbeNotHealthy",
              "只读连接检查未达到健康状态",
            ),
          );
        }
        await adminApi.patch(
          `/suppliers/${encodeURIComponent(savedSupplier.id)}`,
          { status: "active" },
          { headers },
        );
      } catch (error) {
        activationError = apiMessage(
          error,
          supplyText(
            "supply.autoProbeFailedDisabled",
            "连接参数已安全保存，但只读检查未通过；共享店铺保持停用，可修改后重试",
          ),
        );
      }
    }
    supplierDirectory.value = [];
    saving.value = false;
    closeModal();
    if (activationError) {
      showNotice(activationError, "error");
    } else {
      showNotice(
        connectionMutation
          ? supplyText(
              "supply.noticeSupplierVerifiedAndEnabled",
              "连接参数已保存，只读检查通过并已启用",
            )
          : wasEditing
            ? rotated
              ? t("supply.noticeSupplierRotated")
              : t("supply.noticeSupplierUpdated")
            : t("supply.noticeSupplierCreated"),
      );
    }
    await loadList();
  } catch (error) {
    formError.value = apiMessage(error, t("supply.errSaveSupplier"));
  } finally {
    saving.value = false;
  }
}

async function submitSupplierAction() {
  if (!canManage.value) return;
  if (!validReason(actionReason.value)) {
    formError.value = t("supply.errReasonLength");
    return;
  }
  if (modalKind.value !== "supplier-sync-all" && !editingSupplier.value) return;
  saving.value = true;
  formError.value = "";
  const item = editingSupplier.value;
  try {
    const headers = { "X-Change-Reason": actionReason.value.trim() };
    if (modalKind.value === "supplier-sync-all") {
      const { data } = await adminApi.post(
        "/suppliers/sync-all",
        {},
        { headers },
      );
      showNotice(
        data?.data?.status === "partial"
          ? `${t("supply.noticeSyncQueued")} · ${data.data.failed || 0} failed`
          : t("supply.noticeSyncQueued"),
      );
    } else if (modalKind.value === "supplier-status" && item) {
      await adminApi.patch(
        `/suppliers/${encodeURIComponent(item.id)}`,
        { status: targetSupplierStatus.value },
        { headers },
      );
      showNotice(
        targetSupplierStatus.value === "active"
          ? t("supply.noticeSupplierEnabled")
          : t("supply.noticeSupplierDisabled"),
      );
    } else if (item) {
      const { data } = await adminApi.post(
        `/suppliers/${encodeURIComponent(item.id)}/sync`,
        {},
        { headers },
      );
      const queueStatus = data?.data?.status;
      showNotice(
        queueStatus === "already_queued"
          ? t("supply.noticeSyncAlreadyQueued")
          : t("supply.noticeSyncQueued"),
      );
    }
    supplierDirectory.value = [];
    saving.value = false;
    closeModal();
    await loadList();
  } catch (error) {
    formError.value = apiMessage(error, t("supply.errSupplierAction"));
  } finally {
    saving.value = false;
  }
}

async function submitSupplierDelete() {
  if (!canManage.value) return;
  if (!editingSupplier.value || !validReason(actionReason.value)) {
    formError.value = t("supply.errReasonLength");
    return;
  }
  if (editingSupplier.value.status !== "disabled") {
    formError.value = t(
      "supply.deleteSupplierRequiresDisabled",
      "请先停用共享店铺，再执行删除",
    );
    return;
  }
  saving.value = true;
  formError.value = "";
  try {
    await adminApi.delete(
      `/suppliers/${encodeURIComponent(editingSupplier.value.id)}`,
      { headers: { "X-Change-Reason": actionReason.value.trim() } },
    );
    supplierDirectory.value = [];
    saving.value = false;
    closeModal();
    showNotice(
      supplyText("supply.noticeSupplierDeleted", "共享店铺已安全删除"),
    );
    if (suppliers.value.length === 1 && page.value > 1) page.value -= 1;
    await loadList();
  } catch (error) {
    formError.value = apiMessage(
      error,
      supplyText(
        "supply.errDeleteSupplier",
        "删除失败：只有已停用且没有同步、映射或采购历史的共享店铺可以删除",
      ),
    );
  } finally {
    saving.value = false;
  }
}

async function probeSupplier(item: Supplier) {
  if (!canManage.value) return;
  if (!item.credentials_configured || probingSupplierID.value) return;
  probingSupplierID.value = item.id;
  try {
    const { data } = await adminApi.post(
      `/suppliers/${encodeURIComponent(item.id)}/probe`,
      {},
      { headers: { "X-Change-Reason": "运营连接健康检查" } },
    );
    const probe = data?.data || data;
    const status = String(probe?.health_status || "unknown");
    showNotice(
      status === "healthy"
        ? t("supply.probeHealthy", "连接正常")
        : status === "degraded"
          ? t("supply.probeDegraded", "连接部分能力异常")
          : t("supply.probeFailed", "连接检查失败"),
    );
    await loadList();
  } catch (error) {
    showNotice(apiMessage(error, t("supply.probeFailed", "连接检查失败")));
  } finally {
    probingSupplierID.value = "";
  }
}

function validateMappingForm() {
  const form = mappingForm.value;
  if (!form.supplierID) return t("supply.errSelectSupplier");
  const product = catalogDirectory.value.find(
    (item) => item.id === form.productID,
  );
  if (!product) return t("supply.errSelectUpstreamProduct");
  const activeVariants = product.variants.filter(
    (variant) => variant.status === "active",
  );
  if (activeVariants.length && !form.variantID) {
    return t("supply.errSelectVariant");
  }
  if (
    form.variantID &&
    !activeVariants.some((variant) => variant.id === form.variantID)
  ) {
    return t("supply.errVariantInvalid");
  }
  if (!validSupplierExternalID(form.externalProductID)) {
    return t("supply.errExternalId");
  }
  if (form.parameterMappings.length > parameterMappingLimit) {
    return t("supply.errParameterMappingLimit", {
      count: parameterMappingLimit,
    });
  }
  const localKeys = new Set<string>();
  const upstreamKeys = new Set<string>();
  for (const row of form.parameterMappings) {
    const localKey = row.localKey.trim();
    const upstreamKey = row.upstreamKey.trim();
    if (!localKey || !upstreamKey) {
      return t("supply.errParameterMappingIncomplete");
    }
    if (!localParameterKeyPattern.test(localKey)) {
      return t("supply.errParameterLocalKeyFormat");
    }
    if (!upstreamParameterKeyPattern.test(upstreamKey)) {
      return t("supply.errParameterUpstreamKeyFormat");
    }
    if (localKeys.has(localKey)) {
      return t("supply.errParameterLocalKeyDuplicate", { key: localKey });
    }
    if (upstreamKeys.has(upstreamKey)) {
      return t("supply.errParameterUpstreamKeyDuplicate", {
        key: upstreamKey,
      });
    }
    localKeys.add(localKey);
    upstreamKeys.add(upstreamKey);
  }
  if (
    form.priceMode === "fixed_markup" &&
    (!Number.isFinite(Number(form.markupPercent)) ||
      Number(form.markupPercent) < 0 ||
      Number(form.markupPercent) > 1000)
  ) {
    return t("supply.errMarkupRange");
  }
  if (form.priceMode === "fixed_price") {
    try {
      const value = BigInt(
        majorToMinor(form.fixedPriceMajor, mappingCurrency.value),
      );
      const maximum = BigInt(majorToMinor("1000000", mappingCurrency.value));
      if (value <= 0n || value > maximum) throw new Error("price out of range");
    } catch {
      return t("supply.errFixedPriceRange");
    }
  }
  if (form.priceMode === "fixed_amount") {
    try {
      const value = BigInt(
        majorToMinor(form.markupAmountMajor, mappingCurrency.value),
      );
      const maximum = BigInt(majorToMinor("1000000", mappingCurrency.value));
      if (value < 0n || value > maximum) throw new Error("amount out of range");
    } catch {
      return t("supply.errMarkupAmountRange");
    }
  }
  if (!validReason(form.reason)) return t("supply.errReasonLength");
  return "";
}

async function submitMapping() {
  if (!canManage.value) return;
  const validation = validateMappingForm();
  if (validation) {
    formError.value = validation;
    return;
  }
  saving.value = true;
  formError.value = "";
  const form = mappingForm.value;
  const parameterMapping = Object.fromEntries(
    form.parameterMappings.map((row) => [
      row.localKey.trim(),
      row.upstreamKey.trim(),
    ]),
  );
  const payload = {
    supplier_id: form.supplierID,
    product_id: form.productID,
    variant_id: form.variantID || null,
    external_product_id: form.externalProductID.trim(),
    parameter_mapping: parameterMapping,
    price_mode: form.priceMode,
    markup_basis_point:
      form.priceMode === "fixed_markup"
        ? Math.round(Number(form.markupPercent) * 100)
        : 0,
    markup_amount:
      form.priceMode === "fixed_amount"
        ? minorToSafeNumber(
            majorToMinor(form.markupAmountMajor, mappingCurrency.value),
          )
        : 0,
    fixed_price:
      form.priceMode === "fixed_price"
        ? minorToSafeNumber(
            majorToMinor(form.fixedPriceMajor, mappingCurrency.value),
          )
        : 0,
    auto_sync_price: form.autoSyncPrice,
    auto_sync_stock: form.autoSyncStock,
    auto_sync_title: form.autoSyncTitle,
    auto_sync_summary: form.autoSyncSummary,
    auto_sync_description: form.autoSyncDescription,
    auto_sync_media: form.autoSyncMedia,
    mirror_remote_media: form.mirrorRemoteMedia,
    auto_sync_category: form.autoSyncCategory,
    auto_sync_variants: form.autoSyncVariants,
    auto_sync_status: form.autoSyncStatus,
    auto_sync_limits: form.autoSyncLimits,
  };
  try {
    const headers = { "X-Change-Reason": form.reason.trim() };
    if (editingMapping.value) {
      await adminApi.patch(
        `/operations/mappings/${encodeURIComponent(editingMapping.value.id)}`,
        payload,
        { headers },
      );
    } else {
      await adminApi.post("/operations/mappings", payload, { headers });
    }
    const edited = Boolean(editingMapping.value);
    saving.value = false;
    closeModal();
    showNotice(
      edited
        ? t("supply.noticeMappingUpdated")
        : t("supply.noticeMappingCreated"),
    );
    await loadList();
  } catch (error) {
    formError.value = apiMessage(error, t("supply.errSaveMapping"));
  } finally {
    saving.value = false;
  }
}

function addParameterMapping(localKey = "") {
  if (mappingForm.value.parameterMappings.length >= parameterMappingLimit) {
    formError.value = t("supply.errParameterMappingLimit", {
      count: parameterMappingLimit,
    });
    return;
  }
  if (localKey && mappedLocalKeys.value.has(localKey)) {
    formError.value = t("supply.errParameterLocalKeyDuplicate", {
      key: localKey,
    });
    return;
  }
  mappingForm.value.parameterMappings.push(
    createParameterMappingRow(localKey.trim()),
  );
  formError.value = "";
}

function removeParameterMapping(id: number) {
  mappingForm.value.parameterMappings =
    mappingForm.value.parameterMappings.filter((row) => row.id !== id);
  formError.value = "";
}

async function submitMappingDelete() {
  if (!canManage.value) return;
  if (!editingMapping.value || !validReason(actionReason.value)) {
    formError.value = t("supply.errDeleteReason");
    return;
  }
  saving.value = true;
  formError.value = "";
  try {
    await adminApi.delete(
      `/operations/mappings/${encodeURIComponent(editingMapping.value.id)}`,
      { headers: { "X-Change-Reason": actionReason.value.trim() } },
    );
    saving.value = false;
    closeModal();
    showNotice(t("supply.noticeMappingDeleted"));
    if (page.value > 1 && currentItems().length === 1) page.value -= 1;
    await loadList();
  } catch (error) {
    formError.value = apiMessage(error, t("supply.errDeleteMapping"));
  } finally {
    saving.value = false;
  }
}

function handleProductChange() {
  mappingForm.value.variantID = "";
  mappingForm.value.parameterMappings = [];
  formError.value = "";
}

function handleEscape(event: KeyboardEvent) {
  if (event.key === "Escape" && modalKind.value && !saving.value) closeModal();
}

watch(
  () => route.meta.defaultTab,
  (value) => {
    activeTab.value = resolveTab(value);
    resetListState();
    void Promise.all([
      loadList(),
      loadDirectories(),
      loadProtocols(),
      loadCurrencies(),
    ]);
  },
  { immediate: true },
);

watch(modalKind, (value) => {
  document.body.style.overflow = value ? "hidden" : "";
});

window.addEventListener("keydown", handleEscape);

onBeforeUnmount(() => {
  listRequest += 1;
  detailRequest += 1;
  window.removeEventListener("keydown", handleEscape);
  document.body.style.overflow = "";
  if (noticeTimer) clearTimeout(noticeTimer);
});
</script>

<template>
  <section class="supply-shell">
    <template v-if="!catalogSupplier">
      <div class="supply-panel panel">
        <header class="supply-toolbar">
          <form class="supply-search" @submit.prevent="applySearch">
            <Search :size="15" />
            <input
              v-model="searchInput"
              type="search"
              :placeholder="
                activeTab === 'supplier'
                  ? t('supply.searchPlaceholderSupplier')
                  : activeTab === 'mapping'
                    ? t('supply.searchPlaceholderMapping')
                    : t('supply.searchPlaceholderProcurement')
              "
              :aria-label="t('supply.ariaSearch')"
            />
            <button v-if="appliedSearch" type="button" @click="clearSearch">
              <X :size="13" />{{ t("supply.clear") }}
            </button>
            <button type="submit">{{ t("supply.search") }}</button>
          </form>

          <div class="supply-filters">
            <select
              v-if="activeTab !== 'supplier'"
              v-model="supplierFilter"
              :aria-label="t('supply.ariaSupplierFilter')"
              @change="applyFilters"
            >
              <option value="">{{ t("supply.allSuppliers") }}</option>
              <option
                v-for="item in supplierDirectory"
                :key="item.id"
                :value="item.id"
              >
                {{ item.name }}（{{ item.code }}）
              </option>
            </select>
            <select
              v-if="activeTab === 'mapping'"
              v-model="priceModeFilter"
              :aria-label="t('supply.ariaPriceModeFilter')"
              @change="applyFilters"
            >
              <option value="">{{ t("supply.allPriceModes") }}</option>
              <option value="fixed_markup">
                {{ t("supply.priceModeMarkup") }}
              </option>
              <option value="fixed_amount">
                {{ t("supply.priceModeAmount") }}
              </option>
              <option value="fixed_price">
                {{ t("supply.priceModeFixed") }}
              </option>
            </select>
            <select
              v-if="activeTab === 'supplier'"
              v-model="statusFilter"
              :aria-label="t('supply.ariaSupplierStatusFilter')"
              @change="applyFilters"
            >
              <option
                v-for="option in supplierStatusOptions"
                :key="option.value || 'all'"
                :value="option.value"
              >
                {{ t(option.label) }}
              </option>
            </select>
            <select
              v-if="activeTab === 'procurement'"
              v-model="statusFilter"
              :aria-label="t('supply.ariaProcurementStatusFilter')"
              @change="applyFilters"
            >
              <option
                v-for="option in procurementStatusOptions"
                :key="option.value || 'all'"
                :value="option.value"
              >
                {{ t(option.label) }}
              </option>
            </select>
            <button type="button" :disabled="loading" @click="loadList">
              <RefreshCw :size="14" :class="{ spinning: loading }" />{{
                t("supply.refresh")
              }}
            </button>
            <button
              v-if="activeTab === 'supplier' && canManage"
              type="button"
              :disabled="loading || !suppliers.length"
              @click="openSupplierSyncAll"
            >
              <RefreshCw :size="14" />{{
                t("supply.fullCatalogSync", "全量目录同步")
              }}
              ·
              {{ t("supply.allSuppliers") }}
            </button>
            <button
              v-if="activeTab === 'supplier' && canManage"
              type="button"
              class="primary-compact"
              @click="openSupplier()"
            >
              <Plus :size="14" />{{ t("supply.addSupplier") }}
            </button>
            <button
              v-if="activeTab === 'mapping' && canManage"
              type="button"
              class="primary-compact"
              @click="openMapping()"
            >
              <Plus :size="14" />{{ t("supply.createMapping") }}
            </button>
          </div>
        </header>

        <div
          v-if="notice"
          class="supply-notice"
          :class="noticeTone === 'error' ? 'error-notice' : 'success-notice'"
        >
          <AlertCircle v-if="noticeTone === 'error'" :size="15" />
          <Check v-else :size="15" />{{ notice }}
        </div>
        <div v-if="directoryError" class="supply-notice error-notice">
          <AlertCircle :size="15" />{{ directoryError }}
          <button type="button" @click="loadDirectories(true)">
            {{ t("supply.retryDirectory") }}
          </button>
        </div>
        <div v-if="loadError" class="supply-notice error-notice">
          <AlertCircle :size="15" />{{ loadError }}
          <button type="button" @click="loadList">
            {{ t("supply.retry") }}
          </button>
        </div>

        <div v-if="loading && !activeItemsCount" class="supply-state">
          <LoaderCircle class="spinning" :size="23" />
          <span>{{ t("supply.loadingList") }}</span>
        </div>
        <div v-else-if="!loadError && !activeItemsCount" class="supply-state">
          <ServerCog v-if="activeTab === 'supplier'" :size="28" />
          <Boxes v-else-if="activeTab === 'mapping'" :size="28" />
          <PackageSearch v-else :size="28" />
          <strong>
            {{
              appliedSearch || statusFilter || supplierFilter || priceModeFilter
                ? t("supply.noMatch")
                : activeTab === "supplier"
                  ? t("supply.noSuppliers")
                  : activeTab === "mapping"
                    ? t("supply.noMappings")
                    : t("supply.noProcurements")
            }}
          </strong>
          <span v-if="activeTab === 'mapping' && !appliedSearch">
            {{ t("supply.mappingHint") }}
          </span>
        </div>

        <div v-else-if="activeTab === 'supplier'" class="supply-table-wrap">
          <table class="supply-table supplier-table">
            <thead>
              <tr>
                <th>{{ t("supply.colSupplier") }}</th>
                <th>{{ t("supply.colConnection") }}</th>
                <th>{{ t("supply.colBalance") }}</th>
                <th>{{ t("supply.colCredential") }}</th>
                <th>{{ t("supply.colLastSync") }}</th>
                <th>{{ t("supply.colStatus") }}</th>
                <th>
                  <span class="sr-only">{{ t("supply.colActions") }}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in suppliers" :key="item.id">
                <td :data-label="t('supply.colSupplier')">
                  <div class="record-primary">
                    <span><ServerCog :size="15" /></span>
                    <div>
                      <b>{{ item.name }}</b>
                      <code :title="item.id"
                        >{{ item.code }} · {{ shortID(item.id) }}</code
                      >
                    </div>
                  </div>
                </td>
                <td :data-label="t('supply.colConnection')">
                  <b class="truncate" :title="item.base_url">{{
                    item.base_url
                  }}</b>
                  <small>{{ item.protocol }}</small>
                  <small class="currency-semantics">
                    {{
                      t("currency.supply.summary", {
                        currency: item.price_currency || storeCurrency,
                        mode: t(
                          item.currency_mode === "manual"
                            ? "currency.supply.modeManualShort"
                            : "currency.supply.modeAutoShort",
                        ),
                      })
                    }}
                  </small>
                  <small
                    v-if="item.callback_url"
                    class="callback-url"
                    :title="item.callback_url"
                    >{{ item.callback_url }}</small
                  >
                </td>
                <td :data-label="t('supply.colBalance')">
                  <template v-if="item.balance_synced_at">
                    <b>{{
                      formatMoney(item.balance, item.balance_currency)
                    }}</b>
                    <small>{{
                      t("supply.balanceSyncedAt", {
                        currency: item.balance_currency || storeCurrency,
                        time: formatTime(item.balance_synced_at),
                      })
                    }}</small>
                  </template>
                  <template v-else>
                    <b>—</b>
                    <small class="balance-unsynced">{{
                      t("supply.balanceNotSynced")
                    }}</small>
                  </template>
                </td>
                <td :data-label="t('supply.colCredential')">
                  <span
                    class="credential-state"
                    :class="{ configured: item.credentials_configured }"
                  >
                    <KeyRound :size="13" />
                    {{
                      item.credentials_configured
                        ? t("supply.credentialConfigured")
                        : t("supply.credentialNotConfigured")
                    }}
                  </span>
                  <small>{{ t("supply.credentialNeverShown") }}</small>
                </td>
                <td :data-label="t('supply.colLastSync')">
                  <div class="sync-flags">
                    <span :class="{ on: item.sync_price }">{{
                      t("supply.syncPriceFlag")
                    }}</span>
                    <span :class="{ on: item.sync_stock }">{{
                      t("supply.syncStockFlag")
                    }}</span>
                  </div>
                  <small>每 {{ item.sync_interval_minutes }} 分钟</small>
                  <time>{{ formatTime(item.last_sync_at) }}</time>
                  <small>{{
                    t("supply.updatedAt", { time: formatTime(item.updated_at) })
                  }}</small>
                </td>
                <td :data-label="t('supply.colStatus')">
                  <span class="status-badge" :class="`status-${item.status}`">
                    {{ statusLabel(supplierStatusLabels, item.status) }}
                  </span>
                  <span
                    class="health-badge"
                    :class="`health-${item.health_status || 'unknown'}`"
                    :title="item.last_probe_error || ''"
                  >
                    <HeartPulse :size="12" />
                    {{
                      item.health_status === "healthy"
                        ? t("supply.probeHealthy", "连接正常")
                        : item.health_status === "degraded"
                          ? t("supply.probeDegraded", "部分异常")
                          : item.health_status === "unreachable"
                            ? t("supply.probeFailed", "不可达")
                            : t("supply.probeUnknown", "未检查")
                    }}
                  </span>
                  <small v-if="item.last_probe_at">{{
                    t("supply.probeAt", {
                      time: formatTime(item.last_probe_at),
                    })
                  }}</small>
                  <small
                    v-else-if="item.status === 'disabled'"
                    class="balance-unsynced"
                    >待只读连接验证</small
                  >
                </td>
                <td :data-label="t('supply.colActions')" class="record-actions">
                  <button
                    type="button"
                    :disabled="!supplierCatalogAvailable(item)"
                    :title="supplierCatalogGateReason(item)"
                    @click="openSupplierCatalog(item)"
                  >
                    <PackageSearch :size="13" />{{
                      t("supply.catalogButton", "接入货源")
                    }}
                  </button>
                  <template v-if="canManage">
                    <button type="button" @click="openSupplier(item)">
                      <Edit3 :size="13" />{{ t("supply.edit") }}
                    </button>
                    <button
                      type="button"
                      :disabled="
                        !item.credentials_configured ||
                        probingSupplierID === item.id
                      "
                      @click="probeSupplier(item)"
                    >
                      <HeartPulse
                        :size="13"
                        :class="{ spinning: probingSupplierID === item.id }"
                      />{{ t("supply.probe", "测试连接") }}
                    </button>
                    <button
                      type="button"
                      :disabled="
                        item.status !== 'active' &&
                        !supplierReadyForActivation(item)
                      "
                      :title="
                        item.status !== 'active' &&
                        !supplierReadyForActivation(item)
                          ? supplyText(
                              'supply.enableRequiresHealthyProbe',
                              '请先完成只读连接检查并达到健康状态',
                            )
                          : ''
                      "
                      @click="openSupplierStatus(item)"
                    >
                      <Power :size="13" />
                      {{
                        item.status === "active"
                          ? t("supply.disable")
                          : t("supply.enable")
                      }}
                    </button>
                    <button
                      v-if="
                        item.status === 'active' && item.credentials_configured
                      "
                      type="button"
                      @click="openSupplierSync(item)"
                    >
                      <RefreshCw :size="13" />{{ t("supply.sync") }}
                    </button>
                    <button
                      type="button"
                      class="danger-action"
                      :disabled="item.status !== 'disabled'"
                      :title="
                        item.status === 'disabled'
                          ? supplyText(
                              'supply.deleteSupplierHint',
                              '仅无同步、映射与采购历史的共享店铺可删除',
                            )
                          : t(
                              'supply.deleteSupplierRequiresDisabled',
                              '请先停用共享店铺，再执行删除',
                            )
                      "
                      @click="openSupplierDelete(item)"
                    >
                      <Trash2 :size="13" />{{ t("supply.delete") }}
                    </button>
                  </template>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-else-if="activeTab === 'mapping'" class="supply-table-wrap">
          <table class="supply-table mapping-table">
            <thead>
              <tr>
                <th>{{ t("supply.colLocalProduct") }}</th>
                <th>{{ t("supply.colSupplierProduct") }}</th>
                <th>{{ t("supply.colPricing") }}</th>
                <th>{{ t("supply.colSyncPolicy") }}</th>
                <th>{{ t("supply.colUpstreamSnapshot") }}</th>
                <th>{{ t("supply.colSyncResult") }}</th>
                <th v-if="canManage">
                  <span class="sr-only">{{ t("supply.colActions") }}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in mappings" :key="item.id">
                <td :data-label="t('supply.colLocalProduct')">
                  <div class="record-primary">
                    <span><Boxes :size="15" /></span>
                    <div>
                      <b>{{ item.product_name }}</b>
                      <code :title="item.variant_id || item.product_id">
                        {{ item.variant_name || t("supply.defaultVariant") }}
                        <template v-if="item.variant_sku">
                          · {{ item.variant_sku }}</template
                        >
                      </code>
                    </div>
                  </div>
                  <span class="inline-status">{{
                    statusLabel(productStatusLabels, item.product_status)
                  }}</span>
                </td>
                <td :data-label="t('supply.colSupplierProduct')">
                  <b>{{ item.supplier_name }}</b>
                  <small>{{ item.supplier_code }}</small>
                  <code class="external-id" :title="item.external_product_id">{{
                    item.external_product_id
                  }}</code>
                </td>
                <td :data-label="t('supply.colPricing')">
                  <b v-if="item.price_mode === 'fixed_price'">{{
                    t("supply.fixedPriceValue", {
                      amount: formatMoney(
                        item.fixed_price,
                        item.fixed_price_currency,
                      ),
                    })
                  }}</b>
                  <b v-else-if="item.price_mode === 'fixed_amount'">{{
                    t("supply.upstreamPlusAmount", {
                      amount: formatMoney(
                        item.markup_amount,
                        item.markup_currency,
                      ),
                    })
                  }}</b>
                  <b v-else>{{
                    t("supply.upstreamPlusMarkup", {
                      percent: formatMarkup(item.markup_basis_point),
                    })
                  }}</b>
                  <small>{{
                    item.price_mode === "fixed_price"
                      ? t("supply.notFollowUpstream")
                      : t("supply.calcFromUpstream")
                  }}</small>
                </td>
                <td :data-label="t('supply.colSyncPolicy')">
                  <div class="sync-flags">
                    <span :class="{ on: item.auto_sync_price }">{{
                      t("supply.syncPriceFlag")
                    }}</span>
                    <span :class="{ on: item.auto_sync_stock }">{{
                      t("supply.syncStockFlag")
                    }}</span>
                  </div>
                  <small
                    v-if="item.inherit_category_policy"
                    class="policy-source"
                  >
                    <Link2 :size="11" />{{
                      supplyText(
                        "supply.categoryPolicyInherited",
                        "继承分类绑定规则",
                      )
                    }}
                  </small>
                </td>
                <td :data-label="t('supply.colUpstreamSnapshot')">
                  <b>{{
                    item.latest_external_price == null
                      ? "—"
                      : formatMoney(
                          item.latest_external_price,
                          item.latest_external_currency ||
                            supplierPriceCurrency(item.supplier_id),
                        )
                  }}</b>
                  <small>{{
                    t("supply.stockValue", {
                      count:
                        item.latest_external_stock == null
                          ? "—"
                          : item.latest_external_stock,
                    })
                  }}</small>
                </td>
                <td :data-label="t('supply.colSyncResult')">
                  <time>{{ formatTime(item.last_synced_at) }}</time>
                  <small :class="{ 'error-text': item.last_error }">{{
                    item.last_error || t("supply.noSyncError")
                  }}</small>
                </td>
                <td
                  v-if="canManage"
                  :data-label="t('supply.colActions')"
                  class="record-actions"
                >
                  <button type="button" @click="openMapping(item)">
                    <Edit3 :size="13" />{{ t("supply.edit") }}
                  </button>
                  <button
                    type="button"
                    class="danger-action"
                    @click="openMappingDelete(item)"
                  >
                    <Trash2 :size="13" />{{ t("supply.delete") }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-else class="supply-table-wrap">
          <table class="supply-table procurement-table">
            <thead>
              <tr>
                <th>{{ t("supply.colProcurement") }}</th>
                <th>{{ t("supply.colRelatedOrder") }}</th>
                <th>{{ t("supply.colSupplierUpstream") }}</th>
                <th>{{ t("supply.colQtyCost") }}</th>
                <th>{{ t("supply.colRetrySchedule") }}</th>
                <th>{{ t("supply.colStatus") }}</th>
                <th>
                  <span class="sr-only">{{ t("supply.colActions") }}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in procurements" :key="item.id">
                <td :data-label="t('supply.colProcurement')">
                  <div class="record-primary">
                    <span><PackageSearch :size="15" /></span>
                    <div>
                      <b>{{ item.procurement_no }}</b>
                      <code :title="item.id">{{ shortID(item.id) }}</code>
                    </div>
                  </div>
                  <small>{{ formatTime(item.created_at) }}</small>
                </td>
                <td :data-label="t('supply.colRelatedOrder')">
                  <b>{{ item.order_no }}</b>
                  <small
                    >{{ item.product_name
                    }}<template v-if="item.variant_name">
                      · {{ item.variant_name }}</template
                    ></small
                  >
                </td>
                <td :data-label="t('supply.colSupplierUpstream')">
                  <b>{{ item.supplier_name }}</b>
                  <small>{{
                    item.external_order_no || t("supply.upstreamOrderPending")
                  }}</small>
                  <code class="external-id" :title="item.external_product_id">{{
                    item.external_product_id
                  }}</code>
                </td>
                <td :data-label="t('supply.colQtyCost')">
                  <b>{{
                    t("supply.quantityPieces", { count: item.quantity })
                  }}</b>
                  <small>{{
                    formatMoney(item.cost_amount, item.cost_currency)
                  }}</small>
                </td>
                <td :data-label="t('supply.colRetrySchedule')">
                  <b>{{
                    t("supply.attemptsCount", { count: item.attempts })
                  }}</b>
                  <small>{{
                    t("supply.nextPollAt", {
                      time: formatTime(item.next_poll_at),
                    })
                  }}</small>
                  <span class="retry-message">{{ item.retry_message }}</span>
                  <small v-if="item.callback_status" class="callback-state">
                    ↩ {{ item.callback_status }} ·
                    {{
                      formatTime(
                        item.callback_processed_at || item.callback_received_at,
                      )
                    }}
                  </small>
                </td>
                <td :data-label="t('supply.colStatus')">
                  <span class="status-badge" :class="`status-${item.status}`">
                    {{ statusLabel(procurementStatusLabels, item.status) }}
                  </span>
                  <small v-if="item.completed_at">{{
                    formatTime(item.completed_at)
                  }}</small>
                </td>
                <td :data-label="t('supply.colActions')" class="record-actions">
                  <button type="button" @click="openProcurement(item)">
                    <Link2 :size="13" />{{ t("supply.detail") }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <footer v-if="!loadError" class="supply-pagination">
          <span>{{
            t("supply.paginationSummary", { page, totalPages, total })
          }}</span>
          <div>
            <button
              type="button"
              :aria-label="t('supply.ariaPrevPage')"
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
              :aria-label="t('supply.ariaNextPage')"
              :disabled="page >= totalPages || loading"
              @click="changePage(page + 1)"
            >
              <ChevronRight :size="14" />
            </button>
            <select
              v-model.number="pageSize"
              :aria-label="t('supply.ariaPageSize')"
              @change="changePageSize"
            >
              <option :value="10">
                {{ t("supply.pageSizeSuffix", { size: 10 }) }}
              </option>
              <option :value="20">
                {{ t("supply.pageSizeSuffix", { size: 20 }) }}
              </option>
              <option :value="50">
                {{ t("supply.pageSizeSuffix", { size: 50 }) }}
              </option>
            </select>
          </div>
        </footer>
      </div>

      <div
        v-if="modalKind && (modalKind === 'procurement' || canManage)"
        class="supply-modal-backdrop"
        role="presentation"
        @mousedown.self="closeModal"
      >
        <section
          class="supply-modal"
          :class="{ 'detail-modal': modalKind === 'procurement' }"
          role="dialog"
          aria-modal="true"
          :aria-label="modalTitle"
        >
          <header>
            <div>
              <span class="kicker">{{
                t("adminKicker.supplyOperations")
              }}</span>
              <h2>{{ modalTitle }}</h2>
              <p>
                {{
                  modalKind === "supplier"
                    ? t("supply.modalSubSupplier")
                    : modalKind === "mapping"
                      ? t("supply.modalSubMapping")
                      : modalKind === "procurement"
                        ? t("supply.modalSubProcurement")
                        : t("supply.modalSubAction")
                }}
              </p>
            </div>
            <button
              type="button"
              :aria-label="t('supply.ariaClose')"
              :disabled="saving"
              @click="closeModal"
            >
              <X :size="18" />
            </button>
          </header>

          <form
            v-if="modalKind === 'supplier'"
            class="supply-form"
            @submit.prevent="submitSupplier"
          >
            <div v-if="formError" class="form-alert error-notice">
              <AlertCircle :size="15" />{{ formError }}
            </div>
            <fieldset>
              <legend>{{ t("supply.supplierIdentity") }}</legend>
              <div class="form-grid two-columns">
                <label>
                  {{ t("supply.supplierName") }}
                  <input
                    v-model="supplierForm.name"
                    maxlength="120"
                    autocomplete="off"
                    :placeholder="t('supply.supplierNamePlaceholder')"
                    autofocus
                  />
                  <small>{{ t("supply.supplierNameHint") }}</small>
                </label>
                <label>
                  {{ t("supply.supplierCode") }}
                  <input
                    v-model="supplierForm.code"
                    maxlength="60"
                    autocomplete="off"
                    placeholder="apac_supplier"
                  />
                  <small>{{ t("supply.supplierCodeHint") }}</small>
                </label>
              </div>
              <label>
                {{ t("supply.baseUrl") }}
                <input
                  v-model="supplierForm.baseURL"
                  maxlength="500"
                  type="url"
                  autocomplete="off"
                  placeholder="https://supplier.example.com"
                />
                <small>{{ t("supply.baseUrlHint") }}</small>
              </label>
              <label>
                {{ t("supply.protocol") }}
                <select
                  v-model="supplierForm.protocol"
                  :disabled="protocolsLoading"
                  @change="handleProtocolChange"
                >
                  <option
                    v-for="protocol in supplierProtocols"
                    :key="protocol.code"
                    :value="protocol.code"
                    :disabled="protocol.availability !== 'supported'"
                  >
                    {{ protocol.name }} · {{ protocol.auth_mode }}
                    {{
                      protocol.availability === "limited"
                        ? t("supply.protocolProtocolLimited")
                        : ""
                    }}
                  </option>
                </select>
                <small v-if="protocolsError" class="field-error">{{
                  protocolsError
                }}</small>
                <small v-else-if="selectedProtocol">
                  {{ selectedProtocol.family }} ·
                  {{
                    selectedProtocol.capabilities.join(" / ") ||
                    t("supply.protocolProtocolNoCapabilities")
                  }}
                </small>
              </label>
              <label>
                {{ t("supply.protocolSyncIntervalLabel") }}
                <input
                  v-model.number="supplierForm.syncIntervalMinutes"
                  type="number"
                  min="5"
                  max="10080"
                  step="1"
                />
                <small>{{ t("supply.protocolSyncIntervalHint") }}</small>
              </label>
            </fieldset>

            <fieldset class="currency-fieldset">
              <legend>{{ t("currency.supply.configuration") }}</legend>
              <div class="currency-mode-grid">
                <label
                  :class="[
                    'currency-mode-card',
                    { selected: supplierForm.currencyMode === 'auto' },
                  ]"
                >
                  <input
                    v-model="supplierForm.currencyMode"
                    type="radio"
                    value="auto"
                  />
                  <span>
                    <b>{{ t("currency.supply.modeAuto") }}</b>
                    <small>{{ t("currency.supply.modeAutoHint") }}</small>
                  </span>
                </label>
                <label
                  :class="[
                    'currency-mode-card',
                    { selected: supplierForm.currencyMode === 'manual' },
                  ]"
                >
                  <input
                    v-model="supplierForm.currencyMode"
                    type="radio"
                    value="manual"
                  />
                  <span>
                    <b>{{ t("currency.supply.modeManual") }}</b>
                    <small>{{ t("currency.supply.modeManualHint") }}</small>
                  </span>
                </label>
              </div>
              <div class="form-grid two-columns">
                <label>
                  {{ t("currency.supply.priceCurrency") }}
                  <select
                    v-model="supplierForm.priceCurrency"
                    @change="handlePriceCurrencyChange"
                  >
                    <option
                      v-for="currency in currencyDirectory"
                      :key="currency.code"
                      :value="currency.code"
                    >
                      {{ currency.code }} · {{ currency.name }}
                    </option>
                  </select>
                  <small>{{
                    supplierForm.currencyMode === "auto"
                      ? t("currency.supply.priceCurrencyAutoHint")
                      : t("currency.supply.priceCurrencyManualHint")
                  }}</small>
                </label>
                <label>
                  {{ t("currency.supply.priceMinorUnit") }}
                  <input
                    v-model.number="supplierForm.priceMinorUnit"
                    type="number"
                    min="0"
                    max="6"
                    readonly
                  />
                  <small>{{ t("currency.supply.priceMinorUnitHint") }}</small>
                </label>
                <label>
                  {{ t("currency.supply.balanceCurrency") }}
                  <select v-model="supplierForm.balanceCurrency">
                    <option
                      v-for="currency in currencyDirectory"
                      :key="currency.code"
                      :value="currency.code"
                    >
                      {{ currency.code }} · {{ currency.name }}
                    </option>
                  </select>
                  <small>{{ t("currency.supply.balanceCurrencyHint") }}</small>
                </label>
              </div>
              <div class="currency-flow-note">
                <ArrowRightLeft :size="15" />
                <span>{{
                  t("currency.supply.conversionFlow", {
                    upstream: supplierForm.priceCurrency,
                  })
                }}</span>
              </div>
            </fieldset>

            <fieldset class="credential-fieldset">
              <legend>{{ t("supply.credentials") }}</legend>
              <div v-if="editingSupplier" class="credential-control">
                <div>
                  <KeyRound :size="17" />
                  <span>
                    <b>{{
                      editingSupplier.credentials_configured
                        ? t("supply.credentialConfiguredShort")
                        : t("supply.credentialIncomplete")
                    }}</b>
                    <small>{{ t("supply.credentialNotReturned") }}</small>
                  </span>
                </div>
                <label class="switch-label">
                  <input
                    v-model="supplierForm.rotateCredentials"
                    type="checkbox"
                  />
                  <span>{{ t("supply.rotateCredentials") }}</span>
                </label>
              </div>
              <div
                v-if="!editingSupplier || supplierForm.rotateCredentials"
                class="form-grid two-columns"
              >
                <label
                  v-for="field in selectedProtocol?.credential_fields || []"
                  :key="field.key"
                >
                  {{ field.label }}
                  <input
                    v-model="supplierForm.credentials[field.key]"
                    :type="field.secret ? 'password' : 'text'"
                    :minlength="field.min_length"
                    :maxlength="field.max_length"
                    :autocomplete="field.secret ? 'new-password' : 'off'"
                    :placeholder="field.placeholder || field.label"
                  />
                  <small>
                    {{
                      field.help ||
                      t("supply.protocolFieldCharRange", {
                        min: field.min_length,
                        max: field.max_length,
                      })
                    }}
                  </small>
                </label>
                <div
                  v-if="
                    selectedProtocol &&
                    !selectedProtocol.credential_fields.length
                  "
                  class="privacy-banner"
                >
                  {{ t("supply.protocolProtocolNoCredentialFields") }}
                </div>
              </div>
              <div class="privacy-banner">
                <ShieldCheck :size="15" />
                {{ t("supply.credentialPrivacy") }}
              </div>
            </fieldset>

            <fieldset>
              <legend>{{ t("supply.auditInfo") }}</legend>
              <label>
                {{ t("supply.changeReason") }}
                <textarea
                  v-model="supplierForm.reason"
                  maxlength="500"
                  :placeholder="t('supply.changeReasonPlaceholderSupplier')"
                ></textarea>
                <small>{{ reasonLength(supplierForm.reason) }} / 500</small>
              </label>
            </fieldset>
            <footer>
              <button type="button" :disabled="saving" @click="closeModal">
                {{ t("supply.cancel") }}
              </button>
              <button class="primary-button" type="submit" :disabled="saving">
                <LoaderCircle v-if="saving" class="spinning" :size="14" />
                <Check v-else :size="14" />{{
                  editingSupplier
                    ? t("supply.saveChanges")
                    : t("supply.confirmAdd")
                }}
              </button>
            </footer>
          </form>

          <form
            v-else-if="modalKind === 'mapping'"
            class="supply-form"
            @submit.prevent="submitMapping"
          >
            <div v-if="formError" class="form-alert error-notice">
              <AlertCircle :size="15" />{{ formError }}
            </div>
            <div v-if="directoryLoading" class="inline-loading">
              <LoaderCircle class="spinning" :size="17" />{{
                t("supply.loadingDirectories")
              }}
            </div>
            <div v-if="directoryError" class="form-alert error-notice">
              <AlertCircle :size="15" />{{ directoryError }}
              <button type="button" @click="loadDirectories(true)">
                {{ t("supply.reload") }}
              </button>
            </div>
            <fieldset>
              <legend>{{ t("supply.binding") }}</legend>
              <label>
                {{ t("supply.supplier") }}
                <select
                  v-model="mappingForm.supplierID"
                  :disabled="directoryLoading"
                  autofocus
                >
                  <option value="">{{ t("supply.selectSupplier") }}</option>
                  <option
                    v-for="item in supplierDirectory"
                    :key="item.id"
                    :value="item.id"
                  >
                    {{ item.name }}（{{ item.code }}）{{
                      item.status === "disabled"
                        ? t("supply.disabledSuffix")
                        : ""
                    }}
                  </option>
                </select>
              </label>
              <div class="form-grid two-columns">
                <label>
                  {{ t("supply.localProduct") }}
                  <select
                    v-model="mappingForm.productID"
                    :disabled="directoryLoading"
                    @change="handleProductChange"
                  >
                    <option value="">
                      {{ t("supply.selectUpstreamProduct") }}
                    </option>
                    <option
                      v-for="product in catalogDirectory"
                      :key="product.id"
                      :value="product.id"
                    >
                      {{ product.name }}（{{ product.slug }}）
                    </option>
                  </select>
                  <small>{{ t("supply.catalogHint") }}</small>
                </label>
                <label>
                  {{ t("supply.localVariant") }}
                  <select
                    v-model="mappingForm.variantID"
                    :disabled="!mappingForm.productID"
                  >
                    <option value="" :disabled="selectedProductRequiresVariant">
                      {{
                        selectedProductRequiresVariant
                          ? t("supply.selectVariantRequired")
                          : t("supply.defaultVariantMain")
                      }}
                    </option>
                    <option
                      v-for="variant in variantOptions"
                      :key="variant.id"
                      :value="variant.id"
                      :disabled="variant.status !== 'active'"
                    >
                      {{ variant.name
                      }}{{ variant.sku ? `（${variant.sku}）` : ""
                      }}{{
                        variant.status !== "active"
                          ? t("supply.disabledSuffix")
                          : ""
                      }}
                    </option>
                  </select>
                  <small>{{ t("supply.variantHint") }}</small>
                </label>
              </div>
              <label>
                {{ t("supply.externalProductId") }}
                <input
                  v-model="mappingForm.externalProductID"
                  maxlength="180"
                  autocomplete="off"
                  :placeholder="t('supply.externalProductIdPlaceholder')"
                />
                <small>{{ t("supply.externalProductIdHint") }}</small>
              </label>
            </fieldset>

            <fieldset>
              <legend>{{ t("supply.parameterMapping") }}</legend>
              <div class="parameter-mapping-heading">
                <div>
                  <b>{{ t("supply.parameterMappingTitle") }}</b>
                  <small>{{ t("supply.parameterMappingDescription") }}</small>
                </div>
                <button
                  type="button"
                  :disabled="
                    mappingForm.parameterMappings.length >=
                    parameterMappingLimit
                  "
                  @click="addParameterMapping()"
                >
                  <Plus :size="13" />{{ t("supply.addParameterMapping") }}
                </button>
              </div>
              <div
                v-if="supplierInputFields.length"
                class="local-key-suggestions"
              >
                <div>
                  <b>{{ t("supply.parameterLocalKeySuggestions") }}</b>
                  <small>{{
                    t("supply.parameterLocalKeySuggestionsHint")
                  }}</small>
                </div>
                <div>
                  <button
                    v-for="field in supplierInputFields"
                    :key="field.id"
                    type="button"
                    :disabled="
                      mappedLocalKeys.has(field.key) ||
                      mappingForm.parameterMappings.length >=
                        parameterMappingLimit
                    "
                    :title="
                      mappedLocalKeys.has(field.key)
                        ? t('supply.parameterLocalKeyMapped')
                        : t('supply.parameterLocalKeyAdd')
                    "
                    @click="addParameterMapping(field.key)"
                  >
                    <Plus v-if="!mappedLocalKeys.has(field.key)" :size="11" />
                    <Check v-else :size="11" />
                    <code>{{ field.key }}</code>
                    <span>{{ field.label }}</span>
                    <small v-if="field.required">{{
                      t("supply.parameterRequired")
                    }}</small>
                  </button>
                </div>
              </div>
              <div
                v-if="!mappingForm.parameterMappings.length"
                class="parameter-mapping-empty"
              >
                <ArrowRightLeft :size="17" />
                <span>{{ t("supply.parameterMappingEmpty") }}</span>
              </div>
              <div v-else class="parameter-mapping-list">
                <div
                  v-for="(row, index) in mappingForm.parameterMappings"
                  :key="row.id"
                  class="parameter-mapping-row"
                >
                  <label>
                    <span>{{ t("supply.parameterLocalKey") }}</span>
                    <input
                      v-model="row.localKey"
                      maxlength="64"
                      autocomplete="off"
                      :placeholder="t('supply.parameterLocalKeyPlaceholder')"
                    />
                  </label>
                  <ArrowRightLeft class="parameter-mapping-arrow" :size="15" />
                  <label>
                    <span>{{ t("supply.parameterUpstreamKey") }}</span>
                    <input
                      v-model="row.upstreamKey"
                      maxlength="64"
                      autocomplete="off"
                      :placeholder="t('supply.parameterUpstreamKeyPlaceholder')"
                    />
                  </label>
                  <button
                    type="button"
                    class="parameter-mapping-remove"
                    :aria-label="
                      t('supply.removeParameterMapping', { count: index + 1 })
                    "
                    :title="
                      t('supply.removeParameterMapping', { count: index + 1 })
                    "
                    @click="removeParameterMapping(row.id)"
                  >
                    <Trash2 :size="14" />
                  </button>
                </div>
              </div>
              <p class="parameter-mapping-meta">
                <span>{{ t("supply.parameterKeyFormat") }}</span>
                <b>{{
                  t("supply.parameterMappingCount", {
                    count: mappingForm.parameterMappings.length,
                    total: parameterMappingLimit,
                  })
                }}</b>
              </p>
              <p class="form-hint">
                {{ t("supply.parameterMappingBehavior") }}
              </p>
            </fieldset>

            <fieldset>
              <legend>{{ t("supply.pricingStrategy") }}</legend>
              <div class="choice-grid">
                <label
                  :class="{
                    selected: mappingForm.priceMode === 'fixed_markup',
                  }"
                >
                  <input
                    v-model="mappingForm.priceMode"
                    type="radio"
                    value="fixed_markup"
                  />
                  <span>
                    <b>{{ t("supply.priceModeMarkup") }}</b>
                    <small>{{ t("supply.priceModeMarkupDesc") }}</small>
                  </span>
                </label>
                <label
                  :class="{ selected: mappingForm.priceMode === 'fixed_price' }"
                >
                  <input
                    v-model="mappingForm.priceMode"
                    type="radio"
                    value="fixed_price"
                  />
                  <span>
                    <b>{{ t("supply.priceModeFixed") }}</b>
                    <small>{{ t("supply.priceModeFixedDesc") }}</small>
                  </span>
                </label>
                <label
                  :class="{
                    selected: mappingForm.priceMode === 'fixed_amount',
                  }"
                >
                  <input
                    v-model="mappingForm.priceMode"
                    type="radio"
                    value="fixed_amount"
                  />
                  <span>
                    <b>{{ t("supply.priceModeAmount") }}</b>
                    <small>{{ t("supply.priceModeAmountDesc") }}</small>
                  </span>
                </label>
              </div>
              <label v-if="mappingForm.priceMode === 'fixed_markup'">
                {{ t("supply.markupPercent") }}
                <input
                  v-model.number="mappingForm.markupPercent"
                  type="number"
                  min="0"
                  max="1000"
                  step="0.01"
                />
                <small>{{ t("supply.markupPercentHint") }}</small>
              </label>
              <label v-else-if="mappingForm.priceMode === 'fixed_amount'">
                {{ t("supply.markupAmount") }}
                <input
                  v-model="mappingForm.markupAmountMajor"
                  inputmode="decimal"
                  min="0"
                  max="1000000"
                  :step="majorInputStep(mappingCurrency)"
                />
                <small
                  >{{ t("supply.markupAmountHint") }} ·
                  {{ mappingCurrency }}</small
                >
              </label>
              <label v-else>
                {{ t("supply.fixedPrice") }}
                <input
                  v-model="mappingForm.fixedPriceMajor"
                  inputmode="decimal"
                  max="1000000"
                  :step="majorInputStep(mappingCurrency)"
                />
                <small
                  >{{ t("supply.fixedPriceHint") }} ·
                  {{ mappingCurrency }}</small
                >
              </label>
            </fieldset>

            <fieldset>
              <legend>{{ t("supply.syncPolicy") }}</legend>
              <div class="sync-choice-grid">
                <label>
                  <input v-model="mappingForm.autoSyncPrice" type="checkbox" />
                  <span>
                    <b>{{ t("supply.autoSyncPrice") }}</b>
                    <small>{{ t("supply.autoSyncPriceDesc") }}</small>
                  </span>
                </label>
                <label>
                  <input v-model="mappingForm.autoSyncStock" type="checkbox" />
                  <span>
                    <b>{{ t("supply.autoSyncStock") }}</b>
                    <small>{{ t("supply.autoSyncStockDesc") }}</small>
                  </span>
                </label>
                <label>
                  <input v-model="mappingForm.autoSyncTitle" type="checkbox" />
                  <span
                    ><b>{{ t("supply.protocolSyncTitle") }}</b
                    ><small>{{
                      t("supply.protocolSyncTitleDesc")
                    }}</small></span
                  >
                </label>
                <label>
                  <input
                    v-model="mappingForm.autoSyncSummary"
                    type="checkbox"
                  />
                  <span
                    ><b>{{ t("supply.protocolSyncSummaryLabel") }}</b
                    ><small>{{
                      t("supply.protocolSyncSummaryDesc")
                    }}</small></span
                  >
                </label>
                <label>
                  <input
                    v-model="mappingForm.autoSyncDescription"
                    type="checkbox"
                  />
                  <span
                    ><b>{{ t("supply.protocolSyncDescriptionLabel") }}</b
                    ><small>{{
                      t("supply.protocolSyncDescriptionDesc")
                    }}</small></span
                  >
                </label>
                <label>
                  <input v-model="mappingForm.autoSyncMedia" type="checkbox" />
                  <span
                    ><b>{{ t("supply.protocolSyncImagesLabel") }}</b
                    ><small>{{
                      t("supply.protocolSyncImagesDesc")
                    }}</small></span
                  >
                </label>
                <label>
                  <input
                    v-model="mappingForm.mirrorRemoteMedia"
                    type="checkbox"
                    :disabled="!mappingForm.autoSyncMedia"
                  />
                  <span
                    ><b>{{ t("supply.protocolSyncImagesLocalizeLabel") }}</b
                    ><small>{{
                      t("supply.protocolSyncImagesLocalizeDesc")
                    }}</small></span
                  >
                </label>
                <label>
                  <input
                    v-model="mappingForm.autoSyncCategory"
                    type="checkbox"
                  />
                  <span
                    ><b>{{ t("supply.protocolSyncCategoryLabel") }}</b
                    ><small>{{
                      t("supply.protocolSyncCategoryDesc")
                    }}</small></span
                  >
                </label>
                <label>
                  <input
                    v-model="mappingForm.autoSyncVariants"
                    type="checkbox"
                  />
                  <span
                    ><b>{{ t("supply.protocolSyncVariantsLabel") }}</b
                    ><small>{{
                      t("supply.protocolSyncVariantsDesc")
                    }}</small></span
                  >
                </label>
                <label>
                  <input v-model="mappingForm.autoSyncStatus" type="checkbox" />
                  <span
                    ><b>{{ t("supply.protocolSyncListingLabel") }}</b
                    ><small>{{
                      t("supply.protocolSyncListingDesc")
                    }}</small></span
                  >
                </label>
                <label>
                  <input v-model="mappingForm.autoSyncLimits" type="checkbox" />
                  <span
                    ><b>{{ t("supply.protocolSyncPurchaseLimitLabel") }}</b
                    ><small>{{
                      t("supply.protocolSyncPurchaseLimitDesc")
                    }}</small></span
                  >
                </label>
              </div>
              <p class="form-hint">
                {{ t("supply.syncConflictHint") }}
              </p>
            </fieldset>

            <fieldset>
              <legend>{{ t("supply.auditInfo") }}</legend>
              <label>
                {{ t("supply.changeReason") }}
                <textarea
                  v-model="mappingForm.reason"
                  maxlength="500"
                  :placeholder="t('supply.changeReasonPlaceholderMapping')"
                ></textarea>
                <small>{{ reasonLength(mappingForm.reason) }} / 500</small>
              </label>
            </fieldset>
            <footer>
              <button type="button" :disabled="saving" @click="closeModal">
                {{ t("supply.cancel") }}
              </button>
              <button
                class="primary-button"
                type="submit"
                :disabled="
                  saving || directoryLoading || Boolean(directoryError)
                "
              >
                <LoaderCircle v-if="saving" class="spinning" :size="14" />
                <Check v-else :size="14" />{{
                  editingMapping
                    ? t("supply.savePolicy")
                    : t("supply.createMapping")
                }}
              </button>
            </footer>
          </form>

          <form
            v-else-if="
              modalKind === 'supplier-status' ||
              modalKind === 'supplier-sync' ||
              modalKind === 'supplier-sync-all'
            "
            class="supply-form compact-form"
            @submit.prevent="submitSupplierAction"
          >
            <div v-if="formError" class="form-alert error-notice">
              <AlertCircle :size="15" />{{ formError }}
            </div>
            <section v-if="editingSupplier" class="identity-card">
              <span><ServerCog :size="18" /></span>
              <div>
                <b>{{ editingSupplier.name }}</b>
                <code>{{ editingSupplier.code }}</code>
              </div>
              <span
                class="status-badge"
                :class="`status-${editingSupplier.status}`"
              >
                {{ statusLabel(supplierStatusLabels, editingSupplier.status) }}
              </span>
            </section>
            <div class="action-explanation">
              <RefreshCw
                v-if="
                  modalKind === 'supplier-sync' ||
                  modalKind === 'supplier-sync-all'
                "
                :size="19"
              />
              <Power v-else :size="19" />
              <div>
                <b>
                  {{
                    modalKind === "supplier-sync" ||
                    modalKind === "supplier-sync-all"
                      ? modalKind === "supplier-sync-all"
                        ? t("supply.fullCatalogSync", "全量目录同步")
                        : t("supply.syncSnapshot")
                      : targetSupplierStatus === "active"
                        ? t("supply.resumeSupply")
                        : t("supply.stopSync")
                  }}
                </b>
                <p>
                  {{
                    modalKind === "supplier-sync" ||
                    modalKind === "supplier-sync-all"
                      ? modalKind === "supplier-sync-all"
                        ? supplyText(
                            "supply.fullCatalogSyncHint",
                            "将为所有已启用共享店铺读取余额、远端分类与商品目录，并按各店铺当前同步策略处理价格、库存及允许更新的目录字段；不会创建测试订单。",
                          )
                        : t("supply.syncIdempotentHint")
                      : targetSupplierStatus === "active"
                        ? t("supply.enableConfirmHint")
                        : t("supply.disableConfirmHint")
                  }}
                </p>
              </div>
            </div>
            <fieldset>
              <legend>{{ t("supply.auditInfo") }}</legend>
              <label>
                {{ t("supply.actionReason") }}
                <textarea
                  v-model="actionReason"
                  maxlength="500"
                  :placeholder="t('supply.actionReasonPlaceholder')"
                  autofocus
                ></textarea>
                <small>{{ reasonLength(actionReason) }} / 500</small>
              </label>
            </fieldset>
            <footer>
              <button type="button" :disabled="saving" @click="closeModal">
                {{ t("supply.cancel") }}
              </button>
              <button class="primary-button" type="submit" :disabled="saving">
                <LoaderCircle v-if="saving" class="spinning" :size="14" />
                <RefreshCw
                  v-else-if="
                    modalKind === 'supplier-sync' ||
                    modalKind === 'supplier-sync-all'
                  "
                  :size="14"
                />
                <Power v-else :size="14" />{{ t("supply.confirmSubmit") }}
              </button>
            </footer>
          </form>

          <form
            v-else-if="modalKind === 'supplier-delete'"
            class="supply-form compact-form"
            @submit.prevent="submitSupplierDelete"
          >
            <div v-if="formError" class="form-alert error-notice">
              <AlertCircle :size="15" />{{ formError }}
            </div>
            <section v-if="editingSupplier" class="delete-card">
              <Trash2 :size="20" />
              <div>
                <b>{{ editingSupplier.name }} · {{ editingSupplier.code }}</b>
                <span>{{ editingSupplier.base_url }}</span>
                <p>
                  {{
                    supplyText(
                      "supply.deleteSupplierWarning",
                      "只有已停用、且从未产生目录同步、分类或商品映射、同步任务及采购历史的共享店铺才能删除；存在历史时请保持停用以保留完整审计链。",
                    )
                  }}
                </p>
              </div>
            </section>
            <fieldset>
              <legend>{{ t("supply.auditInfo") }}</legend>
              <label>
                {{ t("supply.deleteReason") }}
                <textarea
                  v-model="actionReason"
                  maxlength="500"
                  :placeholder="t('supply.deleteReasonPlaceholder')"
                  autofocus
                ></textarea>
                <small>{{ reasonLength(actionReason) }} / 500</small>
              </label>
            </fieldset>
            <footer>
              <button type="button" :disabled="saving" @click="closeModal">
                {{ t("supply.cancel") }}
              </button>
              <button class="danger-button" type="submit" :disabled="saving">
                <LoaderCircle v-if="saving" class="spinning" :size="14" />
                <Trash2 v-else :size="14" />{{ t("supply.confirmDelete") }}
              </button>
            </footer>
          </form>

          <form
            v-else-if="modalKind === 'mapping-delete'"
            class="supply-form compact-form"
            @submit.prevent="submitMappingDelete"
          >
            <div v-if="formError" class="form-alert error-notice">
              <AlertCircle :size="15" />{{ formError }}
            </div>
            <section v-if="editingMapping" class="delete-card">
              <Trash2 :size="20" />
              <div>
                <b
                  >{{ editingMapping.product_name }} ·
                  {{
                    editingMapping.variant_name || t("supply.defaultVariant")
                  }}</b
                >
                <span
                  >{{ editingMapping.supplier_name }} →
                  {{ editingMapping.external_product_id }}</span
                >
                <p>
                  {{ t("supply.deleteWarning") }}
                </p>
              </div>
            </section>
            <fieldset>
              <legend>{{ t("supply.auditInfo") }}</legend>
              <label>
                {{ t("supply.deleteReason") }}
                <textarea
                  v-model="actionReason"
                  maxlength="500"
                  :placeholder="t('supply.deleteReasonPlaceholder')"
                  autofocus
                ></textarea>
                <small>{{ reasonLength(actionReason) }} / 500</small>
              </label>
            </fieldset>
            <footer>
              <button type="button" :disabled="saving" @click="closeModal">
                {{ t("supply.cancel") }}
              </button>
              <button class="danger-button" type="submit" :disabled="saving">
                <LoaderCircle v-if="saving" class="spinning" :size="14" />
                <Trash2 v-else :size="14" />{{ t("supply.confirmDelete") }}
              </button>
            </footer>
          </form>

          <div v-else class="procurement-detail">
            <div v-if="detailLoading" class="detail-state">
              <LoaderCircle class="spinning" :size="21" />
              {{ t("supply.loadingDetail") }}
            </div>
            <div v-else-if="detailError" class="detail-state error-detail">
              <AlertCircle :size="21" />
              <b>{{ detailError }}</b>
              <button type="button" @click="loadProcurementDetail">
                {{ t("supply.retry") }}
              </button>
            </div>
            <template v-else-if="procurementDetail">
              <section class="procurement-hero">
                <div>
                  <small>{{ t("supply.procurementNo") }}</small>
                  <b>{{ procurementDetail.procurement.procurement_no }}</b>
                  <code>{{ procurementDetail.procurement.id }}</code>
                </div>
                <span
                  class="status-badge"
                  :class="`status-${procurementDetail.procurement.status}`"
                >
                  {{
                    statusLabel(
                      procurementStatusLabels,
                      procurementDetail.procurement.status,
                    )
                  }}
                </span>
              </section>

              <section class="detail-section">
                <header>
                  <div>
                    <Clock3 :size="15" /><b>{{ t("supply.scheduleRetry") }}</b>
                  </div>
                  <span>{{ t("supply.securitySummary") }}</span>
                </header>
                <div class="retry-card">
                  <div>
                    <small>{{ t("supply.attemptsLabel") }}</small>
                    <b>{{
                      t("supply.timesCount", {
                        count: procurementDetail.procurement.attempts,
                      })
                    }}</b>
                  </div>
                  <div>
                    <small>{{ t("supply.nextPoll") }}</small>
                    <b>{{
                      formatTime(procurementDetail.procurement.next_poll_at)
                    }}</b>
                  </div>
                  <div>
                    <small>{{ t("supply.completedAt") }}</small>
                    <b>{{
                      formatTime(procurementDetail.procurement.completed_at)
                    }}</b>
                  </div>
                  <p>{{ procurementDetail.procurement.retry_message }}</p>
                </div>
              </section>

              <section class="detail-section">
                <header>
                  <div>
                    <ServerCog :size="15" /><b>{{
                      t("supply.upstreamSummary")
                    }}</b>
                  </div>
                  <span>{{ t("supply.noRawExchange") }}</span>
                </header>
                <div class="detail-grid">
                  <div>
                    <small>{{ t("supply.supplier") }}</small>
                    <b>{{ procurementDetail.procurement.supplier_name }}</b>
                    <span>{{
                      procurementDetail.procurement.supplier_code
                    }}</span>
                  </div>
                  <div>
                    <small>{{ t("supply.upstreamOrderNo") }}</small>
                    <b>{{
                      procurementDetail.procurement.external_order_no ||
                      t("supply.pendingGenerate")
                    }}</b>
                  </div>
                  <div>
                    <small>{{ t("supply.upstreamProductId") }}</small>
                    <b>{{
                      procurementDetail.procurement.external_product_id
                    }}</b>
                  </div>
                  <div>
                    <small>{{ t("supply.qtyCost") }}</small>
                    <b>{{
                      t("supply.qtyAndAmount", {
                        count: procurementDetail.procurement.quantity,
                        amount: formatMoney(
                          procurementDetail.procurement.cost_amount,
                          procurementDetail.procurement.cost_currency,
                        ),
                      })
                    }}</b>
                  </div>
                  <div>
                    <small>{{ t("supply.createdAt") }}</small>
                    <b>{{
                      formatTime(procurementDetail.procurement.created_at)
                    }}</b>
                  </div>
                  <div>
                    <small>{{ t("supply.updatedAtTime") }}</small>
                    <b>{{
                      formatTime(procurementDetail.procurement.updated_at)
                    }}</b>
                  </div>
                </div>
              </section>

              <section class="detail-section">
                <header>
                  <div>
                    <Link2 :size="15" /><b>{{ t("supply.relatedOrder") }}</b>
                  </div>
                  <span>{{ procurementDetail.order.order_no }}</span>
                </header>
                <div class="order-summary">
                  <div>
                    <small>{{ t("supply.orderStatus") }}</small>
                    <b>{{
                      statusLabel(
                        orderStatusLabels,
                        procurementDetail.order.status,
                      )
                    }}</b>
                    <span>{{
                      statusLabel(
                        paymentStatusLabels,
                        procurementDetail.order.payment_status,
                      )
                    }}</span>
                  </div>
                  <div>
                    <small>{{ t("supply.orderAmount") }}</small>
                    <b>{{
                      formatMoney(
                        procurementDetail.order.total,
                        procurementDetail.order.currency,
                      )
                    }}</b>
                    <span>{{
                      t("supply.subtotalDiscount", {
                        subtotal: formatMoney(
                          procurementDetail.order.subtotal,
                          procurementDetail.order.currency,
                        ),
                        discount: formatMoney(
                          procurementDetail.order.discount,
                          procurementDetail.order.currency,
                        ),
                      })
                    }}</span>
                  </div>
                  <div>
                    <small>{{ t("supply.payDeliver") }}</small>
                    <b>{{ formatTime(procurementDetail.order.paid_at) }}</b>
                    <span>{{
                      t("supply.deliveredAt", {
                        time: formatTime(procurementDetail.order.delivered_at),
                      })
                    }}</span>
                  </div>
                </div>
                <div class="order-item-card">
                  <span><Boxes :size="18" /></span>
                  <div>
                    <small>{{ t("supply.relatedOrderItem") }}</small>
                    <b>{{ procurementDetail.item.product_name }}</b>
                    <span>{{
                      procurementDetail.item.variant_name ||
                      t("supply.defaultVariant")
                    }}</span>
                  </div>
                  <div>
                    <small>{{ t("supply.unitPriceQty") }}</small>
                    <b
                      >{{
                        formatMoney(
                          procurementDetail.item.unit_price,
                          procurementDetail.item.currency ||
                            procurementDetail.order.currency,
                        )
                      }}
                      × {{ procurementDetail.item.quantity }}</b
                    >
                  </div>
                </div>
              </section>

              <section
                v-if="canManage && procurementRecoverable"
                class="detail-section recovery-section"
              >
                <header>
                  <div>
                    <RefreshCw :size="15" /><b>{{
                      t("supply.procurementRecovery")
                    }}</b>
                  </div>
                  <span>{{ t("supply.recoveryTerminalOnly") }}</span>
                </header>
                <div class="recovery-warning">
                  <AlertCircle :size="17" />
                  <p>{{ t("supply.recoveryWarning") }}</p>
                </div>
                <div class="recovery-actions">
                  <button
                    type="button"
                    :class="{ active: recoveryMode === 'retry' }"
                    @click="recoveryMode = 'retry'"
                  >
                    <RefreshCw :size="15" />
                    <span
                      ><b>{{ t("supply.safeRetry") }}</b
                      ><small>{{ t("supply.safeRetryHint") }}</small></span
                    >
                  </button>
                  <button
                    type="button"
                    :class="{ active: recoveryMode === 'manual' }"
                    @click="recoveryMode = 'manual'"
                  >
                    <Check :size="15" />
                    <span
                      ><b>{{ t("supply.manualCompensation") }}</b
                      ><small>{{
                        t("supply.manualCompensationHint")
                      }}</small></span
                    >
                  </button>
                </div>
                <form
                  v-if="recoveryMode"
                  class="recovery-form"
                  @submit.prevent="submitProcurementRecovery"
                >
                  <label
                    >{{ t("supply.recoveryEvidence") }}
                    <textarea
                      v-model="recoveryEvidence"
                      rows="3"
                      maxlength="1000"
                      :placeholder="t('supply.recoveryEvidencePlaceholder')"
                    ></textarea>
                    <small>{{ t("supply.recoveryEvidenceHint") }}</small>
                  </label>
                  <template v-if="recoveryMode === 'manual'">
                    <label
                      >{{
                        t("supply.manualDeliveryValues", {
                          count: procurementDetail.procurement.quantity,
                        })
                      }}
                      <textarea
                        v-model="manualDeliveries"
                        class="secret-delivery-input"
                        rows="6"
                        autocomplete="off"
                        spellcheck="false"
                        :placeholder="t('supply.manualDeliveryPlaceholder')"
                      ></textarea>
                      <small>{{ t("supply.manualDeliveryPrivacy") }}</small>
                    </label>
                    <label
                      >{{ t("supply.manualCost") }}
                      <input
                        v-model="manualCostYuan"
                        inputmode="decimal"
                        min="0"
                        max="10000000"
                        :step="
                          majorInputStep(
                            procurementDetail.procurement.cost_currency,
                          )
                        "
                      />
                    </label>
                  </template>
                  <label
                    >{{ t("supply.recoveryAuditReason") }}
                    <input
                      v-model="recoveryReason"
                      maxlength="500"
                      :placeholder="t('supply.recoveryAuditReasonPlaceholder')"
                    />
                  </label>
                  <p v-if="formError" class="form-error">{{ formError }}</p>
                  <footer>
                    <button type="button" @click="resetProcurementRecovery">
                      {{ t("supply.cancel") }}
                    </button>
                    <button
                      class="primary-action"
                      type="submit"
                      :disabled="saving"
                    >
                      <LoaderCircle v-if="saving" class="spinning" :size="15" />
                      {{
                        recoveryMode === "manual"
                          ? t("supply.confirmManualDelivery")
                          : t("supply.confirmRetry")
                      }}
                    </button>
                  </footer>
                </form>
              </section>

              <div class="privacy-banner detail-privacy">
                <ShieldCheck :size="15" />
                {{ t("supply.detailPrivacy") }}
              </div>
              <footer class="detail-footer">
                <span>{{ t("supply.auditLogged") }}</span>
                <button type="button" @click="closeModal">
                  {{ t("supply.close") }}
                </button>
              </footer>
            </template>
          </div>
        </section>
      </div>
    </template>
    <SupplierCatalogManager
      v-else
      :supplier="catalogSupplier"
      @close="catalogSupplier = null"
      @notice="notice = $event"
    />
  </section>
</template>

<style scoped>
.currency-semantics {
  color: var(--text) !important;
  font-weight: 600;
}

.currency-mode-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.currency-mode-card {
  display: grid !important;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: flex-start;
  gap: 9px !important;
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 12px;
  cursor: pointer;
}

.currency-mode-card.selected {
  border-color: var(--dark);
  background: var(--surface-2);
}

.currency-mode-card input {
  margin-top: 3px;
}

.currency-mode-card span {
  display: grid;
  gap: 5px;
}

.currency-mode-card small,
.currency-flow-note {
  color: var(--muted);
  font-size: 11px;
  line-height: 1.6;
}

.currency-flow-note {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  border: 1px solid var(--line);
  background: var(--surface-2);
  border-radius: 7px;
  padding: 10px 12px;
}

.currency-flow-note svg {
  flex: 0 0 auto;
  margin-top: 1px;
}

.supply-shell {
  display: grid;
  gap: 12px;
}

.supply-nav {
  min-height: 58px;
  padding: 0 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  overflow: hidden;
}

.supply-tabs {
  min-width: 0;
  align-self: stretch;
  display: flex;
  align-items: end;
  gap: 4px;
}

.supply-tabs button {
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

.supply-tabs button.active {
  border-bottom-color: var(--text);
  color: var(--text);
}

.supply-tabs button span {
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
  white-space: nowrap;
}

.supply-panel {
  min-width: 0;
  overflow: hidden;
}

.supply-toolbar {
  min-height: 58px;
  padding: 10px 13px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.supply-search {
  width: min(470px, 100%);
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

.supply-search input {
  min-width: 0;
  flex: 1;
  border: 0;
  outline: none;
  background: transparent;
  font-size: 9px;
}

.supply-search button,
.supply-filters button,
.supply-filters select {
  height: 28px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}

.supply-search button {
  padding: 0 9px;
  border-top: 0;
  border-right: 0;
  border-bottom: 0;
  border-radius: 0;
  display: flex;
  align-items: center;
  gap: 4px;
}

.supply-filters {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
}

.supply-filters select {
  max-width: 170px;
  padding: 0 8px;
}

.supply-filters button {
  padding: 0 9px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  white-space: nowrap;
}

.supply-filters button.primary-compact {
  border-color: var(--dark);
  background: var(--dark);
  color: var(--dark-text);
}

.supply-filters button:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.supply-notice,
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
  top: 83px;
  z-index: 3;
  margin: 0 0 14px;
}

.success-notice {
  background: color-mix(in srgb, var(--success) 9%, transparent);
  color: var(--success);
}

.error-notice {
  background: color-mix(in srgb, var(--danger) 9%, transparent);
  color: var(--danger);
}

.supply-notice button,
.form-alert button {
  margin-left: auto;
  border: 0;
  background: transparent;
  color: inherit;
  font-size: 8px;
  font-weight: 700;
}

.supply-state {
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

.supply-state strong {
  color: var(--text);
  font-size: 11px;
}

.supply-table-wrap {
  width: 100%;
  min-height: 390px;
  overflow-x: auto;
}

.supply-table {
  width: 100%;
  min-width: 1120px;
  border-collapse: collapse;
}

.mapping-table {
  min-width: 1220px;
}

.procurement-table {
  min-width: 1180px;
}

.supply-table th,
.supply-table td {
  padding: 13px 14px;
  border-bottom: 1px solid var(--line);
  text-align: left;
  vertical-align: middle;
}

.supply-table th {
  background: var(--surface-2);
  color: var(--muted);
  font-size: 7px;
  font-weight: 600;
  letter-spacing: 0.04em;
}

.supply-table td {
  font-size: 8px;
}

.supply-table td > b,
.supply-table td > time,
.supply-table td > small,
.supply-table td > code {
  display: block;
}

.supply-table td > b,
.supply-table td > time {
  font-size: 8px;
  font-weight: 600;
}

.supply-table td > small,
.supply-table td > code,
.record-primary code {
  margin-top: 4px;
  color: var(--muted);
  font-size: 7px;
}

.supply-table code,
.identity-card code,
.procurement-hero code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

.record-primary {
  min-width: 175px;
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
.record-primary code,
.external-id,
.truncate {
  max-width: 225px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.record-primary b {
  font-size: 9px;
}

.status-badge,
.inline-status {
  width: fit-content;
  padding: 3px 7px;
  border-radius: 10px;
  display: block;
  background: var(--soft);
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
}

.inline-status {
  margin-top: 6px;
}

.status-active,
.status-completed,
.status-delivered {
  background: color-mix(in srgb, var(--success) 10%, transparent);
  color: var(--success);
}

.status-creating,
.status-dispatching,
.status-processing,
.status-retrying,
.status-pending {
  background: color-mix(in srgb, var(--warn) 10%, transparent);
  color: var(--warn);
}

.status-disabled,
.status-failed,
.status-cancelled {
  background: color-mix(in srgb, var(--danger) 9%, transparent);
  color: var(--danger);
}

.health-badge {
  width: fit-content;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin-top: 5px;
  padding: 3px 7px;
  border-radius: 10px;
  font-size: 7px;
  font-weight: 700;
  background: var(--soft);
  color: var(--muted);
}

.health-healthy {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 10%, transparent);
}

.health-degraded {
  color: var(--warn);
  background: color-mix(in srgb, var(--warn) 10%, transparent);
}

.health-unreachable {
  color: var(--danger);
  background: color-mix(in srgb, var(--danger) 9%, transparent);
}

.credential-state {
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--danger);
  font-size: 8px;
  font-weight: 600;
}

.balance-unsynced {
  color: var(--warn) !important;
}

.credential-state.configured {
  color: var(--success);
}

.sync-flags {
  display: flex;
  gap: 5px;
}

.sync-flags span {
  padding: 3px 6px;
  border-radius: 4px;
  background: var(--soft);
  color: var(--muted);
  font-size: 7px;
}

.sync-flags span.on {
  background: color-mix(in srgb, var(--success) 10%, transparent);
  color: var(--success);
}

.error-text {
  color: var(--danger) !important;
}

.retry-message {
  max-width: 220px;
  margin-top: 5px;
  display: block;
  color: var(--muted);
  font-size: 7px;
  line-height: 1.45;
}

.callback-url,
.callback-state {
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.callback-state {
  color: var(--success) !important;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

.record-actions {
  text-align: right !important;
  white-space: nowrap;
}

.record-actions button {
  height: 29px;
  margin-left: 4px;
  padding: 0 8px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}

.record-actions button.danger-action {
  color: var(--danger);
}

.record-actions button:disabled {
  opacity: 0.38;
  cursor: not-allowed;
}

.record-actions button.danger-action:disabled {
  color: var(--muted);
}

.supply-pagination {
  min-height: 53px;
  padding: 9px 13px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: var(--muted);
  font-size: 8px;
}

.supply-pagination > div {
  display: flex;
  gap: 4px;
}

.supply-pagination button,
.supply-pagination select {
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

.supply-pagination button.active {
  background: var(--dark);
  color: var(--dark-text);
}

.supply-pagination button:disabled {
  cursor: not-allowed;
  opacity: 0.4;
}

.supply-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 120;
  padding: 24px;
  display: flex;
  justify-content: flex-end;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(2px);
}

.supply-modal {
  width: min(710px, 100%);
  height: 100%;
  border: 1px solid var(--line);
  border-radius: 10px;
  overflow-y: auto;
  background: var(--surface);
  color: var(--text);
  box-shadow: var(--shadow);
}

.supply-modal.detail-modal {
  width: min(790px, 100%);
}

.supply-modal > header {
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

.supply-modal h2 {
  margin: 5px 0 3px;
  font-size: 17px;
  letter-spacing: -0.03em;
}

.supply-modal header p {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
}

.supply-modal > header > button {
  width: 32px;
  height: 32px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: grid;
  place-items: center;
  background: var(--surface);
}

.supply-form {
  padding: 18px 20px 0;
}

.supply-form.compact-form {
  min-height: calc(100% - 84px);
  display: flex;
  flex-direction: column;
}

.supply-form fieldset {
  margin: 0 0 15px;
  padding: 15px;
  border: 1px solid var(--line);
  border-radius: 7px;
}

.supply-form legend {
  padding: 0 6px;
  color: var(--muted);
  font-size: 8px;
  font-weight: 700;
  letter-spacing: 0.04em;
}

.supply-form label {
  display: block;
  color: var(--text);
  font-size: 9px;
  font-weight: 600;
}

.supply-form label + label,
.supply-form .form-grid + label,
.supply-form .choice-grid + label {
  margin-top: 12px;
}

.supply-form input:not([type="checkbox"]):not([type="radio"]),
.supply-form select,
.supply-form textarea {
  width: 100%;
  margin-top: 7px;
  border: 1px solid var(--line);
  border-radius: 5px;
  outline: none;
  background: var(--surface-2);
  color: var(--text);
  font-size: 9px;
}

.supply-form input:not([type="checkbox"]):not([type="radio"]),
.supply-form select {
  height: 36px;
  padding: 0 10px;
}

.supply-form textarea {
  min-height: 82px;
  padding: 9px 10px;
  resize: vertical;
  line-height: 1.55;
}

.supply-form input:focus,
.supply-form select:focus,
.supply-form textarea:focus {
  border-color: color-mix(in srgb, var(--text) 45%, var(--line));
}

.supply-form label > small {
  margin-top: 5px;
  display: block;
  color: var(--muted);
  font-size: 7px;
  font-weight: 400;
  line-height: 1.45;
}

.form-grid {
  display: grid;
  gap: 12px;
}

.form-grid.two-columns {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.form-grid label + label {
  margin-top: 0;
}

.credential-control {
  margin-bottom: 13px;
  padding: 10px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  background: var(--surface-2);
}

.credential-control > div {
  display: flex;
  align-items: center;
  gap: 9px;
}

.credential-control > div > span {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.credential-control b {
  font-size: 9px;
}

.credential-control small {
  color: var(--muted);
  font-size: 7px;
}

.switch-label {
  display: flex !important;
  align-items: center;
  gap: 7px;
  white-space: nowrap;
}

.privacy-banner {
  margin-top: 12px;
  padding: 9px 10px;
  border-radius: 5px;
  display: flex;
  align-items: flex-start;
  gap: 7px;
  background: color-mix(in srgb, var(--success) 8%, transparent);
  color: var(--success);
  font-size: 8px;
  line-height: 1.5;
}

.choice-grid,
.sync-choice-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.choice-grid label,
.sync-choice-grid label {
  min-height: 69px;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  background: var(--surface-2);
}

.choice-grid label.selected {
  border-color: var(--text);
}

.choice-grid span,
.sync-choice-grid span {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.choice-grid b,
.sync-choice-grid b {
  font-size: 9px;
}

.choice-grid small,
.sync-choice-grid small {
  color: var(--muted);
  font-size: 7px;
  font-weight: 400;
  line-height: 1.45;
}

.parameter-mapping-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.parameter-mapping-heading > div {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.parameter-mapping-heading b {
  font-size: 9px;
}

.parameter-mapping-heading small,
.parameter-mapping-meta {
  color: var(--muted);
  font-size: 7px;
  line-height: 1.5;
}

.parameter-mapping-heading button {
  height: 30px;
  flex: 0 0 auto;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: flex;
  align-items: center;
  gap: 5px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}

.parameter-mapping-heading button:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.local-key-suggestions {
  margin-top: 12px;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: grid;
  gap: 8px;
  background: var(--surface-2);
}

.local-key-suggestions > div:first-child {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.local-key-suggestions > div:first-child b {
  font-size: 8px;
}

.local-key-suggestions > div:first-child small {
  color: var(--muted);
  font-size: 7px;
  line-height: 1.5;
}

.local-key-suggestions > div:last-child {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.local-key-suggestions button {
  min-width: 0;
  min-height: 29px;
  padding: 5px 8px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: flex;
  align-items: center;
  gap: 5px;
  background: var(--surface);
  color: var(--text);
  font-size: 7px;
}

.local-key-suggestions button code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 7px;
  font-weight: 700;
}

.local-key-suggestions button span {
  max-width: 130px;
  overflow: hidden;
  color: var(--muted);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.local-key-suggestions button small {
  padding: 1px 4px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--warn) 10%, transparent);
  color: var(--warn);
  font-size: 6px;
}

.local-key-suggestions button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.parameter-mapping-empty {
  min-height: 70px;
  margin-top: 12px;
  border: 1px dashed var(--line);
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  background: var(--surface-2);
  color: var(--muted);
  font-size: 8px;
  text-align: center;
}

.parameter-mapping-list {
  margin-top: 12px;
  display: grid;
  gap: 8px;
}

.parameter-mapping-row {
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr) 32px;
  align-items: end;
  gap: 8px;
  background: var(--surface-2);
}

.parameter-mapping-row label > span {
  color: var(--muted);
  font-size: 7px;
}

.parameter-mapping-arrow {
  align-self: center;
  margin-top: 19px;
  color: var(--muted);
}

.parameter-mapping-remove {
  width: 32px;
  height: 36px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--danger);
}

.parameter-mapping-remove:hover {
  border-color: color-mix(in srgb, var(--danger) 45%, var(--line));
  background: color-mix(in srgb, var(--danger) 7%, var(--surface));
}

.parameter-mapping-meta {
  margin: 8px 0 0;
  display: flex;
  justify-content: space-between;
  gap: 10px;
}

.parameter-mapping-meta b {
  flex: 0 0 auto;
  color: var(--text);
  font-weight: 600;
}

.inline-loading {
  margin-bottom: 13px;
  padding: 9px 10px;
  border-radius: 5px;
  display: flex;
  align-items: center;
  gap: 7px;
  background: var(--surface-2);
  color: var(--muted);
  font-size: 8px;
}

.form-hint {
  margin: 11px 0 0;
  padding: 9px 10px;
  border-radius: 5px;
  background: var(--surface-2);
  color: var(--muted);
  font-size: 8px;
  line-height: 1.55;
}

.supply-form > footer {
  position: sticky;
  bottom: 0;
  margin: 0 -20px;
  padding: 13px 20px;
  border-top: 1px solid var(--line);
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  background: color-mix(in srgb, var(--surface) 95%, transparent);
  backdrop-filter: blur(10px);
}

.supply-form > footer button {
  min-width: 94px;
  height: 34px;
  padding: 0 13px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: var(--surface);
  font-size: 8px;
}

.supply-form > footer .primary-button {
  border-color: var(--dark);
  background: var(--dark);
  color: var(--dark-text);
}

.supply-form > footer .danger-button {
  border-color: var(--danger);
  background: var(--danger);
  color: #fff;
}

.supply-form > footer button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.identity-card {
  margin-bottom: 15px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
}

.identity-card > span:first-child {
  width: 35px;
  height: 35px;
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
  font-size: 10px;
}

.identity-card code {
  color: var(--muted);
  font-size: 8px;
}

.action-explanation,
.delete-card {
  margin-bottom: 15px;
  padding: 15px;
  border-radius: 7px;
  display: flex;
  align-items: flex-start;
  gap: 11px;
  background: var(--surface-2);
}

.action-explanation div,
.delete-card div {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.action-explanation b,
.delete-card b {
  font-size: 10px;
}

.action-explanation p,
.delete-card p {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.6;
}

.delete-card {
  background: color-mix(in srgb, var(--danger) 7%, var(--surface));
  color: var(--danger);
}

.delete-card span {
  color: var(--text);
  font-size: 8px;
}

.procurement-detail {
  padding: 18px 20px;
}

.detail-state {
  min-height: 420px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 9px;
  color: var(--muted);
  font-size: 9px;
}

.detail-state button {
  height: 30px;
  padding: 0 10px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}

.error-detail {
  color: var(--danger);
}

.procurement-hero {
  margin-bottom: 14px;
  padding: 15px;
  border: 1px solid var(--line);
  border-radius: 8px;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  background: var(--surface-2);
}

.procurement-hero div {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.procurement-hero small,
.detail-section small,
.order-item-card small {
  color: var(--muted);
  font-size: 7px;
}

.procurement-hero b {
  font-size: 14px;
}

.procurement-hero code {
  color: var(--muted);
  font-size: 7px;
  overflow-wrap: anywhere;
}

.detail-section {
  margin-bottom: 14px;
  border: 1px solid var(--line);
  border-radius: 8px;
  overflow: hidden;
}

.detail-section > header {
  min-height: 41px;
  padding: 9px 12px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  background: var(--surface-2);
}

.detail-section > header div {
  display: flex;
  align-items: center;
  gap: 7px;
}

.detail-section > header b {
  font-size: 9px;
}

.detail-section > header span {
  color: var(--muted);
  font-size: 7px;
}

.retry-card {
  padding: 13px;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.retry-card > div {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.retry-card b {
  font-size: 9px;
}

.retry-card p {
  grid-column: 1 / -1;
  margin: 2px 0 0;
  padding: 9px 10px;
  border-radius: 5px;
  background: color-mix(in srgb, var(--warn) 8%, transparent);
  color: var(--warn);
  font-size: 8px;
  line-height: 1.5;
}

.detail-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.detail-grid > div {
  min-height: 73px;
  padding: 12px;
  border-right: 1px solid var(--line);
  border-bottom: 1px solid var(--line);
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.detail-grid > div:nth-child(3n) {
  border-right: 0;
}

.detail-grid > div:nth-last-child(-n + 3) {
  border-bottom: 0;
}

.detail-grid b {
  font-size: 8px;
  overflow-wrap: anywhere;
}

.detail-grid span {
  color: var(--muted);
  font-size: 7px;
}

.order-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  border-bottom: 1px solid var(--line);
}

.order-summary > div {
  min-height: 79px;
  padding: 12px;
  border-right: 1px solid var(--line);
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.order-summary > div:last-child {
  border-right: 0;
}

.order-summary b {
  font-size: 9px;
}

.order-summary span {
  color: var(--muted);
  font-size: 7px;
  line-height: 1.4;
}

.order-item-card {
  padding: 12px;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
}

.order-item-card > span {
  width: 35px;
  height: 35px;
  border-radius: 6px;
  display: grid;
  place-items: center;
  background: var(--soft);
}

.order-item-card > div {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.order-item-card > div:last-child {
  text-align: right;
}

.order-item-card b {
  font-size: 9px;
}

.order-item-card span {
  color: var(--muted);
  font-size: 7px;
}

.recovery-warning {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 12px;
  border-bottom: 1px solid var(--line);
  color: var(--warn);
  background: color-mix(in srgb, var(--warn) 6%, var(--surface));
}

.recovery-warning p {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.6;
}

.recovery-actions {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 9px;
  padding: 12px;
}

.recovery-actions > button {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface);
  color: var(--text);
  text-align: left;
}

.recovery-actions > button.active {
  border-color: var(--text);
  background: var(--surface-2);
}

.recovery-actions span,
.recovery-actions b,
.recovery-actions small {
  display: block;
}

.recovery-actions b {
  margin-bottom: 4px;
  font-size: 9px;
}

.recovery-actions small {
  line-height: 1.45;
}

.recovery-form {
  display: grid;
  gap: 10px;
  padding: 12px;
  border-top: 1px solid var(--line);
  background: var(--surface-2);
}

.recovery-form label {
  display: grid;
  gap: 6px;
  color: var(--muted);
  font-size: 8px;
  font-weight: 600;
}

.recovery-form input,
.recovery-form textarea {
  width: 100%;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--text);
  padding: 9px;
  outline: 0;
}

.secret-delivery-input {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  -webkit-text-security: disc;
}

.recovery-form .form-error {
  margin: 0;
  color: var(--danger);
  font-size: 8px;
}

.recovery-form footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.recovery-form footer button {
  min-height: 34px;
  padding: 0 12px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}

.recovery-form footer .primary-action {
  background: var(--text);
  color: var(--surface);
}

.detail-privacy {
  margin: 0;
}

.detail-footer {
  margin-top: 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: var(--muted);
  font-size: 8px;
}

.detail-footer button {
  height: 32px;
  padding: 0 13px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  font-size: 8px;
}

.spinning {
  animation: supply-spin 0.9s linear infinite;
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

@keyframes supply-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 1100px) {
  .supply-toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .supply-search {
    width: 100%;
  }

  .supply-filters {
    justify-content: flex-start;
    flex-wrap: wrap;
  }
}

@media (max-width: 760px) {
  .currency-mode-grid {
    grid-template-columns: 1fr;
  }

  .recovery-actions {
    grid-template-columns: 1fr;
  }

  .supply-nav {
    padding: 0 9px;
  }

  .nav-context {
    display: none;
  }

  .supply-tabs {
    width: 100%;
  }

  .supply-tabs button {
    flex: 1;
    justify-content: center;
    padding: 0 6px;
  }

  .supply-toolbar {
    padding: 9px;
  }

  .supply-filters select {
    max-width: none;
    flex: 1 1 130px;
  }

  .supply-filters button.primary-compact {
    margin-left: auto;
  }

  .supply-table-wrap {
    min-height: 300px;
    padding: 7px;
    overflow: visible;
  }

  .supply-table {
    min-width: 0;
    display: block;
  }

  .supply-table thead {
    display: none;
  }

  .supply-table tbody {
    display: grid;
    gap: 8px;
  }

  .supply-table tr {
    padding: 10px;
    border: 1px solid var(--line);
    border-radius: 7px;
    display: grid;
    background: var(--surface);
  }

  .supply-table td {
    min-width: 0;
    padding: 8px 0;
    border-bottom: 1px solid var(--line);
    display: grid;
    grid-template-columns: minmax(95px, 0.38fr) minmax(0, 1fr);
    align-items: start;
    gap: 9px;
    text-align: right;
  }

  .supply-table td::before {
    content: attr(data-label);
    grid-column: 1;
    grid-row: 1;
    color: var(--muted);
    font-size: 7px;
    text-align: left;
  }

  .supply-table td:last-child {
    border-bottom: 0;
  }

  .supply-table td > *,
  .record-primary {
    grid-column: 2;
    margin-left: auto;
  }

  .record-primary {
    min-width: 0;
    justify-content: flex-end;
  }

  .record-primary > span {
    display: none;
  }

  .record-primary div {
    align-items: flex-end;
  }

  .record-actions {
    white-space: normal;
  }

  .record-actions button {
    min-height: 40px;
    margin: 2px 0 2px 4px;
  }

  .supply-search,
  .supply-search button,
  .supply-filters button,
  .supply-filters select {
    min-height: 40px;
  }

  .sync-flags {
    justify-content: flex-end;
  }

  .supply-pagination {
    align-items: flex-start;
    flex-direction: column;
  }

  .supply-pagination > div {
    width: 100%;
    overflow-x: auto;
  }

  .supply-pagination button,
  .supply-pagination select {
    min-width: 40px;
    min-height: 40px;
  }

  .supply-modal-backdrop {
    padding: 0;
  }

  .supply-modal,
  .supply-modal.detail-modal {
    width: 100%;
    border: 0;
    border-radius: 0;
  }

  .supply-modal > header {
    padding: 14px;
  }

  .supply-modal > header > button {
    width: 40px;
    height: 40px;
  }

  .supply-form,
  .procurement-detail {
    padding: 14px 12px 0;
  }

  .supply-form > footer {
    margin: 0 -12px;
    padding: 11px 12px;
  }

  .supply-form input:not([type="checkbox"]):not([type="radio"]),
  .supply-form select,
  .supply-form > footer button {
    min-height: 42px;
  }

  .form-grid.two-columns,
  .choice-grid,
  .sync-choice-grid,
  .retry-card,
  .detail-grid,
  .order-summary {
    grid-template-columns: 1fr;
  }

  .detail-grid > div,
  .detail-grid > div:nth-child(3n),
  .detail-grid > div:nth-last-child(-n + 3),
  .order-summary > div {
    border-right: 0;
    border-bottom: 1px solid var(--line);
  }

  .detail-grid > div:last-child,
  .order-summary > div:last-child {
    border-bottom: 0;
  }

  .credential-control {
    align-items: flex-start;
    flex-direction: column;
  }

  .parameter-mapping-heading {
    align-items: stretch;
    flex-direction: column;
  }

  .parameter-mapping-heading button {
    width: fit-content;
  }

  .parameter-mapping-row {
    grid-template-columns: minmax(0, 1fr) 32px;
    align-items: stretch;
  }

  .parameter-mapping-row label {
    grid-column: 1;
  }

  .parameter-mapping-arrow {
    display: none;
  }

  .parameter-mapping-remove {
    height: auto;
    grid-column: 2;
    grid-row: 1 / span 2;
  }

  .parameter-mapping-meta {
    flex-direction: column;
    gap: 3px;
  }

  .order-item-card {
    grid-template-columns: auto minmax(0, 1fr);
  }

  .order-item-card > div:last-child {
    grid-column: 2;
    text-align: left;
  }

  .procurement-detail {
    padding-bottom: 15px;
  }
}

@media (max-width: 500px) {
  .supply-tabs button {
    font-size: 0;
  }

  .supply-tabs button span {
    display: none;
  }
}
</style>
