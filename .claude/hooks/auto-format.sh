#!/bin/bash
# Auto-format hook for Claude Solo Dev
# PostToolUse hook - runs after Edit/Write operations to format code
#
# Receives JSON via stdin with tool_input containing file_path
# Outputs JSON with formatting results

set -e

# Read JSON input from stdin
INPUT=$(cat)

# Parse file path from tool input
FILE_PATH=$(echo "$INPUT" | python3 -c "import sys, json; data = json.load(sys.stdin); print(data.get('tool_input', {}).get('file_path', ''))" 2>/dev/null || echo "")

# Exit successfully if no file path
if [ -z "$FILE_PATH" ]; then
    echo '{"continue": true}'
    exit 0
fi

# Exit successfully if file doesn't exist
if [ ! -f "$FILE_PATH" ]; then
    echo '{"continue": true}'
    exit 0
fi

# Get file extension
EXT="${FILE_PATH##*.}"

# Track if we formatted
FORMATTED=""

# Change to project directory
cd "${CLAUDE_PROJECT_DIR:-.}"

# Format based on file type
case "$EXT" in
    js|jsx|ts|tsx|json|md|css|scss|html)
        # JavaScript/TypeScript - use Prettier if available
        if command -v npx &> /dev/null && [ -f "node_modules/.bin/prettier" ]; then
            npx prettier --write "$FILE_PATH" 2>/dev/null && FORMATTED="prettier"
        elif command -v prettier &> /dev/null; then
            prettier --write "$FILE_PATH" 2>/dev/null && FORMATTED="prettier"
        fi
        ;;
    py)
        # Python - use Black or Ruff if available
        if command -v ruff &> /dev/null; then
            ruff format "$FILE_PATH" 2>/dev/null && FORMATTED="ruff"
        elif command -v black &> /dev/null; then
            black --quiet "$FILE_PATH" 2>/dev/null && FORMATTED="black"
        fi
        ;;
    go)
        # Go - use gofmt
        if command -v gofmt &> /dev/null; then
            gofmt -w "$FILE_PATH" 2>/dev/null && FORMATTED="gofmt"
        fi
        ;;
    rs)
        # Rust - use rustfmt
        if command -v rustfmt &> /dev/null; then
            rustfmt "$FILE_PATH" 2>/dev/null && FORMATTED="rustfmt"
        fi
        ;;
    php)
        # PHP - use php-cs-fixer or pint
        if command -v pint &> /dev/null; then
            pint "$FILE_PATH" 2>/dev/null && FORMATTED="pint"
        elif command -v php-cs-fixer &> /dev/null; then
            php-cs-fixer fix "$FILE_PATH" --quiet 2>/dev/null && FORMATTED="php-cs-fixer"
        fi
        ;;
    rb)
        # Ruby - use rubocop if available
        if command -v rubocop &> /dev/null; then
            rubocop -a "$FILE_PATH" --fail-level=error 2>/dev/null && FORMATTED="rubocop"
        fi
        ;;
esac

# Output JSON result
if [ -n "$FORMATTED" ]; then
    echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"File formatted with $FORMATTED\"}}"
else
    echo '{"continue": true}'
fi

exit 0
