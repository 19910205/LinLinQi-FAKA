#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

readonly runtime_root="/Users/dahai/.linlinqi"
readonly env_file="${runtime_root}/config/linlinqi.env"
readonly log_directory="${runtime_root}/logs"
readonly lock_directory="${runtime_root}/run/log-rotation.lock"

if [[ "$(id -un)" != "dahai" ]]; then
  echo "run this command as the dahai deployment user, without sudo" >&2
  exit 1
fi

if [[ -r "${env_file}" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "${env_file}"
  set +a
fi

max_bytes="${NATIVE_LOG_MAX_BYTES:-52428800}"
retention_days="${NATIVE_LOG_RETENTION_DAYS:-14}"
if [[ ! "${max_bytes}" =~ ^[0-9]+$ || "${max_bytes}" -lt 1048576 ]]; then
  echo "NATIVE_LOG_MAX_BYTES must be at least 1048576" >&2
  exit 1
fi
if [[ ! "${retention_days}" =~ ^[0-9]+$ || "${retention_days}" -lt 1 || "${retention_days}" -gt 3650 ]]; then
  echo "NATIVE_LOG_RETENTION_DAYS must be between 1 and 3650" >&2
  exit 1
fi

/usr/bin/install -d -m 0700 "${runtime_root}/run" "${log_directory}"
if ! /bin/mkdir "${lock_directory}" 2>/dev/null; then
  echo "LinLinQi native log rotation is already running" >&2
  exit 1
fi

cleanup() {
  cleanup_status=$?
  /bin/rmdir "${lock_directory}" 2>/dev/null || true
  trap - EXIT INT TERM
  exit "${cleanup_status}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
rotated_count=0
for log_file in "${log_directory}"/*.log; do
  [[ -f "${log_file}" && ! -L "${log_file}" ]] || continue
  /bin/chmod 0600 "${log_file}"
  log_size="$(stat -f '%z' "${log_file}")"
  [[ "${log_size}" -ge "${max_bytes}" ]] || continue

  archive_path="${log_file}.${timestamp}"
  temporary_archive="${archive_path}.tmp"
  [[ ! -e "${archive_path}" && ! -e "${archive_path}.gz" && ! -e "${temporary_archive}" ]] || continue

  /bin/cp -p "${log_file}" "${temporary_archive}"
  /bin/chmod 0600 "${temporary_archive}"
  /bin/mv "${temporary_archive}" "${archive_path}"
  : >"${log_file}"
  /bin/chmod 0600 "${log_file}"
  /usr/bin/gzip -9 "${archive_path}"
  rotated_count=$((rotated_count + 1))
done

/usr/bin/find "${log_directory}" -type f -name '*.log.*.gz' \
  -mtime "+${retention_days}" -exec /bin/rm -f {} \;

printf 'LinLinQi native log rotation completed; rotated=%d\n' "${rotated_count}"
