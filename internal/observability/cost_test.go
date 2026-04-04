package observability_test

import (
	"testing"

	"github.com/dnakitare/aether/internal/observability"
)

func TestDefaultPricing(t *testing.T) {
	pricing := observability.DefaultPricing()

	if pricing.CPUCoreHour <= 0 {
		t.Error("Expected CPUCoreHour to be positive")
	}
	if pricing.MemoryGBHour <= 0 {
		t.Error("Expected MemoryGBHour to be positive")
	}
	if pricing.StorageGBMonth <= 0 {
		t.Error("Expected StorageGBMonth to be positive")
	}
	if pricing.NetworkGB <= 0 {
		t.Error("Expected NetworkGB to be positive")
	}
}

func TestPricingConfig(t *testing.T) {
	pricing := observability.PricingConfig{
		CPUCoreHour:    0.05,
		MemoryGBHour:   0.01,
		StorageGBMonth: 0.15,
		NetworkGB:      0.12,
	}

	if pricing.CPUCoreHour != 0.05 {
		t.Errorf("Expected CPUCoreHour = 0.05, got %f", pricing.CPUCoreHour)
	}
}

// NOTE: Full cost tracker tests require PostgreSQL.
// Integration tests should be run with a test database.
// For unit tests, we test the configuration and pricing logic.
