import axios from "axios";
import { safePublicHTTPURL } from "./utils/publicUrl";
import type {
  AffiliateData,
  AffiliateWithdrawal,
  APICredentialCreateResult,
  Cart,
  CheckoutQuote,
  CreateTicketPayload,
  GiftCardRecord,
  GiftCardRedeemResult,
  Order,
  OrderLookupCredential,
  OAuthProvider,
  ProductPage,
  ProductItem,
  ProductQuery,
  PublicAnnouncement,
  PublicBanner,
  PublicBannerPlacement,
  PublicContent,
  PublicPost,
  ResellerCatalogItem,
  ResellerDomain,
  ResellerDomainCreateResult,
  ResellerOrder,
  ResellerOverview,
  ResellerPage,
  ResellerProductRule,
  ResellerProductRulePayload,
  ResellerProfile,
  ResellerSite,
  ResellerSitePayload,
  ResellerWithdrawal,
  RechargeOrder,
  SupportTicket,
  TicketDetail,
  TicketMessage,
  TicketPage,
  WebhookCreateResult,
  WebhookEndpoint,
  WalletData,
} from "./types";
import type { CurrencyDefinition } from "./utils/money";

export const api = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || "http://127.0.0.1:8080/api/v1",
  timeout: 6000,
});

function targetsConfiguredAPI(config: { url?: string; baseURL?: string }) {
  try {
    const pageOrigin = window.location.origin;
    const apiBase = new URL(String(api.defaults.baseURL || ""), pageOrigin);
    const requestBase = new URL(
      String(config.baseURL || api.defaults.baseURL || ""),
      pageOrigin,
    );
    const requestURL = new URL(String(config.url || ""), requestBase);
    return requestURL.origin === apiBase.origin;
  } catch {
    return false;
  }
}

api.interceptors.request.use((config) => {
  if (!targetsConfiguredAPI(config)) return config;
  const token = localStorage.getItem("linlinqi-user-token");
  if (token) config.headers.Authorization = `Bearer ${token}`;
  if (typeof location !== "undefined" && location.hostname) {
    config.headers["X-Storefront-Host"] = location.hostname;
  }
  const locale =
    localStorage.getItem("linlinqi-locale") ||
    document.documentElement.lang ||
    "zh-CN";
  config.headers["X-LinLinQi-Locale"] = locale;
  return config;
});

let refreshPromise: Promise<string> | null = null;
api.interceptors.response.use(
  (response) => response,
  async (error) => {
    const config = error.config as any;
    const refreshToken = localStorage.getItem("linlinqi-user-refresh-token");
    if (
      error.response?.status !== 401 ||
      config?._retry ||
      !targetsConfiguredAPI(config || {}) ||
      !refreshToken ||
      String(config?.url || "").includes("/auth/")
    )
      throw error;
    config._retry = true;
    refreshPromise ||= axios
      .post(`${api.defaults.baseURL}/auth/refresh`, {
        refresh_token: refreshToken,
      })
      .then(({ data }) => {
        localStorage.setItem("linlinqi-user-token", data.data.token);
        localStorage.setItem(
          "linlinqi-user-refresh-token",
          data.data.refresh_token,
        );
        return data.data.token as string;
      })
      .catch((refreshError) => {
        localStorage.removeItem("linlinqi-user-token");
        localStorage.removeItem("linlinqi-user-refresh-token");
        localStorage.removeItem("linlinqi-user-profile");
        throw refreshError;
      })
      .finally(() => {
        refreshPromise = null;
      });
    const token = await refreshPromise;
    if (!targetsConfiguredAPI(config)) throw error;
    config.headers.Authorization = `Bearer ${token}`;
    return api.request(config);
  },
);

function positiveInteger(value: unknown, fallback: number) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
}

async function orderIdempotency(scope: string, payload: unknown) {
  const serialized = JSON.stringify(payload);
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(serialized),
  );
  const fingerprint = Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
  const storageKey = `linlinqi-order-idempotency:${scope}:${fingerprint}`;
  let key = sessionStorage.getItem(storageKey);
  if (!key) {
    key = `order-${crypto.randomUUID()}-${crypto.randomUUID()}`;
    sessionStorage.setItem(storageKey, key);
  }
  return { key, storageKey };
}

