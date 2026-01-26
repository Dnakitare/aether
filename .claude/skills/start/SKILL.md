---
name: start
description: Start working on a new task or feature. Creates branch, gathers context, and sets up for focused development.
---

# Start - Begin New Work

Quick start command to begin working on a new task or feature. Creates a branch, gathers relevant context, and prepares for focused development.

## Usage

```
/start <description>           # Start with description
/start feature user-auth       # Start a feature
/start fix login-bug           # Start a bugfix
/start refactor api-handlers   # Start refactoring
```

## What It Does

1. **Creates Branch** - Properly named git branch
2. **Gathers Context** - Finds related files and code
3. **Sets Focus** - Establishes what you're working on
4. **Ready to Code** - You're set up to start immediately

## Branch Naming

Automatically creates properly named branches:

| Command | Branch Created |
|---------|----------------|
| `/start feature user-auth` | `feature/user-auth` |
| `/start fix login-redirect` | `fix/login-redirect` |
| `/start refactor api-layer` | `refactor/api-layer` |
| `/start docs api-reference` | `docs/api-reference` |
| `/start test payment-flow` | `test/payment-flow` |

### Supported Types
- `feature` - New functionality
- `fix` - Bug fixes
- `refactor` - Code improvements
- `docs` - Documentation
- `test` - Test additions
- `chore` - Maintenance tasks
- `perf` - Performance improvements

### Default Type
If no type specified, defaults to `feature`:
```
/start user-authentication
→ Creates: feature/user-authentication
```

## Context Gathering

When starting, automatically searches for:

### Related Files
- Files with similar names
- Files in related directories
- Recently modified files in the area

### Existing Patterns
- How similar features are implemented
- Existing tests for reference
- Related utilities and helpers

### Documentation
- README sections
- Inline comments
- Type definitions

## Example Session

```
/start feature payment-processing

🚀 Starting: payment-processing

Branch:
  Created: feature/payment-processing
  From: main (up to date)

Related Context Found:
  📁 src/services/stripe.ts - Existing Stripe integration
  📁 src/api/checkout.ts - Checkout endpoint
  📁 src/types/payment.ts - Payment type definitions
  📁 tests/checkout.test.ts - Existing checkout tests

Patterns Detected:
  • Services use dependency injection
  • API handlers follow REST conventions
  • Tests use Jest with mocking

Ready to work on: payment-processing

What would you like to implement?
```

## Quick Start (No Context)

For simple tasks, skip context gathering:

```
/start fix typo-in-readme --quick

🚀 Quick Start: typo-in-readme

Branch: fix/typo-in-readme
Ready to go!
```

## Integration with Other Skills

### After /start
- Just describe what you want to build
- Use `/fix-it` if you encounter issues
- Use `/ship-it` when ready to commit and push

### Workflow
```
/start feature user-profile    # Begin work
[... implement feature ...]
/ship-it                       # Commit, test, push
```

## Git Safety

Before creating a branch:

1. **Checks for uncommitted changes**
   ```
   ⚠️  You have uncommitted changes.
   Commit or stash them before starting new work.
   ```

2. **Ensures main is up to date**
   ```
   Fetching latest from origin...
   ✓ main is up to date
   ```

3. **Prevents duplicate branches**
   ```
   ⚠️  Branch feature/user-auth already exists.
   Switch to it? [y/N]
   ```

## Output Format

```markdown
## Starting: [description]

### Branch
- Name: [branch-name]
- Base: [main/master]
- Status: [created/switched]

### Context
[Related files and patterns found]

### Ready
[Confirmation and next steps]
```

## Configuration

Customize start behavior in CLAUDE.md:

```markdown
## Start Preferences

- Default branch type: feature
- Auto-fetch before branch: true
- Context search depth: 2 directories
- Include test files in context: true
```

## Tips

1. **Be Descriptive** - Better descriptions = better branch names
2. **Use Types** - `fix`, `feature`, etc. keep branches organized
3. **Check Context** - Review found files before diving in
4. **Stay Focused** - One task per branch

## Finishing Work

When done with the task:

```
/ship-it                    # Commit and push
# or
git add . && git commit     # Manual commit
git push -u origin HEAD     # Push branch
```

Then create a PR or merge as appropriate.
