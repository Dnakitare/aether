---
name: autonomous
description: Hands-off development using ralph-loop. Use for tasks with clear completion criteria that can run autonomously.
---

# Autonomous

Wrapper for ralph-wiggum's `/ralph-loop` with sensible defaults for hands-off development.

## Usage

```
/autonomous "[task description]" --until "[completion criteria]"
```

## Examples

```
/autonomous "Build a REST API for todos with CRUD operations" --until "all tests pass"

/autonomous "Fix all TypeScript errors" --until "tsc reports no errors"

/autonomous "Refactor user service to use dependency injection" --until "tests pass and no direct instantiation"

/autonomous "Add input validation to all API endpoints" --until "validation tests pass"
```

## How It Works

1. You provide a task and completion criteria
2. Claude works on the task iteratively
3. When Claude tries to stop, ralph-loop feeds the prompt back
4. Continues until completion criteria is met (or max iterations)

## Good Tasks for Autonomous

### Best Suited
- Tasks with testable completion criteria
- Greenfield implementations
- Test-driven development
- Repetitive refactoring
- Bug fixes with test coverage

### Examples
```
"Implement user authentication with tests"
--until "all auth tests pass"

"Add comprehensive error handling"
--until "error handling tests pass"

"Convert callbacks to async/await"
--until "no callback patterns remain and tests pass"
```

## Completion Criteria

### Good Criteria
- "all tests pass"
- "tsc reports no errors"
- "no linting errors"
- "build succeeds"
- "coverage above 80%"

### Bad Criteria
- "looks good" (subjective)
- "is complete" (vague)
- "works perfectly" (undefined)

## Safety Settings

```
--max-iterations 50   # Safety limit (default)
--timeout 3600        # Max time in seconds
```

## Underlying Command

This skill translates to:
```
/ralph-loop "[task]" --completion-promise "[criteria]" --max-iterations 50
```

## Best Practices

1. **Clear Task Description**
   - Be specific about what to build
   - Include requirements
   - Mention constraints

2. **Testable Completion**
   - Use objective criteria
   - Prefer automated checks
   - Tests > subjective assessment

3. **Start Small**
   - Begin with focused tasks
   - Expand scope as confidence grows

4. **Monitor Progress**
   - Check in periodically
   - Use `/cancel-ralph` if off track

## Output

The autonomous loop will:
1. Work through iterations
2. Report progress
3. Stop when criteria met
4. Summarize what was done

## Requirements

Requires `ralph-wiggum` plugin to be installed.
Run `./install.sh` to install all required plugins.
