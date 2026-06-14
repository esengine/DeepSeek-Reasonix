package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	daemonapi "reasonix/internal/daemon"
	"reasonix/internal/event"
)

type DaemonStatusView struct {
	Connected bool   `json:"connected"`
	Status    string `json:"status,omitempty"`
	Addr      string `json:"addr,omitempty"`
	Sessions  int    `json:"sessions,omitempty"`
	Uptime    string `json:"uptime,omitempty"`
	PID       int    `json:"pid,omitempty"`
	Error     string `json:"error,omitempty"`
}

type DaemonSessionView struct {
	ID                  string     `json:"id"`
	Path                string     `json:"path"`
	GoalText            string     `json:"goalText,omitempty"`
	GoalStatus          string     `json:"goalStatus,omitempty"`
	RunStatus           string     `json:"runStatus,omitempty"`
	WaitKind            string     `json:"waitKind,omitempty"`
	WaitReason          string     `json:"waitReason,omitempty"`
	WaitID              string     `json:"waitId,omitempty"`
	WaitTool            string     `json:"waitTool,omitempty"`
	WaitSubject         string     `json:"waitSubject,omitempty"`
	Active              bool       `json:"active,omitempty"`
	Open                bool       `json:"open,omitempty"`
	Scope               string     `json:"scope,omitempty"`
	Workspace           string     `json:"workspaceRoot,omitempty"`
	TopicID             string     `json:"topicId,omitempty"`
	TopicTitle          string     `json:"topicTitle,omitempty"`
	NextWakeupAt        *time.Time `json:"nextWakeupAt,omitempty"`
	DailyWakeupLimit    int        `json:"dailyWakeupLimit,omitempty"`
	DailyWakeups        int        `json:"dailyWakeups,omitempty"`
	MaxGoalAutoTurns    int        `json:"maxGoalAutoTurns,omitempty"`
	DailyModelCallLimit int        `json:"dailyModelCallLimit,omitempty"`
	DailyModelCalls     int        `json:"dailyModelCalls,omitempty"`
	DailyModelCostLimit float64    `json:"dailyModelCostLimit,omitempty"`
	DailyModelCost      float64    `json:"dailyModelCost,omitempty"`
	ModelCostCurrency   string     `json:"modelCostCurrency,omitempty"`
	BudgetBlockedReason string     `json:"budgetBlockedReason,omitempty"`
	Scheduled           bool       `json:"scheduled,omitempty"`
	Watched             bool       `json:"watched,omitempty"`
}

type DaemonApprovalDeskItemView struct {
	SessionID  string                       `json:"sessionId"`
	Kind       string                       `json:"kind"`
	ID         string                       `json:"id,omitempty"`
	Tool       string                       `json:"tool,omitempty"`
	Subject    string                       `json:"subject,omitempty"`
	Reason     string                       `json:"reason,omitempty"`
	GoalText   string                       `json:"goalText,omitempty"`
	GoalStatus string                       `json:"goalStatus,omitempty"`
	RunStatus  string                       `json:"runStatus,omitempty"`
	Active     bool                         `json:"active,omitempty"`
	Since      time.Time                    `json:"since,omitempty"`
	Questions  []DaemonApprovalQuestionView `json:"questions,omitempty"`
}

type DaemonApprovalQuestionView struct {
	ID      string                     `json:"id,omitempty"`
	Header  string                     `json:"header,omitempty"`
	Prompt  string                     `json:"prompt,omitempty"`
	Options []DaemonApprovalOptionView `json:"options,omitempty"`
	Multi   bool                       `json:"multi,omitempty"`
}

type DaemonApprovalOptionView struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// DaemonStatus reports whether the local daemon API is reachable. It returns an
// error field instead of failing so the frontend can render offline state.
func (a *App) DaemonStatus(addr string) DaemonStatusView {
	var resp daemonapi.StatusResponse
	if err := a.daemonJSON("GET", addr, "/status", nil, &resp); err != nil {
		return DaemonStatusView{Connected: false, Addr: normalizeDaemonAddr(addr), Error: err.Error()}
	}
	return DaemonStatusView{
		Connected: true,
		Status:    resp.Status,
		Addr:      resp.Addr,
		Sessions:  resp.Sessions,
		Uptime:    resp.Uptime,
		PID:       resp.PID,
	}
}

