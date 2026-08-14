<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { RouterLink } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  ArrowLeft,
  Check,
  Copy,
  CreditCard,
  Eye,
  EyeOff,
  Mail,
  RefreshCw,
  ShieldCheck,
  WalletCards,
} from "@lucide/vue";
import {
  createCartOrder,
  createPayment,
  fetchCart,
  fetchAccountResource,
  fetchCheckoutQuote,
  fetchPaymentChannels,
  queryOrder,
} from "../api";
import type {
  CartItem,
  CheckoutInputValue,
  CheckoutQuote,
  Order,
  ProductInputField,
} from "../types";
import {
  formatMinor,
  normalizeCurrency,
  selectedCurrency,
} from "../utils/money";
import { productArtwork } from "../utils/brandAssets";
import { isValidCheckoutContact } from "../utils/contact";
import { safeBrowserPattern } from "../utils/browserPattern";
import { safeNavigationURL } from "../utils/publicUrl";

const { t, locale } = useI18n();

const email = ref("");
const signedIn = ref(Boolean(localStorage.getItem("linlinqi-user-token")));
const couponCode = ref("");
const channel = ref("");
const agreed = ref(false);
const loading = ref(true);
const submitting = ref(false);
const paymentRetrying = ref(false);
const tokenVisible = ref(false);
const tokenCopied = ref(false);
const error = ref("");
const items = ref<CartItem[]>([]);
const cartCurrency = ref("");
const currencyMismatch = computed(
  () =>
    Boolean(cartCurrency.value && selectedCurrency.value) &&
    normalizeCurrency(cartCurrency.value) !==
      normalizeCurrency(selectedCurrency.value),
);
const channels = ref<
  Array<{
    name: string;
    code: string;
    fee_rate: number;
    supported_currencies: string[];
    settlement_currency: string;
  }>
>([]);
const order = ref<Order | null>(null);
const quote = ref<CheckoutQuote | null>(null);
const quoteLoading = ref(false);
const inputValues = reactive<Record<string, string>>({});
const isDelivered = computed(
  () =>
    order.value?.status === "delivered" || order.value?.status === "completed",
);
const canRetryPayment = computed(
  () =>
    order.value?.status === "pending_payment" &&
    !["paid", "succeeded", "refunded"].includes(order.value.payment_status),
);
const subtotal = computed(
  () =>
    quote.value?.subtotal ||
    items.value.reduce((sum, item) => sum + (item.quote?.subtotal || 0), 0),
);
const discount = computed(() => quote.value?.discount || 0);
const fee = computed(() => quote.value?.fee || 0);
const total = computed(
  () =>
    quote.value?.total ||
    items.value.reduce((sum, item) => sum + (item.quote?.total || 0), 0),
);
const money = (value: number, currency = cartCurrency.value) =>
  formatMinor(value, currency, locale);
let quoteSequence = 0;

function inputKey(item: CartItem, field: ProductInputField) {
  return `${item.product_id}:${item.variant_id || ""}:${field.id}`;
}

function validInputValue(field: ProductInputField, raw: string) {
  const value = raw.trim();
  if (!value) return !field.required;
  const length = [...value].length;
  if (length < field.min_length || length > field.max_length) return false;
  if (field.input_type === "email" && !/^\S+@\S+\.\S+$/.test(value))
    return false;
  if (
    field.input_type === "number" &&
    !/^-?(?:0|[1-9][0-9]*)(?:\.[0-9]{1,8})?$/.test(value)
  )
    return false;
  if (field.input_type === "select" && !field.options.includes(value))
    return false;
  if (field.validation_pattern) {
    const pattern = safeBrowserPattern(field.validation_pattern);
    if (pattern && !pattern.test(value)) return false;
  }
  return true;
}

function inputPayload(): CheckoutInputValue[] | null {
  const result: CheckoutInputValue[] = [];
  for (const item of items.value) {
    for (const field of item.input_fields || []) {
      const value = inputValues[inputKey(item, field)] || "";
      if (!validInputValue(field, value)) {
        error.value =
          field.required && !value.trim()
            ? t("checkout.errInputRequired", { field: field.label })
            : t("checkout.errInputInvalid", { field: field.label });
        return null;
      }
      if (value.trim())
        result.push({
          product_id: item.product_id,
          ...(item.variant_id ? { variant_id: item.variant_id } : {}),
          field_id: field.id,
          value,
        });
    }
  }
  return result;
}

