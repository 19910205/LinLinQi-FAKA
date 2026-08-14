#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
export LC_ALL="en_US.UTF-8"
export LANG="en_US.UTF-8"

readonly runtime_root="/Users/dahai/.linlinqi"
readonly env_file="${runtime_root}/config/linlinqi.env"
readonly launch_agents="/Users/dahai/Library/LaunchAgents"
readonly launch_domain="gui/$(id -u)"
readonly lock_directory="${runtime_root}/run/postgres-restore.lock"
readonly backup_lock_directory="${runtime_root}/run/postgres-backup.lock"
readonly redis_cli="/usr/local/opt/redis/bin/redis-cli"
backup_input="${1:-}"

fail() {
  printf 'LinLinQi native PostgreSQL restore failed: %s\n' "$*" >&2
  exit 1
}

[[ -n "${backup_input}" ]] || fail "usage: $0 /absolute/path/to/linlinqi-*.dump"
[[ "$(id -un)" == "dahai" ]] || fail "run this command as the dahai deployment user, without sudo"
[[ -r "${backup_input}" && -f "${backup_input}" && -s "${backup_input}" ]] \
  || fail "backup archive is not a readable non-empty file"
backup_directory="$(cd "$(dirname "${backup_input}")" && pwd -P)"
backup_file="${backup_directory}/$(basename "${backup_input}")"
backup_name="$(basename "${backup_file}")"

required_confirmation="restore:${backup_name}"
[[ "${LINLINQI_RESTORE_CONFIRM:-}" == "${required_confirmation}" ]] \
  || fail "set LINLINQI_RESTORE_CONFIRM=${required_confirmation} to confirm destructive replacement"
[[ -r "${env_file}" ]] || fail "runtime environment is missing or unreadable"

set -a
# shellcheck disable=SC1090
source "${env_file}"
set +a
: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}"
: "${REDIS_PASSWORD:?REDIS_PASSWORD is required}"

redis_db="${REDIS_DB:-0}"
[[ "${redis_db}" =~ ^[0-9]+$ && "${redis_db}" -le 15 ]] \
  || fail "REDIS_DB must be between 0 and 15 for the native Redis instance"
[[ -x "${redis_cli}" ]] || fail "native redis-cli is missing or not executable"

maintenance_socket="${runtime_root}/run/postgres"
maintenance_user="$(id -un)"
maintenance_capable="$(/usr/local/opt/postgresql@18/bin/psql \
  --host="${maintenance_socket}" --port=5433 --username="${maintenance_user}" --dbname=postgres \
  --no-psqlrc --tuples-only --no-align \
  --command "SELECT rolsuper AND rolcreatedb FROM pg_roles WHERE rolname = current_user" 2>/dev/null || true)"
[[ "${maintenance_capable}" == "t" ]] \
  || fail "the native maintenance role must be able to create databases before restore starts"
application_role_exists="$(/usr/local/opt/postgresql@18/bin/psql \
  --host="${maintenance_socket}" --port=5433 --username="${maintenance_user}" --dbname=postgres \
  --no-psqlrc --tuples-only --no-align \
  --command "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'linlinqi')" 2>/dev/null || true)"
[[ "${application_role_exists}" == "t" ]] || fail "the linlinqi PostgreSQL role does not exist"

actual_hash="$(shasum -a 256 "${backup_file}" | awk '{print $1}')"
checksum_file="${backup_file}.sha256"
if [[ -r "${checksum_file}" ]]; then
  expected_hash="$(awk 'NR == 1 { print $1 }' "${checksum_file}")"
  [[ "${expected_hash}" =~ ^[[:xdigit:]]{64}$ ]] || fail "checksum file is malformed"
  normalized_actual_hash="$(printf '%s' "${actual_hash}" | tr '[:upper:]' '[:lower:]')"
  normalized_expected_hash="$(printf '%s' "${expected_hash}" | tr '[:upper:]' '[:lower:]')"
  [[ "${normalized_actual_hash}" == "${normalized_expected_hash}" ]] || fail "backup checksum does not match"
elif [[ "${LINLINQI_ALLOW_UNVERIFIED_BACKUP:-false}" != "true" ]]; then
  fail "checksum file is missing; independently verify it before setting LINLINQI_ALLOW_UNVERIFIED_BACKUP=true"
fi

/usr/local/opt/postgresql@18/bin/pg_restore --list "${backup_file}" >/dev/null \
  || fail "pg_restore could not read the backup archive"
PGPASSWORD="${POSTGRES_PASSWORD}" /usr/local/opt/postgresql@18/bin/pg_isready \
  --host=127.0.0.1 --port=5433 --username=linlinqi --dbname=linlinqi >/dev/null \
  || fail "native PostgreSQL is not ready"
redis_ping="$(REDISCLI_AUTH="${REDIS_PASSWORD}" "${redis_cli}" \
  --no-auth-warning --raw -e -h 127.0.0.1 -p 6379 -n "${redis_db}" PING 2>/dev/null || true)"
