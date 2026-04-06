# Aether - Launch Action Plan

**Created:** February 15, 2026
**Status:** Awaiting decision on launch strategy
**Estimated Completion:** 2-8 weeks depending on chosen path

---

## Choose Your Path

### Path 1: Alpha Demo Launch (2 weeks) 🚀

**Goal:** Show it works end-to-end, invite contributors

**Week 1: Make It Work**
```
Day 1-2:  Database Integration
          - Write PostgreSQL queries for agent CRUD
          - Wire state manager to runtime
          - Test persistence

Day 3:    API Server Integration
          - Remove TODO in server.go
          - Wire HTTP API to runtime
          - Add basic auth endpoints

Day 4-5:  Firecracker Integration
          - Complete VM config generation
          - Make Firecracker API calls
          - Test on Linux with KVM
```

**Week 2: Polish & Launch**
```
Day 6-7:  End-to-End Test
          - Write full integration test
          - Create → Start → Exec → Stop → Delete
          - Verify persistence

Day 8:    Documentation Update
          - Change "Production" to "Alpha"
          - Fix CLI examples
          - Add known limitations
          - Update benchmarks to "projected"

Day 9:    Fix Critical Bugs
          - Fix failing test
          - Add migrations command
          - Test all documented examples

Day 10:   Launch
          - Push to GitHub
          - Write honest blog post
          - Post to HN/Reddit
          - Tag as v0.1.0-alpha
```

**Result:**
- Working demo (with caveats)
- Honest about limitations
- Ready for contributors
- Shows vision + execution

---

### Path 2: Beta Launch (6 weeks) 🎯

**Goal:** Production-ready for early adopters

**Weeks 1-2: Core Integration** (Same as Path 1)

**Weeks 3-4: Make It Usable**
```
Week 3:
  - Agent exec endpoint + CLI command
  - Migrations command
  - Fix all documented examples
  - Add authentication flow
  - CI/CD pipeline (GitHub Actions)

Week 4:
  - Terraform deployment (ECS/EKS)
  - Load balancer configuration
  - Auto-scaling setup
  - Basic monitoring
```

**Weeks 5-6: Production Hardening**
```
Week 5:
  - Comprehensive integration tests
  - Performance profiling
  - Fix memory leaks
  - Optimize hot paths
  - Load testing

Week 6:
  - Security audit
  - Fix vulnerabilities
  - Grafana dashboards
  - Documentation completeness
  - Beta launch
```

**Result:**
- Beta-quality software
- Safe for early adopters
- Full feature set
- Known performance characteristics

---

### Path 3: Reposition as Framework (1 week) 📦

**Goal:** Position as building blocks for agent runtimes

**Week 1: Reframing**
```
Day 1-2:  Documentation Rewrite
          - "Framework for building agent runtimes"
          - Each package is standalone
          - Show how to compose them
          - Architecture as the product

Day 3-4:  Package-level README files
          - internal/scheduler/README.md
          - internal/auth/README.md
          - internal/observability/README.md
          - Usage examples for each

Day 5:    Reference Implementation
          - Simple example using all packages
          - Clear integration points
          - Extension guide

Day 6-7:  Launch
          - Blog: "Building an Agent Runtime"
          - Position as educational + useful
          - Lower expectations, higher value
```

**Result:**
- Honest positioning
- Useful immediately
- Educational value
- Lower pressure to "finish"

---

## Recommended Path: Alpha Demo (Path 1)

### Why Alpha Demo?

1. **Fastest to Value** - 2 weeks vs 6+ weeks
2. **Tests Your Assumptions** - Real user feedback
3. **Attracts Contributors** - Show progress, get help
4. **Builds Momentum** - Ship early, ship often
5. **Honest Marketing** - No overpromising

### What Alpha Launch Looks Like

**README Changes:**
```markdown
# Aether - AI Agent Runtime [ALPHA]

⚠️ **Alpha Status**: Core features work but not production-ready.
Missing: persistence, full Firecracker integration, load balancing.

Best for: Contributors, researchers, early adopters willing to help build.
Not for: Production workloads, mission-critical systems.

✅ What Works:
- Local scheduler with bin-packing
- JWT authentication
- Rate limiting
- Health checks
- Configuration system

🚧 What's In Progress:
- Database persistence
- Firecracker VM execution
- Distributed scheduler
- Production deployment

❌ What's Not Ready:
- Running real workloads
- High availability
- Multi-node deployment
- Performance tuning
```

