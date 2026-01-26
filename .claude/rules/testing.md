---
paths: "**/*.{test,spec}.{js,ts,jsx,tsx}, **/test_*.py, **/*_test.go, **/tests/**"
---

# Testing Guidelines

## Principles

### Test Behavior, Not Implementation
- Test what code does, not how it does it
- Tests should survive refactoring
- Avoid testing private methods directly

### Fast and Reliable
- Tests should run in milliseconds
- No flaky tests allowed
- No dependencies between tests

### Readable
- Test names describe expected behavior
- Tests serve as documentation
- Easy to understand what failed and why

## Test Structure (AAA)

```javascript
test('user login with valid credentials returns token', async () => {
  // Arrange
  const user = createTestUser({ email: 'test@example.com' });
  await userRepository.save(user);

  // Act
  const result = await authService.login('test@example.com', 'password');

  // Assert
  expect(result.token).toBeDefined();
  expect(result.user.id).toBe(user.id);
});
```

## Test Naming

Format: `[unit]_[scenario]_[expected result]`

```javascript
// Good names
test('createUser with valid data saves user to database')
test('createUser with duplicate email throws ValidationError')
test('getUser with nonexistent id returns null')

// Avoid
test('test1')
test('createUser works')
test('should work correctly')
```

## What to Test

### Must Test
- Business logic
- Edge cases
- Error paths
- Integration points

### Skip Testing
- Framework code
- Trivial getters/setters
- Third-party library behavior
- Generated code

## Edge Cases to Cover

| Category | Examples |
|----------|----------|
| Empty | null, undefined, [], '', 0 |
| Boundaries | 0, 1, max-1, max, max+1 |
| Invalid | wrong types, malformed data |
| Special | unicode, whitespace, special chars |
| State | first, last, only, duplicate |

## Mocking

### When to Mock
- External services (APIs, databases in unit tests)
- Time-dependent operations
- Random operations
- Expensive operations

### When Not to Mock
- The code under test
- Simple data transformations
- Things that are fast and deterministic

```javascript
// Good: Mock external service
jest.mock('./emailService', () => ({
  sendEmail: jest.fn().mockResolvedValue(true)
}));

// Avoid: Mocking the thing you're testing
jest.mock('./userService'); // Don't do this in userService.test.ts
```

## Test Organization

```
src/
├── user/
│   ├── userService.ts
│   ├── userService.test.ts      # Unit tests
│   └── userService.integration.ts # Integration tests
tests/
├── e2e/
│   └── userFlow.test.ts         # End-to-end tests
└── fixtures/
    └── users.ts                 # Test data
```

## Coverage

- Aim for meaningful coverage, not numbers
- 80% is a reasonable target
- 100% coverage doesn't mean bug-free
- Cover the important paths, not every line
