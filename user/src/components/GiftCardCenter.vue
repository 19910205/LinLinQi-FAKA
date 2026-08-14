<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  ArrowDownToLine,
  CalendarClock,
  Gift,
  RefreshCw,
  ShieldCheck,
  WalletCards,
} from "@lucide/vue";
import { fetchGiftCards, redeemGiftCard } from "../api";
import type { GiftCardRecord } from "../types";
import { formatMinor, storeCurrency } from "../utils/money";

const { t, locale } = useI18n();

const cards = ref<GiftCardRecord[]>([]);
const code = ref("");
const loading = ref(false);
const redeeming = ref(false);
const error = ref("");
const success = ref<{
  amount: number;
  walletBalance: number;
  preview: string;
  currency: string;
} | null>(null);

const historyCurrency = computed(() => {
  const currencies = new Set(
    cards.value.map((card) => card.currency).filter(Boolean),
  );
  return currencies.size === 1 ? [...currencies][0] : "";
});
const redeemedTotalLabel = computed(() =>
  historyCurrency.value
    ? money(
        cards.value.reduce((total, card) => total + card.initial_balance, 0),
        historyCurrency.value,
      )
    : cards.value.length
      ? t("money.mixedCurrencies")
      : money(0, storeCurrency.value),
);

function money(value: number, currency?: string) {
  return formatMinor(value, currency, locale);
}

function date(value?: string | null) {
  return value
    ? new Date(value).toLocaleString(locale.value, { hour12: false })
    : "—";
}

function statusLabel(status: string) {
  return ["active", "redeemed", "expired", "disabled"].includes(status)
    ? t(`giftcard.status.${status}`)
    : status;
}

function safeStatusClass(status: string) {
  return ["active", "redeemed", "expired", "disabled"].includes(status)
    ? `status-${status}`
    : "status-other";
}

function requestError(reason: any, fallback: string) {
  return reason?.response?.data?.message || fallback;
}

async function loadCards() {
  loading.value = true;
  error.value = "";
  try {
    cards.value = await fetchGiftCards();
  } catch (reason: any) {
    cards.value = [];
    error.value = requestError(reason, t("giftcard.errLoad"));
  } finally {
    loading.value = false;
  }
}

async function submitRedemption() {
  if (redeeming.value) return;
  error.value = "";
  success.value = null;

  // The full code exists only in this local variable for the duration of the
  // request. Clear the bound input before validation/network activity so it is
  // never retained in rendered state or written to browser storage.
  const submittedCode = code.value;
  code.value = "";
  const compact = submittedCode.replace(/[-\s]/g, "").toUpperCase();
  if (
    compact.length < 30 ||
    compact.length > 80 ||
    !compact.startsWith("LLQ") ||
    !/^[A-Z2-7]+$/.test(compact)
  ) {
    error.value = t("giftcard.errCodeFormat");
    return;
  }

  redeeming.value = true;
  try {
    const result = await redeemGiftCard(submittedCode);
    success.value = {
      amount: Math.abs(result.entry.amount),
      walletBalance: result.wallet_balance,
      preview: result.card.code_preview,
      currency: result.card.currency,
    };
    await loadCards();
  } catch (reason: any) {
    error.value = requestError(reason, t("giftcard.errRedeem"));
  } finally {
    redeeming.value = false;
  }
}

onMounted(loadCards);
</script>

