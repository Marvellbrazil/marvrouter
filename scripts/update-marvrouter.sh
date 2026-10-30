#!/usr/bin/env bash
set -euo pipefail

REPO="Marvellbrazil/marvrouter"
FRONT_PORT=20130
FRONT_UPSTREAM_FILE="${HOME}/.9router/front-upstream"
PM2_FRONT_NAME="marvrouter-front"
PM2_GATEWAY_NAME="marvrouter-gateway-v112"
GATEWAY_PORT=20131
MIN_FREE_MB=400
RELEASES_ROOT="${HOME}/marvrouter-releases"
CURRENT_LINK="${RELEASES_ROOT}/current"

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

download() { curl -fsSL -o "$2" "$1"; }

wait_for_health() {
  local url="$1" tries="${2:-30}"
  for ((i=1; i<=tries; i++)); do
    if curl -fsS -m 3 "$url" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  return 1
}

active_streams() {
  if ! command -v lsof >/dev/null 2>&1; then
    echo 0
    return
  fi
  lsof -iTCP:"${GATEWAY_PORT}" -sTCP:ESTABLISHED 2>/dev/null | grep -c ESTABLISHED || echo 0
}

current_sha() {
  [ -f "${CURRENT_LINK}/.build-sha" ] && cat "${CURRENT_LINK}/.build-sha" || echo ""
}

prune_old_releases() {
  local keep
  keep=$(readlink -f "$CURRENT_LINK" 2>/dev/null || echo "")
  for dir in "${RELEASES_ROOT}/"*; do
    [ -d "$dir" ] || continue
    local real
    real=$(readlink -f "$dir")
    [ "$real" = "$keep" ] && continue
    case "$real" in
      */current) continue ;;
    esac
    log "Pruning old release: $(basename "$dir")"
    rm -rf "$dir"
  done
}

