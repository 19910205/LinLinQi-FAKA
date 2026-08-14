<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import {
  AlertCircle,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleGauge,
  Clock3,
  Eye,
  Layers3,
  LoaderCircle,
  Play,
  RefreshCw,
  RotateCw,
  Search,
  ServerCog,
  TimerReset,
  TriangleAlert,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import { useI18n } from "vue-i18n";

const { t } = useI18n();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("system.manage"));

interface JobRecord {
  id: string;
  task_id: string;
  task_type: string;
  queue: string;
  status: string;
  attempts: number;
  payload: string;
  last_error: string;
  scheduled_at?: string | null;
  finished_at?: string | null;
  created_at: string;
  updated_at: string;
  stale: boolean;
  retryable: boolean;
}

interface CountItem {
  name: string;
  count: number;
}

interface Summary {
  status_counts: CountItem[];
  queue_counts_24h: CountItem[];
  type_counts_24h: CountItem[];
  last_24h: number;
  failed_24h: number;
  stale_running: number;
}

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

const taskOptions = [
  ["linlinqi:order:expire", "jobs.typeOrderExpire"],
  ["linlinqi:notification:dispatch", "jobs.typeNotificationDispatch"],
  ["linlinqi:webhook:deliver", "jobs.typeWebhookDeliver"],
  ["linlinqi:supplier:sync", "jobs.typeSupplierSync"],
  ["linlinqi:supplier:catalog-import", "jobs.typeSupplierCatalogImport"],
  ["linlinqi:reconciliation:run", "jobs.typeReconciliationRun"],
  ["linlinqi:refund:process", "jobs.typeRefundProcess"],
  ["linlinqi:supplier:purchase", "jobs.typeSupplierPurchase"],
] as const;

const jobs = ref<JobRecord[]>([]);
const summary = ref<Summary>({
  status_counts: [],
  queue_counts_24h: [],
  type_counts_24h: [],
  last_24h: 0,
  failed_24h: 0,
  stale_running: 0,
});
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const search = ref("");
const statusFilter = ref("");
const queueFilter = ref("");
const typeFilter = ref("");
const dateFrom = ref("");
const dateTo = ref("");
const loading = ref(false);
const loadError = ref("");
const notice = ref("");
const selectedJobIDs = ref<string[]>([]);
const batchRetrySaving = ref(false);
const detailJob = ref<JobRecord | null>(null);
const retryJob = ref<JobRecord | null>(null);
const retryReason = ref("");
const retryError = ref("");
const retrySaving = ref(false);
let requestSequence = 0;

const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const statusCounts = computed(
  () =>
    Object.fromEntries(
      (summary.value.status_counts || []).map((item) => [
        item.name,
        Number(item.count || 0),
      ]),
    ) as Record<string, number>,
);
const activeJobs = computed(
  () =>
    (statusCounts.value.queued || 0) +
    (statusCounts.value.running || 0) +
    (statusCounts.value.retrying || 0),
);
const maxTypeCount = computed(() =>
  Math.max(
    1,
    ...(summary.value.type_counts_24h || []).map((item) => item.count),
  ),
);
const retryablePageJobs = computed(() =>
  jobs.value.filter((job) => job.retryable),
);
const allRetryableJobsSelected = computed(
  () =>
    retryablePageJobs.value.length > 0 &&
    retryablePageJobs.value.every((job) =>
      selectedJobIDs.value.includes(job.id),
    ),
);

function responseMessage(error: unknown, fallback: string) {
  const candidate = error as {
    response?: { data?: { message?: string } };
    message?: string;
  };
  return candidate.response?.data?.message || candidate.message || fallback;
}

function dateTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "—"
    : date.toLocaleString("zh-CN", { hour12: false });
}

function duration(job: JobRecord) {
  const start = new Date(job.created_at).getTime();
  const end = job.finished_at
    ? new Date(job.finished_at).getTime()
    : new Date(job.updated_at).getTime();
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start)
    return "—";
  const milliseconds = end - start;
  if (milliseconds < 1000) return `${milliseconds} ms`;
  if (milliseconds < 60_000)
    return t("jobs.durationSeconds", {
      count: (milliseconds / 1000).toFixed(1),
    });
  return t("jobs.durationMinutes", {
    count: (milliseconds / 60_000).toFixed(1),
  });
}

function taskLabel(value: string) {
  const found = taskOptions.find(([code]) => code === value);
  return found ? t(found[1]) : value;
}

