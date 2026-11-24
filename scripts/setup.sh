#!/usr/bin/env bash
# 生成 docker compose 用的 .env: 你的 uid/gid, 以及允许分享的宿主根目录。
set -euo pipefail
cd "$(dirname "$0")/.."
root="${1:-$HOME}"
root="$(cd "$root" && pwd)"
if [ -f .env ]; then
  echo ".env 已存在, 不覆盖:"; cat .env; exit 0
fi
cat > .env <<ENV
HXOA_UID=$(id -u)
HXOA_GID=$(id -g)
# 容器能看到的宿主目录; 工作区必须在它下面
HXOA_HOST_ROOT=$root
HXOA_ADMIN_PORT=7071
# 固定域名时填写 (见 docker-compose.named.yml)
HXOA_PUBLIC_URL=
CF_TUNNEL_TOKEN=
ENV
mkdir -p data/home
echo "已写入 .env (分享根目录: $root)"
echo "下一步: docker compose up -d --build, 然后打开 http://127.0.0.1:7071"