main() {
  local free_mb
  free_mb=$(awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo)
  if [ "$free_mb" -lt "$MIN_FREE_MB" ]; then
    log "Only ${free_mb}MB available, need ${MIN_FREE_MB}MB. Aborting."
    exit 1
  fi

  local tag version
  tag=$(latest_tag)
  version="${tag#fork-v}"

  log "Downloading build metadata..."
  local build_info
  build_info=$(curl -fsSL "https://github.com/${REPO}/releases/download/${tag}/build-info.txt")
  local build_sha
  build_sha=$(printf '%s\n' "$build_info" | sed -n 's/^sha=//p' | head -n1)
  [ -z "$build_sha" ] && build_sha=$(printf '%s\n' "$build_info" | head -n1 | tr -d '[:space:]')

  if [ -z "$build_sha" ]; then
    log "WARNING: no build SHA in release; falling back to version-only dir"
    build_sha="$version"
  fi
  log "Latest release: ${tag} (version ${version}, sha ${build_sha})"

  local existing
  existing=$(current_sha)
  if [ "$existing" = "$build_sha" ]; then
    log "Already on build ${build_sha}. Nothing to do."
    exit 0
  fi

  local release_dir="${RELEASES_ROOT}/${version}-${build_sha}"
  mkdir -p "$release_dir" "$(dirname "$FRONT_UPSTREAM_FILE")"

  log "Downloading app + gateway..."
  download "https://github.com/${REPO}/releases/download/${tag}/marvrouter-app-${version}.tar.gz" "${release_dir}/app.tar.gz"
  download "https://github.com/${REPO}/releases/download/${tag}/marvrouter-gateway.tar.gz" "${release_dir}/gateway.tar.gz"

  # --- app: extract to a fresh dir, flatten nested app/app, validate, never touch live dir
  local app_staging="${release_dir}/app-staging"
  rm -rf "$app_staging"
  mkdir -p "$app_staging"
  tar -xzf "${release_dir}/app.tar.gz" -C "$app_staging"
  local app_dir="${release_dir}/app"
  rm -rf "$app_dir"
  if [ -d "${app_staging}/app" ]; then
    mv "${app_staging}/app" "$app_dir"
  else
    mv "$app_staging" "$app_dir"
  fi
  rm -rf "$app_staging"
  if [ ! -f "${app_dir}/server.js" ]; then
    log "ERROR: server.js missing in release artifact"
    exit 1
  fi
  # custom-server.js comes from the fork build itself (no symlink to decolua npm pkg)
  if [ ! -f "${app_dir}/custom-server.js" ]; then
    log "WARNING: custom-server.js missing in build; gateway will use server.js"
  fi
  printf '%s\n' "$build_sha" > "${release_dir}/.build-sha"

  # --- gateway binary
  local gw_staging="${release_dir}/gateway-staging"
  rm -rf "$gw_staging"
  mkdir -p "$gw_staging"
  tar -xzf "${release_dir}/gateway.tar.gz" -C "$gw_staging"
  if [ ! -f "${gw_staging}/9router-gateway" ]; then
    log "ERROR: gateway binary missing in release artifact"
    exit 1
  fi
  local gateway_dir="${release_dir}/gateway"
  rm -rf "$gateway_dir"
  mv "$gw_staging" "$gateway_dir"
  local gateway_bin="${gateway_dir}/9router-gateway"
  chmod +x "$gateway_bin"

  log "Starting gateway on ${GATEWAY_PORT} with new app..."
  pm2 delete "$PM2_GATEWAY_NAME" >/dev/null 2>&1 || true
  PORT="$GATEWAY_PORT" NEXT_APP_DIR="$app_dir" pm2 start "$gateway_bin" \
    --name "$PM2_GATEWAY_NAME" --max-memory-restart 700M

  if ! wait_for_health "http://127.0.0.1:${GATEWAY_PORT}/api/health"; then
    log "ERROR: gateway healthcheck failed. Logs:"
    pm2 logs "$PM2_GATEWAY_NAME" --lines 10 --nostream 2>&1 | tail -n 15
    exit 1
  fi

  local next_ready=0
  for ((i=1; i<=40; i++)); do
    if curl -fsS -m 3 "http://127.0.0.1:${GATEWAY_PORT}/api/version" >/dev/null 2>&1; then
      next_ready=1
      break
    fi
    sleep 1
  done
  if [ "$next_ready" -eq 0 ]; then
    log "ERROR: Next.js not ready after 40s. Logs:"
    pm2 logs "$PM2_GATEWAY_NAME" --lines 10 --nostream 2>&1 | tail -n 15
    exit 1
  fi
  log "Gateway + Next.js healthy on ${GATEWAY_PORT}"

  # mark this build as current only after health passes
  ln -sfn "$release_dir" "$CURRENT_LINK"

  # drain then point front proxy
  local current_upstream=""
  [ -f "$FRONT_UPSTREAM_FILE" ] && current_upstream=$(cat "$FRONT_UPSTREAM_FILE")
  if [ "$current_upstream" != "http://127.0.0.1:${GATEWAY_PORT}" ]; then
    local streams
    streams=$(active_streams)
    if [ "$streams" -gt 0 ]; then
      log "${streams} active stream(s), waiting to drain (max 300s)..."
      local waited=0
      while [ "$streams" -gt 0 ] && [ "$waited" -lt 300 ]; do
        sleep 5; waited=$((waited + 5)); streams=$(active_streams)
      done
    fi
  fi

  echo "http://127.0.0.1:${GATEWAY_PORT}" > "$FRONT_UPSTREAM_FILE"
  if wait_for_health "http://127.0.0.1:${FRONT_PORT}/api/health" 10; then
    log "Front proxy healthy on ${FRONT_PORT}"
  else
    log "Front proxy not responding (may need restart)"
  fi

  rm -f "${release_dir}/app.tar.gz" "${release_dir}/gateway.tar.gz"
  prune_old_releases

  log "Done. Build ${build_sha} (v${version}) live on ${FRONT_PORT}."
  pm2 save >/dev/null 2>&1 || true
}

main "$@"
