package httpmiddleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"server-monitor/pkg/logger"
)

const RequestIDHeader = "X-Request-ID"

type requestIDContextKey struct{}

var requestIDCounter uint64

type RecoveryFields func(*http.Request) []zap.Field
type RequestPreparer func(http.ResponseWriter, *http.Request, time.Time) *http.Request
type RequestLogFields func(*http.Request, int, time.Duration) []zap.Field

type StatusRecorder struct {
	http.ResponseWriter
	status int
}

func NewStatusRecorder(w http.ResponseWriter) *StatusRecorder {
	return &StatusRecorder{ResponseWriter: w}
}

func (r *StatusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *StatusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(body)
}

func (r *StatusRecorder) Status() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

func Recovery(next http.Handler, fields RecoveryFields) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logFields := []zap.Field{zap.Any("error", recovered)}
				if fields != nil {
					logFields = append(fields(r), logFields...)
				}
				logger.FromContext(r.Context()).Error("http request panic recovered", logFields...)
				WriteErrorJSON(w, http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func Logging(next http.Handler, prepare RequestPreparer, fields RequestLogFields) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := NewStatusRecorder(w)
		if prepare != nil {
			r = prepare(recorder, r, start)
		}

		next.ServeHTTP(recorder, r)

		latency := time.Since(start)
		logFields := []zap.Field{
			zap.Int("status", recorder.Status()),
			zap.Float64("latency_ms", float64(latency.Microseconds())/1000),
		}
		if fields != nil {
			logFields = append(fields(r, recorder.Status(), latency), logFields...)
		}
		logger.FromContext(r.Context()).Info("http request completed", logFields...)
	})
}

func RequestMetadataFields(r *http.Request, requestID string) []zap.Field {
	fields := []zap.Field{
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("client_ip", r.RemoteAddr),
	}
	if requestID != "" {
		fields = append([]zap.Field{zap.String("request_id", requestID)}, fields...)
	}
	return fields
}

func WriteErrorJSON(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "error",
		"error":  http.StatusText(status),
	})
}

func EnsureRequestID(w http.ResponseWriter, r *http.Request, now time.Time) (*http.Request, string) {
	requestID := r.Header.Get(RequestIDHeader)
	if requestID == "" {
		requestID = NewRequestID(now)
	}
	w.Header().Set(RequestIDHeader, requestID)
	return r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, requestID)), requestID
}

func NewRequestID(now time.Time) string {
	seq := atomic.AddUint64(&requestIDCounter, 1)
	return strconv.FormatInt(now.UnixNano(), 36) + "-" + strconv.FormatUint(seq, 36)
}

func RequestIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if value, ok := r.Context().Value(requestIDContextKey{}).(string); ok {
		return value
	}
	return r.Header.Get(RequestIDHeader)
}
