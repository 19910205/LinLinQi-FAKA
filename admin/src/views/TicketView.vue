<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  AlertCircle,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Copy,
  Hash,
  Inbox,
  LoaderCircle,
  MessageSquareText,
  RefreshCw,
  Search,
  Send,
  ShieldCheck,
  StickyNote,
  UserRound,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";

const { t, locale } = useI18n();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("order.manage"));

interface SupportTicket {
  id: string;
  ticket_no: string;
  user_id?: string | null;
  order_id?: string | null;
  email: string;
  category: string;
  subject: string;
  priority: TicketPriority;
  status: TicketStatus;
  assigned_to?: string | null;
  closed_at?: string | null;
  last_message_at?: string | null;
  user_unread: number;
  admin_unread: number;
  created_at: string;
  updated_at: string;
}

interface TicketMessage {
  id: string;
  ticket_id: string;
  author_type: "user" | "admin" | string;
  author_id?: string | null;
  body: string;
  attachments?: string;
  internal: boolean;
  created_at: string;
}

interface TicketPage {
  items: SupportTicket[];
  total: number;
  page: number;
  page_size: number;
}

type TicketStatus =
  "open" | "in_progress" | "waiting_user" | "resolved" | "closed";
type TicketPriority = "low" | "normal" | "high" | "urgent";
type ReplyMode = "public" | "internal";

const statusOptions: Array<{ value: "" | TicketStatus; label: string }> = [
  { value: "", label: t("ticket.all") },
  { value: "open", label: t("ticket.status.open") },
  { value: "in_progress", label: t("ticket.status.in_progress") },
  { value: "waiting_user", label: t("ticket.status.waiting_user") },
  { value: "resolved", label: t("ticket.status.resolved") },
  { value: "closed", label: t("ticket.status.closed") },
];
const statusLabels: Record<TicketStatus, string> = {
  open: t("ticket.status.open"),
  in_progress: t("ticket.status.in_progress"),
  waiting_user: t("ticket.status.waiting_user"),
  resolved: t("ticket.status.resolved"),
  closed: t("ticket.status.closed"),
};
const priorityOptions: Array<{ value: "" | TicketPriority; label: string }> = [
  { value: "", label: t("ticket.allPriority") },
  { value: "low", label: t("ticket.priority.low") },
  { value: "normal", label: t("ticket.priority.normal") },
  { value: "high", label: t("ticket.priority.high") },
  { value: "urgent", label: t("ticket.priority.urgent") },
];
const transitionMap: Record<TicketStatus, TicketStatus[]> = {
  open: ["open", "in_progress", "waiting_user", "resolved", "closed"],
  in_progress: ["in_progress", "open", "waiting_user", "resolved", "closed"],
  waiting_user: ["waiting_user", "open", "in_progress", "resolved", "closed"],
  resolved: ["resolved", "open", "in_progress", "closed"],
  closed: ["closed", "open"],
};