function requestLines() {
  return items.value.map((item) => ({
    product_id: item.product_id,
    ...(item.variant_id ? { variant_id: item.variant_id } : {}),
    quantity: item.quantity,
  }));
}

function lineQuote(item: CartItem) {
  return quote.value?.lines.find(
    (line) =>
      line.product_id === item.product_id &&
      (line.variant_id || "") === (item.variant_id || ""),
  );
}

async function refreshQuote(showError = false) {
  if (!items.value.length || !channel.value) return false;
  const sequence = ++quoteSequence;
  quoteLoading.value = true;
  try {
    const next = await fetchCheckoutQuote(
      requestLines(),
      channel.value,
      cartCurrency.value,
      email.value,
      couponCode.value,
    );
    if (sequence === quoteSequence) quote.value = next;
    return true;
  } catch (reason: any) {
    if (sequence === quoteSequence) quote.value = null;
    if (showError)
      error.value = reason?.response?.data?.message || t("checkout.errQuote");
    return false;
  } finally {
    if (sequence === quoteSequence) quoteLoading.value = false;
  }
}

function orderStatusLabel(status: string) {
  const key = `checkout.orderStatus.${status}`;
  const translated = t(key);
  return translated === key ? status : translated;
}

async function copyLookupToken() {
  const token = order.value?.lookup_token;
  if (!token) return;
  try {
    await navigator.clipboard.writeText(token);
    tokenCopied.value = true;
    window.setTimeout(() => (tokenCopied.value = false), 1800);
  } catch {
    tokenVisible.value = true;
    error.value = t("checkout.errCopyToken");
  }
}

async function syncCreatedOrder() {
  const current = order.value;
  if (!current?.lookup_token) return false;
  try {
    const latest = await queryOrder(current.order_no, current.lookup_token);
    order.value = {
      ...latest,
      lookup_token: current.lookup_token,
      payment_method: latest.payment_method || current.payment_method,
    };
    return true;
  } catch {
    return false;
  }
}

async function startPayment() {
  const current = order.value;
  if (!current || !canRetryPayment.value) return;
  error.value = "";
  paymentRetrying.value = true;
  try {
    const result = await createPayment(
      current.order_no,
      current.email || email.value,
      current.payment_method || channel.value,
    );
    if (result.intent.checkout_url) {
      const checkoutURL = safeNavigationURL(result.intent.checkout_url);
      if (checkoutURL) location.assign(checkoutURL);
      else error.value = t("checkout.errPaymentSafe");
      return;
    }
    if (
      ["succeeded", "partially_refunded", "refunded"].includes(
        result.intent.status,
      ) &&
      (await syncCreatedOrder())
    ) {
      if (!isDelivered.value)
        error.value = t("checkout.paymentAcceptedPendingDelivery");
      return;
    }
    error.value = t("checkout.errPaymentPending");
  } catch (reason: any) {
    const previousStatus = current.status;
    const synchronized = await syncCreatedOrder();
    if (synchronized && order.value?.status !== previousStatus) {
      if (!isDelivered.value)
        error.value = t("checkout.paymentAcceptedPendingDelivery");
      return;
    }
    const detail = reason?.response?.data?.message;
    error.value = detail
      ? t("checkout.errPaymentSafeWithReason", { reason: detail })
      : t("checkout.errPaymentSafe");
  } finally {
    paymentRetrying.value = false;
  }
}

