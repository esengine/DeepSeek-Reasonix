package serve

import (
	"net/http"
	"path"
	"strings"
)

type hostRoute struct{ method, path string }

// hostOnlyRoutes is the one list of routes a paired device may not use: they send
// data off this machine, destroy it, or act for the account the window is signed in
// as. Each handler reads it, and so does the proxy that forwards a remote pane's
// requests with the host's own token. A HEAD request is judged as the GET it stands for.
// The routes from /plugins on install or enable code the session then runs.
var hostOnlyRoutes = []hostRoute{
	{http.MethodGet, "/git/push"},
	{http.MethodPost, "/git/push"},
	{http.MethodPost, "/changes/discard"},
	{http.MethodGet, "/feedback/board/me"},
	{http.MethodPost, "/feedback/board/join"},
	{http.MethodPost, "/feedback/board/leave"},
	{http.MethodPost, "/plugins/install"},
	{http.MethodPost, "/plugins/enabled"},
	{http.MethodDelete, "/plugins/{name}"},
	{http.MethodPost, "/mcp/install"},
	{http.MethodPost, "/mcp/trust"},
	{http.MethodPost, "/mcp/enabled"},
	{http.MethodPost, "/mcp/load"},
	{http.MethodPost, "/mcp/follow"},
	{http.MethodPost, "/mcp/upgrade"},
	{http.MethodPost, "/hooks"},
	{http.MethodPost, "/market/install"},
	{http.MethodPost, "/market/mine/install"},
	{http.MethodPost, "/workspace-trust"},
	{http.MethodPost, "/team/bootstrap"},
	{http.MethodPost, "/team/agents"},
	{http.MethodPatch, "/team/agents/{id}"},
	{http.MethodDelete, "/team/agents/{id}"},
	{http.MethodPost, "/team/groups"},
	{http.MethodPatch, "/team/groups/{id}"},
	{http.MethodDelete, "/team/groups/{id}"},
	{http.MethodPatch, "/team/prefs"},
	{http.MethodPost, "/team/schedules"},
	{http.MethodPatch, "/team/schedules/{id}"},
	{http.MethodDelete, "/team/schedules/{id}"},
}

// GET /history/tool/{id} is not listed on purpose: it is a window onto what GET /history
// carries, so a device that may read one may read the other; a test holds them together.
func hostOnlyRoute(method, p string) bool {
	if method == http.MethodHead {
		method = http.MethodGet
	}
	p = path.Clean("/" + p)
	if rest, ok := strings.CutPrefix(p, runtimePrefix); ok {
		if _, tail, found := strings.Cut(rest, "/"); found {
			p = "/" + tail
		}
	}
	for _, r := range hostOnlyRoutes {
		if r.method == method && routeMatches(r.path, p) {
			return true
		}
	}
	return false
}

// routeMatches compares segment by segment; a {name} segment matches any one.
func routeMatches(pattern, p string) bool {
	want, got := strings.Split(pattern, "/"), strings.Split(p, "/")
	if len(want) != len(got) {
		return false
	}
	for i, w := range want {
		if w != got[i] && !(strings.HasPrefix(w, "{") && got[i] != "") {
			return false
		}
	}
	return true
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
