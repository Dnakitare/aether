#!/bin/bash
# migration-validator.sh - Check migration safety (warn, don't block)
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

# Check if file is a migration file
is_migration_file() {
    local file="$1"
    case "$file" in
        */migrations/*|*/migrate/*|*_migration.*|*.migration.*|*/db/migrate/*)
            return 0
            ;;
        *prisma/migrations/*|*alembic/versions/*|*knex/migrations/*)
            return 0
            ;;
    esac
    return 1
}

# Check for dangerous operations in migration
check_migration_safety() {
    local file="$1"
    local warnings=()
    local is_safe=1

    if [ ! -f "$file" ]; then
        echo "Migration file not found"
        return 0
    fi

    local content=$(cat "$file" 2>/dev/null || echo "")

    # Check for DROP operations
    if echo "$content" | grep -qiE 'DROP\s+(TABLE|COLUMN|INDEX|CONSTRAINT|DATABASE)'; then
        warnings+=("DROP operation detected")
        is_safe=0
    fi

    # Check for TRUNCATE
    if echo "$content" | grep -qiE 'TRUNCATE\s+TABLE'; then
        warnings+=("TRUNCATE detected")
        is_safe=0
    fi

    # Check for DELETE without WHERE
    if echo "$content" | grep -qiE 'DELETE\s+FROM\s+\w+\s*;'; then
        warnings+=("DELETE without WHERE")
        is_safe=0
    fi

    # Check for NOT NULL without default (on ADD COLUMN)
    if echo "$content" | grep -qiE 'ADD\s+(COLUMN\s+)?\w+.*NOT\s+NULL(?!.*DEFAULT)'; then
        warnings+=("NOT NULL without DEFAULT")
        is_safe=0
    fi

    # Check for column type changes
    if echo "$content" | grep -qiE 'ALTER\s+(COLUMN|TYPE)|MODIFY\s+COLUMN'; then
        warnings+=("Column type change")
        is_safe=0
    fi

    # Check for rename operations
    if echo "$content" | grep -qiE 'RENAME\s+(TABLE|COLUMN|TO)'; then
        warnings+=("RENAME detected - may break code")
        is_safe=0
    fi

    # Check Prisma-specific
    if [[ "$file" == *prisma* ]]; then
        if echo "$content" | grep -qE '@default\(.*\)|\.drop|DROP'; then
            # This is actually safe for Prisma
            :
        fi
    fi

    # Check for reversibility markers
    local is_reversible=0
    if echo "$content" | grep -qiE '(down|rollback|revert|undo)'; then
        is_reversible=1
    fi

    # Build result message
    if [ ${#warnings[@]} -eq 0 ]; then
        if [ $is_reversible -eq 1 ]; then
            echo "Migration safe: reversible, no data loss risk"
        else
            echo "Migration safe: no data loss risk detected"
        fi
    else
        local warning_list=$(IFS=', '; echo "${warnings[*]}")
        echo "Migration warning: $warning_list - ensure data backup"
    fi

    return 0
}

# Only validate if it's a migration file
if is_migration_file "$FILE_PATH"; then
    RESULT=$(check_migration_safety "$FILE_PATH" 2>&1)
else
    # Check if there are migration directories
    if [ -d "prisma/migrations" ] || [ -d "migrations" ] || [ -d "db/migrate" ]; then
        RESULT="Migration directories present"
    else
        RESULT="No migration detected"
    fi
fi

# Log verbose output if enabled
if [ "$CLAUDE_VALIDATOR_VERBOSE" = "1" ]; then
    mkdir -p .claude/logs
    echo "$(date -Iseconds) | $FILE_PATH | $RESULT" >> .claude/logs/migration-validator.log
fi

# Determine if this is a warning
if [[ "$RESULT" == *"warning"* ]]; then
    echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"⚠️ $RESULT\"}}"
else
    echo "{\"continue\": true, \"hookSpecificOutput\": {\"hookEventName\": \"PostToolUse\", \"additionalContext\": \"$RESULT\"}}"
fi
exit 0
