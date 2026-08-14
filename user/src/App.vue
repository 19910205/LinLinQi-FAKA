<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { RouterLink, RouterView } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  Menu,
  Moon,
  Search,
  ShoppingBag,
  Sun,
  UserRound,
  X,
} from "@lucide/vue";
import {
  CART_UPDATED_EVENT,
  fetchCart,
  fetchCurrencies,
  fetchStoreConfig,
} from "./api";
import LocaleSwitcher from "./components/LocaleSwitcher.vue";
import CurrencySwitcher from "./components/CurrencySwitcher.vue";
import { registerCurrencies, setStoreCurrency } from "./utils/money";
import { safePublicHTTPURL } from "./utils/publicUrl";

interface StorefrontConfig {
  name: string;
  tagline: string;
  logo_url?: string;
  support_email?: string;
  currency?: string;
  announcements?: Array<{ title: string }>;
  reseller?: boolean;
  theme?: { mode?: string; density?: string };
  seo?: { title?: string; description?: string };
  support?: { email?: string; url?: string };
}

const { t } = useI18n();
const dark = ref(false);
const menuOpen = ref(false);
const year = new Date().getFullYear();
const store = ref<StorefrontConfig>({
  name: "LinLinQi",
  tagline: t("app.tagline"),
  support_email: "",
});
const signedIn = ref(Boolean(localStorage.getItem("linlinqi-user-token")));
const cartCount = ref(0);
const depthPopTimers = new Map<HTMLElement, number>();
const depthPopSelector = [
  "button:not(:disabled)",
  "a[href]",
  "select:not(:disabled)",
  '[role="button"]:not([aria-disabled="true"])',
  ".product-card",
  ".ticket-card",
  ".notification-item",
  ".gift-card-record",
  ".reseller-entry",
  ".public-banner[href]",
].join(",");
const themeLabel = computed(() =>
  dark.value ? t("app.lightMode") : t("app.darkMode"),
);
const supportEmail = computed(() => {
  const value = String(
    store.value.support?.email || store.value.support_email || "",
  )
    .trim()
    .toLowerCase();
  return /^\S+@\S+\.\S+$/.test(value) && value.length <= 190 ? value : "";
});
const storeLogoURL = computed(() => safePublicHTTPURL(store.value.logo_url));
const supportURL = computed(() => {
  const value = String(store.value.support?.url || "").trim();
  if (!value) return "";
  try {
    const parsed = new URL(value);
    if (
      parsed.protocol !== "https:" ||
      !parsed.hostname ||
      parsed.username ||
      parsed.password ||
      parsed.hash
    )
      return "";
    return parsed.href;
  } catch {
    return "";
  }
});

function applyTheme(persist = true) {
  document.documentElement.dataset.theme = dark.value ? "dark" : "light";
  if (persist)
    localStorage.setItem("linlinqi-theme", dark.value ? "dark" : "light");
}

function toggleTheme() {
  dark.value = !dark.value;
  applyTheme();
}

function applyStorefrontMetadata() {
  const title = String(
    store.value?.seo?.title || store.value?.name || "LinLinQi",
  );
  const description = String(
    store.value?.seo?.description || store.value?.tagline || t("app.tagline"),
  );
  document.title = title;
  let meta = document.querySelector<HTMLMetaElement>(
    'meta[name="description"]',
  );
  if (!meta) {
    meta = document.createElement("meta");
    meta.name = "description";
    document.head.append(meta);
  }
  meta.content = description;
}

function applyStorefrontDensity() {
  document.documentElement.dataset.density =
    store.value.theme?.density === "compact" ? "compact" : "comfortable";
}

async function refreshCartCount(event?: Event) {
  const detail = (event as CustomEvent<{ count?: number }>)?.detail;
  if (typeof detail?.count === "number") {
    cartCount.value = detail.count;
    return;
  }
  try {
    const cart = await fetchCart();
    cartCount.value = cart.items.reduce((sum, item) => sum + item.quantity, 0);
  } catch {
    cartCount.value = 0;
  }
}