[[ "${redis_ping}" == "PONG" ]] || fail "native Redis is not ready or authentication failed"

/usr/bin/install -d -m 0700 "${runtime_root}/run"
if ! /bin/mkdir "${lock_directory}" 2>/dev/null; then
  fail "another native restore is already running"
fi

restore_succeeded=false
applications_stopped=false
backup_guard_held=false
queue_key_file=""

keep_applications_stopped() {
  local application_service
  for application_service in nginx worker api; do
    launchctl bootout "${launch_domain}/com.linlinqi.${application_service}" >/dev/null 2>&1 \
      || launchctl bootout "${launch_domain}" "${launch_agents}/com.linlinqi.${application_service}.plist" >/dev/null 2>&1 \
      || true
  done
  for application_service in nginx worker api; do
    if launchctl print "${launch_domain}/com.linlinqi.${application_service}" >/dev/null 2>&1; then
      printf 'CRITICAL: failed to unload com.linlinqi.%s; manual intervention is required\n' \
        "${application_service}" >&2
    fi
  done
}

cleanup() {
  cleanup_status=$?
  if [[ "${restore_succeeded}" != "true" && "${applications_stopped}" == "true" ]]; then
    keep_applications_stopped
  fi
  [[ -z "${queue_key_file}" ]] || /bin/rm -f "${queue_key_file}"
  if [[ "${backup_guard_held}" == "true" ]]; then
    /bin/rmdir "${backup_lock_directory}" 2>/dev/null || true
  fi
  /bin/rmdir "${lock_directory}" 2>/dev/null || true
  if [[ "${applications_stopped}" == "true" && "${restore_succeeded}" != "true" ]]; then
    printf '%s\n' \
      'Restore did not complete. API, Worker, and nginx were forced off; inspect PostgreSQL and Redis before restarting.' >&2
  fi
  trap - EXIT INT TERM
  exit "${cleanup_status}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Asynq 0.26 stores every task for a queue below asynq:{<queue>}:*. LinLinQi
# owns exactly these three queues. Removing only their namespaced keys avoids
# replaying post-snapshot work while preserving rate-limit, session, cache and
# any unrelated Redis/Asynq keys in the same logical database.
reset_linlinqi_asynq_queues() {
  local queue key unlink_result membership_result
  local removed=0
  queue_key_file="$(mktemp "${runtime_root}/run/.asynq-restore-keys.XXXXXX")"

  for queue in critical default low; do
    REDISCLI_AUTH="${REDIS_PASSWORD}" "${redis_cli}" \
      --no-auth-warning --raw -e -h 127.0.0.1 -p 6379 -n "${redis_db}" \
      --scan --pattern "asynq:{${queue}}:*" --count 500 >>"${queue_key_file}" \
      || fail "could not enumerate the ${queue} Asynq queue"
  done

  while IFS= read -r key; do
    [[ -n "${key}" ]] || continue
    case "${key}" in
      asynq:\{critical\}:* | asynq:\{default\}:* | asynq:\{low\}:*) ;;
      *) fail "Redis queue scan returned a key outside the approved LinLinQi namespaces" ;;
    esac
    unlink_result="$(REDISCLI_AUTH="${REDIS_PASSWORD}" "${redis_cli}" \
      --no-auth-warning --raw -e -h 127.0.0.1 -p 6379 -n "${redis_db}" \
      UNLINK "${key}")" || fail "could not remove a LinLinQi Asynq queue key"
    [[ "${unlink_result}" == "0" || "${unlink_result}" == "1" ]] \
      || fail "Redis returned an unexpected queue-key removal result"
    removed=$((removed + unlink_result))
  done <"${queue_key_file}"

  for queue in critical default low; do
    membership_result="$(REDISCLI_AUTH="${REDIS_PASSWORD}" "${redis_cli}" \
      --no-auth-warning --raw -e -h 127.0.0.1 -p 6379 -n "${redis_db}" \
      SREM asynq:queues "${queue}")" \
      || fail "could not remove the ${queue} queue registry member"
    [[ "${membership_result}" == "0" || "${membership_result}" == "1" ]] \
      || fail "Redis returned an unexpected queue-registry removal result"
  done

  : >"${queue_key_file}"
  for queue in critical default low; do
    REDISCLI_AUTH="${REDIS_PASSWORD}" "${redis_cli}" \
      --no-auth-warning --raw -e -h 127.0.0.1 -p 6379 -n "${redis_db}" \
      --scan --pattern "asynq:{${queue}}:*" --count 500 >>"${queue_key_file}" \
      || fail "could not verify the ${queue} Asynq queue reset"
  done
  [[ ! -s "${queue_key_file}" ]] \
    || fail "LinLinQi Asynq queue keys remain after the targeted reset"
  /bin/rm -f "${queue_key_file}"
  queue_key_file=""
  printf 'LinLinQi Asynq restore reset completed: redis_db=%s queues=critical,default,low keys_removed=%d\n' \
    "${redis_db}" "${removed}"
}

