package serve

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"

	"reasonix/internal/session/control"
)

type inProcessKey struct{}

// fromInProcess reports a request that arrived through InProcessClient. Only
// that transport sets the mark, and no request read off a socket passes
// through it, so a network client cannot claim it with anything it sends.
func fromInProcess(r *http.Request) bool {
	v, _ := r.Context().Value(inProcessKey{}).(bool)
	return v
}

// refuseNetworkShell answers a `!` command that arrived off a socket. The user
// typing a shell command runs it unconfined, which is a grant only a frontend
// in the kernel's own process holds.
func refuseNetworkShell(w http.ResponseWriter, r *http.Request, trimmed string) bool {
	if !strings.HasPrefix(trimmed, "!") || fromInProcess(r) {
		return false
	}
	refuse(w, http.StatusForbidden, "shell.unavailable_over_http", "shell commands are unavailable over HTTP", nil)
	return true
}

// submitOrShell runs a `!` command the transport admitted, and submits
// anything else as a turn.
func submitOrShell(ctrl control.SessionAPI, r *http.Request, input, format string, refuseUnknownSlash, localShell bool) {
	if cmd, ok := strings.CutPrefix(strings.TrimSpace(input), "!"); ok && fromInProcess(r) {
		if localShell {
			ctrl.RunShellWith(cmd, control.ShellRun{LocalOnly: true})
			return
		}
		ctrl.RunShell(cmd)
		return
	}
	submitAs(ctrl, r, input, format, refuseUnknownSlash)
}

// InProcessClient reaches this hub's routes without a listener, for a
// frontend running in the same process as the kernel: no port is opened, so
// there is nothing on the machine or the network to authenticate against.
// Responses stream as the handler flushes them, so /events works through it.
func (h *Hub) InProcessClient() *http.Client {
	return &http.Client{Transport: &handlerTransport{handler: h.Handler()}}
}

type handlerTransport struct{ handler http.Handler }

func (t *handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(context.WithValue(req.Context(), inProcessKey{}, true))
	if req.RequestURI == "" {
		req.RequestURI = req.URL.RequestURI()
	}
	if req.Body == nil {
		req.Body = http.NoBody
	}
	pr, pw := io.Pipe()
	w := &pipeResponseWriter{header: http.Header{}, pipe: pw, ready: make(chan struct{})}
	go func() {
		defer func() {
			if v := recover(); v != nil {
				w.commit()
				_ = pw.CloseWithError(http.ErrAbortHandler)
				return
			}
			w.commit()
			_ = pw.Close()
		}()
		t.handler.ServeHTTP(w, req)
	}()
	select {
	case <-w.ready:
	case <-req.Context().Done():
		_ = pr.CloseWithError(req.Context().Err())
		return nil, req.Context().Err()
	}
	return &http.Response{
		Status:        http.StatusText(w.status),
		StatusCode:    w.status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        w.snapshot,
		Body:          pr,
		ContentLength: -1,
		Request:       req,
	}, nil
}

// pipeResponseWriter hands the response to RoundTrip at the first write,
// flush or return, and streams the body through the pipe after that.
type pipeResponseWriter struct {
	header   http.Header
	snapshot http.Header
	pipe     *io.PipeWriter
	status   int
	once     sync.Once
	ready    chan struct{}
}

func (w *pipeResponseWriter) Header() http.Header { return w.header }

func (w *pipeResponseWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}

func (w *pipeResponseWriter) commit() {
	w.once.Do(func() {
		if w.status == 0 {
			w.status = http.StatusOK
		}
		w.snapshot = w.header.Clone()
		close(w.ready)
	})
}

func (w *pipeResponseWriter) Write(p []byte) (int, error) {
	w.commit()
	return w.pipe.Write(p)
}

func (w *pipeResponseWriter) Flush() { w.commit() }
