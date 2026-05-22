#!/usr/bin/env bash
set -euo pipefail

# cpuguard remote uninstaller.
# Usage:
#   curl -sfL https://raw.githubusercontent.com/jeffusion/cpuguard/main/scripts/uninstall.sh | sudo sh
#   REMOVE_CONFIG=1 REMOVE_STATE=1 sudo bash scripts/uninstall.sh

PREFIX="${PREFIX:-/usr/local/bin}"
ETC_DIR="${ETC_DIR:-/etc/cpuguard}"
STATE_DIR="${STATE_DIR:-/var/lib/cpuguard}"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"
SERVICE_NAME="${SERVICE_NAME:-cpuguard.service}"
REMOVE_CONFIG="${REMOVE_CONFIG:-0}"
REMOVE_STATE="${REMOVE_STATE:-0}"

if [[ "${EUID}" -ne 0 ]]; then
  echo "Please run as root. Example: sudo bash scripts/uninstall.sh" >&2
  exit 1
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl stop "${SERVICE_NAME}" >/dev/null 2>&1 || true
  systemctl disable "${SERVICE_NAME}" >/dev/null 2>&1 || true
fi

rm -f "${SYSTEMD_DIR}/${SERVICE_NAME}"

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
fi

rm -f "${PREFIX}/cpuguardd" "${PREFIX}/cpuguardctl"

if [[ "${REMOVE_CONFIG}" == "1" ]]; then
  rm -rf "${ETC_DIR}"
fi

if [[ "${REMOVE_STATE}" == "1" ]]; then
  rm -rf "${STATE_DIR}"
fi

echo "Uninstall complete. Binaries and systemd unit have been removed."
echo "Config (${ETC_DIR}) and state (${STATE_DIR}) are preserved by default."
echo "To remove them as well: REMOVE_CONFIG=1 REMOVE_STATE=1 bash scripts/uninstall.sh"