if [[ "${LINLINQI_SKIP_SAFETY_BACKUP:-false}" != "true" ]]; then
  LINLINQI_BACKUP_DURING_RESTORE=true \
    "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/backup-postgres-native.sh" \
    "${LINLINQI_SAFETY_BACKUP_DIR:-${runtime_root}/backups/pre-restore}"
fi

# Hold the backup lock across the destructive phase. Backups that started
# before the restore lock was acquired make the restore fail closed here;
# backups starting later reject the restore lock in the backup script.
for _ in {1..5}; do
  if /bin/mkdir "${backup_lock_directory}" 2>/dev/null; then
    backup_guard_held=true
    break
  fi
  sleep 1
done
[[ "${backup_guard_held}" == "true" ]] \
  || fail "could not reserve the PostgreSQL backup lock for restore"

api_was_loaded=false
worker_was_loaded=false
nginx_was_loaded=false
for application_service in nginx worker api; do
  if launchctl print "${launch_domain}/com.linlinqi.${application_service}" >/dev/null 2>&1; then
    case "${application_service}" in
      api) api_was_loaded=true ;;
      worker) worker_was_loaded=true ;;
      nginx) nginx_was_loaded=true ;;
    esac
    applications_stopped=true
    launchctl bootout "${launch_domain}" "${launch_agents}/com.linlinqi.${application_service}.plist"
  fi
done

/usr/local/opt/postgresql@18/bin/dropdb \
  --force \
  --if-exists \
  --maintenance-db=postgres \
  --host="${maintenance_socket}" \
  --port=5433 \
  --username="${maintenance_user}" \
  linlinqi

/usr/local/opt/postgresql@18/bin/createdb \
  --maintenance-db=postgres \
  --host="${maintenance_socket}" \
  --port=5433 \
  --username="${maintenance_user}" \
  --owner=linlinqi \
  --template=template0 \
  linlinqi

PGPASSWORD="${POSTGRES_PASSWORD}" /usr/local/opt/postgresql@18/bin/pg_restore \
  --exit-on-error \
  --no-owner \
  --no-privileges \
  --host=127.0.0.1 \
  --port=5433 \
  --username=linlinqi \
  --dbname=linlinqi \
  "${backup_file}"

"/Users/dahai/Documents/faka/scripts/run-native-macos.sh" migrate

# PostgreSQL was restored to an earlier truth snapshot. Queue tasks created
# after that snapshot must not be replayed against it. The Worker reconstructs
# due domain work from PostgreSQL after startup.
reset_linlinqi_asynq_queues

if [[ "${api_was_loaded}" == "true" ]]; then
  launchctl bootstrap "${launch_domain}" "${launch_agents}/com.linlinqi.api.plist"
  launchctl kickstart "${launch_domain}/com.linlinqi.api"
  api_healthy=false
  for _ in {1..60}; do
    if curl --fail --silent --show-error --max-time 2 http://127.0.0.1:8081/ready >/dev/null 2>&1; then
      api_healthy=true
      break
    fi
    sleep 2
  done
  [[ "${api_healthy}" == "true" ]] || fail "API readiness did not recover after restore"
fi

if [[ "${worker_was_loaded}" == "true" ]]; then
  launchctl bootstrap "${launch_domain}" "${launch_agents}/com.linlinqi.worker.plist"
  launchctl kickstart "${launch_domain}/com.linlinqi.worker"
  worker_healthy=false
  for _ in {1..60}; do
    worker_description="$(launchctl print "${launch_domain}/com.linlinqi.worker" 2>/dev/null || true)"
    worker_state="$(awk -F'= ' '/^[[:space:]]*state =/{print $2; exit}' <<<"${worker_description}")"
    if [[ "${worker_state}" == "running" ]]; then
      worker_healthy=true
      break
    fi
    sleep 2
  done
  [[ "${worker_healthy}" == "true" ]] || fail "Worker did not return to running state after restore"
fi

if [[ "${nginx_was_loaded}" == "true" ]]; then
  launchctl bootstrap "${launch_domain}" "${launch_agents}/com.linlinqi.nginx.plist"
  launchctl kickstart "${launch_domain}/com.linlinqi.nginx"
  for nginx_health_url in http://127.0.0.1:8080/healthz http://127.0.0.1:8082/healthz; do
    nginx_healthy=false
    for _ in {1..30}; do
      if curl --fail --silent --show-error --max-time 2 "${nginx_health_url}" >/dev/null 2>&1; then
        nginx_healthy=true
        break
      fi
      sleep 1
    done
    [[ "${nginx_healthy}" == "true" ]] || fail "nginx health did not recover after restore"
  done
fi

restore_succeeded=true
applications_stopped=false
printf 'LinLinQi native PostgreSQL restore completed from %s\n' "${backup_file}"