function popDepthTarget(
  target: EventTarget | null,
  clientX?: number,
  clientY?: number,
) {
  if (
    !(target instanceof Element) ||
    matchMedia("(prefers-reduced-motion: reduce)").matches
  )
    return;
  const element = target.closest<HTMLElement>(depthPopSelector);
  if (
    !element ||
    !element.closest(".site-shell") ||
    element.hasAttribute("data-no-depth")
  )
    return;

  const rect = element.getBoundingClientRect();
  const x = Number.isFinite(clientX)
    ? ((Number(clientX) - rect.left) / Math.max(rect.width, 1)) * 100
    : 50;
  const y = Number.isFinite(clientY)
    ? ((Number(clientY) - rect.top) / Math.max(rect.height, 1)) * 100
    : 50;
  element.style.setProperty(
    "--depth-pointer-x",
    `${Math.max(0, Math.min(100, x))}%`,
  );
  element.style.setProperty(
    "--depth-pointer-y",
    `${Math.max(0, Math.min(100, y))}%`,
  );

  const previousTimer = depthPopTimers.get(element);
  if (previousTimer) window.clearTimeout(previousTimer);
  element.classList.remove("is-depth-popping");
  void element.offsetWidth;
  element.classList.add("is-depth-popping");
  depthPopTimers.set(
    element,
    window.setTimeout(() => {
      element.classList.remove("is-depth-popping");
      depthPopTimers.delete(element);
    }, 480),
  );
}

function handleDepthPointer(event: PointerEvent) {
  if (event.button !== 0) return;
  popDepthTarget(event.target, event.clientX, event.clientY);
}

function handleDepthKeyboard(event: KeyboardEvent) {
  if (event.key !== "Enter" && event.key !== " ") return;
  popDepthTarget(event.target);
}

onMounted(async () => {
  window.addEventListener(CART_UPDATED_EVENT, refreshCartCount);
  document.addEventListener("pointerup", handleDepthPointer, true);
  document.addEventListener("keyup", handleDepthKeyboard, true);
  void refreshCartCount();
  const savedTheme = localStorage.getItem("linlinqi-theme");
  const hasThemePreference = savedTheme === "dark" || savedTheme === "light";
  dark.value = hasThemePreference
    ? savedTheme === "dark"
    : matchMedia("(prefers-color-scheme: dark)").matches;
  applyTheme(false);
  applyStorefrontDensity();
  void fetchCurrencies()
    .then(registerCurrencies)
    .catch(() => undefined);
  try {
    store.value = {
      ...store.value,
      ...((await fetchStoreConfig()) as Partial<StorefrontConfig>),
    };
    setStoreCurrency(store.value.currency);
    if (!hasThemePreference) {
      const storefrontMode = String(store.value?.theme?.mode || "system");
      if (storefrontMode === "dark" || storefrontMode === "light") {
        dark.value = storefrontMode === "dark";
        applyTheme(false);
      }
    }
    applyStorefrontDensity();
    applyStorefrontMetadata();
  } catch {
    /* shell remains available while API recovers */
  }
});
onBeforeUnmount(() => {
  window.removeEventListener(CART_UPDATED_EVENT, refreshCartCount);
  document.removeEventListener("pointerup", handleDepthPointer, true);
  document.removeEventListener("keyup", handleDepthKeyboard, true);
  for (const timer of depthPopTimers.values()) window.clearTimeout(timer);
  depthPopTimers.clear();
});
</script>

