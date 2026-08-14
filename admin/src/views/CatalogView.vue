<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  AlertTriangle,
  BadgePercent,
  Boxes,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Edit3,
  ImagePlus,
  Layers3,
  Link2,
  ListChecks,
  LoaderCircle,
  Package,
  Plus,
  RefreshCw,
  Search,
  Trash2,
  UploadCloud,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import {
  currencyDirectory,
  formatMoney as formatMinorMoney,
  majorInputStep,
  majorToMinor,
  minorToMajor,
  minorToSafeNumber,
  storeCurrency,
} from "../utils/money";

type CatalogTab = "categories" | "products" | "variants" | "pricing";
type PricingTab = "tiers" | "levels";
type ProductStatus = "draft" | "on_sale" | "off_sale";
type InventoryMode = "local" | "supplier";
type DeliveryType = "auto" | "manual";
type VariantStatus = "active" | "inactive";
type EditorKind = "category" | "product" | "variant" | "tier" | "level";

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface Category {
  id: string;
  parent_id?: string | null;
  name: string;
  slug: string;
  description: string;
  icon: string;
  image_url?: string;
  sort: number;
  enabled: boolean;
  created_at: string;
}

interface Product {
  id: string;
  category_id: string;
  category?: Category;
  name: string;
  slug: string;
  summary: string;
  description: string;
  cover_url?: string;
  media?: CatalogMedia[];
  price: number;
  compare_price: number;
  cost_price: number;
  currency: string;
  delivery_type: DeliveryType;
  inventory_mode: InventoryMode;
  minimum_purchase: number;
  maximum_purchase: number;
  status: ProductStatus;
  featured: boolean;
  tags: string;
  sort: number;
  sold_count: number;
  created_at: string;
  updated_at: string;
  payment_channel_ids: string[];
  payment_channels: PaymentChannel[];
}

type CatalogMediaRole = "cover" | "gallery" | "detail";

interface CatalogMedia {
  id: string;
  owner_type: "category" | "product";
  owner_id: string;
  asset_id: string;
  role: CatalogMediaRole;
  sort: number;
  alt_text: string;
  url: string;
  mime: string;
  source_type: string;
  mirror_status: string;
}

interface PaymentChannel {
  id: string;
  name: string;
  code: string;
  fee_rate: number;
  enabled: boolean;
}

interface ProductVariant {
  id: string;
  product_id: string;
  product_name: string;
  sku: string;
  name: string;
  attributes: string | Record<string, string>;
  price: number;
  compare_price: number;
  cost_price: number;
  status: VariantStatus;
  sort: number;
  purchase_limit: number;
  created_at: string;
  updated_at: string;
  currency?: string;
}

type ProductInputType = "text" | "email" | "number" | "select" | "textarea";

interface ProductInputField {
  id: string;
  product_id: string;
  key: string;
  label: string;
  input_type: ProductInputType;
  required: boolean;
  sensitive: boolean;
  pass_to_supplier: boolean;
  placeholder: string;
  help_text: string;
  options: string[];
  validation_pattern: string;
  min_length: number;
  max_length: number;
  sort: number;
  enabled: boolean;
}

interface ProductPriceTier {
  id: string;
  product_id: string;
  variant_id?: string | null;
  member_level_id?: string | null;
  product_name: string;
  variant_name: string;
  member_level_name: string;
  min_quantity: number;
  unit_price: number;
  starts_at?: string | null;
  ends_at?: string | null;
  created_at: string;
  updated_at: string;
  currency?: string;
}

interface MemberLevel {
  id: string;
  code: string;
  name: string;
  minimum_spend: number;
  currency: string;
  discount_basis_point: number;
  priority: number;
  enabled: boolean;
  membership_count: number;
  price_tier_count: number;
  created_at: string;
  updated_at: string;
}

interface AttributeRow {
  key: string;
  value: string;
}

interface DeleteTarget {
  kind: EditorKind;
  id: string;
  name: string;
}

const route = useRoute();
const router = useRouter();
const { t, locale } = useI18n();
const authStore = useAuthStore();
const canManage = computed(() => authStore.hasPermission("catalog.manage"));
const activeTab = ref<CatalogTab>("products");
const pricingTab = ref<PricingTab>("tiers");
const notice = ref("");

const categories = ref<Category[]>([]);
const categoriesLoading = ref(false);
const categoriesError = ref("");

const products = ref<Product[]>([]);
const paymentChannels = ref<PaymentChannel[]>([]);
const paymentChannelsLoading = ref(false);
const paymentChannelsError = ref("");
const productTotal = ref(0);
const productPage = ref(1);
const productPageSize = ref(20);
const productSearch = ref("");
const productQuery = ref("");
const productStatus = ref("");
const productInventory = ref("");
const productCategory = ref("");
const productLoading = ref(false);
const productError = ref("");
let productRequest = 0;

const variants = ref<ProductVariant[]>([]);
const variantTotal = ref(0);
const variantPage = ref(1);
const variantPageSize = ref(20);
const variantSearch = ref("");
const variantQuery = ref("");
const variantStatus = ref("");
const variantProduct = ref("");
const variantLoading = ref(false);
const variantError = ref("");
let variantRequest = 0;

const tiers = ref<ProductPriceTier[]>([]);
const tierTotal = ref(0);
const tierPage = ref(1);
const tierPageSize = ref(20);
const tierProduct = ref("");
const tierVariant = ref("");
const tierMember = ref("");
const tierLoading = ref(false);
const tierError = ref("");
let tierRequest = 0;

const levels = ref<MemberLevel[]>([]);
const levelTotal = ref(0);
const levelPage = ref(1);
const levelPageSize = ref(20);
const levelSearch = ref("");
const levelQuery = ref("");
const levelEnabled = ref("");
const levelLoading = ref(false);
const levelError = ref("");
let levelRequest = 0;

const productOptions = ref<Product[]>([]);
const productOptionTotal = ref(0);
const productOptionQuery = ref("");
const productOptionLoading = ref(false);
const memberOptions = ref<MemberLevel[]>([]);
const variantChoices = ref<ProductVariant[]>([]);
const variantChoiceLoading = ref(false);

const inputFieldProduct = ref<Product | null>(null);
const inputFields = ref<ProductInputField[]>([]);
const inputFieldsLoading = ref(false);
const inputFieldSaving = ref(false);
const inputFieldError = ref("");
const editingInputFieldID = ref("");
const deletingInputField = ref<ProductInputField | null>(null);
const inputFieldDeleteReason = ref("");
const inputFieldDeleting = ref(false);
const inputFieldForm = reactive({
  key: "",
  label: "",
  inputType: "text" as ProductInputType,
  required: true,
  sensitive: false,
  passToSupplier: false,
  placeholder: "",
  helpText: "",
  optionsText: "",
  validationPattern: "",
  minLength: 1,
  maxLength: 200,
  sort: 0,
  enabled: true,
  reason: "",
});

const editor = ref<EditorKind | null>(null);
const editingID = ref("");
const saving = ref(false);
const formError = ref("");

const categoryForm = reactive({
  parentID: "",
  name: "",
  slug: "",
  description: "",
  icon: "",
  sort: 0,
  enabled: true,
  reason: "",
});

const productForm = reactive({
  categoryID: "",
  name: "",
  slug: "",
  summary: "",
  description: "",
  price: "0.00",
  comparePrice: "0.00",
  costPrice: "0.00",
  currency: storeCurrency.value,
  deliveryType: "auto" as DeliveryType,
  inventoryMode: "local" as InventoryMode,
  originalInventoryMode: "local" as InventoryMode,
  minimumPurchase: 1,
  maximumPurchase: 0,
  status: "draft" as ProductStatus,
  featured: false,
  tags: "",
  sort: 0,
  paymentChannelIDs: [] as string[],
  reason: "",
});

const editorMedia = ref<CatalogMedia[]>([]);
const editorMediaLoading = ref(false);
const editorMediaSaving = ref(false);
const editorMediaError = ref("");
const pendingMediaFile = ref<File | null>(null);
const pendingMediaURL = ref("");
const pendingMediaAlt = ref("");
const pendingMediaRole = ref<CatalogMediaRole>("cover");
const pendingMediaInputKey = ref(0);

interface CategoryTreeOption {
  id: string;
  label: string;
  enabled: boolean;
  disabled: boolean;
}

const categoryTreeOptions = computed<CategoryTreeOption[]>(() => {
  const children = new Map<string, Category[]>();
  for (const category of categories.value) {
    const parent = category.parent_id || "";
    children.set(parent, [...(children.get(parent) || []), category]);
  }
  for (const items of children.values()) {
    items.sort((a, b) => b.sort - a.sort || a.name.localeCompare(b.name));
  }
  const descendantIDs = new Set<string>();
  const collectDescendants = (id: string) => {
    for (const child of children.get(id) || []) {
      if (descendantIDs.has(child.id)) continue;
      descendantIDs.add(child.id);
      collectDescendants(child.id);
    }
  };
  if (editingID.value) collectDescendants(editingID.value);

  const result: CategoryTreeOption[] = [];
  const visited = new Set<string>();
  const append = (category: Category, depth: number) => {
    if (visited.has(category.id)) return;
    visited.add(category.id);
    result.push({
      id: category.id,
      label: `${depth ? "\u00a0\u00a0".repeat(depth) + "└ " : ""}${category.name}`,
      enabled: category.enabled,
      disabled:
        category.id === editingID.value ||
        descendantIDs.has(category.id) ||
        (categoryForm.enabled && !category.enabled),
    });
    for (const child of children.get(category.id) || [])
      append(child, depth + 1);
  };
  for (const root of children.get("") || []) append(root, 0);
  for (const category of categories.value) append(category, 0);
  return result;
});

const variantForm = reactive({
  productID: "",
  sku: "",
  name: "",
  attributes: [{ key: "", value: "" }] as AttributeRow[],
  price: "0.00",
  comparePrice: "0.00",
  costPrice: "0.00",
  status: "active" as VariantStatus,
  sort: 0,
  purchaseLimit: 0,
  reason: "",
});

const tierForm = reactive({
  productID: "",
  variantID: "",
  memberLevelID: "",
  minQuantity: 2,
  unitPrice: "0.00",
  startsAt: "",
  endsAt: "",
  reason: "",
});

const levelForm = reactive({
  code: "",
  name: "",
  minimumSpend: "0.00",
  currency: storeCurrency.value,
  discountPercent: "100.00",
  priority: 0,
  enabled: true,
  reason: "",
});

const deleteTarget = ref<DeleteTarget | null>(null);
const deleteReason = ref("");
const deleteError = ref("");
const deleting = ref(false);

function statusLabel(value: string) {
  const key = `catalog.status.${value}`;
  return t(key) === key ? value : t(key);
}
function inventoryModeLabel(value: string) {
  const key = `catalog.inventoryMode.${value}`;
  return t(key) === key ? value : t(key);
}
function deliveryTypeLabel(value: string) {
  const key = `catalog.deliveryType.${value}`;
  return t(key) === key ? value : t(key);
}

const productPages = computed(() =>
  pageWindow(productPage.value, productPageCount.value),
);
const productPageCount = computed(() =>
  Math.max(1, Math.ceil(productTotal.value / productPageSize.value)),
);
const variantPages = computed(() =>
  pageWindow(variantPage.value, variantPageCount.value),
);
const variantPageCount = computed(() =>
  Math.max(1, Math.ceil(variantTotal.value / variantPageSize.value)),
);
const tierPages = computed(() =>
  pageWindow(tierPage.value, tierPageCount.value),
);
const tierPageCount = computed(() =>
  Math.max(1, Math.ceil(tierTotal.value / tierPageSize.value)),
);
const levelPages = computed(() =>
  pageWindow(levelPage.value, levelPageCount.value),
);
const levelPageCount = computed(() =>
  Math.max(1, Math.ceil(levelTotal.value / levelPageSize.value)),
);
const editorTitle = computed(() => {
  const type = editor.value ? t(`catalog.editor.${editor.value}`) : "";
  return editingID.value
    ? t("catalog.editor.edit", { type })
    : t("catalog.editor.create", { type });
});
const editorHint = computed(() =>
  editor.value ? t(`catalog.editorHint.${editor.value}`) : "",
);
const inventorySwitchWarning = computed(
  () =>
    editingID.value &&
    productForm.inventoryMode !== productForm.originalInventoryMode,
);

function pageWindow(current: number, total: number) {
  const start = Math.max(1, Math.min(current - 2, total - 4));
  const end = Math.min(total, start + 4);
  return Array.from({ length: end - start + 1 }, (_, index) => start + index);
}

function apiMessage(error: unknown, fallback: string) {
  const failure = error as { response?: { data?: { message?: string } } };
  return failure.response?.data?.message || fallback;
}

function productCurrency(productID?: string) {
  return (
    products.value.find((product) => product.id === productID)?.currency ||
    productOptions.value.find((product) => product.id === productID)
      ?.currency ||
    storeCurrency.value
  );
}

function formatMoney(value?: number, currency?: string) {
  return formatMinorMoney(value, currency || storeCurrency.value, locale.value);
}

function centsToInput(value?: number, currency?: string) {
  return minorToMajor(value || 0, currency || storeCurrency.value);
}

function inputToCents(value: string, currency?: string) {
  try {
    const minor = majorToMinor(value, currency || storeCurrency.value);
    return BigInt(minor) >= 0n ? minorToSafeNumber(minor) : null;
  } catch {
    return null;
  }
}

function percentToBasisPoints(value: string) {
  const normalized = value.trim();
  if (!/^\d+(?:\.\d{1,2})?$/.test(normalized)) return null;
  const points = Math.round(Number(normalized) * 100);
  return Number.isInteger(points) && points >= 0 && points <= 10000
    ? points
    : null;
}

function formatPercent(points?: number) {
  return `${((Number(points) || 0) / 100).toFixed(2).replace(/\.00$/, "")}%`;
}