func (a *App) ListDaemonSessions(addr string) ([]DaemonSessionView, error) {
	sessions, err := a.fetchDaemonSessions(addr)
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

func (a *App) ListDaemonApprovals(addr string) ([]DaemonApprovalDeskItemView, error) {
	var resp daemonapi.ApprovalDeskResponse
	if err := a.daemonJSON("GET", addr, "/approvals", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]DaemonApprovalDeskItemView, 0, len(resp.Items))
	for _, item := range resp.Items {
		out = append(out, daemonApprovalDeskItemView(item))
	}
	return out, nil
}

func (a *App) OpenDaemonSession(sessionID, addr string) (TabMeta, error) {
	s, err := a.daemonSessionByID(addr, sessionID)
	if err != nil {
		return TabMeta{}, err
	}
	return a.openDaemonSessionPath(s.Path)
}

func (a *App) ContinueDaemonGoal(sessionID, addr string) error {
	body := map[string]string{"session_id": strings.TrimSpace(sessionID), "reason": "desktop"}
	return a.daemonJSON("POST", addr, "/continue-goal", body, nil)
}

func (a *App) StopDaemonSession(sessionID, addr string) error {
	body := map[string]string{"session_id": strings.TrimSpace(sessionID)}
	return a.daemonJSON("POST", addr, "/stop", body, nil)
}

func (a *App) DisableDaemonSchedule(sessionID, addr string) error {
	body := map[string]interface{}{"session_id": strings.TrimSpace(sessionID), "enabled": false}
	return a.daemonJSON("POST", addr, "/schedule", body, nil)
}

func (a *App) DisableDaemonWatch(sessionID, addr string) error {
	body := map[string]interface{}{"session_id": strings.TrimSpace(sessionID), "enabled": false}
	return a.daemonJSON("POST", addr, "/watch", body, nil)
}

func (a *App) ApproveDaemon(sessionID, approvalID string, allow, session, persist bool, addr string) error {
	path := "/approvals/deny"
	if allow {
		path = "/approvals/approve"
	}
	body := map[string]interface{}{
		"session_id":  strings.TrimSpace(sessionID),
		"approval_id": strings.TrimSpace(approvalID),
		"session":     session,
		"persist":     persist,
	}
	return a.daemonJSON("POST", addr, path, body, nil)
}

func (a *App) AnswerDaemonQuestion(sessionID, askID string, answers []QuestionAnswer, selected, addr string) error {
	out := make([]event.AskAnswer, len(answers))
	for i, an := range answers {
		out[i] = event.AskAnswer{QuestionID: an.QuestionID, Selected: an.Selected}
	}
	body := map[string]interface{}{
		"session_id": strings.TrimSpace(sessionID),
		"ask_id":     strings.TrimSpace(askID),
		"answers":    out,
	}
	if strings.TrimSpace(selected) != "" && len(out) == 0 {
		body["selected"] = strings.TrimSpace(selected)
	}
	return a.daemonJSON("POST", addr, "/asks/answer", body, nil)
}

func daemonApprovalDeskItemView(item daemonapi.ApprovalDeskItem) DaemonApprovalDeskItemView {
	questions := make([]DaemonApprovalQuestionView, 0, len(item.Questions))
	for _, q := range item.Questions {
		options := make([]DaemonApprovalOptionView, 0, len(q.Options))
		for _, opt := range q.Options {
			options = append(options, DaemonApprovalOptionView{Label: opt.Label, Description: opt.Description})
		}
		questions = append(questions, DaemonApprovalQuestionView{
			ID:      q.ID,
			Header:  q.Header,
			Prompt:  q.Prompt,
			Options: options,
			Multi:   q.Multi,
		})
	}
	return DaemonApprovalDeskItemView{
		SessionID:  item.SessionID,
		Kind:       item.Kind,
		ID:         item.ID,
		Tool:       item.Tool,
		Subject:    item.Subject,
		Reason:     item.Reason,
		GoalText:   item.GoalText,
		GoalStatus: item.GoalStatus,
		RunStatus:  item.RunStatus,
		Active:     item.Active,
		Since:      item.Since,
		Questions:  questions,
	}
}

