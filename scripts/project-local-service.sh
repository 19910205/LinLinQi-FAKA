#!/usr/bin/env bash
set -euo pipefail
umask 077

readonly project_root="/Users/dahai/Documents/faka"
readonly state_root="${project_root}/.runtime"
readonly launcher="${project_root}/scripts/run-native-macos.sh"
readonly application_binary="${project_root}/api/linlinqi"

mkdir -p "${state_root}"
chmod 0700 "${state_root}"

pid_file() {
  printf '%s/%s.pid' "${state_root}" "$1"
}

log_file() {
  printf '%s/%s.log' "${state_root}" "$1"
}

read_pid() {
  local service="$1"
  local file
  file="$(pid_file "${service}")"
  if [[ -r "${file}" ]]; then
    local pid
    pid="$(<"${file}")"
    if [[ "${pid}" =~ ^[0-9]+$ ]] && \
      ps -p "${pid}" -o command= | grep -Fq "${application_binary} ${service}"; then
      printf '%s' "${pid}"
      return
    fi
  fi
  pgrep -f "^${application_binary} ${service}$" | head -n 1
}

is_running() {
  local service="$1"
  local pid
  pid="$(read_pid "${service}")" || return 1
  ps -p "${pid}" -o command= | grep -Fq "${project_root}/api/linlinqi ${service}"
}

start_one() {
  local service="$1"
  if is_running "${service}"; then
    printf '%s already running (pid %s)\n' "${service}" "$(read_pid "${service}")"
    return
  fi
  rm -f "$(pid_file "${service}")"
  cd "${project_root}"
  nohup "${launcher}" "${service}" >>"$(log_file "${service}")" 2>&1 </dev/null &
  local pid=$!
  printf '%s\n' "${pid}" >"$(pid_file "${service}")"
  chmod 0600 "$(pid_file "${service}")" "$(log_file "${service}")"
  for _ in {1..50}; do
    if is_running "${service}"; then
      printf '%s started (pid %s)\n' "${service}" "${pid}"
      return
    fi
    sleep 0.1
  done
  printf '%s failed to start; see %s\n' "${service}" "$(log_file "${service}")" >&2
  return 1
}

stop_one() {
  local service="$1"
  local pid
  if ! pid="$(read_pid "${service}")"; then
    printf '%s is not running\n' "${service}"
    return
  fi
  if ! is_running "${service}"; then
    rm -f "$(pid_file "${service}")"
    printf '%s stale pid removed\n' "${service}"
    return
  fi
  kill -TERM "${pid}"
  for _ in {1..100}; do
    if ! kill -0 "${pid}" 2>/dev/null; then
      rm -f "$(pid_file "${service}")"
      printf '%s stopped\n' "${service}"
      return
    fi
    sleep 0.1
  done
  printf '%s did not stop within 10 seconds\n' "${service}" >&2
  return 1
}

status_one() {
  local service="$1"
  if is_running "${service}"; then
    printf '%-8s running pid=%s\n' "${service}" "$(read_pid "${service}")"
  else
    printf '%-8s stopped\n' "${service}"
  fi
}

action="${1:-status}"
case "${action}" in
  start)
    start_one api
    start_one worker
    ;;
  stop)
    stop_one worker
    stop_one api
    ;;
  restart)
    stop_one worker
    stop_one api
    start_one api
    start_one worker
    ;;
  status)
    status_one api
    status_one worker
    ;;
  *)
    echo "usage: $0 {start|stop|restart|status}" >&2
    exit 64
    ;;
esac
