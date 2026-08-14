<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  AlertCircle,
  ArrowRightLeft,
  Check,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Edit3,
  FolderTree,
  Image,
  Link2,
  LoaderCircle,
  Plus,
  Power,
  RefreshCw,
  Save,
  Search,
  ServerCog,
  Trash2,
  Upload,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import {
  formatMoney,
  loadCurrencyDirectory,
  majorInputStep,
  majorToMinor,
  minorToMajor,
  minorToSafeNumber,
  storeCurrency,
} from "../utils/money";
import { validSupplierExternalID } from "../utils/supplierIdentity";

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
  status: string;
  protocol?: string;
}

interface LocalCategory {
  id: string;
  parent_id?: string | null;
  name: string;
  image_url?: string;
  enabled: boolean;
  sort?: number;
}

interface RemoteCategory {
  id: string;
  external_id: string;
  external_parent_id?: string;
  name: string;
  image_url?: string;
  status: string;
  sort?: number;
}

interface CategoryBinding {
  id: string;
  supplier_id: string;
  supplier_name: string;
  supplier_code: string;
  supplier_status: string;
  category_id?: string | null;
  category_name: string;
  category_image_url: string;
  external_category_id: string;
  external_category_name: string;
  remote_category_image_url: string;
  default_cover_url: string;
  sync_category_name: boolean;
  sync_title: boolean;
  sync_description: boolean;
  sync_image: boolean;
  mirror_remote_image: boolean;
  sync_parent: boolean;
  sync_price: boolean;
  sync_stock: boolean;
  auto_publish: boolean;
  price_mode: "fixed_markup" | "fixed_amount";
  markup_basis_point: number;
  markup_amount: number;
  markup_currency: string;
  sort: number;
  enabled: boolean;
  last_synced_at?: string | null;
  last_error: string;
  created_at: string;
  updated_at: string;
}

interface BindingSummary {
  total: number;
  enabled: number;
  disabled: number;
  suppliers: number;
}

interface BindingForm {
  supplierID: string;
  categoryID: string;
  externalCategoryID: string;
  externalCategoryName: string;
  defaultCoverURL: string;
  syncCategoryName: boolean;
  syncTitle: boolean;
  syncDescription: boolean;
  syncImage: boolean;
  mirrorRemoteImage: boolean;
  syncParent: boolean;
  syncPrice: boolean;
  syncStock: boolean;
  autoPublish: boolean;
  priceMode: "fixed_markup" | "fixed_amount";
  markupPercent: number;
  markupAmountMajor: string;
  sort: number;
  enabled: boolean;
  reason: string;
}

interface CategoryOption extends LocalCategory {
  label: string;
}

const { t, te, locale } = useI18n();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("supplier.manage"));

const bindings = ref<CategoryBinding[]>([]);
const suppliers = ref<Supplier[]>([]);
const localCategories = ref<LocalCategory[]>([]);
const remoteCategories = ref<RemoteCategory[]>([]);
const summary = ref<BindingSummary>({
  total: 0,
  enabled: 0,
  disabled: 0,
  suppliers: 0,
});
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const searchInput = ref("");
const search = ref("");
const supplierFilter = ref("");
const categoryFilter = ref("");
const statusFilter = ref("");
const loading = ref(false);
const optionsLoading = ref(false);
const remoteLoading = ref(false);
const saving = ref(false);
const coverUploading = ref(false);
const loadError = ref("");
const notice = ref("");
const selectedIDs = ref<string[]>([]);
const editorOpen = ref(false);
const editing = ref<CategoryBinding | null>(null);
const deleting = ref<CategoryBinding | null>(null);
const batchAction = ref<"enable" | "disable" | "delete" | null>(null);
const batchReason = ref("");
const deleteReason = ref("");
const modalError = ref("");
const remoteQuery = ref("");
const brokenPreview = ref(false);
const form = ref<BindingForm>(emptyForm());
let listRequest = 0;
let remoteRequest = 0;
let noticeTimer: ReturnType<typeof setTimeout> | undefined;

const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const allSelected = computed(
  () =>
    bindings.value.length > 0 &&
    bindings.value.every((item) => selectedIDs.value.includes(item.id)),
);
const hasModal = computed(
  () =>
    editorOpen.value || Boolean(deleting.value) || Boolean(batchAction.value),
);
const selectedRemoteCategory = computed(() =>
  remoteCategories.value.find(
    (item) => item.external_id === form.value.externalCategoryID,
  ),
);
const selectedLocalCategory = computed(() =>
  localCategories.value.find((item) => item.id === form.value.categoryID),
);
const previewURL = computed(
  () =>
    form.value.defaultCoverURL.trim() ||
    selectedRemoteCategory.value?.image_url ||
    selectedLocalCategory.value?.image_url ||
    "",
);
const categoryOptions = computed<CategoryOption[]>(() => {
  const source = localCategories.value
    .slice()
    .sort(
      (left, right) =>
        Number(left.sort || 0) - Number(right.sort || 0) ||
        left.name.localeCompare(right.name),
    );
  const children = new Map<string, LocalCategory[]>();
  for (const item of source) {
    const key = item.parent_id || "__root__";
    const rows = children.get(key) || [];
    rows.push(item);
    children.set(key, rows);
  }
  const result: CategoryOption[] = [];
  const seen = new Set<string>();
  const visit = (item: LocalCategory, depth: number) => {
    if (seen.has(item.id)) return;
    seen.add(item.id);
    result.push({ ...item, label: `${"— ".repeat(depth)}${item.name}` });
    for (const child of children.get(item.id) || []) visit(child, depth + 1);
  };
  for (const root of children.get("__root__") || []) visit(root, 0);
  for (const item of source) visit(item, 0);
  return result;
});

function text(key: string, fallback: string, params?: Record<string, unknown>) {
  return te(key) ? t(key, params || {}) : fallback;
}

function emptyForm(): BindingForm {
  return {
    supplierID: "",
    categoryID: "",
    externalCategoryID: "",
    externalCategoryName: "",
    defaultCoverURL: "",
    syncCategoryName: false,
    syncTitle: true,
    syncDescription: true,
    syncImage: true,
    mirrorRemoteImage: true,
    syncParent: false,
    syncPrice: true,
    syncStock: true,
    autoPublish: false,
    priceMode: "fixed_markup",
    markupPercent: 10,
    markupAmountMajor: "0.00",
    sort: 0,
    enabled: true,
    reason: "",
  };
}

function pagePayload<T>(value: unknown): PagePayload<T> {
  const wrapper = value as { data?: unknown } | null;
  const payload = (
    wrapper && Object.prototype.hasOwnProperty.call(wrapper, "data")
      ? wrapper.data
      : value
  ) as Partial<PagePayload<T>> | null;
  return {
    items: Array.isArray(payload?.items) ? payload.items : [],
    total: Number(payload?.total || 0),
    page: Number(payload?.page || 1),
    page_size: Number(payload?.page_size || pageSize.value),
  };
}

function apiMessage(error: unknown, fallback: string) {
  const candidate = error as {
    response?: { data?: { message?: string } };
    message?: string;
  };
  const message = candidate.response?.data?.message || candidate.message;
  return message && !message.startsWith("error.") ? message : fallback;
}

function reasonLength(value: string) {
  return Array.from(value.trim()).length;
}

function showNotice(value: string) {
  notice.value = value;
  if (noticeTimer) clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => {
    notice.value = "";
  }, 3600);
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

function bindingCover(item: CategoryBinding) {
  return (
    item.default_cover_url ||
    item.remote_category_image_url ||
    item.category_image_url ||
    ""
  );
}

function priceRule(item: CategoryBinding) {
  if (item.price_mode === "fixed_amount") {
    return text("supply.upstreamPlusAmount", "上游价 + {amount}", {
      amount: formatMoney(
        item.markup_amount,
        item.markup_currency || storeCurrency.value,
        locale.value,
      ),
    });
  }
  return text("supply.upstreamPlusMarkup", "上游价 + {percent}", {
    percent: `${(Number(item.markup_basis_point || 0) / 100).toFixed(2)}%`,
  });
}

function statusLabel(enabled: boolean) {
  return enabled
    ? text("supply.statusActive", "已启用")
    : text("supply.statusDisabled", "已停用");
}

function currentParams() {
  return {
    ...(search.value ? { q: search.value } : {}),
    ...(supplierFilter.value ? { supplier_id: supplierFilter.value } : {}),
    ...(categoryFilter.value ? { category_id: categoryFilter.value } : {}),
    ...(statusFilter.value ? { status: statusFilter.value } : {}),
  };
}