export async function fetchProducts(
  params: ProductQuery = {},
): Promise<ProductPage> {
  const { data } = await api.get("/products", { params });
  const payload = (data?.data || {}) as Partial<ProductPage>;
  const items = Array.isArray(payload.items) ? payload.items : [];
  const total = Number(payload.total);
  return {
    items,
    total:
      Number.isFinite(total) && total >= 0 ? Math.trunc(total) : items.length,
    page: positiveInteger(payload.page, positiveInteger(params.page, 1)),
    page_size: positiveInteger(
      payload.page_size,
      positiveInteger(params.page_size, Math.max(items.length, 1)),
    ),
  };
}

export async function fetchCategories() {
  const { data } = await api.get("/categories");
  return data.data as import("./types").Category[];
}

export async function fetchProduct(slug: string, currency?: string) {
  const { data } = await api.get(`/products/${encodeURIComponent(slug)}`, {
    params: currency ? { currency } : undefined,
  });
  return data.data as ProductItem;
}

export async function createOrder(payload: {
  product_id: string;
  variant_id?: string;
  quantity: number;
  contact: string;
  payment_method: string;
  coupon_code?: string;
  input_values?: import("./types").CheckoutInputValue[];
  currency: string;
}) {
  const idempotency = await orderIdempotency("single", payload);
  const { data } = await api.post("/orders", payload, {
    headers: { "Idempotency-Key": idempotency.key },
  });
  sessionStorage.removeItem(idempotency.storageKey);
  const order = data.data as Order;
  rememberOrderLookup(order);
  return order;
}

export async function fetchPaymentChannels(
  productIds: string[] = [],
  currency?: string,
) {
  const uniqueProductIds = [...new Set(productIds.filter(Boolean))];
  const { data } = await api.get("/payment-channels", {
    params: {
      ...(uniqueProductIds.length
        ? { product_ids: uniqueProductIds.join(",") }
        : {}),
      ...(currency ? { currency } : {}),
    },
  });
  return data.data as Array<{
    id: string;
    name: string;
    code: string;
    fee_rate: number;
    supported_currencies: string[];
    settlement_currency: string;
  }>;
}

export async function fetchCurrencies(): Promise<CurrencyDefinition[]> {
  const { data } = await api.get("/currencies");
  return (Array.isArray(data?.data) ? data.data : []) as CurrencyDefinition[];
}

export async function fetchWallet(currency?: string) {
  const { data } = await api.get("/me/wallet", {
    params: currency ? { currency } : undefined,
  });
  return data.data as WalletData;
}

export async function fetchRecharges(page = 1, pageSize = 20) {
  const { data } = await api.get("/me/recharges", {
    params: { page, page_size: pageSize },
  });
  return resellerPage<RechargeOrder>(data.data, page, pageSize);
}

export async function createRecharge(
  amount: number,
  channelCode: string,
  idempotencyKey: string,
  currency: string,
) {
  const { data } = await api.post(
    "/me/recharges",
    { amount, channel_code: channelCode, currency },
    { headers: { "Idempotency-Key": idempotencyKey } },
  );
  return data.data as {
    recharge: RechargeOrder;
    qr_code?: string;
  };
}

export async function createPayment(
  orderNo: string,
  contact: string,
  channelCode: string,
) {
  const lookup = recentOrderLookups().find((item) => item.order_no === orderNo);
  const { data } = await api.post(
    `/orders/${orderNo}/payments`,
    {
      contact,
      channel_code: channelCode,
      return_url: `${location.origin}/orders`,
    },
    lookup?.lookup_token
      ? { headers: { "X-Order-Token": lookup.lookup_token } }
      : undefined,
  );
  return data.data as {
    intent: { checkout_url: string; status: string };
    qr_code?: string;
  };
}

export function guestCartToken() {
  let token = localStorage.getItem("linlinqi-cart-token");
  if (!token) {
    token = crypto.randomUUID();
    localStorage.setItem("linlinqi-cart-token", token);
  }
  return token;
}

export const CART_UPDATED_EVENT = "linlinqi:cart-updated";
function notifyCartUpdated(cart?: Cart, explicitCount?: number) {
  if (typeof window === "undefined") return;
  window.dispatchEvent(
    new CustomEvent(CART_UPDATED_EVENT, {
      detail: {
        count:
          explicitCount ??
          cart?.items?.reduce((sum, item) => sum + item.quantity, 0),
      },
    }),
  );
}

