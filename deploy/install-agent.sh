#!/usr/bin/env bash
set -euo pipefail

# ─────────────────────────────────────────────────────────────────────────────
# Panvex Agent — Interactive Installer
# ─────────────────────────────────────────────────────────────────────────────

APP_NAME="panvex-agent"
SERVICE_NAME="panvex-agent"
REPO="${PANVEX_REPO:-lost-coder/panvex}"
INSTALL_LOG="/var/log/panvex-agent-install-$(date +%Y%m%d-%H%M%S).log"

# ── Colors & formatting ─────────────────────────────────────────────────────

if [ -t 1 ] && command -v tput >/dev/null 2>&1; then
  BOLD=$(tput bold)
  DIM=$(tput dim)
  RESET=$(tput sgr0)
  RED=$(tput setaf 1)
  GREEN=$(tput setaf 2)
  YELLOW=$(tput setaf 3)
  BLUE=$(tput setaf 4)
  CYAN=$(tput setaf 6)
else
  BOLD="" DIM="" RESET="" RED="" GREEN="" YELLOW="" BLUE="" CYAN=""
fi

# ── Output helpers ───────────────────────────────────────────────────────────

banner() {
  echo ""
  echo "${CYAN}${BOLD}    ____                          ${RESET}"
  echo "${CYAN}${BOLD}   / __ \\____ _____  _   _____  __${RESET}"
  echo "${CYAN}${BOLD}  / /_/ / __ \`/ __ \\| | / / _ \\/ /${RESET}"
  echo "${CYAN}${BOLD} / ____/ /_/ / / / /| |/ /  __/ x /${RESET}"
  echo "${CYAN}${BOLD}/_/    \\__,_/_/ /_/ |___/\\___/_/\\_\\${RESET}"
  echo ""
  echo "${DIM}  Agent Installer${RESET}"
  echo ""
}

info()    { echo "  ${BLUE}●${RESET} $*"; }
success() { echo "  ${GREEN}✓${RESET} $*"; }
warn()    { echo "  ${YELLOW}!${RESET} $*"; }
error()   { echo "  ${RED}✗${RESET} $*" >&2; }
step()    { echo ""; echo "${BOLD}── $* ──${RESET}"; echo ""; }
die()     { error "$*"; exit 1; }

# ── Trap with exit code preservation ─────────────────────────────────────────

TMP_DIR=""
cleanup() {
  local code=$?
  [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR"
  exit $code
}
trap cleanup EXIT HUP INT TERM

# ── Prompts ──────────────────────────────────────────────────────────────────

can_prompt() { [ -t 0 ] || [ -r /dev/tty ]; }

ask() {
  local label=$1 default=${2:-} value
  if [ -n "$default" ]; then
    printf "  ${CYAN}?${RESET} %s ${DIM}[%s]${RESET}: " "$label" "$default" >/dev/tty
  else
    printf "  ${CYAN}?${RESET} %s: " "$label" >/dev/tty
  fi
  IFS= read -r value </dev/tty || true
  echo "${value:-$default}"
}

ask_yesno() {
  local label=$1 default=${2:-y} value
  local hint="Y/n"
  [ "$default" = "n" ] && hint="y/N"
  printf "  ${CYAN}?${RESET} %s ${DIM}[%s]${RESET}: " "$label" "$hint" >/dev/tty
  IFS= read -r value </dev/tty || true
  value=${value:-$default}
  case "$value" in
    [yY]*) return 0 ;;
    *) return 1 ;;
  esac
}

ask_choice() {
  local label=$1 default=$2
  shift 2
  local i=1 choice
  echo "  ${CYAN}?${RESET} $label" >/dev/tty
  for opt in "$@"; do
    if [ "$opt" = "$default" ]; then
      echo "    ${GREEN}$i)${RESET} $opt ${DIM}(default)${RESET}" >/dev/tty
    else
      echo "    ${DIM}$i)${RESET} $opt" >/dev/tty
    fi
    i=$((i + 1))
  done
  printf "    Choice: " >/dev/tty
  IFS= read -r choice </dev/tty || true
  if [ -z "$choice" ] || ! echo "$choice" | grep -qE '^[0-9]+$'; then
    echo "$default"
    return
  fi
  i=1
  for opt in "$@"; do
    if [ "$i" = "$choice" ]; then
      echo "$opt"
      return
    fi
    i=$((i + 1))
  done
  echo "$default"
}

