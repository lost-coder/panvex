#!/usr/bin/env sh
set -eu

APP_NAME="panvex-control-plane"
SERVICE_NAME="panvex-control-plane"
REPO="${PANVEX_REPO:-panvex/panvex}"

usage() {
  cat <<'EOF'
Panvex single-binary installer

Usage:
  install.sh [--help]

Defaults:
  PANVEX_VERSION=latest
  PANVEX_STORAGE_DRIVER=sqlite
  PANVEX_HTTP_ADDR=:8080
  PANVEX_GRPC_ADDR=:8443

Root install paths:
  PANVEX_BIN_DIR=/usr/local/bin
  PANVEX_CONFIG_DIR=/etc/panvex
  PANVEX_DATA_DIR=/var/lib/panvex
  PANVEX_INSTALL_SERVICE=1 when systemd is available

User install paths:
  PANVEX_BIN_DIR=$HOME/.local/bin
  PANVEX_CONFIG_DIR=$HOME/.config/panvex
  PANVEX_DATA_DIR=$HOME/.local/share/panvex
  PANVEX_INSTALL_SERVICE=0

Storage overrides:
  PANVEX_STORAGE_DRIVER=sqlite|postgres
  PANVEX_STORAGE_DSN=<dsn>

Examples:
  curl -fsSL https://github.com/panvex/panvex/releases/latest/download/install.sh | sh

  PANVEX_STORAGE_DRIVER=postgres \
  PANVEX_STORAGE_DSN='postgres://panvex:password@127.0.0.1:5432/panvex?sslmode=disable' \
  curl -fsSL https://github.com/panvex/panvex/releases/latest/download/install.sh | sh
EOF
}

if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
  usage
  exit 0
fi

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

is_root() {
  [ "$(id -u)" -eq 0 ]
}

has_systemd() {
  command -v systemctl >/dev/null 2>&1
}

detect_os() {
  case "$(uname -s)" in
    Linux) printf 'linux\n' ;;
    *) die "unsupported operating system: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    aarch64|arm64) printf 'arm64\n' ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac
}

download_file() {
  url=$1
  destination=$2

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$destination"
    return
  fi

  if command -v wget >/dev/null 2>&1; then
    wget -qO "$destination" "$url"
    return
  fi

  die "missing required command: curl or wget"
}

quote_sh() {
  printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

default_bin_dir="$HOME/.local/bin"
default_config_dir="$HOME/.config/panvex"
default_data_dir="$HOME/.local/share/panvex"
default_install_service=0

if is_root; then
  default_bin_dir="/usr/local/bin"
  default_config_dir="/etc/panvex"
  default_data_dir="/var/lib/panvex"
  if has_systemd; then
    default_install_service=1
  fi
fi

PANVEX_VERSION="${PANVEX_VERSION:-latest}"
PANVEX_BIN_DIR="${PANVEX_BIN_DIR:-$default_bin_dir}"
PANVEX_CONFIG_DIR="${PANVEX_CONFIG_DIR:-$default_config_dir}"
PANVEX_DATA_DIR="${PANVEX_DATA_DIR:-$default_data_dir}"
PANVEX_HTTP_ADDR="${PANVEX_HTTP_ADDR:-:8080}"
PANVEX_GRPC_ADDR="${PANVEX_GRPC_ADDR:-:8443}"
PANVEX_STORAGE_DRIVER="${PANVEX_STORAGE_DRIVER:-sqlite}"
PANVEX_STORAGE_DSN="${PANVEX_STORAGE_DSN:-}"
PANVEX_INSTALL_SERVICE="${PANVEX_INSTALL_SERVICE:-$default_install_service}"

case "$PANVEX_STORAGE_DRIVER" in
  sqlite)
    if [ -z "$PANVEX_STORAGE_DSN" ]; then
      PANVEX_STORAGE_DSN="$PANVEX_DATA_DIR/panvex.db"
    fi
    ;;
  postgres)
    [ -n "$PANVEX_STORAGE_DSN" ] || die "PANVEX_STORAGE_DSN is required when PANVEX_STORAGE_DRIVER=postgres"
    ;;
  *)
    die "unsupported PANVEX_STORAGE_DRIVER: $PANVEX_STORAGE_DRIVER"
    ;;
esac

