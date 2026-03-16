#!/usr/bin/env sh
set -eu

APP_NAME="panvex-agent"
SERVICE_NAME="panvex-agent"
REPO="${PANVEX_REPO:-panvex/panvex}"

usage() {
  cat <<'EOF'
Panvex agent installer

Usage:
  install-agent.sh [--help] [--panel-url <url>] [--enrollment-token <token>] [--version <version>] [--telemt-url <url>] [--telemt-auth <value>]

Defaults:
  PANVEX_AGENT_VERSION=latest
  PANVEX_BIN_DIR=/usr/local/bin
  PANVEX_CONFIG_DIR=/etc/panvex-agent
  PANVEX_DATA_DIR=/var/lib/panvex-agent
  PANVEX_STATE_FILE=$PANVEX_DATA_DIR/agent-state.json
  PANVEX_TELEMT_URL=http://127.0.0.1:9091
  PANVEX_TELEMT_AUTH=<empty>

Examples:
  curl -fsSL https://github.com/panvex/panvex/releases/latest/download/install-agent.sh | \
    sudo sh -s -- \
      --panel-url https://panel.example.com \
      --enrollment-token <token>

  PANVEX_AGENT_VERSION=v1.2.3 \
  curl -fsSL https://github.com/panvex/panvex/releases/latest/download/install-agent.sh | \
    sudo sh -s -- \
      --panel-url https://panel.example.com \
      --enrollment-token <token> \
      --telemt-url http://127.0.0.1:9091
EOF
}

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

can_prompt() {
  [ -r /dev/tty ] && [ -w /dev/tty ]
}

prompt_required() {
  label=$1
  default_value=$2

  if can_prompt; then
    if [ -n "$default_value" ]; then
      printf '%s [%s]: ' "$label" "$default_value" >/dev/tty
    else
      printf '%s: ' "$label" >/dev/tty
    fi
    IFS= read -r value </dev/tty || true
    if [ -z "$value" ]; then
      value=$default_value
    fi
  else
    value=$default_value
  fi

  [ -n "$value" ] || die "$label is required"
  printf '%s\n' "$value"
}

prompt_optional() {
  label=$1
  default_value=$2

  if can_prompt; then
    if [ -n "$default_value" ]; then
      printf '%s [%s]: ' "$label" "$default_value" >/dev/tty
    else
      printf '%s: ' "$label" >/dev/tty
    fi
    IFS= read -r value </dev/tty || true
    if [ -z "$value" ]; then
      value=$default_value
    fi
  else
    value=$default_value
  fi

  printf '%s\n' "$value"
}

PANVEX_AGENT_VERSION="${PANVEX_AGENT_VERSION:-latest}"
PANVEX_BIN_DIR="${PANVEX_BIN_DIR:-/usr/local/bin}"
PANVEX_CONFIG_DIR="${PANVEX_CONFIG_DIR:-/etc/panvex-agent}"
PANVEX_DATA_DIR="${PANVEX_DATA_DIR:-/var/lib/panvex-agent}"
PANVEX_STATE_FILE="${PANVEX_STATE_FILE:-$PANVEX_DATA_DIR/agent-state.json}"
PANVEX_PANEL_URL="${PANVEX_PANEL_URL:-}"
PANVEX_ENROLLMENT_TOKEN="${PANVEX_ENROLLMENT_TOKEN:-}"
PANVEX_TELEMT_URL="${PANVEX_TELEMT_URL:-}"
PANVEX_TELEMT_AUTH="${PANVEX_TELEMT_AUTH:-}"

