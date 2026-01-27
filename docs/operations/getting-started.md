# Getting Started with Aether

This guide will help you get Aether up and running for development.

## Prerequisites

### Required
- **Go 1.24+** - [Download](https://go.dev/dl/)
- **Git** - For version control
- **Make** - For build automation

### Optional (for full functionality)
- **Firecracker** - MicroVM runtime (Linux x86_64 only)
- **Docker & Docker Compose** - For development dependencies
- **golangci-lint** - For code linting

## Platform Support

### Linux (Recommended)
Full support with Firecracker microVMs.

### macOS (Development Only)
Firecracker requires Linux and KVM. On macOS, you can:
- Run tests and develop the codebase
- Use mock implementations for testing
- Run Firecracker in a Linux VM or Docker

### Windows
Use WSL2 with Linux kernel 5.10+. See [WSL2 setup guide](wsl2-setup.md).

## Quick Start

### 1. Clone the Repository

```bash
git clone https://github.com/dnakitare/aether.git
cd aether
```

### 2. Install Dependencies

```bash
# Install Go dependencies
go mod download

# Install development tools
make tools
```

### 3. Build Aether

```bash
make build
```

This creates the binary at `bin/aether`.

### 4. Run Tests

```bash
make test
```

### 5. Start Development Environment

```bash
# Start Redis and PostgreSQL
make dev

# In another terminal, run Aether daemon
make run
```

## Setting Up Firecracker (Linux Only)

### Automatic Installation

```bash
./scripts/setup-firecracker.sh
```

### Manual Installation

1. Download Firecracker:
```bash
ARCH=$(uname -m)
VERSION=v1.7.0
curl -L https://github.com/firecracker-microvm/firecracker/releases/download/${VERSION}/firecracker-${VERSION}-${ARCH}.tgz -o firecracker.tgz
tar -xzf firecracker.tgz
sudo cp release-${VERSION}-${ARCH}/firecracker-${VERSION}-${ARCH} /usr/local/bin/firecracker
sudo chmod +x /usr/local/bin/firecracker
```

2. Verify KVM access:
```bash
# Check if /dev/kvm exists
ls -l /dev/kvm

# Add user to kvm group if needed
sudo usermod -aG kvm $USER
```

3. Download kernel and rootfs:
```bash
# Create images directory
sudo mkdir -p /var/lib/aether/images

# Download minimal kernel (example)
# You'll need to build or download a compatible kernel and rootfs
# See: https://github.com/firecracker-microvm/firecracker/blob/main/docs/getting-started.md
```

## Development Workflow

### Building

```bash
# Build with debug symbols
make build-debug

# Build for production (optimized)
make build
```

### Testing

```bash
# Run all tests
make test

# Run with coverage
make coverage

# Run specific package tests
go test -v ./pkg/api/...
```

### Linting

```bash
# Run linters
make lint

# Auto-format code
make fmt
```

### Verification

Run all checks before committing:

```bash
make verify
```

This runs:
- Code formatting check
- `go vet`
- `golangci-lint`
- All tests

## Project Structure

```
aether/
├── cmd/
│   └── aether/              # CLI application
├── internal/
│   ├── runtime/             # Core runtime
│   │   ├── vm/              # Firecracker VM management
│   │   └── agent/           # Agent abstraction
│   └── observability/       # Logging and metrics
├── pkg/
│   └── api/                 # Public API types
├── deployments/
│   ├── docker/              # Docker Compose files
│   └── terraform/           # Infrastructure as Code
├── docs/                    # Documentation
├── tests/                   # Integration tests
└── scripts/                 # Setup and utility scripts
```

## Using the CLI

### Start the Daemon

```bash
./bin/aether daemon
```

With debug logging:

```bash
./bin/aether --log-level=debug daemon
```

### Create an Agent

```bash
./bin/aether agent create \
  --name my-agent \
  --image python:3.11 \
  --cpu 2 \
  --memory 1024
```

### List Agents

```bash
./bin/aether agent list
```

### View Agent Logs

```bash
./bin/aether agent logs <agent-id>

# Follow logs
./bin/aether agent logs -f <agent-id>
```

### Check Agent Health

```bash
./bin/aether agent health <agent-id>
```

### Stop an Agent

```bash
./bin/aether agent stop <agent-id>
```

### Destroy an Agent

```bash
./bin/aether agent destroy <agent-id>
```

## Configuration

### Environment Variables

- `FIRECRACKER_VERSION` - Firecracker version to install (default: v1.7.0)
- `INSTALL_DIR` - Installation directory (default: /usr/local/bin)

### Config File (Future)

Configuration via YAML file will be added in Phase 2.

## Troubleshooting

### "firecracker not found"

Ensure Firecracker is installed and in your PATH:

```bash
which firecracker
firecracker --version
```

### "permission denied" on /dev/kvm

Add your user to the kvm group:

```bash
sudo usermod -aG kvm $USER
# Log out and back in for changes to take effect
```

### "failed to create tap device"

Creating tap devices requires `CAP_NET_ADMIN` capability. Either:

1. Run as root (not recommended for development)
2. Grant capability to Firecracker binary:
```bash
sudo setcap cap_net_admin+ep $(which firecracker)
```

### Tests failing

Ensure all dependencies are installed:

```bash
go mod download
go mod verify
```

## Next Steps

- Read the [Architecture Documentation](../architecture/phase1-foundation.md)
- Explore the [API Reference](../api/runtime-api.md)
- Check out [Contributing Guidelines](../../CONTRIBUTING.md) (coming soon)

## Getting Help

- **GitHub Issues**: https://github.com/dnakitare/aether/issues
- **Documentation**: `/docs` directory
- **Examples**: `/examples` directory (coming in Phase 2)

## Development Tips

### Use Makefile targets

The Makefile provides convenient targets for common tasks:

```bash
make help  # See all available targets
```

### Enable auto-formatting on save

Configure your editor to run `gofmt` on save. For VS Code:

```json
{
  "go.formatTool": "gofmt",
  "editor.formatOnSave": true
}
```

### Run specific tests

```bash
# Run tests in a specific package
go test -v ./internal/runtime/vm

# Run a specific test
go test -v -run TestVMConfig ./internal/runtime/vm
```

### Debug with delve

```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug the daemon
dlv debug ./cmd/aether -- daemon
```
