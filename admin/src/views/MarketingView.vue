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
  Edit3,
  LoaderCircle,
  PackageSearch,
  Plus,
  RefreshCw,
  Search,
  Tags,
  TicketPercent,
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

const { t, locale } = useI18n();
const route = useRoute();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("marketing.manage"));

type MarketingTab = "promotions" | "coupons";
type PromotionType = "percentage" | "fixed" | "threshold_fixed" | "flash_price";
type PromotionStatus = "draft" | "active" | "paused" | "archived";
type CouponType = "fixed" | "percentage";

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface PromotionRules {
  basis_points?: number;
  amount?: number;
  min_amount?: number;
  max_discount?: number;
  unit_price?: number;
}

interface Promotion {
  id: string;
  name: string;
  code: string;
  type: PromotionType;
  currency: string;
  rules: string | PromotionRules;
  priority: number;
  stackable: boolean;
  starts_at: string;
  ends_at: string;
  status: PromotionStatus;
  product_ids: string[];
  created_at: string;
  updated_at: string;
}

interface Coupon {
  id: string;
  code: string;
  type: CouponType;
  value: number;
  currency: string;
  min_amount: number;
  usage_limit: number;
  used_count: number;
  starts_at: string;
  ends_at: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

interface Product {
  id: string;
  name: string;
  slug: string;
  price: number;
  currency: string;
  status: string;
  category?: { name?: string };
}

interface PromotionForm {
  name: string;
  code: string;
  type: PromotionType;
  discountPercent: number;
  amountYuan: string;
  minAmountYuan: string;
  maxDiscountYuan: string;
  unitPriceYuan: string;
  currency: string;
  priority: number;
  stackable: boolean;
  startsAt: string;
  endsAt: string;
  status: PromotionStatus;
  productIDs: string[];
  reason: string;
}

interface CouponForm {
  code: string;
  type: CouponType;
  valueYuan: string;
  discountPercent: number;
  minAmountYuan: string;
  currency: string;
  usageLimit: number;
  enabled: boolean;
  startsAt: string;
  endsAt: string;
  reason: string;
  usedCount: number;
}

const promotionTypeOptions: Array<{
  value: PromotionType;
  label: string;
  hint: string;
}> = [
  {
    value: "percentage",
    label: "marketingadmin.promotionTypeOptions.percentage.label",
    hint: "marketingadmin.promotionTypeOptions.percentage.hint",
  },
  {
    value: "fixed",
    label: "marketingadmin.promotionTypeOptions.fixed.label",
    hint: "marketingadmin.promotionTypeOptions.fixed.hint",
  },
  {
    value: "threshold_fixed",
    label: "marketingadmin.promotionTypeOptions.threshold_fixed.label",
    hint: "marketingadmin.promotionTypeOptions.threshold_fixed.hint",
  },
  {
    value: "flash_price",
    label: "marketingadmin.promotionTypeOptions.flash_price.label",
    hint: "marketingadmin.promotionTypeOptions.flash_price.hint",
  },
];
const promotionStatusOptions: Array<{
  value: "" | PromotionStatus;
  label: string;
}> = [
  { value: "", label: "marketingadmin.promotionStatusOptions.all" },
  { value: "draft", label: "marketingadmin.promotionStatusOptions.draft" },
  { value: "active", label: "marketingadmin.promotionStatusOptions.active" },
  { value: "paused", label: "marketingadmin.promotionStatusOptions.paused" },
  {
    value: "archived",
    label: "marketingadmin.promotionStatusOptions.archived",
  },
];
const couponEnabledOptions = [
  { value: "", label: "marketingadmin.couponEnabledOptions.all" },
  { value: "true", label: "marketingadmin.couponEnabledOptions.enabled" },
  { value: "false", label: "marketingadmin.couponEnabledOptions.disabled" },
];
function promotionStatusLabel(status: string) {
  const key = `marketingadmin.promotionStatus.${status}`;
  return t(key) === key ? status : t(key);
}
function promotionTypeLabel(type: string) {
  const key = `marketingadmin.promotionType.${type}`;
  return t(key) === key ? type : t(key);
}

const activeTab = ref<MarketingTab>("promotions");
const promotions = ref<Promotion[]>([]);
const coupons = ref<Coupon[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const searchInput = ref("");
const appliedSearch = ref("");
const stateFilter = ref("");
const loading = ref(false);
const loadError = ref("");
const notice = ref("");
const modalKind = ref<"promotion" | "coupon" | null>(null);
const editingID = ref("");
const saving = ref(false);
const formError = ref("");
const products = ref<Product[]>([]);
const knownProducts = ref<Product[]>([]);
const productTotal = ref(0);
const productPage = ref(1);
const productSearchInput = ref("");
const productAppliedSearch = ref("");
const productLoading = ref(false);
const productError = ref("");
let listRequest = 0;
let productRequest = 0;

function localDateValue(date: Date) {
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function defaultPeriod() {
  const start = new Date(Date.now() + 5 * 60_000);
  start.setSeconds(0, 0);
  const end = new Date(start.getTime() + 30 * 24 * 60 * 60_000);
  return { startsAt: localDateValue(start), endsAt: localDateValue(end) };
}

function defaultPromotionForm(): PromotionForm {
  const period = defaultPeriod();
  return {
    name: "",
    code: "",
    type: "percentage",
    discountPercent: 10,
    amountYuan: "1.00",
    minAmountYuan: "0.00",
    maxDiscountYuan: "0.00",
    unitPriceYuan: "0.00",
    currency: storeCurrency.value,
    priority: 0,
    stackable: false,
    startsAt: period.startsAt,
    endsAt: period.endsAt,
    status: "draft",
    productIDs: [],
    reason: "",
  };
}

function defaultCouponForm(): CouponForm {
  const period = defaultPeriod();
  return {
    code: "",
    type: "fixed",
    valueYuan: "1.00",
    discountPercent: 10,
    minAmountYuan: "0.00",
    currency: storeCurrency.value,
    usageLimit: 0,
    enabled: false,
    startsAt: period.startsAt,
    endsAt: period.endsAt,
    reason: "",
    usedCount: 0,
  };
}

const promotionForm = ref<PromotionForm>(defaultPromotionForm());
const couponForm = ref<CouponForm>(defaultCouponForm());

const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const activeItemCount = computed(() =>
  activeTab.value === "promotions"
    ? promotions.value.length
    : coupons.value.length,
);
const pageNumbers = computed(() => {
  const start = Math.max(1, Math.min(page.value - 2, totalPages.value - 4));
  const end = Math.min(totalPages.value, start + 4);
  return Array.from({ length: end - start + 1 }, (_, index) => start + index);
});
const modalTitle = computed(() => {
  const action = editingID.value
    ? t("marketingadmin.modalTitleEdit")
    : t("marketingadmin.modalTitleCreate");
  const type = t(
    modalKind.value === "coupon"
      ? "marketingadmin.typeCoupon"
      : "marketingadmin.typePromotion",
  );
  return action.replace("{type}", type);
});
const productLookup = computed(
  () => new Map(knownProducts.value.map((product) => [product.id, product])),
);
const selectedProducts = computed(() =>
  promotionForm.value.productIDs.map((id) => ({
    id,
    product: productLookup.value.get(id),
  })),
);
const canLoadMoreProducts = computed(
  () => products.value.length < productTotal.value,
);

function apiMessage(error: unknown, fallback: string) {
  const failure = error as { response?: { data?: { message?: string } } };
  return failure.response?.data?.message || fallback;
}

function parseRules(value: string | PromotionRules): PromotionRules {
  if (typeof value === "object" && value) return value;
  try {
    const parsed = JSON.parse(value || "{}") as PromotionRules;
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

function centsToYuan(value?: number, currency?: string) {
  return minorToMajor(value || 0, currency || storeCurrency.value);
}

function yuanToCents(value: string, currency?: string) {
  return minorToSafeNumber(
    majorToMinor(value, currency || storeCurrency.value),
  );
}

function formatMoney(value?: number, currency?: string) {
  return formatMinorMoney(value, currency || storeCurrency.value, locale.value);
}

function formatTime(value?: string) {
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

function toDateInput(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "" : localDateValue(date);
}

function promotionRuleLabel(item: Promotion) {
  const rules = parseRules(item.rules);
  const minimum = rules.min_amount
    ? t("marketingadmin.ruleThreshold", {
        amount: formatMoney(rules.min_amount, item.currency),
      })
    : "";
  switch (item.type) {
    case "percentage": {
      const cap = rules.max_discount
        ? t("marketingadmin.ruleCap", {
            amount: formatMoney(rules.max_discount, item.currency),
          })
        : "";
      const percent = ((rules.basis_points || 0) / 100)
        .toFixed(2)
        .replace(/\.00$/, "");
      return `${t("marketingadmin.rulePercent", { percent })}${minimum}${cap}`;
    }
    case "fixed":
      return `${t("marketingadmin.ruleFixed", {
        amount: formatMoney(rules.amount, item.currency),
      })}${minimum}`;
    case "threshold_fixed":
      return t("marketingadmin.ruleThresholdFixed", {
        amount: formatMoney(rules.min_amount, item.currency),
        discount: formatMoney(rules.amount, item.currency),
      });
    case "flash_price":
      return t("marketingadmin.ruleFlash", {
        amount: formatMoney(rules.unit_price, item.currency),
      });
    default:
      return t("marketingadmin.ruleUnknown");
  }
}

function couponValueLabel(item: Coupon) {
  return item.type === "percentage"
    ? t("marketingadmin.rulePercent", {
        percent: (item.value / 100).toFixed(2).replace(/\.00$/, ""),
      })
    : t("marketingadmin.ruleFixed", {
        amount: formatMoney(item.value, item.currency),
      });
}

function lifecycle(startsAt: string, endsAt: string) {
  const now = Date.now();
  const start = new Date(startsAt).getTime();
  const end = new Date(endsAt).getTime();
  if (Number.isFinite(start) && now < start)
    return t("marketingadmin.lifecyclePending");
  if (Number.isFinite(end) && now > end)
    return t("marketingadmin.lifecycleEnded");
  return t("marketingadmin.lifecycleActive");
}

function mergeKnownProducts(items: Product[]) {
  const merged = new Map(knownProducts.value.map((item) => [item.id, item]));
  for (const item of items) merged.set(item.id, item);
  knownProducts.value = [...merged.values()];
}

async function loadList() {
  const request = ++listRequest;
  loading.value = true;
  loadError.value = "";
  try {
    const url =
      activeTab.value === "promotions"
        ? "/marketing/promotions"
        : "/marketing/coupons";
    const params: Record<string, string | number> = {
      page: page.value,
      page_size: pageSize.value,
    };
    if (appliedSearch.value) params.q = appliedSearch.value;
    if (stateFilter.value) {
      if (activeTab.value === "promotions") params.status = stateFilter.value;
      else params.enabled = stateFilter.value;
    }
    const { data } = await adminApi.get(url, { params });
    if (request !== listRequest) return;
    const payload = data.data as PagePayload<Promotion | Coupon>;
    const items = Array.isArray(payload.items) ? payload.items : [];
    if (activeTab.value === "promotions")
      promotions.value = items as Promotion[];
    else coupons.value = items as Coupon[];
    total.value = Number(payload.total || 0);
    page.value = Number(payload.page || page.value);
    pageSize.value = Number(payload.page_size || pageSize.value);
    if (page.value > totalPages.value && page.value > 1) {
      page.value = totalPages.value;
      await loadList();
    }
  } catch (error: unknown) {
    if (request !== listRequest) return;
    if (activeTab.value === "promotions") promotions.value = [];
    else coupons.value = [];
    total.value = 0;
    loadError.value = apiMessage(error, t("marketingadmin.errLoad"));
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

async function applyStateFilter() {
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

async function loadProducts(reset: boolean) {
  const request = ++productRequest;
  if (reset) {
    productPage.value = 1;
    products.value = [];
    productAppliedSearch.value = productSearchInput.value.trim();
  }
  productLoading.value = true;
  productError.value = "";
  try {
    const { data } = await adminApi.get("/products", {
      params: {
        page: productPage.value,
        page_size: 100,
        ...(productAppliedSearch.value
          ? { q: productAppliedSearch.value }
          : {}),
      },
    });
    if (request !== productRequest) return;
    const payload = data.data as PagePayload<Product>;
    const incoming = Array.isArray(payload.items) ? payload.items : [];
    const merged = reset ? incoming : [...products.value, ...incoming];
    products.value = [
      ...new Map(merged.map((item) => [item.id, item])).values(),
    ];
    mergeKnownProducts(incoming);
    productTotal.value = Number(payload.total || 0);
  } catch (error: unknown) {
    if (request !== productRequest) return;
    productError.value = apiMessage(error, t("marketingadmin.errLoadProducts"));
  } finally {
    if (request === productRequest) productLoading.value = false;
  }
}

async function searchProducts() {
  await loadProducts(true);
}

async function clearProductSearch() {
  productSearchInput.value = "";
  await loadProducts(true);
}

async function loadMoreProducts() {
  if (!canLoadMoreProducts.value || productLoading.value) return;
  productPage.value += 1;
  await loadProducts(false);
}

function toggleProduct(id: string) {
  const selected = promotionForm.value.productIDs;
  const index = selected.indexOf(id);
  if (index >= 0) {
    selected.splice(index, 1);
    return;
  }
  if (selected.length >= 200) {
    productError.value = t("marketingadmin.errMaxProducts");
    return;
  }
  selected.push(id);
  productError.value = "";
}

function addVisibleProducts() {
  const merged = new Set(promotionForm.value.productIDs);
  for (const product of products.value) {
    if (merged.size >= 200) break;
    merged.add(product.id);
  }
  promotionForm.value.productIDs = [...merged];
}

function removeProduct(id: string) {
  promotionForm.value.productIDs = promotionForm.value.productIDs.filter(
    (value) => value !== id,
  );
}

function openCreate() {
  if (!canManage.value) return;
  formError.value = "";
  editingID.value = "";
  if (activeTab.value === "promotions") {
    promotionForm.value = defaultPromotionForm();
    modalKind.value = "promotion";
    productSearchInput.value = "";
    void loadProducts(true);
  } else {
    couponForm.value = defaultCouponForm();
    modalKind.value = "coupon";
  }
}

function openPromotion(item: Promotion) {
  if (!canManage.value) return;
  const rules = parseRules(item.rules);
  editingID.value = item.id;
  formError.value = "";
  promotionForm.value = {
    name: item.name,
    code: item.code,
    type: item.type,
    currency: item.currency || storeCurrency.value,
    discountPercent: Number(rules.basis_points || 0) / 100,
    amountYuan: centsToYuan(rules.amount, item.currency),
    minAmountYuan: centsToYuan(rules.min_amount, item.currency),
    maxDiscountYuan: centsToYuan(rules.max_discount, item.currency),
    unitPriceYuan: centsToYuan(rules.unit_price, item.currency),
    priority: item.priority,
    stackable: item.stackable,
    startsAt: toDateInput(item.starts_at),
    endsAt: toDateInput(item.ends_at),
    status: item.status,
    productIDs: [...(item.product_ids || [])],
    reason: "",
  };
  modalKind.value = "promotion";
  productSearchInput.value = "";
  void loadProducts(true);
}

function openCoupon(item: Coupon) {
  if (!canManage.value) return;
  editingID.value = item.id;
  formError.value = "";
  couponForm.value = {
    code: item.code,
    type: item.type,
    currency: item.currency || storeCurrency.value,
    valueYuan:
      item.type === "fixed" ? centsToYuan(item.value, item.currency) : "0",
    discountPercent: item.type === "percentage" ? item.value / 100 : 0,
    minAmountYuan: centsToYuan(item.min_amount, item.currency),
    usageLimit: item.usage_limit,
    enabled: item.enabled,
    startsAt: toDateInput(item.starts_at),
    endsAt: toDateInput(item.ends_at),
    reason: "",
    usedCount: item.used_count,
  };
  modalKind.value = "coupon";
}

function closeModal() {
  if (saving.value) return;
  modalKind.value = null;
  formError.value = "";
}

function validCode(value: string) {
  return /^[A-Z0-9][A-Z0-9_-]{2,79}$/.test(value.toUpperCase().trim());
}

function validPeriod(startsAt: string, endsAt: string) {
  const start = new Date(startsAt);
  const end = new Date(endsAt);
  const max = new Date();
  max.setFullYear(max.getFullYear() + 10);
  return (
    !Number.isNaN(start.getTime()) &&
    !Number.isNaN(end.getTime()) &&
    end > start &&
    end <= max
  );
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function isMoneyInput(value: string, currency: string, positive = false) {
  try {
    const amount = BigInt(majorToMinor(value, currency));
    const maximum = BigInt(majorToMinor("1000000", currency));
    return positive
      ? amount > 0n && amount <= maximum
      : amount >= 0n && amount <= maximum;
  } catch {
    return false;
  }
}

function validatePromotion() {
  const form = promotionForm.value;
  const nameLength = [...form.name.trim()].length;
  if (nameLength < 2 || nameLength > 160)
    return t("marketingadmin.errNameLength");
  if (!validCode(form.code)) return t("marketingadmin.errCodeFormat");
  if (
    !isFiniteNumber(form.priority) ||
    !Number.isInteger(form.priority) ||
    form.priority < -100000 ||
    form.priority > 100000
  )
    return t("marketingadmin.errPriority");
  if (!validPeriod(form.startsAt, form.endsAt))
    return t("marketingadmin.errPeriod");
  if (form.productIDs.length < 1 || form.productIDs.length > 200)
    return t("marketingadmin.errProductCount");
  if (form.reason.trim().length < 4 || form.reason.trim().length > 500)
    return t("marketingadmin.errReasonLength");
  if (form.type === "percentage") {
    if (
      !isFiniteNumber(form.discountPercent) ||
      !(form.discountPercent > 0 && form.discountPercent <= 100)
    )
      return t("marketingadmin.errPercentRange");
    if (
      !isMoneyInput(form.minAmountYuan, form.currency) ||
      !isMoneyInput(form.maxDiscountYuan, form.currency)
    )
      return t("marketingadmin.errMinMaxNonNegative");
  } else if (form.type === "fixed") {
    if (
      !isMoneyInput(form.amountYuan, form.currency, true) ||
      !isMoneyInput(form.minAmountYuan, form.currency)
    )
      return t("marketingadmin.errFixedAmount");
  } else if (form.type === "threshold_fixed") {
    if (
      !isMoneyInput(form.amountYuan, form.currency, true) ||
      !isMoneyInput(form.minAmountYuan, form.currency, true)
    )
      return t("marketingadmin.errThresholdBoth");
  } else if (!isMoneyInput(form.unitPriceYuan, form.currency)) {
    return t("marketingadmin.errUnitPrice");
  }
  return "";
}

function promotionRulesPayload(form: PromotionForm) {
  switch (form.type) {
    case "percentage":
      return {
        basis_points: Math.round(form.discountPercent * 100),
        min_amount: yuanToCents(form.minAmountYuan, form.currency),
        max_discount: yuanToCents(form.maxDiscountYuan, form.currency),
      };
    case "fixed":
      return {
        amount: yuanToCents(form.amountYuan, form.currency),
        min_amount: yuanToCents(form.minAmountYuan, form.currency),
      };
    case "threshold_fixed":
      return {
        amount: yuanToCents(form.amountYuan, form.currency),
        min_amount: yuanToCents(form.minAmountYuan, form.currency),
      };
    case "flash_price":
      return { unit_price: yuanToCents(form.unitPriceYuan, form.currency) };
  }
}

async function submitPromotion() {
  if (!canManage.value) return;
  const validation = validatePromotion();
  if (validation) {
    formError.value = validation;
    return;
  }
  formError.value = "";
  saving.value = true;
  const form = promotionForm.value;
  const payload = {
    name: form.name.trim(),
    code: form.code.toUpperCase().trim(),
    type: form.type,
    currency: form.currency,
    rules: promotionRulesPayload(form),
    priority: Number(form.priority),
    stackable: form.stackable,
    starts_at: new Date(form.startsAt).toISOString(),
    ends_at: new Date(form.endsAt).toISOString(),
    status: form.status,
    product_ids: [...form.productIDs],
  };
  const wasEditing = Boolean(editingID.value);
  try {
    if (wasEditing) {
      await adminApi.put(
        `/marketing/promotions/${encodeURIComponent(editingID.value)}`,
        payload,
        { headers: { "X-Change-Reason": form.reason.trim() } },
      );
    } else {
      await adminApi.post("/marketing/promotions", payload, {
        headers: { "X-Change-Reason": form.reason.trim() },
      });
    }
    modalKind.value = null;
    notice.value = wasEditing
      ? t("marketingadmin.promotionUpdated")
      : t("marketingadmin.promotionCreated");
    await loadList();
  } catch (error: unknown) {
    formError.value = apiMessage(error, t("marketingadmin.errSavePromotion"));
  } finally {
    saving.value = false;
  }
}

function validateCoupon() {
  const form = couponForm.value;
  if (!validCode(form.code)) return t("marketingadmin.errCouponCode");
  if (!validPeriod(form.startsAt, form.endsAt))
    return t("marketingadmin.errCouponPeriod");
  if (!isMoneyInput(form.minAmountYuan, form.currency))
    return t("marketingadmin.errMinAmount");
  if (!Number.isSafeInteger(form.usageLimit) || form.usageLimit < 0)
    return t("marketingadmin.errUsageLimit");
  if (
    form.usageLimit > 0 &&
    editingID.value &&
    form.usageLimit < form.usedCount
  )
    return t("marketingadmin.errUsageBelow");
  if (
    form.type === "fixed" &&
    !isMoneyInput(form.valueYuan, form.currency, true)
  )
    return t("marketingadmin.errCouponValue");
  if (
    form.type === "percentage" &&
    (!isFiniteNumber(form.discountPercent) ||
      !(form.discountPercent > 0 && form.discountPercent <= 100))
  )
    return t("marketingadmin.errCouponPercent");
  if (form.reason.trim().length < 4 || form.reason.trim().length > 500)
    return t("marketingadmin.errReasonLength");
  return "";
}

async function submitCoupon() {
  if (!canManage.value) return;
  const validation = validateCoupon();
  if (validation) {
    formError.value = validation;
    return;
  }
  formError.value = "";
  saving.value = true;
  const form = couponForm.value;
  const payload = {
    code: form.code.toUpperCase().trim(),
    type: form.type,
    value:
      form.type === "fixed"
        ? yuanToCents(form.valueYuan, form.currency)
        : Math.round(form.discountPercent * 100),
    min_amount: yuanToCents(form.minAmountYuan, form.currency),
    currency: form.currency,
    usage_limit: Number(form.usageLimit),
    starts_at: new Date(form.startsAt).toISOString(),
    ends_at: new Date(form.endsAt).toISOString(),
    enabled: form.enabled,
  };
  const wasEditing = Boolean(editingID.value);
  try {
    if (wasEditing) {
      await adminApi.put(
        `/marketing/coupons/${encodeURIComponent(editingID.value)}`,
        payload,
        { headers: { "X-Change-Reason": form.reason.trim() } },
      );
    } else {
      await adminApi.post("/marketing/coupons", payload, {
        headers: { "X-Change-Reason": form.reason.trim() },
      });
    }
    modalKind.value = null;
    notice.value = wasEditing
      ? t("marketingadmin.couponUpdated")
      : t("marketingadmin.couponCreated");
    await loadList();
  } catch (error: unknown) {
    formError.value = apiMessage(error, t("marketingadmin.errSaveCoupon"));
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
    activeTab.value = defaultTab === "coupons" ? "coupons" : "promotions";
    page.value = 1;
    searchInput.value = "";
    appliedSearch.value = "";
    stateFilter.value = "";
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
  <section class="marketing-shell">
    <div class="marketing-top panel">
      <button
        v-if="canManage"
        class="primary-button compact"
        type="button"
        @click="openCreate"
      >
        <Plus :size="15" />
        {{
          activeTab === "promotions"
            ? t("marketingadmin.createPromotion")
            : t("marketingadmin.createCoupon")
        }}
      </button>
    </div>

    <div class="marketing-panel panel">
      <header class="marketing-toolbar">
        <form class="marketing-search" @submit.prevent="applySearch">
          <Search :size="15" />
          <input
            v-model="searchInput"
            type="search"
            :placeholder="
              activeTab === 'promotions'
                ? t('marketingadmin.searchPlaceholderPromotions')
                : t('marketingadmin.searchPlaceholderCoupons')
            "
            :aria-label="t('marketingadmin.searchAria')"
          />
          <button v-if="appliedSearch" type="button" @click="clearSearch">
            <X :size="13" />{{ t("marketingadmin.clear") }}
          </button>
          <button type="submit">{{ t("marketingadmin.search") }}</button>
        </form>
        <div class="marketing-filters">
          <select
            v-if="activeTab === 'promotions'"
            v-model="stateFilter"
            :aria-label="t('marketingadmin.statusFilterAria')"
            @change="applyStateFilter"
          >
            <option
              v-for="option in promotionStatusOptions"
              :key="option.value || 'all'"
              :value="option.value"
            >
              {{ t(option.label) }}
            </option>
          </select>
          <select
            v-else
            v-model="stateFilter"
            :aria-label="t('marketingadmin.couponStatusAria')"
            @change="applyStateFilter"
          >
            <option
              v-for="option in couponEnabledOptions"
              :key="option.value || 'all'"
              :value="option.value"
            >
              {{ t(option.label) }}
            </option>
          </select>
          <button
            class="refresh-button"
            type="button"
            :disabled="loading"
            @click="loadList"
          >
            <RefreshCw :size="14" :class="{ spinning: loading }" />{{
              t("marketingadmin.refresh")
            }}
          </button>
        </div>
      </header>

      <div v-if="notice" class="marketing-notice success-notice">
        <Check :size="15" />{{ notice }}
      </div>
      <div v-if="loadError" class="marketing-notice error-notice">
        <AlertCircle :size="15" />{{ loadError }}
        <button type="button" @click="loadList">
          {{ t("marketingadmin.retry") }}
        </button>
      </div>

      <div v-if="loading && !activeItemCount" class="marketing-state">
        <LoaderCircle class="spinning" :size="23" />
        <span>{{ t("marketingadmin.loading") }}</span>
      </div>
      <div
        v-else-if="
          !loadError &&
          ((activeTab === 'promotions' && !promotions.length) ||
            (activeTab === 'coupons' && !coupons.length))
        "
        class="marketing-state"
      >
        <BadgePercent :size="26" />
        <strong>{{
          appliedSearch || stateFilter
            ? t("marketingadmin.noMatch")
            : t("marketingadmin.noData")
        }}</strong>
        <span>{{ t("marketingadmin.noMatchHint") }}</span>
      </div>

      <div v-else-if="activeTab === 'promotions'" class="marketing-table-wrap">
        <table class="marketing-table">
          <thead>
            <tr>
              <th>{{ t("marketingadmin.colPromotion") }}</th>
              <th>{{ t("marketingadmin.colRules") }}</th>
              <th>{{ t("marketingadmin.colScope") }}</th>
              <th>{{ t("marketingadmin.colSchedule") }}</th>
              <th>{{ t("marketingadmin.colPeriod") }}</th>
              <th>{{ t("marketingadmin.colStatus") }}</th>
              <th>
                <span class="sr-only">{{
                  t("marketingadmin.colActions")
                }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in promotions" :key="item.id">
              <td :data-label="t('marketingadmin.colPromotion')">
                <div class="record-primary">
                  <span><TicketPercent :size="15" /></span>
                  <div>
                    <b>{{ item.name }}</b
                    ><code>{{ item.code }}</code>
                  </div>
                </div>
              </td>
              <td :data-label="t('marketingadmin.colRules')">
                <b class="rule-title">{{ promotionTypeLabel(item.type) }}</b>
                <small>{{ promotionRuleLabel(item) }}</small>
              </td>
              <td :data-label="t('marketingadmin.colScope')">
                <b>{{
                  t("marketingadmin.productCount", {
                    count: item.product_ids?.length || 0,
                  })
                }}</b>
                <small>{{ t("marketingadmin.scopeHint") }}</small>
              </td>
              <td :data-label="t('marketingadmin.colSchedule')">
                <b>{{
                  t("marketingadmin.priority", { value: item.priority })
                }}</b>
                <small>{{
                  item.stackable
                    ? t("marketingadmin.stackable")
                    : t("marketingadmin.nonStackable")
                }}</small>
              </td>
              <td :data-label="t('marketingadmin.colPeriod')">
                <time>{{ formatTime(item.starts_at) }}</time>
                <small>{{
                  t("marketingadmin.until", { time: formatTime(item.ends_at) })
                }}</small>
              </td>
              <td :data-label="t('marketingadmin.colStatus')">
                <span class="state-badge" :class="`state-${item.status}`">
                  {{ promotionStatusLabel(item.status) }}
                </span>
                <small>{{ lifecycle(item.starts_at, item.ends_at) }}</small>
              </td>
              <td
                :data-label="t('marketingadmin.colActions')"
                class="record-actions"
              >
                <button
                  v-if="canManage"
                  type="button"
                  @click="openPromotion(item)"
                >
                  <Edit3 :size="13" />{{ t("marketingadmin.edit") }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="marketing-table-wrap">
        <table class="marketing-table coupon-table">
          <thead>
            <tr>
              <th>{{ t("marketingadmin.colCoupon") }}</th>
              <th>{{ t("marketingadmin.colBenefit") }}</th>
              <th>{{ t("marketingadmin.colMinAmount") }}</th>
              <th>{{ t("marketingadmin.colUsage") }}</th>
              <th>{{ t("marketingadmin.colPeriod") }}</th>
              <th>{{ t("marketingadmin.colStatus") }}</th>
              <th>
                <span class="sr-only">{{
                  t("marketingadmin.colActions")
                }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in coupons" :key="item.id">
              <td :data-label="t('marketingadmin.colCoupon')">
                <div class="record-primary">
                  <span><Tags :size="15" /></span>
                  <div>
                    <b>{{ item.code }}</b
                    ><code>{{ item.id }}</code>
                  </div>
                </div>
              </td>
              <td :data-label="t('marketingadmin.colBenefit')">
                <b class="rule-title">{{ couponValueLabel(item) }}</b>
                <small>{{
                  item.type === "fixed"
                    ? t("marketingadmin.fixedAmount")
                    : t("marketingadmin.percentage")
                }}</small>
              </td>
              <td :data-label="t('marketingadmin.colMinAmount')">
                <b>{{
                  item.min_amount
                    ? formatMoney(item.min_amount, item.currency)
                    : t("marketingadmin.noThreshold")
                }}</b>
              </td>
              <td :data-label="t('marketingadmin.colUsage')">
                <b>{{
                  t("marketingadmin.usageCount", {
                    used: item.used_count,
                    limit: item.usage_limit || t("marketingadmin.unlimited"),
                  })
                }}</b>
                <div v-if="item.usage_limit" class="usage-track">
                  <i
                    :style="{
                      width: `${Math.min(100, (item.used_count / item.usage_limit) * 100)}%`,
                    }"
                  ></i>
                </div>
              </td>
              <td :data-label="t('marketingadmin.colPeriod')">
                <time>{{ formatTime(item.starts_at) }}</time>
                <small>{{
                  t("marketingadmin.until", { time: formatTime(item.ends_at) })
                }}</small>
              </td>
              <td :data-label="t('marketingadmin.colStatus')">
                <span
                  class="state-badge"
                  :class="item.enabled ? 'state-active' : 'state-paused'"
                >
                  {{
                    item.enabled
                      ? t("marketingadmin.enabled")
                      : t("marketingadmin.disabled")
                  }}
                </span>
                <small>{{ lifecycle(item.starts_at, item.ends_at) }}</small>
              </td>
              <td
                :data-label="t('marketingadmin.colActions')"
                class="record-actions"
              >
                <button
                  v-if="canManage"
                  type="button"
                  @click="openCoupon(item)"
                >
                  <Edit3 :size="13" />{{ t("marketingadmin.editToggle") }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <footer v-if="!loadError" class="marketing-pagination">
        <span>{{
          t("marketingadmin.pagination", { page, pages: totalPages, total })
        }}</span>
        <div>
          <button
            type="button"
            :aria-label="t('marketingadmin.prevPage')"
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
            :aria-label="t('marketingadmin.nextPage')"
            :disabled="page >= totalPages || loading"
            @click="changePage(page + 1)"
          >
            <ChevronRight :size="14" />
          </button>
          <select
            v-model.number="pageSize"
            :aria-label="t('marketingadmin.pageSizeAria')"
            @change="changePageSize"
          >
            <option :value="10">
              {{ t("marketingadmin.perPage", { size: 10 }) }}
            </option>
            <option :value="20">
              {{ t("marketingadmin.perPage", { size: 20 }) }}
            </option>
            <option :value="50">
              {{ t("marketingadmin.perPage", { size: 50 }) }}
            </option>
          </select>
        </div>
      </footer>
    </div>

    <div
      v-if="modalKind && canManage"
      class="marketing-modal-backdrop"
      role="presentation"
      @mousedown.self="closeModal"
    >
      <section
        class="marketing-modal"
        role="dialog"
        aria-modal="true"
        :aria-label="modalTitle"
      >
        <header>
          <div>
            <span class="kicker">{{ t("marketingadmin.kicker") }}</span>
            <h2>{{ modalTitle }}</h2>
            <p>{{ t("marketingadmin.modalHint") }}</p>
          </div>
          <button
            type="button"
            :aria-label="t('marketingadmin.close')"
            :disabled="saving"
            @click="closeModal"
          >
            <X :size="18" />
          </button>
        </header>

        <form
          v-if="modalKind === 'promotion'"
          class="marketing-form"
          @submit.prevent="submitPromotion"
        >
          <div v-if="formError" class="form-alert error-notice">
            <AlertCircle :size="15" />{{ formError }}
          </div>

          <fieldset>
            <legend>{{ t("marketingadmin.legendBasic") }}</legend>
            <div class="form-grid two-columns">
              <label>
                {{ t("marketingadmin.name") }}
                <input
                  v-model="promotionForm.name"
                  maxlength="160"
                  :placeholder="t('marketingadmin.namePlaceholder')"
                  autofocus
                />
              </label>
              <label>
                {{ t("marketingadmin.code") }}
                <input
                  v-model="promotionForm.code"
                  maxlength="80"
                  :placeholder="t('marketingadmin.codePlaceholder')"
                  class="code-input"
                />
              </label>
              <label>
                {{ t("marketingadmin.status") }}
                <select v-model="promotionForm.status">
                  <option
                    v-for="option in promotionStatusOptions.slice(1)"
                    :key="option.value"
                    :value="option.value"
                  >
                    {{ t(option.label) }}
                  </option>
                </select>
              </label>
              <label>
                {{ t("marketingadmin.priorityLabel") }}
                <input
                  v-model.number="promotionForm.priority"
                  type="number"
                  min="-100000"
                  max="100000"
                  step="1"
                />
              </label>
            </div>
            <label class="switch-row">
              <input v-model="promotionForm.stackable" type="checkbox" />
              <span
                ><b>{{ t("marketingadmin.stackableLabel") }}</b
                ><small>{{ t("marketingadmin.stackableHint") }}</small></span
              >
            </label>
          </fieldset>

          <fieldset>
            <legend>{{ t("marketingadmin.legendRules") }}</legend>
            <label>
              {{ t("currency.storeCurrency") }}
              <select v-model="promotionForm.currency">
                <option
                  v-for="currency in Object.values(currencyDirectory)"
                  :key="currency.code"
                  :value="currency.code"
                >
                  {{ currency.code }} · {{ currency.name }}
                </option>
              </select>
            </label>
            <div class="type-selector">
              <button
                v-for="option in promotionTypeOptions"
                :key="option.value"
                type="button"
                :class="{ active: promotionForm.type === option.value }"
                @click="promotionForm.type = option.value"
              >
                <BadgePercent :size="15" />
                <span
                  ><b>{{ t(option.label) }}</b
                  ><small>{{ t(option.hint) }}</small></span
                >
              </button>
            </div>
            <div class="form-grid rule-grid">
              <label v-if="promotionForm.type === 'percentage'">
                {{ t("marketingadmin.discountPercent") }}
                <input
                  v-model.number="promotionForm.discountPercent"
                  type="number"
                  min="0.01"
                  max="100"
                  step="0.01"
                />
              </label>
              <label
                v-if="
                  promotionForm.type === 'fixed' ||
                  promotionForm.type === 'threshold_fixed'
                "
              >
                {{ t("marketingadmin.discountAmount") }}
                <input
                  v-model="promotionForm.amountYuan"
                  inputmode="decimal"
                  min="0.01"
                  max="1000000"
                  :step="majorInputStep(promotionForm.currency)"
                />
              </label>
              <label v-if="promotionForm.type !== 'flash_price'">
                {{
                  promotionForm.type === "threshold_fixed"
                    ? t("marketingadmin.thresholdAmount")
                    : t("marketingadmin.minOrderAmount")
                }}
                <input
                  v-model="promotionForm.minAmountYuan"
                  inputmode="decimal"
                  :min="promotionForm.type === 'threshold_fixed' ? 0.01 : 0"
                  max="1000000"
                  :step="majorInputStep(promotionForm.currency)"
                />
              </label>
              <label v-if="promotionForm.type === 'percentage'">
                {{ t("marketingadmin.maxDiscount") }}
                <input
                  v-model="promotionForm.maxDiscountYuan"
                  inputmode="decimal"
                  min="0"
                  max="1000000"
                  :step="majorInputStep(promotionForm.currency)"
                />
              </label>
              <label v-if="promotionForm.type === 'flash_price'">
                {{ t("marketingadmin.unitPrice") }}
                <input
                  v-model="promotionForm.unitPriceYuan"
                  inputmode="decimal"
                  min="0"
                  max="1000000"
                  :step="majorInputStep(promotionForm.currency)"
                />
              </label>
            </div>
          </fieldset>

          <fieldset>
            <legend>{{ t("marketingadmin.legendPeriod") }}</legend>
            <div class="form-grid two-columns">
              <label>
                {{ t("marketingadmin.startsAt") }}
                <input v-model="promotionForm.startsAt" type="datetime-local" />
              </label>
              <label>
                {{ t("marketingadmin.endsAt") }}
                <input v-model="promotionForm.endsAt" type="datetime-local" />
              </label>
            </div>
          </fieldset>

          <fieldset>
            <legend>{{ t("marketingadmin.legendProducts") }}</legend>
            <div class="product-summary">
              <div>
                <b>{{
                  t("marketingadmin.selectedCount", {
                    count: promotionForm.productIDs.length,
                  })
                }}</b>
                <span>{{ t("marketingadmin.scopeOnly") }}</span>
              </div>
              <button
                v-if="promotionForm.productIDs.length"
                type="button"
                @click="promotionForm.productIDs = []"
              >
                {{ t("marketingadmin.clearSelection") }}
              </button>
            </div>
            <div v-if="selectedProducts.length" class="selected-products">
              <span v-for="entry in selectedProducts" :key="entry.id">
                {{ entry.product?.name || entry.id }}
                <button
                  type="button"
                  :aria-label="
                    t('marketingadmin.removeProduct', {
                      name: entry.product?.name || entry.id,
                    })
                  "
                  @click="removeProduct(entry.id)"
                >
                  <X :size="11" />
                </button>
              </span>
            </div>
            <div class="product-search">
              <Search :size="14" />
              <input
                v-model="productSearchInput"
                type="search"
                :placeholder="t('marketingadmin.productSearchPlaceholder')"
                @keydown.enter.prevent="searchProducts"
              />
              <button
                v-if="productAppliedSearch"
                type="button"
                @click="clearProductSearch"
              >
                {{ t("marketingadmin.clear") }}
              </button>
              <button type="button" @click="searchProducts">
                {{ t("marketingadmin.searchProducts") }}
              </button>
            </div>
            <div v-if="productError" class="product-error">
              <AlertCircle :size="14" />{{ productError }}
              <button type="button" @click="loadProducts(true)">
                {{ t("marketingadmin.retry") }}
              </button>
            </div>
            <div class="product-actions">
              <span>{{
                t("marketingadmin.productResultCount", {
                  count: products.length,
                  total: productTotal,
                })
              }}</span>
              <button
                v-if="products.length"
                type="button"
                @click="addVisibleProducts"
              >
                {{ t("marketingadmin.selectVisible") }}
              </button>
            </div>
            <div
              v-if="productLoading && !products.length"
              class="product-loading"
            >
              <LoaderCircle class="spinning" :size="18" />{{
                t("marketingadmin.loadingProducts")
              }}
            </div>
            <div
              v-else-if="!products.length && !productError"
              class="product-loading"
            >
              <PackageSearch :size="18" />{{ t("marketingadmin.noProducts") }}
            </div>
            <div v-else class="product-selector">
              <label v-for="product in products" :key="product.id">
                <input
                  type="checkbox"
                  :checked="promotionForm.productIDs.includes(product.id)"
                  @change="toggleProduct(product.id)"
                />
                <span>
                  <b>{{ product.name }}</b>
                  <small>
                    {{
                      product.category?.name ||
                      t("marketingadmin.uncategorized")
                    }}
                    · {{ formatMoney(product.price, product.currency) }} ·
                    {{ product.status }}
                  </small>
                </span>
                <code>{{ product.id }}</code>
              </label>
            </div>
            <button
              v-if="canLoadMoreProducts"
              class="load-more-products"
              type="button"
              :disabled="productLoading"
              @click="loadMoreProducts"
            >
              <LoaderCircle v-if="productLoading" class="spinning" :size="13" />
              <Plus v-else :size="13" />{{ t("marketingadmin.loadMore") }}
            </button>
          </fieldset>

          <fieldset>
            <legend>{{ t("marketingadmin.legendAudit") }}</legend>
            <label>
              {{ t("marketingadmin.operationReason") }}
              <textarea
                v-model="promotionForm.reason"
                maxlength="500"
                :placeholder="t('marketingadmin.reasonPlaceholder')"
              ></textarea>
            </label>
          </fieldset>

          <footer>
            <button type="button" :disabled="saving" @click="closeModal">
              {{ t("marketingadmin.cancel") }}
            </button>
            <button
              class="primary-button"
              type="submit"
              :disabled="saving || productLoading"
            >
              <LoaderCircle v-if="saving" class="spinning" :size="14" />
              <Check v-else :size="14" />{{
                editingID
                  ? t("marketingadmin.savePromotion")
                  : t("marketingadmin.createPromotionBtn")
              }}
            </button>
          </footer>
        </form>

        <form v-else class="marketing-form" @submit.prevent="submitCoupon">
          <div v-if="formError" class="form-alert error-notice">
            <AlertCircle :size="15" />{{ formError }}
          </div>

          <fieldset>
            <legend>{{ t("marketingadmin.legendCouponInfo") }}</legend>
            <div class="form-grid two-columns">
              <label>
                {{ t("marketingadmin.couponCode") }}
                <input
                  v-model="couponForm.code"
                  maxlength="80"
                  :placeholder="t('marketingadmin.couponCodePlaceholder')"
                  class="code-input"
                  autofocus
                />
              </label>
              <label>
                {{ t("marketingadmin.couponType") }}
                <select v-model="couponForm.type">
                  <option value="fixed">
                    {{ t("marketingadmin.fixedAmount") }}
                  </option>
                  <option value="percentage">
                    {{ t("marketingadmin.percentage") }}
                  </option>
                </select>
              </label>
              <label>
                {{ t("currency.storeCurrency") }}
                <select v-model="couponForm.currency">
                  <option
                    v-for="currency in Object.values(currencyDirectory)"
                    :key="currency.code"
                    :value="currency.code"
                  >
                    {{ currency.code }} · {{ currency.name }}
                  </option>
                </select>
              </label>
              <label v-if="couponForm.type === 'fixed'">
                {{ t("marketingadmin.couponAmount") }}
                <input
                  v-model="couponForm.valueYuan"
                  inputmode="decimal"
                  min="0.01"
                  max="1000000"
                  :step="majorInputStep(couponForm.currency)"
                />
              </label>
              <label v-else>
                {{ t("marketingadmin.discountPercent") }}
                <input
                  v-model.number="couponForm.discountPercent"
                  type="number"
                  min="0.01"
                  max="100"
                  step="0.01"
                />
              </label>
              <label>
                {{ t("marketingadmin.minOrderAmount") }}
                <input
                  v-model="couponForm.minAmountYuan"
                  inputmode="decimal"
                  min="0"
                  max="1000000"
                  :step="majorInputStep(couponForm.currency)"
                />
              </label>
              <label>
                {{ t("marketingadmin.usageLimit") }}
                <input
                  v-model.number="couponForm.usageLimit"
                  type="number"
                  min="0"
                  max="9007199254740991"
                  step="1"
                />
                <small v-if="editingID">{{
                  t("marketingadmin.usedCount", { count: couponForm.usedCount })
                }}</small>
              </label>
            </div>
            <label class="switch-row">
              <input v-model="couponForm.enabled" type="checkbox" />
              <span
                ><b>{{ t("marketingadmin.enableCoupon") }}</b
                ><small>{{ t("marketingadmin.enableCouponHint") }}</small></span
              >
            </label>
          </fieldset>

          <fieldset>
            <legend>{{ t("marketingadmin.legendPeriod") }}</legend>
            <div class="form-grid two-columns">
              <label>
                {{ t("marketingadmin.startsAt") }}
                <input v-model="couponForm.startsAt" type="datetime-local" />
              </label>
              <label>
                {{ t("marketingadmin.endsAt") }}
                <input v-model="couponForm.endsAt" type="datetime-local" />
              </label>
            </div>
          </fieldset>

          <fieldset>
            <legend>{{ t("marketingadmin.legendAudit") }}</legend>
            <label>
              {{ t("marketingadmin.operationReason") }}
              <textarea
                v-model="couponForm.reason"
                maxlength="500"
                :placeholder="t('marketingadmin.reasonPlaceholder')"
              ></textarea>
            </label>
          </fieldset>

          <footer>
            <button type="button" :disabled="saving" @click="closeModal">
              {{ t("marketingadmin.cancel") }}
            </button>
            <button class="primary-button" type="submit" :disabled="saving">
              <LoaderCircle v-if="saving" class="spinning" :size="14" />
              <Check v-else :size="14" />{{
                editingID
                  ? t("marketingadmin.saveCoupon")
                  : t("marketingadmin.createCouponBtn")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>
  </section>
</template>

<style scoped>
.marketing-shell {
  display: grid;
  gap: 12px;
}

.marketing-top {
  min-height: 58px;
  padding: 0 12px 0 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  overflow: hidden;
}

.marketing-tabs {
  min-width: 0;
  align-self: stretch;
  display: flex;
  align-items: end;
  gap: 4px;
}

.marketing-tabs button {
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

.marketing-tabs button.active {
  border-bottom-color: var(--text);
  color: var(--text);
}

.marketing-tabs button span {
  padding: 2px 5px;
  border-radius: 8px;
  background: var(--soft);
  font-size: 7px;
}

.marketing-panel {
  min-width: 0;
  overflow: hidden;
}

.marketing-toolbar {
  min-height: 58px;
  padding: 10px 13px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.marketing-search {
  width: min(430px, 100%);
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

.marketing-search input {
  min-width: 0;
  flex: 1;
  border: 0;
  outline: none;
  background: transparent;
  font-size: 9px;
}

.marketing-search button,
.marketing-filters button,
.marketing-filters select {
  height: 28px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}

.marketing-search button {
  padding: 0 9px;
  border-top: 0;
  border-right: 0;
  border-bottom: 0;
  border-radius: 0;
  display: flex;
  align-items: center;
  gap: 4px;
}

.marketing-filters {
  display: flex;
  align-items: center;
  gap: 6px;
}

.marketing-filters select {
  min-width: 112px;
  padding: 0 8px;
}

.marketing-filters button {
  padding: 0 9px;
  display: flex;
  align-items: center;
  gap: 5px;
}

.marketing-filters button:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.marketing-notice,
.form-alert,
.product-error {
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

.error-notice,
.product-error {
  background: color-mix(in srgb, var(--danger) 9%, transparent);
  color: var(--danger);
}

.marketing-notice button,
.product-error button {
  margin-left: auto;
  border: 0;
  background: transparent;
  color: inherit;
  font-size: 8px;
  font-weight: 700;
}

.marketing-state {
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

.marketing-state strong {
  color: var(--text);
  font-size: 11px;
}

.marketing-table-wrap {
  width: 100%;
  min-height: 390px;
  overflow-x: auto;
}

.marketing-table {
  width: 100%;
  min-width: 980px;
  border-collapse: collapse;
}

.marketing-table th,
.marketing-table td {
  padding: 13px 14px;
  border-bottom: 1px solid var(--line);
  text-align: left;
  vertical-align: middle;
}

.marketing-table th {
  background: var(--surface-2);
  color: var(--muted);
  font-size: 7px;
  font-weight: 600;
  letter-spacing: 0.04em;
}

.marketing-table td {
  font-size: 8px;
}

.marketing-table td > b,
.marketing-table td > time,
.marketing-table td > small {
  display: block;
}

.marketing-table td > b,
.marketing-table td > time {
  font-size: 8px;
  font-weight: 600;
}

.marketing-table td > small,
.record-primary code {
  margin-top: 4px;
  color: var(--muted);
  font-size: 7px;
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

.record-primary b {
  max-width: 190px;
  overflow: hidden;
  font-size: 9px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.record-primary code {
  max-width: 190px;
  overflow: hidden;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rule-title {
  color: var(--text);
}

.state-badge {
  width: fit-content;
  padding: 3px 7px;
  border-radius: 10px;
  display: block;
  background: var(--soft);
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
}

.state-active {
  background: color-mix(in srgb, var(--success) 11%, transparent);
  color: var(--success);
}

.state-paused {
  background: color-mix(in srgb, var(--warn) 12%, transparent);
  color: var(--warn);
}

.state-archived {
  opacity: 0.7;
}

.usage-track {
  width: 90px;
  height: 3px;
  margin-top: 6px;
  border-radius: 3px;
  overflow: hidden;
  background: var(--soft);
}

.usage-track i {
  height: 100%;
  display: block;
  border-radius: inherit;
  background: var(--text);
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

.marketing-pagination {
  min-height: 53px;
  padding: 9px 13px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: var(--muted);
  font-size: 8px;
}

.marketing-pagination > div {
  display: flex;
  gap: 4px;
}

.marketing-pagination button,
.marketing-pagination select {
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

.marketing-pagination button.active {
  background: var(--dark);
  color: var(--dark-text);
}

.marketing-pagination button:disabled {
  cursor: not-allowed;
  opacity: 0.4;
}

.marketing-pagination select {
  padding: 0 5px;
}

.marketing-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 120;
  padding: 24px;
  display: flex;
  justify-content: flex-end;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(2px);
}

.marketing-modal {
  width: min(720px, 100%);
  height: 100%;
  border: 1px solid var(--line);
  border-radius: 10px;
  overflow-y: auto;
  background: var(--surface);
  color: var(--text);
  box-shadow: var(--shadow);
}

.marketing-modal > header {
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

.marketing-modal h2 {
  margin: 5px 0 3px;
  font-size: 17px;
  letter-spacing: -0.03em;
}

.marketing-modal header p {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
}

.marketing-modal > header > button {
  width: 31px;
  height: 31px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: grid;
  place-items: center;
  background: var(--surface);
}

.marketing-form {
  padding: 5px 20px 20px;
}

.marketing-form fieldset {
  margin: 0;
  padding: 18px 0;
  border: 0;
  border-bottom: 1px solid var(--line);
}

.marketing-form legend {
  margin-bottom: 13px;
  padding: 0;
  font-size: 10px;
  font-weight: 700;
}

.form-grid {
  display: grid;
  gap: 12px;
}

.two-columns,
.rule-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.marketing-form label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: var(--muted);
  font-size: 8px;
  font-weight: 600;
}

.marketing-form input:not([type="checkbox"]),
.marketing-form select,
.marketing-form textarea {
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

.marketing-form input:focus,
.marketing-form select:focus,
.marketing-form textarea:focus {
  border-color: var(--text);
}

.marketing-form textarea {
  min-height: 82px;
  resize: vertical;
  line-height: 1.55;
}

.marketing-form small {
  color: var(--muted);
  font-size: 7px;
  font-weight: 400;
}

.code-input {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace !important;
  text-transform: uppercase;
}

.switch-row {
  margin-top: 13px;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 6px;
  flex-direction: row !important;
  align-items: center;
  gap: 9px !important;
  background: var(--surface-2);
}

.switch-row input {
  width: 16px;
  height: 16px;
  accent-color: var(--text);
}

.switch-row span {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.switch-row b {
  color: var(--text);
  font-size: 8px;
}

.type-selector {
  margin-bottom: 12px;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 6px;
}

.type-selector button {
  min-height: 64px;
  padding: 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: flex;
  align-items: flex-start;
  gap: 7px;
  background: var(--surface);
  color: var(--muted);
  text-align: left;
}

.type-selector button.active {
  border-color: var(--text);
  background: var(--surface-2);
  color: var(--text);
  box-shadow: inset 0 0 0 1px var(--text);
}

.type-selector button span {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.type-selector b {
  font-size: 8px;
}

.type-selector small {
  line-height: 1.4;
}

.product-summary,
.product-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.product-summary > div {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.product-summary b {
  font-size: 9px;
}

.product-summary span,
.product-actions {
  color: var(--muted);
  font-size: 7px;
}

.product-summary button,
.product-actions button,
.load-more-products,
.product-search button {
  height: 28px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  color: var(--text);
  font-size: 7px;
}

.selected-products {
  margin-top: 10px;
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
}

.selected-products > span {
  max-width: 230px;
  padding: 5px 5px 5px 8px;
  border-radius: 11px;
  display: flex;
  align-items: center;
  gap: 5px;
  overflow: hidden;
  background: var(--soft);
  color: var(--text);
  font-size: 7px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.selected-products button {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
  padding: 0;
  border: 0;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: var(--surface);
}

.product-search {
  height: 34px;
  margin: 11px 0 8px;
  padding-left: 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  background: var(--surface-2);
}

.product-search input {
  min-width: 0 !important;
  min-height: auto !important;
  flex: 1;
  padding: 0 !important;
  border: 0 !important;
  background: transparent !important;
}

.product-search button {
  height: 32px;
  border-top: 0;
  border-right: 0;
  border-bottom: 0;
  border-radius: 0;
}

.product-error {
  margin: 8px 0;
}

.product-actions {
  min-height: 31px;
}

.product-loading {
  min-height: 90px;
  border: 1px dashed var(--line);
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  color: var(--muted);
  font-size: 8px;
}

.product-selector {
  max-height: 270px;
  border: 1px solid var(--line);
  border-radius: 6px;
  overflow-y: auto;
}

.product-selector label {
  min-height: 48px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--line);
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 9px;
  cursor: pointer;
}

.product-selector label:last-child {
  border-bottom: 0;
}

.product-selector label:hover {
  background: var(--surface-2);
}

.product-selector input {
  width: 15px;
  height: 15px;
  accent-color: var(--text);
}

.product-selector span {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.product-selector b {
  overflow: hidden;
  color: var(--text);
  font-size: 8px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.product-selector code {
  max-width: 170px;
  overflow: hidden;
  color: var(--muted);
  font:
    500 7px ui-monospace,
    SFMono-Regular,
    Menlo,
    monospace;
  text-overflow: ellipsis;
}

.load-more-products {
  width: 100%;
  margin-top: 7px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
}

.marketing-form > footer {
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

.marketing-form > footer > button:first-child {
  height: 36px;
  padding: 0 14px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  font-size: 8px;
}

.marketing-form > footer .primary-button {
  height: 36px;
  padding: 0 14px;
  font-size: 8px;
}

.spinning {
  animation: marketing-spin 0.8s linear infinite;
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

@keyframes marketing-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 800px) {
  .marketing-toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .marketing-search {
    width: 100%;
  }

  .marketing-filters {
    justify-content: flex-end;
  }

  .marketing-modal-backdrop {
    padding: 10px;
  }

  .type-selector {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 620px) {
  .marketing-top {
    padding-left: 10px;
  }

  .marketing-top > .primary-button {
    padding: 0 9px;
    white-space: nowrap;
  }

  .marketing-tabs button {
    padding: 0 7px;
  }

  .marketing-table-wrap {
    padding: 9px;
  }

  .marketing-table {
    min-width: 0;
  }

  .marketing-table thead {
    display: none;
  }

  .marketing-table tbody,
  .marketing-table tr,
  .marketing-table td {
    display: block;
    width: 100%;
  }

  .marketing-table tr {
    margin-bottom: 9px;
    padding: 7px 10px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--surface);
  }

  .marketing-table td {
    min-height: 35px;
    padding: 8px 0 8px 96px;
    border-bottom: 1px solid var(--line);
    position: relative;
  }

  .marketing-table td::before {
    content: attr(data-label);
    position: absolute;
    left: 0;
    top: 10px;
    color: var(--muted);
    font-size: 7px;
  }

  .marketing-table td:last-child {
    border-bottom: 0;
  }

  .record-primary {
    min-width: 0;
  }

  .record-actions {
    text-align: left !important;
  }

  .marketing-pagination {
    align-items: flex-start;
    flex-direction: column;
  }

  .marketing-pagination > div {
    width: 100%;
    overflow-x: auto;
  }

  .marketing-modal-backdrop {
    padding: 0;
  }

  .marketing-modal {
    border-radius: 0;
  }

  .marketing-modal > header,
  .marketing-form {
    padding-right: 14px;
    padding-left: 14px;
  }

  .two-columns,
  .rule-grid {
    grid-template-columns: 1fr;
  }

  .type-selector {
    grid-template-columns: 1fr;
  }

  .type-selector button {
    min-height: 54px;
  }

  .product-selector label {
    grid-template-columns: auto minmax(0, 1fr);
  }

  .product-selector code {
    display: none;
  }

  .marketing-form > footer {
    margin-right: -14px;
    margin-left: -14px;
    padding-right: 14px;
    padding-left: 14px;
  }
}
</style>
