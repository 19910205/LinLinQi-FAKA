# LinLinQi 本机安全与压力冒烟工具

此命令只接受 `127.0.0.1`、`localhost` 或 `[::1]`。HTTP 重定向和实际拨号地址也会再次校验，任何非回环地址都会被硬拒绝。全局请求数最多 500，并发最多 16；默认不创建用户、充值或订单。

先编译：

```bash
cd api
go build -o ./security-smoke ./cmd/security-smoke
```

默认只读/拒绝路径检查：

```bash
./security-smoke \
  --base-url http://127.0.0.1:8081 \
  --concurrency 4 \
  --max-requests 100 \
  --quote-requests 20
```

认证检查不接受命令行密码，需通过进程环境传入；报告不会包含账号、密码、JWT、刷新令牌、卡密、回调正文或幂等键：

```bash
LINLINQI_SMOKE_EMAIL='专用测试账号' \
LINLINQI_SMOKE_PASSWORD='专用测试密码' \
./security-smoke --rate-limit-requests 14
```

也可以用 `--allow-register` 创建一个随机的 `example.invalid` 临时测试账号。该账号不会自动删除，便于审计数据库中的登录与安全事件。

充值、重复下单和库存竞争会改变本机数据库，必须显式启用：

```bash
./security-smoke \
  --allow-register \
  --allow-financial \
  --order-replays 2 \
  --inventory-race \
  --max-requests 120
```

`--inventory-race` 仅在自动发现的本地库存为 1–20 时发出两个并发的“全库存”订单；其余情况自动跳过。`--order-replays` 最大为 3。默认报告写入项目内的 `api/var/security-smoke/`，同时生成权限为 `0600` 的 JSON 和 Markdown 文件。发现失败时进程返回状态码 1，配置或写报告失败返回 2。

建议每次运行前备份 PostgreSQL，并只使用专门的测试商品、测试库存和 sandbox 支付渠道。登录限流测试放在最后执行，以免影响同一来源地址下的前置认证场景。
