#!/usr/bin/env bash
set -euo pipefail

export FRONT_PORT=20140
export UPSTREAM_FILE="${HOME}/.9router/front-upstream"

exec /home/webserver-01/marvrouter-front