<template>
  <div class="site-shell">
    <div class="topline">
      {{ store.announcements?.[0]?.title || store.tagline }}
    </div>
    <header class="site-header">
      <div class="container nav-wrap">
        <RouterLink class="brand" to="/" :aria-label="t('app.ariaHome')">
          <img
            v-if="storeLogoURL"
            class="store-logo"
            :src="storeLogoURL"
            :alt="store.name"
            referrerpolicy="no-referrer"
          />
          <span v-else class="brand-mark">LQ</span><span>{{ store.name }}</span
          ><small>{{
            store.reseller ? "POWERED BY LINLINQI" : "DIGITAL GOODS"
          }}</small>
        </RouterLink>
        <nav :class="['main-nav', { open: menuOpen }]">
          <RouterLink to="/" @click="menuOpen = false">{{
            t("app.nav.store")
          }}</RouterLink>
          <RouterLink to="/orders" @click="menuOpen = false">{{
            t("app.nav.orders")
          }}</RouterLink>
          <RouterLink to="/blog" @click="menuOpen = false">{{
            t("app.nav.blog")
          }}</RouterLink>
          <RouterLink to="/reseller/apply" @click="menuOpen = false">{{
            t("app.nav.reseller")
          }}</RouterLink>
        </nav>
        <div class="nav-actions">
          <RouterLink
            class="icon-button desktop-only"
            to="/?focus=search"
            :aria-label="t('app.ariaSearch')"
            ><Search :size="18"
          /></RouterLink>
          <button
            class="icon-button"
            :aria-label="themeLabel"
            @click="toggleTheme"
          >
            <Sun v-if="dark" :size="18" /><Moon v-else :size="18" />
          </button>
          <RouterLink
            class="icon-button cart-nav-button"
            to="/cart"
            :aria-label="t('app.ariaCart')"
          >
            <ShoppingBag :size="17" />
            <b v-if="cartCount" class="cart-count-badge">{{
              cartCount > 99 ? "99+" : cartCount
            }}</b>
          </RouterLink>
          <LocaleSwitcher />
          <CurrencySwitcher />
          <RouterLink
            class="account-button desktop-only"
            :to="signedIn ? '/account/profile' : '/auth/login'"
            ><UserRound :size="16" />{{
              signedIn ? t("app.account") : t("app.login")
            }}</RouterLink
          >
          <button
            class="icon-button menu-button"
            :aria-label="t('app.ariaMenu')"
            @click="menuOpen = !menuOpen"
          >
            <X v-if="menuOpen" /><Menu v-else />
          </button>
        </div>
      </div>
    </header>
    <main><RouterView /></main>
    <footer>
      <div class="container footer-grid">
        <div>
          <div class="brand footer-brand">
            <img
              v-if="storeLogoURL"
              class="store-logo"
              :src="storeLogoURL"
              :alt="store.name"
              referrerpolicy="no-referrer"
            />
            <span v-else class="brand-mark">LQ</span
            ><span>{{ store.name }}</span>
          </div>
          <p>{{ t("app.footerDesc") }}</p>
        </div>
        <div>
          <strong>{{ t("app.services") }}</strong
          ><RouterLink to="/">{{ t("app.products") }}</RouterLink
          ><RouterLink to="/orders">{{ t("app.nav.orders") }}</RouterLink
          ><RouterLink to="/account/tickets">{{
            t("app.supportTickets")
          }}</RouterLink>
        </div>
        <div>
          <strong>{{ t("app.guarantee") }}</strong
          ><RouterLink to="/legal/terms">{{ t("app.terms") }}</RouterLink
          ><RouterLink to="/legal/privacy">{{ t("app.privacy") }}</RouterLink
          ><RouterLink to="/notice">{{ t("app.notices") }}</RouterLink>
        </div>
        <div>
          <strong>{{ t("app.contact") }}</strong
          ><a v-if="supportEmail" :href="`mailto:${supportEmail}`">{{
            supportEmail
          }}</a
          ><a
            v-if="supportURL"
            :href="supportURL"
            target="_blank"
            rel="noopener noreferrer"
            >{{ t("app.onlineSupport") }}</a
          ><RouterLink to="/account/tickets">{{
            t("app.submitTicket")
          }}</RouterLink>
        </div>
      </div>
      <div class="container copyright">
        © {{ year }} {{ store.name }}. {{ t("app.rights") }}
        <span>{{ t("app.trustLine") }}</span>
      </div>
    </footer>
  </div>
</template>
