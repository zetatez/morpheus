#!/usr/bin/env python3
"""Morpheus CLI Installer - Pythonic installation script."""

import argparse
import logging
import os
import platform
import shutil
import stat
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from urllib.request import urlopen
from urllib.error import URLError

VERSION = "0.1.0"
BINARY_NAME = "morph"
RELEASE_BASE = "https://github.com/zetatez/morpheus/releases/download"


@dataclass
class InstallConfig:
    install_dir: Path
    config_dir: Path
    data_dir: Path
    force: bool = False


def setup_logging(verbose: bool = False) -> None:
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="[%(levelname)s] %(message)s",
        handlers=[logging.StreamHandler(sys.stdout)],
    )


def detect_os() -> str:
    system = platform.system().lower()
    if system.startswith("linux"):
        return "linux"
    if system.startswith("darwin"):
        return "darwin"
    if system.startswith("windows"):
        return "windows"
    return "unknown"


def detect_arch() -> str:
    machine = platform.machine().lower()
    if machine in {"x86_64", "amd64"}:
        return "amd64"
    if machine in {"aarch64", "arm64"}:
        return "arm64"
    if machine.startswith("armv7"):
        return "armv7"
    return "amd64"


def get_default_install_dir() -> Path:
    if hasattr(os, "geteuid") and os.geteuid() == 0:
        return Path("/usr/local/bin")
    if detect_os() == "windows":
        return Path.home() / "AppData" / "Local" / "morpheus" / "bin"
    return Path.home() / ".local" / "bin"


def get_default_config_dir() -> Path:
    if detect_os() == "windows":
        return Path(os.environ.get("APPDATA", Path.home() / "AppData" / "Roaming")) / "morpheus"
    return Path.home() / ".config" / "morpheus"


def get_default_data_dir() -> Path:
    if detect_os() == "windows":
        return Path(os.environ.get("LOCALAPPDATA", Path.home() / "AppData" / "Local")) / "morpheus"
    return Path.home() / ".local" / "share" / "morpheus"


def binary_name(os_name: str) -> str:
    if os_name == "windows":
        return f"{BINARY_NAME}.exe"
    return BINARY_NAME


def prompt_yes_no(prompt: str, default: bool = False) -> bool:
    suffix = "Y/n" if default else "y/N"
    while True:
        try:
            resp = input(f"{prompt} [{suffix}] ").strip().lower()
        except (EOFError, KeyboardInterrupt):
            print()
            sys.exit(0)
        if not resp:
            return default
        if resp in ("y", "yes"):
            return True
        if resp in ("n", "no"):
            return False
        print("Please answer yes or no.")


def ensure_module(module: str, pip_name: str | None = None) -> bool:
    try:
        __import__(module)
        return True
    except ImportError:
        pass

    pip_name = pip_name or module
    logging.warning(f"Python module '{module}' not found. Attempting to install...")

    for cmd in (["-m", "ensurepip", "--upgrade"], []):
        try:
            subprocess.check_call([sys.executable, *cmd, "-m", "pip", "install", "--user", pip_name])
            __import__(module)
            return True
        except Exception:
            continue

    logging.error(f"Failed to install {pip_name}.")
    return False


def download_file(url: str, dest: Path) -> None:
    if ensure_module("requests"):
        import requests as req
        with req.get(url, timeout=60) as resp:
            resp.raise_for_status()
            dest.write_bytes(resp.content)
        return

    try:
        with urlopen(url, timeout=60) as response:
            dest.write_bytes(response.read())
    except URLError as e:
        raise RuntimeError(f"Failed to download: {e}") from e


def install_binary(os_name: str, arch: str, install_dir: Path) -> None:
    name = binary_name(os_name)
    install_dir.mkdir(parents=True, exist_ok=True)
    url = f"{RELEASE_BASE}/v{VERSION}/{name}-{os_name}-{arch}"
    logging.info(f"Downloading Morpheus v{VERSION} for {os_name}-{arch}...")

    with tempfile.TemporaryDirectory() as tmp:
        tmp_path = Path(tmp) / name
        download_file(url, tmp_path)
        target = install_dir / name
        shutil.copy2(tmp_path, target)
        target.chmod(target.stat().st_mode | stat.S_IEXEC)
    logging.info(f"Binary installed to {install_dir / name}")


