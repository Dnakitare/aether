---
name: fix-it
description: Diagnose and fix issues immediately. Use when something is broken, tests are failing, or behavior is unexpected.
hooks:
  post-tool-use:
    - matcher: "Edit|Write"
      command: ".claude/hooks/validators/test-runner.sh"
      timeout: 120
---

# Fix It

Diagnose and fix issues fast. Get back to working state.

## Usage

```
/fix-it [description of problem]
```

## Examples

```
/fix-it tests are failing in user service
/fix-it app crashes on startup
/fix-it API returns 500 error
/fix-it build is broken
/fix-it TypeScript errors after upgrade
```

## Process

1. **Understand the Problem**
   - What's the exact error?
   - When did it start?
   - What changed?

2. **Reproduce**
   - Can we trigger it reliably?
   - What are the minimal steps?

3. **Diagnose**
   - Read error messages carefully
   - Check logs
   - Trace the code path

4. **Fix**
   - Make the smallest change that fixes it
   - Verify the fix works
   - Check for regressions

5. **Prevent**
   - Add a test if appropriate
   - Document if it's a gotcha

## Quick Diagnostics

### Error Messages
```
Read the FULL message
Check stack trace (bottom-up for root cause)
Look for "caused by" sections
```

### Recent Changes
```
git diff HEAD~5
git log --oneline -10
Check recent commits for suspects
```

### Environment
```
Check environment variables
Verify dependencies installed
Compare with working environment
```

### Logs
```
Check application logs
Check system logs
Add temporary logging if needed
```

## Common Fixes

| Problem | Quick Check |
|---------|-------------|
| Module not found | `npm install` / clear cache |
| Port in use | Kill process or change port |
| Permission denied | Check file/folder permissions |
| Connection refused | Service not running |
| Type errors | Check types match, run tsc |
| Test failures | Run single test, check setup |

## Output Format

```markdown
## Problem
[What was broken]

## Cause
[Why it was broken]

## Fix
[What was changed]

## Verification
[How we confirmed it's fixed]

## Prevention
[How to avoid this in future, if applicable]
```