<template>
  <div class="gift-card-center">
    <section class="gift-redeem-hero">
      <div class="gift-redeem-copy">
        <span class="gift-kicker">{{ t("kicker.giftCredit") }}</span>
        <h2>{{ t("giftcard.redeemTitle") }}</h2>
        <p>{{ t("giftcard.redeemSub") }}</p>
        <div class="gift-security-note">
          <ShieldCheck />
          <span>{{ t("giftcard.securityNote") }}</span>
        </div>
      </div>
      <form class="gift-redeem-form" @submit.prevent="submitRedemption">
        <label for="gift-card-code">{{ t("giftcard.codeLabel") }}</label>
        <div>
          <input
            id="gift-card-code"
            v-model="code"
            name="gift-card-code"
            type="password"
            inputmode="text"
            autocomplete="off"
            autocapitalize="characters"
            spellcheck="false"
            maxlength="120"
            :disabled="redeeming"
            placeholder="LLQ-••••••-••••••-••••••"
            aria-describedby="gift-card-hint"
            required
          />
          <button class="button primary" :disabled="redeeming || !code.trim()">
            <ArrowDownToLine />{{
              redeeming ? t("giftcard.redeeming") : t("giftcard.redeemNow")
            }}
          </button>
        </div>
        <small id="gift-card-hint">{{ t("giftcard.codeHint") }}</small>
      </form>
    </section>

    <p v-if="error" class="gift-feedback error" role="alert">{{ error }}</p>
    <section v-if="success" class="gift-success" role="status">
      <span class="gift-success-icon"><WalletCards /></span>
      <div>
        <small>{{
          t("giftcard.redeemed", { preview: success.preview })
        }}</small>
        <b>{{
          t("giftcard.credited", {
            amount: money(success.amount, success.currency),
          })
        }}</b>
        <span>{{
          t("giftcard.balance", {
            balance: money(success.walletBalance, success.currency),
          })
        }}</span>
      </div>
    </section>

    <section class="account-panel gift-history">
      <header>
        <div>
          <span class="gift-kicker">{{ t("kicker.redemptionHistory") }}</span>
          <h2>{{ t("giftcard.history") }}</h2>
          <p>
            {{
              t("giftcard.historySub", {
                n: cards.length,
                total: redeemedTotalLabel,
              })
            }}
          </p>
        </div>
        <button
          type="button"
          :disabled="loading"
          :aria-label="t('giftcard.refresh')"
          @click="loadCards"
        >
          <RefreshCw />{{
            loading ? t("giftcard.loading") : t("giftcard.refresh")
          }}
        </button>
      </header>

      <div v-if="loading" class="gift-empty">
        {{ t("giftcard.loadingList") }}
      </div>
      <div v-else-if="cards.length" class="gift-card-grid">
        <article v-for="card in cards" :key="card.id" class="gift-card-record">
          <header>
            <span><Gift /></span>
            <em :class="safeStatusClass(card.status)">
              {{ statusLabel(card.status) }}
            </em>
          </header>
          <div class="gift-card-value">
            <small>{{ t("giftcard.faceValue") }}</small>
            <strong>{{ money(card.initial_balance, card.currency) }}</strong>
            <code>{{ card.code_preview }}</code>
          </div>
          <dl>
            <div>
              <dt>{{ t("giftcard.redeemedAt") }}</dt>
              <dd>{{ date(card.redeemed_at) }}</dd>
            </div>
            <div>
              <dt>{{ t("giftcard.expiry") }}</dt>
              <dd>
                {{
                  card.expires_at
                    ? date(card.expires_at)
                    : t("giftcard.noExpiry")
                }}
              </dd>
            </div>
            <div>
              <dt>{{ t("giftcard.currency") }}</dt>
              <dd>{{ card.currency || "—" }}</dd>
            </div>
          </dl>
        </article>
      </div>
      <div v-else class="gift-empty">
        <CalendarClock />
        <b>{{ t("giftcard.noRecords") }}</b>
        <span>{{ t("giftcard.noRecordsSub") }}</span>
      </div>
    </section>
  </div>
</template>

