// sentiric-dialplan-service/internal/app/app.go
package app

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
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	"github.com/sentiric/sentiric-dialplan-service/internal/cache"
	"github.com/sentiric/sentiric-dialplan-service/internal/client"
	"github.com/sentiric/sentiric-dialplan-service/internal/config"
	"github.com/sentiric/sentiric-dialplan-service/internal/database"
	"github.com/sentiric/sentiric-dialplan-service/internal/logger"
	"github.com/sentiric/sentiric-dialplan-service/internal/repository/postgres"
	platformServer "github.com/sentiric/sentiric-dialplan-service/internal/server"
	grpchandler "github.com/sentiric/sentiric-dialplan-service/internal/server/grpc"
	"github.com/sentiric/sentiric-dialplan-service/internal/service/dialplan"
)

type App struct {
	Cfg *config.Config
	Log zerolog.Logger
}

func NewApp(cfg *config.Config, log zerolog.Logger) *App {
	return &App{Cfg: cfg, Log: log}
}

func (a *App) Run() {
	// 1. Altyapı Bağlantıları
	dbPool, err := database.NewConnection(a.Cfg.DatabaseURL)
	if err != nil {
		a.Log.Error().Err(err).Str("event", logger.EventDBConnectionFail).Msg("Veritabanı ping başarısız, havuz arka planda tekrar deneyecek (Ghost Mode).")
	} else {
		a.Log.Info().Str("event", "DB_CONNECTION_SUCCESS").Msg("✅ Veritabanı bağlantısı sağlandı.")
	}

	if dbPool != nil {
		defer dbPool.Close()
	}

	redisClient := a.setupRedis()
	defer redisClient.Close()

	userClient, userConn, err := client.NewUserServiceClient(a.Cfg.UserServiceURL, *a.Cfg)
	if err != nil {
		a.Log.Error().Err(err).Str("event", logger.EventUserSvcConnectionFail).Msg("User service istemcisi başlatılamadı. Ghost misafir moduna düşülebilir.")
	}
	if userConn != nil {
		defer userConn.Close()
	}

	// 2. Bağımlılıkların Oluşturulması
	repo := postgres.NewRepository(dbPool, a.Log)
	userCache := cache.NewUserCache(redisClient)
	dialplanSvc := dialplan.NewService(repo, userClient, userCache, a.Log)
	handler := grpchandler.NewHandler(dialplanSvc, a.Log)

	// 3. gRPC Sunucusu
	grpcServer, err := platformServer.NewServer(*a.Cfg, a.Log)
	if err != nil {
		a.Log.Fatal().Err(err).Str("event", logger.EventGRPCServerFail).Msg("gRPC sunucusu oluşturulamadı")
	}
	dialplanv1.RegisterDialplanServiceServer(grpcServer, handler)
	reflection.Register(grpcServer)

	// 4. Sunucuları Başlat (Anında Port Açılır)
	httpServer := a.startHttpServer()
	a.startGRPCServer(grpcServer)

	// 5. Graceful Shutdown
	a.waitForShutdown(grpcServer, httpServer)
}

func (a *App) setupRedis() *redis.Client {
	redisOpts, err := redis.ParseURL(a.Cfg.RedisURL)
	if err != nil {
		a.Log.Error().Err(err).Str("event", logger.EventRedisConnectionFail).Msg("Geçersiz Redis URL. Önbellek kapalı.")
		return redis.NewClient(&redis.Options{})
	}
	redisClient := redis.NewClient(redisOpts)

	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		a.Log.Error().Err(err).Str("event", logger.EventRedisConnectionFail).Msg("Redis bağlantısı başarısız, cache devre dışı kalabilir")
	} else {
		a.Log.Info().Str("event", logger.EventRedisConnected).Str("url", a.Cfg.RedisURL).Msg("✅ Redis bağlantısı sağlandı")
	}
	return redisClient
}

func (a *App) startHttpServer() *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status": "ok"}`)
	})

	addr := fmt.Sprintf(":%s", a.Cfg.Server.HttpPort)
	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		a.Log.Info().Str("event", logger.EventHTTPServerStart).Str("port", a.Cfg.Server.HttpPort).Msg("HTTP sunucusu (health & metrics) dinleniyor...")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.Log.Fatal().Err(err).Str("event", logger.EventHTTPServerFail).Msg("HTTP sunucusu başlatılamadı")
		}
	}()
	return srv
}

func (a *App) startGRPCServer(srv *grpc.Server) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", a.Cfg.Server.GRPCPort))
	if err != nil {
		a.Log.Fatal().Err(err).Str("event", logger.EventGRPCServerFail).Msg("gRPC portu dinlenemedi")
	}

	go func() {
		a.Log.Info().Str("event", logger.EventGRPCServerStart).Str("port", a.Cfg.Server.GRPCPort).Msg("gRPC sunucusu dinleniyor")
		if err := srv.Serve(lis); err != nil {
			a.Log.Fatal().Err(err).Str("event", logger.EventGRPCServerFail).Msg("gRPC sunucusu başlatılamadı")
		}
	}()
}

func (a *App) waitForShutdown(grpcSrv *grpc.Server, httpSrv *http.Server) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	a.Log.Warn().Str("event", logger.EventSystemShutdown).Msg("Kapatma sinyali alındı, servisler durduruluyor...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a.Log.Info().Str("event", logger.EventGRPCServerStop).Msg("gRPC sunucusu durduruluyor...")
	grpcSrv.GracefulStop()

	a.Log.Info().Str("event", logger.EventHTTPServerStop).Msg("HTTP sunucusu durduruluyor...")
	if err := httpSrv.Shutdown(ctx); err != nil {
		a.Log.Error().Err(err).Str("event", logger.EventHTTPServerFail).Msg("HTTP sunucusu düzgün kapatılamadı.")
	}

	a.Log.Info().Str("event", logger.EventSystemShutdown).Msg("Servis başarıyla durduruldu.")
}
