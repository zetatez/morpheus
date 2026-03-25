#!/bin/bash

set -e

# Morpheus CLI Installer
# Usage: curl -sSL https://raw.githubusercontent.com/anomalyco/morph/main/install.sh | bash

VERSION="0.1.0"
BINARY_NAME="morph"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)     echo "linux";;
        Darwin*)    echo "darwin";;
        CYGWIN*)    echo "windows";;
        MINGW*)     echo "windows";;
        *)          echo "unknown";;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64)    echo "amd64";;
        aarch64)   echo "arm64";;
        armv7)     echo "armv7";;
        *)         echo "amd64";;
    esac
}

# Get the directory where the script is running
get_install_dir() {
    if [ -z "$INSTALL_DIR" ]; then
        # Default to /usr/local/bin for system-wide or ~/.local/bin for user
        if [ "$(id -u)" = "0" ]; then
            echo "/usr/local/bin"
        else
            echo "$HOME/.local/bin"
        fi
    fi
}

# Get config directory
get_config_dir() {
    if [ -z "$CONFIG_DIR" ]; then
        echo "$HOME/.config/morph"
    fi
}

# Get data directory
get_data_dir() {
    echo "$HOME/.config/morph"
}

# Download and install binary
install_binary() {
    local os=$1
    local arch=$2
    local install_dir=$3

    local download_url="https://github.com/zetatez/morpheus/releases/download/v${VERSION}/${BINARY_NAME}-${os}-${arch}"

    log_info "Downloading Morpheus v${VERSION} for ${os}-${arch}..."

    if command -v curl &> /dev/null; then
        curl -sSL "$download_url" -o "${install_dir}/${BINARY_NAME}"
    elif command -v wget &> /dev/null; then
        wget -q "$download_url" -O "${install_dir}/${BINARY_NAME}"
    else
        log_error "Neither curl nor wget found. Please install one of them."
        exit 1
    fi

    chmod +x "${install_dir}/${BINARY_NAME}"
    log_info "Binary installed to ${install_dir}/${BINARY_NAME}"
}

