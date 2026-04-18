#!/usr/bin/env bash
set -euo pipefail

# ─────────────────────────────────────────────────────────────────────────────
# Panvex Control Plane — Interactive Installer
# ─────────────────────────────────────────────────────────────────────────────

APP_NAME="panvex-control-plane"
SERVICE_NAME="panvex-control-plane"
REPO="${PANVEX_REPO:-lost-coder/panvex}"

# ── Colors ──────────────────────────────────────────────────────────────────

BOLD=$'\033[1m'
DIM=$'\033[2m'
RESET=$'\033[0m'
RED=$'\033[0;31m'
GREEN=$'\033[0;32m'
YELLOW=$'\033[0;33m'
BLUE=$'\033[0;34m'
CYAN=$'\033[0;36m'

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

can_prompt() { [ -t 0 ]; }

ask() {
  local label=$1 default=${2:-} value
  if [ -n "$default" ]; then
    read -rp "  ${CYAN}?${RESET} $label ${DIM}[$default]${RESET}: " value </dev/tty
  else
    read -rp "  ${CYAN}?${RESET} $label: " value </dev/tty
  fi
  echo "${value:-$default}"
}

ask_password() {
  local label=$1 value
  read -rsp "  ${CYAN}?${RESET} $label: " value </dev/tty
  echo "" >&2
  echo "$value"
}

