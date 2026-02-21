package main

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-dialplan-service/internal/app"
	"github.com/sentiric/sentiric-dialplan-service/internal/config"
	"github.com/sentiric/sentiric-dialplan-service/internal/logger"
)

var (
	ServiceVersion string
	GitCommit      string
	BuildDate      string
)

const serviceName = "dialplan-service"

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Konfigürasyon yüklenemedi: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(
		serviceName,
		cfg.ServiceVersion,
		cfg.Env,
		cfg.NodeHostname,
		cfg.LogLevel,
		cfg.LogFormat,
	)

	// SUTS v4.0 STRICT FORMAT
	log.Info().
		Str("event", "SYSTEM_STARTUP").
		Dict("attributes", zerolog.Dict().
			Str("commit", GitCommit).
			Str("build_date", BuildDate)).
		Msg("🚀 dialplan-service başlatılıyor (SUTS v4.0)...")

	application := app.NewApp(cfg, log)
	application.Run()
}