export async function fetchCart(currency?: string) {
  const { data } = await api.get(`/carts/${guestCartToken()}`, {
    params: currency ? { currency } : undefined,
  });
  const cart = data.data as Cart;
  notifyCartUpdated(cart);
  return cart;
}

export async function upsertCartItem(
  productId: string,
  quantity: number,
  variantId?: string,
  currency?: string,
) {
  const { data } = await api.put(`/carts/${guestCartToken()}/items`, {
    product_id: productId,
    ...(variantId ? { variant_id: variantId } : {}),
    quantity,
    ...(currency ? { currency } : {}),
  });
  const cart = data.data as Cart;
  notifyCartUpdated(cart);
  return cart;
}

export async function removeCartItem(productId: string, variantId?: string) {
  await api.delete(`/carts/${guestCartToken()}/items/${productId}`, {
    params: variantId ? { variant_id: variantId } : undefined,
  });
  notifyCartUpdated();
}

export async function createCartOrder(
  contact: string,
  paymentMethod: string,
  currency: string,
  couponCode = "",
  inputValues: import("./types").CheckoutInputValue[] = [],
) {
  const payload = {
    guest_token: guestCartToken(),
    contact,
    payment_method: paymentMethod,
    currency,
    ...(couponCode.trim() ? { coupon_code: couponCode.trim() } : {}),
    ...(inputValues.length ? { input_values: inputValues } : {}),
  };
  const idempotency = await orderIdempotency("cart", payload);
  const { data } = await api.post("/cart-orders", payload, {
    headers: { "Idempotency-Key": idempotency.key },
  });
  sessionStorage.removeItem(idempotency.storageKey);
  const order = data.data as Order;
  notifyCartUpdated(undefined, 0);
  rememberOrderLookup(order);
  return order;
}

export async function fetchCheckoutQuote(
  lines: Array<{
    product_id: string;
    variant_id?: string;
    quantity: number;
  }>,
  paymentMethod: string,
  currency: string,
  contact = "",
  couponCode = "",
) {
  const { data } = await api.post("/checkout/quote", {
    lines,
    payment_method: paymentMethod,
    currency,
    ...(contact.trim() ? { contact: contact.trim() } : {}),
    ...(couponCode.trim() ? { coupon_code: couponCode.trim() } : {}),
  });
  return data.data as CheckoutQuote;
}

export async function fetchMyReseller() {
  const { data } = await api.get("/me/reseller");
  const payload = data.data || {};
  return {
    ...payload,
    domains: Array.isArray(payload.domains) ? payload.domains : [],
    product_rules: Array.isArray(payload.product_rules)
      ? payload.product_rules
      : [],
  } as ResellerOverview;
}

export async function applyReseller(name: string) {
  const { data } = await api.post("/me/reseller/apply", { name });
  return data.data as ResellerProfile;
}

function resellerPage<T>(
  payload: any,
  requestedPage: number,
  requestedPageSize: number,
): ResellerPage<T> {
  const total = Number(payload?.total);
  const page = Number(payload?.page);
  const pageSize = Number(payload?.page_size);
  return {
    items: Array.isArray(payload?.items) ? payload.items : [],
    total: Number.isFinite(total) && total >= 0 ? total : 0,
    page: Number.isInteger(page) && page > 0 ? page : requestedPage,
    page_size:
      Number.isInteger(pageSize) && pageSize > 0 ? pageSize : requestedPageSize,
  };
}

export async function fetchResellerCatalog(page = 1, q = "", pageSize = 10) {
  const { data } = await api.get("/me/reseller/catalog", {
    params: {
      page,
      page_size: pageSize,
      ...(q.trim() ? { q: q.trim() } : {}),
    },
  });
  const result = resellerPage<ResellerCatalogItem>(data.data, page, pageSize);
  result.items = result.items.map((item) => ({
    ...item,
    variants: Array.isArray(item.variants) ? item.variants : [],
    rules: Array.isArray(item.rules) ? item.rules : [],
  }));
  return result;
}

export async function fetchResellerOrders(page = 1, pageSize = 12) {
  const { data } = await api.get("/me/reseller/orders", {
    params: { page, page_size: pageSize },
  });
  return resellerPage<ResellerOrder>(data.data, page, pageSize);
}

