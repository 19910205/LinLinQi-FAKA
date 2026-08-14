<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { RouterLink } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  ArrowDownRight,
  ArrowRight,
  ArrowUpRight,
  CircleDollarSign,
  ClipboardList,
  Clock3,
  PackageCheck,
  RefreshCw,
  ShieldCheck,
  ShoppingBag,
  UsersRound,
  WalletCards,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import { formatMoney, storeCurrency } from "../utils/money";

const { t, locale } = useI18n();
const authStore = useAuthStore();
const canManageCatalog = computed(() =>
  authStore.hasPermission("catalog.manage"),
);
const canManageInventory = computed(() =>
  authStore.hasPermission("inventory.manage"),
);
const canManageOrders = computed(() => authStore.hasPermission("order.manage"));

const stats = ref({
  currency: "",
  today_revenue: 0,
  today_orders: 0,
  total_users: 0,
  active_products: 0,
  low_stock: 0,
  revenue_change: 0,
  order_change: 0,
  monthly_new_users: 0,
  average_order_value: 0,
  delivery_success_rate: 0,
  payment_success_rate: 0,
  average_delivery_ms: 0,
  retried_orders: 0,
  hourly_revenue: Array(24).fill(0) as number[],
});
const orders = ref<any[]>([]);
const loadError = ref("");
const currentUTCHour = new Date().getUTCHours();
const bars = computed(() => {
  const maximum = Math.max(...stats.value.hourly_revenue, 1);
  return stats.value.hourly_revenue.map((value) =>
    Math.max(2, Math.round((value / maximum) * 100)),
  );
});
const money = (n: number, currency?: string) =>
  formatMoney(
    n,
    currency || stats.value.currency || storeCurrency.value,
    locale.value,
  );
const formatDate = (value: string) =>
  new Date(value).toLocaleString(locale.value, { hour12: false });
const statusLabel = (value: string) =>
  ({
    delivered: t("dashboard.orderStatuses.delivered"),
    pending_payment: t("dashboard.orderStatuses.pending_payment"),
    processing: t("dashboard.orderStatuses.processing"),
    refunding: t("dashboard.orderStatuses.refunding"),
    refunded: t("dashboard.orderStatuses.refunded"),
    expired: t("dashboard.orderStatuses.expired"),
    failed: t("dashboard.orderStatuses.failed"),
  })[value] || value;
onMounted(async () => {
  try {
    const { data } = await adminApi.get("/dashboard");
    stats.value = { ...stats.value, ...data.data };
    orders.value = (data.data.recent_orders || []).map((o: any) => ({
      ...o,
      product: o.items?.[0]?.product_name || t("dashboard.digitalGoods"),
    }));
  } catch {
    loadError.value = t("dashboard.errLoad");
  }
});
</script>

