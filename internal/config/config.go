// DOSYA: sentiric-dialplan-service/internal/config/config.go

package config

import (
	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
	"os"
)

type Config struct {
	Env                string
	LogLevel           string
	GRPCPort           string
	HttpPort           string
	DatabaseURL        string
	UserServiceURL     string
	CertPath           string
	KeyPath            string
	CaPath             string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	return &Config{
		Env:            getEnv("ENV", "production"),
		LogLevel:       getEnv("LOG_LEVEL", "info"),
		GRPCPort:       getEnv("DIALPLAN_SERVICE_GRPC_PORT", "12021"),
		HttpPort:       getEnv("DIALPLAN_SERVICE_HTTP_PORT", "12020"),
		DatabaseURL:    getEnvOrFail("POSTGRES_URL"),
		UserServiceURL: getEnvOrFail("USER_SERVICE_GRPC_URL"),
		CertPath:       getEnvOrFail("DIALPLAN_SERVICE_CERT_PATH"),
		KeyPath:        getEnvOrFail("DIALPLAN_SERVICE_KEY_PATH"),
		CaPath:         getEnvOrFail("GRPC_TLS_CA_PATH"),
	}, nil
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
		log.Fatal().Str("variable", key).Msg("Gerekli ortam değişkeni tanımlı değil")
	}
	return value
}