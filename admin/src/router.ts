import { createRouter, createWebHistory } from "vue-router";
import AdminLayout from "./layouts/AdminLayout.vue";
import { useAuthStore } from "./stores/auth";

const LoginView = () => import("./views/LoginView.vue");
const DashboardView = () => import("./views/DashboardView.vue");
const OrderView = () => import("./views/OrderView.vue");
const CatalogView = () => import("./views/CatalogView.vue");
const InventoryView = () => import("./views/InventoryView.vue");
const SupplyView = () => import("./views/SupplyView.vue");
const SupplierCategoryBindingView = () =>
  import("./views/SupplierCategoryBindingView.vue");
const CustomerView = () => import("./views/CustomerView.vue");
const PaymentOperationsView = () => import("./views/PaymentOperationsView.vue");
const ReconciliationView = () => import("./views/ReconciliationView.vue");
const MarketingView = () => import("./views/MarketingView.vue");
const GiftCardView = () => import("./views/GiftCardView.vue");
const AffiliateView = () => import("./views/AffiliateView.vue");
const ResellerView = () => import("./views/ResellerView.vue");
const ContentView = () => import("./views/ContentView.vue");
const TicketView = () => import("./views/TicketView.vue");
const RiskView = () => import("./views/RiskView.vue");
const AnalyticsView = () => import("./views/AnalyticsView.vue");
const OpenAPIView = () => import("./views/OpenAPIView.vue");
const IntegrationView = () => import("./views/IntegrationView.vue");
const NotificationAutomationView = () =>
  import("./views/NotificationAutomationView.vue");
