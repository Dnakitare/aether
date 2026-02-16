#!/bin/bash
# Database Performance Monitoring Script
# Provides insights into PostgreSQL query performance and index usage

set -e

# Database connection (override with environment variables)
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-aether}"
DB_USER="${DB_USER:-postgres}"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo "========================================"
echo "  Aether Database Performance Monitor"
echo "========================================"
echo ""
echo "Database: $DB_NAME@$DB_HOST:$DB_PORT"
echo ""

# Function to run SQL query
run_query() {
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -t -A -c "$1"
}

# 1. Slow Queries (requires pg_stat_statements extension)
echo -e "${YELLOW}1. Slow Queries (Top 10)${NC}"
echo "----------------------------------------"
run_query "
SELECT
    substring(query, 1, 60) as query_snippet,
    calls,
    round(mean_exec_time::numeric, 2) as avg_ms,
    round(max_exec_time::numeric, 2) as max_ms,
    round(total_exec_time::numeric, 2) as total_ms
FROM pg_stat_statements
WHERE query NOT LIKE '%pg_stat_statements%'
ORDER BY mean_exec_time DESC
LIMIT 10
" 2>/dev/null || echo "pg_stat_statements extension not enabled. Run: CREATE EXTENSION pg_stat_statements;"
echo ""

# 2. Index Usage
echo -e "${YELLOW}2. Index Usage (Least Used)${NC}"
echo "----------------------------------------"
run_query "
SELECT
    schemaname,
    tablename,
    indexname,
    idx_scan as scans,
    idx_tup_read as tuples_read
FROM pg_stat_user_indexes
WHERE schemaname = 'public'
ORDER BY idx_scan ASC
LIMIT 10
"
echo ""

# 3. Table Sizes
echo -e "${YELLOW}3. Table Sizes${NC}"
echo "----------------------------------------"
run_query "
SELECT
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as size
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC
"
echo ""

# 4. Cache Hit Ratio
echo -e "${YELLOW}4. Cache Hit Ratio (should be >90%)${NC}"
echo "----------------------------------------"
run_query "
SELECT
    round(100.0 * sum(heap_blks_hit) / nullif(sum(heap_blks_hit) + sum(heap_blks_read), 0), 2) as cache_hit_ratio
FROM pg_statio_user_tables
"
echo ""

# 5. Active Connections
echo -e "${YELLOW}5. Active Connections${NC}"
echo "----------------------------------------"
run_query "
SELECT
    count(*) filter (where state = 'active') as active,
    count(*) filter (where state = 'idle') as idle,
    count(*) filter (where state = 'idle in transaction') as idle_in_transaction,
    count(*) as total
FROM pg_stat_activity
WHERE datname = '$DB_NAME'
"
echo ""

# 6. Table Bloat Estimate
echo -e "${YELLOW}6. Potential Table Bloat${NC}"
echo "----------------------------------------"
run_query "
SELECT
    schemaname,
    tablename,
    n_dead_tup as dead_tuples,
    n_live_tup as live_tuples,
    round(100.0 * n_dead_tup / nullif(n_live_tup + n_dead_tup, 0), 2) as dead_ratio
FROM pg_stat_user_tables
WHERE n_dead_tup > 1000
ORDER BY n_dead_tup DESC
LIMIT 10
"
echo ""

# 7. Missing Indexes (queries with sequential scans)
echo -e "${YELLOW}7. Tables with Sequential Scans${NC}"
echo "----------------------------------------"
run_query "
SELECT
    schemaname,
    tablename,
    seq_scan as sequential_scans,
    seq_tup_read as tuples_read,
    idx_scan as index_scans,
    round(100.0 * seq_scan / nullif(seq_scan + idx_scan, 0), 2) as seq_scan_pct
FROM pg_stat_user_tables
WHERE seq_scan > 0
ORDER BY seq_scan DESC
LIMIT 10
"
echo ""

# 8. Long Running Queries
echo -e "${YELLOW}8. Long Running Queries (>1 second)${NC}"
echo "----------------------------------------"
run_query "
SELECT
    pid,
    now() - query_start as duration,
    state,
    substring(query, 1, 60) as query_snippet
FROM pg_stat_activity
WHERE state = 'active'
  AND now() - query_start > interval '1 second'
  AND datname = '$DB_NAME'
ORDER BY duration DESC
" 2>/dev/null || echo "No long-running queries found"
echo ""

# 9. Recommendations
echo -e "${GREEN}9. Recommendations${NC}"
echo "----------------------------------------"

# Check if pg_stat_statements is enabled
if ! run_query "SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements'" 2>/dev/null | grep -q 1; then
    echo -e "${RED}⚠ Enable pg_stat_statements for query performance tracking${NC}"
    echo "   Run: CREATE EXTENSION pg_stat_statements;"
else
    echo -e "${GREEN}✓ pg_stat_statements enabled${NC}"
fi

# Check cache hit ratio
cache_ratio=$(run_query "SELECT round(100.0 * sum(heap_blks_hit) / nullif(sum(heap_blks_hit) + sum(heap_blks_read), 0), 2) FROM pg_statio_user_tables" | tr -d ' ')
if (( $(echo "$cache_ratio < 90" | bc -l 2>/dev/null || echo 0) )); then
    echo -e "${RED}⚠ Cache hit ratio is low ($cache_ratio%). Consider increasing shared_buffers${NC}"
else
    echo -e "${GREEN}✓ Cache hit ratio is good ($cache_ratio%)${NC}"
fi

# Check for tables needing VACUUM
dead_tuples=$(run_query "SELECT count(*) FROM pg_stat_user_tables WHERE n_dead_tup > 10000" | tr -d ' ')
if [ "$dead_tuples" -gt 0 ]; then
    echo -e "${YELLOW}⚠ $dead_tuples table(s) have significant dead tuples. Consider VACUUM${NC}"
else
    echo -e "${GREEN}✓ No tables need immediate VACUUM${NC}"
fi

echo ""
echo "========================================"
echo "  Monitor Complete"
echo "========================================"
