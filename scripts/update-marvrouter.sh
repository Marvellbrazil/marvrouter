#!/usr/bin/env bash
set -euo pipefail

REPO="Marvellbrazil/marvrouter"
FRONT_PORT=20130
FRONT_UPSTREAM_FILE="${HOME}/.9router/front-upstream"
PM2_FRONT_NAME="marvrouter-front"
PM2_GATEWAY_NAME="marvrouter-gateway-v112"
GATEWAY_PORT=20131
MIN_FREE_MB=400

log() { printf '[update-marvrouter] %s\n' "$*"; }

latest_tag() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases" \
    | python3 -c '
import sys, json
for rel in json.load(sys.stdin):
    tag = rel.get("tag_name", "")
    if tag.startswith("fork-v") and not rel.get("draft"):
        print(tag)
        sys.exit(0)
sys.exit("no fork release found")'
}

download() {
  curl -fsSL -o "$2" "$1"
}

wait_for_health() {
  local url="$1" tries="${2:-30}"
  for ((i=1; i<=tries; i++)); do
    if curl -fsS -m 3 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

active_streams() {
  if ! command -v lsof >/dev/null 2>&1; then
    log "lsof not available, skipping drain check"
    echo 0
    return
  fi
  lsof -iTCP:"${GATEWAY_PORT}" -sTCP:ESTABLISHED 2>/dev/null | grep -c ESTABLISHED || echo 0
}

main() {
  local free_mb
  free_mb=$(awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo)
  if [ "$free_mb" -lt "$MIN_FREE_MB" ]; then
    log "Only ${free_mb}MB available, need ${MIN_FREE_MB}MB. Aborting."
    exit 1
  fi
  log "Memory OK (${free_mb}MB available)"

  local tag version
  tag=$(latest_tag)
  version="${tag#fork-v}"
  log "Latest release: ${tag} (version ${version})"

  local release_dir="${HOME}/marvrouter-releases/${version}"
  mkdir -p "$release_dir" "$(dirname "$FRONT_UPSTREAM_FILE")"

  local app_tarball="${release_dir}/app.tar.gz"
  local gateway_tarball="${release_dir}/gateway.tar.gz"

  log "Downloading app..."
  download "https://github.com/${REPO}/releases/download/${tag}/marvrouter-app-${version}.tar.gz" "$app_tarball"

  log "Downloading gateway..."
  download "https://github.com/${REPO}/releases/download/${tag}/marvrouter-gateway.tar.gz" "$gateway_tarball"

  local app_staging="${release_dir}/app-staging"
  rm -rf "$app_staging"
  mkdir -p "$app_staging"
  tar -xzf "$app_tarball" -C "$app_staging"
  rm -rf "${release_dir}/app-new"
  if [ -d "${app_staging}/app" ]; then
    mv "${app_staging}/app" "${release_dir}/app-new"
  else
    mv "$app_staging" "${release_dir}/app-new"
  fi
  rm -rf "$app_staging"
  if [ ! -f "${release_dir}/app-new/server.js" ]; then
    log "ERROR: ${release_dir}/app-new/server.js missing in release artifact"
    exit 1
  fi

  local gateway_staging="${release_dir}/gateway-staging"
  rm -rf "$gateway_staging"
  mkdir -p "$gateway_staging"
  tar -xzf "$gateway_tarball" -C "$gateway_staging"
  if [ ! -f "${gateway_staging}/9router-gateway" ]; then
    log "ERROR: gateway binary missing in release artifact"
    exit 1
  fi
  local gateway_dir="${release_dir}/gateway"
  rm -rf "$gateway_dir"
  mv "$gateway_staging" "$gateway_dir"
  local gateway_bin="${gateway_dir}/9router-gateway"
  chmod +x "$gateway_bin"

  local app_dir="${release_dir}/app"
  if [ ! -f "${app_dir}/server.js" ]; then
    log "Installing new app dir"
    rm -rf "$app_dir"
    mv "${release_dir}/app-new" "$app_dir"
  fi

  ln -sf "${HOME}/9router-0.5.112/node_modules/9router/app/custom-server.js" "${app_dir}/custom-server.js"

  log "Stopping old gateway..."
  pm2 delete "$PM2_GATEWAY_NAME" >/dev/null 2>&1 || true

  log "Starting new gateway on ${GATEWAY_PORT}..."
  PORT="$GATEWAY_PORT" NEXT_APP_DIR="$app_dir" pm2 start "$gateway_bin" \
    --name "$PM2_GATEWAY_NAME" --max-memory-restart 700M

  if ! wait_for_health "http://127.0.0.1:${GATEWAY_PORT}/api/health"; then
    log "ERROR: gateway healthcheck failed. Logs:"
    pm2 logs "$PM2_GATEWAY_NAME" --lines 10 --nostream 2>&1 | tail -n 15
    exit 1
  fi
  log "Gateway healthy on ${GATEWAY_PORT}"

  local next_ready=0
  for ((i=1; i<=30; i++)); do
    if curl -fsS -m 3 "http://127.0.0.1:${GATEWAY_PORT}/api/version" >/dev/null 2>&1; then
      next_ready=1
      break
    fi
    sleep 1
  done
  if [ "$next_ready" -eq 0 ]; then
    log "ERROR: Next.js not ready after 30s. Logs:"
    pm2 logs "$PM2_GATEWAY_NAME" --lines 10 --nostream 2>&1 | tail -n 15
    exit 1
  fi
  log "Next.js ready"

  local current_upstream=""
  [ -f "$FRONT_UPSTREAM_FILE" ] && current_upstream=$(cat "$FRONT_UPSTREAM_FILE")

  if [ "$current_upstream" != "http://127.0.0.1:${GATEWAY_PORT}" ]; then
    local streams
    streams=$(active_streams)
    if [ "$streams" -gt 0 ]; then
      log "${streams} active stream(s) on ${GATEWAY_PORT}, waiting to drain..."
      local waited=0
      while [ "$streams" -gt 0 ] && [ "$waited" -lt 300 ]; do
        sleep 5
        waited=$((waited + 5))
        streams=$(active_streams)
      done
      if [ "$streams" -gt 0 ]; then
        log "Streams still active after 300s, switching anyway"
      fi
    fi
  fi

  log "Pointing front proxy to ${GATEWAY_PORT}..."
  echo "http://127.0.0.1:${GATEWAY_PORT}" > "$FRONT_UPSTREAM_FILE"

  if wait_for_health "http://127.0.0.1:${FRONT_PORT}/api/health" 10; then
    log "Front proxy healthy on ${FRONT_PORT}"
  else
    log "Front proxy not responding yet (may need manual start)"
  fi

  rm -rf "${release_dir}/app-new" 2>/dev/null || true

  log "Done. Version ${version} is live via front proxy on ${FRONT_PORT}."
  pm2 save >/dev/null 2>&1 || true
}

main "$@"
