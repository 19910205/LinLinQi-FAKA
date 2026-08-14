<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import {
  Activity,
  BarChart3,
  CalendarRange,
  CreditCard,
  Download,
  LoaderCircle,
  Package,
  RefreshCw,
  RotateCcw,
  ShoppingCart,
  TrendingUp,
  UserPlus,
} from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { adminApi } from "../stores/auth";
import { safeCSVCell } from "../utils/csv";
import { formatMoney, storeCurrency } from "../utils/money";

const { t, locale } = useI18n();

interface AnalyticsRange {
  from: string;
  to: string;
  granularity: "day" | "hour";
  timezone: string;
  currency: string;
}

interface AnalyticsMetrics {
  gross_revenue: number;
  refunds: number;
  net_revenue: number;
  orders_created: number;
  paid_orders: number;
  delivered_orders: number;
  new_users: number;
  average_order_value: number;
  refund_rate: number;
  payment_success_rate: number;
}

interface AnalyticsPoint {
  bucket: string;
  gross_revenue: number;
  refunds: number;
  net_revenue: number;
  orders_created: number;
  paid_orders: number;
  delivered_orders: number;
  new_users: number;
}

interface ProductPerformance {
  product_id: string;
  product_name: string;
  order_count: number;
  units: number;
  gross_revenue: number;
}

interface ChannelPerformance {
  code: string;
  name: string;
  attempts: number;
  succeeded: number;
  paid_orders: number;
  gross_revenue: number;
  success_rate: number;
}

interface Funnel {
  orders: number;
  payment_started: number;
  paid: number;
  delivered: number;
  payment_rate: number;
  delivery_rate: number;
}

interface AnalyticsPayload {
  range: AnalyticsRange;
  metrics: AnalyticsMetrics;
  series: AnalyticsPoint[];
  products: ProductPerformance[];
  channels: ChannelPerformance[];
  funnel: Funnel;
}

const emptyMetrics = (): AnalyticsMetrics => ({
  gross_revenue: 0,
  refunds: 0,
  net_revenue: 0,
  orders_created: 0,
  paid_orders: 0,
  delivered_orders: 0,
  new_users: 0,
  average_order_value: 0,
  refund_rate: 0,
  payment_success_rate: 0,
});

const emptyFunnel = (): Funnel => ({
  orders: 0,
  payment_started: 0,
  paid: 0,
  delivered: 0,
  payment_rate: 0,
  delivery_rate: 0,
});

function localInputValue(value: Date) {
  const offset = value.getTimezoneOffset() * 60_000;
  return new Date(value.getTime() - offset).toISOString().slice(0, 16);
}

const now = new Date();
const fromInput = ref(
  localInputValue(new Date(now.getTime() - 30 * 86_400_000)),
);
const toInput = ref(localInputValue(now));
const granularity = ref<"day" | "hour">("day");
const activePreset = ref(30);
const loading = ref(false);
const loadError = ref("");
const payload = ref<AnalyticsPayload>({
  range: {
    from: new Date(fromInput.value).toISOString(),
    to: new Date(toInput.value).toISOString(),
    granularity: "day",
    timezone: "UTC",
    currency: "",
  },
  metrics: emptyMetrics(),
  series: [],
  products: [],
  channels: [],
  funnel: emptyFunnel(),
});
let requestSequence = 0;

