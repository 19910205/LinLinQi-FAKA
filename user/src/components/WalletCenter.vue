<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  ArrowUpRight,
  CircleDollarSign,
  CreditCard,
  RefreshCw,
  WalletCards,
} from "@lucide/vue";
import {
  createRecharge,
  fetchPaymentChannels,
  fetchRecharges,
  fetchStoreConfig,
  fetchWallet,
} from "../api";
import type { RechargeOrder, WalletData } from "../types";
import {
  formatMinor,
  minorToMajorInput,
  minorUnit,
  normalizeCurrency,
  parseMajorToMinor,
} from "../utils/money";
import { safeNavigationURL } from "../utils/publicUrl";

const { locale, t } = useI18n();
const wallet = ref<WalletData | null>(null);
const recharges = ref<RechargeOrder[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = 20;
const channels = ref<
  Array<{
    id: string;
    name: string;
    code: string;
    fee_rate: number;
    supported_currencies: string[];
    settlement_currency: string;
  }>
>([]);
const amount = ref("");
const channel = ref("");
const loading = ref(false);
const submitting = ref(false);
const error = ref("");
const notice = ref("");
const storeCurrency = ref("CNY");
const draftStorageKey = "linlinqi-wallet-recharge-draft";

const pageCount = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize)),
);
const available = computed(
  () =>
    (wallet.value?.account.balance || 0) - (wallet.value?.account.frozen || 0),
);

function money(value = 0) {
  return formatMinor(value, wallet.value?.account.currency, locale);
}

function date(value?: string | null) {
  if (!value) return "—";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime())
    ? "—"
    : parsed.toLocaleString(locale.value, { hour12: false });
}

function statusLabel(status: string) {
  return (
    {
      creating: t("wallet.statusCreating"),
      pending: t("wallet.statusPending"),
      succeeded: t("wallet.statusSucceeded"),
      failed: t("wallet.statusFailed"),
      expired: t("wallet.statusExpired"),
      cancelled: t("wallet.statusCancelled"),
    }[status] || status
  );
}

function parseAmount() {
  return parseMajorToMinor(amount.value, wallet.value?.account.currency) || 0;
}