const tickets = ref<SupportTicket[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const statusFilter = ref<"" | TicketStatus>("");
const priorityFilter = ref<"" | TicketPriority>("");
const keyword = ref("");
const selectedID = ref("");
const selectedTicket = ref<SupportTicket | null>(null);
const messages = ref<TicketMessage[]>([]);
const listLoading = ref(false);
const detailLoading = ref(false);
const actionBusy = ref(false);
const listError = ref("");
const detailError = ref("");
const actionError = ref("");
const notice = ref("");
const replyMode = ref<ReplyMode>("public");
const replyBody = ref("");
const replyStatus = ref<TicketStatus>("waiting_user");
const replyReason = ref("");
const updateStatus = ref<TicketStatus>("open");
const updatePriority = ref<TicketPriority>("normal");
const assigneeInput = ref("");
const updateReason = ref("");
const messagePane = ref<HTMLElement | null>(null);
let listRequest = 0;
let detailRequest = 0;

const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const visibleTickets = computed(() => {
  const query = keyword.value.trim().toLowerCase();
  return tickets.value.filter((ticket) => {
    if (priorityFilter.value && ticket.priority !== priorityFilter.value)
      return false;
    if (!query) return true;
    return [
      ticket.ticket_no,
      ticket.email,
      ticket.subject,
      ticket.category,
      ticket.order_id || "",
    ].some((value) => value.toLowerCase().includes(query));
  });
});
const pageNumbers = computed(() => {
  const start = Math.max(1, Math.min(page.value - 2, totalPages.value - 4));
  const end = Math.min(totalPages.value, start + 4);
  return Array.from({ length: end - start + 1 }, (_, index) => start + index);
});
const allowedStatuses = computed(() => {
  const status = selectedTicket.value?.status;
  return status ? transitionMap[status] : [];
});
const replyStatusOptions = computed(() =>
  allowedStatuses.value.map((status) => ({
    value: status,
    label: statusLabels[status],
  })),
);
const canSendReply = computed(
  () =>
    canManage.value &&
    Boolean(selectedTicket.value) &&
    replyBody.value.trim().length > 0 &&
    replyBody.value.trim().length <= 10000 &&
    replyReason.value.trim().length >= 3 &&
    !actionBusy.value,
);

function apiMessage(error: unknown, fallback: string) {
  const failure = error as { response?: { data?: { message?: string } } };
  return failure.response?.data?.message || fallback;
}

function statusLabel(status: string) {
  const key = `ticket.status.${status}`;
  return t(key) === key ? status || t("ticket.unknown") : t(key);
}

function priorityLabel(priority: string) {
  const key = `ticket.priority.${priority}`;
  return t(key) === key ? priority || t("ticket.unknown") : t(key);
}

function formatTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString(locale.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

function isUUID(value: string) {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(
    value,
  );
}

function resetActionMessages() {
  actionError.value = "";
  notice.value = "";
}

function setEditorState(ticket: SupportTicket) {
  updateStatus.value = ticket.status;
  updatePriority.value = ticket.priority;
  assigneeInput.value = "";
  updateReason.value = "";
  const preferred: Partial<Record<TicketStatus, TicketStatus>> = {
    open: "waiting_user",
    in_progress: "waiting_user",
    waiting_user: "waiting_user",
    resolved: "in_progress",
    closed: "open",
  };
  const target = preferred[ticket.status] || ticket.status;
  replyStatus.value = transitionMap[ticket.status].includes(target)
    ? target
    : ticket.status;
}

async function scrollMessages() {
  await nextTick();
  if (messagePane.value)
    messagePane.value.scrollTop = messagePane.value.scrollHeight;
}

async function selectTicket(id: string, preserveActionMessages = false) {
  if (!id) return;
  const changedTicket = selectedID.value !== id;
  const request = ++detailRequest;
  selectedID.value = id;
  detailLoading.value = true;
  detailError.value = "";
  if (!preserveActionMessages) resetActionMessages();
  if (changedTicket) {
    replyBody.value = "";
    replyReason.value = "";
  }
  try {
    const { data } = await adminApi.get(`/tickets/${encodeURIComponent(id)}`);
    if (request !== detailRequest) return;
    const payload = data.data as {
      ticket: SupportTicket;
      messages: TicketMessage[];
    };
    selectedTicket.value = payload.ticket;
    messages.value = Array.isArray(payload.messages) ? payload.messages : [];
    setEditorState(payload.ticket);
    const listed = tickets.value.find((ticket) => ticket.id === id);
    if (listed) listed.admin_unread = 0;
    await scrollMessages();
  } catch (error: unknown) {
    if (request !== detailRequest) return;
    selectedTicket.value = null;
    messages.value = [];
    detailError.value = apiMessage(error, t("ticket.errDetail"));
  } finally {
    if (request === detailRequest) detailLoading.value = false;
  }
}

function clearSelection() {
  detailRequest += 1;
  selectedID.value = "";
  selectedTicket.value = null;
  messages.value = [];
  detailError.value = "";
}

async function loadTickets(
  preferredID = selectedID.value,
  preserveActionMessages = false,
) {
  const request = ++listRequest;
  listLoading.value = true;
  listError.value = "";
  try {
    const { data } = await adminApi.get("/operations/tickets", {
      params: {
        page: page.value,
        page_size: pageSize.value,
        ...(statusFilter.value ? { status: statusFilter.value } : {}),
      },
    });
    if (request !== listRequest) return;
    const payload = data.data as TicketPage;
    tickets.value = Array.isArray(payload.items) ? payload.items : [];
    total.value = Number(payload.total || 0);
    page.value = Number(payload.page || page.value);
    pageSize.value = Number(payload.page_size || pageSize.value);
    if (page.value > totalPages.value && page.value > 1) {
      page.value = totalPages.value;
      await loadTickets(preferredID, preserveActionMessages);
      return;
    }
    const target =
      visibleTickets.value.find((ticket) => ticket.id === preferredID) ||
      visibleTickets.value[0];
    if (target) await selectTicket(target.id, preserveActionMessages);
    else clearSelection();
  } catch (error: unknown) {
    if (request !== listRequest) return;
    tickets.value = [];
    total.value = 0;
    clearSelection();
    listError.value = apiMessage(error, t("ticket.errList"));
  } finally {
    if (request === listRequest) listLoading.value = false;
  }
}

async function setStatusFilter(status: "" | TicketStatus) {
  if (statusFilter.value === status && !listError.value) return;
  statusFilter.value = status;
  page.value = 1;
  await loadTickets("");
}

async function changePage(target: number) {
  if (target < 1 || target > totalPages.value || target === page.value) return;
  page.value = target;
  await loadTickets("");
}

async function changePageSize() {
  page.value = 1;
  await loadTickets("");
}

function syncSelectionToClientFilters() {
  const currentVisible = visibleTickets.value.some(
    (ticket) => ticket.id === selectedID.value,
  );
  if (currentVisible) return;
  const target = visibleTickets.value[0];
  if (target) void selectTicket(target.id);
  else clearSelection();
}

async function sendReply() {
  if (!canManage.value) return;
  if (!selectedTicket.value || !canSendReply.value) return;
  resetActionMessages();
  actionBusy.value = true;
  const ticketID = selectedTicket.value.id;
  try {
    const internal = replyMode.value === "internal";
    await adminApi.post(
      `/tickets/${encodeURIComponent(ticketID)}/messages`,
      {
        body: replyBody.value.trim(),
        internal,
        ...(!internal ? { status: replyStatus.value } : {}),
      },
      { headers: { "X-Change-Reason": replyReason.value.trim() } },
    );
    replyBody.value = "";
    replyReason.value = "";
    await loadTickets(ticketID, true);
    notice.value = internal ? t("ticket.noteSaved") : t("ticket.replySent");
  } catch (error: unknown) {
    actionError.value = apiMessage(error, t("ticket.errSend"));
  } finally {
    actionBusy.value = false;
  }
}

async function updateTicket() {
  if (!canManage.value) return;
  const ticket = selectedTicket.value;
  if (!ticket) return;
  resetActionMessages();
  if (updateReason.value.trim().length < 3) {
    actionError.value = t("ticket.errReasonShort");
    return;
  }
  const payload: Record<string, string> = {};
  if (updateStatus.value !== ticket.status) payload.status = updateStatus.value;
  if (updatePriority.value !== ticket.priority)
    payload.priority = updatePriority.value;
  const assignee = assigneeInput.value.trim();
  if (assignee && assignee !== ticket.assigned_to) {
    if (!isUUID(assignee)) {
      actionError.value = t("ticket.errAssignee");
      return;
    }
    payload.assigned_to = assignee;
  }
  if (!Object.keys(payload).length) {
    actionError.value = t("ticket.errNoChange");
    return;
  }
  actionBusy.value = true;
  try {
    await adminApi.patch(`/tickets/${encodeURIComponent(ticket.id)}`, payload, {
      headers: { "X-Change-Reason": updateReason.value.trim() },
    });
    await loadTickets(ticket.id, true);
    notice.value = t("ticket.updated");
  } catch (error: unknown) {
    actionError.value = apiMessage(error, t("ticket.errUpdate"));
  } finally {
    actionBusy.value = false;
  }
}

async function copyText(value?: string | null) {
  if (!value) return;
  try {
    await navigator.clipboard.writeText(value);
    notice.value = t("ticket.copied");
  } catch {
    actionError.value = t("ticket.errClipboard");
  }
}

watch(replyMode, () => {
  actionError.value = "";
});

onMounted(() => {
  void loadTickets("");
});
</script>

<template>
  <section class="ticket-shell">
    <header class="ticket-toolbar panel">
      <div
        class="ticket-status-tabs"
        role="tablist"
        :aria-label="t('ticket.filterAria')"
      >
        <button
          v-for="option in statusOptions"
          :key="option.value || 'all'"
          type="button"
          :class="{ active: statusFilter === option.value }"
          :aria-selected="statusFilter === option.value"
          role="tab"
          @click="setStatusFilter(option.value)"
        >
          {{ option.label }}
          <span v-if="option.value === statusFilter">{{ total }}</span>
        </button>
      </div>
      <button
        class="ticket-icon-button"
        type="button"
        :title="t('ticket.refreshTitle')"
        :disabled="listLoading"
        @click="loadTickets()"
      >
        <RefreshCw :size="15" :class="{ spinning: listLoading }" />
      </button>
    </header>

    <div class="ticket-workspace">
      <aside class="ticket-list panel">
        <header>
          <div>
            <h2>{{ t("ticket.queue") }}</h2>
            <p>{{ t("ticket.paged", { n: total }) }}</p>
          </div>
          <Inbox :size="18" />
        </header>
        <div class="ticket-list-filters">
          <label class="ticket-search">
            <Search :size="14" />
            <input
              v-model="keyword"
              type="search"
              :placeholder="t('ticket.filterPlaceholder')"
              :aria-label="t('ticket.filterAria2')"
              @input="syncSelectionToClientFilters"
            />
          </label>
          <select
            v-model="priorityFilter"
            :aria-label="t('ticket.priorityFilterAria')"
            @change="syncSelectionToClientFilters"
          >
            <option
              v-for="option in priorityOptions"
              :key="option.value || 'all'"
              :value="option.value"
            >
              {{ option.label }}
            </option>
          </select>
        </div>

        <div v-if="listError" class="ticket-state error-state">
          <AlertCircle :size="20" />
          <span>{{ listError }}</span>
          <button type="button" @click="loadTickets()">
            {{ t("ticket.retry") }}
          </button>
        </div>
        <div v-else-if="listLoading && !tickets.length" class="ticket-state">
          <LoaderCircle class="spinning" :size="20" />
          <span>{{ t("ticket.loading") }}</span>
        </div>
        <div v-else-if="!visibleTickets.length" class="ticket-state">
          <Inbox :size="22" />
          <span>{{
            tickets.length ? t("ticket.noMatchPage") : t("ticket.noTickets")
          }}</span>
        </div>
        <div v-else class="ticket-list-scroll">
          <button
            v-for="ticket in visibleTickets"
            :key="ticket.id"
            type="button"
            class="ticket-list-item"
            :class="{ selected: selectedID === ticket.id }"
            @click="selectTicket(ticket.id)"
          >
            <span class="ticket-list-head">
              <b>{{ ticket.ticket_no }}</b>
              <em class="priority-dot" :class="`priority-${ticket.priority}`">{{
                priorityLabel(ticket.priority)
              }}</em>
            </span>
            <strong>{{ ticket.subject }}</strong>
            <span class="ticket-list-meta">
              <span>{{ ticket.email }}</span>
              <time>{{
                formatTime(ticket.last_message_at || ticket.created_at)
              }}</time>
            </span>
            <span class="ticket-list-foot">
              <span class="status-chip" :class="`status-${ticket.status}`">
                {{ statusLabel(ticket.status) }}
              </span>
              <span v-if="ticket.admin_unread" class="unread-chip">
                {{ t("ticket.unread", { n: ticket.admin_unread }) }}
              </span>
            </span>
          </button>
        </div>

        <footer class="ticket-pagination">
          <div>
            <button
              type="button"
              :aria-label="t('ticket.prev')"
              :disabled="page <= 1 || listLoading"
              @click="changePage(page - 1)"
            >
              <ChevronLeft :size="14" />
            </button>
            <button
              v-for="number in pageNumbers"
              :key="number"
              type="button"
              :class="{ active: number === page }"
              :disabled="listLoading"
              @click="changePage(number)"
            >
              {{ number }}
            </button>
            <button
              type="button"
              :aria-label="t('ticket.next')"
              :disabled="page >= totalPages || listLoading"
              @click="changePage(page + 1)"
            >
              <ChevronRight :size="14" />
            </button>
          </div>
          <select
            v-model.number="pageSize"
            :aria-label="t('ticket.pageSize')"
            @change="changePageSize"
          >
            <option :value="10">{{ t("ticket.perPage", { n: 10 }) }}</option>
            <option :value="20">{{ t("ticket.perPage", { n: 20 }) }}</option>
            <option :value="50">{{ t("ticket.perPage", { n: 50 }) }}</option>
          </select>
        </footer>
      </aside>

      <main class="ticket-conversation panel">
        <template v-if="selectedTicket">
          <header class="conversation-head">
            <div>
              <span class="conversation-kicker">
                <Hash :size="12" />{{ selectedTicket.ticket_no }}
              </span>
              <h2>{{ selectedTicket.subject }}</h2>
              <p>
                {{ selectedTicket.email }} ·
                {{ selectedTicket.category || t("ticket.uncategorized") }}
              </p>
            </div>
            <span
              class="status-chip"
              :class="`status-${selectedTicket.status}`"
            >
              {{ statusLabel(selectedTicket.status) }}
            </span>
          </header>

          <div ref="messagePane" class="message-pane">
            <div v-if="detailLoading" class="ticket-state conversation-state">
              <LoaderCircle class="spinning" :size="20" />
              <span>{{ t("ticket.loadingSession") }}</span>
            </div>
            <div
              v-else-if="detailError"
              class="ticket-state conversation-state error-state"
            >
              <AlertCircle :size="20" />
              <span>{{ detailError }}</span>
              <button type="button" @click="selectTicket(selectedID)">
                {{ t("ticket.retry") }}
              </button>
            </div>
            <div
              v-else-if="!messages.length"
              class="ticket-state conversation-state"
            >
              <MessageSquareText :size="22" />
              <span>{{ t("ticket.noMessages") }}</span>
            </div>
            <template v-else>
              <article
                v-for="message in messages"
                :key="message.id"
                class="ticket-message"
                :class="{
                  admin: message.author_type === 'admin',
                  internal: message.internal,
                }"
              >
                <div class="message-avatar">
                  <StickyNote v-if="message.internal" :size="14" />
                  <ShieldCheck
                    v-else-if="message.author_type === 'admin'"
                    :size="14"
                  />
                  <UserRound v-else :size="14" />
                </div>
                <div class="message-content">
                  <header>
                    <b>
                      {{
                        message.internal
                          ? t("ticket.internalNote")
                          : message.author_type === "admin"
                            ? t("ticket.agent")
                            : t("ticket.user")
                      }}
                    </b>
                    <span v-if="message.internal">{{
                      t("ticket.adminOnly")
                    }}</span>
                    <time>{{ formatTime(message.created_at) }}</time>
                  </header>
                  <p>{{ message.body }}</p>
                </div>
              </article>
            </template>
          </div>

          <form
            v-if="canManage"
            class="ticket-composer"
            @submit.prevent="sendReply"
          >
            <div class="composer-tabs">
              <button
                type="button"
                :class="{ active: replyMode === 'public' }"
                @click="replyMode = 'public'"
              >
                <MessageSquareText :size="14" />{{ t("ticket.publicReply") }}
              </button>
              <button
                type="button"
                :class="{ active: replyMode === 'internal' }"
                @click="replyMode = 'internal'"
              >
                <StickyNote :size="14" />{{ t("ticket.internalReply") }}
              </button>
            </div>
            <textarea
              v-model="replyBody"
              maxlength="10000"
              :placeholder="
                replyMode === 'internal'
                  ? t('ticket.replyInternalPlaceholder')
                  : t('ticket.replyPublicPlaceholder')
              "
              :aria-label="t('ticket.replyAria')"
            ></textarea>
            <p v-if="actionError" class="composer-alert error-message">
              <AlertCircle :size="13" />{{ actionError }}
            </p>
            <p v-else-if="notice" class="composer-alert notice-message">
              <CheckCircle2 :size="13" />{{ notice }}
            </p>
            <div class="composer-fields">
              <label v-if="replyMode === 'public'">
                {{ t("ticket.replyStatus") }}
                <select v-model="replyStatus">
                  <option
                    v-for="option in replyStatusOptions"
                    :key="option.value"
                    :value="option.value"
                  >
                    {{ option.label }}
                  </option>
                </select>
              </label>
              <label class="reason-field">
                {{ t("ticket.auditReason") }}
                <input
                  v-model="replyReason"
                  maxlength="500"
                  :placeholder="t('ticket.auditReasonPlaceholder')"
                />
              </label>
              <span class="character-count"
                >{{ replyBody.trim().length }} / 10000</span
              >
              <button
                class="send-button"
                type="submit"
                :disabled="!canSendReply"
              >
                <LoaderCircle v-if="actionBusy" class="spinning" :size="14" />
                <Send v-else :size="14" />
                {{
                  replyMode === "internal"
                    ? t("ticket.saveNote")
                    : t("ticket.sendReply")
                }}
              </button>
            </div>
          </form>
        </template>
        <div v-else-if="detailLoading" class="ticket-state empty-conversation">
          <LoaderCircle class="spinning" :size="22" />
          <span>{{ t("ticket.loadingSessionDetail") }}</span>
        </div>
        <div
          v-else-if="detailError"
          class="ticket-state empty-conversation error-state"
        >
          <AlertCircle :size="24" />
          <strong>{{ t("ticket.sessionLoadFailed") }}</strong>
          <span>{{ detailError }}</span>
          <button type="button" @click="selectTicket(selectedID)">
            {{ t("ticket.retry") }}
          </button>
        </div>
        <div v-else class="ticket-state empty-conversation">
          <MessageSquareText :size="28" />
          <strong>{{ t("ticket.selectTicket") }}</strong>
          <span>{{ t("ticket.selectTicketSub") }}</span>
        </div>
      </main>

      <aside class="ticket-inspector panel">
        <template v-if="selectedTicket">
          <header>
            <div>
              <h2>{{ t("ticket.properties") }}</h2>
              <p>{{ t("ticket.propertiesSub") }}</p>
            </div>
            <CheckCircle2 :size="18" />
          </header>

          <div v-if="notice" class="action-message notice-message">
            <CheckCircle2 :size="15" />{{ notice }}
          </div>
          <div v-if="actionError" class="action-message error-message">
            <AlertCircle :size="15" />{{ actionError }}
          </div>

          <dl class="ticket-facts">
            <div>
              <dt>{{ t("ticket.colLabel.createdAt") }}</dt>
              <dd>
                <Clock3 :size="13" />{{ formatTime(selectedTicket.created_at) }}
              </dd>
            </div>
            <div>
              <dt>{{ t("ticket.colLabel.userEmail") }}</dt>
              <dd>{{ selectedTicket.email }}</dd>
            </div>
            <div>
              <dt>{{ t("ticket.colLabel.userUuid") }}</dt>
              <dd>
                <code>{{
                  selectedTicket.user_id || t("ticket.anonymous")
                }}</code>
                <button
                  v-if="selectedTicket.user_id"
                  type="button"
                  :title="t('ticket.copyUserUuid')"
                  @click="copyText(selectedTicket.user_id)"
                >
                  <Copy :size="12" />
                </button>
              </dd>
            </div>
            <div>
              <dt>{{ t("ticket.colLabel.orderUuid") }}</dt>
              <dd>
                <code>{{
                  selectedTicket.order_id || t("ticket.noOrder")
                }}</code>
                <button
                  v-if="selectedTicket.order_id"
                  type="button"
                  :title="t('ticket.copyOrderUuid')"
                  @click="copyText(selectedTicket.order_id)"
                >
                  <Copy :size="12" />
                </button>
              </dd>
            </div>
            <div>
              <dt>{{ t("ticket.colLabel.currentAssignee") }}</dt>
              <dd>
                <code>{{
                  selectedTicket.assigned_to || t("ticket.unassigned")
                }}</code>
              </dd>
            </div>
          </dl>

          <form
            v-if="canManage"
            class="ticket-update-form"
            @submit.prevent="updateTicket"
          >
            <label>
              {{ t("ticket.colLabel.status") }}
              <select v-model="updateStatus">
                <option
                  v-for="status in allowedStatuses"
                  :key="status"
                  :value="status"
                >
                  {{ statusLabel(status) }}
                </option>
              </select>
            </label>
            <label>
              {{ t("ticket.colLabel.priority") }}
              <select v-model="updatePriority">
                <option
                  v-for="option in priorityOptions.slice(1)"
                  :key="option.value"
                  :value="option.value"
                >
                  {{ option.label }}
                </option>
              </select>
            </label>
            <label>
              {{ t("ticket.assigneeUuid") }}
              <input
                v-model="assigneeInput"
                maxlength="36"
                :placeholder="t('ticket.assigneePlaceholder')"
                autocomplete="off"
              />
              <small>{{ t("ticket.assigneeHint") }}</small>
            </label>
            <label>
              {{ t("ticket.auditReason") }}
              <textarea
                v-model="updateReason"
                maxlength="500"
                :placeholder="t('ticket.updateReasonPlaceholder')"
              ></textarea>
            </label>
            <button class="primary-button" type="submit" :disabled="actionBusy">
              <LoaderCircle v-if="actionBusy" class="spinning" :size="14" />
              <CheckCircle2 v-else :size="14" />{{ t("ticket.submitUpdate") }}
            </button>
          </form>
        </template>
        <div v-else class="ticket-state inspector-empty">
          <UserRound :size="24" />
          <span>{{ t("ticket.inspectorEmpty") }}</span>
        </div>
      </aside>
    </div>
  </section>
</template>

<style scoped>
.ticket-shell {
  display: grid;
  gap: 12px;
}

.ticket-toolbar {
  min-height: 54px;
  padding: 0 10px 0 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  overflow: hidden;
}

.ticket-status-tabs {
  min-width: 0;
  align-self: stretch;
  display: flex;
  align-items: end;
  gap: 2px;
  overflow-x: auto;
}

.ticket-status-tabs button {
  height: 40px;
  padding: 0 11px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--muted);
  font-size: 9px;
  white-space: nowrap;
}

