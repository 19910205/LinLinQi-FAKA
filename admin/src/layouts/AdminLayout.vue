<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
  type Component,
} from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  Activity,
  BadgePercent,
  Bell,
  BookOpen,
  Boxes,
  BriefcaseBusiness,
  ChartNoAxesCombined,
  ChevronDown,
  CircleDollarSign,
  ClipboardList,
  CodeXml,
  CreditCard,
  FileKey,
  Gift,
  GitCompareArrows,
  LayoutDashboard,
  Image,
  Layers3,
  ListRestart,
  LogOut,
  Menu,
  Moon,
  Network,
  PackageOpen,
  Search,
  Settings,
  ShieldAlert,
  Store,
  Sun,
  TicketCheck,
  TicketPercent,
  UsersRound,
  WalletCards,
  Webhook,
  X,
  Zap,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import LocaleSwitcher from "../components/LocaleSwitcher.vue";
import { loadCurrencyDirectory } from "../utils/money";

const { t } = useI18n();
const dark = ref(false);
const sidebarOpen = ref(false);
const route = useRoute();
const router = useRouter();
const auth = useAuthStore();
const operations = ref<Record<string, number>>({});
const integrationAlerts = ref(0);
const healthStatus = ref<"checking" | "healthy" | "degraded">("checking");
const healthCheckedAt = ref<Date | null>(null);
const commandOpen = ref(false);
const commandQuery = ref("");
const commandInput = ref<HTMLInputElement | null>(null);
let runtimeTimer: number | undefined;
const pageKey = computed(() => {
  const path = route.path.replace(/^\/+/, "").replace(/\/+$/, "");
  return path === "" ? "dashboard" : path;
});
const title = computed(() => {
  const key = `page.${pageKey.value}.title`;
  const translated = t(key);
  return translated === key
    ? String(route.meta.title || t("layout.breadcrumbRoot"))
    : translated;
});
const subtitle = computed(() => {
  const key = `page.${pageKey.value}.subtitle`;
  const translated = t(key);
  return translated === key ? String(route.meta.subtitle || "") : translated;
});

interface NavLink {
  id: string;
  to?: string;
  label: string;
  icon: Component;
  badge?: string;
  children?: NavLink[];
}

interface NavGroup {
  label: string;
  links: NavLink[];
}

const navPermissions: Record<string, string> = {
  "/": "dashboard.read",
  "/analytics": "dashboard.read",
  "/orders": "order.view",
  "/tickets": "order.view",
  "/products": "catalog.view",
  "/product-categories": "catalog.view",
  "/variants": "catalog.view",
  "/pricing": "catalog.view",
  "/member-levels": "catalog.view",
  "/inventory": "inventory.view",
  "/card-secrets": "inventory.view",
  "/inventory-batches": "inventory.view",
  "/suppliers": "supplier.view",
  "/category-bindings": "supplier.view",
  "/mappings": "supplier.view",
  "/procurements": "supplier.view",
  "/customers": "customer.view",
  "/wallets": "wallet.view",
  "/payment-channels": "payment.view",
  "/wallet-recharges": "payment.view",
  "/payment-intents": "payment.view",
  "/payment-transactions": "payment.view",
  "/refunds": "payment.view",
  "/reconciliation": "payment.view",
  "/promotions": "marketing.view",
  "/coupons": "marketing.view",
  "/gift-card-batches": "marketing.view",
  "/gift-card-list": "marketing.view",
  "/affiliates": "marketing.view",
  "/affiliate-commissions": "marketing.view",
  "/affiliate-withdrawals": "marketing.view",
  "/posts": "marketing.view",
  "/post-categories": "marketing.view",
  "/announcements": "marketing.view",
  "/banners": "marketing.view",
  "/media": "marketing.view",
  "/resellers": "reseller.view",
  "/reseller-tiers": "reseller.view",
  "/reseller-domains": "reseller.view",
  "/reseller-withdrawals": "reseller.view",
  "/risk-rules": "security.view",
  "/risk-decisions": "security.view",
  "/openapi": "system.view",
  "/webhook-endpoints": "system.view",
  "/webhook-deliveries": "system.view",
  "/notification-templates": "system.view",
  "/notification-deliveries": "system.view",
  "/notification-rules": "system.view",
  "/notification-connectors": "system.view",
  "/notification-events": "system.view",
  "/admins": "system.view",
  "/roles": "system.view",
  "/access-audit": "system.view",
  "/jobs": "system.view",
  "/currencies": "system.view",
  "/fx-providers": "system.view",
  "/manual-rates": "system.view",
  "/fx-snapshots": "system.view",
  "/settings": "system.view",
};

