<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  ArrowRight,
  BadgeCheck,
  Box,
  ChevronRight,
  Clock3,
  Grid2X2,
  LayoutGrid,
  List,
  PanelLeft,
  PanelRight,
  RefreshCw,
  Rows3,
  Search,
  ShieldCheck,
  Sparkles,
  Tags,
  X,
  Zap,
} from "@lucide/vue";
import { fetchCategories, fetchProducts, fetchPublicContent } from "../api";
import HomeCarousel from "../components/HomeCarousel.vue";
import type { Category, ProductItem, PublicBanner as Banner } from "../types";
import { formatMinor, selectedCurrency } from "../utils/money";
import { categoryArtwork, productArtwork } from "../utils/brandAssets";

const { t, locale } = useI18n();
const route = useRoute();
const pageSize = 18;
type ProductLayout = "card" | "compact" | "list" | "dense";

const products = ref<ProductItem[]>([]);
const productTotal = ref(0);
const productPage = ref(1);
const categories = ref<Category[]>([]);
const banners = ref<Banner[]>([]);
const activeCategory = ref("all");
const search = ref("");
const productsLoading = ref(true);
const loadingMore = ref(false);
const categoriesLoading = ref(true);
const bannersLoading = ref(true);
const productError = ref("");
const categoryError = ref("");
const bannerError = ref("");
const isDarkTheme = ref(document.documentElement.dataset.theme === "dark");
const categoryStrip = ref<HTMLElement | null>(null);
const floatingCategoryVisible = ref(false);
const categoryDrawerOpen = ref(false);
const categoryFilter = ref("");
const floatingCategorySide = ref<"left" | "right">(
  localStorage.getItem("linlinqi-category-side") === "right" ? "right" : "left",
);
const productLayout = ref<ProductLayout>(
  (["card", "compact", "list", "dense"] as const).includes(
    localStorage.getItem("linlinqi-product-layout") as ProductLayout,
  )
    ? (localStorage.getItem("linlinqi-product-layout") as ProductLayout)
    : "card",
);

let productRequestSequence = 0;
let searchTimer: number | undefined;
let themeObserver: MutationObserver | undefined;

const hasMore = computed(() => products.value.length < productTotal.value);
const homeHeroBanners = computed(() =>
  banners.value.filter((banner) => banner.placement === "home_hero"),
);
const homeSecondaryBanners = computed(() =>
  banners.value.filter((banner) => banner.placement === "home_secondary"),
);
const staticHeroSlide = computed<Banner>(() => ({
  id: "linlinqi-static-hero",
  title: t("home.title"),
  image_url: "/assets/brand/linlinqi-hero-commerce.webp",
  target_url: "/#market",
  placement: "home_hero",
}));
const fallbackHeroBanners = computed<Banner[]>(() => [
  {
    id: "linlinqi-fallback-carousel-01",
    title: t("home.carouselSlideGaming"),
    image_url: "/assets/brand/linlinqi-game-credit-carousel-01.webp",
    target_url: "/#market",
    placement: "home_hero",
  },
  {
    id: "linlinqi-fallback-carousel-02",
    title: t("home.carouselSlideDelivery"),
    image_url: "/assets/brand/linlinqi-game-credit-carousel-02.webp",
    target_url: "/#market",
    placement: "home_hero",
  },
  {
    id: "linlinqi-fallback-carousel-03",
    title: t("home.carouselSlideSecurity"),
    image_url: "/assets/brand/linlinqi-game-credit-carousel-03.webp",
    target_url: "/#market",
    placement: "home_hero",
  },
  {
    id: "linlinqi-fallback-carousel-04",
    title: t("home.carouselSlideReseller"),
    image_url: "/assets/brand/linlinqi-hero-reseller.webp",
    target_url: "/reseller/apply",
    placement: "home_hero",
  },
]);
const darkHeroBanners = computed<Banner[]>(() => [
  {
    id: "linlinqi-dark-vault-4k",
    title: t("home.carouselSlideSecurity"),
    image_url: "/assets/brand/linlinqi-hero-dark-vault-4k.webp",
    target_url: "/#market",
    placement: "home_hero",
  },
  {
    id: "linlinqi-dark-delivery-4k",
    title: t("home.carouselSlideDelivery"),
    image_url: "/assets/brand/linlinqi-hero-dark-delivery-4k.webp",
    target_url: "/#market",
    placement: "home_hero",
  },
  {
    id: "linlinqi-dark-inventory-4k",
    title: t("home.inventoryStrategy"),
    image_url: "/assets/brand/linlinqi-hero-dark-inventory-4k.webp",
    target_url: "/#market",
    placement: "home_hero",
  },
]);
const heroCarouselSlides = computed(() => {
  if (isDarkTheme.value) return darkHeroBanners.value;
  const configured = [...homeHeroBanners.value, ...homeSecondaryBanners.value];
  const configuredIDs = new Set(configured.map((banner) => banner.id));
  const fallback = fallbackHeroBanners.value.filter(
    (banner) => !configuredIDs.has(banner.id),
  );
  return [staticHeroSlide.value, ...configured, ...fallback].slice(0, 8);
});
const filteredFloatingCategories = computed(() => {
  const keyword = categoryFilter.value.trim().toLocaleLowerCase(locale.value);
  if (!keyword) return categories.value;
  return categories.value.filter((category) =>
    `${category.name} ${category.slug}`
      .toLocaleLowerCase(locale.value)
      .includes(keyword),
  );
});
const productLayouts = computed(
  () =>
    [
      ["card", t("home.layoutCard"), LayoutGrid],
      ["compact", t("home.layoutCompact"), Grid2X2],
      ["list", t("home.layoutList"), List],
      ["dense", t("home.layoutDense"), Rows3],
    ] as const,
);

