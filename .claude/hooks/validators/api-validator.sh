#!/bin/bash
# api-validator.sh - Validate OpenAPI/Swagger specs
# Timeout: 30s
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

# Check if file is an OpenAPI spec
is_openapi_file() {
    local file="$1"
    case "$file" in
        *openapi*.yaml|*openapi*.yml|*openapi*.json|*swagger*.yaml|*swagger*.yml|*swagger*.json)
            return 0
            ;;
    esac

    # Check file content for openapi or swagger key
    if [ -f "$file" ]; then
        if head -20 "$file" 2>/dev/null | grep -qE '(openapi:|swagger:|"openapi"|"swagger")'; then
            return 0
        fi
    fi

    return 1
}

# Find OpenAPI files in project
find_openapi_files() {
    find . -maxdepth 3 -type f \( -name "*openapi*" -o -name "*swagger*" \) \( -name "*.yaml" -o -name "*.yml" -o -name "*.json" \) 2>/dev/null | head -5
}

# Validate OpenAPI spec
validate_openapi() {
    local spec_file="$1"
    local output=""
    local exit_code=0

    # Try swagger-cli if available
    if command -v swagger-cli &>/dev/null; then
        output=$(swagger-cli validate "$spec_file" 2>&1) || exit_code=$?
        if [ $exit_code -eq 0 ]; then
            echo "OpenAPI spec valid"
        else
            ERROR=$(echo "$output" | head -1 | cut -c1-60)
            echo "OpenAPI error: $ERROR"
        fi
        return $exit_code
    fi

    # Try openapi-generator-cli if available
    if command -v openapi-generator-cli &>/dev/null; then
        output=$(openapi-generator-cli validate -i "$spec_file" 2>&1) || exit_code=$?
        if [ $exit_code -eq 0 ]; then
            echo "OpenAPI spec valid"
        else
            ERROR=$(echo "$output" | grep -i error | head -1 | cut -c1-60)
            echo "OpenAPI error: ${ERROR:-validation failed}"
        fi
        return $exit_code
    fi

    # Try spectral if available
    if command -v spectral &>/dev/null; then
        output=$(spectral lint "$spec_file" 2>&1) || exit_code=$?
        ERROR_COUNT=$(echo "$output" | grep -c 'error' || echo "0")
        WARN_COUNT=$(echo "$output" | grep -c 'warning' || echo "0")
        if [ "$ERROR_COUNT" -eq 0 ]; then
            if [ "$WARN_COUNT" -gt 0 ]; then
                echo "OpenAPI spec valid ($WARN_COUNT warnings)"
            else
                echo "OpenAPI spec valid"
            fi
        else
            echo "OpenAPI spec has $ERROR_COUNT errors"
        fi
        return $exit_code
    fi

    # Try npx if in Node project
    if [ -f "package.json" ]; then
        output=$(npx --yes @apidevtools/swagger-cli validate "$spec_file" 2>&1) || exit_code=$?
        if [ $exit_code -eq 0 ]; then
            echo "OpenAPI spec valid"
        else
            ERROR=$(echo "$output" | head -1 | cut -c1-60)
            echo "OpenAPI error: $ERROR"
        fi
        return $exit_code
    fi

    # Basic YAML/JSON syntax check as fallback
    if [[ "$spec_file" == *.json ]]; then
        if python3 -c "import json; json.load(open('$spec_file'))" 2>/dev/null; then
            echo "OpenAPI spec syntax valid (install swagger-cli for full validation)"
        else
            echo "OpenAPI error: Invalid JSON syntax"
            return 1
        fi
    else
        if python3 -c "import yaml; yaml.safe_load(open('$spec_file'))" 2>/dev/null; then
            echo "OpenAPI spec syntax valid (install swagger-cli for full validation)"
        else
            echo "OpenAPI error: Invalid YAML syntax"
            return 1
        fi
    fi

    return 0
}

# Check if edited file is OpenAPI spec
if is_openapi_file "$FILE_PATH"; then
    RESULT=$(validate_openapi "$FILE_PATH" 2>&1)
    EXIT_CODE=$?
else
    # Check if there are OpenAPI files that might be affected
    OPENAPI_FILES=$(find_openapi_files)
    if [ -n "$OPENAPI_FILES" ]; then
        # Validate all found specs
        ALL_VALID=1
        for spec in $OPENAPI_FILES; do
            if ! validate_openapi "$spec" &>/dev/null; then
                ALL_VALID=0
                break
            fi
        done
        if [ $ALL_VALID -eq 1 ]; then
            RESULT="OpenAPI specs valid"
        else
            RESULT="OpenAPI spec validation failed"
        fi
        EXIT_CODE=0
    else
        RESULT="No OpenAPI spec found"
        EXIT_CODE=0
    fi
fi

# Log verbose output if enabled
if [ "$CLAUDE_VALIDATOR_VERBOSE" = "1" ]; then
    mkdir -p .claude/logs
    echo "$(date -Iseconds) | $FILE_PATH | $RESULT" >> .claude/logs/api-validator.log
fi

# Output concise result
echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"$RESULT\"}}"
exit 0
