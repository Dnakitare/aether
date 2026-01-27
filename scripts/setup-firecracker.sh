#!/bin/bash
# Setup script for Firecracker microVM runtime
# This script downloads and installs Firecracker for local development

set -e

VERSION="${FIRECRACKER_VERSION:-v1.7.0}"
ARCH=$(uname -m)
OS=$(uname -s | tr '[:upper:]' '[:lower:]')

echo "Setting up Firecracker ${VERSION} for ${OS}/${ARCH}"

# Determine install directory
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Check if running with appropriate permissions
if [ ! -w "$INSTALL_DIR" ]; then
    echo "Warning: $INSTALL_DIR is not writable. You may need sudo privileges."
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
    echo "Using alternative install directory: $INSTALL_DIR"
fi

# Only Linux x86_64 is officially supported by Firecracker
if [ "$OS" != "linux" ] || [ "$ARCH" != "x86_64" ]; then
    echo "Warning: Firecracker officially supports Linux x86_64 only."
    echo "Current platform: ${OS}/${ARCH}"

    if [ "$OS" = "darwin" ]; then
        echo ""
        echo "For macOS development, you have several options:"
        echo "  1. Use Docker to run Firecracker in a Linux container"
        echo "  2. Use a Linux VM (UTM, Parallels, VMware)"
        echo "  3. Continue with mock implementation (tests only)"
        echo ""
        read -p "Continue anyway? (y/N) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            exit 1
        fi
        echo "Skipping Firecracker installation on macOS."
        echo "Setting up development environment for testing only..."
        exit 0
    fi

    exit 1
fi

# Download Firecracker
echo "Downloading Firecracker ${VERSION}..."
DOWNLOAD_URL="https://github.com/firecracker-microvm/firecracker/releases/download/${VERSION}/firecracker-${VERSION}-${ARCH}.tgz"

TMP_DIR=$(mktemp -d)
cd "$TMP_DIR"

curl -L -o firecracker.tgz "$DOWNLOAD_URL"
tar -xzf firecracker.tgz

# Install binaries
echo "Installing to ${INSTALL_DIR}..."
cp release-${VERSION}-${ARCH}/firecracker-${VERSION}-${ARCH} "${INSTALL_DIR}/firecracker"
chmod +x "${INSTALL_DIR}/firecracker"

# Also install jailer if it exists
if [ -f "release-${VERSION}-${ARCH}/jailer-${VERSION}-${ARCH}" ]; then
    cp release-${VERSION}-${ARCH}/jailer-${VERSION}-${ARCH} "${INSTALL_DIR}/jailer"
    chmod +x "${INSTALL_DIR}/jailer"
fi

# Cleanup
cd -
rm -rf "$TMP_DIR"

echo "✓ Firecracker installed successfully!"
echo ""
echo "Verify installation:"
echo "  ${INSTALL_DIR}/firecracker --version"
echo ""

# Check for KVM support
if [ ! -e /dev/kvm ]; then
    echo "Warning: /dev/kvm not found. KVM support is required for Firecracker."
    echo "You may need to:"
    echo "  1. Enable KVM in your BIOS"
    echo "  2. Load the KVM kernel module: sudo modprobe kvm kvm_intel (or kvm_amd)"
    echo "  3. Add your user to the kvm group: sudo usermod -aG kvm \$USER"
    echo ""
fi

# Check permissions on /dev/kvm
if [ -e /dev/kvm ] && [ ! -w /dev/kvm ]; then
    echo "Warning: /dev/kvm is not writable by current user."
    echo "Run: sudo chmod 666 /dev/kvm"
    echo "Or add user to kvm group: sudo usermod -aG kvm \$USER"
    echo ""
fi

echo "Next steps:"
echo "  1. Download a kernel image and rootfs for your agents"
echo "  2. Configure Aether to use Firecracker"
echo "  3. Run: make build && make run"
echo ""
echo "For development on macOS, see: docs/operations/macos-development.md"
