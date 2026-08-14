<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { RouterLink } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  BadgeCheck,
  BadgePercent,
  Building2,
  ChevronLeft,
  ChevronRight,
  CircleAlert,
  CircleDollarSign,
  Copy,
  Globe2,
  LayoutDashboard,
  PackageSearch,
  RefreshCw,
  Save,
  Search,
  Settings,
  ShieldCheck,
  ShoppingBag,
  Store,
  Trash2,
  WalletCards,
} from "@lucide/vue";
import {
  createResellerDomain,
  deleteResellerDomain,
  fetchMyReseller,
  fetchResellerCatalog,
  fetchResellerOrders,
  updateResellerSite,
  upsertResellerProductRule,
  verifyResellerDomain,
} from "../api";
import type {
  ResellerCatalogItem,
  ResellerDomain,
  ResellerOrder,
  ResellerOverview,
  ResellerPage,
  ResellerProductRule,
  ResellerSite,
  ResellerSitePayload,
  ResellerStatus,
} from "../types";
import ResellerWithdrawalCenter from "./ResellerWithdrawalCenter.vue";
import {
  formatMinor,
  minorToMajorInput,
  parseMajorToMinor,
} from "../utils/money";

const props = defineProps<{ section: string }>();
const { t, locale } = useI18n();

const nav = computed(
  () =>
    [
      ["dashboard", t("reseller.console.nav.dashboard"), LayoutDashboard],
      ["products", t("reseller.console.nav.products"), PackageSearch],
      ["orders", t("reseller.console.nav.orders"), ShoppingBag],
      ["domains", t("reseller.console.nav.domains"), Globe2],
      ["wallet", t("reseller.console.nav.wallet"), WalletCards],
      ["site", t("reseller.console.nav.site"), Settings],
    ] as const,
);
type ConsoleSection = (typeof nav.value)[number][0];

const section = computed<ConsoleSection>(() => {
  const matched = nav.value.find((item) => item[0] === props.section);
  return matched?.[0] || "dashboard";
});
const currentTitle = computed(
  () =>
    nav.value.find((item) => item[0] === section.value)?.[1] ||
    t("reseller.console.nav.dashboard"),
);

const data = ref<ResellerOverview | null>(null);
const loading = ref(false);
const error = ref("");
const sectionError = ref("");
const notice = ref("");
const copied = ref("");
let copyTimer: ReturnType<typeof setTimeout> | undefined;
let loadSequence = 0;

const catalog = ref<ResellerPage<ResellerCatalogItem>>({
  items: [],
  total: 0,
  page: 1,
  page_size: 8,
});
const catalogLoading = ref(false);
const catalogInput = ref("");
const catalogQuery = ref("");
const savingRule = ref("");

const orders = ref<ResellerPage<ResellerOrder>>({
  items: [],
  total: 0,
  page: 1,
  page_size: 12,
});
const ordersLoading = ref(false);
const dashboardOrders = ref<ResellerPage<ResellerOrder>>({
  items: [],
  total: 0,
  page: 1,
  page_size: 5,
});

const domainInput = ref("");
const domainMutation = ref("");
const savingSite = ref(false);

interface RuleDraft {
  enabled: boolean;
  pricingMode: "markup" | "fixed";
  markupPercent: string;
  fixedPrice: string;
}

interface RuleTarget {
  key: string;
  variantID?: string;
  name: string;
  sku: string;
  platformPrice: number;
  currency: string;
  stock: number;
  rule?: ResellerProductRule;
}

const ruleDrafts = reactive<Record<string, RuleDraft>>({});
const siteForm = reactive<ResellerSitePayload>({
  site_name: "",
  logo_url: "",
  theme: { mode: "system", density: "comfortable" },
  seo: { title: "", description: "" },
  support: { email: "", url: "" },
});

const profile = computed(() => data.value?.profile || null);
const creditState = computed(() => data.value?.credit || null);
const wholesalePolicy = computed(() => data.value?.wholesale || null);
const isActive = computed(() => profile.value?.status === "active");
const enabledRuleCount = computed(
  () => data.value?.product_rules.filter((rule) => rule.enabled).length || 0,
);
const readyDomainCount = computed(
  () =>
    data.value?.domains.filter(
      (domain) => domain.status === "active" && domain.tls_status === "active",
    ).length || 0,
);
const dashboardMargin = computed(() =>
  dashboardOrders.value.items.reduce((sum, item) => sum + item.margin, 0),
);
const catalogPageCount = computed(() =>
  Math.max(1, Math.ceil(catalog.value.total / catalog.value.page_size)),
);
const orderPageCount = computed(() =>
  Math.max(1, Math.ceil(orders.value.total / orders.value.page_size)),
);
const readOnlyMessage = computed(() => {
  switch (profile.value?.status) {
    case "pending":
      return t("reseller.console.readOnlyPending");
    case "suspended":
      return t("reseller.console.readOnlySuspended");
    case "rejected":
      return t("reseller.console.readOnlyRejected");
    default:
      return "";
  }
});

function statusLabel(status?: ResellerStatus) {
  return status &&
    ["pending", "active", "suspended", "rejected"].includes(status)
    ? t(`reseller.console.status.${status}`)
    : t("reseller.console.unknown");
}

function domainStatus(status: string) {
  return [
    "pending_verification",
    "verified",
    "active",
    "suspended",
    "rejected",
  ].includes(status)
    ? t(`reseller.console.domainStatus.${status}`)
    : status;
}

function tlsStatus(status: string) {
  return ["pending", "provisioning", "active", "failed", "disabled"].includes(
    status,
  )
    ? t(`reseller.console.tlsStatus.${status}`)
    : status;
}

function orderStatus(status: string) {
  return [
    "pending_payment",
    "paid",
    "processing",
    "delivered",
    "completed",
    "cancelled",
    "expired",
    "refunded",
    "failed",
  ].includes(status)
    ? t(`reseller.console.orderStatus.${status}`)
    : status;
}

function paymentStatus(status: string) {
  return [
    "unpaid",
    "pending",
    "paid",
    "failed",
    "refunded",
    "partially_refunded",
  ].includes(status)
    ? t(`reseller.console.paymentStatus.${status}`)
    : status;
}

function requestError(reason: any, fallback: string) {
  return reason?.response?.data?.message || fallback;
}

function money(value?: number, currency = data.value?.wallet?.currency) {
  return formatMinor(value, currency, locale);
}

function percent(basisPoints?: number) {
  return `${(Number(basisPoints || 0) / 100).toLocaleString(locale.value, {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  })}%`;
}

function date(value?: string | null) {
  if (!value) return "—";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime())
    ? "—"
    : parsed.toLocaleString(locale.value, { hour12: false });
}

function safeStatusClass(status: string) {
  const allowed = [
    "pending",
    "active",
    "suspended",
    "rejected",
    "pending_verification",
    "verified",
    "provisioning",
    "failed",
    "disabled",
    "paid",
    "processing",
    "delivered",
    "completed",
    "cancelled",
    "expired",
    "refunded",
    "unpaid",
    "partially_refunded",
  ];
  return allowed.includes(status) ? `state-${status}` : "state-other";
}

function parseObject<T extends object>(value: unknown, fallback: T): T {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return { ...fallback, ...(value as Partial<T>) };
  }
  if (typeof value !== "string" || !value.trim()) return { ...fallback };
  try {
    const parsed = JSON.parse(value);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? { ...fallback, ...(parsed as Partial<T>) }
      : { ...fallback };
  } catch {
    return { ...fallback };
  }
}

function syncSiteForm(site?: ResellerSite) {
  const theme = parseObject(site?.theme, {
    mode: "system" as const,
    density: "comfortable" as const,
  });
  const seo = parseObject(site?.seo, { title: "", description: "" });
  const support = parseObject(site?.support, { email: "", url: "" });
  siteForm.site_name = site?.site_name || profile.value?.name || "";
  siteForm.logo_url = site?.logo_url || "";
  siteForm.theme.mode = ["light", "dark", "system"].includes(theme.mode)
    ? theme.mode
    : "system";
  siteForm.theme.density = ["comfortable", "compact"].includes(theme.density)
    ? theme.density
    : "comfortable";
  siteForm.seo.title = seo.title || "";
  siteForm.seo.description = seo.description || "";
  siteForm.support.email = support.email || "";
  siteForm.support.url = support.url || "";
}

async function loadDashboardOrders() {
  try {
    dashboardOrders.value = await fetchResellerOrders(1, 5);
  } catch (reason: any) {
    sectionError.value = requestError(
      reason,
      t("reseller.console.errDashboardOrders"),
    );
  }
}

async function loadCatalog(page = catalog.value.page, clearNotice = true) {
  if (!isActive.value) return;
  catalogLoading.value = true;
  sectionError.value = "";
  if (clearNotice) notice.value = "";
  try {
    const result = await fetchResellerCatalog(page, catalogQuery.value, 8);
    catalog.value = result;
    syncRuleDrafts(result.items);
  } catch (reason: any) {
    sectionError.value = requestError(reason, t("reseller.console.errCatalog"));
  } finally {
    catalogLoading.value = false;
  }
}

async function loadOrders(page = orders.value.page) {
  ordersLoading.value = true;
  sectionError.value = "";
  try {
    orders.value = await fetchResellerOrders(page, 12);
  } catch (reason: any) {
    sectionError.value = requestError(reason, t("reseller.console.errOrders"));
  } finally {
    ordersLoading.value = false;
  }
}

