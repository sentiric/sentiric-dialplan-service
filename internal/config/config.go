// sentiric-dialplan-service/internal/config/config.go
package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
)

// ServerConfig, HTTP ve gRPC sunucu portlarını tutar.
type ServerConfig struct {
	HttpPort    string
	GRPCPort    string
	MetricsPort string
}

// TLSConfig, mTLS için sertifika yollarını tutar.
type TLSConfig struct {
	CertPath string
	KeyPath  string
	CaPath   string
}

// Config, uygulamanın tüm yapılandırmasını içerir.
type Config struct {
	Env            string
	LogLevel       string
	DatabaseURL    string
	UserServiceURL string // Bu alan, TARGET URL'i tutacak
	RedisURL       string // ✅ EKLENDİ: Redis Cache URL
	Server         ServerConfig
	TLS            TLSConfig
}

// Load, .env dosyasını ve ortam değişkenlerini okuyarak yapılandırmayı oluşturur.
func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Info().Msg(".env dosyası bulunamadı, ortam değişkenleri kullanılacak.")
	}

	cfg := &Config{
		Env:         getEnv("ENV", "production"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		DatabaseURL: getEnvOrFail("POSTGRES_URL"),

		// --- KRİTİK DEĞİŞİKLİK BURADA ---
		// Artık `user-service` için özel olarak tanımlanmış HEDEF URL'ini okuyoruz.
		UserServiceURL: getEnvOrFail("USER_SERVICE_TARGET_GRPC_URL"),

		RedisURL: getEnv("REDIS_URL", "redis://redis:6379"), // Cache için Redis
		// --- DEĞİŞİKLİK SONA ERDİ ---

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

// getEnv, belirtilen anahtarla bir ortam değişkenini okur, bulunamazsa varsayılan değeri döndürür.
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// getEnvOrFail, belirtilen anahtarla bir ortam değişkenini okur, bulunamazsa programı sonlandırır.
func getEnvOrFail(key string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		// Logger henüz başlatılmadığı için fmt kullanıyoruz.
		fmt.Fprintf(os.Stderr, "Kritik Hata: Gerekli ortam değişkeni tanımlı değil: %s\n", key)
		os.Exit(1)
	}
	return value
}