onMounted(async () => {
  try {
    if (signedIn.value) {
      try {
        const account = await fetchAccountResource();
        email.value = String(account.user?.email || "");
      } catch {
        signedIn.value = false;
      }
    }
    const cart = await fetchCart(selectedCurrency.value);
    items.value = cart.items;
    cartCurrency.value = normalizeCurrency(cart.currency);
    if (currencyMismatch.value) {
      error.value = t("money.cartCurrencyChanged");
      return;
    }
    const paymentChannels = await fetchPaymentChannels(
      items.value.map((item) => item.product_id),
      cartCurrency.value,
    );
    for (const item of items.value)
      for (const field of item.input_fields || [])
        inputValues[inputKey(item, field)] = "";
    channels.value = paymentChannels;
    channel.value = channels.value[0]?.code || "";
    if (items.value.length && !channels.value.length)
      error.value = t("money.noPaymentChannelForCurrency", {
        currency: cartCurrency.value,
      });
    await refreshQuote();
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("checkout.errLoad");
  } finally {
    loading.value = false;
  }
});
async function submit() {
  error.value = "";
  if (!isValidCheckoutContact(email.value)) {
    error.value = t("checkout.errEmail");
    return;
  }
  if (!items.value.length || !channel.value) {
    error.value = t("checkout.errEmptyCart");
    return;
  }
  if (currencyMismatch.value) {
    error.value = t("money.cartCurrencyChanged");
    return;
  }
  if (items.value.some((item) => !item.available)) {
    error.value = t("checkout.errUnavailableItems");
    return;
  }
  if (!agreed.value) {
    error.value = t("checkout.errAgree");
    return;
  }
  const values = inputPayload();
  if (!values) return;
  submitting.value = true;
  try {
    if (!(await refreshQuote(true))) return;
    if (quote.value?.lines.some((item) => !item.available)) {
      error.value = t("checkout.errPartialStock");
      return;
    }
    order.value = await createCartOrder(
      email.value,
      channel.value,
      cartCurrency.value,
      couponCode.value,
      values,
    );
    Object.keys(inputValues).forEach((key) => (inputValues[key] = ""));
    await startPayment();
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("checkout.errCheckout");
  } finally {
    submitting.value = false;
  }
}

watch(channel, () => void refreshQuote());
watch(currencyMismatch, (mismatch) => {
  if (mismatch) error.value = t("money.cartCurrencyChanged");
});
watch(selectedCurrency, () => {
  if (currencyMismatch.value) error.value = t("money.cartCurrencyChanged");
});
</script>