const AccessView = () => import("./views/AccessView.vue");
const SecurityView = () => import("./views/SecurityView.vue");
const JobsView = () => import("./views/JobsView.vue");
const SettingsView = () => import("./views/SettingsView.vue");
const CurrencyView = () => import("./views/CurrencyView.vue");

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/login", component: LoginView, meta: { public: true } },
    {
      path: "/",
      component: AdminLayout,
      children: [
        {
          path: "",
          component: DashboardView,
          meta: { title: "经营概览", subtitle: "实时掌握店铺经营与交付状态" },
        },
        {
          path: "orders",
          component: OrderView,
          meta: {
            title: "订单管理",
            subtitle: "查询、审计并处理订单的支付、交付、风控与售后链路",
          },
        },
        {
          path: "product-categories",
          component: CatalogView,
          meta: {
            defaultTab: "categories",
            title: "商品分类",
            subtitle: "维护多级商品分类、展示顺序与销售可见性",
          },
        },
        {
          path: "products",
          component: CatalogView,
          meta: {
            defaultTab: "products",
            title: "商品管理",
            subtitle: "管理商品、分类、价格与销售状态",
          },
        },
        {
          path: "variants",
          component: CatalogView,
          meta: {
            defaultTab: "variants",
            title: "商品规格",
            subtitle: "多规格 SKU、属性、价格与购买限制",
          },
        },
        {
          path: "pricing",
          component: CatalogView,
          meta: {
            defaultTab: "pricing",
            defaultPricingTab: "tiers",
            title: "等级与批发价",
            subtitle: "会员等级、阶梯价和分销供货价",
          },
        },
        {
          path: "inventory",
          component: InventoryView,
          meta: {
            defaultTab: "stock",
            title: "卡密库存",
            subtitle: "导入、检查和管理自动发货库存",
          },
        },
        {
          path: "card-secrets",
          component: InventoryView,
          meta: {
            defaultTab: "cards",
            title: "卡密管理",
            subtitle: "检索、审计和管理自动发货卡密",
          },
        },
        {
          path: "inventory-batches",
          component: InventoryView,
          meta: {
            defaultTab: "batches",
            title: "导入批次",
            subtitle: "卡密批次、加密导入、重复校验与审计",
          },
        },
        {
          path: "suppliers",
          component: SupplyView,
          meta: {
            defaultTab: "supplier",
            title: "供货商",
            subtitle: "管理货源接口、价格同步与订单路由",
          },
        },
        {
          path: "category-bindings",
          component: SupplierCategoryBindingView,
          meta: {
            title: "分类绑定",
            subtitle:
              "建立本地与远端分类关系，并控制内容、价格、库存与上架策略",
          },
        },
        {
          path: "mappings",
          component: SupplyView,
          meta: {
            defaultTab: "mapping",
            title: "商品映射",
            subtitle: "本地 SKU 与上游商品、价格和库存规则",
          },
        },
        {
          path: "procurements",
          component: SupplyView,
          meta: {
            defaultTab: "procurement",
            title: "采购订单",
            subtitle: "追踪上游采购、成本、回调和异常重试",
          },
        },
        {
          path: "customers",
          component: CustomerView,
          meta: {
            defaultTab: "customers",
            title: "客户管理",
            subtitle: "客户档案、等级身份、消费、会话与账户状态",
          },
        },
        {
          path: "member-levels",
          component: CatalogView,
          meta: {
            defaultTab: "pricing",
            defaultPricingTab: "levels",
            title: "会员等级",
            subtitle: "成长规则、折扣与等级权益",
          },
        },
        { path: "payments", redirect: "/payment-channels" },
        {
          path: "payment-channels",
          component: PaymentOperationsView,
          meta: {
            defaultTab: "channels",
            title: "支付渠道",
            subtitle: "安全配置收款渠道、费率和结算币种",
          },
        },
        {
          path: "wallet-recharges",
          component: PaymentOperationsView,
          meta: {
            defaultTab: "recharges",
            title: "充值订单",
            subtitle: "查询钱包充值、支付状态与入账结果",
          },
        },
        {
          path: "payment-intents",
          component: PaymentOperationsView,
          meta: {
            defaultTab: "intents",
            title: "支付意图",
            subtitle: "审计支付创建、过期、成功与失败状态",
          },
        },
        {
          path: "payment-transactions",
          component: PaymentOperationsView,
          meta: {
            defaultTab: "transactions",
            title: "支付流水",
            subtitle: "核查渠道事件、费用与资金方向",
          },
        },
        {
          path: "refunds",
          component: PaymentOperationsView,
          meta: {
            defaultTab: "refunds",
            title: "退款与售后",
            subtitle: "检索可退订单、原路退款与失败恢复",
          },
        },
        {
          path: "reconciliation",
          component: ReconciliationView,
          meta: {
            title: "支付对账",
            subtitle: "渠道账单、系统流水与差异处理",
          },
        },
        {
          path: "wallets",
          component: CustomerView,
          meta: {
            defaultTab: "wallets",
            title: "钱包账本",
            subtitle: "余额账户、幂等调整、冻结金额与不可变流水",
          },
        },
        { path: "marketing", redirect: "/promotions" },
        {
          path: "promotions",
          component: MarketingView,
          meta: {
            defaultTab: "promotions",
            title: "营销活动",
            subtitle: "管理商品活动价、投放时间和销售状态",
          },
        },
        {
          path: "coupons",
          component: MarketingView,
          meta: {
            defaultTab: "coupons",
            title: "优惠券",
            subtitle: "管理优惠券规则、领取、使用与失效周期",
          },
        },
        {
          path: "gift-card-batches",
          component: GiftCardView,
          meta: {
            defaultTab: "batches",
            title: "礼品卡批次",
            subtitle: "创建发行批次并审计面额、数量和有效期",
          },
        },
        { path: "gift-cards", redirect: "/gift-card-batches" },
        {
          path: "gift-card-list",
          component: GiftCardView,
          meta: {
            defaultTab: "cards",
            title: "礼品卡",
            subtitle: "查询单卡状态、余额、兑换和作废记录",
          },
        },
        {
          path: "affiliates",
          component: AffiliateView,
          meta: {
            defaultTab: "accounts",
            title: "推广返佣",
            subtitle: "审核和管理推广账户与返佣策略",
          },
        },
        {
          path: "affiliate-commissions",
          component: AffiliateView,
          meta: {
            defaultTab: "commissions",
            title: "推广佣金",
            subtitle: "查询佣金来源、状态和结算明细",
          },
        },
        {
          path: "affiliate-withdrawals",
          component: AffiliateView,
          meta: {
            defaultTab: "withdrawals",
            title: "推广提现",
            subtitle: "审核推广提现申请并记录付款结果",
          },
        },
        {
          path: "resellers",
          component: ResellerView,
          meta: {
            defaultTab: "profiles",
            title: "分销商与站群",
            subtitle: "审核和管理分销商档案与经营状态",
          },
        },
        {
          path: "reseller-tiers",
          component: ResellerView,
          meta: {
            defaultTab: "tiers",
            title: "分销等级",
            subtitle: "配置分销商批发等级、折扣和准入规则",
          },
        },
        {
          path: "reseller-domains",
          component: ResellerView,
          meta: {
            defaultTab: "domains",
            title: "分销域名",
            subtitle: "验证分销站点域名并维护 TLS 状态",
          },
        },
        {
          path: "reseller-withdrawals",
          component: ResellerView,
          meta: {
            defaultTab: "withdrawals",
            title: "分销提现",
            subtitle: "审核分销商提现申请与付款记录",
          },
        },
        {
          path: "content",
          redirect: (to) => {
            const tab = String(to.query.tab || "posts");
            const target: Record<string, string> = {
              posts: "/posts",
              categories: "/post-categories",
              announcements: "/announcements",
              banners: "/banners",
              media: "/media",
            };
            const query = { ...to.query };
            delete query.tab;
            return { path: target[tab] || "/posts", query };
          },
        },
        {
          path: "posts",
          component: ContentView,
          meta: {
            defaultTab: "posts",
            title: "文章列表",
            subtitle: "创建、发布和维护博客文章",
          },
        },
        {
          path: "post-categories",
          component: ContentView,
          meta: {
            defaultTab: "categories",
            title: "文章分类",
            subtitle: "维护博客文章的分类、标识和排序",
          },
        },
        {
          path: "announcements",
          component: ContentView,
          meta: {
            defaultTab: "announcements",
            title: "公告管理",
            subtitle: "发布站点公告并控制等级、顺序与状态",
          },
        },
        {
          path: "banners",
          component: ContentView,
          meta: {
            defaultTab: "banners",
            title: "首页轮播图",
            subtitle: "配置多张自动轮播、跳转、排序与定时投放",
          },
        },
        {
          path: "media",
          component: ContentView,
          meta: {
            defaultTab: "media",
            title: "媒体库",
            subtitle: "统一上传、检索和复用内容媒体资源",
          },
        },
        {
          path: "tickets",
          component: TicketView,
          meta: {
            title: "售后工单",
            subtitle: "客户问题、内部备注和处理时效",
          },
        },
        { path: "risk", redirect: "/risk-rules" },
        {
          path: "risk-rules",
          component: RiskView,
          meta: {
            defaultTab: "rules",
            title: "风控规则",
            subtitle: "管理受限规则策略与风险信号",
          },
        },
        {
          path: "risk-decisions",
          component: RiskView,
          meta: {
            defaultTab: "decisions",
            title: "风控判定",
            subtitle: "审计规则命中、风险判定和人工复核依据",
          },
        },
        {
          path: "analytics",
          component: AnalyticsView,
          meta: {
            title: "数据分析",
            subtitle: "按真实账务事件分析收入、退款、转化、商品与支付渠道",
          },
        },
        {
          path: "openapi",
          component: OpenAPIView,
          meta: { title: "OpenAPI", subtitle: "面向供货商与分销商的标准接口" },
        },
        {
          path: "webhooks",
          redirect: (to) => {
            const tab = String(to.query.tab || "endpoints");
            const target: Record<string, string> = {
              endpoints: "/webhook-endpoints",
              "webhook-deliveries": "/webhook-deliveries",
              templates: "/notification-templates",
              "notification-deliveries": "/notification-deliveries",
            };
            const query = { ...to.query };
            delete query.tab;
            return { path: target[tab] || "/webhook-endpoints", query };
          },
        },
        {
          path: "webhook-endpoints",
          component: IntegrationView,
          meta: {
            defaultTab: "endpoints",
            title: "Webhook 端点",
            subtitle: "管理事件订阅端点、签名凭据和启停状态",
          },
        },
        {
          path: "webhook-deliveries",
          component: IntegrationView,
          meta: {
            defaultTab: "webhook-deliveries",
            title: "Webhook 投递",
            subtitle: "审计投递响应、重试和失败诊断",
          },
        },
        {
          path: "notification-templates",
          component: IntegrationView,
          meta: {
            defaultTab: "templates",
            title: "通知模板",
            subtitle: "维护多渠道、多语言消息模板和变量",
          },
        },
        {
          path: "notification-deliveries",
          component: IntegrationView,
          meta: {
            defaultTab: "notification-deliveries",
            title: "通知投递",
            subtitle: "查询消息发送状态、重试与失败诊断",
          },
        },
        { path: "notifications", redirect: "/notification-rules" },
        {
          path: "notification-rules",
          component: NotificationAutomationView,
          meta: {
            defaultTab: "rules",
            title: "通知规则",
            subtitle: "按业务事件配置受众、渠道和模板",
          },
        },
        {
          path: "notification-connectors",
          component: NotificationAutomationView,
          meta: {
            defaultTab: "connectors",
            title: "通知连接器",
            subtitle: "安全配置邮件、Telegram 与企业微信连接器",
          },
        },
        {
          path: "notification-events",
          component: NotificationAutomationView,
          meta: {
            defaultTab: "events",
            title: "通知事件目录",
            subtitle: "查看可订阅业务事件、严重等级与模板变量",
          },
        },
        { path: "access", redirect: "/admins" },
        {
          path: "admins",
          component: AccessView,
          meta: {
            defaultTab: "admins",
            title: "管理员",
            subtitle: "管理后台账号、状态和最小权限角色",
          },
        },
        {
          path: "roles",
          component: AccessView,
          meta: {
            defaultTab: "roles",
            title: "角色与权限",
            subtitle: "配置角色和细粒度权限目录",
          },
        },
        {
          path: "access-audit",
          component: AccessView,
          meta: {
            defaultTab: "audit",
            title: "授权审计",
            subtitle: "追踪管理员授权与关键权限变更",
          },
        },
        {
          path: "security",
          component: SecurityView,
          meta: {
            kind: "security",
            // This personal session/TOTP surface is the permission-safe
            // fallback. Override the parent dashboard permission explicitly;
            // route meta is merged across matched records.
            permission: "",
            title: "安全中心",
            subtitle: "登录日志、IP 黑名单和安全事件",
          },
        },
        {
          path: "jobs",
          component: JobsView,
          meta: {
            title: "任务与调度",
            subtitle: "真实队列执行账本、失败诊断与受控业务重放",
          },
        },
        {
          path: "currencies",
          component: CurrencyView,
          meta: {
            defaultTab: "currencies",
            title: "多货币与汇率",
            subtitle: "管理店铺支持币种和启停状态",
          },
        },
        {
          path: "fx-providers",
          component: CurrencyView,
          meta: {
            defaultTab: "providers",
            title: "汇率源",
            subtitle: "配置汇率提供方、优先级和健康状态",
          },
        },
        {
          path: "manual-rates",
          component: CurrencyView,
          meta: {
            defaultTab: "manual",
            title: "手工汇率",
            subtitle: "维护自动汇率不可用时的受控兜底价格",
          },
        },
        {
          path: "fx-snapshots",
          component: CurrencyView,
          meta: {
            defaultTab: "snapshots",
            title: "汇率快照",
            subtitle: "审计订单定价使用的不可变汇率快照",
          },
        },
        {
          path: "settings",
          component: SettingsView,
          meta: {
            title: "系统设置",
            subtitle: "店铺品牌、订单库存与推广结算策略",
          },
        },
      ],
    },
    { path: "/:pathMatch(.*)*", redirect: "/" },
  ],
});