const money = (cents: number, currency: string) =>
  formatMinor(cents, currency, locale);

function requestMessage(reason: unknown, fallback: string) {
  if (!reason || typeof reason !== "object") return fallback;
  const response = (reason as { response?: { data?: { message?: unknown } } })
    .response;
  return typeof response?.data?.message === "string"
    ? response.data.message
    : fallback;
}

function productTags(value: string) {
  return String(value || "")
    .split(",")
    .map((tag) => tag.trim())
    .filter(Boolean);
}

function mergeProducts(current: ProductItem[], incoming: ProductItem[]) {
  const merged = new Map<string, ProductItem>();
  for (const item of [...current, ...incoming]) {
    if (item?.product?.id) merged.set(item.product.id, item);
  }
  return [...merged.values()];
}

async function loadProducts(targetPage = 1, append = false) {
  const requestSequence = ++productRequestSequence;
  if (append) {
    loadingMore.value = true;
  } else {
    productsLoading.value = true;
    loadingMore.value = false;
    products.value = [];
    productTotal.value = 0;
    productPage.value = 1;
  }
  productError.value = "";
  try {
    const result = await fetchProducts({
      ...(search.value.trim() ? { q: search.value.trim() } : {}),
      ...(activeCategory.value !== "all"
        ? { category: activeCategory.value }
        : {}),
      page: targetPage,
      page_size: pageSize,
      ...(selectedCurrency.value ? { currency: selectedCurrency.value } : {}),
    });
    if (requestSequence !== productRequestSequence) return;
    products.value = append
      ? mergeProducts(products.value, result.items)
      : result.items;
    productTotal.value = Math.max(result.total, products.value.length);
    productPage.value = result.page;
  } catch (reason: unknown) {
    if (requestSequence !== productRequestSequence) return;
    productError.value = requestMessage(reason, t("home.productsLoadFailed"));
  } finally {
    if (requestSequence === productRequestSequence) {
      productsLoading.value = false;
      loadingMore.value = false;
    }
  }
}

async function loadCategories() {
  categoriesLoading.value = true;
  categoryError.value = "";
  try {
    categories.value = await fetchCategories();
  } catch (reason: unknown) {
    categories.value = [];
    categoryError.value = requestMessage(
      reason,
      t("home.categoriesLoadFailed"),
    );
  } finally {
    categoriesLoading.value = false;
  }
}

async function loadBanners() {
  bannersLoading.value = true;
  bannerError.value = "";
  try {
    const content = await fetchPublicContent();
    banners.value = content.banners;
  } catch (reason: unknown) {
    banners.value = [];
    bannerError.value = requestMessage(reason, t("home.bannerLoadFailed"));
  } finally {
    bannersLoading.value = false;
  }
}

