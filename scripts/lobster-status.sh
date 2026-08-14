#!/usr/bin/env bash
set -euo pipefail
umask 077

# LinLinQi "Lobster" read-only status summary.
#
# This is the default surface granted to an ops agent: it never writes to the
# database, never rotates credentials, never restarts services and never
# modifies code. Destructive or state-changing actions must go through the
# explicit approval workflow documented in docs/LOBSTER.md.

readonly runtime_root="${LINLINQI_RUNTIME_ROOT:-/Users/dahai/.linlinqi}"
readonly env_file="${runtime_root}/config/linlinqi.env"
readonly project_root="/Users/dahai/Documents/faka"

if [[ ! -r "${env_file}" ]]; then
  echo "LinLinQi environment file is missing: ${env_file}" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "${env_file}"
set +a

readonly database_url="${DATABASE_URL:-postgres://linlinqi:${POSTGRES_PASSWORD:-}@127.0.0.1:5433/linlinqi?sslmode=disable}"
readonly psql_bin="${PSQL_BIN:-$(command -v psql || true)}"
readonly redis_bin="${REDIS_CLI_BIN:-$(command -v redis-cli || true)}"

section() {
  printf '\n## %s\n' "$1"
}

printf '# LinLinQi Lobster read-only status\n'
printf 'collected_at: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
printf 'project: %s\n' "${project_root}"

section "Processes"
for name in api worker nginx postgres redis-server; do
  count="$(pgrep -f "${name}" 2>/dev/null | wc -l | tr -d ' ')"
  printf '%s: %s\n' "${name}" "$([[ "${count}" -gt 0 ]] && echo running || echo stopped)"
done

section "Database"
if [[ -n "${psql_bin}" ]]; then
  "${psql_bin}" "${database_url}" -At -c \
    "SELECT 'migrations', COUNT(*) FROM linlinqi_schema_migrations;" 2>/dev/null \
    || printf 'migrations: unreachable\n'
  "${psql_bin}" "${database_url}" -At -c \
    "SELECT 'risk_pending_review', COUNT(*) FROM risk_decisions WHERE reviewed_at IS NULL AND decision IN ('review','challenge','deny');" 2>/dev/null \
    || true
  "${psql_bin}" "${database_url}" -At -c \
    "SELECT 'security_events_24h', COUNT(*) FROM security_events WHERE created_at >= now() - interval '24 hours';" 2>/dev/null \
    || true
  "${psql_bin}" "${database_url}" -At -c \
    "SELECT 'supplier_sync_stale', COUNT(*) FROM suppliers WHERE status = 'active' AND (last_sync_at IS NULL OR last_sync_at <= now() - (sync_interval_minutes * interval '1 minute') * 2);" 2>/dev/null \
    || true
else
  printf 'psql: unavailable\n'
fi

section "Redis"
if [[ -n "${redis_bin}" ]]; then
  "${redis_bin}" -h 127.0.0.1 -a "${REDIS_PASSWORD:-}" --no-auth-warning ping 2>/dev/null \
    || printf 'redis: unreachable\n'
else
  printf 'redis-cli: unavailable\n'
fi

section "Storage"
storage_root="${STORAGE_ROOT:-${runtime_root}/storage}"
if [[ -d "${storage_root}" ]]; then
  du -sh "${storage_root}" 2>/dev/null | awk '{print "used:", $1}'
  df -h "$(dirname "${storage_root}")" 2>/dev/null | tail -1 | awk '{print "free:", $4, "(" $6 ")"}'
else
  printf 'storage: %s missing\n' "${storage_root}"
fi

section "Backups"
backup_root="${runtime_root}/backups"
if [[ -d "${backup_root}" ]]; then
  ls -1t "${backup_root}" 2>/dev/null | head -3 | sed 's/^/latest: /'
else
  printf 'backups: directory missing\n'
fi

section "Recent security events"
if [[ -n "${psql_bin}" ]]; then
  "${psql_bin}" "${database_url}" -P pager=off -c \
    "SELECT created_at, severity, event_type FROM security_events ORDER BY created_at DESC LIMIT 5;" 2>/dev/null \
    || true
fi

printf '\n# End of read-only status. State changes require the approval workflow in docs/LOBSTER.md.\n'
