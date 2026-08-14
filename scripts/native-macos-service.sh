#!/usr/bin/env bash
set -euo pipefail

readonly launch_agents="/Users/dahai/Library/LaunchAgents"
readonly domain="gui/$(id -u)"
readonly services=(postgresql redis api worker nginx)
readonly maintenance_services=(backup logrotate)
readonly all_services=("${services[@]}" "${maintenance_services[@]}")
action="${1:-status}"

service_target() {
  printf '%s/com.linlinqi.%s.plist' "${launch_agents}" "$1"
}

case "${action}" in
  start)
    for service in "${all_services[@]}"; do
      target="$(service_target "${service}")"
      if [[ ! -r "${target}" ]]; then
        case "${service}" in
          backup | logrotate)
            printf 'maintenance service is not installed: %s\n' "${target}" >&2
            continue
            ;;
          *)
            printf 'service definition is not readable: %s\n' "${target}" >&2
            exit 1
            ;;
        esac
      fi
      if ! launchctl print "${domain}/com.linlinqi.${service}" >/dev/null 2>&1; then
        launchctl bootstrap "${domain}" "${target}"
      fi
    done
    for service in "${services[@]}"; do
      launchctl kickstart "${domain}/com.linlinqi.${service}"
    done
    ;;
  stop)
    for ((index=${#all_services[@]}-1; index>=0; index--)); do
      service="${all_services[index]}"
      target="$(service_target "${service}")"
      launchctl bootout "${domain}" "${target}" >/dev/null 2>&1 || true
    done
    ;;
  restart)
	# Stop dependants before PostgreSQL/Redis. Sending kickstart -k to every
	# service in forward order leaves PostgreSQL waiting in smart shutdown while
	# newly restarted API processes reconnect to it.
	for ((index=${#services[@]}-1; index>=0; index--)); do
		service="${services[index]}"
		launchctl bootout "${domain}" "$(service_target "${service}")" >/dev/null 2>&1 || true
	done
    for service in "${services[@]}"; do
		launchctl bootstrap "${domain}" "$(service_target "${service}")"
		launchctl kickstart "${domain}/com.linlinqi.${service}"
    done
    ;;
  status)
    for service in "${all_services[@]}"; do
      description="$(launchctl print "${domain}/com.linlinqi.${service}" 2>/dev/null || true)"
      state="$(awk -F'= ' '/^[[:space:]]*state =/{print $2; exit}' <<<"${description}")"
      pid="$(awk -F'= ' '/^[[:space:]]*pid =/{print $2; exit}' <<<"${description}")"
      printf '%-14s state=%-10s pid=%s\n' "${service}" "${state:-not-loaded}" "${pid:-none}"
    done
    ;;
  *)
    echo "usage: $0 {start|stop|restart|status}" >&2
    exit 64
    ;;
esac