function formatTime(value?: string | null) {
  if (!value) return t("catalog.permanent");
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

function toLocalInput(value?: string | null) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function toISOStringOrNull(value: string) {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

function validReason(value: string) {
  const length = [...value.trim()].length;
  return length >= 4 && length <= 500;
}

function reasonHeaders(reason: string) {
  return { "X-Change-Reason": reason.trim() };
}

function parseAttributes(value: ProductVariant["attributes"]): AttributeRow[] {
  let parsed: unknown = value;
  if (typeof value === "string") {
    try {
      parsed = JSON.parse(value || "{}");
    } catch {
      parsed = {};
    }
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed))
    return [{ key: "", value: "" }];
  const rows = Object.entries(parsed as Record<string, unknown>)
    .filter(([, item]) => typeof item === "string")
    .map(([key, item]) => ({ key, value: String(item) }));
  return rows.length ? rows : [{ key: "", value: "" }];
}

function attributeSummary(value: ProductVariant["attributes"]) {
  return (
    parseAttributes(value)
      .filter((item) => item.key.trim())
      .map((item) => `${item.key}: ${item.value || "—"}`)
      .join(" · ") || t("catalog.noAttributes")
  );
}

function categoryName(id: string) {
  return (
    categories.value.find((item) => item.id === id)?.name ||
    t("catalog.uncategorized")
  );
}

function mergeProductOption(
  item: Pick<Product, "id" | "name"> & Partial<Product>,
) {
  if (productOptions.value.some((current) => current.id === item.id)) return;
  productOptions.value.push(item as Product);
}

function mergeMemberOption(
  item: Pick<MemberLevel, "id" | "name"> & Partial<MemberLevel>,
) {
  if (memberOptions.value.some((current) => current.id === item.id)) return;
  memberOptions.value.push(item as MemberLevel);
}

async function loadCategories() {
  categoriesLoading.value = true;
  categoriesError.value = "";
  try {
    const { data } = await adminApi.get("/categories");
    categories.value = Array.isArray(data.data) ? data.data : [];
  } catch (error: unknown) {
    categories.value = [];
    categoriesError.value = apiMessage(error, t("catalog.errLoadCategories"));
  } finally {
    categoriesLoading.value = false;
  }
}

async function loadProducts() {
  const request = ++productRequest;
  productLoading.value = true;
  productError.value = "";
  try {
    const { data } = await adminApi.get("/products", {
      params: {
        page: productPage.value,
        page_size: productPageSize.value,
        ...(productQuery.value ? { q: productQuery.value } : {}),
        ...(productStatus.value ? { status: productStatus.value } : {}),
        ...(productInventory.value
          ? { inventory_mode: productInventory.value }
          : {}),
        ...(productCategory.value
          ? { category_id: productCategory.value }
          : {}),
      },
    });
    if (request !== productRequest) return;
    const payload = data.data as PagePayload<Product>;
    products.value = Array.isArray(payload?.items) ? payload.items : [];
    productTotal.value = Number(payload?.total || 0);
    productPage.value = Number(payload?.page || productPage.value);
    productPageSize.value = Number(payload?.page_size || productPageSize.value);
    if (productPage.value > productPageCount.value && productPage.value > 1) {
      productPage.value = productPageCount.value;
      await loadProducts();
    }
  } catch (error: unknown) {
    if (request !== productRequest) return;
    products.value = [];
    productTotal.value = 0;
    productError.value = apiMessage(error, t("catalog.errLoadProducts"));
  } finally {
    if (request === productRequest) productLoading.value = false;
  }
}

async function loadPaymentChannels() {
  paymentChannelsLoading.value = true;
  paymentChannelsError.value = "";
  try {
    const { data } = await adminApi.get("/catalog/payment-channels");
    paymentChannels.value = Array.isArray(data.data) ? data.data : [];
  } catch (error: unknown) {
    paymentChannels.value = [];
    paymentChannelsError.value = apiMessage(
      error,
      t("catalog.errLoadPaymentChannels"),
    );
  } finally {
    paymentChannelsLoading.value = false;
  }
}

async function loadVariants() {
  const request = ++variantRequest;
  variantLoading.value = true;
  variantError.value = "";
  try {
    const { data } = await adminApi.get("/operations/variants", {
      params: {
        page: variantPage.value,
        page_size: variantPageSize.value,
        ...(variantQuery.value ? { q: variantQuery.value } : {}),
        ...(variantStatus.value ? { status: variantStatus.value } : {}),
        ...(variantProduct.value ? { product_id: variantProduct.value } : {}),
      },
    });
    if (request !== variantRequest) return;
    const payload = data.data as PagePayload<ProductVariant>;
    variants.value = Array.isArray(payload?.items) ? payload.items : [];
    variantTotal.value = Number(payload?.total || 0);
    variantPage.value = Number(payload?.page || variantPage.value);
    variantPageSize.value = Number(payload?.page_size || variantPageSize.value);
    if (variantPage.value > variantPageCount.value && variantPage.value > 1) {
      variantPage.value = variantPageCount.value;
      await loadVariants();
    }
  } catch (error: unknown) {
    if (request !== variantRequest) return;
    variants.value = [];
    variantTotal.value = 0;
    variantError.value = apiMessage(error, t("catalog.errLoadVariants"));
  } finally {
    if (request === variantRequest) variantLoading.value = false;
  }
}

async function loadTiers() {
  const request = ++tierRequest;
  tierLoading.value = true;
  tierError.value = "";
  try {
    const { data } = await adminApi.get("/operations/price-tiers", {
      params: {
        page: tierPage.value,
        page_size: tierPageSize.value,
        ...(tierProduct.value ? { product_id: tierProduct.value } : {}),
        ...(tierVariant.value ? { variant_id: tierVariant.value } : {}),
        ...(tierMember.value ? { member_level_id: tierMember.value } : {}),
      },
    });
    if (request !== tierRequest) return;
    const payload = data.data as PagePayload<ProductPriceTier>;
    tiers.value = Array.isArray(payload?.items) ? payload.items : [];
    tierTotal.value = Number(payload?.total || 0);
    tierPage.value = Number(payload?.page || tierPage.value);
    tierPageSize.value = Number(payload?.page_size || tierPageSize.value);
    if (tierPage.value > tierPageCount.value && tierPage.value > 1) {
      tierPage.value = tierPageCount.value;
      await loadTiers();
    }
  } catch (error: unknown) {
    if (request !== tierRequest) return;
    tiers.value = [];
    tierTotal.value = 0;
    tierError.value = apiMessage(error, t("catalog.errLoadTiers"));
  } finally {
    if (request === tierRequest) tierLoading.value = false;
  }
}

async function loadLevels() {
  const request = ++levelRequest;
  levelLoading.value = true;
  levelError.value = "";
  try {
    const { data } = await adminApi.get("/operations/member-levels", {
      params: {
        page: levelPage.value,
        page_size: levelPageSize.value,
        ...(levelQuery.value ? { q: levelQuery.value } : {}),
        ...(levelEnabled.value ? { enabled: levelEnabled.value } : {}),
      },
    });
    if (request !== levelRequest) return;
    const payload = data.data as PagePayload<MemberLevel>;
    levels.value = Array.isArray(payload?.items) ? payload.items : [];
    levelTotal.value = Number(payload?.total || 0);
    levelPage.value = Number(payload?.page || levelPage.value);
    levelPageSize.value = Number(payload?.page_size || levelPageSize.value);
    if (levelPage.value > levelPageCount.value && levelPage.value > 1) {
      levelPage.value = levelPageCount.value;
      await loadLevels();
    }
  } catch (error: unknown) {
    if (request !== levelRequest) return;
    levels.value = [];
    levelTotal.value = 0;
    levelError.value = apiMessage(error, t("catalog.errLoadLevels"));
  } finally {
    if (request === levelRequest) levelLoading.value = false;
  }
}

async function loadProductOptions() {
  productOptionLoading.value = true;
  try {
    const { data } = await adminApi.get("/products", {
      params: {
        page: 1,
        page_size: 100,
        ...(productOptionQuery.value.trim()
          ? { q: productOptionQuery.value.trim() }
          : {}),
      },
    });
    const payload = data.data as PagePayload<Product>;
    productOptions.value = Array.isArray(payload?.items) ? payload.items : [];
    productOptionTotal.value = Number(payload?.total || 0);
  } catch {
    productOptions.value = [];
    productOptionTotal.value = 0;
  } finally {
    productOptionLoading.value = false;
  }
}

async function loadMemberOptions() {
  try {
    const { data } = await adminApi.get("/operations/member-levels", {
      params: { page: 1, page_size: 100 },
    });
    const payload = data.data as PagePayload<MemberLevel>;
    memberOptions.value = Array.isArray(payload?.items) ? payload.items : [];
  } catch {
    memberOptions.value = [];
  }
}

async function loadVariantChoices(
  productID: string,
  selected?: ProductVariant | null,
) {
  variantChoices.value = [];
  if (!productID) return;
  variantChoiceLoading.value = true;
  try {
    const { data } = await adminApi.get("/operations/variants", {
      params: { page: 1, page_size: 100, product_id: productID },
    });
    const payload = data.data as PagePayload<ProductVariant>;
    variantChoices.value = Array.isArray(payload?.items) ? payload.items : [];
    if (
      selected &&
      !variantChoices.value.some((item) => item.id === selected.id)
    )
      variantChoices.value.push(selected);
  } catch {
    variantChoices.value = selected ? [selected] : [];
  } finally {
    variantChoiceLoading.value = false;
  }
}

async function loadActive() {
  if (activeTab.value === "categories") return loadCategories();
  if (activeTab.value === "products") return loadProducts();
  if (activeTab.value === "variants") return loadVariants();
  return pricingTab.value === "tiers" ? loadTiers() : loadLevels();
}

async function searchProducts() {
  productQuery.value = productSearch.value.trim();
  productPage.value = 1;
  await loadProducts();
}

async function searchVariants() {
  variantQuery.value = variantSearch.value.trim();
  variantPage.value = 1;
  await loadVariants();
}

async function searchLevels() {
  levelQuery.value = levelSearch.value.trim();
  levelPage.value = 1;
  await loadLevels();
}

async function changeProductPage(target: number) {
  if (
    target < 1 ||
    target > productPageCount.value ||
    target === productPage.value
  )
    return;
  productPage.value = target;
  await loadProducts();
}

async function changeVariantPage(target: number) {
  if (
    target < 1 ||
    target > variantPageCount.value ||
    target === variantPage.value
  )
    return;
  variantPage.value = target;
  await loadVariants();
}

async function changeTierPage(target: number) {
  if (target < 1 || target > tierPageCount.value || target === tierPage.value)
    return;
  tierPage.value = target;
  await loadTiers();
}

async function changeLevelPage(target: number) {
  if (target < 1 || target > levelPageCount.value || target === levelPage.value)
    return;
  levelPage.value = target;
  await loadLevels();
}

function resetEditor() {
  editingID.value = "";
  formError.value = "";
  resetEditorMedia();
}

function resetEditorMedia() {
  editorMedia.value = [];
  editorMediaLoading.value = false;
  editorMediaSaving.value = false;
  editorMediaError.value = "";
  pendingMediaFile.value = null;
  pendingMediaURL.value = "";
  pendingMediaAlt.value = "";
  pendingMediaRole.value = "cover";
  pendingMediaInputKey.value += 1;
}

function responseData<T>(payload: { data?: T } | T): T {
  if (
    payload &&
    typeof payload === "object" &&
    "data" in payload &&
    (payload as { data?: T }).data !== undefined
  )
    return (payload as { data: T }).data;
  return payload as T;
}

async function loadEditorMedia(
  ownerType: "category" | "product",
  ownerID: string,
) {
  if (!ownerID) {
    editorMedia.value = [];
    return;
  }
  editorMediaLoading.value = true;
  editorMediaError.value = "";
  try {
    const { data } = await adminApi.get("/catalog/media", {
      params: { owner_type: ownerType, owner_id: ownerID },
    });
    const items = responseData<CatalogMedia[]>(data);
    editorMedia.value = Array.isArray(items) ? items : [];
  } catch (error) {
    editorMediaError.value = apiMessage(error, t("catalog.errMediaLoad"));
  } finally {
    editorMediaLoading.value = false;
  }
}

function onEditorMediaFile(event: Event) {
  if (!canManage.value) return;
  const input = event.target as HTMLInputElement;
  pendingMediaFile.value = input.files?.[0] || null;
  if (pendingMediaFile.value) pendingMediaURL.value = "";
}

function clearPendingMedia() {
  pendingMediaFile.value = null;
  pendingMediaURL.value = "";
  pendingMediaAlt.value = "";
  pendingMediaInputKey.value += 1;
}

async function attachPendingMedia(
  ownerType: "category" | "product",
  ownerID: string,
  reason: string,
) {
  if (!canManage.value) return false;
  const remoteURL = pendingMediaURL.value.trim();
  const file = pendingMediaFile.value;
  if (!file && !remoteURL) return false;
  if (file && remoteURL) throw new Error(t("catalog.errMediaChooseOne"));
  const headers = reasonHeaders(reason);
  let asset: { id: string };
  if (file) {
    const body = new FormData();
    body.append("file", file, file.name);
    body.append("alt_text", pendingMediaAlt.value.trim());
    const { data } = await adminApi.post("/catalog/media/upload", body, {
      headers,
    });
    asset = responseData<{ id: string }>(data);
  } else {
    const { data } = await adminApi.post(
      "/catalog/media/mirror",
      { url: remoteURL, alt_text: pendingMediaAlt.value.trim() },
      { headers },
    );
    asset = responseData<{ id: string }>(data);
  }
  const role = ownerType === "category" ? "cover" : pendingMediaRole.value;
  const nextSort =
    role === "cover"
      ? 0
      : Math.min(
          1000,
          Math.max(
            -1,
            ...editorMedia.value
              .filter((item) => item.role === role)
              .map((item) => item.sort),
          ) + 1,
        );
  await adminApi.post(
    "/catalog/media",
    {
      owner_type: ownerType,
      owner_id: ownerID,
      asset_id: asset.id,
      role,
      sort: nextSort,
      alt_text: pendingMediaAlt.value.trim(),
      source_url: file ? "" : remoteURL,
      source_type: file ? "upload" : "manual",
    },
    { headers },
  );
  clearPendingMedia();
  await loadEditorMedia(ownerType, ownerID);
  return true;
}

async function addEditorMedia() {
  if (!canManage.value) return;
  if (
    !editor.value ||
    (editor.value !== "category" && editor.value !== "product")
  )
    return;
  if (!editingID.value) {
    editorMediaError.value = t("catalog.mediaSaveFirst");
    return;
  }
  const reason =
    editor.value === "category" ? categoryForm.reason : productForm.reason;
  if (!validReason(reason)) {
    editorMediaError.value = t("catalog.errReasonLength");
    return;
  }
  if (!pendingMediaFile.value && !pendingMediaURL.value.trim()) {
    editorMediaError.value = t("catalog.errMediaSourceRequired");
    return;
  }
  editorMediaSaving.value = true;
  editorMediaError.value = "";
  try {
    await attachPendingMedia(editor.value, editingID.value, reason);
    notice.value = t("catalog.mediaAttached");
  } catch (error) {
    editorMediaError.value = apiMessage(error, t("catalog.errMediaSave"));
  } finally {
    editorMediaSaving.value = false;
  }
}

async function removeEditorMedia(item: CatalogMedia) {
  if (!canManage.value) return;
  if (editorMediaSaving.value) return;
  const reason =
    editor.value === "category" ? categoryForm.reason : productForm.reason;
  if (!validReason(reason)) {
    editorMediaError.value = t("catalog.errReasonLength");
    return;
  }
  if (!window.confirm(t("catalog.confirmMediaRemove"))) return;
  editorMediaSaving.value = true;
  editorMediaError.value = "";
  try {
    await adminApi.delete(`/catalog/media/${encodeURIComponent(item.id)}`, {
      headers: reasonHeaders(reason),
    });
    if (editor.value === "category" || editor.value === "product")
      await loadEditorMedia(editor.value, editingID.value);
    notice.value = t("catalog.mediaRemoved");
  } catch (error) {
    editorMediaError.value = apiMessage(error, t("catalog.errMediaRemove"));
  } finally {
    editorMediaSaving.value = false;
  }
}

async function openCategory(item?: Category) {
  if (!canManage.value) return;
  resetEditor();
  editor.value = "category";
  editingID.value = item?.id || "";
  Object.assign(categoryForm, {
    parentID: item?.parent_id || "",
    name: item?.name || "",
    slug: item?.slug || "",
    description: item?.description || "",
    icon: item?.icon || "",
    sort: Number(item?.sort || 0),
    enabled: item?.enabled ?? true,
    reason: "",
  });
  if (item?.id) await loadEditorMedia("category", item.id);
}

async function openProduct(item?: Product) {
  if (!canManage.value) return;
  resetEditor();
  editor.value = "product";
  editingID.value = item?.id || "";
  Object.assign(productForm, {
    categoryID:
      item?.category_id ||
      categories.value.find((category) => category.enabled)?.id ||
      "",
    name: item?.name || "",
    slug: item?.slug || "",
    summary: item?.summary || "",
    description: item?.description || "",
    currency: item?.currency || storeCurrency.value,
    price: centsToInput(item?.price, item?.currency),
    comparePrice: centsToInput(item?.compare_price, item?.currency),
    costPrice: centsToInput(item?.cost_price, item?.currency),
    deliveryType: item?.delivery_type || "auto",
    inventoryMode: item?.inventory_mode || "local",
    originalInventoryMode: item?.inventory_mode || "local",
    minimumPurchase: Math.max(1, Number(item?.minimum_purchase || 1)),
    maximumPurchase: Math.max(0, Number(item?.maximum_purchase || 0)),
    status: item?.status || "draft",
    featured: item?.featured ?? false,
    tags: item?.tags || "",
    sort: Number(item?.sort || 0),
    paymentChannelIDs: Array.isArray(item?.payment_channel_ids)
      ? [...item.payment_channel_ids]
      : [],
    reason: "",
  });
  if (item?.id) {
    editorMedia.value = Array.isArray(item.media) ? [...item.media] : [];
    await loadEditorMedia("product", item.id);
  }
}

function resetInputFieldForm(item?: ProductInputField) {
  editingInputFieldID.value = item?.id || "";
  inputFieldError.value = "";
  Object.assign(inputFieldForm, {
    key: item?.key || "",
    label: item?.label || "",
    inputType: item?.input_type || "text",
    required: item?.required ?? true,
    sensitive: item?.sensitive ?? false,
    passToSupplier: item?.pass_to_supplier ?? false,
    placeholder: item?.placeholder || "",
    helpText: item?.help_text || "",
    optionsText: Array.isArray(item?.options) ? item.options.join("\n") : "",
    validationPattern: item?.validation_pattern || "",
    minLength: Number(item?.min_length ?? 1),
    maxLength: Number(item?.max_length ?? 200),
    sort: Number(item?.sort || 0),
    enabled: item?.enabled ?? true,
    reason: "",
  });
}

async function loadInputFields() {
  if (!inputFieldProduct.value) return;
  inputFieldsLoading.value = true;
  inputFieldError.value = "";
  try {
    const { data } = await adminApi.get(
      `/products/${encodeURIComponent(inputFieldProduct.value.id)}/input-fields`,
    );
    inputFields.value = Array.isArray(data.data) ? data.data : [];
  } catch (error) {
    inputFieldError.value = apiMessage(error, t("catalog.errInputFieldsLoad"));
  } finally {
    inputFieldsLoading.value = false;
  }
}

async function openInputFields(item: Product) {
  if (!canManage.value) return;
  inputFieldProduct.value = item;
  deletingInputField.value = null;
  inputFieldDeleteReason.value = "";
  resetInputFieldForm();
  await loadInputFields();
}

function closeInputFields() {
  if (inputFieldSaving.value || inputFieldDeleting.value) return;
  inputFieldProduct.value = null;
  inputFields.value = [];
  deletingInputField.value = null;
  resetInputFieldForm();
}

async function saveInputField() {
  if (!canManage.value) return;
  if (!inputFieldProduct.value || inputFieldSaving.value) return;
  inputFieldError.value = "";
  const key = inputFieldForm.key.trim().toLowerCase();
  const label = inputFieldForm.label.trim();
  const reason = inputFieldForm.reason.trim();
  const options = inputFieldForm.optionsText
    .split(/\r?\n/)
    .map((value) => value.trim())
    .filter(Boolean);
  if (!/^[a-z][a-z0-9_]{0,63}$/.test(key) || !label) {
    inputFieldError.value = t("catalog.errInputFieldRequired");
    return;
  }
  if (
    inputFieldForm.inputType === "select" &&
    (!options.length || new Set(options).size !== options.length)
  ) {
    inputFieldError.value = t("catalog.errInputFieldOptions");
    return;
  }
  if (
    inputFieldForm.minLength < 0 ||
    inputFieldForm.maxLength < 1 ||
    inputFieldForm.maxLength > 2000 ||
    inputFieldForm.minLength > inputFieldForm.maxLength
  ) {
    inputFieldError.value = t("catalog.errInputFieldLength");
    return;
  }
  if (!validReason(reason)) {
    inputFieldError.value = t("catalog.errReasonLength");
    return;
  }
  const payload = {
    key,
    label,
    input_type: inputFieldForm.inputType,
    required: inputFieldForm.required,
    sensitive: inputFieldForm.sensitive,
    pass_to_supplier: inputFieldForm.passToSupplier,
    placeholder: inputFieldForm.placeholder.trim(),
    help_text: inputFieldForm.helpText.trim(),
    options: inputFieldForm.inputType === "select" ? options : [],
    validation_pattern: inputFieldForm.validationPattern.trim(),
    min_length: Number(inputFieldForm.minLength),
    max_length: Number(inputFieldForm.maxLength),
    sort: Number(inputFieldForm.sort),
    enabled: inputFieldForm.enabled,
  };
  inputFieldSaving.value = true;
  try {
    if (editingInputFieldID.value)
      await adminApi.put(
        `/product-input-fields/${encodeURIComponent(editingInputFieldID.value)}`,
        payload,
        { headers: reasonHeaders(reason) },
      );
    else
      await adminApi.post(
        `/products/${encodeURIComponent(inputFieldProduct.value.id)}/input-fields`,
        payload,
        { headers: reasonHeaders(reason) },
      );
    notice.value = t("catalog.inputFieldSaved", { label });
    resetInputFieldForm();
    await loadInputFields();
  } catch (error) {
    inputFieldError.value = apiMessage(error, t("catalog.errInputFieldSave"));
  } finally {
    inputFieldSaving.value = false;
  }
}

async function confirmDeleteInputField() {
  if (!canManage.value) return;
  if (!deletingInputField.value || inputFieldDeleting.value) return;
  const reason = inputFieldDeleteReason.value.trim();
  if (!validReason(reason)) {
    inputFieldError.value = t("catalog.errReasonLength");
    return;
  }
  inputFieldDeleting.value = true;
  inputFieldError.value = "";
  try {
    await adminApi.delete(
      `/product-input-fields/${encodeURIComponent(deletingInputField.value.id)}`,
      { headers: reasonHeaders(reason) },
    );
    notice.value = t("catalog.inputFieldDeleted", {
      label: deletingInputField.value.label,
    });
    if (editingInputFieldID.value === deletingInputField.value.id)
      resetInputFieldForm();
    deletingInputField.value = null;
    inputFieldDeleteReason.value = "";
    await loadInputFields();
  } catch (error) {
    inputFieldError.value = apiMessage(error, t("catalog.errInputFieldDelete"));
  } finally {
    inputFieldDeleting.value = false;
  }
}

async function openVariant(item?: ProductVariant) {
  if (!canManage.value) return;
  resetEditor();
  editor.value = "variant";
  editingID.value = item?.id || "";
  if (item)
    mergeProductOption({ id: item.product_id, name: item.product_name });
  Object.assign(variantForm, {
    productID: item?.product_id || productOptions.value[0]?.id || "",
    sku: item?.sku || "",
    name: item?.name || "",
    attributes: item
      ? parseAttributes(item.attributes)
      : [{ key: "", value: "" }],
    price: centsToInput(item?.price, productCurrency(item?.product_id)),
    comparePrice: centsToInput(
      item?.compare_price,
      productCurrency(item?.product_id),
    ),
    costPrice: centsToInput(
      item?.cost_price,
      productCurrency(item?.product_id),
    ),
    status: item?.status || "active",
    sort: Number(item?.sort || 0),
    purchaseLimit: Number(item?.purchase_limit || 0),
    reason: "",
  });
}

async function openTier(item?: ProductPriceTier) {
  if (!canManage.value) return;
  resetEditor();
  editor.value = "tier";
  editingID.value = item?.id || "";
  if (item) {
    mergeProductOption({ id: item.product_id, name: item.product_name });
    if (item.member_level_id)
      mergeMemberOption({
        id: item.member_level_id,
        name: item.member_level_name,
      });
  }
  Object.assign(tierForm, {
    productID: item?.product_id || productOptions.value[0]?.id || "",
    variantID: item?.variant_id || "",
    memberLevelID: item?.member_level_id || "",
    minQuantity: Number(item?.min_quantity || 2),
    unitPrice: centsToInput(
      item?.unit_price,
      item?.currency || productCurrency(item?.product_id),
    ),
    startsAt: toLocalInput(item?.starts_at),
    endsAt: toLocalInput(item?.ends_at),
    reason: "",
  });
  const selectedVariant = item?.variant_id
    ? ({
        id: item.variant_id,
        product_id: item.product_id,
        name: item.variant_name,
        sku: "",
      } as ProductVariant)
    : null;
  await loadVariantChoices(tierForm.productID, selectedVariant);
}

function openLevel(item?: MemberLevel) {
  if (!canManage.value) return;
  resetEditor();
  editor.value = "level";
  editingID.value = item?.id || "";
  Object.assign(levelForm, {
    code: item?.code || "",
    name: item?.name || "",
    currency: item?.currency || storeCurrency.value,
    minimumSpend: centsToInput(item?.minimum_spend, item?.currency),
    discountPercent: (
      Number(item?.discount_basis_point ?? 10000) / 100
    ).toFixed(2),
    priority: Number(item?.priority || 0),
    enabled: item?.enabled ?? true,
    reason: "",
  });
}

function closeEditor() {
  if (saving.value || editorMediaSaving.value) return;
  editor.value = null;
  editingID.value = "";
  formError.value = "";
  resetEditorMedia();
}

function addAttribute() {
  if (!canManage.value) return;
  if (variantForm.attributes.length >= 20) return;
  variantForm.attributes.push({ key: "", value: "" });
}

function removeAttribute(index: number) {
  if (!canManage.value) return;
  variantForm.attributes.splice(index, 1);
  if (!variantForm.attributes.length)
    variantForm.attributes.push({ key: "", value: "" });
}

async function onTierProductChange() {
  tierForm.variantID = "";
  await loadVariantChoices(tierForm.productID);
}

function validateProductPayload() {
  const price = inputToCents(productForm.price, productForm.currency);
  const comparePrice = inputToCents(
    productForm.comparePrice,
    productForm.currency,
  );
  const costPrice = inputToCents(productForm.costPrice, productForm.currency);
  if (
    !productForm.categoryID ||
    !productForm.name.trim() ||
    !productForm.slug.trim()
  )
    return t("catalog.errProductRequired");
  if (price === null || comparePrice === null || costPrice === null)
    return t("catalog.errAmountInvalid");
  if (comparePrice !== 0 && comparePrice < price)
    return t("catalog.errCompareBelowPrice");
  if (
    !Number.isInteger(productForm.minimumPurchase) ||
    !Number.isInteger(productForm.maximumPurchase) ||
    productForm.minimumPurchase < 1 ||
    productForm.minimumPurchase > 1_000_000 ||
    productForm.maximumPurchase < 0 ||
    productForm.maximumPurchase > 1_000_000 ||
    (productForm.maximumPurchase > 0 &&
      productForm.maximumPurchase < productForm.minimumPurchase)
  )
    return t("catalog.errPurchaseRange");
  if (!validReason(productForm.reason)) return t("catalog.errReasonLength");
  return { price, comparePrice, costPrice };
}

async function saveCategory() {
  if (!canManage.value) return;
  if (!categoryForm.name.trim() || !categoryForm.slug.trim()) {
    formError.value = t("catalog.errCategoryRequired");
    return;
  }
  if (!validReason(categoryForm.reason)) {
    formError.value = t("catalog.errReasonLength");
    return;
  }
  const payload = {
    parent_id: categoryForm.parentID || null,
    name: categoryForm.name.trim(),
    slug: categoryForm.slug.trim(),
    description: categoryForm.description.trim(),
    icon: categoryForm.icon.trim(),
    sort: Number(categoryForm.sort),
    enabled: categoryForm.enabled,
  };
  const wasEditing = Boolean(editingID.value);
  let savedID = editingID.value;
  if (editingID.value) {
    const { data } = await adminApi.put(
      `/categories/${encodeURIComponent(editingID.value)}`,
      payload,
      { headers: reasonHeaders(categoryForm.reason) },
    );
    savedID = responseData<Category>(data).id || editingID.value;
  } else {
    const { data } = await adminApi.post("/categories", payload, {
      headers: reasonHeaders(categoryForm.reason),
    });
    savedID = responseData<Category>(data).id;
    editingID.value = savedID;
  }
  await attachPendingMedia("category", savedID, categoryForm.reason);
  notice.value = t("catalog.categorySaved", {
    name: payload.name,
    action: t(wasEditing ? "catalog.updated" : "catalog.created"),
  });
  await loadCategories();
}

async function saveProduct() {
  if (!canManage.value) return;
  const validated = validateProductPayload();
  if (typeof validated === "string") {
    formError.value = validated;
    return;
  }
  const payload = {
    category_id: productForm.categoryID,
    name: productForm.name.trim(),
    slug: productForm.slug.trim(),
    summary: productForm.summary.trim(),
    description: productForm.description.trim(),
    price: validated.price,
    compare_price: validated.comparePrice,
    cost_price: validated.costPrice,
    delivery_type: productForm.deliveryType,
    inventory_mode: productForm.inventoryMode,
    minimum_purchase: Number(productForm.minimumPurchase),
    maximum_purchase: Number(productForm.maximumPurchase),
    status: productForm.status,
    featured: productForm.featured,
    tags: productForm.tags.trim(),
    sort: Number(productForm.sort),
    payment_channel_ids: [...productForm.paymentChannelIDs],
  };
  const wasEditing = Boolean(editingID.value);
  let savedID = editingID.value;
  if (editingID.value) {
    const { data } = await adminApi.put(
      `/products/${encodeURIComponent(editingID.value)}`,
      payload,
      { headers: reasonHeaders(productForm.reason) },
    );
    savedID = responseData<Product>(data).id || editingID.value;
  } else {
    const { data } = await adminApi.post("/products", payload, {
      headers: reasonHeaders(productForm.reason),
    });
    savedID = responseData<Product>(data).id;
    editingID.value = savedID;
  }
  await attachPendingMedia("product", savedID, productForm.reason);
  notice.value = t("catalog.productSaved", {
    name: payload.name,
    action: t(wasEditing ? "catalog.updated" : "catalog.created"),
  });
  await Promise.all([loadProducts(), loadProductOptions()]);
}

async function saveVariant() {
  if (!canManage.value) return;
  const currency = productCurrency(variantForm.productID);
  const price = inputToCents(variantForm.price, currency);
  const comparePrice = inputToCents(variantForm.comparePrice, currency);
  const costPrice = inputToCents(variantForm.costPrice, currency);
  const rows = variantForm.attributes
    .map((item) => ({ key: item.key.trim(), value: item.value.trim() }))
    .filter((item) => item.key || item.value);
  if (
    !variantForm.productID ||
    !variantForm.sku.trim() ||
    !variantForm.name.trim()
  ) {
    formError.value = t("catalog.errVariantRequired");
    return;
  }
  if (
    price === null ||
    comparePrice === null ||
    costPrice === null ||
    (comparePrice !== 0 && comparePrice < price)
  ) {
    formError.value = t("catalog.errPriceCheck");
    return;
  }
  if (!rows.length || rows.some((item) => !item.key)) {
    formError.value = t("catalog.errAttributeRequired");
    return;
  }
  const keys = rows.map((item) => item.key.toLocaleLowerCase());
  if (new Set(keys).size !== keys.length) {
    formError.value = t("catalog.errAttributeDuplicate");
    return;
  }
  if (!validReason(variantForm.reason)) {
    formError.value = t("catalog.errReasonLength");
    return;
  }
  const payload = {
    product_id: variantForm.productID,
    sku: variantForm.sku.trim(),
    name: variantForm.name.trim(),
    attributes: Object.fromEntries(rows.map((item) => [item.key, item.value])),
    price,
    compare_price: comparePrice,
    cost_price: costPrice,
    status: variantForm.status,
    sort: Number(variantForm.sort),
    purchase_limit: Number(variantForm.purchaseLimit),
  };
  if (editingID.value)
    await adminApi.put(
      `/operations/variants/${encodeURIComponent(editingID.value)}`,
      payload,
      { headers: reasonHeaders(variantForm.reason) },
    );
  else
    await adminApi.post("/operations/variants", payload, {
      headers: reasonHeaders(variantForm.reason),
    });
  notice.value = t("catalog.variantSaved", {
    name: payload.name,
    action: t(editingID.value ? "catalog.updated" : "catalog.created"),
  });
  await loadVariants();
}

async function saveTier() {
  if (!canManage.value) return;
  const unitPrice = inputToCents(
    tierForm.unitPrice,
    productCurrency(tierForm.productID),
  );
  const startsAt = toISOStringOrNull(tierForm.startsAt);
  const endsAt = toISOStringOrNull(tierForm.endsAt);
  if (
    !tierForm.productID ||
    unitPrice === null ||
    unitPrice <= 0 ||
    Number(tierForm.minQuantity) < 1
  ) {
    formError.value = t("catalog.errTierRequired");
    return;
  }
  if (
    startsAt === undefined ||
    endsAt === undefined ||
    (startsAt && endsAt && new Date(endsAt) <= new Date(startsAt))
  ) {
    formError.value = t("catalog.errTierPeriod");
    return;
  }
  if (!validReason(tierForm.reason)) {
    formError.value = t("catalog.errReasonLength");
    return;
  }
  const payload = {
    product_id: tierForm.productID,
    variant_id: tierForm.variantID || null,
    member_level_id: tierForm.memberLevelID || null,
    min_quantity: Number(tierForm.minQuantity),
    unit_price: unitPrice,
    starts_at: startsAt,
    ends_at: endsAt,
  };
  if (editingID.value)
    await adminApi.put(
      `/operations/price-tiers/${encodeURIComponent(editingID.value)}`,
      payload,
      { headers: reasonHeaders(tierForm.reason) },
    );
  else
    await adminApi.post("/operations/price-tiers", payload, {
      headers: reasonHeaders(tierForm.reason),
    });
  notice.value = t("catalog.tierSaved", {
    action: t(editingID.value ? "catalog.updated" : "catalog.created"),
  });
  await loadTiers();
}

async function saveLevel() {
  if (!canManage.value) return;
  const minimumSpend = inputToCents(levelForm.minimumSpend, levelForm.currency);
  const discountBasisPoint = percentToBasisPoints(levelForm.discountPercent);
  if (
    !levelForm.code.trim() ||
    !levelForm.name.trim() ||
    minimumSpend === null ||
    discountBasisPoint === null
  ) {
    formError.value = t("catalog.errLevelCheck");
    return;
  }
  if (!validReason(levelForm.reason)) {
    formError.value = t("catalog.errReasonLength");
    return;
  }
  const payload = {
    code: levelForm.code.trim(),
    name: levelForm.name.trim(),
    currency: levelForm.currency,
    minimum_spend: minimumSpend,
    discount_basis_point: discountBasisPoint,
    priority: Number(levelForm.priority),
    enabled: levelForm.enabled,
  };
  if (editingID.value)
    await adminApi.put(
      `/operations/member-levels/${encodeURIComponent(editingID.value)}`,
      payload,
      { headers: reasonHeaders(levelForm.reason) },
    );
  else
    await adminApi.post("/operations/member-levels", payload, {
      headers: reasonHeaders(levelForm.reason),
    });
  notice.value = t("catalog.levelSaved", {
    name: payload.name,
    action: t(editingID.value ? "catalog.updated" : "catalog.created"),
  });
  await Promise.all([loadLevels(), loadMemberOptions()]);
}

async function saveEditor() {
  if (!canManage.value) return;
  if (!editor.value || saving.value) return;
  saving.value = true;
  formError.value = "";
  let saved = false;
  try {
    if (editor.value === "category") await saveCategory();
    if (editor.value === "product") await saveProduct();
    if (editor.value === "variant") await saveVariant();
    if (editor.value === "tier") await saveTier();
    if (editor.value === "level") await saveLevel();
    saved = !formError.value;
  } catch (error: unknown) {
    formError.value = apiMessage(error, t("catalog.errSave"));
  } finally {
    saving.value = false;
    if (saved) closeEditor();
  }
}

function askDelete(target: DeleteTarget) {
  if (!canManage.value) return;
  deleteTarget.value = target;
  deleteReason.value = "";
  deleteError.value = "";
}

function closeDelete() {
  if (deleting.value) return;
  deleteTarget.value = null;
  deleteReason.value = "";
  deleteError.value = "";
}

async function confirmDelete() {
  if (!canManage.value) return;
  const target = deleteTarget.value;
  if (!target || deleting.value) return;
  if (!validReason(deleteReason.value)) {
    deleteError.value = t("catalog.errDeleteReason");
    return;
  }
  const paths: Record<EditorKind, string> = {
    category: `/categories/${encodeURIComponent(target.id)}`,
    product: `/products/${encodeURIComponent(target.id)}`,
    variant: `/operations/variants/${encodeURIComponent(target.id)}`,
    tier: `/operations/price-tiers/${encodeURIComponent(target.id)}`,
    level: `/operations/member-levels/${encodeURIComponent(target.id)}`,
  };
  deleting.value = true;
  deleteError.value = "";
  let deleted = false;
  try {
    await adminApi.delete(paths[target.kind], {
      headers: reasonHeaders(deleteReason.value),
    });
    notice.value = t("catalog.deleted", { name: target.name });
    deleted = true;
    if (target.kind === "category") await loadCategories();
    if (target.kind === "product")
      await Promise.all([loadProducts(), loadProductOptions()]);
    if (target.kind === "variant") await loadVariants();
    if (target.kind === "tier") await loadTiers();
    if (target.kind === "level")
      await Promise.all([loadLevels(), loadMemberOptions()]);
  } catch (error: unknown) {
    deleteError.value = apiMessage(error, t("catalog.errDelete"));
  } finally {
    deleting.value = false;
    if (deleted) closeDelete();
  }
}

watch(
  () => [route.meta.defaultTab, route.meta.defaultPricingTab] as const,
  async ([value, pricingValue]) => {
    const next: CatalogTab =
      value === "categories" || value === "variants" || value === "pricing"
        ? value
        : "products";
    activeTab.value = next;
    if (next === "pricing")
      pricingTab.value = pricingValue === "levels" ? "levels" : "tiers";
    notice.value = "";
    await loadActive();
  },
  { immediate: true },
);

onMounted(async () => {
  await Promise.all([
    loadCategories(),
    loadProductOptions(),
    loadMemberOptions(),
    loadPaymentChannels(),
  ]);
  if (route.path === "/products" && route.query.create === "1") {
    openProduct();
    const query = { ...route.query };
    delete query.create;
    await router.replace({ path: route.path, query });
  }
});
</script>

<template>
  <section class="catalog-shell">
    <div v-if="notice" class="catalog-notice success" role="status">
      <CheckCircle2 :size="17" />
      <span>{{ notice }}</span>
      <button :aria-label="t('catalog.closeNotice')" @click="notice = ''">
        <X :size="15" />
      </button>
    </div>

    <template v-if="activeTab === 'categories'">
      <header class="section-heading">
        <div>
          <span class="eyebrow">{{ t("adminKicker.catalogControl") }}</span>
          <h2>{{ t("catalog.categoriesTitle") }}</h2>
          <p>{{ t("catalog.categoriesHint") }}</p>
        </div>
        <button v-if="canManage" class="primary-action" @click="openCategory()">
          <Plus :size="16" /> {{ t("catalog.createCategory") }}
        </button>
      </header>

      <div class="category-panel">
        <div class="panel-title">
          <div>
            <strong>{{ t("catalog.categoriesTitle") }}</strong
            ><span>{{ t("catalog.categoriesHint") }}</span>
          </div>
          <button v-if="canManage" class="small-action" @click="openCategory()">
            <Plus :size="14" /> {{ t("catalog.createCategory") }}
          </button>
        </div>
        <p v-if="categoriesError" class="inline-error">{{ categoriesError }}</p>
        <div v-if="categoriesLoading" class="compact-loading">
          <LoaderCircle :size="16" class="spin" />
          {{ t("catalog.loadingCategories") }}
        </div>
        <div v-else-if="!categories.length" class="compact-empty">
          {{ t("catalog.noCategories") }}
        </div>
        <div v-else class="category-grid">
          <article
            v-for="category in categories"
            :key="category.id"
            class="category-card"
          >
            <div class="category-icon">
              <img
                v-if="category.image_url"
                :src="category.image_url"
                :alt="category.name"
                loading="lazy"
              />
              <span v-else>{{ category.icon || "#" }}</span>
            </div>
            <div class="category-copy">
              <div>
                <strong>{{ category.name }}</strong
                ><span
                  :class="[
                    'status-dot',
                    category.enabled ? 'success' : 'muted',
                  ]"
                  >{{
                    category.enabled
                      ? t("catalog.status.enabled")
                      : t("catalog.status.disabled")
                  }}</span
                >
              </div>
              <code>{{ category.slug }}</code>
              <small v-if="category.parent_id" class="category-parent">
                {{ t("catalog.parentCategory") }}:
                {{ categoryName(category.parent_id) }}
              </small>
              <p>
                {{ category.description || t("catalog.noCategoryDescription") }}
              </p>
            </div>
            <div v-if="canManage" class="row-actions">
              <button
                :title="t('catalog.editCategory')"
                @click="openCategory(category)"
              >
                <Edit3 :size="14" />
              </button>
              <button
                class="danger"
                :title="t('catalog.deleteCategory')"
                @click="
                  askDelete({
                    kind: 'category',
                    id: category.id,
                    name: category.name,
                  })
                "
              >
                <Trash2 :size="14" />
              </button>
            </div>
          </article>
        </div>
      </div>
    </template>

    <template v-else-if="activeTab === 'products'">
      <header class="section-heading">
        <div>
          <span class="eyebrow">{{ t("adminKicker.catalogControl") }}</span>
          <h2>{{ t("catalog.productsHeading") }}</h2>
          <p>{{ t("catalog.productsIntro") }}</p>
        </div>
        <button v-if="canManage" class="primary-action" @click="openProduct()">
          <Plus :size="16" /> {{ t("catalog.createProduct") }}
        </button>
      </header>

      <div class="toolbar">
        <form class="search-box" @submit.prevent="searchProducts">
          <Search :size="15" />
          <input
            v-model="productSearch"
            :placeholder="t('catalog.searchPlaceholder')"
          />
          <button type="submit">{{ t("catalog.search") }}</button>
        </form>
        <select
          v-model="productCategory"
          @change="
            productPage = 1;
            loadProducts();
          "
        >
          <option value="">{{ t("catalog.allCategories") }}</option>
          <option
            v-for="category in categories"
            :key="category.id"
            :value="category.id"
          >
            {{ category.name }}
          </option>
        </select>
        <select
          v-model="productStatus"
          @change="
            productPage = 1;
            loadProducts();
          "
        >
          <option value="">{{ t("catalog.allSaleStatus") }}</option>
          <option value="draft">{{ t("catalog.status.draft") }}</option>
          <option value="on_sale">{{ t("catalog.status.on_sale") }}</option>
          <option value="off_sale">{{ t("catalog.status.off_sale") }}</option>
        </select>
        <select
          v-model="productInventory"
          @change="
            productPage = 1;
            loadProducts();
          "
        >
          <option value="">{{ t("catalog.allInventoryMode") }}</option>
          <option value="local">{{ t("catalog.inventoryMode.local") }}</option>
          <option value="supplier">
            {{ t("catalog.inventoryMode.supplier") }}
          </option>
        </select>
        <button
          class="icon-button"
          :title="t('catalog.refresh')"
          @click="loadProducts"
        >
          <RefreshCw :size="16" :class="{ spin: productLoading }" />
        </button>
      </div>

      <div v-if="productError" class="catalog-notice error">
        <AlertTriangle :size="17" /><span>{{ productError }}</span
        ><button @click="loadProducts">{{ t("catalog.retry") }}</button>
      </div>
      <div class="data-card">
        <div v-if="productLoading" class="table-state">
          <LoaderCircle :size="22" class="spin" />
          {{ t("catalog.loadingProducts") }}
        </div>
        <div v-else-if="!products.length" class="table-state">
          <Package :size="28" /><strong>{{ t("catalog.noProducts") }}</strong
          ><span>{{ t("catalog.noProductsHint") }}</span>
        </div>
        <div v-else class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>{{ t("catalog.colProduct") }}</th>
                <th>{{ t("catalog.colPriceCost") }}</th>
                <th>{{ t("catalog.colFulfillment") }}</th>
                <th>{{ t("catalog.colStatus") }}</th>
                <th>{{ t("catalog.colSortSold") }}</th>
                <th class="align-right">{{ t("catalog.colActions") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in products" :key="item.id">
                <td :data-label="t('catalog.colProduct')">
                  <div class="product-primary">
                    <div class="product-thumb">
                      <img
                        v-if="item.cover_url"
                        :src="item.cover_url"
                        :alt="item.name"
                        loading="lazy"
                      />
                      <Package v-else :size="18" />
                    </div>
                    <div class="primary-cell">
                      <strong>{{ item.name }}</strong
                      ><code>{{ item.slug }}</code
                      ><span
                        >{{
                          item.category?.name || categoryName(item.category_id)
                        }}
                        · {{ item.summary || t("catalog.noSummary") }}</span
                      >
                    </div>
                  </div>
                </td>
                <td :data-label="t('catalog.colPriceCost')">
                  <strong>{{ formatMoney(item.price, item.currency) }}</strong
                  ><span class="cell-sub">{{
                    t("catalog.compareCost", {
                      compare: formatMoney(item.compare_price, item.currency),
                      cost: formatMoney(item.cost_price, item.currency),
                    })
                  }}</span>
                </td>
                <td :data-label="t('catalog.colFulfillment')">
                  <span>{{ inventoryModeLabel(item.inventory_mode) }}</span
                  ><span class="cell-sub">{{
                    deliveryTypeLabel(item.delivery_type)
                  }}</span>
                </td>
                <td :data-label="t('catalog.colStatus')">
                  <span :class="['pill', item.status]">{{
                    statusLabel(item.status)
                  }}</span
                  ><span v-if="item.featured" class="mini-tag">{{
                    t("catalog.featured")
                  }}</span>
                </td>
                <td :data-label="t('catalog.colSortSold')">
                  <strong>{{ item.sort }}</strong
                  ><span class="cell-sub">{{
                    t("catalog.soldCount", { count: item.sold_count || 0 })
                  }}</span>
                </td>
                <td :data-label="t('catalog.colActions')" class="align-right">
                  <div v-if="canManage" class="row-actions">
                    <button
                      :title="t('catalog.manageInputFields')"
                      @click="openInputFields(item)"
                    >
                      <ListChecks :size="15" />
                    </button>
                    <button
                      :title="t('catalog.editProduct')"
                      @click="openProduct(item)"
                    >
                      <Edit3 :size="15" /></button
                    ><button
                      class="danger"
                      :title="t('catalog.deleteProduct')"
                      @click="
                        askDelete({
                          kind: 'product',
                          id: item.id,
                          name: item.name,
                        })
                      "
                    >
                      <Trash2 :size="15" />
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <footer class="pager">
          <span>{{ t("catalog.totalItems", { count: productTotal }) }}</span>
          <select
            v-model.number="productPageSize"
            @change="
              productPage = 1;
              loadProducts();
            "
          >
            <option :value="10">
              {{ t("catalog.perPage", { size: 10 }) }}
            </option>
            <option :value="20">
              {{ t("catalog.perPage", { size: 20 }) }}
            </option>
            <option :value="50">
              {{ t("catalog.perPage", { size: 50 }) }}
            </option>
          </select>
          <div>
            <button
              :disabled="productPage <= 1"
              @click="changeProductPage(productPage - 1)"
            >
              <ChevronLeft :size="15" /></button
            ><button
              v-for="number in productPages"
              :key="number"
              :class="{ active: number === productPage }"
              @click="changeProductPage(number)"
            >
              {{ number }}</button
            ><button
              :disabled="productPage >= productPageCount"
              @click="changeProductPage(productPage + 1)"
            >
              <ChevronRight :size="15" />
            </button>
          </div>
        </footer>
      </div>
    </template>

    <template v-else-if="activeTab === 'variants'">
      <header class="section-heading">
        <div>
          <span class="eyebrow">{{ t("adminKicker.skuOperations") }}</span>
          <h2>{{ t("catalog.variantsHeading") }}</h2>
          <p>{{ t("catalog.variantsIntro") }}</p>
        </div>
        <button v-if="canManage" class="primary-action" @click="openVariant()">
          <Plus :size="16" /> {{ t("catalog.createVariant") }}
        </button>
      </header>
      <div class="toolbar">
        <form class="search-box" @submit.prevent="searchVariants">
          <Search :size="15" /><input
            v-model="variantSearch"
            :placeholder="t('catalog.variantSearchPlaceholder')"
          /><button type="submit">{{ t("catalog.search") }}</button>
        </form>
        <select
          v-model="variantProduct"
          @change="
            variantPage = 1;
            loadVariants();
          "
        >
          <option value="">{{ t("catalog.allProducts") }}</option>
          <option
            v-for="item in productOptions"
            :key="item.id"
            :value="item.id"
          >
            {{ item.name }}
          </option>
        </select>
        <select
          v-model="variantStatus"
          @change="
            variantPage = 1;
            loadVariants();
          "
        >
          <option value="">{{ t("catalog.allStatus") }}</option>
          <option value="active">{{ t("catalog.status.active") }}</option>
          <option value="inactive">{{ t("catalog.status.inactive") }}</option>
        </select>
        <button
          class="icon-button"
          :title="t('catalog.searchMoreProducts')"
          @click="
            productOptionQuery = variantSearch;
            loadProductOptions();
          "
        >
          <Package :size="16" />
        </button>
        <button
          class="icon-button"
          :title="t('catalog.refresh')"
          @click="loadVariants"
        >
          <RefreshCw :size="16" :class="{ spin: variantLoading }" />
        </button>
      </div>
      <p v-if="productOptionTotal > productOptions.length" class="filter-hint">
        {{ t("catalog.variantFilterHint") }}
      </p>
      <div v-if="variantError" class="catalog-notice error">
        <AlertTriangle :size="17" /><span>{{ variantError }}</span
        ><button @click="loadVariants">{{ t("catalog.retry") }}</button>
      </div>
      <div class="data-card">
        <div v-if="variantLoading" class="table-state">
          <LoaderCircle :size="22" class="spin" />
          {{ t("catalog.loadingVariants") }}
        </div>
        <div v-else-if="!variants.length" class="table-state">
          <Boxes :size="28" /><strong>{{ t("catalog.noVariants") }}</strong
          ><span>{{ t("catalog.noVariantsHint") }}</span>
        </div>
        <div v-else class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>{{ t("catalog.colSkuVariant") }}</th>
                <th>{{ t("catalog.colBelongProduct") }}</th>
                <th>{{ t("catalog.colAttributes") }}</th>
                <th>{{ t("catalog.colPriceCost") }}</th>
                <th>{{ t("catalog.colLimit") }}</th>
                <th>{{ t("catalog.colStatus") }}</th>
                <th class="align-right">{{ t("catalog.colActions") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in variants" :key="item.id">
                <td :data-label="t('catalog.colSkuVariant')">
                  <div class="primary-cell">
                    <strong>{{ item.name }}</strong
                    ><code>{{ item.sku }}</code>
                  </div>
                </td>
                <td :data-label="t('catalog.colBelongProduct')">
                  {{ item.product_name || item.product_id }}
                </td>
                <td :data-label="t('catalog.colAttributes')">
                  <span class="attribute-summary">{{
                    attributeSummary(item.attributes)
                  }}</span>
                </td>
                <td :data-label="t('catalog.colPriceCost')">
                  <strong>{{
                    formatMoney(item.price, productCurrency(item.product_id))
                  }}</strong
                  ><span class="cell-sub">{{
                    t("catalog.compareCost", {
                      compare: formatMoney(
                        item.compare_price,
                        productCurrency(item.product_id),
                      ),
                      cost: formatMoney(
                        item.cost_price,
                        productCurrency(item.product_id),
                      ),
                    })
                  }}</span>
                </td>
                <td :data-label="t('catalog.colLimit')">
                  <span>{{
                    item.purchase_limit > 0
                      ? t("catalog.perOrderLimit", {
                          count: item.purchase_limit,
                        })
                      : t("catalog.unlimited")
                  }}</span
                  ><span class="cell-sub">{{
                    t("catalog.sortValue", { value: item.sort })
                  }}</span>
                </td>
                <td :data-label="t('catalog.colStatus')">
                  <span :class="['pill', item.status]">{{
                    statusLabel(item.status)
                  }}</span>
                </td>
                <td :data-label="t('catalog.colActions')" class="align-right">
                  <div v-if="canManage" class="row-actions">
                    <button
                      :title="t('catalog.editVariant')"
                      @click="openVariant(item)"
                    >
                      <Edit3 :size="15" /></button
                    ><button
                      class="danger"
                      :title="t('catalog.deleteVariant')"
                      @click="
                        askDelete({
                          kind: 'variant',
                          id: item.id,
                          name: `${item.sku} ${item.name}`,
                        })
                      "
                    >
                      <Trash2 :size="15" />
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <footer class="pager">
          <span>{{ t("catalog.totalSkus", { count: variantTotal }) }}</span
          ><select
            v-model.number="variantPageSize"
            @change="
              variantPage = 1;
              loadVariants();
            "
          >
            <option :value="10">
              {{ t("catalog.perPage", { size: 10 }) }}
            </option>
            <option :value="20">
              {{ t("catalog.perPage", { size: 20 }) }}
            </option>
            <option :value="50">
              {{ t("catalog.perPage", { size: 50 }) }}
            </option>
          </select>
          <div>
            <button
              :disabled="variantPage <= 1"
              @click="changeVariantPage(variantPage - 1)"
            >
              <ChevronLeft :size="15" /></button
            ><button
              v-for="number in variantPages"
              :key="number"
              :class="{ active: number === variantPage }"
              @click="changeVariantPage(number)"
            >
              {{ number }}</button
            ><button
              :disabled="variantPage >= variantPageCount"
              @click="changeVariantPage(variantPage + 1)"
            >
              <ChevronRight :size="15" />
            </button>
          </div>
        </footer>
      </div>
    </template>

    <template v-else>
      <header class="section-heading">
        <div>
          <span class="eyebrow">{{ t("adminKicker.pricingEngine") }}</span>
          <h2>{{ t("catalog.pricingHeading") }}</h2>
          <p>{{ t("catalog.pricingIntro") }}</p>
        </div>
        <button
          v-if="canManage"
          class="primary-action"
          @click="pricingTab === 'tiers' ? openTier() : openLevel()"
        >
          <Plus :size="16" />
          {{
            pricingTab === "tiers"
              ? t("catalog.createTier")
              : t("catalog.createLevel")
          }}
        </button>
      </header>
      <template v-if="pricingTab === 'tiers'">
        <div class="toolbar">
          <select
            v-model="tierProduct"
            @change="
              tierVariant = '';
              tierPage = 1;
              loadVariantChoices(tierProduct);
              loadTiers();
            "
          >
            <option value="">{{ t("catalog.allProducts") }}</option>
            <option
              v-for="item in productOptions"
              :key="item.id"
              :value="item.id"
            >
              {{ item.name }}
            </option>
          </select>
          <select
            v-model="tierVariant"
            :disabled="!tierProduct || variantChoiceLoading"
            @change="
              tierPage = 1;
              loadTiers();
            "
          >
            <option value="">{{ t("catalog.allVariants") }}</option>
            <option
              v-for="item in variantChoices"
              :key="item.id"
              :value="item.id"
            >
              {{ item.sku }} · {{ item.name }}
            </option>
          </select>
          <select
            v-model="tierMember"
            @change="
              tierPage = 1;
              loadTiers();
            "
          >
            <option value="">{{ t("catalog.allMemberScope") }}</option>
            <option
              v-for="item in memberOptions"
              :key="item.id"
              :value="item.id"
            >
              {{ item.name }}
            </option>
          </select>
          <button
            class="icon-button"
            :title="t('catalog.refresh')"
            @click="loadTiers"
          >
            <RefreshCw :size="16" :class="{ spin: tierLoading }" />
          </button>
        </div>
        <div v-if="tierError" class="catalog-notice error">
          <AlertTriangle :size="17" /><span>{{ tierError }}</span
          ><button @click="loadTiers">{{ t("catalog.retry") }}</button>
        </div>
        <div class="data-card">
          <div v-if="tierLoading" class="table-state">
            <LoaderCircle :size="22" class="spin" />
            {{ t("catalog.loadingTiers") }}
          </div>
          <div v-else-if="!tiers.length" class="table-state">
            <BadgePercent :size="28" /><strong>{{
              t("catalog.noTiers")
            }}</strong
            ><span>{{ t("catalog.noTiersHint") }}</span>
          </div>
          <div v-else class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{{ t("catalog.colProductVariant") }}</th>
                  <th>{{ t("catalog.colMemberScope") }}</th>
                  <th>{{ t("catalog.colMinQuantity") }}</th>
                  <th>{{ t("catalog.colUnitPrice") }}</th>
                  <th>{{ t("catalog.colPeriod") }}</th>
                  <th class="align-right">{{ t("catalog.colActions") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in tiers" :key="item.id">
                  <td :data-label="t('catalog.colProductVariant')">
                    <div class="primary-cell">
                      <strong>{{ item.product_name || item.product_id }}</strong
                      ><span>{{
                        item.variant_name || t("catalog.allVariants")
                      }}</span>
                    </div>
                  </td>
                  <td :data-label="t('catalog.colMemberScope')">
                    {{ item.member_level_name || t("catalog.allCustomers") }}
                  </td>
                  <td :data-label="t('catalog.colMinQuantity')">
                    <strong>{{
                      t("catalog.quantityUnits", {
                        count: item.min_quantity,
                      })
                    }}</strong>
                  </td>
                  <td :data-label="t('catalog.colUnitPrice')">
                    <strong>{{
                      formatMoney(
                        item.unit_price,
                        item.currency || productCurrency(item.product_id),
                      )
                    }}</strong>
                  </td>
                  <td :data-label="t('catalog.colPeriod')">
                    <span>{{
                      item.starts_at
                        ? formatTime(item.starts_at)
                        : t("catalog.immediate")
                    }}</span
                    ><span class="cell-sub">{{
                      item.ends_at
                        ? t("catalog.until", {
                            time: formatTime(item.ends_at),
                          })
                        : t("catalog.longTerm")
                    }}</span>
                  </td>
                  <td :data-label="t('catalog.colActions')" class="align-right">
                    <div v-if="canManage" class="row-actions">
                      <button
                        :title="t('catalog.editTier')"
                        @click="openTier(item)"
                      >
                        <Edit3 :size="15" /></button
                      ><button
                        class="danger"
                        :title="t('catalog.deleteTier')"
                        @click="
                          askDelete({
                            kind: 'tier',
                            id: item.id,
                            name: t('catalog.tierDeleteName', {
                              name: item.product_name,
                              count: item.min_quantity,
                            }),
                          })
                        "
                      >
                        <Trash2 :size="15" />
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <footer class="pager">
            <span>{{ t("catalog.totalTierRules", { count: tierTotal }) }}</span
            ><select
              v-model.number="tierPageSize"
              @change="
                tierPage = 1;
                loadTiers();
              "
            >
              <option :value="10">
                {{ t("catalog.perPage", { size: 10 }) }}
              </option>
              <option :value="20">
                {{ t("catalog.perPage", { size: 20 }) }}
              </option>
              <option :value="50">
                {{ t("catalog.perPage", { size: 50 }) }}
              </option>
            </select>
            <div>
              <button
                :disabled="tierPage <= 1"
                @click="changeTierPage(tierPage - 1)"
              >
                <ChevronLeft :size="15" /></button
              ><button
                v-for="number in tierPages"
                :key="number"
                :class="{ active: number === tierPage }"
                @click="changeTierPage(number)"
              >
                {{ number }}</button
              ><button
                :disabled="tierPage >= tierPageCount"
                @click="changeTierPage(tierPage + 1)"
              >
                <ChevronRight :size="15" />
              </button>
            </div>
          </footer>
        </div>
      </template>

      <template v-else>
        <div class="toolbar">
          <form class="search-box" @submit.prevent="searchLevels">
            <Search :size="15" /><input
              v-model="levelSearch"
              :placeholder="t('catalog.levelSearchPlaceholder')"
            /><button type="submit">{{ t("catalog.search") }}</button>
          </form>
          <select
            v-model="levelEnabled"
            @change="
              levelPage = 1;
              loadLevels();
            "
          >
            <option value="">{{ t("catalog.allStatus") }}</option>
            <option value="true">{{ t("catalog.status.enabled") }}</option>
            <option value="false">
              {{ t("catalog.status.disabled") }}
            </option></select
          ><button
            class="icon-button"
            :title="t('catalog.refresh')"
            @click="loadLevels"
          >
            <RefreshCw :size="16" :class="{ spin: levelLoading }" />
          </button>
        </div>
        <div v-if="levelError" class="catalog-notice error">
          <AlertTriangle :size="17" /><span>{{ levelError }}</span
          ><button @click="loadLevels">{{ t("catalog.retry") }}</button>
        </div>
        <div class="data-card">
          <div v-if="levelLoading" class="table-state">
            <LoaderCircle :size="22" class="spin" />
            {{ t("catalog.loadingLevels") }}
          </div>
          <div v-else-if="!levels.length" class="table-state">
            <Layers3 :size="28" /><strong>{{ t("catalog.noLevels") }}</strong
            ><span>{{ t("catalog.noLevelsHint") }}</span>
          </div>
          <div v-else class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{{ t("catalog.colLevel") }}</th>
                  <th>{{ t("catalog.colSpend") }}</th>
                  <th>{{ t("catalog.colDiscount") }}</th>
                  <th>{{ t("catalog.colPriority") }}</th>
                  <th>{{ t("catalog.colRelated") }}</th>
                  <th>{{ t("catalog.colStatus") }}</th>
                  <th class="align-right">{{ t("catalog.colActions") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in levels" :key="item.id">
                  <td :data-label="t('catalog.colLevel')">
                    <div class="primary-cell">
                      <strong>{{ item.name }}</strong
                      ><code>{{ item.code }}</code>
                    </div>
                  </td>
                  <td :data-label="t('catalog.colSpend')">
                    {{ formatMoney(item.minimum_spend, item.currency) }}
                  </td>
                  <td :data-label="t('catalog.colDiscount')">
                    <strong>{{
                      formatPercent(item.discount_basis_point)
                    }}</strong>
                  </td>
                  <td :data-label="t('catalog.colPriority')">
                    {{ item.priority }}
                  </td>
                  <td :data-label="t('catalog.colRelated')">
                    <span>{{
                      t("catalog.memberCount", {
                        count: item.membership_count || 0,
                      })
                    }}</span
                    ><span class="cell-sub">{{
                      t("catalog.tierRuleCount", {
                        count: item.price_tier_count || 0,
                      })
                    }}</span>
                  </td>
                  <td :data-label="t('catalog.colStatus')">
                    <span
                      :class="['pill', item.enabled ? 'active' : 'inactive']"
                      >{{
                        item.enabled
                          ? t("catalog.status.enabled")
                          : t("catalog.status.disabled")
                      }}</span
                    >
                  </td>
                  <td :data-label="t('catalog.colActions')" class="align-right">
                    <div v-if="canManage" class="row-actions">
                      <button
                        :title="t('catalog.editLevel')"
                        @click="openLevel(item)"
                      >
                        <Edit3 :size="15" /></button
                      ><button
                        class="danger"
                        :title="t('catalog.deleteLevel')"
                        @click="
                          askDelete({
                            kind: 'level',
                            id: item.id,
                            name: item.name,
                          })
                        "
                      >
                        <Trash2 :size="15" />
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <footer class="pager">
            <span>{{ t("catalog.totalLevels", { count: levelTotal }) }}</span
            ><select
              v-model.number="levelPageSize"
              @change="
                levelPage = 1;
                loadLevels();
              "
            >
              <option :value="10">
                {{ t("catalog.perPage", { size: 10 }) }}
              </option>
              <option :value="20">
                {{ t("catalog.perPage", { size: 20 }) }}
              </option>
              <option :value="50">
                {{ t("catalog.perPage", { size: 50 }) }}
              </option>
            </select>
            <div>
              <button
                :disabled="levelPage <= 1"
                @click="changeLevelPage(levelPage - 1)"
              >
                <ChevronLeft :size="15" /></button
              ><button
                v-for="number in levelPages"
                :key="number"
                :class="{ active: number === levelPage }"
                @click="changeLevelPage(number)"
              >
                {{ number }}</button
              ><button
                :disabled="levelPage >= levelPageCount"
                @click="changeLevelPage(levelPage + 1)"
              >
                <ChevronRight :size="15" />
              </button>
            </div>
          </footer>
        </div>
      </template>
    </template>

    <Teleport to="body">
      <div
        v-if="editor && canManage"
        class="modal-backdrop"
        @mousedown.self="closeEditor"
      >
        <section
          class="editor-modal"
          role="dialog"
          aria-modal="true"
          :aria-label="editorTitle"
        >
          <header>
            <div>
              <span class="eyebrow">{{
                t("adminKicker.structuredEditor")
              }}</span>
              <h3>{{ editorTitle }}</h3>
              <p>{{ editorHint }}</p>
            </div>
            <button
              class="close-button"
              :aria-label="t('catalog.close')"
              @click="closeEditor"
            >
              <X :size="18" />
            </button>
          </header>
          <form @submit.prevent="saveEditor">
            <div v-if="editor === 'category'" class="form-grid">
              <label class="full"
                ><span>{{ t("catalog.parentCategory") }}</span
                ><select v-model="categoryForm.parentID">
                  <option value="">{{ t("catalog.topLevelCategory") }}</option>
                  <option
                    v-for="option in categoryTreeOptions"
                    :key="option.id"
                    :value="option.id"
                    :disabled="option.disabled"
                  >
                    {{
                      option.label +
                      (option.enabled ? "" : t("catalog.disabledSuffix"))
                    }}
                  </option>
                </select></label
              >
              <label
                ><span>{{ t("catalog.categoryName") }}</span
                ><input
                  v-model="categoryForm.name"
                  maxlength="100"
                  :placeholder="t('catalog.categoryNamePlaceholder')"
              /></label>
              <label
                ><span>{{ t("catalog.slug") }}</span
                ><input
                  v-model="categoryForm.slug"
                  maxlength="120"
                  placeholder="cloud-services"
              /></label>
              <label
                ><span>{{ t("catalog.iconText") }}</span
                ><input
                  v-model="categoryForm.icon"
                  maxlength="40"
                  :placeholder="t('catalog.iconPlaceholder')"
              /></label>
              <label
                ><span>{{ t("catalog.sort") }}</span
                ><input
                  v-model.number="categoryForm.sort"
                  type="number"
                  min="0"
                  max="1000000"
              /></label>
              <label class="full"
                ><span>{{ t("catalog.categoryDescription") }}</span
                ><textarea
                  v-model="categoryForm.description"
                  maxlength="2000"
                  rows="3"
                  :placeholder="t('catalog.categoryDescriptionPlaceholder')"
                ></textarea>
              </label>
              <label class="toggle-field full"
                ><input v-model="categoryForm.enabled" type="checkbox" /><span
                  ><strong>{{ t("catalog.enableCategory") }}</strong
                  ><small>{{ t("catalog.enableCategoryHint") }}</small></span
                ></label
              >
              <section class="media-editor full">
                <div class="media-editor-heading">
                  <div>
                    <strong>{{ t("catalog.categoryImage") }}</strong>
                    <small>{{ t("catalog.categoryImageHint") }}</small>
                  </div>
                  <ImagePlus :size="18" />
                </div>
                <div class="media-source-grid">
                  <label>
                    <span
                      ><UploadCloud :size="13" />
                      {{ t("catalog.mediaLocalFile") }}</span
                    >
                    <input
                      :key="pendingMediaInputKey"
                      type="file"
                      accept="image/jpeg,image/png,image/gif,image/webp"
                      @change="onEditorMediaFile"
                    />
                  </label>
                  <label>
                    <span
                      ><Link2 :size="13" />
                      {{ t("catalog.mediaRemoteURL") }}</span
                    >
                    <input
                      v-model="pendingMediaURL"
                      type="url"
                      maxlength="1000"
                      placeholder="https://cdn.your-domain.tld/category.webp"
                      @input="
                        pendingMediaFile = null;
                        pendingMediaInputKey += 1;
                      "
                    />
                  </label>
                  <label class="full">
                    <span>{{ t("catalog.mediaAlt") }}</span>
                    <input v-model="pendingMediaAlt" maxlength="300" />
                  </label>
                </div>
                <div class="media-editor-actions">
                  <small v-if="!editingID">{{
                    t("catalog.mediaSaveFirst")
                  }}</small>
                  <button
                    v-else
                    type="button"
                    class="small-action"
                    :disabled="editorMediaSaving"
                    @click="addEditorMedia"
                  >
                    <LoaderCircle
                      v-if="editorMediaSaving"
                      class="spin"
                      :size="14"
                    />
                    <UploadCloud v-else :size="14" />
                    {{ t("catalog.mediaUploadAttach") }}
                  </button>
                </div>
                <p v-if="editorMediaError" class="inline-error">
                  {{ editorMediaError }}
                </p>
                <div v-if="editorMediaLoading" class="compact-loading">
                  <LoaderCircle class="spin" :size="15" />
                  {{ t("catalog.mediaLoading") }}
                </div>
                <div v-else-if="editorMedia.length" class="media-preview-grid">
                  <article v-for="mediaItem in editorMedia" :key="mediaItem.id">
                    <img
                      :src="mediaItem.url"
                      :alt="mediaItem.alt_text || categoryForm.name"
                    />
                    <span>{{ t(`catalog.mediaRole.${mediaItem.role}`) }}</span>
                    <button
                      type="button"
                      class="danger"
                      :title="t('catalog.mediaRemove')"
                      @click="removeEditorMedia(mediaItem)"
                    >
                      <Trash2 :size="14" />
                    </button>
                  </article>
                </div>
              </section>
            </div>

            <div v-if="editor === 'product'" class="form-grid">
              <label
                ><span>{{ t("catalog.categoryLabel") }}</span
                ><select v-model="productForm.categoryID">
                  <option value="" disabled>
                    {{ t("catalog.selectCategory") }}
                  </option>
                  <option
                    v-for="category in categories"
                    :key="category.id"
                    :value="category.id"
                  >
                    {{
                      category.name +
                      (category.enabled ? "" : t("catalog.disabledSuffix"))
                    }}
                  </option>
                </select></label
              >
              <label
                ><span>{{ t("catalog.productName") }}</span
                ><input
                  v-model="productForm.name"
                  maxlength="160"
                  :placeholder="t('catalog.productNamePlaceholder')"
              /></label>
              <label
                ><span>{{ t("catalog.slug") }}</span
                ><input
                  v-model="productForm.slug"
                  maxlength="180"
                  placeholder="product-slug"
              /></label>
              <label
                ><span>{{ t("catalog.saleStatus") }}</span
                ><select v-model="productForm.status">
                  <option value="draft">{{ t("catalog.status.draft") }}</option>
                  <option value="on_sale">
                    {{ t("catalog.status.on_sale") }}
                  </option>
                  <option value="off_sale">
                    {{ t("catalog.status.off_sale") }}
                  </option>
                </select></label
              >
              <label
                ><span>{{ t("catalog.salePrice") }}</span
                ><input
                  v-model="productForm.price"
                  inputmode="decimal"
                  :step="majorInputStep(productForm.currency)"
                  placeholder="99.00"
              /></label>
              <label
                ><span>{{ t("catalog.comparePrice") }}</span
                ><input
                  v-model="productForm.comparePrice"
                  inputmode="decimal"
                  :step="majorInputStep(productForm.currency)"
                  placeholder="129.00"
              /></label>
              <label
                ><span>{{ t("catalog.costPrice") }}</span
                ><input
                  v-model="productForm.costPrice"
                  inputmode="decimal"
                  :step="majorInputStep(productForm.currency)"
                  placeholder="70.00"
              /></label>
              <label
                ><span>{{ t("catalog.sort") }}</span
                ><input
                  v-model.number="productForm.sort"
                  type="number"
                  min="0"
                  max="1000000"
              /></label>
              <label
                ><span>{{ t("catalog.deliveryTypeLabel") }}</span
                ><select v-model="productForm.deliveryType">
                  <option value="auto">
                    {{ t("catalog.deliveryType.auto") }}
                  </option>
                  <option value="manual">
                    {{ t("catalog.deliveryType.manual") }}
                  </option>
                </select></label
              >
              <label
                ><span>{{ t("catalog.inventoryModeLabel") }}</span
                ><select v-model="productForm.inventoryMode">
                  <option value="local">
                    {{ t("catalog.inventoryMode.local") }}
                  </option>
                  <option value="supplier">
                    {{ t("catalog.inventoryMode.supplier") }}
                  </option>
                </select></label
              >
              <label
                ><span>{{ t("catalog.minimumPurchase") }}</span
                ><input
                  v-model.number="productForm.minimumPurchase"
                  type="number"
                  min="1"
                  max="1000000"
              /></label>
              <label
                ><span>{{ t("catalog.maximumPurchase") }}</span
                ><input
                  v-model.number="productForm.maximumPurchase"
                  type="number"
                  min="0"
                  max="1000000"
                /><small>{{ t("catalog.zeroUnlimited") }}</small></label
              >
              <div v-if="inventorySwitchWarning" class="field-warning full">
                <AlertTriangle :size="17" /><span>{{
                  t("catalog.inventorySwitchWarning")
                }}</span>
              </div>
              <fieldset class="channel-picker full">
                <legend>{{ t("catalog.paymentChannels") }}</legend>
                <p>{{ t("catalog.paymentChannelsHint") }}</p>
                <div v-if="paymentChannelsLoading" class="channel-empty">
                  <LoaderCircle class="spin" :size="16" />
                  {{ t("catalog.loadingPaymentChannels") }}
                </div>
                <div v-else-if="paymentChannelsError" class="field-warning">
                  <AlertTriangle :size="16" />
                  <span>{{ paymentChannelsError }}</span>
                  <button type="button" @click="loadPaymentChannels">
                    {{ t("catalog.retry") }}
                  </button>
                </div>
                <div v-else-if="paymentChannels.length" class="channel-options">
                  <label
                    v-for="paymentChannel in paymentChannels"
                    :key="paymentChannel.id"
                    class="channel-option"
                  >
                    <input
                      v-model="productForm.paymentChannelIDs"
                      type="checkbox"
                      :value="paymentChannel.id"
                      :disabled="
                        !paymentChannel.enabled &&
                        !productForm.paymentChannelIDs.includes(
                          paymentChannel.id,
                        )
                      "
                    />
                    <span>
                      <strong>
                        {{ paymentChannel.name }}
                        {{
                          paymentChannel.enabled
                            ? ""
                            : t("catalog.paymentChannelDisabled")
                        }}
                      </strong>
                      <small>
                        {{ paymentChannel.code }} ·
                        {{
                          t("catalog.paymentChannelFee", {
                            rate: (paymentChannel.fee_rate / 100).toFixed(2),
                          })
                        }}
                      </small>
                    </span>
                  </label>
                </div>
                <div v-else class="channel-empty">
                  {{ t("catalog.noPaymentChannels") }}
                </div>
                <small>{{ t("catalog.noChannelRestrictionHint") }}</small>
              </fieldset>
              <label class="full"
                ><span>{{ t("catalog.summary") }}</span
                ><textarea
                  v-model="productForm.summary"
                  maxlength="500"
                  rows="2"
                  :placeholder="t('catalog.summaryPlaceholder')"
                ></textarea>
              </label>
              <label class="full"
                ><span>{{ t("catalog.description") }}</span
                ><textarea
                  v-model="productForm.description"
                  maxlength="100000"
                  rows="6"
                  :placeholder="t('catalog.descriptionPlaceholder')"
                ></textarea>
              </label>
              <label class="full"
                ><span>{{ t("catalog.tags") }}</span
                ><input
                  v-model="productForm.tags"
                  maxlength="500"
                  :placeholder="t('catalog.tagsPlaceholder')"
              /></label>
              <label class="toggle-field full"
                ><input v-model="productForm.featured" type="checkbox" /><span
                  ><strong>{{ t("catalog.featuredLabel") }}</strong
                  ><small>{{ t("catalog.featuredHint") }}</small></span
                ></label
              >
              <section class="media-editor full">
                <div class="media-editor-heading">
                  <div>
                    <strong>{{ t("catalog.productMedia") }}</strong>
                    <small>{{ t("catalog.productMediaHint") }}</small>
                  </div>
                  <ImagePlus :size="18" />
                </div>
                <div class="media-source-grid">
                  <label>
                    <span
                      ><UploadCloud :size="13" />
                      {{ t("catalog.mediaLocalFile") }}</span
                    >
                    <input
                      :key="pendingMediaInputKey"
                      type="file"
                      accept="image/jpeg,image/png,image/gif,image/webp"
                      @change="onEditorMediaFile"
                    />
                  </label>
                  <label>
                    <span
                      ><Link2 :size="13" />
                      {{ t("catalog.mediaRemoteURL") }}</span
                    >
                    <input
                      v-model="pendingMediaURL"
                      type="url"
                      maxlength="1000"
                      placeholder="https://cdn.your-domain.tld/product.webp"
                      @input="
                        pendingMediaFile = null;
                        pendingMediaInputKey += 1;
                      "
                    />
                  </label>
                  <label>
                    <span>{{ t("catalog.mediaRoleLabel") }}</span>
                    <select v-model="pendingMediaRole">
                      <option value="cover">
                        {{ t("catalog.mediaRole.cover") }}
                      </option>
                      <option value="gallery">
                        {{ t("catalog.mediaRole.gallery") }}
                      </option>
                      <option value="detail">
                        {{ t("catalog.mediaRole.detail") }}
                      </option>
                    </select>
                  </label>
                  <label>
                    <span>{{ t("catalog.mediaAlt") }}</span>
                    <input v-model="pendingMediaAlt" maxlength="300" />
                  </label>
                </div>
                <div class="media-editor-actions">
                  <small v-if="!editingID">{{
                    t("catalog.mediaSaveFirst")
                  }}</small>
                  <button
                    v-else
                    type="button"
                    class="small-action"
                    :disabled="editorMediaSaving"
                    @click="addEditorMedia"
                  >
                    <LoaderCircle
                      v-if="editorMediaSaving"
                      class="spin"
                      :size="14"
                    />
                    <UploadCloud v-else :size="14" />
                    {{ t("catalog.mediaUploadAttach") }}
                  </button>
                </div>
                <p v-if="editorMediaError" class="inline-error">
                  {{ editorMediaError }}
                </p>
                <div v-if="editorMediaLoading" class="compact-loading">
                  <LoaderCircle class="spin" :size="15" />
                  {{ t("catalog.mediaLoading") }}
                </div>
                <div v-else-if="editorMedia.length" class="media-preview-grid">
                  <article v-for="mediaItem in editorMedia" :key="mediaItem.id">
                    <img
                      :src="mediaItem.url"
                      :alt="mediaItem.alt_text || productForm.name"
                    />
                    <span>{{ t(`catalog.mediaRole.${mediaItem.role}`) }}</span>
                    <button
                      type="button"
                      class="danger"
                      :title="t('catalog.mediaRemove')"
                      @click="removeEditorMedia(mediaItem)"
                    >
                      <Trash2 :size="14" />
                    </button>
                  </article>
                </div>
              </section>
            </div>

            <div v-if="editor === 'variant'" class="form-grid">
              <label class="full"
                ><span>{{ t("catalog.belongProductLabel") }}</span>
                <div class="compound-field">
                  <select
                    v-model="variantForm.productID"
                    :disabled="Boolean(editingID)"
                  >
                    <option value="" disabled>
                      {{ t("catalog.selectProduct") }}
                    </option>
                    <option
                      v-for="item in productOptions"
                      :key="item.id"
                      :value="item.id"
                    >
                      {{ item.name }}
                    </option></select
                  ><input
                    v-model="productOptionQuery"
                    :placeholder="t('catalog.searchMoreProducts')"
                    @keyup.enter.prevent="loadProductOptions"
                  /><button type="button" @click="loadProductOptions">
                    <Search :size="14" />
                  </button>
                </div>
                <small v-if="editingID">{{
                  t("catalog.variantNotTransferable")
                }}</small></label
              >
              <label
                ><span>{{ t("catalog.sku") }}</span
                ><input
                  v-model="variantForm.sku"
                  maxlength="100"
                  placeholder="SKU.CN-12"
              /></label>
              <label
                ><span>{{ t("catalog.variantName") }}</span
                ><input
                  v-model="variantForm.name"
                  maxlength="160"
                  :placeholder="t('catalog.variantNamePlaceholder')"
              /></label>
              <label
                ><span>{{ t("catalog.salePrice") }}</span
                ><input
                  v-model="variantForm.price"
                  inputmode="decimal"
                  :step="
                    majorInputStep(productCurrency(variantForm.productID))
                  "
              /></label>
              <label
                ><span>{{ t("catalog.comparePrice") }}</span
                ><input
                  v-model="variantForm.comparePrice"
                  inputmode="decimal"
                  :step="
                    majorInputStep(productCurrency(variantForm.productID))
                  "
              /></label>
              <label
                ><span>{{ t("catalog.costPrice") }}</span
                ><input
                  v-model="variantForm.costPrice"
                  inputmode="decimal"
                  :step="
                    majorInputStep(productCurrency(variantForm.productID))
                  "
              /></label>
              <label
                ><span>{{ t("catalog.statusLabel") }}</span
                ><select v-model="variantForm.status">
                  <option value="active">
                    {{ t("catalog.status.active") }}
                  </option>
                  <option value="inactive">
                    {{ t("catalog.status.inactive") }}
                  </option>
                </select></label
              >
              <label
                ><span>{{ t("catalog.purchaseLimit") }}</span
                ><input
                  v-model.number="variantForm.purchaseLimit"
                  type="number"
                  min="0"
                  max="1000000"
                /><small>{{ t("catalog.zeroUnlimited") }}</small></label
              >
              <label
                ><span>{{ t("catalog.sort") }}</span
                ><input
                  v-model.number="variantForm.sort"
                  type="number"
                  min="0"
                  max="1000000"
              /></label>
              <fieldset class="attribute-editor full">
                <legend>{{ t("catalog.attributes") }}</legend>
                <div
                  v-for="(attribute, index) in variantForm.attributes"
                  :key="index"
                  class="attribute-row"
                >
                  <input
                    v-model="attribute.key"
                    maxlength="80"
                    :placeholder="t('catalog.attributeKeyPlaceholder')"
                  /><input
                    v-model="attribute.value"
                    maxlength="200"
                    :placeholder="t('catalog.attributeValuePlaceholder')"
                  /><button
                    type="button"
                    :title="t('catalog.removeAttribute')"
                    @click="removeAttribute(index)"
                  >
                    <X :size="15" />
                  </button>
                </div>
                <button
                  type="button"
                  class="add-row"
                  :disabled="variantForm.attributes.length >= 20"
                  @click="addAttribute"
                >
                  <Plus :size="14" /> {{ t("catalog.addAttribute") }}
                </button>
              </fieldset>
            </div>

            <div v-if="editor === 'tier'" class="form-grid">
              <label class="full"
                ><span>{{ t("catalog.productLabel") }}</span>
                <div class="compound-field">
                  <select
                    v-model="tierForm.productID"
                    @change="onTierProductChange"
                  >
                    <option value="" disabled>
                      {{ t("catalog.selectProduct") }}
                    </option>
                    <option
                      v-for="item in productOptions"
                      :key="item.id"
                      :value="item.id"
                    >
                      {{ item.name }}
                    </option></select
                  ><input
                    v-model="productOptionQuery"
                    :placeholder="t('catalog.searchMoreProducts')"
                    @keyup.enter.prevent="loadProductOptions"
                  /><button type="button" @click="loadProductOptions">
                    <Search :size="14" />
                  </button></div
              ></label>
              <label
                ><span>{{ t("catalog.specifiedVariant") }}</span
                ><select
                  v-model="tierForm.variantID"
                  :disabled="variantChoiceLoading"
                >
                  <option value="">{{ t("catalog.allVariants") }}</option>
                  <option
                    v-for="item in variantChoices"
                    :key="item.id"
                    :value="item.id"
                  >
                    {{ item.sku }} · {{ item.name }}
                  </option>
                </select></label
              >
              <label
                ><span>{{ t("catalog.memberScope") }}</span
                ><select v-model="tierForm.memberLevelID">
                  <option value="">{{ t("catalog.allCustomers") }}</option>
                  <option
                    v-for="item in memberOptions"
                    :key="item.id"
                    :value="item.id"
                  >
                    {{ item.name }}
                  </option>
                </select></label
              >
              <label
                ><span>{{ t("catalog.minQuantity") }}</span
                ><input
                  v-model.number="tierForm.minQuantity"
                  type="number"
                  min="1"
                  max="1000000"
              /></label>
              <label
                ><span>{{ t("catalog.unitPrice") }}</span
                ><input
                  v-model="tierForm.unitPrice"
                  inputmode="decimal"
                  :step="majorInputStep(productCurrency(tierForm.productID))"
                  placeholder="88.00"
              /></label>
              <label
                ><span>{{ t("catalog.startsAt") }}</span
                ><input v-model="tierForm.startsAt" type="datetime-local"
              /></label>
              <label
                ><span>{{ t("catalog.endsAt") }}</span
                ><input v-model="tierForm.endsAt" type="datetime-local"
              /></label>
              <div class="field-note full">
                {{ t("catalog.tierPeriodNote") }}
              </div>
            </div>

            <div v-if="editor === 'level'" class="form-grid">
              <label
                ><span>{{ t("catalog.levelCode") }}</span
                ><input
                  v-model="levelForm.code"
                  maxlength="60"
                  placeholder="vip_gold"
              /></label>
              <label
                ><span>{{ t("catalog.levelName") }}</span
                ><input
                  v-model="levelForm.name"
                  maxlength="100"
                  :placeholder="t('catalog.levelNamePlaceholder')"
              /></label>
              <label
                ><span>{{ t("catalog.minimumSpend") }}</span
                ><input
                  v-model="levelForm.minimumSpend"
                  inputmode="decimal"
                  :step="majorInputStep(levelForm.currency)"
                  placeholder="1000.00"
              /></label>
              <label>
                <span>{{ t("currency.storeCurrency") }}</span>
                <select v-model="levelForm.currency">
                  <option
                    v-for="currency in Object.values(currencyDirectory)"
                    :key="currency.code"
                    :value="currency.code"
                  >
                    {{ currency.code }} · {{ currency.name }}
                  </option>
                </select>
              </label>
              <label
                ><span>{{ t("catalog.discountPercent") }}</span
                ><input
                  v-model="levelForm.discountPercent"
                  inputmode="decimal"
                  placeholder="95.00"
                /><small>{{ t("catalog.discountHint") }}</small></label
              >
              <label
                ><span>{{ t("catalog.priority") }}</span
                ><input
                  v-model.number="levelForm.priority"
                  type="number"
                  min="0"
                  max="1000000"
              /></label>
              <label class="toggle-field"
                ><input v-model="levelForm.enabled" type="checkbox" /><span
                  ><strong>{{ t("catalog.enableLevel") }}</strong
                  ><small>{{ t("catalog.enableLevelHint") }}</small></span
                ></label
              >
            </div>

            <label class="reason-field"
              ><span>{{ t("catalog.changeReason") }}</span
              ><textarea
                v-if="editor === 'category'"
                v-model="categoryForm.reason"
                rows="2"
                maxlength="500"
                :placeholder="t('catalog.reasonPlaceholder')"
              ></textarea
              ><textarea
                v-if="editor === 'product'"
                v-model="productForm.reason"
                rows="2"
                maxlength="500"
                :placeholder="t('catalog.reasonPlaceholder')"
              ></textarea
              ><textarea
                v-if="editor === 'variant'"
                v-model="variantForm.reason"
                rows="2"
                maxlength="500"
                :placeholder="t('catalog.reasonPlaceholder')"
              ></textarea
              ><textarea
                v-if="editor === 'tier'"
                v-model="tierForm.reason"
                rows="2"
                maxlength="500"
                :placeholder="t('catalog.reasonPlaceholder')"
              ></textarea
              ><textarea
                v-if="editor === 'level'"
                v-model="levelForm.reason"
                rows="2"
                maxlength="500"
                :placeholder="t('catalog.reasonPlaceholder')"
              ></textarea>
            </label>
            <p v-if="formError" class="modal-error">
              <AlertTriangle :size="16" /> {{ formError }}
            </p>
            <footer>
              <button
                type="button"
                class="secondary-button"
                @click="closeEditor"
              >
                {{ t("catalog.cancel") }}</button
              ><button type="submit" class="primary-action" :disabled="saving">
                <LoaderCircle
                  v-if="saving"
                  :size="15"
                  class="spin"
                /><CheckCircle2 v-else :size="15" />
                {{ saving ? t("catalog.saving") : t("catalog.save") }}
              </button>
            </footer>
          </form>
        </section>
      </div>

      <div
        v-if="inputFieldProduct && canManage"
        class="modal-backdrop"
        @mousedown.self="closeInputFields"
      >
        <section
          class="editor-modal input-field-modal"
          role="dialog"
          aria-modal="true"
          :aria-label="t('catalog.manageInputFields')"
        >
          <header>
            <div>
              <span class="eyebrow">{{
                t("adminKicker.secureOrderInputs")
              }}</span>
              <h3>
                {{
                  t("catalog.inputFieldsTitle", {
                    name: inputFieldProduct.name,
                  })
                }}
              </h3>
              <p>{{ t("catalog.inputFieldsHint") }}</p>
            </div>
            <button
              class="close-button"
              :aria-label="t('catalog.close')"
              @click="closeInputFields"
            >
              <X :size="18" />
            </button>
          </header>
          <div class="input-field-manager">
            <aside>
              <div class="input-field-list-head">
                <div>
                  <strong>{{ t("catalog.configuredInputFields") }}</strong>
                  <small>{{ inputFields.length }}/20</small>
                </div>
                <button
                  type="button"
                  class="secondary-button"
                  :disabled="inputFields.length >= 20"
                  @click="resetInputFieldForm()"
                >
                  <Plus :size="14" />{{ t("catalog.newInputField") }}
                </button>
              </div>
              <div v-if="inputFieldsLoading" class="input-field-empty">
                <LoaderCircle :size="18" class="spin" />
                {{ t("catalog.loading") }}
              </div>
              <div v-else-if="!inputFields.length" class="input-field-empty">
                <ListChecks :size="22" />
                {{ t("catalog.noInputFields") }}
              </div>
              <div v-else class="input-field-list">
                <article
                  v-for="field in inputFields"
                  :key="field.id"
                  :class="{ active: editingInputFieldID === field.id }"
                >
                  <button
                    type="button"
                    class="input-field-summary"
                    @click="resetInputFieldForm(field)"
                  >
                    <strong>{{ field.label }}</strong>
                    <code>{{ field.key }}</code>
                    <span>
                      {{ t(`catalog.inputType.${field.input_type}`) }}
                      <b v-if="field.required">{{ t("catalog.required") }}</b>
                      <b v-if="field.sensitive">{{ t("catalog.sensitive") }}</b>
                      <b v-if="field.pass_to_supplier">{{
                        t("catalog.passToSupplier")
                      }}</b>
                      <b v-if="!field.enabled">{{ t("catalog.disabled") }}</b>
                    </span>
                  </button>
                  <button
                    type="button"
                    class="input-field-trash"
                    :title="t('catalog.deleteInputField')"
                    @click="
                      deletingInputField = field;
                      inputFieldDeleteReason = '';
                    "
                  >
                    <Trash2 :size="14" />
                  </button>
                </article>
              </div>
              <div v-if="deletingInputField" class="input-field-delete">
                <strong>{{
                  t("catalog.deleteInputFieldConfirm", {
                    label: deletingInputField.label,
                  })
                }}</strong>
                <textarea
                  v-model="inputFieldDeleteReason"
                  rows="2"
                  maxlength="500"
                  :placeholder="t('catalog.reasonPlaceholder')"
                ></textarea>
                <div>
                  <button
                    type="button"
                    class="secondary-button"
                    @click="deletingInputField = null"
                  >
                    {{ t("catalog.cancel") }}
                  </button>
                  <button
                    type="button"
                    class="danger-button"
                    :disabled="inputFieldDeleting"
                    @click="confirmDeleteInputField"
                  >
                    <LoaderCircle
                      v-if="inputFieldDeleting"
                      :size="14"
                      class="spin"
                    /><Trash2 v-else :size="14" />
                    {{ t("catalog.confirmDeleteBtn") }}
                  </button>
                </div>
              </div>
            </aside>
            <form class="input-field-editor" @submit.prevent="saveInputField">
              <div class="input-field-editor-title">
                <div>
                  <strong>{{
                    editingInputFieldID
                      ? t("catalog.editInputField")
                      : t("catalog.newInputField")
                  }}</strong>
                  <small>{{ t("catalog.inputFieldEditorHint") }}</small>
                </div>
                <button
                  v-if="editingInputFieldID"
                  type="button"
                  class="secondary-button"
                  @click="resetInputFieldForm()"
                >
                  <Plus :size="14" />{{ t("catalog.newInputField") }}
                </button>
              </div>
              <div class="form-grid">
                <label
                  ><span>{{ t("catalog.inputFieldKey") }}</span
                  ><input
                    v-model="inputFieldForm.key"
                    maxlength="64"
                    :disabled="Boolean(editingInputFieldID)"
                    placeholder="account_id"
                  /><small v-if="editingInputFieldID">{{
                    t("catalog.inputFieldKeyImmutableHint")
                  }}</small></label
                >
                <label
                  ><span>{{ t("catalog.inputFieldLabel") }}</span
                  ><input
                    v-model="inputFieldForm.label"
                    maxlength="120"
                    :placeholder="t('catalog.inputFieldLabelPlaceholder')"
                /></label>
                <label
                  ><span>{{ t("catalog.inputFieldType") }}</span
                  ><select v-model="inputFieldForm.inputType">
                    <option value="text">
                      {{ t("catalog.inputType.text") }}
                    </option>
                    <option value="email">
                      {{ t("catalog.inputType.email") }}
                    </option>
                    <option value="number">
                      {{ t("catalog.inputType.number") }}
                    </option>
                    <option value="select">
                      {{ t("catalog.inputType.select") }}
                    </option>
                    <option value="textarea">
                      {{ t("catalog.inputType.textarea") }}
                    </option>
                  </select></label
                >
                <label
                  ><span>{{ t("catalog.sort") }}</span
                  ><input
                    v-model.number="inputFieldForm.sort"
                    type="number"
                    min="0"
                    max="1000000"
                /></label>
                <label class="full"
                  ><span>{{ t("catalog.inputFieldPlaceholder") }}</span
                  ><input
                    v-model="inputFieldForm.placeholder"
                    maxlength="200"
                    :placeholder="t('catalog.inputFieldPlaceholderExample')"
                /></label>
                <label class="full"
                  ><span>{{ t("catalog.inputFieldHelp") }}</span
                  ><textarea
                    v-model="inputFieldForm.helpText"
                    maxlength="500"
                    rows="2"
                  ></textarea>
                </label>
                <label v-if="inputFieldForm.inputType === 'select'" class="full"
                  ><span>{{ t("catalog.inputFieldOptions") }}</span
                  ><textarea
                    v-model="inputFieldForm.optionsText"
                    rows="4"
                    :placeholder="t('catalog.inputFieldOptionsHint')"
                  ></textarea>
                </label>
                <label class="full"
                  ><span>{{ t("catalog.inputFieldPattern") }}</span
                  ><input
                    v-model="inputFieldForm.validationPattern"
                    maxlength="300"
                    :placeholder="t('catalog.inputFieldPatternHint')"
                /></label>
                <label
                  ><span>{{ t("catalog.minLength") }}</span
                  ><input
                    v-model.number="inputFieldForm.minLength"
                    type="number"
                    min="0"
                    max="2000"
                /></label>
                <label
                  ><span>{{ t("catalog.maxLength") }}</span
                  ><input
                    v-model.number="inputFieldForm.maxLength"
                    type="number"
                    min="1"
                    max="2000"
                /></label>
                <label class="toggle-field"
                  ><input
                    v-model="inputFieldForm.required"
                    type="checkbox"
                  /><span
                    ><strong>{{ t("catalog.required") }}</strong
                    ><small>{{ t("catalog.requiredHint") }}</small></span
                  ></label
                >
                <label class="toggle-field"
                  ><input
                    v-model="inputFieldForm.sensitive"
                    type="checkbox"
                  /><span
                    ><strong>{{ t("catalog.sensitive") }}</strong
                    ><small>{{ t("catalog.sensitiveHint") }}</small></span
                  ></label
                >
                <label class="toggle-field"
                  ><input
                    v-model="inputFieldForm.passToSupplier"
                    type="checkbox"
                  /><span
                    ><strong>{{ t("catalog.passToSupplier") }}</strong
                    ><small>{{ t("catalog.passToSupplierHint") }}</small></span
                  ></label
                >
                <label class="toggle-field"
                  ><input
                    v-model="inputFieldForm.enabled"
                    type="checkbox"
                  /><span
                    ><strong>{{ t("catalog.enableInputField") }}</strong
                    ><small>{{
                      t("catalog.enableInputFieldHint")
                    }}</small></span
                  ></label
                >
              </div>
              <label class="reason-field"
                ><span>{{ t("catalog.changeReason") }}</span
                ><textarea
                  v-model="inputFieldForm.reason"
                  rows="2"
                  maxlength="500"
                  :placeholder="t('catalog.reasonPlaceholder')"
                ></textarea>
              </label>
              <p v-if="inputFieldError" class="modal-error">
                <AlertTriangle :size="16" />{{ inputFieldError }}
              </p>
              <footer>
                <button
                  type="button"
                  class="secondary-button"
                  @click="resetInputFieldForm()"
                >
                  {{ t("catalog.reset") }}
                </button>
                <button
                  type="submit"
                  class="primary-action"
                  :disabled="inputFieldSaving"
                >
                  <LoaderCircle
                    v-if="inputFieldSaving"
                    :size="15"
                    class="spin"
                  /><CheckCircle2 v-else :size="15" />
                  {{
                    inputFieldSaving ? t("catalog.saving") : t("catalog.save")
                  }}
                </button>
              </footer>
            </form>
          </div>
        </section>
      </div>

      <div
        v-if="deleteTarget && canManage"
        class="modal-backdrop"
        @mousedown.self="closeDelete"
      >
        <section
          class="confirm-modal"
          role="alertdialog"
          aria-modal="true"
          :aria-label="t('catalog.confirmDelete')"
        >
          <div class="danger-icon"><Trash2 :size="22" /></div>
          <h3>{{ t("catalog.confirmDelete") }}</h3>
          <p>
            {{ t("catalog.confirmDeleteHint", { name: deleteTarget.name }) }}
          </p>
          <label
            ><span>{{ t("catalog.deleteReason") }}</span
            ><textarea
              v-model="deleteReason"
              rows="3"
              maxlength="500"
              :placeholder="t('catalog.reasonPlaceholder')"
            ></textarea>
          </label>
          <p v-if="deleteError" class="modal-error">
            <AlertTriangle :size="16" /> {{ deleteError }}
          </p>
          <footer>
            <button class="secondary-button" @click="closeDelete">
              {{ t("catalog.cancel") }}</button
            ><button
              class="danger-button"
              :disabled="deleting"
              @click="confirmDelete"
            >
              <LoaderCircle v-if="deleting" :size="15" class="spin" /><Trash2
                v-else
                :size="15"
              />
              {{
                deleting ? t("catalog.deleting") : t("catalog.confirmDeleteBtn")
              }}
            </button>
          </footer>
        </section>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.catalog-shell {
  display: grid;
  gap: 18px;
  min-width: 0;
}
.catalog-tabs {
  display: inline-flex;
  width: fit-content;
  max-width: 100%;
  padding: 4px;
  gap: 3px;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
  overflow-x: auto;
}
.catalog-tabs button,
.subtabs button {
  border: 0;
  background: transparent;
  color: var(--muted);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  white-space: nowrap;
  font-size: 11px;
  font-weight: 650;
  border-radius: 6px;
  padding: 10px 15px;
}
.catalog-tabs button.active,
.subtabs button.active {
  background: var(--dark);
  color: var(--dark-text);
}
.section-heading {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 24px;
}
.section-heading h2 {
  margin: 6px 0 7px;
  font-size: 26px;
  letter-spacing: -0.035em;
}
.section-heading p {
  margin: 0;
  max-width: 750px;
  color: var(--muted);
  font-size: 11px;
  line-height: 1.75;
}
.eyebrow {
  color: var(--muted);
  font-size: 8px;
  font-weight: 750;
  letter-spacing: 0.18em;
}
.heading-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.primary-action,
.secondary-button,
.small-action,
.danger-button {
  min-height: 36px;
  border-radius: 6px;
  padding: 0 14px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  font-size: 11px;
  font-weight: 650;
}
.primary-action {
  border: 1px solid var(--dark);
  background: var(--dark);
  color: var(--dark-text);
}
.secondary-button,
.small-action {
  border: 1px solid var(--line);
  background: var(--surface);
  color: var(--text);
}
.danger-button {
  border: 1px solid var(--danger);
  background: var(--danger);
  color: #fff;
}
button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.catalog-notice {
  min-height: 42px;
  display: flex;
  align-items: center;
  gap: 9px;
  border: 1px solid var(--line);
  border-radius: 7px;
  padding: 9px 12px;
  font-size: 11px;
  background: var(--surface);
}
.catalog-notice.success {
  color: var(--success);
  border-color: color-mix(in srgb, var(--success) 28%, var(--line));
  background: color-mix(in srgb, var(--success) 6%, var(--surface));
}
.catalog-notice.error {
  color: var(--danger);
  border-color: color-mix(in srgb, var(--danger) 28%, var(--line));
  background: color-mix(in srgb, var(--danger) 6%, var(--surface));
}
.catalog-notice span {
  flex: 1;
}
.catalog-notice button {
  border: 0;
  background: transparent;
  color: inherit;
  display: grid;
  place-items: center;
  font-size: 10px;
}
.category-panel {
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
  padding: 15px;
  box-shadow: var(--shadow);
}
.panel-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 13px;
}
.panel-title > div {
  display: grid;
  gap: 3px;
}
.panel-title strong {
  font-size: 12px;
}
.panel-title span {
  color: var(--muted);
  font-size: 9px;
}
.category-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}
.category-card {
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: 7px;
  padding: 11px;
  display: grid;
  grid-template-columns: 32px 1fr auto;
  align-items: start;
  gap: 10px;
  background: var(--surface-2);
}
.category-icon {
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  border-radius: 6px;
  background: var(--soft);
  font-size: 13px;
  font-weight: 700;
  overflow: hidden;
}
.category-icon img,
.product-thumb img {
  width: 100%;
  height: 100%;
  display: block;
  object-fit: cover;
}
.category-copy {
  min-width: 0;
  display: grid;
  gap: 4px;
}
.category-copy > div {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.category-copy strong {
  font-size: 11px;
}
.category-copy code,
.primary-cell code {
  color: var(--muted);
  font-size: 9px;
  overflow-wrap: anywhere;
}
.category-copy p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.5;
}
.category-parent {
  color: var(--muted);
  font-size: 8px;
}
.product-primary {
  display: flex;
  align-items: center;
  gap: 9px;
  min-width: 220px;
}
.product-thumb {
  width: 40px;
  height: 40px;
  flex: 0 0 40px;
  display: grid;
  place-items: center;
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: 7px;
  color: var(--muted);
  background: var(--soft);
}
.media-editor {
  display: grid;
  gap: 12px;
  padding: 13px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface-2);
}
.media-editor-heading,
.media-editor-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.media-editor-heading > div {
  display: grid;
  gap: 3px;
}
.media-editor-heading strong {
  font-size: 11px;
}
.media-editor-heading small,
.media-editor-actions small {
  color: var(--muted);
  font-size: 9px;
  line-height: 1.5;
}
.media-source-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}
.media-source-grid label {
  display: grid;
  gap: 6px;
}
.media-source-grid label > span {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 9px;
  color: var(--muted);
}
.media-source-grid .full {
  grid-column: 1 / -1;
}
.media-preview-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}
.media-preview-grid article {
  position: relative;
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface);
}
.media-preview-grid img {
  width: 100%;
  aspect-ratio: 4 / 3;
  display: block;
  object-fit: cover;
}
.media-preview-grid span {
  display: block;
  padding: 5px 7px;
  color: var(--muted);
  font-size: 8px;
}
.media-preview-grid button {
  position: absolute;
  top: 5px;
  right: 5px;
  width: 26px;
  height: 26px;
  display: grid;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--danger) 30%, transparent);
  border-radius: 6px;
  color: var(--danger);
  background: color-mix(in srgb, var(--surface) 88%, transparent);
  backdrop-filter: blur(8px);
}
.status-dot {
  font-size: 8px;
  border-radius: 99px;
  padding: 2px 6px;
}
.status-dot.success {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 10%, transparent);
}
.status-dot.muted {
  color: var(--muted);
  background: var(--soft);
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.toolbar > select,
.pager select {
  height: 36px;
  min-width: 145px;
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 0 30px 0 10px;
  background: var(--surface);
  color: var(--text);
  font-size: 10px;
  outline: none;
}
.toolbar > select:focus,
.search-box:focus-within {
  border-color: color-mix(in srgb, var(--text) 42%, var(--line));
}
.search-box {
  min-width: min(320px, 100%);
  height: 36px;
  display: flex;
  align-items: center;
  gap: 8px;
  border: 1px solid var(--line);
  border-radius: 6px;
  padding-left: 10px;
  background: var(--surface);
}
.search-box svg {
  color: var(--muted);
  flex: none;
}
.search-box input {
  min-width: 0;
  flex: 1;
  border: 0;
  outline: 0;
  background: transparent;
  font-size: 11px;
}
.search-box button {
  align-self: stretch;
  border: 0;
  border-left: 1px solid var(--line);
  padding: 0 11px;
  background: transparent;
  color: var(--muted);
  font-size: 10px;
}
.icon-button {
  width: 36px;
  height: 36px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--muted);
  display: grid;
  place-items: center;
}
.filter-hint {
  margin: -10px 0 0;
  color: var(--muted);
  font-size: 9px;
}
.data-card {
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.table-wrap {
  overflow-x: auto;
}
table {
  width: 100%;
  min-width: 900px;
  border-collapse: collapse;
}
th {
  padding: 10px 13px;
  border-bottom: 1px solid var(--line);
  background: var(--surface-2);
  color: var(--muted);
  font-size: 8px;
  font-weight: 700;
  text-align: left;
  letter-spacing: 0.08em;
  white-space: nowrap;
}
td {
  padding: 12px 13px;
  border-bottom: 1px solid var(--line);
  color: var(--text);
  font-size: 10px;
  vertical-align: middle;
}
tbody tr:last-child td {
  border-bottom: 0;
}
tbody tr:hover {
  background: color-mix(in srgb, var(--soft) 50%, transparent);
}
.align-right {
  text-align: right;
}
.primary-cell {
  min-width: 145px;
  display: grid;
  gap: 3px;
}
.primary-cell strong {
  font-size: 11px;
}
.primary-cell span,
.cell-sub {
  display: block;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.45;
}
.attribute-summary {
  display: block;
  max-width: 250px;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.55;
}
.pill,
.mini-tag {
  display: inline-flex;
  align-items: center;
  width: fit-content;
  border-radius: 99px;
  padding: 4px 7px;
  font-size: 8px;
  font-weight: 650;
}
.pill.on_sale,
.pill.active {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 10%, transparent);
}
.pill.draft {
  color: var(--warn);
  background: color-mix(in srgb, var(--warn) 11%, transparent);
}
.pill.off_sale,
.pill.inactive {
  color: var(--muted);
  background: var(--soft);
}
.mini-tag {
  margin-left: 5px;
  color: var(--text);
  background: var(--soft);
}
.row-actions {
  display: inline-flex;
  gap: 5px;
}
.row-actions button {
  width: 30px;
  height: 30px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--muted);
}
.row-actions button:hover {
  color: var(--text);
  border-color: color-mix(in srgb, var(--text) 28%, var(--line));
}
.row-actions button.danger:hover {
  color: var(--danger);
  border-color: color-mix(in srgb, var(--danger) 40%, var(--line));
}
.table-state {
  min-height: 230px;
  padding: 35px;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  gap: 9px;
  color: var(--muted);
  font-size: 10px;
  text-align: center;
}
.table-state strong {
  color: var(--text);
  font-size: 13px;
}
.pager {
  min-height: 50px;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  border-top: 1px solid var(--line);
  padding: 8px 12px;
  color: var(--muted);
  font-size: 9px;
}
.pager select {
  height: 31px;
  min-width: 82px;
}
.pager > div {
  display: flex;
  gap: 3px;
}
.pager button {
  min-width: 30px;
  height: 30px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  color: var(--muted);
  display: grid;
  place-items: center;
  font-size: 9px;
}
.pager button.active {
  background: var(--dark);
  border-color: var(--dark);
  color: var(--dark-text);
}
.subtabs {
  display: flex;
  width: fit-content;
  gap: 3px;
  padding: 3px;
  background: var(--soft);
  border-radius: 7px;
}
.subtabs button {
  padding: 8px 13px;
}
.compact-loading,
.compact-empty {
  min-height: 70px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  color: var(--muted);
  font-size: 10px;
}
.inline-error {
  color: var(--danger);
  font-size: 10px;
}
.modal-backdrop {
  position: fixed;
  z-index: 200;
  inset: 0;
  padding: 24px;
  display: grid;
  place-items: center;
  background: rgba(8, 8, 10, 0.56);
  backdrop-filter: blur(3px);
}
.editor-modal {
  width: min(800px, 100%);
  max-height: calc(100vh - 48px);
  overflow-y: auto;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: 0 28px 90px rgba(0, 0, 0, 0.25);
}
.editor-modal > header {
  position: sticky;
  top: 0;
  z-index: 2;
  display: flex;
  justify-content: space-between;
  gap: 20px;
  padding: 18px 20px 15px;
  border-bottom: 1px solid var(--line);
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  backdrop-filter: blur(12px);
}
.editor-modal h3,
.confirm-modal h3 {
  margin: 5px 0 4px;
  font-size: 19px;
  letter-spacing: -0.025em;
}
.editor-modal header p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.5;
}
.close-button {
  width: 31px;
  height: 31px;
  flex: none;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--muted);
  display: grid;
  place-items: center;
}
.editor-modal form {
  padding: 18px 20px 20px;
}
.input-field-modal {
  width: min(1120px, 100%);
}
.input-field-manager {
  display: grid;
  grid-template-columns: minmax(280px, 360px) minmax(0, 1fr);
  min-height: 560px;
}
.input-field-manager > aside {
  min-width: 0;
  padding: 18px;
  border-right: 1px solid var(--line);
  background: var(--surface-2);
}
.input-field-list-head,
.input-field-editor-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 14px;
}
.input-field-list-head > div,
.input-field-editor-title > div {
  display: grid;
  gap: 3px;
}
.input-field-list-head strong,
.input-field-editor-title strong {
  font-size: 12px;
}
.input-field-list-head small,
.input-field-editor-title small {
  color: var(--muted);
  font-size: 9px;
}
.input-field-empty {
  min-height: 180px;
  display: grid;
  place-items: center;
  align-content: center;
  gap: 8px;
  color: var(--muted);
  font-size: 10px;
  text-align: center;
}
.input-field-list {
  display: grid;
  gap: 7px;
}
.input-field-list article {
  min-width: 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 31px;
  gap: 5px;
  border: 1px solid var(--line);
  border-radius: 7px;
  padding: 7px;
  background: var(--surface);
}
.input-field-list article.active {
  border-color: color-mix(in srgb, var(--text) 45%, var(--line));
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--text) 10%, transparent);
}
.input-field-summary {
  min-width: 0;
  border: 0;
  background: transparent;
  color: var(--text);
  text-align: left;
  display: grid;
  gap: 4px;
}
.input-field-summary strong {
  font-size: 11px;
}
.input-field-summary code {
  color: var(--muted);
  font-size: 9px;
  overflow-wrap: anywhere;
}
.input-field-summary span {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
  color: var(--muted);
  font-size: 8px;
}
.input-field-summary b {
  border-radius: 999px;
  padding: 2px 5px;
  background: var(--soft);
  color: var(--text);
  font-size: 7px;
  font-weight: 650;
}
.input-field-trash {
  width: 31px;
  height: 31px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  color: var(--muted);
  display: grid;
  place-items: center;
}
.input-field-trash:hover {
  color: var(--danger);
  border-color: color-mix(in srgb, var(--danger) 40%, var(--line));
}
.input-field-delete {
  display: grid;
  gap: 9px;
  margin-top: 14px;
  padding: 12px;
  border: 1px solid color-mix(in srgb, var(--danger) 35%, var(--line));
  border-radius: 7px;
  background: color-mix(in srgb, var(--danger) 5%, var(--surface));
}
.input-field-delete strong {
  font-size: 10px;
}
.input-field-delete textarea {
  width: 100%;
  border: 1px solid var(--line);
  border-radius: 5px;
  padding: 8px;
  background: var(--surface);
  color: var(--text);
  resize: vertical;
}
.input-field-delete > div {
  display: flex;
  justify-content: flex-end;
  gap: 7px;
}
.input-field-editor {
  min-width: 0;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
.form-grid label,
.reason-field,
.confirm-modal label {
  min-width: 0;
  display: grid;
  gap: 6px;
  color: var(--text);
  font-size: 10px;
  font-weight: 650;
}
.form-grid label > span,
.reason-field > span,
.confirm-modal label > span {
  display: block;
}
.form-grid input:not([type="checkbox"]),
.form-grid select,
.form-grid textarea,
.reason-field textarea,
.confirm-modal textarea,
.compound-field input,
.compound-field select,
.attribute-row input {
  width: 100%;
  min-height: 39px;
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 9px 10px;
  background: var(--surface-2);
  color: var(--text);
  outline: none;
  font-size: 11px;
  resize: vertical;
}
.form-grid input:focus,
.form-grid select:focus,
.form-grid textarea:focus,
.reason-field textarea:focus,
.confirm-modal textarea:focus {
  border-color: color-mix(in srgb, var(--text) 42%, var(--line));
}
.form-grid small {
  color: var(--muted);
  font-size: 8px;
  font-weight: 400;
  line-height: 1.4;
}
.full {
  grid-column: 1 / -1;
}
.toggle-field {
  min-height: 48px;
  display: flex !important;
  flex-direction: row !important;
  align-items: center;
  gap: 10px !important;
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 10px;
  background: var(--surface-2);
}
.toggle-field input {
  width: 15px;
  height: 15px;
  accent-color: var(--dark);
}
.toggle-field span {
  display: grid !important;
  gap: 2px;
}
.toggle-field strong {
  font-size: 10px;
}
.channel-picker {
  min-width: 0;
  margin: 0;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 12px;
}
.channel-picker legend {
  padding: 0 6px;
  font-weight: 700;
}
.channel-picker > p,
.channel-picker > small {
  display: block;
  margin: 0 0 10px;
  color: var(--muted);
  font-size: 12px;
}
.channel-picker > small {
  margin: 10px 0 0;
}
.channel-options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.channel-option {
  display: flex !important;
  align-items: flex-start;
  gap: 9px;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface-soft);
}
.channel-option input {
  margin-top: 3px;
}
.channel-option span,
.channel-option strong,
.channel-option small {
  display: block;
}
.channel-option small,
.channel-empty {
  color: var(--muted);
  font-size: 12px;
}
.channel-empty {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 10px 0;
}
.field-warning,
.field-note {
  border: 1px solid color-mix(in srgb, var(--warn) 32%, var(--line));
  border-radius: 6px;
  padding: 10px;
  background: color-mix(in srgb, var(--warn) 7%, var(--surface));
  color: var(--warn);
  font-size: 9px;
  line-height: 1.6;
}
.field-warning {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.field-warning svg {
  flex: none;
}
.compound-field {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(150px, 0.8fr) 38px;
  gap: 6px;
}
.compound-field button {
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface-2);
  color: var(--muted);
  display: grid;
  place-items: center;
}
.attribute-editor {
  margin: 0;
  border: 1px solid var(--line);
  border-radius: 7px;
  padding: 11px;
}
.attribute-editor legend {
  padding: 0 5px;
  font-size: 10px;
  font-weight: 650;
}
.attribute-row {
  display: grid;
  grid-template-columns: 1fr 1.5fr 34px;
  gap: 6px;
  margin-bottom: 7px;
}
.attribute-row button {
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--muted);
  display: grid;
  place-items: center;
}
.add-row {
  min-height: 32px;
  border: 1px dashed var(--line);
  border-radius: 6px;
  padding: 0 10px;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  background: transparent;
  color: var(--muted);
  font-size: 9px;
}
.reason-field {
  margin-top: 16px;
}
.modal-error {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  margin: 11px 0 0;
  color: var(--danger);
  font-size: 10px;
  line-height: 1.5;
}
.modal-error svg {
  flex: none;
}
.editor-modal form > footer,
.confirm-modal footer {
  margin-top: 17px;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.confirm-modal {
  width: min(430px, 100%);
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 21px;
  background: var(--surface);
  box-shadow: 0 28px 90px rgba(0, 0, 0, 0.25);
}
.confirm-modal > p {
  margin: 0 0 16px;
  color: var(--muted);
  font-size: 10px;
  line-height: 1.7;
}
.danger-icon {
  width: 42px;
  height: 42px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  color: var(--danger);
  background: color-mix(in srgb, var(--danger) 10%, transparent);
}
.spin {
  animation: spin 0.85s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 1000px) {
  .category-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .section-heading {
    align-items: flex-start;
  }
  .input-field-manager {
    grid-template-columns: 1fr;
  }
  .input-field-manager > aside {
    border-right: 0;
    border-bottom: 1px solid var(--line);
  }
}
@media (max-width: 720px) {
  .catalog-shell {
    gap: 14px;
  }
  .catalog-tabs {
    width: 100%;
  }
  .catalog-tabs button {
    flex: 1;
    padding-inline: 10px;
  }
  .section-heading {
    display: grid;
    gap: 14px;
  }
  .heading-actions {
    width: 100%;
  }
  .heading-actions > button,
  .section-heading > .primary-action {
    flex: 1;
  }
  .category-grid {
    grid-template-columns: 1fr;
  }
  .panel-title {
    align-items: flex-start;
  }
  .toolbar {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }
  .search-box {
    grid-column: 1 / -1;
    min-width: 0;
  }
  .toolbar > select {
    min-width: 0;
    width: 100%;
  }
  .pager {
    justify-content: space-between;
    flex-wrap: wrap;
  }
  .form-grid {
    grid-template-columns: 1fr;
  }
  .channel-options {
    grid-template-columns: 1fr;
  }
  .full {
    grid-column: auto;
  }
  .modal-backdrop {
    padding: 10px;
  }
  .editor-modal {
    max-height: calc(100vh - 20px);
  }
  .editor-modal > header,
  .editor-modal form {
    padding-left: 14px;
    padding-right: 14px;
  }
  .compound-field {
    grid-template-columns: 1fr 38px;
  }
  .compound-field select {
    grid-column: 1 / -1;
  }
}
@media (max-width: 520px) {
  .catalog-tabs button span {
    display: none;
  }
  .toolbar {
    grid-template-columns: 1fr;
  }
  .search-box {
    grid-column: auto;
  }
  .category-card {
    grid-template-columns: 32px 1fr;
  }
  .category-card > .row-actions {
    grid-column: 2;
    justify-self: end;
  }
  .media-source-grid,
  .media-preview-grid {
    grid-template-columns: 1fr;
  }
  .media-preview-grid img {
    aspect-ratio: 16 / 9;
  }
  .attribute-row {
    grid-template-columns: 1fr 34px;
  }
  .attribute-row input:nth-child(2) {
    grid-column: 1;
  }
  .attribute-row button {
    grid-column: 2;
    grid-row: 1 / 3;
  }
}
</style>