<template>
  <div class="dashboard">
    <p v-if="loadError" class="module-state error">{{ loadError }}</p>
    <section class="metrics-grid">
      <article>
        <div class="metric-head">
          <span>{{ t("dashboard.todayRevenue") }}</span
          ><i><CircleDollarSign :size="18" /></i>
        </div>
        <strong>{{ money(stats.today_revenue) }}</strong>
        <p :class="stats.revenue_change >= 0 ? 'positive' : 'warning'">
          <ArrowUpRight
            v-if="stats.revenue_change >= 0"
            :size="14"
          /><ArrowDownRight v-else :size="14" />
          {{ stats.revenue_change.toFixed(1) }}%
          <span>{{ t("dashboard.vsYesterday") }}</span>
        </p>
      </article>
      <article>
        <div class="metric-head">
          <span>{{ t("dashboard.todayOrders") }}</span
          ><i><ShoppingBag :size="18" /></i>
        </div>
        <strong>{{ stats.today_orders }}</strong>
        <p :class="stats.order_change >= 0 ? 'positive' : 'warning'">
          <ArrowUpRight
            v-if="stats.order_change >= 0"
            :size="14"
          /><ArrowDownRight v-else :size="14" />
          {{ stats.order_change.toFixed(1) }}%
          <span>{{ t("dashboard.vsYesterday") }}</span>
        </p>
      </article>
      <article>
        <div class="metric-head">
          <span>{{ t("dashboard.totalUsers") }}</span
          ><i><UsersRound :size="18" /></i>
        </div>
        <strong>{{ stats.total_users.toLocaleString() }}</strong>
        <p class="positive">
          <ArrowUpRight :size="14" /> {{ stats.monthly_new_users }}
          <span>{{ t("dashboard.monthlyNew") }}</span>
        </p>
      </article>
      <article>
        <div class="metric-head">
          <span>{{ t("dashboard.activeProducts") }}</span
          ><i><PackageCheck :size="18" /></i>
        </div>
        <strong>{{ stats.active_products }}</strong>
        <p class="warning">
          <ArrowDownRight :size="14" /> {{ stats.low_stock }}
          <span>{{ t("dashboard.lowStockWarn") }}</span>
        </p>
      </article>
    </section>
    <section class="dashboard-row">
      <article class="panel revenue-panel">
        <header>
          <div>
            <h2>{{ t("dashboard.revenueTrend") }}</h2>
            <p>{{ t("dashboard.revenueTrendSub") }}</p>
          </div>
          <div class="panel-tools">
            <span class="active">{{ t("dashboard.utcToday") }}</span>
          </div>
        </header>
        <div class="chart-summary">
          <div>
            <span>{{ t("dashboard.totalRevenue") }}</span
            ><strong>{{ money(stats.today_revenue) }}</strong>
          </div>
          <div>
            <span>{{ t("dashboard.avgOrderValue") }}</span
            ><strong>{{ money(stats.average_order_value) }}</strong>
          </div>
          <div>
            <span>{{ t("dashboard.paymentSuccessRate") }}</span
            ><strong>{{ stats.payment_success_rate.toFixed(2) }}%</strong>
          </div>
        </div>
        <div class="bar-chart">
          <span
            v-for="(bar, index) in bars"
            :key="index"
            :style="{ height: `${bar}%` }"
            :class="{ hot: index === currentUTCHour }"
          ></span>
        </div>
        <div class="chart-axis">
          <span>00:00</span><span>06:00</span><span>12:00</span
          ><span>18:00</span><span>{{ t("dashboard.now") }}</span>
        </div>
      </article>
      <article class="panel delivery-panel">
        <header>
          <div>
            <h2>{{ t("dashboard.deliveryQuality") }}</h2>
            <p>{{ t("dashboard.deliveryQualitySub") }}</p>
          </div>
          <span class="healthy"
            ><i></i>
            {{
              stats.today_orders ? t("dashboard.live") : t("dashboard.noOrders")
            }}</span
          >
        </header>
        <div class="delivery-score">
          <div
            class="score-ring"
            :style="{
              background: `conic-gradient(var(--dark) ${stats.delivery_success_rate}%,var(--soft) 0)`,
            }"
          >
            <strong
              >{{ stats.delivery_success_rate.toFixed(2)
              }}<small>%</small></strong
            ><span>{{ t("dashboard.deliverySuccessRate") }}</span>
          </div>
        </div>
        <div class="quality-list">
          <div>
            <span
              ><Clock3 :size="15" />{{ t("dashboard.avgDeliveryTime") }}</span
            ><b>{{
              t("dashboard.seconds", {
                n: (stats.average_delivery_ms / 1000).toFixed(2),
              })
            }}</b>
          </div>
          <div>
            <span
              ><ShieldCheck :size="15" />{{
                t("dashboard.paymentSuccessRate")
              }}</span
            ><b>{{ stats.payment_success_rate.toFixed(2) }}%</b>
          </div>
          <div>
            <span><RefreshCw :size="15" />{{ t("dashboard.autoRetry") }}</span
            ><b>{{
              t("dashboard.ordersCount", { n: stats.retried_orders })
            }}</b>
          </div>
        </div>
      </article>
    </section>
    <section class="dashboard-row lower">
      <article class="panel orders-panel">
        <header>
          <div>
            <h2>{{ t("dashboard.recentOrders") }}</h2>
            <p>{{ t("dashboard.recentOrdersSub") }}</p>
          </div>
          <RouterLink to="/orders"
            >{{ t("dashboard.viewAll") }} <ArrowRight :size="14"
          /></RouterLink>
        </header>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>{{ t("dashboard.orderNo") }}</th>
                <th>{{ t("dashboard.product") }}</th>
                <th>{{ t("dashboard.amount") }}</th>
                <th>{{ t("dashboard.status") }}</th>
                <th>{{ t("dashboard.time") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="order in orders" :key="order.order_no">
                <td>
                  <b>{{ order.order_no }}</b>
                </td>
                <td>{{ order.product }}</td>
                <td>
                  <b>{{ money(order.total, order.currency) }}</b>
                </td>
                <td>
                  <span :class="['status', order.status]">{{
                    statusLabel(order.status)
                  }}</span>
                </td>
                <td>{{ formatDate(order.created_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </article>
      <article class="panel quick-panel">
        <header>
          <div>
            <h2>{{ t("dashboard.quickActions") }}</h2>
            <p>{{ t("dashboard.quickActionsSub") }}</p>
          </div>
        </header>
        <div class="quick-list">
          <RouterLink v-if="canManageCatalog" to="/products?create=1"
            ><i><ShoppingBag :size="17" /></i>
            <div>
              <b>{{ t("dashboard.createProduct") }}</b
              ><span>{{ t("dashboard.createProductSub") }}</span>
            </div>
            <ArrowRight :size="15" /></RouterLink
          ><RouterLink v-if="canManageInventory" to="/inventory?import=1"
            ><i><WalletCards :size="17" /></i>
            <div>
              <b>{{ t("dashboard.importCards") }}</b
              ><span>{{ t("dashboard.importCardsSub") }}</span>
            </div>
            <ArrowRight :size="15" /></RouterLink
          ><RouterLink v-if="canManageOrders" to="/orders?manual=1"
            ><i><ClipboardList :size="17" /></i>
            <div>
              <b>{{ t("dashboard.manualOrder") }}</b
              ><span>{{ t("dashboard.manualOrderSub") }}</span>
            </div>
            <ArrowRight :size="15"
          /></RouterLink>
        </div>
      </article>
    </section>
  </div>
</template>