function shortTaskID(value: string) {
  return value.length > 20 ? `${value.slice(0, 9)}…${value.slice(-6)}` : value;
}

function statusLabel(value: string) {
  return (
    {
      queued: t("jobs.statusQueued"),
      running: t("jobs.statusRunning"),
      retrying: t("jobs.statusRetrying"),
      succeeded: t("jobs.statusSucceeded"),
      failed: t("jobs.statusFailed"),
      cancelled: t("jobs.statusCancelled"),
      replayed: t("jobs.statusReplayed"),
    }[value] || value
  );
}

function statusTone(value: string) {
  if (value === "succeeded" || value === "replayed") return "success";
  if (value === "failed") return "danger";
  if (value === "running" || value === "retrying") return "warning";
  return "neutral";
}

function prettyPayload(raw: string) {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return "{}";
  }
}

async function loadSummary() {
  try {
    const { data } = await adminApi.get("/jobs/summary");
    summary.value = data.data as Summary;
  } catch {
    /* The list remains actionable when the summary request fails. */
  }
}

async function loadJobs() {
  const sequence = ++requestSequence;
  loading.value = true;
  loadError.value = "";
  try {
    const { data } = await adminApi.get("/jobs", {
      params: {
        page: page.value,
        page_size: pageSize.value,
        q: search.value.trim() || undefined,
        status: statusFilter.value || undefined,
        queue: queueFilter.value || undefined,
        task_type: typeFilter.value || undefined,
        date_from: dateFrom.value || undefined,
        date_to: dateTo.value || undefined,
      },
    });
    if (sequence !== requestSequence) return;
    const result = data.data as PagePayload<JobRecord>;
    jobs.value = result?.items || [];
    selectedJobIDs.value = [];
    total.value = Number(result?.total || 0);
    loadSummary();
  } catch (error) {
    if (sequence === requestSequence)
      loadError.value = responseMessage(error, t("jobs.errLoadJobs"));
  } finally {
    if (sequence === requestSequence) loading.value = false;
  }
}

function toggleJobSelection(id: string) {
  if (!canManage.value) return;
  selectedJobIDs.value = selectedJobIDs.value.includes(id)
    ? selectedJobIDs.value.filter((value) => value !== id)
    : [...selectedJobIDs.value, id];
}

function toggleAllRetryableJobs() {
  if (!canManage.value) return;
  selectedJobIDs.value = allRetryableJobsSelected.value
    ? []
    : retryablePageJobs.value.map((job) => job.id);
}

async function batchRetryJobs() {
  if (!canManage.value) return;
  if (!selectedJobIDs.value.length || batchRetrySaving.value) return;
  const reason = window.prompt(t("jobs.batchReasonPrompt"), "")?.trim();
  if (!reason) return;
  if ([...reason].length < 4 || [...reason].length > 500) {
    loadError.value = t("jobs.errRetryReasonLength");
    return;
  }
  batchRetrySaving.value = true;
  loadError.value = "";
  try {
    const { data } = await adminApi.post(
      "/jobs/batch-retry",
      { ids: selectedJobIDs.value },
      { headers: { "X-Change-Reason": reason } },
    );
    const succeeded = Array.isArray(data.data?.succeeded)
      ? data.data.succeeded.length
      : 0;
    const failed = Array.isArray(data.data?.failed)
      ? data.data.failed.length
      : 0;
    notice.value = t("jobs.batchRetryResult", { succeeded, failed });
    selectedJobIDs.value = [];
    await loadJobs();
  } catch (error) {
    loadError.value = responseMessage(error, t("jobs.errBatchRetry"));
  } finally {
    batchRetrySaving.value = false;
  }
}

function applyFilters() {
  page.value = 1;
  loadJobs();
}

function resetFilters() {
  search.value = "";
  statusFilter.value = "";
  queueFilter.value = "";
  typeFilter.value = "";
  dateFrom.value = "";
  dateTo.value = "";
  page.value = 1;
  loadJobs();
}

function changePage(next: number) {
  if (next < 1 || next > totalPages.value || next === page.value) return;
  page.value = next;
  loadJobs();
}

function openRetry(job: JobRecord) {
  if (!canManage.value) return;
  retryJob.value = job;
  retryReason.value = "";
  retryError.value = "";
}

