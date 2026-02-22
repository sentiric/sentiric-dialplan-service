// internal/logger/logger.go
package logger

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc/metadata"
)

const (
	SchemaVersion = "1.0.0"
	DefaultTenant = "system"
)

// New: SUTS v4.0 uyumlu Logger oluşturur. Hook yapısı kaldırıldı, With() ile context kuruldu.
func New(serviceName, version, env, hostname, logLevel, logFormat string) zerolog.Logger {
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}

	// SUTS Alan Adı Değişiklikleri
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.TimestampFieldName = "ts"
	zerolog.LevelFieldName = "severity"
	zerolog.MessageFieldName = "message"

	// Severity Değerlerini Büyük Harf Yap
	zerolog.LevelFieldMarshalFunc = func(l zerolog.Level) string {
		return strings.ToUpper(l.String())
	}

	// Statik SUTS alanlarını baştan tanımla
	resourceContext := zerolog.Dict().
		Str("service.name", serviceName).
		Str("service.version", version).
		Str("service.env", env).
		Str("host.name", hostname)

	var logger zerolog.Logger

	if logFormat == "json" {
		// Production: JSON formatı. Hook yerine With() kullanılıyor.
		logger = zerolog.New(os.Stderr).With().
			Timestamp().
			Str("schema_v", SchemaVersion).
			Dict("resource", resourceContext).
			Logger()
	} else {
		// Development: Okunabilir konsol çıktısı
		output := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
		logger = zerolog.New(output).With().Timestamp().Str("service", serviceName).Logger()
	}

	return logger.Level(level)
}

// ContextLogger: gRPC context'inden trace_id'yi alıp logger'a ekler.
func ContextLogger(ctx context.Context, baseLog zerolog.Logger) zerolog.Logger {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-trace-id"); len(vals) > 0 && vals[0] != "" {
			return baseLog.With().Str("trace_id", vals[0]).Logger()
		}
	}
	// Trace ID yoksa bile log atmaya devam et ama alanı ekleme
	return baseLog
}
