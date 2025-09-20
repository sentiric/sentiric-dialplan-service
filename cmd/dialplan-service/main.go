// sentiric-dialplan-service/cmd/dialplan-service/main.go
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	"github.com/sentiric/sentiric-dialplan-service/internal/config"
	"github.com/sentiric/sentiric-dialplan-service/internal/database"
	"github.com/sentiric/sentiric-dialplan-service/internal/logger"
	"github.com/sentiric/sentiric-dialplan-service/internal/repository/postgres"
	platformServer "github.com/sentiric/sentiric-dialplan-service/internal/server"
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

	log := logger.New(serviceName, cfg.Env, cfg.LogLevel)

	log.Info().
		Str("version", ServiceVersion).
		Str("commit", GitCommit).
		Str("build_date", BuildDate).
		Str("profile", cfg.Env).
		Msg("🚀 dialplan-service başlatılıyor...")

	dbPool, err := database.NewConnection(cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Veritabanı bağlantısı kurulamadı")
	}
	defer dbPool.Close()

	repo := postgres.NewRepository(dbPool, log)

	userClient, userConn, err := dialplan.NewUserServiceClient(cfg.UserServiceURL, *cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("User service istemcisi oluşturulamadı")
	}
	defer userConn.Close()

	dialplanSvc := dialplan.NewService(repo, userClient, log)
	handler := grpchandler.NewHandler(dialplanSvc, log)

	// DEĞİŞİKLİK: Artık 'platformServer' paketi üzerinden çağırıyoruz
	grpcServer, err := platformServer.NewServer(*cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("gRPC sunucusu oluşturulamadı")
	}
	dialplanv1.RegisterDialplanServiceServer(grpcServer, handler)
	reflection.Register(grpcServer)

	httpServer := startHttpServer(log, cfg.Server.HttpPort)

	startGRPCServer(log, cfg.Server.GRPCPort, grpcServer)

	waitForShutdown(log, grpcServer, httpServer)
}

func startHttpServer(log zerolog.Logger, port string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status": "ok"}`)
	})

	addr := fmt.Sprintf(":%s", port)
	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		log.Info().Str("port", port).Msg("HTTP sunucusu (health & metrics) dinleniyor...")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("HTTP sunucusu başlatılamadı")
		}
	}()
	return srv
}

func startGRPCServer(log zerolog.Logger, port string, srv *grpc.Server) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Fatal().Err(err).Msg("gRPC portu dinlenemedi")
	}

	go func() {
		log.Info().Str("port", port).Msg("gRPC sunucusu dinleniyor")
		if err := srv.Serve(lis); err != nil {
			log.Fatal().Err(err).Msg("gRPC sunucusu başlatılamadı")
		}
	}()
}

func waitForShutdown(log zerolog.Logger, grpcSrv *grpc.Server, httpSrv *http.Server) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Warn().Msg("Kapatma sinyali alındı, servisler durduruluyor...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Info().Msg("gRPC sunucusu durduruluyor...")
	grpcSrv.GracefulStop()
	log.Info().Msg("gRPC sunucusu durduruldu.")

	log.Info().Msg("HTTP sunucusu durduruluyor...")
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("HTTP sunucusu düzgün kapatılamadı.")
	} else {
		log.Info().Msg("HTTP sunucusu durduruldu.")
	}

	log.Info().Msg("Servis başarıyla durduruldu.")
}