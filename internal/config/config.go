// Package config loads the service configuration from environment variables.
package config

import (
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"github.com/yuusufyan/go-common/pkg/database"
)

// Config holds all runtime configuration for the service.
type Config struct {
	AppName string
	AppEnv  string
	AppPort string

	DB    database.DBConfig
	Redis database.RedisConfig
	JWT   JWTConfig
	Seed  SeedConfig
}

type JWTConfig struct {
	Secret     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

// SeedConfig describes the superadmin account created on startup when it does not exist yet.
type SeedConfig struct {
	AdminName     string
	AdminEmail    string
	AdminPassword string
}

// IsProd reports whether the service runs in production mode.
func (c *Config) IsProd() bool {
	return c.AppEnv == "production"
}

// Load reads the configuration from environment variables, applying defaults for local development.
func Load() *Config {
	// A missing .env is fine: real environment variables still apply.
	_ = godotenv.Load()

	return &Config{
		AppName: getEnv("APP_NAME", "niskaladaisy-academy-be"),
		AppEnv:  getEnv("APP_ENV", "development"),
		AppPort: getEnv("APP_PORT", "8080"),

		DB: database.DBConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", ""),
			DBName:   getEnv("DB_NAME", "niskaladaisy_academy"),
			Port:     getEnvInt("DB_PORT", 5432),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
			TimeZone: getEnv("DB_TIMEZONE", "Asia/Jakarta"),
		},

		Redis: database.RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnvInt("REDIS_PORT", 6379),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvInt("REDIS_DB", 0),
		},

		JWT: JWTConfig{
			Secret:     getEnv("JWT_SECRET", ""),
			AccessTTL:  getEnvDuration("JWT_ACCESS_TTL", 15*time.Minute),
			RefreshTTL: getEnvDuration("JWT_REFRESH_TTL", 7*24*time.Hour),
		},

		Seed: SeedConfig{
			AdminName:     getEnv("SEED_ADMIN_NAME", "Super Admin"),
			AdminEmail:    getEnv("SEED_ADMIN_EMAIL", ""),
			AdminPassword: getEnv("SEED_ADMIN_PASSWORD", ""),
		},
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return fallback
}
