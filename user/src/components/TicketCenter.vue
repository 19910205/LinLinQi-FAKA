<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  ChevronLeft,
  ChevronRight,
  CircleHelp,
  Clock3,
  Inbox,
  MessageSquare,
  Plus,
  RefreshCw,
  Send,
  Ticket,
  X,
} from "@lucide/vue";
import {
  createTicket,
  fetchTicket,
  fetchTicketOrderOptions,
  fetchTickets,
  replyTicket,
} from "../api";
import type {
  Order,
  SupportTicket,
  TicketCategory,
  TicketDetail,
} from "../types";

const { t, locale } = useI18n();

const tickets = ref<SupportTicket[]>([]);
const orders = ref<Order[]>([]);
const detail = ref<TicketDetail | null>(null);
const selectedID = ref("");
const page = ref(1);
const pageSize = 12;
const total = ref(0);
const loadingList = ref(false);
const loadingDetail = ref(false);
const ordersUnavailable = ref(false);
const createOpen = ref(false);
const creating = ref(false);
const replying = ref(false);
const error = ref("");
const notice = ref("");
const replyBody = ref("");
let detailSequence = 0;

const createForm = reactive({
  category: "delivery" as TicketCategory,
  order_id: "",
  subject: "",
  body: "",
});

const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize)),
);
const canReply = computed(
  () => detail.value?.ticket && detail.value.ticket.status !== "closed",
);
const categories = computed<Array<{ value: TicketCategory; label: string }>>(
  () =>
    (["delivery", "product", "billing", "refund", "other"] as const).map(
      (value) => ({ value, label: t(`ticket.categories.${value}`) }),
    ),
);

function date(value?: string | null) {
  return value
    ? new Date(value).toLocaleString(locale.value, { hour12: false })
    : "—";
}

function statusLabel(status: string) {
  return ["open", "in_progress", "waiting_user", "resolved", "closed"].includes(
    status,
  )
    ? t(`ticket.status.${status}`)
    : status;
}

function categoryLabel(category: string) {
  return (
    categories.value.find((item) => item.value === category)?.label || category
  );
}

function orderLabel(id: string) {
  return (
    orders.value.find((order) => order.id === id)?.order_no || id.slice(0, 8)
  );
}

function requestError(reason: any, fallback: string) {
  return reason?.response?.data?.message || fallback;
}

async function loadOrders() {
  ordersUnavailable.value = false;
  try {
    orders.value = await fetchTicketOrderOptions();
  } catch {
    orders.value = [];
    ordersUnavailable.value = true;
  }
}

async function loadTickets(autoOpen = false) {
  loadingList.value = true;
  error.value = "";
  try {
    const result = await fetchTickets(page.value, pageSize);
    tickets.value = result.items;
    total.value = result.total;
    page.value = result.page;
    if (autoOpen && !selectedID.value && result.items.length) {
      await openTicket(result.items[0].id);
    }
  } catch (reason: any) {
    tickets.value = [];
    total.value = 0;
    error.value = requestError(reason, t("ticket.errList"));
  } finally {
    loadingList.value = false;
  }
}

async function openTicket(id: string) {
  const sequence = ++detailSequence;
  selectedID.value = id;
  loadingDetail.value = true;
  error.value = "";
  notice.value = "";
  try {
    const result = await fetchTicket(id);
    if (sequence !== detailSequence) return;
    detail.value = result;
    const item = tickets.value.find((ticket) => ticket.id === id);
    if (item) {
      item.user_unread = 0;
      item.status = result.ticket.status;
      item.last_message_at = result.ticket.last_message_at;
    }
  } catch (reason: any) {
    if (sequence !== detailSequence) return;
    detail.value = null;
    error.value = requestError(reason, t("ticket.errDetail"));
  } finally {
    if (sequence === detailSequence) loadingDetail.value = false;
  }
}

function resetCreateForm() {
  createForm.category = "delivery";
  createForm.order_id = "";
  createForm.subject = "";
  createForm.body = "";
}

async function submitTicket() {
  error.value = "";
  notice.value = "";
  const subject = createForm.subject.trim();
  const body = createForm.body.trim();
  if (Array.from(subject).length < 3) {
    error.value = t("ticket.errSubject");
    return;
  }
  if (Array.from(body).length < 2) {
    error.value = t("ticket.errBody");
    return;
  }
  creating.value = true;
  try {
    const ticket = await createTicket({
      category: createForm.category,
      subject,
      body,
      ...(createForm.order_id ? { order_id: createForm.order_id } : {}),
    });
    createOpen.value = false;
    resetCreateForm();
    page.value = 1;
    selectedID.value = ticket.id;
    await loadTickets(false);
    await openTicket(ticket.id);
    notice.value = t("ticket.submitted", { no: ticket.ticket_no });
  } catch (reason: any) {
    error.value = requestError(reason, t("ticket.errSubmit"));
  } finally {
    creating.value = false;
  }
}

