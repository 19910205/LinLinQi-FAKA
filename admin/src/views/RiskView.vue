<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  AlertCircle,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleGauge,
  Edit3,
  Eye,
  FileWarning,
  LoaderCircle,
  Plus,
  RefreshCw,
  Search,
  ShieldAlert,
  ShieldCheck,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";

const { t, locale } = useI18n();
const route = useRoute();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("security.manage"));

type Tab = "rules" | "decisions";
type RuleKind = "ip_orders" | "email_failures" | "high_value_guest";
type RiskAction = "review" | "challenge" | "deny";

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}
interface RiskRule {
  id: string;
  code: string;
  name: string;
  scope: string;
  expression: string;
  action: RiskAction;
  score: number;
  enabled: boolean;
  priority: number;
  created_at: string;
  updated_at: string;
}
interface RiskDecision {
  id: string;
  order_id?: string | null;
  user_id?: string | null;
  ip: string;
  score: number;
  decision: string;
  matched_rules: string | string[];
  signals: string | Record<string, unknown>;
  reviewed_by?: string | null;
  reviewed_at?: string | null;
  created_at: string;
}
interface Summary {
  since: string;
  decision_counts: Array<{ decision: string; count: number }>;
  pending_review: number;
  active_rules: number;
  daily_series: Array<{
    date: string;
    decisions: number;
    security_events: number;
  }>;
}
interface RuleForm {
  code: string;
  name: string;
  kind: RuleKind;
  threshold: number;
  action: RiskAction;
  score: number;
  enabled: boolean;
  priority: number;
  reason: string;
}

const activeTab = ref<Tab>("rules");

const rules = ref<RiskRule[]>([]);
const decisions = ref<RiskDecision[]>([]);
const summary = ref<Summary>({
  since: "",
  decision_counts: [],
  pending_review: 0,
  active_rules: 0,
  daily_series: [],
});
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const searchInput = ref("");
const appliedSearch = ref("");
const scopeFilter = ref("");
const enabledFilter = ref("");
const decisionFilter = ref("");
const reviewedFilter = ref("");
const scoreMinimum = ref("");
const ipFilter = ref("");
const loading = ref(false);
const loadError = ref("");
const notice = ref("");
const ruleModal = ref(false);
const editingRule = ref<RiskRule | null>(null);
const ruleSaving = ref(false);
const ruleError = ref("");
const reviewModal = ref(false);
const reviewingDecision = ref<RiskDecision | null>(null);
const reviewOutcome = ref<"allow" | "deny">("allow");
const reviewReason = ref("");
const reviewSaving = ref(false);
const reviewError = ref("");
const detailDecision = ref<RiskDecision | null>(null);
let requestSequence = 0;

function blankRuleForm(): RuleForm {
  return {
    code: "",
    name: "",
    kind: "ip_orders",
    threshold: 12,
    action: "challenge",
    score: 40,
    enabled: true,
    priority: 100,
    reason: "",
  };
}
const ruleForm = ref<RuleForm>(blankRuleForm());
const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);
const decisionCount = computed(
  () =>
    Object.fromEntries(
      summary.value.decision_counts.map((item) => [item.decision, item.count]),
    ) as Record<string, number>,
);
const maxDaily = computed(() =>
  Math.max(
    1,
    ...summary.value.daily_series.flatMap((point) => [
      point.decisions,
      point.security_events,
    ]),
  ),
);

function dailyBarHeight(value: number) {
  return `${Math.max(4, Math.round((value / maxDaily.value) * 100))}%`;
}

function dailyLabel(date: string) {
  return date.length >= 10 ? date.slice(5) : date;
}

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
    : date.toLocaleString(locale.value, { hour12: false });
}
function shortID(value?: string | null) {
  if (!value) return "—";
  return value.length > 20 ? `${value.slice(0, 9)}…${value.slice(-6)}` : value;
}
const ACTION_KEYS: Record<string, string> = {
  allow: "risk.actionAllow",
  review: "risk.actionReview",
  challenge: "risk.actionChallenge",
  deny: "risk.actionDeny",
};
function decisionLabel(value: string) {
  const key = ACTION_KEYS[value];
  return key ? t(key) : value;
}
function actionTone(value: string) {
  if (value === "allow") return "success";
  if (value === "deny") return "danger";
  if (value === "challenge" || value === "review") return "warning";
  return "neutral";
}
const SCOPE_KEYS: Record<string, string> = {
  payment: "risk.scopePayment",
  checkout: "risk.scopeCheckout",
};
function scopeLabel(value: string) {
  const key = SCOPE_KEYS[value];
  return key ? t(key) : value;
}
function parseJSON<T>(value: string | T, fallback: T): T {
  if (typeof value !== "string") return value || fallback;
  try {
    return JSON.parse(value) as T;
  } catch {
    return fallback;
  }
}
function signalEntries(decision: RiskDecision) {
  return Object.entries(
    parseJSON<Record<string, unknown>>(decision.signals, {}),
  );
}
function matchedRules(decision: RiskDecision) {
  return parseJSON<string[]>(decision.matched_rules, []);
}

function ruleKind(rule: RiskRule): RuleKind {
  if (rule.expression.startsWith("failures(email,")) return "email_failures";
  if (rule.expression.startsWith("guest")) return "high_value_guest";
  return "ip_orders";
}
function ruleThreshold(rule: RiskRule) {
  const match = rule.expression.match(/>\s*(\d+)$/);
  return Number(match?.[1] || 1);
}
function expressionFor(form: RuleForm) {
  const threshold = Math.trunc(Number(form.threshold));
  if (form.kind === "email_failures")
    return `failures(email,1h) > ${threshold}`;
  if (form.kind === "high_value_guest") return `guest && total > ${threshold}`;
  return `orders(ip,10m) > ${threshold}`;
}
function kindHint(kind: RuleKind) {
  if (kind === "email_failures") return t("risk.kindHintEmailFailures");
  if (kind === "high_value_guest") return t("risk.kindHintHighValueGuest");
  return t("risk.kindHintIpOrders");
}

