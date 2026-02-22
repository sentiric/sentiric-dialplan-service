// sentiric-dialplan-service/internal/logger/logger.go
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
	DefaultTenant = "sentiric_demo"
)

// Standart Event İsimleri
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

// SutsHook: Her log satırına SUTS zorunlu alanlarını ekler.
type SutsHook struct {
	Resource map[string]string
}

func (h SutsHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	// 1. Governance
	e.Str("schema_v", SchemaVersion)
	e.Str("tenant_id", DefaultTenant)

	// 2. Resource (Nested Object)
	dict := zerolog.Dict()
	for k, v := range h.Resource {
		dict.Str(k, v)
	}
	e.Dict("resource", dict)
}

// New: SUTS v4.0 uyumlu Logger oluşturur.
func New(serviceName, version, env, hostname, logLevel, logFormat string) zerolog.Logger {
	var logger zerolog.Logger

	// Log Seviyesini Parse Et
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}

	// --- ZEROLOG GLOBAL AYARLARI (SUTS v4.0 DÖNÜŞÜMÜ) ---
	zerolog.TimeFieldFormat = time.RFC3339Nano // SUTS milisaniye hassasiyeti sever
	zerolog.TimestampFieldName = "ts"
	zerolog.LevelFieldName = "severity"
	zerolog.MessageFieldName = "message"

	// Severity değerlerini Uppercase yap (info -> INFO)
	zerolog.LevelFieldMarshalFunc = func(l zerolog.Level) string {
		return strings.ToUpper(l.String())
	}

	// Resource Context Hazırla
	resource := map[string]string{
		"service.name":    serviceName,
		"service.version": version,
		"service.env":     env,
		"host.name":       hostname,
	}

	sutsHook := SutsHook{Resource: resource}

	if logFormat == "json" {
		// Production: JSON + SUTS Hook
		logger = zerolog.New(os.Stderr).
			Hook(sutsHook).
			With().
			Timestamp().
			Logger()
	} else {
		// Development: Renkli Console
		output := zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
		}
		// Dev modunda bile temel alanları görelim
		logger = zerolog.New(output).
			With().
			Timestamp().
			Str("service", serviceName).
			Logger()
	}

	return logger.Level(level)
}

// ExtractTraceIDFromContext: gRPC Metadata'dan trace_id çeker
func ExtractTraceIDFromContext(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		// İstek başlıklarında "x-trace-id" veya "trace-id" ara
		if vals := md.Get("x-trace-id"); len(vals) > 0 && vals[0] != "" {
			return vals[0]
		}
		if vals := md.Get("trace_id"); len(vals) > 0 && vals[0] != "" {
			return vals[0]
		}
	}
	return "unknown"
}

// ContextLogger: Trace ID ile zenginleştirilmiş logger döndürür
func ContextLogger(ctx context.Context, baseLog zerolog.Logger) zerolog.Logger {
	traceID := ExtractTraceIDFromContext(ctx)
	if traceID != "unknown" {
		return baseLog.With().Str("trace_id", traceID).Logger()
	}
	return baseLog
}
