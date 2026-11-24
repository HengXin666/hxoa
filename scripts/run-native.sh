#!/usr/bin/env bash
# 不用 Docker, 直接在宿主上跑: 命令用你本机的工具链和环境。
# 需要 Go 1.22+ 和 cloudflared (https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)
set -euo pipefail
cd "$(dirname "$0")/.."
go build -o bin/hxoad ./cmd/hxoad
args=(-quick-tunnel)
if [ -n "${HXOA_PUBLIC_URL:-}" ]; then args=(-public-url "$HXOA_PUBLIC_URL"); fi
exec bin/hxoad "${args[@]}" "$@"
