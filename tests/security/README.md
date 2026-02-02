# Security Test Suite

Comprehensive security tests for Aether Phase 1 security remediation.

## Test Coverage

### 1. Authentication Tests (`auth_test.go`)
- ✅ All API endpoints require authentication
- ✅ Invalid tokens are rejected
- ✅ Malformed tokens are rejected
- ✅ Expired tokens are rejected
- ✅ Valid tokens are accepted
- ✅ Health endpoints don't require authentication

### 2. Tenant Isolation Tests (`tenant_isolation_test.go`)
- ✅ Cross-tenant access is denied
- ✅ List operations filter by tenant
- ✅ Admin role can access all tenants
- ✅ Non-admin roles cannot cross tenant boundaries

### 3. Injection Prevention Tests (`injection_test.go`)
- ✅ SQL injection attacks are blocked
- ✅ Command injection attacks are blocked
- ✅ Table whitelist is enforced
- ✅ Device name validation prevents injection

### 4. Input Validation Tests (`validation_test.go`)
- ✅ Agent name validation
- ✅ Container image validation
- ✅ Resource limits validation
- ✅ Boot arguments validation

## Running Tests

Run all security tests:
```bash
go test -v ./tests/security/...
```

Run with coverage:
```bash
go test -v -cover ./tests/security/...
```

Run specific test file:
```bash
go test -v ./tests/security/auth_test.go
```

Run with race detection:
```bash
go test -v -race ./tests/security/...
```

## Test Categories

### P0 Security Tests (Must Pass)
- Authentication enforcement
- Tenant isolation
- SQL injection prevention
- Command injection prevention

### P1 Security Tests (Should Pass)
- Input validation
- Resource limits
- Boot args validation

## Integration with CI/CD

Add to `.github/workflows/test.yml`:
```yaml
- name: Run Security Tests
  run: go test -v -race -cover ./tests/security/...

- name: Security Test Coverage
  run: |
    go test -v -cover -coverprofile=coverage-security.out ./tests/security/...
    go tool cover -func=coverage-security.out
```

## Expected Test Results

All tests should pass before deployment:
```
PASS: TestAuthenticationRequired
PASS: TestInvalidTokenRejected
PASS: TestValidTokenAccepted
PASS: TestHealthEndpointsUnauthenticated
PASS: TestCrossTenantAccessDenied
PASS: TestTenantIsolationInListOperations
PASS: TestAdminCanAccessAllTenants
PASS: TestSQLInjectionPrevention
PASS: TestCommandInjectionPrevention
PASS: TestTableWhitelistEnforcement
PASS: TestDeviceNameValidation
PASS: TestAgentNameValidation
PASS: TestImageValidation
PASS: TestResourceValidation
PASS: TestBootArgsValidation
```

## Failure Investigation

If a security test fails:

1. **DO NOT bypass the test**
2. **DO NOT disable the test**
3. **Investigate root cause**
4. **Fix the security vulnerability**
5. **Re-run tests**
6. **Deploy only when all tests pass**

## Adding New Security Tests

When adding new security-sensitive features:

1. Create test file in `tests/security/`
2. Follow AAA pattern (Arrange, Act, Assert)
3. Test both valid and malicious inputs
4. Include edge cases
5. Document expected behavior

## Security Test Maintenance

- Run security tests on every PR
- Add tests for new security features
- Update tests when security requirements change
- Review tests quarterly for completeness
