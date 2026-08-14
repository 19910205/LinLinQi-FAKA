# LinLinQi 管理后台

LinLinQi 自动虚拟发卡平台的运营管理端，基于 Vue 3、Vite 与 TypeScript。

## 本地开发

```bash
npm ci
npm run dev
```

开发服务器通过 Vite 将 `/api` 代理到 `http://localhost:8080`。生产构建：

```bash
npm run build
```

## 管理范围

- 商品、规格、分类、卡密库存与批次
- 订单、支付通道、退款、钱包与对账
- 用户、等级、经销商、推广、优惠活动与礼品卡
- 供应商、采购、库存同步与 OpenAPI 凭证
- 工单、内容、通知、Webhook、审计与风控
- 管理员 RBAC、TOTP 双因素认证及恢复码
- 运行指标、任务队列、系统配置与维护状态
- 黑白主题与响应式运营界面

管理端不包含离线登录或生产演示兜底；所有权限和数据均由后端验证并提供。