# ── System checks ────────────────────────────────────────────────────────────

need_cmd() { command -v "$1" >/dev/null 2>&1 || die "Required command not found: $1"; }

is_root() { [ "$(id -u)" -eq 0 ]; }

has_systemd() { command -v systemctl >/dev/null 2>&1; }

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) die "Unsupported architecture: $(uname -m)" ;;
  esac
}

# ── Download with progress ───────────────────────────────────────────────────

download_file() {
  local url=$1 dest=$2
  if command -v curl >/dev/null 2>&1; then
    if [ -t 1 ]; then
      curl -fSL --progress-bar "$url" -o "$dest"
    else
      curl -fsSL "$url" -o "$dest"
    fi
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -q --show-progress -O "$dest" "$url" 2>/dev/null || wget -qO "$dest" "$url"
    return
  fi
  die "curl or wget is required"
}

# ── Summary box ──────────────────────────────────────────────────────────────

summary_box() {
  local width=60
  local border="${CYAN}$(printf '─%.0s' $(seq 1 $width))${RESET}"
  echo ""
  echo "  $border"
  while [ $# -gt 0 ]; do
    printf "  ${CYAN}│${RESET} %-28s %s\n" "$1" "$2"
    shift 2
  done
  echo "  $border"
  echo ""
}

# ═════════════════════════════════════════════════════════════════════════════
# Core installation logic
# ═════════════════════════════════════════════════════════════════════════════

install_agent() {
  local arch=$1 version=$2 bin_dir=$3 config_dir=$4 data_dir=$5
  local panel_url=$6 enrollment_token=$7 telemt_url=$8 telemt_auth=$9
  local node_name=${10} start_now=${11}

  local state_file="$data_dir/agent-state.json"
  local env_file="$config_dir/agent.env"
  local start_script="$config_dir/start-agent.sh"
  local is_upgrade=false
  local current_ver=""

  # ── Detect existing installation ─────────────────────────────────────
  if [ -f "$bin_dir/$APP_NAME" ]; then
    is_upgrade=true
    current_ver=$("$bin_dir/$APP_NAME" -version 2>/dev/null | head -1 || echo "unknown")
    warn "Existing installation detected: $current_ver"

    if [ -f "$env_file" ]; then
      local backup="${env_file}.bak.$(date +%s)"
      cp "$env_file" "$backup"
      success "Config backed up: $backup"
    fi

    systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
    info "Service stopped for upgrade"
  fi

  # ── Download ───────────────────────────────────────────────────────────
  step "Downloading"

  local asset_name="${APP_NAME}-linux-${arch}.tar.gz"

  # Resolve "latest" to the most recent agent/* tag via GitHub API
  if [ "$version" = "latest" ]; then
    info "Resolving latest agent version..."
    local releases_json
    releases_json=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases?per_page=20" 2>/dev/null) \
      || die "Failed to query GitHub releases API"
    local agent_tag
    agent_tag=$(echo "$releases_json" | grep -o '"tag_name":\s*"agent/[^"]*"' | head -1 | grep -o 'agent/[^"]*') \
      || die "No agent release found in repository"
    info "Latest agent release: $agent_tag"
    version="${agent_tag#agent/}"
  fi

  local archive_url="https://github.com/${REPO}/releases/download/agent/${version}/${asset_name}"
  TMP_DIR=$(mktemp -d)

  info "Downloading ${asset_name}..."
  download_file "$archive_url" "$TMP_DIR/$asset_name"

  # Checksum verification
  info "Verifying checksum..."
  download_file "${archive_url}.sha256" "$TMP_DIR/checksum"
  local expected_hash
  expected_hash=$(awk '{print $1}' "$TMP_DIR/checksum")
  local actual_hash
  actual_hash=$(sha256sum "$TMP_DIR/$asset_name" | awk '{print $1}')
  if [ "$expected_hash" != "$actual_hash" ]; then
    die "Checksum verification failed (expected: $expected_hash, got: $actual_hash)"
  fi
  success "Checksum verified"

  info "Extracting..."
  tar -xzf "$TMP_DIR/$asset_name" -C "$TMP_DIR"
  local binary_name="${APP_NAME}-linux-${arch}"
  [ -f "$TMP_DIR/$binary_name" ] || die "Archive does not contain $binary_name"
  success "Download complete"

  # ── Install binary ─────────────────────────────────────────────────────
  step "Installing"

  mkdir -p "$bin_dir" "$config_dir" "$data_dir"
  install -m 0755 "$TMP_DIR/$binary_name" "$bin_dir/$APP_NAME"
  success "Binary installed: $bin_dir/$APP_NAME"

  # Show installed version
  local installed_ver
  installed_ver=$("$bin_dir/$APP_NAME" -version 2>/dev/null | head -1 || echo "unknown")
  info "Version: $installed_ver"

  # Create system user if not exists
  if ! id -u panvex-agent >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin panvex-agent
    success "Created system user: panvex-agent"
  fi

  chown -R panvex-agent:panvex-agent "$data_dir"
  chown -R panvex-agent:panvex-agent "$config_dir"

  # ── Configuration ──────────────────────────────────────────────────────

  if [ "$is_upgrade" = true ] && [ -f "$env_file" ]; then
    info "Keeping existing config: $env_file"
  else
    cat >"$env_file" <<EOF
PANVEX_STATE_FILE=${state_file}
PANVEX_TELEMT_URL=${telemt_url}
PANVEX_TELEMT_AUTH=${telemt_auth}
EOF
    chmod 0640 "$env_file"
    chown panvex-agent:panvex-agent "$env_file"
    success "Config written: $env_file"
  fi

  cat >"$start_script" <<EOF
#!/usr/bin/env sh
set -eu
. "${env_file}"
exec "${bin_dir}/${APP_NAME}" \\
  -state-file "\$PANVEX_STATE_FILE" \\
  -telemt-url "\$PANVEX_TELEMT_URL" \\
  -telemt-auth "\$PANVEX_TELEMT_AUTH" \\
  -version "${installed_ver}"
EOF
  chmod 0755 "$start_script"

  # ── Bootstrap (enroll with panel) — skip on upgrade ────────────────────

  if [ "$is_upgrade" = false ]; then
    step "Enrolling with Panel"

    info "Bootstrapping agent..."
    if "$bin_dir/$APP_NAME" bootstrap \
      -panel-url "$panel_url" \
      -enrollment-token "$enrollment_token" \
      -state-file "$state_file" \
      -node-name "$node_name" 2>&1; then
      success "Agent enrolled successfully"
    else
      error "Enrollment failed — check panel URL and token"
      warn "Retry: $bin_dir/$APP_NAME bootstrap -panel-url $panel_url -enrollment-token <token> -state-file $state_file -node-name $node_name"
    fi

    chown panvex-agent:panvex-agent "$state_file" 2>/dev/null || true
  fi

  # ── Systemd service ────────────────────────────────────────────────────

  local service_file="/etc/systemd/system/${SERVICE_NAME}.service"
  cat >"$service_file" <<EOF
[Unit]
Description=Panvex Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=panvex-agent
Group=panvex-agent
WorkingDirectory=${data_dir}
ExecStart=${start_script}
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

# Security hardening
ProtectSystem=full
ProtectHome=true
NoNewPrivileges=true
PrivateTmp=true
ReadWritePaths=${data_dir} ${config_dir}

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}.service" >/dev/null
  success "Systemd service installed"

  # ── Generate uninstall script ──────────────────────────────────────────

  local uninstall_script="${bin_dir}/${APP_NAME}-uninstall.sh"
  cat >"$uninstall_script" <<UNINSTALL
#!/usr/bin/env bash
set -euo pipefail
echo "Uninstalling Panvex Agent..."
systemctl stop ${SERVICE_NAME} 2>/dev/null || true
systemctl disable ${SERVICE_NAME} 2>/dev/null || true
rm -f /etc/systemd/system/${SERVICE_NAME}.service
systemctl daemon-reload
rm -f "${bin_dir}/${APP_NAME}" "${uninstall_script}"
userdel panvex-agent 2>/dev/null || true
echo ""
echo "Panvex Agent removed. Data preserved at:"
echo "  Config: ${config_dir}"
echo "  Data:   ${data_dir}"
echo ""
echo "To remove all data: rm -rf ${config_dir} ${data_dir}"
UNINSTALL
  chmod 0755 "$uninstall_script"
  success "Uninstall script: $uninstall_script"

  # ── Start service ──────────────────────────────────────────────────────

  if [ "$start_now" = "1" ] || [ "$start_now" = "y" ]; then
    systemctl start "${SERVICE_NAME}.service"
    sleep 3
    if systemctl is-active --quiet "${SERVICE_NAME}.service"; then
      success "Agent is running"
    else
      warn "Service started but may not be healthy — check: journalctl -u ${SERVICE_NAME}"
    fi
  fi

  # ── Done ───────────────────────────────────────────────────────────────
  step "Installation Complete"

  summary_box \
    "Version:" "${installed_ver}" \
    "Node name:" "${node_name}" \
    "Panel:" "${panel_url}" \
    "Telemt:" "${telemt_url}" \
    "Config:" "${env_file}" \
    "State:" "${state_file}" \
    "Service:" "systemctl status ${SERVICE_NAME}" \
    "Uninstall:" "${uninstall_script}" \
    "Log:" "${INSTALL_LOG}"

  if [ "$is_upgrade" = true ]; then
    echo "  ${GREEN}${BOLD}Agent upgraded successfully!${RESET}"
  else
    echo "  ${GREEN}${BOLD}Agent is ready!${RESET}"
  fi
  echo ""
  echo "  Useful commands:"
  echo "    ${DIM}systemctl status ${SERVICE_NAME}${RESET}       — check status"
  echo "    ${DIM}journalctl -u ${SERVICE_NAME} -f${RESET}       — view logs"
  echo "    ${DIM}systemctl restart ${SERVICE_NAME}${RESET}       — restart"
  echo "    ${DIM}${uninstall_script}${RESET}  — uninstall"
  echo ""
}