function setCategory(category: string) {
  if (activeCategory.value !== category) activeCategory.value = category;
  categoryDrawerOpen.value = false;
}

function setProductLayout(layout: ProductLayout) {
  productLayout.value = layout;
  localStorage.setItem("linlinqi-product-layout", layout);
}

function toggleFloatingCategorySide() {
  floatingCategorySide.value =
    floatingCategorySide.value === "left" ? "right" : "left";
  localStorage.setItem("linlinqi-category-side", floatingCategorySide.value);
}

function updateFloatingCategoryVisibility() {
  const element = categoryStrip.value;
  floatingCategoryVisible.value = Boolean(
    element && element.getBoundingClientRect().bottom < 88,
  );
  if (!floatingCategoryVisible.value) categoryDrawerOpen.value = false;
}

function focusSearch() {
  if (route.query.focus !== "search") return;
  window.setTimeout(
    () => document.querySelector<HTMLInputElement>("#store-search")?.focus(),
    100,
  );
}

function syncTheme() {
  isDarkTheme.value = document.documentElement.dataset.theme === "dark";
}

watch(search, () => {
  window.clearTimeout(searchTimer);
  searchTimer = window.setTimeout(() => void loadProducts(1), 320);
});

watch(activeCategory, () => {
  window.clearTimeout(searchTimer);
  void loadProducts(1);
});

watch(selectedCurrency, () => {
  window.clearTimeout(searchTimer);
  void loadProducts(1);
});

watch(() => route.query.focus, focusSearch);

onMounted(() => {
  syncTheme();
  themeObserver = new MutationObserver(syncTheme);
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["data-theme"],
  });
  focusSearch();
  void Promise.all([loadProducts(), loadCategories(), loadBanners()]);
  window.addEventListener("scroll", updateFloatingCategoryVisibility, {
    passive: true,
  });
  window.addEventListener("resize", updateFloatingCategoryVisibility, {
    passive: true,
  });
  window.requestAnimationFrame(updateFloatingCategoryVisibility);
});

onBeforeUnmount(() => {
  themeObserver?.disconnect();
  window.clearTimeout(searchTimer);
  productRequestSequence += 1;
  window.removeEventListener("scroll", updateFloatingCategoryVisibility);
  window.removeEventListener("resize", updateFloatingCategoryVisibility);
});
</script>

