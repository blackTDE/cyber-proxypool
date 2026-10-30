#!/usr/bin/env bash
set -e

ROUTER_HOST="${1:-192.168.4.223}"
ROUTER_USER="${2:-root}"
TARGET="${ROUTER_USER}@${ROUTER_HOST}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "=========================================================="
echo "  🚀 CyberProxyPool Deployment to iStoreOS (${TARGET})"
echo "=========================================================="

echo "[1/5] Detecting router CPU architecture..."
ARCH=$(ssh -o ConnectTimeout=5 "${TARGET}" "uname -m" 2>/dev/null || true)

if [ -z "${ARCH}" ]; then
    echo "⚠️  Direct passwordless SSH failed. Please run manually or enter password when prompted."
    ARCH=$(ssh "${TARGET}" "uname -m")
fi

echo "Detected remote architecture: ${ARCH}"

BINARY_NAME=""
case "${ARCH}" in
    x86_64|amd64)
        BINARY_NAME="cyberproxypool-linux-amd64"
        ;;
    aarch64|arm64)
        BINARY_NAME="cyberproxypool-linux-arm64"
        ;;
    armv7*|armv6*|arm)
        BINARY_NAME="cyberproxypool-linux-armv7"
        ;;
    *)
        echo "❌ Unsupported architecture: ${ARCH}"
        exit 1
        ;;
esac

LOCAL_BIN="${PROJECT_ROOT}/bin/${BINARY_NAME}"

# Build if missing
if [ ! -f "${LOCAL_BIN}" ]; then
    echo "Building ${BINARY_NAME}..."
    cd "${PROJECT_ROOT}"
    case "${ARCH}" in
        x86_64|amd64)
            CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "${LOCAL_BIN}" -trimpath -ldflags="-s -w" .
            ;;
        aarch64|arm64)
            CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "${LOCAL_BIN}" -trimpath -ldflags="-s -w" .
            ;;
        armv7*)
            CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o "${LOCAL_BIN}" -trimpath -ldflags="-s -w" .
            ;;
    esac
fi

echo "[2/5] Stopping existing service (if running)..."
ssh "${TARGET}" "/etc/init.d/cyberproxypool stop 2>/dev/null || killall cyberproxypool 2>/dev/null || true"

echo "[3/5] Uploading ${BINARY_NAME} to ${TARGET}:/usr/bin/cyberproxypool..."
scp "${LOCAL_BIN}" "${TARGET}:/usr/bin/cyberproxypool"
ssh "${TARGET}" "chmod +x /usr/bin/cyberproxypool"

echo "[4/5] Installing OpenWrt / iStoreOS procd service..."
scp "${SCRIPT_DIR}/cyberproxypool.init" "${TARGET}:/etc/init.d/cyberproxypool"
ssh "${TARGET}" "chmod +x /etc/init.d/cyberproxypool && /etc/init.d/cyberproxypool enable"

echo "[5/5] Starting CyberProxyPool service..."
ssh "${TARGET}" "/etc/init.d/cyberproxypool restart"

sleep 2
echo ""
echo "Checking service status on router:"
ssh "${TARGET}" "ps | grep [c]yberproxypool || true"

echo ""
echo "=========================================================="
echo "  🎉 CyberProxyPool Successfully Deployed to iStoreOS!"
echo "=========================================================="
echo "  🌐 Web Dashboard:    http://${ROUTER_HOST}:8088"
echo "  🔄 Rotating Tunnel:  ${ROUTER_HOST}:10808 (HTTP & SOCKS5)"
echo "  📁 Config/Data Dir:  /etc/cyberproxypool"
echo "  🔧 Service Commands: /etc/init.d/cyberproxypool [start|stop|restart|status]"
echo "=========================================================="
