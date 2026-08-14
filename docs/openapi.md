# LinLinQi OpenAPI v1

这是 LinLinQi 的 clean-room 供货契约。它描述的是本项目实际实现的行为，不是参考站点的源码或私有接口。机器可读契约见 [`openapi.yaml`](openapi.yaml)。

## 认证与幂等

所有请求都使用 API 凭证签名：

- `X-API-Key`：凭证公钥。
- `X-Timestamp`：Unix 秒；服务端接受前后 5 分钟。
- `X-Nonce`：每次请求唯一，至少 16 个字符；Redis `SET NX` 防重放 6 分钟。
- `X-Signature`：`hex(hmac_sha256(api_secret, timestamp + "." + nonce + "." + method + "." + request_target + "." + sha256_hex(raw_body)))`。

`request_target` 是实际请求路径（含 `/openapi/v1` 前缀）；存在查询参数时必须原样追加 `?` 与收到的 raw query，包括参数顺序和百分号编码。GET 请求的正文为空字节串。API Secret 只在服务端 AES-GCM 密文中保存。

`POST /orders` 的 `client_order_no` 在同一 API 凭证下幂等。重试必须复用相同的商品、规格、数量、邮箱、币种、履约参数和回调地址；事实不同会返回 `40909` 幂等冲突。

## 统一响应与金额

响应外层始终是 `{ "code": 0, "message": "ok", "data": ... }`。错误响应仍会返回稳定业务码和可本地化的消息键。

金额不是浮点数：每个金额字段都是对应币种的最小单位 `int64`，并且同一对象同时给出 `currency` 与 `minor_unit`。例如 USD 的 `1.00` 是 `100`，CNY 的 `¥10.54` 是 `1054`；JPY/VND 等零位币种不除以 100。采购方必须按目录返回的币种解释价格，不能把所有成本硬编码为人民币分。

同步和订单会保存不可变的上游币种、目标币种、汇率快照和加价快照。典型计算：

```text
100 USD cents × 7.0267 × (1 + 5000/10000) = 1054 CNY cents（最终一次 half-up）
```

## 目录接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/openapi/v1/account/balance` | 读取凭证所属结算账户余额 |
| GET | `/openapi/v1/categories` | 分类树、层级、描述和本地媒体 URL |
| GET | `/openapi/v1/products` | 商品、规格、富描述、图库、输入字段和实时可售库存 |
| GET | `/openapi/v1/products/{product_id}` | 按 UUID 或 slug 读取单商品完整字段 |
| GET | `/openapi/v1/products/{product_id}/stock` | 读取扣除本地预占后的可售库存；可传 `variant_id` |
| POST | `/openapi/v1/products/{product_id}/quote` | 按凭证身份计算与下单一致的服务端权威报价 |
| POST | `/openapi/v1/orders` | 创建供货订单并触发自动履约 |
| GET | `/openapi/v1/orders/{order_no}` | 使用订单号和查询令牌轮询订单 |

目录默认只返回上架记录。同步器可使用：

- `currency=USD|CNY|...`：请求展示/结算报价币种；不改变历史订单事实账。
- `page`、`page_size`（1–500）：可选分页。未传时保持兼容，返回完整目录。
- `updated_after=<RFC3339>`：只返回更新时间晚于该时间的记录。
- `include_inactive=true`：同时返回下架记录，用于同步 tombstone。

分页元数据通过响应头返回：`X-LinLinQi-Total-Count`、`X-LinLinQi-Page`、`X-LinLinQi-Page-Size`、`X-LinLinQi-Has-More`、`X-LinLinQi-Returned-Count`。

单商品接口支持 `currency` 与 `include_inactive`；库存接口返回 `external_product_id`、`product_id`、可选 `variant_id`、`stock`、`stock_status`、`observed_at`。`stock` 已扣除尚未终态订单的本地预占，不泄露上游路由与预占明细。

权威报价使用与订单创建相同的价格解析器，校验商品状态、必选规格、规格状态、数量以及商品/规格购买上下限。请求体严格限定为：

```json
{
  "variant_id": "可选规格 UUID 或 SKU",
  "quantity": 1,
  "currency": "CNY"
}
```

响应中的金额均为 `currency` 对应的 `minor_unit` 最小单位；`quoted_at` 为 UTC RFC3339 时间。用户凭证应用该用户可用的会员价、阶梯价和活动价，分站凭证应用该分站的已启用售价。接口不创建订单、不预占库存，并设置 `Cache-Control: no-store`；不会返回商品成本、上游成本、平台结算价或分站毛利。

```json
{
  "external_product_id": "PRODUCT_UUID",
  "product_id": "PRODUCT_UUID",
  "variant_id": "VARIANT_UUID",
  "quantity": 1,
  "unit_amount": 1054,
  "subtotal": 1054,
  "discount_amount": 0,
  "amount": 1054,
  "currency": "CNY",
  "minor_unit": 2,
  "fx": {
    "source_currency": "USD",
    "target_currency": "CNY",
    "rate": "7.0267",
    "source_tier": "manual"
  },
  "quoted_at": "2026-08-10T13:14:15Z"
}
```

