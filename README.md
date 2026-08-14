# LinLinQi-FAKA 搭建教程 / 使用说明

> **LinLinQi-FAKA** 是一个开源的企业级数字商品自动交付平台参考实现，由 Go API、Vue 用户商城、Vue 运营后台、PostgreSQL 与 Redis 组成。

---

## ⚠️ 法律声明 / Legal Disclaimer

> **请在使用、下载、克隆或部署本项目之前，仔细阅读并同意以下条款。继续使用即表示您已阅读并同意本声明。**

**中文：**

1. **仅供开发学习研究**：本项目仅用于编程学习、技术研究和开发测试目的，**严禁用于任何商业运营**。
2. **禁止售卖**：**不得使用本项目（含其源码、构建产物、衍生版本）向任何人或机构售卖任何商品、服务或虚拟产品**。
3. **项目开源**：本项目以开源形式发布，任何人可在遵守本许可条款的前提下学习、研究、修改与再分发。
4. **责任自负**：使用本项目产生的一切后果（包括但不限于运营、交易、法律、安全风险）**均由使用者自行承担，与作者无关**。作者不对任何直接、间接或衍生损害负责。
5. 本项目不内置任何真实支付机构、供货商凭证或生产密钥；生产接入外部系统需自行完成合法合规与安全审计。

**English:**

1. **For development / learning / research only**: This project is provided solely for programming education, technical research, and development testing. **Commercial operation is strictly prohibited.**
2. **No selling**: You **must not use this project** (including its source code, built artifacts, or derivatives) **to sell any goods, services, or virtual products** to anyone.
3. **Open source**: This project is released as open source. Everyone may learn, research, modify, and redistribute it in compliance with the license terms.
4. **Use at your own risk**: All consequences arising from the use of this project (including but not limited to operation, transactions, legal, and security risks) **are the sole responsibility of the user and have nothing to do with the author**. The author is not liable for any direct, indirect, or consequential damages.
5. This project ships with no real payment providers, supplier credentials, or production keys; integrating external systems for production requires your own compliance and security review.

---

## 中文文档

### 项目简介

LinLinQi 是一个**独立实现**的企业级数字商品自动交付平台，覆盖零售、开放供货、分销、支付、库存、交付、财务、风控和运营后台。项目只把 Dujiaoka 与 Dujiao Next 的公开功能范围当作需求研究材料，**没有复制**两个项目的代码、模板、组件、页面结构或视觉样式，使用自己的领域模型、接口协议、交易状态机和中性黑白视觉体系。

### 目录结构

```text
api/      Go 1.26.5 + Gin + GORM，API、迁移与异步 Worker
user/     Vue 3 + Vite 8 + TypeScript 用户商城
admin/    Vue 3 + Vite 8 + TypeScript 运营后台
docs/     架构、安全、OpenAPI、支付、运维与功能矩阵
scripts/  备份/恢复/运维/工具链校验脚本
deploy/   部署相关配置
```

### 已实现的核心链路

- 商品分类、SKU、阶梯价格、会员折扣、促销、优惠券和渠道限制模型
- 游客购物车、多商品结算、事务库存预占、支付超时释放和加密自动交付
- 支付意图、签名支付连接器、回调验签、金额核对、事件幂等、退款与对账模型
- AES-256-GCM 应用层加密：卡密、OpenAPI Secret、支付配置、供货商凭证与 TOTP Secret
- 用户注册登录、15 分钟访问令牌、旋转式刷新令牌、设备会话与退出撤销
- 管理员独立 JWT、数据库 RBAC、TOTP 双因素认证、恢复码和审计日志
- HMAC-SHA256 OpenAPI、时间窗、随机 Nonce、Redis 防重放和外部订单幂等
- 独立供货商客户端、商品映射、定时库存/价格同步和采购领域模型
- ISO 4217 多货币、27 个汇率源配置、实时/手工/可信缓存三档换算、不可变 FX 快照与店铺换币原子重定价
- 钱包双重记账式流水、礼品卡、推广返佣、提现、分销商、独立域名与站点规则
- 工单、文章、公告、横幅、媒体、通知、Webhook、风控、黑名单与安全事件
- Asynq 多优先级队列：订单过期、通知中继、Webhook、供应同步、对账聚合
- Prometheus 指标、存活/就绪探针、请求 ID、请求体限制、CORS、安全响应头和优雅停机
- 事务化迁移账本、迁移校验和与 PostgreSQL advisory lock，避免多实例并发迁移