const allGroups = computed<NavGroup[]>(() => [
  {
    label: t("layout.navGroups.ops"),
    links: [
      {
        id: "dashboard",
        to: "/",
        label: t("layout.nav.dashboard"),
        icon: LayoutDashboard,
      },
      {
        id: "analytics",
        to: "/analytics",
        label: t("layout.nav.analytics"),
        icon: ChartNoAxesCombined,
      },
      {
        id: "orders",
        to: "/orders",
        label: t("layout.nav.orders"),
        icon: ClipboardList,
        badge: operations.value.orders
          ? String(operations.value.orders)
          : undefined,
      },
      {
        id: "tickets",
        to: "/tickets",
        label: t("layout.nav.tickets"),
        icon: TicketCheck,
      },
    ],
  },
  {
    label: t("layout.navGroups.catalog"),
    links: [
      {
        id: "catalog",
        label: t("layout.nav.catalogModule"),
        icon: Store,
        children: [
          {
            id: "product-categories",
            to: "/product-categories",
            label: t("layout.nav.productCategories"),
            icon: Layers3,
          },
          {
            id: "products",
            to: "/products",
            label: t("layout.nav.products"),
            icon: Store,
          },
          {
            id: "variants",
            to: "/variants",
            label: t("layout.nav.variants"),
            icon: Layers3,
          },
          {
            id: "card-secrets",
            to: "/card-secrets",
            label: t("layout.nav.cardSecrets"),
            icon: FileKey,
          },
          {
            id: "inventory-batches",
            to: "/inventory-batches",
            label: t("layout.nav.inventoryBatches"),
            icon: Boxes,
          },
          {
            id: "inventory",
            to: "/inventory",
            label: t("layout.nav.inventoryOverview"),
            icon: Boxes,
          },
          {
            id: "pricing",
            to: "/pricing",
            label: t("layout.nav.pricing"),
            icon: BadgePercent,
          },
          {
            id: "member-levels",
            to: "/member-levels",
            label: t("layout.nav.memberLevels"),
            icon: BadgePercent,
          },
        ],
      },
    ],
  },
  {
    label: t("layout.navGroups.growth"),
    links: [
      {
        id: "customers",
        label: t("layout.nav.customerModule"),
        icon: UsersRound,
        children: [
          {
            id: "customer-profiles",
            to: "/customers",
            label: t("layout.nav.customers"),
            icon: UsersRound,
          },
          {
            id: "wallets",
            to: "/wallets",
            label: t("layout.nav.wallets"),
            icon: WalletCards,
          },
        ],
      },
      {
        id: "marketing",
        label: t("layout.nav.marketingModule"),
        icon: TicketPercent,
        children: [
          {
            id: "promotions",
            to: "/promotions",
            label: t("layout.nav.promotions"),
            icon: TicketPercent,
          },
          {
            id: "coupons",
            to: "/coupons",
            label: t("layout.nav.coupons"),
            icon: BadgePercent,
          },
        ],
      },
      {
        id: "gift-cards",
        label: t("layout.nav.giftCardModule"),
        icon: Gift,
        children: [
          {
            id: "gift-card-batches",
            to: "/gift-card-batches",
            label: t("layout.nav.giftCardBatches"),
            icon: Boxes,
          },
          {
            id: "gift-card-list",
            to: "/gift-card-list",
            label: t("layout.nav.giftCards"),
            icon: Gift,
          },
        ],
      },
      {
        id: "affiliates",
        label: t("layout.nav.affiliateModule"),
        icon: Network,
        children: [
          {
            id: "affiliate-accounts",
            to: "/affiliates",
            label: t("layout.nav.affiliateAccounts"),
            icon: Network,
          },
          {
            id: "affiliate-commissions",
            to: "/affiliate-commissions",
            label: t("layout.nav.affiliateCommissions"),
            icon: BadgePercent,
          },
          {
            id: "affiliate-withdrawals",
            to: "/affiliate-withdrawals",
            label: t("layout.nav.affiliateWithdrawals"),
            icon: WalletCards,
          },
        ],
      },
      {
        id: "resellers",
        label: t("layout.nav.resellerModule"),
        icon: Store,
        children: [
          {
            id: "reseller-profiles",
            to: "/resellers",
            label: t("layout.nav.resellerProfiles"),
            icon: Store,
          },
          {
            id: "reseller-tiers",
            to: "/reseller-tiers",
            label: t("layout.nav.resellerTiers"),
            icon: BadgePercent,
          },
          {
            id: "reseller-domains",
            to: "/reseller-domains",
            label: t("layout.nav.resellerDomains"),
            icon: Webhook,
          },
          {
            id: "reseller-withdrawals",
            to: "/reseller-withdrawals",
            label: t("layout.nav.resellerWithdrawals"),
            icon: WalletCards,
          },
        ],
      },
    ],
  },
  {
    label: t("layout.navGroups.finance"),
    links: [
      {
        id: "payments",
        label: t("layout.nav.paymentModule"),
        icon: CreditCard,
        children: [
          {
            id: "payment-channels",
            to: "/payment-channels",
            label: t("layout.nav.paymentChannels"),
            icon: CreditCard,
          },
          {
            id: "wallet-recharges",
            to: "/wallet-recharges",
            label: t("layout.nav.walletRecharges"),
            icon: WalletCards,
          },
          {
            id: "payment-intents",
            to: "/payment-intents",
            label: t("layout.nav.paymentIntents"),
            icon: ClipboardList,
          },
          {
            id: "payment-transactions",
            to: "/payment-transactions",
            label: t("layout.nav.paymentTransactions"),
            icon: GitCompareArrows,
          },
          {
            id: "refunds",
            to: "/refunds",
            label: t("layout.nav.refunds"),
            icon: TicketCheck,
          },
          {
            id: "reconciliation",
            to: "/reconciliation",
            label: t("layout.nav.reconciliation"),
            icon: GitCompareArrows,
          },
        ],
      },
    ],
  },
  {
    label: t("layout.navGroups.content"),
    links: [
      {
        id: "content",
        label: t("layout.nav.contentModule"),
        icon: BookOpen,
        children: [
          {
            id: "posts",
            to: "/posts",
            label: t("layout.nav.posts"),
            icon: BookOpen,
          },
          {
            id: "post-categories",
            to: "/post-categories",
            label: t("layout.nav.postCategories"),
            icon: Layers3,
          },
          {
            id: "announcements",
            to: "/announcements",
            label: t("layout.nav.announcements"),
            icon: Bell,
          },
          {
            id: "banners",
            to: "/banners",
            label: t("layout.nav.banners"),
            icon: Image,
          },
          {
            id: "media",
            to: "/media",
            label: t("layout.nav.media"),
            icon: Image,
          },
        ],
      },
    ],
  },
  {
    label: t("layout.navGroups.supply"),
    links: [
      {
        id: "supply",
        label: t("layout.nav.supplyModule"),
        icon: Zap,
        children: [
          {
            id: "suppliers",
            to: "/suppliers",
            label: t("layout.nav.suppliers"),
            icon: Zap,
          },
          {
            id: "category-bindings",
            to: "/category-bindings",
            label: t("layout.nav.categoryBindings"),
            icon: Layers3,
          },
          {
            id: "mappings",
            to: "/mappings",
            label: t("layout.nav.mappings"),
            icon: GitCompareArrows,
          },
          {
            id: "procurements",
            to: "/procurements",
            label: t("layout.nav.procurements"),
            icon: BriefcaseBusiness,
          },
        ],
      },
    ],
  },
  {
    label: t("layout.navGroups.platform"),
    links: [
      {
        id: "integration",
        label: t("layout.nav.integrationModule"),
        icon: Webhook,
        badge: integrationAlerts.value
          ? String(integrationAlerts.value)
          : undefined,
        children: [
          {
            id: "webhook-endpoints",
            to: "/webhook-endpoints",
            label: t("layout.nav.webhookEndpoints"),
            icon: Webhook,
          },
          {
            id: "webhook-deliveries",
            to: "/webhook-deliveries",
            label: t("layout.nav.webhookDeliveries"),
            icon: GitCompareArrows,
          },
          {
            id: "notification-templates",
            to: "/notification-templates",
            label: t("layout.nav.notificationTemplates"),
            icon: Bell,
          },
          {
            id: "notification-deliveries",
            to: "/notification-deliveries",
            label: t("layout.nav.notificationDeliveries"),
            icon: Activity,
          },
          {
            id: "notification-rules",
            to: "/notification-rules",
            label: t("layout.nav.notificationRules"),
            icon: Bell,
          },
          {
            id: "notification-connectors",
            to: "/notification-connectors",
            label: t("layout.nav.notificationConnectors"),
            icon: Zap,
          },
          {
            id: "notification-events",
            to: "/notification-events",
            label: t("layout.nav.notificationEvents"),
            icon: ListRestart,
          },
          {
            id: "openapi",
            to: "/openapi",
            label: t("layout.nav.openapi"),
            icon: CodeXml,
          },
        ],
      },
      {
        id: "access",
        label: t("layout.nav.accessModule"),
        icon: FileKey,
        children: [
          {
            id: "admins",
            to: "/admins",
            label: t("layout.nav.admins"),
            icon: UsersRound,
          },
          {
            id: "roles",
            to: "/roles",
            label: t("layout.nav.roles"),
            icon: FileKey,
          },
          {
            id: "access-audit",
            to: "/access-audit",
            label: t("layout.nav.accessAudit"),
            icon: ClipboardList,
          },
        ],
      },
      {
        id: "risk",
        label: t("layout.nav.riskModule"),
        icon: ShieldAlert,
        children: [
          {
            id: "risk-rules",
            to: "/risk-rules",
            label: t("layout.nav.riskRules"),
            icon: ShieldAlert,
          },
          {
            id: "risk-decisions",
            to: "/risk-decisions",
            label: t("layout.nav.riskDecisions"),
            icon: ClipboardList,
          },
        ],
      },
      {
        id: "currency",
        label: t("layout.nav.currencyModule"),
        icon: CircleDollarSign,
        children: [
          {
            id: "currencies",
            to: "/currencies",
            label: t("layout.nav.currencies"),
            icon: CircleDollarSign,
          },
          {
            id: "fx-providers",
            to: "/fx-providers",
            label: t("layout.nav.fxProviders"),
            icon: Activity,
          },
          {
            id: "manual-rates",
            to: "/manual-rates",
            label: t("layout.nav.manualRates"),
            icon: CircleDollarSign,
          },
          {
            id: "fx-snapshots",
            to: "/fx-snapshots",
            label: t("layout.nav.fxSnapshots"),
            icon: ClipboardList,
          },
        ],
      },
      {
        id: "security",
        to: "/security",
        label: t("layout.nav.security"),
        icon: ShieldAlert,
      },
      {
        id: "jobs",
        to: "/jobs",
        label: t("layout.nav.jobs"),
        icon: ListRestart,
      },
      {
        id: "settings",
        to: "/settings",
        label: t("layout.nav.settings"),
        icon: Settings,
      },
    ],
  },
]);

