---
name: learn-codebase
description: Systematic codebase exploration and understanding. Use when onboarding to a new project or trying to understand how a codebase works.
---

# Learn Codebase

Systematic exploration to understand how a codebase works.

## Usage

```
/learn-codebase [focus area]
```

## Examples

```
/learn-codebase
/learn-codebase authentication system
/learn-codebase how data flows from API to database
/learn-codebase the testing setup
```

## Exploration Process

### 1. Get the Big Picture
- Read README
- Check package.json / go.mod / requirements.txt
- Look at directory structure
- Identify the tech stack

### 2. Find Entry Points
- Main application entry
- API routes
- CLI commands
- Event handlers

### 3. Trace Key Flows
- User authentication
- Main business operations
- Data persistence
- External integrations

### 4. Understand Architecture
- How is code organized?
- What patterns are used?
- Where is business logic?
- How do components communicate?

### 5. Note Conventions
- Naming patterns
- File organization
- Code style
- Testing approach

## Key Questions to Answer

### Structure
- What's the directory layout?
- Where does code live?
- How are files organized?

### Dependencies
- What frameworks are used?
- What external services?
- What database?

### Patterns
- What architectural pattern? (MVC, Clean, etc.)
- What design patterns?
- What testing patterns?

### Data
- What's the data model?
- How does data flow?
- Where is data stored?

### Operations
- How is it deployed?
- How is it configured?
- How is it monitored?

## Output Format

```markdown
## Codebase Overview: [Project Name]

### Tech Stack
- **Language**:
- **Framework**:
- **Database**:
- **Key Libraries**:

### Directory Structure
```
[Annotated directory tree]
```

### Entry Points
- [Entry 1]: [Description]
- [Entry 2]: [Description]

### Key Flows
1. [Flow name]: [A → B → C]

### Architecture
[Description with diagram if helpful]

### Patterns Used
- [Pattern 1]: [Where/How]
- [Pattern 2]: [Where/How]

### Conventions
- Naming: [Convention]
- Testing: [Approach]
- Error handling: [Approach]

### Key Files to Know
| File | Purpose |
|------|---------|
| [file] | [purpose] |

### Next Steps
[What to explore next based on your goal]
```
