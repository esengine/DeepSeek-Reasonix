package serve

import (
	"encoding/json"
	"net/http"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/desktopsidebar"
)

// registerDesktopAPIRoutes adds Electron/Wails-shared desktop metadata routes
// that do not require multi-tab (but are most useful with it).
func (s *Server) registerDesktopAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /desktop/project-tree", s.desktopProjectTree)
	mux.HandleFunc("POST /desktop/topics", s.desktopCreateTopic)
	mux.HandleFunc("POST /desktop/topics/rename", s.desktopRenameTopic)
	mux.HandleFunc("POST /desktop/topics/delete", s.desktopDeleteTopic)
	mux.HandleFunc("POST /desktop/topics/trash", s.desktopTrashTopic)
	mux.HandleFunc("POST /desktop/projects/remove", s.desktopRemoveProject)
	mux.HandleFunc("POST /desktop/projects/rename", s.desktopRenameProject)
	mux.HandleFunc("POST /desktop/projects/reorder", s.desktopReorderProjects)
	mux.HandleFunc("GET /desktop/startup-settings", s.desktopStartupSettings)
	mux.HandleFunc("GET /desktop/settings", s.desktopSettings)
}

func (s *Server) desktopProjectTree(w http.ResponseWriter, _ *http.Request) {
	openHints := make([]desktopsidebar.SessionHint, 0)
	if h := s.tabHost(); h != nil {
		for _, t := range h.ListTabs() {
			openHints = append(openHints, desktopsidebar.SessionHint{
				Path:          t.SessionPath,
				WorkspaceRoot: t.WorkspaceRoot,
				TopicID:       t.TopicID,
				TopicTitle:    t.TopicTitle,
				Running:       t.Running,
			})
		}
	}
	sessions := s.sessionHints()
	writeJSON(w, desktopsidebar.BuildTree(openHints, sessions))
}

