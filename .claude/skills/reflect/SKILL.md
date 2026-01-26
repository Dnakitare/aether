---
name: reflect
description: Analyze the current session for corrections and learnings, then update skill files with memories. Use at end of sessions or when significant learnings occur.
---

# Reflect - Self-Improving Skills

Analyze the current session to extract learnings, corrections, and patterns. Update skill files with memories that persist across sessions.

## Usage

```
/reflect                    # Full session reflection
/reflect [skill-name]       # Focus reflection on specific skill
/reflect --dry-run          # Show what would be learned without saving
```

## What Gets Captured

### Corrections (High Confidence)
Things you explicitly corrected during this session:
- "No, use X instead of Y"
- "That's wrong, the correct approach is..."
- "Stop doing X, do Y instead"

### Preferences (High Confidence)
Your stated preferences:
- "I prefer X over Y"
- "Always use X for this project"
- "Don't add X"

### Patterns (Medium Confidence)
Repeated behaviors that suggest a pattern:
- Same approach used multiple times
- Consistent style choices
- Recurring solutions

### Discoveries (Low-Medium Confidence)
New learnings from the codebase or problem-solving:
- "Learned that this codebase uses X pattern"
- "Discovered that Y API behaves this way"

## Confidence Levels

| Level | Criteria | Action |
|-------|----------|--------|
| HIGH | Explicit correction or stated preference | Save immediately |
| MEDIUM | Repeated 2+ times or implied preference | Save with note |
| LOW | Single observation | Ask before saving |

## Process

1. **Scan Session**
   - Review conversation for corrections
   - Identify repeated patterns
   - Note explicit preferences

2. **Categorize Learnings**
   - Assign confidence levels
   - Map to relevant skills
   - Identify cross-cutting concerns

3. **Update Skills**
   - Add to "## Learnings" section in skill files
   - Include date and confidence level
   - Preserve existing learnings

4. **Commit Changes**
   - Commit updated skill files
   - Clear, descriptive commit message

## Learnings Format

When learnings are added to skill files, they appear as:

```markdown
## Learnings

<!-- Auto-updated by /reflect - Do not remove this section -->

### High Confidence
- [2026-01-06] Prefer async/await over .then() chains
- [2026-01-06] Always validate user input at API boundaries

### Medium Confidence
- [2026-01-06] User prefers smaller functions (under 30 lines)

### Low Confidence
- [2026-01-06] This codebase uses repository pattern for data access
```

## Automatic Reflection

When enabled (default), reflection runs automatically at the end of each session via Stop hook.

### Toggle Commands
- `/reflect-on` - Enable automatic reflection
- `/reflect-off` - Disable automatic reflection
- `/reflect-status` - Check current setting

### Configuration
Auto-reflect state is stored in `.claude/.reflect-auto` file:
- File exists with "on" → enabled
- File exists with "off" → disabled
- File missing → enabled (default)

## Example Session

**During session:**
```
User: No, don't use moment.js, use date-fns instead
Claude: Got it, I'll use date-fns.

User: I prefer smaller commits
Claude: I'll make commits more granular.

[... work continues ...]

User: /reflect
```

**Reflection output:**
```markdown
## Session Reflection

### Learnings Captured

**HIGH CONFIDENCE:**
1. Prefer date-fns over moment.js
   → Adding to: scaffold, rapid-prototype

**MEDIUM CONFIDENCE:**
1. Preference for smaller, granular commits
   → Adding to: ship-it

### Skills Updated
- .claude/skills/scaffold/SKILL.md
- .claude/skills/rapid-prototype/SKILL.md
- .claude/skills/ship-it/SKILL.md

### Committed
[reflect] Add learnings from session 2026-01-06
```

## Manual vs Automatic

| Aspect | Manual `/reflect` | Automatic (Stop hook) |
|--------|-------------------|----------------------|
| Trigger | User runs command | End of session |
| Scope | Full analysis | Quick scan |
| Prompts | Asks about low-confidence | Skips low-confidence |
| Output | Detailed report | Brief summary |

## Skill File Structure

Skills should have this structure to support learnings:

```markdown
---
name: skill-name
description: What this skill does
---

# Skill Name

[Main content...]

## Learnings

<!-- Auto-updated by /reflect - Do not remove this section -->

[Learnings appear here...]
```

## Viewing Learnings

To see what has been learned:

```bash
# All learnings across skills
grep -r "## Learnings" -A 20 .claude/skills/

# Learnings for specific skill
cat .claude/skills/fix-it/SKILL.md | grep -A 30 "## Learnings"
```

## Clearing Learnings

To reset learnings for a skill:

1. Edit the skill file
2. Remove entries under `## Learnings`
3. Keep the section header and HTML comment
4. Commit the change
