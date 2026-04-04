// Package config provides centralized configuration management.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all application configuration.
type Config struct {
	// Server configuration
	Server ServerConfig `mapstructure:"server"`

	// Scheduler configuration
	Scheduler SchedulerConfig `mapstructure:"scheduler"`

	// Database configuration
	Database DatabaseConfig `mapstructure:"database"`

	// Redis configuration
	Redis RedisConfig `mapstructure:"redis"`

	// Kafka configuration
	Kafka KafkaConfig `mapstructure:"kafka"`

	// Etcd configuration
	Etcd EtcdConfig `mapstructure:"etcd"`

	// Observability configuration
	Observability ObservabilityConfig `mapstructure:"observability"`

	// Security configuration
	Security SecurityConfig `mapstructure:"security"`
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	Address        string        `mapstructure:"address"`
	ReadTimeout    time.Duration `mapstructure:"read_timeout"`
	WriteTimeout   time.Duration `mapstructure:"write_timeout"`
	EnableCORS     bool          `mapstructure:"enable_cors"`
	EnableAuth     bool          `mapstructure:"enable_auth"`
	AllowedOrigins []string      `mapstructure:"allowed_origins"`
}

// SchedulerConfig holds scheduler configuration.
type SchedulerConfig struct {
	// Mode: "local" or "distributed"
	Mode string `mapstructure:"mode"`

	// Local scheduler settings
	Strategy        string        `mapstructure:"strategy"`
	Interval        time.Duration `mapstructure:"interval"`
	EventBufferSize int           `mapstructure:"event_buffer_size"`

	// Distributed scheduler settings
	SchedulerID       string        `mapstructure:"scheduler_id"`
	InstanceID        string        `mapstructure:"instance_id"`
	Hostname          string        `mapstructure:"hostname"`
	VirtualNodes      int           `mapstructure:"virtual_nodes"`
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"`
	NumWorkers        int           `mapstructure:"num_workers"`
}

// DatabaseConfig holds PostgreSQL configuration.
type DatabaseConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	Database        string        `mapstructure:"database"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	SSLMode         string        `mapstructure:"ssl_mode"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	MigrationsPath  string        `mapstructure:"migrations_path"`
}

// RedisConfig holds Redis configuration.
type RedisConfig struct {
	Address  string        `mapstructure:"address"`
	Password string        `mapstructure:"password"`
	DB       int           `mapstructure:"db"`
	NodeTTL  time.Duration `mapstructure:"node_ttl"`
}

// KafkaConfig holds Kafka configuration.
type KafkaConfig struct {
	Brokers           []string      `mapstructure:"brokers"`
	Topic             string        `mapstructure:"topic"`
	DLQTopic          string        `mapstructure:"dlq_topic"`
	ConsumerGroup     string        `mapstructure:"consumer_group"`
	MaxRetries        int           `mapstructure:"max_retries"`
	RequestTimeout    time.Duration `mapstructure:"request_timeout"`
	PartitionStrategy string        `mapstructure:"partition_strategy"`
}

// EtcdConfig holds etcd configuration.
type EtcdConfig struct {
	Endpoints  []string `mapstructure:"endpoints"`
	KeyPrefix  string   `mapstructure:"key_prefix"`
	SessionTTL int      `mapstructure:"session_ttl"`
}

// ObservabilityConfig holds observability configuration.
type ObservabilityConfig struct {
	// Logging
	LogLevel  string `mapstructure:"log_level"`
	LogFormat string `mapstructure:"log_format"` // "json" or "text"

	// Metrics
	MetricsEnabled bool   `mapstructure:"metrics_enabled"`
	MetricsPort    int    `mapstructure:"metrics_port"`
	MetricsPath    string `mapstructure:"metrics_path"`

	// Tracing
	TracingEnabled    bool    `mapstructure:"tracing_enabled"`
	TracingSampleRate float64 `mapstructure:"tracing_sample_rate"`
	JaegerEndpoint    string  `mapstructure:"jaeger_endpoint"`
}

