#!/usr/bin/env bash

set -Eeuo pipefail
umask 077

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd -- "$script_dir/.." && pwd -P)"
env_file="${LINLINQI_ENV_FILE:-$repo_root/.env}"
compose_file="${LINLINQI_COMPOSE_FILE:-$repo_root/docker-compose.yml}"
backup_dir="${1:-$repo_root/backups}"

fail() {
  printf 'LinLinQi PostgreSQL backup failed: %s\n' "$*" >&2
  exit 1
}

command -v docker >/dev/null 2>&1 || fail "docker is not installed"
[[ -r "$env_file" ]] || fail "environment file is not readable: $env_file"
[[ -r "$compose_file" ]] || fail "Compose file is not readable: $compose_file"

mkdir -p -- "$backup_dir"
backup_dir="$(cd -- "$backup_dir" && pwd -P)"

compose=(docker compose --env-file "$env_file" -f "$compose_file")
"${compose[@]}" config --quiet
"${compose[@]}" exec -T postgres pg_isready -U linlinqi -d linlinqi >/dev/null \
  || fail "PostgreSQL service is not ready"

timestamp="$(date -u '+%Y%m%dT%H%M%SZ')"
archive="$backup_dir/linlinqi-postgres-$timestamp.dump"
checksum="$archive.sha256"
metadata="$archive.metadata"
[[ ! -e "$archive" && ! -e "$checksum" && ! -e "$metadata" ]] \
  || fail "backup with timestamp $timestamp already exists"

temporary_archive="$(mktemp "$backup_dir/.linlinqi-postgres.XXXXXX")"
temporary_checksum="$(mktemp "$backup_dir/.linlinqi-postgres-checksum.XXXXXX")"
temporary_metadata="$(mktemp "$backup_dir/.linlinqi-postgres-metadata.XXXXXX")"
cleanup() {
  rm -f -- "$temporary_archive" "$temporary_checksum" "$temporary_metadata"
}
trap cleanup EXIT

"${compose[@]}" exec -T postgres sh -ceu '
  export PGPASSWORD="$POSTGRES_PASSWORD"
  exec pg_dump \
    --username="$POSTGRES_USER" \
    --dbname="$POSTGRES_DB" \
    --format=custom \
    --compress=zstd:9 \
    --no-owner \
    --no-privileges
' >"$temporary_archive"

[[ -s "$temporary_archive" ]] || fail "pg_dump produced an empty archive"
"${compose[@]}" exec -T postgres pg_restore --list <"$temporary_archive" >/dev/null \
  || fail "pg_restore could not read the generated archive"

if command -v sha256sum >/dev/null 2>&1; then
  archive_hash="$(sha256sum "$temporary_archive" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  archive_hash="$(shasum -a 256 "$temporary_archive" | awk '{print $1}')"
else
  fail "sha256sum or shasum is required"
fi

dump_version="$("${compose[@]}" exec -T postgres pg_dump --version | tr -d '\r')"
printf '%s  %s\n' "$archive_hash" "$(basename -- "$archive")" >"$temporary_checksum"
{
  printf 'format=postgresql-custom\n'
  printf 'created_at=%s\n' "$timestamp"
  printf 'database=linlinqi\n'
  printf 'sha256=%s\n' "$archive_hash"
  printf 'pg_dump_version=%s\n' "$dump_version"
} >"$temporary_metadata"

chmod 600 "$temporary_archive" "$temporary_checksum" "$temporary_metadata"
mv -- "$temporary_checksum" "$checksum"
mv -- "$temporary_metadata" "$metadata"
mv -- "$temporary_archive" "$archive"
trap - EXIT

printf 'PostgreSQL backup verified and written to:\n%s\n%s\n%s\n' \
  "$archive" "$checksum" "$metadata"
