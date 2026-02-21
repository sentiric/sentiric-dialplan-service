// sentiric-dialplan-service/internal/logger/logger.go
package logger

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/metadata"
)

// SUTS v4.0 sabitleri
const (
	SchemaVersion = "1.0.0"
	DefaultTenant = "sentiric_demo"
)

// Olay adları sözlüğü (Gelecekte genişletilebilir)
const (
	EventSystemStartup        = "SYSTEM_STARTUP"
	EventGrpcRequest          = "GRPC_REQUEST_RECEIVED"
	EventDialplanResolveStart = "DIALPLAN_RESOLUTION_START"
	EventDialplanResolveDone  = "DIALPLAN_RESOLUTION_SUCCESS"
	EventRouteNotFound        = "ROUTE_NOT_FOUND"
	EventMaintenanceMode      = "MAINTENANCE_MODE_ACTIVE"
	EventUserCacheHit         = "USER_CACHE_HIT"
	EventUserCacheMiss        = "USER_CACHE_MISS"
	EventUserLookupFailed     = "USER_LOOKUP_FAILED"
)

// SutsHook: Zerolog'a her log satırı yazıldığında araya girip SUTS v4.0 formatını dayatır.
type SutsHook struct {
	Resource map[string]string
}

// Run, her log olayı tetiklendiğinde çalışır.
func (h SutsHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	// 1. Schema ve Tenant Enjeksiyonu
	e.Str("schema_v", SchemaVersion)
	e.Str("tenant_id", DefaultTenant)

	// 2. Resource Enjeksiyonu
	dict := zerolog.Dict()
	for k, v := range h.Resource {
		dict.Str(k, v)
	}
	e.Dict("resource", dict)

	// 3. Severity Normalization (Zerolog'un default "level" alanını silip kendi "severity" alanımızı yazıyoruz)
	e.Discard() // Bu bir zerolog trick'i değildir, ama standart alanları ayarlamak için ayarları aşağıda değiştiriyoruz.
}

// New, SUTS v4.0 uyumlu bir zerolog logger örneği oluşturur.
func New(serviceName, version, env, hostname, logLevel, logFormat string) zerolog.Logger {
	var logger zerolog.Logger

	// Log seviyesini belirle
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		level = zerolog.InfoLevel
		log.Warn().Msgf("Geçersiz LOG_LEVEL '%s', varsayılan olarak 'info' kullanılıyor.", logLevel)
	}

	// --- ZEROLOG GLOBAL AYARLARI (SUTS UYUMU) ---
	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.TimestampFieldName = "ts"
	zerolog.LevelFieldName = "severity"
	zerolog.MessageFieldName = "message"

	// Zerolog default olarak "info", "error" gibi küçük harf basar.
	// SUTS standardı BÜYÜK HARF (INFO, ERROR) ister.
	zerolog.LevelFieldMarshalFunc = func(l zerolog.Level) string {
		return strings.ToUpper(l.String())
	}

	// Resource (Kimlik) Objesi
	resource := map[string]string{
		"service.name":    serviceName,
		"service.version": version,
		"service.env":     env,
		"host.name":       hostname,
	}

	// SUTS Hook'u tanımla
	sutsHook := SutsHook{Resource: resource}

	// Logger'ı oluştur
	if logFormat == "json" {
		logger = zerolog.New(os.Stderr).Hook(sutsHook).With().Timestamp().Logger()
	} else {
		// Development ortamı için okunabilir format
		output := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
		logger = zerolog.New(output).With().Timestamp().Str("service", serviceName).Logger()
	}

	return logger.Level(level)
}

// ExtractTraceIDFromContext, gRPC context'inden x-trace-id okur
func ExtractTraceIDFromContext(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-trace-id"); len(vals) > 0 && vals[0] != "" {
			return vals[0]
		}
	}
	return "unknown"
}

// ContextLogger, trace_id eklenmiş bir logger döndürür
func ContextLogger(ctx context.Context, baseLog zerolog.Logger) zerolog.Logger {
	traceID := ExtractTraceIDFromContext(ctx)
	if traceID != "unknown" {
		return baseLog.With().Str("trace_id", traceID).Logger()
	}
	return baseLog
}