func (a *App) fetchDaemonSessions(addr string) ([]DaemonSessionView, error) {
	var resp daemonapi.SessionsResponse
	if err := a.daemonJSON("GET", addr, "/sessions", nil, &resp); err != nil {
		return nil, err
	}
	open := a.openDaemonSessionPaths()
	out := make([]DaemonSessionView, 0, len(resp.Sessions))
	for _, s := range resp.Sessions {
		view := DaemonSessionView{
			ID:                  s.ID,
			Path:                s.Path,
			GoalText:            s.GoalText,
			GoalStatus:          s.GoalStatus,
			RunStatus:           s.RunStatus,
			WaitKind:            s.WaitKind,
			WaitReason:          s.WaitReason,
			WaitID:              s.WaitID,
			WaitTool:            s.WaitTool,
			WaitSubject:         s.WaitSubject,
			Active:              s.Active,
			Scope:               s.Scope,
			Workspace:           s.WorkspaceRoot,
			TopicID:             s.TopicID,
			TopicTitle:          s.TopicTitle,
			NextWakeupAt:        s.NextWakeupAt,
			DailyWakeupLimit:    s.DailyWakeupLimit,
			DailyWakeups:        s.DailyWakeups,
			MaxGoalAutoTurns:    s.MaxGoalAutoTurns,
			DailyModelCallLimit: s.DailyModelCallLimit,
			DailyModelCalls:     s.DailyModelCalls,
			DailyModelCostLimit: s.DailyModelCostLimit,
			DailyModelCost:      s.DailyModelCost,
			ModelCostCurrency:   s.ModelCostCurrency,
			BudgetBlockedReason: s.BudgetBlockedReason,
			Scheduled:           s.Scheduled,
			Watched:             s.Watched,
		}
		if path, err := filepath.Abs(s.Path); err == nil {
			_, view.Open = open[path]
		}
		if meta, ok, err := agent.LoadBranchMeta(s.Path); err == nil && ok {
			if view.Scope == "" {
				view.Scope = meta.DefaultScope()
			}
			if view.Workspace == "" {
				view.Workspace = meta.WorkspaceRoot
			}
			if view.TopicID == "" {
				view.TopicID = meta.TopicID
			}
			if view.TopicTitle == "" {
				view.TopicTitle = meta.TopicTitle
			}
		}
		out = append(out, view)
	}
	return out, nil
}

func (a *App) daemonSessionByID(addr, sessionID string) (DaemonSessionView, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return DaemonSessionView{}, fmt.Errorf("session_id required")
	}
	sessions, err := a.fetchDaemonSessions(addr)
	if err != nil {
		return DaemonSessionView{}, err
	}
	for _, s := range sessions {
		if s.ID == sessionID {
			return s, nil
		}
	}
	return DaemonSessionView{}, fmt.Errorf("daemon session not found: %s", sessionID)
}

