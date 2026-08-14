<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  AlertTriangle,
  Archive,
  Boxes,
  CheckCircle2,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  CircleSlash2,
  FileKey2,
  History,
  KeyRound,
  LoaderCircle,
  LockKeyhole,
  PackageCheck,
  RefreshCw,
  RotateCcw,
  Search,
  ShieldCheck,
  Upload,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";

type InventoryTab = "stock" | "cards" | "batches";
type CardStatus = "available" | "locked" | "sold" | "disabled";

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface InventoryVariant {
  id: string;
  sku: string;
  name: string;
  status: "active" | "inactive";
  available: number;
  locked: number;
  sold: number;
  disabled: number;
  total: number;
  purchase_limit: number;
}

interface InventoryProduct {
  id: string;
  name: string;
  slug: string;
  status: "draft" | "on_sale" | "off_sale";
  inventory_mode: "local" | "supplier";
  delivery_type: "auto" | "manual";
  available: number;
  locked: number;
  sold: number;
  disabled: number;
  total: number;
  variants: InventoryVariant[];
}

interface InventoryCard {
  id: string;
  product_id: string;
  variant_id?: string | null;
  product_name: string;
  variant_name: string;
  variant_sku: string;
  preview: string;
  status: CardStatus;
  order_id?: string | null;
  sold_at?: string | null;
  created_at: string;
  updated_at: string;
}

interface InventoryBatch {
  id: string;
  product_id: string;
  variant_id?: string | null;
  batch_no: string;
  source: string;
  total_count: number;
  valid_count: number;
  invalid_count: number;
  imported_by?: string | null;
  importer_name: string;
  product_name: string;
  variant_name: string;
  variant_sku: string;
  expires_at?: string | null;
  created_at: string;
}

interface ImportResult {
  batch: InventoryBatch;
  imported: number;
  invalid: number;
}

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const authStore = useAuthStore();
const canManage = computed(() => authStore.hasPermission("inventory.manage"));
const activeTab = ref<InventoryTab>("stock");
const notice = ref("");

const products = ref<InventoryProduct[]>([]);
const productTotal = ref(0);
const productPage = ref(1);
const productPageSize = ref(20);
const productSearch = ref("");
const productQuery = ref("");
const productStatus = ref("");
const inventoryMode = ref("");
const productsLoading = ref(false);
const productsError = ref("");
const expandedProducts = ref(new Set<string>());
let productRequest = 0;

const cards = ref<InventoryCard[]>([]);
const cardTotal = ref(0);
const cardPage = ref(1);
const cardPageSize = ref(20);
const cardSearch = ref("");
const cardQuery = ref("");
const cardStatus = ref("");
const cardProduct = ref("");
const cardVariant = ref("");
const cardsLoading = ref(false);
const cardsError = ref("");
const selectedCardIDs = ref<string[]>([]);
const cardBatchSaving = ref(false);
let cardRequest = 0;

const batches = ref<InventoryBatch[]>([]);
const batchTotal = ref(0);
const batchPage = ref(1);
const batchPageSize = ref(20);
const batchSearch = ref("");
const batchQuery = ref("");
const batchProduct = ref("");
const batchInvalid = ref("");
const batchesLoading = ref(false);
const batchesError = ref("");
let batchRequest = 0;

const inventoryOptions = ref<InventoryProduct[]>([]);
const inventoryOptionTotal = ref(0);
const inventoryOptionSearch = ref("");
const inventoryOptionsLoading = ref(false);

const importOpen = ref(false);
const importing = ref(false);
const importError = ref("");
const importForm = reactive({
  productID: "",
  variantID: "",
  cards: "",
  reason: "",
});

const statusTarget = ref<InventoryCard | null>(null);
const targetStatus = ref<"available" | "disabled">("disabled");
const statusReason = ref("");
const statusError = ref("");
const statusSaving = ref(false);

const productPageCount = computed(() =>
  Math.max(1, Math.ceil(productTotal.value / productPageSize.value)),
);
const productPages = computed(() =>
  pageWindow(productPage.value, productPageCount.value),
);
const cardPageCount = computed(() =>
  Math.max(1, Math.ceil(cardTotal.value / cardPageSize.value)),
);
const cardPages = computed(() =>
  pageWindow(cardPage.value, cardPageCount.value),
);
const selectableCards = computed(() =>
  cards.value.filter(
    (item) => item.status === "available" || item.status === "disabled",
  ),
);
const selectedCards = computed(() =>
  selectableCards.value.filter((item) =>
    selectedCardIDs.value.includes(item.id),
  ),
);
const allSelectableCardsSelected = computed(
  () =>
    selectableCards.value.length > 0 &&
    selectableCards.value.every((item) =>
      selectedCardIDs.value.includes(item.id),
    ),
);
const canBatchDisableCards = computed(
  () =>
    selectedCards.value.length > 0 &&
    selectedCards.value.every((item) => item.status === "available"),
);
const canBatchRestoreCards = computed(
  () =>
    selectedCards.value.length > 0 &&
    selectedCards.value.every((item) => item.status === "disabled"),
);
const batchPageCount = computed(() =>
  Math.max(1, Math.ceil(batchTotal.value / batchPageSize.value)),
);
const batchPages = computed(() =>
  pageWindow(batchPage.value, batchPageCount.value),
);
const selectedInventoryProduct = computed(
  () =>
    inventoryOptions.value.find((item) => item.id === importForm.productID) ||
    null,
);
const selectedCardProduct = computed(
  () =>
    inventoryOptions.value.find((item) => item.id === cardProduct.value) ||
    null,
);
const activeImportVariants = computed(
  () =>
    selectedInventoryProduct.value?.variants.filter(
      (item) => item.status === "active",
    ) || [],
);
const importLineStats = computed(() => {
  const lines = importForm.cards.split(/\r?\n/);
  const nonEmpty = lines.map((item) => item.trim()).filter(Boolean);
  return {
    total: lines.length === 1 && !lines[0] ? 0 : lines.length,
    nonEmpty: nonEmpty.length,
    unique: new Set(nonEmpty).size,
  };
});
const availableTotal = computed(() =>
  products.value.reduce((sum, item) => sum + Number(item.available || 0), 0),
);
const lockedTotal = computed(() =>
  products.value.reduce((sum, item) => sum + Number(item.locked || 0), 0),
);
const disabledTotal = computed(() =>
  products.value.reduce((sum, item) => sum + Number(item.disabled || 0), 0),
);

const cardStatusLabels: Record<string, string> = {
  available: "inventory.statusAvailable",
  locked: "inventory.statusLocked",
  sold: "inventory.statusSold",
  disabled: "inventory.statusDisabled",
};
const productStatusLabels: Record<string, string> = {
  draft: "inventory.statusDraft",
  on_sale: "inventory.statusOnSale",
  off_sale: "inventory.statusOffSale",
};
const sourceLabels: Record<string, string> = {
  manual_import: "inventory.sourceManualImport",
  supplier_api: "inventory.sourceSupplierApi",
  migration: "inventory.sourceMigration",
};

function statusLabel(map: Record<string, string>, value: string) {
  const key = map[value];
  return key ? t(key) : value;
}