function navLinkAllowed(link: NavLink) {
  return Boolean(link.to && auth.hasPermission(navPermissions[link.to]));
}

function visibleNavLink(link: NavLink): NavLink | null {
  const children = (link.children || [])
    .map(visibleNavLink)
    .filter((child): child is NavLink => Boolean(child));
  if (children.length) return { ...link, children };
  if (navLinkAllowed(link)) return { ...link, children: undefined };
  return null;
}

const groups = computed<NavGroup[]>(() =>
  allGroups.value
    .map((group) => ({
      ...group,
      links: group.links
        .map(visibleNavLink)
        .filter((link): link is NavLink => Boolean(link)),
    }))
    .filter((group) => group.links.length > 0),
);
const commandLinks = computed(() =>
  groups.value
    .flatMap((group) => group.links)
    .flatMap((link) => (link.children?.length ? link.children : [link]))
    .filter((link): link is NavLink & { to: string } => Boolean(link.to))
    .filter((link) =>
      `${link.label} ${link.to}`
        .toLocaleLowerCase()
        .includes(commandQuery.value.trim().toLocaleLowerCase()),
    ),
);

function linkIsActive(link: NavLink) {
  return (
    Boolean(link.to && route.path === link.to) ||
    Boolean(link.children?.some((child) => route.path === child.to))
  );
}
const expandedModules = ref<Set<string>>(new Set());