完整模块见 [功能矩阵](docs/FEATURES.md)，运行设计见 [架构说明](docs/ARCHITECTURE.md)，金额与换汇边界见 [多货币与汇率体系](docs/CURRENCY.md)，默认商用视觉与替换规则见 [品牌图片资产](docs/BRAND_ASSETS.md)，受控运维代理边界见 [小龙虾手册](docs/LOBSTER.md)。

### 技术基线

| 组件 | 版本/策略 |
| --- | --- |
| Go | 1.26.5 |
| PostgreSQL | 18.4 |
| Redis | 8.8.1 stable（8.10 仍为 RC，不进入生产） |
| Node.js | 24.18.1 LTS |
| Alpine Linux | 3.24.1 |
| nginx | 1.30.4 stable |
| npm | 12.0.2 |
| Vite | 8.2.1 |
| TypeScript | 5.9.3 |
| vue-tsc | 3.3.9 |
| vue-i18n | 11.4.8 |
| @lucide/vue | 1.30.0 |
| GitHub Actions | checkout/setup-go/setup-node v7 |

### 快速开始（搭建教程）

#### 环境要求

- Docker 与 Docker Compose（推荐，一条命令启动全部服务）
- 或本地工具链：Go 1.26.5、Node.js 24.18.1 LTS（npm 12.0.2）、PostgreSQL 18.4、Redis 8.8.1

#### 方式一：Docker Compose（推荐）

```bash
# 1. 克隆仓库
git clone https://github.com/19910205/LinLinQi-FAKA.git && cd LinLinQi-FAKA

# 2. 复制环境配置（开发环境可保留示例值）
cp .env.example .env

# 3. 启动（开发模式：开放本机端口、启用演示数据、创建初始管理员）
docker compose --env-file .env -f docker-compose.yml -f docker-compose.dev.yml up --build
```

启动后访问：

- 商城：<http://localhost:5173>
- 后台：<http://localhost:5174>
- API：<http://localhost:8080>
- 就绪检查：<http://localhost:8080/ready>
- Prometheus：`GET http://localhost:8080/metrics`，请求头 `Authorization: Bearer $METRICS_TOKEN`

> **默认开发口令仅为 `admin / LinLinQi@2026`**；请勿在公网或生产环境使用。

#### 方式二：宿主机本地开发

根目录 `.tool-versions`、`.node-version`、`.nvmrc` 与 `.go-version` 固定全局工具链；Node 24.18.1 安装后先执行 `npm install --global npm@12.0.2`。然后：

```bash
make dev-api     # API（Go，端口 8080）
make dev-user    # 用户商城（Vite，端口 5173）
make dev-admin   # 运营后台（Vite，端口 5174）
make migrate     # 手动执行数据库迁移
```

`make verify-toolchain` 会对照 `toolchain.json` 检查开发、CI、Docker、Compose 与两个前端锁文件是否发生版本漂移。

### 使用说明

- **注册登录**：用户商城支持注册登录，访问令牌 15 分钟、旋转式刷新令牌；管理员使用独立 JWT + 数据库 RBAC + TOTP 双因素认证。
- **商品与结算**：商品分类 / SKU / 阶梯价格 / 会员折扣 / 促销 / 优惠券；游客购物车、多商品结算、事务库存预占、支付超时释放、加密自动交付。
- **支付接入**：在管理后台创建 `signed_http` 支付渠道并配置回调验签；支付机构属外部系统，需自行联调验收。
- **供货商**：在管理后台创建供货商连接，配置商品映射与定时库存/价格同步。
- **OpenAPI**：HMAC-SHA256 签名、时间窗、随机 Nonce、Redis 防重放与外部订单幂等，详见 [docs/openapi.md](docs/openapi.md)。
- **备份与恢复**：

  ```bash
  make backup-postgres
  export LINLINQI_RESTORE_CONFIRM="restore:linlinqi-postgres-20260809T120000Z.dump"
  make restore-postgres BACKUP=/absolute/path/to/linlinqi-postgres-20260809T120000Z.dump
  ```

  恢复是破坏性维护操作，必须在隔离恢复演练通过后执行；脚本逻辑备份不是 PITR 或异地备份的替代品。

### 生产部署要点