def install_config(config_dir: Path, data_dir: Path) -> None:
    config_dir.mkdir(parents=True, exist_ok=True)
    config_file = config_dir / "config.yaml"
    if config_file.exists():
        logging.warning(f"Config file already exists at {config_file}")
        logging.info("Skipping config installation")
        return

    content = f"""\
workspace_root: ~/

logging:
  level: info
  file: {data_dir}/logs/morpheus.log
  audit: {data_dir}/logs/audit.log

planner:
  provider: openai
  model: gpt-4o-mini
  temperature: 0.2

server:
  listen: :8080
  limits:
    enabled: true
    max_cpu_percent: 85
    max_memory_mb: 2048
    sample_interval_ms: 1000

session:
  path: {data_dir}/sessions
  sqlite_path: {data_dir}/sessions.db
  retention: 720h

knowledge_base:
  path: {config_dir}/knowledge_base

agent:
  default_mode: build

permissions:
  confirm_above: high
  confirm_protected_paths:
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
    - ~/.ssh
    - ~/.aws
    - ~/.gnupg
    - ~/.kube
    - ~/.docker
    - ~/.git-credentials
    - ~/.aws/credentials
    - ~/.ssh/id_rsa
    - ~/.ssh/id_ed25519
    - ~/.config
    - ~/.local
    - ~/.cache
    - ~/.npm
    - ~/.pip

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
      - "chmod\\s+[0-7]{{4,4}}\\s+-R"
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
      - "chmod\\s+[0-9]{{3,3}}"
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
"""
    config_file.write_text(content, encoding="utf-8")
    logging.info(f"Config file created at {config_file}")


def create_dirs(data_dir: Path) -> None:
    for name in ("sessions", "skills", "logs"):
        (data_dir / name).mkdir(parents=True, exist_ok=True)
    logging.info(f"Created data directories in {data_dir}")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="install.py",
        description="Morpheus CLI Installer",
    )
    parser.add_argument("--version", action="version", version=f"%(prog)s {VERSION}")
    parser.add_argument("-v", "--verbose", action="store_true", help="Enable verbose output")
    parser.add_argument("--install-dir", type=Path, default=None, help="Installation directory (default: auto)")
    parser.add_argument("--config-dir", type=Path, default=None, help="Configuration directory (default: auto)")
    parser.add_argument("--data-dir", type=Path, default=None, help="Data directory (default: auto)")
    parser.add_argument("-f", "--force", action="store_true", help="Force reinstall")
    parser.add_argument("--no-config", action="store_true", help="Skip config file creation")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    setup_logging(args.verbose)

    print(f"Morpheus CLI Installer v{VERSION}")
    print("=" * 40)

    os_name = detect_os()
    arch = detect_arch()

    install_dir = args.install_dir or get_default_install_dir()
    config_dir = args.config_dir or get_default_config_dir()
    data_dir = args.data_dir or get_default_data_dir()

    logging.info(f"OS: {os_name}, Arch: {arch}")
    logging.info(f"Install dir: {install_dir}")
    logging.info(f"Config dir: {config_dir}")
    logging.info(f"Data dir: {data_dir}")

    if os_name == "unknown":
        logging.error("Unsupported OS")
        sys.exit(1)

    bin_name = binary_name(os_name)
    if shutil.which(bin_name) and not args.force:
        logging.warning("Morpheus is already installed")
        if not prompt_yes_no("Do you want to reinstall?", default=False):
            sys.exit(0)

    if not os.access(install_dir, os.W_OK):
        logging.warning(f"Install dir {install_dir} is not writable.")
        fallback = get_default_install_dir()
        if prompt_yes_no(f"Install to {fallback} instead?", default=True):
            install_dir = fallback
        else:
            logging.error("Install directory not writable. Use --install-dir or run as root.")
            sys.exit(1)

    create_dirs(data_dir)
    if not args.no_config:
        install_config(config_dir, data_dir)
    install_binary(os_name, arch, install_dir)

    logging.info("Installation complete!")
    print("\nNext steps:")
    print(f"  1. Edit config: {config_dir / 'config.yaml'}")
    print(f"  2. Set API key: env:OPENAI_API_KEY or edit config")
    print(f"  3. Run: {bin_name} serve")
    print("\nData directories:")
    print(f"  - Sessions: {data_dir / 'sessions'}")
    print(f"  - Skills:   {data_dir / 'skills'}")
    print(f"  - Logs:     {data_dir / 'logs'}")


if __name__ == "__main__":
    main()