if [ "$PANVEX_INSTALL_SERVICE" = "1" ] && ! is_root; then
  die "PANVEX_INSTALL_SERVICE=1 requires root privileges"
fi

if [ "$PANVEX_INSTALL_SERVICE" = "1" ] && ! has_systemd; then
  die "PANVEX_INSTALL_SERVICE=1 requires systemd"
fi

need_cmd tar
need_cmd mktemp
need_cmd sed

os=$(detect_os)
arch=$(detect_arch)
asset_name="$APP_NAME-$os-$arch.tar.gz"

release_path="latest/download"
if [ "$PANVEX_VERSION" != "latest" ]; then
  release_path="download/$PANVEX_VERSION"
fi

archive_url="https://github.com/$REPO/releases/$release_path/$asset_name"
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

archive_path="$tmp_dir/$asset_name"
download_file "$archive_url" "$archive_path"
tar -xzf "$archive_path" -C "$tmp_dir"

binary_path="$tmp_dir/$APP_NAME"
[ -f "$binary_path" ] || die "release archive did not contain $APP_NAME"

mkdir -p "$PANVEX_BIN_DIR" "$PANVEX_CONFIG_DIR" "$PANVEX_DATA_DIR"
install -m 0755 "$binary_path" "$PANVEX_BIN_DIR/$APP_NAME"

env_file="$PANVEX_CONFIG_DIR/panvex.env"
start_script="$PANVEX_CONFIG_DIR/start-control-plane.sh"

{
  printf 'PANVEX_HTTP_ADDR=%s\n' "$(quote_sh "$PANVEX_HTTP_ADDR")"
  printf 'PANVEX_GRPC_ADDR=%s\n' "$(quote_sh "$PANVEX_GRPC_ADDR")"
  printf 'PANVEX_STORAGE_DRIVER=%s\n' "$(quote_sh "$PANVEX_STORAGE_DRIVER")"
  printf 'PANVEX_STORAGE_DSN=%s\n' "$(quote_sh "$PANVEX_STORAGE_DSN")"
} >"$env_file"

cat >"$start_script" <<EOF
#!/usr/bin/env sh
set -eu
. "$env_file"
exec "$PANVEX_BIN_DIR/$APP_NAME" \\
  -http-addr "\$PANVEX_HTTP_ADDR" \\
  -grpc-addr "\$PANVEX_GRPC_ADDR" \\
  -storage-driver "\$PANVEX_STORAGE_DRIVER" \\
  -storage-dsn "\$PANVEX_STORAGE_DSN"
EOF
chmod 0755 "$start_script"

service_file="/etc/systemd/system/$SERVICE_NAME.service"
if [ "$PANVEX_INSTALL_SERVICE" = "1" ]; then
  cat >"$service_file" <<EOF
[Unit]
Description=Panvex Control Plane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$PANVEX_DATA_DIR
ExecStart=$start_script
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable "$SERVICE_NAME.service" >/dev/null
fi

printf 'Panvex control-plane installed.\n'
printf 'Binary: %s/%s\n' "$PANVEX_BIN_DIR" "$APP_NAME"
printf 'Config: %s\n' "$env_file"
printf 'Start script: %s\n' "$start_script"
printf 'Storage driver: %s\n' "$PANVEX_STORAGE_DRIVER"
printf 'Storage DSN: %s\n' "$PANVEX_STORAGE_DSN"

if [ "$PANVEX_INSTALL_SERVICE" = "1" ]; then
  printf 'Systemd service installed: %s\n' "$service_file"
  printf 'Next: bootstrap the first admin, then start the service.\n'
  printf '  %s/%s bootstrap-admin -storage-driver %s -storage-dsn %s -username admin -password <strong-password>\n' "$PANVEX_BIN_DIR" "$APP_NAME" "$PANVEX_STORAGE_DRIVER" "$PANVEX_STORAGE_DSN"
  printf '  systemctl start %s.service\n' "$SERVICE_NAME"
else
  printf 'Next: bootstrap the first admin, then run the start script.\n'
  printf '  %s/%s bootstrap-admin -storage-driver %s -storage-dsn %s -username admin -password <strong-password>\n' "$PANVEX_BIN_DIR" "$APP_NAME" "$PANVEX_STORAGE_DRIVER" "$PANVEX_STORAGE_DSN"
  printf '  %s\n' "$start_script"
fi