1. 以 `umask 077` 创建 `.env`，设置真实域名，并用 `openssl rand -hex 32` 分别生成 PostgreSQL、Redis、JWT、数据加密、OpenAPI 和指标密钥。PostgreSQL 密码必须保持 URL 安全（Compose 会把它嵌入连接 URL）；`.env` 权限必须为 `0600` 且不得提交。
2. 首次引导时临时设置 `BOOTSTRAP_ADMIN=true` 与强 `BOOTSTRAP_ADMIN_PASSWORD`，启动完成后立即改回 `BOOTSTRAP_ADMIN=false` 并删除引导口令。`SEED_DATA` 只允许开发环境使用，生产开启会拒绝启动。
3. 在管理 API 中创建 `signed_http` 支付渠道、供货商连接、通知中继与 Webhook。
4. 在同一主机配置受信任的 TLS 反向代理，把公网 HTTPS 域名转发到 `.env` 中的三个环回端口；基础 Compose 自身不签发证书。
5. 只使用基础 Compose 文件启动：

   ```bash
   docker compose --env-file .env -f docker-compose.yml up -d --build
   ```

基础 Compose 将 API、商城和后台绑定到 `127.0.0.1`，PostgreSQL/Redis 只加入隔离数据网络；数据库、Redis 与本地媒体分别使用持久卷。一次性 `migrate` 服务成功退出后 API/Worker 才会启动，迁移失败会阻断发布。该 Compose 是单机参考拓扑，不替代 TLS 证书管理、异机备份、PITR、集中日志、监控告警或高可用编排。生产检查清单见 [运营手册](docs/OPERATIONS.md) 与 [安全基线](docs/SECURITY.md)。

### 验证

```bash
make verify-toolchain
make test
cd api && CGO_ENABLED=0 go build -trimpath ./cmd/linlinqi
cd ../user && npm run build
cd ../admin && npm run build
```

### 文档索引

- [架构说明](docs/ARCHITECTURE.md) · [功能矩阵](docs/FEATURES.md) · [安全基线](docs/SECURITY.md)
- [运营手册](docs/OPERATIONS.md) · [供应链运行手册](docs/SUPPLY.md) · [支付](docs/PAYMENTS.md)
- [多货币与汇率](docs/CURRENCY.md) · [品牌图片资产](docs/BRAND_ASSETS.md) · [小龙虾手册](docs/LOBSTER.md)
- [OpenAPI Markdown](docs/openapi.md) · [OpenAPI 3.1 YAML](docs/openapi.yaml)

> 支付机构、通知中继、对象存储、域名/TLS 和真实供货商均属于部署方外部系统，仓库不会虚构这些服务或内置真实凭证。

---

## English Documentation

### Overview

**LinLinQi** is an **independently implemented** enterprise-grade digital goods auto-delivery platform covering retail, open supply, distribution, payment, inventory, delivery, finance, risk control, and an operations console. It uses Dujiaoka and Dujiao Next only as public requirement/research material and **does not copy** their code, templates, components, page structure, or visual style. LinLinQi has its own domain model, API protocol, transaction state machine, and neutral black-and-white visual system.

### Repository Layout

```text
api/      Go 1.26.5 + Gin + GORM — API, migrations and async workers
user/     Vue 3 + Vite 8 + TypeScript customer storefront
admin/    Vue 3 + Vite 8 + TypeScript operations console
docs/     Architecture, security, OpenAPI, payments, operations and feature matrix
scripts/  Backup / restore / ops / toolchain verification scripts
deploy/   Deployment assets
```

### Tech Baseline

| Component | Version / Policy |
| --- | --- |
| Go | 1.26.5 |
| PostgreSQL | 18.4 |
| Redis | 8.8.1 stable |
| Node.js | 24.18.1 LTS |
| Alpine Linux | 3.24.1 |
| nginx | 1.30.4 stable |
| npm | 12.0.2 |
| Vite | 8.2.1 |
| TypeScript | 5.9.3 |
| vue-tsc | 3.3.9 |
| vue-i18n | 11.4.8 |
| GitHub Actions | checkout/setup-go/setup-node v7 |

### Quick Start (Setup Tutorial)

#### Prerequisites

- Docker + Docker Compose (recommended — one command boots everything), or
- Local toolchain: Go 1.26.5, Node.js 24.18.1 LTS (npm 12.0.2), PostgreSQL 18.4, Redis 8.8.1

#### Option A: Docker Compose (recommended)

```bash
git clone https://github.com/19910205/LinLinQi-FAKA.git && cd LinLinQi-FAKA
cp .env.example .env
docker compose --env-file .env -f docker-compose.yml -f docker-compose.dev.yml up --build
```

Access after boot:

- Storefront: <http://localhost:5173>
- Admin console: <http://localhost:5174>
- API: <http://localhost:8080>
- Readiness: <http://localhost:8080/ready>
- Prometheus: `GET http://localhost:8080/metrics` with header `Authorization: Bearer $METRICS_TOKEN`

