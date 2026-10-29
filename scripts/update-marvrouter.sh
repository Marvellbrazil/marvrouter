#!/usr/bin/env bash
set -euo pipefail

REPO="Marvellbrazil/marvrouter"
RELEASE_TAG="fork-v0.5.112"
APP_DIR_NAME="marvrouter-app"
FRONT_DIR_NAME="marvrouter-front"
GATEWAY_DIR_NAME="9router-gateway"
FRONT_PORT=20130
FRONT_UPSTREAM_FILE="${HOME}/.9router/front-upstream"
PM2_FRONT_NAME="marvrouter-front"
PM2_GATEWAY_NAME="marvrouter-gateway"

log() { printf '[update-marvrouter] %s\n' "$*"; }

fetch_version() {
  curl -fsSL "https://raw.githubusercontent.com/${REPO}/master/package.json" \
    | python3 -c 'import sys,json; print(json.load(sys.stdin)["version"])'
}

version_from_release() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/tags/${RELEASE_TAG}" \
    | python3 -c 'import sys,json; print(json.load(sys.stdin)["tag_name"].lstrip("fork-v"))'
}

download_artifact() {
  local url="$1" dest="$2"
  curl -fsSL -o "$dest" "$url"
}

extract_app() {
  local version="$1" base="$2"
  local tarball
  tarball=$(mktemp --suffix=.tar.gz)
  download_artifact \
    "https://github.com/${REPO}/releases/download/${RELEASE_TAG}/marvrouter-app-${version}.tar.gz" \
    "$tarball"
  local app_dir="${base}/${APP_DIR_NAME}"
  rm -rf "$app_dir" && mkdir -p "$app_dir"
  tar -xzf "$tarball" -C "$app_dir"
  rm -f "$tarball"
  echo "$app_dir"
}

extract_binary() {
  local name="$1" base="$2"
  local tarball
  tarball=$(mktemp --suffix=.tar.gz)
  download_artifact \
    "https://github.com/${REPO}/releases/download/${RELEASE_TAG}/${name}.tar.gz" \
    "$tarball"
  local dest_dir="${base}/${name}"
  rm -rf "$dest_dir" && mkdir -p "$dest_dir"
  tar -xzf "$tarball" -C "$dest_dir"
  chmod +x "${dest_dir}/${name}"
  rm -f "$tarball"
  echo "${dest_dir}/${name}"
}

wait_for_health() {
  local url="$1" tries="${2:-20}"
  for ((i=1; i<=tries; i++)); do
    if curl -fsS -m 3 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

main() {
  local version app_base front_base gateway_base
  version=$(fetch_version)
  log "Target version: ${version}"

  app_base="${HOME}/marvrouter-releases/${version}"
  front_base="${app_base}"
  gateway_base="${app_base}"

  mkdir -p "$app_base" "$(dirname "$FRONT_UPSTREAM_FILE")"

  log "Downloading app..."
  local app_path
  app_path=$(extract_app "$version" "$app_base")
  log "App: ${app_path}"

  log "Downloading front proxy..."
  local front_bin
  front_bin=$(extract_binary "$FRONT_DIR_NAME" "$front_base")

  log "Downloading gateway..."
  local gateway_bin
  gateway_bin=$(extract_binary "$GATEWAY_DIR_NAME" "$gateway_base")

  log "Restarting gateway (backend Next.js will pick the new app dir)..."
  NEXT_APP_DIR="$app_path" pm2 restart "$PM2_GATEWAY_NAME" \
    --update-env \
    -- "$gateway_bin" 2>/dev/null \
    || NEXT_APP_DIR="$app_path" pm2 start "$gateway_bin" --name "$PM2_GATEWAY_NAME" --max-memory-restart 700M

  local gateway_port="${PORT:-20131}"
  wait_for_health "http://127.0.0.1:${gateway_port}/api/health" \
    || { log "ERROR: gateway healthcheck failed on ${gateway_port}"; exit 1; }
  log "Gateway healthy on ${gateway_port}"

  log "Updating front upstream to ${gateway_port}..."
  echo "http://127.0.0.1:${gateway_port}" > "$FRONT_UPSTREAM_FILE"

  log "Restarting front proxy..."
  PORT="$FRONT_PORT" UPSTREAM_FILE="$FRONT_UPSTREAM_FILE" pm2 restart "$PM2_FRONT_NAME" \
    --update-env \
    -- "$front_bin" 2>/dev/null \
    || PORT="$FRONT_PORT" UPSTREAM_FILE="$FRONT_UPSTREAM_FILE" pm2 start "$front_bin" --name "$PM2_FRONT_NAME"

  wait_for_health "http://127.0.0.1:${FRONT_PORT}/api/health" \
    || { log "ERROR: front healthcheck failed on ${FRONT_PORT}"; exit 1; }
  log "Front proxy healthy on ${FRONT_PORT}"

  log "Done. Version ${version} is live on ${FRONT_PORT}."
  pm2 save >/dev/null 2>&1 || true
}

main "$@"
