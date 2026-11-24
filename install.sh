#!/usr/bin/env bash
# hxoad 一键安装 (Linux / macOS), 不需要 Docker:
#
#   curl -fsSL https://raw.githubusercontent.com/HengXin666/hxoa/main/install.sh | bash
#
# 选项 (放在 "bash -s --" 后面):
#   --public-panel     面板监听 0.0.0.0:7071 并自动生成密码 (VPS 上不想用 ssh 转发时)
#   --from-source      不下载预编译文件, 用本机 Go 编译
#   --no-service       只装二进制, 不注册 systemd 服务
#   --uninstall        停止并移除服务和二进制 (数据目录保留)
# 环境变量: HXOA_VERSION (默认 latest), HXOA_USER (以 root 安装时服务运行的用户)
set -euo pipefail

REPO="HengXin666/hxoa"
VERSION="${HXOA_VERSION:-latest}"
PUBLIC_PANEL=0; FROM_SOURCE=0; SERVICE=1; UNINSTALL=0
for a in "$@"; do
  case "$a" in
    --public-panel) PUBLIC_PANEL=1 ;;
    --from-source) FROM_SOURCE=1 ;;
    --no-service) SERVICE=0 ;;
    --uninstall) UNINSTALL=1 ;;
    *) echo "未知选项 $a" >&2; exit 2 ;;
  esac
done

say() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m错误:\033[0m %s\n' "$*" >&2; exit 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "不支持的架构 $(uname -m)" ;;
esac
[ "$os" = linux ] || [ "$os" = darwin ] || die "不支持的系统 $os"

if [ "$(id -u)" = 0 ]; then
  BIN_DIR=/usr/local/bin
  RUN_USER="${HXOA_USER:-${SUDO_USER:-root}}"
else
  BIN_DIR="$HOME/.local/bin"
  RUN_USER="$(id -un)"
fi
RUN_HOME=$(getent passwd "$RUN_USER" 2>/dev/null | cut -d: -f6 || true)
RUN_HOME="${RUN_HOME:-$HOME}"

systemd_ok() { [ "$os" = linux ] && command -v systemctl >/dev/null && [ -d /run/systemd/system ]; }

if [ "$UNINSTALL" = 1 ]; then
  if systemd_ok; then
    if [ "$(id -u)" = 0 ]; then
      systemctl disable --now hxoad 2>/dev/null || true; rm -f /etc/systemd/system/hxoad.service; systemctl daemon-reload
    else
      systemctl --user disable --now hxoad 2>/dev/null || true; rm -f "$HOME/.config/systemd/user/hxoad.service"; systemctl --user daemon-reload
    fi
  fi
  rm -f "$BIN_DIR/hxoad"
  say "已卸载。数据目录 $RUN_HOME/.config/hxoad 保留, 不需要可手动删除。"
  exit 0
fi

mkdir -p "$BIN_DIR"
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT

fetch_release() {
  local url
  if [ "$VERSION" = latest ]; then
    url="https://github.com/$REPO/releases/latest/download/hxoad-$os-$arch"
  else
    url="https://github.com/$REPO/releases/download/$VERSION/hxoad-$os-$arch"
  fi
  say "下载 $url"
  curl -fL --retry 3 -o "$tmp/hxoad" "$url" 2>/dev/null
}

build_source() {
  command -v go >/dev/null || die "没有可下载的发布版本, 本机也没有 Go。请安装 Go 1.22+ 后加 --from-source 重试"
  command -v git >/dev/null || die "需要 git"
  say "用 Go 从源码编译"
  git clone --depth 1 "https://github.com/$REPO.git" "$tmp/src" >/dev/null 2>&1
  (cd "$tmp/src" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$tmp/hxoad" ./cmd/hxoad)
}

if [ "$FROM_SOURCE" = 1 ]; then build_source; else fetch_release || { say "下载失败, 改为源码编译"; build_source; }; fi
install -m 0755 "$tmp/hxoad" "$BIN_DIR/hxoad"
say "已安装 $BIN_DIR/hxoad ($("$BIN_DIR/hxoad" -version))"

command -v git >/dev/null || say "提示: 没装 git, workers 功能需要它"

ADMIN_ADDR=127.0.0.1:7071
PASSWORD=""
if [ "$PUBLIC_PANEL" = 1 ]; then
  ADMIN_ADDR=0.0.0.0:7071
  PASSWORD=$(head -c 18 /dev/urandom | base64 | tr -d '/+=' | head -c 20)
fi

if [ "$SERVICE" = 0 ] || ! systemd_ok; then
  [ "$SERVICE" = 1 ] && say "没有 systemd, 跳过服务注册"
  echo
  echo "手动启动:  HXOA_ADMIN_ADDR=$ADMIN_ADDR ${PASSWORD:+HXOA_ADMIN_PASSWORD=$PASSWORD }$BIN_DIR/hxoad"
  exit 0
fi

unit="[Unit]
Description=hxoa workspace server
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=$BIN_DIR/hxoad
Environment=HXOA_ADMIN_ADDR=$ADMIN_ADDR
${PASSWORD:+Environment=HXOA_ADMIN_PASSWORD=$PASSWORD}
Restart=always
RestartSec=3
"
if [ "$(id -u)" = 0 ]; then
  [ "$RUN_USER" = root ] && say "注意: 服务以 root 运行, AI 执行的命令也会是 root。可用 HXOA_USER=<用户> 重新安装。"
  printf '%sUser=%s\nWorkingDirectory=%s\n\n[Install]\nWantedBy=multi-user.target\n' "$unit" "$RUN_USER" "$RUN_HOME" > /etc/systemd/system/hxoad.service
  chmod 600 /etc/systemd/system/hxoad.service
  systemctl daemon-reload
  systemctl enable --now hxoad
  systemctl restart hxoad
else
  mkdir -p "$HOME/.config/systemd/user"
  printf '%sWorkingDirectory=%s\n\n[Install]\nWantedBy=default.target\n' "$unit" "$HOME" > "$HOME/.config/systemd/user/hxoad.service"
  chmod 600 "$HOME/.config/systemd/user/hxoad.service"
  systemctl --user daemon-reload
  systemctl --user enable --now hxoad
  systemctl --user restart hxoad
  loginctl enable-linger "$RUN_USER" 2>/dev/null || say "提示: 运行 sudo loginctl enable-linger $RUN_USER, 退出登录后服务才会继续运行"
fi

say "hxoad 已启动"
echo
if [ "$PUBLIC_PANEL" = 1 ]; then
  ip=$(curl -fsS --max-time 3 https://api.ipify.org 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}')
  echo "  面板:  http://${ip:-<服务器IP>}:7071   (用户名随意, 密码: $PASSWORD)"
  echo "  建议之后在面板里配好固定域名, 并用防火墙限制 7071 的来源。"
else
  echo "  面板只监听本机。在你自己的电脑上运行:"
  echo "      ssh -N -L 7071:127.0.0.1:7071 $RUN_USER@<服务器地址>"
  echo "  然后打开 http://127.0.0.1:7071"
fi
echo "  在面板里: 1) 开启公网隧道  2) 添加工作区  3) 新建连接并复制提示给 AI"
