#!/usr/bin/env bash

set -euo pipefail

# Usage:
#   sudo ./install-datamanagementd.sh --binary /path/to/datamanagementd
# Or:
#   sudo ./install-datamanagementd.sh --source /path/to/sub2api/repo

BIN_PATH=""
SOURCE_PATH=""
INSTALL_DIR="/opt/sub2api"
DATA_DIR="/var/lib/sub2api/datamanagement"
SERVICE_FILE_NAME="sub2api-datamanagementd.service"

function print_help() {
  cat <<'EOF'
Usage:
  install-datamanagementd.sh [--binary <path-to-datamanagementd-binary>] [--source <repo-path>]

Options:
  --binary  Specify the path to a pre-built datamanagementd binary
  --source  Specify the sub2api repository path (the script will run go build)
  -h, --help Show help

Examples:
  sudo ./install-datamanagementd.sh --binary ./datamanagement/datamanagementd
  sudo ./install-datamanagementd.sh --source /opt/sub2api-src
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary)
      BIN_PATH="${2:-}"
      shift 2
      ;;
    --source)
      SOURCE_PATH="${2:-}"
      shift 2
      ;;
    -h|--help)
      print_help
      exit 0
      ;;
    *)
      echo "Unknown argument: $1"
      print_help
      exit 1
      ;;
  esac
done

if [[ -n "$BIN_PATH" && -n "$SOURCE_PATH" ]]; then
  echo "Error: --binary and --source are mutually exclusive"
  exit 1
fi

if [[ -z "$BIN_PATH" && -z "$SOURCE_PATH" ]]; then
  echo "Error: Either --binary or --source must be provided"
  exit 1
fi

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Error: Please run with root privileges (e.g. sudo)"
  exit 1
fi

if [[ -n "$SOURCE_PATH" ]]; then
  if [[ ! -d "$SOURCE_PATH/datamanagement" ]]; then
    echo "Error: Invalid repository path, $SOURCE_PATH/datamanagement not found"
    exit 1
  fi
  echo "[1/6] Building datamanagementd from source..."
  (cd "$SOURCE_PATH/datamanagement" && go build -o datamanagementd ./cmd/datamanagementd)
  BIN_PATH="$SOURCE_PATH/datamanagement/datamanagementd"
fi

if [[ ! -f "$BIN_PATH" ]]; then
  echo "Error: Binary file does not exist: $BIN_PATH"
  exit 1
fi

if ! id sub2api >/dev/null 2>&1; then
  echo "[2/6] Creating system user sub2api..."
  useradd --system --no-create-home --shell /usr/sbin/nologin sub2api
else
  echo "[2/6] System user sub2api already exists, skipping creation"
fi

echo "[3/6] Installing datamanagementd binary..."
mkdir -p "$INSTALL_DIR"
install -m 0755 "$BIN_PATH" "$INSTALL_DIR/datamanagementd"

echo "[4/6] Preparing data directory..."
mkdir -p "$DATA_DIR"
chown -R sub2api:sub2api /var/lib/sub2api
chmod 0750 "$DATA_DIR"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICE_TEMPLATE="$SCRIPT_DIR/$SERVICE_FILE_NAME"
if [[ ! -f "$SERVICE_TEMPLATE" ]]; then
  echo "Error: Service template not found: $SERVICE_TEMPLATE"
  exit 1
fi

echo "[5/6] Installing systemd service..."
cp "$SERVICE_TEMPLATE" "/etc/systemd/system/$SERVICE_FILE_NAME"
systemctl daemon-reload
systemctl enable --now sub2api-datamanagementd

echo "[6/6] Done, current status:"
systemctl --no-pager --full status sub2api-datamanagementd || true

cat <<'EOF'

Next steps:
1. View logs: sudo journalctl -u sub2api-datamanagementd -f
2. When deploying sub2api in Docker, mount the socket:
   /tmp/sub2api-datamanagement.sock:/tmp/sub2api-datamanagement.sock
3. Go to the admin panel “Data Management” page and verify agent=enabled

EOF
