<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  Check,
  ChevronDown,
  Download,
  Filter,
  MoreHorizontal,
  Plus,
  Search,
  SlidersHorizontal,
  Upload,
  X,
} from "@lucide/vue";
import { adminApi } from "../stores/auth";
import { safeCSVCell } from "../utils/csv";

const { t, locale } = useI18n();

const route = useRoute();
const search = ref("");
const modal = ref(false);
const actionPayload = ref("{}");
const actionReason = ref("");
const actionError = ref("");
const actionNotice = ref("");
const actionIdempotencyKey = ref("");
const saving = ref(false);
const kind = computed(() => String(route.meta.kind || "products"));
const definitions: Record<string, any> = {
  orders: {
    action: t("module.actionExportOrders"),
    actionIcon: Download,
    filters: [
      t("module.filterAllOrders"),
      t("module.filterPending"),
      t("module.filterProcessing"),
      t("module.filterDelivered"),
      t("module.filterRefundAfterSales"),
    ],
    columns: [
      t("module.colOrderNo"),
      t("module.colCustomer"),
      t("module.colProduct"),
      t("module.colAmount"),
      t("module.colPayMethod"),
      t("module.colStatus"),
      t("module.colOrderTime"),
    ],
    rows: [
      [
        "LLQ2026080900186",
        "lin***@mail.com",
        "AI Pro 月度会员",
        "¥129.00",
        "支付宝",
        "已交付",
        "2026-08-09 14:32",
      ],
      [
        "LLQ2026080900185",
        "chen***@qq.com",
        "Steam 100 元充值卡 ×2",
        "¥194.00",
        "微信支付",
        "已交付",
        "2026-08-09 14:29",
      ],
      [
        "LLQ2026080900184",
        "alex***@outlook.com",
        "云服务器基础版",
        "¥49.00",
        "支付宝",
        "处理中",
        "2026-08-09 14:25",
      ],
      [
        "LLQ2026080900183",
        "gao***@163.com",
        "流媒体家庭组季卡",
        "¥88.00",
        "余额",
        "已交付",
        "2026-08-09 14:21",
      ],
      [
        "LLQ2026080900182",
        "yu***@mail.com",
        "设计协作专业版",
        "¥39.00",
        "支付宝",
        "退款中",
        "2026-08-09 14:16",
      ],
    ],
  },
  products: {
    action: t("module.actionCreateProduct"),
    actionIcon: Plus,
    filters: [
      t("module.filterAllProducts"),
      t("module.filterOnSale"),
      t("module.filterDraft"),
      t("module.filterOffSale"),
    ],
    columns: [
      t("module.colProduct"),
      t("module.colCategory"),
      t("module.colPrice"),
      t("module.colStock"),
      t("module.colSales"),
      t("module.colDeliveryType"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "AI Pro 月度会员",
        "AI 与效率",
        "¥129.00",
        "38",
        "1,286",
        "自动发货",
        "销售中",
      ],
      [
        "云服务器基础版",
        "开发与云服务",
        "¥49.00",
        "99",
        "869",
        "供应商直充",
        "销售中",
      ],
      [
        "流媒体家庭组季卡",
        "影音娱乐",
        "¥88.00",
        "12",
        "2,158",
        "自动发货",
        "销售中",
      ],
      [
        "Steam 100 元充值卡",
        "游戏点卡",
        "¥97.00",
        "66",
        "5,320",
        "自动发货",
        "销售中",
      ],
      [
        "设计协作专业版",
        "AI 与效率",
        "¥39.00",
        "21",
        "740",
        "自动发货",
        "销售中",
      ],
    ],
  },
  inventory: {
    action: t("module.actionBulkImport"),
    actionIcon: Upload,
    filters: [
      t("module.filterAllInventory"),
      t("module.filterAvailable"),
      t("module.filterLocked"),
      t("module.filterSold"),
      t("module.filterAbnormal"),
    ],
    columns: [
      t("module.colProduct"),
      t("module.colInventoryMode"),
      t("module.colAvailableStock"),
      t("module.colLocked"),
      t("module.filterSold"),
      t("module.colThreshold"),
      t("module.colStatus"),
    ],
    rows: [
      ["AI Pro 月度会员", "本地卡密", "38", "2", "1,286", "10", "充足"],
      ["云服务器基础版", "供应商同步", "99", "0", "869", "20", "充足"],
      ["流媒体家庭组季卡", "本地卡密", "12", "1", "2,158", "10", "偏低"],
      ["Git 托管年度增强版", "供应商同步", "8", "0", "326", "10", "预警"],
      ["设计协作专业版", "本地卡密", "21", "0", "740", "10", "充足"],
    ],
  },
  suppliers: {
    action: t("module.actionAddSupplier"),
    actionIcon: Plus,
    filters: [
      t("module.filterAllSuppliers"),
      t("module.filterHealthy"),
      t("module.filterSyncError"),
      t("module.filterDisabled"),
    ],
    columns: [
      t("module.colSupplier"),
      t("module.colProtocol"),
      t("module.colLinkedProducts"),
      t("module.colBalance"),
      t("module.colSuccessRate"),
      t("module.colLastSync"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "星云数字供应链",
        "LinLinQi Standard",
        "12",
        "¥28,640.00",
        "99.98%",
        "1 分钟前",
        "运行正常",
      ],
      [
        "CloudBridge",
        "REST / HMAC",
        "8",
        "¥12,850.40",
        "99.72%",
        "3 分钟前",
        "运行正常",
      ],
      [
        "点卡全球仓",
        "Partner REST v2",
        "24",
        "¥8,960.00",
        "98.91%",
        "6 分钟前",
        "运行正常",
      ],
      [
        "MediaHub",
        "REST / Token",
        "5",
        "¥3,210.00",
        "94.20%",
        "28 分钟前",
        "同步异常",
      ],
    ],
  },
  customers: {
    action: t("module.actionExportCustomers"),
    actionIcon: Download,
    filters: [
      t("module.filterAllCustomers"),
      t("module.filterActiveCustomers"),
      t("module.filterHighValue"),
      t("module.filterBanned"),
    ],
    columns: [
      t("module.colCustomer"),
      t("module.colContact"),
      t("module.colOrders"),
      t("module.colTotalSpend"),
      t("module.colBalance"),
      t("module.colLastActive"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "陈屿",
        "chen***@qq.com",
        "28",
        "¥3,860.00",
        "¥120.00",
        "2 分钟前",
        "正常",
      ],
      [
        "林晓",
        "lin***@mail.com",
        "16",
        "¥2,412.00",
        "¥0.00",
        "5 分钟前",
        "正常",
      ],
      [
        "Alex Wang",
        "alex***@outlook.com",
        "9",
        "¥1,328.00",
        "¥58.00",
        "9 分钟前",
        "正常",
      ],
      [
        "高宁",
        "gao***@163.com",
        "42",
        "¥6,930.00",
        "¥340.00",
        "12 分钟前",
        "高价值",
      ],
    ],
  },
  payments: {
    action: t("module.actionAddChannel"),
    actionIcon: Plus,
    filters: [
      t("module.filterAllChannels"),
      t("module.filterEnabled"),
      t("module.filterMaintenance"),
      t("module.filterDisabled"),
    ],
    columns: [
      t("module.colPayChannel"),
      t("module.colProvider"),
      t("module.colTodayVolume"),
      t("module.colFeeRate"),
      t("module.colSuccessRate"),
      t("module.colLastCallback"),
      t("module.colStatus"),
    ],
    rows: [
      ["支付宝", "官方直连", "¥68,420.00", "0.60%", "99.96%", "刚刚", "已启用"],
      [
        "微信支付",
        "官方直连",
        "¥52,680.00",
        "0.60%",
        "99.91%",
        "刚刚",
        "已启用",
      ],
      [
        "余额支付",
        "站内余额",
        "¥7,360.00",
        "0.00%",
        "100%",
        "2 分钟前",
        "已启用",
      ],
      ["USDT", "TRC20 聚合", "¥0.00", "1.20%", "—", "—", "维护中"],
    ],
  },
  analytics: {
    action: t("module.actionExportReport"),
    actionIcon: Download,
    filters: [
      t("module.filterOverview"),
      t("module.filterProductAnalysis"),
      t("module.filterCustomerAnalysis"),
      t("module.filterChannelAnalysis"),
    ],
    columns: [
      t("module.colMetric"),
      t("module.colToday"),
      t("module.colYesterday"),
      t("module.colChange"),
      t("module.colMonthTotal"),
      t("module.colTarget"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "成交额",
        "¥128,460",
        "¥113,920",
        "+12.8%",
        "¥1,826,540",
        "¥3,000,000",
        "良好",
      ],
      ["订单数", "1,860", "1,719", "+8.2%", "26,482", "40,000", "良好"],
      ["支付转化率", "4.82%", "4.61%", "+0.21%", "4.72%", "5.00%", "关注"],
      ["客单价", "¥69.06", "¥66.27", "+4.2%", "¥68.97", "¥70.00", "良好"],
    ],
  },
  settings: {
    action: t("module.actionSaveChanges"),
    actionIcon: Check,
    filters: [
      t("module.filterSiteSettings"),
      t("module.filterTransactionSettings"),
      t("module.filterDeliveryPolicy"),
      t("module.filterSecuritySettings"),
      t("module.filterNotificationSettings"),
    ],
    columns: [
      t("module.colConfigItem"),
      t("module.colCurrentValue"),
      t("module.colDescription"),
      t("module.colUpdatedAt"),
      t("module.colOperator"),
      t("module.colStatus"),
      "",
    ],
    rows: [
      [
        "站点名称",
        "LinLinQi",
        "前台与邮件中显示",
        "2026-08-09 09:12",
        "超级管理员",
        "已生效",
        "",
      ],
      [
        "订单超时",
        "15 分钟",
        "未支付订单自动关闭",
        "2026-08-08 18:30",
        "超级管理员",
        "已生效",
        "",
      ],
      [
        "库存预占",
        "5 分钟",
        "支付过程中锁定库存",
        "2026-08-08 18:25",
        "超级管理员",
        "已生效",
        "",
      ],
      [
        "异常自动重试",
        "最多 3 次",
        "指数退避策略",
        "2026-08-07 14:06",
        "运维管理员",
        "已生效",
        "",
      ],
    ],
  },
};
Object.assign(definitions, {
  variants: {
    action: t("module.actionCreateVariant"),
    actionIcon: Plus,
    filters: [
      t("module.filterAllSkus"),
      t("module.filterEnabled2"),
      t("module.filterDiscontinued"),
    ],
    columns: [
      "SKU",
      t("module.colBelongProduct"),
      t("module.colAttributes"),
      t("module.colSalePrice"),
      t("module.colCostPrice"),
      t("module.colPurchaseLimit"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "AIPRO-MONTH",
        "AI Pro 月度会员",
        "周期：1 个月",
        "¥129.00",
        "¥82.00",
        "20",
        "启用",
      ],
      [
        "STEAM-CNY-100",
        "Steam 100 元充值卡",
        "面额：¥100",
        "¥97.00",
        "¥92.00",
        "10",
        "启用",
      ],
      [
        "CLOUD-2C4G",
        "云服务器基础版",
        "2C / 4G / 80G",
        "¥49.00",
        "¥29.00",
        "5",
        "启用",
      ],
    ],
  },
  pricing: {
    action: t("module.actionAddPricingRule"),
    actionIcon: Plus,
    filters: [
      t("module.filterMemberLevels"),
      t("module.filterTierPrices"),
      t("module.filterResellerPrices"),
    ],
    columns: [
      t("module.colRule"),
      t("module.colScope"),
      t("module.colThreshold2"),
      t("module.colDiscountPrice"),
      t("module.colPriority"),
      t("module.colValidPeriod"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "金卡会员折扣",
        "全部商品",
        "累计消费 ¥5,000",
        "95 折",
        "20",
        "长期",
        "启用",
      ],
      [
        "Steam 批发价",
        "STEAM-CNY-100",
        "单次 ≥ 10",
        "¥93.50",
        "50",
        "长期",
        "启用",
      ],
      [
        "企业会员折扣",
        "全部商品",
        "累计消费 ¥20,000",
        "92 折",
        "30",
        "长期",
        "启用",
      ],
    ],
  },
  inventoryBatches: {
    action: t("module.actionImportCards"),
    actionIcon: Upload,
    filters: [
      t("module.filterImportBatches"),
      t("module.filterExportJobs"),
      t("module.filterFailedRecords"),
    ],
    columns: [
      t("module.colBatchNo"),
      t("module.colProduct"),
      t("module.colSource"),
      t("module.colTotal"),
      t("module.colValid"),
      t("module.colInvalid"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "LQB20260809018",
        "AI Pro 月度会员",
        "管理员导入",
        "500",
        "498",
        "2",
        "已完成",
      ],
      [
        "LQB20260808042",
        "Steam 100 元充值卡",
        "API 同步",
        "1,000",
        "1,000",
        "0",
        "已完成",
      ],
      ["LQE20260807008", "受控导出任务", "售后核验", "12", "12", "0", "已审计"],
    ],
  },
  mappings: {
    action: t("module.actionAddMapping"),
    actionIcon: Plus,
    filters: [
      t("module.filterAllMappings"),
      t("module.filterSyncOk"),
      t("module.filterPriceError"),
      t("module.filterDisabled"),
    ],
    columns: [
      t("module.colLocalProduct"),
      t("module.colSupplier"),
      t("module.colExternalProductId"),
      t("module.colPricingRule"),
      t("module.colExternalStock"),
      t("module.colLastSync"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "云服务器基础版",
        "CloudBridge",
        "cloud-2c4g-cn",
        "成本 + 18%",
        "99",
        "1 分钟前",
        "同步正常",
      ],
      [
        "Git 托管年度增强版",
        "星云数字供应链",
        "git-pro-12m",
        "固定 ¥238",
        "8",
        "3 分钟前",
        "库存偏低",
      ],
      [
        "流媒体家庭组季卡",
        "MediaHub",
        "stream-family-q",
        "成本 + 22%",
        "—",
        "28 分钟前",
        "价格异常",
      ],
    ],
  },
  procurements: {
    action: t("module.actionManualProcurement"),
    actionIcon: Plus,
    filters: [
      t("module.filterAllProcurements"),
      t("module.filterProcessing"),
      t("module.filterCompleted"),
      t("module.filterFailedRetry"),
    ],
    columns: [
      t("module.colProcurementNo"),
      t("module.colSalesOrder"),
      t("module.colSupplier"),
      t("module.colExternalOrder"),
      t("module.colCost"),
      t("module.colAttempts"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "LQP202608090086",
        "LLQ2026080900184",
        "CloudBridge",
        "CB-991820",
        "¥29.00",
        "1",
        "已完成",
      ],
      [
        "LQP202608090085",
        "LLQ2026080900179",
        "MediaHub",
        "MH-620183",
        "¥60.00",
        "2",
        "处理中",
      ],
      [
        "LQP202608090081",
        "LLQ2026080900168",
        "点卡全球仓",
        "—",
        "¥184.00",
        "3",
        "等待重试",
      ],
    ],
  },
  memberLevels: {
    action: t("module.actionCreateLevel"),
    actionIcon: Plus,
    filters: [
      t("module.filterMemberLevels"),
      t("module.filterGrowthRecords"),
      t("module.filterManualGrant"),
    ],
    columns: [
      t("module.colLevel"),
      t("module.colUpgradeThreshold"),
      t("module.colDefaultDiscount"),
      t("module.colMemberCount"),
      t("module.colMonthUpgrades"),
      t("module.colPriority"),
      t("module.colStatus"),
    ],
    rows: [
      ["标准会员", "¥0", "无折扣", "21,680", "—", "0", "启用"],
      ["银卡会员", "¥1,000", "98 折", "2,420", "186", "10", "启用"],
      ["金卡会员", "¥5,000", "95 折", "542", "38", "20", "启用"],
      ["企业会员", "¥20,000", "92 折", "40", "6", "30", "启用"],
    ],
  },
  refunds: {
    action: t("module.actionCreateRefund"),
    actionIcon: Plus,
    filters: [
      t("module.filterAllRefunds"),
      t("module.filterPendingReview"),
      t("module.filterRefunding"),
      t("module.filterCompleted"),
      t("module.filterRejected"),
    ],
    columns: [
      t("module.colRefundNo"),
      t("module.colOrderNo"),
      t("module.colCustomer"),
      t("module.colRefundAmount"),
      t("module.colReason"),
      t("module.colChannelStatus"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "LQR202608090032",
        "LLQ2026080900182",
        "yu***@mail.com",
        "¥39.00",
        "权益异常",
        "等待渠道",
        "退款中",
      ],
      [
        "LQR202608080061",
        "LLQ2026080801024",
        "lin***@mail.com",
        "¥97.00",
        "重复购买",
        "已退回",
        "已完成",
      ],
      [
        "LQR202608070029",
        "LLQ2026080700831",
        "gao***@163.com",
        "¥49.00",
        "不符合规则",
        "—",
        "已驳回",
      ],
    ],
  },
  reconciliation: {
    action: t("module.actionStartReconciliation"),
    actionIcon: Plus,
    filters: [
      t("module.filterReconBatches"),
      t("module.filterDiffLedger"),
      t("module.filterResolved"),
    ],
    columns: [
      t("module.colBatchNo"),
      t("module.colPayChannel"),
      t("module.colPeriod"),
      t("module.colSystemCount"),
      t("module.colChannelCount"),
      t("module.colDiff"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "LQRC20260809-A",
        "支付宝",
        "2026-08-08",
        "1,286",
        "1,286",
        "0",
        "已匹配",
      ],
      ["LQRC20260809-W", "微信支付", "2026-08-08", "942", "943", "1", "待处理"],
      ["LQRC20260809-B", "账户余额", "2026-08-08", "186", "186", "0", "已匹配"],
    ],
  },
  wallets: {
    action: t("module.actionAdjustBalance"),
    actionIcon: Plus,
    filters: [
      t("module.colBalance"),
      t("module.filterRechargeOrders"),
      t("module.filterFundLedger"),
      t("module.filterWithdrawals"),
    ],
    columns: [
      t("module.colAccount"),
      t("module.colOwnerType"),
      t("module.colAvailableBalance"),
      t("module.colFrozenBalance"),
      t("module.colMonthIn"),
      t("module.colMonthOut"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "lin***@mail.com",
        "用户",
        "¥120.00",
        "¥0.00",
        "¥249.00",
        "¥129.00",
        "正常",
      ],
      [
        "星河数字商城",
        "分销商",
        "¥12,860.00",
        "¥320.00",
        "¥82,600.00",
        "¥69,740.00",
        "正常",
      ],
      [
        "推广用户 LQ8F2A",
        "佣金账户",
        "¥368.20",
        "¥80.00",
        "¥1,286.40",
        "¥918.20",
        "正常",
      ],
    ],
  },
  content: {
    action: t("module.actionPublishContent"),
    actionIcon: Plus,
    filters: [
      t("module.filterArticles"),
      t("module.colCategory"),
      t("module.filterNotices"),
      t("module.filterBanners"),
      t("module.filterMediaLib"),
    ],
    columns: [
      t("module.colTitleFile"),
      t("module.colType"),
      t("module.colLocationCategory"),
      t("module.colAuthor"),
      t("module.colPublishTime"),
      t("module.colViewsSize"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "LinLinQi 如何保障每一笔数字交付",
        "文章",
        "平台能力",
        "运营团队",
        "2026-08-08",
        "2,860",
        "已发布",
      ],
      [
        "支付与交付服务升级完成",
        "公告",
        "全站顶部",
        "超级管理员",
        "2026-08-09",
        "24,682",
        "展示中",
      ],
      [
        "channel-hero.webp",
        "媒体",
        "分销商页面",
        "设计团队",
        "2026-08-07",
        "186 KB",
        "已使用",
      ],
    ],
  },
  risk: {
    action: t("module.actionCreateRiskRule"),
    actionIcon: Plus,
    filters: [
      t("module.filterRiskDecisions"),
      t("module.filterRiskRules"),
      t("module.filterManualReview"),
      t("module.filterIpBlacklist"),
    ],
    columns: [
      t("module.colObject"),
      t("module.colRiskScore"),
      t("module.colMatchedRules"),
      t("module.colIpAccount"),
      t("module.colDecision"),
      t("module.colReviewer"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "LLQ2026080900172",
        "80",
        "IP 高频、代理网络",
        "103.***.18.2",
        "拒绝",
        "系统",
        "已拦截",
      ],
      [
        "LLQ2026080900169",
        "55",
        "支付失败频率",
        "lin***@mail.com",
        "人工复核",
        "风控 01",
        "已放行",
      ],
      ["185.***.42.0/24", "—", "恶意扫描", "CIDR", "黑名单", "系统", "启用"],
    ],
  },
  webhooks: {
    action: t("module.actionAddSubscription"),
    actionIcon: Plus,
    filters: [
      t("module.filterSubscriptionEndpoints"),
      t("module.filterDeliveryRecords"),
      t("module.filterFailedRetry"),
    ],
    columns: [
      t("module.colEndpointEvent"),
      t("module.colOwner"),
      t("module.colRequestCount"),
      t("module.colSuccessRate"),
      t("module.colLastStatus"),
      t("module.colNextRetry"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "https://shop.example/hooks/linlinqi",
        "星河数字商城",
        "18,420",
        "99.97%",
        "200",
        "—",
        "启用",
      ],
      [
        "order.delivered / evt_8f2a",
        "星河数字商城",
        "3",
        "—",
        "503",
        "2 分钟后",
        "等待重试",
      ],
      [
        "payment.succeeded / evt_71bc",
        "API 客户 02",
        "1",
        "100%",
        "204",
        "—",
        "已投递",
      ],
    ],
  },
  notifications: {
    action: t("module.actionCreateTemplate"),
    actionIcon: Plus,
    filters: [
      t("module.filterMessageTemplates"),
      t("module.filterSendRecords"),
      t("module.filterFailedJobs"),
      t("module.filterChannelSettings"),
    ],
    columns: [
      t("module.colTemplateRecipient"),
      t("module.colChannel"),
      t("module.colEvent"),
      t("module.colTodaySent"),
      t("module.colSuccessRate"),
      t("module.colLastError"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "订单交付完成",
        "邮件",
        "order.delivered",
        "1,286",
        "99.91%",
        "—",
        "启用",
      ],
      ["库存不足提醒", "管理员通知", "stock.low", "8", "100%", "—", "启用"],
      [
        "lin***@mail.com",
        "邮件",
        "order.paid",
        "1",
        "—",
        "SMTP 429",
        "等待重试",
      ],
    ],
  },
  access: {
    action: t("module.actionCreateRole"),
    actionIcon: Plus,
    filters: [
      t("module.filterAdmins"),
      t("module.filterRoles"),
      t("module.filterPermissions"),
      t("module.filterAuthAudit"),
    ],
    columns: [
      t("module.colAccountRole"),
      t("module.colPermissionScope"),
      "2FA",
      t("module.colLastLogin"),
      "IP",
      t("module.colChangedBy"),
      t("module.colStatus"),
    ],
    rows: [
      ["admin", "super_admin", "已启用", "刚刚", "127.0.0.1", "—", "正常"],
      [
        "运营管理员",
        "商品、订单、客户",
        "已启用",
        "18 分钟前",
        "103.***.1.8",
        "超级管理员",
        "正常",
      ],
      [
        "财务审计员",
        "支付、对账、只读",
        "未启用",
        "2 小时前",
        "114.***.8.2",
        "超级管理员",
        "需加固",
      ],
    ],
  },
  security: {
    action: t("module.actionAddBlacklist"),
    actionIcon: Plus,
    filters: [
      t("module.filterSecurityEvents"),
      t("module.filterLoginLogs"),
      t("module.filterIpBlacklist"),
      t("module.filterDeviceSessions"),
    ],
    columns: [
      t("module.colEventAccount"),
      t("module.colSeverity"),
      "IP",
      t("module.colDeviceRule"),
      t("module.colOccurTime"),
      t("module.colDisposal"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "管理员登录成功",
        "信息",
        "127.0.0.1",
        "Chrome / macOS",
        "刚刚",
        "—",
        "正常",
      ],
      [
        "连续登录失败",
        "中",
        "185.***.42.18",
        "5 次 / 10 分钟",
        "8 分钟前",
        "临时封禁",
        "已处置",
      ],
      [
        "高风险订单尝试",
        "高",
        "103.***.18.2",
        "代理 + 高频",
        "16 分钟前",
        "拒绝",
        "已处置",
      ],
    ],
  },
  jobs: {
    action: t("module.actionRetryJob"),
    actionIcon: Plus,
    filters: [
      t("module.filterRunning"),
      t("module.filterWaiting"),
      t("module.filterFailedRetry"),
      t("module.filterDeadLetter"),
      t("module.filterScheduled"),
    ],
    columns: [
      t("module.colTaskId"),
      t("module.colTaskType"),
      t("module.colQueue"),
      t("module.colAttempts"),
      t("module.colScheduleDuration"),
      t("module.colLastError"),
      t("module.colStatus"),
    ],
    rows: [
      [
        "task_8f2a",
        "webhook.deliver",
        "critical",
        "2 / 12",
        "2 分钟后",
        "HTTP 503",
        "等待重试",
      ],
      [
        "task_71bc",
        "supplier.sync",
        "default",
        "1 / 12",
        "820ms",
        "—",
        "已完成",
      ],
      [
        "task_9c20",
        "reconciliation.run",
        "low",
        "0 / 12",
        "02:00",
        "—",
        "等待执行",
      ],
    ],
  },
});
const current = computed(() => definitions[kind.value] || definitions.products);
const endpointMap: Record<string, string> = {
  orders: "/orders",
  products: "/products",
  inventory: "/cards",
  suppliers: "/suppliers",
  customers: "/users",
  payments: "/payments",
  variants: "/operations/variants",
  pricing: "/operations/price-tiers",
  inventoryBatches: "/operations/inventory-batches",
  mappings: "/operations/mappings",
  procurements: "/operations/procurements",
  memberLevels: "/operations/member-levels",
  refunds: "/operations/refunds",
  reconciliation: "/operations/reconciliations",
  wallets: "/operations/wallets",
  content: "/operations/posts",
  risk: "/operations/risk-decisions",
  webhooks: "/operations/webhook-deliveries",
  notifications: "/operations/notifications",
  access: "/operations/roles",
  security: "/operations/security-events",
  jobs: "/operations/jobs",
  analytics: "/dashboard",
  settings: "/settings",
};
type Operation =
  | { type: "export" }
  | {
      type: "post";
      method?: "post" | "put";
      endpoint: string;
      example: () => Record<string, unknown>;
    };
