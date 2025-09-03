package main

import (
	"fmt"
	"net"
	"net/http"
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

// YENİ: ldflags ile doldurulacak değişkenler
var (
	ServiceVersion string
	GitCommit      string
	BuildDate      string
)

const serviceName = "dialplan-service"

func main() {
	log := logger.New(serviceName, os.Getenv("ENV"))

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("Konfigürasyon yüklenemedi")
	}

	// YENİ: Başlangıçta versiyon bilgisini logla
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

	go startMetricsServer(log, cfg.Server.MetricsPort)

	startGRPCServer(log, cfg.Server.GRPCPort, grpcServer)

	waitForShutdown(log, grpcServer)
}

func startMetricsServer(log zerolog.Logger, port string) {
	http.Handle("/metrics", promhttp.Handler())
	log.Info().Str("port", port).Msg("Metrics sunucusu dinleniyor")
	if err := http.ListenAndServe(fmt.Sprintf(":%s", port), nil); err != nil {
		log.Error().Err(err).Msg("Metrics sunucusu başlatılamadı")
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