# ═════════════════════════════════════════════════════════════════════════════
# Interactive mode
# ═════════════════════════════════════════════════════════════════════════════

run_interactive() {
  banner

  case "$(uname -s)" in
    Linux) ;;
    *) die "Panvex supports Linux only" ;;
  esac

  if ! is_root; then
    die "Root privileges required. Run with: sudo bash install-agent.sh"
  fi

  if ! has_systemd; then
    die "systemd is required for service management"
  fi

  need_cmd tar
  need_cmd mktemp
  need_cmd sha256sum

  if ! can_prompt; then
    die "Interactive terminal required. Use environment variables for non-interactive mode (see --help)."
  fi

  local arch
  arch=$(detect_arch)
  success "System: Linux $arch"
  success "Init system: systemd"

  # ── Installation mode ──────────────────────────────────────────────────
  step "Installation Mode"

  local mode
  mode=$(ask_choice "Select installation mode" "Standard" "Standard" "Advanced")

  # ── Version ────────────────────────────────────────────────────────────
  local version="latest"
  if [ "$mode" = "Advanced" ]; then
    version=$(ask "Version (tag or 'latest')" "latest")
  fi

  # ── Paths ──────────────────────────────────────────────────────────────
  local bin_dir="/usr/local/bin"
  local config_dir="/etc/panvex-agent"
  local data_dir="/var/lib/panvex-agent"

  if [ "$mode" = "Advanced" ]; then
    step "Installation Paths"
    bin_dir=$(ask "Binary directory" "$bin_dir")
    config_dir=$(ask "Config directory" "$config_dir")
    data_dir=$(ask "Data directory" "$data_dir")
  fi

  # ── Panel connection ───────────────────────────────────────────────────
  step "Panel Connection"

  local panel_url enrollment_token
  panel_url=$(ask "Panel URL (e.g. https://panel.example.com)" "")
  [ -n "$panel_url" ] || die "Panel URL is required"

  enrollment_token=$(ask "Enrollment token" "")
  [ -n "$enrollment_token" ] || die "Enrollment token is required"

  # ── Telemt connection ──────────────────────────────────────────────────
  step "Telemt Proxy"

  local telemt_url telemt_auth
  telemt_url=$(ask "Telemt API URL" "http://127.0.0.1:9091")
  telemt_auth=$(ask "Telemt authorization header (leave empty if not required)" "")

  # ── Node name ──────────────────────────────────────────────────────────
  local node_name
  node_name=$(ask "Node name" "$(hostname)")

  # ── Confirmation ───────────────────────────────────────────────────────
  step "Review"

  summary_box \
    "Version:" "${version}" \
    "Panel URL:" "${panel_url}" \
    "Node name:" "${node_name}" \
    "Telemt URL:" "${telemt_url}" \
    "Binary:" "${bin_dir}/${APP_NAME}" \
    "Config:" "${config_dir}" \
    "Data:" "${data_dir}"

  if ! ask_yesno "Proceed with installation?" "y"; then
    info "Installation cancelled."
    exit 0
  fi

  local start_now="0"
  if ask_yesno "Start agent after installation?" "y"; then
    start_now="1"
  fi

  # ── Run installation ───────────────────────────────────────────────────
  install_agent "$arch" "$version" "$bin_dir" "$config_dir" "$data_dir" \
    "$panel_url" "$enrollment_token" "$telemt_url" "$telemt_auth" \
    "$node_name" "$start_now"
}

