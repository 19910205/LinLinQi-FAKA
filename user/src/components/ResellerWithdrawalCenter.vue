<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Banknote, RefreshCw, Send } from "@lucide/vue";
import { fetchResellerWithdrawals, requestResellerWithdrawal } from "../api";
import type { ResellerWithdrawal } from "../types";
import {
  formatMinor,
  minorToMajorInput,
  parseMajorToMinor,
} from "../utils/money";

const props = defineProps<{
  active: boolean;
  balance: number;
  frozen: number;
  currency: string;
}>();
const emit = defineEmits<{ changed: [] }>();
const { locale, t } = useI18n();
const items = ref<ResellerWithdrawal[]>([]);
const page = ref(1);
const total = ref(0);
const pageSize = 12;
const amount = ref("");
const method = ref<"alipay" | "bank" | "usdt">("alipay");
const account = ref("");
const loading = ref(false);
const submitting = ref(false);
const error = ref("");
const notice = ref("");
const available = computed(() => Math.max(0, props.balance - props.frozen));
const pageCount = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize)),
);

function money(value = 0) {
  return formatMinor(value, props.currency, locale);
}
function date(value?: string | null) {
  return value
    ? new Date(value).toLocaleString(locale.value, { hour12: false })
    : "—";
}
function parseAmount() {
  return parseMajorToMinor(amount.value, props.currency) || 0;
}
function statusLabel(status: string) {
  return (
    {
      pending: t("withdrawal.statusPending"),
      processing: t("withdrawal.statusProcessing"),
      completed: t("withdrawal.statusCompleted"),
      rejected: t("withdrawal.statusRejected"),
    }[status] || status
  );
}
async function load(targetPage = page.value) {
  loading.value = true;
  error.value = "";
  try {
    const result = await fetchResellerWithdrawals(targetPage, pageSize);
    items.value = result.items;
    total.value = result.total;
    page.value = result.page;
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("withdrawal.errLoad");
  } finally {
    loading.value = false;
  }
}
async function submit() {
  error.value = "";
  notice.value = "";
  const cents = parseAmount();
  const payoutAccount = account.value.trim();
  if (cents < 10_000 || cents > 100_000_000 || cents > available.value) {
    error.value = t("withdrawal.errAmount");
    return;
  }
  if ([...payoutAccount].length < 3 || [...payoutAccount].length > 255) {
    error.value = t("withdrawal.errAccount");
    return;
  }
  submitting.value = true;
  try {
    await requestResellerWithdrawal({
      amount: cents,
      method: method.value,
      account: payoutAccount,
    });
    account.value = "";
    amount.value = "";
    notice.value = t("withdrawal.noticeSubmitted");
    await load(1);
    emit("changed");
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("withdrawal.errSubmit");
  } finally {
    submitting.value = false;
  }
}
onMounted(() => void load(1));
</script>

<template>
  <section class="console-panel withdrawal-center">
    <header>
      <div>
        <span>{{ t("kicker.payoutWorkflow") }}</span>
        <h2>{{ t("withdrawal.title") }}</h2>
        <p>{{ t("withdrawal.desc") }}</p>
      </div>
      <button type="button" :disabled="loading" @click="load(page)">
        <RefreshCw />{{
          loading ? t("withdrawal.refreshing") : t("withdrawal.refresh")
        }}
      </button>
    </header>
    <p v-if="error" class="withdrawal-feedback error" role="alert">
      {{ error }}
    </p>
    <p v-if="notice" class="withdrawal-feedback notice" role="status">
      {{ notice }}
    </p>
    <div class="withdrawal-layout">
      <form @submit.prevent="submit">
        <div class="available">
          <Banknote /><span>{{ t("withdrawal.available") }}</span
          ><strong>{{ money(available) }}</strong>
        </div>
        <label
          >{{ t("withdrawal.amountLabel")
          }}<input
            v-model="amount"
            inputmode="decimal"
            autocomplete="off"
            maxlength="12"
            :placeholder="minorToMajorInput(10_000, currency)"
            :disabled="!active || submitting"
        /></label>
        <label
          >{{ t("withdrawal.methodLabel")
          }}<select v-model="method" :disabled="!active || submitting">
            <option value="alipay">{{ t("withdrawal.alipay") }}</option>
            <option value="bank">{{ t("withdrawal.bank") }}</option>
            <option value="usdt">{{ t("withdrawal.usdt") }}</option>
          </select></label
        >
        <label
          >{{ t("withdrawal.accountLabel")
          }}<input
            v-model="account"
            maxlength="255"
            autocomplete="off"
            spellcheck="false"
            :placeholder="
              method === 'bank'
                ? t('withdrawal.bankPlaceholder')
                : method === 'usdt'
                  ? t('withdrawal.usdtPlaceholder')
                  : t('withdrawal.alipayPlaceholder')
            "
            :disabled="!active || submitting"
        /></label>
        <small>{{ t("withdrawal.accountHint") }}</small>
        <button class="button primary" :disabled="!active || submitting">
          <Send />{{
            submitting
              ? t("withdrawal.submitting")
              : active
                ? t("withdrawal.submit")
                : t("withdrawal.inactive")
          }}
        </button>
      </form>
      <div class="withdrawal-list">
        <article v-for="item in items" :key="item.id">
          <div>
            <b>{{ item.withdrawal_no }}</b
            ><small
              >{{ date(item.created_at) }} · {{ item.method.toUpperCase() }} ·
              {{ item.account_preview }}</small
            >
          </div>
          <strong>{{ money(item.amount) }}</strong>
          <em :class="`state-${item.status}`">{{
            statusLabel(item.status)
          }}</em>
          <small v-if="item.payout_reference">{{
            t("withdrawal.payoutRef", { ref: item.payout_reference })
          }}</small>
          <small v-if="item.reason">{{
            t("withdrawal.reason", { reason: item.reason })
          }}</small>
        </article>
        <p v-if="!loading && !items.length">{{ t("withdrawal.noRecords") }}</p>
        <nav v-if="total > pageSize">
          <button
            type="button"
            :disabled="page <= 1 || loading"
            @click="load(page - 1)"
          >
            {{ t("withdrawal.prevPage") }}</button
          ><span>{{ page }} / {{ pageCount }}</span
          ><button
            type="button"
            :disabled="page >= pageCount || loading"
            @click="load(page + 1)"
          >
            {{ t("withdrawal.nextPage") }}
          </button>
        </nav>
      </div>
    </div>
  </section>