<template>
  <section class="section checkout-page">
    <div class="container checkout-narrow">
      <RouterLink class="back-link" to="/cart"
        ><ArrowLeft :size="16" />{{ t("checkout.backToCart") }}</RouterLink
      >
      <div v-if="order && isDelivered" class="success-panel">
        <div class="success-icon"><Check /></div>
        <span class="kicker">{{ t("checkout.deliveredKicker") }}</span>
        <h1>{{ t("checkout.delivered") }}</h1>
        <p>{{ t("checkout.deliveredDesc", { no: order.order_no }) }}</p>
        <div class="credential-list">
          <div v-for="entry in order.items" :key="entry.id">
            <span>{{ entry.product_name }}</span
            ><code>{{ entry.card_content }}</code>
          </div>
        </div>
        <RouterLink class="button secondary" to="/orders">{{
          t("checkout.lookupOrder")
        }}</RouterLink>
      </div>
      <div v-else-if="order" class="payment-recovery-panel">
        <div class="recovery-mark"><ShieldCheck :size="28" /></div>
        <span class="kicker">{{ t("checkout.reservedKicker") }}</span>
        <h1>{{ t("checkout.pendingTitle") }}</h1>
        <p>{{ t("checkout.pendingDesc") }}</p>

        <div class="recovery-summary">
          <div>
            <span>{{ t("checkout.pendingOrderNo") }}</span>
            <strong>{{ order.order_no }}</strong>
          </div>
          <div>
            <span>{{ t("checkout.pendingAmount") }}</span>
            <strong>{{ money(order.total, order.currency) }}</strong>
          </div>
          <div>
            <span>{{ t("checkout.pendingStatus") }}</span>
            <strong>{{ orderStatusLabel(order.status) }}</strong>
          </div>
        </div>

        <div v-if="order.lookup_token" class="lookup-token-card">
          <div>
            <span>{{ t("checkout.lookupToken") }}</span>
            <code>{{
              tokenVisible ? order.lookup_token : "••••••••••••••••••••••••"
            }}</code>
          </div>
          <div class="lookup-token-actions">
            <button
              type="button"
              class="icon-action"
              :aria-label="
                tokenVisible ? t('checkout.hideToken') : t('checkout.showToken')
              "
              @click="tokenVisible = !tokenVisible"
            >
              <EyeOff v-if="tokenVisible" :size="17" />
              <Eye v-else :size="17" />
            </button>
            <button
              type="button"
              class="icon-action"
              :aria-label="t('checkout.copyToken')"
              @click="copyLookupToken"
            >
              <Check v-if="tokenCopied" :size="17" />
              <Copy v-else :size="17" />
            </button>
          </div>
          <small>{{ t("checkout.lookupTokenHint") }}</small>
        </div>

        <p v-if="error" class="form-error recovery-error">{{ error }}</p>
        <p class="recovery-warning">{{ t("checkout.pendingWarning") }}</p>
        <div class="recovery-actions">
          <button
            v-if="canRetryPayment"
            type="button"
            class="button primary"
            :disabled="paymentRetrying"
            @click="startPayment"
          >
            <RefreshCw :size="16" :class="{ spinning: paymentRetrying }" />
            {{
              paymentRetrying
                ? t("checkout.retryingPayment")
                : t("checkout.retryPayment")
            }}
          </button>
          <RouterLink class="button secondary" to="/orders">{{
            t("checkout.openLookup")
          }}</RouterLink>
        </div>
      </div>
      <template v-else
        ><div class="page-hero-row">
          <div>
            <span class="kicker">{{ t("checkout.checkoutKicker") }}</span>
            <h1>{{ t("checkout.title") }}</h1>
            <p>{{ t("checkout.subtitle") }}</p>
          </div>
          <div class="steps">
            <b>1</b><i></i><b class="active">2</b><i></i><b>3</b>
          </div>
        </div>
        <p v-if="error" class="form-error">{{ error }}</p>
        <p v-if="loading">{{ t("checkout.loading") }}</p>
        <div v-else class="checkout-columns">
          <div>
            <section class="form-panel">
              <header>
                <span>01</span>
                <div>
                  <h2>{{ t("checkout.receiveInfo") }}</h2>
                  <p>{{ t("checkout.receiveInfoDesc") }}</p>
                </div>
              </header>
              <label
                >{{ t("checkout.email") }}
                <div class="input-icon">
                  <Mail /><input
                    v-model="email"
                    type="text"
                    minlength="8"
                    maxlength="190"
                    autocomplete="off"
                    @blur="refreshQuote()"
                    placeholder="a2456836 / 86256hfikg"
                    :readonly="signedIn"
                    :class="{ 'contact-locked': signedIn }"
                  />
                </div>
                <small v-if="signedIn" class="contact-locked-hint">{{
                  t("checkout.signedInContactLocked")
                }}</small>
              </label>
              <label
                >{{ t("checkout.coupon") }}
                <div class="coupon-row">
                  <input
                    v-model.trim="couponCode"
                    maxlength="80"
                    :placeholder="t('checkout.couponPlaceholder')"
                    @keyup.enter="refreshQuote(true)"
                  />
                  <button
                    type="button"
                    class="button secondary"
                    :disabled="quoteLoading || !couponCode"
                    @click="refreshQuote(true)"
                  >
                    {{
                      quoteLoading
                        ? t("product.calculating")
                        : t("product.apply")
                    }}
                  </button>
                </div>
              </label>
            </section>
            <section
              v-if="items.some((item) => item.input_fields?.length)"
              class="form-panel"
            >
              <header>
                <span>02</span>
                <div>
                  <h2>{{ t("checkout.productInfoTitle") }}</h2>
                  <p>{{ t("checkout.productInfoDesc") }}</p>
                </div>
              </header>
              <div
                v-for="item in items.filter(
                  (entry) => entry.input_fields?.length,
                )"
                :key="`inputs-${item.id}`"
                class="checkout-product-inputs"
              >
                <h3>
                  {{ item.product?.name || item.product_id }}
                  <small v-if="item.variant">· {{ item.variant.name }}</small>
                </h3>
                <label
                  v-for="field in item.input_fields || []"
                  :key="field.id"
                  class="custom-input-field"
                  >{{ field.label }}<em v-if="field.required">*</em>
                  <select
                    v-if="field.input_type === 'select'"
                    v-model="inputValues[inputKey(item, field)]"
                    :required="field.required"
                  >
                    <option value="" disabled>
                      {{ field.placeholder || field.label }}
                    </option>
                    <option
                      v-for="option in field.options"
                      :key="option"
                      :value="option"
                    >
                      {{ option }}
                    </option>
                  </select>
                  <textarea
                    v-else-if="field.input_type === 'textarea'"
                    v-model="inputValues[inputKey(item, field)]"
                    :required="field.required"
                    :minlength="field.min_length"
                    :maxlength="field.max_length"
                    :placeholder="field.placeholder"
                    :autocomplete="field.sensitive ? 'off' : 'on'"
                    rows="3"
                  ></textarea>
                  <input
                    v-else
                    v-model="inputValues[inputKey(item, field)]"
                    :type="
                      field.sensitive
                        ? 'password'
                        : field.input_type === 'email'
                          ? 'email'
                          : 'text'
                    "
                    :inputmode="
                      field.input_type === 'number' ? 'decimal' : undefined
                    "
                    :required="field.required"
                    :minlength="field.min_length"
                    :maxlength="field.max_length"
                    :placeholder="field.placeholder"
                    :autocomplete="
                      field.sensitive
                        ? 'new-password'
                        : field.input_type === 'email'
                          ? 'email'
                          : 'on'
                    "
                  />
                  <small v-if="field.help_text">{{ field.help_text }}</small>
                </label>
              </div>
            </section>
            <section class="form-panel">
              <header>
                <span>03</span>
                <div>
                  <h2>{{ t("checkout.paymentTitle") }}</h2>
                  <p>{{ t("checkout.paymentSub") }}</p>
                </div>
              </header>
              <div class="payment-options">
                <button
                  v-for="option in channels"
                  :key="option.code"
                  :class="{ active: channel === option.code }"
                  @click="channel = option.code"
                >
                  <WalletCards />
                  <div>
                    <b>{{ option.name }}</b
                    ><small>{{
                      option.fee_rate
                        ? t("checkout.fee", {
                            rate: (option.fee_rate / 100).toFixed(2),
                          })
                        : t("checkout.noFee")
                    }}</small>
                  </div>
                  <Check v-if="channel === option.code" />
                </button>
              </div>
            </section>
          </div>
          <aside class="checkout-card order-confirm">
            <span class="kicker">{{ t("checkout.orderKicker") }}</span>
            <div v-for="item in items" :key="item.id" class="confirm-item">
              <div>
                <img
                  :src="productArtwork(item.product)"
                  :alt="item.product?.name || ''"
                />
              </div>
              <span
                ><b>{{ item.product?.name || item.product_id }}</b
                ><small
                  >{{ item.variant ? `${item.variant.name} · ` : ""
                  }}{{ t("checkout.quantity", { n: item.quantity }) }} ·
                  {{
                    item.available
                      ? t("cart.available")
                      : t("checkout.unavailable")
                  }}</small
                ></span
              ><strong>{{
                money(lineQuote(item)?.quote.total ?? item.quote?.total ?? 0)
              }}</strong>
            </div>
            <div class="checkout-row">
              <span>{{ t("checkout.itemsAmount") }}</span
              ><b>{{ money(subtotal) }}</b>
            </div>
            <div v-if="discount" class="checkout-row discount-row">
              <span>{{ t("checkout.discountLabel") }}</span
              ><b>{{ money(-discount) }}</b>
            </div>
            <div class="checkout-row">
              <span>{{ t("checkout.feeLabel") }}</span
              ><b>{{ money(fee) }}</b>
            </div>
            <div v-if="quote?.adjustments.length" class="price-adjustments">
              <small
                v-for="adjustment in quote.adjustments"
                :key="`${adjustment.code}-${adjustment.label}`"
              >
                <span>{{ adjustment.label }}</span>
                <b>{{ money(adjustment.amount) }}</b>
              </small>
            </div>
            <div class="checkout-row total">
              <span>{{ t("checkout.totalLabel") }}</span
              ><strong>{{ money(total) }}</strong>
            </div>
            <label class="checkbox-row agreement">
              <input v-model="agreed" type="checkbox" />
              <span class="agreement-box"><Check :size="14" /></span>
              <span>{{ t("checkout.agree") }}</span> </label
            ><button
              class="button primary wide"
              :disabled="
                submitting ||
                quoteLoading ||
                !agreed ||
                !items.length ||
                items.some((item) => !item.available)
              "
              @click="submit"
            >
              <CreditCard :size="16" />{{
                submitting
                  ? t("checkout.creating")
                  : t("checkout.payNow", { amount: money(total) })
              }}
            </button>
            <p class="secure-note">
              <ShieldCheck />{{ t("checkout.secureNote") }}
            </p>
          </aside>
        </div></template
      >
    </div>
  </section>