# ═════════════════════════════════════════════════════════════════════════════
# Non-interactive mode
# ═════════════════════════════════════════════════════════════════════════════

run_noninteractive() {
  local arch
  arch=$(detect_arch)

  local version="${PANVEX_AGENT_VERSION:-latest}"
  local bin_dir="${PANVEX_BIN_DIR:-/usr/local/bin}"
  local config_dir="${PANVEX_CONFIG_DIR:-/etc/panvex-agent}"
  local data_dir="${PANVEX_DATA_DIR:-/var/lib/panvex-agent}"
  local panel_url="${PANVEX_PANEL_URL:-}"
  local enrollment_token="${PANVEX_ENROLLMENT_TOKEN:-}"
  local telemt_url="${PANVEX_TELEMT_URL:-http://127.0.0.1:9091}"
  local telemt_auth="${PANVEX_TELEMT_AUTH:-}"
  local node_name="${PANVEX_NODE_NAME:-$(hostname)}"
  local start_now="${PANVEX_START_NOW:-1}"

  [ -n "$panel_url" ] || die "PANVEX_PANEL_URL is required for non-interactive install"
  [ -n "$enrollment_token" ] || die "PANVEX_ENROLLMENT_TOKEN is required for non-interactive install"

  info "Non-interactive install: $version ($arch)"

  install_agent "$arch" "$version" "$bin_dir" "$config_dir" "$data_dir" \
    "$panel_url" "$enrollment_token" "$telemt_url" "$telemt_auth" \
    "$node_name" "$start_now"
}

