package serve

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

type requestClassKey struct{}

// requestClass is filled in by the route that serves a request, which is the
// only place that knows what kind of request it is.
type requestClass struct{ routine bool }

// routine declares a route a client calls on a timer or per frame. A request
// it serves that succeeds is logged at Debug; a failed one keeps its INFO line.
func routine(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, ok := r.Context().Value(requestClassKey{}).(*requestClass); ok {
			c.routine = true
		}
		h(w, r)
	}
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		class := &requestClass{}
		r = r.WithContext(context.WithValue(r.Context(), requestClassKey{}, class))
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		level := slog.LevelInfo
		if class.routine && rw.status < http.StatusBadRequest {
			level = slog.LevelDebug
		}
		slog.Log(r.Context(), level, "serve: request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration", time.Since(start).String(),
		)
	})
}

// responseWriter captures the status code for logging.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush keeps SSE working: the events handler asserts http.Flusher.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
