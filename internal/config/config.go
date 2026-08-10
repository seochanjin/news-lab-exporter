package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config는 exporter 실행에 필요한 설정을 담는다.
type Config struct {
	DatabaseURL   string
	ListenAddr    string
	MetricsPath   string
	ScrapeTimeout time.Duration
	DBMaxConns    int
}

// Load는 환경변수에서 설정을 읽는다.
func Load() (Config, error) {
	maxConns, err := getEnvInt("DB_MAX_CONNS", 4)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		ListenAddr:    getEnv("LISTEN_ADDR", ":9310"),
		MetricsPath:   getEnv("METRICS_PATH", "/metrics"),
		ScrapeTimeout: 10 * time.Second,
		DBMaxConns:    maxConns,
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

// getEnvInt는 환경변수를 정수로 읽는다.
// 값이 비어 있으면 기본값을 쓰고, 숫자가 아니거나 1 미만이면 오류로 처리한다.
// 잘못된 설정으로 반쯤 동작하는 상태를 만들지 않기 위해서다.
func getEnvInt(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}

	if value < 1 {
		return 0, fmt.Errorf("%s must be >= 1, got %d", key, value)
	}

	return value, nil
}