const routePermissions: Record<string, string> = {
  "/": "dashboard.read",
  "/analytics": "dashboard.read",
  "/orders": "order.view",
  "/tickets": "order.view",
  "/products": "catalog.view",
  "/product-categories": "catalog.view",
  "/variants": "catalog.view",
  "/pricing": "catalog.view",
  "/member-levels": "catalog.view",
  "/inventory": "inventory.view",
  "/card-secrets": "inventory.view",
  "/inventory-batches": "inventory.view",
  "/suppliers": "supplier.view",
  "/category-bindings": "supplier.view",
  "/mappings": "supplier.view",
  "/procurements": "supplier.view",
  "/customers": "customer.view",
  "/wallets": "wallet.view",
  "/payment-channels": "payment.view",
  "/wallet-recharges": "payment.view",
  "/payment-intents": "payment.view",
  "/payment-transactions": "payment.view",
  "/refunds": "payment.view",
  "/reconciliation": "payment.view",
  "/promotions": "marketing.view",
  "/coupons": "marketing.view",
  "/gift-card-batches": "marketing.view",
  "/gift-card-list": "marketing.view",
  "/affiliates": "marketing.view",
  "/affiliate-commissions": "marketing.view",
  "/affiliate-withdrawals": "marketing.view",
  "/posts": "marketing.view",
  "/post-categories": "marketing.view",
  "/announcements": "marketing.view",
  "/banners": "marketing.view",
  "/media": "marketing.view",
  "/resellers": "reseller.view",
  "/reseller-tiers": "reseller.view",
  "/reseller-domains": "reseller.view",
  "/reseller-withdrawals": "reseller.view",
  "/risk-rules": "security.view",
  "/risk-decisions": "security.view",
  "/openapi": "system.view",
  "/webhook-endpoints": "system.view",
  "/webhook-deliveries": "system.view",
  "/notification-templates": "system.view",
  "/notification-deliveries": "system.view",
  "/notification-rules": "system.view",
  "/notification-connectors": "system.view",
  "/notification-events": "system.view",
  "/admins": "system.view",
  "/roles": "system.view",
  "/access-audit": "system.view",
  "/jobs": "system.view",
  "/currencies": "system.view",
  "/fx-providers": "system.view",
  "/manual-rates": "system.view",
  "/fx-snapshots": "system.view",
  "/settings": "system.view",
};

