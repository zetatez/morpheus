#!/usr/bin/env python3
"""Morpheus Release Tool - Cross-platform release and package builder."""

import argparse
import hashlib
import json
import logging
import os
import platform
import shutil
import subprocess
import sys
import tarfile
import tempfile
import zipfile
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Optional

VERSION = "0.1.0"
BINARY_NAME = "morpheus"
REPO_OWNER = "zetatez"
REPO_NAME = "morpheus"
GITHUB_REPO_URL = f"https://github.com/{REPO_OWNER}/{REPO_NAME}"


@dataclass
class Platform:
    os_name: str
    arch: str
    ext: str = ""

    def __post_init__(self):
        if not self.ext:
            self.ext = ".exe" if self.os_name == "windows" else ""


PLATFORMS = [
    Platform("linux", "amd64"),
    Platform("linux", "arm64"),
    Platform("darwin", "amd64"),
    Platform("darwin", "arm64"),
    Platform("windows", "amd64"),
    Platform("windows", "arm64"),
]


@dataclass
class ReleaseConfig:
    version: str
    root_dir: Path
    build_dir: Path
    dist_dir: Path
    source_dir: Path
    go_path: Optional[str] = None


def setup_logging(verbose: bool = False) -> None:
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="[%(levelname)s] %(message)s",
        handlers=[logging.StreamHandler(sys.stdout)],
    )