export async function fetchResellerWithdrawals(page = 1, pageSize = 12) {
  const { data } = await api.get("/me/reseller/withdrawals", {
    params: { page, page_size: pageSize },
  });
  return resellerPage<ResellerWithdrawal>(data.data, page, pageSize);
}

export async function requestResellerWithdrawal(payload: {
  amount: number;
  method: "alipay" | "bank" | "usdt";
  account: string;
}) {
  const { data } = await api.post("/me/reseller/withdrawals", payload);
  return data.data.withdrawal as ResellerWithdrawal;
}

export async function createResellerDomain(domain: string) {
  const { data } = await api.post("/me/reseller/domains", { domain });
  return data.data as ResellerDomainCreateResult;
}

export async function verifyResellerDomain(id: string) {
  const { data } = await api.post(
    `/me/reseller/domains/${encodeURIComponent(id)}/verify`,
  );
  return data.data as { domain: ResellerDomain; notice?: string };
}

export async function deleteResellerDomain(id: string) {
  await api.delete(`/me/reseller/domains/${encodeURIComponent(id)}`);
}

export async function updateResellerSite(payload: ResellerSitePayload) {
  const { data } = await api.put("/me/reseller/site", payload);
  return data.data.site as ResellerSite;
}

export async function upsertResellerProductRule(
  productID: string,
  payload: ResellerProductRulePayload,
) {
  const { data } = await api.put(
    `/me/reseller/product-rules/${encodeURIComponent(productID)}`,
    payload,
  );
  return data.data.rule as ResellerProductRule;
}

export async function fetchAccountResource(resource = "") {
  const { data } = await api.get(`/me${resource ? `/${resource}` : ""}`);
  return data.data;
}

export async function fetchMyNotifications(
  params: { page?: number; page_size?: number; unread?: boolean } = {},
) {
  const { data } = await api.get("/me/notifications", { params });
  return data.data as {
    items: Array<{
      id: string;
      event_code: string;
      entity_id: string;
      title: string;
      body: string;
      read_at?: string;
      created_at: string;
    }>;
    total: number;
    page: number;
    page_size: number;
  };
}

export async function markMyNotificationRead(id: string) {
  const { data } = await api.post(
    `/me/notifications/${encodeURIComponent(id)}/read`,
  );
  return data.data as { read: boolean };
}

export async function updateMyProfile(
  nickname: string,
  email: string,
  currentPassword = "",
) {
  const { data } = await api.patch("/me/profile", {
    nickname,
    email,
    current_password: currentPassword,
  });
  return data.data.user as {
    id: string;
    email: string;
    nickname: string;
    avatar_url?: string;
    status: string;
    last_login_at: string;
    created_at: string;
  };
}

export async function uploadMyAvatar(file: File) {
  const form = new FormData();
  form.append("file", file);
  const { data } = await api.post("/me/profile/avatar", form, {
    headers: { "Content-Type": "multipart/form-data" },
  });
  return data.data.user as {
    id: string;
    email: string;
    nickname: string;
    avatar_url?: string;
    status: string;
    last_login_at: string;
    created_at: string;
  };
}

export async function changeMyPassword(
  currentPassword: string,
  newPassword: string,
) {
  const { data } = await api.post("/me/password", {
    current_password: currentPassword,
    new_password: newPassword,
  });
  return data.data as { changed: boolean; sessions_revoked: boolean };
}

export async function fetchTickets(page = 1, pageSize = 12) {
  const { data } = await api.get("/me/tickets", {
    params: { page, page_size: pageSize },
  });
  const payload = data.data || {};
  const items = Array.isArray(payload) ? payload : payload.items || [];
  return {
    items,
    total: Number(Array.isArray(payload) ? payload.length : payload.total || 0),
    page: Number(payload.page || page),
    page_size: Number(payload.page_size || pageSize),
  } as TicketPage;
}

export async function createTicket(payload: CreateTicketPayload) {
  const { data } = await api.post("/me/tickets", payload);
  return data.data.ticket as SupportTicket;
}

export async function fetchTicket(id: string) {
  const { data } = await api.get(`/me/tickets/${encodeURIComponent(id)}`);
  return data.data as TicketDetail;
}