const operations: Record<string, Operation> = {
  orders: { type: "export" },
  customers: { type: "export" },
  analytics: { type: "export" },
  products: {
    type: "post",
    endpoint: "/products",
    example: () => ({
      category_id: "<分类 UUID>",
      name: "新商品",
      slug: "new-product",
      summary: "商品摘要",
      description: "商品完整说明",
      price: 9900,
      compare_price: 0,
      cost_price: 0,
      delivery_type: "auto",
      inventory_mode: "local",
      status: "draft",
      featured: false,
      tags: "",
    }),
  },
  refunds: {
    type: "post",
    endpoint: "/refunds",
    example: () => ({
      order_id: "<已支付订单 UUID>",
      amount: 0,
      reason: "退款原因；amount 为 0 时退还全部剩余可退金额",
    }),
  },
  inventory: {
    type: "post",
    endpoint: "/cards/import",
    example: () => ({ product_id: "<商品 UUID>", cards: ["<真实卡密>"] }),
  },
  inventoryBatches: {
    type: "post",
    endpoint: "/cards/import",
    example: () => ({ product_id: "<商品 UUID>", cards: ["<真实卡密>"] }),
  },
  suppliers: {
    type: "post",
    endpoint: "/suppliers",
    example: () => ({
      name: "供货商名称",
      code: "supplier-code",
      base_url: "https://supplier.example/api",
      api_key: "<真实 API Key>",
      api_secret: "<真实 API Secret>",
      protocol: "linlinqi-standard",
    }),
  },
  payments: {
    type: "post",
    endpoint: "/payments",
    example: () => ({
      name: "支付渠道",
      code: "payment-code",
      provider: "signed_http",
      fee_rate: 60,
      enabled: false,
      sort: 0,
      config: {
        base_url: "https://payment.example/api",
        merchant_id: "<真实商户号>",
        secret: "<至少 24 字符的真实密钥>",
      },
    }),
  },
  variants: {
    type: "post",
    endpoint: "/operations/variants",
    example: () => ({
      product_id: "<商品 UUID>",
      sku: "SKU-001",
      name: "规格名称",
      attributes: "{}",
      price: 9900,
      status: "active",
      purchase_limit: 0,
    }),
  },
  pricing: {
    type: "post",
    endpoint: "/operations/price-tiers",
    example: () => ({
      product_id: "<商品 UUID>",
      variant_id: null,
      member_level_id: null,
      min_quantity: 10,
      unit_price: 8800,
    }),
  },
  mappings: {
    type: "post",
    endpoint: "/operations/mappings",
    example: () => ({
      supplier_id: "<供货商 UUID>",
      product_id: "<商品 UUID>",
      external_product_id: "external-sku",
      price_mode: "fixed_markup",
      markup_basis_point: 1000,
      auto_sync_price: true,
      auto_sync_stock: true,
    }),
  },
  memberLevels: {
    type: "post",
    endpoint: "/operations/member-levels",
    example: () => ({
      code: "member-level",
      name: t("module.filterMemberLevels"),
      minimum_spend: 0,
      discount_basis_point: 0,
      priority: 0,
      enabled: true,
    }),
  },
  content: {
    type: "post",
    endpoint: "/operations/posts",
    example: () => ({
      title: "文章标题",
      slug: "article-slug",
      summary: "文章摘要",
      content: "文章正文",
      status: "draft",
      seo: "{}",
    }),
  },
  risk: {
    type: "post",
    endpoint: "/operations/risk-rules",
    example: () => ({
      code: "risk-rule",
      name: t("module.filterRiskRules"),
      scope: "order",
      expression: "order.amount > 100000",
      action: "review",
      score: 50,
      enabled: true,
      priority: 0,
    }),
  },
  notifications: {
    type: "post",
    endpoint: "/operations/notification-templates",
    example: () => ({
      code: "notification-code",
      name: "通知模板",
      channel: "email",
      locale: "zh-CN",
      subject: "通知标题",
      body: "通知正文 {{variable}}",
      variables: '["variable"]',
      enabled: true,
      version: 1,
    }),
  },
  access: {
    type: "post",
    endpoint: "/operations/roles",
    example: () => ({
      code: "custom-role",
      name: "自定义角色",
      description: "角色说明",
      system: false,
    }),
  },
  settings: {
    type: "post",
    method: "put",
    endpoint: "/settings",
    example: () => ({
      store_name: "LinLinQi",
      order_timeout_minutes: "15",
      inventory_warning_threshold: "10",
    }),
  },
};
const operation = computed(() => operations[kind.value]);
const fieldMap: Record<string, string[]> = {
  orders: [
    "order_no",
    "email",
    "items",
    "total",
    "payment_method",
    "status",
    "created_at",
  ],
  products: [
    "name",
    "category.name",
    "price",
    "inventory_mode",
    "sold_count",
    "delivery_type",
    "status",
  ],
  inventory: [
    "preview",
    "product_id",
    "status",
    "order_id",
    "sold_at",
    "created_at",
    "status",
  ],
  suppliers: [
    "name",
    "protocol",
    "code",
    "balance",
    "status",
    "last_sync_at",
    "status",
  ],
  customers: [
    "nickname",
    "email",
    "id",
    "balance",
    "status",
    "last_login_at",
    "status",
  ],
  payments: [
    "name",
    "provider",
    "code",
    "fee_rate",
    "enabled",
    "updated_at",
    "enabled",
  ],
  variants: [
    "sku",
    "name",
    "attributes",
    "price",
    "cost_price",
    "purchase_limit",
    "status",
  ],
  pricing: [
    "id",
    "product_id",
    "min_quantity",
    "unit_price",
    "starts_at",
    "ends_at",
    "status",
  ],
  inventoryBatches: [
    "batch_no",
    "product_id",
    "source",
    "total_count",
    "valid_count",
    "invalid_count",
    "created_at",
  ],
  mappings: [
    "product_id",
    "supplier_id",
    "external_product_id",
    "price_mode",
    "last_error",
    "last_synced_at",
    "status",
  ],
  procurements: [
    "procurement_no",
    "order_id",
    "supplier_id",
    "external_order_no",
    "cost_amount",
    "quantity",
    "status",
  ],
  memberLevels: [
    "name",
    "minimum_spend",
    "discount_basis_point",
    "priority",
    "created_at",
    "updated_at",
    "enabled",
  ],
  refunds: [
    "refund_no",
    "order_id",
    "requested_by",
    "amount",
    "reason",
    "provider_refund_no",
    "status",
  ],
  reconciliation: [
    "batch_no",
    "channel_id",
    "period_from",
    "matched",
    "mismatched",
    "completed_at",
    "status",
  ],
  wallets: [
    "owner_id",
    "owner_type",
    "balance",
    "frozen",
    "version",
    "updated_at",
    "status",
  ],
  content: [
    "title",
    "slug",
    "category_id",
    "author_id",
    "published_at",
    "updated_at",
    "status",
  ],
  risk: [
    "order_id",
    "score",
    "matched_rules",
    "ip",
    "decision",
    "reviewed_by",
    "decision",
  ],
  webhooks: [
    "event_type",
    "endpoint_id",
    "attempts",
    "response_code",
    "status",
    "next_attempt_at",
    "status",
  ],
  notifications: [
    "recipient",
    "channel",
    "subject",
    "attempts",
    "status",
    "last_error",
    "status",
  ],
  access: [
    "name",
    "code",
    "description",
    "created_at",
    "updated_at",
    "system",
    "system",
  ],
  security: [
    "event_type",
    "severity",
    "ip",
    "user_agent",
    "created_at",
    "resolved",
    "resolved",
  ],
  jobs: [
    "task_id",
    "task_type",
    "queue",
    "attempts",
    "scheduled_at",
    "last_error",
    "status",
  ],
  settings: [
    "key",
    "value",
    "group",
    "updated_at",
    "updated_at",
    "group",
    "key",
  ],
};
const liveRows = ref<string[][]>([]);
const loaded = ref(false);
const loading = ref(false);
const loadError = ref("");
const demoEnabled =
  import.meta.env.DEV && import.meta.env.VITE_ENABLE_DEMO_DATA === "true";
