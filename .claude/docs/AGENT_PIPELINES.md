# Agent Pipelines

Token-efficient agent chaining patterns for common workflows. Each pipeline combines agents and skills with automatic validation at each step.

## Pipeline Concepts

### Self-Validating Steps
Each agent and skill can have embedded hooks that run validation after changes:
- **post-tool-use hooks:** Run after Edit/Write operations
- **stop hooks:** Run when the skill completes

This ensures quality at each step without manual intervention.

### Token Efficiency
Pipelines are designed to minimize token usage:
- Concise validator output (no verbose logs in context)
- Specialized agents focus on specific tasks
- Early failure detection prevents wasted effort

---

## Common Pipelines

### Bug Fix Pipeline
**Flow:** `@debugger` → `/fix-it` → `/ship-it`

**Use When:** Something is broken and needs to be fixed and shipped.

```
1. @debugger
   - Investigates the issue
   - Traces root cause
   - Hook: test-runner validates each fix attempt

2. /fix-it
   - Implements the fix
   - Hook: test-runner after each edit

3. /ship-it
   - Builds, tests, versions
   - Hook: build-validator + test-runner at completion
```

**Example:**
```
User: The login page returns a 500 error

@debugger investigate the login error
[Debugger traces to auth service, finds null pointer]

/fix-it the null pointer in auth service
[Fix-it patches the code, tests pass]

/ship-it patch release
[Build succeeds, tests pass, version bumped]
```

---

### Feature Pipeline
**Flow:** `@architect` → `/scaffold` → implement → `/ship-it`

**Use When:** Building a new feature from design to deployment.

```
1. @architect
   - Designs the feature
   - Identifies components and interfaces
   - No hooks (design phase)

2. /scaffold
   - Creates file structure
   - Hook: code-quality at completion

3. Implement
   - Use appropriate agents (@api-designer, @database, etc.)
   - Each has its own validation hooks

4. /ship-it
   - Final build and deploy
   - Hook: build-validator + test-runner
```

---

### API Pipeline
**Flow:** `@api-designer` → `@database` → implement → `/ship-it`

**Use When:** Building a new API with database schema.

```
1. @api-designer
   - Designs endpoints
   - Generates OpenAPI spec
   - Hook: api-validator validates spec

2. @database
   - Creates schema/migrations
   - Hook: migration-validator checks safety

3. Implement
   - Build controllers, services
   - Regular test-runner hooks

4. /ship-it
   - Build and deploy
```

---

### Refactoring Pipeline
**Flow:** `@refactorer` → verify → `/ship-it`

**Use When:** Improving code quality without changing behavior.

```
1. @refactorer
   - Identifies code smells
   - Applies refactoring patterns
   - Hook: code-quality after each edit

2. Verify
   - Manual or automated testing
   - Ensure behavior unchanged

3. /ship-it
   - Build and deploy
```

---

### Performance Pipeline
**Flow:** profile → `/optimize` → `/ship-it`

**Use When:** Improving performance of existing code.

```
1. Profile
   - Identify bottlenecks
   - Establish baseline metrics

2. /optimize
   - Apply optimizations
   - Hook: test-runner ensures no regressions

3. /ship-it
   - Build and deploy
   - Compare with baseline
```

---

## Pipeline Best Practices

### 1. Start Fresh When Needed
If a pipeline step fails repeatedly, start a fresh session to clear context and try again.

### 2. Use Plan Mode for Complex Features
For features requiring multiple pipelines, use plan mode first to design the overall approach.

### 3. Check Validator Output
Pay attention to validator feedback in the context - it's there for a reason.

### 4. Don't Skip Validation
If validators report issues, fix them before moving to the next step.

### 5. Commit Between Steps
For long pipelines, consider committing after each major step to create restore points.

---

## Custom Pipelines

Create your own pipelines by combining agents and skills:

```markdown
## My Custom Pipeline

1. @agent-name
   - What it does
   - Expected hooks

2. /skill-name
   - What it does
   - Expected hooks

3. Final step
```

### Adding Custom Validators

Create validators in `.claude/hooks/validators/` and reference them in skill/agent frontmatter:

```yaml
hooks:
  post-tool-use:
    - matcher: "Edit|Write"
      command: ".claude/hooks/validators/my-validator.sh"
      timeout: 60
```

See `.claude/hooks/validators/README.md` for the validator template.

---

## Troubleshooting

### Validator Not Running
- Check the hook is in the frontmatter
- Verify the script is executable (`chmod +x`)
- Check the matcher regex matches the tool name

### Validator Always Fails
- Enable verbose logging: `CLAUDE_VALIDATOR_VERBOSE=1`
- Check logs in `.claude/logs/`
- Run validator manually to debug

### Pipeline Seems Slow
- Validators add overhead - this is intentional
- Reduce timeout if checks are fast
- Consider skipping validation for exploratory work