分类字段：

```json
{
  "id": "UUID",
  "external_id": "稳定分类 ID",
  "external_parent_id": "父分类 ID 或空字符串",
  "name": "账号服务",
  "slug": "accounts",
  "description": "经过白名单净化的描述",
  "icon": "local-icon-or-name",
  "image_url": "https://store.example/media/...",
  "sort": 100,
  "status": "active",
  "created_at": "2026-08-10T00:00:00Z",
  "updated_at": "2026-08-10T00:00:00Z"
}
```

商品字段包含：`id`、`external_id`、`external_sku`、`external_category_id`、`category_id`、`name`、`slug`、`summary`、`description`、`cover_url`、`image_urls[]`、`source_currency`、`currency`、`fx`、`price`、`compare_price`、`stock`、`minimum`、`maximum`、`status`、`delivery`、`delivery_type`、`inventory_mode`、`tags`、`variants[]`、`input_fields[]`、`created_at`、`updated_at`。

开启媒体镜像时，`image_urls` 只返回可公开访问的本地媒体地址；远端封面、图库和描述内图片由同步策略控制，下载失败不会覆盖已有本地资源，并记录失败等待重试。关闭镜像属于运营显式选择，可能返回经过校验的来源 URL。描述 HTML 在公开输出前经过服务端白名单净化，采购方仍不应执行任意脚本。

规格额外提供 `external_id`、`external_sku`、`attributes`、`price`、`compare_price`、`stock`、`minimum`、`maximum`、`purchase_limit` 和 `status`。输入字段提供 `key`、`label`、`input_type`、`required`、`sensitive`、`placeholder`、`help_text`、`options`、`validation_pattern`、`min_length`、`max_length`、`sort`。

## 余额

```json
{
  "balance": 1250000,
  "currency": "USD",
  "minor_unit": 2,
  "updated_at": "2026-08-10T12:00:00Z"
}
```

`balance=0` 只表示上游确认的零余额。未成功读取时接口返回错误，管理端显示“未同步”，不会用默认零值伪装成功。

## 创建订单

```json
{
  "external_product_id": "REMOTE-SKU-001",
  "variant_id": "可选本地规格 UUID 或 SKU",
  "quantity": 1,
  "email": "buyer@example.com",
  "payment_method": "supplier_balance",
  "client_order_no": "YOUR-ORDER-20260810-0001",
  "callback_url": "https://buyer.example/api/callback",
  "parameters": {"account_id": "9384750291"},
  "input_values": [{"field_id": "FIELD_UUID", "value": "9384750291"}],
  "currency": "CNY"
}
```

`product_id` 是兼容字段；优先使用目录中的 `external_product_id`。商品有规格时必须选择规格的稳定 ID/SKU。`parameters` 与 `input_values` 不可对同一字段重复提交，敏感值不会写入请求日志。服务端校验未知键、必填项、类型、长度、选项和 RE2 表达式。

订单响应和回调 `data`：

```json
{
  "client_order_no": "YOUR-ORDER-20260810-0001",
  "external_order_no": "LQ202608100001",
  "status": "delivered",
  "deliveries": ["CARD-CONTENT-1"],
  "cost": 1054,
  "cost_currency": "CNY",
  "cost_minor_unit": 2
}
```

`deliveries` 仅在 `delivered`/`completed` 且已支付时返回。状态流转为 `pending_payment → paid → processing → delivered`，采购失败进入 `failed`/人工复核；支付超时释放库存 hold。回调与轮询通过同一采购单幂等收口。

## 回调

LinLinQi 对 `callback_url` 发送 `POST application/json`，头部为 `X-LinLinQi-Timestamp` 与 `X-LinLinQi-Signature`。签名是 `HMAC-SHA256(api_secret, timestamp + "." + raw_body)`。接收方必须对原始字节验签、校验时间窗并按 `event_id` 幂等处理。网络失败按指数退避；回调失败不影响轮询兜底。

## 安全与运营边界

- 供应商凭证、客户输入、卡密、回调正文和采购审计字段分级 AES-GCM 加密，密钥由部署环境/KMS 注入并支持轮换。
- 出站 URL 执行 HTTPS、公网 DNS、重定向复验、SSRF 私网阻断、大小/MIME/像素限制；生产不关闭 TLS 校验。
- 每个供应商可在后台按协议动态填写用户名+密码、ID+KEY、单 KEY、Bearer 等字段；没有运行时 driver 的协议显示为不可启用，`POST /admin/v1/suppliers/{id}/probe` 只执行余额/分类/商品读取，不会下单。
- 自动风控动作采用最小权限、可回滚 playbook 和人工审批；本项目不提供未经授权的抓包、渗透或任意停站能力。