const valueAt = (record: any, path: string) =>
  path.split(".").reduce((value, key) => value?.[key], record);
function formatValue(value: any, path: string) {
  if (value === null || value === undefined || value === "") return "—";
  if (Array.isArray(value))
    return value.length
      ? `${value[0]?.product_name || value[0]?.name || "记录"}${value.length > 1 ? ` ×${value.length}` : ""}`
      : "—";
  if (typeof value === "object") return JSON.stringify(value);
  if (typeof value === "boolean")
    return value ? t("module.filterEnabled2") : "停用";
  if (
    /price|amount|balance|total|frozen|commission|spend/.test(path) &&
    typeof value === "number"
  )
    return `¥${(value / 100).toFixed(2)}`;
  if (/_at$|^period_/.test(path)) {
    const date = new Date(value);
    if (!Number.isNaN(date.getTime()))
      return date.toLocaleString(locale.value, { hour12: false });
  }
  return String(value);
}
async function loadRows() {
  const endpoint = endpointMap[kind.value];
  liveRows.value = [];
  loaded.value = false;
  loadError.value = "";
  if (!endpoint) {
    loaded.value = true;
    return;
  }
  loading.value = true;
  try {
    const { data } = await adminApi.get(endpoint, {
      params: { page_size: 100 },
    });
    const payload = data.data;
    if (kind.value === "analytics") {
      const money = (value: number) => `¥${((value || 0) / 100).toFixed(2)}`;
      liveRows.value = [
        [
          t("module.metricNetRevenue"),
          money(payload.today_revenue),
          "—",
          `${Number(payload.revenue_change || 0).toFixed(2)}%`,
          "—",
          "—",
          t("module.metricRealtime"),
        ],
        [
          t("module.colOrders"),
          String(payload.today_orders || 0),
          "—",
          `${Number(payload.order_change || 0).toFixed(2)}%`,
          "—",
          "—",
          t("module.metricRealtime"),
        ],
        [
          t("module.metricPaySuccess"),
          `${Number(payload.payment_success_rate || 0).toFixed(2)}%`,
          "—",
          "—",
          "—",
          "—",
          t("module.metricRealtime"),
        ],
        [
          t("module.metricAov"),
          money(payload.average_order_value),
          "—",
          "—",
          "—",
          "—",
          t("module.metricRealtime"),
        ],
      ];
      loaded.value = true;
      return;
    }
    if (kind.value === "settings" && Array.isArray(payload)) {
      liveRows.value = payload.map((setting: any) => [
        String(setting.key || "—"),
        String(setting.value || "—"),
        String(setting.group || "general"),
        formatValue(setting.updated_at, "updated_at"),
        t("module.filterAdmins"),
        t("module.filterEffective"),
        "",
      ]);
      loaded.value = true;
      return;
    }
    const records = Array.isArray(payload)
      ? payload
      : Array.isArray(payload?.items)
        ? payload.items
        : [];
    const fields = fieldMap[kind.value] || [];
    liveRows.value = records.map((record: any) =>
      fields.map((field) => formatValue(valueAt(record, field), field)),
    );
    loaded.value = true;
  } catch (error: any) {
    loadError.value = error?.response?.data?.message || t("module.errLoad");
    loaded.value = true;
  } finally {
    loading.value = false;
  }
}
function exportRows() {
  const lines = [current.value.columns, ...displayRows.value].map((row) =>
    row.map((value: string) => safeCSVCell(value)).join(","),
  );
  const blob = new Blob(["\uFEFF", lines.join("\n")], {
    type: "text/csv;charset=utf-8",
  });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `linlinqi-${kind.value}-${new Date().toISOString().slice(0, 10)}.csv`;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
  actionNotice.value = t("module.exported", { n: displayRows.value.length });
}
function openAction() {
  actionNotice.value = "";
  actionError.value = "";
  const selected = operation.value;
  if (!selected) return;
  if (selected.type === "export") {
    exportRows();
    return;
  }
  actionPayload.value = JSON.stringify(selected.example(), null, 2);
  actionReason.value = "";
  actionIdempotencyKey.value = crypto.randomUUID();
  modal.value = true;
}
async function submitAction() {
  const selected = operation.value;
  if (!selected || selected.type !== "post") return;
  actionError.value = "";
  if (!actionReason.value.trim()) {
    actionError.value = t("module.errReason");
    return;
  }
  let payload: Record<string, unknown>;
  try {
    payload = JSON.parse(actionPayload.value);
    if (!payload || Array.isArray(payload) || typeof payload !== "object")
      throw new Error("invalid object");
  } catch {
    actionError.value = t("module.errJson");
    return;
  }
  saving.value = true;
  try {
    const headers: Record<string, string> = {
      "X-Change-Reason": actionReason.value.trim(),
    };
    if (selected.endpoint === "/refunds") {
      if (!actionIdempotencyKey.value)
        actionIdempotencyKey.value = crypto.randomUUID();
      headers["Idempotency-Key"] = actionIdempotencyKey.value;
    }
    await adminApi.request({
      method: selected.method || "post",
      url: selected.endpoint,
      data: payload,
      headers,
    });
    modal.value = false;
    actionIdempotencyKey.value = "";
    actionNotice.value = t("module.actionSubmitted", {
      action: current.value.action,
    });
    await loadRows();
  } catch (error: any) {
    actionError.value = error?.response?.data?.message || t("module.errAction");
  } finally {
    saving.value = false;
  }
}
watch(kind, loadRows, { immediate: true });
const displayRows = computed(() =>
  liveRows.value.length || !demoEnabled ? liveRows.value : current.value.rows,
);
const filteredRows = computed(() =>
  displayRows.value.filter((r: string[]) =>
    r.join(" ").toLowerCase().includes(search.value.toLowerCase()),
  ),
);
</script>

