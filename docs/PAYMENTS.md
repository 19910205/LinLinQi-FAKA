# 支付连接器

LinLinQi 核心只依赖 `payment.Driver` 接口。仓库提供开发沙箱和生产 `signed_http` 连接器，不内置任何第三方商户密钥。

生产连接器配置：

```json
{
  "name": "Production Gateway",
  "code": "gateway_cny",
  "provider": "signed_http",
  "fee_rate": 60,
  "supported_currencies": ["CNY", "USD"],
  "settlement_currency": "CNY",
  "enabled": true,
  "sort": 100,
  "config": {
    "base_url": "https://payments.example.com",
    "merchant_id": "merchant-id",
    "secret": "at-least-24-character-secret"
  }
}
```

`fee_rate` 使用基点，60 表示 0.60%。配置经 AES-GCM 加密后存储。

网关地址、商户号与签名密钥构成不可变的渠道身份。渠道一旦被支付意图或充值单引用，后台禁止原地替换这三项；轮换时必须新建新的渠道代码，再停用旧渠道。停用只阻止新收款，旧渠道仍可验签历史回调和执行历史退款，因此密钥轮换不会把旧退款发往新商户，也不会切断已付款交易。名称、排序和启停等运营字段不受此限制。

`settlement_currency` 是渠道实际收款与退款币种，必须属于 `supported_currencies`。订单始终使用后台 `store_currency` 记账；两者不同时，LinLinQi 在创建支付时通过不可变 FX 快照生成渠道金额，并把订单侧与网关侧金额同时写入支付意图。充值同样把网关实付与钱包入账拆分保存，不能用前台展示币种替代任何一侧事实账。

连接器调用 `/v1/payments`、`/v1/payments/{trade_no}` 和 `/v1/refunds`。支付与退款请求都显式携带最小单位 `amount` 和 ISO `currency`，并包含商户号、时间戳和 HMAC 签名；回调签名为 `HMAC(secret, timestamp + "." + body)`。回调 JSON 必须携带三位大写 ISO 4217 `currency`，服务端同时校验时间窗、事件 ID、交易号、最小货币单位金额、币种与状态；金额相同但币种不同也绝不会履约、入账或被当成同一笔退款。

退款只允许对已交付、已完成或采购失败的终态履约发起，不允许与正在调用上游的 `processing` 订单并发。活动退款存在时，后台采购重试、人工补偿交付、订单恢复与失败退款任务重放都会在行锁内拒绝。Worker 在上游回包入库前再锁定订单并复核 `processing + paid`；旧任务延迟回包也不能发卡或把 `refunded` 改回 `delivered`。全额退款会释放该订单的供应商库存预占。

## 本地钱包订单与退款

商城 `balance` 和 OpenAPI `supplier_balance` 都不是“无支付记录”的特殊订单。扣款事务会先写入确定性的 `WalletEntry`（`LQW-STORE-{order_id}` 或 `LQW-API-{order_id}`），再写入 `channel_id=00000000-0000-0000-0000-000000000000` 的钱包支付意图和成功支付交易；任何一步失败都会连同订单一起回滚。历史订单首次退款时也必须通过原始扣款流水、订单金额币种和账本所有者校验后才能补齐支付审计，不能仅凭订单字段发钱。

钱包退款只能原路贷记原始扣款流水指向的账户，退款流水号固定为 `LQW-REFUND-{refund_id}`。Worker 识别零渠道 ID 后不会读取或调用外部 `PaymentChannel`，而是在同一数据库事务中完成钱包贷记、退款交易、支付意图和订单状态更新；任务重复投递只会命中同一账本流水。部分退款按订单金额累计占用，后一笔必须等待前一笔结束；全额累计退款后订单与支付意图同时进入 `refunded`。创建退款前还会拒绝活动采购和另一笔活动退款，避免采购交付、外部退款与钱包反向入账并发。

## 充值回调错额与错币种

已通过渠道验签且状态为 `succeeded` 的充值回调是不可丢失的收款事实。只有实收金额、实收币种、渠道交易号、充值单活动状态和有效期全部精确匹配时，系统才创建钱包账；任一条件不符都不会创建钱包账户或钱包流水，也不会发放充值赠额。

异常回调在原数据库事务内写入 `recharge_transactions`：`status=succeeded` 保存渠道事实，`disposition=refund_pending` 保存本地处置状态，并同时记录应收/实收金额币种、退款单号和原因。`raw_payload` 不保存渠道原始正文，只保存验签结果、必要字段与原始正文 SHA-256；任意额外客户字段、令牌或渠道私有字段都不会落库。充值单进入 `requires_refund`，后台充值列表同时返回异常数量、最新退款状态、原因与重试次数供运营查看。

`linlinqi:recharge-refund:process` 关键队列使用同一个 `refund_no` 向原渠道退款，成功后进入 `refunded`；网络或渠道暂态错误按指数退避进入 `refund_retrying`，Worker 每分钟从数据库恢复未完成或卡在 `refund_processing` 的任务。24 次失败后进入 `refund_failed`，运营人员可在后台任务中心审阅并重放。渠道事件 ID、同一充值单内渠道交易号和退款单号均有持久幂等保护，重复回调或任务重试不会重复入账或生成第二笔退款。

渠道对账同时读取普通订单收款/退款、`requires_refund` 的异常实收、充值实收以及充值异常退款，金额使用渠道实际最小单位和实际币种。付款与退款分别以渠道交易号构成唯一对账身份；同一渠道出现重复身份会让批次失败并要求人工核查，而不是覆盖其中一条资金事实。
