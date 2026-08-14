# 运营与上线手册

## 首次引导

1. 用生成器一键创建 `.env`（chmod 600）：它会为 PostgreSQL、Redis、JWT、数据加密、OpenAPI、指标和通知中继生成独立强随机密钥，并根据域名推导 `APP_URL`、`USER_APP_URL`、CORS 与媒体基址；已有非占位值会原样保留，重复运行不会轮换在线密钥。

   ```bash
   node scripts/generate-production-env.mjs \
     --api-url https://api.example.com \
     --user-url https://store.example.com \
     --admin-url https://admin.example.com \
     --support-email ops@example.com
   # 或者域名 DNS 已指向本机时：
   node scripts/generate-production-env.mjs --auto-host store.example.com
   ```

2. 首次仅临时追加 `BOOTSTRAP_ADMIN=true` 与强引导密码；生产环境必须保持 `SEED_DATA=false`。
3. 生产前端 API URL 必须使用 HTTPS；只有 localhost、127.0.0.1 和 ::1 的开发构建允许 HTTP。
4. 登录后启用管理员 TOTP、创建独立运营角色并修改引导账户密码。
5. 关闭 Seed，删除引导口令，滚动重启 API/Worker。

域名识别不需要人工维护：商城/后台 API 按浏览器 `Host` 与 `X-Storefront-Host` 自动路由到对应分销站点；主站域名由生成器写入的 `APP_URL`/`USER_APP_URL` 一次性确定，后续证书与反向代理指向同一组域名即可。

## 迁移与发布门禁

- 基础 Compose 包含一次性 `migrate` 服务。它以 `BOOTSTRAP_ADMIN=false`、`SEED_DATA=false` 执行迁移；只有成功退出后 API 和 Worker 才会启动。
- 常规 `docker compose up -d --build` 会自动执行该门禁。需要在维护窗口单独验证时运行 `make compose-migrate`。
- 迁移有 PostgreSQL advisory lock 和校验和保护，但上线前仍须在生产快照副本演练；禁止通过修改迁移账本绕过失败。
- 发布镜像必须使用唯一且不可变的 `LINLINQI_IMAGE_TAG`。不要在生产使用 `latest`、`local` 或被覆盖的历史标签。

## Compose 部署边界

- 基础 `docker-compose.yml` 是生产 fail-closed 的单机参考拓扑；不要叠加 `docker-compose.dev.yml` 上线。
- API、商城和后台默认只监听宿主机 `127.0.0.1`。公网入口必须由同机 TLS 反向代理承担证书、HTTPS 跳转、HSTS 和请求大小/超时策略，并把 `APP_URL`、前端构建 URL、CORS 与真实域名保持一致。
- PostgreSQL 与 Redis 不发布生产端口，只连接 `internal` 数据网络；API/Worker 同时连接出口网络以访问已配置的支付、供货和通知端点。
- `media_data` 单独持久化本地内容寻址对象；`media-init` 只获得建立目录所需的 capabilities，完成后退出。API 与 Worker 以 UID 10001 共享该卷，其他服务不挂载，应用根文件系统继续只读。
- 前端 nginx 与 API 使用固定非 root UID、只读根文件系统、`no-new-privileges`、全部 capability drop 和独立 `/tmp`；PostgreSQL/Redis 保留官方入口所需的初始化权限。
- `.env` 仅允许部署账户读取（`0600`），不能进入镜像、备份日志或版本控制。Docker 管理员仍能查看容器环境；有 Secret Manager 的编排环境应在部署层注入同名变量。
- Compose 内部 PostgreSQL/Redis 流量依赖主机隔离网络，不提供东西向 TLS。若合规要求数据库链路 TLS，需要在上线架构中增加证书、服务端 TLS 和相应客户端配置，不能把本参考 Compose 描述成已启用数据库 TLS。

## macOS 原生部署

