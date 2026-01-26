---
paths: "**/*.{js,jsx,ts,tsx,py,go,java,php,rb,rs,c,cpp,cs}"
---

# Error Handling Guidelines

## Principles

### Fail Fast
- Check preconditions at function entry
- Throw early rather than continue with bad state
- Don't try to "fix" invalid input silently

### Be Explicit
- Errors should have clear messages
- Include context (what failed, why, how to fix)
- Use typed errors when possible

### Handle at the Right Level
- Handle errors where you have context to do something useful
- Let unhandleable errors bubble up
- Don't catch just to re-throw

## Patterns

### Guard Clauses
```javascript
function processOrder(order) {
  if (!order) throw new Error('Order is required');
  if (!order.items?.length) throw new Error('Order must have items');
  if (order.status !== 'pending') {
    throw new Error(`Cannot process order in ${order.status} status`);
  }

  // Happy path continues...
}
```

### Custom Error Types
```javascript
class ValidationError extends Error {
  constructor(field, message) {
    super(`Validation failed for ${field}: ${message}`);
    this.name = 'ValidationError';
    this.field = field;
  }
}

class NotFoundError extends Error {
  constructor(resource, id) {
    super(`${resource} with id ${id} not found`);
    this.name = 'NotFoundError';
    this.resource = resource;
    this.id = id;
  }
}
```

### Try-Catch Boundaries
```javascript
// Good: Handle where you can do something useful
async function handleRequest(req, res) {
  try {
    const result = await processOrder(req.body);
    res.json(result);
  } catch (error) {
    if (error instanceof ValidationError) {
      res.status(400).json({ error: error.message });
    } else if (error instanceof NotFoundError) {
      res.status(404).json({ error: error.message });
    } else {
      logger.error('Unexpected error', { error });
      res.status(500).json({ error: 'Internal server error' });
    }
  }
}
```

### Result Types (Alternative)
```typescript
type Result<T, E> = { ok: true; value: T } | { ok: false; error: E };

function divide(a: number, b: number): Result<number, string> {
  if (b === 0) return { ok: false, error: 'Division by zero' };
  return { ok: true, value: a / b };
}
```

## Anti-Patterns

### Don't Swallow Errors
```javascript
// Bad
try {
  doSomething();
} catch (e) {
  // Silently ignored
}

// Good
try {
  doSomething();
} catch (e) {
  logger.error('doSomething failed', { error: e });
  throw e;
}
```

### Don't Use Exceptions for Flow Control
```javascript
// Bad
try {
  return users[index];
} catch (e) {
  return null;
}

// Good
if (index >= 0 && index < users.length) {
  return users[index];
}
return null;
```

### Don't Catch Generic Errors Unless Necessary
```javascript
// Bad
try {
  await saveUser(user);
} catch (e) {
  // Catches everything including bugs
}

// Good
try {
  await saveUser(user);
} catch (e) {
  if (e instanceof DatabaseError) {
    // Handle known error
  } else {
    throw e; // Re-throw unknown errors
  }
}
```