async function loadBindings() {
  const request = ++listRequest;
  loading.value = true;
  loadError.value = "";
  try {
    const { data } = await adminApi.get("/supplier-category-mappings", {
      params: {
        page: page.value,
        page_size: pageSize.value,
        ...currentParams(),
      },
    });
    if (request !== listRequest) return;
    const payload = pagePayload<CategoryBinding>(data);
    bindings.value = payload.items;
    total.value = payload.total;
    page.value = payload.page;
    selectedIDs.value = [];
  } catch (error) {
    if (request !== listRequest) return;
    bindings.value = [];
    total.value = 0;
    loadError.value = apiMessage(error, "分类绑定读取失败，请稍后重试");
  } finally {
    if (request === listRequest) loading.value = false;
  }
}

async function loadSummary() {
  try {
    const { data } = await adminApi.get("/supplier-category-mappings/summary", {
      params: currentParams(),
    });
    summary.value = {
      total: Number(data?.data?.total || 0),
      enabled: Number(data?.data?.enabled || 0),
      disabled: Number(data?.data?.disabled || 0),
      suppliers: Number(data?.data?.suppliers || 0),
    };
  } catch {
    summary.value = { total: 0, enabled: 0, disabled: 0, suppliers: 0 };
  }
}

async function loadAllSupplierOptions() {
  const { data } = await adminApi.get("/suppliers", {
    params: { page: 1, page_size: 100 },
  });
  const first = pagePayload<Supplier>(data);
  const result = [...first.items];
  const pages = Math.ceil(first.total / Math.max(1, first.page_size));
  for (let nextPage = 2; nextPage <= pages; nextPage += 1) {
    const response = await adminApi.get("/suppliers", {
      params: { page: nextPage, page_size: 100 },
    });
    result.push(...pagePayload<Supplier>(response.data).items);
  }
  return [...new Map(result.map((item) => [item.id, item])).values()];
}

async function loadOptions() {
  optionsLoading.value = true;
  try {
    const [supplierItems, categoryResponse] = await Promise.all([
      loadAllSupplierOptions(),
      adminApi.get("/categories"),
    ]);
    suppliers.value = supplierItems;
    localCategories.value = Array.isArray(categoryResponse.data?.data)
      ? categoryResponse.data.data
      : [];
  } catch (error) {
    loadError.value = apiMessage(error, "店铺或本地分类读取失败");
  } finally {
    optionsLoading.value = false;
  }
}

async function loadRemoteCategories(query = remoteQuery.value) {
  const request = ++remoteRequest;
  const supplierID = form.value.supplierID;
  remoteCategories.value = [];
  if (!supplierID) {
    remoteLoading.value = false;
    return;
  }
  remoteLoading.value = true;
  modalError.value = "";
  try {
    const { data } = await adminApi.get(
      `/suppliers/${encodeURIComponent(supplierID)}/remote-categories`,
      {
        params: {
          page: 1,
          page_size: 100,
          ...(query.trim() ? { q: query.trim() } : {}),
        },
      },
    );
    if (request !== remoteRequest || supplierID !== form.value.supplierID)
      return;
    remoteCategories.value = pagePayload<RemoteCategory>(data).items;
  } catch (error) {
    if (request !== remoteRequest) return;
    modalError.value = apiMessage(error, "远端分类读取失败，请先同步店铺目录");
  } finally {
    if (request === remoteRequest) remoteLoading.value = false;
  }
}

async function refreshPage() {
  await Promise.all([loadBindings(), loadSummary()]);
}

function applySearch() {
  search.value = searchInput.value.trim();
  page.value = 1;
  void refreshPage();
}

function applyFilter() {
  page.value = 1;
  void refreshPage();
}

function changePage(next: number) {
  if (next < 1 || next > totalPages.value || next === page.value) return;
  page.value = next;
  void loadBindings();
}

function changePageSize() {
  page.value = 1;
  void loadBindings();
}

function toggleAll() {
  if (!canManage.value) return;
  selectedIDs.value = allSelected.value
    ? []
    : bindings.value.map((item) => item.id);
}