</template>

<style scoped>
.payment-recovery-panel {
  max-width: 720px;
  margin: 28px auto;
  text-align: center;
}

.recovery-mark {
  width: 62px;
  height: 62px;
  margin: 0 auto 22px;
  border-radius: 18px;
  display: grid;
  place-items: center;
  color: var(--inverse-text);
  background: var(--inverse);
}

.payment-recovery-panel h1 {
  margin: 12px 0 10px;
  font-size: clamp(34px, 7vw, 52px);
  line-height: 1.02;
  letter-spacing: -0.055em;
}

.payment-recovery-panel > p {
  color: var(--muted);
}

.recovery-summary {
  display: grid;
  grid-template-columns: 1.7fr 0.8fr 0.8fr;
  margin: 32px 0 14px;
  border: 1px solid var(--line);
  border-radius: 10px;
  overflow: hidden;
  text-align: left;
  background: var(--surface);
}

.recovery-summary > div {
  min-width: 0;
  padding: 17px 18px;
  border-right: 1px solid var(--line);
}

.recovery-summary > div:last-child {
  border-right: 0;
}

.recovery-summary span,
.lookup-token-card span {
  display: block;
  margin-bottom: 7px;
  color: var(--muted);
  font-size: 10px;
}

.recovery-summary strong {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 13px;
}