**Blog Post:**
```
Title: "Aether - Building an AI Agent Runtime in Public"

I've been building Aether for 5 months - a runtime for AI agents using
Firecracker microVMs. Today I'm releasing it as an alpha to get feedback
and contributors.

What Works:
- Solid architecture (Go + Firecracker)
- Core components (scheduler, auth, observability)
- 60% test coverage
- Good documentation

What Doesn't Work Yet:
- End-to-end agent execution (in progress)
- Database persistence (coming this week)
- Production deployment (in design)

Why Release Now?
- Get feedback on architecture
- Find contributors
- Build in public
- Validate use cases

Looking for:
- Feedback on API design
- Contributors for missing pieces
- Beta testers (in 4-6 weeks)

GitHub: [link]
```

---

## Critical Path (Alpha Launch)

### Day 1-2: Database Integration ✅

**File:** `/internal/state/postgres.go`

```go
// Add these functions:
func (s *PostgresStore) CreateAgent(ctx, agentConfig) error
func (s *PostgresStore) GetAgent(ctx, agentID) (*AgentInfo, error)
func (s *PostgresStore) ListAgents(ctx, tenantID) ([]*AgentInfo, error)
func (s *PostgresStore) UpdateAgentStatus(ctx, agentID, status) error
func (s *PostgresStore) DeleteAgent(ctx, agentID) error

// Wire to runtime:
// internal/runtime/runtime.go - use postgres store
```

**Tests:**
```go
// internal/state/postgres_test.go
TestCreateAgent
TestGetAgent
TestListAgents
TestUpdateAgentStatus
TestDeleteAgent
TestAgentNotFound
TestDuplicateAgent
```

---

### Day 3: API Server Integration ✅

**File:** `/cmd/aether/server.go`

**Change this:**
```go
// TODO: Create and start HTTP API server
// For now, just wait for shutdown signal
```

**To this:**
```go
// Create HTTP API server
apiServer := api.New(logger, api.Config{
    Address: cfg.Server.Address,
    EnableAuth: cfg.Server.EnableAuth,
    // ... other config
}, rt, sched, sc, qm, jwtMgr)

// Start server
if err := apiServer.Start(ctx); err != nil {
    return fmt.Errorf("failed to start API server: %w", err)
}

// Register for graceful shutdown
defer apiServer.Stop(ctx)
```

**Add:**
```
POST /v1/auth/login    - Return JWT token
POST /v1/auth/register - Create user (admin only)
```

---

### Day 4-5: Firecracker Execution ✅

**File:** `/internal/runtime/vm/lifecycle.go`

**Complete `writeFirecrackerConfig()` properly:**
```go
config := FirecrackerConfig{
    BootSource: BootSource{
        KernelImagePath: v.Config.KernelImagePath,
        BootArgs: v.Config.BootArgs,
    },
    Drives: []Drive{{
        DriveID: "rootfs",
        PathOnHost: v.Config.RootfsPath,
        IsRootDevice: true,
        IsReadOnly: false,
    }},
    MachineConfig: MachineConfig{
        VcpuCount: v.Config.CPUCount,
        MemSizeMib: v.Config.MemoryMB,
    },
    NetworkInterfaces: buildNetworkConfig(v.Config),
}
return json.MarshalIndent(config, "", "  ")
```

**Add Firecracker API client:**
```go
// Use github.com/firecracker-microvm/firecracker-go-sdk
// Or raw HTTP calls to unix socket
```

---

### Day 6-7: End-to-End Test ✅

**File:** `/tests/integration/e2e_test.go`

```go
func TestAgentLifecycle(t *testing.T) {
    if testing.Short() {
        t.Skip("E2E test requires Firecracker")
    }

    // Setup
    db, redis := setupInfrastructure(t)
    defer cleanup(t, db, redis)

    // Start server
    server := startAetherServer(t)
    defer server.Stop()

    // Create agent
    agentID := createAgent(t, server, AgentConfig{
        Name: "test-agent",
        Image: "python:3.11",
    })

    // Verify agent running
    assert.Equal(t, "Running", getAgentStatus(t, server, agentID))

    // Execute code
    output := execInAgent(t, server, agentID, "print('hello')")
    assert.Contains(t, output, "hello")

    // Stop agent
    stopAgent(t, server, agentID)
    assert.Equal(t, "Stopped", getAgentStatus(t, server, agentID))

    // Verify persistence
    server.Restart()
    agent := getAgent(t, server, agentID)
    assert.NotNil(t, agent)

    // Cleanup
    deleteAgent(t, server, agentID)
}
```