.ticket-status-tabs button.active {
  color: var(--text);
  border-bottom-color: var(--text);
}

.ticket-status-tabs span {
  margin-left: 4px;
  padding: 2px 5px;
  border-radius: 8px;
  background: var(--soft);
  font-size: 7px;
}

.ticket-icon-button {
  width: 32px;
  height: 32px;
  flex: 0 0 auto;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: grid;
  place-items: center;
  background: var(--surface);
}

.ticket-icon-button:disabled,
.ticket-pagination button:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.ticket-workspace {
  min-height: 690px;
  display: grid;
  grid-template-columns: minmax(290px, 350px) minmax(430px, 1fr) minmax(
      260px,
      300px
    );
  gap: 12px;
  align-items: stretch;
}

.ticket-list,
.ticket-conversation,
.ticket-inspector {
  min-width: 0;
  overflow: hidden;
}

.ticket-list,
.ticket-conversation {
  display: flex;
  flex-direction: column;
  height: min(760px, calc(100vh - 190px));
  min-height: 620px;
}

.ticket-list > header,
.ticket-inspector > header,
.conversation-head {
  min-height: 62px;
  padding: 12px 15px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.ticket-list h2,
.ticket-inspector h2,
.conversation-head h2 {
  margin: 0;
  color: var(--text);
  font-size: 12px;
}

.ticket-list header p,
.ticket-inspector header p,
.conversation-head p {
  margin: 4px 0 0;
  color: var(--muted);
  font-size: 8px;
}

.ticket-list > header > svg,
.ticket-inspector > header > svg {
  color: var(--muted);
}

.ticket-list-filters {
  padding: 10px;
  border-bottom: 1px solid var(--line);
  display: grid;
  grid-template-columns: minmax(0, 1fr) 108px;
  gap: 7px;
}

.ticket-search {
  height: 33px;
  min-width: 0;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--muted);
  background: var(--surface-2);
}