- 原生部署使用全局 Homebrew 二进制，不依赖 Docker、Colima 或其他虚拟机。当前端口为 API `127.0.0.1:8081`、商城 `127.0.0.1:8080`、后台 `127.0.0.1:8082`、Redis `127.0.0.1:6379`、PostgreSQL 18 `127.0.0.1:5433`；5433 用于避开宿主机既有的 5432 数据库。
- API、Worker 与前端构建产物直接从项目目录 `/Users/dahai/Documents/faka` 运行：后端为 `api/linlinqi`，前端为 `user/dist` 与 `admin/dist`。`~/Library/LaunchAgents/com.linlinqi.*.plist` 负责登录后自启，`scripts/native-macos-service.sh status|start|stop|restart` 统一管理五个服务。
- macOS 会阻止 LaunchAgent 直接执行 `Documents` 中的程序。当前本机因此使用 `make local-start|local-stop|local-restart|local-status` 管理项目目录内的 API/Worker；PID 与日志保存在项目的 `.runtime/`。Nginx、PostgreSQL、Redis 仍由 LaunchAgent 托管。
- PostgreSQL 仅监听环回地址，TCP 使用 SCRAM-SHA-256；Redis 使用只保存密码哈希的 ACL、AOF `everysec`、512MB 上限和 `noeviction`。运行环境副本和管理员初始凭据必须保持 `0600`。
- 本地 HTTP 验收环境必须保持 `APP_ENV=development`。要转为公网生产，必须先准备真实域名和证书，将 API/前端置于 TLS 入口之后，再把 URL、CORS、支持邮箱和 `APP_ENV=production` 一次性切换；禁止把环回 HTTP 冒充公网生产。
- `deploy/macos/` 保存原生服务模板。执行 `make build` 会在本项目生成 `api/linlinqi`、`user/dist` 与 `admin/dist`，不使用系统临时目录；完成迁移与验证后重启 API、Worker 与 Nginx。`~/.linlinqi` 仅保存环境密钥、运行日志、媒体、数据库配置与备份等持久数据。

### 原生媒体目录与容量

- 原生包装器默认设置 `STORAGE_ROOT=/Users/dahai/.linlinqi/storage`，并以 `0700` 创建 `media/objects/sha256`、`media/staging`、`media/quarantine`、`mirror/objects`、`spool/protocol-sync` 与 `tmp`。可在发布前运行 `make native-prepare-storage`；该命令只补目录和权限，不覆盖 `~/.linlinqi/config/linlinqi.env`。
- 上传与镜像共享 `media/objects/sha256/<前两位>/<SHA-256>.<扩展名>`，相同内容只保留一份。只有对象目录属于永久业务数据；staging、quarantine、spool 与 tmp 不得经由 nginx 暴露，也不应进入常规备份。
- `MEDIA_MAX_IMAGE_BYTES` 控制单文件上限，`MEDIA_STORAGE_MAX_BYTES` 控制对象总量，`MEDIA_MIN_FREE_BYTES` 是拒绝继续写入前必须保留的文件系统余量。默认分别为 20 MiB、200 GiB 与 100 GiB。变更上限前必须确认 PostgreSQL 所在卷仍有独立安全余量。
- `MEDIA_PUBLIC_BASE_URL` 必须指向公开 `/media` 基址。生产使用 HTTPS，例如 `https://api.example.com/media`；原生环回验收默认使用商城 nginx 同源代理 `http://127.0.0.1:8080/media`。
- API 的 `/ready` 会验证存储目录、真实写入与剩余空间。nginx 模板将 `/media/` 反向代理给 API，由 API 校验内容寻址路径和 MIME，并返回一年 immutable 缓存；nginx 没有 staging 或对象根目录的直接文件权限映射。
- 当前 macOS 用户没有目录级 quota 时，应用配额只是第一道保护。正式保存客户媒体前应启用 FileVault，并优先把 `STORAGE_ROOT` 放在有 quota 的独立 APFS 卷或独立磁盘，避免媒体写满拖垮 PostgreSQL。

### 原生维护任务安装

先静态检查模板，再把脚本和 LaunchAgent 原子安装到运行目录；以下操作不会改写现有环境文件：

