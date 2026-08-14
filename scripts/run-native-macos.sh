#!/usr/bin/env bash
set -euo pipefail
umask 077

readonly runtime_root="/Users/dahai/.linlinqi"
readonly project_root="/Users/dahai/Documents/faka"
readonly env_file="${runtime_root}/config/linlinqi.env"
readonly application_binary="${project_root}/api/linlinqi"

if [[ "$(id -un)" != "dahai" ]]; then
  echo "Warning: LinLinQi native services are normally run as the dahai deployment user (current: $(id -un))" >&2
fi

if [[ ! -r "${env_file}" ]]; then
  echo "LinLinQi environment file is missing or unreadable: ${env_file}" >&2
  exit 1
fi

if [[ ! -x "${application_binary}" ]]; then
  echo "LinLinQi application binary is missing or not executable: ${application_binary}" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "${env_file}"
set +a

: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}"
: "${REDIS_PASSWORD:?REDIS_PASSWORD is required}"

if [[ ! "${POSTGRES_PASSWORD}" =~ ^[A-Za-z0-9._~-]+$ ]]; then
  echo "POSTGRES_PASSWORD contains URL-reserved characters; rotate it to a URL-safe random value" >&2
  exit 1
fi

export APP_BIND_ADDRESS="127.0.0.1"
export APP_PORT="8080"
export DATABASE_URL="postgres://linlinqi:${POSTGRES_PASSWORD}@127.0.0.1:5433/linlinqi?sslmode=disable"
export REDIS_ADDR="127.0.0.1:6379"
export SEED_DATA="false"
export STORAGE_ROOT="${STORAGE_ROOT:-${runtime_root}/storage}"
export MEDIA_PUBLIC_BASE_URL="${MEDIA_PUBLIC_BASE_URL:-http://127.0.0.1:8080/media}"
export MEDIA_MAX_IMAGE_BYTES="${MEDIA_MAX_IMAGE_BYTES:-20971520}"
export MEDIA_STORAGE_MAX_BYTES="${MEDIA_STORAGE_MAX_BYTES:-214748364800}"
export MEDIA_MIN_FREE_BYTES="${MEDIA_MIN_FREE_BYTES:-107374182400}"

if [[ "${STORAGE_ROOT}" != /* || "${STORAGE_ROOT}" == "/" || "${STORAGE_ROOT}" == *"/../"* || "${STORAGE_ROOT}" == *"/.." ]]; then
  echo "STORAGE_ROOT must be a safe absolute directory" >&2
  exit 1
fi

for storage_directory in \
  "${STORAGE_ROOT}" \
  "${STORAGE_ROOT}/media" \
  "${STORAGE_ROOT}/media/objects" \
  "${STORAGE_ROOT}/media/objects/sha256" \
  "${STORAGE_ROOT}/media/staging" \
  "${STORAGE_ROOT}/media/quarantine" \
  "${STORAGE_ROOT}/mirror" \
  "${STORAGE_ROOT}/mirror/objects" \
  "${STORAGE_ROOT}/spool" \
  "${STORAGE_ROOT}/spool/protocol-sync" \
  "${STORAGE_ROOT}/tmp"; do
  /usr/bin/install -d -m 0700 "${storage_directory}"
done

mode="${1:-}"
case "${mode}" in
  api | worker)
    export BOOTSTRAP_ADMIN="false"
    export BOOTSTRAP_ADMIN_PASSWORD=""
    exec "${application_binary}" "${mode}"
    ;;
  migrate)
    export BOOTSTRAP_ADMIN="false"
    export BOOTSTRAP_ADMIN_PASSWORD=""
    exec "${application_binary}" migrate
    ;;
  bootstrap)
    export BOOTSTRAP_ADMIN="true"
    : "${BOOTSTRAP_ADMIN_PASSWORD:?BOOTSTRAP_ADMIN_PASSWORD is required for bootstrap}"
    exec "${application_binary}" migrate
    ;;
  *)
    echo "usage: $0 {api|worker|migrate|bootstrap}" >&2
    exit 64
    ;;
esac
