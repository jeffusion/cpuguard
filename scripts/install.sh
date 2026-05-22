#!/usr/bin/env bash
set -euo pipefail

# cpuguard remote installer — downloads pre-built binaries from GitHub Release.
# Usage:
#   curl -sfL https://raw.githubusercontent.com/jeffusion/cpuguard/main/scripts/install.sh | sudo sh
#   curl -sfL https://raw.githubusercontent.com/jeffusion/cpuguard/main/scripts/install.sh | sudo sh -s -- v0.1.0

REPO="jeffusion/cpuguard"
PREFIX="${PREFIX:-/usr/local/bin}"
ETC_DIR="${ETC_DIR:-/etc/cpuguard}"
STATE_DIR="${STATE_DIR:-/var/lib/cpuguard}"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"
SERVICE_NAME="${SERVICE_NAME:-cpuguard.service}"
REQUESTED_VERSION="${1:-}"

info()  { echo "[cpuguard] $*"; }
err()   { echo "[cpuguard] ERROR: $*" >&2; exit 1; }

require_root() {
	if [ "${EUID:-$(id -u)}" -ne 0 ]; then
		err "This script must be run as root. Try: curl ... | sudo sh"
	fi
}

detect_arch() {
	local uname_m
	uname_m="$(uname -m)"
	case "${uname_m}" in
		x86_64|amd64)  echo "amd64" ;;
		aarch64|arm64) echo "arm64" ;;
		*)             err "Unsupported architecture: ${uname_m}. cpuguard supports amd64 and arm64." ;;
	esac
}

detect_latest_version() {
	local url="https://github.com/${REPO}/releases/latest"
	local tmp
	tmp=$(curl -sfL -o /dev/null -w '%{url_effective}' "${url}" 2>/dev/null) \
		|| err "Failed to query latest release from ${url}"
	# https://github.com/<org>/<repo>/releases/latest redirects to /tag/<version>
	local version="${tmp##*/}"
	[ -n "${version}" ] || err "Could not determine latest version from ${tmp}"
	echo "${version}"
}

require_root

ARCH="$(detect_arch)"
if [ -n "${REQUESTED_VERSION}" ]; then
	VERSION="${REQUESTED_VERSION}"
else
	VERSION="$(detect_latest_version)"
fi
VERSION_NO_V="${VERSION#v}"

TARBALL="cpuguard_${VERSION_NO_V}_linux_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${TARBALL}"

info "Installing cpuguard ${VERSION} for linux/${ARCH}..."

if [ ! -d /sys/fs/cgroup/cgroup.controllers ]; then
	err "cgroup v2 is required but not available on this system."
fi

# ── Download ─────────────────────────────────────────────────────────

TMPDIR="$(mktemp -d)"
trap 'rm -rf "${TMPDIR}"' EXIT

info "Downloading ${DOWNLOAD_URL}..."
curl -sfL -o "${TMPDIR}/${TARBALL}" "${DOWNLOAD_URL}" \
	|| err "Download failed. Check that version ${VERSION} exists at ${DOWNLOAD_URL}"

# ── Extract & Install ────────────────────────────────────────────────

tar -xzf "${TMPDIR}/${TARBALL}" -C "${TMPDIR}"
# tarball contains a directory: cpuguard_<version>_linux_<arch>/
SRC="${TMPDIR}/cpuguard_${VERSION_NO_V}_linux_${ARCH}"

[ -f "${SRC}/cpuguardd" ]   || err "cpuguardd not found in archive"
[ -f "${SRC}/cpuguardctl" ] || err "cpuguardctl not found in archive"

install -d "${PREFIX}" "${ETC_DIR}" "${STATE_DIR}" "${SYSTEMD_DIR}"

install -m 0755 "${SRC}/cpuguardd"   "${PREFIX}/cpuguardd"
install -m 0755 "${SRC}/cpuguardctl" "${PREFIX}/cpuguardctl"

# Config: preserve existing
if [ ! -f "${ETC_DIR}/config.yaml" ]; then
	install -m 0644 "${SRC}/config.yaml" "${ETC_DIR}/config.yaml"
else
	info "Preserving existing config: ${ETC_DIR}/config.yaml"
fi

# Systemd unit
install -m 0644 "${SRC}/cpuguard.service" "${SYSTEMD_DIR}/${SERVICE_NAME}"
systemctl daemon-reload

# ── Done ─────────────────────────────────────────────────────────────

cat <<EOF

[cpuguard] Installation complete. ${VERSION} linux/${ARCH}

Next steps:
  1. Edit config:     ${ETC_DIR}/config.yaml
  2. Validate config: ${PREFIX}/cpuguardctl check-config ${ETC_DIR}/config.yaml
  3. Enable at boot:  ${PREFIX}/cpuguardctl enable
  4. Start service:   ${PREFIX}/cpuguardctl start
  5. Check status:    ${PREFIX}/cpuguardctl status

EOF