# Install config file
install_config() {
    local config_dir=$1
    local config_file="${config_dir}/config.yaml"

    # Create config directory
    mkdir -p "$config_dir"

    if [ -f "$config_file" ]; then
        log_warn "Config file already exists at ${config_file}"
        log_info "Skipping config installation"
    else
        # Create default config
        cat > "$config_file" << 'EOF'
workspace_root: ~/

logging:
  level: info
  file: ~/.config/morph/logs/morph.log
  audit: ~/.config/morph/logs/audit.log

planner:
  provider: openai
  model: gpt-4o-mini
  temperature: 0.2
  # api_key: env:OPENAI_API_KEY

server:
  listen: :8080

# Session memory (per-session storage)
session:
  path: ~/.config/morph/sessions
  retention: 720h

# RAG (cross-session semantic search)
rag:
  enabled: false
  path: ~/.config/morph/rag

permissions:
  # Risk level >= this value requires user confirmation
  confirm_above: high

  # Protected paths - all operations require confirmation
  confirm_protected_paths:
    # System critical directories
    - /etc
    - /usr/bin
    - /usr/sbin
    - /usr/local/bin
    - /usr/local/sbin
    - /var
    - /var/log
    - /var/etc
    - /boot
    - /sys
    - /proc
    - /dev

    # Sensitive data directories
    - ~/.ssh
    - ~/.aws
    - ~/.gnupg
    - ~/.kube
    - ~/.docker
    - ~/.git-credentials
    - ~/.aws/credentials
    - ~/.ssh/id_rsa
    - ~/.ssh/id_ed25519

    # User configuration directories
    - ~/.config
    - ~/.local
    - ~/.cache
    - ~/.npm
    - ~/.pip

  # Risk factors: grouped by risk level
  risk_factors:
    critical:
      - "dd\\s+of="
      - "dd\\s+if=.*of=/dev/"
      - "shred"
      - "mkfs"
      - "mkfs\\."
      - "fdisk"
      - "parted"
      - ">:/dev/"
      - ">\\s*/dev/"
      - "echo\\s+.*\\s*>\\s*/dev/"
      - "systemctl\\s+enable"
      - "systemctl\\s+start.*@"
      - "ssh-keygen.*-t\\s+rsa"

    high:
      - "rm\\s+-[rf]"
      - "rm\\s+-[rfv]"
      - "rmdir"
      - "rm\\s+-R"
      - "rm\\s+--recursive"
      - "curl.*\\|.*sh"
      - "wget.*\\|.*sh"
      - "fetch.*\\|.*sh"
      - "curl.*\\|\\s*bash"
      - "wget.*-O-\\s*\\|"
      - "chmod\\s+([ugo]+=[+,-][rwxst]+)\\s*-R"
      - "chmod\\s+[47]777"
      - "chmod\\s+[0-7]{4,4}\\s+-R"
      - "useradd"
      - "userdel"
      - "groupadd"
      - "groupdel"
      - "usermod"
      - "passwd\\s+root"
      - "systemctl\\s+stop"
      - "systemctl\\s+restart"
      - "service\\s+stop"
      - "service\\s+restart"
      - "iptables"
      - "ufw\\s+allow"
      - "ufw\\s+deny"
      - "firewall-cmd"
      - "nc\\s+-l\\s+-p"
      - "ncat\\s+-l"
      - "kill\\s+-9"
      - "killall"
      - "pkill\\s+-9"
      - "export\\s+.*=.*\\$\\("
      - "env\\s+.*=.*\\`"

    medium:
      - "chmod\\s+[0-9]{3,3}"
      - "chown"
      - "chgrp"
      - "setfacl"
      - "setfattr"
      - "kill\\s+-[0-9]+"
      - "killall\\s+-i"
      - "pkill"
      - "kill\\s+(?!-9)"
      - "tee"
      - "dd\\s+of="
      - ">\\s*[^/]"
      - "touch\\s+/etc"
      - "touch\\s+/var"
      - "apt\\s+remove"
      - "apt\\s+purge"
      - "apt-get\\s+remove"
      - "yum\\s+remove"
      - "dnf\\s+remove"
      - "pip\\s+uninstall"
      - "npm\\s+uninstall"
      - "gem\\s+uninstall"
      - "sysctl"
      - "echo\\s+.*\\s*>\\s*/proc"
      - "mount\\s+-o\\s+remount"
      - "umount"
      - "tar\\s+-cvzf.*--exclude"
      - "rsync.*--delete"

    low:
      - "pip\\s+install"
      - "pip3\\s+install"
      - "npm\\s+install"
      - "npm\\s+i"
      - "yarn\\s+add"
      - "pnpm\\s+add"
      - "apt\\s+install"
      - "apt-get\\s+install"
      - "yum\\s+install"
      - "dnf\\s+install"
      - "brew\\s+install"
      - "gem\\s+install"
      - "cargo\\s+install"
      - "go\\s+install"
      - "composer\\s+require"
      - "curl\\s+-O"
      - "curl\\s+--output"
      - "wget"
      - "fetch"
      - "aria2c"
      - "ping"
      - "traceroute"
      - "tracepath"
      - "netstat"
      - "ss\\s+-tuln"
      - "curl"
      - "wget"
      - "httpie"
      - "df\\s+-h"
      - "du\\s+-sh"
      - "free\\s+-m"
      - "top"
      - "htop"
      - "ps\\s+aux"
      - "lsblk"
      - "mount\\s+-l"
      - "make\\s+build"
      - "make\\s+install"
      - "cmake"
      - "go\\s+build"
      - "go\\s+run"
      - "go\\s+test"
      - "npm\\s+run"
      - "npm\\s+test"
      - "yarn\\s+run"
      - "pytest"
      - "cargo\\s+build"
      - "cargo\\s+test"
EOF
        log_info "Config file created at ${config_file}"
    fi
}

# Create necessary directories
create_dirs() {
    local data_dir=$1

    mkdir -p "${data_dir}/sessions"
    mkdir -p "${data_dir}/rag"
    mkdir -p "${data_dir}/skills"
    mkdir -p "${data_dir}/logs"

    log_info "Created data directories in ${data_dir}"
}

# Main installation
main() {
    echo "========================================="
    echo "  Morpheus CLI Installer v${VERSION}"
    echo "========================================="
    echo ""

    local os=$(detect_os)
    local arch=$(detect_arch)
    local install_dir=$(get_install_dir)
    local config_dir=$(get_config_dir)
    local data_dir=$(get_data_dir)

    log_info "OS: ${os}"
    log_info "Arch: ${arch}"
    log_info "Install dir: ${install_dir}"
    log_info "Config dir: ${config_dir}"
    echo ""

    # Check if we need sudo
    if [ ! -w "$install_dir" ]; then
        log_warn "Need sudo to install to ${install_dir}"
        install_dir="/usr/local/bin"
    fi

    # Install binary
    if command -v morph &> /dev/null; then
        log_warn "Morpheus is already installed"
        read -p "Do you want to reinstall? (y/N) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            exit 0
        fi
    fi

    # Create directories
    create_dirs "$data_dir"

    # Install config
    install_config "$config_dir"

    # Install binary
    install_binary "$os" "$arch" "$install_dir"

    echo ""
    log_info "Installation complete!"
    echo ""
    echo "Next steps:"
    echo "  1. Edit your config: ${config_dir}/config.yaml"
    echo "  2. Add your API key: set api_key or use env:OPENAI_API_KEY"
    echo "  3. Run: morph serve"
    echo ""
    echo "Data directories:"
    echo "  - Sessions: ${data_dir}/sessions"
    echo "  - RAG:      ${data_dir}/rag"
    echo "  - Skills:   ${data_dir}/skills"
    echo "  - Logs:     ${data_dir}/logs"
    echo ""
}

main "$@"