async function load() {
  const sequence = ++loadSequence;
  loading.value = true;
  error.value = "";
  sectionError.value = "";
  notice.value = "";
  try {
    const overview = await fetchMyReseller();
    if (sequence !== loadSequence) return;
    data.value = overview;
    syncSiteForm(overview.site);
    if (section.value === "dashboard") await loadDashboardOrders();
    if (section.value === "products" && overview.profile.status === "active") {
      await loadCatalog(catalog.value.page, false);
    }
    if (section.value === "orders") await loadOrders(orders.value.page);
  } catch (reason: any) {
    if (sequence !== loadSequence) return;
    data.value = null;
    error.value = requestError(reason, t("reseller.console.errLoad"));
  } finally {
    if (sequence === loadSequence) loading.value = false;
  }
}

function productTargets(item: ResellerCatalogItem): RuleTarget[] {
  const variants = Array.isArray(item.variants) ? item.variants : [];
  const basePrice = Math.max(
    item.product.price,
    ...variants.map((variant) => variant.price),
  );
  const baseRule = item.rules.find((rule) => !rule.variant_id);
  return [
    {
      key: `${item.product.id}:base`,
      name: variants.length
        ? t("reseller.console.defaultRule")
        : t("reseller.console.productRule"),
      sku: variants.length
        ? t("reseller.console.defaultRuleHint")
        : item.product.slug,
      platformPrice: basePrice,
      currency: item.product.currency,
      stock: item.stock,
      rule: baseRule,
    },
    ...variants.map((variant) => ({
      key: `${item.product.id}:${variant.id}`,
      variantID: variant.id,
      name: variant.name,
      sku: variant.sku,
      platformPrice: variant.price,
      currency: item.product.currency,
      stock: variant.stock,
      rule: item.rules.find((rule) => rule.variant_id === variant.id),
    })),
  ];
}

function priceInput(value: number, currency: string) {
  return minorToMajorInput(value || 0, currency);
}

function syncRuleDrafts(items: ResellerCatalogItem[]) {
  for (const key of Object.keys(ruleDrafts)) delete ruleDrafts[key];
  for (const item of items) {
    for (const target of productTargets(item)) {
      const rule = target.rule;
      ruleDrafts[target.key] = {
        enabled: rule?.enabled || false,
        pricingMode: rule?.pricing_mode || "markup",
        markupPercent: ((rule?.markup_basis_point || 0) / 100).toFixed(2),
        fixedPrice: priceInput(
          rule?.pricing_mode === "fixed" && rule.fixed_price > 0
            ? rule.fixed_price
            : target.platformPrice,
          target.currency,
        ),
      };
    }
  }
}

function parsePercentage(value: string) {
  const normalized = value.trim();
  if (!/^(?:0|[1-9]\d{0,2})(?:\.\d{1,2})?$/.test(normalized)) return null;
  const basisPoints = Math.round(Number(normalized) * 100);
  return basisPoints >= 0 && basisPoints <= 10000 ? basisPoints : null;
}

function ruleSummary(target: RuleTarget) {
  const draft = ruleDrafts[target.key];
  if (!draft?.enabled) return t("reseller.console.notDistributed");
  if (draft.pricingMode === "fixed") {
    const fixed = parseMajorToMinor(draft.fixedPrice, target.currency);
    return fixed === null
      ? t("reseller.console.fixedPending")
      : t("reseller.console.fixedPrice", {
          amount: money(fixed, target.currency),
        });
  }
  const basisPoints = parsePercentage(draft.markupPercent);
  return basisPoints === null
    ? t("reseller.console.markupPending")
    : t("reseller.console.markupSummary", {
        rate: (basisPoints / 100).toFixed(2),
      });
}

async function saveRule(item: ResellerCatalogItem, target: RuleTarget) {
  if (!isActive.value || savingRule.value) return;
  const draft = ruleDrafts[target.key];
  if (!draft) return;
  error.value = "";
  sectionError.value = "";
  notice.value = "";
  const basisPoints = parsePercentage(draft.markupPercent);
  const fixedPrice = parseMajorToMinor(draft.fixedPrice, target.currency);
  if (draft.pricingMode === "markup" && basisPoints === null) {
    sectionError.value = t("reseller.console.errMarkup");
    return;
  }
  if (
    draft.pricingMode === "fixed" &&
    (fixedPrice === null ||
      fixedPrice < target.platformPrice ||
      fixedPrice > target.platformPrice * 10)
  ) {
    sectionError.value = t("reseller.console.errFixedRange", {
      min: money(target.platformPrice, target.currency),
      max: money(target.platformPrice * 10, target.currency),
    });
    return;
  }
  savingRule.value = target.key;
  try {
    const saved = await upsertResellerProductRule(item.product.id, {
      ...(target.variantID ? { variant_id: target.variantID } : {}),
      enabled: draft.enabled,
      pricing_mode: draft.pricingMode,
      markup_basis_point: draft.pricingMode === "markup" ? basisPoints! : 0,
      fixed_price: draft.pricingMode === "fixed" ? fixedPrice! : 0,
    });
    item.rules = [
      ...item.rules.filter(
        (rule) => (rule.variant_id || "") !== (target.variantID || ""),
      ),
      saved,
    ];
    if (data.value) {
      data.value.product_rules = [
        ...data.value.product_rules.filter(
          (rule) =>
            !(
              rule.product_id === item.product.id &&
              (rule.variant_id || "") === (target.variantID || "")
            ),
        ),
        saved,
      ];
    }
    notice.value = t("reseller.console.ruleSaved", {
      product: item.product.name,
      target: target.name,
    });
  } catch (reason: any) {
    sectionError.value = requestError(
      reason,
      t("reseller.console.errRuleSave"),
    );
  } finally {
    savingRule.value = "";
  }
}

function searchCatalog() {
  catalogQuery.value = catalogInput.value.trim().slice(0, 160);
  loadCatalog(1);
}

async function copyValue(value: string, label: string) {
  error.value = "";
  sectionError.value = "";
  if (!value || !window.isSecureContext || !navigator.clipboard) {
    sectionError.value = t("reseller.console.errClipboard");
    return;
  }
  try {
    await navigator.clipboard.writeText(value);
    copied.value = label;
    if (copyTimer) clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      copied.value = "";
    }, 2200);
  } catch {
    sectionError.value = t("reseller.console.errCopy");
  }
}

function normalizedDomain(value: string) {
  return value.trim().replace(/\.$/, "").toLowerCase();
}