async function loadSummary() {
  try {
    const { data } = await adminApi.get("/risk/summary");
    summary.value = data.data as Summary;
  } catch {
    /* list remains usable */
  }
}
async function loadData() {
  const sequence = ++requestSequence;
  loading.value = true;
  loadError.value = "";
  try {
    if (activeTab.value === "rules") {
      const { data } = await adminApi.get("/risk/rules", {
        params: {
          page: page.value,
          page_size: pageSize.value,
          q: appliedSearch.value || undefined,
          scope: scopeFilter.value || undefined,
          enabled: enabledFilter.value || undefined,
        },
      });
      if (sequence !== requestSequence) return;
      const payload = data.data as PagePayload<RiskRule>;
      rules.value = payload?.items || [];
      total.value = Number(payload?.total || 0);
    } else {
      const { data } = await adminApi.get("/risk/decisions", {
        params: {
          page: page.value,
          page_size: pageSize.value,
          decision: decisionFilter.value || undefined,
          reviewed: reviewedFilter.value || undefined,
          score_min: scoreMinimum.value || undefined,
          ip: ipFilter.value.trim() || undefined,
        },
      });
      if (sequence !== requestSequence) return;
      const payload = data.data as PagePayload<RiskDecision>;
      decisions.value = payload?.items || [];
      total.value = Number(payload?.total || 0);
    }
    loadSummary();
  } catch (error) {
    loadError.value = responseMessage(error, t("risk.errLoad"));
  } finally {
    if (sequence === requestSequence) loading.value = false;
  }
}
function applySearch() {
  appliedSearch.value = searchInput.value.trim();
  page.value = 1;
  loadData();
}
function resetFilters() {
  searchInput.value = "";
  appliedSearch.value = "";
  scopeFilter.value = "";
  enabledFilter.value = "";
  decisionFilter.value = "";
  reviewedFilter.value = "";
  scoreMinimum.value = "";
  ipFilter.value = "";
  page.value = 1;
  loadData();
}
function changePage(next: number) {
  if (next < 1 || next > totalPages.value || next === page.value) return;
  page.value = next;
  loadData();
}

function openCreateRule() {
  if (!canManage.value) return;
  editingRule.value = null;
  ruleForm.value = blankRuleForm();
  ruleError.value = "";
  ruleModal.value = true;
}
function openEditRule(rule: RiskRule) {
  if (!canManage.value) return;
  editingRule.value = rule;
  ruleForm.value = {
    code: rule.code,
    name: rule.name,
    kind: ruleKind(rule),
    threshold: ruleThreshold(rule),
    action: rule.action,
    score: rule.score,
    enabled: rule.enabled,
    priority: rule.priority,
    reason: "",
  };
  ruleError.value = "";
  ruleModal.value = true;
}
async function saveRule() {
  if (!canManage.value) return;
  const form = ruleForm.value;
  ruleError.value = "";
  if (!/^[a-z][a-z0-9_]{2,79}$/.test(form.code.trim().toLowerCase())) {
    ruleError.value = t("risk.errCodeFormat");
    return;
  }
  if (form.name.trim().length < 2 || form.name.trim().length > 160) {
    ruleError.value = t("risk.errNameLength");
    return;
  }
  if (
    !Number.isInteger(Number(form.threshold)) ||
    Number(form.threshold) < 1 ||
    Number(form.threshold) >
      (form.kind === "high_value_guest" ? 9000000000000000 : 10000)
  ) {
    ruleError.value =
      form.kind === "high_value_guest"
        ? t("risk.errAmountThreshold")
        : t("risk.errCountThreshold");
    return;
  }
  if (
    !Number.isInteger(Number(form.score)) ||
    form.score < 0 ||
    form.score > 100
  ) {
    ruleError.value = t("risk.errScoreRange");
    return;
  }
  if (form.reason.trim().length < 4 || form.reason.trim().length > 500) {
    ruleError.value = t("risk.errChangeReasonLength");
    return;
  }
  const payload = {
    code: form.code.trim().toLowerCase(),
    name: form.name.trim(),
    scope: form.kind === "email_failures" ? "payment" : "checkout",
    expression: expressionFor(form),
    action: form.action,
    score: Math.trunc(form.score),
    enabled: form.enabled,
    priority: Math.trunc(Number(form.priority) || 0),
  };
  ruleSaving.value = true;
  try {
    const headers = { "X-Change-Reason": form.reason.trim() };
    if (editingRule.value)
      await adminApi.put(`/risk/rules/${editingRule.value.id}`, payload, {
        headers,
      });
    else await adminApi.post("/risk/rules", payload, { headers });
    notice.value = editingRule.value
      ? t("risk.ruleUpdated", { name: payload.name })
      : t("risk.ruleCreated", { name: payload.name });
    ruleModal.value = false;
    await loadData();
  } catch (error) {
    ruleError.value = responseMessage(error, t("risk.errRuleSave"));
  } finally {
    ruleSaving.value = false;
  }
}

function openReview(
  decision: RiskDecision,
  outcome: "allow" | "deny" = "allow",
) {
  if (!canManage.value) return;
  reviewingDecision.value = decision;
  reviewOutcome.value = outcome;
  reviewReason.value = "";
  reviewError.value = "";
  reviewModal.value = true;
}
async function submitReview() {
  if (!canManage.value) return;
  if (!reviewingDecision.value) return;
  if (
    reviewReason.value.trim().length < 4 ||
    reviewReason.value.trim().length > 500
  ) {
    reviewError.value = t("risk.errReviewReasonLength");
    return;
  }
  reviewSaving.value = true;
  try {
    const { data } = await adminApi.patch(
      `/risk/decisions/${reviewingDecision.value.id}/review`,
      { outcome: reviewOutcome.value },
      { headers: { "X-Change-Reason": reviewReason.value.trim() } },
    );
    const transitioned = Boolean(data.data?.order_transitioned);
    notice.value = transitioned
      ? t("risk.reviewRecordedTransitioned", {
          outcome: decisionLabel(reviewOutcome.value),
        })
      : t("risk.reviewRecorded", {
          outcome: decisionLabel(reviewOutcome.value),
        });
    reviewModal.value = false;
    detailDecision.value = null;
    await loadData();
  } catch (error) {
    reviewError.value = responseMessage(error, t("risk.errReviewSave"));
  } finally {
    reviewSaving.value = false;
  }
}