def run_cmd(cmd: list[str], cwd: Optional[Path] = None, env: Optional[dict] = None) -> subprocess.CompletedProcess:
    merged_env = {**os.environ, **(env or {})}
    logging.debug(f"Running: {' '.join(str(c) for c in cmd)}")
    result = subprocess.run(
        cmd,
        cwd=cwd,
        env=merged_env,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        logging.error(f"Command failed: {' '.join(str(c) for c in cmd)}")
        logging.error(f"stdout: {result.stdout}")
        logging.error(f"stderr: {result.stderr}")
        raise RuntimeError(f"Command failed with return code {result.returncode}")
    return result


def get_git_sha() -> str:
    result = run_cmd(["git", "rev-parse", "--short", "HEAD"], cwd=Path(__file__).parent)
    return result.stdout.strip()


def get_git_branch() -> str:
    result = run_cmd(["git", "rev-parse", "--abbrev-ref", "HEAD"], cwd=Path(__file__).parent)
    return result.stdout.strip()


def detect_go_version() -> str:
    result = run_cmd(["go", "version"])
    return result.stdout.strip()


def ensure_go() -> None:
    try:
        run_cmd(["go", "version"])
    except RuntimeError:
        logging.error("Go is not installed. Please install Go 1.23 or later.")
        sys.exit(1)


def calculate_sha256(file_path: Path) -> str:
    sha256_hash = hashlib.sha256()
    with open(file_path, "rb") as f:
        for byte_block in iter(lambda: f.read(4096), b""):
            sha256_hash.update(byte_block)
    return sha256_hash.hexdigest()


def calculate_checksums(dist_dir: Path) -> dict[str, str]:
    checksums = {}
    for file in dist_dir.iterdir():
        if file.is_file() and file.suffix not in (".sha256", ".md5"):
            checksums[file.name] = calculate_sha256(file)
            logging.info(f"Checksum: {file.name} -> {checksums[file.name]}")
    return checksums


def build_binary(config: ReleaseConfig, platform: Platform) -> Path:
    output_name = f"{BINARY_NAME}-{platform.os_name}-{platform.arch}{platform.ext}"
    output_path = config.dist_dir / output_name

    logging.info(f"Building {platform.os_name}/{platform.arch}...")

    env = {
        "GOOS": platform.os_name,
        "GOARCH": platform.arch,
        "CGO_ENABLED": "0",
    }

    if config.go_path:
        env["GOPATH"] = str(config.go_path)

    build_env = {**os.environ, **env}

    cmd = ["go", "build", "-ldflags", f"-s -w -X main.version={config.version}", "-o", str(output_path), "./cmd/morpheus"]

    run_cmd(cmd, cwd=config.source_dir, env=build_env)

    if not output_path.exists():
        raise RuntimeError(f"Build failed: {output_path} not created")

    output_path.chmod(0o755)
    logging.info(f"Built: {output_path}")
    return output_path


def create_archive(config: ReleaseConfig, platform: Platform, binary_path: Path) -> Path:
    archive_name = f"{BINARY_NAME}-{platform.os_name}-{platform.arch}{platform.ext}.tar.gz"
    archive_path = config.dist_dir / archive_name

    with tarfile.open(archive_path, "w:gz") as tar:
        tar.add(binary_path, arcname=f"{BINARY_NAME}{platform.ext}")

    logging.info(f"Created archive: {archive_path}")
    return archive_path


def create_windows_zip(config: ReleaseConfig, platform: Platform, binary_path: Path) -> Path:
    zip_name = f"{BINARY_NAME}-{platform.os_name}-{platform.arch}{platform.ext}.zip"
    zip_path = config.dist_dir / zip_name

    with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as zf:
        zf.write(binary_path, arcname=f"{BINARY_NAME}{platform.ext}")

    logging.info(f"Created zip: {zip_path}")
    return zip_path


def create_linux_package(config: ReleaseConfig) -> Path:
    pkg_dir = config.dist_dir / "linux"
    pkg_dir.mkdir(exist_ok=True)

    for platform in PLATFORMS:
        if platform.os_name != "linux":
            continue
        binary_name = f"{BINARY_NAME}-{platform.os_name}-{platform.arch}{platform.ext}"
        binary_path = config.dist_dir / binary_name

        archive_name = f"{BINARY_NAME}-{platform.arch}{platform.ext}.tar.gz"
        archive_path = pkg_dir / archive_name

        with tarfile.open(archive_path, "w:gz") as tar:
            tar.add(binary_path, arcname=f"{BINARY_NAME}{platform.ext}")

    install_script = """#!/bin/bash
set -e

INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BINARY_NAME="morph"

if [ -d "/usr/local/bin" ] && [ -w "/usr/local/bin" ]; then
    INSTALL_DIR="/usr/local/bin"
fi

install -Dm755 "$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"

echo "Installed to $INSTALL_DIR/$BINARY_NAME"
echo "Add $INSTALL_DIR to your PATH if needed."
"""
    (pkg_dir / "install.sh").write_text(install_script)
    (pkg_dir / "install.sh").chmod(0o755)

    readme = f"""# Morpheus Linux Package

## Installation

```bash
tar -xzf morph-{config.version}-linux.tar.gz
./install.sh
```

Or manually:

```bash
tar -xzf morph-{config.version}-linux.tar.gz
install -Dm755 morph $HOME/.local/bin/morph
```

## Supported Architectures

- x86_64 (amd64)
- ARM64 (aarch64)
"""
    (pkg_dir / "README.md").write_text(readme)

    return pkg_dir


def create_arch_aur_pkgbuild(config: ReleaseConfig) -> Path:
    aur_dir = config.dist_dir / "arch-aur"
    aur_dir.mkdir(exist_ok=True)

    sha256_amd64 = ""
    sha256_arm64 = ""
    for platform in PLATFORMS:
        if platform.os_name != "linux":
            continue
        binary_name = f"{BINARY_NAME}-{platform.os_name}-{platform.arch}{platform.ext}"
        binary_path = config.dist_dir / binary_name
        checksum = calculate_sha256(binary_path)
        if platform.arch == "amd64":
            sha256_amd64 = checksum
        else:
            sha256_arm64 = checksum

    pkgbuild = f"""# Maintainer: {REPO_OWNER}
# Contributor: {REPO_OWNER}

pkgname={BINARY_NAME}
pkgver={config.version}
pkgrel=1
pkgdesc="Local AI agent runtime with tool execution, session persistence, MCP protocol support, and interactive TUI client"
arch=('x86_64' 'aarch64')
url="{GITHUB_REPO_URL}"
license=('MIT')
depends=()
provides=("$pkgname")
conflicts=("$pkgname")
options=(!strip)

source_amd64=("{GITHUB_REPO_URL}/releases/download/v$pkgver/$pkgname-linux-amd64.tar.gz")
source_arm64=("{GITHUB_REPO_URL}/releases/download/v$pkgver/$pkgname-linux-arm64.tar.gz")
sha256sums_amd64=('{sha256_amd64}')
sha256sums_arm64=('{sha256_arm64}')

build() {{
    cd $srcdir

    if [ "$CARCH" = "x86_64" ]; then
        tar -xzf $pkgname-linux-amd64.tar.gz
    else
        tar -xzf $pkgname-linux-arm64.tar.gz
    fi
}}

package() {{
    install -Dm755 "$pkgname" "$pkgdir/usr/bin/$pkgname"
}}
"""
    (aur_dir / "PKGBUILD").write_text(pkgbuild)

    install_script = """#!/bin/bash
set -e

cd "$(dirname "$0")"

echo "Installing Morpheus AUR package..."
makepkg -si
"""
    (aur_dir / "install-aur.sh").write_text(install_script)
    (aur_dir / "install-aur.sh").chmod(0o755)

    readme = f"""# Morpheus Arch Linux AUR Package

## Installation with yay

```bash
yay -S morpheus
```

## Installation from this package

### Option 1: Use the install script
```bash
cd arch-aur
./install-aur.sh
```

### Option 2: Manual installation
```bash
cd arch-aur
makepkg -si
```

## For Maintainers

Update `pkgver` in PKGBUILD when releasing new versions.

Generate new sha256 sums:
```bash
cd arch-aur
updpkgsums
makepkg -G
```
"""
    (aur_dir / "README.md").write_text(readme)
    (aur_dir / ".SRCINFO").write_text(f"""pkgbase = {BINARY_NAME}
    pkgdesc = Local AI agent runtime with tool execution, session persistence, MCP protocol support, and interactive TUI client
    pkgver = {config.version}
    pkgrel = 1
    url = {GITHUB_REPO_URL}
    license = MIT
    architecture = x86_64 aarch64
    source_x86_64 = {GITHUB_REPO_URL}/releases/download/v{config.version}/{BINARY_NAME}-linux-amd64.tar.gz
    source_aarch64 = {GITHUB_REPO_URL}/releases/download/v{config.version}/{BINARY_NAME}-linux-arm64.tar.gz
""")

    return aur_dir


def create_macos_package(config: ReleaseConfig) -> Path:
    pkg_dir = config.dist_dir / "macos"
    pkg_dir.mkdir(exist_ok=True)

    for platform in PLATFORMS:
        if platform.os_name != "darwin":
            continue
        binary_name = f"{BINARY_NAME}-{platform.os_name}-{platform.arch}{platform.ext}"
        binary_path = config.dist_dir / binary_name

        archive_name = f"{BINARY_NAME}-{platform.os_name}-{platform.arch}{platform.ext}.tar.gz"
        archive_path = pkg_dir / archive_name

        with tarfile.open(archive_path, "w:gz") as tar:
            tar.add(binary_path, arcname=f"{BINARY_NAME}{platform.ext}")

    install_script = """#!/bin/bash
set -e

INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BINARY_NAME="morph"

mkdir -p "$INSTALL_DIR"
tar -xzf "morph-darwin-$(uname -m).tar.gz"
install -Dm755 "$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"

echo "Installed to $INSTALL_DIR/$BINARY_NAME"
echo "Add $INSTALL_DIR to your PATH if needed."
"""
    (pkg_dir / "install.sh").write_text(install_script)
    (pkg_dir / "install.sh").chmod(0o755)

    readme = f"""# Morpheus macOS Package

## Installation

```bash
tar -xzf morph-darwin-$(uname -m).tar.gz
./install.sh
```

## Supported Architectures

- x86_64 (Intel Macs)
- ARM64 (Apple Silicon)
"""
    (pkg_dir / "README.md").write_text(readme)

    return pkg_dir


def create_homebrew_formula(config: ReleaseConfig) -> Path:
    brew_dir = config.dist_dir / "homebrew" / "Formula"
    brew_dir.mkdir(parents=True, exist_ok=True)

    sha256_intel = ""
    sha256_arm64 = ""
    for platform in PLATFORMS:
        if platform.os_name == "darwin":
            binary_name = f"{BINARY_NAME}-{platform.os_name}-{platform.arch}{platform.ext}.tar.gz"
            archive_path = config.dist_dir / binary_name
            checksum = calculate_sha256(archive_path)
            if platform.arch == "amd64":
                sha256_intel = checksum
            else:
                sha256_arm64 = checksum

    formula = f'''class Morpheus < Formula
  desc "Local AI agent runtime with tool execution, session persistence, MCP protocol support"
  homepage "{GITHUB_REPO_URL}"
  version "{config.version}"

  on_macos do
    if Hardware::CPU.intel?
      url "{GITHUB_REPO_URL}/releases/download/v{config.version}/morph-darwin-amd64.tar.gz"
      sha256 "{sha256_intel}"
    elsif Hardware::CPU.arm?
      url "{GITHUB_REPO_URL}/releases/download/v{config.version}/morph-darwin-arm64.tar.gz"
      sha256 "{sha256_arm64}"
    end
  end

  def install
    bin.install "morph"
  end

  test do
    system "{bin}/morph", "--version"
  end
end
'''
    (brew_dir / "morpheus.rb").write_text(formula)

    readme = f"""# Morpheus Homebrew Tap

## Installation

```bash
brew tap {REPO_OWNER}/morpheus
brew install morpheus
```

## For Maintainers

When releasing a new version:
1. Build macOS binaries first
2. Run `release.py` to generate packages
3. Update `version`, `url`, and `sha256` in `Formula/morpheus.rb`
4. Commit and push to the tap repository
5. Tag the release

## GitHub Repository

Create a GitHub repository named `homebrew-tap` (or similar) under your account.
The tap URL format: `https://github.com/{REPO_OWNER}/homebrew-tap`

Users will then run:
```bash
brew tap {REPO_OWNER}/homebrew-tap
brew install morpheus
```
"""
    (config.dist_dir / "homebrew" / "README.md").write_text(readme)

    return config.dist_dir / "homebrew"


def create_windows_package(config: ReleaseConfig) -> Path:
    pkg_dir = config.dist_dir / "windows"
    pkg_dir.mkdir(exist_ok=True)

    for platform in PLATFORMS:
        if platform.os_name != "windows":
            continue
        binary_name = f"{BINARY_NAME}-{platform.os_name}-{platform.arch}{platform.ext}"
        binary_path = config.dist_dir / binary_name

        zip_name = f"{BINARY_NAME}-{platform.os_name}-{platform.arch}{platform.ext}.zip"
        zip_path = pkg_dir / zip_name

        with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as zf:
            zf.write(binary_path, arcname=f"{BINARY_NAME}{platform.ext}")

    install_script = """@echo off
setlocal

set "INSTALL_DIR=%USERPROFILE%\\AppData\\Local\\morpheus\\bin"
if exist "%LOCALAPPDATA%\\morpheus\\bin" set "INSTALL_DIR=%LOCALAPPDATA%\\morpheus\\bin"

mkdir "%INSTALL_DIR%" 2>nul
expand /r /f:* morph-windows-*.zip "%INSTALL_DIR%\\" >nul 2>&1
if %ERRORLEVEL% neq 0 (
    powershell -Command "Expand-Archive -Force 'morph-windows-%PROCESSOR_ARCHITECTURE:.=%*.zip' '%INSTALL_DIR%'"
)

echo Installed to %INSTALL_DIR%
echo Add %INSTALL_DIR% to your PATH if needed.
"""
    (pkg_dir / "install.bat").write_text(install_script)

    readme = f"""# Morpheus Windows Package

## Installation

```batch
install.bat
```

Or manually:

```batch
mkdir %LOCALAPPDATA%\\morpheus\\bin
powershell -Command "Expand-Archive -Force 'morph-windows-*.zip' '%LOCALAPPDATA%\\morpheus\\bin'"
setx PATH "%PATH%;%LOCALAPPDATA%\\morpheus\\bin"
```

## Supported Architectures

- x86_64 (amd64)
- ARM64
"""
    (pkg_dir / "README.md").write_text(readme)

    return pkg_dir


def create_chocolatey_package(config: ReleaseConfig) -> Path:
    choco_dir = config.dist_dir / "chocolatey"
    tools_dir = choco_dir / "tools"
    tools_dir.mkdir(parents=True, exist_ok=True)

    sha256_amd64 = ""
    sha256_arm64 = ""
    for platform in PLATFORMS:
        if platform.os_name == "windows":
            binary_name = f"{BINARY_NAME}-{platform.os_name}-{platform.arch}{platform.ext}.zip"
            binary_path = config.dist_dir / binary_name
            checksum = calculate_sha256(binary_path)
            if platform.arch == "amd64":
                sha256_amd64 = checksum
            else:
                sha256_arm64 = checksum

    nuspec = f"""<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2015/06/nuspec.xsd">
  <metadata>
    <id>{BINARY_NAME}</id>
    <version>{config.version}</version>
    <title>Morpheus</title>
    <authors>{REPO_OWNER}</authors>
    <projectUrl>{GITHUB_REPO_URL}</projectUrl>
    <packageSourceUrl>{GITHUB_REPO_URL}/tree/main/chocolatey</packageSourceUrl>
    <docsUrl>{GITHUB_REPO_URL}/blob/main/README.md</docsUrl>
    <bugTrackerUrl>{GITHUB_REPO_URL}/issues</bugTrackerUrl>
    <tags>ai agent gpt llm mcp productivity cli</tags>
    <summary>Local AI agent runtime with tool execution and MCP protocol support</summary>
    <description>
Morpheus is a local AI agent runtime with tool execution, session persistence,
MCP protocol support, and an interactive TUI client.

Features:
- AI Agent Runtime with iterative agent execution and tool calling
- Multi-Agent Coordination with parallel task execution
- MCP Protocol support (stdio/HTTP/SSE transports)
- Session Persistence with SQLite storage
- Interactive TUI Client
- REST API with streaming support
    </description>
    <licenseUrl>{GITHUB_REPO_URL}/blob/main/LICENSE</licenseUrl>
    <requireLicenseAcceptance>false</requireLicenseAcceptance>
    <iconUrl>{GITHUB_REPO_URL}/raw/main/docs/icon.png</iconUrl>
  </metadata>
  <files>
    <file src="tools\\*" target="tools" />
  </files>
</package>
"""
    (choco_dir / f"{BINARY_NAME}.nuspec").write_text(nuspec)

    install_ps1 = rf"""$ErrorActionPreference = 'Stop'

$packageName = '{BINARY_NAME}'
$version = '{config.version}'
$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$rootDir = Split-Path -Parent $toolsDir

$url_x64 = '{GITHUB_REPO_URL}/releases/download/v$version/$packageName-windows-amd64.zip'
$sha256_x64 = '{sha256_amd64}'

$url_arm64 = '{GITHUB_REPO_URL}/releases/download/v$version/$packageName-windows-arm64.zip'
$sha256_arm64 = '{sha256_arm64}'

$packageDir = "$env:USERPROFILE\AppData\Local\$packageName"
$binDir = "$packageDir\bin"

function Get-Checksum {{
    param([string]$FilePath, [string]$Algorithm = 'sha256')
    $hash = Get-FileHash -Path $FilePath -Algorithm $Algorithm
    return $hash.Hash.ToLower()
}}

function Install-Morpheus {{
    param([string]$Url, [string]$ExpectedSha256, [string]$Arch)

    $zipPath = "$env:TEMP\$packageName-$version-$Arch.zip"

    Write-Host "Downloading Morpheus $version ($Arch)..."
    Invoke-WebRequest -Uri $Url -OutFile $zipPath -UseBasicParsing

    $actualSha256 = Get-Checksum -FilePath $zipPath
    if ($actualSha256 -ne $ExpectedSha256) {{
        Write-Host "ERROR: Checksum mismatch for $Arch"
        Write-Host "Expected: $ExpectedSha256"
        Write-Host "Actual:   $actualSha256"
        Remove-Item $zipPath -Force -ErrorAction SilentlyContinue
        exit 1
    }}

    Write-Host "Installing..."
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null
    Expand-Archive -Path $zipPath -DestinationPath $binDir -Force
    Remove-Item $zipPath -Force -ErrorAction SilentlyContinue

    $binPath = "$binDir\$packageName.exe"

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath -notlike "*$binDir*") {{
        Write-Host "Adding $binDir to user PATH..."
        [Environment]::SetEnvironmentVariable('Path', "$userPath;$binDir", 'User')
        $env:Path = "$env:Path;$binDir"
    }}

    Write-Host "Morpheus installed to $binPath"
    Write-Host "Run 'morph --version' to verify."
}}

$arch = $env:PROCESSOR_ARCHITECTURE
if ($arch -eq 'AMD64') {{
    Install-Morpheus -Url $url_x64 -ExpectedSha256 $sha256_x64 -Arch 'x64'
}} elseif ($arch -eq 'ARM64') {{
    Install-Morpheus -Url $url_arm64 -ExpectedSha256 $sha256_arm64 -Arch 'arm64'
}} else {{
    Write-Host "Unsupported architecture: $arch"
    exit 1
}}
"""
    (tools_dir / "chocolateyinstall.ps1").write_text(install_ps1)

    uninstall_ps1 = r"""$ErrorActionPreference = 'Stop'

$packageName = 'morph'
$packageDir = "$env:USERPROFILE\AppData\Local\$packageName"
$binDir = "$packageDir\bin"

Write-Host "Uninstalling Morpheus..."

if (Test-Path $binDir) {
    Remove-Item $binDir -Recurse -Force -ErrorAction SilentlyContinue
}

if (Test-Path $packageDir) {
    Remove-Item $packageDir -Recurse -Force -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -like "*$binDir*") {
    $newPath = ($userPath -split ';' | Where-Object { $_ -notlike "*$binDir*" }) -join ';'
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
}

Write-Host "Morpheus uninstalled."
"""
    (tools_dir / "chocolateyuninstall.ps1").write_text(uninstall_ps1)

    readme = rf"""# Morpheus Chocolatey Package

## Installation

```powershell
choco install morpheus -y
```

Or install from local package:

```powershell
cd chocolatey
choco install morpheus -y --source .
```

## Update

```powershell
choco update morpheus
```

## Uninstall

```powershell
choco uninstall morpheus
```

## Notes

- Package ID: `morph`
- Version: `{config.version}`
- Installation directory: `%USERPROFILE%\AppData\Local\morph\bin`
- Automatically adds to user PATH

## For Maintainers

When releasing a new version:
1. Build Windows binaries first
2. Run `release.py` to generate packages
3. Update `$version`, `$sha256_x64`, `$sha256_arm64` in chocolateyinstall.ps1
4. Test locally: `choco install .\morph.nuspec -y --force`
5. Push to Chocolatey repository
"""
    (choco_dir / "README.md").write_text(readme)

    return choco_dir


def create_winget_manifest(config: ReleaseConfig) -> Path:
    winget_dir = config.dist_dir / "winget"
    winget_dir.mkdir(exist_ok=True)

    sha256_amd64 = ""
    sha256_arm64 = ""
    for platform in PLATFORMS:
        if platform.os_name == "windows":
            binary_name = f"{BINARY_NAME}-{platform.os_name}-{platform.arch}{platform.ext}.zip"
            binary_path = config.dist_dir / binary_name
            checksum = calculate_sha256(binary_path)
            if platform.arch == "amd64":
                sha256_amd64 = checksum
            else:
                sha256_arm64 = checksum

    manifest = f"""PackageIdentifier: {REPO_OWNER}.{BINARY_NAME}
PackageVersion: {config.version}
PackageName: Morpheus
Publisher: {REPO_OWNER}
License: MIT
LicenseUrl: {GITHUB_REPO_URL}/blob/main/LICENSE
ShortDescription: Local AI agent runtime with tool execution and MCP protocol support
Description: >
  Morpheus is a local AI agent runtime with tool execution, session persistence,
  MCP protocol support, and an interactive TUI client.
Homepage: {GITHUB_REPO_URL}
ReleaseNotesUrl: {GITHUB_REPO_URL}/releases/tag/v{config.version}
InstallerLocale: en-US
Tags:
  - ai
  - agent
  - gpt
  - llm
  - mcp
  - productivity
ProtocolHandlers:
  - morpheus
InstallerLoops:
  - morpheus
Architectures:
  - x64
  - arm64
InstallerType: zip
Installers:
  - Architecture: x64
    InstallerUrl: {GITHUB_REPO_URL}/releases/download/v{config.version}/morph-windows-amd64.zip
    InstallerSha256: {sha256_amd64}
    Signature: ""
  - Architecture: arm64
    InstallerUrl: {GITHUB_REPO_URL}/releases/download/v{config.version}/morph-windows-arm64.zip
    InstallerSha256: {sha256_arm64}
    Signature: ""
PackageSearchKeywords:
  - morpheus
  - ai agent
  - coding assistant
DevTool: false
"""
    (winget_dir / "morpheus.yaml").write_text(manifest)

    readme = f"""# Morpheus winget Manifest

## Installation via winget

```powershell
winget install --manifest ./morpheus.yaml
```

## Update Manifest

When releasing a new version:
1. Build release binaries first
2. Calculate sha256 for both architectures
3. Update `PackageVersion`, `InstallerUrl`, and `InstallerSha256` in morpheus.yaml

## Notes

- winget requires the manifest to be submitted to the Windows Package Manager community repo
- For private testing, use `--manifest` flag with local manifest
"""
    (winget_dir / "README.md").write_text(readme)

    return winget_dir


def create_release_notes(config: ReleaseConfig) -> Path:
    notes = f"""# Morpheus v{config.version}

Release date: {datetime.now().strftime('%Y-%m-%d')}

## Download

### Linux
- [morph-linux-amd64.tar.gz]({GITHUB_REPO_URL}/releases/download/v{config.version}/morph-linux-amd64.tar.gz)
- [morph-linux-arm64.tar.gz]({GITHUB_REPO_URL}/releases/download/v{config.version}/morph-linux-arm64.tar.gz)

### macOS
- [morph-darwin-amd64.tar.gz]({GITHUB_REPO_URL}/releases/download/v{config.version}/morph-darwin-amd64.tar.gz)
- [morph-darwin-arm64.tar.gz]({GITHUB_REPO_URL}/releases/download/v{config.version}/morph-darwin-arm64.tar.gz)

### Windows
- [morph-windows-amd64.zip]({GITHUB_REPO_URL}/releases/download/v{config.version}/morph-windows-amd64.zip)
- [morph-windows-arm64.zip]({GITHUB_REPO_URL}/releases/download/v{config.version}/morph-windows-arm64.zip)

## Installation

### Linux
```bash
tar -xzf morph-linux-$(uname -m).tar.gz
./install.sh
# or
install -Dm755 morph ~/.local/bin/morph
```

### macOS
```bash
tar -xzf morph-darwin-$(uname -m).tar.gz
./install.sh
```

### Windows
```batch
install.bat
```

### Arch Linux (yay)
```bash
yay -S morpheus
```

### Homebrew (macOS/Linux)
```bash
brew tap {REPO_OWNER}/morpheus
brew install morpheus
```

### Chocolatey (Windows)
```powershell
choco install morpheus -y
```

### winget (Windows)
```powershell
winget install --manifest ./morpheus.yaml
```

## Checksums

"""
    checksums = calculate_checksums(config.dist_dir)
    for filename, checksum in sorted(checksums.items()):
        if filename.endswith((".tar.gz", ".zip")):
            notes += f"- `{filename}`: `{checksum}`\n"

    notes += f"""
## Changelog

Full changelog: {GITHUB_REPO_URL}/releases
"""
    notes_path = config.dist_dir / "RELEASE_NOTES.md"
    notes_path.write_text(notes)
    return notes_path


def create_checksum_file(dist_dir: Path) -> Path:
    checksums = calculate_checksums(dist_dir)
    checksum_path = dist_dir / "checksums.txt"
    with open(checksum_path, "w") as f:
        for filename, checksum in sorted(checksums.items()):
            f.write(f"{checksum}  {filename}\n")
    return checksum_path


def build_all(config: ReleaseConfig) -> None:
    logging.info("Building cross-platform binaries...")

    for platform in PLATFORMS:
        binary_path = build_binary(config, platform)
        if platform.os_name == "windows":
            create_windows_zip(config, platform, binary_path)
        else:
            create_archive(config, platform, binary_path)

    logging.info("Build complete!")


def create_all_packages(config: ReleaseConfig) -> None:
    logging.info("Creating distribution packages...")

    create_linux_package(config)
    create_arch_aur_pkgbuild(config)
    create_macos_package(config)
    create_homebrew_formula(config)
    create_windows_package(config)
    create_chocolatey_package(config)
    create_winget_manifest(config)
    create_checksum_file(config.dist_dir)
    create_release_notes(config)

    logging.info("Package creation complete!")


def upload_to_github(config: ReleaseConfig, draft: bool = True) -> None:
    logging.info("Uploading to GitHub...")

    try:
        import requests
    except ImportError:
        logging.error("requests library required for GitHub upload. Install with: pip install requests")
        return

    import requests

    release_name = f"v{config.version}"

    headers = {
        "Accept": "application/vnd.github+json",
        "Authorization": f"token {os.environ.get('GITHUB_TOKEN', '')}",
        "X-GitHub-Api-Version": "2022-11-28",
    }

    if not os.environ.get("GITHUB_TOKEN"):
        logging.warning("GITHUB_TOKEN not set. Cannot upload to GitHub.")
        logging.info("Set with: export GITHUB_TOKEN=your_token")
        return

    create_release_url = f"https://api.github.com/repos/{REPO_OWNER}/{REPO_NAME}/releases"
    release_data = {
        "tag_name": release_name,
        "name": release_name,
        "draft": draft,
        "generate_release_notes": True,
    }

    response = requests.post(create_release_url, headers=headers, json=release_data)
    if response.status_code == 201:
        release = response.json()
        release_id = release["id"]
        upload_url = release["upload_url"].replace("{?name,label}", "")

        for file in config.dist_dir.iterdir():
            if file.is_file() and file.suffix in (".tar.gz", ".zip") or file.name in ("checksums.txt", "RELEASE_NOTES.md"):
                logging.info(f"Uploading {file.name}...")
                with open(file, "rb") as f:
                    files = {"file": (file.name, f)}
                    upload_response = requests.post(
                        upload_url,
                        headers={**headers, "Content-Type": "application/octet-stream"},
                        params={"name": file.name},
                        files=files,
                    )
                    if upload_response.status_code != 201:
                        logging.error(f"Failed to upload {file.name}: {upload_response.text}")

        logging.info(f"Release created: {release['html_url']}")
    else:
        logging.error(f"Failed to create release: {response.text}")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="release.py",
        description="Morpheus Release Tool - Cross-platform release and package builder",
    )
    parser.add_argument("--version", default=VERSION, help="Release version")
    parser.add_argument("-v", "--verbose", action="store_true", help="Enable verbose output")
    parser.add_argument("--skip-build", action="store_true", help="Skip building binaries")
    parser.add_argument("--skip-packages", action="store_true", help="Skip creating packages")
    parser.add_argument("--upload", action="store_true", help="Upload to GitHub")
    parser.add_argument("--draft", action="store_true", default=True, help="Create draft release (default: true)")
    parser.add_argument("--published", action="store_false", dest="draft", help="Create published release")
    parser.add_argument("--dist-dir", type=Path, default=None, help="Distribution directory")

    return parser.parse_args()


def main() -> None:
    args = parse_args()
    setup_logging(args.verbose)

    ensure_go()

    root_dir = Path(__file__).parent.resolve()
    dist_dir = args.dist_dir or (root_dir / "dist" / args.version)

    config = ReleaseConfig(
        version=args.version,
        root_dir=root_dir,
        build_dir=root_dir / "build",
        dist_dir=dist_dir,
        source_dir=root_dir,
    )

    dist_dir.mkdir(parents=True, exist_ok=True)

    logging.info(f"Morpheus Release Tool v{VERSION}")
    logging.info(f"Version: {config.version}")
    logging.info(f"Dist dir: {config.dist_dir}")
    logging.info(f"Go: {detect_go_version()}")
    logging.info(f"Git SHA: {get_git_sha()}")
    logging.info(f"Git branch: {get_git_branch()}")
    print("=" * 50)

    if not args.skip_build:
        build_all(config)

    if not args.skip_packages:
        create_all_packages(config)

    print("=" * 50)
    logging.info(f"Release files in: {config.dist_dir}")

    if args.upload:
        upload_to_github(config, draft=args.draft)


if __name__ == "__main__":
    main()