```bash
bash -n scripts/backup-postgres-native.sh scripts/restore-postgres-native.sh \
  scripts/prepare-native-storage.sh scripts/rotate-native-macos-logs.sh
for file in deploy/macos/com.linlinqi.*.plist; do plutil -lint "$file"; done
/usr/local/opt/nginx/bin/nginx -t \
  -p /Users/dahai/.linlinqi/run/nginx/ \
  -c "$PWD/deploy/macos/nginx.conf"

install -m 0755 scripts/backup-postgres-native.sh /Users/dahai/.linlinqi/bin/
install -m 0755 scripts/restore-postgres-native.sh /Users/dahai/.linlinqi/bin/
install -m 0755 scripts/rotate-native-macos-logs.sh /Users/dahai/.linlinqi/bin/
install -m 0644 deploy/macos/nginx.conf /Users/dahai/.linlinqi/nginx/nginx.conf
for file in deploy/macos/com.linlinqi.*.plist; do
  install -m 0644 "$file" /Users/dahai/Library/LaunchAgents/
done
```

确认脚本和目标路径后，使用 `make native-start` 可加载两个定时任务以及已有常驻服务。该命令不会立即执行备份/轮转任务，但会 kickstart 常驻服务；在线实例应在维护窗口执行。已经加载的 LaunchAgent 不会自动采用更新后的 `Umask`，需在维护窗口执行一次 `make native-stop && make native-start`。定时备份为每日 02:30，日志轮转为每日 03:15。

- 日志默认超过 50 MiB 才 copy-truncate、gzip，14 天后只清理匹配 `*.log.*.gz` 的归档。所有当前与归档日志会收紧为 `0600`。
- 原生包装器会在每次 API/Worker 启动前再次幂等补齐存储目录。部署新 nginx 模板后必须先 `nginx -t`，再在维护窗口 reload/restart；本轮只更新模板不会改变正在运行的 nginx。

## 支付上线

1. 在支付机构创建商户并设置回调到 `/api/v1/payments/{channel}/callback`。
2. 通过管理 API 创建 `signed_http` 渠道；配置会加密入库。
3. 验收成功、失败、重复回调、乱序回调、金额错误、超时、退款和对账。
4. 停用渠道后再次发送历史订单回调，确认系统仍可结算已在途资金；停用只阻止新建支付。
5. 验收支付回调先于创建响应、订单超时后到账和金额不一致到账：前者不能把终态 intent 回退，后两者必须创建可恢复的原路退款。
6. 用不存在的 intent 发送验签成功的成功回调，确认安全中心只生成一条未解决的 `payment.orphan_received` 高危事件，并进入人工对账。
7. 只有完整验收后的渠道才设置 `enabled=true`。

## 供货链上线

1. 将 `SUPPLIER_CALLBACK_URL` 设置为上游可访问的公网 HTTPS API 地址；与 `APP_URL` 相同时可留空继承。
2. 在运营后台创建供货商并保存只写不回显的 Key/Secret，随后执行手动同步。
3. 商品映射使用同步目录返回的商品或规格 `external_id`，分别验收价格、库存和停售场景。
4. 验收即时交付、异步回调、回调重放、回调丢失后的轮询恢复、上游超时和 Worker 重启恢复。
5. 在采购订单页确认回调接收/处理状态、轮询计划和最终采购成本；运营界面不会显示原始卡密或上游正文。

## 多语言通知上线

- 系统内置 27 类管理员事件和 13 类用户事件，覆盖账户、登录、订单、充值、OpenAPI、库存、供货、采购、风控与安全。每类事件均提供简体中文、繁体中文、英语、越南语、俄语、日语、韩语和泰语的完整主题、业务字段、处理建议与隐私说明。
- 管理员事件提供后台记录、Email、Telegram、企业微信四种渠道模板；用户事件提供账户中心站内信和 Email 模板。管理员与用户 audience 强制隔离，用户模板没有来源 IP、后台实体、安全内部信息等管理员字段。
- 用户端会把当前浏览器语言通过 `X-LinLinQi-Locale` 发送给 API。登录用户最近一次选择会写入 `users.preferred_locale`；注册、登录、订单、充值和恢复任务只选择该语言的用户规则。用户 Email 规则的收件人使用保留值 `event_user`，投递时从事件所属用户解析地址，禁止配置固定客户地址。
- 管理员规则在后台明确选择语言和收件人，不继承触发事件所属客户的语言。系统只默认启用简体中文后台记录规则，其他管理员语言及 Email/Telegram/企业微信规则必须在连接器验收后由运营人员显式启用，避免一次事件重复发送八种语言。
- 上线外部渠道前分别完成：错误凭证、超时、限流、重复事件、Worker 重启、失败重试及收件人脱敏验收。模板变量必须与事件目录一致；正文中不得放卡密、支付密钥、完整凭证或其他用户数据。

