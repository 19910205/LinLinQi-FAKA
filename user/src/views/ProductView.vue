<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";
import { RouterLink, useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  ArrowLeft,
  ChevronLeft,
  ChevronRight,
  Check,
  Copy,
  Minus,
  Plus,
  ShieldCheck,
  ShoppingCart,
  WalletCards,
  X,
  Zap,
  ZoomIn,
} from "@lucide/vue";
import {
  createOrder,
  createPayment,
  fetchCheckoutQuote,
  fetchAccountResource,
  fetchPaymentChannels,
  fetchProduct,
  queryOrder,
  upsertCartItem,
} from "../api";
import type {
  CheckoutInputValue,
  CheckoutQuote,
  Order,
  ProductInputField,
  ProductItem,
} from "../types";
import { formatMinor, selectedCurrency } from "../utils/money";
import { productArtworkGallery } from "../utils/brandAssets";
import { isValidCheckoutContact } from "../utils/contact";
import { safeBrowserPattern } from "../utils/browserPattern";
import { safeNavigationURL } from "../utils/publicUrl";
import { sanitizeProductHTML } from "../utils/sanitizeHTML";

const { t, locale } = useI18n();

const route = useRoute();
const item = ref<ProductItem | null>(null);
const productReady = ref(false);
const quantity = ref(1);
const email = ref("");
const signedIn = ref(Boolean(localStorage.getItem("linlinqi-user-token")));
const submitting = ref(false);
const error = ref("");
const order = ref<Order | null>(null);
const copied = ref(false);
const channels = ref<
  Array<{
    name: string;
    code: string;
    fee_rate: number;
    supported_currencies: string[];
    settlement_currency: string;
  }>
>([]);
const selectedChannel = ref("");
const selectedVariantID = ref("");
const couponCode = ref("");
const quote = ref<CheckoutQuote | null>(null);
const quoteLoading = ref(false);
const cartAdded = ref(false);
const inputValues = reactive<Record<string, string>>({});
const activeProductImage = ref("");
const imageLightboxOpen = ref(false);
const carouselPaused = ref(false);
const touchStartX = ref<number | null>(null);
const productImages = computed(() =>
  productArtworkGallery(item.value?.product),
);
const safeProductDescription = computed(() =>
  sanitizeProductHTML(item.value?.product.description),
);
const productCurrency = computed(() => item.value?.product.currency || "");
const money = (cents: number, currency = productCurrency.value) =>
  formatMinor(cents, currency, locale);
const selectedVariant = computed(() =>
  item.value?.variants?.find(
    (variant) => variant.id === selectedVariantID.value,
  ),
);
const currentStock = computed(
  () => selectedVariant.value?.stock ?? item.value?.stock ?? 0,
);
const purchaseLimit = computed(() => {
  const configured =
    selectedVariant.value?.purchase_limit || item.value?.product.maximum || 20;
  return Math.max(1, Math.min(20, configured, currentStock.value || 1));
});
const minimumPurchase = computed(() =>
  Math.max(1, Math.min(purchaseLimit.value, item.value?.product.minimum || 1)),
);
const displayPrice = computed(
  () => selectedVariant.value?.price ?? item.value?.product.price ?? 0,
);
const displayComparePrice = computed(
  () =>
    selectedVariant.value?.compare_price ??
    item.value?.product.compare_price ??
    0,
);
let quoteSequence = 0;
let quoteTimer: ReturnType<typeof setTimeout> | undefined;

