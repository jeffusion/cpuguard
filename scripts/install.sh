#!/usr/bin/env bash
set -euo pipefail

PREFIX="${PREFIX:-/usr/local/bin}"
ETC_DIR="${ETC_DIR:-/etc/cpuguard}"
STATE_DIR="${STATE_DIR:-/var/lib/cpuguard}"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"
SERVICE_NAME="${SERVICE_NAME:-cpuguard.service}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "请使用 root 运行安装脚本，例如：sudo bash scripts/install.sh" >&2
    exit 1
  fi
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "缺少命令: $1" >&2
    exit 1
  fi
}

install_files() {
  mkdir -p "${PREFIX}" "${ETC_DIR}" "${STATE_DIR}" "${SYSTEMD_DIR}"

  echo "构建 cpuguardd..."
  (cd "${ROOT_DIR}" && go build -buildvcs=false -o /tmp/cpuguardd ./cmd/cpuguardd)
  echo "构建 cpuguardctl..."
  (cd "${ROOT_DIR}" && go build -buildvcs=false -o /tmp/cpuguardctl ./cmd/cpuguardctl)

  install -m 0755 /tmp/cpuguardd "${PREFIX}/cpuguardd"
  install -m 0755 /tmp/cpuguardctl "${PREFIX}/cpuguardctl"

  if [[ ! -f "${ETC_DIR}/config.yaml" ]]; then
    install -m 0644 "${ROOT_DIR}/examples/config.yaml" "${ETC_DIR}/config.yaml"
  else
    echo "保留已有配置: ${ETC_DIR}/config.yaml"
  fi

  install -m 0644 "${ROOT_DIR}/packaging/systemd/cpuguard.service" "${SYSTEMD_DIR}/${SERVICE_NAME}"
  systemctl daemon-reload
}

print_next_steps() {
  cat <<EOF
安装完成。

下一步：
1. 编辑配置文件: ${ETC_DIR}/config.yaml
2. 检查配置: ${PREFIX}/cpuguardctl check-config ${ETC_DIR}/config.yaml
3. 设置开机启动: ${PREFIX}/cpuguardctl enable
4. 启动服务: ${PREFIX}/cpuguardctl start
5. 查看状态: ${PREFIX}/cpuguardctl status
EOF
}

main() {
  require_root
  require_cmd go
  require_cmd systemctl
  install_files
  print_next_steps
}

main "$@"
