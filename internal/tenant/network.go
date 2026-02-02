// Package tenant provides multi-tenant resource management.
package tenant

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/aether-runtime/aether/pkg/api"
)

// NetworkIsolationManager manages network isolation for tenants.
type NetworkIsolationManager struct {
	logger *slog.Logger
	mu     sync.RWMutex

	// Network namespaces by tenant
	tenantNetworks map[api.TenantID]*TenantNetwork

	// CIDR allocator for tenant subnets
	cidrAllocator *CIDRAllocator
}

// TenantNetwork represents an isolated network for a tenant.
type TenantNetwork struct {
	TenantID      api.TenantID
	Subnet        *net.IPNet
	Gateway       net.IP
	DNSServers    []net.IP
	FirewallRules []*FirewallRule
	CreatedAt     string
}

// FirewallRule represents a network firewall rule.
type FirewallRule struct {
	ID         string
	Action     FirewallAction
	Protocol   string // tcp, udp, icmp, all
	SourceIP   string // CIDR notation
	DestIP     string // CIDR notation
	SourcePort int
	DestPort   int
	Direction  Direction
	Priority   int
}

// FirewallAction defines what to do with matching traffic.
type FirewallAction string

const (
	// FirewallAllow permits traffic.
	FirewallAllow FirewallAction = "allow"

	// FirewallDeny blocks traffic.
	FirewallDeny FirewallAction = "deny"

	// FirewallLog logs traffic without blocking.
	FirewallLog FirewallAction = "log"
)

// Direction specifies traffic direction.
type Direction string

const (
	// DirectionInbound is incoming traffic.
	DirectionInbound Direction = "inbound"

	// DirectionOutbound is outgoing traffic.
	DirectionOutbound Direction = "outbound"

	// DirectionBoth is bidirectional traffic.
	DirectionBoth Direction = "both"
)

// NewNetworkIsolationManager creates a new network isolation manager.
func NewNetworkIsolationManager(logger *slog.Logger, baseSubnet string) (*NetworkIsolationManager, error) {
	_, subnet, err := net.ParseCIDR(baseSubnet)
	if err != nil {
		return nil, fmt.Errorf("invalid base subnet: %w", err)
	}

	return &NetworkIsolationManager{
		logger:         logger.With("component", "network_isolation"),
		tenantNetworks: make(map[api.TenantID]*TenantNetwork),
		cidrAllocator:  NewCIDRAllocator(subnet),
	}, nil
}

// CreateTenantNetwork creates an isolated network for a tenant.
func (nim *NetworkIsolationManager) CreateTenantNetwork(ctx context.Context, tenantID api.TenantID) (*TenantNetwork, error) {
	nim.mu.Lock()
	defer nim.mu.Unlock()

	// Check if network already exists
	if network, exists := nim.tenantNetworks[tenantID]; exists {
		return network, nil
	}

	// Allocate subnet
	subnet, err := nim.cidrAllocator.Allocate()
	if err != nil {
		return nil, fmt.Errorf("failed to allocate subnet: %w", err)
	}

	// Calculate gateway (first usable IP in subnet)
	gateway := incrementIP(subnet.IP, 1)

	// Default DNS servers (using common public DNS)
	dnsServers := []net.IP{
		net.ParseIP("8.8.8.8"),
		net.ParseIP("8.8.4.4"),
	}

	// Create default firewall rules
	defaultRules := []*FirewallRule{
		{
			ID:        "default-deny-inbound",
			Action:    FirewallDeny,
			Protocol:  "all",
			Direction: DirectionInbound,
			Priority:  1000,
		},
		{
			ID:        "allow-outbound-dns",
			Action:    FirewallAllow,
			Protocol:  "udp",
			DestPort:  53,
			Direction: DirectionOutbound,
			Priority:  100,
		},
		{
			ID:        "allow-outbound-http",
			Action:    FirewallAllow,
			Protocol:  "tcp",
			DestPort:  80,
			Direction: DirectionOutbound,
			Priority:  100,
		},
		{
			ID:        "allow-outbound-https",
			Action:    FirewallAllow,
			Protocol:  "tcp",
			DestPort:  443,
			Direction: DirectionOutbound,
			Priority:  100,
		},
	}

	network := &TenantNetwork{
		TenantID:      tenantID,
		Subnet:        subnet,
		Gateway:       gateway,
		DNSServers:    dnsServers,
		FirewallRules: defaultRules,
		CreatedAt:     "2026-01-31T00:00:00Z", // Placeholder
	}

	nim.tenantNetworks[tenantID] = network

	nim.logger.InfoContext(ctx,
		"created tenant network",
		"tenant_id", tenantID,
		"subnet", subnet.String(),
		"gateway", gateway.String(),
	)

	return network, nil
}

// GetTenantNetwork retrieves a tenant's network.
func (nim *NetworkIsolationManager) GetTenantNetwork(tenantID api.TenantID) (*TenantNetwork, error) {
	nim.mu.RLock()
	defer nim.mu.RUnlock()

	network, exists := nim.tenantNetworks[tenantID]
	if !exists {
		return nil, fmt.Errorf("network not found for tenant %s", tenantID)
	}

	return network, nil
}

