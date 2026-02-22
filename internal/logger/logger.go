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
	DefaultTenant = "system"
)

type SutsHook struct {
	Resource map[string]string
}

func (h SutsHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	e.Str("schema_v", SchemaVersion)
	dict := zerolog.Dict()
	for k, v := range h.Resource {
		dict.Str(k, v)
	}
	e.Dict("resource", dict)
}

func New(serviceName, version, env, hostname, logLevel, logFormat string) zerolog.Logger {
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}

	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.TimestampFieldName = "ts"
	zerolog.LevelFieldName = "severity"
	zerolog.MessageFieldName = "message"
	zerolog.LevelFieldMarshalFunc = func(l zerolog.Level) string {
		return strings.ToUpper(l.String())
	}

	resource := map[string]string{
		"service.name":    serviceName,
		"service.version": version,
		"service.env":     env,
		"host.name":       hostname,
	}

	var logger zerolog.Logger
	if logFormat == "json" {
		logger = zerolog.New(os.Stderr).Hook(SutsHook{Resource: resource}).With().Timestamp().Logger()
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
		if vals := md.Get("trace_id"); len(vals) > 0 && vals[0] != "" {
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