<template>
  <section class="hero">
    <div class="hero-grid-bg"></div>
    <div class="container hero-inner">
      <div class="eyebrow">
        <span></span> {{ t("home.badge") }} <Sparkles :size="14" />
      </div>
      <h1>
        {{ t("home.title") }}<br /><em>{{ t("home.titleAccent") }}</em>
      </h1>
      <p>{{ t("home.subtitle") }}</p>
      <div class="hero-actions">
        <a class="button primary" href="#market"
          >{{ t("home.browse") }} <ArrowRight :size="17" /></a
        ><RouterLink class="button secondary" to="/orders">{{
          t("home.lookup")
        }}</RouterLink>
      </div>
      <div class="trust-row">
        <span><ShieldCheck :size="16" /> {{ t("home.securePay") }}</span
        ><span><Zap :size="16" /> {{ t("home.autoDelivery") }}</span
        ><span><Clock3 :size="16" /> {{ t("home.service247") }}</span>
      </div>
      <figure class="hero-brand-visual">
        <HomeCarousel :slides="heroCarouselSlides" :interval="5200" />
      </figure>
    </div>
    <div class="container ticker">
      <span>{{ t("home.capabilities") }}</span
      ><b><i></i> {{ t("home.paymentVerify") }}</b
      ><span>{{ t("home.inventoryStrategy") }}</span
      ><b>{{ t("home.txnReserve") }}</b
      ><span>{{ t("home.deliveryGuarantee") }}</span
      ><b>{{ t("home.encryptedStorage") }}</b>
    </div>
  </section>

  <div v-if="bannerError" class="banner-inline-error" role="status">
    <span>{{ bannerError }}</span>
    <button type="button" @click="loadBanners">
      <RefreshCw :size="14" /> {{ t("home.retry") }}
    </button>
  </div>

  <section id="market" class="section marketplace">
    <div class="container">
      <div class="section-heading">
        <div>
          <span class="kicker">{{ t("kicker.marketplace") }}</span>
          <h2>{{ t("home.discover") }}</h2>
          <p>{{ t("home.discoverSub") }}</p>
        </div>
        <div class="search-field">
          <Search :size="18" /><input
            id="store-search"
            v-model="search"
            type="search"
            :aria-label="t('home.searchPlaceholder')"
            :placeholder="t('home.searchPlaceholder')"
          />
        </div>
      </div>
      <div class="market-layout-toolbar">
        <div>
          <span><LayoutGrid :size="15" />{{ t("home.productLayout") }}</span>
          <button
            v-for="[layout, label, icon] in productLayouts"
            :key="layout"
            type="button"
            :class="{ active: productLayout === layout }"
            :title="label"
            :aria-label="label"
            :aria-pressed="productLayout === layout"
            @click="setProductLayout(layout)"
          >
            <component :is="icon" :size="16" /><b>{{ label }}</b>
          </button>
        </div>
        <small>{{ t("home.layoutSavedHint") }}</small>
      </div>
      <div
        ref="categoryStrip"
        class="category-tabs"
        :aria-busy="categoriesLoading"
      >
        <button
          :class="{ active: activeCategory === 'all' }"
          @click="setCategory('all')"
        >
          {{ t("home.allProducts") }}</button
        ><button
          v-for="category in categories"
          :key="category.slug"
          :class="{ active: activeCategory === category.slug }"
          @click="setCategory(category.slug)"
        >
          <img
            :src="categoryArtwork(category)"
            alt=""
            loading="lazy"
            decoding="async"
          />
          {{ category.name }}
        </button>
        <span v-if="categoriesLoading" class="category-state">{{
          t("home.categoriesLoading")
        }}</span>
        <button
          v-else-if="categoryError"
          type="button"
          class="category-retry"
          @click="loadCategories"
        >
          {{ categoryError }} · {{ t("home.retry") }}
        </button>
      </div>

      <div
        v-if="productsLoading"
        class="product-skeleton-grid"
        role="status"
        aria-live="polite"
      >
        <span class="market-loading-label">{{
          t("home.productsLoading")
        }}</span>
        <i v-for="index in 6" :key="index" aria-hidden="true"></i>
      </div>

      <div v-else-if="productError && !products.length" class="market-state">
        <Box :size="30" />
        <strong>{{ t("home.productsLoadFailedTitle") }}</strong>
        <span>{{ productError }}</span>
        <button class="button secondary" type="button" @click="loadProducts(1)">
          <RefreshCw :size="15" /> {{ t("home.retry") }}
        </button>
      </div>

      <div
        v-else-if="products.length"
        :class="['product-grid', `product-layout-${productLayout}`]"
      >
        <RouterLink
          v-for="({ product, stock }, index) in products"
          :key="product.id"
          class="product-card"
          :to="`/products/${product.slug}`"
        >
          <div class="product-visual" :class="`tone-${(index % 4) + 1}`">
            <img
              :src="productArtwork(product)"
              :alt="product.name"
              loading="lazy"
              decoding="async"
            />
            <span>{{ product.category.name }}</span>
            <small>{{ t("kicker.linlinqiSelected") }}</small>
          </div>
          <div class="product-body">
            <div class="product-meta">
              <span>{{ product.category.name }}</span
              ><b><i></i> {{ t("home.stock", { stock }) }}</b>
            </div>
            <h3>{{ product.name }}</h3>
            <p>{{ product.summary }}</p>
            <div v-if="productTags(product.tags).length" class="tag-row">
              <span v-for="tag in productTags(product.tags)" :key="tag">{{
                tag
              }}</span>
            </div>
            <div class="product-bottom">
              <div class="price">
                <strong>{{ money(product.price, product.currency) }}</strong
                ><del v-if="product.compare_price > product.price">{{
                  money(product.compare_price, product.currency)
                }}</del>
              </div>
              <span class="round-arrow"><ChevronRight :size="18" /></span>
            </div>
          </div>
        </RouterLink>
      </div>

      <div v-else class="empty">
        <Box :size="30" /><strong>{{ t("home.noMatch") }}</strong
        ><span>{{ t("home.noMatchSub") }}</span>
      </div>

      <div v-if="products.length" class="market-footer">
        <span>{{
          t("home.resultsSummary", {
            visible: products.length,
            total: productTotal,
          })
        }}</span>
        <button
          v-if="hasMore"
          class="button secondary"
          type="button"
          :disabled="loadingMore"
          @click="loadProducts(productPage + 1, true)"
        >
          <RefreshCw :size="15" :class="{ spinning: loadingMore }" />
          {{ loadingMore ? t("home.loadingMore") : t("home.loadMore") }}
        </button>
      </div>
      <p v-if="productError && products.length" class="market-inline-error">
        {{ productError }}
      </p>
    </div>
  </section>

  <Teleport to="body">
    <div
      v-if="floatingCategoryVisible"
      :class="['floating-category-nav', `side-${floatingCategorySide}`]"
    >
      <button
        type="button"
        class="floating-category-trigger"
        :aria-expanded="categoryDrawerOpen"
        :aria-label="t('home.openFloatingCategories')"
        @click="categoryDrawerOpen = !categoryDrawerOpen"
      >
        <Tags :size="19" />
        <span>{{ t("home.categories") }}</span>
        <b>{{ categories.length }}</b>
      </button>
      <aside v-if="categoryDrawerOpen" class="floating-category-drawer">
        <header>
          <div>
            <strong>{{ t("home.allCategoriesTitle") }}</strong>
            <small>{{
              t("home.allCategoriesCount", { count: categories.length })
            }}</small>
          </div>
          <button
            type="button"
            :aria-label="t('home.close')"
            @click="categoryDrawerOpen = false"
          >
            <X :size="17" />
          </button>
        </header>
        <label class="floating-category-search">
          <Search :size="15" />
          <input
            v-model="categoryFilter"
            type="search"
            :placeholder="t('home.searchCategories')"
          />
        </label>
        <nav :aria-label="t('home.floatingCategoryAria')">
          <button
            :class="{ active: activeCategory === 'all' }"
            @click="setCategory('all')"
          >
            <span class="category-all-mark"><LayoutGrid :size="16" /></span>
            <b>{{ t("home.allProducts") }}</b>
          </button>
          <button
            v-for="category in filteredFloatingCategories"
            :key="category.id || category.slug"
            :class="{ active: activeCategory === category.slug }"
            @click="setCategory(category.slug)"
          >
            <img
              :src="categoryArtwork(category)"
              alt=""
              loading="lazy"
              decoding="async"
            />
            <b>{{ category.name }}</b>
          </button>
        </nav>
        <p v-if="!filteredFloatingCategories.length">
          {{ t("home.noCategoriesMatch") }}
        </p>
        <footer>
          <button type="button" @click="toggleFloatingCategorySide">
            <PanelRight v-if="floatingCategorySide === 'left'" :size="16" />
            <PanelLeft v-else :size="16" />
            {{
              floatingCategorySide === "left"
                ? t("home.floatRight")
                : t("home.floatLeft")
            }}
          </button>
        </footer>
      </aside>
    </div>
  </Teleport>

  <section class="promise section">
    <div class="container">
      <div class="section-heading compact">
        <div>
          <span class="kicker">{{ t("kicker.ourPromise") }}</span>
          <h2>{{ t("home.deliveryTitle") }}</h2>
        </div>
      </div>
      <div class="promise-grid">
        <article>
          <span>01</span><Zap />
          <h3>{{ t("home.fastDelivery") }}</h3>
          <p>{{ t("home.fastDeliveryDesc") }}</p>
        </article>
        <article>
          <span>02</span><ShieldCheck />
          <h3>{{ t("home.fullSecurity") }}</h3>
          <p>{{ t("home.fullSecurityDesc") }}</p>
        </article>
        <article>
          <span>03</span><BadgeCheck />
          <h3>{{ t("home.proSupport") }}</h3>
          <p>{{ t("home.proSupportDesc") }}</p>
        </article>
      </div>
    </div>
  </section>
