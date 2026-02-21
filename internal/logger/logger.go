// sentiric-dialplan-service/internal/logger/logger.go
package logger

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc/metadata"
)

const (
	SchemaVersion = "1.0.0"
	DefaultTenant = "sentiric_demo"
)

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
	// SUTS v4.0 Ana Alanları
	e.Str("schema_v", SchemaVersion)
	e.Str("tenant_id", DefaultTenant)

	// Resource Bloğu
	dict := zerolog.Dict()
	for k, v := range h.Resource {
		dict.Str(k, v)
	}
	e.Dict("resource", dict)

	// DİKKAT: e.Discard() KALDIRILDI! Sistem artık konuşacak.
}

func New(serviceName, version, env, hostname, logLevel, logFormat string) zerolog.Logger {
	var logger zerolog.Logger

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
		switch l {
		case zerolog.TraceLevel, zerolog.DebugLevel:
			return "DEBUG"
		case zerolog.InfoLevel:
			return "INFO"
		case zerolog.WarnLevel:
			return "WARN"
		case zerolog.ErrorLevel:
			return "ERROR"
		case zerolog.FatalLevel, zerolog.PanicLevel:
			return "FATAL"
		default:
			return "INFO"
		}
	}

	resource := map[string]string{
		"service.name":    serviceName,
		"service.version": version,
		"service.env":     env,
		"host.name":       hostname,
	}

	sutsHook := SutsHook{Resource: resource}

	if logFormat == "json" {
		logger = zerolog.New(os.Stderr).Hook(sutsHook).With().Timestamp().Logger()
	} else {
		output := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
		logger = zerolog.New(output).With().Timestamp().Str("service", serviceName).Logger()
	}

	return logger.Level(level)
}

func ExtractTraceIDFromContext(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-trace-id"); len(vals) > 0 && vals[0] != "" {
			return vals[0]
		}
	}
	return "unknown"
}

func ContextLogger(ctx context.Context, baseLog zerolog.Logger) zerolog.Logger {
	traceID := ExtractTraceIDFromContext(ctx)
	if traceID != "unknown" {
		return baseLog.With().Str("trace_id", traceID).Logger()
	}
	return baseLog
}
