<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  AlertCircle,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Clock3,
  FolderTree,
  Image,
  Info,
  Layers3,
  LoaderCircle,
  PackageCheck,
  RefreshCw,
  Save,
  Search,
  Settings2,
  UploadCloud,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import {
  formatMoney,
  loadCurrencyDirectory,
  majorInputStep,
  majorToMinor,
  minorToSafeNumber,
  storeCurrency,
} from "../utils/money";

interface Supplier {
  id: string;
  name: string;
  code: string;
  protocol: string;
  price_currency: string;
  status: string;
  sync_interval_minutes: number;
}

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface RemoteCategory {
  id: string;
  external_id: string;
  external_parent_id: string;
  name: string;
  description: string;
  image_url: string;
  sort: number;
  status: string;
  mapping_id?: string | null;
  local_category_id?: string | null;
  local_category_name?: string;
  auto_create: boolean;
  auto_publish: boolean;
  sync_name: boolean;
  sync_description: boolean;
  sync_image: boolean;
  mirror_remote_image: boolean;
  price_mode: string;
  markup_basis_point: number;
  mapping_last_synced_at?: string | null;
  mapping_last_error?: string;
  product_count: number;
  last_seen_at: string;
}

interface RemoteProduct {
  id: string;
  external_id: string;
  parent_external_id: string;
  external_category_id: string;
  external_sku: string;
  name: string;
  summary: string;
  description: string;
  cover_url: string;
  image_urls: unknown;
  country: string;
  tags: unknown;
  currency: string;
  price: number;
  original_price: number;
  member_price: number;
  wholesale_prices: unknown;
  stock: number;
  stock_status: string;
  minimum: number;
  maximum: number;
  fulfillment_type: string;
  status: string;
  variants: unknown;
  input_fields: unknown;
  snapshot_hash: string;
  last_seen_at: string;
  mapped: boolean;
  mapping_ids: string[];
  local_product_ids: string[];
  local_product_names: string[];
}

interface CategoryProductPage {
  items: RemoteProduct[];
  total: number;
  page: number;
  page_size: number;
  loading: boolean;
}

interface LocalCategory {
  id: string;
  parent_id?: string | null;
  name: string;
  enabled: boolean;
}

interface SyncPolicy {
  supplier_id: string;
  auto_sync_categories: boolean;
  auto_create_categories: boolean;
  auto_sync_products: boolean;
  auto_create_products: boolean;
  sync_title: boolean;
  sync_summary: boolean;
  sync_description: boolean;
  sync_media: boolean;
  mirror_remote_media: boolean;
  sync_price: boolean;
  sync_stock: boolean;
  sync_variants: boolean;
  sync_status: boolean;
  sync_purchase_limits: boolean;
  missing_product_action: "keep" | "unpublish" | "disable_mapping";
}

interface ImportForm {
  category_mode: "mirror" | "target";
  target_category_id: string;
  auto_publish: boolean;
  price_mode: "fixed_markup" | "fixed_amount";
  markup_percent: number;
  markup_amount_major: string;
  sync_title: boolean;
  sync_summary: boolean;
  sync_description: boolean;
  sync_media: boolean;
  mirror_remote_media: boolean;
  sync_price: boolean;
  sync_stock: boolean;
  sync_variants: boolean;
  sync_status: boolean;
  sync_purchase_limits: boolean;
  reason: string;
}

interface SyncRun {
  id: string;
  supplier_id: string;
  trigger: string;
  status: string;
  protocol: string;
  categories_seen: number;
  categories_created: number;
  products_seen: number;
  products_created: number;
  products_updated: number;
  media_mirrored: number;
  warnings: number;
  error_summary: string;
  started_at: string;
  completed_at?: string | null;
}

interface SyncChange {
  id: string;
  entity_type: string;
  external_id: string;
  local_id?: string | null;
  action: string;
  changed_fields: unknown;
  applied: boolean;
  message: string;
  created_at: string;
}

interface ImportJobResult {
  requested?: number;
  imported?: number;
  skipped_mapped?: number;
  categories_created?: number;
  category_mappings_configured?: number;
  product_ids?: string[];
  sync_queue_status?: string;
}

interface ImportJob {
  id: string;
  supplier_id: string;
  task_id?: string;
  status:
    "queued" | "running" | "retrying" | "succeeded" | "failed" | "cancelled";
  attempts: number;
  requested_count: number;
  imported_count: number;
  skipped_count: number;
  processed_count: number;
  progress_percent: number;
  categories_created: number;
  mappings_configured: number;
  result: ImportJobResult;
  error_summary: string;
  started_at?: string | null;
  completed_at?: string | null;
  next_attempt_at?: string | null;
  can_retry: boolean;
  created_at: string;
  updated_at: string;
}

const props = defineProps<{ supplier: Supplier }>();
const emit = defineEmits<{
  close: [];
  notice: [message: string];
}>();
const { t, te, locale } = useI18n();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("supplier.manage"));

type ViewTab = "products" | "categories" | "policy" | "runs";
const tab = ref<ViewTab>("products");
const loading = ref(false);
const saving = ref(false);
const importing = ref(false);
const error = ref("");
const notice = ref("");
const products = ref<RemoteProduct[]>([]);
const categories = ref<RemoteCategory[]>([]);
const treeCategories = ref<RemoteCategory[]>([]);
const localCategories = ref<LocalCategory[]>([]);
const runs = ref<SyncRun[]>([]);
const importJobs = ref<ImportJob[]>([]);
const importJobsLoading = ref(false);
const retryingImportJobID = ref("");
const retryImportReason = ref("");
const retryImportSaving = ref(false);
const changes = ref<SyncChange[]>([]);
const selectedRun = ref<SyncRun | null>(null);
const changesLoading = ref(false);
const changePage = ref(1);
const changePageSize = ref(100);
const changeTotal = ref(0);
const policy = ref<SyncPolicy>(defaultPolicy());
const policyReason = ref("");
const importResult = ref<Record<string, unknown> | null>(null);

const productQuery = ref("");
const productStatus = ref("");
const productPage = ref(1);
const productPageSize = ref(24);
const productTotal = ref(0);
const productLoading = ref(false);
const categoryQuery = ref("");
const categoryPage = ref(1);
const categoryPageSize = ref(100);
const categoryTotal = ref(0);
const categoryLoading = ref(false);
const treeCategoryLoading = ref(false);
const runPage = ref(1);
const runTotal = ref(0);
const runPageSize = ref(10);
const runLoading = ref(false);

const selectedIDs = ref<string[]>([]);
const selectedProductCategoryIDs = ref<Record<string, string>>({});
const expandedProductCategoryIDs = ref<string[]>([]);
const categorySelectionLoadingID = ref("");
const categoryProductIDs = ref<Record<string, string[]>>({});
const categoryProductIDsComplete = ref<string[]>([]);
const categoryProductPages = ref<Record<string, CategoryProductPage>>({});
const importForm = ref<ImportForm>(defaultImportForm());
const importDockExpanded = ref(false);
let queryTimer: ReturnType<typeof setTimeout> | undefined;
let importPollTimer: ReturnType<typeof setTimeout> | undefined;
let changeRequestSequence = 0;
let treeCategoryRequestSequence = 0;
let categorySelectionRequestSequence = 0;
const categoryProductPageRequests = new Map<string, number>();
let productTreeInitializedForSupplier = "";
let componentMounted = false;

function defaultPolicy(): SyncPolicy {
  return {
    supplier_id: props.supplier.id,
    auto_sync_categories: false,
    auto_create_categories: false,
    auto_sync_products: false,
    auto_create_products: false,
    sync_title: false,
    sync_summary: false,
    sync_description: false,
    sync_media: false,
    mirror_remote_media: true,
    sync_price: true,
    sync_stock: true,
    sync_variants: false,
    sync_status: false,
    sync_purchase_limits: false,
    missing_product_action: "keep",
  };
}

function defaultImportForm(): ImportForm {
  return {
    category_mode: "mirror",
    target_category_id: "",
    auto_publish: false,
    price_mode: "fixed_markup",
    markup_percent: 50,
    markup_amount_major: "0.00",
    sync_title: true,
    sync_summary: true,
    sync_description: true,
    sync_media: true,
    mirror_remote_media: true,
    sync_price: true,
    sync_stock: true,
    sync_variants: true,
    sync_status: false,
    sync_purchase_limits: true,
    reason: "",
  };
}

function unwrap<T>(value: unknown, fallback: T): T {
  const root = value as { data?: unknown } | null;
  const payload =
    root && Object.prototype.hasOwnProperty.call(root, "data")
      ? root.data
      : value;
  return (payload ?? fallback) as T;
}

function pagePayload<T>(value: unknown): PagePayload<T> {
  const payload = unwrap<Partial<PagePayload<T>>>(value, {});
  return {
    items: Array.isArray(payload.items) ? payload.items : [],
    total: Number(payload.total || 0),
    page: Number(payload.page || 1),
    page_size: Number(payload.page_size || 20),
  };
}

function apiMessage(failure: unknown, fallback: string) {
  const errorValue = failure as { response?: { data?: { message?: string } } };
  const message = errorValue.response?.data?.message;
  return message && !message.startsWith("error.") ? message : fallback;
}

function text(key: string, fallback: string, params?: Record<string, unknown>) {
  // New catalog keys are progressively translated. Never invoke `t` for a
  // missing key: doing so floods production consoles when an older locale is
  // selected. Existing translated keys still use the active locale.
  return te(key) ? t(key, params || {}) : fallback;
}

const catalogPolicyFallbacks: Record<string, string> = {
  "supply.catalogAutoSyncCategories": "自动同步分类",
  "supply.catalogAutoSyncCategoriesDesc": "上游分类变化时自动更新本地分类",
  "supply.catalogAutoCreateCategories": "自动创建分类",
  "supply.catalogAutoCreateCategoriesDesc": "同步时自动创建缺失的本地分类",
  "supply.catalogAutoSyncProducts": "自动同步商品",
  "supply.catalogAutoSyncProductsDesc": "上游商品变化时自动更新本地商品",
  "supply.catalogAutoCreateProducts": "自动创建商品",
  "supply.catalogAutoCreateProductsDesc": "同步时自动创建缺失的本地商品",
};

function policyText(key: string) {
  return catalogPolicyFallbacks[key] || key;
}

function formatTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString(locale.value || "zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

function formatRemoteMoney(value: number, currency: string) {
  return formatMoney(
    value,
    currency || props.supplier.price_currency || "CNY",
    locale.value,
  );
}

function parseArray(value: unknown): unknown[] {
  if (Array.isArray(value)) return value;
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value);
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  }
  return [];
}

