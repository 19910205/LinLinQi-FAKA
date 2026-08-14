<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { RouterLink } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  ArrowDownToLine,
  BadgeDollarSign,
  Check,
  CircleAlert,
  Clock3,
  Copy,
  Link2,
  ReceiptText,
  RefreshCw,
  ShieldCheck,
  UserPlus,
  WalletCards,
} from "@lucide/vue";
import {
  applyAffiliate,
  fetchAffiliate,
  requestAffiliateWithdrawal,
} from "../api";
import type {
  AffiliateCommission,
  AffiliateData,
  AffiliateProfile,
  AffiliateWithdrawal,
} from "../types";
import {
  formatMinor,
  minorToMajorInput,
  parseMajorToMinor,
  storeCurrency,
} from "../utils/money";

const { t, locale } = useI18n();

const affiliate = ref<AffiliateData>({
  profile: null,
  commissions: [],
  withdrawals: [],
  balances: [],
  referral_count: 0,
  referral_link: "",
});
const loading = ref(false);
const applying = ref(false);
const withdrawing = ref(false);
const acceptedTerms = ref(false);
const error = ref("");
const notice = ref("");
const copied = ref("");
const withdrawalAmount = ref("");
const withdrawalMethod = ref<"alipay" | "bank" | "usdt">("alipay");
const withdrawalAccount = ref("");
let copyTimer: ReturnType<typeof setTimeout> | undefined;

const profile = computed(() => affiliate.value.profile);
const isActive = computed(() => profile.value?.status === "active");
const netCommissionTotal = computed(() =>
  affiliate.value.commissions.reduce(
    (total, item) =>
      item.currency === (profile.value?.currency || storeCurrency.value)
        ? total + item.commission - item.reversed_amount
        : total,
    0,
  ),
);
const referralLink = computed(() => {
  const code = profile.value?.referral_code;
  if (!code || !isActive.value) return "";
  const fallback = new URL("/auth/register", location.origin);
  fallback.searchParams.set("ref", code);
  const candidate = affiliate.value.referral_link || fallback.toString();
  try {
    const url = new URL(candidate);
    if (
      !["http:", "https:"].includes(url.protocol) ||
      url.username ||
      url.password ||
      url.searchParams.get("ref")?.toUpperCase() !== code.toUpperCase()
    ) {
      return fallback.toString();
    }
    return url.toString();
  } catch {
    return fallback.toString();
  }
});

function money(
  value: number,
  currency = profile.value?.currency || storeCurrency.value,
) {
  return formatMinor(value, currency, locale);
}

function date(value?: string | null) {
  return value
    ? new Date(value).toLocaleString(locale.value, { hour12: false })
    : "—";
}

function requestError(reason: any, fallback: string) {
  return reason?.response?.data?.message || fallback;
}

function profileStatus(profileValue: AffiliateProfile) {
  return ["pending", "active", "suspended", "rejected"].includes(
    profileValue.status,
  )
    ? t(`affiliate.profileStatus.${profileValue.status}`)
    : profileValue.status;
}

function profileStatusMessage(profileValue: AffiliateProfile) {
  return ["pending", "active", "suspended", "rejected"].includes(
    profileValue.status,
  )
    ? t(`affiliate.profileMessages.${profileValue.status}`)
    : t("affiliate.statusUpdated");
}

function commissionStatus(status: string) {
  return ["pending", "available", "partially_reversed", "reversed"].includes(
    status,
  )
    ? t(`affiliate.commissionStatus.${status}`)
    : status;
}

function withdrawalStatus(status: string) {
  return ["pending", "processing", "completed", "rejected"].includes(status)
    ? t(`affiliate.withdrawalStatus.${status}`)
    : status;
}

function methodLabel(method: string) {
  return ["alipay", "bank", "usdt"].includes(method)
    ? t(`affiliate.withdrawalMethods.${method}`)
    : method;
}