async function createDomain() {
  if (!isActive.value || domainMutation.value) return;
  error.value = "";
  sectionError.value = "";
  notice.value = "";
  const domain = normalizedDomain(domainInput.value);
  if (
    !domain ||
    domain.includes("://") ||
    /[\s/?#]/.test(domain) ||
    !domain.includes(".")
  ) {
    sectionError.value = t("reseller.console.errDomain");
    return;
  }
  domainMutation.value = "create";
  try {
    const result = await createResellerDomain(domain);
    if (data.value) data.value.domains = [result.domain, ...data.value.domains];
    domainInput.value = "";
    notice.value = t("reseller.console.domainAdded", { dns: result.dns_name });
  } catch (reason: any) {
    sectionError.value = requestError(
      reason,
      t("reseller.console.errDomainAdd"),
    );
  } finally {
    domainMutation.value = "";
  }
}

function replaceDomain(next: ResellerDomain) {
  if (!data.value) return;
  data.value.domains = data.value.domains.map((item) =>
    item.id === next.id ? next : item,
  );
}

async function verifyDomain(item: ResellerDomain) {
  if (!isActive.value || domainMutation.value) return;
  error.value = "";
  sectionError.value = "";
  notice.value = "";
  domainMutation.value = item.id;
  try {
    const result = await verifyResellerDomain(item.id);
    replaceDomain(result.domain);
    notice.value = result.notice || t("reseller.console.domainVerified");
  } catch (reason: any) {
    sectionError.value = requestError(reason, t("reseller.console.errVerify"));
  } finally {
    domainMutation.value = "";
  }
}

async function removeDomain(item: ResellerDomain) {
  if (!isActive.value || domainMutation.value) return;
  if (
    !window.confirm(
      t("reseller.console.confirmDeleteDomain", { domain: item.domain }),
    )
  ) {
    return;
  }
  error.value = "";
  sectionError.value = "";
  notice.value = "";
  domainMutation.value = item.id;
  try {
    await deleteResellerDomain(item.id);
    if (data.value) {
      data.value.domains = data.value.domains.filter(
        (domain) => domain.id !== item.id,
      );
    }
    notice.value = t("reseller.console.domainDeleted", { domain: item.domain });
  } catch (reason: any) {
    sectionError.value = requestError(
      reason,
      t("reseller.console.errDomainDelete"),
    );
  } finally {
    domainMutation.value = "";
  }
}

function validOptionalHTTPS(value: string) {
  if (!value) return true;
  try {
    const parsed = new URL(value);
    return (
      parsed.protocol === "https:" &&
      Boolean(parsed.hostname) &&
      !parsed.username &&
      !parsed.password &&
      !parsed.hash
    );
  } catch {
    return false;
  }
}

async function saveSite() {
  if (!isActive.value || savingSite.value) return;
  error.value = "";
  sectionError.value = "";
  notice.value = "";
  const payload: ResellerSitePayload = {
    site_name: siteForm.site_name.trim(),
    logo_url: siteForm.logo_url.trim(),
    theme: { ...siteForm.theme },
    seo: {
      title: siteForm.seo.title.trim(),
      description: siteForm.seo.description.trim(),
    },
    support: {
      email: siteForm.support.email.trim().toLowerCase(),
      url: siteForm.support.url.trim(),
    },
  };
  const nameLength = Array.from(payload.site_name).length;
  if (nameLength < 2 || nameLength > 160) {
    sectionError.value = t("reseller.console.errSiteName");
    return;
  }
  if (Array.from(payload.seo.title).length > 160) {
    sectionError.value = t("reseller.console.errSeoTitle");
    return;
  }
  if (Array.from(payload.seo.description).length > 500) {
    sectionError.value = t("reseller.console.errSeoDesc");
    return;
  }
  if (
    payload.support.email &&
    (!/^\S+@\S+\.\S+$/.test(payload.support.email) ||
      Array.from(payload.support.email).length > 190)
  ) {
    sectionError.value = t("reseller.console.errSupportEmail");
    return;
  }
  if (!validOptionalHTTPS(payload.logo_url)) {
    sectionError.value = t("reseller.console.errLogoUrl");
    return;
  }
  if (!validOptionalHTTPS(payload.support.url)) {
    sectionError.value = t("reseller.console.errSupportUrl");
    return;
  }
  savingSite.value = true;
  try {
    const site = await updateResellerSite(payload);
    if (data.value) data.value.site = site;
    syncSiteForm(site);
    notice.value = t("reseller.console.siteSaved");
  } catch (reason: any) {
    sectionError.value = requestError(
      reason,
      t("reseller.console.errSiteSave"),
    );
  } finally {
    savingSite.value = false;
  }
}

watch(
  () => props.section,
  () => load(),
  { immediate: true },
);
onBeforeUnmount(() => {
  loadSequence += 1;
  if (copyTimer) clearTimeout(copyTimer);
});
</script>

<template>
  <section class="reseller-console">
    <aside>
      <div class="reseller-brand">
        <span>LQ</span>
        <div>
          <b>{{ profile?.name || "LinLinQi Channel" }}</b>
          <small>{{ t("kicker.resellerConsole") }}</small>
        </div>
      </div>
      <nav>
        <RouterLink
          v-for="item in nav"
          :key="item[0]"
          :to="`/reseller/${item[0]}`"
        >
          <component :is="item[2]" />{{ item[1] }}
        </RouterLink>
      </nav>
      <RouterLink to="/">{{ t("reseller.console.backToMain") }}</RouterLink>
    </aside>

    <main>
      <header class="console-header">
        <div>
          <span class="kicker">{{ t("kicker.resellerConsole") }}</span>
          <h1>{{ currentTitle }}</h1>
        </div>
        <span
          v-if="profile"
          :class="['profile-state', safeStatusClass(profile.status)]"
        >
          <BadgeCheck v-if="isActive" />
          <CircleAlert v-else />
          {{ statusLabel(profile.status) }}
        </span>
      </header>

      <nav
        class="console-mobile-nav"
        :aria-label="t('reseller.console.navAria')"
      >
        <RouterLink
          v-for="item in nav"
          :key="item[0]"
          :to="`/reseller/${item[0]}`"
        >
          <component :is="item[2]" />{{ item[1] }}
        </RouterLink>
      </nav>

      <p
        v-if="error || sectionError"
        class="console-feedback error"
        role="alert"
      >
        {{ error || sectionError }}
      </p>
      <p v-if="notice" class="console-feedback notice" role="status">
        {{ notice }}
      </p>

      <section v-if="loading && !data" class="console-loading">
        <RefreshCw />
        <span>{{ t("reseller.console.loading") }}</span>
      </section>

      <section v-else-if="!data" class="console-unavailable">
        <Building2 />
        <h2>{{ t("reseller.console.unavailable") }}</h2>
        <p>{{ error || t("reseller.console.notLoaded") }}</p>
        <div>
          <button class="button secondary" type="button" @click="load">
            {{ t("reseller.console.reload") }}
          </button>
          <RouterLink class="button primary" to="/reseller/apply">
            {{ t("reseller.console.goApply") }}
          </RouterLink>
        </div>
      </section>

      <template v-else>
        <section
          v-if="!isActive"
          :class="['readonly-banner', safeStatusClass(profile!.status)]"
        >
          <CircleAlert />
          <div>
            <b>{{ t("reseller.console.readonlyMode") }}</b>
            <span>{{ readOnlyMessage }}</span>
          </div>
          <RouterLink to="/account/tickets">{{
            t("reseller.console.contactOps")
          }}</RouterLink>
        </section>

        <section
          v-if="creditState?.breached"
          class="credit-breach-banner"
          role="alert"
        >
          <CircleAlert />
          <div>
            <b>{{ t("reseller.console.creditBreachTitle") }}</b>
            <span>{{
              t("reseller.console.creditBreachBody", {
                exposure: money(creditState.exposure),
                limit: money(creditState.limit),
              })
            }}</span>
          </div>
          <RouterLink to="/account/tickets">{{
            t("reseller.console.contactOps")
          }}</RouterLink>
        </section>

        <template v-if="section === 'dashboard'">
          <section class="section-heading">
            <div>
              <span>{{ t("kicker.businessSnapshot") }}</span>
              <h2>{{ t("reseller.console.liveData") }}</h2>
            </div>
            <button type="button" :disabled="loading" @click="load">
              <RefreshCw />{{
                loading
                  ? t("reseller.console.refreshing")
                  : t("reseller.console.refresh")
              }}
            </button>
          </section>
          <div class="reseller-metrics">
            <article>
              <span>{{ t("reseller.console.walletBalance") }}</span>
              <strong>{{ money(data.wallet?.balance) }}</strong>
              <small>{{
                t("reseller.console.frozen", {
                  amount: money(data.wallet?.frozen),
                })
              }}</small>
            </article>
            <article>
              <span>{{ t("reseller.console.totalOrders") }}</span>
              <strong>{{ dashboardOrders.total }}</strong>
              <small>{{
                t("reseller.console.recentProfit", {
                  amount: money(dashboardMargin),
                })
              }}</small>
            </article>
            <article>
              <span>{{ t("reseller.console.enabledRules") }}</span>
              <strong>{{ enabledRuleCount }}</strong>
              <small>{{
                t("reseller.console.totalRules", {
                  n: data.product_rules.length,
                })
              }}</small>
            </article>
            <article>
              <span>{{ t("reseller.console.liveDomains") }}</span>
              <strong>{{ readyDomainCount }}</strong>
              <small>{{
                t("reseller.console.submittedDomains", {
                  n: data.domains.length,
                })
              }}</small>
            </article>
          </div>

          <div class="dashboard-grid">
            <section class="console-panel recent-orders-panel">
              <header>
                <div>
                  <span>{{ t("kicker.recentOrders") }}</span>
                  <h2>{{ t("reseller.console.recentOrders") }}</h2>
                </div>
                <RouterLink to="/reseller/orders">{{
                  t("reseller.console.viewAll")
                }}</RouterLink>
              </header>
              <div v-if="dashboardOrders.items.length" class="compact-orders">
                <article v-for="item in dashboardOrders.items" :key="item.id">
                  <div>
                    <b>{{ item.order_no }}</b>
                    <small>{{ date(item.created_at) }}</small>
                  </div>
                  <span>{{ money(item.total, item.currency) }}</span>
                  <em>{{
                    t("reseller.console.profit", {
                      amount: money(item.margin, item.currency),
                    })
                  }}</em>
                  <i :class="safeStatusClass(item.status)">
                    {{ orderStatus(item.status) }}
                  </i>
                </article>
              </div>
              <p v-else class="true-empty">
                {{ t("reseller.console.noOrders") }}
              </p>
            </section>

            <section class="console-panel account-summary">
              <header>
                <div>
                  <span>{{ t("kicker.account") }}</span>
                  <h2>{{ t("reseller.console.accountSite") }}</h2>
                </div>
                <Store />
              </header>
              <dl>
                <div>
                  <dt>{{ t("reseller.console.businessName") }}</dt>
                  <dd>{{ profile!.name }}</dd>
                </div>
                <div>
                  <dt>{{ t("reseller.console.resellerCode") }}</dt>
                  <dd>{{ profile!.code }}</dd>
                </div>
                <div>
                  <dt>{{ t("reseller.console.wholesaleLevel") }}</dt>
                  <dd>
                    {{
                      wholesalePolicy?.configured
                        ? t("reseller.console.wholesalePolicyValue", {
                            level: wholesalePolicy.level,
                            name: wholesalePolicy.name,
                          })
                        : t("reseller.console.wholesaleUnconfigured", {
                            level: profile!.wholesale_level,
                          })
                    }}
                  </dd>
                </div>
                <div>
                  <dt>{{ t("reseller.console.settlementDiscount") }}</dt>
                  <dd>
                    {{
                      wholesalePolicy?.configured
                        ? percent(wholesalePolicy.discount_basis_point)
                        : "—"
                    }}
                  </dd>
                </div>
                <div>
                  <dt>{{ t("reseller.console.creditLimit") }}</dt>
                  <dd>{{ money(creditState?.limit) }}</dd>
                </div>
                <div>
                  <dt>{{ t("reseller.console.creditExposure") }}</dt>
                  <dd :class="{ 'danger-text': creditState?.breached }">
                    {{ money(creditState?.exposure) }} /
                    {{ money(creditState?.remaining) }}
                  </dd>
                </div>
                <div>
                  <dt>{{ t("reseller.console.siteName") }}</dt>
                  <dd>{{ data.site?.site_name || profile!.name }}</dd>
                </div>
                <div>
                  <dt>{{ t("reseller.console.appliedAt") }}</dt>
                  <dd>{{ date(profile!.applied_at) }}</dd>
                </div>
              </dl>
            </section>
          </div>

          <section class="console-panel dashboard-domains">
            <header>
              <div>
                <span>{{ t("kicker.domainHealth") }}</span>
                <h2>{{ t("reseller.console.domainHealth") }}</h2>
              </div>
              <RouterLink to="/reseller/domains">{{
                t("reseller.console.manageDomains")
              }}</RouterLink>
            </header>
            <div v-if="data.domains.length" class="domain-health-list">
              <article v-for="item in data.domains" :key="item.id">
                <Globe2 />
                <div>
                  <b>{{ item.domain }}</b
                  ><small>{{ date(item.updated_at) }}</small>
                </div>
                <span :class="safeStatusClass(item.status)">{{
                  domainStatus(item.status)
                }}</span>
                <em :class="safeStatusClass(item.tls_status)"
                  >TLS {{ tlsStatus(item.tls_status) }}</em
                >
              </article>
            </div>
            <p v-else class="true-empty">
              {{ t("reseller.console.noDomains") }}
            </p>
          </section>
        </template>

        <template v-else-if="section === 'products'">
          <section class="section-heading product-heading">
            <div>
              <span>{{ t("kicker.catalogPricing") }}</span>
              <h2>{{ t("reseller.console.catalogPricing") }}</h2>
              <p>{{ t("reseller.console.catalogSub") }}</p>
            </div>
            <form
              v-if="isActive"
              class="catalog-search"
              @submit.prevent="searchCatalog"
            >
              <Search />
              <input
                v-model="catalogInput"
                maxlength="160"
                :placeholder="t('reseller.console.searchPlaceholder')"
              />
              <button type="submit" :disabled="catalogLoading">
                {{ t("reseller.console.search") }}
              </button>
            </form>
          </section>

          <section
            class="console-panel settlement-policy-card"
            :class="{
              unavailable:
                !wholesalePolicy?.configured || !wholesalePolicy?.enabled,
            }"
          >
            <BadgePercent />
            <div>
              <span>{{ t("kicker.wholesaleSettlement") }}</span>
              <h3>
                {{
                  wholesalePolicy?.configured
                    ? t("reseller.console.wholesalePolicyValue", {
                        level: wholesalePolicy.level,
                        name: wholesalePolicy.name,
                      })
                    : t("reseller.console.policyUnavailable")
                }}
              </h3>
              <p>{{ t("reseller.console.settlementRuleExplanation") }}</p>
            </div>
            <dl>
              <div>
                <dt>{{ t("reseller.console.settlementDiscount") }}</dt>
                <dd>
                  {{
                    wholesalePolicy?.configured
                      ? percent(wholesalePolicy.discount_basis_point)
                      : "—"
                  }}
                </dd>
              </div>
              <div>
                <dt>{{ t("reseller.console.customerPriceFloor") }}</dt>
                <dd>{{ t("reseller.console.publicPrice") }}</dd>
              </div>
              <div>
                <dt>{{ t("reseller.console.policyEffectScope") }}</dt>
                <dd>{{ t("reseller.console.newOrdersOnly") }}</dd>
              </div>
            </dl>
          </section>

          <section v-if="!isActive" class="console-panel restricted-panel">
            <PackageSearch />
            <div>
              <h2>{{ t("reseller.console.restrictedTitle") }}</h2>
              <p>
                {{
                  t("reseller.console.restrictedDesc", {
                    n: data.product_rules.length,
                  })
                }}
              </p>
            </div>
          </section>

          <section
            v-else-if="catalogLoading && !catalog.items.length"
            class="console-loading inline"
          >
            <RefreshCw />{{ t("reseller.console.loadingCatalog") }}
          </section>

          <div v-else class="product-rule-list">
            <article
              v-for="item in catalog.items"
              :key="item.product.id"
              class="console-panel product-rule-card"
            >
              <header>
                <div>
                  <span>{{
                    item.product.category?.name ||
                    t("reseller.console.digitalGoods")
                  }}</span>
                  <h2>{{ item.product.name }}</h2>
                  <p v-if="item.product.summary">{{ item.product.summary }}</p>
                </div>
                <div class="product-stock">
                  <b>{{ item.stock }}</b>
                  <small>{{ t("reseller.console.sellableStock") }}</small>
                </div>
              </header>

              <div class="rule-table-head">
                <span>{{ t("reseller.console.ruleTarget") }}</span
                ><span>{{ t("reseller.console.salesStatus") }}</span
                ><span>{{ t("reseller.console.pricingMode") }}</span
                ><span>{{ t("reseller.console.pricingValue") }}</span
                ><span>{{ t("reseller.console.actions") }}</span>
              </div>
              <form
                v-for="target in productTargets(item)"
                :key="target.key"
                class="rule-row"
                @submit.prevent="saveRule(item, target)"
              >
                <div class="rule-target">
                  <b>{{ target.name }}</b>
                  <small
                    >{{ target.sku }} ·
                    {{
                      t("reseller.console.platformPrice", {
                        amount: money(target.platformPrice, target.currency),
                      })
                    }}
                    ·
                    {{
                      t("reseller.console.stockCount", { n: target.stock })
                    }}</small
                  >
                </div>
                <label class="rule-toggle">
                  <input
                    v-model="ruleDrafts[target.key].enabled"
                    type="checkbox"
                    :disabled="savingRule === target.key"
                  />
                  <span>{{
                    ruleDrafts[target.key].enabled
                      ? t("reseller.console.enabled")
                      : t("reseller.console.disabled")
                  }}</span>
                </label>
                <select
                  v-model="ruleDrafts[target.key].pricingMode"
                  :disabled="savingRule === target.key"
                >
                  <option value="markup">
                    {{ t("reseller.console.markup") }}
                  </option>
                  <option value="fixed">
                    {{ t("reseller.console.fixed") }}
                  </option>
                </select>
                <label class="rule-value">
                  <input
                    v-if="ruleDrafts[target.key].pricingMode === 'markup'"
                    v-model="ruleDrafts[target.key].markupPercent"
                    inputmode="decimal"
                    maxlength="6"
                    :disabled="savingRule === target.key"
                  />
                  <input
                    v-else
                    v-model="ruleDrafts[target.key].fixedPrice"
                    inputmode="decimal"
                    maxlength="13"
                    :disabled="savingRule === target.key"
                  />
                  <span>{{
                    ruleDrafts[target.key].pricingMode === "markup"
                      ? "%"
                      : target.currency
                  }}</span>
                  <small>{{ ruleSummary(target) }}</small>
                </label>
                <button class="rule-save" :disabled="savingRule === target.key">
                  <Save />{{
                    savingRule === target.key
                      ? t("reseller.console.saving")
                      : t("reseller.console.save")
                  }}
                </button>
              </form>
            </article>
            <section
              v-if="!catalog.items.length"
              class="console-panel true-empty large"
            >
              {{
                catalogQuery
                  ? t("reseller.console.noMatchCatalog")
                  : t("reseller.console.noCatalog")
              }}
            </section>
          </div>

          <nav
            v-if="isActive && catalog.total > catalog.page_size"
            class="console-pagination"
            :aria-label="t('reseller.console.catalogPagination')"
          >
            <button
              type="button"
              :disabled="catalogLoading || catalog.page <= 1"
              @click="loadCatalog(catalog.page - 1)"
            >
              <ChevronLeft />{{ t("reseller.console.prev") }}
            </button>
            <span>{{
              t("reseller.console.catalogPage", {
                page: catalog.page,
                total: catalogPageCount,
                count: catalog.total,
              })
            }}</span>
            <button
              type="button"
              :disabled="catalogLoading || catalog.page >= catalogPageCount"
              @click="loadCatalog(catalog.page + 1)"
            >
              {{ t("reseller.console.next") }}<ChevronRight />
            </button>
          </nav>
        </template>

        <template v-else-if="section === 'orders'">
          <section class="section-heading">
            <div>
              <span>{{ t("kicker.resellerOrders") }}</span>
              <h2>{{ t("reseller.console.ordersTitle") }}</h2>
              <p>{{ t("reseller.console.ordersSub") }}</p>
            </div>
            <button
              type="button"
              :disabled="ordersLoading"
              @click="loadOrders(orders.page)"
            >
              <RefreshCw />{{
                ordersLoading
                  ? t("reseller.console.refreshing")
                  : t("reseller.console.refresh")
              }}
            </button>
          </section>
          <section class="console-panel order-ledger">
            <div class="order-table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>{{ t("reseller.console.orderNo") }}</th>
                    <th>{{ t("reseller.console.createdAt") }}</th>
                    <th>{{ t("reseller.console.orderStatus") }}</th>
                    <th>{{ t("reseller.console.paymentStatus") }}</th>
                    <th>{{ t("reseller.console.orderAmount") }}</th>
                    <th>{{ t("reseller.console.resellerMargin") }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="item in orders.items" :key="item.id">
                    <td :data-label="t('reseller.console.orderNo')">
                      <b>{{ item.order_no }}</b>
                    </td>
                    <td :data-label="t('reseller.console.createdAt')">
                      {{ date(item.created_at) }}
                    </td>
                    <td :data-label="t('reseller.console.orderStatus')">
                      <span :class="safeStatusClass(item.status)">{{
                        orderStatus(item.status)
                      }}</span>
                    </td>
                    <td :data-label="t('reseller.console.paymentStatus')">
                      <span :class="safeStatusClass(item.payment_status)">{{
                        paymentStatus(item.payment_status)
                      }}</span>
                    </td>
                    <td :data-label="t('reseller.console.orderAmount')">
                      {{ money(item.total, item.currency) }}
                    </td>
                    <td
                      class="margin"
                      :data-label="t('reseller.console.resellerMargin')"
                    >
                      +{{ money(item.margin, item.currency) }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p
              v-if="!ordersLoading && !orders.items.length"
              class="true-empty large"
            >
              {{ t("reseller.console.noOrders") }}
            </p>
          </section>
          <nav
            v-if="orders.total > orders.page_size"
            class="console-pagination"
            :aria-label="t('reseller.console.orderPagination')"
          >
            <button
              type="button"
              :disabled="ordersLoading || orders.page <= 1"
              @click="loadOrders(orders.page - 1)"
            >
              <ChevronLeft />{{ t("reseller.console.prev") }}
            </button>
            <span>{{
              t("reseller.console.orderPage", {
                page: orders.page,
                total: orderPageCount,
                count: orders.total,
              })
            }}</span>
            <button
              type="button"
              :disabled="ordersLoading || orders.page >= orderPageCount"
              @click="loadOrders(orders.page + 1)"
            >
              {{ t("reseller.console.next") }}<ChevronRight />
            </button>
          </nav>
        </template>

        <template v-else-if="section === 'domains'">
          <section class="section-heading">
            <div>
              <span>{{ t("kicker.customDomains") }}</span>
              <h2>{{ t("reseller.console.domainTitle") }}</h2>
              <p>{{ t("reseller.console.domainSub") }}</p>
            </div>
          </section>
          <section class="console-panel domain-create-panel">
            <form @submit.prevent="createDomain">
              <label>
                {{ t("reseller.console.newDomain") }}
                <input
                  v-model="domainInput"
                  maxlength="253"
                  autocomplete="off"
                  placeholder="shop.example.com"
                  :disabled="
                    !isActive ||
                    Boolean(domainMutation) ||
                    data.domains.length >= 5
                  "
                />
              </label>
              <button
                class="button primary"
                :disabled="
                  !isActive ||
                  Boolean(domainMutation) ||
                  data.domains.length >= 5
                "
              >
                {{
                  domainMutation === "create"
                    ? t("reseller.console.adding")
                    : t("reseller.console.addDomain")
                }}
              </button>
            </form>
            <small>{{
              t("reseller.console.domainHint", { n: data.domains.length })
            }}</small>
          </section>

          <div class="domain-management-list">
            <article
              v-for="item in data.domains"
              :key="item.id"
              class="console-panel domain-management-card"
            >
              <header>
                <div class="domain-title">
                  <Globe2 />
                  <div>
                    <h2>{{ item.domain }}</h2>
                    <small>{{
                      t("reseller.console.addedAt", {
                        time: date(item.created_at),
                      })
                    }}</small>
                  </div>
                </div>
                <div class="domain-states">
                  <span :class="safeStatusClass(item.status)">{{
                    domainStatus(item.status)
                  }}</span>
                  <span :class="safeStatusClass(item.tls_status)"
                    >TLS {{ tlsStatus(item.tls_status) }}</span
                  >
                </div>
              </header>
              <div class="dns-guide">
                <div>
                  <span>{{ t("reseller.console.recordType") }}</span
                  ><code>TXT</code>
                </div>
                <div>
                  <span>{{ t("reseller.console.hostRecord") }}</span
                  ><code>_linlinqi.{{ item.domain }}</code>
                  <button
                    type="button"
                    @click="
                      copyValue(
                        `_linlinqi.${item.domain}`,
                        `dns-name-${item.id}`,
                      )
                    "
                  >
                    <Copy />{{
                      copied === `dns-name-${item.id}`
                        ? t("reseller.console.copied")
                        : t("reseller.console.copy")
                    }}
                  </button>
                </div>
                <div>
                  <span>{{ t("reseller.console.recordValue") }}</span
                  ><code>{{ item.verification_token }}</code>
                  <button
                    type="button"
                    @click="
                      copyValue(item.verification_token, `dns-value-${item.id}`)
                    "
                  >
                    <Copy />{{
                      copied === `dns-value-${item.id}`
                        ? t("reseller.console.copied")
                        : t("reseller.console.copy")
                    }}
                  </button>
                </div>
              </div>
              <p class="dns-note">{{ t("reseller.console.dnsNote") }}</p>
              <footer>
                <span v-if="item.verified_at">{{
                  t("reseller.console.verifiedAt", {
                    time: date(item.verified_at),
                  })
                }}</span>
                <span v-else>{{ t("reseller.console.noTxt") }}</span>
                <div>
                  <button
                    v-if="item.status === 'pending_verification'"
                    type="button"
                    :disabled="!isActive || Boolean(domainMutation)"
                    @click="verifyDomain(item)"
                  >
                    <BadgeCheck />{{
                      domainMutation === item.id
                        ? t("reseller.console.detecting")
                        : t("reseller.console.verifyDns")
                    }}
                  </button>
                  <button
                    class="danger"
                    type="button"
                    :disabled="!isActive || Boolean(domainMutation)"
                    @click="removeDomain(item)"
                  >
                    <Trash2 />{{ t("reseller.console.delete") }}
                  </button>
                </div>
              </footer>
            </article>
            <section
              v-if="!data.domains.length"
              class="console-panel true-empty large"
            >
              {{ t("reseller.console.noDomains") }}
            </section>
          </div>
        </template>

        <template v-else-if="section === 'wallet'">
          <section class="section-heading">
            <div>
              <span>{{ t("kicker.walletLedger") }}</span>
              <h2>{{ t("reseller.console.walletLedger") }}</h2>
              <p>{{ t("reseller.console.walletSub") }}</p>
            </div>
            <button type="button" :disabled="loading" @click="load">
              <RefreshCw />{{
                loading
                  ? t("reseller.console.refreshing")
                  : t("reseller.console.refresh")
              }}
            </button>
          </section>
          <div class="wallet-summary-grid">
            <article class="console-panel wallet-balance-card">
              <CircleDollarSign />
              <span>{{ t("reseller.console.bookBalance") }}</span>
              <strong>{{ money(data.wallet?.balance) }}</strong>
              <small>{{ data.wallet?.currency || "—" }}</small>
            </article>
            <article class="console-panel">
              <span>{{ t("reseller.console.frozenAmount") }}</span>
              <strong>{{ money(data.wallet?.frozen) }}</strong>
              <small>{{ t("reseller.console.frozenSub") }}</small>
            </article>
            <article class="console-panel">
              <span>{{ t("reseller.console.creditLimit") }}</span>
              <strong>{{ money(creditState?.limit) }}</strong>
              <small>{{ t("reseller.console.creditLimitSub") }}</small>
            </article>
            <article
              class="console-panel credit-risk-card"
              :class="{ breached: creditState?.breached }"
            >
              <span>{{ t("reseller.console.creditExposure") }}</span>
              <strong>{{ money(creditState?.exposure) }}</strong>
              <small>{{
                creditState?.breached
                  ? t("reseller.console.creditExposureBreached")
                  : t("reseller.console.creditExposureSub")
              }}</small>
            </article>
            <article class="console-panel">
              <span>{{ t("reseller.console.creditRemaining") }}</span>
              <strong>{{ money(creditState?.remaining) }}</strong>
              <small>{{ t("reseller.console.creditRemainingSub") }}</small>
            </article>
            <article class="console-panel">
              <span>{{ t("reseller.console.ledgerVersion") }}</span>
              <strong>#{{ data.wallet?.version || 0 }}</strong>
              <small>{{ t("reseller.console.versionSub") }}</small>
            </article>
          </div>
          <section class="console-panel wallet-metadata">
            <header>
              <div>
                <span>{{ t("kicker.accountLedger") }}</span>
                <h2>{{ t("reseller.console.accountLedger") }}</h2>
              </div>
              <WalletCards />
            </header>
            <dl>
              <div>
                <dt>{{ t("reseller.console.currency") }}</dt>
                <dd>{{ data.wallet?.currency || "—" }}</dd>
              </div>
              <div>
                <dt>{{ t("reseller.console.accountType") }}</dt>
                <dd>{{ t("reseller.console.resellerWallet") }}</dd>
              </div>
              <div>
                <dt>{{ t("reseller.console.ledgerCreated") }}</dt>
                <dd>{{ date(data.wallet?.created_at) }}</dd>
              </div>
              <div>
                <dt>{{ t("reseller.console.lastUpdated") }}</dt>
                <dd>{{ date(data.wallet?.updated_at) }}</dd>
              </div>
            </dl>
            <p>{{ t("reseller.console.ledgerNote") }}</p>
            <p class="credit-policy-note">
              <ShieldCheck />{{ t("reseller.console.creditSemanticNote") }}
            </p>
          </section>
          <ResellerWithdrawalCenter
            :active="isActive"
            :balance="data.wallet?.balance || 0"
            :frozen="data.wallet?.frozen || 0"
            :currency="data.wallet?.currency || ''"
            @changed="load"
          />
        </template>

        <template v-else-if="section === 'site'">
          <section class="section-heading">
            <div>
              <span>{{ t("kicker.storefrontSettings") }}</span>
              <h2>{{ t("reseller.console.storefront") }}</h2>
              <p>{{ t("reseller.console.storefrontSub") }}</p>
            </div>
          </section>
          <div class="site-settings-grid">
            <form class="console-panel site-form" @submit.prevent="saveSite">
              <div class="form-section">
                <span>{{ t("kicker.brand") }}</span>
                <h3>{{ t("reseller.console.brandInfo") }}</h3>
                <label
                  >{{ t("reseller.console.siteNameLabel")
                  }}<input
                    v-model="siteForm.site_name"
                    maxlength="160"
                    :disabled="!isActive || savingSite"
                /></label>
                <label
                  >{{ t("reseller.console.logoUrl")
                  }}<input
                    v-model="siteForm.logo_url"
                    maxlength="500"
                    autocomplete="url"
                    placeholder="https://cdn.example.com/logo.svg"
                    :disabled="!isActive || savingSite"
                /></label>
              </div>
              <div class="form-section two-columns">
                <div>
                  <span>{{ t("kicker.appearance") }}</span>
                  <h3>{{ t("reseller.console.appearance") }}</h3>
                  <label
                    >{{ t("reseller.console.theme")
                    }}<select
                      v-model="siteForm.theme.mode"
                      :disabled="!isActive || savingSite"
                    >
                      <option value="system">
                        {{ t("reseller.console.themeSystem") }}
                      </option>
                      <option value="light">
                        {{ t("reseller.console.themeLight") }}
                      </option>
                      <option value="dark">
                        {{ t("reseller.console.themeDark") }}
                      </option>
                    </select></label
                  >
                  <label
                    >{{ t("reseller.console.density")
                    }}<select
                      v-model="siteForm.theme.density"
                      :disabled="!isActive || savingSite"
                    >
                      <option value="comfortable">
                        {{ t("reseller.console.densityComfortable") }}
                      </option>
                      <option value="compact">
                        {{ t("reseller.console.densityCompact") }}
                      </option>
                    </select></label
                  >
                </div>
                <div>
                  <span>{{ t("kicker.support") }}</span>
                  <h3>{{ t("reseller.console.supportEntry") }}</h3>
                  <label
                    >{{ t("reseller.console.supportEmail")
                    }}<input
                      v-model="siteForm.support.email"
                      type="email"
                      maxlength="190"
                      autocomplete="email"
                      placeholder="support@example.com"
                      :disabled="!isActive || savingSite"
                  /></label>
                  <label
                    >{{ t("reseller.console.supportUrl")
                    }}<input
                      v-model="siteForm.support.url"
                      maxlength="500"
                      autocomplete="url"
                      placeholder="https://support.example.com"
                      :disabled="!isActive || savingSite"
                  /></label>
                </div>
              </div>
              <div class="form-section">
                <span>{{ t("kicker.searchEngine") }}</span>
                <h3>{{ t("reseller.console.seoInfo") }}</h3>
                <label
                  >{{ t("reseller.console.seoTitle")
                  }}<input
                    v-model="siteForm.seo.title"
                    maxlength="160"
                    :disabled="!isActive || savingSite"
                /></label>
                <label
                  >{{ t("reseller.console.seoDescription")
                  }}<textarea
                    v-model="siteForm.seo.description"
                    maxlength="500"
                    rows="4"
                    :disabled="!isActive || savingSite"
                  />
                </label>
                <small>{{
                  t("reseller.console.seoCount", {
                    n: Array.from(siteForm.seo.description).length,
                  })
                }}</small>
              </div>
              <button
                class="button primary"
                :disabled="!isActive || savingSite"
              >
                <Save />{{
                  savingSite
                    ? t("reseller.console.savingSite")
                    : t("reseller.console.saveSite")
                }}
              </button>
            </form>

            <aside class="console-panel site-preview">
              <span>{{ t("kicker.configPreview") }}</span>
              <div
                :class="[
                  'preview-window',
                  `preview-${siteForm.theme.mode}`,
                  `density-${siteForm.theme.density}`,
                ]"
              >
                <header>
                  <i>LQ</i><b>{{ siteForm.site_name || profile!.name }}</b>
                </header>
                <main>
                  <small>{{ t("reseller.console.configPreview") }}</small>
                  <h3>
                    {{
                      siteForm.seo.title || siteForm.site_name || profile!.name
                    }}
                  </h3>
                  <p>
                    {{
                      siteForm.seo.description ||
                      t("reseller.console.seoNotSet")
                    }}
                  </p>
                </main>
              </div>
              <dl>
                <div>
                  <dt>{{ t("reseller.console.theme") }}</dt>
                  <dd>{{ siteForm.theme.mode }}</dd>
                </div>
                <div>
                  <dt>{{ t("reseller.console.density") }}</dt>
                  <dd>{{ siteForm.theme.density }}</dd>
                </div>
                <div>
                  <dt>Logo</dt>
                  <dd>
                    {{ siteForm.logo_url || t("reseller.console.defaultLogo") }}
                  </dd>
                </div>
                <div>
                  <dt>{{ t("reseller.console.supportEntry") }}</dt>
                  <dd>
                    {{
                      siteForm.support.email ||
                      siteForm.support.url ||
                      t("reseller.console.notConfigured")
                    }}
                  </dd>
                </div>
                <div>
                  <dt>{{ t("reseller.console.lastSaved") }}</dt>
                  <dd>{{ date(data.site?.updated_at) }}</dd>
                </div>
              </dl>
            </aside>
          </div>
        </template>
      </template>
    </main>
  </section>
</template>

<style scoped>
.reseller-console {
  min-height: calc(100vh - 100px);
}
.reseller-console > main {
  min-width: 0;
}
.console-header {
  gap: 18px;
}
.profile-state,
.domain-states span,
.compact-orders i,
.order-ledger td span,
.domain-health-list article > span,
.domain-health-list article > em {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  width: fit-content;
  border: 1px solid var(--line);
  border-radius: 99px;
  padding: 5px 8px;
  color: var(--muted);
  font-size: 8px;
  font-style: normal;
  white-space: nowrap;
}
.profile-state svg {
  width: 14px;
}
.state-active,
.state-verified,
.state-paid,
.state-delivered,
.state-completed {
  color: var(--success) !important;
  border-color: color-mix(in srgb, var(--success) 35%, var(--line)) !important;
}
.state-pending,
.state-pending_verification,
.state-provisioning,
.state-processing,
.state-unpaid {
  color: #9a6300 !important;
  border-color: color-mix(in srgb, #c78300 42%, var(--line)) !important;
}
.state-suspended,
.state-rejected,
.state-failed,
.state-disabled,
.state-cancelled,
.state-expired,
.state-refunded,
.state-partially_refunded {
  color: #b33a34 !important;
  border-color: color-mix(in srgb, #c4433c 38%, var(--line)) !important;
}
:global(:root[data-theme="dark"]) .state-pending,
:global(:root[data-theme="dark"]) .state-pending_verification,
:global(:root[data-theme="dark"]) .state-provisioning,
:global(:root[data-theme="dark"]) .state-processing,
:global(:root[data-theme="dark"]) .state-unpaid {
  color: #efbd61 !important;
}
:global(:root[data-theme="dark"]) .state-suspended,
:global(:root[data-theme="dark"]) .state-rejected,
:global(:root[data-theme="dark"]) .state-failed,
:global(:root[data-theme="dark"]) .state-disabled,
:global(:root[data-theme="dark"]) .state-cancelled,
:global(:root[data-theme="dark"]) .state-expired,
:global(:root[data-theme="dark"]) .state-refunded,
:global(:root[data-theme="dark"]) .state-partially_refunded {
  color: #f08a84 !important;
}
.console-mobile-nav {
  display: none;
}
.console-feedback {
  margin: 0 0 12px;
  padding: 11px 13px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface);
  font-size: 10px;
  line-height: 1.6;
}
.console-feedback.error {
  color: #b33a34;
  border-color: color-mix(in srgb, #c4433c 40%, var(--line));
}
.console-feedback.notice {
  color: var(--success);
  border-color: color-mix(in srgb, var(--success) 40%, var(--line));
}
.console-loading,
.console-unavailable {
  min-height: 360px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
  color: var(--muted);
  font-size: 10px;
  text-align: center;
}
.console-loading svg {
  width: 22px;
  animation: console-spin 1s linear infinite;
}
.console-loading.inline {
  min-height: 180px;
  flex-direction: row;
}
.console-unavailable > svg {
  width: 38px;
  height: 38px;
  color: var(--text);
}
.console-unavailable h2 {
  margin: 5px 0 0;
  color: var(--text);
  font-size: 22px;
}
.console-unavailable p {
  margin: 0;
  max-width: 520px;
  line-height: 1.7;
}
.console-unavailable > div {
  display: flex;
  gap: 8px;
}
@keyframes console-spin {
  to {
    transform: rotate(360deg);
  }
}
.readonly-banner {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
}
.readonly-banner > svg {
  width: 20px;
}
.readonly-banner div {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.readonly-banner b,
.readonly-banner a {
  font-size: 9px;
}
.readonly-banner span {
  color: var(--muted);
  font-size: 9px;
  line-height: 1.55;
}
.readonly-banner a {
  text-decoration: underline;
  text-underline-offset: 3px;
}
.credit-breach-banner {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
  padding: 14px;
  border: 1px solid color-mix(in srgb, #c4433c 42%, var(--line));
  border-radius: 8px;
  background: color-mix(in srgb, #c4433c 7%, var(--surface));
  color: #b33a34;
}
.credit-breach-banner > svg {
  width: 20px;
}
.credit-breach-banner div {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.credit-breach-banner b,
.credit-breach-banner a {
  font-size: 9px;
}
.credit-breach-banner span {
  color: color-mix(in srgb, #b33a34 82%, var(--muted));
  font-size: 9px;
  line-height: 1.55;
}
.credit-breach-banner a {
  color: inherit;
  text-decoration: underline;
  text-underline-offset: 3px;
}
.danger-text {
  color: #b33a34 !important;
  font-weight: 700;
}
.section-heading {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
  margin: 4px 0 14px;
}
.section-heading > div > span,
.console-panel header > div > span,
.site-preview > span,
.form-section > span,
.form-section > div > span {
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
  letter-spacing: 0.14em;
}
.section-heading h2,
.console-panel header h2 {
  margin: 5px 0 0;
  font-size: 17px;
  letter-spacing: -0.02em;
}
.section-heading p,
.console-panel header p {
  margin: 6px 0 0;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.6;
}
.section-heading > button,
.console-panel header a,
.console-panel header > button,
.console-pagination button,
.dns-guide button,
.domain-management-card footer button,
.rule-save,
.catalog-search button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  min-height: 33px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--text);
  padding: 0 10px;
  font-size: 8px;
  cursor: pointer;
}
.section-heading button svg,
.console-panel header a svg,
.console-pagination button svg,
.dns-guide button svg,
.domain-management-card footer button svg,
.rule-save svg {
  width: 13px;
}
button:disabled,
input:disabled,
select:disabled,
textarea:disabled {
  cursor: not-allowed !important;
  opacity: 0.52;
}
.console-panel {
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
  padding: 20px;
}
.console-panel > header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 14px;
}
.dashboard-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) minmax(280px, 0.75fr);
  gap: 10px;
  margin-top: 10px;
}
.compact-orders article {
  display: grid;
  grid-template-columns: minmax(150px, 1fr) auto auto auto;
  align-items: center;
  gap: 12px;
  padding: 13px 0;
  border-bottom: 1px solid var(--line);
  font-size: 9px;
}
.compact-orders article:last-child {
  border-bottom: 0;
}
.compact-orders div {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.compact-orders b {
  overflow: hidden;
  text-overflow: ellipsis;
}
.compact-orders small,
.compact-orders em {
  color: var(--muted);
  font-size: 8px;
  font-style: normal;
}
.compact-orders em {
  color: var(--success);
}
.account-summary > header > svg,
.wallet-metadata > header > svg {
  width: 22px;
}
.account-summary dl,
.site-preview dl,
.wallet-metadata dl {
  margin: 12px 0 0;
}
.account-summary dl div,
.site-preview dl div,
.wallet-metadata dl div {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px solid var(--line);
  font-size: 8px;
}
.account-summary dt,
.site-preview dt,
.wallet-metadata dt {
  color: var(--muted);
}
.account-summary dd,
.site-preview dd,
.wallet-metadata dd {
  margin: 0;
  max-width: 65%;
  overflow-wrap: anywhere;
  text-align: right;
}
.dashboard-domains {
  margin-top: 10px;
}
.settlement-policy-card {
  margin-bottom: 10px;
  display: grid;
  grid-template-columns: auto minmax(240px, 1fr) minmax(320px, 0.9fr);
  align-items: center;
  gap: 14px;
}
.settlement-policy-card > svg {
  width: 25px;
}
.settlement-policy-card > div > span {
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
  letter-spacing: 0.13em;
}
.settlement-policy-card h3 {
  margin: 5px 0 0;
  font-size: 13px;
}
.settlement-policy-card p {
  margin: 6px 0 0;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.6;
}
.settlement-policy-card dl {
  margin: 0;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 7px;
}
.settlement-policy-card dl div {
  padding: 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface-2);
}
.settlement-policy-card dt {
  color: var(--muted);
  font-size: 7px;
}
.settlement-policy-card dd {
  margin: 6px 0 0;
  font-size: 9px;
  font-weight: 700;
}
.settlement-policy-card.unavailable {
  border-color: color-mix(in srgb, var(--warn) 42%, var(--line));
}
.domain-health-list article {
  display: grid;
  grid-template-columns: auto minmax(150px, 1fr) auto auto;
  align-items: center;
  gap: 10px;
  padding: 13px 0;
  border-bottom: 1px solid var(--line);
}
.domain-health-list article:last-child {
  border-bottom: 0;
}
.domain-health-list svg {
  width: 16px;
}
.domain-health-list div {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.domain-health-list b {
  font-size: 9px;
}
.domain-health-list small {
  color: var(--muted);
  font-size: 8px;
}
.true-empty {
  margin: 15px 0 0;
  color: var(--muted);
  font-size: 9px;
  text-align: center;
}
.true-empty.large {
  margin: 0;
  padding: 55px 20px;
}
.product-heading {
  align-items: center;
}
.catalog-search {
  display: grid;
  grid-template-columns: auto minmax(190px, 300px) auto;
  align-items: center;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface);
  padding-left: 10px;
}
.catalog-search svg {
  width: 14px;
  color: var(--muted);
}
.catalog-search input {
  height: 36px;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--text);
  padding: 0 8px;
  font-size: 9px;
}
.catalog-search button {
  border-width: 0 0 0 1px;
  border-radius: 0 6px 6px 0;
}
.restricted-panel {
  display: flex;
  align-items: center;
  gap: 16px;
}
.restricted-panel > svg {
  width: 30px;
  height: 30px;
}
.restricted-panel h2 {
  margin: 0 0 6px;
  font-size: 15px;
}
.restricted-panel p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.6;
}
.product-rule-list {
  display: grid;
  gap: 10px;
}
.product-rule-card {
  padding: 0;
  overflow: hidden;
}
.product-rule-card > header {
  padding: 18px 20px;
}
.product-rule-card > header p {
  max-width: 660px;
}
.product-stock {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 3px;
}
.product-stock b {
  font-size: 18px;
}
.product-stock small {
  color: var(--muted);
  font-size: 7px;
}
.rule-table-head,
.rule-row {
  display: grid;
  grid-template-columns:
    minmax(230px, 1.4fr) 90px 125px minmax(150px, 0.8fr)
    76px;
  gap: 10px;
  align-items: center;
  padding: 10px 20px;
  border-top: 1px solid var(--line);
}
.rule-table-head {
  background: var(--soft);
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
  letter-spacing: 0.06em;
}
.rule-target {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.rule-target b {
  font-size: 9px;
}
.rule-target small {
  color: var(--muted);
  font-size: 7px;
  line-height: 1.5;
}
.rule-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 8px;
}
.rule-toggle input {
  accent-color: var(--text);
}
.rule-row select,
.rule-value input,
.site-form input,
.site-form select,
.site-form textarea,
.domain-create-panel input {
  width: 100%;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--bg);
  color: var(--text);
  font-size: 9px;
  outline: none;
}
.rule-row select,
.rule-value input,
.site-form input,
.site-form select,
.domain-create-panel input {
  height: 37px;
  padding: 0 9px;
}
.rule-value {
  display: grid;
  grid-template-columns: 1fr auto;
  align-items: center;
  gap: 5px;
}
.rule-value > span {
  color: var(--muted);
  font-size: 8px;
}
.rule-value small {
  grid-column: 1 / -1;
  color: var(--muted);
  font-size: 7px;
}
.rule-save {
  width: 100%;
}
.console-pagination {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 12px;
  margin-top: 14px;
}
.console-pagination span {
  color: var(--muted);
  font-size: 8px;
}
.order-ledger {
  padding: 0;
  overflow: hidden;
}
.order-table-wrap {
  overflow-x: auto;
}
.order-ledger table {
  width: 100%;
  min-width: 760px;
  border-collapse: collapse;
}
.order-ledger th,
.order-ledger td {
  padding: 13px 14px;
  border-bottom: 1px solid var(--line);
  text-align: left;
  font-size: 8px;
  white-space: nowrap;
}
.order-ledger th {
  background: var(--soft);
  color: var(--muted);
  font-size: 7px;
  letter-spacing: 0.05em;
}
.order-ledger tbody tr:last-child td {
  border-bottom: 0;
}
.order-ledger .margin {
  color: var(--success);
  font-weight: 700;
}
.domain-create-panel {
  margin-bottom: 10px;
}
.domain-create-panel form {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) auto;
  align-items: end;
  gap: 10px;
}
.domain-create-panel label,
.site-form label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 8px;
}
.domain-create-panel > small {
  display: block;
  margin-top: 9px;
  color: var(--muted);
  font-size: 8px;
}
.domain-management-list {
  display: grid;
  gap: 10px;
}
.domain-management-card header {
  align-items: flex-start;
}
.domain-title {
  display: flex;
  gap: 10px;
  align-items: center;
}
.domain-title > svg {
  width: 21px;
}
.domain-title h2 {
  margin: 0;
  font-size: 14px;
}
.domain-title small {
  color: var(--muted);
  font-size: 8px;
}
.domain-states {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 6px;
}
.dns-guide {
  display: grid;
  gap: 7px;
  margin-top: 17px;
}
.dns-guide > div {
  display: grid;
  grid-template-columns: 80px minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--soft);
}
.dns-guide span {
  color: var(--muted);
  font-size: 8px;
}
.dns-guide code {
  overflow-wrap: anywhere;
  font-size: 8px;
}
.dns-guide > div:first-child {
  grid-template-columns: 80px 1fr;
}
.dns-guide button {
  min-height: 28px;
  padding: 0 8px;
  background: var(--surface);
}
.dns-note {
  margin: 12px 0;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.6;
}
.domain-management-card footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--line);
}
.domain-management-card footer > span {
  color: var(--muted);
  font-size: 8px;
}
.domain-management-card footer > div {
  display: flex;
  gap: 6px;
}
.domain-management-card footer button.danger {
  color: #b33a34;
}
.wallet-summary-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
  gap: 10px;
}
.wallet-summary-grid article {
  display: flex;
  flex-direction: column;
  gap: 7px;
}
.wallet-summary-grid article > svg {
  width: 19px;
  margin-bottom: 3px;
}
.wallet-summary-grid span,
.wallet-summary-grid small {
  color: var(--muted);
  font-size: 8px;
}
.wallet-summary-grid strong {
  font-size: 21px;
}
.wallet-balance-card {
  background: var(--inverse);
  color: var(--inverse-text);
}
.wallet-balance-card span,
.wallet-balance-card small {
  color: color-mix(in srgb, var(--inverse-text) 60%, transparent);
}
.credit-risk-card.breached {
  border-color: color-mix(in srgb, #c4433c 45%, var(--line));
  background: color-mix(in srgb, #c4433c 6%, var(--surface));
}
.credit-risk-card.breached strong,
.credit-risk-card.breached small {
  color: #b33a34;
}
.wallet-metadata {
  margin-top: 10px;
}
.wallet-metadata dl {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 28px;
}
.wallet-metadata > p {
  margin: 14px 0 0;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.7;
}
.wallet-metadata > p.credit-policy-note {
  padding: 9px 10px;
  border-left: 2px solid var(--text);
  display: flex;
  align-items: flex-start;
  gap: 7px;
  background: var(--surface-2);
}
.credit-policy-note svg {
  width: 14px;
  flex: 0 0 auto;
}
.site-settings-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.35fr) minmax(300px, 0.65fr);
  gap: 10px;
  align-items: start;
}
.site-form {
  display: grid;
  gap: 20px;
}
.form-section {
  display: grid;
  gap: 10px;
  padding-bottom: 20px;
  border-bottom: 1px solid var(--line);
}
.form-section h3 {
  margin: -5px 0 2px;
  font-size: 13px;
}
.form-section.two-columns {
  grid-template-columns: 1fr 1fr;
  gap: 18px;
}
.form-section.two-columns > div {
  display: grid;
  gap: 10px;
  align-content: start;
}
.site-form textarea {
  padding: 9px;
  resize: vertical;
  line-height: 1.6;
}
.site-form label + small {
  margin-top: -5px;
  color: var(--muted);
  font-size: 7px;
  text-align: right;
}
.site-form > button {
  width: fit-content;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.site-form > button svg {
  width: 14px;
}
.site-preview {
  position: sticky;
  top: 116px;
}
.preview-window {
  margin: 12px 0 16px;
  overflow: hidden;
  border: 1px solid #d9d9d5;
  border-radius: 7px;
  background: #fff;
  color: #111214;
}
.preview-window.preview-dark {
  border-color: #303235;
  background: #111214;
  color: #f5f5f3;
}
.preview-window.preview-system {
  background: linear-gradient(135deg, #fff 0 49.8%, #111214 50.2% 100%);
}
.preview-window.preview-system main {
  text-shadow: 0 0 14px var(--surface);
}
.preview-window header {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 10px;
  border-bottom: 1px solid currentColor;
  border-color: color-mix(in srgb, currentColor 18%, transparent);
}
.preview-window header i {
  width: 22px;
  height: 22px;
  display: grid;
  place-items: center;
  border-radius: 4px;
  background: currentColor;
  color: #fff;
  font-size: 7px;
  font-style: normal;
}
.preview-window.preview-dark header i {
  color: #111214;
}
.preview-window header b {
  font-size: 8px;
}
.preview-window main {
  padding: 28px 18px;
}
.preview-window.density-compact main {
  padding: 18px 14px;
}
.preview-window main small {
  font-size: 6px;
  letter-spacing: 0.1em;
}
.preview-window main h3 {
  max-width: 230px;
  margin: 8px 0;
  font-size: 18px;
  line-height: 1.1;
}
.preview-window main p {
  min-height: 28px;
  margin: 0 0 12px;
  color: currentColor;
  opacity: 0.65;
  font-size: 7px;
  line-height: 1.5;
}
@media (max-width: 1100px) {
  .rule-table-head {
    display: none;
  }
  .rule-row {
    grid-template-columns:
      minmax(190px, 1fr) 80px 120px minmax(135px, 0.75fr)
      70px;
  }
  .site-settings-grid {
    grid-template-columns: 1fr;
  }
  .site-preview {
    position: static;
  }
}
@media (max-width: 900px) {
  .console-mobile-nav {
    display: flex;
    gap: 6px;
    overflow-x: auto;
    margin: -8px 0 18px;
    padding-bottom: 4px;
    scrollbar-width: thin;
  }
  .console-mobile-nav a {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    flex: 0 0 auto;
    min-height: 40px;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--surface);
    padding: 0 9px;
    color: var(--muted);
    font-size: 8px;
  }
  .console-mobile-nav a.router-link-active {
    background: var(--inverse);
    color: var(--inverse-text);
  }
  .console-mobile-nav svg {
    width: 13px;
  }
  .dashboard-grid {
    grid-template-columns: 1fr;
  }
  .rule-row {
    grid-template-columns: minmax(180px, 1.2fr) 80px 120px minmax(135px, 0.8fr);
  }
  .rule-save {
    grid-column: 4;
  }
  .wallet-summary-grid {
    grid-template-columns: 1fr 1fr;
  }
  .settlement-policy-card {
    grid-template-columns: auto 1fr;
  }
  .settlement-policy-card dl {
    grid-column: 1 / -1;
  }
}
@media (max-width: 680px) {
  .console-header {
    align-items: flex-start !important;
  }
  .readonly-banner {
    grid-template-columns: auto 1fr;
  }
  .readonly-banner a {
    grid-column: 2;
  }
  .credit-breach-banner {
    grid-template-columns: auto 1fr;
  }
  .credit-breach-banner a {
    grid-column: 2;
  }
  .section-heading {
    align-items: flex-start;
    flex-direction: column;
  }
  .section-heading > button {
    width: 100%;
  }
  .catalog-search {
    width: 100%;
    grid-template-columns: auto minmax(0, 1fr) auto;
  }
  .compact-orders article {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .compact-orders em {
    grid-column: 1;
  }
  .compact-orders i {
    grid-column: 2;
    grid-row: 2;
  }
  .domain-health-list article {
    grid-template-columns: auto 1fr;
  }
  .domain-health-list article > span {
    grid-column: 1 / 2;
  }
  .domain-health-list article > em {
    grid-column: 2;
  }
  .product-rule-card > header {
    align-items: flex-start;
  }
  .rule-row {
    grid-template-columns: 1fr 1fr;
    padding: 14px;
  }
  .order-table-wrap {
    overflow: visible;
  }
  .order-ledger table,
  .order-ledger tbody {
    min-width: 0;
    display: block;
    width: 100%;
  }
  .order-ledger thead {
    display: none;
  }
  .order-ledger tr {
    display: grid;
    padding: 8px 14px;
    border-bottom: 1px solid var(--line);
  }
  .order-ledger tbody tr:last-child {
    border-bottom: 0;
  }
  .order-ledger td {
    min-width: 0;
    display: grid;
    grid-template-columns: minmax(105px, 0.45fr) minmax(0, 1fr);
    align-items: center;
    gap: 10px;
    padding: 9px 0;
    border-bottom: 1px solid var(--line);
    text-align: right;
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .order-ledger td::before {
    content: attr(data-label);
    color: var(--muted);
    font-size: 8px;
    text-align: left;
  }
  .order-ledger td:last-child {
    border-bottom: 0;
  }
  .order-ledger td > * {
    justify-self: end;
  }
  .rule-target {
    grid-column: 1 / -1;
  }
  .rule-value {
    grid-column: 1 / -1;
  }
  .rule-save {
    grid-column: 2;
  }
  .domain-create-panel form {
    grid-template-columns: 1fr;
  }
  .domain-create-panel .button {
    width: 100%;
  }
  .domain-management-card header,
  .domain-management-card footer {
    align-items: flex-start;
    flex-direction: column;
  }
  .domain-states {
    justify-content: flex-start;
  }
  .dns-guide > div,
  .dns-guide > div:first-child {
    grid-template-columns: 1fr auto;
  }
  .dns-guide code {
    grid-column: 1 / -1;
  }
  .dns-guide button {
    grid-column: 2;
    grid-row: 1;
  }
  .wallet-summary-grid {
    grid-template-columns: 1fr;
  }
  .settlement-policy-card {
    align-items: start;
    grid-template-columns: auto 1fr;
  }
  .settlement-policy-card dl {
    grid-template-columns: 1fr;
  }
  .wallet-metadata dl {
    grid-template-columns: 1fr;
  }
  .form-section.two-columns {
    grid-template-columns: 1fr;
  }
  .console-pagination {
    justify-content: space-between;
    gap: 6px;
  }
  .console-pagination span {
    text-align: center;
  }
  .section-heading > button,
  .console-panel header a,
  .console-panel header > button,
  .console-pagination button,
  .dns-guide button,
  .domain-management-card footer button,
  .rule-save,
  .catalog-search button,
  .rule-row select,
  .rule-value input,
  .site-form input,
  .site-form select,
  .domain-create-panel input {
    min-height: 42px;
  }
}
</style>
