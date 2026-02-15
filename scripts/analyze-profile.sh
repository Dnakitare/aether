#!/bin/bash
set -e

# Quick Profile Analysis Tool
# Usage: ./scripts/analyze-profile.sh <profile-directory>

if [ $# -eq 0 ]; then
    # Find most recent profile directory
    PROFILE_DIR=$(ls -td profiles/*/ 2>/dev/null | head -1)
    if [ -z "$PROFILE_DIR" ]; then
        echo "No profiles found. Run ./scripts/profile-load-test.sh first."
        exit 1
    fi
else
    PROFILE_DIR="$1"
fi

echo "========================================"
echo "Profile Analysis: $PROFILE_DIR"
echo "========================================"
echo ""

# Check if profiles exist
if [ ! -d "$PROFILE_DIR" ]; then
    echo "Error: Directory $PROFILE_DIR not found"
    exit 1
fi

# Show metadata
if [ -f "$PROFILE_DIR/metadata.txt" ]; then
    echo "=== Metadata ==="
    cat "$PROFILE_DIR/metadata.txt"
    echo ""
fi

# Analyze CPU profile
if [ -f "$PROFILE_DIR/cpu.prof" ]; then
    echo "=== CPU Profile (Top 15 Functions) ==="
    go tool pprof -top -cum "$PROFILE_DIR/cpu.prof" 2>/dev/null | head -20
    echo ""

    echo "=== CPU Hotspots ==="
    go tool pprof -top "$PROFILE_DIR/cpu.prof" 2>/dev/null | grep -v "^$" | head -15
    echo ""
fi

# Analyze Memory profile
if [ -f "$PROFILE_DIR/mem.prof" ]; then
    echo "=== Memory Allocations (Top 15) ==="
    go tool pprof -top -alloc_space "$PROFILE_DIR/mem.prof" 2>/dev/null | head -20
    echo ""

    echo "=== In-Use Memory ==="
    go tool pprof -top -inuse_space "$PROFILE_DIR/mem.prof" 2>/dev/null | head -15
    echo ""
fi

# Analyze Block profile
if [ -f "$PROFILE_DIR/block.prof" ]; then
    echo "=== Blocking Operations (Top 10) ==="
    go tool pprof -top "$PROFILE_DIR/block.prof" 2>/dev/null | head -15
    echo ""
fi

# Show test output summary
if [ -f "$PROFILE_DIR/test-output.log" ]; then
    echo "=== Test Performance Report ==="
    grep -A 20 "=== PERFORMANCE REPORT ===" "$PROFILE_DIR/test-output.log" || echo "No performance report found"
    echo ""
fi

echo "========================================"
echo "Interactive Analysis Commands:"
echo "========================================"
echo ""
echo "Open in browser:"
echo "  go tool pprof -http=:8080 $PROFILE_DIR/cpu.prof"
echo "  go tool pprof -http=:8081 $PROFILE_DIR/mem.prof"
echo ""
echo "Command-line exploration:"
echo "  go tool pprof $PROFILE_DIR/cpu.prof"
echo "  (pprof) top10"
echo "  (pprof) list <function-name>"
echo "  (pprof) web"
echo ""