async function submitRetry() {
  if (!canManage.value) return;
  if (!retryJob.value) return;
  const reason = retryReason.value.trim();
  if ([...reason].length < 4 || [...reason].length > 500) {
    retryError.value = t("jobs.errRetryReasonLength");
    return;
  }
  retrySaving.value = true;
  try {
    const { data } = await adminApi.post(
      `/jobs/${retryJob.value.id}/retry`,
      {},
      { headers: { "X-Change-Reason": reason } },
    );
    notice.value = t("jobs.retryNotice", {
      taskId: shortTaskID(data.data?.task_id || ""),
    });
    retryJob.value = null;
    await loadJobs();
  } catch (error) {
    retryError.value = responseMessage(error, t("jobs.errRetryUnavailable"));
  } finally {
    retrySaving.value = false;
  }
}

onMounted(() => {
  loadJobs();
  loadSummary();
});
</script>

<template>
  <section class="jobs-view">
    <header class="jobs-topbar">
      <div>
        <span><ServerCog :size="15" />{{ t("jobs.topbarTitle") }}</span>
        <p>{{ t("jobs.topbarDesc") }}</p>
      </div>
      <button
        type="button"
        class="secondary-button"
        :disabled="loading"
        @click="loadJobs"
      >
        <RefreshCw :size="14" :class="{ spinning: loading }" />{{
          t("jobs.refresh")
        }}
      </button>
    </header>

    <section class="jobs-metrics">
      <article>
        <span><Layers3 :size="15" />{{ t("jobs.metricActive") }}</span
        ><strong>{{ activeJobs }}</strong
        ><small>{{ t("jobs.metricActiveDesc") }}</small>
      </article>
      <article>
        <span><CircleGauge :size="15" />{{ t("jobs.metricLast24h") }}</span
        ><strong>{{ summary.last_24h }}</strong
        ><small>{{ t("jobs.metricLast24hDesc") }}</small>
      </article>
      <article>
        <span><TriangleAlert :size="15" />{{ t("jobs.metricFailed24h") }}</span
        ><strong>{{ summary.failed_24h }}</strong
        ><small>{{ t("jobs.metricFailed24hDesc") }}</small>
      </article>
      <article>
        <span><Clock3 :size="15" />{{ t("jobs.metricStale") }}</span
        ><strong>{{ summary.stale_running }}</strong
        ><small>{{ t("jobs.metricStaleDesc") }}</small>
      </article>
    </section>

    <div v-if="notice" class="jobs-alert success">
      <Check :size="14" /><span>{{ notice }}</span
      ><button type="button" @click="notice = ''"><X :size="13" /></button>
    </div>
    <div v-if="loadError" class="jobs-alert danger">
      <AlertCircle :size="14" /><span>{{ loadError }}</span
      ><button type="button" @click="loadJobs">{{ t("jobs.retry") }}</button>
    </div>

    <section class="jobs-insights">
      <article class="jobs-panel queue-panel">
        <header>
          <div>
            <h2>{{ t("jobs.insightQueueTitle") }}</h2>
            <p>{{ t("jobs.insightQueueDesc") }}</p>
          </div>
        </header>
        <div class="queue-cards">
          <div v-for="queue in summary.queue_counts_24h" :key="queue.name">
            <span>{{ queue.name }}</span
            ><strong>{{ queue.count }}</strong>
          </div>
          <div v-if="!summary.queue_counts_24h.length" class="muted">
            {{ t("jobs.noQueueRecords") }}
          </div>
        </div>
      </article>
      <article class="jobs-panel type-panel">
        <header>
          <div>
            <h2>{{ t("jobs.insightTypeTitle") }}</h2>
            <p>{{ t("jobs.insightTypeDesc") }}</p>
          </div>
        </header>
        <div class="type-bars">
          <div v-for="item in summary.type_counts_24h" :key="item.name">
            <span>{{ taskLabel(item.name) }}</span>
            <div>
              <i
                :style="{ width: `${(item.count / maxTypeCount) * 100}%` }"
              ></i>
            </div>
            <strong>{{ item.count }}</strong>
          </div>
          <div v-if="!summary.type_counts_24h.length" class="muted">
            {{ t("jobs.noTypeRecords") }}
          </div>
        </div>
      </article>
    </section>

    <section class="jobs-panel jobs-list-panel">
      <header>
        <div>
          <h2>{{ t("jobs.listTitle") }}</h2>
          <p>{{ t("jobs.listDesc") }}</p>
        </div>
        <span>{{ t("jobs.totalCount", { count: total }) }}</span>
      </header>
      <div class="job-filters">
        <div>
          <Search :size="14" /><input
            v-model="search"
            :placeholder="t('jobs.searchPlaceholder')"
            @keydown.enter="applyFilters"
          />
        </div>
        <select v-model="statusFilter">
          <option value="">{{ t("jobs.filterAllStatus") }}</option>
          <option value="queued">{{ t("jobs.statusQueued") }}</option>
          <option value="running">{{ t("jobs.statusRunning") }}</option>
          <option value="retrying">{{ t("jobs.statusRetrying") }}</option>
          <option value="succeeded">{{ t("jobs.statusSucceeded") }}</option>
          <option value="failed">{{ t("jobs.statusFailed") }}</option>
          <option value="replayed">{{ t("jobs.statusReplayed") }}</option>
          <option value="cancelled">{{ t("jobs.statusCancelled") }}</option>
        </select>
        <select v-model="queueFilter">
          <option value="">{{ t("jobs.filterAllQueues") }}</option>
          <option value="critical">critical</option>
          <option value="default">default</option>
          <option value="low">low</option>
        </select>
        <select v-model="typeFilter">
          <option value="">{{ t("jobs.filterAllTypes") }}</option>
          <option
            v-for="[code, label] in taskOptions"
            :key="code"
            :value="code"
          >
            {{ t(label) }}
          </option>
        </select>
        <input
          v-model="dateFrom"
          type="date"
          :title="t('jobs.dateFromTitle')"
        /><input v-model="dateTo" type="date" :title="t('jobs.dateToTitle')" />
        <button
          type="button"
          class="primary-button compact"
          @click="applyFilters"
        >
          {{ t("jobs.apply") }}</button
        ><button type="button" class="text-button" @click="resetFilters">
          {{ t("jobs.reset") }}
        </button>
      </div>
      <div class="jobs-table-wrap" :aria-busy="loading">
        <div
          v-if="canManage && selectedJobIDs.length"
          class="jobs-batch-toolbar"
        >
          <strong>{{
            t("jobs.batchSelected", { count: selectedJobIDs.length })
          }}</strong>
          <span>{{ t("jobs.batchRetryHint") }}</span>
          <div>
            <button
              type="button"
              class="retry"
              :disabled="batchRetrySaving"
              @click="batchRetryJobs"
            >
              <RotateCw :size="13" />{{ t("jobs.batchRetry") }}
            </button>
            <button
              type="button"
              :disabled="batchRetrySaving"
              @click="selectedJobIDs = []"
            >
              {{ t("jobs.batchClear") }}
            </button>
          </div>
        </div>
        <table v-if="jobs.length">
          <thead>
            <tr>
              <th class="selection-cell">
                <input
                  v-if="canManage"
                  type="checkbox"
                  :checked="allRetryableJobsSelected"
                  :aria-label="t('jobs.batchSelectPage')"
                  @change="toggleAllRetryableJobs"
                />
              </th>
              <th>{{ t("jobs.colTask") }}</th>
              <th>{{ t("jobs.colQueue") }}</th>
              <th>{{ t("jobs.colStatusAttempts") }}</th>
              <th>{{ t("jobs.colScheduleDuration") }}</th>
              <th>{{ t("jobs.colLastError") }}</th>
              <th>{{ t("jobs.colActions") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="job in jobs" :key="job.id" :class="{ stale: job.stale }">
              <td class="selection-cell" data-label="">
                <input
                  v-if="canManage && job.retryable"
                  type="checkbox"
                  :checked="selectedJobIDs.includes(job.id)"
                  :aria-label="
                    t('jobs.batchSelectTask', { id: shortTaskID(job.task_id) })
                  "
                  @change="toggleJobSelection(job.id)"
                />
              </td>
              <td :data-label="t('jobs.colTask')">
                <strong>{{ taskLabel(job.task_type) }}</strong
                ><code>{{ shortTaskID(job.task_id) }}</code>
              </td>
              <td :data-label="t('jobs.colQueue')">
                <span class="queue-chip">{{ job.queue }}</span>
              </td>
              <td :data-label="t('jobs.colStatusAttempts')">
                <span class="status-chip" :class="statusTone(job.status)">{{
                  job.stale ? t("jobs.metricStale") : statusLabel(job.status)
                }}</span
                ><small>{{
                  t("jobs.attemptsCount", { count: job.attempts })
                }}</small>
              </td>
              <td :data-label="t('jobs.colScheduleDuration')">
                <strong>{{
                  dateTime(job.scheduled_at || job.created_at)
                }}</strong
                ><small>{{ duration(job) }}</small>
              </td>
              <td :data-label="t('jobs.colLastError')">
                <p
                  class="job-error"
                  :class="{ danger: job.last_error && job.status === 'failed' }"
                >
                  {{ job.last_error || "—" }}
                </p>
              </td>
              <td :data-label="t('jobs.colActions')">
                <div class="row-actions">
                  <button type="button" @click="detailJob = job">
                    <Eye :size="13" />{{ t("jobs.detail") }}</button
                  ><button
                    v-if="canManage && job.retryable"
                    type="button"
                    class="retry"
                    @click="openRetry(job)"
                  >
                    <RotateCw :size="13" />{{ t("jobs.safeRetry") }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-else class="jobs-empty">
          <LoaderCircle v-if="loading" :size="25" class="spinning" /><ServerCog
            v-else
            :size="28"
          /><strong>{{
            loading ? t("jobs.loadingJobs") : t("jobs.noJobs")
          }}</strong>
        </div>
      </div>
      <footer class="jobs-pagination">
        <span>{{ t("jobs.pageInfo", { page, total: totalPages }) }}</span>
        <div>
          <button
            type="button"
            :disabled="page <= 1 || loading"
            @click="changePage(page - 1)"
          >
            <ChevronLeft :size="14" />{{ t("jobs.prevPage") }}</button
          ><button
            type="button"
            :disabled="page >= totalPages || loading"
            @click="changePage(page + 1)"
          >
            {{ t("jobs.nextPage") }}<ChevronRight :size="14" />
          </button>
        </div>
      </footer>
    </section>

    <div
      v-if="detailJob"
      class="jobs-modal-backdrop"
      @mousedown.self="detailJob = null"
    >
      <section class="jobs-modal">
        <header>
          <div>
            <span><Eye :size="18" /></span>
            <div>
              <h2>{{ t("jobs.detailTitle") }}</h2>
              <p>
                {{ taskLabel(detailJob.task_type) }} · {{ detailJob.queue }}
              </p>
            </div>
          </div>
          <button type="button" @click="detailJob = null">
            <X :size="16" />
          </button>
        </header>
        <div class="job-detail">
          <dl>
            <div>
              <dt>{{ t("jobs.detailTaskId") }}</dt>
              <dd>
                <code>{{ detailJob.task_id }}</code>
              </dd>
            </div>
            <div>
              <dt>{{ t("jobs.detailStatus") }}</dt>
              <dd>
                <span
                  class="status-chip"
                  :class="statusTone(detailJob.status)"
                  >{{ statusLabel(detailJob.status) }}</span
                >
              </dd>
            </div>
            <div>
              <dt>{{ t("jobs.detailCreatedScheduled") }}</dt>
              <dd>
                {{ dateTime(detailJob.created_at) }}<br />{{
                  dateTime(detailJob.scheduled_at)
                }}
              </dd>
            </div>
            <div>
              <dt>{{ t("jobs.detailUpdatedFinished") }}</dt>
              <dd>
                {{ dateTime(detailJob.updated_at) }}<br />{{
                  dateTime(detailJob.finished_at)
                }}
              </dd>
            </div>
          </dl>
          <section>
            <h3>{{ t("jobs.detailPayloadTitle") }}</h3>
            <p>{{ t("jobs.detailPayloadDesc") }}</p>
            <pre>{{ prettyPayload(detailJob.payload) }}</pre>
          </section>
          <section>
            <h3>{{ t("jobs.detailLastError") }}</h3>
            <pre class="error-pre">{{
              detailJob.last_error || t("jobs.noError")
            }}</pre>
          </section>
        </div>
      </section>
    </div>

    <div
      v-if="retryJob && canManage"
      class="jobs-modal-backdrop"
      @mousedown.self="!retrySaving && (retryJob = null)"
    >
      <section class="jobs-modal small">
        <header>
          <div>
            <span><TimerReset :size="18" /></span>
            <div>
              <h2>{{ t("jobs.retryTitle") }}</h2>
              <p>
                {{ taskLabel(retryJob.task_type) }} ·
                {{ shortTaskID(retryJob.task_id) }}
              </p>
            </div>
          </div>
          <button
            type="button"
            :disabled="retrySaving"
            @click="retryJob = null"
          >
            <X :size="16" />
          </button>
        </header>
        <form class="retry-form" @submit.prevent="submitRetry">
          <div class="retry-warning">
            <TriangleAlert :size="15" />
            <p>{{ t("jobs.retryDesc") }}</p>
          </div>
          <label
            ><span>{{ t("jobs.retryReasonLabel") }}</span
            ><textarea
              v-model="retryReason"
              rows="4"
              maxlength="500"
              :placeholder="t('jobs.retryReasonPlaceholder')"
            ></textarea>
          </label>
          <div v-if="retryError" class="inline-error">
            <AlertCircle :size="14" />{{ retryError }}
          </div>
          <footer>
            <button
              type="button"
              class="secondary-button"
              :disabled="retrySaving"
              @click="retryJob = null"
            >
              {{ t("jobs.cancel") }}</button
            ><button
              type="submit"
              class="primary-button"
              :disabled="retrySaving"
            >
              <LoaderCircle
                v-if="retrySaving"
                :size="14"
                class="spinning"
              /><Play v-else :size="14" />{{
                retrySaving ? t("jobs.retrySaving") : t("jobs.retryConfirm")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>
  </section>
</template>

<style scoped>
.jobs-view {
  display: grid;
  gap: 14px;
  padding: 18px;
}
.jobs-topbar {
  min-height: 62px;
  padding: 11px 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.jobs-topbar > div > span {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 11px;
  font-weight: 700;
}
.jobs-topbar p {
  margin: 4px 0 0;
  color: var(--muted);
  font-size: 9px;
}
.secondary-button,
.text-button,
.row-actions button,
.jobs-pagination button {
  min-height: 34px;
  padding: 0 10px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface-2);
  color: var(--muted);
  font-size: 10px;
  font-weight: 650;
}
.jobs-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.jobs-metrics article {
  min-height: 112px;
  padding: 14px;
  display: grid;
  gap: 8px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.jobs-metrics span {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--muted);
  font-size: 9px;
}
.jobs-metrics strong {
  font-size: 24px;
}
.jobs-metrics small {
  color: var(--muted);
  font-size: 9px;
}
.jobs-alert {
  min-height: 40px;
  padding: 8px 12px;
  display: flex;
  align-items: center;
  gap: 8px;
  border: 1px solid var(--line);
  border-radius: 8px;
  font-size: 10px;
}
.jobs-alert span {
  flex: 1;
}
.jobs-alert button {
  border: 0;
  background: transparent;
  color: inherit;
}
.jobs-alert.success {
  border-color: color-mix(in srgb, var(--success) 30%, var(--line));
  background: color-mix(in srgb, var(--success) 7%, var(--surface));
  color: var(--success);
}
.jobs-alert.danger {
  border-color: color-mix(in srgb, var(--danger) 30%, var(--line));
  background: color-mix(in srgb, var(--danger) 7%, var(--surface));
  color: var(--danger);
}
.jobs-insights {
  display: grid;
  grid-template-columns: minmax(280px, 0.65fr) minmax(0, 1.35fr);
  gap: 12px;
}
.jobs-panel {
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: 10px;
  overflow: hidden;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.jobs-panel > header {
  min-height: 64px;
  padding: 13px 15px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid var(--line);
}
.jobs-panel h2 {
  margin: 0 0 4px;
  font-size: 13px;
}
.jobs-panel header p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
}
.jobs-panel > header > span {
  color: var(--muted);
  font-size: 9px;
}
.queue-cards {
  min-height: 135px;
  padding: 14px;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}
.queue-cards > div:not(.muted) {
  padding: 12px;
  display: grid;
  gap: 9px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface-2);
}
.queue-cards span {
  color: var(--muted);
  font-size: 9px;
}
.queue-cards strong {
  font-size: 20px;
}
.muted {
  grid-column: 1 / -1;
  display: grid;
  place-items: center;
  color: var(--muted);
  font-size: 9px;
}
.type-bars {
  min-height: 135px;
  padding: 12px 15px;
  display: grid;
  align-content: center;
  gap: 9px;
}
.type-bars > div:not(.muted) {
  display: grid;
  grid-template-columns: 115px 1fr 40px;
  align-items: center;
  gap: 8px;
}
.type-bars span {
  color: var(--muted);
  font-size: 9px;
}
.type-bars > div > div {
  height: 7px;
  overflow: hidden;
  border-radius: 8px;
  background: var(--soft);
}
.type-bars i {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--dark);
}
.type-bars strong {
  text-align: right;
  font-size: 10px;
}
.job-filters {
  padding: 11px 13px;
  display: grid;
  grid-template-columns: 1.2fr repeat(3, 0.85fr) repeat(2, 0.65fr) auto auto;
  gap: 7px;
  border-bottom: 1px solid var(--line);
}
.job-filters > div {
  position: relative;
}
.job-filters > div svg {
  position: absolute;
  left: 9px;
  top: 10px;
  color: var(--muted);
}
.job-filters input,
.job-filters select {
  width: 100%;
  height: 34px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  outline: 0;
  background: var(--surface-2);
  font-size: 9px;
}
.job-filters > div input {
  padding-left: 29px;
}
.jobs-table-wrap {
  overflow: auto;
}
table {
  width: 100%;
  border-collapse: collapse;
}
th {
  padding: 9px 12px;
  text-align: left;
  background: var(--surface-2);
  color: var(--muted);
  font-size: 8px;
  white-space: nowrap;
}
td {
  padding: 11px 12px;
  border-top: 1px solid var(--line);
  font-size: 10px;
  vertical-align: middle;
}
tr.stale {
  background: color-mix(in srgb, var(--warn) 4%, var(--surface));
}
td strong,
td code,
td small {
  display: block;
}
td code {
  margin-top: 4px;
  color: var(--muted);
  font-size: 8px;
}
td small {
  margin-top: 4px;
  color: var(--muted);
  font-size: 8px;
}
.queue-chip,
.status-chip {
  padding: 4px 7px;
  display: inline-flex;
  border-radius: 10px;
  font-size: 8px;
  font-weight: 700;
  white-space: nowrap;
}
.queue-chip,
.status-chip.neutral {
  background: var(--soft);
  color: var(--muted);
}
.status-chip.success {
  background: color-mix(in srgb, var(--success) 10%, var(--surface));
  color: var(--success);
}
.status-chip.warning {
  background: color-mix(in srgb, var(--warn) 11%, var(--surface));
  color: var(--warn);
}
.status-chip.danger {
  background: color-mix(in srgb, var(--danger) 10%, var(--surface));
  color: var(--danger);
}
.job-error {
  max-width: 380px;
  margin: 0;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.5;
  white-space: normal;
  overflow-wrap: anywhere;
}
.job-error.danger {
  color: var(--danger);
}
.row-actions {
  display: flex;
  gap: 5px;
}
.row-actions button {
  min-height: 29px;
  padding: 0 7px;
  white-space: nowrap;
}
.row-actions button.retry {
  color: var(--warn);
}
.jobs-empty {
  min-height: 260px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 9px;
  color: var(--muted);
}
.jobs-empty strong {
  font-size: 10px;
}
.jobs-pagination {
  padding: 10px 13px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-top: 1px solid var(--line);
  color: var(--muted);
  font-size: 9px;
}
.jobs-pagination div {
  display: flex;
  gap: 5px;
}
button:disabled {
  cursor: not-allowed;
  opacity: 0.48;
}
.jobs-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 100;
  padding: 22px;
  display: grid;
  place-items: center;
  background: rgba(0, 0, 0, 0.48);
  backdrop-filter: blur(3px);
}
.jobs-modal {
  width: min(760px, 100%);
  max-height: calc(100vh - 44px);
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: 11px;
  background: var(--surface);
  box-shadow: 0 25px 80px rgba(0, 0, 0, 0.25);
}
.jobs-modal.small {
  width: min(540px, 100%);
}
.jobs-modal > header {
  position: sticky;
  top: 0;
  z-index: 2;
  min-height: 68px;
  padding: 13px 15px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
}
.jobs-modal > header > div {
  display: flex;
  align-items: center;
  gap: 10px;
}
.jobs-modal > header > div > span {
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  background: var(--soft);
}
.jobs-modal h2 {
  margin: 0 0 4px;
  font-size: 13px;
}
.jobs-modal header p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
}
.jobs-modal > header > button {
  width: 31px;
  height: 31px;
  display: grid;
  place-items: center;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface-2);
}
.job-detail {
  padding: 15px;
  display: grid;
  gap: 13px;
}
.job-detail dl {
  margin: 0;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}
