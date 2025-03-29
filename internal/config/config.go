package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for our application
type Config struct {
	Environment string
	Server      ServerConfig
	Database    DatabaseConfig
	Auth        AuthConfig
	Logging     LoggingConfig
}

// ServerConfig holds all server related configuration
type ServerConfig struct {
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// DatabaseConfig holds all database related configuration
type DatabaseConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	Name     string
	SSLMode  string
}

// AuthConfig holds all auth related configuration
type AuthConfig struct {
	JWTSecret     string
	TokenDuration time.Duration
}

// LoggingConfig holds all logging related configuration
type LoggingConfig struct {
	Level string
	JSON  bool
}

// LoadConfig loads the config from a file if specified, otherwise from env vars
func LoadConfig(configPath string) (*Config, error) {
	conf := &Config{
		Environment: "development",
	}

	// Set defaults
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.readTimeout", 5*time.Second)
	viper.SetDefault("server.writeTimeout", 10*time.Second)
	viper.SetDefault("server.idleTimeout", 120*time.Second)
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.json", false)

	// Load config from file
	if configPath != "" {
		viper.SetConfigFile(configPath)
		if err := viper.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	// Load config from environment variables
	viper.SetEnvPrefix("PIPEWARDEN")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Unmarshal config
	if err := viper.Unmarshal(conf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return conf, nil
}
