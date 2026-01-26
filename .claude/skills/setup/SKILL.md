---
name: setup
description: Interactive first-time setup wizard. Configures preferences, tech stack, and personalizes the Claude Code environment for your project.
---

# Setup - First-Time Configuration

Interactive wizard to configure Claude Code for your project. Run once when you first copy this configuration to a new project.

## Usage

```
/setup              # Full interactive setup
/setup --quick      # Quick setup with smart defaults
/setup --reset      # Reset to defaults and reconfigure
```

## What Gets Configured

### 1. Project Basics
- Project name and description
- Primary programming language
- Framework/runtime (React, FastAPI, Go, etc.)
- Package manager (npm, yarn, pnpm, pip, go mod, cargo)

### 2. Code Style
- Formatting tool (Prettier, Black, gofmt, rustfmt, Pint)
- Linting tool (ESLint, Ruff, golangci-lint, clippy)
- Tab vs spaces, indent size
- Line length preference

### 3. Testing
- Test framework (Jest, Vitest, pytest, go test, cargo test)
- Coverage requirements (optional)
- Test file naming convention

### 4. Git Preferences
- Default branch name (main/master)
- Commit message style (conventional/freeform)
- Auto-stage preference

### 5. Optional Integrations
- MCP servers to enable
- Editor/IDE being used
- CI/CD platform

## Process

1. **Detect Existing Config**
   - Check for package.json, pyproject.toml, go.mod, Cargo.toml
   - Detect existing .prettierrc, .eslintrc, etc.
   - Read any existing settings

2. **Ask Questions**
   - Only ask what can't be auto-detected
   - Provide smart defaults based on detection
   - Allow skipping with defaults

3. **Update Configuration**
   - Update `.claude/CLAUDE.md` with preferences
   - Configure hooks for detected tools
   - Update `.claude/settings.json` if needed

4. **Verify Setup**
   - Run format/lint to verify tools work
   - Check test command works
   - Confirm git is configured

## Example Session

```
/setup

🔧 Claude Code Setup Wizard

Detected:
  ✓ package.json found (Node.js project)
  ✓ TypeScript configuration (tsconfig.json)
  ✓ Prettier configuration (.prettierrc)
  ✓ Jest for testing

Questions:

1. Project name: [my-app] ▸
2. Primary framework: [React] ▸ Next.js
3. Package manager: [npm] ▸ pnpm
4. Line length preference: [80] ▸ 100

Configuring...
  ✓ Updated CLAUDE.md with project context
  ✓ Configured auto-format hook for Prettier
  ✓ Configured type-check hook for TypeScript
  ✓ Set test command: pnpm test

Setup complete! Your preferences have been saved.

Quick commands:
  pnpm test      - Run tests
  pnpm run lint  - Check linting
  pnpm run build - Build project
```

## Configuration Storage

Preferences are stored in `.claude/CLAUDE.md` under "Personal Conventions":

```markdown
## Personal Conventions

- **Project**: my-app (Next.js)
- **Language**: TypeScript
- **Package Manager**: pnpm
- **Formatter**: Prettier (100 char lines)
- **Linter**: ESLint
- **Test Framework**: Jest
- **Test Command**: `pnpm test`
- **Build Command**: `pnpm run build`
```

## Auto-Detection

The setup wizard automatically detects:

| File | Detection |
|------|-----------|
| `package.json` | Node.js, dependencies, scripts |
| `tsconfig.json` | TypeScript project |
| `pyproject.toml` | Python project, tools |
| `go.mod` | Go project |
| `Cargo.toml` | Rust project |
| `composer.json` | PHP project |
| `.prettierrc` | Prettier config |
| `.eslintrc.*` | ESLint config |
| `ruff.toml` | Ruff config |
| `jest.config.*` | Jest testing |
| `vitest.config.*` | Vitest testing |
| `pytest.ini` | Pytest config |

## Quick Setup Mode

With `--quick`, uses all detected/default values without prompting:

```
/setup --quick

🔧 Quick Setup

Auto-configured based on project detection:
  ✓ Node.js + TypeScript
  ✓ Prettier formatting
  ✓ ESLint linting
  ✓ Jest testing
  ✓ npm package manager

Run /setup to customize further.
```

## Reset Mode

With `--reset`, clears existing preferences:

```
/setup --reset

⚠️  This will reset your Claude Code configuration.
Existing preferences in CLAUDE.md will be cleared.

Continue? [y/N]
```

## Post-Setup

After setup completes:

1. **Hooks are configured** - Format and type-check run automatically
2. **Commands work** - Test, lint, build commands are known
3. **Context is set** - Claude understands your project stack
4. **Ready to code** - Start with `/start` or just describe what you need

## Updating Settings

To change settings later:
- Run `/setup` again to reconfigure
- Or manually edit `.claude/CLAUDE.md`