async function submitReply() {
  const ticket = detail.value?.ticket;
  const body = replyBody.value.trim();
  error.value = "";
  notice.value = "";
  if (!ticket || ticket.status === "closed") return;
  if (!body) {
    error.value = t("ticket.errReplyEmpty");
    return;
  }
  replying.value = true;
  try {
    await replyTicket(ticket.id, body);
    replyBody.value = "";
    await openTicket(ticket.id);
    await loadTickets(false);
    notice.value = t("ticket.replied");
  } catch (reason: any) {
    error.value = requestError(reason, t("ticket.errReply"));
  } finally {
    replying.value = false;
  }
}

async function changePage(nextPage: number) {
  if (nextPage < 1 || nextPage > totalPages.value || nextPage === page.value)
    return;
  page.value = nextPage;
  selectedID.value = "";
  detail.value = null;
  replyBody.value = "";
  await loadTickets(true);
}

async function refresh() {
  await loadTickets(false);
  if (selectedID.value) await openTicket(selectedID.value);
}

onMounted(async () => {
  await Promise.all([loadOrders(), loadTickets(true)]);
});
</script>

<template>
  <div class="ticket-center">
    <section class="account-panel ticket-toolbar">
      <div>
        <span class="ticket-kicker">{{ t("kicker.supportDesk") }}</span>
        <h2>{{ t("ticket.title") }}</h2>
        <p>{{ t("ticket.subtitle") }}</p>
      </div>
      <div class="ticket-toolbar-actions">
        <button
          type="button"
          class="ticket-refresh"
          :disabled="loadingList || loadingDetail"
          :aria-label="t('ticket.refresh')"
          @click="refresh"
        >
          <RefreshCw />{{ t("ticket.refresh") }}
        </button>
        <button
          type="button"
          class="button primary"
          @click="createOpen = !createOpen"
        >
          <X v-if="createOpen" /><Plus v-else />
          {{ createOpen ? t("ticket.collapse") : t("ticket.create") }}
        </button>
      </div>
    </section>

    <p v-if="error" class="ticket-feedback error" role="alert">{{ error }}</p>
    <p v-if="notice" class="ticket-feedback notice" role="status">
      {{ notice }}
    </p>

    <section v-if="createOpen" class="account-panel ticket-create-panel">
      <div class="ticket-section-heading">
        <div>
          <h2>{{ t("ticket.createTitle") }}</h2>
          <p>{{ t("ticket.createSub") }}</p>
        </div>
      </div>
      <form @submit.prevent="submitTicket">
        <div class="ticket-form-grid">
          <label>
            {{ t("ticket.category") }}
            <select v-model="createForm.category" required>
              <option
                v-for="category in categories"
                :key="category.value"
                :value="category.value"
              >
                {{ category.label }}
              </option>
            </select>
          </label>
          <label>
            {{ t("ticket.order") }}
            <select v-model="createForm.order_id" :disabled="ordersUnavailable">
              <option value="">{{ t("ticket.noOrder") }}</option>
              <option v-for="order in orders" :key="order.id" :value="order.id">
                {{ order.order_no }} · {{ date(order.created_at) }}
              </option>
            </select>
            <small v-if="ordersUnavailable">{{
              t("ticket.ordersUnavailable")
            }}</small>
          </label>
        </div>
        <label>
          {{ t("ticket.subject") }}
          <input
            v-model="createForm.subject"
            type="text"
            minlength="3"
            maxlength="200"
            autocomplete="off"
            :placeholder="t('ticket.subjectPlaceholder')"
            required
          />
        </label>
        <label>
          {{ t("ticket.body") }}
          <textarea
            v-model="createForm.body"
            minlength="2"
            maxlength="10000"
            rows="6"
            :placeholder="t('ticket.bodyPlaceholder')"
            required
          ></textarea>
          <small>{{ Array.from(createForm.body).length }} / 10000</small>
        </label>
        <div class="ticket-form-actions">
          <button
            type="button"
            class="button secondary"
            :disabled="creating"
            @click="createOpen = false"
          >
            {{ t("ticket.cancel") }}
          </button>
          <button class="button primary" :disabled="creating">
            <Send />{{ creating ? t("ticket.submitting") : t("ticket.submit") }}
          </button>
        </div>
      </form>
    </section>

    <div class="ticket-workspace">
      <section class="account-panel ticket-list-panel">
        <div class="ticket-section-heading compact">
          <div>
            <h2>{{ t("ticket.myTickets") }}</h2>
            <p>{{ t("ticket.total", { n: total }) }}</p>
          </div>
          <Inbox />
        </div>
        <div v-if="loadingList" class="ticket-empty">
          {{ t("ticket.loadingList") }}
        </div>
        <div v-else-if="tickets.length" class="ticket-list">
          <button
            v-for="item in tickets"
            :key="item.id"
            type="button"
            :class="['ticket-card', { active: selectedID === item.id }]"
            @click="openTicket(item.id)"
          >
            <Ticket />
            <span class="ticket-card-copy">
              <small>{{ item.ticket_no }}</small>
              <b>{{ item.subject }}</b>
              <span>
                {{ categoryLabel(item.category) }} ·
                {{ date(item.last_message_at || item.created_at) }}
              </span>
            </span>
            <span class="ticket-card-state">
              <em :class="`status-${item.status}`">{{
                statusLabel(item.status)
              }}</em>
              <strong v-if="item.user_unread > 0">
                {{ item.user_unread > 99 ? "99+" : item.user_unread }}
                {{ t("ticket.unread") }}
              </strong>
            </span>
          </button>
        </div>
        <div v-else class="ticket-empty">
          <CircleHelp />
          <b>{{ t("ticket.noTickets") }}</b>
          <span>{{ t("ticket.noTicketsSub") }}</span>
        </div>
        <div v-if="totalPages > 1" class="ticket-pagination">
          <button
            type="button"
            :disabled="page <= 1 || loadingList"
            :aria-label="t('ticket.prev')"
            @click="changePage(page - 1)"
          >
            <ChevronLeft />
          </button>
          <span>{{ page }} / {{ totalPages }}</span>
          <button
            type="button"
            :disabled="page >= totalPages || loadingList"
            :aria-label="t('ticket.next')"
            @click="changePage(page + 1)"
          >
            <ChevronRight />
          </button>
        </div>
      </section>

      <section class="account-panel ticket-detail-panel">
        <div v-if="loadingDetail" class="ticket-empty">
          {{ t("ticket.loadingDetail") }}
        </div>
        <template v-else-if="detail">
          <header class="ticket-detail-header">
            <div>
              <span>{{ detail.ticket.ticket_no }}</span>
              <h2>{{ detail.ticket.subject }}</h2>
              <p>
                {{ categoryLabel(detail.ticket.category) }} ·
                {{
                  t("ticket.createdAt", {
                    time: date(detail.ticket.created_at),
                  })
                }}
                <template v-if="detail.ticket.order_id">
                  ·
                  {{
                    t("ticket.order", {
                      no: orderLabel(detail.ticket.order_id),
                    })
                  }}
                </template>
              </p>
            </div>
            <em :class="`status-${detail.ticket.status}`">
              {{ statusLabel(detail.ticket.status) }}
            </em>
          </header>

          <div class="ticket-conversation" aria-live="polite">
            <article
              v-for="message in detail.messages"
              :key="message.id"
              :class="['ticket-message', message.author_type]"
            >
              <header>
                <b>{{
                  message.author_type === "admin"
                    ? t("ticket.agent")
                    : t("ticket.me")
                }}</b>
                <time :datetime="message.created_at">{{
                  date(message.created_at)
                }}</time>
              </header>
              <p>{{ message.body }}</p>
            </article>
            <div v-if="!detail.messages.length" class="ticket-empty">
              {{ t("ticket.noMessages") }}
            </div>
          </div>

          <form
            v-if="canReply"
            class="ticket-reply"
            @submit.prevent="submitReply"
          >
            <label for="ticket-reply-body">{{ t("ticket.replyLabel") }}</label>
            <textarea
              id="ticket-reply-body"
              v-model="replyBody"
              rows="4"
              maxlength="10000"
              :placeholder="t('ticket.replyPlaceholder')"
              required
            ></textarea>
            <div>
              <small>{{ Array.from(replyBody).length }} / 10000</small>
              <button
                class="button primary"
                :disabled="replying || !replyBody.trim()"
              >
                <Send />{{ replying ? t("ticket.sending") : t("ticket.send") }}
              </button>
            </div>
          </form>
          <div v-else class="ticket-closed-note">
            <Clock3 />
            <div>
              <b>{{ t("ticket.closed") }}</b>
              <span>{{ t("ticket.closedSub") }}</span>
            </div>
          </div>
        </template>
        <div v-else class="ticket-empty ticket-detail-empty">
          <MessageSquare />
          <b>{{ t("ticket.selectHint") }}</b>
          <span>{{ t("ticket.selectHintSub") }}</span>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.ticket-center {
  display: grid;
  gap: 14px;
}
.ticket-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
}
.ticket-toolbar h2,
.ticket-section-heading h2,
.ticket-detail-header h2 {
  margin: 4px 0;
}
.ticket-toolbar p,
.ticket-section-heading p,
.ticket-detail-header p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.6;
}
.ticket-kicker {
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
  letter-spacing: 0.17em;
}
.ticket-toolbar-actions,
.ticket-form-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.ticket-toolbar-actions svg,
.ticket-form-actions svg,
.ticket-reply button svg {
  width: 13px;
}
.ticket-refresh {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 36px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  padding: 0 11px;
  font-size: 9px;
  cursor: pointer;
}
.ticket-refresh svg {
  width: 13px;
}
.ticket-refresh:disabled {
  opacity: 0.5;
  cursor: wait;
}
.ticket-feedback {
  margin: 0;
  padding: 11px 13px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  font-size: 10px;
}
.ticket-feedback.error {
  border-color: color-mix(in srgb, #c03b35 45%, var(--line));
  color: #b8322c;
}
:global(:root[data-theme="dark"]) .ticket-feedback.error {
  color: #f18d87;
}
.ticket-feedback.notice {
  border-color: color-mix(in srgb, var(--success) 45%, var(--line));
  color: var(--success);
}
.ticket-create-panel {
  display: grid;
  gap: 18px;
}
.ticket-create-panel form,
.ticket-create-panel label {
  display: grid;
  gap: 7px;
}
.ticket-create-panel form {
  gap: 14px;
}
.ticket-create-panel label,
.ticket-reply > label {
  font-size: 9px;
  font-weight: 600;
}
.ticket-form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.ticket-create-panel input,
.ticket-create-panel select,
.ticket-create-panel textarea,
.ticket-reply textarea {
  width: 100%;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--bg);
  color: var(--text);
  padding: 10px;
  font-size: 10px;
  outline: none;
}
.ticket-create-panel input,
.ticket-create-panel select {
  height: 40px;
  padding-top: 0;
  padding-bottom: 0;
}
.ticket-create-panel textarea,
.ticket-reply textarea {
  resize: vertical;
  line-height: 1.7;
}
.ticket-create-panel input:focus,
.ticket-create-panel select:focus,
.ticket-create-panel textarea:focus,
.ticket-reply textarea:focus {
  border-color: var(--text);
}
.ticket-create-panel label > small,
.ticket-reply small {
  color: var(--muted);
  font-size: 8px;
  font-weight: 400;
  text-align: right;
}
.ticket-form-actions {
  justify-content: flex-end;
}
.ticket-workspace {
  display: grid;
  grid-template-columns: minmax(250px, 0.72fr) minmax(0, 1.5fr);
  gap: 14px;
  align-items: start;
}
.ticket-list-panel,
.ticket-detail-panel {
  min-width: 0;
}
.ticket-section-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.ticket-section-heading.compact {
  padding-bottom: 13px;
  border-bottom: 1px solid var(--line);
}
.ticket-section-heading.compact h2 {
  margin: 0;
}
.ticket-section-heading > svg {
  width: 17px;
  color: var(--muted);
}
.ticket-list {
  display: grid;
}
.ticket-card {
  width: 100%;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 9px;
  padding: 13px 0;
  border: 0;
  border-bottom: 1px solid var(--line);
  background: transparent;
  color: var(--text);
  text-align: left;
  cursor: pointer;
}
.ticket-card:last-child {
  border-bottom: 0;
}
.ticket-card > svg {
  width: 15px;
  margin-top: 2px;
}
.ticket-card-copy {
  min-width: 0;
  display: grid;
  gap: 4px;
}
.ticket-card-copy small,
.ticket-card-copy span {
  color: var(--muted);
  font-size: 7px;
}
.ticket-card-copy b {
  overflow: hidden;
  font-size: 9px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ticket-card-state {
  grid-column: 2;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.ticket-card-state em,
.ticket-detail-header > em {
  border: 1px solid var(--line);
  border-radius: 999px;
  padding: 3px 7px;
  color: var(--muted);
  font-size: 7px;
  font-style: normal;
  white-space: nowrap;
}
.ticket-card-state em.status-waiting_user,
.ticket-detail-header > em.status-waiting_user {
  border-color: var(--text);
  color: var(--text);
}
.ticket-card-state em.status-closed,
.ticket-detail-header > em.status-closed {
  opacity: 0.65;
}
.ticket-card-state strong {
  border-radius: 999px;
  background: var(--inverse);
  color: var(--inverse-text);
  padding: 3px 6px;
  font-size: 7px;
  white-space: nowrap;
}
.ticket-card.active {
  margin: 5px 0;
  border: 0;
  border-radius: 6px;
  background: var(--inverse);
  color: var(--inverse-text);
  padding: 12px 10px;
}
.ticket-card.active .ticket-card-copy small,
.ticket-card.active .ticket-card-copy span {
  color: color-mix(in srgb, var(--inverse-text) 62%, transparent);
}
.ticket-card.active .ticket-card-state em {
  border-color: color-mix(in srgb, var(--inverse-text) 28%, transparent);
  color: var(--inverse-text);
}
.ticket-card.active .ticket-card-state strong {
  background: var(--inverse-text);
  color: var(--inverse);
}
.ticket-empty {
  min-height: 150px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 7px;
  color: var(--muted);
  font-size: 9px;
  text-align: center;
}
.ticket-empty svg {
  width: 22px;
}
.ticket-empty b {
  color: var(--text);
  font-size: 10px;
}
.ticket-pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding-top: 12px;
  border-top: 1px solid var(--line);
}
.ticket-pagination span {
  color: var(--muted);
  font-size: 8px;
}
.ticket-pagination button {
  width: 27px;
  height: 27px;
  display: grid;
  place-items: center;
  border: 1px solid var(--line);
  border-radius: 4px;
  background: var(--surface);
  cursor: pointer;
}
.ticket-pagination button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.ticket-pagination svg {
  width: 12px;
}
.ticket-detail-panel {
  padding: 0;
  overflow: hidden;
}
.ticket-detail-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 19px 20px;
  border-bottom: 1px solid var(--line);
}
.ticket-detail-header > div > span {
  color: var(--muted);
  font-size: 7px;
}
.ticket-conversation {
  min-height: 300px;
  max-height: 560px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  overflow-y: auto;
  padding: 20px;
  background: var(--soft);
}
.ticket-message {
  width: min(86%, 610px);
  border: 1px solid var(--line);
  border-radius: 8px 8px 8px 2px;
  background: var(--surface);
  padding: 12px;
}
.ticket-message.user {
  align-self: flex-end;
  border-color: var(--inverse);
  border-radius: 8px 8px 2px 8px;
  background: var(--inverse);
  color: var(--inverse-text);
}
.ticket-message header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.ticket-message header b {
  font-size: 8px;
}
.ticket-message time {
  color: var(--muted);
  font-size: 7px;
}
.ticket-message.user time {
  color: color-mix(in srgb, var(--inverse-text) 58%, transparent);
}
.ticket-message p {
  margin: 8px 0 0;
  overflow-wrap: anywhere;
  font-size: 10px;
  line-height: 1.75;
  white-space: pre-wrap;
}
.ticket-reply {
  display: grid;
  gap: 8px;
  padding: 17px 20px 20px;
  border-top: 1px solid var(--line);
}
.ticket-reply > div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.ticket-closed-note {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 18px 20px;
  border-top: 1px solid var(--line);
  background: var(--soft);
}
.ticket-closed-note > svg {
  width: 17px;
}
.ticket-closed-note > div {
  display: grid;
  gap: 3px;
}
.ticket-closed-note b {
  font-size: 9px;
}
.ticket-closed-note span {
  color: var(--muted);
  font-size: 8px;
}
.ticket-detail-empty {
  min-height: 420px;
}
@media (max-width: 760px) {
  .ticket-workspace {
    grid-template-columns: 1fr;
  }
  .ticket-conversation {
    max-height: none;
  }
}
@media (max-width: 540px) {
  .ticket-toolbar,
  .ticket-detail-header {
    align-items: stretch;
    flex-direction: column;
  }
  .ticket-toolbar-actions {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }
  .ticket-toolbar-actions > button {
    width: 100%;
    justify-content: center;
  }
  .ticket-form-grid {
    grid-template-columns: 1fr;
  }
  .ticket-message {
    width: 94%;
  }
  .ticket-form-actions {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }
}
</style>