.lookup-token-card {
  position: relative;
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 12px;
  padding: 18px;
  border: 1px solid var(--line);
  border-radius: 10px;
  text-align: left;
  background: var(--soft);
}

.lookup-token-card code {
  display: block;
  overflow-wrap: anywhere;
  font-size: 13px;
  letter-spacing: 0.035em;
}

.lookup-token-card small {
  grid-column: 1 / -1;
  color: var(--muted);
  font-size: 10px;
}

.lookup-token-actions {
  display: flex;
  gap: 7px;
}

.icon-action {
  width: 35px;
  height: 35px;
  border: 1px solid var(--line);
  border-radius: 7px;
  display: grid;
  place-items: center;
  color: var(--text);
  background: var(--surface);
  cursor: pointer;
}

.recovery-warning {
  margin: 17px 0 !important;
  padding: 12px 14px;
  border-left: 3px solid var(--text);
  text-align: left;
  font-size: 11px;
  background: var(--soft);
}

.recovery-error {
  margin: 16px 0 0 !important;
  text-align: left;
}

.recovery-actions {
  display: flex;
  justify-content: center;
  gap: 10px;
}

.spinning {
  animation: checkout-spin 0.9s linear infinite;
}

@keyframes checkout-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 640px) {
  .recovery-summary {
    grid-template-columns: 1fr;
  }

  .recovery-summary > div {
    border-right: 0;
    border-bottom: 1px solid var(--line);
  }

  .recovery-summary > div:last-child {
    border-bottom: 0;
  }

  .lookup-token-card {
    grid-template-columns: 1fr;
  }

  .lookup-token-actions {
    position: absolute;
    top: 11px;
    right: 11px;
  }

  .icon-action {
    width: 42px;
    height: 42px;
  }

  .lookup-token-card code {
    padding-right: 96px;
  }

  .recovery-actions {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