function safeStatusClass(status: string) {
  const safe = [
    "pending",
    "active",
    "suspended",
    "rejected",
    "available",
    "partially_reversed",
    "reversed",
    "processing",
    "completed",
  ];
  return safe.includes(status) ? `status-${status}` : "status-other";
}

async function loadAffiliate(clearFeedback = true) {
  loading.value = true;
  if (clearFeedback) {
    error.value = "";
    notice.value = "";
  }
  try {
    affiliate.value = await fetchAffiliate();
  } catch (reason: any) {
    error.value = requestError(reason, t("affiliate.errLoad"));
  } finally {
    loading.value = false;
  }
}

async function submitApplication() {
  if (applying.value) return;
  error.value = "";
  notice.value = "";
  if (!acceptedTerms.value) {
    error.value = t("affiliate.errTerms");
    return;
  }
  applying.value = true;
  try {
    await applyAffiliate();
    await loadAffiliate(false);
    notice.value = t("affiliate.applied");
  } catch (reason: any) {
    error.value = requestError(reason, t("affiliate.errApply"));
  } finally {
    applying.value = false;
  }
}

async function copyValue(value: string, target: string) {
  error.value = "";
  if (!value || !window.isSecureContext || !navigator.clipboard) {
    error.value = t("affiliate.errClipboard");
    return;
  }
  try {
    await navigator.clipboard.writeText(value);
    copied.value = target;
    if (copyTimer) clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      copied.value = "";
    }, 2200);
  } catch {
    error.value = t("affiliate.errCopy");
  }
}

function useMaximumWithdrawal() {
  const available = Math.max(0, profile.value?.available_commission || 0);
  withdrawalAmount.value = minorToMajorInput(
    available,
    profile.value?.currency || storeCurrency.value,
  );
}

async function submitWithdrawal() {
  if (withdrawing.value) return;
  error.value = "";
  notice.value = "";
  const currency = profile.value?.currency || storeCurrency.value;
  const amount = parseMajorToMinor(withdrawalAmount.value, currency);
  if (!amount) {
    error.value = t("affiliate.errAmount");
    return;
  }
  if (amount > (profile.value?.available_commission || 0)) {
    error.value = t("affiliate.errExceed");
    return;
  }
  const account = withdrawalAccount.value.trim();
  withdrawalAccount.value = "";
  if (Array.from(account).length < 3 || Array.from(account).length > 255) {
    error.value = t("affiliate.errAccount");
    return;
  }
  withdrawing.value = true;
  try {
    const result = await requestAffiliateWithdrawal({
      amount,
      method: withdrawalMethod.value,
      account,
      currency,
    });
    withdrawalAmount.value = "";
    await loadAffiliate(false);
    notice.value = t("affiliate.withdrawalSubmitted", {
      no: result.withdrawal_no,
      preview: result.account_preview,
    });
  } catch (reason: any) {
    error.value = requestError(reason, t("affiliate.errWithdrawal"));
  } finally {
    withdrawing.value = false;
  }
}

function commissionNet(item: AffiliateCommission) {
  return item.commission - item.reversed_amount;
}

function withdrawalNet(item: AffiliateWithdrawal) {
  return Math.max(0, item.amount - item.fee);
}

onMounted(loadAffiliate);
onBeforeUnmount(() => {
  if (copyTimer) clearTimeout(copyTimer);
});
</script>

