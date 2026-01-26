#!/bin/bash
# reflect.sh - Stop hook for automatic session reflection
#
# This hook runs at the end of each session to trigger reflection
# if auto-reflect is enabled (default: on).
#
# Input: JSON from stdin (Stop hook context)
# Output: JSON with optional message
#
# Toggle auto-reflect:
#   /reflect-on   - Enable
#   /reflect-off  - Disable
#   /reflect-status - Check current setting

# Read input from stdin (required for Claude Code hooks)
INPUT=$(cat)

# Config file location
CONFIG_FILE=".claude/.reflect-auto"

# Check if auto-reflect is enabled
# Default is ON if config file doesn't exist or contains "on"
AUTO_ENABLED="on"
if [ -f "$CONFIG_FILE" ]; then
    SETTING=$(cat "$CONFIG_FILE" | tr -d '[:space:]')
    if [ "$SETTING" = "off" ]; then
        AUTO_ENABLED="off"
    fi
fi

# If disabled, exit silently
if [ "$AUTO_ENABLED" = "off" ]; then
    echo '{"continue": true}'
    exit 0
fi

# Auto-reflect is enabled - remind Claude to reflect
# The Stop hook can suggest actions via the message field
cat << 'EOF'
{
  "continue": true,
  "message": "Auto-reflect is enabled. Consider running /reflect to capture learnings from this session before ending. Key things to look for: explicit corrections, stated preferences, repeated patterns, and new discoveries about the codebase."
}
EOF

exit 0
