// sentiric-dialplan-service/internal/server/grpc.go
package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-dialplan-service/internal/config"
	"github.com/sentiric/sentiric-dialplan-service/internal/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// LoggingInterceptor: Gelen gRPC isteklerini SUTS uyumlu şekilde loglar.
func LoggingInterceptor(log zerolog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		traceID := logger.ExtractTraceIDFromContext(ctx)

		l := log.With().
			Str("trace_id", traceID).
			Str("grpc.method", info.FullMethod).
			Logger()

		// l.Info().Str("event", "GRPC_IN_START").Msg("📥 gRPC isteği alındı") // Gürültüyü azaltmak için Start logunu kaldırdık

		resp, err := handler(ctx, req)
		duration := time.Since(start).Milliseconds()

		if err != nil {
			l.Error().
				Str("event", "GRPC_IN_FAIL").
				Int64("latency_ms", duration).
				Err(err).
				Msg("❌ gRPC isteği hatayla sonuçlandı")
		} else {
			l.Info().
				Str("event", "GRPC_IN_SUCCESS").
				Int64("latency_ms", duration).
				Msg("✅ gRPC isteği başarıyla yanıtlandı")
		}
		return resp, err
	}
}

// NewServer, TLS kimlik bilgilerini yükleyerek yeni bir gRPC sunucu örneği oluşturur.
func NewServer(cfg config.Config, log zerolog.Logger) (*grpc.Server, error) {
	certificate, err := tls.LoadX509KeyPair(cfg.TLS.CertPath, cfg.TLS.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("sunucu sertifikası yüklenemedi: %w", err)
	}

	caCert, err := os.ReadFile(cfg.TLS.CaPath)
	if err != nil {
		return nil, fmt.Errorf("CA sertifikası okunamadı: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("CA sertifikası havuza eklenemedi")
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	}

	opts := []grpc.ServerOption{
		grpc.Creds(credentials.NewTLS(tlsCfg)),
		grpc.UnaryInterceptor(LoggingInterceptor(log)), // Interceptor Eklendi
	}

	return grpc.NewServer(opts...), nil
}
