#!/bin/bash
# test-runner.sh - Run tests and report concise pass/fail summary
# Timeout: 120s
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

# Detect test framework and run tests
run_tests() {
    local output=""
    local exit_code=0
    local test_count=0
    local failed_count=0
    local duration=""

    if [ -f "package.json" ]; then
        # Node.js project - check for test script
        if grep -q '"test"' package.json 2>/dev/null; then
            output=$(npm test 2>&1) || exit_code=$?
            # Parse npm test output
            if [ $exit_code -eq 0 ]; then
                # Try to extract test count from common formats
                test_count=$(echo "$output" | grep -oE '[0-9]+ (passing|passed|tests?)' | grep -oE '[0-9]+' | head -1 || echo "")
                duration=$(echo "$output" | grep -oE '[0-9]+(\.[0-9]+)?s' | tail -1 || echo "")
                if [ -n "$test_count" ]; then
                    echo "Tests passed (${test_count} tests${duration:+ in $duration})"
                else
                    echo "Tests passed"
                fi
            else
                # Extract failed test names
                failed_tests=$(echo "$output" | grep -E '(FAIL|✗|✕|failing)' | head -3 | tr '\n' ', ' | sed 's/,$//')
                failed_count=$(echo "$output" | grep -cE '(FAIL|✗|✕)' || echo "0")
                if [ -n "$failed_tests" ]; then
                    echo "${failed_count} tests failed: ${failed_tests}"
                else
                    echo "Tests failed"
                fi
            fi
            return $exit_code
        fi
    fi

    if [ -f "pyproject.toml" ] || [ -f "pytest.ini" ] || [ -f "setup.py" ]; then
        # Python project
        if command -v pytest &>/dev/null; then
            output=$(pytest --tb=no -q 2>&1) || exit_code=$?
            if [ $exit_code -eq 0 ]; then
                test_count=$(echo "$output" | grep -oE '[0-9]+ passed' | grep -oE '[0-9]+' || echo "")
                duration=$(echo "$output" | grep -oE 'in [0-9]+\.[0-9]+s' | sed 's/in //' || echo "")
                echo "Tests passed (${test_count:-all} tests${duration:+ in $duration})"
            else
                failed_count=$(echo "$output" | grep -oE '[0-9]+ failed' | grep -oE '[0-9]+' || echo "?")
                failed_tests=$(echo "$output" | grep -E '^FAILED' | head -3 | sed 's/FAILED //' | tr '\n' ', ' | sed 's/,$//')
                echo "${failed_count} tests failed${failed_tests:+: $failed_tests}"
            fi
            return $exit_code
        fi
    fi

    if [ -f "go.mod" ]; then
        # Go project
        output=$(go test ./... 2>&1) || exit_code=$?
        if [ $exit_code -eq 0 ]; then
            test_count=$(echo "$output" | grep -c 'ok' || echo "")
            echo "Tests passed (${test_count:-all} packages)"
        else
            failed_tests=$(echo "$output" | grep -E '^---\s*FAIL' | head -3 | sed 's/.*FAIL: //' | tr '\n' ', ' | sed 's/,$//')
            echo "Tests failed${failed_tests:+: $failed_tests}"
        fi
        return $exit_code
    fi

    if [ -f "Cargo.toml" ]; then
        # Rust project
        output=$(cargo test 2>&1) || exit_code=$?
        if [ $exit_code -eq 0 ]; then
            test_count=$(echo "$output" | grep -oE '[0-9]+ passed' | grep -oE '[0-9]+' || echo "")
            echo "Tests passed (${test_count:-all} tests)"
        else
            failed_count=$(echo "$output" | grep -oE '[0-9]+ failed' | grep -oE '[0-9]+' || echo "?")
            echo "${failed_count} tests failed"
        fi
        return $exit_code
    fi

    # No test framework detected
    echo "No test framework detected"
    return 0
}

# Run tests and capture result
TEST_RESULT=$(run_tests 2>&1)
TEST_EXIT_CODE=$?

# Log verbose output if enabled
if [ "$CLAUDE_VALIDATOR_VERBOSE" = "1" ]; then
    mkdir -p .claude/logs
    echo "$(date -Iseconds) | $FILE_PATH | $TEST_RESULT" >> .claude/logs/test-runner.log
fi

# Output concise result
if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"$TEST_RESULT\"}}"
else
    echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"⚠️ $TEST_RESULT\"}}"
fi
exit 0
