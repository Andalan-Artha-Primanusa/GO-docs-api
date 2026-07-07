package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Port          string
	MySQLDSN      string
	JWTSecret     string
	UploadDir     string
	UploadStorage string
	SFTPHost      string
	SFTPPort      string
	SFTPUser      string
	SFTPPassword  string
	SFTPDir       string
	FTPHost       string
	FTPPort       string
	FTPUser       string
	FTPPassword   string
	FTPDir        string
	FTPTLS        bool
}

func Load() Config {
	loadDotEnv(findDotEnv())
	return Config{
		Port:          env("APP_PORT", "3000"),
		MySQLDSN:      env("MYSQL_DSN", "root:password@tcp(127.0.0.1:3306)/rbac_request_engine?parseTime=true&multiStatements=true"),
		JWTSecret:     env("APP_JWT_SECRET", "dev-secret-change-me"),
		UploadDir:     env("UPLOAD_DIR", "uploads"),
		UploadStorage: strings.ToLower(env("UPLOAD_STORAGE", "local")),
		SFTPHost:      env("SFTP_HOST", ""),
		SFTPPort:      env("SFTP_PORT", "22"),
		SFTPUser:      env("SFTP_USER", ""),
		SFTPPassword:  env("SFTP_PASSWORD", ""),
		SFTPDir:       env("SFTP_DIR", "/uploads"),
		FTPHost:       env("FTP_HOST", ""),
		FTPPort:       env("FTP_PORT", "21"),
		FTPUser:       env("FTP_USER", ""),
		FTPPassword:   env("FTP_PASSWORD", ""),
		FTPDir:        env("FTP_DIR", "/uploads"),
		FTPTLS:        strings.ToLower(env("FTP_TLS", "false")) == "true",
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