function validateInputField(field: ProductInputField, raw: string) {
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

function orderInputPayload(): CheckoutInputValue[] | null {
  const result: CheckoutInputValue[] = [];
  for (const field of item.value?.input_fields || []) {
    const value = inputValues[field.id] || "";
    if (!validateInputField(field, value)) {
      error.value =
        field.required && !value.trim()
          ? t("product.errInputRequired", { field: field.label })
          : t("product.errInputInvalid", { field: field.label });
      return null;
    }
    if (value.trim()) result.push({ field_id: field.id, value });
  }
  return result;
}

function quoteLines() {
  if (!item.value) return [];
  return [
    {
      product_id: item.value.product.id,
      ...(selectedVariantID.value
        ? { variant_id: selectedVariantID.value }
        : {}),
      quantity: quantity.value,
    },
  ];
}

async function refreshQuote(showError = false) {
  if (!item.value || !selectedChannel.value) return false;
  if ((item.value.variants?.length || 0) > 0 && !selectedVariantID.value)
    return false;
  const sequence = ++quoteSequence;
  quoteLoading.value = true;
  try {
    const next = await fetchCheckoutQuote(
      quoteLines(),
      selectedChannel.value,
      selectedCurrency.value,
      email.value,
      couponCode.value,
    );
    if (sequence === quoteSequence) quote.value = next;
    return true;
  } catch (reason: any) {
    if (sequence === quoteSequence) quote.value = null;
    if (showError)
      error.value = reason?.response?.data?.message || t("product.errQuote");
    return false;
  } finally {
    if (sequence === quoteSequence) quoteLoading.value = false;
  }
}

function scheduleQuote() {
  clearTimeout(quoteTimer);
  quoteTimer = setTimeout(() => void refreshQuote(), 120);
}

async function submit() {
  error.value = "";
  if (!item.value) {
    error.value = t("product.errNotLoaded");
    return;
  }
  if (!isValidCheckoutContact(email.value)) {
    error.value = t("product.errEmail");
    return;
  }
  if ((item.value.variants?.length || 0) > 0 && !selectedVariantID.value) {
    error.value = t("product.errVariant");
    return;
  }
  if (currentStock.value < quantity.value) {
    error.value = t("product.errStock");
    return;
  }
  const inputPayload = orderInputPayload();
  if (!inputPayload) return;
  submitting.value = true;
  try {
    if (!selectedChannel.value) {
      error.value = t("product.errNoChannel");
      return;
    }
    if (!(await refreshQuote(true))) return;
    order.value = await createOrder({
      product_id: item.value.product.id,
      ...(selectedVariantID.value
        ? { variant_id: selectedVariantID.value }
        : {}),
      quantity: quantity.value,
      contact: email.value,
      payment_method: selectedChannel.value,
      currency: selectedCurrency.value,
      ...(couponCode.value.trim()
        ? { coupon_code: couponCode.value.trim() }
        : {}),
      ...(inputPayload.length ? { input_values: inputPayload } : {}),
    });
    Object.keys(inputValues).forEach((key) => (inputValues[key] = ""));
    if (order.value.status === "pending_payment") {
      const payment = await createPayment(
        order.value.order_no,
        email.value,
        selectedChannel.value,
      );
      if (payment.intent.checkout_url) {
        const checkoutURL = safeNavigationURL(payment.intent.checkout_url);
        if (checkoutURL) window.location.assign(checkoutURL);
        else
          error.value = t("product.errOrderCreated", {
            no: order.value.order_no,
          });
      } else if (order.value.lookup_token) {
        try {
          order.value = await queryOrder(
            order.value.order_no,
            order.value.lookup_token,
          );
        } catch {
          error.value = t("product.errOrderCreated", {
            no: order.value.order_no,
          });
        }
      }
    }
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("product.errCreate");
  } finally {
    submitting.value = false;
  }
}

async function copy(text: string) {
  await navigator.clipboard.writeText(text);
  copied.value = true;
  setTimeout(() => (copied.value = false), 1500);
}
async function addToCart() {
  error.value = "";
  if (!item.value) return;
  if ((item.value.variants?.length || 0) > 0 && !selectedVariantID.value) {
    error.value = t("product.errVariant");
    return;
  }
  try {
    await upsertCartItem(
      item.value.product.id,
      quantity.value,
      selectedVariantID.value || undefined,
      selectedCurrency.value,
    );
    cartAdded.value = true;
    setTimeout(() => (cartAdded.value = false), 1800);
  } catch (reason: any) {
    error.value =
      reason?.response?.status === 409
        ? t("money.cartCurrencyChanged")
        : reason?.response?.data?.message || t("product.errAddCart");
  }
}
async function loadSelectedCurrencyProduct() {
  productReady.value = false;
  quote.value = null;
  error.value = "";
  try {
    item.value = await fetchProduct(
      String(route.params.slug),
      selectedCurrency.value,
    );
    activeProductImage.value =
      productArtworkGallery(item.value.product)[0] || "";
    selectedVariantID.value = item.value.variants?.[0]?.id || "";
    for (const field of item.value.input_fields || [])
      inputValues[field.id] = "";
    productReady.value = true;
  } catch {
    error.value = t("product.errUnavailable");
    return;
  }
  try {
    const fetchedChannels = await fetchPaymentChannels(
      item.value?.product.id ? [item.value.product.id] : [],
      selectedCurrency.value,
    );
    channels.value = fetchedChannels;
    selectedChannel.value = channels.value[0]?.code || "";
    if (item.value && !channels.value.length)
      error.value ||= t("product.errChannel");
  } catch {
    error.value ||= t("product.errChannel");
  }
  await refreshQuote();
}

watch(selectedCurrency, () => void loadSelectedCurrencyProduct());
function closeImageLightbox() {
  imageLightboxOpen.value = false;
}
function selectProductImage(image: string) {
  activeProductImage.value = image;
}
function nextProductImage() {
  if (productImages.value.length < 2) return;
  const index = productImages.value.indexOf(activeProductImage.value);
  activeProductImage.value =
    productImages.value[(index + 1) % productImages.value.length];
}
function previousProductImage() {
  if (productImages.value.length < 2) return;
  const index = productImages.value.indexOf(activeProductImage.value);
  activeProductImage.value =
    productImages.value[
      (index - 1 + productImages.value.length) % productImages.value.length
    ];
}
function onImageTouchStart(event: TouchEvent) {
  touchStartX.value = event.changedTouches[0]?.clientX ?? null;
}
function onImageTouchEnd(event: TouchEvent) {
  if (touchStartX.value === null) return;
  const delta =
    (event.changedTouches[0]?.clientX ?? touchStartX.value) - touchStartX.value;
  touchStartX.value = null;
  if (Math.abs(delta) > 40)
    delta < 0 ? nextProductImage() : previousProductImage();
}
function onImageKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") closeImageLightbox();
  if (imageLightboxOpen.value && event.key === "ArrowRight") nextProductImage();
  if (imageLightboxOpen.value && event.key === "ArrowLeft")
    previousProductImage();
}
let carouselTimer: ReturnType<typeof setInterval> | undefined;
onMounted(async () => {
  if (signedIn.value) {
    try {
      const account = await fetchAccountResource();
      email.value = String(account.user?.email || "");
    } catch {
      signedIn.value = false;
    }
  }
  void loadSelectedCurrencyProduct();
  window.addEventListener("keydown", onImageKeydown);
  carouselTimer = setInterval(() => {
    if (!carouselPaused.value && !imageLightboxOpen.value) nextProductImage();
  }, 5000);
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", onImageKeydown);
  if (carouselTimer) clearInterval(carouselTimer);
});

