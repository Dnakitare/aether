# Contributing to Aether

Thank you for your interest in contributing to Aether! We welcome contributions from the community.

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

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

- Go 1.21 or later
- Docker and Docker Compose
- Firecracker (for local testing)
- Make

### Getting Started

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/aether.git
cd aether

# Install dependencies
go mod download

# Build
make build

# Run tests
make test

# Run linters
make lint
```

### Running Locally

```bash
# Start dependencies (PostgreSQL, Redis, etcd, Kafka)
docker-compose -f deployments/docker/docker-compose.dev.yml up -d

# Run database migrations
./aether migrate up

# Start Aether server
./aether server

# In another terminal, test it
./aether agent create test-agent --image python:3.11-slim
```

## Coding Standards

### Go Style

- Follow [Effective Go](https://golang.org/doc/effective_go)
- Use `gofmt` for formatting (enforced by CI)
- Run `golangci-lint` and fix all issues
- Keep functions under 50 lines when possible
- Use meaningful variable names (no single letters except loops)

### Testing

- Write table-driven tests where appropriate
- Test both success and failure paths
- Use `testify` for assertions
- Mock external dependencies (use interfaces)
- Aim for 80%+ code coverage

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

1. Copy `docs/architecture/adr/000-template.md`
2. Fill in the ADR with context, decision, and consequences
3. Submit as part of your PR

## Community

- **Discord**: [Join our Discord](https://discord.gg/aether) (TBD)
- **GitHub Discussions**: For questions and ideas
- **GitHub Issues**: For bugs and features

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.

## Recognition

Contributors will be recognized in:
- GitHub contributors page
- CHANGELOG.md (for significant contributions)
- Release notes

Thank you for contributing to Aether! 🚀