# ═════════════════════════════════════════════════════════════════════════════
# Entry point
# ═════════════════════════════════════════════════════════════════════════════

if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
  cat <<'EOF'
Panvex Agent — Installer

Usage:
  sudo bash install-agent.sh              Interactive installation wizard
  sudo bash install-agent.sh --help       Show this help
  sudo bash install-agent.sh --dry-run    Show what would be done without executing

  # Automatic mode (from enrollment wizard):
  curl -fsSL .../install-agent.sh | sudo bash -s -- \
    --panel-url https://panel.example.com \
    --token <enrollment-token> \
    --node-name my-node

CLI arguments:
  --panel-url URL             Panel URL (required for automatic mode)
  --token TOKEN               Enrollment token (required for automatic mode)
  --node-name NAME            Node name (default: hostname)
  --telemt-url URL            Telemt API URL (default: http://127.0.0.1:9091)
  --telemt-auth HEADER        Telemt authorization header (optional)

Environment variables (alternative to CLI args):
  PANVEX_AGENT_VERSION      Version tag (default: latest)
  PANVEX_PANEL_URL          Panel URL (required)
  PANVEX_ENROLLMENT_TOKEN   Enrollment token (required)
  PANVEX_TELEMT_URL         Telemt API URL (default: http://127.0.0.1:9091)
  PANVEX_TELEMT_AUTH        Telemt authorization header (optional)
  PANVEX_NODE_NAME          Node name (default: hostname)
  PANVEX_START_NOW          Start service after install: 0 or 1 (default: 1)
  PANVEX_BIN_DIR            Binary directory (default: /usr/local/bin)
  PANVEX_CONFIG_DIR         Config directory (default: /etc/panvex-agent)
  PANVEX_DATA_DIR           Data directory (default: /var/lib/panvex-agent)
  PANVEX_REPO               GitHub repo (default: lost-coder/panvex)
EOF
  exit 0
fi

if [ "${1:-}" = "--dry-run" ]; then
  echo "Dry-run mode: would install Panvex Agent"
  echo "  Arch: $(detect_arch)"
  echo "  Version: ${PANVEX_AGENT_VERSION:-latest}"
  echo "  Panel: ${PANVEX_PANEL_URL:-<not set>}"
  echo "  Node: ${PANVEX_NODE_NAME:-$(hostname)}"
  echo "  Telemt: ${PANVEX_TELEMT_URL:-http://127.0.0.1:9091}"
  echo "  Bin: ${PANVEX_BIN_DIR:-/usr/local/bin}"
  echo "  Config: ${PANVEX_CONFIG_DIR:-/etc/panvex-agent}"
  echo "  Data: ${PANVEX_DATA_DIR:-/var/lib/panvex-agent}"
  exit 0
fi

# ── Parse CLI arguments ──────────────────────────────────────────────────────

_CLI_PANEL_URL=""
_CLI_ENROLLMENT_TOKEN=""
_CLI_NODE_NAME=""
_CLI_TELEMT_URL=""
_CLI_TELEMT_AUTH=""

while [ $# -gt 0 ]; do
  case "$1" in
    --panel-url)       _CLI_PANEL_URL="$2"; shift 2 ;;
    --token|--enrollment-token) _CLI_ENROLLMENT_TOKEN="$2"; shift 2 ;;
    --node-name)       _CLI_NODE_NAME="$2"; shift 2 ;;
    --telemt-url)      _CLI_TELEMT_URL="$2"; shift 2 ;;
    --telemt-auth)     _CLI_TELEMT_AUTH="$2"; shift 2 ;;
    *) shift ;;
  esac
