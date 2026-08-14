#!/usr/bin/env bash

set -Eeuo pipefail
umask 077

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd -- "$script_dir/.." && pwd -P)"
env_file="${LINLINQI_ENV_FILE:-$repo_root/.env}"
compose_file="${LINLINQI_COMPOSE_FILE:-$repo_root/docker-compose.yml}"
backup_input="${1:-}"

fail() {
  printf 'LinLinQi PostgreSQL restore failed: %s\n' "$*" >&2
  exit 1
}

[[ -n "$backup_input" ]] || fail "usage: $0 /absolute/path/to/linlinqi-postgres-*.dump"
[[ -r "$backup_input" && -f "$backup_input" && -s "$backup_input" ]] \
  || fail "backup archive is not a readable non-empty file: $backup_input"
backup_dir="$(cd -- "$(dirname -- "$backup_input")" && pwd -P)"
backup_file="$backup_dir/$(basename -- "$backup_input")"
backup_name="$(basename -- "$backup_file")"

required_confirmation="restore:$backup_name"
[[ "${LINLINQI_RESTORE_CONFIRM:-}" == "$required_confirmation" ]] || fail \
  "set LINLINQI_RESTORE_CONFIRM=$required_confirmation to confirm destructive replacement"

command -v docker >/dev/null 2>&1 || fail "docker is not installed"
[[ -r "$env_file" ]] || fail "environment file is not readable: $env_file"
[[ -r "$compose_file" ]] || fail "Compose file is not readable: $compose_file"

if command -v sha256sum >/dev/null 2>&1; then
  actual_hash="$(sha256sum "$backup_file" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual_hash="$(shasum -a 256 "$backup_file" | awk '{print $1}')"
else
  fail "sha256sum or shasum is required"
fi

checksum_file="$backup_file.sha256"
if [[ -r "$checksum_file" ]]; then
  expected_hash="$(awk 'NR == 1 { print $1 }' "$checksum_file")"
  [[ "$expected_hash" =~ ^[[:xdigit:]]{64}$ ]] || fail "checksum file is malformed: $checksum_file"
  normalized_actual_hash="$(printf '%s' "$actual_hash" | tr '[:upper:]' '[:lower:]')"
  normalized_expected_hash="$(printf '%s' "$expected_hash" | tr '[:upper:]' '[:lower:]')"
  [[ "$normalized_actual_hash" == "$normalized_expected_hash" ]] || fail "backup checksum does not match"
elif [[ "${LINLINQI_ALLOW_UNVERIFIED_BACKUP:-false}" != "true" ]]; then
  fail "checksum file is missing: $checksum_file (set LINLINQI_ALLOW_UNVERIFIED_BACKUP=true only after independent verification)"
fi

compose=(docker compose --env-file "$env_file" -f "$compose_file")
"${compose[@]}" config --quiet
"${compose[@]}" up -d --wait --wait-timeout 120 postgres redis
"${compose[@]}" exec -T postgres pg_restore --list <"$backup_file" >/dev/null \
  || fail "pg_restore could not read the backup archive"

if [[ "${LINLINQI_SKIP_SAFETY_BACKUP:-false}" != "true" ]]; then
  safety_dir="${LINLINQI_SAFETY_BACKUP_DIR:-$repo_root/backups/pre-restore}"
  LINLINQI_ENV_FILE="$env_file" LINLINQI_COMPOSE_FILE="$compose_file" \
    "$script_dir/backup-postgres.sh" "$safety_dir"
fi

restore_succeeded=false
report_incomplete_restore() {
  if [[ "$restore_succeeded" != "true" ]]; then
    printf '%s\n' \
      'Restore did not complete. Application services remain stopped; inspect PostgreSQL before restarting them.' >&2
  fi
}
trap report_incomplete_restore EXIT

printf 'Stopping LinLinQi application services for database replacement...\n'
"${compose[@]}" stop -t 60 api worker user admin

"${compose[@]}" exec -T postgres sh -ceu '
  export PGPASSWORD="$POSTGRES_PASSWORD"
  dropdb \
    --force \
    --if-exists \
    --maintenance-db=postgres \
    --username="$POSTGRES_USER" \
    "$POSTGRES_DB"
  createdb \
    --maintenance-db=postgres \
    --username="$POSTGRES_USER" \
    --owner="$POSTGRES_USER" \
    --template=template0 \
    "$POSTGRES_DB"
'

"${compose[@]}" exec -T postgres sh -ceu '
  export PGPASSWORD="$POSTGRES_PASSWORD"
  exec pg_restore \
    --exit-on-error \
    --no-owner \
    --no-privileges \
    --username="$POSTGRES_USER" \
    --dbname="$POSTGRES_DB"
' <"$backup_file"

# Re-run the idempotent migration command so an older valid archive is upgraded
# before any request-serving process can reconnect.
"${compose[@]}" run --rm --no-deps migrate
"${compose[@]}" up -d --wait --wait-timeout 180 api worker user admin

restore_succeeded=true
trap - EXIT
printf 'PostgreSQL restore completed and all LinLinQi services are healthy.\n'
