---
paths: "**/*.{js,jsx,ts,tsx,py,go,java,php,rb,rs,c,cpp,cs}"
---

# Code Quality Guidelines

## Principles

### Keep It Simple
- Prefer obvious solutions over clever ones
- If it needs a comment to explain, consider simplifying
- Complexity is a cost, not a feature

### Fail Fast
- Validate inputs early
- Throw errors instead of returning special values
- Don't swallow exceptions silently

### Single Responsibility
- Functions do one thing
- Classes have one reason to change
- Files have clear purposes

## Code Style

### Functions
- Keep under 50 lines when possible
- Limit parameters to 4 or fewer
- Use descriptive names (verbs for actions)
- Early returns for guard clauses

```javascript
// Good
function getUserById(id) {
  if (!id) throw new Error('ID required');

  const user = db.find(id);
  if (!user) return null;

  return user;
}

// Avoid
function getUser(id, options, callback, defaultValue) {
  // 100 lines of nested logic
}
```

### Variables
- Declare close to usage
- Prefer const/immutable
- Use descriptive names

### Error Handling
- Always handle errors explicitly
- Log with context
- Don't catch and ignore

```javascript
// Good
try {
  await saveUser(user);
} catch (error) {
  logger.error('Failed to save user', { userId: user.id, error });
  throw error;
}

// Avoid
try {
  await saveUser(user);
} catch (e) {
  // silently ignored
}
```

## Anti-Patterns to Avoid

- Deep nesting (flatten with early returns)
- Magic numbers/strings (use constants)
- Copy-paste code (extract functions)
- Long parameter lists (use objects)
- Boolean parameters (use descriptive options)