function newIdempotencyKey() {
  const bytes = crypto.getRandomValues(new Uint8Array(24));
  return `recharge-${Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
}

function restoreDraft() {
  try {
    const draft = JSON.parse(sessionStorage.getItem(draftStorageKey) || "null");
    if (
      draft &&
      typeof draft.key === "string" &&
      typeof draft.amount === "number" &&
      typeof draft.channel === "string" &&
      draft.currency === storeCurrency.value
    ) {
      amount.value = minorToMajorInput(
        draft.amount,
        wallet.value?.account.currency,
      );
      channel.value = draft.channel;
    }
  } catch {
    sessionStorage.removeItem(draftStorageKey);
  }
}

async function load(targetPage = page.value) {
  loading.value = true;
  error.value = "";
  try {
    const config = await fetchStoreConfig();
    storeCurrency.value = normalizeCurrency(config.currency);
    const [walletResult, rechargeResult, channelResult] = await Promise.all([
      fetchWallet(storeCurrency.value),
      fetchRecharges(targetPage, pageSize),
      fetchPaymentChannels([]),
    ]);
    wallet.value = walletResult;
    recharges.value = rechargeResult.items;
    total.value = rechargeResult.total;
    page.value = rechargeResult.page;
    channels.value = channelResult.filter((c: any) => c.code !== "balance");
    if (!channel.value && channels.value.length)
      channel.value = channels.value[0].code;
    if (
      channel.value &&
      !channels.value.some((item) => item.code === channel.value)
    )
      channel.value = channels.value[0]?.code || "";
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("wallet.errLoad");
  } finally {
    loading.value = false;
  }
}

function openCheckout(target: string) {
  const checkoutURL = safeNavigationURL(target);
  if (checkoutURL) location.assign(checkoutURL);
  else error.value = t("wallet.errCheckoutUrl");
}

async function submitRecharge() {
  error.value = "";
  notice.value = "";
  const cents = parseAmount();
  if (cents < 1 || !channel.value || !wallet.value?.account.currency) {
    error.value = t("wallet.errAmount");
    return;
  }
  let draft: {
    key: string;
    amount: number;
    channel: string;
    currency: string;
  } | null = null;
  try {
    const stored = JSON.parse(
      sessionStorage.getItem(draftStorageKey) || "null",
    );
    if (
      stored?.amount === cents &&
      stored?.channel === channel.value &&
      stored?.currency === wallet.value.account.currency &&
      typeof stored?.key === "string"
    ) {
      draft = stored;
    }
  } catch {
    // A corrupt transient draft is replaced below.
  }
  draft ||= {
    key: newIdempotencyKey(),
    amount: cents,
    channel: channel.value,
    currency: wallet.value.account.currency,
  };
  sessionStorage.setItem(draftStorageKey, JSON.stringify(draft));
  submitting.value = true;
  try {
    const result = await createRecharge(
      cents,
      channel.value,
      draft.key,
      wallet.value.account.currency,
    );
    sessionStorage.removeItem(draftStorageKey);
    notice.value = t("wallet.noticeCreated");
    await load(1);
    if (result.recharge.checkout_url)
      openCheckout(result.recharge.checkout_url);
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("wallet.errUncertain");
  } finally {
    submitting.value = false;
  }
}

onMounted(() => {
  restoreDraft();
  void load(1);
});
</script>

<template>
  <div class="wallet-center">
    <p v-if="error" class="form-error" role="alert">{{ error }}</p>
    <p v-if="notice" class="form-notice" role="status">{{ notice }}</p>

    <div class="wallet-overview">
      <article class="balance-card">
        <WalletCards />
        <span>{{ t("wallet.bookBalance") }}</span>
        <strong>{{ money(wallet?.account.balance) }}</strong>
        <small>{{
          t("wallet.availableFrozen", {
            amount: money(available),
            frozen: money(wallet?.account.frozen),
          })
        }}</small>
      </article>
      <form class="recharge-card" @submit.prevent="submitRecharge">
        <div>
          <span>{{ t("wallet.rechargeTitle") }}</span>
          <h2>{{ t("wallet.rechargeDesc") }}</h2>
          <p>{{ t("wallet.rechargeHint") }}</p>
        </div>
        <label>
          {{ t("wallet.amountLabel") }}
          <div class="amount-input">
            <CircleDollarSign /><input
              v-model="amount"
              inputmode="decimal"
              autocomplete="off"
              maxlength="12"
              :placeholder="
                minorToMajorInput(
                  10 ** minorUnit(wallet?.account.currency),
                  wallet?.account.currency,
                )
              "
            />
          </div>
        </label>
        <label>
          {{ t("wallet.channelLabel") }}
          <select v-model="channel">
            <option v-for="item in channels" :key="item.id" :value="item.code">
              {{ item.name
              }}{{
                item.fee_rate
                  ? ` ${t("wallet.feeRate", { rate: (item.fee_rate / 100).toLocaleString(locale) })}`
                  : ""
              }}
            </option>
          </select>
        </label>
        <button
          class="button primary"
          :disabled="submitting || !channels.length"
        >
          <CreditCard />{{
            submitting ? t("wallet.creating") : t("wallet.createRecharge")
          }}
        </button>
      </form>
    </div>

    <section class="account-panel wallet-ledger">
      <header>
        <div>
          <span>{{ t("kicker.walletLedger") }}</span>
          <h2>{{ t("wallet.ledgerTitle") }}</h2>
        </div>
        <small>{{
          t("wallet.recentCount", { n: wallet?.entries.length || 0 })
        }}</small>
      </header>
      <div class="ledger-list">
        <article v-for="entry in wallet?.entries || []" :key="entry.id">
          <i :class="{ in: entry.amount > 0 }">{{
            entry.amount > 0 ? "+" : "−"
          }}</i>
          <div>
            <b>{{ entry.description || entry.type }}</b>
            <small
              >{{ date(entry.created_at) }} · {{ entry.entry_no
              }}<template v-if="entry.reference_type">
                · {{ entry.reference_type }}</template
              ></small
            >
          </div>
          <span>
            <strong :class="{ credit: entry.amount > 0 }"
              >{{ entry.amount > 0 ? "+" : ""
              }}{{ money(Math.abs(entry.amount)) }}</strong
            >
            <small>{{
              t("wallet.balanceAfter", { amount: money(entry.balance_after) })
            }}</small>
          </span>
        </article>
        <p v-if="!loading && !(wallet?.entries.length || 0)">
          {{ t("wallet.noLedger") }}
        </p>
      </div>
    </section>

    <section class="account-panel recharge-history">
      <header>
        <div>
          <span>{{ t("kicker.rechargeHistory") }}</span>
          <h2>{{ t("wallet.historyTitle") }}</h2>
        </div>
        <button type="button" :disabled="loading" @click="load(page)">
          <RefreshCw />{{
            loading ? t("wallet.refreshing") : t("wallet.refresh")
          }}
        </button>
      </header>
      <div class="recharge-list">
        <article v-for="item in recharges" :key="item.id">
          <div>
            <b>{{ item.recharge_no }}</b>
            <small
              >{{ date(item.created_at) }} ·
              {{ item.channel_name || item.channel_code }} ·
              {{ formatMinor(item.amount, item.currency, locale) }} →
              {{
                formatMinor(item.credit_amount, item.credit_currency, locale)
              }}</small
            >
          </div>
          <strong>{{
            formatMinor(
              item.credit_amount + item.bonus,
              item.credit_currency,
              locale,
            )
          }}</strong>
          <em :class="`state-${item.status}`">{{
            statusLabel(item.status)
          }}</em>
          <button
            v-if="item.status === 'pending' && item.checkout_url"
            type="button"
            @click="openCheckout(item.checkout_url)"
          >
            {{ t("wallet.continuePay") }}<ArrowUpRight />
          </button>
        </article>
        <p v-if="!loading && !recharges.length">{{ t("wallet.noHistory") }}</p>
      </div>
      <nav v-if="total > pageSize">
        <button
          type="button"
          :disabled="loading || page <= 1"
          @click="load(page - 1)"
        >
          {{ t("wallet.prevPage") }}
        </button>
        <span>{{ t("wallet.pageInfo", { page, total: pageCount }) }}</span>
        <button
          type="button"
          :disabled="loading || page >= pageCount"
          @click="load(page + 1)"
        >
          {{ t("wallet.nextPage") }}
        </button>
      </nav>
    </section>
  </div>
</template>

<style scoped>
.wallet-center {
  display: grid;
  gap: 18px;
}
.wallet-overview {
  display: grid;
  grid-template-columns: minmax(260px, 0.8fr) minmax(360px, 1.2fr);
  gap: 18px;
}
.balance-card,
.recharge-card {
  border: 1px solid var(--line);
  border-radius: 12px;
  padding: 24px;
  background: var(--surface);
}
.balance-card {
  display: flex;
  flex-direction: column;
  justify-content: center;
  min-height: 250px;
  background: var(--inverse);
  color: var(--inverse-text);
}
.balance-card > svg {
  width: 32px;
  height: 32px;
  margin-bottom: 28px;
}
.balance-card span,
.balance-card small {
  color: color-mix(in srgb, currentColor 66%, transparent);
}
.balance-card strong {
  font-size: clamp(34px, 5vw, 58px);
  letter-spacing: -0.05em;
  margin: 8px 0;
}
.recharge-card {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}
.recharge-card > div:first-child {
  grid-column: 1 / -1;
}
.recharge-card h2,
.recharge-card p {
  margin: 5px 0;
}
.recharge-card p,
.recharge-card label {
  color: var(--muted);
  font-size: 12px;
}
.recharge-card label {
  display: grid;
  gap: 7px;
}
.recharge-card input,
.recharge-card select {
  width: 100%;
  height: 44px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--bg);
  color: var(--text);
  padding: 0 12px;
}
.amount-input {
  position: relative;
}
.amount-input svg {
  position: absolute;
  left: 11px;
  top: 12px;
  width: 18px;
}
.amount-input input {
  padding-left: 38px;
}
.recharge-card .button {
  grid-column: 1 / -1;
  justify-content: center;
}
.wallet-ledger > header,
.recharge-history > header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.wallet-ledger header > small {
  color: var(--muted);
  font-size: 11px;
}
.wallet-ledger h2,
.wallet-ledger header span,
.recharge-history h2,
.recharge-history header span {
  margin: 3px 0;
}
.wallet-ledger header span,
.recharge-history header span {
  color: var(--muted);
  font-size: 10px;
  letter-spacing: 0.12em;
}
.ledger-list {
  display: grid;
}
.ledger-list article {
  display: grid;
  grid-template-columns: 32px minmax(220px, 1fr) auto;
  gap: 12px;
  align-items: center;
  padding: 14px 0;
  border-top: 1px solid var(--line);
}
.ledger-list article > i {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: var(--soft);
  color: var(--muted);
  font-style: normal;
  font-weight: 700;
}
.ledger-list article > i.in,
.ledger-list strong.credit {
  color: var(--success);
}
.ledger-list article > div,
.ledger-list article > span {
  display: grid;
  gap: 5px;
}
.ledger-list article > span {
  justify-items: end;
  text-align: right;
}
.ledger-list small {
  color: var(--muted);
  font-size: 10px;
  word-break: break-word;
}
.recharge-history header button,
.recharge-history nav button,
.recharge-list article > button {
  border: 1px solid var(--line);
  background: var(--surface);
  color: var(--text);
  border-radius: 6px;
  padding: 8px 10px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.recharge-history button svg {
  width: 15px;
  height: 15px;
}
.recharge-list {
  display: grid;
}
.recharge-list article {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) 120px 90px auto;
  gap: 14px;
  align-items: center;
  padding: 15px 0;
  border-top: 1px solid var(--line);
}
.recharge-list article div {
  display: grid;
  gap: 5px;
}
.recharge-list small {
  color: var(--muted);
}
.recharge-list em {
  font-style: normal;
  font-size: 11px;
}
.state-succeeded {
  color: var(--success);
}
.state-failed,
.state-cancelled {
  color: #b42318;
}
.state-pending,
.state-creating,
.state-expired {
  color: var(--muted);
}
.recharge-history nav {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 12px;
  margin-top: 18px;
  font-size: 12px;
}
@media (max-width: 760px) {
  .wallet-overview {
    grid-template-columns: 1fr;
  }
  .recharge-card {
    grid-template-columns: 1fr;
  }
  .recharge-list article {
    grid-template-columns: 1fr auto;
  }
  .ledger-list article {
    grid-template-columns: 32px minmax(0, 1fr);
  }
  .ledger-list article > span {
    grid-column: 2;
    justify-items: start;
    text-align: left;
  }
  .recharge-list article em,
  .recharge-list article > button {
    justify-self: start;
  }
}
</style>
