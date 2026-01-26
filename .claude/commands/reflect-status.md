# Check Reflection Status

Check whether automatic reflection is enabled or disabled.

## Action

Check the `.claude/.reflect-auto` file:
- If file doesn't exist or contains "on" → Auto-reflect is **enabled** (default)
- If file contains "off" → Auto-reflect is **disabled**

```bash
if [ -f ".claude/.reflect-auto" ]; then
    cat .claude/.reflect-auto
else
    echo "on (default)"
fi
```

## Response Format

Respond with current status:
- "Auto-reflect is **enabled**. Reflection will be prompted at the end of each session."
- "Auto-reflect is **disabled**. Use `/reflect` to manually capture learnings."