function productImages(product: RemoteProduct) {
  const all = [
    product.cover_url,
    ...parseArray(product.image_urls).map((item) => String(item || "")),
  ];
  const seen = new Set<string>();
  return all.filter((url) => {
    if (!/^https?:\/\//i.test(url) || seen.has(url)) return false;
    seen.add(url);
    return true;
  });
}

function stripHTML(value: string) {
  return String(value || "")
    .replace(/<script[\s\S]*?<\/script>/gi, " ")
    .replace(/<style[\s\S]*?<\/style>/gi, " ")
    .replace(/<[^>]+>/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

const productCategoryByID = computed(
  () =>
    new Map(
      treeCategories.value.map((item) => [item.external_id, item] as const),
    ),
);

const productCategoryChildren = computed(() => {
  const result = new Map<string, RemoteCategory[]>();
  for (const item of treeCategories.value) {
    const parent = item.external_parent_id || "__root__";
    const siblings = result.get(parent) || [];
    siblings.push(item);
    result.set(parent, siblings);
  }
  for (const siblings of result.values())
    siblings.sort((a, b) => b.sort - a.sort || a.name.localeCompare(b.name));
  return result;
});

function categoryDescendantIDs(externalID: string) {
  const result: string[] = [];
  const pending = [externalID];
  const visited = new Set<string>();
  while (pending.length) {
    const current = pending.shift() || "";
    if (!current || visited.has(current)) continue;
    visited.add(current);
    result.push(current);
    for (const child of productCategoryChildren.value.get(current) || [])
      pending.push(child.external_id);
  }
  return result;
}

function categoryPath(externalID: string) {
  if (!externalID) return text("supply.catalogUncategorized", "未分类");
  const names: string[] = [];
  const visited = new Set<string>();
  let current = productCategoryByID.value.get(externalID);
  while (current && !visited.has(current.external_id)) {
    visited.add(current.external_id);
    names.unshift(current.name || current.external_id);
    current = current.external_parent_id
      ? productCategoryByID.value.get(current.external_parent_id)
      : undefined;
  }
  return names.length ? names.join(" / ") : externalID;
}

interface ProductCategoryTreeRow {
  item: RemoteCategory;
  depth: number;
  hasChildren: boolean;
}

function searchVisibleProductCategoryIDs() {
  if (!productSearchMode.value) return null;
  const visible = new Set<string>();
  const includePath = (externalID: string) => {
    const visited = new Set<string>();
    let currentID = externalID;
    while (currentID && !visited.has(currentID)) {
      visited.add(currentID);
      visible.add(currentID);
      currentID =
        productCategoryByID.value.get(currentID)?.external_parent_id || "";
    }
  };
  products.value.forEach((product) =>
    includePath(product.external_category_id),
  );
  for (const category of matchingProductCategories.value) {
    includePath(category.external_id);
    categoryDescendantIDs(category.external_id).forEach((id) =>
      visible.add(id),
    );
  }
  return visible;
}

const productCategoryTreeRows = computed<ProductCategoryTreeRow[]>(() => {
  const rows: ProductCategoryTreeRow[] = [];
  const sourceIDs = new Set(
    treeCategories.value.map((item) => item.external_id),
  );
  const roots = treeCategories.value
    .filter(
      (item) =>
        !item.external_parent_id || !sourceIDs.has(item.external_parent_id),
    )
    .slice()
    .sort((a, b) => b.sort - a.sort || a.name.localeCompare(b.name));
  const expanded = new Set(expandedProductCategoryIDs.value);
  const searchVisible = searchVisibleProductCategoryIDs();
  const visited = new Set<string>();
  const walk = (item: RemoteCategory, depth: number) => {
    if (visited.has(item.external_id)) return;
    if (searchVisible && !searchVisible.has(item.external_id)) return;
    visited.add(item.external_id);
    const children = productCategoryChildren.value.get(item.external_id) || [];
    rows.push({
      item,
      depth: Math.min(depth, 8),
      hasChildren: children.length > 0,
    });
    if (expanded.has(item.external_id))
      children.forEach((child) => walk(child, depth + 1));
  };
  roots.forEach((item) => walk(item, 0));
  for (const item of treeCategories.value) {
    if (
      !visited.has(item.external_id) &&
      (!searchVisible || searchVisible.has(item.external_id))
    )
      walk(item, 0);
  }
  return rows;
});

const missingProductCategories = computed(() => {
  const known = productCategoryByID.value;
  const result = new Set<string>();
  for (const product of products.value) {
    if (
      product.external_category_id &&
      !known.has(product.external_category_id)
    )
      result.add(product.external_category_id);
  }
  return [...result].sort((a, b) => a.localeCompare(b));
});

const selectedIDSet = computed(() => new Set(selectedIDs.value));
const categoryProductIDsCompleteSet = computed(
  () => new Set(categoryProductIDsComplete.value),
);

const selectedProductIDsByCategory = computed(() => {
  const result = new Map<string, string[]>();
  for (const [productID, categoryID] of Object.entries(
    selectedProductCategoryIDs.value,
  )) {
    if (!selectedIDSet.value.has(productID)) continue;
    const ids = result.get(categoryID) || [];
    ids.push(productID);
    result.set(categoryID, ids);
  }
  return result;
});

function branchProductIDs(externalID: string) {
  const branch = new Set(categoryDescendantIDs(externalID));
  const complete = [...branch].every((id) =>
    categoryProductIDsCompleteSet.value.has(id),
  );
  const ids = new Set<string>();
  if (complete) {
    for (const id of branch)
      for (const productID of categoryProductIDs.value[id] || [])
        ids.add(productID);
  } else {
    for (const product of products.value) {
      if (branch.has(product.external_category_id))
        ids.add(product.external_id);
    }
    for (const categoryID of branch) {
      for (const product of categoryProductPages.value[categoryID]?.items || [])
        ids.add(product.external_id);
    }
    for (const categoryID of branch) {
      for (const productID of selectedProductIDsByCategory.value.get(
        categoryID,
      ) || [])
        ids.add(productID);
    }
  }
  return { ids: [...ids], complete };
}

function calculateCategorySelectionState(externalID: string) {
  const state = branchProductIDs(externalID);
  const selected = state.ids.filter((id) => selectedIDSet.value.has(id)).length;
  return {
    ...state,
    selected,
    checked:
      state.complete && state.ids.length > 0 && selected === state.ids.length,
    indeterminate:
      selected > 0 && (!state.complete || selected < state.ids.length),
  };
}

const productCategorySelectionStates = computed(() => {
  const result = new Map<
    string,
    ReturnType<typeof calculateCategorySelectionState>
  >();
  const categoryIDs = new Set(
    treeCategories.value.map((item) => item.external_id),
  );
  missingProductCategories.value.forEach((id) => categoryIDs.add(id));
  categoryIDs.forEach((id) =>
    result.set(id, calculateCategorySelectionState(id)),
  );
  return result;
});

function categorySelectionState(externalID: string) {
  return (
    productCategorySelectionStates.value.get(externalID) ||
    calculateCategorySelectionState(externalID)
  );
}

function categoryTreeProductCount(externalID: string) {
  return categoryDescendantIDs(externalID).reduce(
    (total, id) =>
      total + Number(productCategoryByID.value.get(id)?.product_count || 0),
    0,
  );
}

const productSearchMode = computed(() =>
  Boolean(productQuery.value.trim() || productStatus.value),
);

const matchingProductCategories = computed(() => {
  const keyword = productQuery.value.trim().toLocaleLowerCase();
  if (!keyword) return [];
  return treeCategories.value
    .filter((item) =>
      `${item.name} ${item.external_id}`.toLocaleLowerCase().includes(keyword),
    )
    .sort((a, b) => {
      const aName = a.name.toLocaleLowerCase();
      const bName = b.name.toLocaleLowerCase();
      const aRank = aName === keyword ? 0 : aName.startsWith(keyword) ? 1 : 2;
      const bRank = bName === keyword ? 0 : bName.startsWith(keyword) ? 1 : 2;
      return aRank - bRank || a.name.localeCompare(b.name);
    })
    .slice(0, 8);
});

function categoryNameMatchesProductSearch(externalID: string) {
  return matchingProductCategories.value.some(
    (item) => item.external_id === externalID,
  );
}

function categoryProductPageState(externalID: string): CategoryProductPage {
  return (
    categoryProductPages.value[externalID] || {
      items: [],
      total: 0,
      page: 1,
      page_size: 50,
      loading: false,
    }
  );
}

function visibleCategoryProducts(externalID: string) {
  if (productSearchMode.value) {
    const visible = products.value.filter(
      (product) => product.external_category_id === externalID,
    );
    if (categoryNameMatchesProductSearch(externalID))
      visible.push(...categoryProductPageState(externalID).items);
    return [
      ...new Map(visible.map((item) => [item.external_id, item])).values(),
    ];
  }
  return categoryProductPageState(externalID).items;
}

function categoryProductPageCount(externalID: string) {
  const state = categoryProductPageState(externalID);
  return Math.max(1, Math.ceil(state.total / state.page_size));
}

const categoryRows = computed(() => {
  const source = categories.value
    .slice()
    .sort((a, b) => a.sort - b.sort || a.name.localeCompare(b.name));
  const byParent = new Map<string, RemoteCategory[]>();
  for (const item of source) {
    const key = item.external_parent_id || "__root__";
    const list = byParent.get(key) || [];
    list.push(item);
    byParent.set(key, list);
  }
  const rows: Array<{ item: RemoteCategory; depth: number }> = [];
  const visited = new Set<string>();
  const walk = (parent: string, depth: number) => {
    for (const item of byParent.get(parent) || []) {
      if (visited.has(item.external_id)) continue;
      visited.add(item.external_id);
      rows.push({ item, depth });
      walk(item.external_id, Math.min(depth + 1, 5));
    }
  };
  walk("__root__", 0);
  for (const item of source) {
    if (!visited.has(item.external_id)) rows.push({ item, depth: 0 });
  }
  return rows;
});

const localCategoryRows = computed(() => {
  const source = localCategories.value
    .slice()
    .sort((a, b) => a.name.localeCompare(b.name));
  const byParent = new Map<string, LocalCategory[]>();
  for (const item of source) {
    const key = item.parent_id || "__root__";
    const list = byParent.get(key) || [];
    list.push(item);
    byParent.set(key, list);
  }
  const rows: Array<{ item: LocalCategory; depth: number }> = [];
  const visited = new Set<string>();
  const walk = (parent: string, depth: number) => {
    for (const item of byParent.get(parent) || []) {
      if (visited.has(item.id)) continue;
      visited.add(item.id);
      rows.push({ item, depth });
      walk(item.id, Math.min(depth + 1, 5));
    }
  };
  walk("__root__", 0);
  for (const item of source)
    if (!visited.has(item.id)) rows.push({ item, depth: 0 });
  return rows;
});

const productPages = computed(() =>
  Math.max(1, Math.ceil(productTotal.value / productPageSize.value)),
);
const categoryPages = computed(() =>
  Math.max(1, Math.ceil(categoryTotal.value / categoryPageSize.value)),
);
const runPages = computed(() =>
  Math.max(1, Math.ceil(runTotal.value / runPageSize.value)),
);
const changePages = computed(() =>
  Math.max(1, Math.ceil(changeTotal.value / changePageSize.value)),
);
const selectedCount = computed(() => selectedIDs.value.length);
const activeImportJobs = computed(() =>
  importJobs.value.filter((job) =>
    ["queued", "running", "retrying"].includes(job.status),
  ),
);
const currentPageSelected = computed(
  () =>
    products.value.length > 0 &&
    products.value.every((item) => selectedIDSet.value.has(item.external_id)),
);
const policyMirrorDisabled = computed(() => !policy.value.sync_media);
const importMirrorDisabled = computed(() => !importForm.value.sync_media);
const canImport = computed(
  () =>
    canManage.value &&
    selectedCount.value > 0 &&
    selectedCount.value <= 500 &&
    !importing.value,
);

async function loadPolicy() {
  try {
    const { data } = await adminApi.get(
      `/suppliers/${encodeURIComponent(props.supplier.id)}/sync-policy`,
    );
    const incoming = unwrap<Partial<SyncPolicy>>(data, {});
    policy.value = {
      ...defaultPolicy(),
      ...incoming,
      supplier_id: props.supplier.id,
    } as SyncPolicy;
    importForm.value = {
      ...defaultImportForm(),
      sync_title: Boolean(policy.value.sync_title),
      sync_summary: Boolean(policy.value.sync_summary),
      sync_description: Boolean(policy.value.sync_description),
      sync_media: Boolean(policy.value.sync_media),
      mirror_remote_media: Boolean(
        policy.value.mirror_remote_media && policy.value.sync_media,
      ),
      sync_price: Boolean(policy.value.sync_price),
      sync_stock: Boolean(policy.value.sync_stock),
      sync_variants: Boolean(policy.value.sync_variants),
      sync_status: Boolean(policy.value.sync_status),
      sync_purchase_limits: Boolean(policy.value.sync_purchase_limits),
    };
  } catch (failure) {
    error.value = apiMessage(
      failure,
      text("supply.catalogPolicyLoadError", "同步策略读取失败"),
    );
  }
}

async function loadLocalCategories() {
  try {
    const { data } = await adminApi.get("/categories");
    const incoming = unwrap<unknown>(data, []);
    localCategories.value = Array.isArray(incoming)
      ? (incoming as LocalCategory[])
      : [];
  } catch (failure) {
    error.value = apiMessage(
      failure,
      text("supply.catalogLocalCategoryLoadError", "本地分类读取失败"),
    );
  }
}

async function loadCategories() {
  categoryLoading.value = true;
  try {
    const { data } = await adminApi.get(
      `/suppliers/${encodeURIComponent(props.supplier.id)}/remote-categories`,
      {
        params: {
          page: categoryPage.value,
          page_size: categoryPageSize.value,
          ...(categoryQuery.value.trim()
            ? { q: categoryQuery.value.trim() }
            : {}),
        },
      },
    );
    const payload = pagePayload<RemoteCategory>(data);
    categories.value = payload.items;
    categoryTotal.value = payload.total;
    categoryPage.value = payload.page;
  } catch (failure) {
    error.value = apiMessage(
      failure,
      text("supply.catalogCategoryLoadError", "远端分类读取失败"),
    );
  } finally {
    categoryLoading.value = false;
  }
}

async function loadProductTreeCategories() {
  const requestSequence = ++treeCategoryRequestSequence;
  const supplierID = props.supplier.id;
  treeCategoryLoading.value = true;
  try {
    const collected = new Map<string, RemoteCategory>();
    let page = 1;
    let total = 0;
    do {
      const { data } = await adminApi.get(
        `/suppliers/${encodeURIComponent(supplierID)}/remote-categories`,
        { params: { page, page_size: 100 } },
      );
      if (
        requestSequence !== treeCategoryRequestSequence ||
        props.supplier.id !== supplierID
      )
        return;
      const payload = pagePayload<RemoteCategory>(data);
      total = payload.total;
      for (const item of payload.items) collected.set(item.external_id, item);
      if (!payload.items.length || collected.size >= total) break;
      page += 1;
    } while (true);
    treeCategories.value = [...collected.values()];
    if (productTreeInitializedForSupplier !== supplierID) {
      expandedProductCategoryIDs.value = [];
      productTreeInitializedForSupplier = supplierID;
    }
    if (productSearchMode.value)
      products.value.forEach((product) =>
        revealProductCategory(product.external_category_id, true),
      );
    if (productSearchMode.value) void loadMatchingCategoryProducts();
  } catch (failure) {
    error.value = apiMessage(
      failure,
      text("supply.catalogCategoryLoadError", "远端分类读取失败"),
    );
  } finally {
    if (requestSequence === treeCategoryRequestSequence)
      treeCategoryLoading.value = false;
  }
}

async function loadProducts() {
  productLoading.value = true;
  try {
    const params: Record<string, string | number> = {
      page: productPage.value,
      page_size: productPageSize.value,
    };
    if (productQuery.value.trim()) params.q = productQuery.value.trim();
    if (productStatus.value) params.status = productStatus.value;
    const { data } = await adminApi.get(
      `/suppliers/${encodeURIComponent(props.supplier.id)}/remote-products`,
      { params },
    );
    const payload = pagePayload<RemoteProduct>(data);
    products.value = payload.items;
    productTotal.value = payload.total;
    productPage.value = payload.page;
    if (productSearchMode.value) {
      for (const product of payload.items)
        revealProductCategory(product.external_category_id, true);
      await loadMatchingCategoryProducts();
    }
  } catch (failure) {
    error.value = apiMessage(
      failure,
      text("supply.catalogProductLoadError", "远端商品读取失败"),
    );
  } finally {
    productLoading.value = false;
  }
}

async function loadRuns() {
  runLoading.value = true;
  try {
    const { data } = await adminApi.get(
      `/suppliers/${encodeURIComponent(props.supplier.id)}/sync-runs`,
      { params: { page: runPage.value, page_size: runPageSize.value } },
    );
    const payload = pagePayload<SyncRun>(data);
    runs.value = payload.items;
    runTotal.value = payload.total;
    runPage.value = payload.page;
  } catch (failure) {
    error.value = apiMessage(
      failure,
      text("supply.catalogRunsLoadError", "同步运行记录读取失败"),
    );
  } finally {
    runLoading.value = false;
  }
}

async function refreshRunHistory() {
  await Promise.all([loadImportJobs(), loadRuns()]);
}

function scheduleImportJobPoll() {
  if (importPollTimer) clearTimeout(importPollTimer);
  importPollTimer = undefined;
  if (!componentMounted || !activeImportJobs.value.length) return;
  importPollTimer = setTimeout(() => void loadImportJobs(false), 2500);
}

async function loadImportJobs(showLoading = true) {
  if (showLoading) importJobsLoading.value = true;
  const before = new Map(importJobs.value.map((job) => [job.id, job.status]));
  try {
    const { data } = await adminApi.get(
      `/suppliers/${encodeURIComponent(props.supplier.id)}/import-jobs`,
      { params: { page: 1, page_size: 20 } },
    );
    if (!componentMounted) return;
    const payload = pagePayload<ImportJob>(data);
    importJobs.value = payload.items;
    const completed = payload.items.some(
      (job) =>
        job.status === "succeeded" &&
        before.has(job.id) &&
        before.get(job.id) !== "succeeded",
    );
    if (completed) {
      await Promise.all([loadProducts(), loadCategories(), loadRuns()]);
    }
  } catch (failure) {
    if (showLoading && componentMounted) {
      error.value = apiMessage(
        failure,
        text("supply.catalogImportJobsLoadError", "接入任务读取失败"),
      );
    }
  } finally {
    if (showLoading && componentMounted) importJobsLoading.value = false;
    if (componentMounted) scheduleImportJobPoll();
  }
}

async function refreshAll() {
  loading.value = true;
  error.value = "";
  categoryProductIDs.value = {};
  categoryProductIDsComplete.value = [];
  categoryProductPages.value = {};
  categoryProductPageRequests.clear();
  try {
    await Promise.all([
      loadPolicy(),
      loadCategories(),
      loadProductTreeCategories(),
      loadProducts(),
      loadRuns(),
      loadImportJobs(),
      loadLocalCategories(),
    ]);
    if (!productSearchMode.value)
      await Promise.all(
        expandedProductCategoryIDs.value.map((id) =>
          loadCategoryProductPage(id, 1),
        ),
      );
  } finally {
    loading.value = false;
  }
}

function scheduleProductSearch() {
  if (queryTimer) clearTimeout(queryTimer);
  queryTimer = setTimeout(() => {
    productPage.value = 1;
    void loadProducts();
  }, 260);
}

function scheduleCategorySearch() {
  if (queryTimer) clearTimeout(queryTimer);
  queryTimer = setTimeout(() => {
    categoryPage.value = 1;
    void loadCategories();
  }, 260);
}

function toggleProduct(product: RemoteProduct) {
  if (!canManage.value) return;
  const externalID = product.external_id;
  const next = new Set(selectedIDs.value);
  const categories = { ...selectedProductCategoryIDs.value };
  if (next.has(externalID)) {
    next.delete(externalID);
    delete categories[externalID];
  } else if (next.size < 500) {
    next.add(externalID);
    categories[externalID] = product.external_category_id;
  }
  selectedIDs.value = [...next];
  selectedProductCategoryIDs.value = categories;
}

function toggleProductCategoryExpanded(externalID: string) {
  const next = new Set(expandedProductCategoryIDs.value);
  if (next.has(externalID)) next.delete(externalID);
  else {
    next.add(externalID);
    if (!productSearchMode.value) void loadCategoryProductPage(externalID, 1);
  }
  expandedProductCategoryIDs.value = [...next];
}

function revealProductCategory(externalID: string, expandSelf = false) {
  if (!externalID) return;
  const next = new Set(expandedProductCategoryIDs.value);
  if (expandSelf) next.add(externalID);
  const visited = new Set<string>();
  let current = productCategoryByID.value.get(externalID);
  while (current?.external_parent_id && !visited.has(current.external_id)) {
    visited.add(current.external_id);
    next.add(current.external_parent_id);
    current = productCategoryByID.value.get(current.external_parent_id);
  }
  expandedProductCategoryIDs.value = [...next];
}

async function loadCategoryProductPage(
  externalID: string,
  page: number,
  allowDuringSearch = false,
) {
  if (!externalID || (productSearchMode.value && !allowDuringSearch)) return;
  const supplierID = props.supplier.id;
  const requestID = (categoryProductPageRequests.get(externalID) || 0) + 1;
  categoryProductPageRequests.set(externalID, requestID);
  const current = categoryProductPageState(externalID);
  const pageSize = allowDuringSearch ? 20 : current.page_size;
  categoryProductPages.value = {
    ...categoryProductPages.value,
    [externalID]: { ...current, loading: true },
  };
  try {
    const { data } = await adminApi.get(
      `/suppliers/${encodeURIComponent(supplierID)}/remote-products`,
      {
        params: {
          category_id: externalID,
          page,
          page_size: pageSize,
          ...(allowDuringSearch && productStatus.value
            ? { status: productStatus.value }
            : {}),
        },
      },
    );
    if (
      categoryProductPageRequests.get(externalID) !== requestID ||
      props.supplier.id !== supplierID
    )
      return;
    const payload = pagePayload<RemoteProduct>(data);
    categoryProductPages.value = {
      ...categoryProductPages.value,
      [externalID]: {
        items: payload.items,
        total: payload.total,
        page: payload.page,
        page_size: payload.page_size || pageSize,
        loading: false,
      },
    };
  } catch (failure) {
    if (categoryProductPageRequests.get(externalID) !== requestID) return;
    categoryProductPages.value = {
      ...categoryProductPages.value,
      [externalID]: { ...current, loading: false },
    };
    error.value = apiMessage(failure, "分类商品读取失败，请稍后重试");
  }
}

async function loadMatchingCategoryProducts() {
  if (!productQuery.value.trim() || !matchingProductCategories.value.length)
    return;
  for (const category of matchingProductCategories.value)
    revealProductCategory(category.external_id, true);
  await Promise.all(
    matchingProductCategories.value.map((category) =>
      loadCategoryProductPage(category.external_id, 1, true),
    ),
  );
}

async function fetchDirectCategoryProductIDs(
  supplierID: string,
  externalCategoryID: string,
  requestSequence: number,
) {
  const ids = new Set<string>();
  let page = 1;
  let total = 0;
  do {
    const { data } = await adminApi.get(
      `/suppliers/${encodeURIComponent(supplierID)}/remote-products`,
      {
        params: {
          category_id: externalCategoryID,
          page,
          page_size: 100,
        },
      },
    );
    if (
      requestSequence !== categorySelectionRequestSequence ||
      props.supplier.id !== supplierID
    )
      throw new Error("category selection cancelled");
    const payload = pagePayload<RemoteProduct>(data);
    total = payload.total;
    if (total > 500) return { ids: [] as string[], total, overLimit: true };
    for (const product of payload.items) ids.add(product.external_id);
    if (!payload.items.length || ids.size >= total) break;
    page += 1;
  } while (true);
  return { ids: [...ids], total, overLimit: false };
}

async function toggleProductCategorySelection(externalID: string) {
  if (!canManage.value) return;
  if (categorySelectionLoadingID.value) return;
  const existing = categorySelectionState(externalID);
  if (existing.complete && existing.checked) {
    const next = new Set(selectedIDs.value);
    const categories = { ...selectedProductCategoryIDs.value };
    existing.ids.forEach((id) => {
      next.delete(id);
      delete categories[id];
    });
    selectedIDs.value = [...next];
    selectedProductCategoryIDs.value = categories;
    return;
  }

  const requestSequence = ++categorySelectionRequestSequence;
  const supplierID = props.supplier.id;
  categorySelectionLoadingID.value = externalID;
  error.value = "";
  try {
    const branch = categoryDescendantIDs(externalID);
    const knownBranchCount = branch.reduce((total, categoryID) => {
      const count = productCategoryByID.value.get(categoryID)?.product_count;
      return total + (typeof count === "number" ? Math.max(0, count) : 0);
    }, 0);
    if (knownBranchCount > 500) {
      error.value = `“${categoryPath(externalID)}”分类（含子分类）共有 ${knownBranchCount} 个商品，超过单次 500 个上限，请展开后分批勾选子分类。`;
      return;
    }
    const nextCache = { ...categoryProductIDs.value };
    const complete = new Set(categoryProductIDsComplete.value);
    const branchIDs = new Set<string>();
    const branchCategories = new Map<string, string>();
    const pending: string[] = [];
    for (const categoryID of branch) {
      if (complete.has(categoryID)) {
        (nextCache[categoryID] || []).forEach((id) => {
          branchIDs.add(id);
          branchCategories.set(id, categoryID);
        });
        continue;
      }
      const advertisedCount =
        productCategoryByID.value.get(categoryID)?.product_count;
      if (advertisedCount === 0) {
        nextCache[categoryID] = [];
        complete.add(categoryID);
        continue;
      }
      pending.push(categoryID);
    }
    if (branchIDs.size > 500) {
      error.value = `“${categoryPath(externalID)}”分类（含子分类）超过 500 个商品，不能一次整类接入，请展开后分批勾选子分类。`;
      return;
    }
    for (let offset = 0; offset < pending.length; offset += 4) {
      const batch = pending.slice(offset, offset + 4);
      const results = await Promise.all(
        batch.map(async (categoryID) => ({
          categoryID,
          result: await fetchDirectCategoryProductIDs(
            supplierID,
            categoryID,
            requestSequence,
          ),
        })),
      );
      for (const { categoryID, result } of results) {
        if (result.overLimit) {
          error.value = `“${categoryPath(externalID)}”分类（含子分类）超过 500 个商品，不能一次整类接入，请展开后分批勾选子分类。`;
          return;
        }
        nextCache[categoryID] = result.ids;
        complete.add(categoryID);
        result.ids.forEach((id) => {
          branchIDs.add(id);
          branchCategories.set(id, categoryID);
        });
        if (branchIDs.size > 500) {
          error.value = `“${categoryPath(externalID)}”分类（含子分类）超过 500 个商品，不能一次整类接入，请展开后分批勾选子分类。`;
          return;
        }
      }
    }
    if (requestSequence !== categorySelectionRequestSequence) return;
    categoryProductIDs.value = nextCache;
    categoryProductIDsComplete.value = [...complete];
    if (!branchIDs.size) {
      notice.value = `“${categoryPath(externalID)}”分类中暂无商品。`;
      return;
    }
    const next = new Set(selectedIDs.value);
    const selectedCategories = { ...selectedProductCategoryIDs.value };
    const alreadySelected = [...branchIDs].every((id) => next.has(id));
    if (alreadySelected)
      branchIDs.forEach((id) => {
        next.delete(id);
        delete selectedCategories[id];
      });
    else {
      const additions = [...branchIDs].filter((id) => !next.has(id));
      if (next.size + additions.length > 500) {
        error.value = `当前已选择 ${next.size} 个商品，再勾选“${categoryPath(externalID)}”将超过 500 个上限，请先清理部分选择。`;
        return;
      }
      additions.forEach((id) => {
        next.add(id);
        selectedCategories[id] = branchCategories.get(id) || externalID;
      });
    }
    branchIDs.forEach((id) => {
      if (next.has(id) && !selectedCategories[id])
        selectedCategories[id] = branchCategories.get(id) || externalID;
    });
    selectedIDs.value = [...next];
    selectedProductCategoryIDs.value = selectedCategories;
  } catch (failure) {
    if (requestSequence !== categorySelectionRequestSequence) return;
    error.value = apiMessage(failure, "分类商品读取失败，请稍后重试");
  } finally {
    if (requestSequence === categorySelectionRequestSequence)
      categorySelectionLoadingID.value = "";
  }
}

function toggleCurrentPage() {
  if (!canManage.value) return;
  const next = new Set(selectedIDs.value);
  const categories = { ...selectedProductCategoryIDs.value };
  if (currentPageSelected.value)
    products.value.forEach((item) => {
      next.delete(item.external_id);
      delete categories[item.external_id];
    });
  else
    products.value.forEach((item) => {
      if (next.size < 500) {
        next.add(item.external_id);
        categories[item.external_id] = item.external_category_id;
      }
    });
  selectedIDs.value = [...next];
  selectedProductCategoryIDs.value = categories;
}

function clearSelection() {
  selectedIDs.value = [];
  selectedProductCategoryIDs.value = {};
  importDockExpanded.value = false;
}

function normalizePolicyBeforeSave() {
  if (!policy.value.sync_media) policy.value.mirror_remote_media = false;
  if (!policy.value.auto_sync_categories)
    policy.value.auto_create_categories = false;
  if (!policy.value.auto_sync_products)
    policy.value.auto_create_products = false;
}

async function savePolicy() {
  if (!canManage.value) return;
  if (policyReason.value.trim().length < 4) {
    error.value = text(
      "supply.catalogReasonError",
      "请填写至少 4 个字的变更原因",
    );
    return;
  }
  normalizePolicyBeforeSave();
  saving.value = true;
  error.value = "";
  try {
    const payload = { ...policy.value };
    delete (payload as Partial<SyncPolicy>).supplier_id;
    const { data } = await adminApi.put(
      `/suppliers/${encodeURIComponent(props.supplier.id)}/sync-policy`,
      payload,
      { headers: { "X-Change-Reason": policyReason.value.trim() } },
    );
    policy.value = {
      ...policy.value,
      ...unwrap<Partial<SyncPolicy>>(data, {}),
    };
    policyReason.value = "";
    notice.value = text("supply.catalogPolicySaved", "同步策略已保存");
    emit("notice", notice.value);
  } catch (failure) {
    error.value = apiMessage(
      failure,
      text("supply.catalogPolicySaveError", "同步策略保存失败"),
    );
  } finally {
    saving.value = false;
  }
}

function validateImport() {
  if (!selectedCount.value)
    return text("supply.catalogSelectProducts", "请先选择要接入的商品");
  if (selectedCount.value > 500)
    return text("supply.catalogMaxProducts", "单次最多接入 500 个商品");
  if (
    importForm.value.category_mode === "target" &&
    !importForm.value.target_category_id
  )
    return text("supply.catalogTargetRequired", "请选择目标本地分类");
  if (
    !Number.isFinite(importForm.value.markup_percent) ||
    importForm.value.markup_percent < 0 ||
    importForm.value.markup_percent > 1000
  )
    return text("supply.catalogMarkupRange", "加价比例需在 0% 至 1000% 之间");
  if (importForm.value.price_mode === "fixed_amount") {
    try {
      const amount = BigInt(
        majorToMinor(importForm.value.markup_amount_major, storeCurrency.value),
      );
      const maximum = BigInt(majorToMinor("1000000", storeCurrency.value));
      if (amount < 0n || amount > maximum)
        throw new Error("amount out of range");
    } catch {
      return text(
        "supply.catalogMarkupAmountRange",
        "固定加价金额必须在 0 至 1,000,000 之间",
      );
    }
  }
  if (importForm.value.reason.trim().length < 4)
    return text("supply.catalogReasonError", "请填写至少 4 个字的变更原因");
  return "";
}

async function importSelected() {
  if (!canManage.value) return;
  const validation = validateImport();
  if (validation) {
    error.value = validation;
    return;
  }
  importing.value = true;
  error.value = "";
  importResult.value = null;
  try {
    const form = importForm.value;
    const payload = {
      external_product_ids: selectedIDs.value,
      category_mode: form.category_mode,
      target_category_id:
        form.category_mode === "target" ? form.target_category_id : null,
      auto_publish: form.auto_publish,
      price_mode: form.price_mode,
      markup_basis_point: Math.round(Number(form.markup_percent) * 100),
      markup_amount:
        form.price_mode === "fixed_amount"
          ? minorToSafeNumber(
              majorToMinor(form.markup_amount_major, storeCurrency.value),
            )
          : 0,
      sync_title: form.sync_title,
      sync_summary: form.sync_summary,
      sync_description: form.sync_description,
      sync_media: form.sync_media,
      mirror_remote_media: form.mirror_remote_media,
      sync_price: form.sync_price,
      sync_stock: form.sync_stock,
      sync_variants: form.sync_variants,
      sync_status: form.sync_status,
      sync_purchase_limits: form.sync_purchase_limits,
    };
    const { data } = await adminApi.post(
      `/suppliers/${encodeURIComponent(props.supplier.id)}/import`,
      payload,
      { headers: { "X-Change-Reason": form.reason.trim() } },
    );
    const accepted = unwrap<ImportJob>(data, {} as ImportJob);
    importResult.value = {
      requested: Number(accepted.requested_count || selectedCount.value),
      imported: Number(accepted.imported_count || 0),
      skipped_mapped: Number(accepted.skipped_count || 0),
      sync_queue_status: accepted.status || "queued",
      job_id: accepted.id,
    };
    form.reason = "";
    notice.value = text(
      "supply.catalogImportAccepted",
      "接入任务已提交，正在后台同步库存与媒体",
    );
    emit("notice", notice.value);
    selectedIDs.value = [];
    selectedProductCategoryIDs.value = {};
    importDockExpanded.value = false;
    tab.value = "runs";
    await loadImportJobs();
  } catch (failure) {
    error.value = apiMessage(
      failure,
      text("supply.catalogImportError", "商品接入失败"),
    );
  } finally {
    importing.value = false;
  }
}

function beginRetryImport(job: ImportJob) {
  if (!canManage.value) return;
  retryingImportJobID.value = job.id;
  retryImportReason.value = "";
}

function cancelRetryImport() {
  retryingImportJobID.value = "";
  retryImportReason.value = "";
}

async function retryImportJob(job: ImportJob) {
  if (!canManage.value) return;
  if (retryImportReason.value.trim().length < 4) {
    error.value = text(
      "supply.catalogReasonError",
      "请填写至少 4 个字的变更原因",
    );
    return;
  }
  retryImportSaving.value = true;
  error.value = "";
  try {
    await adminApi.post(
      `/suppliers/${encodeURIComponent(props.supplier.id)}/import-jobs/${encodeURIComponent(job.id)}/retry`,
      {},
      { headers: { "X-Change-Reason": retryImportReason.value.trim() } },
    );
    notice.value = text(
      "supply.catalogImportRetryAccepted",
      "接入任务已重新排队",
    );
    cancelRetryImport();
    await loadImportJobs();
  } catch (failure) {
    error.value = apiMessage(
      failure,
      text("supply.catalogImportRetryError", "接入任务重试失败"),
    );
  } finally {
    retryImportSaving.value = false;
  }
}

async function loadRunChanges() {
  const run = selectedRun.value;
  if (!run) return;
  const requestSequence = ++changeRequestSequence;
  changes.value = [];
  changesLoading.value = true;
  try {
    const { data } = await adminApi.get(
      `/suppliers/${encodeURIComponent(props.supplier.id)}/sync-runs/${encodeURIComponent(run.id)}/changes`,
      {
        params: {
          page: changePage.value,
          page_size: changePageSize.value,
        },
      },
    );
    if (
      requestSequence !== changeRequestSequence ||
      selectedRun.value?.id !== run.id
    )
      return;
    const payload = pagePayload<SyncChange>(data);
    changes.value = payload.items;
    changeTotal.value = payload.total;
    changePage.value = payload.page;
  } catch (failure) {
    if (
      requestSequence !== changeRequestSequence ||
      selectedRun.value?.id !== run.id
    )
      return;
    error.value = apiMessage(
      failure,
      text("supply.catalogChangesLoadError", "同步变更明细读取失败"),
    );
  } finally {
    if (requestSequence === changeRequestSequence) changesLoading.value = false;
  }
}

function openRun(run: SyncRun) {
  selectedRun.value = run;
  changePage.value = 1;
  changeTotal.value = 0;
  void loadRunChanges();
}

function closeRun() {
  changeRequestSequence += 1;
  selectedRun.value = null;
  changes.value = [];
  changesLoading.value = false;
  changePage.value = 1;
  changeTotal.value = 0;
}

function statusText(status: string) {
  const map: Record<string, string> = {
    active: text("supply.statusActive", "已启用"),
    inactive: text("supply.catalogStatusInactive", "已失效"),
    missing: text("supply.catalogStatusMissing", "上游缺失"),
    queued: text("supply.catalogRunQueued", "排队中"),
    running: text("supply.catalogRunRunning", "执行中"),
    retrying: text("supply.catalogRunRetrying", "等待重试"),
    succeeded: text("supply.catalogRunSucceeded", "成功"),
    partial: text("supply.catalogRunPartial", "部分成功"),
    failed: text("supply.catalogRunFailed", "失败"),
    cancelled: text("supply.catalogRunCancelled", "已取消"),
  };
  return map[status] || status || "—";
}

function triggerText(trigger: string) {
  const map: Record<string, string> = {
    manual: text("supply.catalogTriggerManual", "手动"),
    schedule: text("supply.catalogTriggerSchedule", "定时"),
    webhook: text("supply.catalogTriggerWebhook", "Webhook"),
    recovery: text("supply.catalogTriggerRecovery", "恢复"),
  };
  return map[trigger] || trigger || "—";
}

function changedFields(value: unknown) {
  const parsed =
    typeof value === "string"
      ? (() => {
          try {
            return JSON.parse(value);
          } catch {
            return [];
          }
        })()
      : value;
  if (Array.isArray(parsed))
    return parsed.map((item) => String(item)).join("、") || "—";
  if (parsed && typeof parsed === "object")
    return Object.keys(parsed as Record<string, unknown>).join("、") || "—";
  return "—";
}

function goProductPage(next: number) {
  if (next < 1 || next > productPages.value || next === productPage.value)
    return;
  productPage.value = next;
  void loadProducts();
}

function goCategoryPage(next: number) {
  if (next < 1 || next > categoryPages.value || next === categoryPage.value)
    return;
  categoryPage.value = next;
  void loadCategories();
}

function goRunPage(next: number) {
  if (next < 1 || next > runPages.value || next === runPage.value) return;
  runPage.value = next;
  void loadRuns();
}

function goChangePage(next: number) {
  if (
    next < 1 ||
    next > changePages.value ||
    next === changePage.value ||
    changesLoading.value
  )
    return;
  changePage.value = next;
  void loadRunChanges();
}

watch(
  () => importForm.value.category_mode,
  (mode) => {
    if (mode === "mirror") importForm.value.target_category_id = "";
  },
);
watch(
  () => importForm.value.sync_media,
  (enabled) => {
    if (!enabled) importForm.value.mirror_remote_media = false;
  },
);
watch(
  () => policy.value.sync_media,
  (enabled) => {
    if (!enabled) policy.value.mirror_remote_media = false;
  },
);
watch(productSearchMode, (enabled, wasEnabled) => {
  if (!enabled && wasEnabled) expandedProductCategoryIDs.value = [];
});
watch(
  () => props.supplier.id,
  () => {
    treeCategoryRequestSequence += 1;
    categorySelectionRequestSequence += 1;
    productTreeInitializedForSupplier = "";
    selectedIDs.value = [];
    selectedProductCategoryIDs.value = {};
    treeCategories.value = [];
    expandedProductCategoryIDs.value = [];
    categorySelectionLoadingID.value = "";
    categoryProductIDs.value = {};
    categoryProductIDsComplete.value = [];
    categoryProductPages.value = {};
    categoryProductPageRequests.clear();
    importDockExpanded.value = false;
    productPage.value = 1;
    categoryPage.value = 1;
    runPage.value = 1;
    importJobs.value = [];
    cancelRetryImport();
    tab.value = "products";
    void refreshAll();
  },
);

onMounted(() => {
  componentMounted = true;
  window.addEventListener("keydown", handleEscape);
  void loadCurrencyDirectory();
  void refreshAll();
});

onBeforeUnmount(() => {
  componentMounted = false;
  if (queryTimer) clearTimeout(queryTimer);
  if (importPollTimer) clearTimeout(importPollTimer);
  window.removeEventListener("keydown", handleEscape);
});

function handleEscape(event: KeyboardEvent) {
  if (event.key !== "Escape" || saving.value || importing.value) return;
  if (selectedRun.value) {
    closeRun();
    return;
  }
  if (importDockExpanded.value) {
    importDockExpanded.value = false;
    return;
  }
  emit("close");
}
</script>

<template>
  <div class="supplier-catalog-backdrop" role="presentation">
    <section
      class="supplier-catalog-modal"
      role="dialog"
      aria-modal="true"
      :aria-label="text('supply.catalogTitle', '接入供应商货源')"
    >
      <header class="supplier-catalog-header">
        <div class="supplier-catalog-heading">
          <span class="supplier-catalog-kicker"
            >LINLINQI · SUPPLIER CATALOG</span
          >
          <h2>{{ text("supply.catalogTitle", "接入供应商货源") }}</h2>
          <p>
            {{ props.supplier.name }} · {{ props.supplier.protocol }} ·
            {{ props.supplier.price_currency }}
          </p>
        </div>
        <div class="supplier-catalog-head-actions">
          <button
            type="button"
            class="icon-button"
            :disabled="loading || importing"
            :aria-label="text('supply.catalogRefresh', '刷新')"
            @click="refreshAll"
          >
            <RefreshCw :size="16" :class="{ spinning: loading }" />
          </button>
          <button
            type="button"
            class="icon-button"
            :aria-label="text('supply.ariaClose', '关闭')"
            @click="emit('close')"
          >
            <X :size="18" />
          </button>
        </div>
      </header>

      <div v-if="error" class="supplier-catalog-alert error">
        <AlertCircle :size="15" /><span>{{ error }}</span
        ><button type="button" @click="error = ''"><X :size="13" /></button>
      </div>
      <div v-if="notice" class="supplier-catalog-alert success">
        <CheckCircle2 :size="15" /><span>{{ notice }}</span>
      </div>

      <nav class="supplier-catalog-tabs" role="tablist">
        <button
          type="button"
          :class="{ active: tab === 'products' }"
          @click="tab = 'products'"
        >
          <PackageCheck :size="15" />{{
            text("supply.catalogProducts", "远端商品")
          }}<b>{{ selectedCount }}</b>
        </button>
        <button
          type="button"
          :class="{ active: tab === 'categories' }"
          @click="tab = 'categories'"
        >
          <FolderTree :size="15" />{{
            text("supply.catalogCategories", "远端分类")
          }}<b>{{ categoryTotal }}</b>
        </button>
        <button
          type="button"
          :class="{ active: tab === 'policy' }"
          @click="tab = 'policy'"
        >
          <Settings2 :size="15" />{{ text("supply.catalogPolicy", "同步策略") }}
        </button>
        <button
          type="button"
          :class="{ active: tab === 'runs' }"
          @click="tab = 'runs'"
        >
          <Clock3 :size="15" />{{ text("supply.catalogRuns", "运行记录")
          }}<b>{{ runTotal }}</b>
        </button>
      </nav>

      <main class="supplier-catalog-content">
        <section v-if="tab === 'products'" class="catalog-products-view">
          <div class="catalog-toolbar">
            <form
              class="catalog-search"
              @submit.prevent="
                productPage = 1;
                loadProducts();
              "
            >
              <Search :size="15" /><input
                v-model="productQuery"
                type="search"
                :placeholder="
                  text(
                    'supply.catalogProductSearch',
                    '搜索商品名称、ID、SKU或描述',
                  )
                "
                @input="scheduleProductSearch"
              /><button
                v-if="productQuery"
                type="button"
                @click="
                  productQuery = '';
                  productPage = 1;
                  loadProducts();
                "
              >
                <X :size="13" />
              </button>
            </form>
            <select
              v-model="productStatus"
              :aria-label="text('supply.catalogStatusFilter', '按状态筛选')"
              @change="
                productPage = 1;
                loadProducts();
              "
            >
              <option value="">
                {{ text("supply.catalogAllStatuses", "全部状态") }}
              </option>
              <option value="active">
                {{ text("supply.statusActive", "已启用") }}
              </option>
              <option value="inactive">
                {{ text("supply.catalogStatusInactive", "已失效") }}
              </option>
              <option value="missing">
                {{ text("supply.catalogStatusMissing", "上游缺失") }}
              </option>
            </select>
            <button
              v-if="canManage && productSearchMode"
              type="button"
              class="catalog-outline-button"
              @click="toggleCurrentPage"
            >
              <Check :size="14" />{{
                currentPageSelected
                  ? text("supply.catalogClearPage", "取消本页")
                  : text("supply.catalogSelectPage", "全选本页")
              }}
            </button>
            <button
              v-if="canManage && selectedCount"
              type="button"
              class="catalog-quiet-button"
              @click="clearSelection"
            >
              {{ text("supply.catalogClearSelection", "清空选择") }}
            </button>
          </div>

          <div v-if="canManage" class="catalog-selection-bar">
            <span
              ><b>{{ selectedCount }}</b> / 500
              {{ text("supply.catalogSelectedSuffix", "个商品已选择") }}</span
            >
            <span class="catalog-selection-hint"
              ><Info :size="13" />{{
                text(
                  "supply.catalogSelectionHint",
                  "可跨页选择，接入前可在右侧配置同步策略",
                )
              }}</span
            >
          </div>

          <div
            v-if="
              productLoading &&
              !products.length &&
              !matchingProductCategories.length
            "
            class="catalog-state"
          >
            <LoaderCircle :size="24" class="spinning" />{{
              text("supply.catalogLoadingProducts", "正在读取远端商品…")
            }}
          </div>
          <div
            v-else-if="!products.length && !matchingProductCategories.length"
            class="catalog-state"
          >
            <PackageCheck :size="25" />{{
              text(
                "supply.catalogNoProducts",
                "暂无远端商品，请先执行供应商同步",
              )
            }}
          </div>
          <div
            v-else
            class="remote-product-tree-list"
            role="tree"
            aria-label="远端商品分类树列表"
            :aria-busy="treeCategoryLoading || productLoading"
          >
            <div class="product-tree-list-head" role="presentation">
              <span></span>
              <span></span>
              <span></span>
              <b>分类 / 商品</b>
              <b>所属分类</b>
              <b>价格与库存</b>
              <b>状态</b>
            </div>

            <div
              v-if="treeCategoryLoading && !treeCategories.length"
              class="product-tree-inline-state"
            >
              <LoaderCircle :size="16" class="spinning" />
              正在加载完整分类树…
            </div>

            <template
              v-for="row in productCategoryTreeRows"
              :key="row.item.external_id"
            >
              <div
                class="product-tree-category-row"
                :class="{
                  selected: categorySelectionState(row.item.external_id)
                    .checked,
                }"
                :style="{ '--product-category-depth': row.depth }"
                role="treeitem"
                :aria-level="row.depth + 1"
                :aria-expanded="
                  row.hasChildren ||
                  categoryTreeProductCount(row.item.external_id) > 0
                    ? expandedProductCategoryIDs.includes(row.item.external_id)
                    : undefined
                "
              >
                <span class="product-category-indent"></span>
                <button
                  v-if="
                    row.hasChildren ||
                    categoryTreeProductCount(row.item.external_id) > 0
                  "
                  type="button"
                  class="product-category-expander"
                  :aria-label="
                    expandedProductCategoryIDs.includes(row.item.external_id)
                      ? '收起 ' + row.item.name
                      : '展开 ' + row.item.name
                  "
                  @click.stop="
                    toggleProductCategoryExpanded(row.item.external_id)
                  "
                >
                  <ChevronDown
                    v-if="
                      expandedProductCategoryIDs.includes(row.item.external_id)
                    "
                    :size="13"
                  />
                  <ChevronRight v-else :size="13" />
                </button>
                <span v-else class="product-category-expander placeholder" />
                <label
                  class="product-category-check"
                  :title="'勾选“' + row.item.name + '”及其全部子分类商品'"
                  @click.stop
                >
                  <input
                    type="checkbox"
                    :checked="
                      categorySelectionState(row.item.external_id).checked
                    "
                    :indeterminate="
                      categorySelectionState(row.item.external_id).indeterminate
                    "
                    :aria-checked="
                      categorySelectionState(row.item.external_id).indeterminate
                        ? 'mixed'
                        : categorySelectionState(row.item.external_id).checked
                    "
                    :disabled="
                      !canManage || Boolean(categorySelectionLoadingID)
                    "
                    @change="
                      toggleProductCategorySelection(row.item.external_id)
                    "
                  />
                  <LoaderCircle
                    v-if="categorySelectionLoadingID === row.item.external_id"
                    :size="11"
                    class="product-category-check-loader spinning"
                  />
                  <span class="sr-only">
                    勾选 {{ row.item.name }} 分类及全部子分类
                  </span>
                </label>
                <button
                  type="button"
                  class="product-tree-category-main"
                  :title="categoryPath(row.item.external_id)"
                  @click="toggleProductCategoryExpanded(row.item.external_id)"
                >
                  <span>
                    <b>{{ row.item.name || row.item.external_id }}</b>
                    <code>{{ row.item.external_id }}</code>
                  </span>
                  <small v-if="row.item.external_parent_id">
                    {{ categoryPath(row.item.external_id) }}
                  </small>
                </button>
                <span class="product-tree-category-path">
                  {{ categoryPath(row.item.external_id) }}
                </span>
                <span class="product-tree-category-count">
                  {{ categoryTreeProductCount(row.item.external_id) }} 件
                </span>
                <span class="status-badge" :class="'status-' + row.item.status">
                  {{ statusText(row.item.status) }}
                </span>
              </div>

              <div
                v-if="
                  expandedProductCategoryIDs.includes(row.item.external_id) &&
                  (!productSearchMode ||
                    visibleCategoryProducts(row.item.external_id).length)
                "
                class="product-tree-leaves"
                role="group"
              >
                <div
                  v-if="
                    !productSearchMode &&
                    categoryProductPageState(row.item.external_id).loading
                  "
                  class="product-tree-inline-state child"
                  :style="{ '--product-category-depth': row.depth + 1 }"
                >
                  <LoaderCircle :size="14" class="spinning" />
                  正在加载“{{ row.item.name }}”直属商品…
                </div>
                <div
                  v-else-if="
                    !productSearchMode &&
                    !visibleCategoryProducts(row.item.external_id).length
                  "
                  class="product-tree-inline-state child"
                  :style="{ '--product-category-depth': row.depth + 1 }"
                >
                  此分类暂无直属商品
                </div>
                <article
                  v-for="product in visibleCategoryProducts(
                    row.item.external_id,
                  )"
                  :key="
                    row.item.external_id +
                    '-' +
                    product.external_id +
                    '-' +
                    categoryProductPageState(row.item.external_id).page
                  "
                  class="remote-product-list-row"
                  :class="{
                    selected: selectedIDSet.has(product.external_id),
                  }"
                  :style="{ '--product-category-depth': row.depth + 1 }"
                  role="treeitem"
                  :aria-level="row.depth + 2"
                >
                  <span class="remote-product-indent"></span>
                  <label class="remote-product-list-check">
                    <input
                      type="checkbox"
                      :checked="selectedIDSet.has(product.external_id)"
                      :disabled="
                        !canManage ||
                        (!selectedIDSet.has(product.external_id) &&
                          selectedCount >= 500)
                      "
                      @change="toggleProduct(product)"
                    />
                    <span class="sr-only">勾选 {{ product.name }}</span>
                  </label>
                  <div class="remote-product-list-media">
                    <img
                      v-if="productImages(product)[0]"
                      :src="productImages(product)[0]"
                      loading="lazy"
                      referrerpolicy="no-referrer"
                      alt=""
                    />
                    <Image v-else :size="17" />
                    <span v-if="productImages(product).length > 1">
                      {{ productImages(product).length }}
                    </span>
                  </div>
                  <div class="remote-product-list-main">
                    <div>
                      <h3 :title="product.name">
                        {{ product.name || product.external_id }}
                      </h3>
                      <span v-if="product.mapped" class="mapped-tag">
                        {{ text("supply.catalogMapped", "已接入") }}
                      </span>
                    </div>
                    <code :title="product.external_id">
                      {{ product.external_id }}
                      <template v-if="product.external_sku">
                        · SKU {{ product.external_sku }}
                      </template>
                    </code>
                    <p
                      v-if="product.summary || product.description"
                      :title="stripHTML(product.description || product.summary)"
                    >
                      {{ stripHTML(product.summary || product.description) }}
                    </p>
                  </div>
                  <div class="remote-product-list-category">
                    <small>所属分类</small>
                    <b :title="categoryPath(product.external_category_id)">
                      {{ categoryPath(product.external_category_id) }}
                    </b>
                    <code>
                      {{ product.external_category_id || "uncategorized" }}
                    </code>
                  </div>
                  <div class="remote-product-list-commerce">
                    <b>
                      <s
                        v-if="
                          Number(product.original_price || 0) >
                          Number(product.price || 0)
                        "
                      >
                        {{
                          formatRemoteMoney(
                            product.original_price,
                            product.currency,
                          )
                        }}
                      </s>
                      {{ formatRemoteMoney(product.price, product.currency) }}
                    </b>
                    <span>
                      {{ text("supply.catalogStock", "库存") }}
                      {{ product.stock }}
                    </span>
                    <small>
                      {{ text("supply.catalogMinimum", "最小购买") }}
                      {{ Math.max(1, Number(product.minimum || 1)) }} ·
                      {{ text("supply.catalogMaximum", "最大购买") }}
                      {{
                        Number(product.maximum || 0) > 0 ? product.maximum : "∞"
                      }}
                    </small>
                  </div>
                  <div class="remote-product-list-state">
                    <span
                      class="status-badge"
                      :class="'status-' + product.status"
                    >
                      {{ statusText(product.status) }}
                    </span>
                    <small v-if="product.mapped">已建立映射</small>
                  </div>
                </article>
                <div
                  v-if="
                    (!productSearchMode ||
                      categoryNameMatchesProductSearch(row.item.external_id)) &&
                    categoryProductPageCount(row.item.external_id) > 1
                  "
                  class="category-product-pager"
                  :style="{ '--product-category-depth': row.depth + 1 }"
                >
                  <span>
                    直属商品
                    {{ categoryProductPageState(row.item.external_id).page }} /
                    {{ categoryProductPageCount(row.item.external_id) }} ·
                    {{ categoryProductPageState(row.item.external_id).total }}
                    条
                  </span>
                  <span>
                    <button
                      type="button"
                      :disabled="
                        categoryProductPageState(row.item.external_id).page <=
                          1 ||
                        categoryProductPageState(row.item.external_id).loading
                      "
                      aria-label="上一页直属商品"
                      @click="
                        loadCategoryProductPage(
                          row.item.external_id,
                          categoryProductPageState(row.item.external_id).page -
                            1,
                          categoryNameMatchesProductSearch(
                            row.item.external_id,
                          ),
                        )
                      "
                    >
                      <ChevronLeft :size="13" />
                    </button>
                    <button
                      type="button"
                      :disabled="
                        categoryProductPageState(row.item.external_id).page >=
                          categoryProductPageCount(row.item.external_id) ||
                        categoryProductPageState(row.item.external_id).loading
                      "
                      aria-label="下一页直属商品"
                      @click="
                        loadCategoryProductPage(
                          row.item.external_id,
                          categoryProductPageState(row.item.external_id).page +
                            1,
                          categoryNameMatchesProductSearch(
                            row.item.external_id,
                          ),
                        )
                      "
                    >
                      <ChevronRight :size="13" />
                    </button>
                  </span>
                </div>
              </div>
            </template>

            <template
              v-for="externalCategoryID in missingProductCategories"
              :key="'missing-' + externalCategoryID"
            >
              <div
                v-if="productSearchMode"
                class="product-tree-category-row missing"
                :style="{ '--product-category-depth': 0 }"
                role="treeitem"
                aria-expanded="true"
              >
                <span class="product-category-indent"></span>
                <span class="product-category-expander placeholder"></span>
                <label class="product-category-check">
                  <input
                    type="checkbox"
                    :checked="
                      categorySelectionState(externalCategoryID).checked
                    "
                    :indeterminate="
                      categorySelectionState(externalCategoryID).indeterminate
                    "
                    :aria-checked="
                      categorySelectionState(externalCategoryID).indeterminate
                        ? 'mixed'
                        : categorySelectionState(externalCategoryID).checked
                    "
                    :disabled="
                      !canManage || Boolean(categorySelectionLoadingID)
                    "
                    @change="toggleProductCategorySelection(externalCategoryID)"
                  />
                </label>
                <span class="product-tree-category-main">
                  <span>
                    <b>{{ externalCategoryID }}</b>
                    <code>分类快照缺失</code>
                  </span>
                </span>
                <span class="product-tree-category-path">
                  {{ externalCategoryID }}
                </span>
                <span class="product-tree-category-count">
                  {{ visibleCategoryProducts(externalCategoryID).length }} 件
                </span>
                <span class="status-badge status-missing">缺失</span>
              </div>
              <template v-if="productSearchMode">
                <article
                  v-for="product in visibleCategoryProducts(externalCategoryID)"
                  :key="'missing-product-' + product.external_id"
                  class="remote-product-list-row"
                  :class="{
                    selected: selectedIDSet.has(product.external_id),
                  }"
                  :style="{ '--product-category-depth': 1 }"
                  role="treeitem"
                >
                  <span class="remote-product-indent"></span>
                  <label class="remote-product-list-check">
                    <input
                      type="checkbox"
                      :checked="selectedIDSet.has(product.external_id)"
                      :disabled="
                        !canManage ||
                        (!selectedIDSet.has(product.external_id) &&
                          selectedCount >= 500)
                      "
                      @change="toggleProduct(product)"
                    />
                  </label>
                  <div class="remote-product-list-media">
                    <Image :size="17" />
                  </div>
                  <div class="remote-product-list-main">
                    <div>
                      <h3>{{ product.name || product.external_id }}</h3>
                    </div>
                    <code>{{ product.external_id }}</code>
                  </div>
                  <div class="remote-product-list-category">
                    <small>所属分类</small>
                    <b>{{ externalCategoryID }}</b>
                    <code>分类快照缺失</code>
                  </div>
                  <div class="remote-product-list-commerce">
                    <b>{{
                      formatRemoteMoney(product.price, product.currency)
                    }}</b>
                    <span>库存 {{ product.stock }}</span>
                  </div>
                  <div class="remote-product-list-state">
                    <span
                      class="status-badge"
                      :class="'status-' + product.status"
                    >
                      {{ statusText(product.status) }}
                    </span>
                  </div>
                </article>
              </template>
            </template>
          </div>

          <footer v-if="productSearchMode" class="catalog-pager">
            <span
              >{{ productPage }} / {{ productPages }} · {{ productTotal }}
              {{ text("supply.catalogRows", "条") }}</span
            >
            <div>
              <button
                type="button"
                :disabled="productPage <= 1 || productLoading"
                @click="goProductPage(productPage - 1)"
              >
                <ChevronLeft :size="14" /></button
              ><button
                type="button"
                :disabled="productPage >= productPages || productLoading"
                @click="goProductPage(productPage + 1)"
              >
                <ChevronRight :size="14" />
              </button>
            </div>
          </footer>
        </section>

        <section
          v-else-if="tab === 'categories'"
          class="catalog-categories-view"
        >
          <div class="catalog-toolbar">
            <form
              class="catalog-search"
              @submit.prevent="
                categoryPage = 1;
                loadCategories();
              "
            >
              <Search :size="15" /><input
                v-model="categoryQuery"
                type="search"
                :placeholder="
                  text('supply.catalogCategorySearch', '搜索远端分类名称或 ID')
                "
                @input="scheduleCategorySearch"
              /><button
                v-if="categoryQuery"
                type="button"
                @click="
                  categoryQuery = '';
                  categoryPage = 1;
                  loadCategories();
                "
              >
                <X :size="13" />
              </button>
            </form>
            <span class="catalog-toolbar-note"
              ><FolderTree :size="14" />{{
                text(
                  "supply.catalogCategoryTreeHint",
                  "分类图片、描述和层级会随策略同步",
                )
              }}</span
            >
          </div>
          <div
            v-if="categoryLoading && !categories.length"
            class="catalog-state"
          >
            <LoaderCircle :size="24" class="spinning" />{{
              text("supply.catalogLoadingCategories", "正在读取远端分类…")
            }}
          </div>
          <div v-else-if="!categoryRows.length" class="catalog-state">
            <FolderTree :size="25" />{{
              text(
                "supply.catalogNoCategories",
                "暂无远端分类，请先执行供应商同步",
              )
            }}
          </div>
          <div v-else class="remote-category-tree">
            <div
              v-for="row in categoryRows"
              :key="row.item.external_id"
              class="remote-category-row"
              :style="{ '--category-depth': row.depth }"
            >
              <span class="category-indent"></span>
              <div class="category-thumb">
                <img
                  v-if="row.item.image_url"
                  :src="row.item.image_url"
                  loading="lazy"
                  referrerpolicy="no-referrer"
                  alt=""
                /><FolderTree v-else :size="15" />
              </div>
              <div class="category-main">
                <b>{{ row.item.name }}</b
                ><code
                  >{{ row.item.external_id
                  }}<template v-if="row.item.external_parent_id">
                    · ↑ {{ row.item.external_parent_id }}</template
                  ></code
                ><small v-if="row.item.description">{{
                  stripHTML(row.item.description).slice(0, 120)
                }}</small>
              </div>
              <span
                class="category-local"
                :class="{ mapped: row.item.mapping_id }"
                >{{
                  row.item.local_category_name ||
                  text("supply.catalogUnmapped", "未绑定本地分类")
                }}</span
              ><span
                class="status-badge"
                :class="`status-${row.item.status}`"
                >{{ statusText(row.item.status) }}</span
              ><span
                v-if="row.item.mapping_last_error"
                class="category-error"
                :title="row.item.mapping_last_error"
                ><AlertCircle :size="13"
              /></span>
            </div>
          </div>
          <footer class="catalog-pager">
            <span
              >{{ categoryPage }} / {{ categoryPages }} · {{ categoryTotal }}
              {{ text("supply.catalogRows", "条") }}</span
            >
            <div>
              <button
                type="button"
                :disabled="categoryPage <= 1 || categoryLoading"
                @click="goCategoryPage(categoryPage - 1)"
              >
                <ChevronLeft :size="14" /></button
              ><button
                type="button"
                :disabled="categoryPage >= categoryPages || categoryLoading"
                @click="goCategoryPage(categoryPage + 1)"
              >
                <ChevronRight :size="14" />
              </button>
            </div>
          </footer>
        </section>

        <section v-else-if="tab === 'policy'" class="catalog-policy-view">
          <div class="catalog-section-intro">
            <div>
              <Settings2 :size="19" />
              <div>
                <h3>
                  {{ text("supply.catalogPolicyTitle", "自动同步与接入策略") }}
                </h3>
                <p>
                  {{
                    text(
                      "supply.catalogPolicyHint",
                      "逐字段控制上游可以改动的内容；价格始终按上游币种 → 店铺币种的快照汇率结算。",
                    )
                  }}
                </p>
              </div>
            </div>
            <span>{{ props.supplier.sync_interval_minutes }} min</span>
          </div>
          <div class="policy-grid">
            <label
              v-for="item in [
                [
                  'auto_sync_categories',
                  'supply.catalogAutoSyncCategories',
                  'supply.catalogAutoSyncCategoriesDesc',
                ],
                [
                  'auto_create_categories',
                  'supply.catalogAutoCreateCategories',
                  'supply.catalogAutoCreateCategoriesDesc',
                ],
                [
                  'auto_sync_products',
                  'supply.catalogAutoSyncProducts',
                  'supply.catalogAutoSyncProductsDesc',
                ],
                [
                  'auto_create_products',
                  'supply.catalogAutoCreateProducts',
                  'supply.catalogAutoCreateProductsDesc',
                ],
                [
                  'sync_title',
                  'supply.protocolSyncTitle',
                  'supply.protocolSyncTitleDesc',
                ],
                [
                  'sync_summary',
                  'supply.protocolSyncSummaryLabel',
                  'supply.protocolSyncSummaryDesc',
                ],
                [
                  'sync_description',
                  'supply.protocolSyncDescriptionLabel',
                  'supply.protocolSyncDescriptionDesc',
                ],
                [
                  'sync_media',
                  'supply.protocolSyncImagesLabel',
                  'supply.protocolSyncImagesDesc',
                ],
                [
                  'mirror_remote_media',
                  'supply.protocolSyncImagesLocalizeLabel',
                  'supply.protocolSyncImagesLocalizeDesc',
                ],
                [
                  'sync_price',
                  'supply.autoSyncPrice',
                  'supply.autoSyncPriceDesc',
                ],
                [
                  'sync_stock',
                  'supply.autoSyncStock',
                  'supply.autoSyncStockDesc',
                ],
                [
                  'sync_variants',
                  'supply.protocolSyncVariantsLabel',
                  'supply.protocolSyncVariantsDesc',
                ],
                [
                  'sync_status',
                  'supply.protocolSyncListingLabel',
                  'supply.protocolSyncListingDesc',
                ],
                [
                  'sync_purchase_limits',
                  'supply.protocolSyncPurchaseLimitLabel',
                  'supply.protocolSyncPurchaseLimitDesc',
                ],
              ]"
              :key="String(item[0])"
              class="policy-toggle"
              :class="{
                disabled:
                  !canManage ||
                  (item[0] === 'mirror_remote_media' && policyMirrorDisabled) ||
                  (item[0] === 'auto_create_categories' &&
                    !policy.auto_sync_categories) ||
                  (item[0] === 'auto_create_products' &&
                    (!policy.auto_sync_products ||
                      !policy.auto_sync_categories)),
              }"
            >
              <input
                v-model="policy[item[0] as keyof SyncPolicy]"
                type="checkbox"
                :disabled="
                  !canManage ||
                  (item[0] === 'mirror_remote_media' && policyMirrorDisabled) ||
                  (item[0] === 'auto_create_categories' &&
                    !policy.auto_sync_categories) ||
                  (item[0] === 'auto_create_products' &&
                    (!policy.auto_sync_products ||
                      !policy.auto_sync_categories))
                "
              />
              <span
                ><b>{{ text(String(item[1]), policyText(String(item[1]))) }}</b
                ><small>{{
                  text(String(item[2]), policyText(String(item[2])))
                }}</small></span
              >
            </label>
          </div>
          <div class="policy-bottom-grid">
            <label
              ><span>{{
                text("supply.catalogMissingAction", "上游商品消失时")
              }}</span
              ><select
                v-model="policy.missing_product_action"
                :disabled="!canManage"
              >
                <option value="keep">
                  {{ text("supply.catalogMissingKeep", "保留商品并记录警告") }}
                </option>
                <option value="unpublish">
                  {{
                    text("supply.catalogMissingUnpublish", "自动下架本地商品")
                  }}
                </option>
                <option value="disable_mapping">
                  {{ text("supply.catalogMissingDisable", "停用供货映射") }}
                </option>
              </select></label
            >
            <div class="policy-info">
              <Info :size="15" /><span>{{
                text(
                  "supply.catalogPolicyAuditHint",
                  "每次策略变更均需填写原因并写入管理员审计日志。",
                )
              }}</span>
            </div>
          </div>
          <label v-if="canManage" class="catalog-reason"
            ><span>{{ text("supply.changeReason", "变更原因") }}</span
            ><textarea
              v-model="policyReason"
              maxlength="500"
              :placeholder="
                text(
                  'supply.catalogReasonPlaceholder',
                  '说明本次同步策略调整原因（4 至 500 个字）',
                )
              "
            ></textarea
            ><small>{{ policyReason.trim().length }} / 500</small></label
          >
          <div v-if="canManage" class="catalog-form-actions">
            <button
              type="button"
              class="catalog-primary-button"
              :disabled="saving"
              @click="savePolicy"
            >
              <LoaderCircle v-if="saving" :size="15" class="spinning" /><Save
                v-else
                :size="15"
              />{{ text("supply.savePolicy", "保存策略") }}
            </button>
          </div>
        </section>

        <section v-else class="catalog-runs-view">
          <section class="import-job-panel">
            <header class="import-job-heading">
              <div>
                <UploadCloud :size="16" />
                <span>
                  <b>{{ text("supply.catalogImportJobs", "商品接入任务") }}</b>
                  <small>{{
                    text(
                      "supply.catalogImportJobsHint",
                      "持久化后台执行；失败任务可按原配置安全重试",
                    )
                  }}</small>
                </span>
              </div>
              <span v-if="activeImportJobs.length" class="import-active-count">
                {{ activeImportJobs.length }}
                {{ text("supply.catalogActiveJobs", "个进行中") }}
              </span>
            </header>
            <div
              v-if="importJobsLoading && !importJobs.length"
              class="catalog-state compact"
            >
              <LoaderCircle :size="18" class="spinning" />{{
                text("supply.catalogLoadingImportJobs", "正在读取接入任务…")
              }}
            </div>
            <div v-else-if="!importJobs.length" class="catalog-state compact">
              <UploadCloud :size="20" />{{
                text("supply.catalogNoImportJobs", "暂无商品接入任务")
              }}
            </div>
            <div v-else class="import-job-list">
              <article
                v-for="job in importJobs"
                :key="job.id"
                class="import-job-card"
              >
                <div class="import-job-topline">
                  <span class="status-badge" :class="`status-${job.status}`">{{
                    statusText(job.status)
                  }}</span>
                  <code>{{ job.id.slice(0, 8) }}</code>
                  <span>{{ formatTime(job.created_at) }}</span>
                  <small>
                    {{ text("supply.catalogAttempts", "尝试") }}
                    {{ job.attempts }}
                  </small>
                </div>
                <div class="import-progress-line">
                  <span>
                    <i
                      :style="{
                        width: `${Math.max(0, Math.min(100, job.progress_percent))}%`,
                      }"
                    ></i>
                  </span>
                  <b>{{ job.progress_percent }}%</b>
                </div>
                <div class="import-job-metrics">
                  <span>{{
                    text("supply.catalogRequestedMetric", "请求 {count}", {
                      count: job.requested_count,
                    })
                  }}</span>
                  <span class="success">{{
                    text("supply.catalogImportedMetric", "成功 {count}", {
                      count: job.imported_count,
                    })
                  }}</span>
                  <span>{{
                    text("supply.catalogSkippedMetric", "跳过 {count}", {
                      count: job.skipped_count,
                    })
                  }}</span>
                  <span>{{
                    text("supply.catalogCategoryMetric", "分类 {count}", {
                      count: job.categories_created,
                    })
                  }}</span>
                </div>
                <p v-if="job.error_summary" class="import-job-error">
                  <AlertCircle :size="14" />{{ job.error_summary }}
                </p>
                <div class="import-job-footer">
                  <small v-if="job.next_attempt_at">
                    {{ text("supply.catalogNextAttempt", "下次重试") }}：{{
                      formatTime(job.next_attempt_at)
                    }}
                  </small>
                  <small v-else-if="job.completed_at">
                    {{ text("supply.catalogCompletedAt", "完成") }}：{{
                      formatTime(job.completed_at)
                    }}
                  </small>
                  <span v-else></span>
                  <button
                    v-if="
                      canManage &&
                      job.can_retry &&
                      retryingImportJobID !== job.id
                    "
                    type="button"
                    class="catalog-outline-button compact"
                    @click="beginRetryImport(job)"
                  >
                    <RefreshCw :size="13" />{{
                      text("supply.catalogRetryImport", "重试")
                    }}
                  </button>
                </div>
                <div
                  v-if="canManage && retryingImportJobID === job.id"
                  class="import-retry-form"
                >
                  <textarea
                    v-model="retryImportReason"
                    rows="2"
                    maxlength="500"
                    :placeholder="
                      text(
                        'supply.catalogRetryReasonPlaceholder',
                        '填写重试原因（至少 4 个字）',
                      )
                    "
                  ></textarea>
                  <div>
                    <small>{{ retryImportReason.trim().length }} / 500</small>
                    <button
                      type="button"
                      :disabled="retryImportSaving"
                      @click="cancelRetryImport"
                    >
                      {{ text("common.cancel", "取消") }}
                    </button>
                    <button
                      type="button"
                      class="catalog-primary-button compact"
                      :disabled="
                        retryImportSaving || retryImportReason.trim().length < 4
                      "
                      @click="retryImportJob(job)"
                    >
                      <LoaderCircle
                        v-if="retryImportSaving"
                        :size="13"
                        class="spinning"
                      />
                      <RefreshCw v-else :size="13" />{{
                        text("supply.catalogConfirmRetry", "确认重试")
                      }}
                    </button>
                  </div>
                </div>
              </article>
            </div>
          </section>
          <div class="catalog-toolbar">
            <span class="catalog-toolbar-note"
              ><Clock3 :size="14" />{{
                text(
                  "supply.catalogRunsHint",
                  "同步任务可追溯；原始凭证和响应不会返回前台",
                )
              }}</span
            ><button
              type="button"
              class="catalog-outline-button"
              :disabled="runLoading"
              @click="refreshRunHistory"
            >
              <RefreshCw :size="14" :class="{ spinning: runLoading }" />{{
                text("supply.catalogRefresh", "刷新")
              }}
            </button>
          </div>
          <div v-if="runLoading && !runs.length" class="catalog-state">
            <LoaderCircle :size="24" class="spinning" />{{
              text("supply.catalogLoadingRuns", "正在读取运行记录…")
            }}
          </div>
          <div v-else-if="!runs.length" class="catalog-state">
            <Clock3 :size="25" />{{
              text("supply.catalogNoRuns", "暂无运行记录")
            }}
          </div>
          <div v-else class="run-list">
            <article
              v-for="run in runs"
              :key="run.id"
              class="run-card"
              :class="{ selected: selectedRun?.id === run.id }"
              @click="openRun(run)"
            >
              <div class="run-status">
                <span class="status-badge" :class="`status-${run.status}`">{{
                  statusText(run.status)
                }}</span
                ><small>{{ triggerText(run.trigger) }}</small>
              </div>
              <div class="run-main">
                <b>{{ formatTime(run.started_at) }}</b
                ><code>{{ run.protocol }} · {{ run.id.slice(0, 8) }}</code>
                <p v-if="run.error_summary">{{ run.error_summary }}</p>
              </div>
              <div class="run-metrics">
                <span
                  >{{ text("supply.catalogCategories", "分类") }}
                  {{ run.categories_seen }} / {{ run.categories_created }}</span
                ><span
                  >{{ text("supply.catalogProducts", "商品") }}
                  {{ run.products_seen }} / {{ run.products_created }} +{{
                    run.products_updated
                  }}</span
                ><span
                  >{{ text("supply.catalogMediaMirrored", "媒体本地化") }}
                  {{ run.media_mirrored }}</span
                ><span v-if="run.warnings">⚠ {{ run.warnings }}</span>
              </div>
              <ChevronDown :size="16" class="run-chevron" />
            </article>
          </div>
          <footer class="catalog-pager">
            <span
              >{{ runPage }} / {{ runPages }} · {{ runTotal }}
              {{ text("supply.catalogRows", "条") }}</span
            >
            <div>
              <button
                type="button"
                :disabled="runPage <= 1 || runLoading"
                @click="goRunPage(runPage - 1)"
              >
                <ChevronLeft :size="14" /></button
              ><button
                type="button"
                :disabled="runPage >= runPages || runLoading"
                @click="goRunPage(runPage + 1)"
              >
                <ChevronRight :size="14" />
              </button>
            </div>
          </footer>
          <div v-if="selectedRun" class="run-change-panel">
            <header>
              <div>
                <Layers3 :size="15" /><b>{{
                  text("supply.catalogChanges", "变更明细")
                }}</b
                ><small>{{ formatTime(selectedRun.started_at) }}</small>
              </div>
              <button type="button" @click="closeRun"><X :size="15" /></button>
            </header>
            <div v-if="changesLoading" class="catalog-state compact">
              <LoaderCircle :size="18" class="spinning" />{{
                text("supply.catalogLoadingChanges", "正在读取变更…")
              }}
            </div>
            <div v-else-if="!changes.length" class="catalog-state compact">
              {{ text("supply.catalogNoChanges", "该任务没有字段变更") }}
            </div>
            <div v-else class="change-list">
              <div
                v-for="change in changes"
                :key="change.id"
                class="change-row"
              >
                <span
                  class="change-action"
                  :class="{ applied: change.applied }"
                  >{{ change.action }}</span
                ><span class="change-entity">{{ change.entity_type }}</span
                ><code>{{ change.external_id }}</code
                ><span>{{ changedFields(change.changed_fields) }}</span
                ><small>{{ change.message || "—" }}</small>
              </div>
            </div>
            <footer
              v-if="!changesLoading && changeTotal > 0"
              class="catalog-pager change-pager"
            >
              <span
                >{{ changePage }} / {{ changePages }} · {{ changeTotal }}
                {{ text("supply.catalogRows", "条") }}</span
              >
              <div>
                <button
                  type="button"
                  :disabled="changePage <= 1 || changesLoading"
                  @click="goChangePage(changePage - 1)"
                >
                  <ChevronLeft :size="14" /></button
                ><button
                  type="button"
                  :disabled="changePage >= changePages || changesLoading"
                  @click="goChangePage(changePage + 1)"
                >
                  <ChevronRight :size="14" />
                </button>
              </div>
            </footer>
          </div>
        </section>
      </main>

      <aside
        v-if="canManage"
        class="supplier-import-dock"
        :class="{ expanded: selectedCount > 0 && importDockExpanded }"
      >
        <div class="import-dock-summary">
          <div>
            <UploadCloud :size="17" /><b>{{
              text("supply.catalogImportTitle", "批量接入货源")
            }}</b
            ><span
              >{{ selectedCount }}
              {{ text("supply.catalogSelectedSuffix", "个商品已选择") }}</span
            >
          </div>
          <button
            type="button"
            class="dock-toggle"
            :disabled="!selectedCount"
            :aria-expanded="selectedCount > 0 && importDockExpanded"
            @click="importDockExpanded = !importDockExpanded"
          >
            {{
              !selectedCount
                ? text("supply.catalogChooseFirst", "先选择商品")
                : importDockExpanded
                  ? text("supply.catalogCollapse", "收起")
                  : text("supply.catalogConfigure", "配置")
            }}<ChevronDown
              :size="14"
              :class="{ 'dock-chevron-open': importDockExpanded }"
            />
          </button>
        </div>
        <div v-if="selectedCount" class="import-dock-form">
          <div class="import-mode-grid">
            <label :class="{ selected: importForm.category_mode === 'mirror' }"
              ><input
                v-model="importForm.category_mode"
                type="radio"
                value="mirror"
              /><FolderTree :size="15" /><span
                ><b>{{
                  text("supply.catalogMirrorCategory", "镜像上游分类")
                }}</b
                ><small>{{
                  text(
                    "supply.catalogMirrorCategoryDesc",
                    "自动创建层级、图片和描述",
                  )
                }}</small></span
              ></label
            ><label :class="{ selected: importForm.category_mode === 'target' }"
              ><input
                v-model="importForm.category_mode"
                type="radio"
                value="target"
              /><Layers3 :size="15" /><span
                ><b>{{
                  text("supply.catalogTargetCategory", "指定本地分类")
                }}</b
                ><small>{{
                  text(
                    "supply.catalogTargetCategoryDesc",
                    "所有商品进入指定分类",
                  )
                }}</small></span
              ></label
            >
          </div>
          <label v-if="importForm.category_mode === 'target'" class="dock-label"
            >{{ text("supply.catalogTargetCategory", "指定本地分类")
            }}<select v-model="importForm.target_category_id">
              <option value="">
                {{ text("supply.catalogTargetRequired", "请选择本地分类") }}
              </option>
              <option
                v-for="row in localCategoryRows"
                :key="row.item.id"
                :value="row.item.id"
                :disabled="!row.item.enabled"
              >
                {{ "　".repeat(row.depth) }}{{ row.item.name
                }}{{
                  row.item.enabled
                    ? ""
                    : text("supply.catalogDisabledSuffix", "（已停用）")
                }}
              </option>
            </select></label
          >
          <div class="dock-field-grid">
            <label class="dock-label"
              >{{ text("supply.catalogMarkupMode", "加价模式")
              }}<select v-model="importForm.price_mode">
                <option value="fixed_markup">
                  {{ text("supply.catalogMarkupPercentMode", "百分比加价") }}
                </option>
                <option value="fixed_amount">
                  {{ text("supply.catalogMarkupAmountMode", "固定金额加价") }}
                </option>
              </select></label
            >
            <label
              v-if="importForm.price_mode === 'fixed_markup'"
              class="dock-label"
              >{{ text("supply.catalogMarkup", "加价比例（%）")
              }}<input
                v-model.number="importForm.markup_percent"
                type="number"
                min="0"
                max="1000"
                step="0.01"
              /><small>{{
                text(
                  "supply.catalogMarkupHint",
                  "上游 1 USD × 汇率 × (1 + 50%)；约 ¥10.54",
                )
              }}</small></label
            >
            <label v-else class="dock-label"
              >{{ text("supply.catalogMarkupAmount", "固定加价金额")
              }}<input
                v-model="importForm.markup_amount_major"
                inputmode="decimal"
                min="0"
                max="1000000"
                :step="majorInputStep(storeCurrency)"
              /><small
                >{{
                  text(
                    "supply.catalogMarkupAmountHint",
                    "先换算上游成本，再叠加本地固定金额",
                  )
                }}
                · {{ storeCurrency }}</small
              ></label
            >
            <label class="dock-toggle-line"
              ><input v-model="importForm.auto_publish" type="checkbox" /><span
                ><b>{{ text("supply.catalogAutoPublish", "立即上架") }}</b
                ><small>{{
                  text(
                    "supply.catalogAutoPublishDesc",
                    "仅上游 active 商品自动上架",
                  )
                }}</small></span
              ></label
            >
          </div>
          <div class="dock-sync-grid">
            <label
              v-for="item in [
                ['sync_title', 'supply.protocolSyncTitle'],
                ['sync_summary', 'supply.protocolSyncSummaryLabel'],
                ['sync_description', 'supply.protocolSyncDescriptionLabel'],
                ['sync_media', 'supply.protocolSyncImagesLabel'],
                [
                  'mirror_remote_media',
                  'supply.protocolSyncImagesLocalizeLabel',
                ],
                ['sync_price', 'supply.autoSyncPrice'],
                ['sync_stock', 'supply.autoSyncStock'],
                ['sync_variants', 'supply.protocolSyncVariantsLabel'],
                ['sync_status', 'supply.protocolSyncListingLabel'],
                [
                  'sync_purchase_limits',
                  'supply.protocolSyncPurchaseLimitLabel',
                ],
              ]"
              :key="String(item[0])"
              class="dock-toggle-line"
              :class="{
                disabled:
                  item[0] === 'mirror_remote_media' && importMirrorDisabled,
              }"
              ><input
                v-model="importForm[item[0] as keyof ImportForm]"
                type="checkbox"
                :disabled="
                  item[0] === 'mirror_remote_media' && importMirrorDisabled
                "
              /><span
                ><b>{{ text(String(item[1]), policyText(String(item[1]))) }}</b
                ><small v-if="item[0] === 'mirror_remote_media'">{{
                  text(
                    "supply.catalogLocalizeHint",
                    "远端图片通过 SSRF/MIME 校验后镜像到本地",
                  )
                }}</small></span
              ></label
            >
          </div>
          <label class="dock-label"
            ><span>{{ text("supply.changeReason", "变更原因") }}</span
            ><textarea
              v-model="importForm.reason"
              maxlength="500"
              rows="2"
              :placeholder="
                text(
                  'supply.catalogReasonPlaceholder',
                  '说明本次批量接入原因（4 至 500 个字）',
                )
              "
            ></textarea
            ><small>{{ importForm.reason.trim().length }} / 500</small></label
          >
          <div v-if="importResult" class="import-result">
            <CheckCircle2 :size="17" />
            <div>
              <b>{{ text("supply.catalogImportResult", "接入任务已受理") }}</b
              ><span>{{
                text(
                  "supply.catalogResultSummary",
                  "请求 {requested} · 成功 {imported} · 已跳过 {skipped}",
                  {
                    requested: Number(importResult.requested || selectedCount),
                    imported: Number(importResult.imported || 0),
                    skipped: Number(importResult.skipped_mapped || 0),
                  },
                )
              }}</span
              ><small
                >{{ text("supply.catalogQueueStatus", "队列状态") }}：{{
                  String(importResult.sync_queue_status || "queued")
                }}</small
              >
            </div>
          </div>
          <button
            type="button"
            class="catalog-primary-button import-submit"
            :disabled="!canImport"
            @click="importSelected"
          >
            <LoaderCircle
              v-if="importing"
              :size="16"
              class="spinning"
            /><UploadCloud v-else :size="16" />{{
              importing
                ? text("supply.catalogImporting", "正在接入…")
                : text("supply.catalogConfirmImport", "确认批量接入")
            }}
          </button>
        </div>
      </aside>
    </section>
  </div>