done

# Export as env vars so both modes can use them
[ -n "$_CLI_PANEL_URL" ]         && export PANVEX_PANEL_URL="$_CLI_PANEL_URL"
[ -n "$_CLI_ENROLLMENT_TOKEN" ]  && export PANVEX_ENROLLMENT_TOKEN="$_CLI_ENROLLMENT_TOKEN"
[ -n "$_CLI_NODE_NAME" ]         && export PANVEX_NODE_NAME="$_CLI_NODE_NAME"
[ -n "$_CLI_TELEMT_URL" ]        && export PANVEX_TELEMT_URL="$_CLI_TELEMT_URL"
[ -n "$_CLI_TELEMT_AUTH" ]       && export PANVEX_TELEMT_AUTH="$_CLI_TELEMT_AUTH"

# Start installation log
mkdir -p "$(dirname "$INSTALL_LOG")" 2>/dev/null || true
exec > >(tee -a "$INSTALL_LOG") 2>&1

# Route: if required args passed via CLI/env → automatic, otherwise interactive
if [ -n "${PANVEX_PANEL_URL:-}" ] && [ -n "${PANVEX_ENROLLMENT_TOKEN:-}" ]; then
  run_noninteractive
elif can_prompt; then
  run_interactive
else
  die "Missing required arguments. Pass --panel-url and --token, or run interactively. See --help."
fi
