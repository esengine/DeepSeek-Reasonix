package serve

import "net/http"

func (s *Server) registerMachineSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /account", s.account)
	mux.HandleFunc("GET /usage", s.usage)
	s.registerThemeRoutes(mux)
	s.registerAppearanceRoutes(mux)
	s.registerStorageRoutes(mux)
	s.registerFeedbackRoutes(mux)
	s.registerDisplayCurrencyRoutes(mux)
	s.registerFoldWorkRoutes(mux)
	s.registerTurnNarrationRoutes(mux)
	s.registerWebAccessRoutes(mux)
	s.registerReceiptRoutes(mux)
	s.registerBackupFileRoutes(mux)
	mux.HandleFunc("GET /models", s.models)
	mux.HandleFunc("GET /memory", s.memories)
	mux.HandleFunc("POST /memory/forget", s.forgetMemory)
	mux.HandleFunc("POST /memory/save", s.saveMemory)
	mux.HandleFunc("GET /memory/revisions", s.memoryRevisions)
	mux.HandleFunc("POST /memory/restore", s.restoreMemory)
	mux.HandleFunc("GET /network", s.network)
	mux.HandleFunc("POST /network", s.saveNetwork)
	mux.HandleFunc("POST /network/diagnose", s.diagnoseNetwork)
	mux.HandleFunc("POST /default-model", s.setDefaultModel)
}

func (h *Hub) localWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	http.StripPrefix("/host/settings/runtime/"+r.PathValue("runtime"), http.HandlerFunc(h.machineSettings)).ServeHTTP(w, r)
}

// A machine request resolves a live local owner each time; runtime IDs are
// process-local and a remote pane never owns this machine's settings.
func (h *Hub) machineSettings(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	var local *Server
	ids := h.order
	if id := r.PathValue("runtime"); id != "" {
		ids = []string{id}
	}
	for _, id := range ids {
		if rt := h.runtimes[id]; rt != nil && rt.remote == nil && rt.Server != nil {
			local = rt.Server
			break
		}
	}
	h.mu.RUnlock()
	if local == nil {
		refuse(w, http.StatusServiceUnavailable, "settings.local_runtime_required", "open a local session to read this machine's settings", nil)
		return
	}
	mux := http.NewServeMux()
	local.registerMachineSettingsRoutes(mux)
	mux.ServeHTTP(w, r)
}
