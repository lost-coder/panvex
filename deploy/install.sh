#!/usr/bin/env bash
set -euo pipefail

# ─────────────────────────────────────────────────────────────────────────────
# Panvex Control Plane — Interactive Installer
# ─────────────────────────────────────────────────────────────────────────────

APP_NAME="panvex-control-plane"
SERVICE_NAME="panvex-control-plane"
REPO="${PANVEX_REPO:-lost-coder/panvex}"
INSTALL_LOG="/var/log/panvex-install-$(date +%Y%m%d-%H%M%S).log"

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
  echo "${DIM}  Control Plane Installer${RESET}"
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

ask_password() {
  local label=$1 value
  printf "  ${CYAN}?${RESET} %s: " "$label" >/dev/tty
  stty -echo 2>/dev/null || true
  IFS= read -r value </dev/tty || true
  stty echo 2>/dev/null || true
  echo "" >/dev/tty
  echo "$value"
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

has_ufw() { command -v ufw >/dev/null 2>&1; }

has_firewalld() { command -v firewall-cmd >/dev/null 2>&1; }

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) die "Unsupported architecture: $(uname -m)" ;;
  esac
}

check_port() {
  local port=$1
  if command -v ss >/dev/null 2>&1; then
    ss -tlnp 2>/dev/null | grep -q ":${port} " && return 1
  elif command -v netstat >/dev/null 2>&1; then
    netstat -tlnp 2>/dev/null | grep -q ":${port} " && return 1
  fi
  return 0
}