> **Default dev credentials are only `admin / LinLinQi@2026`** — never use them on the public internet or in production.

#### Option B: Local Development on Host

Pin the global toolchain via `.tool-versions`, `.node-version`, `.nvmrc` and `.go-version`; after installing Node 24.18.1 run `npm install --global npm@12.0.2`. Then:

```bash
make dev-api     # API (Go, port 8080)
make dev-user    # storefront (Vite, port 5173)
make dev-admin   # admin console (Vite, port 5174)
make migrate     # run DB migrations manually
```

`make verify-toolchain` checks dev/CI/Docker/Compose and both frontend lock files against `toolchain.json` for version drift.

### Usage

- **Auth**: Customer sign-up/sign-in with 15-minute access tokens and rotating refresh tokens; admins use separate JWT + DB RBAC + TOTP 2FA.
- **Catalog & checkout**: categories / SKUs / tiered pricing / member discounts / promotions / coupons; guest cart, multi-item checkout, transactional stock reservation, timeout release, encrypted auto-delivery.
- **Payments**: create a `signed_http` channel in the admin console and configure callback signature verification; payment providers are external systems — complete your own integration testing.
- **Suppliers**: create supplier connections, product mapping, scheduled stock/price sync.
- **OpenAPI**: HMAC-SHA256 signing, time windows, random nonces, Redis replay protection and external order idempotency — see [docs/openapi.md](docs/openapi.md).
- **Backup & restore**:

  ```bash
  make backup-postgres
  export LINLINQI_RESTORE_CONFIRM="restore:linlinqi-postgres-20260809T120000Z.dump"
  make restore-postgres BACKUP=/absolute/path/to/linlinqi-postgres-20260809T120000Z.dump
  ```

  Restore is a destructive maintenance operation; run it only after an isolated restore drill. Script backups are not a substitute for PITR or off-site backups.

### Production Deployment Notes

1. Create `.env` with `umask 077`, set real domains, and generate separate keys for PostgreSQL, Redis, JWT, data encryption, OpenAPI and metrics with `openssl rand -hex 32`. The PostgreSQL password must stay URL-safe; keep `.env` at `0600` and never commit it.
2. Set `BOOTSTRAP_ADMIN=true` with a strong `BOOTSTRAP_ADMIN_PASSWORD` only for the first boot, then flip it back to `false` and remove the bootstrap password. `SEED_DATA` is dev-only; production refuses to start with it enabled.
3. Create `signed_http` payment channels, supplier connections, notification relays and webhooks in the admin API.
4. Put a trusted TLS reverse proxy in front, forwarding public HTTPS domains to the three loopback ports in `.env`; the base Compose does not issue certificates.
5. Start with the base Compose file only:

   ```bash
   docker compose --env-file .env -f docker-compose.yml up -d --build
   ```

The base Compose binds API/storefront/admin to `127.0.0.1`; PostgreSQL/Redis join an isolated data network only; database, Redis and local media use persistent volumes. The one-shot `migrate` service must exit successfully before API/Worker start — migration failure blocks release. This Compose is a single-host reference topology, not a substitute for TLS certificate management, off-site backups, PITR, centralized logging, alerting, or HA orchestration. See the [Operations](docs/OPERATIONS.md) and [Security](docs/SECURITY.md) runbooks for the production checklist.

### Verification

```bash
make verify-toolchain
make test
cd api && CGO_ENABLED=0 go build -trimpath ./cmd/linlinqi
cd ../user && npm run build
cd ../admin && npm run build
```

### Documentation Index

- [Architecture](docs/ARCHITECTURE.md) · [Features](docs/FEATURES.md) · [Security](docs/SECURITY.md)
- [Operations](docs/OPERATIONS.md) · [Supply](docs/SUPPLY.md) · [Payments](docs/PAYMENTS.md)
- [Currency & FX](docs/CURRENCY.md) · [Brand Assets](docs/BRAND_ASSETS.md) · [Lobster](docs/LOBSTER.md)
- [OpenAPI Markdown](docs/openapi.md) · [OpenAPI 3.1 YAML](docs/openapi.yaml)

> Payment providers, notification relays, object storage, domains/TLS and real suppliers are external systems owned by the deployer — this repository does not fabricate these services or ship real credentials.

---

## License / 开源许可

本项目以开源形式发布，但附带**非商用限制**，详见 [LICENSE](LICENSE)。

This project is released as open source with **non-commercial restrictions**. See [LICENSE](LICENSE).
