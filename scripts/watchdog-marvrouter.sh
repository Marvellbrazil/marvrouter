#!/usr/bin/env bash
set -euo pipefail

PM2_FRONT="marvrouter-front"
PM2_GATEWAY="marvrouter-gateway-v112"
FRONT_PORT=20130
GATEWAY_PORT=20131
LOG="${HOME}/.9router/watchdog.log"
MAX_LOG_LINES=100

log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >> "$LOG"; }

check_health() {
  curl -fsS -m 5 "http://127.0.0.1:$1/api/health" >/dev/null 2>&1
}

restart_process() {
  local name="$1" port="$2"
  log "Restarting $name (port $port)..."
  pm2 restart "$name" >/dev/null 2>&1 || pm2 start "$name" >/dev/null 2>&1
  sleep 10
  if check_health "$port"; then
    log "$name recovered on port $port"
  else
    log "ERROR: $name failed to recover on port $port"
  fi
}

if ! check_health "$FRONT_PORT"; then
  log "Front proxy unhealthy on $FRONT_PORT"
  restart_process "$PM2_FRONT" "$FRONT_PORT"
fi

if ! check_health "$GATEWAY_PORT"; then
  log "Gateway unhealthy on $GATEWAY_PORT"
  restart_process "$PM2_GATEWAY" "$GATEWAY_PORT"
fi

if [ -f "$LOG" ] && [ "$(wc -l < "$LOG")" -gt "$MAX_LOG_LINES" ]; then
  tail -n "$MAX_LOG_LINES" "$LOG" > "${LOG}.tmp" && mv "${LOG}.tmp" "$LOG"
fi
