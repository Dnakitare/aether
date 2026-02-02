package observability_test

import (
	"context"
	"testing"

	"github.com/aether-runtime/aether/internal/observability"
)

func TestSetup(t *testing.T) {
	tests := []struct {
		name    string
		config  observability.Config
		wantErr bool
	}{
		{
			name: "development environment",
			config: observability.Config{
				LogLevel:       observability.LogLevelInfo,
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Environment:    "development",
				EnableTracing:  false,
			},
			wantErr: false,
		},
		{
			name: "production environment",
			config: observability.Config{
				LogLevel:       observability.LogLevelWarn,
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Environment:    "production",
				EnableTracing:  false,
			},
			wantErr: false,
		},
		{
			name: "debug level",
			config: observability.Config{
				LogLevel:       observability.LogLevelDebug,
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Environment:    "development",
				EnableTracing:  false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			logger, cleanup, err := observability.Setup(ctx, tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("Setup() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if logger == nil && !tt.wantErr {
				t.Error("Setup() returned nil logger")
			}

			if cleanup != nil {
				cleanup()
			}
		})
	}
}

func TestLogLevels(t *testing.T) {
	levels := []observability.LogLevel{
		observability.LogLevelDebug,
		observability.LogLevelInfo,
		observability.LogLevelWarn,
		observability.LogLevelError,
	}

	for _, level := range levels {
		t.Run(string(level), func(t *testing.T) {
			ctx := context.Background()
			config := observability.Config{
				LogLevel:       level,
				ServiceName:    "test",
				ServiceVersion: "1.0.0",
				Environment:    "test",
				EnableTracing:  false,
			}

			logger, cleanup, err := observability.Setup(ctx, config)
			if err != nil {
				t.Fatalf("Setup() failed: %v", err)
			}
			defer cleanup()

			if logger == nil {
				t.Fatal("Setup() returned nil logger")
			}
		})
	}
}
