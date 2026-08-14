<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  AlertCircle,
  Banknote,
  Check,
  ChevronLeft,
  ChevronRight,
  Copy,
  Eye,
  LoaderCircle,
  RefreshCw,
  Search,
  ShieldCheck,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import { useI18n } from "vue-i18n";
import { formatMoney } from "../utils/money";

const { t, locale } = useI18n();
const authStore = useAuthStore();
const canManage = computed(() => authStore.hasPermission("reseller.manage"));

interface Withdrawal {
  id: string;
  withdrawal_no: string;
  reseller_id: string;
  reseller_code: string;
  reseller_name: string;
  user_email: string;
  amount: number;
  fee: number;
  currency: string;
  method: string;
  account_preview: string;
  status: string;
  payout_reference?: string;
  reason?: string;
  processed_at?: string | null;
  created_at: string;
  updated_at: string;
}

const items = ref<Withdrawal[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const status = ref("");
const queryInput = ref("");
const appliedQuery = ref("");
const loading = ref(false);
const error = ref("");
const notice = ref("");
const selected = ref<Withdrawal | null>(null);
const targetStatus = ref("");
const payoutReference = ref("");
const reason = ref("");
const revealedAccount = ref("");
const saving = ref(false);
const revealing = ref(false);
const formError = ref("");
const copied = ref(false);
let requestSequence = 0;

const pages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const transitions = computed(() => {
  if (selected.value?.status === "pending")
    return ["processing", "completed", "rejected"];
  if (selected.value?.status === "processing") return ["completed", "rejected"];
  return [];
});

function money(value = 0, currency?: string) {
  return formatMoney(value, currency, locale.value);
}
function date(value?: string | null) {
  return value
    ? new Date(value).toLocaleString("zh-CN", { hour12: false })
    : "—";
}
function statusLabel(value: string) {
  return (
    {
      pending: t("reselleradmin.withdrawal.statusPending"),
      processing: t("reselleradmin.withdrawal.statusProcessing"),
      completed: t("reselleradmin.withdrawal.statusCompleted"),
      rejected: t("reselleradmin.withdrawal.statusRejected"),
    }[value] || value
  );
}
function message(reason: any, fallback: string) {
  return reason?.response?.data?.message || fallback;
}
function validReason() {
  const length = [...reason.value.trim()].length;
  return length >= 4 && length <= 500;
}

async function load(targetPage = page.value) {
  const sequence = ++requestSequence;
  loading.value = true;
  error.value = "";
  try {
    const { data } = await adminApi.get("/reseller-withdrawals", {
      params: {
        page: targetPage,
        page_size: pageSize.value,
        status: status.value || undefined,
        q: appliedQuery.value || undefined,
      },
    });
    if (sequence !== requestSequence) return;
    items.value = Array.isArray(data.data?.items) ? data.data.items : [];
    total.value = Number(data.data?.total || 0);
    page.value = Number(data.data?.page || targetPage);
  } catch (failure: any) {
    if (sequence === requestSequence)
      error.value = message(failure, t("reselleradmin.withdrawal.errLoad"));
  } finally {
    if (sequence === requestSequence) loading.value = false;
  }
}

function search() {
  appliedQuery.value = queryInput.value.trim();
  void load(1);
}
function open(item: Withdrawal) {
  selected.value = item;
  targetStatus.value = "";
  payoutReference.value = "";
  reason.value = "";
  revealedAccount.value = "";
  formError.value = "";
  copied.value = false;
}
function close() {
  if (saving.value || revealing.value) return;
  selected.value = null;
  revealedAccount.value = "";
  payoutReference.value = "";
  reason.value = "";
  formError.value = "";
}
async function reveal() {
  if (!canManage.value) return;
  if (!selected.value || !validReason()) {
    formError.value = t("reselleradmin.withdrawal.errReasonReveal");
    return;
  }
  revealing.value = true;
  formError.value = "";
  try {
    const { data } = await adminApi.post(
      `/reseller-withdrawals/${encodeURIComponent(selected.value.id)}/reveal`,
      {},
      { headers: { "X-Change-Reason": reason.value.trim() } },
    );
    revealedAccount.value = String(data.data?.account || "");
    if (!revealedAccount.value) throw new Error("missing account");
  } catch (failure: any) {
    formError.value = message(failure, t("reselleradmin.withdrawal.errReveal"));
  } finally {
    revealing.value = false;
  }
}
async function copyAccount() {
  if (!canManage.value) return;
  if (
    !revealedAccount.value ||
    !window.isSecureContext ||
    !navigator.clipboard
  ) {
    formError.value = t("reselleradmin.withdrawal.errCopyContext");
    return;
  }
  try {
    await navigator.clipboard.writeText(revealedAccount.value);
    copied.value = true;
    window.setTimeout(() => (copied.value = false), 1800);
  } catch {
    formError.value = t("reselleradmin.withdrawal.errCopy");
  }
}
async function submit() {
  if (!canManage.value) return;
  if (!selected.value || !transitions.value.includes(targetStatus.value)) {
    formError.value = t("reselleradmin.withdrawal.errTransition");
    return;
  }
  if (!validReason()) {
    formError.value = t("reselleradmin.withdrawal.errReasonProcess");
    return;
  }
  const reference = payoutReference.value.trim();
  if (
    targetStatus.value === "completed" &&
    ([...reference].length < 4 || [...reference].length > 160)
  ) {
    formError.value = t("reselleradmin.withdrawal.errReference");
    return;
  }
  saving.value = true;
  formError.value = "";
  try {
    await adminApi.patch(
      `/reseller-withdrawals/${encodeURIComponent(selected.value.id)}`,
      { status: targetStatus.value, payout_reference: reference },
      { headers: { "X-Change-Reason": reason.value.trim() } },
    );
    notice.value = t("reselleradmin.withdrawal.updated", {
      no: selected.value.withdrawal_no,
      status: statusLabel(targetStatus.value),
    });
    closeAfterSave();
    await load(page.value);
  } catch (failure: any) {
    formError.value = message(failure, t("reselleradmin.withdrawal.errSave"));
  } finally {
    saving.value = false;
  }
}
function closeAfterSave() {
  selected.value = null;
  revealedAccount.value = "";
  payoutReference.value = "";
  reason.value = "";
}
function handleEscape(event: KeyboardEvent) {
  if (event.key === "Escape") close();
}
onMounted(() => {
  window.addEventListener("keydown", handleEscape);
  void load(1);
});
onBeforeUnmount(() => window.removeEventListener("keydown", handleEscape));
</script>

<template>
  <section class="withdrawal-admin panel">
    <header>
      <div>
        <span>{{ t("adminKicker.payoutOperations") }}</span>
        <h2>{{ t("reselleradmin.withdrawal.title") }}</h2>
        <p>{{ t("reselleradmin.withdrawal.subtitle") }}</p>
      </div>
      <button type="button" :disabled="loading" @click="load(page)">
        <RefreshCw :class="{ spinning: loading }" />{{
          t("reselleradmin.withdrawal.refresh")
        }}
      </button>
    </header>
    <form class="withdrawal-toolbar" @submit.prevent="search">
      <label
        ><Search /><input
          v-model="queryInput"
          type="search"
          maxlength="190"
          :placeholder="t('reselleradmin.withdrawal.searchPlaceholder')"
      /></label>
      <select v-model="status" @change="load(1)">
        <option value="">{{ t("reselleradmin.withdrawal.statusAll") }}</option>
        <option value="pending">
          {{ t("reselleradmin.withdrawal.statusPending") }}
        </option>
        <option value="processing">
          {{ t("reselleradmin.withdrawal.statusProcessing") }}
        </option>
        <option value="completed">
          {{ t("reselleradmin.withdrawal.statusCompleted") }}
        </option>
        <option value="rejected">
          {{ t("reselleradmin.withdrawal.statusRejected") }}
        </option>
      </select>
      <button type="submit">{{ t("reselleradmin.withdrawal.filter") }}</button>
    </form>
    <p v-if="notice" class="withdrawal-notice success"><Check />{{ notice }}</p>
    <p v-if="error" class="withdrawal-notice error">
      <AlertCircle />{{ error }}
    </p>
    <div class="withdrawal-table-wrap">
      <table v-if="items.length">
        <thead>
          <tr>
            <th>{{ t("reselleradmin.withdrawal.colWithdrawal") }}</th>
            <th>{{ t("reselleradmin.withdrawal.colReseller") }}</th>
            <th>{{ t("reselleradmin.withdrawal.colAmount") }}</th>
            <th>{{ t("reselleradmin.withdrawal.colMethod") }}</th>
            <th>{{ t("reselleradmin.withdrawal.colStatus") }}</th>
            <th>{{ t("reselleradmin.withdrawal.colTime") }}</th>
            <th>{{ t("reselleradmin.withdrawal.colActions") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in items" :key="item.id">
            <td>
              <b>{{ item.withdrawal_no }}</b
              ><small>{{ item.id }}</small>
            </td>
            <td>
              <b>{{ item.reseller_name }}</b
              ><small>{{ item.reseller_code }} · {{ item.user_email }}</small>
            </td>
            <td>
              <b>{{ money(item.amount, item.currency) }}</b
              ><small>{{
                t("reselleradmin.withdrawal.fee", {
                  amount: money(item.fee, item.currency),
                })
              }}</small>
            </td>
            <td>
              <b>{{ item.method.toUpperCase() }}</b
              ><small>{{ item.account_preview }}</small>
            </td>
            <td>
              <span :class="`status-${item.status}`">{{
                statusLabel(item.status)
              }}</span>
            </td>
            <td>
              <b>{{ date(item.created_at) }}</b
              ><small v-if="item.processed_at">{{
                t("reselleradmin.withdrawal.processedAt", {
                  time: date(item.processed_at),
                })
              }}</small>
            </td>
            <td>
              <button type="button" @click="open(item)">
                <ShieldCheck />{{ t("reselleradmin.withdrawal.actionReview") }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="withdrawal-empty">
        <LoaderCircle v-if="loading" class="spinning" /><Banknote v-else /><b>{{
          loading
            ? t("reselleradmin.withdrawal.loadingRows")
            : t("reselleradmin.withdrawal.noRecords")
        }}</b>
      </div>
    </div>
    <footer>
      <span>{{
        t("reselleradmin.withdrawal.pagination", { total, page, pages })
      }}</span>
      <div>
        <button :disabled="page <= 1 || loading" @click="load(page - 1)">
          <ChevronLeft /></button
        ><button :disabled="page >= pages || loading" @click="load(page + 1)">
          <ChevronRight />
        </button>
      </div>
    </footer>

    <div
      v-if="selected"
      class="withdrawal-modal-backdrop"
      @mousedown.self="close"
    >
      <section
        class="withdrawal-modal"
        role="dialog"
        aria-modal="true"
        :aria-label="t('reselleradmin.withdrawal.modalTitle')"
      >
        <header>
          <div>
            <span>{{ t("adminKicker.payoutReview") }}</span>
            <h2>{{ selected.withdrawal_no }}</h2>
            <p>{{ selected.reseller_name }} · {{ selected.user_email }}</p>
          </div>
          <button :disabled="saving || revealing" @click="close"><X /></button>
        </header>
        <p v-if="formError" class="withdrawal-notice error">
          <AlertCircle />{{ formError }}
        </p>
        <div class="withdrawal-facts">
          <div>
            <span>{{ t("reselleradmin.withdrawal.amountLabel") }}</span
            ><strong>{{ money(selected.amount, selected.currency) }}</strong>
          </div>
          <div>
            <span>{{ t("reselleradmin.withdrawal.methodLabel") }}</span
            ><strong>{{ selected.method.toUpperCase() }}</strong>
          </div>
          <div>
            <span>{{ t("reselleradmin.withdrawal.accountSummary") }}</span
            ><strong>{{ selected.account_preview }}</strong>
          </div>
          <div>
            <span>{{ t("reselleradmin.withdrawal.currentStatus") }}</span
            ><strong>{{ statusLabel(selected.status) }}</strong>
          </div>
        </div>
        <label v-if="canManage"
          >{{ t("reselleradmin.withdrawal.reasonLabel")
          }}<textarea
            v-model="reason"
            maxlength="500"
            :placeholder="t('reselleradmin.withdrawal.reasonPlaceholder')"
          ></textarea>
        </label>
        <div v-if="canManage" class="reveal-box">
          <button type="button" :disabled="revealing" @click="reveal">
            <Eye />{{
              revealing
                ? t("reselleradmin.withdrawal.revealing")
                : t("reselleradmin.withdrawal.revealAccount")
            }}</button
          ><template v-if="revealedAccount"
            ><code>{{ revealedAccount }}</code
            ><button type="button" @click="copyAccount">
              <Copy />{{
                copied
                  ? t("reselleradmin.withdrawal.copied")
                  : t("reselleradmin.withdrawal.copy")
              }}
            </button></template
          ><small v-else>{{
            t("reselleradmin.withdrawal.accountMemoryNote")
          }}</small>
        </div>
        <form v-if="canManage" @submit.prevent="submit">
          <label
            >{{ t("reselleradmin.withdrawal.resultLabel")
            }}<select v-model="targetStatus">
              <option value="">
                {{ t("reselleradmin.withdrawal.selectResult") }}
              </option>
              <option v-for="value in transitions" :key="value" :value="value">
                {{ statusLabel(value) }}
              </option>
            </select></label
          ><label
            >{{ t("reselleradmin.withdrawal.payoutRefLabel")
            }}<input
              v-model="payoutReference"
              maxlength="160"
              :required="targetStatus === 'completed'"
              :placeholder="t('reselleradmin.withdrawal.payoutRefPlaceholder')"
          /></label>
          <footer>
            <button type="button" :disabled="saving" @click="close">
              {{ t("reselleradmin.withdrawal.cancel") }}</button
            ><button class="primary" :disabled="saving">
              <LoaderCircle v-if="saving" class="spinning" /><Check v-else />{{
                saving
                  ? t("reselleradmin.withdrawal.saving")
                  : t("reselleradmin.withdrawal.confirm")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>
  </section>
</template>

<style scoped>
.withdrawal-admin {
  padding: 18px;
  display: grid;
  gap: 14px;
}
.withdrawal-admin > header,
.withdrawal-modal > header {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: start;
}
.withdrawal-admin h2,
.withdrawal-admin p,
.withdrawal-modal h2,
.withdrawal-modal p {
  margin: 4px 0;
}
.withdrawal-admin header span,
.withdrawal-modal header span {
  font-size: 9px;
  letter-spacing: 0.14em;
  color: var(--muted);
}
.withdrawal-admin header p,
.withdrawal-modal header p {
  font-size: 10px;
  color: var(--muted);
}
button {
  border: 1px solid var(--line);
  background: var(--surface);
  color: var(--text);
  border-radius: 6px;
  min-height: 32px;
  padding: 0 10px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  cursor: pointer;
}
button svg {
  width: 14px;
  height: 14px;
}
button:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.withdrawal-toolbar {
  display: flex;
  gap: 8px;
  padding: 10px;
  background: var(--soft);
  border-radius: 8px;
}
.withdrawal-toolbar label {
  flex: 1;
  position: relative;
}
.withdrawal-toolbar label svg {
  position: absolute;
  left: 9px;
  top: 9px;
  width: 14px;
}
input,
select,
textarea {
  border: 1px solid var(--line);
  background: var(--surface);
  color: var(--text);
  border-radius: 6px;
  font: inherit;
}
.withdrawal-toolbar input {
  width: 100%;
  height: 34px;
  padding: 0 10px 0 31px;
}
.withdrawal-toolbar select {
  height: 34px;
  padding: 0 28px 0 9px;
}
.withdrawal-notice {
  display: flex;
  gap: 8px;
  align-items: center;
  padding: 10px;
  border-radius: 6px;
  font-size: 10px;
}
.withdrawal-notice svg {
  width: 15px;
}
.withdrawal-notice.success {
  color: #166534;
  background: #f0fdf4;
}
.withdrawal-notice.error {
  color: #991b1b;
  background: #fef2f2;
}
:global([data-theme="dark"]) .withdrawal-notice.success {
  color: #bbf7d0;
  background: #052e16;
}
:global([data-theme="dark"]) .withdrawal-notice.error {
  color: #fecaca;
  background: #450a0a;
}
.withdrawal-table-wrap {
  overflow-x: auto;
  border: 1px solid var(--line);
  border-radius: 8px;
}
table {
  width: 100%;
  border-collapse: collapse;
  min-width: 940px;
}
th,
td {
  padding: 11px 12px;
  text-align: left;
  border-bottom: 1px solid var(--line);
  font-size: 10px;
}
th {
  color: var(--muted);
  background: var(--soft);
  font-size: 8px;
  text-transform: uppercase;
}
td b,
td small {
  display: block;
}
td small {
  color: var(--muted);
  margin-top: 4px;
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
}
td span {
  font-size: 9px;
}
.status-completed {
  color: #15803d;
}
.status-rejected {
  color: #b42318;
}
.status-pending,
.status-processing {
  color: var(--muted);
}
.withdrawal-empty {
  min-height: 220px;
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 10px;
  color: var(--muted);
}
.withdrawal-empty svg {
  width: 26px;
}
.withdrawal-admin > footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  color: var(--muted);
  font-size: 9px;
}
.withdrawal-admin > footer div {
  display: flex;
  gap: 5px;
}
.withdrawal-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 80;
  background: rgba(0, 0, 0, 0.55);
  display: grid;
  place-items: center;
  padding: 18px;
}
.withdrawal-modal {
  width: min(680px, 100%);
  max-height: calc(100vh - 36px);
  overflow: auto;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 12px;
  padding: 20px;
  box-shadow: var(--shadow);
  display: grid;
  gap: 15px;
}
.withdrawal-modal > label,
.withdrawal-modal form > label {
  display: grid;
  gap: 6px;
  color: var(--muted);
  font-size: 10px;
}
.withdrawal-modal textarea {
  min-height: 80px;
  resize: vertical;
  padding: 9px;
}
.withdrawal-modal input,
.withdrawal-modal select {
  height: 37px;
  padding: 0 9px;
}
.withdrawal-facts {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
}
.withdrawal-facts div {
  padding: 11px;
  background: var(--soft);
  border-radius: 7px;
  display: grid;
  gap: 5px;
}
.withdrawal-facts span {
  color: var(--muted);
  font-size: 9px;
}
.withdrawal-facts strong {
  font-size: 12px;
}
.reveal-box {
  border: 1px dashed var(--line);
  border-radius: 8px;
  padding: 10px;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.reveal-box code {
  flex: 1;
  min-width: 180px;
  padding: 9px;
  background: var(--soft);
  overflow-wrap: anywhere;
}
.reveal-box small {
  color: var(--muted);
}
.withdrawal-modal form {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.withdrawal-modal form footer {
  grid-column: 1 / -1;
  display: flex;
  justify-content: end;
  gap: 8px;
  margin-top: 6px;
}
.withdrawal-modal .primary {
  background: var(--text);
  color: var(--surface);
}
.spinning {
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 700px) {
  .withdrawal-toolbar {
    display: grid;
  }
  .withdrawal-facts {
    grid-template-columns: 1fr 1fr;
  }
  .withdrawal-modal form {
    grid-template-columns: 1fr;
  }
  .withdrawal-modal form footer {
    grid-column: auto;
  }
}
</style>
