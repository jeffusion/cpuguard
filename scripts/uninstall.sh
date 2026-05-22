#!/usr/bin/env bash
set -euo pipefail

PREFIX="${PREFIX:-/usr/local/bin}"
ETC_DIR="${ETC_DIR:-/etc/cpuguard}"
STATE_DIR="${STATE_DIR:-/var/lib/cpuguard}"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"
SERVICE_NAME="${SERVICE_NAME:-cpuguard.service}"
REMOVE_CONFIG="${REMOVE_CONFIG:-0}"
REMOVE_STATE="${REMOVE_STATE:-0}"

if [[ "${EUID}" -ne 0 ]]; then
  echo "请使用 root 运行卸载脚本，例如：sudo bash scripts/uninstall.sh" >&2
  exit 1
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now "${SERVICE_NAME}" >/dev/null 2>&1 || true
fi

rm -f "${SYSTEMD_DIR}/${SERVICE_NAME}"
rm -f "${PREFIX}/cpuguardd" "${PREFIX}/cpuguardctl"

if [[ "${REMOVE_CONFIG}" == "1" ]]; then
  rm -rf "${ETC_DIR}"
fi

if [[ "${REMOVE_STATE}" == "1" ]]; then
  rm -rf "${STATE_DIR}"
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
fi

echo "卸载完成。默认保留 ${ETC_DIR} 和 ${STATE_DIR}；如需删除，使用 REMOVE_CONFIG=1 REMOVE_STATE=1。"
