# Disable Automatic Reflection

Disable automatic reflection at the end of sessions.

## Action

Write "off" to `.claude/.reflect-auto` to disable auto-reflection.

```bash
echo "off" > .claude/.reflect-auto
```

## Confirmation

Respond: "Auto-reflect is now **disabled**. You can still run `/reflect` manually to capture learnings when needed."
