package serve

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"reasonix/internal/platform/remotecloud"
)

const (
	cloudDesktopRequestLimit  = 48 << 10
	cloudDesktopResponseLimit = 8 << 20
)

var ErrCloudDesktopRequest = errors.New("remote desktop request is not allowed")

type cloudDesktopRecorder struct {
	header   http.Header
	body     bytes.Buffer
	status   int
	tooLarge bool
}

func (r *cloudDesktopRecorder) Header() http.Header {
	return r.header
}

func (r *cloudDesktopRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

func (r *cloudDesktopRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.body.Len()+len(p) > cloudDesktopResponseLimit {
		r.tooLarge = true
		return 0, errors.New("remote desktop response is too large")
	}
	return r.body.Write(p)
}

func (h *Hub) CloudDesktop(ctx context.Context, input remotecloud.DesktopRequest, deviceID string) (remotecloud.DesktopResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	if !cloudDesktopMethod(method) || len(input.Body) > cloudDesktopRequestLimit {
		return remotecloud.DesktopResponse{}, ErrCloudDesktopRequest
	}
	target, err := url.ParseRequestURI(input.Path)
	if err != nil || target.IsAbs() || target.Host != "" || !strings.HasPrefix(target.Path, "/") || strings.HasPrefix(target.Path, "//") {
		return remotecloud.DesktopResponse{}, ErrCloudDesktopRequest
	}
	if cloudDesktopLiveEvents(target.Path) {
		return remotecloud.DesktopResponse{}, ErrCloudDesktopRequest
	}
	req := httptest.NewRequestWithContext(ctx, method, target.RequestURI(), bytes.NewReader(input.Body))
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	req = req.WithContext(withDeviceReach(req.Context(), deviceID, input.Ordinal))
	recorder := &cloudDesktopRecorder{header: make(http.Header)}
	h.Handler().ServeHTTP(recorder, req)
	if recorder.tooLarge {
		return remotecloud.DesktopResponse{}, errors.New("remote desktop response is too large")
	}
	status := recorder.status
	if status == 0 {
		status = http.StatusOK
	}
	return remotecloud.DesktopResponse{
		Status: status, ContentType: recorder.Header().Get("Content-Type"),
		ETag: recorder.Header().Get("ETag"), Body: append([]byte(nil), recorder.body.Bytes()...),
	}, nil
}

func cloudDesktopMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func cloudDesktopLiveEvents(path string) bool {
	if path == "/events" {
		return true
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 3 && parts[0] == "rt" && parts[2] == "events"
}