ask_port() {
  local label=$1 default=$2 port
  while true; do
    port=$(ask "$label" "$default")
    if ! echo "$port" | grep -qE '^[0-9]+$' || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
      echo "  ${YELLOW}!${RESET} Invalid port number. Enter a value between 1 and 65535." >/dev/tty
      continue
    fi
    if ! check_port "$port"; then
      local pid_info
      pid_info=$(ss -tlnp 2>/dev/null | grep ":${port} " | sed 's/.*users:(("\([^"]*\)".*/\1/' | head -1)
      if [ -n "$pid_info" ]; then
        echo "  ${YELLOW}!${RESET} Port $port is already in use by: ${BOLD}$pid_info${RESET}" >/dev/tty
      else
        echo "  ${YELLOW}!${RESET} Port $port is already in use by another process" >/dev/tty
      fi
      if ! ask_yesno "Choose a different port?" "y"; then
        echo "$port"
        return
      fi
      continue
    fi
    echo "$port"
    return
  done
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

# ── Firewall helper ──────────────────────────────────────────────────────────

open_port() {
  local port=$1
  if has_ufw; then
    ufw allow "$port/tcp" >/dev/null 2>&1 && success "Opened port $port (ufw)" && return
  fi
  if has_firewalld; then
    firewall-cmd --permanent --add-port="$port/tcp" >/dev/null 2>&1 &&
      firewall-cmd --reload >/dev/null 2>&1 &&
      success "Opened port $port (firewalld)" && return
  fi
  warn "Could not auto-configure firewall for port $port — open it manually"
}

# ── Health check ─────────────────────────────────────────────────────────────

wait_healthy() {
  local port=$1 timeout=${2:-30} waited=0
  info "Waiting for service to become healthy..."
  while [ $waited -lt $timeout ]; do
    if curl -sf "http://127.0.0.1:${port}/healthz" >/dev/null 2>&1; then
      success "Health check passed"
      return 0
    fi
    sleep 2
    waited=$((waited + 2))
    printf "\r  ${BLUE}●${RESET} Waiting... %ds/%ds" "$waited" "$timeout" >/dev/tty
  done
  echo "" >/dev/tty
  warn "Service not responding after ${timeout}s — check: journalctl -u ${SERVICE_NAME}"
  return 1
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
# Non-interactive mode
# ═════════════════════════════════════════════════════════════════════════════

run_noninteractive() {
  local arch
  arch=$(detect_arch)

  local version="${PANVEX_VERSION:-latest}"
  local bin_dir="${PANVEX_BIN_DIR:-/usr/local/bin}"
  local config_dir="${PANVEX_CONFIG_DIR:-/etc/panvex}"
  local data_dir="${PANVEX_DATA_DIR:-/var/lib/panvex}"
  local http_port="${PANVEX_HTTP_PORT:-8080}"
  local grpc_port="${PANVEX_GRPC_PORT:-8443}"
  local tls_mode="${PANVEX_TLS_MODE:-proxy}"
  local storage_driver="${PANVEX_STORAGE_DRIVER:-sqlite}"
  local storage_dsn="${PANVEX_STORAGE_DSN:-}"
  local encryption_key="${PANVEX_ENCRYPTION_KEY:-}"
  local admin_user="${PANVEX_ADMIN_USER:-admin}"
  local admin_pass="${PANVEX_ADMIN_PASS:-}"
  local open_fw="${PANVEX_OPEN_FIREWALL:-0}"
  local start_now="${PANVEX_START_NOW:-1}"

  [ -n "$admin_pass" ] || die "PANVEX_ADMIN_PASS is required for non-interactive install"

  if [ "$storage_driver" = "sqlite" ] && [ -z "$storage_dsn" ]; then
    storage_dsn="$data_dir/panvex.db"
  fi

  info "Non-interactive install: $version ($arch)"

  install_panvex "$arch" "$version" "$bin_dir" "$config_dir" "$data_dir" \
    "$http_port" "$grpc_port" "$tls_mode" "$storage_driver" "$storage_dsn" \
    "$encryption_key" "$admin_user" "$admin_pass" "$open_fw" "$start_now"
}

# ═════════════════════════════════════════════════════════════════════════════
# Core installation logic (shared by interactive and non-interactive)
# ═════════════════════════════════════════════════════════════════════════════

install_panvex() {
  local arch=$1 version=$2 bin_dir=$3 config_dir=$4 data_dir=$5
  local http_port=$6 grpc_port=$7 tls_mode=$8 storage_driver=$9
  local storage_dsn=${10} encryption_key=${11} admin_user=${12} admin_pass=${13}
  local open_fw=${14} start_now=${15}

  local config_file="$config_dir/config.toml"
  local is_upgrade=false
  local current_ver=""

  # ── Detect existing installation ─────────────────────────────────────
  if [ -f "$bin_dir/$APP_NAME" ]; then
    is_upgrade=true
    current_ver=$("$bin_dir/$APP_NAME" -version 2>/dev/null | head -1 || echo "unknown")
    warn "Existing installation detected: $current_ver"

    if [ -f "$config_file" ]; then
      local backup="${config_file}.bak.$(date +%s)"
      cp "$config_file" "$backup"
      success "Config backed up: $backup"
    fi

    systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
    info "Service stopped for upgrade"
  fi

  # ── Download ───────────────────────────────────────────────────────────
  step "Downloading"

  local asset_name="${APP_NAME}-linux-${arch}.tar.gz"
  local release_path="latest/download"
  if [ "$version" != "latest" ]; then
    release_path="download/control-plane/${version}"
  fi

  local archive_url="https://github.com/${REPO}/releases/${release_path}/${asset_name}"
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
  if ! id -u panvex >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin panvex
    success "Created system user: panvex"
  fi

  chown -R panvex:panvex "$data_dir"
  chown -R panvex:panvex "$config_dir"

  # ── Configuration (skip on upgrade if config exists) ───────────────────

  if [ "$is_upgrade" = true ] && [ -f "$config_file" ]; then
    info "Keeping existing config: $config_file"
  else
    cat >"$config_file" <<EOF
[storage]
driver = "${storage_driver}"
dsn = "${storage_dsn}"

[http]
listen_address = ":${http_port}"

[grpc]
listen_address = ":${grpc_port}"

[tls]
mode = "${tls_mode}"

[panel]
restart_mode = "supervised"
EOF
    chmod 0640 "$config_file"
    chown panvex:panvex "$config_file"
    success "Config written: $config_file"
  fi

  # Encryption key in separate secrets file
  if [ -n "$encryption_key" ]; then
    local secrets_file="$config_dir/secrets.env"
    echo "PANVEX_ENCRYPTION_KEY=${encryption_key}" >"$secrets_file"
    chmod 0600 "$secrets_file"
    chown panvex:panvex "$secrets_file"
    success "Encryption key stored: $secrets_file"
  fi

  # ── Systemd service ────────────────────────────────────────────────────

  local service_file="/etc/systemd/system/${SERVICE_NAME}.service"
  local env_file_directive=""
  if [ -n "$encryption_key" ]; then
    env_file_directive="EnvironmentFile=${config_dir}/secrets.env"
  fi

  cat >"$service_file" <<EOF
[Unit]
Description=Panvex Control Plane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=panvex
Group=panvex
WorkingDirectory=${data_dir}
ExecStart=${bin_dir}/${APP_NAME} -config ${config_file}
${env_file_directive}
Restart=on-failure
RestartSec=5
SuccessExitStatus=78
RestartForceExitStatus=78
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
echo "Uninstalling Panvex Control Plane..."
systemctl stop ${SERVICE_NAME} 2>/dev/null || true
systemctl disable ${SERVICE_NAME} 2>/dev/null || true
rm -f /etc/systemd/system/${SERVICE_NAME}.service
systemctl daemon-reload
rm -f "${bin_dir}/${APP_NAME}" "${uninstall_script}"
userdel panvex 2>/dev/null || true
echo ""
echo "Panvex removed. Data preserved at:"
echo "  Config: ${config_dir}"
echo "  Data:   ${data_dir}"
echo ""
echo "To remove all data: rm -rf ${config_dir} ${data_dir}"
UNINSTALL
  chmod 0755 "$uninstall_script"
  success "Uninstall script: $uninstall_script"

  # ── Bootstrap admin (skip on upgrade) ──────────────────────────────────

  if [ "$is_upgrade" = false ]; then
    info "Creating administrator account..."
    if "$bin_dir/$APP_NAME" bootstrap-admin \
      -storage-driver "$storage_driver" \
      -storage-dsn "$storage_dsn" \
      -username "$admin_user" \
      -password "$admin_pass" 2>&1; then
      success "Admin account created: $admin_user"
    else
      warn "Admin bootstrap failed (account may already exist)"
    fi
    # Fix ownership: bootstrap-admin runs as root and may create the SQLite
    # database file owned by root. The service runs as panvex and needs
    # read-write access.
    chown -R panvex:panvex "$data_dir"
  fi

  # ── Firewall ───────────────────────────────────────────────────────────

  if [ "$open_fw" = "1" ] || [ "$open_fw" = "y" ]; then
    open_port "$http_port"
    open_port "$grpc_port"
  fi

  # ── Start service ──────────────────────────────────────────────────────

  if [ "$start_now" = "1" ] || [ "$start_now" = "y" ]; then
    systemctl start "${SERVICE_NAME}.service"
    wait_healthy "$http_port" 30
  fi

  # ── Done ───────────────────────────────────────────────────────────────
  step "Installation Complete"

  local host_ip
  host_ip=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "127.0.0.1")

  summary_box \
    "Version:" "${installed_ver}" \
    "Web UI:" "http://${host_ip}:${http_port}" \
    "gRPC:" "${host_ip}:${grpc_port}" \
    "Login:" "${admin_user}" \
    "Config:" "${config_file}" \
    "Service:" "systemctl status ${SERVICE_NAME}" \
    "Uninstall:" "${uninstall_script}" \
    "Log:" "${INSTALL_LOG}"

  if [ "$is_upgrade" = true ]; then
    echo "  ${GREEN}${BOLD}Panvex upgraded successfully!${RESET}"
  else
    echo "  ${GREEN}${BOLD}Panvex is ready!${RESET}"
  fi
  echo ""
  echo "  Useful commands:"
  echo "    ${DIM}systemctl status ${SERVICE_NAME}${RESET}        — check status"
  echo "    ${DIM}journalctl -u ${SERVICE_NAME} -f${RESET}        — view logs"
  echo "    ${DIM}systemctl restart ${SERVICE_NAME}${RESET}        — restart"
  echo "    ${DIM}${uninstall_script}${RESET}  — uninstall"
  echo ""
}

# ═════════════════════════════════════════════════════════════════════════════
# Interactive mode
# ═════════════════════════════════════════════════════════════════════════════

run_interactive() {
  banner

  # ── Pre-flight checks ──────────────────────────────────────────────────
  case "$(uname -s)" in
    Linux) ;;
    *) die "Panvex supports Linux only" ;;
  esac

  if ! is_root; then
    die "Root privileges required. Run with: sudo bash install.sh"
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
  local config_dir="/etc/panvex"
  local data_dir="/var/lib/panvex"

  if [ "$mode" = "Advanced" ]; then
    step "Installation Paths"
    bin_dir=$(ask "Binary directory" "$bin_dir")
    config_dir=$(ask "Config directory" "$config_dir")
    data_dir=$(ask "Data directory" "$data_dir")
  fi

  # ── Network ────────────────────────────────────────────────────────────
  step "Network Configuration"

  local http_port grpc_port
  http_port=$(ask_port "Web interface port (HTTP)" "8080")
  grpc_port=$(ask_port "Agent gateway port (gRPC)" "8443")

  # ── TLS ────────────────────────────────────────────────────────────────
  local tls_mode="proxy"
  echo ""
  if ask_yesno "Will Panvex be behind a reverse proxy (nginx, Caddy, etc.)?" "y"; then
    tls_mode="proxy"
    info "TLS will be handled by your reverse proxy"
  else
    tls_mode="direct"
    info "Panvex will handle TLS directly"
    warn "You will need to provide TLS certificate files in the config"
  fi

  # ── Storage ────────────────────────────────────────────────────────────
  step "Storage"

  local storage_driver="sqlite"
  local storage_dsn=""

  if [ "$mode" = "Advanced" ]; then
    storage_driver=$(ask_choice "Storage driver" "sqlite" "sqlite" "postgres")
  fi

  case "$storage_driver" in
    sqlite)
      storage_dsn="$data_dir/panvex.db"
      info "SQLite database: $storage_dsn"
      ;;
    postgres)
      storage_dsn=$(ask "PostgreSQL DSN" "postgres://panvex:password@127.0.0.1:5432/panvex?sslmode=disable")
      ;;
  esac

  # ── Encryption key (advanced) ──────────────────────────────────────────
  local encryption_key=""
  if [ "$mode" = "Advanced" ]; then
    step "Security"
    if ask_yesno "Encrypt CA private key at rest?" "n"; then
      encryption_key=$(ask_password "Encryption passphrase")
      [ -n "$encryption_key" ] || die "Encryption passphrase cannot be empty"
    fi
  fi

  # ── Admin account ──────────────────────────────────────────────────────
  step "Administrator Account"

  local admin_user admin_pass admin_pass2
  admin_user=$(ask "Admin username" "admin")

  while true; do
    admin_pass=$(ask_password "Admin password (min 12 chars, mixed case + digit)")
    if [ ${#admin_pass} -lt 12 ]; then
      warn "Password must be at least 12 characters"
      continue
    fi
    admin_pass2=$(ask_password "Confirm password")
    if [ "$admin_pass" != "$admin_pass2" ]; then
      warn "Passwords do not match"
      continue
    fi
    break
  done

  # ── Confirmation ───────────────────────────────────────────────────────
  step "Review"

  summary_box \
    "Version:" "${version}" \
    "HTTP port:" "${http_port}" \
    "gRPC port:" "${grpc_port}" \
    "TLS mode:" "${tls_mode}" \
    "Storage:" "${storage_driver}" \
    "Admin user:" "${admin_user}" \
    "Binary:" "${bin_dir}/${APP_NAME}" \
    "Config:" "${config_dir}" \
    "Data:" "${data_dir}"

  if ! ask_yesno "Proceed with installation?" "y"; then
    info "Installation cancelled."
    exit 0
  fi

  # ── Firewall ───────────────────────────────────────────────────────────
  local open_fw="0"
  if has_ufw || has_firewalld; then
    echo ""
    if ask_yesno "Open ports ${http_port} and ${grpc_port} in firewall?" "y"; then
      open_fw="1"
    fi
  fi

  # ── Start? ─────────────────────────────────────────────────────────────
  local start_now="0"
  if ask_yesno "Start Panvex after installation?" "y"; then
    start_now="1"
  fi

  # ── Run installation ───────────────────────────────────────────────────
  install_panvex "$arch" "$version" "$bin_dir" "$config_dir" "$data_dir" \
    "$http_port" "$grpc_port" "$tls_mode" "$storage_driver" "$storage_dsn" \
    "$encryption_key" "$admin_user" "$admin_pass" "$open_fw" "$start_now"
}

# ═════════════════════════════════════════════════════════════════════════════
# Entry point
# ═════════════════════════════════════════════════════════════════════════════

if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
  cat <<'EOF'
Panvex Control Plane — Interactive Installer

Usage:
  sudo bash install.sh              Interactive installation wizard
  sudo bash install.sh --help       Show this help
  sudo bash install.sh --dry-run    Show what would be done without executing

Non-interactive mode (set environment variables):
  PANVEX_VERSION          Version tag (default: latest)
  PANVEX_HTTP_PORT        Web interface port (default: 8080)
  PANVEX_GRPC_PORT        gRPC port (default: 8443)
  PANVEX_STORAGE_DRIVER   sqlite or postgres (default: sqlite)
  PANVEX_STORAGE_DSN      Database connection string
  PANVEX_TLS_MODE         proxy or direct (default: proxy)
  PANVEX_ADMIN_USER       Admin username (default: admin)
  PANVEX_ADMIN_PASS       Admin password (required)
  PANVEX_ENCRYPTION_KEY   CA key encryption passphrase (optional)
  PANVEX_OPEN_FIREWALL    Open ports in firewall: 0 or 1 (default: 0)
  PANVEX_START_NOW        Start service after install: 0 or 1 (default: 1)
  PANVEX_BIN_DIR          Binary directory (default: /usr/local/bin)
  PANVEX_CONFIG_DIR       Config directory (default: /etc/panvex)
  PANVEX_DATA_DIR         Data directory (default: /var/lib/panvex)
  PANVEX_REPO             GitHub repo (default: lost-coder/panvex)
EOF
  exit 0
fi

if [ "${1:-}" = "--dry-run" ]; then
  echo "Dry-run mode: would install Panvex Control Plane"
  echo "  Arch: $(detect_arch)"
  echo "  Version: ${PANVEX_VERSION:-latest}"
  echo "  HTTP: :${PANVEX_HTTP_PORT:-8080}"
  echo "  gRPC: :${PANVEX_GRPC_PORT:-8443}"
  echo "  Storage: ${PANVEX_STORAGE_DRIVER:-sqlite}"
  echo "  TLS: ${PANVEX_TLS_MODE:-proxy}"
  echo "  Admin: ${PANVEX_ADMIN_USER:-admin}"
  echo "  Bin: ${PANVEX_BIN_DIR:-/usr/local/bin}"
  echo "  Config: ${PANVEX_CONFIG_DIR:-/etc/panvex}"
  echo "  Data: ${PANVEX_DATA_DIR:-/var/lib/panvex}"
  exit 0
fi

# Start installation log
mkdir -p "$(dirname "$INSTALL_LOG")" 2>/dev/null || true
exec > >(tee -a "$INSTALL_LOG") 2>&1

# Route to interactive or non-interactive
if can_prompt; then
  run_interactive
else
  run_noninteractive
fi