ask_yesno() {
  local label=$1 default=${2:-y} value
  local hint="Y/n"
  [ "$default" = "n" ] && hint="y/N"
  read -rp "  ${CYAN}?${RESET} $label ${DIM}[$hint]${RESET}: " value </dev/tty
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
  echo "  ${CYAN}?${RESET} $label" >&2
  for opt in "$@"; do
    if [ "$opt" = "$default" ]; then
      echo "    ${GREEN}$i)${RESET} $opt ${DIM}(default)${RESET}" >&2
    else
      echo "    ${DIM}$i)${RESET} $opt" >&2
    fi
    i=$((i + 1))
  done
  read -rp "    Choice: " choice </dev/tty
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

# ── Download ─────────────────────────────────────────────────────────────────

download_file() {
  local url=$1 dest=$2
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
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

check_port_free() {
  local port=$1
  if ss -tlnp 2>/dev/null | grep -q ":${port} "; then
    local proc
    proc=$(ss -tlnp 2>/dev/null | grep ":${port} " | grep -oP 'users:\(\("\K[^"]+' | head -1)
    echo "$proc"
    return 1
  fi
  return 0
}

generate_random_path() {
  local path
  path=$(head -c 6 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=' | head -c 8)
  echo "/$path"
}

install_acme_sh() {
  info "Installing acme.sh..."
  curl -fsSL https://get.acme.sh | sh -s -- >/dev/null 2>&1
  if [ ! -f "$HOME/.acme.sh/acme.sh" ]; then
    die "acme.sh installation failed"
  fi
  success "acme.sh installed"
}

# is_ip_address returns 0 if the argument looks like an IPv4 address.
is_ip_address() {
  echo "$1" | grep -qP '^\d+\.\d+\.\d+\.\d+$'
}

issue_acme_certificate() {
  local domain=$1 email=$2 tls_dir=$3

  mkdir -p "$tls_dir"

  local email_flag=""
  if [ -n "$email" ]; then
    email_flag="--accountemail $email"
  fi

  # IP addresses require the shortlived certificate profile (6-day certs).
  local profile_flag="" days_flag=""
  if is_ip_address "$domain"; then
    profile_flag="--certificate-profile shortlived"
    days_flag="--days 3"
    info "Using short-lived certificate profile for IP address (valid ~6 days, auto-renews)"
  fi

  # Build firewall hooks to temporarily open/close port 80 for the ACME challenge.
  # Only add close hook if port 80 was NOT already open before we started.
  local pre_hook="" post_hook=""
  local port80_was_open=false

  # Check if port 80 is already allowed in firewall
  if has_ufw; then
    if ufw status 2>/dev/null | grep -q "80/tcp.*ALLOW"; then
      port80_was_open=true
    fi
    pre_hook="--pre-hook 'ufw allow 80/tcp >/dev/null 2>&1'"
    if [ "$port80_was_open" = false ]; then
      post_hook="--post-hook 'ufw delete allow 80/tcp >/dev/null 2>&1'"
      info "Firewall (ufw): port 80 will be opened temporarily for verification"
    else
      info "Firewall (ufw): port 80 is already open"
    fi
  elif has_firewalld; then
    if firewall-cmd --query-port=80/tcp >/dev/null 2>&1; then
      port80_was_open=true
    fi
    pre_hook="--pre-hook 'firewall-cmd --add-port=80/tcp >/dev/null 2>&1'"
    if [ "$port80_was_open" = false ]; then
      post_hook="--post-hook 'firewall-cmd --remove-port=80/tcp >/dev/null 2>&1'"
      info "Firewall (firewalld): port 80 will be opened temporarily for verification"
    else
      info "Firewall (firewalld): port 80 is already open"
    fi
  elif command -v iptables >/dev/null 2>&1; then
    if iptables -C INPUT -p tcp --dport 80 -j ACCEPT >/dev/null 2>&1; then
      port80_was_open=true
    fi
    pre_hook="--pre-hook 'iptables -I INPUT -p tcp --dport 80 -j ACCEPT 2>/dev/null'"
    if [ "$port80_was_open" = false ]; then
      post_hook="--post-hook 'iptables -D INPUT -p tcp --dport 80 -j ACCEPT 2>/dev/null'"
      info "Firewall (iptables): port 80 will be opened temporarily for verification"
    else
      info "Firewall (iptables): port 80 is already open"
    fi
  fi

  info "Requesting certificate for ${domain}..."

  local acme_exit=0
  local acme_cmd="\"$HOME/.acme.sh/acme.sh\" --issue --standalone -d \"$domain\" --httpport 80 --server letsencrypt $email_flag $profile_flag $days_flag $pre_hook $post_hook"
  eval "$acme_cmd" 2>&1 || acme_exit=$?

  # Exit code 0 = issued, 2 = skipped (already valid, not due for renewal) — both OK.
  if [ "$acme_exit" -ne 0 ] && [ "$acme_exit" -ne 2 ]; then
    die "Certificate issuance failed (exit code $acme_exit). Check that port 80 is reachable and ${domain} resolves to this server."
  fi

  if [ "$acme_exit" -eq 2 ]; then
    info "Certificate already exists and is still valid — reusing."
  fi

  info "Installing certificate files..."
  "$HOME/.acme.sh/acme.sh" --install-cert -d "$domain" \
    --fullchain-file "$tls_dir/fullchain.pem" \
    --key-file "$tls_dir/key.pem" >/dev/null 2>&1

  success "Certificate ready: $tls_dir/"
}

# ── Health check ─────────────────────────────────────────────────────────────

wait_healthy() {
  local port=$1 timeout=${2:-60} scheme=${3:-http} path_prefix=${4:-} waited=0
  local url="${scheme}://127.0.0.1:${port}${path_prefix}/healthz"
  info "Waiting for service (${url})..."
  while [ $waited -lt $timeout ]; do
    if curl -sfk --max-time 3 "$url" >/dev/null 2>&1; then
      echo ""
      success "Health check passed"
      return 0
    fi
    sleep 2
    waited=$((waited + 2))
    printf "\r  ${BLUE}●${RESET} Waiting... %ds/%ds" "$waited" "$timeout"
  done
  echo ""
  warn "Service not responding after ${timeout}s"
  warn "Try manually: curl -k ${url}"
  warn "Check logs: journalctl -u ${SERVICE_NAME} -n 20"
  return 1
}

# ── Summary box ──────────────────────────────────────────────────────────────

summary_box() {
  local width=60
  local border
  border=$(printf '─%.0s' $(seq 1 $width))
  echo ""
  echo "  ${CYAN}${border}${RESET}"
  while [ $# -gt 0 ]; do
    printf "  ${CYAN}│${RESET} %-28s %s\n" "$1" "$2"
    shift 2
  done
  echo "  ${CYAN}${border}${RESET}"
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
  local panel_path=${16} agent_path=${17} panel_allowed_cidrs=${18}
  local tls_cert_file=${19} tls_key_file=${20}

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

  # Tighten TLS key permissions if cert was provisioned
  if [ -f "$data_dir/tls/key.pem" ]; then
    chmod 0600 "$data_dir/tls/key.pem"
    chmod 0644 "$data_dir/tls/fullchain.pem"
  fi

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
root_path = "${panel_path}"
agent_root_path = "${agent_path}"
panel_allowed_cidrs = [$(echo "$panel_allowed_cidrs" | tr ',' '\n' | sed 's/^[[:space:]]*//' | sed 's/[[:space:]]*$//' | sed '/^$/d' | sed 's/.*/"&"/' | paste -sd, -)]

[grpc]
listen_address = ":${grpc_port}"

[tls]
mode = "${tls_mode}"
cert_file = "${tls_cert_file}"
key_file = "${tls_key_file}"

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

  local cap_directive=""
  if [ "$http_port" -lt 1024 ] || [ "$grpc_port" -lt 1024 ]; then
    cap_directive="AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE"
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
${cap_directive}

# Security hardening
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ReadWritePaths=${data_dir} ${config_dir}

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}.service" >/dev/null
  success "Systemd service installed"

  # ── Configure acme.sh reload hook (now that service exists) ──────────
  if [ -n "$tls_cert_file" ] && [ -f "$HOME/.acme.sh/acme.sh" ]; then
    # Extract domain from cert path: /var/lib/panvex/tls/fullchain.pem → look up from acme.sh
    local acme_domain=""
    acme_domain=$("$HOME/.acme.sh/acme.sh" --list 2>/dev/null | tail -1 | awk '{print $1}')
    if [ -n "$acme_domain" ]; then
      local tls_dir
      tls_dir=$(dirname "$tls_cert_file")
      "$HOME/.acme.sh/acme.sh" --install-cert -d "$acme_domain" \
        --fullchain-file "$tls_dir/fullchain.pem" \
        --key-file "$tls_dir/key.pem" \
        --reloadcmd "systemctl reload ${SERVICE_NAME} 2>/dev/null || systemctl restart ${SERVICE_NAME}" \
        >/dev/null 2>&1
      success "Certificate renewal will auto-restart ${SERVICE_NAME}"
    fi
  fi

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

  # Ensure service is stopped before bootstrap (avoids SQLite lock)
  systemctl stop "${SERVICE_NAME}" 2>/dev/null || true

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
    local health_scheme="http"
    [ "$tls_mode" = "direct" ] && health_scheme="https"
    wait_healthy "$http_port" 15 "$health_scheme" "$panel_path"
  fi

  # ── Done ───────────────────────────────────────────────────────────────
  step "Installation Complete"

  local host_ip
  host_ip=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "127.0.0.1")

  local ui_scheme="http"
  [ "$tls_mode" = "direct" ] && ui_scheme="https"

  summary_box \
    "Version:" "${installed_ver}" \
    "Web UI:" "${ui_scheme}://${host_ip}:${http_port}${panel_path}/" \
    "gRPC:" "${host_ip}:${grpc_port}" \
    "Login:" "${admin_user}" \
    "Config:" "${config_file}" \
    "Service:" "systemctl status ${SERVICE_NAME}" \
    "Uninstall:" "${uninstall_script}" \

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

  local version="latest"
  local bin_dir="/usr/local/bin"
  local config_dir="/etc/panvex"
  local data_dir="/var/lib/panvex"
  local storage_driver="sqlite"
  local storage_dsn=""
  local encryption_key=""

  if [ "$mode" = "Advanced" ]; then
    version=$(ask "Version (tag or 'latest')" "latest")
    bin_dir=$(ask "Binary directory" "$bin_dir")
    config_dir=$(ask "Config directory" "$config_dir")
    data_dir=$(ask "Data directory" "$data_dir")
    storage_driver=$(ask_choice "Storage driver" "sqlite" "sqlite" "postgres")
  fi

  case "$storage_driver" in
    sqlite) storage_dsn="$data_dir/panvex.db" ;;
    postgres) storage_dsn=$(ask "PostgreSQL DSN" "postgres://panvex:password@127.0.0.1:5432/panvex?sslmode=disable") ;;
  esac

  if [ "$mode" = "Advanced" ]; then
    if ask_yesno "Encrypt CA private key at rest?" "n"; then
      encryption_key=$(ask_password "Encryption passphrase")
      [ -n "$encryption_key" ] || die "Encryption passphrase cannot be empty"
    fi
  fi

  # ── 1. TLS ─────────────────────────────────────────────────────────────
  step "1. HTTPS Certificate"

  local tls_mode="proxy"
  local tls_cert_file="" tls_key_file=""
  local http_port="" grpc_port=""
  local server_ip
  server_ip=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "")

  tls_mode=$(ask_choice "How to handle HTTPS" "Automatic (Let's Encrypt)" \
    "Automatic (Let's Encrypt)" "Manual (own certificate)" "None / behind reverse proxy")

  case "$tls_mode" in
    "Automatic (Let's Encrypt)")
      tls_mode="direct"

      local acme_domain acme_email
      acme_domain=$(ask "Panel domain or IP (Enter = ${server_ip})" "")
      if [ -z "$acme_domain" ]; then
        acme_domain="$server_ip"
      fi

      if [ -z "$acme_domain" ]; then
        die "Could not detect server IP. Please provide a domain or IP."
      fi

      if is_ip_address "$acme_domain"; then
        warn "IP certificates are short-lived (~6 days) and renew automatically."
        warn "A domain name is recommended for production use."
      fi

      local port80_proc
      if ! port80_proc=$(check_port_free 80); then
        warn "Port 80 is in use by: ${port80_proc:-unknown}"
        warn "It must be free during certificate issuance (a few seconds)."
        if ! ask_yesno "Continue anyway? (port 80 will be needed briefly)" "y"; then
          die "Free port 80 and re-run the installer."
        fi
      else
        success "Port 80 is available"
      fi

      acme_email=$(ask "Email for certificate notifications (optional, Enter to skip)" "")

      local tls_dir="$data_dir/tls"
      install_acme_sh
      issue_acme_certificate "$acme_domain" "$acme_email" "$tls_dir"

      tls_cert_file="$tls_dir/fullchain.pem"
      tls_key_file="$tls_dir/key.pem"

      http_port="443"
      grpc_port="8443"
      ;;
    "Manual (own certificate)")
      tls_mode="direct"
      info "After installation, set cert_file and key_file in config.toml"
      http_port="443"
      grpc_port="8443"
      ;;
    "None / behind reverse proxy")
      tls_mode="proxy"
      info "TLS will be handled by your reverse proxy"
      http_port="8080"
      grpc_port="8443"
      ;;
  esac

  # ── 2. Ports ───────────────────────────────────────────────────────────
  step "2. Network"

  http_port=$(ask_port "Panel port" "$http_port")
  grpc_port=$(ask_port "Agent gateway port (gRPC)" "$grpc_port")

  local open_fw="0"
  if has_ufw || has_firewalld; then
    if ask_yesno "Open ports ${http_port} and ${grpc_port} in firewall?" "y"; then
      open_fw="1"
    fi
  fi

  # ── 3. Access control ──────────────────────────────────────────────────
  step "3. Access Control"

  local panel_path="" agent_path="" panel_allowed_cidrs=""

  info "Hide panel and agent API behind secret URL paths."
  info "Anyone who doesn't know the path gets a 404."

  panel_path=$(ask "Panel path (Enter = auto-generate)" "")
  if [ -z "$panel_path" ]; then
    panel_path=$(generate_random_path)
  fi

  agent_path=$(ask "Agent API path (Enter = auto-generate)" "")
  if [ -z "$agent_path" ]; then
    agent_path=$(generate_random_path)
  fi

  if ask_yesno "Restrict panel to specific IPs?" "n"; then
    panel_allowed_cidrs=$(ask "Allowed CIDRs (comma-separated, e.g. 10.0.0.0/8)" "")
  fi

  # ── 4. Admin account ──────────────────────────────────────────────────
  step "4. Administrator"

  local admin_user admin_pass admin_pass2
  admin_user=$(ask "Username" "admin")

  while true; do
    admin_pass=$(ask_password "Password")
    if [ -z "$admin_pass" ]; then
      warn "Password cannot be empty"
      continue
    fi
    admin_pass2=$(ask_password "Confirm password")
    if [ "$admin_pass" != "$admin_pass2" ]; then
      warn "Passwords do not match"
      continue
    fi
    break
  done

  # ── Review ─────────────────────────────────────────────────────────────
  step "Review"

  local panel_url_display
  if [ "$tls_mode" = "direct" ]; then
    panel_url_display="https://${server_ip}:${http_port}${panel_path}/"
  else
    panel_url_display="http://${server_ip}:${http_port}${panel_path}/"
  fi

  summary_box \
    "Panel URL:" "${panel_url_display}" \
    "gRPC:" ":${grpc_port}" \
    "TLS:" "${tls_mode}" \
    "Panel path:" "${panel_path}" \
    "Agent path:" "${agent_path}" \
    "IP whitelist:" "${panel_allowed_cidrs:-any}" \
    "Admin:" "${admin_user}"

  if ! ask_yesno "Proceed with installation?" "y"; then
    info "Installation cancelled."
    exit 0
  fi

  local start_now="0"
  if ask_yesno "Start Panvex after installation?" "y"; then
    start_now="1"
  fi

  # ── Run installation ───────────────────────────────────────────────────
  install_panvex "$arch" "$version" "$bin_dir" "$config_dir" "$data_dir" \
    "$http_port" "$grpc_port" "$tls_mode" "$storage_driver" "$storage_dsn" \
    "$encryption_key" "$admin_user" "$admin_pass" "$open_fw" "$start_now" \
    "$panel_path" "$agent_path" "$panel_allowed_cidrs" \
    "$tls_cert_file" "$tls_key_file"
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

# Route to interactive or non-interactive
if can_prompt; then
  run_interactive
else
  run_noninteractive
fi
