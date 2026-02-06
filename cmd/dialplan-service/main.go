// sentiric-dialplan-service/cmd/dialplan-service/main.go
package main

import (
	"fmt"
	"os"

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

	log := logger.New(serviceName, cfg.Env, cfg.LogLevel)

	log.Info().
		Str("version", ServiceVersion).
		Str("commit", GitCommit).
		Str("build_date", BuildDate).
		Str("profile", cfg.Env).
		Msg("🚀 dialplan-service başlatılıyor...")

	application := app.NewApp(cfg, log)
	application.Run()
}