.ticket-search input {
  width: 100%;
  min-width: 0;
  border: 0;
  outline: 0;
  background: transparent;
  font-size: 9px;
}

.ticket-list-filters select,
.ticket-pagination select,
.composer-fields select,
.ticket-update-form select,
.ticket-update-form input,
.composer-fields input {
  border: 1px solid var(--line);
  border-radius: 5px;
  outline: none;
  background: var(--surface-2);
  color: var(--text);
}

.ticket-list-filters select {
  width: 100%;
  padding: 0 8px;
  font-size: 8px;
}

.ticket-list-scroll {
  min-height: 0;
  flex: 1;
  overflow-y: auto;
}

.ticket-list-item {
  width: 100%;
  padding: 13px 14px;
  border: 0;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
  color: var(--text);
  display: flex;
  flex-direction: column;
  gap: 7px;
  text-align: left;
}

.ticket-list-item:hover {
  background: var(--surface-2);
}

.ticket-list-item.selected {
  background: color-mix(in srgb, var(--text) 5%, var(--surface));
  box-shadow: inset 2px 0 var(--text);
}

.ticket-list-head,
.ticket-list-meta,
.ticket-list-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.ticket-list-head b {
  font:
    600 8px ui-monospace,
    SFMono-Regular,
    Menlo,
    monospace;
  color: var(--muted);
}