watch([selectedVariantID, quantity, selectedChannel, minimumPurchase], () => {
  if (quantity.value > purchaseLimit.value)
    quantity.value = purchaseLimit.value;
  if (quantity.value < minimumPurchase.value)
    quantity.value = minimumPurchase.value;
  scheduleQuote();
});
</script>

<template>
  <section class="product-page section">
    <div class="container">
      <RouterLink class="back-link" to="/"
        ><ArrowLeft :size="16" /> {{ t("product.backToStore") }}</RouterLink
      >
      <div v-if="!order && productReady && item" class="product-detail">
        <div class="product-media-column">
          <div
            class="product-stage"
            role="button"
            tabindex="0"
            :aria-label="t('product.zoomAria', { name: item.product.name })"
            @click="imageLightboxOpen = true"
            @keydown.enter.prevent="imageLightboxOpen = true"
            @keydown.space.prevent="imageLightboxOpen = true"
            @mouseenter="carouselPaused = true"
            @mouseleave="carouselPaused = false"
            @touchstart="onImageTouchStart"
            @touchend="onImageTouchEnd"
          >
            <img
              :key="activeProductImage"
              :src="activeProductImage"
              :alt="item.product.name"
              decoding="async"
            />
            <span>{{ item.product.category.name }}</span>
            <small>{{ t("product.selectedServiceKicker") }}</small>
            <b class="product-zoom-hint"
              ><ZoomIn :size="16" /> {{ t("product.zoomHint") }}</b
            >
            <button
              v-if="productImages.length > 1"
              class="product-carousel-arrow product-carousel-prev"
              type="button"
              :aria-label="t('product.previousImage')"
              @click.stop="previousProductImage"
            >
              <ChevronLeft :size="22" />
            </button>
            <button
              v-if="productImages.length > 1"
              class="product-carousel-arrow product-carousel-next"
              type="button"
              :aria-label="t('product.nextImage')"
              @click.stop="nextProductImage"
            >
              <ChevronRight :size="22" />
            </button>
            <div v-if="productImages.length > 1" class="product-carousel-dots">
              <button
                v-for="image in productImages"
                :key="image"
                type="button"
                :class="{ active: image === activeProductImage }"
                :aria-label="t('product.switchImage')"
                @click.stop="selectProductImage(image)"
              />
            </div>
          </div>
          <div v-if="productImages.length > 1" class="product-thumbnails">
            <button
              v-for="(image, index) in productImages"
              :key="image"
              type="button"
              :class="{ active: image === activeProductImage }"
              :aria-label="`${item.product.name} ${index + 1}`"
              @click="selectProductImage(image)"
            >
              <img :src="image" alt="" loading="lazy" decoding="async" />
            </button>
          </div>
        </div>
        <div class="product-info">
          <div class="eyebrow">
            <span></span>{{ item.product.category.name }}
          </div>
          <h1>{{ item.product.name }}</h1>
          <p class="lead">{{ item.product.summary }}</p>
          <div class="detail-price">
            <strong>{{ money(displayPrice) }}</strong
            ><del v-if="displayComparePrice > displayPrice">{{
              money(displayComparePrice)
            }}</del>
          </div>
          <div class="availability">
            <i></i> {{ t("product.stockLine", { currentStock }) }}
          </div>
          <div class="description">
            <h3>{{ t("product.description") }}</h3>
            <div
              v-if="safeProductDescription"
              class="description-rich"
              v-html="safeProductDescription"
            ></div>
            <ul>
              <li><Check :size="16" /> {{ t("product.featureAutoSend") }}</li>
              <li><Check :size="16" /> {{ t("product.featureAfterSale") }}</li>
              <li><Check :size="16" /> {{ t("product.featureEncrypted") }}</li>
            </ul>
          </div>
        </div>
        <aside class="checkout-card">
          <div>
            <span class="kicker">{{ t("product.checkoutKicker") }}</span>
            <h2>{{ t("product.confirmPurchase") }}</h2>
          </div>
          <div v-if="item.variants?.length" class="product-choice-section">
            <span class="choice-label">{{ t("product.variant") }}</span>
            <div class="product-choice-grid">
              <button
                v-for="variant in item.variants"
                :key="variant.id"
                type="button"
                :disabled="variant.stock < 1"
                :class="{ active: selectedVariantID === variant.id }"
                @click="selectedVariantID = variant.id"
              >
                <span>{{ variant.name }}</span>
                <small
                  >{{ money(variant.price) }} ·
                  {{ t("product.stock", { stock: variant.stock }) }}</small
                >
                <Check v-if="selectedVariantID === variant.id" :size="15" />
              </button>
            </div>
          </div>
          <label
            v-for="field in item.input_fields || []"
            :key="field.id"
            class="custom-input-field"
            >{{ field.label }}<em v-if="field.required">*</em>
            <select
              v-if="field.input_type === 'select'"
              v-model="inputValues[field.id]"
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
              v-model="inputValues[field.id]"
              :required="field.required"
              :minlength="field.min_length"
              :maxlength="field.max_length"
              :placeholder="field.placeholder"
              :autocomplete="field.sensitive ? 'off' : 'on'"
              rows="3"
            ></textarea>
            <input
              v-else
              v-model="inputValues[field.id]"
              :type="
                field.sensitive
                  ? 'password'
                  : field.input_type === 'email'
                    ? 'email'
                    : 'text'
              "
              :inputmode="field.input_type === 'number' ? 'decimal' : undefined"
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
          <label class="contact-field"
            >{{ t("product.email")
            }}<input
              v-model="email"
              type="text"
              minlength="8"
              maxlength="190"
              @blur="refreshQuote()"
              placeholder="a2456836 / 86256hfikg"
              :readonly="signedIn"
              :class="{ 'contact-locked': signedIn }"
            /><small v-if="signedIn" class="contact-locked-hint">{{
              t("product.signedInContactLocked")
            }}</small></label
          >
          <div class="product-choice-section">
            <span class="choice-label">{{ t("product.paymentChannel") }}</span>
            <div class="product-choice-grid">
              <button
                v-for="channel in channels"
                :key="channel.code"
                type="button"
                :class="{ active: selectedChannel === channel.code }"
                @click="selectedChannel = channel.code"
              >
                <WalletCards :size="16" />
                <span>{{ channel.name }}</span>
                <small v-if="channel.fee_rate">{{
                  t("product.fee", {
                    rate: (channel.fee_rate / 100).toFixed(2),
                  })
                }}</small>
                <Check v-if="selectedChannel === channel.code" :size="15" />
              </button>
            </div>
          </div>
          <label
            >{{ t("product.quantity") }}
            <div class="stepper">
              <button
                :disabled="quantity <= minimumPurchase"
                @click="quantity = Math.max(minimumPurchase, quantity - 1)"
              >
                <Minus :size="16" /></button
              ><span>{{ quantity }}</span
              ><button
                :disabled="quantity >= purchaseLimit"
                @click="quantity = Math.min(purchaseLimit, quantity + 1)"
              >
                <Plus :size="16" />
              </button></div
          ></label>
          <label
            >{{ t("product.coupon") }}
            <div class="coupon-row">
              <input
                v-model.trim="couponCode"
                maxlength="80"
                :placeholder="t('product.couponPlaceholder')"
                @keyup.enter="refreshQuote(true)"
              />
              <button
                type="button"
                class="button secondary"
                :disabled="quoteLoading || !couponCode"
                @click="refreshQuote(true)"
              >
                {{
                  quoteLoading ? t("product.calculating") : t("product.apply")
                }}
              </button>
            </div>
          </label>
          <div class="checkout-row">
            <span>{{ t("product.subtotal") }}</span
            ><strong>{{
              money(quote?.subtotal ?? displayPrice * quantity)
            }}</strong>
          </div>
          <div v-if="quote?.discount" class="checkout-row discount-row">
            <span>{{ t("product.discount") }}</span
            ><strong>{{ money(-quote.discount) }}</strong>
          </div>
          <div v-if="quote?.fee" class="checkout-row">
            <span>{{ t("product.feeLabel") }}</span
            ><strong>{{ money(quote.fee) }}</strong>
          </div>
          <div v-if="quote?.adjustments.length" class="price-adjustments">
            <small
              v-for="adjustment in quote.adjustments"
              :key="adjustment.code"
            >
              <span>{{ adjustment.label }}</span>
              <b>{{ money(adjustment.amount) }}</b>
            </small>
          </div>
          <div class="checkout-row total">
            <span>{{ t("product.total") }}</span
            ><strong>{{
              money(quote?.total ?? displayPrice * quantity)
            }}</strong>
          </div>
          <p v-if="error" class="form-error">{{ error }}</p>
          <button
            class="button primary wide"
            :disabled="submitting || quoteLoading || currentStock < quantity"
            @click="submit"
          >
            <span v-if="submitting">{{ t("product.creating") }}</span
            ><template v-else
              >{{ t("product.payAndDeliver") }} <Zap :size="16"
            /></template>
          </button>
          <button class="button secondary wide" @click="addToCart">
            <ShoppingCart :size="16" />{{
              cartAdded ? t("product.addedToCart") : t("product.addToCart")
            }}
          </button>
          <p class="secure-note">
            <ShieldCheck :size="14" />
            {{ t("product.verificationNote") }}
          </p>
        </aside>
      </div>
      <div v-else-if="order" class="success-panel">
        <div class="success-icon"><Check /></div>
        <span class="kicker">{{
          order.status === "delivered"
            ? t("product.deliveredKicker")
            : t("product.paymentPendingKicker")
        }}</span>
        <h1>
          {{
            order.status === "delivered"
              ? t("product.delivered")
              : t("product.awaitingPayment")
          }}
        </h1>
        <p>
          {{ t("product.orderNo") }} <b>{{ order.order_no }}</b
          >{{
            order.status === "delivered"
              ? t("product.orderDeliveredTo", { email: order.email })
              : t("product.orderReserved")
          }}
        </p>
        <div v-if="order.status === 'delivered'" class="credential-list">
          <div v-for="entry in order.items" :key="entry.id">
            <span>{{ entry.product_name }}</span
            ><code>{{ entry.card_content }}</code
            ><button @click="copy(entry.card_content)">
              <Check v-if="copied" :size="16" /><Copy v-else :size="16" />
            </button>
          </div>
        </div>
        <RouterLink class="button secondary" to="/orders">{{
          t("product.goLookup")
        }}</RouterLink>
      </div>
      <div v-else class="empty">
        <strong>{{ t("product.errDisplay") }}</strong
        ><span>{{ error }}</span>
      </div>
    </div>
  </section>
  <Teleport to="body">
    <div
      v-if="imageLightboxOpen && activeProductImage"
      class="product-lightbox"
      role="dialog"
      aria-modal="true"
      :aria-label="item?.product.name"
      @click.self="closeImageLightbox"
    >
      <button
        class="product-lightbox-close"
        type="button"
        :aria-label="t('product.closeImage')"
        @click="closeImageLightbox"
      >
        <X :size="24" />
      </button>
      <img :src="activeProductImage" :alt="item?.product.name || ''" />
      <div v-if="productImages.length > 1" class="product-lightbox-thumbnails">
        <button
          v-for="image in productImages"
          :key="image"
          type="button"
          :class="{ active: image === activeProductImage }"
          @click="selectProductImage(image)"
        >
          <img :src="image" alt="" />
        </button>
      </div>
    </div>
  </Teleport>
</template>