</template>

<style scoped>
.supplier-catalog-backdrop {
  position: relative;
  inset: auto;
  z-index: auto;
  padding: 0;
  display: block;
  justify-content: initial;
  background: transparent;
  backdrop-filter: none;
}
.supplier-catalog-modal {
  width: 100%;
  height: auto;
  min-height: 62vh;
  position: relative;
  border: 1px solid var(--line);
  border-radius: 12px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  background: var(--surface);
  color: var(--text);
  box-shadow: none;
}
.supplier-catalog-header {
  min-height: 78px;
  padding: 15px 20px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  background: color-mix(in srgb, var(--surface) 96%, transparent);
}
.supplier-catalog-heading {
  min-width: 0;
}
.supplier-catalog-kicker {
  color: var(--muted);
  font-size: 8px;
  font-weight: 800;
  letter-spacing: 0.14em;
}
.supplier-catalog-heading h2 {
  margin: 5px 0 3px;
  font-size: 17px;
  letter-spacing: -0.03em;
}
.supplier-catalog-heading p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.supplier-catalog-head-actions {
  display: flex;
  gap: 6px;
}
.icon-button {
  width: 32px;
  height: 32px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--text);
}
.icon-button:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.supplier-catalog-alert {
  margin: 10px 16px 0;
  padding: 9px 10px;
  border-radius: 6px;
  display: flex;
  align-items: flex-start;
  gap: 7px;
  font-size: 9px;
  line-height: 1.45;
}
.supplier-catalog-alert span {
  min-width: 0;
  flex: 1;
}
.supplier-catalog-alert button {
  margin-left: auto;
  border: 0;
  background: transparent;
  color: inherit;
}
.supplier-catalog-alert.error {
  background: color-mix(in srgb, var(--danger) 9%, transparent);
  color: var(--danger);
}
.supplier-catalog-alert.success {
  background: color-mix(in srgb, var(--success) 9%, transparent);
  color: var(--success);
}
.supplier-catalog-tabs {
  min-height: 48px;
  flex: 0 0 auto;
  padding: 0 16px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: end;
  gap: 5px;
  overflow-x: auto;
}
.supplier-catalog-tabs button {
  height: 38px;
  padding: 0 12px;
  border: 0;
  border-bottom: 2px solid transparent;
  display: inline-flex;
  align-items: center;
  gap: 7px;
  background: transparent;
  color: var(--muted);
  font-size: 9px;
  white-space: nowrap;
}
.supplier-catalog-tabs button.active {
  border-bottom-color: var(--text);
  color: var(--text);
}
.supplier-catalog-tabs b {
  min-width: 18px;
  padding: 2px 5px;
  border-radius: 9px;
  background: var(--soft);
  font-size: 7px;
  text-align: center;
}
.supplier-catalog-content {
  min-height: 0;
  flex: 1 1 auto;
  overflow-y: auto;
  padding: 14px 16px 150px;
}
.catalog-toolbar {
  min-height: 35px;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 7px;
}
.catalog-search {
  width: min(430px, 100%);
  height: 34px;
  padding: 0 8px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: flex;
  align-items: center;
  gap: 7px;
  background: var(--surface-2);
  color: var(--muted);
}
.catalog-search input {
  min-width: 0;
  flex: 1;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--text);
  font-size: 9px;
}
.catalog-search button {
  border: 0;
  background: transparent;
  color: var(--muted);
}
.catalog-toolbar > select {
  height: 34px;
  max-width: 180px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}
