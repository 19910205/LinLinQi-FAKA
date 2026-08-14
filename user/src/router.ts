import { createRouter, createWebHistory } from "vue-router";

const HomeView = () => import("./views/HomeView.vue");
const ProductView = () => import("./views/ProductView.vue");
const OrderLookupView = () => import("./views/OrderLookupView.vue");
const AccountView = () => import("./views/AccountView.vue");
const CartView = () => import("./views/CartView.vue");
const CheckoutView = () => import("./views/CheckoutView.vue");
const ContentView = () => import("./views/ContentView.vue");
const AccountCenterView = () => import("./views/AccountCenterView.vue");
const ResellerView = () => import("./views/ResellerView.vue");
const NotFoundView = () => import("./views/NotFoundView.vue");

const accountSections = new Set([
  "profile",
  "orders",
  "wallet",
  "notifications",
  "gift-cards",
  "tickets",
  "affiliate",
  "api",
  "webhooks",
  "security",
]);
const resellerSections = new Set([
  "apply",
  "dashboard",
  "products",
  "orders",
  "domains",
  "wallet",
  "site",
]);
const legalPages = new Set(["terms", "privacy"]);

const router = createRouter({
  history: createWebHistory(),
  scrollBehavior: () => ({ top: 0 }),
  routes: [
    { path: "/", component: HomeView },
    { path: "/products/:slug", component: ProductView },
    { path: "/orders", component: OrderLookupView },
    { path: "/cart", component: CartView },
    { path: "/checkout", component: CheckoutView },
    { path: "/blog", component: ContentView, meta: { kind: "blog" } },
    { path: "/blog/:slug", component: ContentView, meta: { kind: "article" } },
    { path: "/notice", component: ContentView, meta: { kind: "notice" } },
    { path: "/legal/:type", component: ContentView, meta: { kind: "legal" } },
    { path: "/auth/login", component: AccountView },
    { path: "/auth/register", component: AccountView },
    { path: "/auth/forgot", component: AccountView },
    { path: "/auth/reset", component: AccountView },
    { path: "/auth/oauth/callback", component: AccountView },
    { path: "/account", redirect: "/account/profile" },
    { path: "/account/:section", component: AccountCenterView },
    { path: "/reseller/:section?", component: ResellerView },
    { path: "/:pathMatch(.*)*", component: NotFoundView },
  ],
});

router.beforeEach((to) => {
  if (
    to.path.startsWith("/account/") &&
    String(to.params.section || "") &&
    !accountSections.has(String(to.params.section || ""))
  ) {
    return { path: "/account/profile", replace: true };
  }
  if (
    to.path.startsWith("/reseller/") &&
    String(to.params.section || "") &&
    !resellerSections.has(String(to.params.section || ""))
  ) {
    return { path: "/reseller/dashboard", replace: true };
  }
  if (
    to.path.startsWith("/legal/") &&
    !legalPages.has(String(to.params.type || ""))
  ) {
    return { path: "/legal/terms", replace: true };
  }
  const protectedPage =
    to.path.startsWith("/account") || to.path.startsWith("/reseller");
  if (protectedPage && !localStorage.getItem("linlinqi-user-token")) {
    return { path: "/auth/login", query: { redirect: to.fullPath } };
  }
});

export default router;
