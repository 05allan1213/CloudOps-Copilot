package logger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const defaultLevel = zapcore.InfoLevel

func Init(service string) (*zap.Logger, error) {
	level, err := parseLevel(os.Getenv("LOG_LEVEL"))
	if err != nil {
		return nil, err
	}

	cfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(level),
		Development:      false,
		Encoding:         "json",
		EncoderConfig:    newEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	base, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	log := base.With(
		zap.String("service", service),
		zap.String("instance", instance()),
	)
	zap.ReplaceGlobals(log)
	slog.SetDefault(slog.New(newSlogHandler(log)))

	return log, nil
}

func Sync(log *zap.Logger) {
	if log == nil {
		return
	}
	if err := log.Sync(); err != nil && !isIgnorableSyncError(err) {
		fmt.Fprintf(os.Stderr, "logger sync failed: %v\n", err)
	}
}

func FromContext(ctx context.Context) *zap.Logger {
	log := zap.L()
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return log
	}
	return log.With(
		zap.String("trace_id", spanCtx.TraceID().String()),
		zap.String("span_id", spanCtx.SpanID().String()),
	)
}

func parseLevel(raw string) (zapcore.Level, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return defaultLevel, nil
	}

	var level zapcore.Level
	if err := level.UnmarshalText([]byte(value)); err != nil {
		return defaultLevel, fmt.Errorf("invalid LOG_LEVEL %q", raw)
	}
	return level, nil
}

func instance() string {
	for _, key := range []string{"INSTANCE", "HOSTNAME"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}

	hostname, err := os.Hostname()
	if err == nil && strings.TrimSpace(hostname) != "" {
		return hostname
	}
	return "unknown"
}

func isIgnorableSyncError(err error) bool {
	return errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.ENOTTY) ||
		errors.Is(err, syscall.EBADF)
}

func newEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     utcMillisTimeEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
		EncodeName:     zapcore.FullNameEncoder,
	}
}

func utcMillisTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.UTC().Format("2006-01-02T15:04:05.000Z"))
}

type slogHandler struct {
	logger *zap.Logger
	attrs  []slog.Attr
	groups []string
}

func newSlogHandler(log *zap.Logger) slog.Handler {
	return &slogHandler{logger: log}
}

func (h *slogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return h.logger.Core().Enabled(zapLevel(level))
}

func (h *slogHandler) Handle(_ context.Context, record slog.Record) error {
	fields := make([]zap.Field, 0, len(h.attrs)+record.NumAttrs())
	for _, attr := range h.attrs {
		fields = appendSlogAttr(fields, h.groups, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		fields = appendSlogAttr(fields, h.groups, attr)
		return true
	})

	checked := h.logger.Check(zapLevel(record.Level), record.Message)
	if checked != nil {
		checked.Write(fields...)
	}
	return nil
}

func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := &slogHandler{
		logger: h.logger,
		attrs:  append([]slog.Attr{}, h.attrs...),
		groups: append([]string{}, h.groups...),
	}
	next.attrs = append(next.attrs, attrs...)
	return next
}

func (h *slogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &slogHandler{
		logger: h.logger,
		attrs:  append([]slog.Attr{}, h.attrs...),
		groups: append(append([]string{}, h.groups...), name),
	}
}

func zapLevel(level slog.Level) zapcore.Level {
	switch {
	case level <= slog.LevelDebug:
		return zapcore.DebugLevel
	case level >= slog.LevelError:
		return zapcore.ErrorLevel
	case level >= slog.LevelWarn:
		return zapcore.WarnLevel
	default:
		return zapcore.InfoLevel
	}
}

func appendSlogAttr(fields []zap.Field, groups []string, attr slog.Attr) []zap.Field {
	attr.Value = attr.Value.Resolve()
	if attr.Key == "" && attr.Value.Kind() == slog.KindAny && attr.Value.Any() == nil {
		return fields
	}

	key := fieldKey(groups, attr.Key)
	switch attr.Value.Kind() {
	case slog.KindString:
		return append(fields, zap.String(key, attr.Value.String()))
	case slog.KindBool:
		return append(fields, zap.Bool(key, attr.Value.Bool()))
	case slog.KindInt64:
		return append(fields, zap.Int64(key, attr.Value.Int64()))
	case slog.KindUint64:
		return append(fields, zap.Uint64(key, attr.Value.Uint64()))
	case slog.KindFloat64:
		return append(fields, zap.Float64(key, attr.Value.Float64()))
	case slog.KindDuration:
		return append(fields, zap.Duration(key, attr.Value.Duration()))
	case slog.KindTime:
		return append(fields, zap.Time(key, attr.Value.Time()))
	case slog.KindGroup:
		nextGroups := groups
		if attr.Key != "" {
			nextGroups = append(append([]string{}, groups...), attr.Key)
		}
		for _, child := range attr.Value.Group() {
			fields = appendSlogAttr(fields, nextGroups, child)
		}
		return fields
	default:
		value := attr.Value.Any()
		if err, ok := value.(error); ok {
			if key == "error" {
				return append(fields, zap.Error(err))
			}
			return append(fields, zap.NamedError(key, err))
		}
		return append(fields, zap.Any(key, value))
	}
}

func fieldKey(groups []string, key string) string {
	if len(groups) == 0 {
		return key
	}
	parts := make([]string, 0, len(groups)+1)
	parts = append(parts, groups...)
	if key != "" {
		parts = append(parts, key)
	}
	return strings.Join(parts, ".")
}

var _ slog.Handler = (*slogHandler)(nil)