watch(activeTab, () => {
  page.value = 1;
  resetFilters();
});
watch(
  () => [route.path, route.meta.defaultTab] as const,
  ([, defaultTab]) => {
    activeTab.value = defaultTab === "decisions" ? "decisions" : "rules";
  },
  { immediate: true },
);
onMounted(() => {
  loadData();
  loadSummary();
});
</script>

<template>
  <section class="risk-view">
    <div class="risk-topbar">
      <div>
        <button
          v-if="activeTab === 'rules' && canManage"
          type="button"
          class="primary-button"
          @click="openCreateRule"
        >
          <Plus :size="14" />{{ t("risk.addRule") }}</button
        ><button
          type="button"
          class="secondary-button"
          :disabled="loading"
          @click="loadData"
        >
          <RefreshCw :size="14" :class="{ spinning: loading }" />{{
            t("risk.refresh")
          }}
        </button>
      </div>
    </div>
    <div class="risk-metrics">
      <article>
        <span><ShieldCheck :size="14" />{{ t("risk.enableRule") }}</span
        ><strong>{{ summary.active_rules }}</strong
        ><small>{{ t("risk.metricActiveRulesHint") }}</small>
      </article>
      <article>
        <span
          ><FileWarning :size="14" />{{ t("risk.metricPendingReview") }}</span
        ><strong>{{ summary.pending_review }}</strong
        ><small>{{ t("risk.metricPendingReviewHint") }}</small>
      </article>
      <article>
        <span><Check :size="14" />{{ t("risk.metricAllow24h") }}</span
        ><strong>{{ decisionCount.allow || 0 }}</strong
        ><small>{{
          t("risk.metricSince", { time: dateTime(summary.since) })
        }}</small>
      </article>
      <article>
        <span
          ><ShieldAlert :size="14" />{{
            t("risk.metricDenyChallenge24h")
          }}</span
        ><strong>{{
          (decisionCount.deny || 0) + (decisionCount.challenge || 0)
        }}</strong
        ><small>{{ t("risk.metricHighRiskCheckout") }}</small>
      </article>
    </div>
    <div
      v-if="summary.daily_series.length"
      class="risk-trend"
      :aria-label="t('risk.trendTitle')"
    >
      <div class="risk-trend-head">
        <span><CircleGauge :size="15" />{{ t("risk.trendTitle") }}</span>
        <small>{{ t("risk.trendDays", { days: 7 }) }}</small>
      </div>
      <div class="risk-trend-bars">
        <div
          v-for="point in summary.daily_series"
          :key="point.date"
          class="risk-trend-col"
        >
          <div class="risk-trend-track">
            <div
              class="risk-trend-bar decisions"
              :style="{ height: dailyBarHeight(point.decisions) }"
              :title="`${point.date} · ${t('risk.trendDecisions')} ${point.decisions}`"
            ></div>
            <div
              class="risk-trend-bar events"
              :style="{ height: dailyBarHeight(point.security_events) }"
              :title="`${point.date} · ${t('risk.trendEvents')} ${point.security_events}`"
            ></div>
          </div>
          <small>{{ dailyLabel(point.date) }}</small>
        </div>
      </div>
      <div class="risk-trend-legend">
        <span><i class="decisions"></i>{{ t("risk.trendDecisions") }}</span>
        <span><i class="events"></i>{{ t("risk.trendEvents") }}</span>
      </div>
    </div>
    <div v-if="notice" class="risk-alert success">
      <Check :size="14" /><span>{{ notice }}</span
      ><button type="button" @click="notice = ''"><X :size="13" /></button>
    </div>
    <div v-if="loadError" class="risk-alert danger">
      <AlertCircle :size="14" /><span>{{ loadError }}</span
      ><button type="button" @click="loadData">{{ t("risk.retry") }}</button>
    </div>
    <div class="risk-filterbar">
      <template v-if="activeTab === 'rules'"
        ><div class="risk-search">
          <Search :size="14" /><input
            v-model="searchInput"
            :placeholder="t('risk.searchPlaceholder')"
            @keydown.enter="applySearch"
          /><button type="button" @click="applySearch">
            {{ t("risk.search") }}
          </button>
        </div>
        <select
          v-model="scopeFilter"
          @change="
            page = 1;
            loadData();
          "
        >
          <option value="">{{ t("risk.scopeAll") }}</option>
          <option value="checkout">{{ t("risk.scopeCheckout") }}</option>
          <option value="payment">{{ t("risk.scopePayment") }}</option></select
        ><select
          v-model="enabledFilter"
          @change="
            page = 1;
            loadData();
          "
        >
          <option value="">{{ t("risk.enabledAll") }}</option>
          <option value="true">{{ t("risk.enabled") }}</option>
          <option value="false">{{ t("risk.disabled") }}</option>
        </select></template
      ><template v-else
        ><select
          v-model="decisionFilter"
          @change="
            page = 1;
            loadData();
          "
        >
          <option value="">{{ t("risk.decisionAll") }}</option>
          <option value="allow">{{ t("risk.actionAllow") }}</option>
          <option value="review">{{ t("risk.actionReview") }}</option>
          <option value="challenge">{{ t("risk.actionChallenge") }}</option>
          <option value="deny">{{ t("risk.actionDeny") }}</option></select
        ><select
          v-model="reviewedFilter"
          @change="
            page = 1;
            loadData();
          "
        >
          <option value="">{{ t("risk.reviewedAll") }}</option>
          <option value="false">{{ t("risk.notReviewed") }}</option>
          <option value="true">{{ t("risk.reviewed") }}</option></select
        ><input
          v-model="scoreMinimum"
          type="number"
          min="0"
          max="100"
          :placeholder="t('risk.minScorePlaceholder')"
          @change="
            page = 1;
            loadData();
          " /><input
          v-model="ipFilter"
          :placeholder="t('risk.ipPlaceholder')"
          @keydown.enter="
            page = 1;
            loadData();
          " /></template
      ><button type="button" class="text-button" @click="resetFilters">
        {{ t("risk.reset") }}</button
      ><span>{{ t("risk.totalCount", { total }) }}</span>
    </div>
    <div class="risk-table-shell" :aria-busy="loading">
      <table v-if="activeTab === 'rules' && rules.length" class="risk-table">
        <thead>
          <tr>
            <th>{{ t("risk.colRule") }}</th>
            <th>{{ t("risk.colScope") }}</th>
            <th>{{ t("risk.colExpression") }}</th>
            <th>{{ t("risk.colActionScore") }}</th>
            <th>{{ t("risk.colPriority") }}</th>
            <th>{{ t("risk.colStatus") }}</th>
            <th>{{ t("risk.colAction") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="rule in rules" :key="rule.id">
            <td :data-label="t('risk.colRule')">
              <strong>{{ rule.name }}</strong
              ><code>{{ rule.code }}</code>
            </td>
            <td :data-label="t('risk.colScope')">
              {{ scopeLabel(rule.scope) }}
            </td>
            <td :data-label="t('risk.colExpression')">
              <code>{{ rule.expression }}</code>
            </td>
            <td :data-label="t('risk.colActionScore')">
              <span class="status-chip" :class="actionTone(rule.action)">{{
                decisionLabel(rule.action)
              }}</span
              ><small>{{ t("risk.scorePoints", { score: rule.score }) }}</small>
            </td>
            <td :data-label="t('risk.colPriority')">{{ rule.priority }}</td>
            <td :data-label="t('risk.colStatus')">
              <span
                class="status-chip"
                :class="rule.enabled ? 'success' : 'neutral'"
                >{{
                  rule.enabled
                    ? t("risk.statusEnabled")
                    : t("risk.statusDisabled")
                }}</span
              >
            </td>
            <td :data-label="t('risk.colAction')">
              <button
                v-if="canManage"
                type="button"
                class="row-action"
                @click="openEditRule(rule)"
              >
                <Edit3 :size="13" />{{ t("risk.edit") }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <table
        v-else-if="activeTab === 'decisions' && decisions.length"
        class="risk-table"
      >
        <thead>
          <tr>
            <th>{{ t("risk.colDecision") }}</th>
            <th>{{ t("risk.colScoreMatchedRules") }}</th>
            <th>{{ t("risk.colSubject") }}</th>
            <th>{{ t("risk.colIp") }}</th>
            <th>{{ t("risk.colTime") }}</th>
            <th>{{ t("risk.colReview") }}</th>
            <th>{{ t("risk.colAction") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="decision in decisions" :key="decision.id">
            <td :data-label="t('risk.colDecision')">
              <span
                class="status-chip"
                :class="actionTone(decision.decision)"
                >{{ decisionLabel(decision.decision) }}</span
              ><code>{{ shortID(decision.id) }}</code>
            </td>
            <td :data-label="t('risk.colScoreRules')">
              <strong>{{
                t("risk.scorePoints", { score: decision.score })
              }}</strong
              ><small>{{
                matchedRules(decision).join(t("risk.listSeparator")) ||
                t("risk.noRuleMatched")
              }}</small>
            </td>
            <td :data-label="t('risk.colSubject')">
              <code v-if="decision.order_id">{{
                t("risk.orderRef", { id: shortID(decision.order_id) })
              }}</code
              ><code v-if="decision.user_id">{{
                t("risk.userRef", { id: shortID(decision.user_id) })
              }}</code
              ><span v-if="!decision.order_id && !decision.user_id">{{
                t("risk.guestCheckout")
              }}</span>
            </td>
            <td :data-label="t('risk.colIp')">
              <code>{{ decision.ip || "—" }}</code>
            </td>
            <td :data-label="t('risk.colTime')">
              {{ dateTime(decision.created_at) }}
            </td>
            <td :data-label="t('risk.colReview')">
              <span
                class="status-chip"
                :class="decision.reviewed_at ? 'success' : 'warning'"
                >{{
                  decision.reviewed_at
                    ? t("risk.reviewed")
                    : t("risk.reviewPending")
                }}</span
              ><small v-if="decision.reviewed_at">{{
                dateTime(decision.reviewed_at)
              }}</small>
            </td>
            <td :data-label="t('risk.colAction')">
              <div class="row-actions">
                <button
                  type="button"
                  class="row-action"
                  @click="detailDecision = decision"
                >
                  <Eye :size="13" />{{ t("risk.signals") }}</button
                ><button
                  v-if="
                    canManage &&
                    !decision.reviewed_at &&
                    decision.decision !== 'allow'
                  "
                  type="button"
                  class="row-action"
                  @click="openReview(decision)"
                >
                  <ShieldCheck :size="13" />{{ t("risk.colReview") }}
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="risk-empty">
        <LoaderCircle v-if="loading" :size="25" class="spinning" /><ShieldAlert
          v-else
          :size="28"
        /><strong>{{
          loading ? t("risk.loadingData") : t("risk.noRecordsFiltered")
        }}</strong>
      </div>
    </div>
    <div class="risk-pagination">
      <span>{{
        t("risk.pageInfo", { current: page, total: totalPages })
      }}</span>
      <div>
        <button
          type="button"
          :disabled="page <= 1 || loading"
          @click="changePage(page - 1)"
        >
          <ChevronLeft :size="14" />{{ t("risk.prevPage") }}</button
        ><button
          type="button"
          :disabled="page >= totalPages || loading"
          @click="changePage(page + 1)"
        >
          {{ t("risk.nextPage") }}<ChevronRight :size="14" />
        </button>
      </div>
    </div>

    <div
      v-if="ruleModal && canManage"
      class="risk-modal-backdrop"
      @mousedown.self="!ruleSaving && (ruleModal = false)"
    >
      <section class="risk-modal">
        <header>
          <div>
            <span><CircleGauge :size="18" /></span>
            <div>
              <h2>
                {{
                  editingRule
                    ? t("risk.editRuleTitle")
                    : t("risk.createRuleTitle")
                }}
              </h2>
              <p>{{ t("risk.ruleExpressionHint") }}</p>
            </div>
          </div>
          <button
            type="button"
            :disabled="ruleSaving"
            @click="ruleModal = false"
          >
            <X :size="16" />
          </button>
        </header>
        <form class="risk-form" @submit.prevent="saveRule">
          <div class="form-grid">
            <label
              ><span>{{ t("risk.colRuleName") }}</span
              ><input v-model="ruleForm.name" maxlength="160" /></label
            ><label
              ><span>{{ t("risk.colRuleCode") }}</span
              ><input
                v-model="ruleForm.code"
                maxlength="80"
                placeholder="lowercase_code"
            /></label>
          </div>
          <fieldset>
            <legend>{{ t("risk.signalType") }}</legend>
            <div class="kind-options">
              <button
                type="button"
                :class="{ active: ruleForm.kind === 'ip_orders' }"
                @click="ruleForm.kind = 'ip_orders'"
              >
                {{ t("risk.kindIpOrders") }}</button
              ><button
                type="button"
                :class="{ active: ruleForm.kind === 'email_failures' }"
                @click="ruleForm.kind = 'email_failures'"
              >
                {{ t("risk.kindEmailFailures") }}</button
              ><button
                type="button"
                :class="{ active: ruleForm.kind === 'high_value_guest' }"
                @click="ruleForm.kind = 'high_value_guest'"
              >
                {{ t("risk.kindHighValueGuest") }}
              </button>
            </div>
            <p>{{ kindHint(ruleForm.kind) }}</p>
            <label
              ><span>{{
                ruleForm.kind === "high_value_guest"
                  ? t("risk.thresholdAmount")
                  : t("risk.thresholdCount")
              }}</span
              ><input
                v-model.number="ruleForm.threshold"
                type="number"
                min="1"
                step="1" /></label
            ><code>{{ expressionFor(ruleForm) }}</code>
          </fieldset>
          <div class="form-grid">
            <label
              ><span>{{ t("risk.hitAction") }}</span
              ><select v-model="ruleForm.action">
                <option value="review">{{ t("risk.actionReview") }}</option>
                <option value="challenge">
                  {{ t("risk.actionChallenge") }}
                </option>
                <option value="deny">{{ t("risk.actionDeny") }}</option>
              </select></label
            ><label
              ><span>{{ t("risk.riskScore") }}</span
              ><input
                v-model.number="ruleForm.score"
                type="number"
                min="0"
                max="100"
                step="1" /></label
            ><label
              ><span>{{ t("risk.colPriority") }}</span
              ><input
                v-model.number="ruleForm.priority"
                type="number"
                step="1" /></label
            ><label class="switch-line"
              ><input v-model="ruleForm.enabled" type="checkbox" /><span
                ><strong>{{ t("risk.enableRule") }}</strong
                ><small>{{ t("risk.disabledHint") }}</small></span
              ></label
            >
          </div>
          <label
            ><span>{{ t("risk.changeReason") }}</span
            ><textarea
              v-model="ruleForm.reason"
              maxlength="500"
              rows="3"
              :placeholder="t('risk.changeReasonPlaceholder')"
            ></textarea>
          </label>
          <div v-if="ruleError" class="inline-error">
            <AlertCircle :size="14" />{{ ruleError }}
          </div>
          <footer>
            <button
              type="button"
              class="secondary-button"
              :disabled="ruleSaving"
              @click="ruleModal = false"
            >
              {{ t("risk.cancel") }}</button
            ><button
              type="submit"
              class="primary-button"
              :disabled="ruleSaving"
            >
              <LoaderCircle
                v-if="ruleSaving"
                :size="14"
                class="spinning"
              /><Check v-else :size="14" />{{
                ruleSaving ? t("risk.saving") : t("risk.confirmSave")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>

    <div
      v-if="reviewModal && reviewingDecision && canManage"
      class="risk-modal-backdrop"
      @mousedown.self="!reviewSaving && (reviewModal = false)"
    >
      <section class="risk-modal small-modal">
        <header>
          <div>
            <span><ShieldCheck :size="18" /></span>
            <div>
              <h2>{{ t("risk.reviewDecisionTitle") }}</h2>
              <p>
                {{ t("risk.scorePoints", { score: reviewingDecision.score }) }}
                ·
                {{ decisionLabel(reviewingDecision.decision) }}
              </p>
            </div>
          </div>
          <button
            type="button"
            :disabled="reviewSaving"
            @click="reviewModal = false"
          >
            <X :size="16" />
          </button>
        </header>
        <form class="risk-form" @submit.prevent="submitReview">
          <div class="review-options">
            <button
              type="button"
              :class="{ active: reviewOutcome === 'allow' }"
              @click="reviewOutcome = 'allow'"
            >
              <ShieldCheck :size="16" /><strong>{{
                t("risk.confirmAllow")
              }}</strong
              ><small>{{ t("risk.allowHint") }}</small></button
            ><button
              type="button"
              class="deny-option"
              :class="{ active: reviewOutcome === 'deny' }"
              @click="reviewOutcome = 'deny'"
            >
              <ShieldAlert :size="16" /><strong>{{
                t("risk.confirmDeny")
              }}</strong
              ><small>{{ t("risk.denyHint") }}</small>
            </button>
          </div>
          <label
            ><span>{{ t("risk.reviewBasis") }}</span
            ><textarea
              v-model="reviewReason"
              maxlength="500"
              rows="4"
              :placeholder="t('risk.reviewBasisPlaceholder')"
            ></textarea>
          </label>
          <p class="irreversible-note">{{ t("risk.irreversibleNote") }}</p>
          <div v-if="reviewError" class="inline-error">
            <AlertCircle :size="14" />{{ reviewError }}
          </div>
          <footer>
            <button
              type="button"
              class="secondary-button"
              :disabled="reviewSaving"
              @click="reviewModal = false"
            >
              {{ t("risk.cancel") }}</button
            ><button
              type="submit"
              :class="
                reviewOutcome === 'deny' ? 'danger-button' : 'primary-button'
              "
              :disabled="reviewSaving"
            >
              <LoaderCircle
                v-if="reviewSaving"
                :size="14"
                class="spinning"
              /><Check v-else :size="14" />{{ t("risk.confirmReview") }}
            </button>
          </footer>
        </form>
      </section>
    </div>

    <div
      v-if="detailDecision"
      class="risk-modal-backdrop"
      @mousedown.self="detailDecision = null"
    >
      <section class="risk-modal small-modal">
        <header>
          <div>
            <span><CircleGauge :size="18" /></span>
            <div>
              <h2>{{ t("risk.signalSnapshotTitle") }}</h2>
              <p>{{ shortID(detailDecision.id) }}</p>
            </div>
          </div>
          <button type="button" @click="detailDecision = null">
            <X :size="16" />
          </button>
        </header>
        <div class="signal-content">
          <section>
            <h3>{{ t("risk.matchedRulesTitle") }}</h3>
            <div class="rule-tags">
              <span v-for="rule in matchedRules(detailDecision)" :key="rule">{{
                rule
              }}</span
              ><small v-if="!matchedRules(detailDecision).length">{{
                t("risk.noRuleMatched")
              }}</small>
            </div>
          </section>
          <section>
            <h3>{{ t("risk.collectedSignals") }}</h3>
            <dl>
              <div
                v-for="entry in signalEntries(detailDecision)"
                :key="entry[0]"
              >
                <dt>{{ entry[0] }}</dt>
                <dd>{{ entry[1] }}</dd>
              </div>
            </dl>
          </section>
          <section>
            <h3>{{ t("risk.colSubject") }}</h3>
            <p>{{ t("risk.ipLabel", { ip: detailDecision.ip || "—" }) }}</p>
            <p>
              {{
                t("risk.orderLabel", {
                  id: detailDecision.order_id || t("risk.orderNotLinked"),
                })
              }}
            </p>
            <p>
              {{
                t("risk.userLabel", {
                  id: detailDecision.user_id || t("risk.guest"),
                })
              }}
            </p>
          </section>
        </div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.risk-view {
  display: grid;
  gap: 13px;
  color: var(--text);
}
.risk-topbar,
.risk-topbar > div,
.risk-filterbar,
.risk-pagination,
.risk-pagination > div {
  display: flex;
  align-items: center;
  gap: 7px;
}
.risk-topbar,
.risk-pagination {
  justify-content: space-between;
}
.risk-tabs {
  padding: 3px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface);
}
.risk-tabs button {
  display: inline-flex;
  min-height: 31px;
  align-items: center;
  gap: 5px;
  padding: 0 10px;
  border: 0;
  border-radius: 5px;
  color: var(--muted);
  background: transparent;
  font: inherit;
  font-size: 9px;
  cursor: pointer;
}
.risk-tabs button.active {
  color: var(--surface);
  background: var(--text);
}
.primary-button,
.secondary-button,
.danger-button {
  display: inline-flex;
  min-height: 35px;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 0 11px;
  border: 1px solid var(--text);
  border-radius: 6px;
  font: inherit;
  font-size: 9px;
  font-weight: 700;
  cursor: pointer;
}
.primary-button {
  color: var(--surface);
  background: var(--text);
}
.secondary-button {
  color: var(--text);
  border-color: var(--line);
  background: var(--surface);
}
.danger-button {
  color: white;
  border-color: #b42318;
  background: #b42318;
}
button:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.risk-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}
.risk-metrics article {
  min-height: 82px;
  padding: 13px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
}
.risk-metrics span {
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: 8px;
}
.risk-metrics strong {
  display: block;
  margin-top: 9px;
  font-size: 17px;
}
.risk-metrics small {
  display: block;
  margin-top: 4px;
  color: var(--muted);
  font-size: 8px;
}
.risk-trend {
  margin-top: 14px;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 14px;
  background: var(--surface);
}
.risk-trend-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}
.risk-trend-head span {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-weight: 650;
  font-size: 13px;
}
.risk-trend-head small {
  color: var(--muted);
  font-size: 11px;
}
.risk-trend-bars {
  display: grid;
  grid-template-columns: repeat(7, minmax(28px, 1fr));
  gap: 8px;
  align-items: end;
}
.risk-trend-col {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.risk-trend-col > small {
  color: var(--muted);
  font-size: 10px;
  white-space: nowrap;
}
.risk-trend-track {
  display: flex;
  align-items: flex-end;
  justify-content: center;
  gap: 3px;
  height: 92px;
  width: 100%;
}
.risk-trend-bar {
  width: 9px;
  min-height: 4px;
  border-radius: 4px 4px 1px 1px;
  transition: height 0.25s ease;
}
.risk-trend-bar.decisions {
  background: var(--accent, #4f6ef7);
}
.risk-trend-bar.events {
  background: var(--danger, #d64545);
}
.risk-trend-legend {
  display: flex;
  gap: 16px;
  margin-top: 10px;
  font-size: 11px;
  color: var(--muted);
}
.risk-trend-legend span {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.risk-trend-legend i {
  width: 9px;
  height: 9px;
  border-radius: 2px;
  display: inline-block;
}
.risk-trend-legend i.decisions {
  background: var(--accent, #4f6ef7);
}
.risk-trend-legend i.events {
  background: var(--danger, #d64545);
}
.risk-alert {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 9px 10px;
  border: 1px solid;
  border-radius: 7px;
  font-size: 9px;
}
.risk-alert span {
  flex: 1;
}
.risk-alert button {
  border: 0;
  color: inherit;
  background: transparent;
  font: inherit;
  cursor: pointer;
}
.risk-alert.success {
  color: #166534;
  border-color: #86efac;
  background: #f0fdf4;
}
.risk-alert.danger,
.inline-error {
  color: #991b1b;
  border-color: #fecaca;
  background: #fef2f2;
}
:global([data-theme="dark"]) .risk-alert.success {
  color: #bbf7d0;
  border-color: #166534;
  background: #052e16;
}
:global([data-theme="dark"]) .risk-alert.danger,
:global([data-theme="dark"]) .inline-error {
  color: #fecaca;
  border-color: #7f1d1d;
  background: #450a0a;
}
.risk-filterbar {
  flex-wrap: wrap;
  padding: 9px 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
}
.risk-search {
  display: flex;
  min-width: 280px;
  height: 32px;
  align-items: center;
  gap: 6px;
  padding-left: 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--muted);
}
.risk-search input {
  min-width: 0;
  flex: 1;
  border: 0;
  outline: 0;
  color: var(--text);
  background: transparent;
  font: inherit;
  font-size: 9px;
}
.risk-search button {
  height: 26px;
  margin-right: 3px;
  padding: 0 9px;
  border: 0;
  border-radius: 4px;
  color: var(--surface);
  background: var(--text);
  font: inherit;
  font-size: 8px;
  cursor: pointer;
}
.risk-filterbar > select,
.risk-filterbar > input {
  height: 32px;
  box-sizing: border-box;
  padding: 0 8px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 8px;
}
.risk-filterbar > input {
  width: 120px;
}
.risk-filterbar > span {
  margin-left: auto;
  color: var(--muted);
  font-size: 8px;
}
.text-button {
  border: 0;
  color: var(--muted);
  background: transparent;
  font: inherit;
  font-size: 8px;
  cursor: pointer;
}
.risk-table-shell {
  min-height: 310px;
  overflow-x: auto;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
}
.risk-table {
  width: 100%;
  min-width: 920px;
  border-collapse: collapse;
}
.risk-table th {
  padding: 11px 12px;
  color: var(--muted);
  border-bottom: 1px solid var(--line);
  background: var(--soft);
  font-size: 8px;
  font-weight: 600;
  text-align: left;
}
.risk-table td {
  padding: 12px;
  border-bottom: 1px solid var(--line);
  font-size: 9px;
}
.risk-table tr:last-child td {
  border-bottom: 0;
}
.risk-table td > strong,
.risk-table td > code,
.risk-table td > span,
.risk-table td > small {
  display: block;
}
.risk-table td > code,
.risk-table td > small {
  margin-top: 4px;
  color: var(--muted);
  background: transparent;
  font-size: 8px;
}
.status-chip {
  display: inline-flex !important;
  width: fit-content;
  min-height: 22px;
  align-items: center;
  padding: 0 6px;
  border: 1px solid var(--line);
  border-radius: 999px;
  font-size: 8px;
}
.status-chip.success {
  color: #166534;
  border-color: #bbf7d0;
  background: #f0fdf4;
}
.status-chip.warning {
  color: #92400e;
  border-color: #fde68a;
  background: #fffbeb;
}
.status-chip.danger {
  color: #991b1b;
  border-color: #fecaca;
  background: #fef2f2;
}
.status-chip.neutral {
  color: var(--muted);
  background: var(--soft);
}
:global([data-theme="dark"]) .status-chip.success {
  color: #bbf7d0;
  border-color: #166534;
  background: #052e16;
}
:global([data-theme="dark"]) .status-chip.warning {
  color: #fde68a;
  border-color: #92400e;
  background: #451a03;
}
:global([data-theme="dark"]) .status-chip.danger {
  color: #fecaca;
  border-color: #991b1b;
  background: #450a0a;
}
.row-actions {
  display: flex;
  gap: 5px;
}
.row-action {
  display: inline-flex;
  min-height: 28px;
  align-items: center;
  gap: 4px;
  padding: 0 7px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 8px;
  cursor: pointer;
}
.risk-empty {
  min-height: 310px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--muted);
  font-size: 9px;
}
.risk-empty strong {
  color: var(--text);
  font-size: 11px;
}
.risk-pagination {
  color: var(--muted);
  font-size: 8px;
}
.risk-pagination button {
  display: inline-flex;
  min-height: 31px;
  align-items: center;
  gap: 4px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 8px;
  cursor: pointer;
}
.risk-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 85;
  display: grid;
  place-items: center;
  padding: 18px;
  background: rgb(0 0 0 / 55%);
  backdrop-filter: blur(2px);
}
.risk-modal {
  width: min(700px, 100%);
  max-height: calc(100vh - 36px);
  overflow-y: auto;
  border: 1px solid var(--line);
  border-radius: 11px;
  background: var(--surface);
}
.small-modal {
  width: min(540px, 100%);
}
.risk-modal > header {
  position: sticky;
  top: 0;
  z-index: 2;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  padding: 15px 17px;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
}
.risk-modal header > div {
  display: flex;
  gap: 9px;
}
.risk-modal header > div > span {
  display: grid;
  width: 35px;
  height: 35px;
  place-items: center;
  border-radius: 8px;
  color: var(--surface);
  background: var(--text);
}
.risk-modal h2 {
  margin: 0;
  font-size: 13px;
}
.risk-modal header p {
  margin: 5px 0 0;
  color: var(--muted);
  font-size: 8px;
}
.risk-modal header > button {
  border: 0;
  color: var(--text);
  background: transparent;
  cursor: pointer;
}
.risk-form {
  display: grid;
  gap: 13px;
  padding: 16px 17px;
}
.risk-form label {
  display: grid;
  gap: 6px;
  color: var(--muted);
  font-size: 8px;
}
.risk-form label > span,
.risk-form legend {
  color: var(--text);
  font-size: 9px;
  font-weight: 650;
}
.risk-form input,
.risk-form select,
.risk-form textarea {
  width: 100%;
  box-sizing: border-box;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 9px;
  outline: none;
}
.risk-form input,
.risk-form select {
  height: 36px;
  padding: 0 9px;
}
.risk-form textarea {
  padding: 9px;
  resize: vertical;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}
.risk-form fieldset {
  display: grid;
  gap: 9px;
  margin: 0;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 7px;
}
.risk-form legend {
  padding: 0 5px;
}
.kind-options {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 5px;
}
.kind-options button {
  min-height: 32px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 8px;
  cursor: pointer;
}
.kind-options button.active {
  color: var(--surface);
  border-color: var(--text);
  background: var(--text);
}
.risk-form fieldset > p,
.irreversible-note {
  margin: 0;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.5;
}
.risk-form fieldset > code {
  padding: 8px;
  border-radius: 5px;
  color: var(--text);
  background: var(--soft);
  font-size: 8px;
}
.switch-line {
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  padding: 8px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--soft);
}
.switch-line input {
  width: 14px;
  height: 14px;
}
.switch-line span {
  display: grid;
  gap: 3px;
}
.switch-line small {
  color: var(--muted);
  font-size: 7px;
}
.inline-error {
  display: flex;
  align-items: flex-start;
  gap: 5px;
  padding: 8px 9px;
  border: 1px solid;
  border-radius: 6px;
  font-size: 8px;
}
.risk-form footer {
  display: flex;
  justify-content: flex-end;
  gap: 7px;
  margin: 2px -17px -16px;
  padding: 12px 17px;
  border-top: 1px solid var(--line);
}
.review-options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 7px;
}
.review-options button {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 3px 7px;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
  color: var(--text);
  background: var(--surface);
  text-align: left;
  cursor: pointer;
}
.review-options button svg {
  grid-row: 1 / 3;
}
.review-options strong {
  font-size: 9px;
}
.review-options small {
  color: var(--muted);
  font-size: 7px;
}
.review-options button.active {
  border-color: #166534;
  background: #f0fdf4;
}
.review-options .deny-option.active {
  border-color: #b42318;
  background: #fef2f2;
}
:global([data-theme="dark"]) .review-options button.active {
  background: #052e16;
}
:global([data-theme="dark"]) .review-options .deny-option.active {
  background: #450a0a;
}
.signal-content {
  display: grid;
  gap: 11px;
  padding: 16px 17px;
}
.signal-content section {
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
}
.signal-content h3 {
  margin: 0 0 8px;
  font-size: 9px;
}
.rule-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
}
.rule-tags span {
  padding: 4px 6px;
  border-radius: 999px;
  color: var(--surface);
  background: var(--text);
  font-size: 7px;
}
.rule-tags small {
  color: var(--muted);
  font-size: 8px;
}
.signal-content dl {
  display: grid;
  gap: 0;
  margin: 0;
}
.signal-content dl div {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  padding: 7px 0;
  border-bottom: 1px solid var(--line);
  font-size: 8px;
}
.signal-content dl div:last-child {
  border-bottom: 0;
}
.signal-content dt {
  color: var(--muted);
}
.signal-content dd {
  margin: 0;
}
.signal-content p {
  margin: 5px 0;
  color: var(--muted);
  font-size: 8px;
}
.spinning {
  animation: risk-spin 0.8s linear infinite;
}
@keyframes risk-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 820px) {
  .risk-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 620px) {
  .risk-topbar {
    align-items: stretch;
    flex-direction: column;
  }
  .risk-topbar > div:last-child {
    justify-content: flex-end;
  }
  .risk-search {
    min-width: 0;
    width: 100%;
  }
  .risk-filterbar > span {
    margin-left: 0;
  }
  .risk-table-shell {
    overflow: visible;
    border: 0;
    background: transparent;
  }
  .risk-table {
    min-width: 0;
    display: block;
  }
  .risk-table thead {
    display: none;
  }
  .risk-table tbody,
  .risk-table tr,
  .risk-table td {
    display: block;
    width: 100%;
    box-sizing: border-box;
  }
  .risk-table tr {
    margin-bottom: 8px;
    padding: 7px 10px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--surface);
  }
  .risk-table td {
    min-height: 35px;
    padding: 9px 0 9px 104px;
    position: relative;
    border-bottom: 1px solid var(--line);
  }
  .risk-table td:last-child {
    border-bottom: 0;
  }
  .risk-table td::before {
    content: attr(data-label);
    position: absolute;
    left: 0;
    top: 11px;
    color: var(--muted);
    font-size: 8px;
  }
  .form-grid,
  .review-options {
    grid-template-columns: 1fr;
  }
  .kind-options {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 450px) {
  .risk-metrics {
    grid-template-columns: 1fr;
  }
  .risk-pagination {
    align-items: flex-start;
    flex-direction: column;
  }
  .risk-pagination > div {
    width: 100%;
  }
  .risk-pagination button {
    flex: 1;
  }
  .risk-modal-backdrop {
    padding: 0;
  }
  .risk-modal {
    width: 100%;
    min-height: 100vh;
    max-height: 100vh;
    border-radius: 0;
  }
}
</style>