## 日常监控

- `/live` 只检查进程；`/ready` 检查 PostgreSQL、Redis 以及媒体存储的可写性与剩余空间。
- 使用 `METRICS_TOKEN` Bearer 鉴权抓取 `/metrics`，至少告警 HTTP 5xx、P95 延迟、队列积压、失败任务、支付成功率、库存低水位和 Webhook 连续失败。
- 观察订单 `pending_payment` 时长、库存 `locked` 数量和支付意图/订单状态差异。

## 备份与恢复

- `postgres_data` 和 `redis_data` 是持久卷，不是备份。PostgreSQL 使用持续归档/PITR，并把备份复制到故障域之外，每日验证备份可读取。
- `make backup-postgres` 使用容器内同版本 `pg_dump` 创建 custom archive，写入前用 `pg_restore --list` 验证，并生成权限为 `0600` 的 `.sha256` 与 `.metadata` 文件。默认目录 `backups/` 已从版本控制排除；备份完成后必须由部署系统加密并复制到异地不可变存储。
- macOS 原生部署使用 `scripts/backup-postgres-native.sh`，以 PostgreSQL 18 custom/zstd 格式写入 `~/.linlinqi/backups`，并在返回成功前验证归档目录和 SHA-256。该本机目录仍不属于异地备份，必须另行加密复制。
- 原生备份先写同目录临时文件，完成 `pg_restore --list` 与 SHA-256 后才原子发布 `.dump`、`.sha256` 和 `.metadata`；锁目录阻止重叠执行，并与原生恢复的破坏性阶段互斥。`com.linlinqi.backup` 每日执行，默认仅在新备份成功后删除超过 14 天的匹配归档。
- 恢复命令需要与归档文件名绑定的显式确认，例如：

  ```bash
  export LINLINQI_RESTORE_CONFIRM='restore:linlinqi-postgres-20260809T120000Z.dump'
  make restore-postgres BACKUP=/srv/linlinqi-backups/linlinqi-postgres-20260809T120000Z.dump
  ```

  脚本先校验 SHA-256 和归档目录，默认再写一份 `backups/pre-restore/` 安全备份，然后停止 API、Worker 与两个前端，重建 `linlinqi` 数据库、恢复归档、执行当前版本迁移并等待所有服务健康。任一步失败都会让应用保持停止，必须查明数据库状态后人工恢复。只有已用其他方式完成安全快照时，才可显式设置 `LINLINQI_SKIP_SAFETY_BACKUP=true`；只有完成独立校验时，才可设置 `LINLINQI_ALLOW_UNVERIFIED_BACKUP=true`。
- 原生恢复使用文件名绑定确认，并且不会调用 Docker：

  ```bash
  export LINLINQI_RESTORE_CONFIRM='restore:linlinqi-20260809T120000Z.dump'
  make restore-postgres-native BACKUP=/absolute/path/to/linlinqi-20260809T120000Z.dump
  ```

  它在破坏性操作前创建安全备份，只暂停已加载的 API、Worker 与 nginx，恢复后执行迁移并等待 API `/ready`。PostgreSQL 恢复成功后，脚本仅扫描并 `UNLINK` 当前 Redis DB 中 `asynq:{critical}:*`、`asynq:{default}:*`、`asynq:{low}:*` 三个 LinLinQi 队列命名空间，并从 `asynq:queues` 集合移除这三个成员；严禁使用 `FLUSHDB` 或 `FLUSHALL`，限流、缓存和其他应用/队列键不会被删除。Worker 启动后从恢复后的 PostgreSQL 状态重新排队到期业务。