export async function replyTicket(id: string, body: string) {
  const { data } = await api.post(
    `/me/tickets/${encodeURIComponent(id)}/messages`,
    { body },
  );
  return data.data as TicketMessage;
}

export async function fetchTicketOrderOptions() {
  const { data } = await api.get("/me/orders", {
    params: { page: 1, page_size: 100 },
  });
  const payload = data.data;
  return (Array.isArray(payload) ? payload : payload?.items || []) as Order[];
}

export async function fetchGiftCards() {
  const { data } = await api.get("/me/gift-cards");
  const payload = data.data;
  return (
    Array.isArray(payload) ? payload : payload?.items || []
  ) as GiftCardRecord[];
}

export async function redeemGiftCard(code: string) {
  const { data } = await api.post("/me/gift-cards/redeem", { code });
  return data.data as GiftCardRedeemResult;
}

export async function fetchAffiliate() {
  const { data } = await api.get("/me/affiliate");
  const payload = data.data || {};
  return {
    profile: payload.profile || null,
    commissions: Array.isArray(payload.commissions) ? payload.commissions : [],
    withdrawals: Array.isArray(payload.withdrawals) ? payload.withdrawals : [],
    balances: Array.isArray(payload.balances) ? payload.balances : [],
    referral_count: Number(payload.referral_count || 0),
    referral_link:
      typeof payload.referral_link === "string" ? payload.referral_link : "",
  } as AffiliateData;
}

export async function applyAffiliate() {
  const { data } = await api.post("/me/affiliate/apply", {
    accepted_terms: true,
  });
  return data.data;
}

export async function requestAffiliateWithdrawal(payload: {
  amount: number;
  method: "alipay" | "bank" | "usdt";
  account: string;
  currency: string;
}) {
  const { data } = await api.post("/me/affiliate/withdrawals", payload);
  return data.data.withdrawal as AffiliateWithdrawal;
}

export async function createAPICredential(name: string) {
  const { data } = await api.post("/me/api-credentials", { name });
  return data.data as APICredentialCreateResult;
}

export async function revokeAPICredential(id: string) {
  const { data } = await api.delete(
    "/me/api-credentials/" + encodeURIComponent(id),
  );
  return data.data as { revoked: boolean; changed: boolean };
}

export async function fetchWebhooks() {
  const { data } = await api.get("/me/webhooks");
  const payload = data.data;
  return (
    Array.isArray(payload) ? payload : payload?.items || []
  ) as WebhookEndpoint[];
}

export async function createWebhook(url: string) {
  const { data } = await api.post("/me/webhooks", {
    url,
    events: ["order.delivered"],
  });
  return data.data as WebhookCreateResult;
}

export async function deleteWebhook(id: string) {
  await api.delete(`/me/webhooks/${encodeURIComponent(id)}`);
}

export async function revokeSession(id: string) {
  await api.delete(`/me/sessions/${id}`);
}

export async function logout() {
  const refreshToken = localStorage.getItem("linlinqi-user-refresh-token");
  try {
    if (refreshToken)
      await api.post("/auth/logout", { refresh_token: refreshToken });
  } catch {
    // Local sign-out must remain available during an API outage. The short-lived
    // access token is discarded locally and the refresh token cannot be retried
    // by this browser after cleanup.
  } finally {
    localStorage.removeItem("linlinqi-user-token");
    localStorage.removeItem("linlinqi-user-refresh-token");
    localStorage.removeItem("linlinqi-user-profile");
  }
}

export async function requestPasswordReset(email: string) {
  const { data } = await api.post("/auth/forgot", { email });
  return data.data;
}

export async function resetPassword(token: string, password: string) {
  const { data } = await api.post("/auth/reset", { token, password });
  return data.data;
}

const publicBannerPlacements = new Set<PublicBannerPlacement>([
  "home_hero",
  "home_secondary",
  "content_header",
]);

function normalizePublicBanner(value: unknown): PublicBanner | null {
  if (!value || typeof value !== "object") return null;
  const record = value as Record<string, unknown>;
  const id = typeof record.id === "string" ? record.id.trim() : "";
  const title = typeof record.title === "string" ? record.title.trim() : "";
  const imageURL = safePublicHTTPURL(record.image_url);
  const placement = String(record.placement || "") as PublicBannerPlacement;
  if (!id || !title || !imageURL || !publicBannerPlacements.has(placement))
    return null;
  return {
    id,
    title,
    image_url: imageURL,
    target_url: safePublicHTTPURL(record.target_url),
    placement,
  };
}

