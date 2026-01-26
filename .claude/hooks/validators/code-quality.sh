#!/bin/bash
# code-quality.sh - Combined lint + format check + type check
# Timeout: 60s
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

# Track issues
ISSUES=()
HAS_ERRORS=0

# Check Node.js/TypeScript project
if [ -f "package.json" ]; then
    # ESLint check
    if [ -f "node_modules/.bin/eslint" ] || command -v eslint &>/dev/null; then
        LINT_OUTPUT=$(npx eslint --format compact "$FILE_PATH" 2>&1) || true
        LINT_ERRORS=$(echo "$LINT_OUTPUT" | grep -c 'Error' || echo "0")
        if [ "$LINT_ERRORS" -gt 0 ]; then
            ISSUES+=("$LINT_ERRORS lint errors")
            HAS_ERRORS=1
        fi
    fi

    # TypeScript check
    if [ -f "tsconfig.json" ]; then
        TSC_OUTPUT=$(npx tsc --noEmit 2>&1) || true
        TYPE_ERRORS=$(echo "$TSC_OUTPUT" | grep -c 'error TS' || echo "0")
        if [ "$TYPE_ERRORS" -gt 0 ]; then
            # Get first error location
            FIRST_ERROR=$(echo "$TSC_OUTPUT" | grep 'error TS' | head -1 | sed 's/(.*//' || echo "")
            ISSUES+=("$TYPE_ERRORS type errors${FIRST_ERROR:+ in $FIRST_ERROR}")
            HAS_ERRORS=1
        fi
    fi

    # Prettier check (format)
    if [ -f "node_modules/.bin/prettier" ] || command -v prettier &>/dev/null; then
        if ! npx prettier --check "$FILE_PATH" &>/dev/null; then
            ISSUES+=("format issues")
        fi
    fi
fi

# Check Python project
if [ -f "pyproject.toml" ] || [ -f "setup.py" ] || [[ "$FILE_PATH" == *.py ]]; then
    # Ruff (fast Python linter)
    if command -v ruff &>/dev/null; then
        RUFF_OUTPUT=$(ruff check "$FILE_PATH" 2>&1) || true
        RUFF_ERRORS=$(echo "$RUFF_OUTPUT" | grep -cE '^[^:]+:[0-9]+:' || echo "0")
        if [ "$RUFF_ERRORS" -gt 0 ]; then
            ISSUES+=("$RUFF_ERRORS lint issues")
            HAS_ERRORS=1
        fi
    fi

    # Black (format check)
    if command -v black &>/dev/null; then
        if ! black --check "$FILE_PATH" &>/dev/null 2>&1; then
            ISSUES+=("format issues")
        fi
    fi

    # Pyright (type check)
    if command -v pyright &>/dev/null; then
        PYRIGHT_OUTPUT=$(pyright "$FILE_PATH" 2>&1) || true
        PYRIGHT_ERRORS=$(echo "$PYRIGHT_OUTPUT" | grep -c 'error:' || echo "0")
        if [ "$PYRIGHT_ERRORS" -gt 0 ]; then
            ISSUES+=("$PYRIGHT_ERRORS type errors")
            HAS_ERRORS=1
        fi
    fi
fi

# Check Go project
if [ -f "go.mod" ] || [[ "$FILE_PATH" == *.go ]]; then
    # golangci-lint
    if command -v golangci-lint &>/dev/null; then
        GOLINT_OUTPUT=$(golangci-lint run "$FILE_PATH" 2>&1) || true
        GOLINT_ERRORS=$(echo "$GOLINT_OUTPUT" | grep -cE '^[^:]+:[0-9]+:' || echo "0")
        if [ "$GOLINT_ERRORS" -gt 0 ]; then
            ISSUES+=("$GOLINT_ERRORS lint issues")
            HAS_ERRORS=1
        fi
    fi

    # gofmt check
    if command -v gofmt &>/dev/null; then
        if [ -n "$(gofmt -l "$FILE_PATH" 2>/dev/null)" ]; then
            ISSUES+=("format issues")
        fi
    fi
fi

# Build result message
if [ ${#ISSUES[@]} -eq 0 ]; then
    RESULT="Code quality OK (lint, format, types)"
else
    RESULT=$(IFS=', '; echo "${ISSUES[*]}")
fi

# Log verbose output if enabled
if [ "$CLAUDE_VALIDATOR_VERBOSE" = "1" ]; then
    mkdir -p .claude/logs
    echo "$(date -Iseconds) | $FILE_PATH | $RESULT" >> .claude/logs/code-quality.log
fi

# Output concise result
if [ $HAS_ERRORS -eq 0 ]; then
    echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"$RESULT\"}}"
else
    echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"⚠️ $RESULT\"}}"
fi
exit 0
