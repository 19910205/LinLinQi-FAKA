#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

readonly runtime_root="/Users/dahai/.linlinqi"
readonly env_file="${runtime_root}/config/linlinqi.env"

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

storage_root="${STORAGE_ROOT:-${runtime_root}/storage}"
if [[ "${storage_root}" != /* || "${storage_root}" == "/" || "${storage_root}" == *"/../"* || "${storage_root}" == *"/.." ]]; then
  echo "STORAGE_ROOT must be a safe absolute directory" >&2
  exit 1
fi

for storage_directory in \
  "${storage_root}" \
  "${storage_root}/media" \
  "${storage_root}/media/objects" \
  "${storage_root}/media/objects/sha256" \
  "${storage_root}/media/staging" \
  "${storage_root}/media/quarantine" \
  "${storage_root}/mirror" \
  "${storage_root}/mirror/objects" \
  "${storage_root}/spool" \
  "${storage_root}/spool/protocol-sync" \
  "${storage_root}/tmp"; do
  /usr/bin/install -d -m 0700 "${storage_directory}"
done

printf 'LinLinQi native storage is prepared at %s\n' "${storage_root}"