.ticket-list-item > strong {
  overflow: hidden;
  font-size: 10px;
  line-height: 1.45;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ticket-list-meta {
  color: var(--muted);
  font-size: 7px;
}

.ticket-list-meta > span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ticket-list-meta time {
  flex: 0 0 auto;
}

.priority-dot,
.status-chip,
.unread-chip {
  width: fit-content;
  padding: 3px 6px;
  border-radius: 10px;
  font-style: normal;
  font-size: 7px;
  font-weight: 600;
}

.priority-dot {
  background: var(--soft);
  color: var(--muted);
}

.priority-high,
.priority-urgent {
  background: color-mix(in srgb, var(--danger) 11%, transparent);
  color: var(--danger);
}

.priority-low {
  color: var(--success);
}

.status-chip {
  background: var(--soft);
  color: var(--muted);
}

.status-open,
.status-urgent {
  background: color-mix(in srgb, var(--danger) 10%, transparent);
  color: var(--danger);
}

.status-in_progress {
  background: color-mix(in srgb, var(--warn) 12%, transparent);
  color: var(--warn);
}

.status-waiting_user {
  background: color-mix(in srgb, var(--text) 7%, transparent);
  color: var(--text);
}

.status-resolved {
  background: color-mix(in srgb, var(--success) 11%, transparent);
  color: var(--success);
}

.unread-chip {
  background: var(--dark);
  color: var(--dark-text);
}

.ticket-pagination {
  min-height: 51px;
  padding: 8px 10px;
  border-top: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.ticket-pagination > div {
  min-width: 0;
  display: flex;
  gap: 3px;
}

.ticket-pagination button {
  min-width: 26px;
  height: 26px;
  padding: 0 6px;
  border: 1px solid var(--line);
  border-radius: 4px;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--muted);
  font-size: 8px;
}

.ticket-pagination button.active {
  background: var(--dark);
  color: var(--dark-text);
}

.ticket-pagination select {
  height: 27px;
  padding: 0 4px;
  font-size: 7px;
}

.conversation-head {
  flex: 0 0 auto;
  min-height: 78px;
  padding: 13px 17px;
}

.conversation-head > div {
  min-width: 0;
}

.conversation-head h2 {
  margin-top: 5px;
  overflow: hidden;
  font-size: 14px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.conversation-kicker {
  display: flex;
  align-items: center;
  gap: 3px;
  color: var(--muted);
  font:
    600 8px ui-monospace,
    SFMono-Regular,
    Menlo,
    monospace;
}

.message-pane {
  min-height: 0;
  flex: 1;
  padding: 20px 18px;
  overflow-y: auto;
  background:
    linear-gradient(var(--surface-2), var(--surface-2)) padding-box,
    repeating-linear-gradient(
      0deg,
      transparent 0,
      transparent 31px,
      color-mix(in srgb, var(--line) 40%, transparent) 32px
    );
}

.ticket-message {
  max-width: 82%;
  margin-bottom: 15px;
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.ticket-message.admin {
  margin-left: auto;
  flex-direction: row-reverse;
}

.ticket-message.internal {
  max-width: 94%;
  margin-right: auto;
  margin-left: auto;
}

.message-avatar {
  width: 28px;
  height: 28px;
  flex: 0 0 auto;
  border: 1px solid var(--line);
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--muted);
}

.message-content {
  min-width: 0;
  padding: 10px 11px;
  border: 1px solid var(--line);
  border-radius: 4px 10px 10px 10px;
  background: var(--surface);
  box-shadow: 0 4px 14px color-mix(in srgb, var(--text) 4%, transparent);
}

.ticket-message.admin .message-content {
  border-radius: 10px 4px 10px 10px;
  background: var(--dark);
  color: var(--dark-text);
}

.ticket-message.internal .message-content {
  width: 100%;
  border-style: dashed;
  border-color: color-mix(in srgb, var(--warn) 45%, var(--line));
  border-radius: 7px;
  background: color-mix(in srgb, var(--warn) 7%, var(--surface));
  color: var(--text);
  box-shadow: none;
}

.message-content header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 8px;
}

.message-content header b {
  font-size: 8px;
}

.message-content header span {
  padding: 2px 5px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--warn) 13%, transparent);
  color: var(--warn);
  font-size: 7px;
}

