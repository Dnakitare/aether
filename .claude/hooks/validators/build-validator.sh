#!/bin/bash
# build-validator.sh - Ensure build/compile succeeds
# Timeout: 180s
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

# Run build and capture result
run_build() {
    local output=""
    local exit_code=0

    if [ -f "package.json" ]; then
        # Node.js project - check for build script
        if grep -q '"build"' package.json 2>/dev/null; then
            output=$(npm run build 2>&1) || exit_code=$?
            if [ $exit_code -eq 0 ]; then
                echo "Build succeeded"
            else
                # Extract first error
                ERROR_LINE=$(echo "$output" | grep -E '(error|Error|ERROR)' | head -1 | sed 's/^[[:space:]]*//' | cut -c1-80)
                if [ -n "$ERROR_LINE" ]; then
                    echo "Build failed: $ERROR_LINE"
                else
                    echo "Build failed"
                fi
            fi
            return $exit_code
        fi
    fi

    if [ -f "go.mod" ]; then
        # Go project
        output=$(go build ./... 2>&1) || exit_code=$?
        if [ $exit_code -eq 0 ]; then
            echo "Build succeeded"
        else
            ERROR_LINE=$(echo "$output" | grep -E ':.*:' | head -1 | cut -c1-80)
            echo "Build failed${ERROR_LINE:+: $ERROR_LINE}"
        fi
        return $exit_code
    fi

    if [ -f "Cargo.toml" ]; then
        # Rust project
        output=$(cargo build 2>&1) || exit_code=$?
        if [ $exit_code -eq 0 ]; then
            echo "Build succeeded"
        else
            ERROR_LINE=$(echo "$output" | grep -E '^error' | head -1 | cut -c1-80)
            echo "Build failed${ERROR_LINE:+: $ERROR_LINE}"
        fi
        return $exit_code
    fi

    if [ -f "Makefile" ]; then
        # Make-based project
        output=$(make 2>&1) || exit_code=$?
        if [ $exit_code -eq 0 ]; then
            echo "Build succeeded"
        else
            echo "Build failed"
        fi
        return $exit_code
    fi

    if [ -f "pyproject.toml" ]; then
        # Python project with build
        if grep -q '\[build-system\]' pyproject.toml 2>/dev/null; then
            output=$(python -m build 2>&1) || exit_code=$?
            if [ $exit_code -eq 0 ]; then
                echo "Build succeeded"
            else
                echo "Build failed"
            fi
            return $exit_code
        fi
    fi

    # No build system detected
    echo "No build system detected"
    return 0
}

# Run build and capture result
BUILD_RESULT=$(run_build 2>&1)
BUILD_EXIT_CODE=$?

# Log verbose output if enabled
if [ "$CLAUDE_VALIDATOR_VERBOSE" = "1" ]; then
    mkdir -p .claude/logs
    echo "$(date -Iseconds) | $FILE_PATH | $BUILD_RESULT" >> .claude/logs/build-validator.log
fi

# Output concise result
if [ $BUILD_EXIT_CODE -eq 0 ]; then
    echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"$BUILD_RESULT\"}}"
else
    echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"❌ $BUILD_RESULT\"}}"
fi
exit 0