function moduleIsOpen(link: NavLink) {
  return expandedModules.value.has(link.id);
}

function toggleModule(link: NavLink) {
  const next = new Set(expandedModules.value);
  if (next.has(link.id)) next.delete(link.id);
  else next.add(link.id);
  expandedModules.value = next;
}

watch(
  [() => route.path, groups],
  () => {
    const next = new Set(expandedModules.value);
    for (const group of groups.value) {
      for (const link of group.links) {
        if (link.children?.some((child) => child.to === route.path))
          next.add(link.id);
      }
    }
    expandedModules.value = next;
  },
  { immediate: true },
);
const profileInitials = computed(() =>
  String(auth.profile.name || "LQ")
    .trim()
    .slice(0, 2)
    .toUpperCase(),
);
const healthLabel = computed(() => {
  if (healthStatus.value === "healthy") return t("layout.healthy");
  if (healthStatus.value === "degraded") return t("layout.unavailable");
  return t("layout.checking");
});
const healthTime = computed(() =>
  healthCheckedAt.value
    ? t("layout.lastCheckTime", {
        time: healthCheckedAt.value.toLocaleTimeString([], {
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        }),
      })
    : t("layout.checkingHealth"),
);

function apiRoot() {
  const configured = String(
    adminApi.defaults.baseURL || window.location.origin,
  );
  const parsed = new URL(configured, window.location.origin);
  parsed.pathname = parsed.pathname.replace(/\/admin\/v1\/?$/, "");
  parsed.search = "";
  parsed.hash = "";
  return parsed.toString().replace(/\/$/, "");
}

