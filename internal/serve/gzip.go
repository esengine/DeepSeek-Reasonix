package serve

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

const gzipThreshold = 1024

var gzipWriterPool = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

// gzipMiddleware compresses sufficiently large responses for remote clients.
// SSE and HEAD bypass it; streaming handlers must retain their flush contract.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead || r.URL.Path == "/events" || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipBufferedWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

func acceptsGzip(header string) bool {
	gzipQ, wildcardQ := -1.0, -1.0
	for part := range strings.SplitSeq(header, ",") {
		codingPart, parameters, _ := strings.Cut(part, ";")
		coding := strings.ToLower(strings.TrimSpace(codingPart))
		q := 1.0
		for parameter := range strings.SplitSeq(parameters, ";") {
			name, value, ok := strings.Cut(parameter, "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(name), "q") {
				continue
			}
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || parsed < 0 || parsed > 1 {
				q = 0
			} else {
				q = parsed
			}
		}
		switch coding {
		case "gzip":
			gzipQ = q
		case "*":
			wildcardQ = q
		}
	}
	if gzipQ >= 0 {
		return gzipQ > 0
	}
	return wildcardQ > 0
}

type gzipBufferedWriter struct {
	http.ResponseWriter
	buf     bytes.Buffer
	gz      *gzip.Writer
	started bool
	plain   bool
	status  int
}

func (g *gzipBufferedWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipBufferedWriter) WriteHeader(code int) {
	if g.started || g.status != 0 {
		return
	}
	g.status = code
}

func (g *gzipBufferedWriter) Write(p []byte) (int, error) {
	if g.gz != nil {
		return g.gz.Write(p)
	}
	if g.plain {
		return g.ResponseWriter.Write(p)
	}
	if g.status == 0 {
		g.status = http.StatusOK
	}
	if responseHasNoBody(g.status) {
		g.startPlain()
		return len(p), nil
	}
	_, _ = g.buf.Write(p)
	if g.buf.Len() >= gzipThreshold {
		g.start()
	}
	return len(p), nil
}

func (g *gzipBufferedWriter) start() {
	if g.started {
		return
	}
	if g.status == 0 {
		g.status = http.StatusOK
	}
	if responseHasNoBody(g.status) || g.Header().Get("Content-Encoding") != "" {
		g.startPlain()
		return
	}
	h := g.Header()
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	h.Del("Content-Length")
	g.ResponseWriter.WriteHeader(g.status)
	g.started = true
	g.gz = gzipWriterPool.Get().(*gzip.Writer)
	g.gz.Reset(g.ResponseWriter)
	_, _ = g.buf.WriteTo(g.gz)
}

func (g *gzipBufferedWriter) startPlain() {
	if g.started {
		return
	}
	if g.status == 0 {
		g.status = http.StatusOK
	}
	g.ResponseWriter.WriteHeader(g.status)
	g.started = true
	g.plain = true
	if !responseHasNoBody(g.status) {
		_, _ = g.buf.WriteTo(g.ResponseWriter)
	} else {
		g.buf.Reset()
	}
}

func (g *gzipBufferedWriter) Flush() {
	if !g.started {
		g.start()
	}
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if flusher, ok := g.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (g *gzipBufferedWriter) close() {
	if g.gz != nil {
		_ = g.gz.Close()
		gzipWriterPool.Put(g.gz)
		g.gz = nil
		return
	}
	if !g.started {
		g.startPlain()
	}
}

func responseHasNoBody(status int) bool {
	return status >= 100 && status < 200 || status == http.StatusNoContent || status == http.StatusNotModified
}
