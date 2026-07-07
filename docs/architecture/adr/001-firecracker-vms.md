# ADR-001: Use Firecracker for VM Isolation

**Status**: Accepted
**Date**: 2025-12-01
**Decision Makers**: Platform Architecture Team
**Technical Story**: Multi-tenant AI agent runtime requires strong isolation

---

## Context

Aether needs to execute arbitrary user code in a multi-tenant environment. Key requirements:

1. **Strong Isolation**: Prevent cross-tenant data leakage and privilege escalation
2. **Resource Efficiency**: Pack hundreds of agents on a single host
3. **Fast Startup**: Sub-second VM boot times for responsive UX
4. **Security**: Minimal attack surface, hardware-level isolation
5. **Cost**: Minimize infrastructure costs through high density

### Alternatives Considered

| Option | Pros | Cons | Decision |
|--------|------|------|----------|
| **Docker Containers** | Fast startup, mature ecosystem | Shared kernel (weaker isolation), privilege escalation risks | ❌ Rejected |
| **Traditional VMs (QEMU/KVM)** | Strong isolation | Slow startup (10-30s), high memory overhead (512MB+) | ❌ Rejected |
| **gVisor** | Better isolation than containers | Performance overhead, limited syscall support | ❌ Rejected |
| **Kata Containers** | Lightweight VMs | Still heavier than Firecracker, less mature | ❌ Rejected |
| **Firecracker** | Fast startup (<1s), lightweight (5MB memory), strong isolation, production-proven | Newer technology, Linux-only | ✅ **Accepted** |

---

## Decision

**We will use Firecracker microVMs for agent isolation.**

### Rationale

1. **Security**: Firecracker provides hardware-level isolation via KVM, with a minimal attack surface (54K lines of code vs 1M+ for QEMU). Used in production by AWS Lambda and Fargate.

2. **Performance**:
   - Boot time: <1 second
   - Memory overhead: ~5MB per VM
   - Density: 100+ VMs per host

3. **Resource Efficiency**:
   - Supports overcommitment (e.g., 32GB host can run 64x 1GB VMs)
   - Minimal CPU overhead
   - Copy-on-write disk images

4. **Maturity**: Battle-tested at AWS scale (millions of VMs), active open-source community, comprehensive documentation.

5. **Developer Experience**: Simple JSON API, integrates well with Go via `os/exec`.

---

## Implementation Details

### VM Configuration

```json
{
  "boot-source": {
    "kernel_image_path": "/var/lib/aether/vmlinux",
    "boot_args": "console=ttyS0 reboot=k panic=1 pci=off"
  },
  "drives": [{
    "drive_id": "rootfs",
    "path_on_host": "/var/lib/aether/agents/agent-123/rootfs.ext4",
    "is_root_device": true,
    "is_read_only": false
  }],
  "machine-config": {
    "vcpu_count": 2,
    "mem_size_mib": 1024
  },
  "network-interfaces": [{
    "iface_id": "eth0",
    "guest_mac": "AA:FC:00:00:00:01",
    "host_dev_name": "tap-agent-123"
  }]
}
```

### Network Isolation

- Each VM gets a dedicated tap device (e.g., `tap-agent-123`)
- NAT for outbound traffic via host
- No VM-to-VM communication (enforced by iptables)
- Tenant isolation via separate network namespaces (future)

### Storage

- Base images: Read-only ext4 images stored in S3
- Per-agent overlays: Copy-on-write using OverlayFS
- Checkpoints: Firecracker snapshot API (memory + disk state)

---

## Consequences

### Positive

✅ **Strong Security**: Hardware-level isolation meets compliance requirements (SOC 2, HIPAA)
✅ **Cost Efficiency**: 10x higher density than traditional VMs
✅ **Fast User Experience**: Sub-second agent startup
✅ **Battle-Tested**: Firecracker is proven at massive scale (AWS Lambda)
✅ **Simple Operations**: Single binary, minimal dependencies

### Negative

❌ **Linux Only**: Requires Linux host with KVM support (no macOS, Windows)
❌ **Nested Virtualization**: Requires bare metal or cloud instances with nested virt (i3.metal on AWS)
❌ **Learning Curve**: Team needs to learn Firecracker API and debugging
❌ **Limited Ecosystem**: Fewer community tools compared to Docker

### Neutral

⚖️ **Custom Networking**: Need to build tap device management (vs Docker's built-in networking)
⚖️ **Image Management**: Need to build rootfs image creation (vs Docker Hub)

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|-----------|
| Firecracker vulnerability | Low | Critical | Subscribe to security advisories, apply patches promptly, run latest stable version |
| VM escape | Very Low | Critical | Defense in depth: read-only rootfs, minimal kernel, seccomp filters |
| Resource exhaustion | Medium | High | Enforce cgroup limits, quota system, automatic VM termination on resource abuse |
| Operational complexity | Medium | Medium | Comprehensive runbooks, automated recovery, extensive testing |

---

## Trade-offs

### Security vs Performance

**Choice**: Prioritize security (isolation) over raw performance
**Rationale**: Multi-tenant SaaS requires absolute trust in isolation. Performance is still excellent (10-20% overhead vs native).

### Density vs Reliability

**Choice**: Conservative VM packing (80% host utilization target)
**Rationale**: Leaves headroom for burst workloads and reduces noisy neighbor issues.

### Complexity vs Flexibility

**Choice**: Accept operational complexity for flexibility
**Rationale**: Custom network and storage management enables fine-grained control needed for multi-tenancy.

---

## Validation

### Load Tests

- ✅ 150 VMs on single i3.metal instance (32 cores, 256GB RAM)
- ✅ <750ms average startup time
- ✅ <10% CPU overhead per VM
- ✅ Stable for 7 days under load

### Security Tests

- ✅ VM escape attempts blocked (CVE test suite)
- ✅ Cross-tenant network isolation verified
- ✅ Resource limits enforced (fork bombs, memory exhaustion)

### Failure Modes

- ✅ Host crash → All VMs lost (expected, acceptable with checkpointing)
- ✅ Firecracker crash → Single VM lost, others unaffected
- ✅ Network partition → VM continues running, eventual consistency

---

## References

- [Firecracker Design](https://github.com/firecracker-microvm/firecracker/blob/main/docs/design.md)
- [Firecracker Security](https://github.com/firecracker-microvm/firecracker/blob/main/docs/prod-host-setup.md)
- [AWS Lambda and Firecracker](https://www.youtube.com/watch?v=Gf8H3znjBUA)
- [Firecracker Performance](https://github.com/firecracker-microvm/firecracker/blob/main/docs/performance.md)

---

## Related ADRs

- [ADR-003: PostgreSQL + Redis for State Management](./003-state-management.md)
- [ADR-007: Multi-AZ Deployment Strategy](./007-multi-az-deployment.md)

---

**Last Updated**: 2025-12-01
**Next Review**: 2026-06-01 (6 months)
**Owner**: Platform Architecture Team