function toggleOne(id: string) {
  if (!canManage.value) return;
  const next = new Set(selectedIDs.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  selectedIDs.value = [...next];
}

function openCreate() {
  if (!canManage.value) return;
  editing.value = null;
  form.value = emptyForm();
  if (supplierFilter.value) form.value.supplierID = supplierFilter.value;
  remoteCategories.value = [];
  remoteQuery.value = "";
  modalError.value = "";
  brokenPreview.value = false;
  editorOpen.value = true;
  if (form.value.supplierID) void loadRemoteCategories("");
}

function openEdit(item: CategoryBinding) {
  if (!canManage.value) return;
  if (!suppliers.value.some((supplier) => supplier.id === item.supplier_id)) {
    suppliers.value.push({
      id: item.supplier_id,
      name: item.supplier_name,
      code: item.supplier_code,
      status: item.supplier_status,
    });
  }
  editing.value = item;
  form.value = {
    supplierID: item.supplier_id,
    categoryID: item.category_id || "",
    externalCategoryID: item.external_category_id,
    externalCategoryName: item.external_category_name,
    defaultCoverURL: item.default_cover_url,
    syncCategoryName: item.sync_category_name,
    syncTitle: item.sync_title,
    syncDescription: item.sync_description,
    syncImage: item.sync_image,
    mirrorRemoteImage: item.mirror_remote_image,
    syncParent: item.sync_parent,
    syncPrice: item.sync_price,
    syncStock: item.sync_stock,
    autoPublish: item.auto_publish,
    priceMode: item.price_mode,
    markupPercent: Number(item.markup_basis_point || 0) / 100,
    markupAmountMajor: minorToMajor(
      item.markup_amount || 0,
      item.markup_currency || storeCurrency.value,
    ),
    sort: Number(item.sort || 0),
    enabled: item.enabled,
    reason: "",
  };
  remoteCategories.value = [];
  remoteQuery.value = "";
  modalError.value = "";
  brokenPreview.value = false;
  editorOpen.value = true;
  void loadRemoteCategories("");
}

function closeEditor() {
  if (saving.value || coverUploading.value) return;
  editorOpen.value = false;
  editing.value = null;
  modalError.value = "";
}

function chooseRemoteCategory() {
  const item = selectedRemoteCategory.value;
  if (!item) return;
  form.value.externalCategoryName = item.name;
  brokenPreview.value = false;
}

async function uploadDefaultCover(event: Event) {
  if (!canManage.value) return;
  const input = event.currentTarget as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  if (!file.type.startsWith("image/")) {
    modalError.value = "请选择图片文件";
    return;
  }
  if (
    reasonLength(form.value.reason) < 4 ||
    reasonLength(form.value.reason) > 500
  ) {
    modalError.value = "上传封面前，请先填写 4 至 500 个字符的审计原因";
    return;
  }
  coverUploading.value = true;
  modalError.value = "";
  try {
    const body = new FormData();
    body.append("file", file, file.name);
    body.append(
      "alt_text",
      form.value.externalCategoryName.trim() || "供应分类默认封面",
    );
    const { data } = await adminApi.post(
      "/supplier-category-mappings/media/upload",
      body,
      { headers: { "X-Change-Reason": form.value.reason.trim() } },
    );
    const url = String(data?.data?.url || "").trim();
    if (!url) throw new Error("媒体服务未返回可用地址");
    form.value.defaultCoverURL = url;
    brokenPreview.value = false;
  } catch (error) {
    modalError.value = apiMessage(error, "默认封面上传失败");
  } finally {
    coverUploading.value = false;
  }
}

function validateEditor() {
  const value = form.value;
  if (!value.supplierID) return "请选择共享店铺";
  if (!value.categoryID) return "请选择要绑定的本地分类";
  if (!validSupplierExternalID(value.externalCategoryID))
    return "请选择远端分类，或填写有效的远端分类 ID";
  if (value.defaultCoverURL.trim()) {
    try {
      const parsed = new URL(value.defaultCoverURL.trim());
      if (
        !/^https?:$/.test(parsed.protocol) ||
        parsed.username ||
        parsed.password ||
        parsed.hash
      )
        throw new Error("invalid cover URL");
    } catch {
      return "默认封面必须是有效的 HTTP(S) 地址";
    }
  }
  if (
    !Number.isInteger(Number(value.sort)) ||
    value.sort < 0 ||
    value.sort > 1_000_000
  )
    return "排序值必须是 0 至 1,000,000 的整数";
  if (value.priceMode === "fixed_markup") {
    if (
      !Number.isFinite(value.markupPercent) ||
      value.markupPercent < 0 ||
      value.markupPercent > 1000
    )
      return "加价比例需在 0% 至 1000% 之间";
  } else {
    try {
      const amount = BigInt(
        majorToMinor(value.markupAmountMajor, storeCurrency.value),
      );
      const maximum = BigInt(majorToMinor("1000000", storeCurrency.value));
      if (amount < 0n || amount > maximum) throw new Error("out of range");
    } catch {
      return "固定加价金额必须在 0 至 1,000,000 之间";
    }
  }
  if (reasonLength(value.reason) < 4 || reasonLength(value.reason) > 500)
    return "请填写 4 至 500 个字符的变更原因";
  return "";
}

function editorPayload() {
  const value = form.value;
  return {
    supplier_id: value.supplierID,
    category_id: value.categoryID,
    external_category_id: value.externalCategoryID.trim(),
    external_category_name: value.externalCategoryName.trim(),
    default_cover_url: value.defaultCoverURL.trim(),
    sync_category_name: value.syncCategoryName,
    sync_title: value.syncTitle,
    sync_description: value.syncDescription,
    sync_image: value.syncImage,
    mirror_remote_image: value.syncImage && value.mirrorRemoteImage,
    sync_parent: value.syncParent,
    sync_price: value.syncPrice,
    sync_stock: value.syncStock,
    auto_publish: value.autoPublish,
    price_mode: value.priceMode,
    markup_basis_point:
      value.priceMode === "fixed_markup"
        ? Math.round(Number(value.markupPercent) * 100)
        : 0,
    markup_amount:
      value.priceMode === "fixed_amount"
        ? minorToSafeNumber(
            majorToMinor(value.markupAmountMajor, storeCurrency.value),
          )
        : 0,
    markup_currency: storeCurrency.value,
    sort: Number(value.sort),
    enabled: value.enabled,
  };
}

async function saveEditor() {
  if (!canManage.value) return;
  if (coverUploading.value) return;
  const validation = validateEditor();
  if (validation) {
    modalError.value = validation;
    return;
  }
  saving.value = true;
  modalError.value = "";
  const wasEditing = Boolean(editing.value);
  try {
    const headers = { "X-Change-Reason": form.value.reason.trim() };
    if (editing.value) {
      await adminApi.put(
        `/supplier-category-mappings/${encodeURIComponent(editing.value.id)}`,
        editorPayload(),
        { headers },
      );
    } else {
      await adminApi.post("/supplier-category-mappings", editorPayload(), {
        headers,
      });
    }
    editorOpen.value = false;
    editing.value = null;
    showNotice(wasEditing ? "分类绑定已更新" : "分类绑定已创建");
    await refreshPage();
  } catch (error) {
    modalError.value = apiMessage(
      error,
      editing.value
        ? "分类绑定更新失败，请检查是否存在重复绑定"
        : "分类绑定创建失败，请检查是否存在重复绑定",
    );
  } finally {
    saving.value = false;
  }
}

function openDelete(item: CategoryBinding) {
  if (!canManage.value) return;
  deleting.value = item;
  deleteReason.value = "";
  modalError.value = "";
}

async function confirmDelete() {
  if (!canManage.value) return;
  if (!deleting.value) return;
  if (
    reasonLength(deleteReason.value) < 4 ||
    reasonLength(deleteReason.value) > 500
  ) {
    modalError.value = "请填写 4 至 500 个字符的删除原因";
    return;
  }
  saving.value = true;
  modalError.value = "";
  try {
    await adminApi.delete(
      `/supplier-category-mappings/${encodeURIComponent(deleting.value.id)}`,
      { headers: { "X-Change-Reason": deleteReason.value.trim() } },
    );
    deleting.value = null;
    showNotice("分类绑定已删除，不会再参与目录同步");
    if (bindings.value.length === 1 && page.value > 1) page.value -= 1;
    await refreshPage();
  } catch (error) {
    modalError.value = apiMessage(error, "分类绑定删除失败");
  } finally {
    saving.value = false;
  }
}

function openBatch(
  action: "enable" | "disable" | "delete",
  ids = selectedIDs.value,
) {
  if (!canManage.value) return;
  if (!ids.length) return;
  selectedIDs.value = [...ids];
  batchAction.value = action;
  batchReason.value = "";
  modalError.value = "";
}

async function confirmBatch() {
  if (!canManage.value) return;
  if (!batchAction.value || !selectedIDs.value.length) return;
  if (
    reasonLength(batchReason.value) < 4 ||
    reasonLength(batchReason.value) > 500
  ) {
    modalError.value =
      batchAction.value === "delete"
        ? "请填写 4 至 500 个字符的批量删除原因"
        : "请填写 4 至 500 个字符的批量变更原因";
    return;
  }
  saving.value = true;
  modalError.value = "";
  try {
    const action = batchAction.value;
    const enabled = action === "enable";
    const deletingEntirePage =
      action === "delete" && selectedIDs.value.length >= bindings.value.length;
    if (action === "delete") {
      await adminApi.delete("/supplier-category-mappings/batch", {
        data: { ids: selectedIDs.value },
        headers: { "X-Change-Reason": batchReason.value.trim() },
      });
    } else {
      await adminApi.patch(
        "/supplier-category-mappings/batch-status",
        { ids: selectedIDs.value, enabled },
        { headers: { "X-Change-Reason": batchReason.value.trim() } },
      );
    }
    batchAction.value = null;
    selectedIDs.value = [];
    showNotice(
      action === "delete"
        ? "所选分类绑定已删除，不会再参与目录同步"
        : enabled
          ? "所选分类绑定已启用"
          : "所选分类绑定已停用",
    );
    if (deletingEntirePage && page.value > 1) page.value -= 1;
    await refreshPage();
  } catch (error) {
    modalError.value = apiMessage(
      error,
      batchAction.value === "delete" ? "批量删除失败" : "批量状态更新失败",
    );
  } finally {
    saving.value = false;
  }
}

function closeConfirm() {
  if (saving.value) return;
  deleting.value = null;
  batchAction.value = null;
  modalError.value = "";
}

function handleEscape(event: KeyboardEvent) {
  if (event.key !== "Escape" || saving.value || coverUploading.value) return;
  if (editorOpen.value) closeEditor();
  else if (deleting.value || batchAction.value) closeConfirm();
}

watch(hasModal, (value) => {
  document.body.style.overflow = value ? "hidden" : "";
});

watch(
  () => form.value.supplierID,
  (value, previous) => {
    if (!editorOpen.value || value === previous) return;
    if (
      editing.value &&
      value === editing.value.supplier_id &&
      form.value.externalCategoryID === editing.value.external_category_id
    ) {
      return;
    }
    form.value.externalCategoryID = "";
    form.value.externalCategoryName = "";
    remoteQuery.value = "";
    brokenPreview.value = false;
    void loadRemoteCategories("");
  },
);

watch(
  () => form.value.defaultCoverURL,
  () => {
    brokenPreview.value = false;
  },
);

watch(
  () => form.value.syncImage,
  (enabled) => {
    if (!enabled) form.value.mirrorRemoteImage = false;
  },
);

onMounted(async () => {
  window.addEventListener("keydown", handleEscape);
  await Promise.allSettled([
    loadCurrencyDirectory(),
    loadOptions(),
    refreshPage(),
  ]);
});

onBeforeUnmount(() => {
  listRequest += 1;
  remoteRequest += 1;
  window.removeEventListener("keydown", handleEscape);
  document.body.style.overflow = "";
  if (noticeTimer) clearTimeout(noticeTimer);
});
</script>

<template>
  <section class="binding-shell">
    <div class="metric-grid">
      <article class="metric-card panel">
        <span>分类绑定总数</span><strong>{{ summary.total }}</strong>
        <small><Link2 :size="13" />本地与远端分类关系</small>
      </article>
      <article class="metric-card panel success">
        <span>已启用</span><strong>{{ summary.enabled }}</strong>
        <small><Check :size="13" />参与自动同步</small>
      </article>
      <article class="metric-card panel muted">
        <span>已停用</span><strong>{{ summary.disabled }}</strong>
        <small><Power :size="13" />保留配置但不执行</small>
      </article>
      <article class="metric-card panel">
        <span>已接入店铺</span><strong>{{ summary.suppliers }}</strong>
        <small><ServerCog :size="13" />覆盖当前筛选结果</small>
      </article>
    </div>

    <div class="binding-panel panel">
      <header class="binding-toolbar">
        <form class="search-box" @submit.prevent="applySearch">
          <Search :size="15" />
          <input
            v-model="searchInput"
            type="search"
            placeholder="搜索店铺、本地分类或远端分类 ID…"
          />
        </form>
        <div class="filters">
          <select
            v-model="supplierFilter"
            aria-label="按共享店铺筛选"
            @change="applyFilter"
          >
            <option value="">全部共享店铺</option>
            <option v-for="item in suppliers" :key="item.id" :value="item.id">
              {{ item.name }} · {{ item.code }}
            </option>
          </select>
          <select
            v-model="categoryFilter"
            aria-label="按本地分类筛选"
            @change="applyFilter"
          >
            <option value="">全部本地分类</option>
            <option
              v-for="item in categoryOptions"
              :key="item.id"
              :value="item.id"
            >
              {{ item.label }}{{ item.enabled ? "" : "（已停用）" }}
            </option>
          </select>
          <select
            v-model="statusFilter"
            aria-label="按状态筛选"
            @change="applyFilter"
          >
            <option value="">全部状态</option>
            <option value="enabled">已启用</option>
            <option value="disabled">已停用</option>
          </select>
          <button
            type="button"
            class="icon-button"
            title="刷新"
            :disabled="loading"
            @click="refreshPage"
          >
            <RefreshCw :size="15" :class="{ spinning: loading }" />
          </button>
          <button
            v-if="canManage"
            type="button"
            class="primary-action"
            :disabled="optionsLoading"
            @click="openCreate"
          >
            <Plus :size="15" />新建绑定
          </button>
        </div>
      </header>

      <div v-if="notice" class="message success-message">
        <Check :size="15" /><span>{{ notice }}</span
        ><button type="button" @click="notice = ''"><X :size="13" /></button>
      </div>
      <div v-if="loadError" class="message error-message">
        <AlertCircle :size="15" /><span>{{ loadError }}</span
        ><button type="button" @click="loadError = ''"><X :size="13" /></button>
      </div>

      <div v-if="canManage && selectedIDs.length" class="batch-toolbar">
        <span
          >已选择 <b>{{ selectedIDs.length }}</b> 条绑定</span
        >
        <div>
          <button type="button" @click="openBatch('enable')">
            <Check :size="14" />批量启用
          </button>
          <button type="button" @click="openBatch('disable')">
            <Power :size="14" />批量停用
          </button>
          <button type="button" class="danger" @click="openBatch('delete')">
            <Trash2 :size="14" />批量删除
          </button>
          <button type="button" class="quiet" @click="selectedIDs = []">
            <X :size="14" />取消选择
          </button>
        </div>
      </div>

      <div class="table-wrap">
        <table class="binding-table">
          <thead>
            <tr>
              <th class="selection-cell">
                <input
                  type="checkbox"
                  :checked="allSelected"
                  :disabled="!canManage"
                  aria-label="选择当前页全部绑定"
                  @change="toggleAll"
                />
              </th>
              <th>分类关系</th>
              <th>共享店铺</th>
              <th>同步规则</th>
              <th>定价规则</th>
              <th>状态 / 排序</th>
              <th>最近更新</th>
              <th class="actions-cell">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading && !bindings.length">
              <td colspan="8" class="empty-row">
                <LoaderCircle :size="18" class="spinning" />正在读取分类绑定…
              </td>
            </tr>
            <tr v-else-if="!bindings.length">
              <td colspan="8" class="empty-row">
                <FolderTree :size="24" />
                <b>暂无分类绑定</b>
                <span
                  >建立本地分类与远端分类的明确关系后，自动同步会更可控。</span
                >
                <button v-if="canManage" type="button" @click="openCreate">
                  <Plus :size="14" />新建第一条绑定
                </button>
              </td>
            </tr>
            <tr
              v-for="item in bindings"
              v-else
              :key="item.id"
              :class="{
                selected: selectedIDs.includes(item.id),
                disabled: !item.enabled,
              }"
            >
              <td class="selection-cell" data-label="选择">
                <input
                  type="checkbox"
                  :checked="selectedIDs.includes(item.id)"
                  :disabled="!canManage"
                  :aria-label="`选择 ${item.external_category_name}`"
                  @change="toggleOne(item.id)"
                />
              </td>
              <td data-label="分类关系">
                <div class="category-pair">
                  <div class="cover-box">
                    <img
                      v-if="bindingCover(item)"
                      :src="bindingCover(item)"
                      alt=""
                      loading="lazy"
                      @error="
                        (
                          $event.currentTarget as HTMLImageElement
                        ).style.display = 'none'
                      "
                    />
                    <Image v-else :size="16" />
                  </div>
                  <div class="pair-copy">
                    <b>{{
                      item.external_category_name || item.external_category_id
                    }}</b>
                    <code>{{ item.external_category_id }}</code>
                    <span
                      ><ArrowRightLeft :size="11" />{{
                        item.category_name || "本地分类已删除"
                      }}</span
                    >
                  </div>
                </div>
                <p
                  v-if="item.last_error"
                  class="row-error"
                  :title="item.last_error"
                >
                  <AlertCircle :size="11" />{{ item.last_error }}
                </p>
              </td>
              <td data-label="共享店铺">
                <div class="supplier-cell">
                  <b>{{ item.supplier_name }}</b
                  ><code>{{ item.supplier_code }}</code
                  ><span :class="['mini-status', item.supplier_status]">{{
                    item.supplier_status === "active" ? "连接启用" : "连接停用"
                  }}</span>
                </div>
              </td>
              <td data-label="同步规则">
                <div class="sync-flags">
                  <span :class="{ on: item.sync_category_name }">分类名</span>
                  <span :class="{ on: item.sync_title }">商品标题</span>
                  <span :class="{ on: item.sync_description }">信息</span>
                  <span :class="{ on: item.sync_image }">图片</span>
                  <span
                    :class="{ on: item.sync_image && item.mirror_remote_image }"
                    >本地化</span
                  >
                  <span :class="{ on: item.sync_parent }">层级</span>
                  <span :class="{ on: item.sync_price }">价格</span>
                  <span :class="{ on: item.sync_stock }">库存</span>
                  <span :class="{ on: item.auto_publish }">上架</span>
                </div>
              </td>
              <td data-label="定价规则">
                <div class="price-cell">
                  <b>{{ priceRule(item) }}</b
                  ><span>{{ item.markup_currency || storeCurrency }}</span>
                </div>
              </td>
              <td data-label="状态 / 排序">
                <button
                  type="button"
                  :class="[
                    'status-pill',
                    item.enabled ? 'enabled' : 'disabled',
                  ]"
                  @click="
                    openBatch(item.enabled ? 'disable' : 'enable', [item.id])
                  "
                >
                  <span></span>{{ statusLabel(item.enabled) }}
                </button>
                <small class="sort-value">排序 {{ item.sort }}</small>
              </td>
              <td data-label="最近更新">
                <div class="time-cell">
                  <span>{{ formatTime(item.updated_at) }}</span
                  ><small
                    ><Clock3 :size="11" />同步
                    {{ formatTime(item.last_synced_at) }}</small
                  >
                </div>
              </td>
              <td class="actions-cell" data-label="操作">
                <div v-if="canManage" class="row-actions">
                  <button
                    type="button"
                    title="编辑分类绑定"
                    @click="openEdit(item)"
                  >
                    <Edit3 :size="14" />
                  </button>
                  <button
                    type="button"
                    class="danger"
                    title="删除分类绑定"
                    @click="openDelete(item)"
                  >
                    <Trash2 :size="14" />
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <footer class="pager">
        <div>
          <span>共 {{ total }} 条</span
          ><select
            v-model="pageSize"
            aria-label="每页数量"
            @change="changePageSize"
          >
            <option :value="10">10 / 页</option>
            <option :value="20">20 / 页</option>
            <option :value="50">50 / 页</option>
          </select>
        </div>
        <div>
          <button
            type="button"
            :disabled="page <= 1 || loading"
            @click="changePage(page - 1)"
          >
            <ChevronLeft :size="14" />{{ t("common.prev") }}
          </button>
          <span>第 {{ page }} / {{ totalPages }} 页</span>
          <button
            type="button"
            :disabled="page >= totalPages || loading"
            @click="changePage(page + 1)"
          >
            {{ t("common.next") }}<ChevronRight :size="14" />
          </button>
        </div>
      </footer>
    </div>

    <div
      v-if="editorOpen && canManage"
      class="modal-backdrop"
      role="presentation"
      @mousedown.self="closeEditor"
    >
      <section
        class="editor-modal"
        role="dialog"
        aria-modal="true"
        :aria-label="editing ? '编辑分类绑定' : '新建分类绑定'"
      >
        <header>
          <div>
            <span>SUPPLIER CATEGORY BINDING</span>
            <h2>{{ editing ? "编辑分类绑定" : "新建分类绑定" }}</h2>
            <p>
              明确指定远端分类进入哪个本地分类，并为其设置独立同步及定价策略。
            </p>
          </div>
          <button
            type="button"
            class="icon-button"
            aria-label="关闭"
            @click="closeEditor"
          >
            <X :size="18" />
          </button>
        </header>
        <form @submit.prevent="saveEditor">
          <div class="form-content">
            <section class="form-section">
              <div class="section-title">
                <span>01</span>
                <div>
                  <h3>分类关系</h3>
                  <p>同一共享店铺的同一个远端分类只能绑定一次。</p>
                </div>
              </div>
              <div class="form-grid">
                <label
                  ><span>共享店铺 <b>*</b></span
                  ><select v-model="form.supplierID" required>
                    <option value="">请选择共享店铺</option>
                    <option
                      v-for="item in suppliers"
                      :key="item.id"
                      :value="item.id"
                    >
                      {{ item.name }} · {{ item.code }}
                    </option>
                  </select></label
                >
                <label
                  ><span>本地分类 <b>*</b></span
                  ><select v-model="form.categoryID" required>
                    <option value="">请选择本地目标分类</option>
                    <option
                      v-for="item in categoryOptions"
                      :key="item.id"
                      :value="item.id"
                    >
                      {{ item.label }}{{ item.enabled ? "" : "（已停用）" }}
                    </option>
                  </select></label
                >
                <div class="remote-picker full">
                  <label
                    ><span>远端分类 <b>*</b></span
                    ><select
                      v-model="form.externalCategoryID"
                      :disabled="!form.supplierID || remoteLoading"
                      @change="chooseRemoteCategory"
                    >
                      <option value="">
                        {{
                          remoteLoading ? "正在读取远端分类…" : "请选择远端分类"
                        }}
                      </option>
                      <option
                        v-for="item in remoteCategories"
                        :key="item.id || item.external_id"
                        :value="item.external_id"
                      >
                        {{ item.name }} · {{ item.external_id }}
                      </option>
                    </select></label
                  >
                  <div class="remote-search">
                    <Search :size="14" /><input
                      v-model="remoteQuery"
                      type="search"
                      placeholder="按名称或远端 ID 搜索"
                      :disabled="!form.supplierID"
                    /><button
                      type="button"
                      :disabled="!form.supplierID || remoteLoading"
                      @click="loadRemoteCategories()"
                    >
                      <RefreshCw
                        :size="14"
                        :class="{ spinning: remoteLoading }"
                      />查询
                    </button>
                  </div>
                </div>
                <label
                  ><span>远端分类 ID <b>*</b></span
                  ><input
                    v-model.trim="form.externalCategoryID"
                    type="text"
                    maxlength="180"
                    placeholder="可从上方选择，也可手工填写"
                    required
                /></label>
                <label
                  ><span>远端分类名称</span
                  ><input
                    v-model.trim="form.externalCategoryName"
                    type="text"
                    maxlength="200"
                    placeholder="留空时使用目录快照名称"
                /></label>
              </div>
            </section>

            <section class="form-section cover-section">
              <div class="section-title">
                <span>02</span>
                <div>
                  <h3>默认封面</h3>
                  <p>优先于远端分类和本地分类图片，可留空继承目录图片。</p>
                </div>
              </div>
              <div class="cover-editor">
                <div class="cover-preview">
                  <img
                    v-if="previewURL && !brokenPreview"
                    :src="previewURL"
                    alt="分类封面预览"
                    @error="brokenPreview = true"
                  /><Image v-else :size="24" /><small>{{
                    previewURL && brokenPreview ? "图片无法加载" : "封面预览"
                  }}</small>
                </div>
                <div class="cover-control">
                  <label
                    ><span>默认封面 URL</span
                    ><input
                      v-model.trim="form.defaultCoverURL"
                      type="url"
                      maxlength="1000"
                      placeholder="https://cdn.example.com/category.png"
                    /><small
                      >仅允许 HTTP(S) 地址，不接受含账号密码或片段的
                      URL。</small
                    ></label
                  >
                  <label
                    :class="[
                      'cover-upload-button',
                      { disabled: saving || coverUploading },
                    ]"
                  >
                    <LoaderCircle
                      v-if="coverUploading"
                      :size="14"
                      class="spinning"
                    /><Upload v-else :size="14" />
                    {{ coverUploading ? "正在上传…" : "上传本地图片" }}
                    <input
                      type="file"
                      accept="image/*"
                      :disabled="saving || coverUploading"
                      @change="uploadDefaultCover"
                    />
                  </label>
                  <small class="cover-upload-hint"
                    >上传会复用商品媒体存储；请先填写下方审计原因。</small
                  >
                </div>
              </div>
            </section>

            <section class="form-section">
              <div class="section-title">
                <span>03</span>
                <div>
                  <h3>同步开关</h3>
                  <p>
                    商品标题、价格和库存会由分类规则持续继承；分类信息开关只维护本地分类本身。
                  </p>
                </div>
              </div>
              <div class="toggle-grid">
                <label :class="{ active: form.syncCategoryName }"
                  ><input
                    v-model="form.syncCategoryName"
                    type="checkbox"
                  /><span
                    ><b>同步分类名称</b
                    ><small>使用上游分类名称持续更新本地分类名称</small></span
                  ></label
                >
                <label :class="{ active: form.syncTitle }"
                  ><input v-model="form.syncTitle" type="checkbox" /><span
                    ><b>同步标题</b
                    ><small>持续更新继承该规则的商品标题</small></span
                  ></label
                >
                <label :class="{ active: form.syncDescription }"
                  ><input v-model="form.syncDescription" type="checkbox" /><span
                    ><b>同步摘要 / 描述</b
                    ><small>使用上游分类说明更新本地分类描述</small></span
                  ></label
                >
                <label :class="{ active: form.syncImage }"
                  ><input v-model="form.syncImage" type="checkbox" /><span
                    ><b>同步远端图片</b
                    ><small>读取上游分类图片并更新本地分类图</small></span
                  ></label
                >
                <label
                  :class="{
                    active: form.syncImage && form.mirrorRemoteImage,
                    disabled: !form.syncImage,
                  }"
                  ><input
                    v-model="form.mirrorRemoteImage"
                    type="checkbox"
                    :disabled="!form.syncImage"
                  /><span
                    ><b>本地化远端图片</b
                    ><small>将分类图下载到本地媒体存储</small></span
                  ></label
                >
                <label :class="{ active: form.syncParent }"
                  ><input v-model="form.syncParent" type="checkbox" /><span
                    ><b>同步分类层级</b
                    ><small
                      >仅在需要镜像上游分类树时启用；会校验并拒绝循环层级</small
                    ></span
                  ></label
                >
                <label :class="{ active: form.syncPrice }"
                  ><input v-model="form.syncPrice" type="checkbox" /><span
                    ><b>同步价格</b
                    ><small>按下方加价规则持续更新销售价格</small></span
                  ></label
                >
                <label :class="{ active: form.syncStock }"
                  ><input v-model="form.syncStock" type="checkbox" /><span
                    ><b>同步库存</b
                    ><small>使用上游库存快照更新本地可售状态</small></span
                  ></label
                >
                <label :class="{ active: form.autoPublish }"
                  ><input v-model="form.autoPublish" type="checkbox" /><span
                    ><b>自动上架</b
                    ><small>自动创建的新商品直接进入销售状态</small></span
                  ></label
                >
              </div>
            </section>

            <section class="form-section">
              <div class="section-title">
                <span>04</span>
                <div>
                  <h3>定价、排序与状态</h3>
                  <p>比例或固定金额均基于同步后的上游价格计算。</p>
                </div>
              </div>
              <div class="pricing-modes">
                <label :class="{ active: form.priceMode === 'fixed_markup' }"
                  ><input
                    v-model="form.priceMode"
                    type="radio"
                    value="fixed_markup"
                  /><span
                    ><b>按比例加价</b
                    ><small>适合不同价位的整类商品</small></span
                  ></label
                >
                <label :class="{ active: form.priceMode === 'fixed_amount' }"
                  ><input
                    v-model="form.priceMode"
                    type="radio"
                    value="fixed_amount"
                  /><span
                    ><b>固定金额加价</b
                    ><small>每件商品增加相同金额</small></span
                  ></label
                >
              </div>
              <div class="form-grid compact-grid">
                <label v-if="form.priceMode === 'fixed_markup'"
                  ><span>加价比例</span>
                  <div class="suffix-input">
                    <input
                      v-model.number="form.markupPercent"
                      type="number"
                      min="0"
                      max="1000"
                      step="0.01"
                    /><span>%</span>
                  </div></label
                >
                <label v-else
                  ><span>固定加价金额</span>
                  <div class="suffix-input">
                    <input
                      v-model="form.markupAmountMajor"
                      type="number"
                      min="0"
                      max="1000000"
                      :step="majorInputStep(storeCurrency)"
                    /><span>{{ storeCurrency }}</span>
                  </div></label
                >
                <label
                  ><span>排序值</span
                  ><input
                    v-model.number="form.sort"
                    type="number"
                    min="0"
                    max="1000000"
                    step="1"
                /></label>
                <label class="enabled-control"
                  ><span>绑定状态</span
                  ><span class="check-control"
                    ><input v-model="form.enabled" type="checkbox" /><b>{{
                      form.enabled ? "启用并参与同步" : "停用但保留配置"
                    }}</b></span
                  ></label
                >
              </div>
            </section>

            <section class="form-section audit-section">
              <div class="section-title">
                <span>05</span>
                <div>
                  <h3>审计原因</h3>
                  <p>原因将写入 X-Change-Reason 与后台审计日志。</p>
                </div>
              </div>
              <label>
                <textarea
                  v-model.trim="form.reason"
                  rows="3"
                  maxlength="500"
                  placeholder="请说明此次分类绑定变更的业务原因（4–500 字）"
                  required
                ></textarea
                ><small>{{ reasonLength(form.reason) }} / 500</small></label
              >
              <p v-if="modalError" class="modal-error">
                <AlertCircle :size="14" />{{ modalError }}
              </p>
            </section>
          </div>
          <footer>
            <button
              type="button"
              class="secondary-button"
              :disabled="saving || coverUploading"
              @click="closeEditor"
            >
              {{ t("common.cancel") }}</button
            ><button
              type="submit"
              class="primary-action"
              :disabled="saving || coverUploading"
            >
              <LoaderCircle v-if="saving" :size="15" class="spinning" /><Save
                v-else
                :size="15"
              />{{ saving ? "正在保存…" : t("common.save") }}
            </button>
          </footer>
        </form>
      </section>
    </div>

    <div
      v-if="canManage && (deleting || batchAction)"
      class="modal-backdrop"
      role="presentation"
      @mousedown.self="closeConfirm"
    >
      <section class="confirm-modal" role="dialog" aria-modal="true">
        <div
          :class="[
            'confirm-icon',
            deleting || batchAction === 'delete' ? 'danger' : 'neutral',
          ]"
        >
          <Trash2
            v-if="deleting || batchAction === 'delete'"
            :size="19"
          /><Power v-else :size="19" />
        </div>
        <h2 v-if="deleting">删除分类绑定？</h2>
        <h2 v-else>
          {{
            batchAction === "delete"
              ? "批量删除分类绑定？"
              : batchAction === "enable"
                ? "批量启用分类绑定？"
                : "批量停用分类绑定？"
          }}
        </h2>
        <p v-if="deleting">
          将删除 <b>{{ deleting.external_category_name }}</b> 到
          <b>{{ deleting.category_name }}</b>
          的关系。已有商品不会被删除，但后续不再继承这条分类策略。
        </p>
        <p v-else>
          <template v-if="batchAction === 'delete'">
            即将删除所选 <b>{{ selectedIDs.length }}</b>
            条绑定。已有商品不会被删除，但会脱离分类策略；删除记录会保留为防止自动重建的安全标记。
          </template>
          <template v-else>
            即将{{ batchAction === "enable" ? "启用" : "停用" }}所选
            <b>{{ selectedIDs.length }}</b> 条绑定。停用后绑定仍可编辑和恢复。
          </template>
        </p>
        <label
          ><span>变更原因</span
          ><textarea
            v-if="deleting"
            v-model.trim="deleteReason"
            rows="3"
            maxlength="500"
            placeholder="请输入删除原因（4–500 字）"
          ></textarea
          ><textarea
            v-else
            v-model.trim="batchReason"
            rows="3"
            maxlength="500"
            :placeholder="
              batchAction === 'delete'
                ? '请输入批量删除原因（4–500 字）'
                : '请输入批量变更原因（4–500 字）'
            "
          ></textarea>
        </label>
        <p v-if="modalError" class="modal-error">
          <AlertCircle :size="14" />{{ modalError }}
        </p>
        <footer>
          <button
            type="button"
            class="secondary-button"
            :disabled="saving"
            @click="closeConfirm"
          >
            {{ t("common.cancel") }}</button
          ><button
            v-if="deleting"
            type="button"
            class="danger-button"
            :disabled="saving"
            @click="confirmDelete"
          >
            <LoaderCircle v-if="saving" :size="15" class="spinning" /><Trash2
              v-else
              :size="15"
            />确认删除</button
          ><button
            v-else
            type="button"
            :class="
              batchAction === 'delete' ? 'danger-button' : 'primary-action'
            "
            :disabled="saving"
            @click="confirmBatch"
          >
            <LoaderCircle v-if="saving" :size="15" class="spinning" /><Trash2
              v-else-if="batchAction === 'delete'"
              :size="15"
            /><Power v-else :size="15" />确认{{
              batchAction === "delete"
                ? "删除"
                : batchAction === "enable"
                  ? "启用"
                  : "停用"
            }}
          </button>
        </footer>
      </section>
    </div>
  </section>