</template>

<style scoped>
.banner-inline-error {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 9px;
  color: var(--danger);
  font-size: 10px;
}
.banner-inline-error button {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;
}

.banner-status {
  min-height: 90px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 14px;
  border: 1px dashed var(--line);
  border-radius: 10px;
  color: var(--muted);
  font-size: 12px;
}

.banner-status-error button,
.category-retry {
  border: 0;
  background: transparent;
  color: var(--text);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font: inherit;
}

.market-layout-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  margin: 4px 0 14px;
}

.market-layout-toolbar > div {
  display: flex;
  align-items: center;
  gap: 7px;
  flex-wrap: wrap;
}

.market-layout-toolbar > div > span,
.market-layout-toolbar > small {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  font-size: 11px;
}

.market-layout-toolbar button {
  min-height: 34px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0 10px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface);
  color: var(--muted);
  cursor: pointer;
}

.market-layout-toolbar button b {
  font-size: 10px;
}

.market-layout-toolbar button.active {
  border-color: var(--text);
  background: var(--inverse);
  color: var(--inverse-text);
}

.product-grid.product-layout-compact {
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 13px;
}

.product-layout-compact .product-visual {
  height: 138px;
}

.product-layout-compact .product-body {
  padding: 14px;
}

.product-layout-list {
  grid-template-columns: 1fr;
}

