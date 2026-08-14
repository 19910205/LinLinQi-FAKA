# LinLinQi security smoke report

- Target: `http://127.0.0.1:8081`
- Started: `2026-08-09T15:04:33Z`
- Requests: 95/180
- Concurrency: 8
- Summary: pass=20, fail=0, warn=0, skip=1

| Category | Check | Status | Severity | HTTP | Requests | Duration | Note |
|---|---|---:|---:|---:|---:|---:|---|
| auth | refresh_token_replay | pass |  | 401 | 1 | 7 ms |  |
| auth | refresh_token_rotation | pass |  | 0 | 1 | 2 ms |  |
| auth | temporary_user_registration | pass |  | 201 | 1 | 68 ms |  |
| auth | wrong_password_rejected | pass |  | 401 | 1 | 62 ms |  |
| authz | idor_ticket | pass |  | 404 | 1 | 3 ms |  |
| authz | jwt_tamper | pass |  | 401 | 1 | 0 ms |  |
| authz | order_lookup_token_enforced | pass |  | 404 | 1 | 2 ms |  |
| availability | health | pass |  | 200 | 1 | 23 ms |  |
| catalog | catalog_discovery | pass |  | 200 | 1 | 26 ms |  |
| catalog | product_detail_discovery | pass |  | 200 | 1 | 6 ms |  |
| financial | inventory_race | skip |  | 0 | 0 | 0 ms | disabled |
| financial | order_replay | pass |  | 0 | 2 | 37 ms | created=2 unique_orders=1 |
| financial | recharge_idempotency | pass |  | 0 | 2 | 7 ms | unique recharge records observed=1 |
| financial | recharge_idempotency_conflict | pass |  | 409 | 1 | 1 ms |  |
| injection | catalog_sql_xss_payload | pass |  | 200 | 1 | 1 ms |  |
| injection | quote_identifier_injection | pass |  | 422 | 1 | 23 ms |  |
| load | quote_concurrency | pass |  | 0 | 60 | 66 ms | bounded concurrency=8 throttled=0 |
| payment | forged_payment_callback | pass |  | 401 | 1 | 0 ms |  |
| payment | payment_channel_discovery | pass |  | 200 | 1 | 2 ms |  |
| rate_limit | login_rate_limit | pass |  | 0 | 13 | 659 ms | throttled=2 |
| ssrf | webhook_ssrf | pass |  | 0 | 3 | 2 ms |  |

Secrets, access tokens, passwords, callback payload bodies, card contents, and idempotency keys are intentionally omitted.