</template>

<style scoped>
.binding-shell {
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
  white-space: nowrap;
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
.metric-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}
.metric-card {
  min-width: 0;
  padding: 14px 16px;
  display: grid;
  gap: 5px;
}
.metric-card > span {
  color: var(--muted);
  font-size: 9px;
}
.metric-card strong {
  font-size: 25px;
  letter-spacing: -0.04em;
}
.metric-card small {
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: 8px;
}
.metric-card.success small {
  color: var(--success);
}
.metric-card.muted strong {
  color: var(--muted);
}
.binding-panel {
  min-width: 0;
  overflow: hidden;
}
.binding-toolbar {
  height: auto;
  min-height: 58px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  gap: 12px;
}
.search-box {
  min-width: 220px;
  max-width: 430px;
  flex: 1;
  height: 36px;
  padding: 0 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--surface-2);
  color: var(--muted);
}
.search-box input {
  width: 100%;
  min-width: 0;
  border: 0;
  outline: 0;
  background: transparent;
  font-size: 10px;
}
.filters {
  display: flex;
  align-items: center;
  gap: 7px;
}
.filters select,
.pager select {
  height: 34px;
  border: 1px solid var(--line);
  border-radius: 7px;
  padding: 0 28px 0 9px;
  background: var(--surface);
  color: var(--text);
  font-size: 9px;
}
.icon-button {
  width: 34px;
  height: 34px;
  padding: 0;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--muted);
}
.icon-button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.primary-action,
.secondary-button,
.danger-button {
  min-height: 34px;
  padding: 0 12px;
  border-radius: 7px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  font-size: 9px;
  font-weight: 600;
}
.primary-action {
  border: 1px solid var(--dark);
  background: var(--dark);
  color: var(--dark-text);
}
.secondary-button {
  border: 1px solid var(--line);
  background: var(--surface);
  color: var(--text);
}
.danger-button {
  border: 1px solid var(--danger);
  background: var(--danger);
  color: #fff;
}
.primary-action:disabled,
.secondary-button:disabled,
.danger-button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.message {
  min-height: 38px;
  padding: 8px 14px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 9px;
}
.message span {
  flex: 1;
}
.message button {
  border: 0;
  background: transparent;
  color: inherit;
}
.success-message {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 6%, var(--surface));
}
.error-message {
  color: var(--danger);
  background: color-mix(in srgb, var(--danger) 6%, var(--surface));
}
.batch-toolbar {
  min-height: 46px;
  padding: 7px 14px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  background: var(--soft);
  font-size: 9px;
}
.batch-toolbar > div {
  display: flex;
  gap: 6px;
}
.batch-toolbar button {
  min-height: 31px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  background: var(--surface);
}
.batch-toolbar button.quiet {
  color: var(--muted);
}
.batch-toolbar button.danger {
  border-color: color-mix(in srgb, var(--danger) 28%, var(--line));
  color: var(--danger);
}
.table-wrap {
  width: 100%;
  overflow-x: auto;
}
.binding-table {
  width: 100%;
  min-width: 1080px;
  border-collapse: collapse;
  table-layout: fixed;
}
.binding-table th {
  height: 38px;
  padding: 0 10px;
  border-bottom: 1px solid var(--line);
  background: var(--surface-2);
  color: var(--muted);
  text-align: left;
  font-size: 8px;
  font-weight: 600;
}
.binding-table td {
  min-height: 60px;
  padding: 10px;
  border-bottom: 1px solid var(--line);
  vertical-align: middle;
  font-size: 9px;
}
.binding-table tbody tr {
  transition: background 0.16s ease;
}
.binding-table tbody tr:hover,
.binding-table tbody tr.selected {
  background: color-mix(in srgb, var(--text) 2.5%, var(--surface));
}
.binding-table tbody tr.disabled {
  opacity: 0.72;
}
.binding-table th:nth-child(1),
.binding-table td:nth-child(1) {
  width: 42px;
}
.binding-table th:nth-child(2),
.binding-table td:nth-child(2) {
  width: 245px;
}
.binding-table th:nth-child(3),
.binding-table td:nth-child(3) {
  width: 140px;
}
.binding-table th:nth-child(4),
.binding-table td:nth-child(4) {
  width: 155px;
}
.binding-table th:nth-child(5),
.binding-table td:nth-child(5) {
  width: 155px;
}
.binding-table th:nth-child(6),
.binding-table td:nth-child(6) {
  width: 105px;
}
.binding-table th:nth-child(7),
.binding-table td:nth-child(7) {
  width: 155px;
}
.selection-cell {
  text-align: center !important;
}
.selection-cell input {
  width: 15px;
  height: 15px;
}
.category-pair {
  display: flex;
  align-items: center;
  gap: 9px;
  min-width: 0;
}
.cover-box {
  width: 39px;
  height: 39px;
  flex: none;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: grid;
  place-items: center;
  overflow: hidden;
  background: var(--soft);
  color: var(--muted);
}
.cover-box img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.pair-copy,
.supplier-cell,
.price-cell,
.time-cell {
  min-width: 0;
  display: grid;
  gap: 3px;
}
.pair-copy b,
.supplier-cell b,
.price-cell b {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  color: var(--muted);
  font-size: 8px;
}
.pair-copy span {
  display: flex;
  align-items: center;
  gap: 4px;
  color: var(--muted);
  font-size: 8px;
}
.row-error {
  max-width: 220px;
  margin: 6px 0 0 48px;
  display: flex;
  align-items: center;
  gap: 4px;
  overflow: hidden;
  color: var(--danger);
  font-size: 8px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mini-status {
  width: max-content;
  padding: 2px 6px;
  border-radius: 10px;
  background: var(--soft);
  color: var(--muted);
  font-size: 7px;
}
.mini-status.active {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 8%, var(--surface));
}
.sync-flags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.sync-flags span {
  padding: 3px 6px;
  border: 1px solid var(--line);
  border-radius: 10px;
  color: var(--muted);
  font-size: 7px;
}
.sync-flags span.on {
  border-color: color-mix(in srgb, var(--success) 28%, var(--line));
  background: color-mix(in srgb, var(--success) 7%, var(--surface));
  color: var(--success);
}
.price-cell span,
.time-cell span,
.time-cell small {
  color: var(--muted);
  font-size: 8px;
}
.time-cell small {
  display: flex;
  align-items: center;
  gap: 4px;
}
.status-pill {
  padding: 0;
  border: 0;
  display: flex;
  align-items: center;
  gap: 5px;
  background: transparent;
  font-size: 8px;
}
.status-pill > span {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}
.status-pill.enabled {
  color: var(--success);
}
.status-pill.enabled > span {
  background: var(--success);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--success) 12%, transparent);
}
.status-pill.disabled {
  color: var(--muted);
}
.status-pill.disabled > span {
  background: var(--muted);
}
.sort-value {
  display: block;
  margin-top: 6px;
  color: var(--muted);
  font-size: 7px;
}
.actions-cell {
  text-align: right !important;
}
.row-actions {
  display: flex;
  justify-content: flex-end;
  gap: 5px;
}
.row-actions button {
  width: 30px;
  height: 30px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--muted);
}
.row-actions button:hover {
  color: var(--text);
}
.row-actions button.danger:hover {
  border-color: color-mix(in srgb, var(--danger) 35%, var(--line));
  color: var(--danger);
}
.empty-row {
  height: 250px;
  text-align: center !important;
  color: var(--muted);
}
.empty-row > * {
  margin: 4px auto;
}
.empty-row b,
.empty-row span {
  display: block;
}
.empty-row button {
  min-height: 31px;
  padding: 0 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  background: var(--surface);
}
.pager {
  min-height: 50px;
  padding: 8px 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: var(--muted);
  font-size: 8px;
}
.pager > div {
  display: flex;
  align-items: center;
  gap: 8px;
}
.pager button {
  min-height: 31px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: var(--surface);
}
.pager button:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 80;
  padding: 24px;
  display: grid;
  place-items: center;
  background: rgba(12, 12, 13, 0.62);
  backdrop-filter: blur(4px);
}
.editor-modal {
  width: min(920px, 100%);
  max-height: calc(100dvh - 48px);
  border: 1px solid var(--line);
  border-radius: 12px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--surface);
  box-shadow: 0 32px 100px rgba(0, 0, 0, 0.28);
}
.editor-modal > header {
  flex: none;
  padding: 18px 20px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.editor-modal > header span {
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
  letter-spacing: 0.14em;
}
.editor-modal h2,
.confirm-modal h2 {
  margin: 5px 0 3px;
  font-size: 20px;
  letter-spacing: -0.025em;
}
.editor-modal > header p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.5;
}
.editor-modal > form {
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.form-content {
  min-height: 0;
  padding: 0 20px;
  overflow-y: auto;
}
.form-section {
  padding: 18px 0;
  border-bottom: 1px solid var(--line);
}
.form-section:last-child {
  border-bottom: 0;
}
.section-title {
  margin-bottom: 13px;
  display: flex;
  align-items: flex-start;
  gap: 10px;
}
.section-title > span {
  width: 23px;
  height: 23px;
  flex: none;
  border-radius: 6px;
  display: grid;
  place-items: center;
  background: var(--soft);
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
}
.section-title h3 {
  margin: 0 0 3px;
  font-size: 11px;
}
.section-title p {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 11px;
}
.form-grid .full {
  grid-column: 1 / -1;
}
.form-grid label,
.cover-editor label,
.audit-section > label,
.confirm-modal label {
  display: grid;
  gap: 6px;
  color: var(--muted);
  font-size: 9px;
}
.form-grid label > span,
.cover-editor label > span,
.confirm-modal label > span {
  color: var(--text);
  font-weight: 600;
}
.form-grid label > span b {
  color: var(--danger);
}
.form-grid input,
.form-grid select,
.cover-editor input,
.remote-search input,
.audit-section textarea,
.confirm-modal textarea {
  width: 100%;
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: 7px;
  outline: 0;
  background: var(--surface-2);
}
.form-grid input,
.form-grid select,
.cover-editor input {
  height: 38px;
  padding: 0 10px;
  font-size: 10px;
}
.form-grid input:focus,
.form-grid select:focus,
.cover-editor input:focus,
.remote-search input:focus,
textarea:focus {
  border-color: var(--text);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--text) 8%, transparent);
}
.remote-picker {
  display: grid;
  grid-template-columns: minmax(0, 1.35fr) minmax(260px, 0.65fr);
  gap: 10px;
  align-items: end;
}
.remote-search {
  height: 38px;
  padding-left: 10px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: flex;
  align-items: center;
  gap: 6px;
  background: var(--surface-2);
  color: var(--muted);
}
.remote-search input {
  height: 34px;
  padding: 0;
  border: 0;
  background: transparent;
  font-size: 9px;
}
.remote-search button {
  height: 30px;
  margin-right: 3px;
  padding: 0 8px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: flex;
  align-items: center;
  gap: 4px;
  background: var(--surface);
  white-space: nowrap;
  font-size: 8px;
}
.cover-editor {
  display: grid;
  grid-template-columns: 92px minmax(0, 1fr);
  gap: 13px;
  align-items: center;
}
.cover-control {
  min-width: 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 7px 9px;
  align-items: end;
}
.cover-control > label:first-child {
  grid-row: 1 / span 2;
}
.cover-upload-button {
  min-height: 38px;
  padding: 0 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: inline-flex !important;
  align-items: center;
  justify-content: center;
  gap: 6px !important;
  background: var(--surface);
  color: var(--text) !important;
  white-space: nowrap;
  cursor: pointer;
}
.cover-upload-button.disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.cover-upload-button input {
  display: none;
}
.cover-upload-hint {
  color: var(--muted);
  font-size: 8px;
  line-height: 1.4;
}
.cover-preview {
  width: 92px;
  height: 70px;
  border: 1px dashed var(--line);
  border-radius: 8px;
  display: grid;
  place-items: center;
  overflow: hidden;
  background: var(--soft);
  color: var(--muted);
}
.cover-preview img {
  width: 100%;
  height: 100%;
  grid-area: 1 / 1;
  object-fit: cover;
}
.cover-preview small {
  grid-area: 1 / 1;
  align-self: end;
  width: 100%;
  padding: 3px;
  background: rgba(0, 0, 0, 0.45);
  color: #fff;
  text-align: center;
  font-size: 7px;
}
.cover-editor label small {
  font-size: 8px;
  font-weight: 400;
}
.toggle-grid,
.pricing-modes {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.toggle-grid label,
.pricing-modes label {
  min-height: 57px;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  display: flex;
  align-items: flex-start;
  gap: 9px;
  background: var(--surface-2);
  cursor: pointer;
}
.toggle-grid label.active,
.pricing-modes label.active {
  border-color: color-mix(in srgb, var(--success) 35%, var(--line));
  background: color-mix(in srgb, var(--success) 5%, var(--surface));
}
.toggle-grid label.disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.toggle-grid label > span,
.pricing-modes label > span {
  display: grid;
  gap: 3px;
}
.toggle-grid b,
.pricing-modes b {
  font-size: 9px;
}
.toggle-grid small,
.pricing-modes small {
  color: var(--muted);
  font-size: 8px;
  line-height: 1.45;
}
.pricing-modes {
  margin-bottom: 11px;
}
.compact-grid {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}
.suffix-input {
  height: 38px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: flex;
  align-items: center;
  overflow: hidden;
  background: var(--surface-2);
}
.suffix-input input {
  height: 36px;
  border: 0;
}
.suffix-input span {
  padding: 0 10px;
  color: var(--muted) !important;
  font-size: 8px;
}
.enabled-control .check-control {
  height: 38px;
  padding: 0 10px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--surface-2);
}
.enabled-control .check-control b {
  font-size: 9px;
  font-weight: 500;
}
.audit-section textarea,
.confirm-modal textarea {
  padding: 10px;
  resize: vertical;
  font-size: 10px;
  line-height: 1.5;
}
.audit-section label {
  position: relative;
}
.audit-section label small {
  position: absolute;
  right: 8px;
  bottom: 6px;
  color: var(--muted);
  font-size: 7px;
}
.modal-error {
  margin: 9px 0 0;
  display: flex;
  align-items: flex-start;
  gap: 6px;
  color: var(--danger);
  font-size: 9px;
  line-height: 1.5;
}
.modal-error svg {
  flex: none;
}
.editor-modal form > footer {
  flex: none;
  min-height: 62px;
  padding: 11px 20px;
  border-top: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  background: var(--surface-2);
}
.confirm-modal {
  width: min(430px, 100%);
  padding: 21px;
  border: 1px solid var(--line);
  border-radius: 11px;
  background: var(--surface);
  box-shadow: 0 28px 90px rgba(0, 0, 0, 0.28);
}
.confirm-icon {
  width: 42px;
  height: 42px;
  border-radius: 9px;
  display: grid;
  place-items: center;
  background: var(--soft);
  color: var(--muted);
}
.confirm-icon.danger {
  background: color-mix(in srgb, var(--danger) 9%, var(--surface));
  color: var(--danger);
}
.confirm-modal > p {
  margin: 9px 0 16px;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.65;
}
.confirm-modal > p b {
  color: var(--text);
}
.confirm-modal footer {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.spinning {
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 1040px) {
  .metric-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .binding-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .search-box {
    width: 100%;
    max-width: none;
  }
  .filters {
    flex-wrap: wrap;
  }
  .filters select {
    flex: 1;
  }
}

@media (max-width: 760px) {
  .binding-shell {
    gap: 10px;
  }
  .supply-nav {
    padding: 0 7px;
    overflow-x: auto;
  }
  .supply-tabs {
    width: 100%;
  }
  .supply-tabs button {
    flex: 1;
    justify-content: center;
    padding: 0 6px;
  }
  .nav-context {
    display: none;
  }
  .binding-toolbar {
    padding: 9px;
  }
  .filters select {
    min-width: 130px;
  }
  .filters .primary-action {
    flex: 1;
  }
  .batch-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }
  .batch-toolbar > div {
    width: 100%;
    flex-wrap: wrap;
  }
  .table-wrap {
    overflow: visible;
  }
  .binding-table {
    min-width: 0;
    display: block;
  }
  .binding-table thead {
    display: none;
  }
  .binding-table tbody {
    padding: 9px;
    display: grid;
    gap: 9px;
  }
  .binding-table tbody tr {
    border: 1px solid var(--line);
    border-radius: 9px;
    display: grid;
    grid-template-columns: 1fr 1fr;
    overflow: hidden;
    background: var(--surface);
  }
  .binding-table tbody tr > td {
    width: auto !important;
    min-height: 0;
    padding: 10px;
    border-bottom: 1px solid var(--line);
    display: grid;
    gap: 5px;
    text-align: left !important;
  }
  .binding-table tbody tr > td::before {
    content: attr(data-label);
    color: var(--muted);
    font-size: 7px;
    font-weight: 600;
  }
  .binding-table tbody tr > td.selection-cell {
    position: absolute;
    z-index: 1;
    padding: 10px;
    border: 0;
    display: block;
  }
  .binding-table tbody tr > td.selection-cell::before {
    display: none;
  }
  .binding-table tbody tr > td:nth-child(2) {
    grid-column: 1 / -1;
    padding-left: 41px;
  }
  .binding-table tbody tr > td.actions-cell {
    display: flex;
    align-items: center;
    justify-content: flex-end;
  }
  .row-actions {
    justify-content: flex-start;
  }
  .empty-row {
    grid-column: 1 / -1 !important;
    min-height: 230px !important;
    padding: 40px !important;
    display: block !important;
  }
  .pager {
    align-items: flex-start;
    flex-direction: column;
  }
  .pager > div:last-child {
    width: 100%;
    justify-content: space-between;
  }
  .modal-backdrop {
    padding: 10px;
  }
  .editor-modal {
    max-height: calc(100dvh - 20px);
  }
  .form-content {
    padding: 0 14px;
  }
  .editor-modal > header,
  .editor-modal form > footer {
    padding-left: 14px;
    padding-right: 14px;
  }
  .form-grid,
  .toggle-grid,
  .pricing-modes,
  .compact-grid {
    grid-template-columns: 1fr;
  }
  .form-grid .full {
    grid-column: auto;
  }
  .remote-picker {
    grid-template-columns: 1fr;
  }
  .cover-control {
    grid-template-columns: 1fr;
  }
  .cover-control > label:first-child {
    grid-row: auto;
  }
  .cover-upload-button {
    width: 100%;
  }
}

@media (max-width: 500px) {
  .supply-tabs button {
    font-size: 0;
  }
  .supply-tabs button span {
    display: none;
  }
  .metric-grid {
    grid-template-columns: 1fr 1fr;
  }
  .metric-card {
    padding: 12px;
  }
  .filters select {
    width: 100%;
    flex-basis: 100%;
  }
  .filters .icon-button {
    flex: none;
  }
  .binding-table tbody tr {
    grid-template-columns: 1fr;
  }
  .binding-table tbody tr > td:nth-child(2) {
    grid-column: auto;
  }
  .binding-table tbody tr > td.selection-cell {
    position: absolute;
  }
  .cover-editor {
    grid-template-columns: 1fr;
  }
  .cover-preview {
    width: 100%;
    height: 120px;
  }
  .confirm-modal {
    padding: 17px;
  }
}
</style>
