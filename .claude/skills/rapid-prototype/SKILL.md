---
name: rapid-prototype
description: Create minimal working implementations fast. Use for quick prototypes, MVPs, proof of concepts, or when speed matters more than perfection.
---

# Rapid Prototype

Create working implementations quickly. Speed over perfection.

## Usage

```
/rapid-prototype [what to build]
```

## Examples

```
/rapid-prototype REST API for todo items
/rapid-prototype CLI tool to convert CSV to JSON
/rapid-prototype React form with validation
/rapid-prototype WebSocket chat server
```

## Philosophy

- **Working > Perfect** - Get it running first
- **Simple > Complete** - Core features only
- **Fast > Elegant** - Optimize later
- **Inline > Abstract** - Avoid premature abstraction

## Process

1. **Clarify the Core**
   - What's the ONE thing this needs to do?
   - What's the simplest path to working?

2. **Build the Happy Path**
   - Implement the main flow
   - Skip edge cases for now
   - Use simple data structures

3. **Make It Work**
   - Test the basic flow
   - Fix obvious issues
   - Get to "it works" state

4. **Document Next Steps**
   - What's missing?
   - What needs improvement?
   - What are the known limitations?

## Shortcuts Allowed

- Hardcoded values (extract to config later)
- Console.log debugging (proper logging later)
- In-memory storage (database later)
- No authentication (add when needed)
- Minimal error handling (improve later)
- No tests (add when stabilized)

## Output Format

```markdown
## Prototype: [Name]

### What It Does
[Brief description]

### Quick Start
```bash
[Commands to run]
```

### Files Created
- [file 1] - [purpose]
- [file 2] - [purpose]

### Limitations
- [Known limitation 1]
- [Known limitation 2]

### Next Steps
- [ ] [Improvement 1]
- [ ] [Improvement 2]
```

## After Prototyping

Use other skills to improve:
- `/fix-it` - Fix issues that come up
- `/optimize` - Improve performance
- `/ship-it` - Prepare for production
