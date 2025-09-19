// sentiric-dialplan-service/cmd/dialplan-service/main.go
package main

import (
	"fmt"
	"net"
	"net/http" // YENİ
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	"github.com/sentiric/sentiric-dialplan-service/internal/config"
	"github.com/sentiric/sentiric-dialplan-service/internal/platform/db"
	platformgrpc "github.com/sentiric/sentiric-dialplan-service/internal/platform/grpc"
	"github.com/sentiric/sentiric-dialplan-service/internal/platform/logger"
	"github.com/sentiric/sentiric-dialplan-service/internal/repository/postgres"
	grpchandler "github.com/sentiric/sentiric-dialplan-service/internal/server/grpc"
	"github.com/sentiric/sentiric-dialplan-service/internal/service/dialplan"
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

	log := logger.New(serviceName, cfg.Env)

	log.Info().
		Str("version", ServiceVersion).
		Str("commit", GitCommit).
		Str("build_date", BuildDate).
		Str("profile", cfg.Env).
		Msg("🚀 dialplan-service başlatılıyor...")

	dbPool, err := db.NewConnection(cfg.Postgres.URL)
	if err != nil {
		log.Fatal().Err(err).Msg("Veritabanı bağlantısı kurulamadı")
	}
	defer dbPool.Close()

	repo := postgres.NewRepository(dbPool, log)

	userClient, userConn, err := dialplan.NewUserServiceClient(cfg.Clients.UserServiceURL, *cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("User service istemcisi oluşturulamadı")
	}
	defer userConn.Close()

	dialplanSvc := dialplan.NewService(repo, userClient, log)
	handler := grpchandler.NewHandler(dialplanSvc, log)

	grpcServer, err := platformgrpc.NewServer(*cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("gRPC sunucusu oluşturulamadı")
	}
	dialplanv1.RegisterDialplanServiceServer(grpcServer, handler)
	reflection.Register(grpcServer)
	
	// DEĞİŞİKLİK: startMetricsServer -> startHttpServer
	go startHttpServer(log, cfg.Server.MetricsPort, cfg.Server.HttpPort)

	startGRPCServer(log, cfg.Server.GRPCPort, grpcServer)

	waitForShutdown(log, grpcServer)
}

// DEĞİŞİKLİK: Fonksiyonun adı ve içeriği güncellendi
func startHttpServer(log zerolog.Logger, metricsPort string, httpPort string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status": "ok"}`)
	})
	
	// Metrik sunucusu
	go func() {
		metricsAddr := fmt.Sprintf(":%s", metricsPort)
		log.Info().Str("port", metricsPort).Msg("Metrics sunucusu dinleniyor")
		if err := http.ListenAndServe(metricsAddr, mux); err != nil {
			log.Error().Err(err).Msg("Metrics sunucusu başlatılamadı")
		}
	}()

	// Health check sunucusu
	httpAddr := fmt.Sprintf(":%s", httpPort)
	log.Info().Str("port", httpPort).Msg("HTTP sunucusu (health) dinleniyor")
	if err := http.ListenAndServe(httpAddr, mux); err != nil {
		log.Error().Err(err).Msg("HTTP sunucusu başlatılamadı")
	}
}

func startGRPCServer(log zerolog.Logger, port string, server *grpc.Server) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Fatal().Err(err).Msg("gRPC portu dinlenemedi")
	}

	go func() {
		log.Info().Str("port", port).Msg("gRPC sunucusu dinleniyor")
		if err := server.Serve(lis); err != nil {
			log.Fatal().Err(err).Msg("gRPC sunucusu başlatılamadı")
		}
	}()
}

func waitForShutdown(log zerolog.Logger, server *grpc.Server) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Servis kapatılıyor...")
	server.GracefulStop()
	log.Info().Msg("Servis başarıyla kapatıldı.")
}