<template>
  <div class="affiliate-center">
    <p v-if="error" class="affiliate-feedback error" role="alert">
      {{ error }}
    </p>
    <p v-if="notice" class="affiliate-feedback notice" role="status">
      {{ notice }}
    </p>

    <section v-if="loading && !profile" class="account-panel affiliate-empty">
      {{ t("affiliate.loading") }}
    </section>

    <section v-else-if="!profile" class="affiliate-application">
      <div class="affiliate-application-copy">
        <span class="affiliate-kicker">{{ t("kicker.affiliateProgram") }}</span>
        <h2>{{ t("affiliate.applyTitle") }}</h2>
        <p>{{ t("affiliate.applySub") }}</p>
        <ul>
          <li><Check />{{ t("affiliate.applyPoint1") }}</li>
          <li><Check />{{ t("affiliate.applyPoint2") }}</li>
          <li><Check />{{ t("affiliate.applyPoint3") }}</li>
        </ul>
      </div>
      <form
        class="affiliate-application-form"
        @submit.prevent="submitApplication"
      >
        <ShieldCheck />
        <h3>{{ t("affiliate.applyCta") }}</h3>
        <p>{{ t("affiliate.applyCtaSub") }}</p>
        <label>
          <input v-model="acceptedTerms" type="checkbox" />
          <span>
            {{ t("affiliate.agreeTerms") }}
            <RouterLink to="/legal/terms">{{
              t("affiliate.termsLink")
            }}</RouterLink>
          </span>
        </label>
        <button class="button primary" :disabled="applying || !acceptedTerms">
          {{ applying ? t("affiliate.submitting") : t("affiliate.submit") }}
        </button>
      </form>
    </section>

    <template v-else-if="profile">
      <section :class="['affiliate-status', safeStatusClass(profile.status)]">
        <Clock3 v-if="profile.status === 'pending'" />
        <CircleAlert v-else-if="profile.status !== 'active'" />
        <ShieldCheck v-else />
        <div>
          <span>{{
            t("affiliate.statusPrefix", { status: profileStatus(profile) })
          }}</span>
          <b>{{ profileStatusMessage(profile) }}</b>
          <small>
            {{
              t("affiliate.appliedAt", {
                date: date(profile.applied_at),
                rate: (profile.commission_basis_point / 100).toFixed(2),
              })
            }}
          </small>
        </div>
        <RouterLink
          v-if="profile.status === 'suspended' || profile.status === 'rejected'"
          to="/account/tickets"
        >
          {{ t("affiliate.contactOps") }}
        </RouterLink>
      </section>

      <section class="affiliate-metrics">
        <article>
          <UserPlus />
          <span>{{ t("affiliate.invites") }}</span>
          <strong>{{ affiliate.referral_count }}</strong>
          <small>{{ t("affiliate.invitesSub") }}</small>
        </article>
        <article>
          <BadgeDollarSign />
          <span>{{ t("affiliate.totalCommission") }}</span>
          <strong>{{ money(profile.total_commission) }}</strong>
          <small>{{
            t("affiliate.netRecord", { amount: money(netCommissionTotal) })
          }}</small>
        </article>
        <article>
          <WalletCards />
          <span>{{ t("affiliate.availableCommission") }}</span>
          <strong>{{ money(profile.available_commission) }}</strong>
          <small>{{ t("affiliate.settled") }}</small>
        </article>
        <article>
          <Clock3 />
          <span>{{ t("affiliate.frozenCommission") }}</span>
          <strong>{{ money(profile.frozen_commission) }}</strong>
          <small>{{ t("affiliate.frozenSub") }}</small>
        </article>
      </section>

      <section class="affiliate-main-grid">
        <article class="account-panel referral-panel">
          <header>
            <div>
              <span class="affiliate-kicker">{{
                t("kicker.yourReferral")
              }}</span>
              <h2>{{ t("affiliate.referral") }}</h2>
            </div>
            <Link2 />
          </header>
          <template v-if="isActive">
            <label>
              {{ t("affiliate.referralCode") }}
              <span class="affiliate-copy-field">
                <code>{{ profile.referral_code }}</code>
                <button
                  type="button"
                  @click="copyValue(profile.referral_code, 'code')"
                >
                  <Copy />{{
                    copied === "code"
                      ? t("affiliate.copied")
                      : t("affiliate.copy")
                  }}
                </button>
              </span>
            </label>
            <label>
              {{ t("affiliate.referralLink") }}
              <span class="affiliate-copy-field link">
                <code>{{ referralLink }}</code>
                <button type="button" @click="copyValue(referralLink, 'link')">
                  <Copy />{{
                    copied === "link"
                      ? t("affiliate.copied")
                      : t("affiliate.copy")
                  }}
                </button>
              </span>
            </label>
            <small>{{ t("affiliate.copyHint") }}</small>
          </template>
          <div v-else class="affiliate-panel-placeholder">
            <Clock3 />
            <p>{{ t("affiliate.reserved") }}</p>
          </div>
        </article>

        <article class="account-panel withdrawal-panel">
          <header>
            <div>
              <span class="affiliate-kicker">{{
                t("kicker.payoutRequest")
              }}</span>
              <h2>{{ t("affiliate.withdrawalTitle") }}</h2>
            </div>
            <ArrowDownToLine />
          </header>
          <form v-if="isActive" @submit.prevent="submitWithdrawal">
            <label>
              {{ t("affiliate.amountLabel") }}
              <span class="withdrawal-amount-field">
                <input
                  v-model="withdrawalAmount"
                  type="text"
                  inputmode="decimal"
                  autocomplete="off"
                  placeholder="0.00"
                  maxlength="10"
                  :disabled="withdrawing"
                />
                <button
                  type="button"
                  :disabled="withdrawing || profile.available_commission <= 0"
                  @click="useMaximumWithdrawal"
                >
                  {{ t("affiliate.all") }}
                </button>
              </span>
            </label>
            <label>
              {{ t("affiliate.methodLabel") }}
              <select v-model="withdrawalMethod" :disabled="withdrawing">
                <option value="alipay">
                  {{ t("affiliate.withdrawalMethods.alipay") }}
                </option>
                <option value="bank">
                  {{ t("affiliate.withdrawalMethods.bank") }}
                </option>
                <option value="usdt">
                  {{ t("affiliate.withdrawalMethods.usdt") }}
                </option>
              </select>
            </label>
            <label>
              {{ t("affiliate.accountLabel") }}
              <input
                v-model="withdrawalAccount"
                type="text"
                autocomplete="off"
                spellcheck="false"
                maxlength="255"
                :disabled="withdrawing"
                :placeholder="
                  withdrawalMethod === 'alipay'
                    ? t('affiliate.alipayPlaceholder')
                    : withdrawalMethod === 'bank'
                      ? t('affiliate.bankPlaceholder')
                      : t('affiliate.usdtPlaceholder')
                "
              />
            </label>
            <small>
              {{
                t("affiliate.withdrawableHint", {
                  amount: money(profile.available_commission),
                })
              }}
            </small>
            <button
              class="button primary"
              :disabled="withdrawing || profile.available_commission <= 0"
            >
              {{
                withdrawing
                  ? t("affiliate.submitting")
                  : t("affiliate.submitWithdrawal")
              }}
            </button>
          </form>
          <div v-else class="affiliate-panel-placeholder">
            <CircleAlert />
            <p>{{ t("affiliate.withdrawalUnavailable") }}</p>
          </div>
        </article>
      </section>

      <section class="account-panel affiliate-records">
        <header>
          <div>
            <span class="affiliate-kicker">{{
              t("kicker.commissionLedger")
            }}</span>
            <h2>{{ t("affiliate.records") }}</h2>
            <p>{{ t("affiliate.recordsSub") }}</p>
          </div>
          <button type="button" :disabled="loading" @click="loadAffiliate()">
            <RefreshCw />{{
              loading ? t("affiliate.refreshing") : t("affiliate.refresh")
            }}
          </button>
        </header>
        <div v-if="affiliate.commissions.length" class="commission-list">
          <article v-for="item in affiliate.commissions" :key="item.id">
            <span class="record-icon"><ReceiptText /></span>
            <div class="record-main">
              <span>{{
                t("affiliate.orderNo", { id: item.order_id.slice(0, 8) })
              }}</span>
              <b>{{
                t("affiliate.netCommission", {
                  amount: money(commissionNet(item), item.currency),
                })
              }}</b>
              <small>{{
                t("affiliate.orderMeta", {
                  amount: money(item.order_amount, item.currency),
                  time: date(item.created_at),
                })
              }}</small>
            </div>
            <div class="record-adjustment">
              <span>{{
                t("affiliate.originalCommission", {
                  amount: money(item.commission, item.currency),
                })
              }}</span>
              <strong v-if="item.reversed_amount > 0">
                {{
                  t("affiliate.reversed", {
                    amount: money(item.reversed_amount, item.currency),
                  })
                }}
              </strong>
            </div>
            <div class="record-state">
              <em :class="safeStatusClass(item.status)">{{
                commissionStatus(item.status)
              }}</em>
              <small>
                {{
                  item.settled_at
                    ? t("affiliate.settledAt", { time: date(item.settled_at) })
                    : t("affiliate.expectedAt", { time: date(item.settles_at) })
                }}
              </small>
            </div>
          </article>
        </div>
        <div v-else class="affiliate-empty">
          {{ t("affiliate.noCommissions") }}
        </div>
      </section>

      <section class="account-panel affiliate-records withdrawals">
        <header>
          <div>
            <span class="affiliate-kicker">{{
              t("kicker.payoutHistory")
            }}</span>
            <h2>{{ t("affiliate.withdrawals") }}</h2>
            <p>{{ t("affiliate.withdrawalsSub") }}</p>
          </div>
        </header>
        <div v-if="affiliate.withdrawals.length" class="withdrawal-list">
          <article v-for="item in affiliate.withdrawals" :key="item.id">
            <span class="record-icon"><ArrowDownToLine /></span>
            <div class="record-main">
              <span>{{ item.withdrawal_no }}</span>
              <b>{{ money(item.amount, item.currency) }}</b>
              <small>
                {{ methodLabel(item.method) }} · {{ item.account_preview }} ·
                {{ date(item.created_at) }}
              </small>
            </div>
            <div class="record-adjustment">
              <span>{{
                t("affiliate.fee", { amount: money(item.fee, item.currency) })
              }}</span>
              <strong>{{
                t("affiliate.expectedArrival", {
                  amount: money(withdrawalNet(item), item.currency),
                })
              }}</strong>
            </div>
            <div class="record-state">
              <em :class="safeStatusClass(item.status)">{{
                withdrawalStatus(item.status)
              }}</em>
              <small v-if="item.processed_at">{{
                t("affiliate.processedAt", { time: date(item.processed_at) })
              }}</small>
              <small v-else>{{ t("affiliate.pendingOps") }}</small>
            </div>
            <p v-if="item.reason" class="withdrawal-reason">
              {{ t("affiliate.reason", { reason: item.reason }) }}
            </p>
          </article>
        </div>
        <div v-else class="affiliate-empty">
          {{ t("affiliate.noWithdrawals") }}
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.affiliate-center {
  display: grid;
  gap: 14px;
}
.affiliate-kicker {
  display: block;
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
  letter-spacing: 0.16em;
}
.affiliate-feedback {
  margin: 0;
  padding: 11px 13px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  font-size: 10px;
}
.affiliate-feedback.error {
  border-color: color-mix(in srgb, #c03b35 45%, var(--line));
  color: #b8322c;
}
:global(:root[data-theme="dark"]) .affiliate-feedback.error {
  color: #f18d87;
}
.affiliate-feedback.notice {
  border-color: color-mix(in srgb, var(--success) 45%, var(--line));
  color: var(--success);
}
.affiliate-application {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(280px, 0.8fr);
  border-radius: 9px;
  overflow: hidden;
  background: var(--inverse);
  color: var(--inverse-text);
}
.affiliate-application-copy {
  padding: 42px;
}
.affiliate-application-copy .affiliate-kicker {
  color: color-mix(in srgb, var(--inverse-text) 55%, transparent);
}
.affiliate-application-copy h2 {
  max-width: 560px;
  margin: 12px 0;
  font-size: clamp(27px, 4vw, 43px);
  letter-spacing: -0.05em;
  line-height: 1.08;
}
.affiliate-application-copy > p {
  max-width: 610px;
  color: color-mix(in srgb, var(--inverse-text) 65%, transparent);
  font-size: 10px;
  line-height: 1.8;
}
.affiliate-application-copy ul {
  display: grid;
  gap: 10px;
  margin: 24px 0 0;
  padding: 0;
  list-style: none;
}
.affiliate-application-copy li {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 9px;
}
.affiliate-application-copy li svg {
  width: 14px;
}
.affiliate-application-form {
  align-self: center;
  display: grid;
  gap: 12px;
  margin: 26px;
  border-radius: 8px;
  background: var(--surface);
  color: var(--text);
  padding: 24px;
}
.affiliate-application-form > svg {
  width: 25px;
}
.affiliate-application-form h3 {
  margin: 0;
  font-size: 16px;
}
.affiliate-application-form > p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.7;
}
.affiliate-application-form label {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.6;
}
.affiliate-application-form input {
  margin-top: 2px;
}
.affiliate-application-form a {
  color: var(--text);
  text-decoration: underline;
}
.affiliate-status {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 13px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
  padding: 16px;
}
.affiliate-status > svg {
  width: 20px;
}
.affiliate-status > div {
  display: grid;
  gap: 4px;
}
.affiliate-status span,
.affiliate-status small {
  color: var(--muted);
  font-size: 8px;
}
.affiliate-status b {
  font-size: 10px;
}
.affiliate-status > a {
  border: 1px solid var(--line);
  border-radius: 5px;
  padding: 7px 10px;
  font-size: 8px;
}
.affiliate-status.status-active {
  border-color: color-mix(in srgb, var(--success) 40%, var(--line));
}
.affiliate-status.status-active > svg {
  color: var(--success);
}
.affiliate-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}
.affiliate-metrics article {
  min-width: 0;
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 6px 8px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
  padding: 16px;
}
.affiliate-metrics svg {
  width: 16px;
  grid-row: span 2;
}
.affiliate-metrics span,
.affiliate-metrics small {
  color: var(--muted);
  font-size: 8px;
}
.affiliate-metrics strong {
  grid-column: 2;
  overflow-wrap: anywhere;
  font-size: 18px;
}
.affiliate-metrics small {
  grid-column: 1 / -1;
}
.affiliate-main-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(300px, 0.72fr);
  gap: 14px;
}
.referral-panel > header,
.withdrawal-panel > header,
.affiliate-records > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--line);
}
.referral-panel > header h2,
.withdrawal-panel > header h2,
.affiliate-records > header h2 {
  margin: 5px 0 0;
}
.referral-panel > header > svg,
.withdrawal-panel > header > svg {
  width: 18px;
  color: var(--muted);
}
.referral-panel > label,
.withdrawal-panel form,
.withdrawal-panel form > label {
  display: grid;
  gap: 7px;
}
.referral-panel > label {
  margin-top: 15px;
  font-size: 8px;
  font-weight: 600;
}
.affiliate-copy-field {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--soft);
}
.affiliate-copy-field code {
  min-width: 0;
  overflow: hidden;
  padding: 10px;
  font-size: 9px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.affiliate-copy-field button,
.withdrawal-amount-field button {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: 0;
  border-left: 1px solid var(--line);
  background: var(--surface);
  padding: 0 10px;
  font-size: 8px;
  cursor: pointer;
}
.affiliate-copy-field button svg {
  width: 12px;
}
.referral-panel > small,
.withdrawal-panel form > small {
  display: block;
  margin-top: 10px;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.6;
}
.withdrawal-panel form {
  grid-template-columns: 1fr 1fr;
  gap: 11px;
  padding-top: 15px;
}
.withdrawal-panel form > label {
  font-size: 8px;
  font-weight: 600;
}
.withdrawal-panel form > label:first-child,
.withdrawal-panel form > label:nth-child(3),
.withdrawal-panel form > small,
.withdrawal-panel form > button {
  grid-column: 1 / -1;
}
.withdrawal-panel input,
.withdrawal-panel select {
  width: 100%;
  min-width: 0;
  height: 38px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--bg);
  color: var(--text);
  padding: 0 10px;
  font-size: 9px;
  outline: none;
}
.withdrawal-panel input:focus,
.withdrawal-panel select:focus {
  border-color: var(--text);
}
.withdrawal-amount-field {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
}
.withdrawal-amount-field input {
  border-radius: 5px 0 0 5px;
}
.withdrawal-amount-field button {
  border: 1px solid var(--line);
  border-left: 0;
  border-radius: 0 5px 5px 0;
}
.affiliate-panel-placeholder,
.affiliate-empty {
  min-height: 150px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 8px;
  color: var(--muted);
  text-align: center;
}
.affiliate-panel-placeholder svg {
  width: 21px;
}
.affiliate-panel-placeholder p {
  max-width: 330px;
  margin: 0;
  font-size: 9px;
  line-height: 1.7;
}
.affiliate-records > header p {
  margin: 4px 0 0;
  color: var(--muted);
  font-size: 8px;
}
.affiliate-records > header button {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 31px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  padding: 0 9px;
  font-size: 8px;
  cursor: pointer;
}
.affiliate-records > header button:disabled {
  opacity: 0.5;
  cursor: wait;
}
.affiliate-records > header button svg {
  width: 12px;
}
.commission-list,
.withdrawal-list {
  display: grid;
}
.commission-list > article,
.withdrawal-list > article {
  display: grid;
  grid-template-columns: auto minmax(180px, 1fr) minmax(120px, 0.55fr) minmax(
      120px,
      0.55fr
    );
  align-items: center;
  gap: 12px;
  padding: 14px 0;
  border-bottom: 1px solid var(--line);
}
.commission-list > article:last-child,
.withdrawal-list > article:last-child {
  border-bottom: 0;
}
.record-icon {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: var(--soft);
}
.record-icon svg {
  width: 14px;
}
.record-main,
.record-adjustment,
.record-state {
  min-width: 0;
  display: grid;
  gap: 4px;
}
.record-main > span,
.record-main small,
.record-adjustment span,
.record-state small {
  color: var(--muted);
  font-size: 8px;
}
.record-main b,
.record-adjustment strong {
  font-size: 9px;
}
.record-adjustment strong {
  color: var(--text);
}
.record-state {
  justify-items: end;
  text-align: right;
}
.record-state em {
  border: 1px solid var(--line);
  border-radius: 999px;
  padding: 3px 7px;
  color: var(--muted);
  font-size: 7px;
  font-style: normal;
}
.record-state em.status-available,
.record-state em.status-completed {
  border-color: color-mix(in srgb, var(--success) 45%, var(--line));
  color: var(--success);
}
.withdrawal-reason {
  grid-column: 2 / -1;
  margin: -4px 0 0;
  border-radius: 4px;
  background: var(--soft);
  color: var(--muted);
  padding: 7px 9px;
  font-size: 8px;
}
.affiliate-empty {
  font-size: 9px;
}
@media (max-width: 900px) {
  .affiliate-application,
  .affiliate-main-grid {
    grid-template-columns: 1fr;
  }
  .affiliate-metrics {
    grid-template-columns: 1fr 1fr;
  }
}
@media (max-width: 650px) {
  .affiliate-application-copy {
    padding: 28px;
  }
  .affiliate-application-form {
    margin: 0 18px 18px;
  }
  .affiliate-status {
    grid-template-columns: auto 1fr;
  }
  .affiliate-status > a {
    grid-column: 2;
    justify-self: start;
  }
  .commission-list > article,
  .withdrawal-list > article {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .record-adjustment,
  .record-state,
  .withdrawal-reason {
    grid-column: 2;
    justify-items: start;
    text-align: left;
  }
}
@media (max-width: 480px) {
  .affiliate-metrics,
  .withdrawal-panel form {
    grid-template-columns: 1fr;
  }
  .withdrawal-panel form > label,
  .withdrawal-panel form > small,
  .withdrawal-panel form > button {
    grid-column: 1;
  }
  .affiliate-copy-field {
    grid-template-columns: 1fr;
  }
  .affiliate-copy-field button {
    justify-content: center;
    border-top: 1px solid var(--line);
    border-left: 0;
    padding: 8px;
  }
}
</style>