- 原生恢复按 API、Worker、nginx 顺序逐级启动和验收。队列清理、迁移或任一健康检查失败时，退出陷阱会再次卸载这三个业务进程，使入口和后台处理保持关闭；查清 PostgreSQL、Redis 与日志状态后才可人工启动。Asynq 新增队列名时，必须在同一发布中把该队列加入恢复脚本的精确允许列表并完成恢复演练，不能扩大成通配删除所有 Redis 数据。
- PostgreSQL 逻辑备份只包含媒体元数据，不包含 `STORAGE_ROOT/media/objects`。对象是不可变、内容寻址文件：先完成数据库逻辑备份，再复制全部不可变对象并生成 SHA-256 清单，可保证备份时间点已有元数据引用的对象进入同一恢复批次，而稍后产生的对象至多成为可清理的额外文件。对象副本必须加密并存放在异地；镜像对象可按策略重建，但上传原件不得只保存在本机。
- 可用 `LINLINQI_ENV_FILE` 和 `LINLINQI_COMPOSE_FILE` 指向非默认部署文件；不要在这些参数中传递不受信任路径。
- Redis AOF 只用于队列恢复，不是交易事实来源；订单事实以 PostgreSQL 为准。
- Redis 使用 `noeviction`，通过 `REDIS_MAXMEMORY` 设置明确内存上限；容量告警必须早于上限触发，不能依靠淘汰队列任务维持可用性。
- 加密主密钥必须独立备份。丢失密钥会永久失去卡密、凭证和 TOTP 的解密能力。
- 每月至少执行一次隔离环境恢复演练并记录 RPO/RTO。

## 扩缩容

- API 无本地会话，可水平扩容；Worker 可增加副本，任务幂等由数据库唯一约束和行锁保障。
- 迁移通过 PostgreSQL transaction advisory lock 串行执行。
- 扩容前先校准数据库连接池，避免副本数乘以 60 超过 PostgreSQL 连接上限。

## 发布检查

- `make verify-toolchain` 确认 Go/Node/npm、Docker 基础镜像、CI 与前端锁文件版本一致。
- `go test ./...`、`go vet ./...`、两个前端生产构建全部通过。
- `docker compose --env-file .env -f docker-compose.yml config` 通过，所有容器健康，三个 HTTP 发布端口仍绑定环回地址。
- 数据迁移在生产快照副本演练通过。
- 从异地备份恢复到隔离环境，核对校验和、迁移账本、订单/库存/钱包抽样数据与登录流程。
- 变更支付/库存/钱包时必须包含并发与回滚测试。
- 镜像使用不可变 tag/digest，发布后验证探针、指标、日志与一笔小额真实交易。

## 供应商上线与故障处理

1. 按协议下拉填写该协议真实需要的凭证，明确 `price_currency`、`balance_currency` 与 `minor_unit`；美元上游不能配置为 CNY，也不能把 `$1` 填成 `1` 分。
2. 保存后执行后台“测试连接”。探针只读余额、分类与商品，不会创建订单；必须核对健康状态、币种和样本商品价格。不可用/参考协议禁止启用。
3. 在“接入货源”先同步快照，再预览分类映射、媒体本地化、同步字段、加价模式与 `$1 → CNY` 试算；小批量导入通过后才开启自动建类/建商品和定时同步。
4. 观察 `supplier_sync_runs`、`supplier_sync_changes`、媒体失败数、最后成功时间和供应商探针。连续失败先停用新采购，不删除历史映射/采购单；进行中的上游订单仍由原绑定轮询或回调收口。
5. 订单库存预占状态为 `reserved/consumed/released`。出现长时间 reserved 时先核对订单与采购状态；不得直接删行或手工改上游库存。使用受审计的订单失败/取消或采购恢复流程释放。
6. 调价前固定汇率来源和 basis points。抽样确认 `100 USD cents × 7.0267 × 1.5 = 1054 CNY cents`，同时检查订单响应的 `cost_currency/cost_minor_unit`。

供应商故障时允许的自动动作只有预授权且可回滚的：暂停新采购、停止发放未验证卡密、提高风险复核、隔离单个连接、创建告警/工单。改代码、部署、停站、数据库写入、抓取客户明文流量或渗透测试必须在隔离环境并经过人工审批。

## 小龙虾只读状态与受控处置

运行 `scripts/lobster-status.sh` 获取进程、数据库迁移、Redis、存储、备份、风险待审、最近安全事件与供应商同步异常的只读汇总。该脚本不写库、不改配置、不重启服务；需要改变状态的处置动作必须走 [docs/LOBSTER.md](LOBSTER.md) 中定义的预授权+TTL+审计流程或人工审批，禁止直接对生产库执行高危写入或关闭服务。
