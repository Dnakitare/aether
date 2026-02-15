## 📝 Description

<!-- Provide a clear and concise description of your changes -->

### What does this PR do?

<!-- Briefly describe what this PR accomplishes -->

### Why is this change needed?

<!-- Explain the motivation and context for this change -->

---

## 🔗 Related Issues

<!-- Link related issues using keywords: Fixes #123, Closes #456, Relates to #789 -->

- Fixes #
- Closes #
- Relates to #

---

## 🎯 Type of Change

<!-- Check all that apply -->

- [ ] 🐛 Bug fix (non-breaking change that fixes an issue)
- [ ] ✨ New feature (non-breaking change that adds functionality)
- [ ] 💥 Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] 📝 Documentation update
- [ ] 🎨 Code style update (formatting, renaming)
- [ ] ♻️ Refactoring (no functional changes)
- [ ] ⚡ Performance improvement
- [ ] ✅ Test update (adding or updating tests)
- [ ] 🔧 Configuration change
- [ ] 🏗️ Infrastructure change

---

## 📋 Changes Made

<!-- List the main changes in this PR -->

-
-
-

---

## 🧪 Testing

### How has this been tested?

<!-- Describe the tests you ran and their results -->

- [ ] Unit tests (short mode)
- [ ] Integration tests (with Docker infrastructure)
- [ ] E2E tests (complete workflow)
- [ ] Manual testing

### Test Commands Run

```bash
# Unit tests
go test -short ./...

# Integration tests (if applicable)
docker-compose -f docker-compose.test.yml up -d
go test ./tests/integration/...

# E2E tests (if applicable)
go test -v ./tests/integration/e2e_workflow_test.go

# Coverage check
go test -coverprofile=coverage.out ./...
go tool cover -func coverage.out
```

### Test Results

<!-- Paste relevant test output or summary -->

```
=== RUN   TestName
--- PASS: TestName (0.00s)
PASS
ok      package/path    0.123s
```

### Test Coverage

- **Current Coverage**: __%
- **Coverage Change**: +/- __%
- **Target**: 60% (Beta goal)

---

## ✅ Checklist

### Code Quality

- [ ] My code follows the Go style guidelines
- [ ] I have performed a self-review of my code
- [ ] I have commented my code, particularly in hard-to-understand areas
- [ ] My changes generate no new warnings
- [ ] I have run `go fmt ./...` on my code
- [ ] I have run `go vet ./...` with no issues
- [ ] I have run `golangci-lint run` (if available)

### Testing

- [ ] I have added tests that prove my fix is effective or that my feature works
- [ ] New and existing unit tests pass locally (`go test -short ./...`)
- [ ] Integration tests pass (if modified integration code)
- [ ] Tests are not flaky (ran multiple times successfully)

### Documentation

- [ ] I have updated relevant documentation in `docs/`
- [ ] I have updated code comments and godoc
- [ ] I have updated the README.md (if user-facing changes)
- [ ] I have updated CHANGELOG.md (if applicable)

### Security

- [ ] My changes don't introduce security vulnerabilities
- [ ] I have not committed secrets, credentials, or sensitive data
- [ ] I have validated all user inputs
- [ ] I have followed security best practices (input validation, SQL injection prevention, etc.)

---

## 🚀 Deployment Notes

<!-- Any special deployment considerations? -->

### Breaking Changes

<!-- If this is a breaking change, describe what breaks and how to migrate -->

- [ ] No breaking changes
- [ ] Breaking changes (describe below)

**Migration steps:**

### Configuration Changes

<!-- List any new environment variables, config options, or infrastructure requirements -->

**New environment variables:**
- `VARIABLE_NAME`: Description and default value

**Configuration changes:**
-

### Database Changes

<!-- List any schema changes, migrations, or data changes -->

- [ ] No database changes
- [ ] Schema changes (migrations included)

---

## 📸 Screenshots / Logs

<!-- If applicable, add screenshots or log output to demonstrate the changes -->

<details>
<summary>Click to view screenshots/logs</summary>

<!-- Add screenshots or log output here -->

</details>

---

## ⚡ Performance Impact

<!-- Describe any performance implications -->

- [ ] No performance impact
- [ ] Performance improvement (describe below)
- [ ] Potential performance impact (describe and justify below)

**Details:**

---

## 📚 Additional Context

### Dependencies

<!-- List any new dependencies added -->

**New dependencies:**
- Package: version (reason)

### Follow-up Work

<!-- List any follow-up work or future improvements -->

- [ ] TODO item 1
- [ ] TODO item 2

### Reviewer Notes

<!-- Any specific areas you'd like reviewers to focus on? -->

**Please review:**
-
-

**Known limitations:**
-

---

**Before requesting review:**
- [ ] I have read the [CONTRIBUTING.md](../CONTRIBUTING.md) guide
- [ ] I have assigned appropriate labels to this PR
- [ ] I have linked related issues above
- [ ] CI checks are passing (or I know why they're failing)
- [ ] I am ready for code review
- [ ] I have tested my changes thoroughly