while [ $# -gt 0 ]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    --panel-url)
      [ $# -ge 2 ] || die "--panel-url requires a value"
      PANVEX_PANEL_URL=$2
      shift 2
      ;;
    --enrollment-token)
      [ $# -ge 2 ] || die "--enrollment-token requires a value"
      PANVEX_ENROLLMENT_TOKEN=$2
      shift 2
      ;;
    --version)
      [ $# -ge 2 ] || die "--version requires a value"
      PANVEX_AGENT_VERSION=$2
      shift 2
      ;;
    --telemt-url)
      [ $# -ge 2 ] || die "--telemt-url requires a value"
      PANVEX_TELEMT_URL=$2
      shift 2
      ;;
    --telemt-auth)
      [ $# -ge 2 ] || die "--telemt-auth requires a value"
      PANVEX_TELEMT_AUTH=$2
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

is_root || die "install-agent.sh requires root privileges"
has_systemd || die "install-agent.sh requires systemd"

need_cmd install
need_cmd mktemp
need_cmd sed
need_cmd tar

os=$(detect_os)
arch=$(detect_arch)
asset_name="$APP_NAME-$os-$arch.tar.gz"

release_path="latest/download"
if [ "$PANVEX_AGENT_VERSION" != "latest" ]; then
  release_path="download/$PANVEX_AGENT_VERSION"
fi

archive_url="https://github.com/$REPO/releases/$release_path/$asset_name"
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

archive_path="$tmp_dir/$asset_name"
download_file "$archive_url" "$archive_path"
tar -xzf "$archive_path" -C "$tmp_dir"

binary_path="$tmp_dir/$APP_NAME"
[ -f "$binary_path" ] || die "release archive did not contain $APP_NAME"

PANVEX_PANEL_URL=$(prompt_required "Panel URL" "$PANVEX_PANEL_URL")
PANVEX_ENROLLMENT_TOKEN=$(prompt_required "Enrollment token" "$PANVEX_ENROLLMENT_TOKEN")
if [ -z "$PANVEX_TELEMT_URL" ]; then
  PANVEX_TELEMT_URL=$(prompt_optional "Telemt API URL" "http://127.0.0.1:9091")
fi
if [ -z "$PANVEX_TELEMT_AUTH" ]; then
  PANVEX_TELEMT_AUTH=$(prompt_optional "Telemt authorization (leave empty if not required)" "")
fi

mkdir -p "$PANVEX_BIN_DIR" "$PANVEX_CONFIG_DIR" "$PANVEX_DATA_DIR"
install -m 0755 "$binary_path" "$PANVEX_BIN_DIR/$APP_NAME"

env_file="$PANVEX_CONFIG_DIR/agent.env"
start_script="$PANVEX_CONFIG_DIR/start-agent.sh"
service_file="/etc/systemd/system/$SERVICE_NAME.service"

{
  printf 'PANVEX_PANEL_URL=%s\n' "$(quote_sh "$PANVEX_PANEL_URL")"
  printf 'PANVEX_STATE_FILE=%s\n' "$(quote_sh "$PANVEX_STATE_FILE")"
  printf 'PANVEX_TELEMT_URL=%s\n' "$(quote_sh "$PANVEX_TELEMT_URL")"
  printf 'PANVEX_TELEMT_AUTH=%s\n' "$(quote_sh "$PANVEX_TELEMT_AUTH")"
  printf 'PANVEX_AGENT_VERSION=%s\n' "$(quote_sh "$PANVEX_AGENT_VERSION")"
} >"$env_file"

cat >"$start_script" <<EOF
#!/usr/bin/env sh
set -eu
. "$env_file"
exec "$PANVEX_BIN_DIR/$APP_NAME" \\
  -state-file "\$PANVEX_STATE_FILE" \\
  -telemt-url "\$PANVEX_TELEMT_URL" \\
  -telemt-auth "\$PANVEX_TELEMT_AUTH"
EOF
chmod 0755 "$start_script"

"$PANVEX_BIN_DIR/$APP_NAME" bootstrap \
  -panel-url "$PANVEX_PANEL_URL" \
  -enrollment-token "$PANVEX_ENROLLMENT_TOKEN" \
  -state-file "$PANVEX_STATE_FILE"

cat >"$service_file" <<EOF
[Unit]
Description=Panvex Agent
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
systemctl enable --now "$SERVICE_NAME.service" >/dev/null

printf 'Panvex agent installed.\n'
printf 'Binary: %s/%s\n' "$PANVEX_BIN_DIR" "$APP_NAME"
printf 'Config: %s\n' "$env_file"
printf 'State: %s\n' "$PANVEX_STATE_FILE"
printf 'Systemd service: %s\n' "$service_file"
printf 'Telemt API URL: %s\n' "$PANVEX_TELEMT_URL"