.catalog-outline-button,
.catalog-quiet-button {
  min-height: 32px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
  white-space: nowrap;
}
.catalog-quiet-button {
  color: var(--muted);
}
.catalog-toolbar-note {
  min-height: 32px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  font-size: 8px;
}
.catalog-selection-bar {
  min-height: 35px;
  margin: 12px 0 9px;
  padding: 8px 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  background: var(--surface-2);
  color: var(--muted);
  font-size: 8px;
}
.catalog-selection-bar b {
  color: var(--text);
  font-size: 10px;
}
.catalog-selection-hint {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.catalog-state {
  min-height: 250px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--muted);
  font-size: 9px;
  text-align: center;
}
.catalog-state.compact {
  min-height: 100px;
}
.remote-product-tree-list {
  max-height: 360px;
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: 8px;
  overflow-x: hidden;
  overflow-y: auto;
  background: var(--surface);
}
.product-tree-list-head,
.product-tree-category-row,
.remote-product-list-row {
  --product-category-depth: 0;
  min-width: 0;
  display: grid;
  grid-template-columns:
    calc(var(--product-category-depth) * 16px) 22px 24px
    minmax(180px, 1.35fr) minmax(125px, 0.75fr) 70px 64px;
  align-items: center;
  gap: 6px;
}
.product-tree-list-head {
  min-height: 31px;
  position: sticky;
  z-index: 4;
  top: 0;
  padding: 0 9px;
  border-bottom: 1px solid var(--line);
  background: var(--surface-2);
  color: var(--muted);
}
.product-tree-list-head b {
  font-size: 7px;
  font-weight: 600;
}
.product-tree-category-row {
  min-height: 39px;
  padding: 4px 9px;
  border-bottom: 1px solid var(--line);
  background: color-mix(in srgb, var(--surface-2) 88%, var(--surface));
  color: var(--text);
}
.product-tree-category-row:hover {
  background: var(--surface-2);
}
.product-tree-category-row.selected {
  box-shadow: inset 2px 0 0 var(--success);
  background: color-mix(in srgb, var(--success) 7%, var(--surface-2));
}
.product-tree-category-row.missing {
  color: var(--warn);
}
.product-category-indent,
.remote-product-indent,
.product-category-expander.placeholder {
  pointer-events: none;
}
.product-category-expander {
  width: 22px;
  height: 28px;
  padding: 0;
  border: 0;
  border-radius: 4px;
  display: grid;
  place-items: center;
  background: transparent;
  color: var(--muted);
}
.product-category-expander:hover {
  background: var(--soft);
  color: var(--text);
}
.product-category-check {
  width: 24px;
  height: 28px;
  position: relative;
  display: grid;
  place-items: center;
}
.product-category-check input,
.remote-product-list-check input {
  width: 15px;
  height: 15px;
  margin: 0;
  accent-color: var(--success);
}
.product-category-check input:disabled,
.remote-product-list-check input:disabled {
  opacity: 0.45;
}
.product-category-check-loader {
  position: absolute;
  color: var(--text);
  pointer-events: none;
}
.product-tree-category-main {
  min-width: 0;
  padding: 0;
  border: 0;
  display: grid;
  gap: 2px;
  background: transparent;
  color: inherit;
  text-align: left;
}
.product-tree-category-main > span {
  min-width: 0;
  display: flex;
  align-items: baseline;
  gap: 6px;
}
.product-tree-category-main b {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 9px;
}
.product-tree-category-main code,
.product-tree-category-main small {
  min-width: 0;
  overflow: hidden;
  color: var(--muted);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 7px;
}
.product-tree-category-main code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.product-tree-category-path {
  min-width: 0;
  overflow: hidden;
  color: var(--muted);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 7px;
}
.product-tree-category-count {
  width: fit-content;
  padding: 3px 6px;
  border-radius: 8px;
  background: var(--soft);
  color: var(--muted);
  font-size: 7px;
  white-space: nowrap;
}
.product-tree-leaves {
  display: contents;
}
.product-tree-inline-state {
  min-height: 72px;
  padding: 12px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: var(--muted);
  font-size: 8px;
}
.product-tree-inline-state.child {
  min-height: 40px;
  padding-left: calc(58px + var(--product-category-depth) * 16px);
  justify-content: flex-start;
  background: var(--surface);
}
.remote-product-list-row {
  min-height: 67px;
  padding: 6px 9px;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
  transition: background 0.12s;
}
.remote-product-list-row:hover {
  background: var(--surface-2);
}
.remote-product-list-row.selected {
  box-shadow: inset 2px 0 0 var(--success);
  background: color-mix(in srgb, var(--success) 5%, var(--surface));
}
.remote-product-list-check {
  min-height: 34px;
  display: grid;
  place-items: center;
}
.remote-product-list-media {
  width: 38px;
  height: 38px;
  position: relative;
  border-radius: 5px;
  overflow: hidden;
  display: grid;
  place-items: center;
  background: var(--soft);
  color: var(--muted);
}
.remote-product-list-media img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.remote-product-list-media span {
  position: absolute;
  right: 2px;
  bottom: 2px;
  min-width: 13px;
  padding: 1px 3px;
  border-radius: 6px;
  background: rgba(0, 0, 0, 0.62);
  color: #fff;
  font-size: 6px;
  text-align: center;
}
.remote-product-list-main,
.remote-product-list-category,
.remote-product-list-commerce,
.remote-product-list-state {
  min-width: 0;
}
.remote-product-list-main {
  display: grid;
  gap: 3px;
}
.remote-product-list-main > div {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 5px;
}
.remote-product-list-main h3 {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  color: var(--text);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 9px;
}
.remote-product-list-main .mapped-tag {
  flex: 0 0 auto;
  padding: 2px 5px;
  border-radius: 7px;
  background: color-mix(in srgb, var(--success) 10%, transparent);
  color: var(--success);
  font-size: 6px;
  font-weight: 700;
}
.remote-product-list-main code,
.remote-product-list-category code {
  min-width: 0;
  overflow: hidden;
  color: var(--muted);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 7px;
}
.remote-product-list-main p {
  margin: 0;
  overflow: hidden;
  color: var(--muted);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 7px;
}
.remote-product-list-category,
.remote-product-list-commerce,
.remote-product-list-state {
  display: grid;
  align-content: center;
  gap: 3px;
}
.remote-product-list-category > small {
  display: none;
}
.remote-product-list-category b {
  overflow: hidden;
  color: var(--text);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 8px;
}
.remote-product-list-commerce b {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 3px;
  color: var(--text);
  font-size: 9px;
}
.remote-product-list-commerce b s {
  color: var(--muted);
  font-size: 6px;
  font-weight: 400;
}
.remote-product-list-commerce span {
  color: var(--muted);
  font-size: 7px;
}
.remote-product-list-commerce small {
  color: var(--warn);
  font-size: 6px;
}
.remote-product-list-state {
  justify-items: start;
}
.remote-product-list-state small {
  color: var(--success);
  font-size: 6px;
}
.category-product-pager {
  min-height: 36px;
  padding: 4px 9px 4px calc(58px + var(--product-category-depth) * 16px);
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  background: var(--surface);
  color: var(--muted);
  font-size: 7px;
}
.category-product-pager > span:last-child {
  display: flex;
  gap: 4px;
}
.category-product-pager button {
  width: 27px;
  height: 27px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--text);
}
.category-product-pager button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.catalog-pager {
  min-height: 38px;
  margin-top: 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  color: var(--muted);
  font-size: 8px;
}
.catalog-pager > div {
  display: flex;
  gap: 5px;
}
.catalog-pager button {
  width: 29px;
  height: 29px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--text);
}
.catalog-pager button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.remote-category-tree {
  margin-top: 11px;
  border: 1px solid var(--line);
  border-radius: 8px;
  overflow: hidden;
}
.remote-category-row {
  min-height: 62px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--line);
  display: grid;
  grid-template-columns:
    calc(var(--category-depth) * 18px) 31px minmax(0, 1fr)
    minmax(100px, 0.4fr) auto auto;
  align-items: center;
  gap: 8px;
}
.remote-category-row:last-child {
  border-bottom: 0;
}
.category-indent {
  display: block;
}
.category-thumb {
  width: 30px;
  height: 30px;
  border-radius: 6px;
  display: grid;
  place-items: center;
  overflow: hidden;
  background: var(--soft);
  color: var(--muted);
}
.category-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.category-main {
  min-width: 0;
  display: grid;
  gap: 3px;
}
.category-main b {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 9px;
}
.category-main small {
  overflow: hidden;
  color: var(--muted);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 7px;
}
.category-local {
  min-width: 0;
  overflow: hidden;
  color: var(--muted);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 8px;
}
.category-local.mapped {
  color: var(--success);
  font-weight: 600;
}
.category-error {
  color: var(--danger);
}
.status-badge {
  width: fit-content;
  padding: 3px 7px;
  border-radius: 10px;
  background: var(--soft);
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
  white-space: nowrap;
}
.status-active,
.status-succeeded {
  background: color-mix(in srgb, var(--success) 10%, transparent);
  color: var(--success);
}
.status-inactive,
.status-missing,
.status-failed,
.status-cancelled {
  background: color-mix(in srgb, var(--danger) 9%, transparent);
  color: var(--danger);
}
.status-running,
.status-queued,
.status-retrying,
.status-partial {
  background: color-mix(in srgb, var(--warn) 10%, transparent);
  color: var(--warn);
}
.catalog-section-intro {
  min-height: 58px;
  margin-bottom: 13px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  background: var(--surface-2);
}
.catalog-section-intro > div {
  display: flex;
  align-items: flex-start;
  gap: 9px;
}
.catalog-section-intro h3 {
  margin: 0 0 4px;
  font-size: 11px;
}
.catalog-section-intro p {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.5;
}
.catalog-section-intro > span {
  color: var(--muted);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 8px;
  white-space: nowrap;
}
.policy-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.policy-toggle {
  min-height: 65px;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  background: var(--surface);
}
.policy-toggle:hover,
.policy-toggle:not(.disabled):has(input:checked) {
  border-color: color-mix(in srgb, var(--text) 42%, var(--line));
  background: var(--surface-2);
}
.policy-toggle.disabled {
  opacity: 0.5;
}
.policy-toggle span {
  display: grid;
  gap: 4px;
}
.policy-toggle b {
  font-size: 9px;
}
.policy-toggle small {
  color: var(--muted);
  font-size: 7px;
  line-height: 1.45;
}
.policy-bottom-grid {
  margin-top: 13px;
  display: grid;
  grid-template-columns: minmax(220px, 0.5fr) minmax(0, 1fr);
  gap: 10px;
  align-items: end;
}
.policy-bottom-grid label,
.catalog-reason {
  display: grid;
  gap: 6px;
  color: var(--text);
  font-size: 8px;
  font-weight: 600;
}
.policy-bottom-grid select {
  height: 35px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface-2);
  color: var(--text);
  font-size: 8px;
}
.policy-info {
  min-height: 35px;
  padding: 8px 10px;
  border-radius: 6px;
  display: flex;
  align-items: flex-start;
  gap: 6px;
  background: color-mix(in srgb, var(--success) 8%, transparent);
  color: var(--success);
  font-size: 8px;
  line-height: 1.45;
}
.catalog-reason {
  margin-top: 13px;
}
.catalog-reason textarea,
.dock-label textarea {
  width: 100%;
  padding: 8px 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  outline: 0;
  resize: vertical;
  background: var(--surface-2);
  color: var(--text);
  font-size: 8px;
}
.catalog-reason small,
.dock-label small {
  color: var(--muted);
  font-size: 7px;
  font-weight: 400;
}
.catalog-form-actions {
  margin-top: 13px;
  display: flex;
  justify-content: flex-end;
}
.catalog-primary-button {
  min-height: 35px;
  padding: 0 13px;
  border: 1px solid var(--dark);
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: var(--dark);
  color: var(--dark-text);
  font-size: 8px;
  font-weight: 700;
}
.catalog-primary-button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.catalog-primary-button.compact,
.catalog-outline-button.compact {
  min-height: 29px;
  padding: 0 9px;
}
.import-job-panel {
  margin-bottom: 14px;
  border: 1px solid var(--line);
  border-radius: 9px;
  overflow: hidden;
  background: var(--surface-2);
}
.import-job-heading {
  min-height: 54px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.import-job-heading > div {
  min-width: 0;
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.import-job-heading > div > span {
  display: grid;
  gap: 3px;
}
.import-job-heading b {
  font-size: 10px;
}
.import-job-heading small {
  color: var(--muted);
  font-size: 7px;
}
.import-active-count {
  padding: 4px 7px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--warn) 10%, transparent);
  color: var(--warn);
  font-size: 7px;
  white-space: nowrap;
}
.import-job-list {
  padding: 8px;
  display: grid;
  gap: 7px;
}
.import-job-card {
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: grid;
  gap: 8px;
  background: var(--surface);
}
.import-job-topline {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 7px;
  color: var(--muted);
  font-size: 7px;
}
.import-job-topline code {
  color: var(--text);
}
.import-job-topline small {
  margin-left: auto;
}
.import-progress-line {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 34px;
  align-items: center;
  gap: 8px;
}
.import-progress-line > span {
  height: 6px;
  border-radius: 999px;
  overflow: hidden;
  background: var(--soft);
}
.import-progress-line i {
  height: 100%;
  display: block;
  border-radius: inherit;
  background: var(--success);
  transition: width 0.25s ease;
}
.import-progress-line b {
  font-size: 8px;
  text-align: right;
}
.import-job-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
}
.import-job-metrics span {
  padding: 3px 6px;
  border-radius: 4px;
  background: var(--soft);
  color: var(--muted);
  font-size: 7px;
}
.import-job-metrics .success {
  color: var(--success);
}
.import-job-error {
  margin: 0;
  display: flex;
  align-items: flex-start;
  gap: 5px;
  color: var(--danger);
  font-size: 7px;
  line-height: 1.45;
}
.import-job-footer {
  min-height: 29px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.import-job-footer small {
  color: var(--muted);
  font-size: 7px;
}
.import-retry-form {
  padding-top: 8px;
  border-top: 1px solid var(--line);
  display: grid;
  gap: 7px;
}
.import-retry-form textarea {
  width: 100%;
  padding: 8px;
  border: 1px solid var(--line);
  border-radius: 6px;
  outline: 0;
  resize: vertical;
  background: var(--surface-2);
  color: var(--text);
  font-size: 8px;
}
.import-retry-form > div {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 7px;
}
.import-retry-form small {
  margin-right: auto;
  color: var(--muted);
  font-size: 7px;
}
.import-retry-form button:not(.catalog-primary-button) {
  min-height: 29px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}
.run-list {
  margin-top: 11px;
  display: grid;
  gap: 7px;
}
.run-card {
  min-height: 70px;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: grid;
  grid-template-columns: 75px minmax(150px, 1fr) minmax(280px, 1fr) auto;
  align-items: center;
  gap: 11px;
  cursor: pointer;
}
.run-card:hover,
.run-card.selected {
  border-color: color-mix(in srgb, var(--text) 40%, var(--line));
  background: var(--surface-2);
}
.run-status {
  display: grid;
  justify-items: start;
  gap: 5px;
}
.run-status small {
  color: var(--muted);
  font-size: 7px;
}
.run-main {
  min-width: 0;
  display: grid;
  gap: 4px;
}
.run-main b {
  font-size: 9px;
}
.run-main code {
  color: var(--muted);
  font-size: 7px;
}
.run-main p {
  margin: 0;
  overflow: hidden;
  color: var(--danger);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 7px;
}
.run-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
}
.run-metrics span {
  padding: 3px 5px;
  border-radius: 4px;
  background: var(--soft);
  color: var(--muted);
  font-size: 7px;
}
.run-chevron {
  color: var(--muted);
}
.run-change-panel {
  margin-top: 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  overflow: hidden;
}
.run-change-panel > header {
  min-height: 39px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: var(--surface-2);
}
.run-change-panel > header > div {
  display: flex;
  align-items: center;
  gap: 6px;
}
.run-change-panel > header b {
  font-size: 9px;
}
.run-change-panel > header small {
  color: var(--muted);
  font-size: 7px;
}
.run-change-panel > header button {
  border: 0;
  background: transparent;
  color: var(--muted);
}
.change-list {
  max-height: 260px;
  overflow-y: auto;
}
.run-change-panel > .change-pager {
  min-height: 40px;
  margin-top: 0;
  padding: 5px 10px;
  border-top: 1px solid var(--line);
  background: var(--surface-2);
}
.change-row {
  min-height: 37px;
  padding: 7px 10px;
  border-bottom: 1px solid var(--line);
  display: grid;
  grid-template-columns:
    75px 75px minmax(100px, 0.7fr) minmax(100px, 1fr)
    minmax(100px, 1fr);
  align-items: center;
  gap: 7px;
  font-size: 7px;
}
.change-row:last-child {
  border-bottom: 0;
}
.change-row code {
  overflow: hidden;
  color: var(--muted);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 7px;
}
.change-row small {
  overflow: hidden;
  color: var(--muted);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.change-action {
  width: fit-content;
  padding: 2px 5px;
  border-radius: 5px;
  background: var(--soft);
  color: var(--muted);
}
.change-action.applied {
  background: color-mix(in srgb, var(--success) 10%, transparent);
  color: var(--success);
}
.supplier-import-dock {
  position: absolute;
  right: 32px;
  bottom: 32px;
  width: min(620px, calc(100% - 64px));
  max-height: 58px;
  border: 1px solid var(--line);
  border-radius: 9px;
  overflow: hidden;
  background: color-mix(in srgb, var(--surface) 96%, transparent);
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.15);
  backdrop-filter: blur(12px);
  transition: max-height 0.2s ease;
}
.supplier-import-dock.expanded {
  max-height: min(590px, calc(100% - 90px));
  overflow-y: auto;
}
.import-dock-summary {
  min-height: 57px;
  padding: 9px 11px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.import-dock-summary > div {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 7px;
}
.import-dock-summary b {
  font-size: 9px;
}
.import-dock-summary span {
  color: var(--muted);
  font-size: 8px;
}
.dock-toggle {
  min-height: 30px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}
.dock-toggle:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.dock-toggle svg {
  transition: transform 0.18s ease;
}
.dock-toggle svg.dock-chevron-open {
  transform: rotate(180deg);
}
.import-dock-form {
  padding: 0 11px 11px;
  display: grid;
  gap: 10px;
}
.import-mode-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 7px;
}
.import-mode-grid label {
  min-height: 55px;
  padding: 8px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: flex;
  align-items: flex-start;
  gap: 6px;
  background: var(--surface-2);
}
.import-mode-grid label.selected {
  border-color: var(--text);
}
.import-mode-grid span {
  display: grid;
  gap: 3px;
}
.import-mode-grid b {
  font-size: 8px;
}
.import-mode-grid small {
  color: var(--muted);
  font-size: 7px;
  line-height: 1.4;
}
.dock-field-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  align-items: start;
}
.dock-label {
  display: grid;
  gap: 5px;
  color: var(--text);
  font-size: 8px;
  font-weight: 600;
}
.dock-label input,
.dock-label select {
  width: 100%;
  height: 33px;
  padding: 0 8px;
  border: 1px solid var(--line);
  border-radius: 5px;
  outline: 0;
  background: var(--surface-2);
  color: var(--text);
  font-size: 8px;
}
.dock-toggle-line {
  min-height: 38px;
  display: flex;
  align-items: flex-start;
  gap: 7px;
}
.dock-toggle-line span {
  display: grid;
  gap: 3px;
}
.dock-toggle-line b {
  font-size: 8px;
}
.dock-toggle-line small {
  color: var(--muted);
  font-size: 7px;
  line-height: 1.4;
}
.dock-toggle-line.disabled {
  opacity: 0.5;
}
.dock-sync-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 7px;
  padding: 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
}
.import-result {
  padding: 8px 9px;
  border-radius: 6px;
  display: flex;
  align-items: flex-start;
  gap: 7px;
  background: color-mix(in srgb, var(--success) 9%, transparent);
  color: var(--success);
}
.import-result > div {
  display: grid;
  gap: 3px;
}
.import-result b {
  font-size: 8px;
}
.import-result span,
.import-result small {
  font-size: 7px;
}
.import-result small {
  color: var(--muted);
}
.import-submit {
  width: 100%;
  min-height: 37px;
}
.spinning {
  animation: supplier-catalog-spin 0.9s linear infinite;
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
@keyframes supplier-catalog-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 1050px) {
  .run-card {
    grid-template-columns: 75px minmax(130px, 1fr) minmax(180px, 1fr) auto;
  }
  .supplier-import-dock {
    right: 20px;
    bottom: 20px;
    width: min(620px, calc(100% - 40px));
  }
}
@media (max-width: 760px) {
  .supplier-catalog-backdrop {
    padding: 0;
    height: auto;
  }
  .supplier-catalog-modal {
    border: 0;
    border-radius: 0;
  }
  .supplier-catalog-header {
    padding: 13px 12px;
  }
  .supplier-catalog-head-actions .icon-button {
    width: 40px;
    height: 40px;
  }
  .supplier-catalog-content {
    padding: 10px 10px 185px;
  }
  .supplier-catalog-tabs {
    padding: 0 8px;
  }
  .supplier-catalog-tabs button {
    min-height: 40px;
    padding: 0 8px;
    font-size: 10px;
  }
  .catalog-selection-bar {
    align-items: flex-start;
    flex-direction: column;
  }
  .catalog-toolbar > select {
    flex: 1 1 120px;
    max-width: none;
  }
  .catalog-search,
  .catalog-toolbar > select,
  .catalog-outline-button,
  .catalog-quiet-button {
    min-height: 40px;
  }
  .catalog-search input,
  .catalog-toolbar > select,
  .catalog-outline-button,
  .catalog-quiet-button {
    font-size: 11px;
  }
  .remote-product-tree-list {
    max-height: 360px;
    overflow-x: hidden;
  }
  .product-tree-list-head {
    display: none;
  }
  .product-tree-category-row {
    min-height: 44px;
    grid-template-columns:
      calc(var(--product-category-depth) * 9px) 32px 32px
      minmax(0, 1fr) auto;
    gap: 2px;
    padding: 5px 6px;
  }
  .product-tree-category-path,
  .product-tree-category-row > .status-badge {
    display: none;
  }
  .product-tree-category-count {
    grid-column: 5;
    font-size: 8px;
  }
  .product-category-expander,
  .product-category-check {
    width: 32px;
    height: 34px;
  }
  .product-category-check input,
  .remote-product-list-check input {
    width: 18px;
    height: 18px;
  }
  .product-tree-category-main b {
    font-size: 11px;
  }
  .product-tree-category-main code,
  .product-tree-category-main small {
    font-size: 8px;
  }
  .remote-product-list-row {
    min-height: 92px;
    grid-template-columns:
      calc(var(--product-category-depth) * 7px) 32px 40px
      minmax(0, 1fr);
    align-items: start;
    gap: 4px;
    padding: 8px 6px;
  }
  .remote-product-list-check {
    min-height: 38px;
  }
  .remote-product-list-media {
    width: 38px;
    height: 38px;
  }
  .remote-product-list-main,
  .remote-product-list-category,
  .remote-product-list-commerce,
  .remote-product-list-state {
    grid-column: 4;
    max-width: 100%;
  }
  .remote-product-list-main h3 {
    font-size: 11px;
  }
  .remote-product-list-main p {
    display: none;
  }
  .remote-product-list-category {
    margin-top: 3px;
  }
  .remote-product-list-category > small {
    display: block;
    color: var(--muted);
    font-size: 8px;
  }
  .remote-product-list-category b,
  .remote-product-list-category code {
    overflow-wrap: anywhere;
    white-space: normal;
  }
  .remote-product-list-commerce,
  .remote-product-list-state {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 5px;
  }
  .category-product-pager {
    padding-left: calc(82px + var(--product-category-depth) * 7px);
  }
  .product-tree-inline-state.child {
    padding-left: calc(82px + var(--product-category-depth) * 7px);
  }
  .remote-category-row {
    grid-template-columns:
      calc(var(--category-depth) * 12px) 28px minmax(0, 1fr)
      auto;
  }
  .remote-category-row .category-local,
  .remote-category-row .status-badge,
  .remote-category-row .category-error {
    grid-column: 3 / -1;
    justify-self: start;
  }
  .policy-grid,
  .policy-bottom-grid,
  .import-mode-grid,
  .dock-field-grid,
  .dock-sync-grid {
    grid-template-columns: 1fr;
  }
  .run-card {
    grid-template-columns: 70px minmax(0, 1fr) auto;
  }
  .import-job-heading {
    align-items: flex-start;
  }
  .import-job-footer {
    align-items: flex-start;
    flex-direction: column;
  }
  .import-job-footer .catalog-outline-button {
    width: 100%;
  }
  .run-metrics {
    grid-column: 1 / -1;
  }
  .supplier-import-dock {
    right: 8px;
    bottom: max(8px, env(safe-area-inset-bottom));
    width: calc(100% - 16px);
  }
  .supplier-import-dock.expanded {
    max-height: calc(100% - 75px);
  }
  .dock-toggle,
  .catalog-pager button,
  .catalog-primary-button,
  .dock-label input,
  .dock-label select {
    min-height: 40px;
  }
  .import-dock-summary {
    min-height: 52px;
  }
  .supplier-catalog-heading h2 {
    font-size: 15px;
  }
  .supplier-catalog-heading p {
    max-width: 220px;
  }
  .change-row {
    grid-template-columns: 65px 65px minmax(80px, 1fr);
  }
  .change-row span:nth-of-type(3),
  .change-row small {
    grid-column: 1 / -1;
  }
  .change-row span:nth-of-type(3) {
    color: var(--muted);
  }
  .remote-product-list-main code,
  .remote-product-list-category,
  .remote-product-list-commerce,
  .category-main small,
  .category-main code,
  .category-local,
  .catalog-selection-bar,
  .catalog-pager {
    font-size: 9px;
  }
  .policy-toggle b,
  .dock-toggle-line b,
  .import-mode-grid b,
  .dock-label,
  .run-main b {
    font-size: 10px;
  }
  .policy-toggle small,
  .dock-toggle-line small,
  .import-mode-grid small,
  .dock-label small {
    font-size: 9px;
  }
  .dock-label input,
  .dock-label select,
  .dock-label textarea {
    font-size: 12px;
  }
}
@media (max-width: 420px) {
  .supplier-catalog-kicker,
  .import-dock-summary > div > span {
    display: none;
  }
  .supplier-catalog-heading p {
    max-width: 190px;
  }
}
</style>