async function refreshRuntime() {
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), 3500);
  try {
    const result = await fetch(`${apiRoot()}/ready`, {
      signal: controller.signal,
      headers: { Accept: "application/json" },
    });
    healthStatus.value = result.ok ? "healthy" : "degraded";
  } catch {
    healthStatus.value = "degraded";
  } finally {
    window.clearTimeout(timeout);
    healthCheckedAt.value = new Date();
  }
  const [operationResult, integrationResult] = await Promise.allSettled([
    auth.hasPermission("dashboard.read")
      ? adminApi.get("/operations/summary")
      : Promise.resolve(null),
    auth.hasPermission("system.view")
      ? adminApi.get("/integrations/summary")
      : Promise.resolve(null),
  ]);
  if (operationResult.status === "fulfilled" && operationResult.value !== null)
    operations.value = operationResult.value.data.data || {};
  else operations.value = {};
  if (
    integrationResult.status === "fulfilled" &&
    integrationResult.value !== null
  ) {
    const value = integrationResult.value.data.data || {};
    integrationAlerts.value =
      Number(value.webhook_deliveries_failed || 0) +
      Number(value.notification_failed || 0);
  } else integrationAlerts.value = 0;
}

async function openCommand() {
  commandOpen.value = true;
  commandQuery.value = "";
  await nextTick();
  commandInput.value?.focus();
}

async function navigateCommand(path: string) {
  commandOpen.value = false;
  await router.push(path);
}

function handleShortcut(event: KeyboardEvent) {
  if (
    (event.metaKey || event.ctrlKey) &&
    event.key.toLocaleLowerCase() === "k"
  ) {
    event.preventDefault();
    void openCommand();
  } else if (event.key === "Escape") commandOpen.value = false;
}
function theme() {
  dark.value = !dark.value;
  document.documentElement.dataset.theme = dark.value ? "dark" : "light";
  localStorage.setItem("linlinqi-admin-theme", dark.value ? "dark" : "light");
}
function logout() {
  auth.logout();
  router.push("/login");
}
onMounted(() => {
  dark.value = localStorage.getItem("linlinqi-admin-theme") === "dark";
  document.documentElement.dataset.theme = dark.value ? "dark" : "light";
  window.addEventListener("keydown", handleShortcut);
  void loadCurrencyDirectory().catch(() => undefined);
  void refreshRuntime();
  runtimeTimer = window.setInterval(refreshRuntime, 60_000);
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", handleShortcut);
  if (runtimeTimer) window.clearInterval(runtimeTimer);
});
</script>