.job-detail dl div {
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-2);
}
.job-detail dt {
  margin-bottom: 6px;
  color: var(--muted);
  font-size: 8px;
}
.job-detail dd {
  margin: 0;
  font-size: 9px;
  line-height: 1.6;
  overflow-wrap: anywhere;
}
.job-detail h3 {
  margin: 0 0 4px;
  font-size: 10px;
}
.job-detail section > p {
  margin: 0 0 8px;
  color: var(--muted);
  font-size: 8px;
}
.job-detail pre {
  max-height: 210px;
  overflow: auto;
  margin: 0;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-2);
  color: var(--muted);
  font-size: 9px;
  line-height: 1.55;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.job-detail pre.error-pre {
  color: var(--danger);
}
.retry-form {
  padding: 15px;
  display: grid;
  gap: 13px;
}
.retry-warning {
  padding: 10px;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  border-radius: 7px;
  background: color-mix(in srgb, var(--warn) 9%, var(--surface));
  color: var(--warn);
}
.retry-warning p {
  margin: 0;
  font-size: 9px;
  line-height: 1.6;
}
.retry-form label {
  display: grid;
  gap: 6px;
}
.retry-form label span {
  color: var(--muted);
  font-size: 9px;
  font-weight: 650;
}
.retry-form textarea {
  width: 100%;
  padding: 9px 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  outline: 0;
  resize: vertical;
  background: var(--surface-2);
  font-size: 10px;
  line-height: 1.55;
}
.inline-error {
  padding: 9px 10px;
  display: flex;
  align-items: center;
  gap: 7px;
  border-radius: 6px;
  background: color-mix(in srgb, var(--danger) 8%, var(--surface));
  color: var(--danger);
  font-size: 9px;
}
.retry-form footer {
  padding-top: 10px;
  display: flex;
  justify-content: flex-end;
  gap: 7px;
  border-top: 1px solid var(--line);
}
.retry-form footer button {
  min-width: 120px;
  height: 36px;
}
.spinning {
  animation: jobs-spin 0.8s linear infinite;
}
@keyframes jobs-spin {
  to {
    transform: rotate(360deg);
  }
}
.jobs-batch-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--line);
  background: var(--surface-2);
}
.jobs-batch-toolbar > span {
  color: var(--muted);
  font-size: 9px;
}
.jobs-batch-toolbar > div {
  display: flex;
  gap: 7px;
  margin-left: auto;
}
.jobs-batch-toolbar button {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  min-height: 32px;
  padding: 0 10px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface);
  cursor: pointer;
}
.selection-cell {
  width: 42px;
  text-align: center;
}
.selection-cell input {
  width: 16px;
  height: 16px;
  accent-color: var(--text);
}
@media (max-width: 1100px) {
  .jobs-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .job-filters {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}
@media (max-width: 760px) {
  .jobs-view {
    padding: 12px;
  }
  .jobs-insights {
    grid-template-columns: 1fr;
  }
  .job-filters {
    grid-template-columns: 1fr 1fr;
  }
  table,
  thead,
  tbody,
  tr,
  th,
  td {
    display: block;
  }
  thead {
    display: none;
  }
  tbody {
    display: grid;
    gap: 8px;
    padding: 9px;
  }
  tr {
    padding: 10px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--surface-2);
  }
  td {
    padding: 7px 0;
    display: grid;
    grid-template-columns: 105px 1fr;
    border: 0;
  }
  td:before {
    content: attr(data-label);
    color: var(--muted);
    font-size: 8px;
  }
  .job-error {
    max-width: none;
  }
  .row-actions {
    flex-wrap: wrap;
  }
  .jobs-batch-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }
  .jobs-batch-toolbar > div {
    margin-left: 0;
  }
}
@media (max-width: 500px) {
  .jobs-metrics {
    grid-template-columns: 1fr;
  }
  .jobs-topbar {
    align-items: flex-start;
    flex-direction: column;
  }
  .job-filters {
    grid-template-columns: 1fr;
  }
  .queue-cards {
    grid-template-columns: 1fr;
  }
  .type-bars > div:not(.muted) {
    grid-template-columns: 95px 1fr 32px;
  }
  .jobs-modal-backdrop {
    padding: 8px;
  }
  .jobs-modal {
    max-height: calc(100vh - 16px);
  }
  .job-detail dl {
    grid-template-columns: 1fr;
  }
  .retry-form footer {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }
}
</style>
