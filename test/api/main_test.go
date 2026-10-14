// Package apitest exercises the premium membership and video catalog endpoints through
// a fully assembled Fiber app (real router, real middleware.Auth/RequirePermission, real
// PostgreSQL) instead of calling the service layer directly, so authentication and
// authorization are covered by the same tests as the business logic.
//
// The suite needs the database started by ../../docker-compose.test.yml and configured
// by ../test.env; see that file for how to start it.
package apitest

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/joho/godotenv"
	"github.com/yuusufyan/go-common/pkg/database"
	gclogger "github.com/yuusufyan/go-common/pkg/logger"
	"gorm.io/gorm"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/migration"
)

var (
	testDB        *gorm.DB
	jwtTestSecret string
)

func TestMain(m *testing.M) {
	envPath := testEnvPath()
	if err := godotenv.Load(envPath); err != nil {
		fmt.Fprintf(os.Stderr, "api test: cannot load %s: %v\n"+
			"Start the test database first: docker compose -f docker-compose.test.yml up -d\n", envPath, err)
		os.Exit(1)
	}

	cfg, err := loadTestDBConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "api test:", err)
		os.Exit(1)
	}

	jwtTestSecret = mustEnv("JWT_SECRET")
	if len(jwtTestSecret) < 32 {
		fmt.Fprintln(os.Stderr, "api test: JWT_SECRET in test/test.env must be at least 32 characters")
		os.Exit(1)
	}

	quietLog := gclogger.NewWithConfig(gclogger.Config{Output: io.Discard})
	db, err := database.Connect(cfg, quietLog, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "api test: connect to test database: %v\n"+
			"Start it first: docker compose -f docker-compose.test.yml up -d\n", err)
		os.Exit(1)
	}
	if err := migration.Up(db); err != nil {
		fmt.Fprintln(os.Stderr, "api test: apply migrations:", err)
		os.Exit(1)
	}
	testDB = db

	code := m.Run()

	if sqlDB, dbErr := db.DB(); dbErr == nil {
		_ = sqlDB.Close()
	}
	os.Exit(code)
}

// testEnvPath resolves ../test.env relative to this file, regardless of the working
// directory the test binary is invoked from.
func testEnvPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "test.env")
}

// loadTestDBConfig reads the test database connection from the environment loaded out of
// test/test.env. It refuses to run against anything that looks like the development
// database instead of silently falling back to development defaults.
func loadTestDBConfig() (*database.DBConfig, error) {
	port, err := strconv.Atoi(mustEnv("DB_PORT"))
	if err != nil {
		return nil, fmt.Errorf("DB_PORT: %w", err)
	}
	if port == 5432 {
		return nil, fmt.Errorf("DB_PORT is 5432, the development default; test/test.env must point at an isolated test database")
	}
	name := mustEnv("DB_NAME")
	if !strings.Contains(strings.ToLower(name), "test") {
		return nil, fmt.Errorf("DB_NAME %q does not look like a test database (expected it to contain \"test\")", name)
	}
	return &database.DBConfig{
		Host:     mustEnv("DB_HOST"),
		User:     mustEnv("DB_USER"),
		Password: mustEnv("DB_PASSWORD"),
		DBName:   name,
		Port:     port,
		SSLMode:  mustEnv("DB_SSLMODE"),
		TimeZone: mustEnv("DB_TIMEZONE"),
	}, nil
}

func mustEnv(key string) string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		fmt.Fprintf(os.Stderr, "api test: environment variable %s is not set; load test/test.env first\n", key)
		os.Exit(1)
	}
	return v
}