.message-content header time {
  margin-left: auto;
  color: var(--muted);
  font-size: 7px;
}

.ticket-message.admin:not(.internal) .message-content header time {
  color: color-mix(in srgb, var(--dark-text) 58%, transparent);
}

.message-content p {
  margin: 7px 0 0;
  overflow-wrap: anywhere;
  font-size: 10px;
  line-height: 1.7;
  white-space: pre-wrap;
}

.ticket-composer {
  flex: 0 0 auto;
  padding: 11px 13px 13px;
  border-top: 1px solid var(--line);
  background: var(--surface);
}

.composer-tabs {
  margin-bottom: 8px;
  display: flex;
  gap: 4px;
}

.composer-tabs button {
  height: 27px;
  padding: 0 9px;
  border: 1px solid transparent;
  border-radius: 5px;
  display: flex;
  align-items: center;
  gap: 5px;
  background: transparent;
  color: var(--muted);
  font-size: 8px;
}

.composer-tabs button.active {
  border-color: var(--line);
  background: var(--surface-2);
  color: var(--text);
}

.ticket-composer > textarea {
  width: 100%;
  min-height: 76px;
  max-height: 180px;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  resize: vertical;
  outline: none;
  background: var(--surface-2);
  color: var(--text);
  font-size: 10px;
  line-height: 1.6;
}

