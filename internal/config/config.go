package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Port      string
	MySQLDSN  string
	JWTSecret string
}

func Load() Config {
	loadDotEnv(findDotEnv())
	return Config{
		Port:      env("APP_PORT", "3000"),
		MySQLDSN:  env("MYSQL_DSN", "root:password@tcp(127.0.0.1:3306)/rbac_request_engine?parseTime=true&multiStatements=true"),
		JWTSecret: env("APP_JWT_SECRET", "dev-secret-change-me"),
	}
}

func findDotEnv() string {
	dir, err := os.Getwd()
	if err != nil {
		return ".env"
	}
	for {
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ".env"
		}
		dir = parent
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}
