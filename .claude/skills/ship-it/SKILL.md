---
name: ship-it
description: Build, test, version, and deploy workflow. Use when ready to release, deploy, or publish changes.
hooks:
  stop:
    - command: ".claude/hooks/validators/build-validator.sh"
      timeout: 180
    - command: ".claude/hooks/validators/test-runner.sh"
      timeout: 120
---

# Ship It

Complete deployment workflow: build, test, version, deploy.

## Usage

```
/ship-it [release-type]
```

## Examples

```
/ship-it patch release
/ship-it minor version with changelog
/ship-it deploy to staging
/ship-it production deploy
```

## Release Types

- **patch** (1.0.0 → 1.0.1) - Bug fixes
- **minor** (1.0.0 → 1.1.0) - New features, backwards compatible
- **major** (1.0.0 → 2.0.0) - Breaking changes

## Pre-Ship Checklist

### Code Quality
- [ ] All tests passing
- [ ] No linting errors
- [ ] Type checks pass
- [ ] No console.logs/debuggers

### Documentation
- [ ] README updated if needed
- [ ] CHANGELOG updated
- [ ] API docs current

### Git
- [ ] Working on correct branch
- [ ] All changes committed
- [ ] Branch is up to date

## Ship Process

### 1. Verify Tests Pass
```bash
npm test
# or pytest, go test, etc.
```

### 2. Build
```bash
npm run build
# Verify build succeeds
```

### 3. Version Bump
```bash
npm version patch/minor/major
# or manually update version
```

### 4. Update Changelog
```markdown
## [1.0.1] - 2024-01-15

### Fixed
- Bug description

### Added
- Feature description
```

### 5. Commit & Tag
```bash
git add .
git commit -m "chore: release v1.0.1"
git tag v1.0.1
```

### 6. Push
```bash
git push origin main --tags
```

### 7. Deploy
```bash
# Varies by platform
npm publish
# or deploy script
```

## Quick Deploy Commands

### NPM Package
```bash
npm test && npm run build && npm publish
```

### Docker
```bash
docker build -t app:latest .
docker push app:latest
```

### Vercel/Netlify
```bash
vercel --prod
# or netlify deploy --prod
```

## Output Format

```markdown
## Release: v[VERSION]

### Pre-flight
- [x] Tests: PASS
- [x] Build: SUCCESS
- [x] Lint: CLEAN

### Changes
[Summary of what's included]

### Commands Run
```bash
[Commands executed]
```

### Deployed To
[Where it was deployed]

### Verification
[How to verify it's working]
```