func (a *App) openDaemonSessionPath(path string) (TabMeta, error) {
	sessionDir, sessionPath, err := a.sessionDirForPath(path)
	if err != nil {
		sessionDir, sessionPath, err = daemonSessionDirForPath(path)
		if err != nil {
			return TabMeta{}, err
		}
	}
	if existing := a.tabWithSessionPath(sessionPath); existing != nil {
		a.mu.Lock()
		a.activeTabID = existing.ID
		meta := a.tabMeta(existing, true)
		a.saveTabsLocked()
		a.mu.Unlock()
		return meta, nil
	}

	meta, ok, err := agent.LoadBranchMeta(sessionPath)
	if err != nil {
		return TabMeta{}, err
	}
	if !ok {
		meta = agent.BranchMeta{ID: agent.BranchID(sessionPath), Scope: "global", TopicID: legacySessionTopicID(sessionPath)}
	}
	scope := meta.DefaultScope()
	workspaceRoot := strings.TrimSpace(meta.WorkspaceRoot)
	topicID := strings.TrimSpace(meta.TopicID)
	if topicID == "" {
		topicID = legacySessionTopicID(sessionPath)
	}
	if topicID == "" {
		topicID = agent.BranchID(sessionPath)
	}
	if scope == "project" && workspaceRoot == "" {
		return TabMeta{}, fmt.Errorf("daemon session %s is project-scoped but has no workspace root", meta.ID)
	}
	if scope == "global" && workspaceRoot == "" {
		workspaceRoot = globalWorkspaceRoot()
	}

	if tab := a.tabForDaemonSession(scope, workspaceRoot, topicID, sessionDir); tab != nil {
		return a.resumeDaemonSessionOnTab(tab, sessionDir, sessionPath)
	}
	return a.openDaemonSessionTab(scope, workspaceRoot, topicID, meta.TopicTitle, sessionPath)
}

func daemonSessionDirForPath(path string) (string, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", fmt.Errorf("session path required")
	}
	if !filepath.IsAbs(path) {
		return "", "", fmt.Errorf("daemon session path must be absolute: %s", path)
	}
	sessionDir := filepath.Dir(path)
	sessionPath, _, err := validateSessionPath(sessionDir, path)
	if err != nil {
		return "", "", err
	}
	return sessionDir, sessionPath, nil
}

func (a *App) resumeDaemonSessionOnTab(tab *WorkspaceTab, sessionDir, sessionPath string) (TabMeta, error) {
	a.mu.Lock()
	a.activeTabID = tab.ID
	tab.SessionPath = sessionPath
	ctrl := tab.Ctrl
	tabID := tab.ID
	a.saveTabsLocked()
	a.mu.Unlock()
	if ctrl != nil {
		if ctrl.Running() {
			return TabMeta{}, fmt.Errorf("tab is running")
		}
		if _, _, err := validateSessionPath(sessionDir, sessionPath); err != nil {
			return TabMeta{}, err
		}
		loaded, err := agent.LoadSession(sessionPath)
		if err != nil {
			return TabMeta{}, err
		}
		_ = ctrl.Snapshot()
		ctrl.Resume(loaded, sessionPath)
		a.rememberTabSessionPath(tab, sessionPath)
	}
	a.emitProjectTreeChanged()
	return a.tabMetaByID(tabID), nil
}

func (a *App) openDaemonSessionTab(scope, workspaceRoot, topicID, topicTitle, sessionPath string) (TabMeta, error) {
	if scope == "project" {
		if abs, err := filepath.Abs(workspaceRoot); err == nil {
			workspaceRoot = abs
		}
		saveWorkspace(workspaceRoot)
		_ = addProject(workspaceRoot, "")
	} else {
		scope = "global"
		if workspaceRoot == "" {
			workspaceRoot = globalWorkspaceRoot()
		}
		if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
			return TabMeta{}, fmt.Errorf("create global workspace: %w", err)
		}
	}
	if strings.TrimSpace(topicTitle) == "" {
		topicTitle = topicTitleForTab(scope, workspaceRoot, topicID)
	}
	if err := ensureTopicIndexed(scope, workspaceRoot, topicID, topicTitle, topicTitleSourceManual); err != nil {
		return TabMeta{}, err
	}

	a.mu.Lock()
	tabID := a.newUniqueTabIDLocked()
	tab := &WorkspaceTab{
		ID:               tabID,
		Scope:            scope,
		WorkspaceRoot:    workspaceRoot,
		TopicID:          topicID,
		TopicTitle:       topicTitle,
		SessionPath:      sessionPath,
		tokenMode:        boot.TokenModeFull,
		mode:             "normal",
		toolApprovalMode: control.ToolApprovalAsk,
		disabledMCP:      map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tabID, app: a}
	a.tabs[tabID] = tab
	a.tabOrder = append(a.tabOrder, tabID)
	a.activeTabID = tabID
	a.saveTabsLocked()
	meta := a.tabMeta(tab, true)
	a.mu.Unlock()

	a.startTabControllerBuild(tab)
	a.emitProjectTreeChanged()
	return meta, nil
}