---

### Day 8-9: Documentation & Fixes ✅

**README.md:**
- Add Alpha banner
- Add "What Works" section
- Add "What Doesn't Work Yet" section
- Fix CLI examples
- Add contributing guide

**Fix:**
- Failing API key test
- Add `migrate` command
- Test all README examples
- Fix benchmarks (mark as projected)

---

### Day 10: Launch ✅

**Morning:**
1. Final test run
2. Tag v0.1.0-alpha
3. Push to GitHub
4. Create release with changelog

**Afternoon:**
1. Post to Hacker News
2. Post to Reddit (r/golang, r/selfhosted)
3. Tweet announcement
4. Share on LinkedIn

**Evening:**
1. Monitor feedback
2. Respond to questions
3. Fix critical bugs
4. Thank contributors

---

## Success Metrics

### Alpha Launch (2 weeks)

**Code:**
- ✅ Agent can be created
- ✅ Agent can run code
- ✅ Agent persists to database
- ✅ All tests pass
- ✅ One end-to-end test works

**Documentation:**
- ✅ Honest about alpha status
- ✅ Clear what works/doesn't
- ✅ Contributing guide exists
- ✅ All examples tested

**Community:**
- 🎯 50+ GitHub stars (week 1)
- 🎯 5+ contributors interested
- 🎯 10+ issues filed (shows interest)
- 🎯 Positive HN discussion

---

## Risk Mitigation

### What Could Go Wrong

**Risk 1: Firecracker Integration Harder Than Expected**
- Mitigation: Use firecracker-go-sdk library
- Fallback: Ship without VM execution, containers only
- Timeline impact: +3-5 days

**Risk 2: Database Integration Complex**
- Mitigation: Start with minimal CRUD
- Fallback: Keep in-memory for alpha
- Timeline impact: +2-3 days

**Risk 3: Negative Community Response**
- Mitigation: Be honest about alpha status
- Fallback: Reposition as framework
- Timeline impact: None

**Risk 4: Critical Bugs Found**
- Mitigation: Thorough testing in days 6-9
- Fallback: Delay launch by 3-5 days
- Timeline impact: +3-5 days

---

## Resources Needed

### Technical
- ✅ Linux machine with KVM (for Firecracker testing)
- ✅ PostgreSQL instance
- ✅ Redis instance
- ⚠️ Firecracker binary (~3MB download)

### Time
- 2 weeks full-time (Path 1)
- 6 weeks full-time (Path 2)
- 1 week part-time (Path 3)

### Skills
- ✅ Go programming (you have this)
- ✅ Systems programming (you have this)
- ⚠️ Firecracker API (learn as you go)
- ⚠️ PostgreSQL (basic CRUD needed)

---

## Next Steps

1. **Choose a path** (1, 2, or 3)
2. **Review this plan** with someone else
3. **Set up project tracking** (GitHub projects?)
4. **Start Day 1** tasks
5. **Ship in 2 weeks** (or 6, or 1)

---

## Questions to Answer

Before starting:
- [ ] Which path aligns with your goals?
- [ ] Do you have a Linux machine with KVM?
- [ ] Can you commit 2-6 weeks full-time?
- [ ] Are you comfortable with "alpha" launch?
- [ ] Do you want contributors or users first?

---

## Final Thoughts

**You've built something impressive.** The hard part (architecture, design, core components) is done.

**The remaining work is integration.** Connecting the pieces you've already built.

**Alpha launch in 2 weeks is achievable.** If you focus and execute.

**Choose your path and commit.** All three paths are valid.

**Good luck! 🚀**

---

**Plan Created:** February 15, 2026
**Review Date:** February 22, 2026 (Week 1 check-in)
**Launch Target:** March 1, 2026 (Alpha) or April 15, 2026 (Beta)
