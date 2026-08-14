<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { RouterLink } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  ArrowLeft,
  Minus,
  Plus,
  ShieldCheck,
  ShoppingBag,
  Trash2,
  Zap,
} from "@lucide/vue";
import { fetchCart, removeCartItem, upsertCartItem } from "../api";
import type { CartItem } from "../types";
import {
  formatMinor,
  normalizeCurrency,
  selectedCurrency,
} from "../utils/money";
import { productArtwork } from "../utils/brandAssets";

const { t, locale } = useI18n();

const items = ref<CartItem[]>([]);
const loading = ref(true);
const error = ref("");
const cartCurrency = ref("");
const currencyMismatch = computed(
  () =>
    Boolean(cartCurrency.value && selectedCurrency.value) &&
    normalizeCurrency(cartCurrency.value) !==
      normalizeCurrency(selectedCurrency.value),
);
const subtotal = computed(() =>
  items.value.reduce((sum, item) => sum + (item.quote?.total || 0), 0),
);
const money = (value: number) => formatMinor(value, cartCurrency.value, locale);

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const cart = await fetchCart(selectedCurrency.value);
    items.value = cart.items;
    cartCurrency.value = normalizeCurrency(cart.currency);
    if (currencyMismatch.value) error.value = t("money.cartCurrencyChanged");
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("cart.errLoad");
  } finally {
    loading.value = false;
  }
}
async function change(item: CartItem, delta: number) {
  const next = Math.max(
    1,
    Math.min(item.stock || item.quantity, item.quantity + delta),
  );
  if (next === item.quantity) return;
  const previous = item.quantity;
  item.quantity = next;
  try {
    const cart = await upsertCartItem(
      item.product_id,
      next,
      item.variant_id || undefined,
      selectedCurrency.value,
    );
    items.value = cart.items;
    cartCurrency.value = normalizeCurrency(cart.currency);
    if (currencyMismatch.value) error.value = t("money.cartCurrencyChanged");
  } catch (reason: any) {
    item.quantity = previous;
    error.value =
      reason?.response?.status === 409
        ? t("money.cartCurrencyChanged")
        : reason?.response?.data?.message || t("cart.errUpdate");
  }
}
async function remove(index: number) {
  const item = items.value[index];
  try {
    await removeCartItem(item.product_id, item.variant_id || undefined);
    items.value.splice(index, 1);
  } catch {
    error.value = t("cart.errRemove");
  }
}
async function clearForSelectedCurrency() {
  if (!window.confirm(t("money.clearCartConfirm"))) return;
  loading.value = true;
  try {
    await Promise.all(
      items.value.map((item) =>
        removeCartItem(item.product_id, item.variant_id || undefined),
      ),
    );
    await load();
  } catch {
    error.value = t("cart.errRemove");
  } finally {
    loading.value = false;
  }
}
onMounted(load);
watch(selectedCurrency, () => void load());
</script>

<template>
  <section class="section cart-page">
    <div class="container">
      <RouterLink class="back-link" to="/"
        ><ArrowLeft :size="16" />{{ t("cart.continueShopping") }}</RouterLink
      >
      <div class="page-hero-row">
        <div>
          <span class="kicker">{{ t("kicker.shoppingCart") }}</span>
          <h1>{{ t("cart.title") }}</h1>
          <p>{{ t("cart.subtitle") }}</p>
        </div>
        <span>{{ t("cart.itemsCount", { n: items.length }) }}</span>
      </div>
      <p v-if="error" class="form-error">{{ error }}</p>
      <button
        v-if="currencyMismatch"
        class="button secondary"
        type="button"
        @click="clearForSelectedCurrency"
      >
        {{ t("money.clearCartAndSwitch") }}
      </button>
      <div class="cart-layout">
        <div class="cart-list">
          <p v-if="loading" class="empty">{{ t("cart.syncing") }}</p>
          <article v-for="(item, index) in items" :key="item.id">
            <div class="mini-product">
              <img
                :src="productArtwork(item.product)"
                :alt="item.product?.name || ''"
              />
            </div>
            <div class="cart-copy">
              <span>{{
                item.product?.category.name || t("cart.unavailable")
              }}</span>
              <h3>{{ item.product?.name || item.product_id }}</h3>
              <p>
                {{
                  item.variant
                    ? t("cart.variant", { name: item.variant.name })
                    : item.product?.summary
                }}
              </p>
              <b :class="{ 'stock-unavailable': !item.available }">
                {{
                  item.available
                    ? t("cart.available")
                    : t("cart.unavailableShort")
                }}
                · {{ t("cart.stock", { stock: item.stock || 0 }) }}
              </b>
            </div>
            <div class="cart-price">
              <strong>{{ money(item.quote?.unit_price || 0) }}</strong>
              <div class="stepper mini">
                <button @click="change(item, -1)"><Minus /></button
                ><span>{{ item.quantity }}</span
                ><button :disabled="!item.available" @click="change(item, 1)">
                  <Plus />
                </button>
              </div>
            </div>
            <button class="remove" @click="remove(index)">
              <Trash2 :size="16" />
            </button>
          </article>
          <div v-if="!loading && !items.length" class="empty">
            <ShoppingBag /><strong>{{ t("cart.empty") }}</strong
            ><RouterLink class="button secondary" to="/">{{
              t("cart.browse")
            }}</RouterLink>
          </div>
        </div>
        <aside class="checkout-card">
          <span class="kicker">{{ t("kicker.orderSummary") }}</span>
          <h2>{{ t("cart.summary") }}</h2>
          <div class="checkout-row">
            <span>{{ t("cart.kindsLabel") }}</span
            ><b>{{ t("cart.kinds", { n: items.length }) }}</b>
          </div>
          <div class="checkout-row">
            <span>{{ t("cart.countLabel") }}</span
            ><b>{{
              t("cart.count", {
                n: items.reduce((sum, item) => sum + item.quantity, 0),
              })
            }}</b>
          </div>
          <div class="checkout-row total">
            <span>{{ t("cart.referenceTotal") }}</span
            ><strong>{{ money(subtotal) }}</strong>
          </div>
          <RouterLink
            :class="[
              'button',
              'primary',
              'wide',
              {
                disabled:
                  currencyMismatch ||
                  !items.length ||
                  items.some((item) => !item.available),
              },
            ]"
            :to="
              !currencyMismatch &&
              items.length &&
              items.every((item) => item.available)
                ? '/checkout'
                : '/cart'
            "
            >{{ t("cart.checkout") }} <Zap :size="16"
          /></RouterLink>
          <p class="secure-note">
            <ShieldCheck :size="14" />{{ t("cart.secureNote") }}
          </p>
        </aside>
      </div>
    </div>
  </section>
</template>
