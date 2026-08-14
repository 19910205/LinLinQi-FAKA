#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
export LC_ALL="en_US.UTF-8"
export LANG="en_US.UTF-8"

readonly runtime_root="/Users/dahai/.linlinqi"
readonly env_file="${runtime_root}/config/linlinqi.env"
readonly lock_directory="${runtime_root}/run/postgres-backup.lock"
readonly restore_lock_directory="${runtime_root}/run/postgres-restore.lock"
backup_directory="${1:-${runtime_root}/backups}"
backup_during_restore="${LINLINQI_BACKUP_DURING_RESTORE:-false}"

fail() {
  printf 'LinLinQi native PostgreSQL backup failed: %s\n' "$*" >&2
  exit 1
}

[[ -r "${env_file}" ]] || fail "runtime environment is missing or unreadable"
[[ "$(id -un)" == "dahai" ]] || fail "run this command as the dahai deployment user, without sudo"
[[ "${backup_directory}" == /* && "${backup_directory}" != "/" && "${backup_directory}" != "/Users/dahai" ]] \
  || fail "backup destination must be a dedicated absolute directory"

set -a
# shellcheck disable=SC1090
source "${env_file}"
set +a
: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}"

retention_days="${NATIVE_BACKUP_RETENTION_DAYS:-14}"
[[ "${retention_days}" =~ ^[0-9]+$ && "${retention_days}" -ge 1 && "${retention_days}" -le 3650 ]] \
  || fail "NATIVE_BACKUP_RETENTION_DAYS must be between 1 and 3650"

/usr/bin/install -d -m 0700 "${runtime_root}/run" "${backup_directory}"
if ! /bin/mkdir "${lock_directory}" 2>/dev/null; then
  fail "another native backup is already running"
fi

temporary_dump=""
temporary_checksum=""
temporary_metadata=""
cleanup() {
  cleanup_status=$?
  [[ -z "${temporary_dump}" ]] || /bin/rm -f "${temporary_dump}"
  [[ -z "${temporary_checksum}" ]] || /bin/rm -f "${temporary_checksum}"
  [[ -z "${temporary_metadata}" ]] || /bin/rm -f "${temporary_metadata}"
  /bin/rmdir "${lock_directory}" 2>/dev/null || true
  trap - EXIT INT TERM
  exit "${cleanup_status}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# A restore invokes one safety backup explicitly, but every independently
# scheduled or manual backup must stay out of the destructive restore window.
if [[ -d "${restore_lock_directory}" && "${backup_during_restore}" != "true" ]]; then
  fail "a native PostgreSQL restore is in progress"
fi

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
dump_path="${backup_directory}/linlinqi-${timestamp}.dump"
checksum_path="${dump_path}.sha256"
metadata_path="${dump_path}.metadata"
[[ ! -e "${dump_path}" && ! -e "${checksum_path}" && ! -e "${metadata_path}" ]] \
  || fail "a backup with timestamp ${timestamp} already exists"

temporary_dump="$(mktemp "${backup_directory}/.linlinqi-dump.XXXXXX")"
temporary_checksum="$(mktemp "${backup_directory}/.linlinqi-checksum.XXXXXX")"
temporary_metadata="$(mktemp "${backup_directory}/.linlinqi-metadata.XXXXXX")"

PGPASSWORD="${POSTGRES_PASSWORD}" /usr/local/opt/postgresql@18/bin/pg_dump \
  --host=127.0.0.1 \
  --port=5433 \
  --username=linlinqi \
  --dbname=linlinqi \
  --format=custom \
  --compress=zstd:9 \
  --no-owner \
  --no-privileges \
  --file="${temporary_dump}"

[[ -s "${temporary_dump}" ]] || fail "pg_dump produced an empty archive"
/usr/local/opt/postgresql@18/bin/pg_restore --list "${temporary_dump}" >/dev/null \
  || fail "pg_restore could not read the generated archive"

archive_hash="$(shasum -a 256 "${temporary_dump}" | awk '{print $1}')"
printf '%s  %s\n' "${archive_hash}" "$(basename "${dump_path}")" >"${temporary_checksum}"
{
  printf 'format=postgresql-custom\n'
  printf 'created_at=%s\n' "${timestamp}"
  printf 'database=linlinqi\n'
  printf 'sha256=%s\n' "${archive_hash}"
  printf 'pg_dump_version=%s\n' "$(/usr/local/opt/postgresql@18/bin/pg_dump --version)"
} >"${temporary_metadata}"

/bin/chmod 0600 "${temporary_dump}" "${temporary_checksum}" "${temporary_metadata}"
/bin/mv "${temporary_checksum}" "${checksum_path}"
temporary_checksum=""
/bin/mv "${temporary_metadata}" "${metadata_path}"
temporary_metadata=""
/bin/mv "${temporary_dump}" "${dump_path}"
temporary_dump=""

# Retention runs only after a newly verified archive has been published.
/usr/bin/find "${backup_directory}" -type f \
  \( -name 'linlinqi-*.dump' -o -name 'linlinqi-*.dump.sha256' -o -name 'linlinqi-*.dump.metadata' \) \
  -mtime "+${retention_days}" -exec /bin/rm -f {} \;

printf 'LinLinQi native PostgreSQL backup completed: %s\n' "${dump_path}"