.product-layout-list .product-card {
  display: grid;
  grid-template-columns: minmax(190px, 240px) minmax(0, 1fr);
}

.product-layout-list .product-visual {
  height: 100%;
  min-height: 190px;
}

.product-layout-list .product-body {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-content: center;
  column-gap: 28px;
}

.product-layout-list .product-meta,
.product-layout-list h3,
.product-layout-list p,
.product-layout-list .tag-row {
  grid-column: 1;
}

.product-layout-list .product-bottom {
  grid-column: 2;
  grid-row: 1 / span 4;
  min-width: 170px;
  margin: 0;
  border: 0;
  align-self: center;
}

.product-grid.product-layout-dense {
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 10px;
}

.product-layout-dense .product-visual {
  height: 116px;
}

.product-layout-dense .product-body {
  padding: 12px;
}

.product-layout-dense .product-body > p,
.product-layout-dense .tag-row,
.product-layout-dense .product-meta > span {
  display: none;
}

.product-layout-dense .product-body h3 {
  min-height: 38px;
  font-size: 13px;
}

.product-layout-dense .product-bottom {
  margin-top: 8px;
  padding-top: 9px;
}

.floating-category-nav {
  position: fixed;
  z-index: 90;
  top: 50%;
  display: flex;
  align-items: center;
  transform: translateY(-50%);
}

.floating-category-nav.side-left {
  left: 10px;
  flex-direction: row;
}

.floating-category-nav.side-right {
  right: 10px;
  flex-direction: row-reverse;
}

.floating-category-trigger {
  width: 54px;
  min-height: 118px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 7px;
  border: 1px solid var(--line);
  border-radius: 14px;
  background: color-mix(in srgb, var(--surface) 92%, transparent);
  color: var(--text);
  box-shadow: var(--shadow);
  backdrop-filter: blur(16px);
  cursor: pointer;
}

.floating-category-trigger span {
  writing-mode: vertical-rl;
  font-size: 11px;
  letter-spacing: 0.08em;
}

.floating-category-trigger b {
  min-width: 22px;
  height: 22px;
  display: grid;
  place-items: center;
  border-radius: 999px;
  background: var(--inverse);
  color: var(--inverse-text);
  font-size: 9px;
}

.floating-category-drawer {
  width: min(340px, calc(100vw - 82px));
  margin: 0 8px;
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: 14px;
  background: var(--surface);
  box-shadow: var(--shadow);
}

.floating-category-drawer > header,
.floating-category-drawer > footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px;
  border-bottom: 1px solid var(--line);
}

.floating-category-drawer > header div {
  display: grid;
  gap: 3px;
}

.floating-category-drawer > header small,
.floating-category-drawer > p {
  color: var(--muted);
  font-size: 10px;
}

.floating-category-drawer > header button,
.floating-category-drawer > footer button {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 0;
  background: transparent;
  color: var(--muted);
  cursor: pointer;
}

.floating-category-search {
  display: grid;
  grid-template-columns: auto 1fr;
  align-items: center;
  gap: 8px;
  margin: 12px;
  padding: 0 11px;
  border: 1px solid var(--line);
  border-radius: 8px;
}

.floating-category-search input {
  min-width: 0;
  height: 38px;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--text);
}

