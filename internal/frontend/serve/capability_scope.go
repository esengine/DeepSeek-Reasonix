package serve

import (
	"net/http"
	"strings"

	"reasonix/internal/session/control"
)

func scopeView(ctl control.SessionAPI) control.CapabilityScope { return ctl.CapabilityScope() }

// resolveRoot is the project a capability request is about. Empty means the
// running one, so every existing caller keeps its meaning; a root the shell has
// never opened is refused rather than silently answered for, since the request
// would otherwise be a way to read or edit any directory's config.
func (s *Server) resolveRoot(asked string) (root string, other bool, ok bool) {
	asked = strings.TrimSpace(asked)
	current := s.ctl().WorkspaceRoot()
	if asked == "" || asked == current {
		return current, false, true
	}
	for _, scope := range s.capabilityScopes() {
		if scope.Root == asked {
			return asked, true, true
		}
	}
	return "", false, false
}

func (s *Server) requestedRoot(r *http.Request) (root string, other bool, ok bool) {
	return s.resolveRoot(r.URL.Query().Get("root"))
}

// capabilityScopes lists the projects a picker may switch between: the running
// one first, then the folders this frontend remembers driving.
func (s *Server) capabilityScopes() []control.CapabilityScope {
	return control.DescribeScopes(Workspaces(), s.ctl().WorkspaceRoot())
}

// capabilityScopeHandler answers the scope bar on its own, so a surface can
// name its project before either listing has loaded. With ?all it answers the
// whole picker, which is what lets one project be managed from another.
func (s *Server) capabilityScopeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Has("all") {
		writeJSONCached(w, r, map[string]any{"scopes": s.capabilityScopes()})
		return
	}
	writeJSONCached(w, r, scopeView(s.ctl()))
}
