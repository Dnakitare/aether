# Solo Developer Configuration

You are configured for maximum development velocity. Move fast, iterate rapidly, ship working code.

## Philosophy

- Working code over perfect code
- Fail fast, learn faster
- Automate repetitive tasks
- Every session should produce value
- AI generates code, humans own it - always review before shipping

## Key Principles (Lessons Learned)

### Garbage In = Garbage Out
If your prompt is unclear, the output will be unclear. Break problems into smaller pieces. Use plan mode for Q&A before coding if the task is vague.

### Agents for TASKS, not ROLES
Don't use agents as "frontend developer" or "product manager". Use them for specific tasks like "clean up code", "generate docs", "review security". Task-specific agents produce better results.

### Review Before Shipping
Before pushing to production, start a fresh session and ask for a code review on recent changes. Don't let speed make you lazy about security, performance, and error handling.

### Gather Context Before Coding
Before writing any code:
- **Codebase**: Search for related files, existing implementations, patterns
- **Git history**: Check recent changes and past decisions (`git log`, `git blame`)
- **Documentation**: READMEs, comments, any docs in the project
- **Tests**: Understand expected behavior from existing tests

### Ask Questions, Don't Guess
- If requirements are unclear, **ask**
- If unsure about architecture, **ask**
- If you don't know something, **say so** and investigate
- Never hallucinate - wrong assumptions waste more time than asking

**It's better to say "I don't know, let me check" than to guess wrong.**

## Available Agents

Use these specialized agents for complex tasks:

- **architect** - System design, feature planning, technical specs
- **debugger** - Issue diagnosis, error tracing, root cause analysis
- **refactorer** - Code improvement, pattern application, complexity reduction
- **tester** - Test generation, edge case identification, coverage improvement
- **documenter** - Documentation, code explanation, README creation
- **api-designer** - REST/GraphQL design, OpenAPI specs, endpoint planning
- **database** - Schema design, migrations, query optimization
- **devops** - Docker, Kubernetes, CI/CD, infrastructure as code

## Available Skills

Quick actions via slash commands:

- `/setup` - First-time project configuration wizard
- `/start` - Begin work on a new task (creates branch, gathers context)
- `/scaffold` - Bootstrap new project structure
- `/rapid-prototype` - Create minimal working implementation fast
- `/fix-it` - Diagnose and fix issues immediately
- `/optimize` - Profile and improve performance
- `/ship-it` - Build, test, version, and deploy
- `/explain` - Deep code explanation with context
- `/learn-codebase` - Systematic codebase exploration
- `/autonomous` - Hands-off development with ralph-loop
- `/reflect` - Analyze session and update skills with learnings

## Self-Improving Skills

Skills automatically improve by capturing learnings from each session.

### How It Works
1. **During Session**: Corrections, preferences, and patterns are noted
2. **End of Session**: `/reflect` analyzes and extracts learnings
3. **Skill Updates**: Learnings are added to relevant skill files
4. **Git Commit**: Changes are versioned for history

### Confidence Levels
- **HIGH**: Explicit corrections ("use X instead of Y") - saved immediately
- **MEDIUM**: Repeated patterns or implied preferences - saved with note
- **LOW**: Single observations - asks before saving

### Auto-Reflect (Default: ON)
Automatic reflection is enabled by default. At the end of each session, you'll be prompted to run `/reflect` to capture learnings.

Toggle commands:
- `/reflect-on` - Enable automatic reflection
- `/reflect-off` - Disable automatic reflection
- `/reflect-status` - Check current setting

### Viewing Learnings
```bash
# All learnings
grep -r "## Learnings" -A 20 .claude/skills/

# Specific skill
cat .claude/skills/fix-it/SKILL.md | grep -A 30 "## Learnings"
```

## Self-Validating Agents

Agents and skills now have embedded validation hooks that run automatically after code changes.

### How It Works
- **post-tool-use hooks**: Run after Edit/Write operations
- **stop hooks**: Run when a skill completes
- Output is concise (not verbose) to save tokens

### Available Validators

| Validator | Purpose | Timeout |
|-----------|---------|---------|
| test-runner | Run tests, report pass/fail | 120s |
| code-quality | Lint + format + types | 60s |
| build-validator | Verify build succeeds | 180s |
| api-validator | Validate OpenAPI specs | 30s |
| migration-validator | Check migration safety | 30s |

### Which Skills/Agents Use Validators

**Skills:**
- `/fix-it` → test-runner (after edits)
- `/scaffold` → code-quality (at completion)
- `/ship-it` → build-validator + test-runner (at completion)
- `/optimize` → test-runner (after edits)

**Agents:**
- `@debugger` → test-runner (after edits)
- `@refactorer` → code-quality (after edits)
- `@database` → migration-validator (after edits)
- `@api-designer` → api-validator (after edits)

### Agent Pipelines

Common workflows that chain agents with validation:

- **Bug fix**: `@debugger` → `/fix-it` → `/ship-it`
- **Feature**: `@architect` → `/scaffold` → implement → `/ship-it`
- **API**: `@api-designer` → `@database` → implement → `/ship-it`

See `.claude/docs/AGENT_PIPELINES.md` for detailed pipeline documentation.

### Customizing Validators

Add custom validators in `.claude/hooks/validators/`:

```yaml
# In skill/agent frontmatter
hooks:
  post-tool-use:
    - matcher: "Edit|Write"
      command: ".claude/hooks/validators/your-validator.sh"
      timeout: 60
```

See `.claude/hooks/validators/README.md` for the validator template.

## Workflow Preferences

### Code Quality
- Prefer explicit over implicit
- Keep functions focused and under 50 lines
- Add inline comments only for complex logic
- Run tests before considering work complete

### Error Handling
- Fail fast with clear error messages
- Never swallow exceptions silently
- Log errors with context

### Git Commits
- NO AI attribution in commits (no "Generated with Claude", "Co-Authored-By", 🤖 emojis)
- Clean, professional commit messages only

### Performance
- Profile before optimizing
- Prefer readable code over micro-optimizations
- Cache expensive operations

## Quick Commands

```bash
# Run tests
npm test / pytest / go test ./...

# Build
npm run build / python -m build / go build

# Lint
npm run lint / ruff check / golangci-lint run
```

## Custom Commands

Reusable prompts in `.claude/commands/`:
- `/api-endpoint` - Create API endpoint with validation and error handling
- `/fix-lint` - Run linter and fix all errors
- `/review-code` - Fresh review of recent code before shipping

## Recommended MCP Servers

For enhanced capabilities, consider adding:
- **context7** - Latest documentation for popular libraries
- **playwright** - Browser automation for frontend testing
- **supabase** - Direct database queries and migrations
- **stripe** - Payment integration assistance

## Personal Conventions

<!-- Customize this section for your preferences -->
- Preferred language: [Your choice]
- Tab vs spaces: [Your choice]
- Naming style: [Your choice]
