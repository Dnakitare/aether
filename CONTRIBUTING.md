# Contributing to Aether

Thank you for your interest in contributing to Aether! We're excited to have you join our community building a modern AI agent runtime.

**Project Status:** Beta v0.2.0 - single-region control plane with the core components integrated: HTTP API, in-process scheduler, PostgreSQL state, observability, and deployment automation

---

## 📜 Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

**In short:** Be respectful, collaborative, and constructive. We're building something great together!

---

## 🚀 First Time Contributors

Never contributed to open source before? Welcome! Here are some helpful resources:
- [How to Contribute to Open Source](https://opensource.guide/how-to-contribute/)
- [First Timers Only](https://www.firsttimersonly.com/)
- [GitHub Flow Guide](https://docs.github.com/en/get-started/quickstart/github-flow)

### Good First Issues

Look for issues labeled `good first issue` - these are specifically chosen for newcomers:
- [Good First Issues](https://github.com/Dnakitare/aether/labels/good%20first%20issue)
- These issues have clear descriptions and limited scope
- Maintainers will provide extra guidance on these issues

### Priority Areas for Contributions

🔴 **High Priority** (Beta v0.2.0 focus):
- **Observability**: OpenTelemetry tracing, Prometheus metrics, Grafana dashboards
- **Performance**: Load testing, optimization, profiling
- **Testing**: Increase coverage to 60%+, add chaos tests
- **Bug Fixes**: Any and all bug fixes welcome

🟡 **Medium Priority**:
- **CLI Enhancements**: Improve user experience, add commands
- **Documentation**: Tutorials, guides, API docs
- **Examples**: Sample applications, integration examples
- **Deployment**: Terraform modules, Helm charts

🟢 **Low Priority**:
- **Code Quality**: Refactoring, cleanup
- **Additional Tests**: Edge cases, integration tests
- **Minor Features**: Nice-to-have enhancements

---

## How to Contribute

### Reporting Bugs

Before creating bug reports, please check existing issues. When creating a bug report, include:

- **Clear title and description**
- **Steps to reproduce** the issue
- **Expected behavior** vs actual behavior
- **Environment details** (OS, Go version, etc.)
- **Logs or error messages** if applicable

### Suggesting Features

Feature suggestions are welcome! Please:

- **Check existing issues** to avoid duplicates
- **Describe the problem** you're trying to solve
- **Propose a solution** with use cases
- **Consider alternatives** you've thought about

### Pull Requests

1. **Fork the repository** and create your branch from `main`
2. **Follow the coding standards** (see below)
3. **Write tests** for new features (aim for 80%+ coverage)
4. **Update documentation** for user-facing changes
5. **Run the test suite** (`make test`)
6. **Run linters** (`make lint`)
7. **Write clear commit messages** (see below)

## Development Setup

### Prerequisites

- Go 1.24 or later
- Docker and Docker Compose
- Firecracker (for local testing)
- Make

### Getting Started

```bash
# 1. Fork the repository on GitHub
# Click "Fork" at https://github.com/Dnakitare/aether

# 2. Clone your fork
git clone https://github.com/YOUR_USERNAME/aether.git
cd aether

# 3. Add upstream remote
git remote add upstream https://github.com/Dnakitare/aether.git

# 4. Install dependencies
go mod download

# 5. Build
go build -o aether ./cmd/aether

# 6. Run tests (short mode)
go test -short ./...

# 7. Run linters (if available)
golangci-lint run
```

### Running Locally

```bash
# 1. Start infrastructure (PostgreSQL, Redis)
docker-compose up -d

# Wait for services to be ready (5-10 seconds)
sleep 5

# 2. Set environment variables
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/aether?sslmode=disable"
export JWT_SECRET="dev-secret-key-change-in-production"
export SERVER_ADDRESS=":8080"

# 3. Start Aether server
./aether server

# Server will start on http://localhost:8080
# Logs will show: "Aether server started successfully"

# 4. In another terminal, test the API
curl http://localhost:8080/health

# 5. Stop infrastructure when done
docker-compose -f deployments/docker/docker-compose.dev.yml down
```

### Development Workflow

```bash
# Create a feature branch
git checkout -b feature/your-feature-name

# Make your changes
# ... edit files ...

# Run tests
go test ./...

# Format code
go fmt ./...

# Commit changes
git add .
git commit -m "feat(component): description of changes"

# Keep your branch updated
git fetch upstream
git rebase upstream/main

# Push to your fork
git push origin feature/your-feature-name

# Open a Pull Request on GitHub
```

## Coding Standards

### Go Style

- Follow [Effective Go](https://golang.org/doc/effective_go)
- Use `gofmt` for formatting (enforced by CI)
- Run `golangci-lint` and fix all issues
- Keep functions under 50 lines when possible
- Use meaningful variable names (no single letters except loops)

### Testing Requirements

**Coverage Goals:**
- **Minimum**: 60% coverage for new code (Beta target)
- **Current**: ~35% overall (Alpha baseline)
- **Target**: 70%+ for critical paths

**Test Types:**

1. **Unit Tests** (Required for all new code)
```bash
# Run all unit tests
go test -short ./...

# Run with coverage
go test -short -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

2. **Integration Tests** (If modifying integration points)
```bash
# Start test infrastructure
docker-compose -f docker-compose.test.yml up -d

# Run integration tests
go test ./tests/integration/...

# Cleanup
docker-compose -f docker-compose.test.yml down
```

3. **E2E Tests** (If modifying agent lifecycle)
```bash
# Run end-to-end tests
go test -v ./tests/integration/e2e_workflow_test.go
```

**Testing Guidelines:**
- Write table-driven tests where appropriate
- Test both success and failure paths
- Use `testify/require` and `testify/assert`
- Mock external dependencies (use interfaces)
- Tests should be fast (<1s per unit test)
- Tests should be deterministic (no flaky tests)
- Tests should be isolated (no shared state)

Example test structure:

```go
func TestCreateAgent(t *testing.T) {
    tests := []struct {
        name    string
        input   AgentConfig
        want    *AgentInfo
        wantErr bool
    }{
        {
            name: "valid agent",
            input: AgentConfig{Name: "test", Image: "python:3.11"},
            want: &AgentInfo{Name: "test"},
            wantErr: false,
        },
        // More test cases...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

### Error Handling

- Return errors, don't panic (except in truly exceptional cases)
- Wrap errors with context: `fmt.Errorf("failed to create agent: %w", err)`
- Log errors with structured logging (slog)

### Documentation

- Add godoc comments for exported functions/types
- Include examples in godoc where helpful
- Update user-facing docs in `docs/` for feature changes

## Commit Message Guidelines

We follow conventional commits:

```
<type>(<scope>): <subject>

<body>

<footer>
```

### Types

- **feat**: New feature
- **fix**: Bug fix
- **docs**: Documentation changes
- **refactor**: Code refactoring (no behavior change)
- **test**: Adding or updating tests
- **chore**: Maintenance tasks (dependencies, build, etc.)
- **perf**: Performance improvements

### Examples

```
feat(scheduler): add GPU support for agent placement

Implement GPU resource tracking and placement decisions for agents
that require GPU acceleration. Includes Firecracker GPU passthrough
configuration.

Closes #123
```

```
fix(api): prevent race condition in agent creation

Add mutex lock around agent state initialization to prevent
concurrent creation requests from causing data corruption.

Fixes #456
```

## Pull Request Process

1. **Update CHANGELOG.md** with your changes (if applicable)
2. **Ensure CI passes** (tests, lints, security scans)
3. **Request review** from maintainers
4. **Address feedback** promptly
5. **Squash commits** if requested before merge

### PR Title Format

Use conventional commit format:

```
feat(component): description
fix(component): description
docs(component): description
```

### PR Description Template

```markdown
## Description
Brief description of changes

## Motivation
Why is this change needed?

## Changes
- List of changes made
- Another change

## Testing
How was this tested?

## Checklist
- [ ] Tests added/updated
- [ ] Documentation updated
- [ ] CHANGELOG.md updated (if applicable)
- [ ] CI passes
```

## Architecture Decisions

For significant architectural changes, create an Architecture Decision Record (ADR):

1. Create a new file `docs/architecture/adr/NNN-title.md` (see existing ADRs 001-008 for format)
2. Fill in the ADR with context, decision, and consequences
3. Submit as part of your PR

## 👥 Community

### Get Help

- **GitHub Discussions**: [Ask questions and share ideas](https://github.com/Dnakitare/aether/discussions)
- **GitHub Issues**: [Report bugs and request features](https://github.com/Dnakitare/aether/issues)
- **Documentation**: Check [docs/](docs/) for architecture and guides

### Stay Updated

- **Watch** the repository for notifications on new issues and PRs
- **Star** the repository to show support and follow the project
- **Follow** releases to get notified of new versions

### Resources

- **Alpha Release Notes**: [ALPHA_RELEASE_NOTES.md](ALPHA_RELEASE_NOTES.md)
- **Architecture Docs**: [docs/architecture/](docs/architecture/)
- **API Documentation**: Coming in Beta v0.2.0

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.

## Recognition

Contributors will be recognized in:
- GitHub contributors page
- CHANGELOG.md (for significant contributions)
- Release notes

Thank you for contributing to Aether! 🚀