<template>
  <div class="admin-shell">
    <aside :class="['sidebar', { open: sidebarOpen }]">
      <div class="side-head">
        <div class="admin-brand">
          <span class="brand-mark">LQ</span>
          <div>
            <b>LinLinQi</b><small>{{ t("adminKicker.adminConsole") }}</small>
          </div>
        </div>
        <button class="side-close" @click="sidebarOpen = false"><X /></button>
      </div>
      <div class="workspace">
        <span>{{ t("layout.workspaceLabel") }}</span>
        <div class="workspace-current">
          <span class="workspace-icon"><PackageOpen :size="16" /></span>
          <div>
            <b>{{ t("layout.workspaceName") }}</b
            ><small>{{ t("layout.production") }}</small>
          </div>
        </div>
      </div>
      <nav>
        <div v-for="group in groups" :key="group.label" class="nav-group">
          <span>{{ group.label }}</span>
          <template v-for="link in group.links" :key="link.id">
            <RouterLink
              v-if="!link.children?.length && link.to"
              :to="link.to"
              :class="{ 'module-active': linkIsActive(link) }"
              @click="sidebarOpen = false"
            >
              <component :is="link.icon" :size="17" />
              <b>{{ link.label }}</b>
              <em v-if="link.badge">{{ link.badge }}</em>
            </RouterLink>
            <div v-else class="nav-module">
              <button
                type="button"
                class="nav-module-trigger"
                :class="{ 'module-active': linkIsActive(link) }"
                :aria-expanded="moduleIsOpen(link)"
                :aria-controls="`nav-module-${link.id}`"
                @click="toggleModule(link)"
              >
                <component :is="link.icon" :size="17" />
                <b>{{ link.label }}</b>
                <em v-if="link.badge">{{ link.badge }}</em>
                <ChevronDown
                  :size="14"
                  :class="['nav-module-chevron', { open: moduleIsOpen(link) }]"
                />
              </button>
              <div
                v-show="moduleIsOpen(link)"
                :id="`nav-module-${link.id}`"
                class="nav-children"
              >
                <RouterLink
                  v-for="child in link.children"
                  :key="child.id"
                  :to="child.to || '/'"
                  @click="sidebarOpen = false"
                >
                  <component :is="child.icon" :size="14" />
                  <b>{{ child.label }}</b>
                  <em v-if="child.badge">{{ child.badge }}</em>
                </RouterLink>
              </div>
            </div>
          </template>
        </div>
      </nav>
      <div :class="['system-health', `runtime-health-${healthStatus}`]">
        <div>
          <Activity :size="15" /><span>{{ t("layout.systemHealth") }}</span
          ><b>{{ healthLabel }}</b>
        </div>
        <div class="health-line"><i></i></div>
        <small>{{ healthTime }}</small>
      </div>
      <button
        class="user-card"
        :title="t('layout.openSecurity')"
        @click="router.push('/security')"
      >
        <span>{{ profileInitials }}</span>
        <div>
          <b>{{ auth.profile.name || t("layout.superAdmin") }}</b
          ><small>{{ auth.profile.role || t("layout.defaultRole") }}</small>
        </div>
        <ShieldAlert :size="15" />
      </button>
    </aside>
    <div
      v-if="sidebarOpen"
      class="side-backdrop"
      @click="sidebarOpen = false"
    ></div>
    <section class="admin-main">
      <header class="admin-header">
        <button class="mobile-menu" @click="sidebarOpen = true">
          <Menu />
        </button>
        <div class="header-actions">
          <button
            class="command-search"
            :title="t('layout.openCommand')"
            @click="openCommand"
          >
            <Search :size="16" /><span>{{ t("layout.searchPlaceholder") }}</span
            ><kbd>⌘ K</kbd>
          </button>
          <LocaleSwitcher />
          <button @click="theme">
            <Sun v-if="dark" :size="17" /><Moon v-else :size="17" /></button
          ><button
            class="notification"
            :title="t('layout.openNotifications')"
            @click="router.push('/notification-rules')"
          >
            <Bell :size="17" /><i v-if="integrationAlerts"></i></button
          ><button :title="t('layout.logoutTitle')" @click="logout">
            <LogOut :size="17" />
          </button>
        </div>
      </header>
      <main class="content">
        <div class="page-title">
          <div>
            <h1>{{ title }}</h1>
            <p>{{ subtitle }}</p>
          </div>
          <div :class="['live-pill', `runtime-health-${healthStatus}`]">
            <i></i>
            {{
              healthStatus === "healthy" ? t("layout.liveData") : healthLabel
            }}
          </div>
        </div>
        <RouterView />
      </main>
    </section>
    <div
      v-if="commandOpen"
      class="command-backdrop"
      @click.self="commandOpen = false"
    >
      <section class="command-palette">
        <header>
          <Search :size="18" /><input
            ref="commandInput"
            v-model="commandQuery"
            :placeholder="t('layout.cmdPlaceholder')"
          />
        </header>
        <div>
          <button
            v-for="link in commandLinks"
            :key="link.to"
            @click="navigateCommand(link.to)"
          >
            <component :is="link.icon" :size="17" /><span>{{ link.label }}</span
            ><small>{{ link.to }}</small>
          </button>
          <p v-if="!commandLinks.length">{{ t("layout.cmdNoMatch") }}</p>
        </div>
        <footer>
          <span>{{ t("layout.cmdEnter") }}</span
          ><span>{{ t("layout.cmdEsc") }}</span>
        </footer>
      </section>
    </div>
  </div>
</template>
