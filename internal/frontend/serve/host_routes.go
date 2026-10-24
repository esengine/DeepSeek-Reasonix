package serve

import (
	"net/http"
	"path"
)

type hostRoute struct{ method, path string }

// hostOnlyRoutes is the one list of routes a paired device may not use: they send
// data off this machine, destroy it, or act for the account the window is signed in
// as. Each handler reads it, and so does the proxy that forwards a remote pane's
// requests with the host's own token. A HEAD request is judged as the GET it stands for.
var hostOnlyRoutes = []hostRoute{
	{http.MethodGet, "/git/push"},
	{http.MethodPost, "/git/push"},
	{http.MethodPost, "/changes/discard"},
	{http.MethodPost, "/isolated-results/discard-preview"},
	{http.MethodPost, "/isolated-results/discard"},
	{http.MethodPost, "/isolated-results/recover"},
	{http.MethodPost, "/isolated-results/command"},
	{http.MethodPost, "/isolated-results/root"},
	{http.MethodGet, "/feedback/board/me"},
	{http.MethodPost, "/feedback/board/join"},
	{http.MethodPost, "/feedback/board/leave"},
}

// GET /history/tool/{id} is not listed on purpose: it is a window onto what GET /history
// carries, so a device that may read one may read the other; a test holds them together.
func hostOnlyRoute(method, p string) bool {
	if method == http.MethodHead {
		method = http.MethodGet
	}
	p = path.Clean("/" + p)
	for _, r := range hostOnlyRoutes {
		if r.method == method && r.path == p {
			return true
		}
	}
	return false
}

// refuseDevice answers a paired device that reached a host-only route, before the
// request is read or anything else starts.
func refuseDevice(w http.ResponseWriter, r *http.Request) bool {
	if hostReach(r) || !hostOnlyRoute(r.Method, r.URL.Path) {
		return false
	}
	refuse(w, http.StatusNotFound, codeDeviceHostOnly, "a paired device cannot use this", nil)
	return true
}

// refuseDeviceBeforeForwarding is refuseDevice in front of a remote pane's proxy.
func refuseDeviceBeforeForwarding(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if refuseDevice(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}