for (const route of router.getRoutes()) {
  const permission = routePermissions[route.path];
  if (permission) route.meta.permission = permission;
}

function firstAuthorizedPath(auth: ReturnType<typeof useAuthStore>) {
  for (const [path, permission] of Object.entries(routePermissions)) {
    if (auth.hasPermission(permission)) return path;
  }
  // Personal TOTP/session controls intentionally remain available even when
  // an administrator has no business-module role assignment.
  return "/security";
}

router.beforeEach(async (to) => {
  if (to.path === "/inventory" && to.query.view === "cards") {
    const query = { ...to.query };
    delete query.view;
    return { path: "/card-secrets", query, replace: true };
  }
  const hasToken = Boolean(localStorage.getItem("linlinqi-admin-token"));
  if (!to.meta.public && !hasToken) return "/login";
  if (!hasToken) return;

  const auth = useAuthStore();
  try {
    await auth.ensureProfile();
  } catch (error) {
    const status = (error as { response?: { status?: number } }).response
      ?.status;
    if (status === 401) {
      auth.logout();
      return "/login";
    }
  }

  if (to.path === "/login") return firstAuthorizedPath(auth);
  const permission = String(to.meta.permission || "");
  if (permission && !auth.hasPermission(permission))
    return firstAuthorizedPath(auth);
});

export default router;