const metrics = computed(() => payload.value.metrics || emptyMetrics());
const series = computed(() => payload.value.series || []);
const products = computed(() => payload.value.products || []);
const channels = computed(() => payload.value.channels || []);
const funnel = computed(() => payload.value.funnel || emptyFunnel());
const chartMaximum = computed(() =>
  Math.max(
    1,
    ...series.value.map((point) =>
      Math.max(point.gross_revenue, point.refunds),
    ),
  ),
);
const chartCoordinates = computed(() =>
  series.value.map((point, index) => {
    const count = Math.max(series.value.length - 1, 1);
    const x = series.value.length === 1 ? 380 : 34 + (index / count) * 692;
    const grossY = 216 - (point.gross_revenue / chartMaximum.value) * 174;
    const refundY = 216 - (point.refunds / chartMaximum.value) * 174;
    return { point, x, grossY, refundY };
  }),
);
const grossPolyline = computed(() =>
  chartCoordinates.value.map((point) => `${point.x},${point.grossY}`).join(" "),
);
const refundPolyline = computed(() =>
  chartCoordinates.value
    .map((point) => `${point.x},${point.refundY}`)
    .join(" "),
);
const grossArea = computed(() => {
  if (!chartCoordinates.value.length) return "";
  const first = chartCoordinates.value[0];
  const last = chartCoordinates.value[chartCoordinates.value.length - 1];
  return `${first.x},216 ${grossPolyline.value} ${last.x},216`;
});
const axisLabels = computed(() => {
  const count = series.value.length;
  if (!count) return [];
  const step = Math.max(1, Math.ceil((count - 1) / 5));
  return chartCoordinates.value.filter(
    (_, index) => index === 0 || index === count - 1 || index % step === 0,
  );
});
const maxProductRevenue = computed(() =>
  Math.max(1, ...products.value.map((item) => item.gross_revenue)),
);
const funnelRows = computed(() => [
  {
    key: "orders",
    label: t("analytics.ordersCreated"),
    value: funnel.value.orders,
  },
  {
    key: "payment_started",
    label: t("analytics.paymentStarted"),
    value: funnel.value.payment_started,
  },
  {
    key: "paid",
    label: t("analytics.paymentCompleted"),
    value: funnel.value.paid,
  },
  {
    key: "delivered",
    label: t("analytics.deliveryCompleted"),
    value: funnel.value.delivered,
  },
]);

function money(value: number) {
  return formatMoney(
    value,
    payload.value.range.currency || storeCurrency.value,
    locale.value,
  );
}

function integer(value: number) {
  return new Intl.NumberFormat("zh-CN").format(Number(value || 0));
}

function percent(value: number) {
  return `${Number(value || 0).toFixed(2)}%`;
}

function responseMessage(error: unknown, fallback: string) {
  const candidate = error as {
    response?: { data?: { message?: string } };
    message?: string;
  };
  return candidate.response?.data?.message || candidate.message || fallback;
}

function pointLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  if (payload.value.range.granularity === "hour") {
    return date.toLocaleString("zh-CN", {
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    });
  }
  return date.toLocaleDateString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
  });
}

function pointTitle(point: AnalyticsPoint) {
  return `${pointLabel(point.bucket)}｜${t("analytics.revenue")} ${money(point.gross_revenue)}｜${t("analytics.refunds")} ${money(point.refunds)}｜${t("analytics.orders")} ${integer(point.orders_created)}`;
}

function preset(days: number) {
  const end = new Date();
  const start = new Date(end.getTime() - days * 86_400_000);
  fromInput.value = localInputValue(start);
  toInput.value = localInputValue(end);
  granularity.value = "day";
  activePreset.value = days;
  loadAnalytics();
}

async function loadAnalytics() {
  const from = new Date(fromInput.value);
  const to = new Date(toInput.value);
  loadError.value = "";
  if (
    Number.isNaN(from.getTime()) ||
    Number.isNaN(to.getTime()) ||
    to.getTime() <= from.getTime()
  ) {
    loadError.value = t("analytics.errInvalidRange");
    return;
  }
  const duration = to.getTime() - from.getTime();
  if (duration > 366 * 86_400_000) {
    loadError.value = t("analytics.errMax366Days");
    return;
  }
  if (granularity.value === "hour" && duration > 7 * 86_400_000) {
    loadError.value = t("analytics.errHourMax7Days");
    return;
  }
  const sequence = ++requestSequence;
  loading.value = true;
  try {
    const { data } = await adminApi.get("/analytics/overview", {
      params: {
        from: from.toISOString(),
        to: to.toISOString(),
        granularity: granularity.value,
      },
    });
    if (sequence !== requestSequence) return;
    payload.value = data.data as AnalyticsPayload;
  } catch (error) {
    if (sequence === requestSequence)
      loadError.value = responseMessage(error, t("analytics.errLoad"));
  } finally {
    if (sequence === requestSequence) loading.value = false;
  }
}

function applyCustomRange() {
  activePreset.value = 0;
  loadAnalytics();
}

function resetRange() {
  preset(30);
}