.ticket-composer > textarea:focus,
.ticket-update-form input:focus,
.ticket-update-form textarea:focus,
.composer-fields input:focus {
  border-color: var(--text);
}

.composer-alert {
  margin: 7px 0 0;
  padding: 7px 9px;
  border-radius: 5px;
  display: flex;
  align-items: flex-start;
  gap: 6px;
  font-size: 8px;
  line-height: 1.5;
}

.composer-fields {
  margin-top: 8px;
  display: grid;
  grid-template-columns: 110px minmax(150px, 1fr) auto auto;
  align-items: end;
  gap: 7px;
}

.composer-fields label,
.ticket-update-form label {
  display: flex;
  flex-direction: column;
  gap: 5px;
  color: var(--muted);
  font-size: 8px;
  font-weight: 600;
}

.composer-fields select,
.composer-fields input {
  width: 100%;
  height: 31px;
  padding: 0 8px;
  font-size: 8px;
}

.composer-fields .reason-field:first-child:last-of-type,
.composer-fields .reason-field {
  min-width: 0;
}

.character-count {
  padding-bottom: 9px;
  color: var(--muted);
  font-size: 7px;
  white-space: nowrap;
}

.send-button {
  height: 31px;
  padding: 0 11px;
  border: 0;
  border-radius: 5px;
  display: flex;
  align-items: center;
  gap: 6px;
  background: var(--dark);
  color: var(--dark-text);
  font-size: 8px;
  font-weight: 600;
}

