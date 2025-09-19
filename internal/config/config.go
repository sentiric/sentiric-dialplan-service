// sentiric-dialplan-service/internal/config/config.go
package config

import (
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Env    string `envconfig:"ENV" default:"development"`
	Server struct {
		HttpPort    string `envconfig:"DIALPLAN_SERVICE_HTTP_PORT" default:"12020"` // YENİ ALAN
		GRPCPort    string `envconfig:"DIALPLAN_SERVICE_GRPC_PORT" default:"12021"`
		MetricsPort string `envconfig:"DIALPLAN_SERVICE_METRICS_PORT" default:"12022"`
	}
	Postgres struct {
		URL string `envconfig:"POSTGRES_URL" required:"true"`
	}
	Clients struct {
		UserServiceURL string `envconfig:"USER_SERVICE_GRPC_URL" required:"true"`
	}
	TLS struct {
		CertPath string `envconfig:"DIALPLAN_SERVICE_CERT_PATH" required:"true"`
		KeyPath  string `envconfig:"DIALPLAN_SERVICE_KEY_PATH" required:"true"`
		CAPath   string `envconfig:"GRPC_TLS_CA_PATH" required:"true"`
	}
}

func Load() (*Config, error) {
	_ = godotenv.Load()
	var cfg Config
	err := envconfig.Process("", &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}