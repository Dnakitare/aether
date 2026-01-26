#!/bin/bash
# Type check hook for Claude Solo Dev
# PostToolUse hook - runs after Edit/Write to verify type safety
#
# Receives JSON via stdin with tool_input containing file_path
# Outputs JSON with type check results as context for Claude

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

# Get file extension
EXT="${FILE_PATH##*.}"

# Change to project directory
cd "${CLAUDE_PROJECT_DIR:-.}"

# Type check based on file type
case "$EXT" in
    ts|tsx)
        # TypeScript - run tsc if available
        if command -v npx &> /dev/null && [ -f "tsconfig.json" ]; then
            # Quick check - capture errors
            ERRORS=$(npx tsc --noEmit --pretty false 2>&1 | grep -E "^${FILE_PATH}" | head -5 || true)
            if [ -n "$ERRORS" ]; then
                # Report type errors as context (not blocking)
                ERRORS_ESCAPED=$(echo "$ERRORS" | python3 -c "import sys, json; print(json.dumps(sys.stdin.read()))" 2>/dev/null | sed 's/^"//;s/"$//')
                echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"TypeScript errors found:\\n$ERRORS_ESCAPED\"}}"
                exit 0
            fi
        fi
        ;;
    py)
        # Python - run mypy or pyright if available
        if command -v pyright &> /dev/null; then
            ERRORS=$(pyright "$FILE_PATH" 2>&1 | grep -E "error:" | head -5 || true)
            if [ -n "$ERRORS" ]; then
                ERRORS_ESCAPED=$(echo "$ERRORS" | python3 -c "import sys, json; print(json.dumps(sys.stdin.read()))" 2>/dev/null | sed 's/^"//;s/"$//')
                echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"Type errors found:\\n$ERRORS_ESCAPED\"}}"
                exit 0
            fi
        elif command -v mypy &> /dev/null; then
            ERRORS=$(mypy "$FILE_PATH" 2>&1 | grep -E "error:" | head -5 || true)
            if [ -n "$ERRORS" ]; then
                ERRORS_ESCAPED=$(echo "$ERRORS" | python3 -c "import sys, json; print(json.dumps(sys.stdin.read()))" 2>/dev/null | sed 's/^"//;s/"$//')
                echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"Type errors found:\\n$ERRORS_ESCAPED\"}}"
                exit 0
            fi
        fi
        ;;
esac

# No errors or not a typed language
echo '{"continue": true}'
exit 0