.floating-category-drawer nav {
  max-height: min(62vh, 620px);
  overflow: auto;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 7px;
  padding: 0 12px 12px;
  overscroll-behavior: contain;
}

.floating-category-drawer nav button {
  min-width: 0;
  min-height: 48px;
  display: grid;
  grid-template-columns: 30px minmax(0, 1fr);
  align-items: center;
  gap: 8px;
  padding: 7px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
  color: var(--text);
  text-align: left;
  cursor: pointer;
}

.floating-category-drawer nav button.active {
  border-color: var(--text);
  box-shadow: inset 0 0 0 1px var(--text);
}

.floating-category-drawer nav img,
.category-all-mark {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border-radius: 7px;
  object-fit: cover;
  background: var(--soft);
}

.floating-category-drawer nav b {
  overflow: hidden;
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.floating-category-drawer > p {
  padding: 18px;
  text-align: center;
}

.floating-category-drawer > footer {
  border-top: 1px solid var(--line);
  border-bottom: 0;
}

.category-state {
  align-self: center;
  margin-left: auto;
  padding: 0 12px;
  color: var(--muted);
  font-size: 11px;
  white-space: nowrap;
}

.category-retry {
  margin-left: auto;
  padding: 12px 14px;
  color: var(--danger);
  white-space: nowrap;
}

.product-skeleton-grid {
  position: relative;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
}

.product-skeleton-grid i {
  min-height: 360px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: linear-gradient(
    105deg,
    var(--soft) 35%,
    var(--surface) 50%,
    var(--soft) 65%
  );
  background-size: 220% 100%;
  animation: skeleton-shimmer 1.25s linear infinite;
}

.market-loading-label {
  position: absolute;
  left: 50%;
  top: 50%;
  z-index: 1;
  transform: translate(-50%, -50%);
  padding: 9px 13px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--muted);
  font-size: 12px;
}

.market-state {
  min-height: 260px;
  border: 1px dashed var(--line);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 10px;
  color: var(--muted);
}

.market-state strong {
  color: var(--text);
}

.market-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-top: 28px;
  color: var(--muted);
  font-size: 12px;
}

.market-inline-error {
  margin: 14px 0 0;
  color: var(--danger);
  text-align: center;
  font-size: 12px;
}

.spinning {
  animation: spin 0.8s linear infinite;
}

@keyframes skeleton-shimmer {
  to {
    background-position-x: -220%;
  }
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 900px) {
  .product-skeleton-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .product-grid.product-layout-compact,
  .product-grid.product-layout-dense {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  .product-layout-list .product-card {
    grid-template-columns: 180px minmax(0, 1fr);
  }
  .product-layout-list .product-body {
    display: block;
  }
  .product-layout-list .product-bottom {
    margin-top: 12px;
    border-top: 1px solid var(--line);
  }
}

@media (min-width: 901px) and (max-width: 1100px) {
  .product-skeleton-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  .product-grid.product-layout-compact,
  .product-grid.product-layout-dense {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@media (max-width: 620px) {
  .home-banner-secondary-grid,
  .product-skeleton-grid {
    grid-template-columns: 1fr;
  }

  .banner-status,
  .market-footer {
    align-items: stretch;
    flex-direction: column;
    text-align: center;
  }

  .market-footer .button {
    width: 100%;
  }
  .market-layout-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }
  .market-layout-toolbar > small {
    display: none;
  }
  .market-layout-toolbar button b {
    display: none;
  }
  .product-grid.product-layout-compact,
  .product-grid.product-layout-dense {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .product-layout-list .product-card {
    grid-template-columns: 118px minmax(0, 1fr);
  }
  .product-layout-list .product-visual {
    min-height: 170px;
  }
  .product-layout-list .product-body {
    padding: 13px;
  }
  .product-layout-list .tag-row,
  .product-layout-list .product-body > p,
  .product-layout-list .product-meta > span {
    display: none;
  }
  .floating-category-nav {
    top: auto;
    bottom: 18px;
    transform: none;
  }
  .floating-category-trigger {
    width: 48px;
    min-height: 86px;
  }
  .floating-category-trigger span {
    display: none;
  }
  .floating-category-drawer nav {
    max-height: 55vh;
  }
}
</style>
