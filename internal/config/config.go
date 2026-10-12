// Package config loads the service configuration from environment variables.
package config

import (
	"os"
	"strconv"

	"github.com/yuusufyan/go-common/pkg/database"
)

// Config holds all runtime configuration for the service.
type Config struct {
	AppName string
	AppEnv  string
	AppPort string

	DB    database.DBConfig
	Redis database.RedisConfig
}

// IsProd reports whether the service runs in production mode.
func (c *Config) IsProd() bool {
	return c.AppEnv == "production"
}

// Load reads the configuration from environment variables, applying defaults for local development.
func Load() *Config {
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