.send-button:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.ticket-inspector {
  align-self: start;
}

.action-message {
  margin: 10px 12px 0;
  padding: 9px;
  border-radius: 5px;
  display: flex;
  align-items: flex-start;
  gap: 6px;
  font-size: 8px;
  line-height: 1.5;
}

.notice-message {
  background: color-mix(in srgb, var(--success) 9%, transparent);
  color: var(--success);
}

.error-message,
.error-state {
  color: var(--danger) !important;
}

.error-message {
  background: color-mix(in srgb, var(--danger) 9%, transparent);
}

.ticket-facts {
  margin: 0;
  padding: 2px 14px 12px;
}

.ticket-facts > div {
  padding: 11px 0;
  border-bottom: 1px solid var(--line);
}

.ticket-facts dt {
  margin-bottom: 5px;
  color: var(--muted);
  font-size: 7px;
}

.ticket-facts dd {
  min-width: 0;
  margin: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  font-size: 8px;
}

.ticket-facts code {
  min-width: 0;
  overflow: hidden;
  color: var(--text);
  font:
    500 7px ui-monospace,
    SFMono-Regular,
    Menlo,
    monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ticket-facts button {
  width: 24px;
  height: 24px;
  flex: 0 0 auto;
  border: 1px solid var(--line);
  border-radius: 4px;
  display: grid;
  place-items: center;
  background: var(--surface);
  color: var(--muted);
}

.ticket-update-form {
  padding: 2px 14px 15px;
}

.ticket-update-form label {
  margin-bottom: 11px;
}

.ticket-update-form select,
.ticket-update-form input,
.ticket-update-form textarea {
  width: 100%;
  padding: 8px;
  border: 1px solid var(--line);
  border-radius: 5px;
  outline: none;
  background: var(--surface-2);
  color: var(--text);
  font-size: 8px;
}

.ticket-update-form textarea {
  min-height: 70px;
  resize: vertical;
  line-height: 1.5;
}

.ticket-update-form small {
  color: var(--muted);
  font-size: 7px;
  font-weight: 400;
  line-height: 1.5;
}

.ticket-update-form .primary-button {
  width: 100%;
  height: 35px;
  font-size: 9px;
}

.ticket-state {
  min-height: 160px;
  flex: 1;
  padding: 20px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--muted);
  text-align: center;
  font-size: 9px;
}

.ticket-state button {
  height: 27px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 4px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}

.conversation-state {
  min-height: 260px;
}

.empty-conversation {
  min-height: 100%;
}

.empty-conversation strong {
  color: var(--text);
  font-size: 11px;
}

.inspector-empty {
  min-height: 300px;
}

.spinning {
  animation: ticket-spin 0.8s linear infinite;
}

@keyframes ticket-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 1260px) {
  .ticket-workspace {
    grid-template-columns: minmax(280px, 330px) minmax(430px, 1fr);
  }

  .ticket-inspector {
    grid-column: 1 / -1;
    display: grid;
    grid-template-columns: minmax(230px, 0.7fr) minmax(300px, 1fr);
  }

  .ticket-inspector > header {
    grid-column: 1 / -1;
  }

  .ticket-facts {
    border-right: 1px solid var(--line);
  }

  .action-message {
    grid-column: 1 / -1;
  }
}

@media (max-width: 860px) {
  .ticket-workspace {
    grid-template-columns: 1fr;
  }

  .ticket-list,
  .ticket-conversation {
    height: auto;
    min-height: 560px;
  }

  .ticket-list-scroll {
    max-height: 430px;
  }

  .ticket-conversation {
    min-height: 680px;
  }

  .ticket-inspector {
    grid-column: auto;
  }
}

@media (max-width: 600px) {
  .ticket-toolbar {
    align-items: stretch;
  }

  .ticket-icon-button {
    align-self: center;
  }

  .ticket-list-filters {
    grid-template-columns: 1fr;
  }

  .conversation-head {
    align-items: flex-start;
  }

  .ticket-message,
  .ticket-message.internal {
    max-width: 96%;
  }

  .composer-fields {
    grid-template-columns: 1fr;
  }

  .character-count {
    padding: 0;
  }

  .send-button {
    justify-content: center;
  }

  .ticket-inspector {
    display: block;
  }

  .ticket-facts {
    border-right: 0;
  }
}
</style>