<template>
  <section class="module-panel panel">
    <div class="module-toolbar">
      <div class="filter-tabs">
        <button
          v-for="(filter, index) in current.filters"
          :key="filter"
          :class="{ active: index === 0 }"
        >
          {{ filter }}<span v-if="index === 0">{{ displayRows.length }}</span>
        </button>
      </div>
      <button
        v-if="operation"
        class="primary-button compact"
        @click="openAction"
      >
        <component :is="current.actionIcon" :size="15" />{{ current.action }}
      </button>
    </div>
    <p v-if="actionNotice" class="module-state success">
      {{ actionNotice }}
    </p>
    <p v-if="loading" class="module-state">{{ t("module.loading") }}</p>
    <p v-else-if="loadError" class="module-state error">{{ loadError }}</p>
    <p v-else-if="loaded && !displayRows.length" class="module-state">
      {{ t("module.noRecords") }}
    </p>
    <div class="table-tools">
      <div class="table-search">
        <Search :size="16" /><input
          v-model="search"
          :placeholder="t('module.searchPlaceholder')"
        />
      </div>
      <div>
        <button>
          <Filter :size="15" />{{ t("module.filter")
          }}<ChevronDown :size="14" /></button
        ><button>
          <SlidersHorizontal :size="15" />{{ t("module.columns") }}
        </button>
      </div>
    </div>
    <div class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th><input type="checkbox" /></th>
            <th v-for="column in current.columns" :key="column">
              {{ column }}
            </th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, rowIndex) in filteredRows" :key="rowIndex">
            <td><input type="checkbox" /></td>
            <td v-for="(cell, index) in row" :key="index">
              <div v-if="index === 0" class="primary-cell">
                <span>{{
                  kind === "customers"
                    ? cell.slice(0, 1)
                    : kind === "suppliers"
                      ? cell.slice(0, 2)
                      : kind === "payments"
                        ? cell.slice(0, 1)
                        : "LQ"
                }}</span
                ><b>{{ cell }}</b>
              </div>
              <span
                v-else-if="index === row.length - 1"
                :class="[
                  'status',
                  cell.includes('异常') ||
                  cell.includes('预警') ||
                  cell.includes('维护')
                    ? 'warning'
                    : cell.includes('退款')
                      ? 'refunding'
                      : 'delivered',
                ]"
                >{{ cell }}</span
              ><template v-else>{{ cell }}</template>
            </td>
            <td>
              <button class="dots"><MoreHorizontal :size="17" /></button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="pagination">
      <span>{{ t("module.totalRecords", { n: filteredRows.length }) }}</span>
      <div>
        <button disabled>{{ t("module.prev") }}</button
        ><button class="active">1</button
        ><button>{{ t("module.next") }}</button>
      </div>
    </div>
  </section>
  <Teleport to="body"
    ><div v-if="modal" class="modal-backdrop" @click.self="modal = false">
      <div class="modal">
        <header>
          <div>
            <span class="kicker">OPERATION ACTION</span>
            <h2>{{ current.action }}</h2>
          </div>
          <button @click="modal = false"><X /></button>
        </header>
        <p>
          {{ t("module.modalDesc") }}
        </p>
        <label
          >{{ t("module.requestJson")
          }}<textarea
            v-model="actionPayload"
            class="json-editor"
            spellcheck="false"
          ></textarea>
        </label>
        <label
          >{{ t("module.auditReason")
          }}<textarea
            v-model="actionReason"
            :placeholder="t('module.auditReasonPlaceholder')"
          ></textarea>
        </label>
        <p v-if="actionError" class="modal-error">{{ actionError }}</p>
        <footer>
          <button @click="modal = false">{{ t("module.cancel") }}</button
          ><button
            class="primary-button compact"
            :disabled="saving"
            @click="submitAction"
          >
            <Check :size="15" />{{
              saving ? t("module.saving") : t("module.confirmSave")
            }}
          </button>
        </footer>
      </div>
    </div></Teleport
  >
</template>