function pageWindow(current: number, total: number) {
  const start = Math.max(1, Math.min(current - 2, total - 4));
  const end = Math.min(total, start + 4);
  return Array.from({ length: end - start + 1 }, (_, index) => start + index);
}

function apiMessage(error: unknown, fallback: string) {
  const failure = error as { response?: { data?: { message?: string } } };
  return failure.response?.data?.message || fallback;
}

function validReason(value: string) {
  const length = [...value.trim()].length;
  return length >= 4 && length <= 500;
}

function reasonHeaders(value: string) {
  return { "X-Change-Reason": value.trim() };
}

function formatNumber(value?: number) {
  return Number(value || 0).toLocaleString("zh-CN");
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
  return `${value.slice(0, 8)}…${value.slice(-4)}`;
}

function batchState(item: InventoryBatch) {
  if (item.valid_count === 0)
    return { label: t("inventory.batchNoValid"), className: "failed" };
  if (item.invalid_count > 0)
    return {
      label: t("inventory.batchPartialSkipped"),
      className: "warning",
    };
  return { label: t("inventory.batchAllImported"), className: "success" };
}

function stockHealth(item: InventoryProduct) {
  if (item.inventory_mode === "supplier")
    return { label: t("inventory.supplierSync"), className: "supplier" };
  if (item.status !== "on_sale")
    return {
      label: statusLabel(productStatusLabels, item.status),
      className: "muted",
    };
  if (item.available === 0)
    return { label: t("inventory.stockSoldOut"), className: "danger" };
  if (item.available < 10)
    return { label: t("inventory.stockLow"), className: "warning" };
  return { label: t("inventory.stockSufficient"), className: "success" };
}

async function loadProducts() {
  const request = ++productRequest;
  productsLoading.value = true;
  productsError.value = "";
  try {
    const { data } = await adminApi.get("/inventory/products", {
      params: {
        page: productPage.value,
        page_size: productPageSize.value,
        ...(productQuery.value ? { q: productQuery.value } : {}),
        ...(productStatus.value ? { status: productStatus.value } : {}),
        ...(inventoryMode.value ? { inventory_mode: inventoryMode.value } : {}),
      },
    });
    if (request !== productRequest) return;
    const payload = data.data as PagePayload<InventoryProduct>;
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
    productsError.value = apiMessage(error, t("inventory.errLoadProducts"));
  } finally {
    if (request === productRequest) productsLoading.value = false;
  }
}

async function loadCards() {
  const request = ++cardRequest;
  cardsLoading.value = true;
  cardsError.value = "";
  try {
    const { data } = await adminApi.get("/cards", {
      params: {
        page: cardPage.value,
        page_size: cardPageSize.value,
        ...(cardQuery.value ? { q: cardQuery.value } : {}),
        ...(cardStatus.value ? { status: cardStatus.value } : {}),
        ...(cardProduct.value ? { product_id: cardProduct.value } : {}),
        ...(cardVariant.value ? { variant_id: cardVariant.value } : {}),
      },
    });
    if (request !== cardRequest) return;
    const payload = data.data as PagePayload<InventoryCard>;
    cards.value = Array.isArray(payload?.items) ? payload.items : [];
    selectedCardIDs.value = [];
    cardTotal.value = Number(payload?.total || 0);
    cardPage.value = Number(payload?.page || cardPage.value);
    cardPageSize.value = Number(payload?.page_size || cardPageSize.value);
    if (cardPage.value > cardPageCount.value && cardPage.value > 1) {
      cardPage.value = cardPageCount.value;
      await loadCards();
    }
  } catch (error: unknown) {
    if (request !== cardRequest) return;
    cards.value = [];
    cardTotal.value = 0;
    cardsError.value = apiMessage(error, t("inventory.errLoadCards"));
  } finally {
    if (request === cardRequest) cardsLoading.value = false;
  }
}

function toggleCardSelection(id: string) {
  if (!canManage.value) return;
  selectedCardIDs.value = selectedCardIDs.value.includes(id)
    ? selectedCardIDs.value.filter((value) => value !== id)
    : [...selectedCardIDs.value, id];
}

function toggleAllSelectableCards() {
  if (!canManage.value) return;
  selectedCardIDs.value = allSelectableCardsSelected.value
    ? []
    : selectableCards.value.map((item) => item.id);
}

async function batchCardStatus(status: "available" | "disabled") {
  if (!canManage.value) return;
  if (!selectedCardIDs.value.length || cardBatchSaving.value) return;
  const reason = window.prompt(t("inventory.batchReasonPrompt"), "")?.trim();
  if (!reason) return;
  if (!validReason(reason)) {
    cardsError.value = t("inventory.errStatusReasonLength");
    return;
  }
  cardBatchSaving.value = true;
  cardsError.value = "";
  try {
    const { data } = await adminApi.patch(
      "/cards/batch-status",
      { ids: selectedCardIDs.value, status },
      { headers: reasonHeaders(reason) },
    );
    notice.value = t("inventory.batchStatusChanged", {
      count: Number(data.data?.changed || 0),
    });
    selectedCardIDs.value = [];
    await Promise.all([loadCards(), loadProducts()]);
  } catch (error: unknown) {
    cardsError.value = apiMessage(error, t("inventory.errBatchStatusChange"));
  } finally {
    cardBatchSaving.value = false;
  }
}

async function loadBatches() {
  const request = ++batchRequest;
  batchesLoading.value = true;
  batchesError.value = "";
  try {
    const { data } = await adminApi.get("/operations/inventory-batches", {
      params: {
        page: batchPage.value,
        page_size: batchPageSize.value,
        ...(batchQuery.value ? { q: batchQuery.value } : {}),
        ...(batchProduct.value ? { product_id: batchProduct.value } : {}),
        ...(batchInvalid.value ? { has_invalid: batchInvalid.value } : {}),
      },
    });
    if (request !== batchRequest) return;
    const payload = data.data as PagePayload<InventoryBatch>;
    batches.value = Array.isArray(payload?.items) ? payload.items : [];
    batchTotal.value = Number(payload?.total || 0);
    batchPage.value = Number(payload?.page || batchPage.value);
    batchPageSize.value = Number(payload?.page_size || batchPageSize.value);
    if (batchPage.value > batchPageCount.value && batchPage.value > 1) {
      batchPage.value = batchPageCount.value;
      await loadBatches();
    }
  } catch (error: unknown) {
    if (request !== batchRequest) return;
    batches.value = [];
    batchTotal.value = 0;
    batchesError.value = apiMessage(error, t("inventory.errLoadBatches"));
  } finally {
    if (request === batchRequest) batchesLoading.value = false;
  }
}

async function loadInventoryOptions(search = inventoryOptionSearch.value) {
  inventoryOptionsLoading.value = true;
  try {
    const { data } = await adminApi.get("/inventory/products", {
      params: {
        page: 1,
        page_size: 100,
        ...(search.trim() ? { q: search.trim() } : {}),
      },
    });
    const payload = data.data as PagePayload<InventoryProduct>;
    inventoryOptions.value = Array.isArray(payload?.items) ? payload.items : [];
    inventoryOptionTotal.value = Number(payload?.total || 0);
  } catch {
    inventoryOptions.value = [];
    inventoryOptionTotal.value = 0;
  } finally {
    inventoryOptionsLoading.value = false;
  }
}

