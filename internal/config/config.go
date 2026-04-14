// sentiric-dialplan-service/internal/config/config.go
package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type ServerConfig struct {
	HttpPort    string
	GRPCPort    string
	MetricsPort string
}

type TLSConfig struct {
	CertPath string
	KeyPath  string
	CaPath   string
}

type Config struct {
	Env            string
	LogLevel       string
	LogFormat      string
	NodeHostname   string
	ServiceVersion string
	DatabaseURL    string
	UserServiceURL string
	RedisURL       string
	Server         ServerConfig
	TLS            TLSConfig
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Env:          getEnv("ENV", "production"),
		LogLevel:     getEnv("LOG_LEVEL", "info"),
		LogFormat:    getEnv("LOG_FORMAT", "json"),
		NodeHostname: getEnv("NODE_HOSTNAME", "localhost"),
		// versionlama bilgisini aynı rust projeleri gibi bir standartda bağlamalıyız!!!!
		ServiceVersion: getEnv("SERVICE_VERSION", "1.0.4"),
		DatabaseURL:    getEnvOrFail("POSTGRES_URL"),
		UserServiceURL: getEnvOrFail("USER_SERVICE_TARGET_GRPC_URL"),
		RedisURL:       getEnv("REDIS_URL", "redis://redis.service.sentiric.cloud:6379/0"),
		Server: ServerConfig{
			HttpPort:    getEnv("DIALPLAN_SERVICE_HTTP_PORT", "12020"),
			GRPCPort:    getEnv("DIALPLAN_SERVICE_GRPC_PORT", "12021"),
			MetricsPort: getEnv("DIALPLAN_SERVICE_METRICS_PORT", "12022"),
		},
		TLS: TLSConfig{
			CertPath: getEnvOrFail("DIALPLAN_SERVICE_CERT_PATH"),
			KeyPath:  getEnvOrFail("DIALPLAN_SERVICE_KEY_PATH"),
			CaPath:   getEnvOrFail("GRPC_TLS_CA_PATH"),
		},
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvOrFail(key string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		fmt.Fprintf(os.Stderr, "Kritik Hata: Gerekli ortam değişkeni tanımlı değil: %s\n", key)
		os.Exit(1)
	}
	return value
}