</template>

<style scoped>
.withdrawal-center {
  margin-top: 18px;
}
.withdrawal-center > header {
  display: flex;
  align-items: start;
  justify-content: space-between;
  gap: 16px;
}
.withdrawal-center h2,
.withdrawal-center p {
  margin: 4px 0;
}
.withdrawal-center header span {
  color: var(--muted);
  font-size: 10px;
  letter-spacing: 0.14em;
}
.withdrawal-center header p {
  color: var(--muted);
  font-size: 12px;
}
.withdrawal-center header button,
.withdrawal-list nav button {
  border: 1px solid var(--line);
  background: var(--surface);
  color: var(--text);
  border-radius: 6px;
  padding: 8px 10px;
  display: inline-flex;
  gap: 6px;
  align-items: center;
}
.withdrawal-center button svg {
  width: 16px;
}
.withdrawal-feedback {
  padding: 10px 12px;
  border-radius: 6px;
  font-size: 12px;
}
.withdrawal-feedback.error {
  background: color-mix(in srgb, #b42318 10%, transparent);
  color: #b42318;
}
.withdrawal-feedback.notice {
  background: color-mix(in srgb, var(--success) 10%, transparent);
  color: var(--success);
}
.withdrawal-layout {
  display: grid;
  grid-template-columns: minmax(260px, 0.75fr) minmax(420px, 1.25fr);
  gap: 22px;
  margin-top: 20px;
}
.withdrawal-layout form {
  display: grid;
  gap: 13px;
  align-content: start;
}
.withdrawal-layout label {
  display: grid;
  gap: 7px;
  color: var(--muted);
  font-size: 12px;
}
.withdrawal-layout input,
.withdrawal-layout select {
  height: 42px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--bg);
  color: var(--text);
  padding: 0 11px;
}
.withdrawal-layout form > small {
  color: var(--muted);
  line-height: 1.6;
}
.withdrawal-layout .button {
  justify-content: center;
}
.available {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 4px 10px;
  padding: 15px;
  border-radius: 8px;
  background: var(--soft);
}
.available svg {
  grid-row: 1 / 3;
  align-self: center;
  width: 25px;
}
.available span {
  color: var(--muted);
  font-size: 11px;
}
.available strong {
  font-size: 22px;
}
.withdrawal-list {
  display: grid;
  align-content: start;
}
.withdrawal-list article {
  display: grid;
  grid-template-columns: 1fr auto auto;
  align-items: center;
  gap: 6px 14px;
  border-top: 1px solid var(--line);
  padding: 14px 0;
}
.withdrawal-list article div {
  display: grid;
  gap: 4px;
}
.withdrawal-list article small {
  color: var(--muted);
}
.withdrawal-list article > small {
  grid-column: 1 / -1;
}
.withdrawal-list em {
  font-style: normal;
  font-size: 11px;
}
.state-completed {
  color: var(--success);
}
.state-rejected {
  color: #b42318;
}
.state-pending,
.state-processing {
  color: var(--muted);
}
.withdrawal-list nav {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-top: 12px;
}
@media (max-width: 820px) {
  .withdrawal-layout {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 520px) {
  .withdrawal-center > header {
    display: grid;
  }
  .withdrawal-list article {
    grid-template-columns: 1fr auto;
  }
  .withdrawal-list article em {
    justify-self: start;
  }
}
</style>