// SecurityConfig holds security configuration.
type SecurityConfig struct {
	JWTSecretKey     string        `mapstructure:"jwt_secret_key"`
	JWTTokenDuration time.Duration `mapstructure:"jwt_token_duration"`
	VaultEnabled     bool          `mapstructure:"vault_enabled"`
	VaultAddress     string        `mapstructure:"vault_address"`
	VaultToken       string        `mapstructure:"vault_token"`

	// Asymmetric JWT signing (RS256). When both paths are set, RS256 is used
	// instead of the symmetric HS256 secret above.
	JWTPrivateKeyPath string `mapstructure:"jwt_private_key_path"`
	JWTPublicKeyPath  string `mapstructure:"jwt_public_key_path"`
}

// Load loads configuration from file and environment variables.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Environment variables (set up before reading config)
	v.SetEnvPrefix("AETHER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Configuration file
	if configPath != "" {
		v.SetConfigFile(configPath)
		// Read configuration file
		if err := v.ReadInConfig(); err != nil {
			// If a specific config path was provided but doesn't exist, only fail if it's not a "not found" error
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				// Check if it's an os.PathError for file not found
				if !os.IsNotExist(err) {
					return nil, fmt.Errorf("failed to read config file: %w", err)
				}
			}
			// Config file not found is OK - use defaults and env vars
		}
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
		v.AddConfigPath("/etc/aether")

		// Try to read config file
		_ = v.ReadInConfig() // Ignore error - config file is optional
	}

	// Unmarshal into config struct
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets default configuration values.
func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.address", ":8080")
	v.SetDefault("server.read_timeout", 15*time.Second)
	v.SetDefault("server.write_timeout", 15*time.Second)
	v.SetDefault("server.enable_cors", false)
	v.SetDefault("server.enable_auth", true)
	v.SetDefault("server.allowed_origins", []string{})

	// Scheduler defaults
	v.SetDefault("scheduler.mode", "local")
	v.SetDefault("scheduler.strategy", "bin-packing")
	v.SetDefault("scheduler.interval", 1*time.Second)
	v.SetDefault("scheduler.event_buffer_size", 100)
	v.SetDefault("scheduler.virtual_nodes", 100)
	v.SetDefault("scheduler.heartbeat_interval", 15*time.Second)
	v.SetDefault("scheduler.num_workers", 10)

	// Database defaults
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.database", "aether")
	v.SetDefault("database.user", "aether")
	v.SetDefault("database.ssl_mode", "disable")
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", 5*time.Minute)
	v.SetDefault("database.migrations_path", "migrations")

	// Redis defaults
	v.SetDefault("redis.address", "localhost:6379")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.node_ttl", 60*time.Second)

	// Kafka defaults
	v.SetDefault("kafka.brokers", []string{"localhost:9092"})
	v.SetDefault("kafka.topic", "aether.scheduling.requests")
	v.SetDefault("kafka.dlq_topic", "aether.scheduling.dlq")
	v.SetDefault("kafka.consumer_group", "aether-schedulers")
	v.SetDefault("kafka.max_retries", 3)
	v.SetDefault("kafka.request_timeout", 30*time.Second)
	v.SetDefault("kafka.partition_strategy", "tenant")

	// Etcd defaults
	v.SetDefault("etcd.endpoints", []string{"localhost:2379"})
	v.SetDefault("etcd.key_prefix", "/aether/scheduler/shards")
	v.SetDefault("etcd.session_ttl", 30)

	// Observability defaults
	v.SetDefault("observability.log_level", "info")
	v.SetDefault("observability.log_format", "json")
	v.SetDefault("observability.metrics_enabled", true)
	v.SetDefault("observability.metrics_port", 9090)
	v.SetDefault("observability.metrics_path", "/metrics")
	v.SetDefault("observability.tracing_enabled", false)
	v.SetDefault("observability.tracing_sample_rate", 0.1)

	// Security defaults
	v.SetDefault("security.jwt_token_duration", 1*time.Hour)
	v.SetDefault("security.vault_enabled", false)
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	// Server validation
	if c.Server.Address == "" {
		return fmt.Errorf("server.address is required")
	}

	// Scheduler validation
	if c.Scheduler.Mode != "local" && c.Scheduler.Mode != "distributed" {
		return fmt.Errorf("scheduler.mode must be 'local' or 'distributed'")
	}

	if c.Scheduler.Mode == "distributed" {
		if c.Scheduler.SchedulerID == "" {
			return fmt.Errorf("scheduler.scheduler_id is required in distributed mode")
		}
		if c.Scheduler.InstanceID == "" {
			return fmt.Errorf("scheduler.instance_id is required in distributed mode")
		}
	}

	validStrategies := map[string]bool{
		"bin-packing": true,
		"spread":      true,
		"best-fit":    true,
	}
	if !validStrategies[c.Scheduler.Strategy] {
		return fmt.Errorf("scheduler.strategy must be one of: bin-packing, spread, best-fit")
	}

	// Database validation
	if c.Database.Host == "" {
		return fmt.Errorf("database.host is required")
	}
	if c.Database.Port <= 0 || c.Database.Port > 65535 {
		return fmt.Errorf("database.port must be between 1 and 65535")
	}
	if c.Database.Database == "" {
		return fmt.Errorf("database.database is required")
	}
	if c.Database.User == "" {
		return fmt.Errorf("database.user is required")
	}

	// Redis validation (only in distributed mode)
	if c.Scheduler.Mode == "distributed" {
		if c.Redis.Address == "" {
			return fmt.Errorf("redis.address is required in distributed mode")
		}
	}

	// Kafka validation (only in distributed mode)
	if c.Scheduler.Mode == "distributed" {
		if len(c.Kafka.Brokers) == 0 {
			return fmt.Errorf("kafka.brokers is required in distributed mode")
		}
		if c.Kafka.Topic == "" {
			return fmt.Errorf("kafka.topic is required in distributed mode")
		}
		if c.Kafka.ConsumerGroup == "" {
			return fmt.Errorf("kafka.consumer_group is required in distributed mode")
		}
		if c.Kafka.PartitionStrategy != "tenant" && c.Kafka.PartitionStrategy != "round-robin" {
			return fmt.Errorf("kafka.partition_strategy must be 'tenant' or 'round-robin'")
		}
	}

	// Etcd validation (only in distributed mode)
	if c.Scheduler.Mode == "distributed" {
		if len(c.Etcd.Endpoints) == 0 {
			return fmt.Errorf("etcd.endpoints is required in distributed mode")
		}
	}

	// Security validation
	if c.Server.EnableAuth {
		if c.Security.JWTSecretKey == "" {
			return fmt.Errorf("security.jwt_secret_key is required when authentication is enabled")
		}
		if len(c.Security.JWTSecretKey) < 32 {
			return fmt.Errorf("security.jwt_secret_key must be at least 32 characters")
		}
	}

	// Observability validation
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLogLevels[c.Observability.LogLevel] {
		return fmt.Errorf("observability.log_level must be one of: debug, info, warn, error")
	}

	if c.Observability.LogFormat != "json" && c.Observability.LogFormat != "text" {
		return fmt.Errorf("observability.log_format must be 'json' or 'text'")
	}

	if c.Observability.MetricsPort <= 0 || c.Observability.MetricsPort > 65535 {
		return fmt.Errorf("observability.metrics_port must be between 1 and 65535")
	}

	return nil
}

// GetLogger creates a logger from configuration.
func (c *Config) GetLogger() *slog.Logger {
	var level slog.Level
	switch c.Observability.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	if c.Observability.LogFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

// ConnectionString returns the PostgreSQL connection string.
func (c *DatabaseConfig) ConnectionString() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Database, c.SSLMode,
	)
}

// IsDevelopment returns true if running in development mode.
func (c *Config) IsDevelopment() bool {
	return c.Observability.LogLevel == "debug"
}

// IsProduction returns true if running in production mode.
func (c *Config) IsProduction() bool {
	return c.Server.EnableAuth && !c.IsDevelopment()
}
