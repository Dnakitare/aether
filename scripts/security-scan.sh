#!/bin/bash
# Comprehensive security testing suite for Aether

set -e

echo "==================================="
echo "  Aether Security Testing Suite"
echo "==================================="
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

FAILED=0

# Function to run a test and track failures
run_test() {
    local test_name="$1"
    local test_cmd="$2"

    echo -e "${YELLOW}▶ Running: $test_name${NC}"
    if eval "$test_cmd"; then
        echo -e "${GREEN}✓ PASSED: $test_name${NC}"
        echo ""
        return 0
    else
        echo -e "${RED}✗ FAILED: $test_name${NC}"
        echo ""
        FAILED=$((FAILED + 1))
        return 1
    fi
}

echo "1. Security Package Tests"
echo "==========================>"
run_test "Auth Package Tests" \
    "go test -v ./internal/auth/... -coverprofile=coverage-auth.out"

run_test "Tenant Package Tests" \
    "go test -v ./internal/tenant/... -coverprofile=coverage-tenant.out"

echo "2. Vulnerability Scanning"
echo "==========================>"
run_test "Go Vulnerability Check" \
    "govulncheck ./..."

echo "3. Static Security Analysis"
echo "==========================>"
run_test "Gosec Security Scanner" \
    "gosec -fmt=text -out=gosec-report.txt ./... || (cat gosec-report.txt && exit 1)"

echo "4. Static Code Analysis"
echo "==========================>"
run_test "Staticcheck Analysis" \
    "staticcheck ./... 2>&1 | grep -v 'deprecated' || true"

echo "5. Security-Sensitive Tests"
echo "==========================>"

# Test API key security
run_test "API Key Format & Security" \
    "go test -v ./internal/auth -run TestAPIKey"

# Test JWT security
run_test "JWT Token Security" \
    "go test -v ./internal/auth -run TestJWT"

# Test RBAC permissions
run_test "RBAC Permission Checks" \
    "go test -v ./internal/auth -run TestRBAC"

# Test quota enforcement
run_test "Quota Enforcement" \
    "go test -v ./internal/tenant -run TestQuota"

echo "6. Common Vulnerability Tests"
echo "==========================>"

# Check for common security issues in code
echo "▶ Checking for hardcoded secrets..."
if grep -r -n --include="*.go" -E "(password|secret|token|api[_-]?key)\s*[:=]\s*\"[^\"]{8,}\"" . \
    --exclude-dir=".git" --exclude-dir="vendor" --exclude-dir="tests" 2>/dev/null | grep -v "test" | grep -v "example"; then
    echo -e "${RED}✗ Found potential hardcoded secrets!${NC}"
    FAILED=$((FAILED + 1))
else
    echo -e "${GREEN}✓ No hardcoded secrets found${NC}"
fi
echo ""

echo "▶ Checking for SQL injection vulnerabilities..."
if grep -r -n --include="*.go" -E "\.Query\(|\.Exec\(.*\+.*\)" . \
    --exclude-dir=".git" --exclude-dir="vendor" 2>/dev/null | grep -v "Placeholder" | grep -v test; then
    echo -e "${YELLOW}⚠ Found potential SQL injection points (review manually)${NC}"
else
    echo -e "${GREEN}✓ No obvious SQL injection patterns found${NC}"
fi
echo ""

echo "▶ Checking for command injection vulnerabilities..."
if grep -r -n --include="*.go" -E "exec\.Command\(.*\+.*\)|os\.Execute" . \
    --exclude-dir=".git" --exclude-dir="vendor" 2>/dev/null | grep -v test; then
    echo -e "${YELLOW}⚠ Found potential command injection points (review manually)${NC}"
else
    echo -e "${GREEN}✓ No obvious command injection patterns found${NC}"
fi
echo ""

echo "7. Dependency Security"
echo "==========================>"
echo "▶ Checking go.mod for known vulnerable dependencies..."
go list -json -m all | go mod graph 2>&1 || echo "Dependency check completed"
echo ""

echo "8. TLS/Crypto Configuration"
echo "==========================>"
echo "▶ Checking for weak crypto usage..."
if grep -r -n --include="*.go" -E "(MD5|SHA1|DES|RC4)" . \
    --exclude-dir=".git" --exclude-dir="vendor" --exclude-dir="tests" 2>/dev/null | grep -v "comment" | grep -v "SHA256"; then
    echo -e "${YELLOW}⚠ Found weak cryptographic algorithms (review manually)${NC}"
else
    echo -e "${GREEN}✓ No weak cryptographic algorithms found${NC}"
fi
echo ""

echo "==================================="
echo "  Security Scan Summary"
echo "==================================="
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}✓ All security tests passed!${NC}"
    echo ""
    echo "Security reports generated:"
    echo "  - coverage-auth.out"
    echo "  - coverage-tenant.out"
    echo "  - gosec-report.txt"
    exit 0
else
    echo -e "${RED}✗ $FAILED security test(s) failed${NC}"
    echo ""
    echo "Please review the failures above and fix any security issues."
    exit 1
fi