async function loadActive() {
  if (activeTab.value === "stock") return loadProducts();
  if (activeTab.value === "cards") return loadCards();
  return loadBatches();
}

async function searchProducts() {
  productQuery.value = productSearch.value.trim();
  productPage.value = 1;
  await loadProducts();
}

async function searchCards() {
  cardQuery.value = cardSearch.value.trim();
  cardPage.value = 1;
  await loadCards();
}

async function searchBatches() {
  batchQuery.value = batchSearch.value.trim();
  batchPage.value = 1;
  await loadBatches();
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

async function changeCardPage(target: number) {
  if (target < 1 || target > cardPageCount.value || target === cardPage.value)
    return;
  cardPage.value = target;
  await loadCards();
}

async function changeBatchPage(target: number) {
  if (target < 1 || target > batchPageCount.value || target === batchPage.value)
    return;
  batchPage.value = target;
  await loadBatches();
}

function toggleProduct(id: string) {
  const next = new Set(expandedProducts.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  expandedProducts.value = next;
}

function onCardProductChange() {
  cardVariant.value = "";
  cardPage.value = 1;
  void loadCards();
}

function resetImport() {
  Object.assign(importForm, {
    productID: "",
    variantID: "",
    cards: "",
    reason: "",
  });
  inventoryOptionSearch.value = "";
  importError.value = "";
}

async function openImport() {
  if (!canManage.value) return;
  resetImport();
  importOpen.value = true;
  await loadInventoryOptions();
  const firstLocal = inventoryOptions.value.find(
    (item) => item.inventory_mode === "local",
  );
  if (firstLocal) importForm.productID = firstLocal.id;
}

function closeImport() {
  if (importing.value) return;
  importForm.cards = "";
  importOpen.value = false;
  resetImport();
}

function onImportProductChange() {
  importForm.variantID = "";
}

async function submitImport() {
  if (!canManage.value) return;
  importError.value = "";
  const product = selectedInventoryProduct.value;
  if (!product || product.inventory_mode !== "local") {
    importError.value = t("inventory.errSelectLocalProduct");
    return;
  }
  if (activeImportVariants.value.length && !importForm.variantID) {
    importError.value = t("inventory.errSelectVariant");
    return;
  }
  if (
    importLineStats.value.nonEmpty < 1 ||
    importLineStats.value.total > 5000
  ) {
    importError.value = t("inventory.errCardLineCount");
    return;
  }
  if (!validReason(importForm.reason)) {
    importError.value = t("inventory.errImportReasonLength");
    return;
  }
  importing.value = true;
  try {
    const rawCards = importForm.cards.split(/\r?\n/);
    const { data } = await adminApi.post(
      "/cards/import",
      {
        product_id: importForm.productID,
        variant_id: importForm.variantID || null,
        cards: rawCards,
      },
      { headers: reasonHeaders(importForm.reason), timeout: 30_000 },
    );
    const result = data.data as ImportResult;
    importForm.cards = "";
    notice.value = t("inventory.importDone", {
      batchNo: result.batch.batch_no,
      imported: result.imported,
      invalid: result.invalid,
    });
    importing.value = false;
    closeImport();
    await Promise.all([
      loadProducts(),
      loadCards(),
      loadBatches(),
      loadInventoryOptions(),
    ]);
  } catch (error: unknown) {
    importError.value = apiMessage(error, t("inventory.errImport"));
  } finally {
    importing.value = false;
  }
}

function openStatusChange(item: InventoryCard) {
  if (!canManage.value) return;
  statusTarget.value = item;
  targetStatus.value = item.status === "available" ? "disabled" : "available";
  statusReason.value = "";
  statusError.value = "";
}

function closeStatusChange() {
  if (statusSaving.value) return;
  statusTarget.value = null;
  statusReason.value = "";
  statusError.value = "";
}

async function submitStatusChange() {
  if (!canManage.value) return;
  if (!statusTarget.value || statusSaving.value) return;
  if (!validReason(statusReason.value)) {
    statusError.value = t("inventory.errStatusReasonLength");
    return;
  }
  statusSaving.value = true;
  let saved = false;
  try {
    await adminApi.patch(
      `/cards/${encodeURIComponent(statusTarget.value.id)}/status`,
      { status: targetStatus.value },
      { headers: reasonHeaders(statusReason.value) },
    );
    notice.value = t("inventory.statusChanged", {
      preview: statusTarget.value.preview,
      action:
        targetStatus.value === "disabled"
          ? t("inventory.actionDisable")
          : t("inventory.actionRestored"),
    });
    saved = true;
    await Promise.all([loadCards(), loadProducts()]);
  } catch (error: unknown) {
    statusError.value = apiMessage(error, t("inventory.errStatusChange"));
  } finally {
    statusSaving.value = false;
    if (saved) closeStatusChange();
  }
}

watch(
  () => [route.meta.defaultTab, route.query.view] as const,
  async ([defaultTab, view]) => {
    activeTab.value =
      defaultTab === "batches"
        ? "batches"
        : defaultTab === "cards"
          ? "cards"
          : view === "cards"
            ? "cards"
            : "stock";
    notice.value = "";
    await loadActive();
  },
  { immediate: true },
);

onMounted(async () => {
  if (
    (route.path === "/inventory" || route.path === "/card-secrets") &&
    route.query.import === "1"
  ) {
    await openImport();
    const query = { ...route.query };
    delete query.import;
    await router.replace({ path: route.path, query });
    return;
  }
  await loadInventoryOptions();
});
</script>

<template>
  <section class="inventory-shell">
    <div v-if="notice" class="inventory-notice success" role="status">
      <CheckCircle2 :size="17" /><span>{{ notice }}</span
      ><button :aria-label="t('inventory.closeNotice')" @click="notice = ''">
        <X :size="15" />
      </button>
    </div>

    <header class="section-heading">
      <div>
        <span class="eyebrow">{{ t("adminKicker.encryptedInventory") }}</span>
        <h2>
          {{
            activeTab === "stock"
              ? t("inventory.stockOverview")
              : activeTab === "cards"
                ? t("inventory.cardRecords")
                : t("inventory.importBatches")
          }}
        </h2>
        <p v-if="activeTab === 'stock'">{{ t("inventory.stockDesc") }}</p>
        <p v-else-if="activeTab === 'cards'">{{ t("inventory.cardsDesc") }}</p>
        <p v-else>{{ t("inventory.batchesDesc") }}</p>
      </div>
      <button v-if="canManage" class="primary-action" @click="openImport">
        <Upload :size="16" /> {{ t("inventory.encryptedImport") }}
      </button>
    </header>

    <div class="security-strip">
      <ShieldCheck :size="18" />
      <div>
        <strong>{{ t("inventory.securityTitle") }}</strong
        ><span>{{ t("inventory.securityDesc") }}</span>
      </div>
    </div>

    <template v-if="activeTab === 'stock'">
      <div class="metric-grid">
        <article>
          <PackageCheck :size="17" /><span>{{
            t("inventory.metricAvailable")
          }}</span
          ><strong>{{ formatNumber(availableTotal) }}</strong>
        </article>
        <article>
          <LockKeyhole :size="17" /><span>{{
            t("inventory.metricLocked")
          }}</span
          ><strong>{{ formatNumber(lockedTotal) }}</strong>
        </article>
        <article>
          <CircleSlash2 :size="17" /><span>{{
            t("inventory.metricDisabled")
          }}</span
          ><strong>{{ formatNumber(disabledTotal) }}</strong>
        </article>
        <article>
          <Archive :size="17" /><span>{{ t("inventory.metricMatched") }}</span
          ><strong>{{ formatNumber(productTotal) }}</strong>
        </article>
      </div>
      <div class="toolbar">
        <form class="search-box" @submit.prevent="searchProducts">
          <Search :size="15" /><input
            v-model="productSearch"
            :placeholder="t('inventory.searchProductPlaceholder')"
          /><button type="submit">{{ t("inventory.search") }}</button>
        </form>
        <select
          v-model="productStatus"
          @change="
            productPage = 1;
            loadProducts();
          "
        >
          <option value="">{{ t("inventory.filterAllSaleStatus") }}</option>
          <option value="on_sale">{{ t("inventory.statusOnSale") }}</option>
          <option value="off_sale">{{ t("inventory.statusOffSale") }}</option>
          <option value="draft">{{ t("inventory.statusDraft") }}</option>
        </select>
        <select
          v-model="inventoryMode"
          @change="
            productPage = 1;
            loadProducts();
          "
        >
          <option value="">{{ t("inventory.filterAllInventoryMode") }}</option>
          <option value="local">{{ t("inventory.localCards") }}</option>
          <option value="supplier">{{ t("inventory.supplier") }}</option>
        </select>
        <button
          class="icon-button"
          :title="t('inventory.refresh')"
          @click="loadProducts"
        >
          <RefreshCw :size="16" :class="{ spin: productsLoading }" />
        </button>
      </div>
      <div v-if="productsError" class="inventory-notice error">
        <AlertTriangle :size="17" /><span>{{ productsError }}</span
        ><button @click="loadProducts">{{ t("inventory.retry") }}</button>
      </div>
      <div class="data-card">
        <div v-if="productsLoading" class="table-state">
          <LoaderCircle :size="22" class="spin" />
          {{ t("inventory.loadingProducts") }}
        </div>
        <div v-else-if="!products.length" class="table-state">
          <Boxes :size="28" /><strong>{{ t("inventory.noProducts") }}</strong
          ><span>{{ t("inventory.noProductsHint") }}</span>
        </div>
        <div v-else class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>{{ t("inventory.colProduct") }}</th>
                <th>{{ t("inventory.colInventoryMode") }}</th>
                <th>{{ t("inventory.colAvailable") }}</th>
                <th>{{ t("inventory.colLocked") }}</th>
                <th>{{ t("inventory.colSold") }}</th>
                <th>{{ t("inventory.colDisabled") }}</th>
                <th>{{ t("inventory.colTotal") }}</th>
                <th>{{ t("inventory.colHealth") }}</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <template v-for="item in products" :key="item.id">
                <tr>
                  <td :data-label="t('inventory.colProduct')">
                    <div class="primary-cell">
                      <strong>{{ item.name }}</strong
                      ><code>{{ item.slug }}</code
                      ><span
                        >{{ statusLabel(productStatusLabels, item.status) }} ·
                        {{
                          item.delivery_type === "auto"
                            ? t("inventory.autoDelivery")
                            : t("inventory.manualDelivery")
                        }}</span
                      >
                    </div>
                  </td>
                  <td :data-label="t('inventory.colInventoryMode')">
                    <span class="mode-label">{{
                      item.inventory_mode === "local"
                        ? t("inventory.localCards")
                        : t("inventory.supplierSync")
                    }}</span>
                  </td>
                  <td :data-label="t('inventory.colAvailable')">
                    <strong class="success-text">{{
                      formatNumber(item.available)
                    }}</strong>
                  </td>
                  <td :data-label="t('inventory.colLocked')">
                    {{ formatNumber(item.locked) }}
                  </td>
                  <td :data-label="t('inventory.colSold')">
                    {{ formatNumber(item.sold) }}
                  </td>
                  <td :data-label="t('inventory.colDisabled')">
                    {{ formatNumber(item.disabled) }}
                  </td>
                  <td :data-label="t('inventory.colTotal')">
                    {{ formatNumber(item.total) }}
                  </td>
                  <td :data-label="t('inventory.colHealth')">
                    <span :class="['pill', stockHealth(item).className]">{{
                      stockHealth(item).label
                    }}</span>
                  </td>
                  <td :data-label="t('inventory.colVariants')">
                    <button
                      v-if="item.variants.length"
                      class="expand-button"
                      :title="
                        expandedProducts.has(item.id)
                          ? t('inventory.collapseVariants')
                          : t('inventory.expandVariants')
                      "
                      @click="toggleProduct(item.id)"
                    >
                      <ChevronUp
                        v-if="expandedProducts.has(item.id)"
                        :size="15"
                      /><ChevronDown v-else :size="15" />
                    </button>
                  </td>
                </tr>
                <tr
                  v-if="expandedProducts.has(item.id)"
                  class="variant-detail-row"
                >
                  <td colspan="9">
                    <div class="variant-grid">
                      <article
                        v-for="variant in item.variants"
                        :key="variant.id"
                      >
                        <div>
                          <strong>{{ variant.name }}</strong
                          ><code>{{ variant.sku }}</code>
                        </div>
                        <span :class="['variant-state', variant.status]">{{
                          variant.status === "active"
                            ? t("inventory.variantActive")
                            : t("inventory.colDisabled")
                        }}</span>
                        <dl>
                          <div>
                            <dt>{{ t("inventory.colAvailable") }}</dt>
                            <dd>{{ formatNumber(variant.available) }}</dd>
                          </div>
                          <div>
                            <dt>{{ t("inventory.colLocked") }}</dt>
                            <dd>{{ formatNumber(variant.locked) }}</dd>
                          </div>
                          <div>
                            <dt>{{ t("inventory.colSold") }}</dt>
                            <dd>{{ formatNumber(variant.sold) }}</dd>
                          </div>
                          <div>
                            <dt>{{ t("inventory.colDisabled") }}</dt>
                            <dd>{{ formatNumber(variant.disabled) }}</dd>
                          </div>
                        </dl>
                      </article>
                    </div>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
        <footer class="pager">
          <span>{{ t("inventory.productCount", { count: productTotal }) }}</span
          ><select
            v-model.number="productPageSize"
            @change="
              productPage = 1;
              loadProducts();
            "
          >
            <option :value="10">
              {{ t("inventory.perPage", { count: 10 }) }}
            </option>
            <option :value="20">
              {{ t("inventory.perPage", { count: 20 }) }}
            </option>
            <option :value="50">
              {{ t("inventory.perPage", { count: 50 }) }}
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

    <template v-else-if="activeTab === 'cards'">
      <div class="toolbar">
        <form class="search-box" @submit.prevent="searchCards">
          <Search :size="15" /><input
            v-model="cardSearch"
            :placeholder="t('inventory.searchCardPlaceholder')"
          /><button type="submit">{{ t("inventory.search") }}</button>
        </form>
        <select v-model="cardProduct" @change="onCardProductChange">
          <option value="">{{ t("inventory.filterAllProducts") }}</option>
          <option
            v-for="item in inventoryOptions"
            :key="item.id"
            :value="item.id"
          >
            {{ item.name }}
          </option>
        </select>
        <select
          v-model="cardVariant"
          :disabled="!cardProduct"
          @change="
            cardPage = 1;
            loadCards();
          "
        >
          <option value="">{{ t("inventory.filterAllVariants") }}</option>
          <option
            v-for="item in selectedCardProduct?.variants || []"
            :key="item.id"
            :value="item.id"
          >
            {{ item.sku }} · {{ item.name }}
          </option>
        </select>
        <select
          v-model="cardStatus"
          @change="
            cardPage = 1;
            loadCards();
          "
        >
          <option value="">{{ t("inventory.filterAllStatus") }}</option>
          <option value="available">
            {{ t("inventory.statusAvailable") }}
          </option>
          <option value="locked">{{ t("inventory.statusLocked") }}</option>
          <option value="sold">{{ t("inventory.statusSold") }}</option>
          <option value="disabled">{{ t("inventory.statusDisabled") }}</option>
        </select>
        <button
          class="icon-button"
          :title="t('inventory.refresh')"
          @click="loadCards"
        >
          <RefreshCw :size="16" :class="{ spin: cardsLoading }" />
        </button>
      </div>
      <p
        v-if="inventoryOptionTotal > inventoryOptions.length"
        class="filter-hint"
      >
        {{ t("inventory.filterHint") }}
      </p>
      <div v-if="cardsError" class="inventory-notice error">
        <AlertTriangle :size="17" /><span>{{ cardsError }}</span
        ><button @click="loadCards">{{ t("inventory.retry") }}</button>
      </div>
      <div class="data-card">
        <div
          v-if="canManage && selectedCardIDs.length"
          class="inventory-batch-toolbar"
        >
          <strong>{{
            t("inventory.batchSelected", { count: selectedCardIDs.length })
          }}</strong>
          <span>{{ t("inventory.batchSameStatusHint") }}</span>
          <div>
            <button
              type="button"
              class="danger"
              :disabled="!canBatchDisableCards || cardBatchSaving"
              @click="batchCardStatus('disabled')"
            >
              <CircleSlash2 :size="14" />{{ t("inventory.batchDisable") }}
            </button>
            <button
              type="button"
              :disabled="!canBatchRestoreCards || cardBatchSaving"
              @click="batchCardStatus('available')"
            >
              <RotateCcw :size="14" />{{ t("inventory.batchRestore") }}
            </button>
            <button
              type="button"
              :disabled="cardBatchSaving"
              @click="selectedCardIDs = []"
            >
              {{ t("inventory.batchClear") }}
            </button>
          </div>
        </div>
        <div v-if="cardsLoading" class="table-state">
          <LoaderCircle :size="22" class="spin" />
          {{ t("inventory.loadingCards") }}
        </div>
        <div v-else-if="!cards.length" class="table-state">
          <FileKey2 :size="28" /><strong>{{ t("inventory.noCards") }}</strong
          ><span>{{ t("inventory.noCardsHint") }}</span>
        </div>
        <div v-else class="table-wrap">
          <table>
            <thead>
              <tr>
                <th class="selection-cell">
                  <input
                    v-if="canManage"
                    type="checkbox"
                    :checked="allSelectableCardsSelected"
                    :aria-label="t('inventory.batchSelectPage')"
                    @change="toggleAllSelectableCards"
                  />
                </th>
                <th>{{ t("inventory.colPreview") }}</th>
                <th>{{ t("inventory.colProductVariant") }}</th>
                <th>{{ t("inventory.colStatus") }}</th>
                <th>{{ t("inventory.colOrderRef") }}</th>
                <th>{{ t("inventory.colCreatedAt") }}</th>
                <th>{{ t("inventory.colSoldAt") }}</th>
                <th class="align-right">
                  {{ t("inventory.colSecurityAction") }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in cards" :key="item.id">
                <td class="selection-cell" data-label="">
                  <input
                    v-if="
                      canManage &&
                      (item.status === 'available' ||
                        item.status === 'disabled')
                    "
                    type="checkbox"
                    :checked="selectedCardIDs.includes(item.id)"
                    :aria-label="
                      t('inventory.batchSelectCard', { preview: item.preview })
                    "
                    @change="toggleCardSelection(item.id)"
                  />
                </td>
                <td :data-label="t('inventory.colPreview')">
                  <div class="secret-preview">
                    <KeyRound :size="14" /><code>{{ item.preview }}</code>
                  </div>
                </td>
                <td :data-label="t('inventory.colProductVariant')">
                  <div class="primary-cell">
                    <strong>{{ item.product_name }}</strong
                    ><span>{{
                      item.variant_sku
                        ? `${item.variant_sku} · ${item.variant_name}`
                        : t("inventory.baseProduct")
                    }}</span>
                  </div>
                </td>
                <td :data-label="t('inventory.colStatus')">
                  <span :class="['pill', item.status]">{{
                    statusLabel(cardStatusLabels, item.status)
                  }}</span>
                </td>
                <td :data-label="t('inventory.colOrderRef')">
                  <code class="short-id">{{ shortID(item.order_id) }}</code>
                </td>
                <td :data-label="t('inventory.colCreatedAt')">
                  {{ formatTime(item.created_at) }}
                </td>
                <td :data-label="t('inventory.colSoldAt')">
                  {{ formatTime(item.sold_at) }}
                </td>
                <td
                  :data-label="t('inventory.colSecurityAction')"
                  class="align-right"
                >
                  <button
                    v-if="canManage && item.status === 'available'"
                    class="row-action danger"
                    @click="openStatusChange(item)"
                  >
                    <CircleSlash2 :size="14" />
                    {{ t("inventory.actionDisable") }}</button
                  ><button
                    v-else-if="canManage && item.status === 'disabled'"
                    class="row-action"
                    @click="openStatusChange(item)"
                  >
                    <RotateCcw :size="14" />
                    {{ t("inventory.actionRestore") }}</button
                  ><span
                    v-else-if="
                      item.status !== 'available' && item.status !== 'disabled'
                    "
                    class="managed-state"
                    ><LockKeyhole :size="13" />
                    {{ t("inventory.orderManaged") }}</span
                  >
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <footer class="pager">
          <span>{{ t("inventory.cardCount", { count: cardTotal }) }}</span
          ><select
            v-model.number="cardPageSize"
            @change="
              cardPage = 1;
              loadCards();
            "
          >
            <option :value="10">
              {{ t("inventory.perPage", { count: 10 }) }}
            </option>
            <option :value="20">
              {{ t("inventory.perPage", { count: 20 }) }}
            </option>
            <option :value="50">
              {{ t("inventory.perPage", { count: 50 }) }}
            </option>
          </select>
          <div>
            <button
              :disabled="cardPage <= 1"
              @click="changeCardPage(cardPage - 1)"
            >
              <ChevronLeft :size="15" /></button
            ><button
              v-for="number in cardPages"
              :key="number"
              :class="{ active: number === cardPage }"
              @click="changeCardPage(number)"
            >
              {{ number }}</button
            ><button
              :disabled="cardPage >= cardPageCount"
              @click="changeCardPage(cardPage + 1)"
            >
              <ChevronRight :size="15" />
            </button>
          </div>
        </footer>
      </div>
    </template>

    <template v-else>
      <div class="toolbar">
        <form class="search-box" @submit.prevent="searchBatches">
          <Search :size="15" /><input
            v-model="batchSearch"
            :placeholder="t('inventory.searchBatchPlaceholder')"
          /><button type="submit">{{ t("inventory.search") }}</button>
        </form>
        <select
          v-model="batchProduct"
          @change="
            batchPage = 1;
            loadBatches();
          "
        >
          <option value="">{{ t("inventory.filterAllProducts") }}</option>
          <option
            v-for="item in inventoryOptions"
            :key="item.id"
            :value="item.id"
          >
            {{ item.name }}
          </option>
        </select>
        <select
          v-model="batchInvalid"
          @change="
            batchPage = 1;
            loadBatches();
          "
        >
          <option value="">{{ t("inventory.filterAllResult") }}</option>
          <option value="false">{{ t("inventory.filterAllValid") }}</option>
          <option value="true">{{ t("inventory.filterHasSkipped") }}</option>
        </select>
        <button
          class="icon-button"
          :title="t('inventory.refresh')"
          @click="loadBatches"
        >
          <RefreshCw :size="16" :class="{ spin: batchesLoading }" />
        </button>
      </div>
      <div v-if="batchesError" class="inventory-notice error">
        <AlertTriangle :size="17" /><span>{{ batchesError }}</span
        ><button @click="loadBatches">{{ t("inventory.retry") }}</button>
      </div>
      <div class="data-card">
        <div v-if="batchesLoading" class="table-state">
          <LoaderCircle :size="22" class="spin" />
          {{ t("inventory.loadingBatches") }}
        </div>
        <div v-else-if="!batches.length" class="table-state">
          <History :size="28" /><strong>{{ t("inventory.noBatches") }}</strong
          ><span>{{ t("inventory.noBatchesHint") }}</span>
        </div>
        <div v-else class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>{{ t("inventory.colBatchNo") }}</th>
                <th>{{ t("inventory.colProductVariant") }}</th>
                <th>{{ t("inventory.colSource") }}</th>
                <th>{{ t("inventory.colTotalLines") }}</th>
                <th>{{ t("inventory.colValidImported") }}</th>
                <th>{{ t("inventory.colSkipped") }}</th>
                <th>{{ t("inventory.colImporter") }}</th>
                <th>{{ t("inventory.colTime") }}</th>
                <th>{{ t("inventory.colResult") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in batches" :key="item.id">
                <td :data-label="t('inventory.colBatchNo')">
                  <code class="batch-no">{{ item.batch_no }}</code>
                </td>
                <td :data-label="t('inventory.colProductVariant')">
                  <div class="primary-cell">
                    <strong>{{ item.product_name }}</strong
                    ><span>{{
                      item.variant_sku
                        ? `${item.variant_sku} · ${item.variant_name}`
                        : t("inventory.baseProduct")
                    }}</span>
                  </div>
                </td>
                <td :data-label="t('inventory.colSource')">
                  {{ statusLabel(sourceLabels, item.source) }}
                </td>
                <td :data-label="t('inventory.colTotalLines')">
                  {{ formatNumber(item.total_count) }}
                </td>
                <td :data-label="t('inventory.colValidImported')">
                  <strong class="success-text">{{
                    formatNumber(item.valid_count)
                  }}</strong>
                </td>
                <td :data-label="t('inventory.colSkipped')">
                  <strong :class="{ 'warning-text': item.invalid_count > 0 }">{{
                    formatNumber(item.invalid_count)
                  }}</strong>
                </td>
                <td :data-label="t('inventory.colImporter')">
                  {{ item.importer_name || t("inventory.admin") }}
                </td>
                <td :data-label="t('inventory.colTime')">
                  {{ formatTime(item.created_at) }}
                </td>
                <td :data-label="t('inventory.colResult')">
                  <span :class="['pill', batchState(item).className]">{{
                    batchState(item).label
                  }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <footer class="pager">
          <span>{{ t("inventory.batchCount", { count: batchTotal }) }}</span
          ><select
            v-model.number="batchPageSize"
            @change="
              batchPage = 1;
              loadBatches();
            "
          >
            <option :value="10">
              {{ t("inventory.perPage", { count: 10 }) }}
            </option>
            <option :value="20">
              {{ t("inventory.perPage", { count: 20 }) }}
            </option>
            <option :value="50">
              {{ t("inventory.perPage", { count: 50 }) }}
            </option>
          </select>
          <div>
            <button
              :disabled="batchPage <= 1"
              @click="changeBatchPage(batchPage - 1)"
            >
              <ChevronLeft :size="15" /></button
            ><button
              v-for="number in batchPages"
              :key="number"
              :class="{ active: number === batchPage }"
              @click="changeBatchPage(number)"
            >
              {{ number }}</button
            ><button
              :disabled="batchPage >= batchPageCount"
              @click="changeBatchPage(batchPage + 1)"
            >
              <ChevronRight :size="15" />
            </button>
          </div>
        </footer>
      </div>
    </template>

    <Teleport to="body">
      <div
        v-if="importOpen && canManage"
        class="modal-backdrop"
        @mousedown.self="closeImport"
      >
        <section
          class="import-modal"
          role="dialog"
          aria-modal="true"
          :aria-label="t('inventory.encryptedImport')"
        >
          <header>
            <div>
              <span class="eyebrow">{{
                t("adminKicker.encryptedImport")
              }}</span>
              <h3>{{ t("inventory.encryptedImport") }}</h3>
              <p>{{ t("inventory.importHint") }}</p>
            </div>
            <button
              class="close-button"
              :aria-label="t('inventory.close')"
              @click="closeImport"
            >
              <X :size="18" />
            </button>
          </header>
          <form @submit.prevent="submitImport">
            <div class="form-grid">
              <label class="full"
                ><span>{{ t("inventory.importLocalProduct") }}</span>
                <div class="compound-field">
                  <select
                    v-model="importForm.productID"
                    @change="onImportProductChange"
                  >
                    <option value="" disabled>
                      {{ t("inventory.selectProduct") }}
                    </option>
                    <option
                      v-for="item in inventoryOptions.filter(
                        (product) => product.inventory_mode === 'local',
                      )"
                      :key="item.id"
                      :value="item.id"
                    >
                      {{
                        t("inventory.optionAvailable", {
                          name: item.name,
                          count: item.available,
                        })
                      }}
                    </option></select
                  ><input
                    v-model="inventoryOptionSearch"
                    :placeholder="t('inventory.searchMoreProducts')"
                    @keyup.enter.prevent="loadInventoryOptions()"
                  /><button
                    type="button"
                    :disabled="inventoryOptionsLoading"
                    @click="loadInventoryOptions()"
                  >
                    <LoaderCircle
                      v-if="inventoryOptionsLoading"
                      :size="14"
                      class="spin"
                    /><Search v-else :size="14" />
                  </button></div
              ></label>
              <label v-if="activeImportVariants.length" class="full"
                ><span>{{ t("inventory.importVariant") }}</span
                ><select v-model="importForm.variantID">
                  <option value="" disabled>
                    {{ t("inventory.selectActiveVariant") }}
                  </option>
                  <option
                    v-for="item in activeImportVariants"
                    :key="item.id"
                    :value="item.id"
                  >
                    {{
                      t("inventory.optionVariantAvailable", {
                        sku: item.sku,
                        name: item.name,
                        count: item.available,
                      })
                    }}
                  </option>
                </select></label
              >
              <div
                v-if="selectedInventoryProduct && !activeImportVariants.length"
                class="field-note full"
              >
                {{ t("inventory.noVariantNote") }}
              </div>
              <label class="full secret-field"
                ><span>{{ t("inventory.importCardsLabel") }}</span
                ><textarea
                  v-model="importForm.cards"
                  rows="13"
                  spellcheck="false"
                  autocomplete="off"
                  :placeholder="t('inventory.importCardsPlaceholder')"
                  @paste.stop
                ></textarea
                ><small>{{
                  t("inventory.importLineStats", {
                    total: importLineStats.total,
                    nonEmpty: importLineStats.nonEmpty,
                    unique: importLineStats.unique,
                  })
                }}</small></label
              >
              <label class="full"
                ><span>{{ t("inventory.importReason") }}</span
                ><textarea
                  v-model="importForm.reason"
                  rows="2"
                  maxlength="500"
                  :placeholder="t('inventory.importReasonPlaceholder')"
                ></textarea>
              </label>
            </div>
            <div class="no-export-note">
              <ShieldCheck :size="17" /><span>{{
                t("inventory.noExportNote")
              }}</span>
            </div>
            <p v-if="importError" class="modal-error">
              <AlertTriangle :size="16" /> {{ importError }}
            </p>
            <footer>
              <button
                type="button"
                class="secondary-button"
                @click="closeImport"
              >
                {{ t("inventory.cancelAndClear") }}</button
              ><button
                type="submit"
                class="primary-action"
                :disabled="importing"
              >
                <LoaderCircle v-if="importing" :size="15" class="spin" /><Upload
                  v-else
                  :size="15"
                />
                {{
                  importing
                    ? t("inventory.importing")
                    : t("inventory.confirmImport")
                }}
              </button>
            </footer>
          </form>
        </section>
      </div>

      <div
        v-if="statusTarget && canManage"
        class="modal-backdrop"
        @mousedown.self="closeStatusChange"
      >
        <section
          class="status-modal"
          role="alertdialog"
          aria-modal="true"
          :aria-label="t('inventory.changeStatusAria')"
        >
          <div
            :class="[
              'status-icon',
              targetStatus === 'disabled' ? 'danger' : 'success',
            ]"
          >
            <CircleSlash2
              v-if="targetStatus === 'disabled'"
              :size="22"
            /><RotateCcw v-else :size="22" />
          </div>
          <h3>
            {{
              targetStatus === "disabled"
                ? t("inventory.disableCardTitle")
                : t("inventory.restoreCardTitle")
            }}
          </h3>
          <p>
            {{
              t("inventory.statusChangeCard", {
                preview: statusTarget.preview,
                product: statusTarget.product_name,
              })
            }}{{
              targetStatus === "disabled"
                ? t("inventory.disableReason")
                : t("inventory.restoreReason")
            }}
          </p>
          <label
            ><span>{{ t("inventory.changeReason") }}</span
            ><textarea
              v-model="statusReason"
              rows="3"
              maxlength="500"
              :placeholder="t('inventory.reasonPlaceholder')"
            ></textarea>
          </label>
          <p v-if="statusError" class="modal-error">
            <AlertTriangle :size="16" /> {{ statusError }}
          </p>
          <footer>
            <button class="secondary-button" @click="closeStatusChange">
              {{ t("inventory.cancel") }}</button
            ><button
              :class="
                targetStatus === 'disabled' ? 'danger-button' : 'primary-action'
              "
              :disabled="statusSaving"
              @click="submitStatusChange"
            >
              <LoaderCircle
                v-if="statusSaving"
                :size="15"
                class="spin"
              /><CheckCircle2 v-else :size="15" />
              {{
                statusSaving
                  ? t("inventory.processing")
                  : t("inventory.confirmChange")
              }}
            </button>
          </footer>
        </section>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.inventory-shell {
  display: grid;
  gap: 18px;
  min-width: 0;
}
.inventory-tabs {
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
.inventory-tabs button {
  border: 0;
  border-radius: 6px;
  padding: 10px 15px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  background: transparent;
  color: var(--muted);
  font-size: 11px;
  font-weight: 650;
  white-space: nowrap;
}
.inventory-tabs button.active {
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
  max-width: 760px;
  margin: 0;
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
.primary-action,
.secondary-button,
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
button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.inventory-notice {
  min-height: 42px;
  display: flex;
  align-items: center;
  gap: 9px;
  border: 1px solid var(--line);
  border-radius: 7px;
  padding: 9px 12px;
  background: var(--surface);
  font-size: 11px;
}
.inventory-notice.success {
  color: var(--success);
  border-color: color-mix(in srgb, var(--success) 28%, var(--line));
  background: color-mix(in srgb, var(--success) 6%, var(--surface));
}
.inventory-notice.error {
  color: var(--danger);
  border-color: color-mix(in srgb, var(--danger) 28%, var(--line));
  background: color-mix(in srgb, var(--danger) 6%, var(--surface));
}
.inventory-notice span {
  flex: 1;
}
.inventory-notice button {
  border: 0;
  background: transparent;
  color: inherit;
  font-size: 10px;
}
.security-strip {
  min-height: 54px;
  display: flex;
  align-items: center;
  gap: 11px;
  border: 1px solid color-mix(in srgb, var(--success) 24%, var(--line));
  border-radius: 8px;
  padding: 10px 13px;
  background: color-mix(in srgb, var(--success) 5%, var(--surface));
  color: var(--success);
}
.security-strip > div {
  display: grid;
  gap: 3px;
}
.security-strip strong {
  font-size: 10px;
}
.security-strip span {
  color: var(--muted);
  font-size: 9px;
  line-height: 1.5;
}
.metric-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}
.metric-grid article {
  min-height: 82px;
  display: grid;
  grid-template-columns: auto 1fr;
  align-items: center;
  gap: 5px 9px;
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 13px;
  background: var(--surface);
}
.metric-grid svg {
  grid-row: 1 / 3;
  color: var(--muted);
}
.metric-grid span {
  color: var(--muted);
  font-size: 9px;
}
.metric-grid strong {
  font-size: 20px;
  letter-spacing: -0.03em;
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
  min-width: 940px;
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
tbody tr:hover:not(.variant-detail-row) {
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
.primary-cell code,
.primary-cell span {
  color: var(--muted);
  font-size: 9px;
  overflow-wrap: anywhere;
}
.success-text {
  color: var(--success);
}
.warning-text {
  color: var(--warn);
}
.mode-label {
  white-space: nowrap;
}
.pill {
  display: inline-flex;
  width: fit-content;
  border-radius: 99px;
  padding: 4px 7px;
  font-size: 8px;
  font-weight: 650;
  white-space: nowrap;
}
.pill.success,
.pill.available {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 10%, transparent);
}
.pill.warning {
  color: var(--warn);
  background: color-mix(in srgb, var(--warn) 11%, transparent);
}
.pill.danger,
.pill.failed {
  color: var(--danger);
  background: color-mix(in srgb, var(--danger) 10%, transparent);
}
.pill.muted,
.pill.disabled,
.pill.supplier {
  color: var(--muted);
  background: var(--soft);
}
.pill.locked {
  color: var(--warn);
  background: color-mix(in srgb, var(--warn) 11%, transparent);
}
.pill.sold {
  color: var(--text);
  background: var(--soft);
}
.expand-button {
  width: 29px;
  height: 29px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--muted);
}
.variant-detail-row td {
  padding: 10px 13px 15px 42px;
  background: var(--surface-2);
}
.variant-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 7px;
}
.variant-grid article {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 10px;
  background: var(--surface);
}
.variant-grid article > div:first-child {
  min-width: 0;
  display: grid;
  gap: 2px;
}
.variant-grid strong {
  font-size: 10px;
}
.variant-grid code {
  color: var(--muted);
  font-size: 8px;
  overflow-wrap: anywhere;
}
.variant-state {
  height: fit-content;
  border-radius: 99px;
  padding: 3px 6px;
  font-size: 7px;
}
.variant-state.active {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 9%, transparent);
}
.variant-state.inactive {
  color: var(--muted);
  background: var(--soft);
}
.variant-grid dl {
  grid-column: 1 / -1;
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 5px;
  margin: 3px 0 0;
}
.variant-grid dl div {
  display: grid;
  gap: 2px;
}
.variant-grid dt {
  color: var(--muted);
  font-size: 7px;
}
.variant-grid dd {
  margin: 0;
  font-size: 10px;
  font-weight: 650;
}
.secret-preview {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  border: 1px solid var(--line);
  border-radius: 5px;
  padding: 6px 8px;
  background: var(--surface-2);
}
.secret-preview svg {
  color: var(--muted);
}
.secret-preview code {
  font-size: 10px;
  letter-spacing: 0.035em;
}
.short-id,
.batch-no {
  color: var(--muted);
  font-size: 9px;
}
.batch-no {
  color: var(--text);
}
.row-action {
  min-height: 30px;
  border: 1px solid var(--line);
  border-radius: 5px;
  padding: 0 9px;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  background: var(--surface);
  color: var(--text);
  font-size: 9px;
}
.row-action.danger {
  color: var(--danger);
}
.managed-state {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: 8px;
}
.table-state {
  min-height: 230px;
  padding: 35px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
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
  border-color: var(--dark);
  background: var(--dark);
  color: var(--dark-text);
}
.modal-backdrop {
  position: fixed;
  z-index: 220;
  inset: 0;
  padding: 24px;
  display: grid;
  place-items: center;
  background: rgba(8, 8, 10, 0.58);
  backdrop-filter: blur(3px);
}
.import-modal {
  width: min(760px, 100%);
  max-height: calc(100vh - 48px);
  overflow-y: auto;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: 0 28px 90px rgba(0, 0, 0, 0.28);
}
.import-modal > header {
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
.import-modal h3,
.status-modal h3 {
  margin: 5px 0 4px;
  font-size: 19px;
  letter-spacing: -0.025em;
}
.import-modal header p {
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
.import-modal form {
  padding: 18px 20px 20px;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
.form-grid label,
.status-modal label {
  min-width: 0;
  display: grid;
  gap: 6px;
  color: var(--text);
  font-size: 10px;
  font-weight: 650;
}
.form-grid input,
.form-grid select,
.form-grid textarea,
.status-modal textarea,
.compound-field input,
.compound-field select {
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
.form-grid small {
  color: var(--muted);
  font-size: 8px;
  font-weight: 400;
}
.full {
  grid-column: 1 / -1;
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
.secret-field textarea {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  line-height: 1.55;
}
.field-note,
.no-export-note {
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 10px;
  background: var(--surface-2);
  color: var(--muted);
  font-size: 9px;
  line-height: 1.6;
}
.no-export-note {
  margin-top: 14px;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  color: var(--success);
  border-color: color-mix(in srgb, var(--success) 25%, var(--line));
  background: color-mix(in srgb, var(--success) 5%, var(--surface));
}
.no-export-note svg {
  flex: none;
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
.import-modal form > footer,
.status-modal footer {
  margin-top: 17px;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.status-modal {
  width: min(430px, 100%);
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 21px;
  background: var(--surface);
  box-shadow: 0 28px 90px rgba(0, 0, 0, 0.28);
}
.status-modal > p {
  margin: 0 0 16px;
  color: var(--muted);
  font-size: 10px;
  line-height: 1.7;
}
.status-modal > p code {
  color: var(--text);
}
.status-icon {
  width: 42px;
  height: 42px;
  display: grid;
  place-items: center;
  border-radius: 8px;
}
.status-icon.danger {
  color: var(--danger);
  background: color-mix(in srgb, var(--danger) 10%, transparent);
}
.status-icon.success {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 10%, transparent);
}
.spin {
  animation: spin 0.85s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
.inventory-batch-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--line);
  background: var(--soft);
}
.inventory-batch-toolbar > span {
  color: var(--muted);
  font-size: 9px;
}
.inventory-batch-toolbar > div {
  display: flex;
  gap: 7px;
  margin-left: auto;
}
.inventory-batch-toolbar button {
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
.inventory-batch-toolbar button.danger {
  color: var(--danger);
}
.inventory-batch-toolbar button:disabled {
  opacity: 0.42;
  cursor: not-allowed;
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
@media (max-width: 1050px) {
  .metric-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .variant-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
@media (max-width: 720px) {
  .inventory-shell {
    gap: 14px;
  }
  .inventory-tabs {
    width: 100%;
  }
  .inventory-tabs button {
    flex: 1;
    padding-inline: 10px;
  }
  .section-heading {
    display: grid;
    align-items: start;
    gap: 14px;
  }
  .section-heading .primary-action {
    width: 100%;
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
  .variant-grid {
    grid-template-columns: 1fr;
  }
  .pager {
    justify-content: space-between;
    flex-wrap: wrap;
  }
  .modal-backdrop {
    padding: 10px;
  }
  .import-modal {
    max-height: calc(100vh - 20px);
  }
  .import-modal > header,
  .import-modal form {
    padding-left: 14px;
    padding-right: 14px;
  }
  .form-grid {
    grid-template-columns: 1fr;
  }
  .full {
    grid-column: auto;
  }
  .inventory-batch-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }
  .inventory-batch-toolbar > div {
    margin-left: 0;
    flex-wrap: wrap;
  }
  .compound-field {
    grid-template-columns: 1fr 38px;
  }
  .compound-field select {
    grid-column: 1 / -1;
  }
}
@media (max-width: 500px) {
  .inventory-tabs button span {
    display: none;
  }
  .metric-grid,
  .toolbar {
    grid-template-columns: 1fr;
  }
  .search-box {
    grid-column: auto;
  }
}
</style>