func (s *Server) sessionHints() []desktopsidebar.SessionHint {
	// Prefer multi-tab session dirs via active controller SessionDir, then ListSessions.
	dir := ""
	if c := s.ctl(); c != nil {
		dir = c.SessionDir()
	}
	if dir == "" {
		return nil
	}
	infos, err := agent.ListSessions(dir)
	if err != nil {
		return nil
	}
	out := make([]desktopsidebar.SessionHint, 0, len(infos))
	for _, info := range infos {
		out = append(out, desktopsidebar.SessionHint{
			Path:           info.Path,
			WorkspaceRoot:  info.WorkspaceRoot,
			TopicID:        info.TopicID,
			TopicTitle:     firstNonEmpty(info.TopicTitle, info.CustomTitle),
			Turns:          info.Turns,
			LastActivityAt: info.LastActivityAt.UnixMilli(),
		})
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (s *Server) desktopCreateTopic(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope         string `json:"scope"`
		WorkspaceRoot string `json:"workspaceRoot"`
		Title         string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	meta, err := desktopsidebar.CreateTopic(body.Scope, body.WorkspaceRoot, body.Title)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, meta)
}

func (s *Server) desktopRenameTopic(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkspaceRoot string `json:"workspaceRoot"`
		TopicID       string `json:"topicId"`
		Title         string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := desktopsidebar.RenameTopic(body.WorkspaceRoot, body.TopicID, body.Title); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) desktopDeleteTopic(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkspaceRoot string `json:"workspaceRoot"`
		TopicID       string `json:"topicId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := desktopsidebar.DeleteTopic(body.WorkspaceRoot, body.TopicID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) desktopTrashTopic(w http.ResponseWriter, r *http.Request) {
	s.desktopDeleteTopic(w, r)
}

func (s *Server) desktopRemoveProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkspaceRoot string `json:"workspaceRoot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := desktopsidebar.RemoveWorkspace(body.WorkspaceRoot); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) desktopRenameProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkspaceRoot string `json:"workspaceRoot"`
		Title         string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := desktopsidebar.RenameProject(body.WorkspaceRoot, body.Title); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) desktopReorderProjects(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkspaceRoots []string `json:"workspaceRoots"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := desktopsidebar.ReorderProjects(body.WorkspaceRoots); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) desktopStartupSettings(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		cfg = &config.Config{}
	}
	writeJSON(w, map[string]any{
		"bot": map[string]any{
			"enabled":            false,
			"model":              "",
			"toolApprovalMode":   "ask",
			"maxSteps":           0,
			"debounceMs":         0,
			"queueMode":          "",
			"queueCap":           0,
			"queueDrop":          "",
			"ignoreSelfMessages": true,
			"selfUserIds":        map[string]any{"qq": []any{}, "feishu": []any{}, "weixin": []any{}},
			"control":            map[string]any{"enabled": false, "addr": "", "tokenEnv": ""},
			"pairing":            map[string]any{"enabled": false, "requestTtlMinutes": 60, "maxPendingPerPlatform": 3},
			"routes":             []any{},
			"allowlist": map[string]any{
				"enabled": false, "allowAll": false,
				"qqUsers": []any{}, "feishuUsers": []any{}, "weixinUsers": []any{},
				"qqApprovers": []any{}, "feishuApprovers": []any{}, "weixinApprovers": []any{},
				"qqAdmins": []any{}, "feishuAdmins": []any{}, "weixinAdmins": []any{},
				"qqGroups": []any{}, "feishuGroups": []any{}, "weixinGroups": []any{},
			},
			"qq": map[string]any{
				"enabled": false, "appId": "", "appSecretEnv": "", "secretSet": false, "sandbox": false,
				"model": "", "toolApprovalMode": "ask", "workspaceRoot": "",
				"access": map[string]any{"enabled": false, "allowAll": false, "pairingEnabled": false, "users": []any{}, "groups": []any{}, "approvers": []any{}, "admins": []any{}},
			},
			"feishu": map[string]any{
				"enabled": false, "domain": "feishu", "appId": "", "appSecretEnv": "", "secretSet": false,
				"verificationToken": "", "mode": "webhook", "webhookPort": 8080, "requireMention": true,
			},
			"weixin": map[string]any{"enabled": false, "accountId": "", "tokenEnv": "", "tokenSet": false, "apiBase": ""},
			"connections": []any{},
		},
		"desktopLanguage":      cfg.DesktopLanguage(),
		"desktopLayoutStyle":   cfg.DesktopLayoutStyle(),
		"desktopTheme":         cfg.DesktopTheme(),
		"desktopThemeStyle":    cfg.DesktopThemeStyle(),
		"desktopTerminalTheme": cfg.DesktopTerminalTheme(),
		"displayMode":          "standard",
		"statusBarStyle":       "icon",
		"statusBarItems": []string{
			"model", "workspace", "git_branch", "cache", "cache_avg", "session_tokens",
			"turn_tokens", "turn_cost", "session_turns", "context", "compact", "cost", "balance",
		},
		"checkUpdates":       false,
		"updateChannel":      "stable",
		"conversationWidth":  "standard",
		"configWarnings":     []any{},
		"configPath":         config.UserConfigPath(),
	})
}

func (s *Server) desktopSettings(w http.ResponseWriter, _ *http.Request) {
	// Lightweight settings surface for Electron: providers/models from /models shape
	// plus desktop prefs. Full Wails Settings write path remains desktop-owned.
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		cfg = &config.Config{}
	}
	models := []any{}
	// Callers that need the live model list should still hit GET /models.
	writeJSON(w, map[string]any{
		"defaultModel":         cfg.DefaultModel,
		"plannerModel":         "",
		"subagentModel":        "",
		"subagentEffort":       "",
		"autoPlan":             "off",
		"providers":            models,
		"officialProviders":    models,
		"providerPresets":      []any{},
		"permissions":          map[string]any{"mode": "ask", "allow": []any{}, "ask": []any{}, "deny": []any{}},
		"sandbox":              map[string]any{"bash": "workspace", "network": true, "workspaceRoot": "", "allowWrite": []any{}, "effectiveWorkspaceRoot": "", "effectiveWriteRoots": []any{}, "shell": "auto", "effectiveShell": ""},
		"network":              map[string]any{"proxyMode": "auto", "proxyUrl": "", "noProxy": "", "proxy": map[string]any{"type": "", "server": "", "port": 0, "username": "", "password": ""}},
		"agent":                map[string]any{"temperature": 0, "maxSteps": 0, "plannerMaxSteps": 0, "maxSubagentDepth": 2, "maxSubagentConcurrency": 1, "maxParallelWriters": 1, "systemPrompt": "", "coldResumePrune": false, "reasoningLanguage": "auto", "compactRatio": 0.8},
		"bot":                  map[string]any{"enabled": false, "connections": []any{}, "routes": []any{}, "allowlist": map[string]any{"enabled": false, "allowAll": false, "qqUsers": []any{}, "feishuUsers": []any{}, "weixinUsers": []any{}, "qqApprovers": []any{}, "feishuApprovers": []any{}, "weixinApprovers": []any{}, "qqAdmins": []any{}, "feishuAdmins": []any{}, "weixinAdmins": []any{}, "qqGroups": []any{}, "feishuGroups": []any{}, "weixinGroups": []any{}}, "selfUserIds": map[string]any{"qq": []any{}, "feishu": []any{}, "weixin": []any{}}, "control": map[string]any{"enabled": false, "addr": "", "tokenEnv": ""}, "pairing": map[string]any{"enabled": false, "requestTtlMinutes": 60, "maxPendingPerPlatform": 3}, "qq": map[string]any{"enabled": false, "appId": "", "appSecretEnv": "", "secretSet": false, "sandbox": false, "model": "", "toolApprovalMode": "ask", "workspaceRoot": "", "access": map[string]any{"enabled": false, "allowAll": false, "pairingEnabled": false, "users": []any{}, "groups": []any{}, "approvers": []any{}, "admins": []any{}}}, "feishu": map[string]any{"enabled": false, "domain": "feishu", "appId": "", "appSecretEnv": "", "secretSet": false, "verificationToken": "", "mode": "webhook", "webhookPort": 8080, "requireMention": true}, "weixin": map[string]any{"enabled": false, "accountId": "", "tokenEnv": "", "tokenSet": false, "apiBase": ""}},
		"desktopLanguage":      cfg.DesktopLanguage(),
		"desktopLayoutStyle":   cfg.DesktopLayoutStyle(),
		"desktopTheme":         cfg.DesktopTheme(),
		"desktopThemeStyle":    cfg.DesktopThemeStyle(),
		"desktopTerminalTheme": cfg.DesktopTerminalTheme(),
		"closeBehavior":        "background",
		"displayMode":          "standard",
		"statusBarStyle":       "icon",
		"statusBarItems": []string{
			"model", "workspace", "git_branch", "cache", "cache_avg", "session_tokens",
			"turn_tokens", "turn_cost", "session_turns", "context", "compact", "cost", "balance",
		},
		"defaultToolApprovalMode": "ask",
		"checkUpdates":            false,
		"updateChannel":           "stable",
		"telemetry":               false,
		"metrics":                 false,
		"configPath":              config.UserConfigPath(),
		"providerKinds":           []any{},
		"autoApproveTools":        false,
		"bypass":                  false,
		"conversationWidth":       "standard",
	})
}