// DeleteTenantNetwork removes a tenant's network.
func (nim *NetworkIsolationManager) DeleteTenantNetwork(ctx context.Context, tenantID api.TenantID) error {
	nim.mu.Lock()
	defer nim.mu.Unlock()

	network, exists := nim.tenantNetworks[tenantID]
	if !exists {
		return fmt.Errorf("network not found for tenant %s", tenantID)
	}

	// Release subnet back to pool
	nim.cidrAllocator.Release(network.Subnet)

	delete(nim.tenantNetworks, tenantID)

	nim.logger.InfoContext(ctx, "deleted tenant network", "tenant_id", tenantID)
	return nil
}

// AddFirewallRule adds a firewall rule to a tenant's network.
func (nim *NetworkIsolationManager) AddFirewallRule(ctx context.Context, tenantID api.TenantID, rule *FirewallRule) error {
	nim.mu.Lock()
	defer nim.mu.Unlock()

	network, exists := nim.tenantNetworks[tenantID]
	if !exists {
		return fmt.Errorf("network not found for tenant %s", tenantID)
	}

	// Validate rule
	if err := validateFirewallRule(rule); err != nil {
		return fmt.Errorf("invalid firewall rule: %w", err)
	}

	network.FirewallRules = append(network.FirewallRules, rule)

	nim.logger.InfoContext(ctx,
		"added firewall rule",
		"tenant_id", tenantID,
		"rule_id", rule.ID,
		"action", rule.Action,
	)

	return nil
}

// RemoveFirewallRule removes a firewall rule from a tenant's network.
func (nim *NetworkIsolationManager) RemoveFirewallRule(ctx context.Context, tenantID api.TenantID, ruleID string) error {
	nim.mu.Lock()
	defer nim.mu.Unlock()

	network, exists := nim.tenantNetworks[tenantID]
	if !exists {
		return fmt.Errorf("network not found for tenant %s", tenantID)
	}

	// Find and remove rule
	for i, rule := range network.FirewallRules {
		if rule.ID == ruleID {
			network.FirewallRules = append(network.FirewallRules[:i], network.FirewallRules[i+1:]...)
			nim.logger.InfoContext(ctx, "removed firewall rule", "tenant_id", tenantID, "rule_id", ruleID)
			return nil
		}
	}

	return fmt.Errorf("firewall rule %s not found", ruleID)
}

// CIDRAllocator manages CIDR subnet allocation.
type CIDRAllocator struct {
	mu           sync.Mutex
	baseSubnet   *net.IPNet
	allocated    map[string]bool
	subnetPrefix int
}

// NewCIDRAllocator creates a new CIDR allocator.
func NewCIDRAllocator(baseSubnet *net.IPNet) *CIDRAllocator {
	return &CIDRAllocator{
		baseSubnet:   baseSubnet,
		allocated:    make(map[string]bool),
		subnetPrefix: 24, // /24 subnets (256 IPs)
	}
}

// Allocate allocates a new subnet.
func (ca *CIDRAllocator) Allocate() (*net.IPNet, error) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	// Simple allocation strategy: increment third octet
	// In production, use a more sophisticated algorithm
	baseIP := ca.baseSubnet.IP.To4()
	if baseIP == nil {
		return nil, fmt.Errorf("only IPv4 supported")
	}

	for i := 0; i < 256; i++ {
		thirdOctet := byte(i)
		subnet := &net.IPNet{
			IP:   net.IPv4(baseIP[0], baseIP[1], thirdOctet, 0),
			Mask: net.CIDRMask(ca.subnetPrefix, 32),
		}

		if !ca.allocated[subnet.String()] {
			ca.allocated[subnet.String()] = true
			return subnet, nil
		}
	}

	return nil, fmt.Errorf("no available subnets")
}

// Release releases a subnet back to the pool.
func (ca *CIDRAllocator) Release(subnet *net.IPNet) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	delete(ca.allocated, subnet.String())
}

// validateFirewallRule validates a firewall rule.
func validateFirewallRule(rule *FirewallRule) error {
	if rule.ID == "" {
		return fmt.Errorf("rule ID is required")
	}

	if rule.Action != FirewallAllow && rule.Action != FirewallDeny && rule.Action != FirewallLog {
		return fmt.Errorf("invalid action: %s", rule.Action)
	}

	if rule.Protocol != "tcp" && rule.Protocol != "udp" && rule.Protocol != "icmp" && rule.Protocol != "all" {
		return fmt.Errorf("invalid protocol: %s", rule.Protocol)
	}

	if rule.Direction != DirectionInbound && rule.Direction != DirectionOutbound && rule.Direction != DirectionBoth {
		return fmt.Errorf("invalid direction: %s", rule.Direction)
	}

	return nil
}

// incrementIP increments an IP address by n.
func incrementIP(ip net.IP, n int) net.IP {
	result := make(net.IP, len(ip))
	copy(result, ip)

	for i := len(result) - 1; i >= 0 && n > 0; i-- {
		sum := int(result[i]) + n
		result[i] = byte(sum % 256)
		n = sum / 256
	}

	return result
}