export async function fetchPublicContent(): Promise<PublicContent> {
  const { data } = await api.get("/content");
  const payload = (data?.data || {}) as Record<string, unknown>;
  const banners = Array.isArray(payload.banners)
    ? payload.banners
        .map(normalizePublicBanner)
        .filter((item): item is PublicBanner => item !== null)
    : [];
  return {
    banners,
    posts: Array.isArray(payload.posts)
      ? (payload.posts as PublicPost[])
      : ([] as PublicPost[]),
    announcements: Array.isArray(payload.announcements)
      ? (payload.announcements as PublicAnnouncement[])
      : ([] as PublicAnnouncement[]),
  };
}

export async function fetchPost(slug: string): Promise<PublicPost> {
  const { data } = await api.get(`/posts/${encodeURIComponent(slug)}`);
  return data.data as PublicPost;
}

export async function fetchStoreConfig() {
  const { data } = await api.get("/store/config");
  return data.data;
}

export async function queryOrder(orderNo: string, lookupToken: string) {
  const { data } = await api.get(`/orders/${encodeURIComponent(orderNo)}`, {
    headers: { "X-Order-Token": lookupToken },
  });
  return data.data as Order;
}

const orderLookupStorageKey = "linlinqi-order-lookups";

export function rememberOrderLookup(order: Order) {
  if (!order.order_no || !order.lookup_token) return;
  const current = recentOrderLookups().filter(
    (item) => item.order_no !== order.order_no,
  );
  current.unshift({
    order_no: order.order_no,
    lookup_token: order.lookup_token,
    saved_at: new Date().toISOString(),
  });
  sessionStorage.setItem(
    orderLookupStorageKey,
    JSON.stringify(current.slice(0, 10)),
  );
}

export function recentOrderLookups(): OrderLookupCredential[] {
  try {
    const parsed = JSON.parse(
      sessionStorage.getItem(orderLookupStorageKey) || "[]",
    );
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (item) =>
        typeof item?.order_no === "string" &&
        typeof item?.lookup_token === "string" &&
        typeof item?.saved_at === "string",
    );
  } catch {
    sessionStorage.removeItem(orderLookupStorageKey);
    return [];
  }
}

export async function login(account: string, password: string) {
  const { data } = await api.post("/auth/login", { account, password });
  localStorage.setItem("linlinqi-user-token", data.data.token);
  localStorage.setItem("linlinqi-user-refresh-token", data.data.refresh_token);
  localStorage.setItem("linlinqi-user-profile", JSON.stringify(data.data.user));
  return data.data;
}

export async function fetchOAuthProviders() {
  const { data } = await api.get("/auth/oauth/providers");
  return (Array.isArray(data.data) ? data.data : []) as OAuthProvider[];
}

export async function startOAuth(provider: string, redirect = "") {
  const { data } = await api.get(
    `/auth/oauth/${encodeURIComponent(provider)}/start`,
    { params: redirect ? { redirect } : undefined },
  );
  return data.data as { auth_url: string };
}

export async function exchangeOAuth(code: string) {
  const { data } = await api.post("/auth/oauth/exchange", { code });
  localStorage.setItem("linlinqi-user-token", data.data.token);
  localStorage.setItem("linlinqi-user-refresh-token", data.data.refresh_token);
  localStorage.setItem("linlinqi-user-profile", JSON.stringify(data.data.user));
  return data.data as {
    token: string;
    refresh_token: string;
    expires_in: number;
    user: Record<string, unknown>;
    redirect: string;
  };
}

export async function register(
  email: string,
  password: string,
  nickname: string,
  referralCode = "",
) {
  const { data } = await api.post("/auth/register", {
    email,
    password,
    nickname,
    ...(referralCode.trim()
      ? { referral_code: referralCode.trim().toUpperCase() }
      : {}),
  });
  localStorage.setItem("linlinqi-user-token", data.data.token);
  localStorage.setItem("linlinqi-user-refresh-token", data.data.refresh_token);
  localStorage.setItem("linlinqi-user-profile", JSON.stringify(data.data.user));
  return data.data;
}
