# Validators

Self-validating hooks for skills and agents. These run automatically after tool usage to provide immediate feedback.

## Available Validators

### test-runner.sh
**Purpose:** Run tests and report concise pass/fail summary.

**Timeout:** 120s

**Detection:**
- `package.json` → npm test
- `pyproject.toml` / `pytest.ini` → pytest
- `go.mod` → go test
- `Cargo.toml` → cargo test

**Output Examples:**
- ✅ `Tests passed (42 tests in 3.2s)`
- ⚠️ `3 tests failed: test_login, test_auth, test_session`
- `No test framework detected`

---

### code-quality.sh
**Purpose:** Combined lint + format check + type check.

**Timeout:** 60s

**Tools Used:**
- **JavaScript/TypeScript:** eslint, prettier, tsc
- **Python:** ruff, black, pyright
- **Go:** golangci-lint, gofmt

**Output Examples:**
- ✅ `Code quality OK (lint, format, types)`
- ⚠️ `2 lint errors, 1 type error in user.ts:45`

---

### build-validator.sh
**Purpose:** Ensure build/compile succeeds.

**Timeout:** 180s

**Detection:**
- `package.json` with "build" script → npm run build
- `go.mod` → go build
- `Cargo.toml` → cargo build
- `Makefile` → make
- `pyproject.toml` with build-system → python -m build

**Output Examples:**
- ✅ `Build succeeded`
- ❌ `Build failed: TS2304 Cannot find name 'User' in api/routes.ts:23`

---

### api-validator.sh
**Purpose:** Validate OpenAPI/Swagger specifications.

**Timeout:** 30s

**Detection:** Files named `*openapi*` or `*swagger*` with `.yaml`, `.yml`, or `.json` extensions.

**Tools Used:** swagger-cli, openapi-generator-cli, spectral (in order of preference)

**Output Examples:**
- ✅ `OpenAPI spec valid`
- ❌ `OpenAPI error: Missing required field 'responses' in POST /users`

---

### migration-validator.sh
**Purpose:** Check migration safety (warns but doesn't block).

**Timeout:** 30s

**Detection:** Files in migration directories (Prisma, Knex, Django, Rails, Alembic).

**Checks For:**
- DROP operations (tables, columns, indexes)
- TRUNCATE statements
- DELETE without WHERE
- NOT NULL without DEFAULT
- Column type changes
- RENAME operations

**Output Examples:**
- ✅ `Migration safe: reversible, no data loss risk`
- ⚠️ `Migration warning: DROP COLUMN detected - ensure data backup`

---

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `CLAUDE_PROJECT_DIR` | Project root directory (defaults to `.`) |
| `CLAUDE_VALIDATOR_VERBOSE` | Set to `1` for verbose logging to `.claude/logs/` |

## Customizing Validators

### Adding a New Validator

1. Create a new script in `.claude/hooks/validators/`
2. Follow the template pattern:

```bash
#!/bin/bash
set -e

INPUT=$(cat)

# Extract file path from hook input
FILE_PATH=$(echo "$INPUT" | python3 -c "
import sys, json
data = json.load(sys.stdin)
print(data.get('tool_input', {}).get('file_path', ''))
" 2>/dev/null || echo "")

# If no file path, skip validation
if [ -z "$FILE_PATH" ]; then
    echo '{"continue": true}'
    exit 0
fi

cd "${CLAUDE_PROJECT_DIR:-.}"

# ... your validation logic here ...

# Output concise result
echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"Your message\"}}"
exit 0
```

3. Make it executable: `chmod +x your-validator.sh`
4. Add to skill/agent frontmatter

### Hook Configuration in Skills/Agents

```yaml
---
name: your-skill
hooks:
  post-tool-use:
    - matcher: "Edit|Write"     # Regex for tool names
      command: ".claude/hooks/validators/your-validator.sh"
      timeout: 60               # Seconds
  stop:
    - command: ".claude/hooks/validators/another-validator.sh"
      timeout: 30
---
```

## Logs

When `CLAUDE_VALIDATOR_VERBOSE=1`, validators log to `.claude/logs/`:
- `test-runner.log`
- `code-quality.log`
- `build-validator.log`
- `api-validator.log`
- `migration-validator.log`

These logs are gitignored by default.

## Graceful Degradation

All validators are designed to fail gracefully:
- If a tool is not installed, the validator skips that check
- If a file doesn't exist, the validator returns success
- Errors in the validator itself don't block the agent/skill

## Testing Validators

```bash
# Test individual validator
echo '{"tool_input": {"file_path": "src/index.ts"}}' | .claude/hooks/validators/test-runner.sh

# Test with verbose logging
CLAUDE_VALIDATOR_VERBOSE=1 echo '{"tool_input": {"file_path": "src/index.ts"}}' | .claude/hooks/validators/code-quality.sh
```