<style scoped>
.gift-card-center {
  display: grid;
  gap: 14px;
}
.gift-redeem-hero {
  display: grid;
  grid-template-columns: minmax(230px, 0.85fr) minmax(0, 1.15fr);
  gap: 30px;
  align-items: center;
  border-radius: 9px;
  background: var(--inverse);
  color: var(--inverse-text);
  padding: 26px;
}
.gift-kicker {
  display: block;
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
  letter-spacing: 0.16em;
}
.gift-redeem-hero .gift-kicker {
  color: color-mix(in srgb, var(--inverse-text) 55%, transparent);
}
.gift-redeem-copy h2 {
  margin: 7px 0;
  font-size: 25px;
}
.gift-redeem-copy > p {
  margin: 0;
  color: color-mix(in srgb, var(--inverse-text) 68%, transparent);
  font-size: 9px;
  line-height: 1.7;
}
.gift-security-note {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-top: 15px;
  color: color-mix(in srgb, var(--inverse-text) 68%, transparent);
  font-size: 8px;
}
.gift-security-note svg {
  width: 14px;
  flex: 0 0 auto;
}
.gift-redeem-form {
  display: grid;
  gap: 8px;
  border: 1px solid color-mix(in srgb, var(--inverse-text) 18%, transparent);
  border-radius: 7px;
  background: color-mix(in srgb, var(--inverse-text) 6%, transparent);
  padding: 16px;
}
.gift-redeem-form > label {
  font-size: 9px;
  font-weight: 600;
}
.gift-redeem-form > div {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
}
.gift-redeem-form input {
  min-width: 0;
  height: 41px;
  border: 1px solid color-mix(in srgb, var(--inverse-text) 25%, transparent);
  border-radius: 5px;
  background: var(--surface);
  color: var(--text);
  padding: 0 11px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 10px;
  letter-spacing: 0.08em;
  outline: none;
}
.gift-redeem-form input:focus {
  border-color: var(--inverse-text);
}
.gift-redeem-form .button {
  height: 41px;
  border-color: var(--inverse-text);
  background: var(--inverse-text);
  color: var(--inverse);
}
.gift-redeem-form .button svg {
  width: 13px;
}
.gift-redeem-form > small {
  color: color-mix(in srgb, var(--inverse-text) 58%, transparent);
  font-size: 7px;
}
.gift-feedback {
  margin: 0;
  padding: 11px 13px;
  border: 1px solid color-mix(in srgb, #c03b35 45%, var(--line));
  border-radius: 6px;
  background: var(--surface);
  color: #b8322c;
  font-size: 10px;
}
:global(:root[data-theme="dark"]) .gift-feedback {
  color: #f18d87;
}
.gift-success {
  display: flex;
  align-items: center;
  gap: 14px;
  border: 1px solid color-mix(in srgb, var(--success) 45%, var(--line));
  border-radius: 8px;
  background: color-mix(in srgb, var(--success) 7%, var(--surface));
  padding: 16px;
}
.gift-success-icon {
  width: 38px;
  height: 38px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: var(--success);
  color: var(--surface);
}
.gift-success-icon svg {
  width: 18px;
}
.gift-success > div {
  display: grid;
  gap: 4px;
}
.gift-success small,
.gift-success span {
  color: var(--muted);
  font-size: 8px;
}
.gift-success b {
  color: var(--success);
  font-size: 15px;
}
.gift-history > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--line);
}
.gift-history > header h2 {
  margin: 5px 0;
}
.gift-history > header p {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
}
.gift-history > header button {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 32px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  color: var(--text);
  padding: 0 9px;
  font-size: 8px;
  cursor: pointer;
}
.gift-history > header button:disabled {
  opacity: 0.5;
  cursor: wait;
}
.gift-history > header button svg {
  width: 12px;
}
.gift-card-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 11px;
  padding-top: 16px;
}
.gift-card-record {
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--soft);
  padding: 15px;
}
.gift-card-record > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.gift-card-record > header > span {
  width: 29px;
  height: 29px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: var(--inverse);
  color: var(--inverse-text);
}
.gift-card-record > header svg {
  width: 14px;
}
.gift-card-record > header em {
  border: 1px solid var(--line);
  border-radius: 999px;
  padding: 3px 7px;
  color: var(--muted);
  font-size: 7px;
  font-style: normal;
}
.gift-card-record > header em.status-redeemed {
  border-color: color-mix(in srgb, var(--success) 45%, var(--line));
  color: var(--success);
}
.gift-card-value {
  display: grid;
  gap: 5px;
  padding: 17px 0 13px;
}
.gift-card-value small {
  color: var(--muted);
  font-size: 7px;
}
.gift-card-value strong {
  font-size: 24px;
  letter-spacing: -0.04em;
}
.gift-card-value code {
  overflow-wrap: anywhere;
  color: var(--muted);
  font-size: 8px;
}
.gift-card-record dl {
  display: grid;
  gap: 7px;
  margin: 0;
  padding-top: 12px;
  border-top: 1px solid var(--line);
}
.gift-card-record dl > div {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}
.gift-card-record dt,
.gift-card-record dd {
  margin: 0;
  font-size: 8px;
}
.gift-card-record dt {
  color: var(--muted);
}
.gift-card-record dd {
  text-align: right;
}
.gift-empty {
  min-height: 180px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 7px;
  color: var(--muted);
  font-size: 9px;
  text-align: center;
}
.gift-empty svg {
  width: 23px;
}
.gift-empty b {
  color: var(--text);
  font-size: 10px;
}
@media (max-width: 760px) {
  .gift-redeem-hero {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 540px) {
  .gift-redeem-hero {
    padding: 20px;
  }
  .gift-redeem-form > div,
  .gift-card-grid {
    grid-template-columns: 1fr;
  }
  .gift-redeem-form .button {
    width: 100%;
  }
  .gift-history > header {
    align-items: flex-start;
  }
}
</style>