func (a *App) tabMetaByID(tabID string) TabMeta {
	a.mu.RLock()
	tab := a.tabs[tabID]
	if tab == nil {
		a.mu.RUnlock()
		return TabMeta{}
	}
	meta := a.tabMeta(tab, true)
	a.mu.RUnlock()
	return meta
}

func (a *App) tabWithSessionPath(path string) *WorkspaceTab {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.tabs {
		p := tab.currentSessionPath()
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if p == path {
			return tab
		}
	}
	return nil
}

func (a *App) tabForDaemonSession(scope, workspaceRoot, topicID, sessionDir string) *WorkspaceTab {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.tabs {
		if tab.Scope != scope || strings.TrimSpace(tab.TopicID) != topicID {
			continue
		}
		if scope == "project" && strings.TrimSpace(tab.WorkspaceRoot) != workspaceRoot {
			continue
		}
		if dir := tabSessionDir(tab); dir == sessionDir {
			return tab
		}
	}
	return nil
}

func (a *App) openDaemonSessionPaths() map[string]struct{} {
	out := map[string]struct{}{}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.tabs {
		path := strings.TrimSpace(tab.currentSessionPath())
		if path == "" {
			continue
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		out[path] = struct{}{}
	}
	return out
}

func (a *App) daemonJSON(method, addr, path string, body interface{}, out interface{}) error {
	base, err := daemonBaseURL(addr)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(body)
	if body == nil {
		payload = nil
		err = nil
	}
	if err != nil {
		return err
	}
	tokens := a.daemonAuthTokens()
	client := &http.Client{Timeout: 10 * time.Second}
	var unauthorized bool
	for _, token := range tokens {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequest(method, base+path, reader)
		if err != nil {
			return err
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("X-Reasonix-Daemon-Token", token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		b, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode == http.StatusUnauthorized {
			unauthorized = true
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return daemonHTTPError(resp.StatusCode, b)
		}
		if out != nil && len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, out); err != nil {
				return err
			}
		}
		return nil
	}
	if unauthorized {
		return fmt.Errorf("daemon unauthorized")
	}
	return fmt.Errorf("daemon request failed")
}

func daemonHTTPError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("daemon http %d: %s", status, msg)
}

func (a *App) daemonAuthTokens() []string {
	seen := map[string]bool{}
	out := []string{""}
	add := func(token string) {
		token = strings.TrimSpace(token)
		if token == "" || seen[token] {
			return
		}
		seen[token] = true
		out = append(out, token)
	}
	add(readDaemonToken(config.SessionDir()))
	for _, dir := range a.knownSessionDirs() {
		add(readDaemonToken(dir))
	}
	return out
}

func readDaemonToken(sessionDir string) string {
	if strings.TrimSpace(sessionDir) == "" {
		return ""
	}
	b, err := os.ReadFile(daemonapi.TokenFile(sessionDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func daemonBaseURL(addr string) (string, error) {
	addr = normalizeDaemonAddr(addr)
	u, err := url.Parse(addr)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" {
		u, err = url.Parse("http://" + addr)
		if err != nil {
			return "", err
		}
	}
	if u.Scheme != "http" {
		return "", fmt.Errorf("daemon address must use http")
	}
	if !isLoopbackHost(u.Hostname()) {
		return "", fmt.Errorf("daemon address must be localhost")
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func normalizeDaemonAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = daemonapi.DefaultAddr
	}
	return addr
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
