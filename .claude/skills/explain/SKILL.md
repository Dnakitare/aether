---
name: explain
description: Deep code explanation with context. Use when you need to understand how code works, what it does, or why it was written this way.
---

# Explain

Deep code explanation with context and reasoning.

## Usage

```
/explain [file or code section]
```

## Examples

```
/explain src/auth/middleware.ts
/explain the authentication flow
/explain how the caching works
/explain this function [paste code]
```

## Explanation Levels

### Overview
- What does this do at a high level?
- What problem does it solve?
- How does it fit in the system?

### Detailed
- Step-by-step walkthrough
- Data flow through the code
- Key decision points

### Deep Dive
- Why was it written this way?
- What are the trade-offs?
- What are the edge cases?

## Explanation Format

```markdown
## Overview
[One paragraph summary of what this code does]

## Purpose
[Why this code exists, what problem it solves]

## How It Works

### Step 1: [Name]
[Explanation with code references]

### Step 2: [Name]
[Explanation with code references]

## Key Components

### [Component 1]
- **What**: [Description]
- **Why**: [Reasoning]
- **Where**: [File:line]

## Data Flow
[How data moves through this code]

## Important Details
- [Detail 1]
- [Detail 2]

## Related Code
- [Related file 1] - [Why it's related]
- [Related file 2] - [Why it's related]

## Gotchas
- [Things to be aware of]
```

## When Explaining

### For Functions
- What are the inputs?
- What does it return?
- What side effects does it have?
- When would you call this?

### For Classes
- What's the responsibility?
- What's the public interface?
- What's the lifecycle?
- How does it interact with others?

### For Files
- What's the purpose of this file?
- What does it export?
- What are the key functions/classes?
- How is it organized?

### For Systems
- What's the architecture?
- How do components interact?
- What's the data flow?
- Where are the boundaries?

## Tips

- Use diagrams when helpful (ASCII is fine)
- Reference specific line numbers
- Explain the "why" not just the "what"
- Point out non-obvious behavior
- Mention historical context if known
