package config

import (
	"fmt"
	"os"
	"time"
)

// Config는 exporter 실행에 필요한 설정을 담는다.
type Config struct {
	DatabaseURL   string
	ListenAddr    string
	MetricsPath   string
	ScrapeTimeout time.Duration
}

// Load는 환경변수에서 설정을 읽는다.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		ListenAddr:    getEnv("LISTEN_ADDR", ":9310"),
		MetricsPath:   getEnv("METRICS_PATH", "/metrics"),
		ScrapeTimeout: 10 * time.Second,
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

// getEnv는 환경변수를 읽고, 비어 있으면 기본값을 반환
func getEnv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