function exportSeries() {
  if (!series.value.length) return;
  const rows = [
    [
      t("analytics.colUtcTime"),
      t("analytics.colGrossRevenueCents"),
      t("analytics.colRefundsCents"),
      t("analytics.colNetRevenueCents"),
      t("analytics.ordersCreated"),
      t("analytics.paidOrders"),
      t("analytics.colDeliveredOrders"),
      t("analytics.colNewUsers"),
    ],
    ...series.value.map((point) => [
      point.bucket,
      point.gross_revenue,
      point.refunds,
      point.net_revenue,
      point.orders_created,
      point.paid_orders,
      point.delivered_orders,
      point.new_users,
    ]),
  ];
  const csv = `\uFEFF${rows.map((row) => row.map(safeCSVCell).join(",")).join("\r\n")}`;
  const url = URL.createObjectURL(
    new Blob([csv], { type: "text/csv;charset=utf-8" }),
  );
  const link = document.createElement("a");
  link.href = url;
  link.download = `linlinqi-analytics-${new Date().toISOString().slice(0, 10)}.csv`;
  link.click();
  URL.revokeObjectURL(url);
}

onMounted(loadAnalytics);
</script>

<template>
  <section class="analytics-view">
    <header class="analytics-toolbar">
      <div class="preset-group" :aria-label="t('analytics.presetLabel')">
        <button
          v-for="days in [7, 30, 90]"
          :key="days"
          type="button"
          :class="{ active: activePreset === days }"
          :disabled="loading"
          @click="preset(days)"
        >
          {{ t("analytics.lastNDays", { days }) }}
        </button>
      </div>
      <div class="range-controls">
        <label
          ><span>{{ t("analytics.startTime") }}</span
          ><input v-model="fromInput" type="datetime-local"
        /></label>
        <label
          ><span>{{ t("analytics.endTime") }}</span
          ><input v-model="toInput" type="datetime-local"
        /></label>
        <label
          ><span>{{ t("analytics.granularity") }}</span
          ><select v-model="granularity">
            <option value="day">{{ t("analytics.granularityDay") }}</option>
            <option value="hour">
              {{ t("analytics.granularityHour") }}
            </option>
          </select></label
        >
        <button
          type="button"
          class="primary-button compact"
          :disabled="loading"
          @click="applyCustomRange"
        >
          <CalendarRange :size="14" />{{ t("analytics.apply") }}
        </button>
      </div>
      <div class="toolbar-actions">
        <button type="button" :disabled="loading" @click="resetRange">
          <RotateCcw :size="14" />{{ t("analytics.reset") }}</button
        ><button type="button" :disabled="!series.length" @click="exportSeries">
          <Download :size="14" />{{ t("analytics.exportAggregated") }}</button
        ><button type="button" :disabled="loading" @click="loadAnalytics">
          <RefreshCw :size="14" :class="{ spinning: loading }" />{{
            t("analytics.refresh")
          }}
        </button>
      </div>
    </header>

    <div v-if="loadError" class="analytics-alert">
      <Activity :size="15" /><span>{{ loadError }}</span
      ><button type="button" @click="loadAnalytics">
        {{ t("analytics.retry") }}
      </button>
    </div>

    <div class="analytics-context">
      <span><i></i>{{ t("analytics.accountingBasis") }}</span>
      <span
        >{{ payload.range.timezone }} ·
        {{
          payload.range.granularity === "hour"
            ? t("analytics.granularityHourLabel")
            : t("analytics.granularityDayLabel")
        }}</span
      >
    </div>

    <section class="analytics-kpis" :aria-busy="loading">
      <article>
        <span class="kpi-icon"><TrendingUp :size="17" /></span>
        <div>
          <small>{{ t("analytics.netRevenue") }}</small
          ><strong>{{ money(metrics.net_revenue) }}</strong>
        </div>
        <em>{{
          t("analytics.grossMinusRefunds", {
            gross: money(metrics.gross_revenue),
            refunds: money(metrics.refunds),
          })
        }}</em>
      </article>
      <article>
        <span class="kpi-icon"><ShoppingCart :size="17" /></span>
        <div>
          <small>{{ t("analytics.paidOrders") }}</small
          ><strong>{{ integer(metrics.paid_orders) }}</strong>
        </div>
        <em>{{
          t("analytics.createdAndAov", {
            created: integer(metrics.orders_created),
            aov: money(metrics.average_order_value),
          })
        }}</em>
      </article>
      <article>
        <span class="kpi-icon"><CreditCard :size="17" /></span>
        <div>
          <small>{{ t("analytics.paymentIntentSuccessRate") }}</small
          ><strong>{{ percent(metrics.payment_success_rate) }}</strong>
        </div>
        <em>{{
          t("analytics.refundAmountRate", {
            rate: percent(metrics.refund_rate),
          })
        }}</em>
      </article>
      <article>
        <span class="kpi-icon"><Package :size="17" /></span>
        <div>
          <small>{{ t("analytics.deliveryCompleted") }}</small
          ><strong>{{ integer(metrics.delivered_orders) }}</strong>
        </div>
        <em>{{
          t("analytics.newUsersCount", { count: integer(metrics.new_users) })
        }}</em>
      </article>
    </section>

    <section class="analytics-main-grid">
      <article class="analytics-panel trend-panel">
        <header>
          <div>
            <h2>{{ t("analytics.revenueTrend") }}</h2>
            <p>{{ t("analytics.revenueTrendDesc") }}</p>
          </div>
          <div class="chart-legend">
            <span class="gross">{{ t("analytics.grossRevenue") }}</span
            ><span class="refund">{{ t("analytics.successfulRefunds") }}</span>
          </div>
        </header>
        <div v-if="series.length" class="trend-chart">
          <svg
            viewBox="0 0 760 250"
            role="img"
            :aria-label="t('analytics.revenueTrendChart')"
          >
            <line
              v-for="y in [42, 100, 158, 216]"
              :key="y"
              x1="34"
              :y1="y"
              x2="726"
              :y2="y"
              class="grid-line"
            />
            <polygon v-if="grossArea" :points="grossArea" class="gross-area" />
            <polyline :points="grossPolyline" class="gross-line" />
            <polyline :points="refundPolyline" class="refund-line" />
            <g
              v-for="coordinate in chartCoordinates"
              :key="coordinate.point.bucket"
            >
              <circle
                :cx="coordinate.x"
                :cy="coordinate.grossY"
                r="3"
                class="gross-dot"
              >
                <title>{{ pointTitle(coordinate.point) }}</title>
              </circle>
              <circle
                :cx="coordinate.x"
                :cy="coordinate.refundY"
                r="2.5"
                class="refund-dot"
              >
                <title>{{ pointTitle(coordinate.point) }}</title>
              </circle>
            </g>
          </svg>
          <div class="trend-axis">
            <span
              v-for="coordinate in axisLabels"
              :key="coordinate.point.bucket"
              :style="{ left: `${(coordinate.x / 760) * 100}%` }"
              >{{ pointLabel(coordinate.point.bucket) }}</span
            >
          </div>
        </div>
        <div v-else class="analytics-empty">
          <BarChart3 :size="27" /><strong>{{
            t("analytics.noTrendData")
          }}</strong>
        </div>
      </article>

      <article class="analytics-panel events-panel">
        <header>
          <div>
            <h2>{{ t("analytics.businessEvents") }}</h2>
            <p>{{ t("analytics.businessEventsDesc") }}</p>
          </div>
        </header>
        <div class="event-list">
          <div>
            <span
              ><ShoppingCart :size="15" />{{
                t("analytics.ordersCreated")
              }}</span
            ><strong>{{ integer(metrics.orders_created) }}</strong>
          </div>
          <div>
            <span
              ><CreditCard :size="15" />{{
                t("analytics.paymentCompleted")
              }}</span
            ><strong>{{ integer(metrics.paid_orders) }}</strong>
          </div>
          <div>
            <span
              ><Package :size="15" />{{
                t("analytics.deliveryCompleted")
              }}</span
            ><strong>{{ integer(metrics.delivered_orders) }}</strong>
          </div>
          <div>
            <span
              ><UserPlus :size="15" />{{
                t("analytics.newRegisteredCustomers")
              }}</span
            ><strong>{{ integer(metrics.new_users) }}</strong>
          </div>
        </div>
        <p class="event-note">{{ t("analytics.eventNote") }}</p>
      </article>
    </section>

    <section class="analytics-lower-grid">
      <article class="analytics-panel funnel-panel">
        <header>
          <div>
            <h2>{{ t("analytics.funnelTitle") }}</h2>
            <p>{{ t("analytics.funnelDesc") }}</p>
          </div>
        </header>
        <div class="funnel-list">
          <div v-for="(row, index) in funnelRows" :key="row.key">
            <span
              >{{ index + 1 }}<b>{{ row.label }}</b></span
            >
            <div>
              <i
                :style="{
                  width: `${funnel.orders ? Math.max(3, (row.value / funnel.orders) * 100) : 0}%`,
                }"
              ></i>
            </div>
            <strong>{{ integer(row.value) }}</strong>
          </div>
        </div>
        <footer>
          <span
            >{{ t("analytics.funnelPaymentRate") }}
            <b>{{ percent(funnel.payment_rate) }}</b></span
          ><span
            >{{ t("analytics.funnelDeliveryRate") }}
            <b>{{ percent(funnel.delivery_rate) }}</b></span
          >
        </footer>
      </article>

      <article class="analytics-panel channels-panel">
        <header>
          <div>
            <h2>{{ t("analytics.channelsTitle") }}</h2>
            <p>{{ t("analytics.channelsDesc") }}</p>
          </div>
        </header>
        <div v-if="channels.length" class="channel-table-wrap">
          <table>
            <thead>
              <tr>
                <th>{{ t("analytics.colChannel") }}</th>
                <th>{{ t("analytics.colIntent") }}</th>
                <th>{{ t("analytics.colSuccessRate") }}</th>
                <th>{{ t("analytics.paidOrders") }}</th>
                <th>{{ t("analytics.colRevenue") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="channel in channels" :key="channel.code">
                <td>
                  <strong>{{ channel.name || channel.code }}</strong
                  ><code>{{ channel.code }}</code>
                </td>
                <td>
                  {{ integer(channel.succeeded) }} /
                  {{ integer(channel.attempts) }}
                </td>
                <td>
                  <span class="rate-chip">{{
                    percent(channel.success_rate)
                  }}</span>
                </td>
                <td>{{ integer(channel.paid_orders) }}</td>
                <td>
                  <strong>{{ money(channel.gross_revenue) }}</strong>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="analytics-empty small">
          <CreditCard :size="24" /><strong>{{
            t("analytics.noChannelsData")
          }}</strong>
        </div>
      </article>
    </section>

    <article class="analytics-panel products-panel">
      <header>
        <div>
          <h2>{{ t("analytics.productsTitle") }}</h2>
          <p>{{ t("analytics.productsDesc") }}</p>
        </div>
      </header>
      <div v-if="products.length" class="product-table-wrap">
        <table>
          <thead>
            <tr>
              <th>{{ t("analytics.colRankProduct") }}</th>
              <th>{{ t("analytics.colRevenueShare") }}</th>
              <th>{{ t("analytics.paidOrders") }}</th>
              <th>{{ t("analytics.colUnitsSold") }}</th>
              <th>{{ t("analytics.grossRevenue") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(product, index) in products" :key="product.product_id">
              <td>
                <span class="rank">{{ index + 1 }}</span
                ><strong>{{ product.product_name }}</strong>
              </td>
              <td>
                <div class="product-share">
                  <i
                    :style="{
                      width: `${(product.gross_revenue / maxProductRevenue) * 100}%`,
                    }"
                  ></i>
                </div>
              </td>
              <td>{{ integer(product.order_count) }}</td>
              <td>{{ integer(product.units) }}</td>
              <td>
                <strong>{{ money(product.gross_revenue) }}</strong>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="analytics-empty small">
        <Package :size="24" /><strong>{{
          t("analytics.noProductsData")
        }}</strong>
      </div>
    </article>

    <div v-if="loading" class="analytics-loading">
      <LoaderCircle :size="22" class="spinning" /><span>{{
        t("analytics.loading")
      }}</span>
    </div>
  </section>
</template>

<style scoped>
.analytics-view {
  position: relative;
  display: grid;
  gap: 14px;
  padding: 18px;
}
.analytics-toolbar {
  display: grid;
  grid-template-columns: auto minmax(520px, 1fr) auto;
  align-items: end;
  gap: 14px;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.preset-group,
.toolbar-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}
.preset-group button,
.toolbar-actions button {
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
  white-space: nowrap;
}
.preset-group button.active {
  background: var(--dark);
  color: var(--dark-text);
  border-color: var(--dark);
}
button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.range-controls {
  display: grid;
  grid-template-columns: 1fr 1fr minmax(145px, 0.7fr) auto;
  align-items: end;
  gap: 8px;
}
.range-controls label {
  display: grid;
  gap: 5px;
}
.range-controls label span {
  color: var(--muted);
  font-size: 9px;
  font-weight: 650;
}
.range-controls input,
.range-controls select {
  width: 100%;
  height: 34px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  outline: 0;
  background: var(--surface-2);
  font-size: 10px;
}
.range-controls input:focus,
.range-controls select:focus {
  border-color: var(--dark);
}
.analytics-alert {
  min-height: 40px;
  padding: 8px 12px;
  display: flex;
  align-items: center;
  gap: 8px;
  border: 1px solid color-mix(in srgb, var(--danger) 35%, var(--line));
  border-radius: 8px;
  background: color-mix(in srgb, var(--danger) 7%, var(--surface));
  color: var(--danger);
  font-size: 10px;
}
.analytics-alert span {
  flex: 1;
}
.analytics-alert button {
  border: 0;
  background: transparent;
  color: inherit;
  font-weight: 700;
}
.analytics-context {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  color: var(--muted);
  font-size: 9px;
}
.analytics-context span {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.analytics-context i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--success);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--success) 13%, transparent);
}
.analytics-kpis {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.analytics-kpis article {
  min-height: 126px;
  padding: 16px;
  display: grid;
  grid-template-columns: auto 1fr;
  align-items: start;
  gap: 12px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.kpi-icon {
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  background: var(--soft);
}
.analytics-kpis article div {
  display: grid;
  gap: 7px;
  min-width: 0;
}
.analytics-kpis small {
  color: var(--muted);
  font-size: 9px;
  font-weight: 650;
}
.analytics-kpis strong {
  font-size: clamp(18px, 2vw, 25px);
  letter-spacing: -0.04em;
  overflow-wrap: anywhere;
}
.analytics-kpis em {
  grid-column: 1 / -1;
  align-self: end;
  color: var(--muted);
  font-size: 9px;
  font-style: normal;
}
.analytics-main-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.75fr) minmax(260px, 0.65fr);
  gap: 12px;
}
.analytics-lower-grid {
  display: grid;
  grid-template-columns: minmax(300px, 0.75fr) minmax(0, 1.25fr);
  gap: 12px;
}
.analytics-panel {
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: var(--shadow);
  overflow: hidden;
}
.analytics-panel > header {
  min-height: 66px;
  padding: 14px 16px;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid var(--line);
}
.analytics-panel h2 {
  margin: 0 0 4px;
  font-size: 13px;
}
.analytics-panel header p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.6;
}
.chart-legend {
  display: flex;
  gap: 12px;
  color: var(--muted);
  font-size: 9px;
  white-space: nowrap;
}
.chart-legend span {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.chart-legend span:before {
  content: "";
  width: 16px;
  height: 2px;
  border-radius: 3px;
}
.chart-legend .gross:before {
  background: var(--dark);
}
.chart-legend .refund:before {
  background: var(--danger);
}
.trend-chart {
  position: relative;
  padding: 12px 14px 25px;
}
.trend-chart svg {
  width: 100%;
  height: 240px;
  overflow: visible;
}
.grid-line {
  stroke: var(--line);
  stroke-width: 1;
  stroke-dasharray: 3 5;
}
.gross-area {
  fill: color-mix(in srgb, var(--dark) 7%, transparent);
}
.gross-line,
.refund-line {
  fill: none;
  stroke-linejoin: round;
  stroke-linecap: round;
}
.gross-line {
  stroke: var(--dark);
  stroke-width: 2.5;
}
.refund-line {
  stroke: var(--danger);
  stroke-width: 2;
}
.gross-dot {
  fill: var(--surface);
  stroke: var(--dark);
  stroke-width: 2;
}
.refund-dot {
  fill: var(--surface);
  stroke: var(--danger);
  stroke-width: 1.5;
}
.trend-axis {
  position: absolute;
  left: 14px;
  right: 14px;
  bottom: 13px;
  height: 14px;
  color: var(--muted);
  font-size: 8px;
}
.trend-axis span {
  position: absolute;
  transform: translateX(-50%);
  white-space: nowrap;
}
.event-list {
  display: grid;
  padding: 8px 16px;
}
.event-list div {
  min-height: 49px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid var(--line);
}
.event-list div:last-child {
  border: 0;
}
.event-list span {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  color: var(--muted);
  font-size: 10px;
}
.event-list strong {
  font-size: 15px;
}
.event-note {
  margin: 0 16px 16px;
  padding: 10px;
  border-radius: 7px;
  background: var(--soft);
  color: var(--muted);
  font-size: 9px;
  line-height: 1.65;
}
.funnel-list {
  padding: 15px 16px 8px;
  display: grid;
  gap: 12px;
}
.funnel-list > div {
  display: grid;
  grid-template-columns: 105px 1fr 65px;
  align-items: center;
  gap: 9px;
}
.funnel-list span {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  font-size: 9px;
}
.funnel-list span b {
  color: var(--text);
  font-size: 10px;
}
.funnel-list > div > div {
  height: 7px;
  overflow: hidden;
  border-radius: 10px;
  background: var(--soft);
}
.funnel-list i {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--dark);
}
.funnel-list strong {
  text-align: right;
  font-size: 12px;
}
.funnel-panel footer {
  padding: 12px 16px;
  display: flex;
  justify-content: space-between;
  gap: 12px;
  border-top: 1px solid var(--line);
  color: var(--muted);
  font-size: 9px;
}
.funnel-panel footer b {
  margin-left: 4px;
  color: var(--text);
}
.channel-table-wrap,
.product-table-wrap {
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
  letter-spacing: 0.04em;
  white-space: nowrap;
}
td {
  padding: 11px 12px;
  border-top: 1px solid var(--line);
  font-size: 10px;
  white-space: nowrap;
}
td strong {
  font-size: 10px;
}
.channels-panel td:first-child {
  display: grid;
  gap: 3px;
}
.channels-panel code {
  color: var(--muted);
  font-size: 8px;
}
.rate-chip {
  padding: 3px 6px;
  border-radius: 10px;
  background: color-mix(in srgb, var(--success) 10%, var(--surface));
  color: var(--success);
  font-size: 8px;
  font-weight: 700;
}
.products-panel td:first-child {
  display: flex;
  align-items: center;
  gap: 9px;
  min-width: 240px;
}
.rank {
  width: 23px;
  height: 23px;
  display: grid;
  place-items: center;
  border-radius: 6px;
  background: var(--soft);
  color: var(--muted);
  font-size: 9px;
  font-weight: 700;
}
.product-share {
  width: min(210px, 18vw);
  height: 7px;
  overflow: hidden;
  border-radius: 8px;
  background: var(--soft);
}
.product-share i {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--dark);
}
.analytics-empty {
  min-height: 250px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 9px;
  color: var(--muted);
}
.analytics-empty.small {
  min-height: 145px;
}
.analytics-empty strong {
  font-size: 10px;
}
.analytics-loading {
  position: fixed;
  right: 24px;
  bottom: 22px;
  z-index: 20;
  padding: 10px 13px;
  display: flex;
  align-items: center;
  gap: 8px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
  box-shadow: var(--shadow);
  color: var(--muted);
  font-size: 10px;
}
.spinning {
  animation: analytics-spin 0.8s linear infinite;
}
@keyframes analytics-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 1220px) {
  .analytics-toolbar {
    grid-template-columns: 1fr auto;
  }
  .range-controls {
    grid-column: 1 / -1;
    grid-row: 2;
  }
  .analytics-kpis {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 850px) {
  .analytics-view {
    padding: 12px;
  }
  .analytics-main-grid,
  .analytics-lower-grid {
    grid-template-columns: 1fr;
  }
  .analytics-toolbar {
    grid-template-columns: 1fr;
    align-items: stretch;
  }
  .range-controls {
    grid-column: auto;
    grid-row: auto;
    grid-template-columns: 1fr 1fr;
  }
  .toolbar-actions {
    flex-wrap: wrap;
  }
  .analytics-context {
    align-items: flex-start;
    flex-direction: column;
  }
  .trend-chart svg {
    height: 205px;
  }
}
@media (max-width: 560px) {
  .analytics-kpis {
    grid-template-columns: 1fr;
  }
  .range-controls {
    grid-template-columns: 1fr;
  }
  .preset-group {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
  }
  .toolbar-actions {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }
  .toolbar-actions button:last-child {
    grid-column: 1 / -1;
  }
  .funnel-list > div {
    grid-template-columns: 92px 1fr 50px;
  }
  .chart-legend {
    flex-direction: column;
    gap: 5px;
  }
  .analytics-panel > header {
    min-height: 60px;
  }
  .product-share {
    width: 110px;
  }
}
</style>
