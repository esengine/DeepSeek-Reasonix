package daemon

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/agent"
)

// WebhookConfig configures the webhook receiver endpoint.
type WebhookConfig struct {
	// Secret is the HMAC-SHA256 key for validating inbound webhook payloads.
	// Required — webhooks without a valid signature are rejected.
	Secret string `json:"secret"`
	// Enabled controls whether the /webhook endpoint accepts events.
	Enabled bool `json:"enabled"`
}

// WebhookEvent is the envelope for all inbound webhook payloads.
type WebhookEvent struct {
	// Type identifies the event class: "github.ci", "github.pr", "custom", etc.
	Type string `json:"type"`
	// SessionID targets a specific session. If empty, the daemon routes by other fields.
	SessionID string `json:"session_id,omitempty"`
	// Summary is a short human-readable description injected as the user turn.
	Summary string `json:"summary"`
	// Payload is arbitrary event data. The raw request is stored in a local
	// sidecar; model context only receives a bounded summary.
	Payload json.RawMessage `json:"payload,omitempty"`
	// EventID is an optional dedup key. If set, duplicate events are ignored.
	EventID string `json:"event_id,omitempty"`
}

// WebhookAdapter normalizes provider-specific webhook requests into the daemon's
// common envelope plus routing fields. New providers should plug in here instead
// of adding conditionals to the worker or scheduler path.
type WebhookAdapter interface {
	Normalize(r *http.Request, body []byte, evt WebhookEvent) (WebhookEvent, WebhookInfo, bool)
}

type webhookAdapterRegistry struct {
	adapters []WebhookAdapter
}

var defaultWebhookAdapters = webhookAdapterRegistry{
	adapters: []WebhookAdapter{githubWebhookAdapter{}},
}

// RegisterWebhookAdapter prepends an adapter to the daemon's default webhook
// registry. Register provider-specific adapters during initialization so the
// daemon core continues to operate on the common WebhookEvent/WebhookInfo shape.
func RegisterWebhookAdapter(adapter WebhookAdapter) {
	if adapter == nil {
		return
	}
	defaultWebhookAdapters.adapters = append([]WebhookAdapter{adapter}, defaultWebhookAdapters.adapters...)
}

func (r webhookAdapterRegistry) normalize(req *http.Request, body []byte, evt WebhookEvent) (WebhookEvent, WebhookInfo) {
	for _, adapter := range r.adapters {
		if adapter == nil {
			continue
		}
		if normalized, info, ok := adapter.Normalize(req, body, evt); ok {
			return normalized, info
		}
	}
	return evt, WebhookInfo{}
}

// handleWebhook receives external events, validates the signature, and queues
// a wakeup for the targeted session.
func (d *Daemon) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if d.webhookCfg == nil || !d.webhookCfg.Enabled {
		http.Error(w, `{"error":"webhook not enabled"}`, http.StatusNotFound)
		return
	}

	// Read body.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB max
	if err != nil {
		http.Error(w, `{"error":"read body failed"}`, http.StatusBadRequest)
		return
	}

	// Validate HMAC-SHA256 signature.
	sig := r.Header.Get("X-Webhook-Signature")
	if sig == "" {
		sig = r.Header.Get("X-Hub-Signature-256") // GitHub compat
	}
	if !d.validateSignature(body, sig) {
		http.Error(w, `{"error":"invalid signature"}`, http.StatusUnauthorized)
		return
	}

	// Parse event.
	var evt WebhookEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	evt, info := defaultWebhookAdapters.normalize(r, body, evt)
	if evt.SessionID == "" {
		sessionID, ok := d.routeWebhookEvent(evt, info)
		if !ok {
			http.Error(w, `{"error":"session_id required or no matching session"}`, http.StatusBadRequest)
			return
		}
		evt.SessionID = sessionID
	}
	if evt.Summary == "" {
		evt.Summary = webhookSummary(evt, info)
	}
	wakeupKey := webhookWakeupKey(evt, info)

	// Find session.
	d.mu.Lock()
	entry, ok := d.registry[evt.SessionID]
	if !ok {
		d.mu.Unlock()
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	// Dedup check.
	if evt.EventID != "" && entry.Runtime.Scheduler.LastWakeupEventID == evt.EventID {
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":true,"status":"duplicate","event_id":%q}`, evt.EventID)
		return
	}
	if wakeupKey != "" && entry.Runtime.Scheduler.LastWakeupKey == wakeupKey {
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":true,"status":"duplicate","event_id":%q,"wakeup_key":%q}`, evt.EventID, wakeupKey)
		return
	}
	if _, running := d.activeRuns[evt.SessionID]; running {
		d.mu.Unlock()
		http.Error(w, `{"error":"session already running"}`, http.StatusConflict)
		return
	}
	payloadRef, err := saveWebhookPayload(entry.Path, body, time.Now().UTC())
	if err != nil {
		d.mu.Unlock()
		http.Error(w, fmt.Sprintf(`{"error":"save payload failed: %s"}`, err), http.StatusInternalServerError)
		return
	}
	waitingEvent := entry.Runtime.Wait.Kind == "event"
	if waitingEvent && !webhookMatchesWait(entry.Runtime.Wait, evt, info) {
		if webhookShouldDiagnoseWaitFailure(entry.Runtime.Wait, evt, info) {
			wait := entry.Runtime.Wait
			now := time.Now()
			reasonKey := "webhook:" + evt.Type + ":failure"
			if ok, reason := reserveAutoWakeupBudget(&entry.Runtime, reasonKey, now); !ok {
				entry.Runtime.Scheduler.LastWakeupAt = now
				entry.Runtime.Scheduler.LastWakeupReason = "budget_blocked:" + reasonKey
				if evt.EventID != "" {
					entry.Runtime.Scheduler.LastWakeupEventID = evt.EventID
				}
				if wakeupKey != "" {
					entry.Runtime.Scheduler.LastWakeupKey = wakeupKey
				}
				runtime := entry.Runtime
				path := entry.Path
				d.mu.Unlock()
				if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
					http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, err), http.StatusInternalServerError)
					return
				}
				d.appendTimeline(path, agent.RuntimeTimelineEvent{
					Type:       "wakeup_budget_blocked",
					Source:     "webhook",
					Reason:     reason,
					EventID:    evt.EventID,
					Step:       "deterministic",
					RunStatus:  runtime.Run.Status,
					GoalStatus: runtime.Goal.Status,
					WaitKind:   wait.Kind,
					WaitID:     wait.EventID,
					Subject:    wait.Subject,
					Message:    reason,
					PayloadRef: payloadRef,
				})
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"ok":true,"status":"budget_blocked","event_id":%q}`, evt.EventID)
				return
			}

			entry.Runtime.Run.Status = agent.RunStatusQueued
			entry.Runtime.Run.LastWakeupReason = reasonKey
			entry.Runtime.Run.ResumeCount++
			entry.Runtime.Scheduler.LastWakeupAt = now
			entry.Runtime.Scheduler.LastWakeupReason = reasonKey
			if evt.EventID != "" {
				entry.Runtime.Scheduler.LastWakeupEventID = evt.EventID
			}
			if wakeupKey != "" {
				entry.Runtime.Scheduler.LastWakeupKey = wakeupKey
			}
			runtime := entry.Runtime
			path := entry.Path
			d.mu.Unlock()

			if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, err), http.StatusInternalServerError)
				return
			}
			d.appendTimeline(path, agent.RuntimeTimelineEvent{
				Type:       "wait_event_failure_detected",
				Source:     "webhook",
				Reason:     reasonKey,
				EventID:    evt.EventID,
				Step:       "deterministic",
				RunStatus:  runtime.Run.Status,
				GoalStatus: runtime.Goal.Status,
				WaitKind:   wait.Kind,
				WaitID:     wait.EventID,
				Subject:    wait.Subject,
				Message:    webhookSummary(evt, info),
				PayloadRef: payloadRef,
			})
			d.enqueueIntent(RunIntent{
				SessionID:   evt.SessionID,
				SessionPath: path,
				Source:      "webhook",
				Reason:      reasonKey,
				EventID:     evt.EventID,
				Context:     boundedWebhookFailureContext(evt, info, wait),
			})
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"session_id":%q,"status":"pending_diagnosis","event_id":%q}`, evt.SessionID, evt.EventID)
			return
		}
		wait := entry.Runtime.Wait
		runtime := entry.Runtime
		path := entry.Path
		d.mu.Unlock()
		d.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       "wait_event_ignored",
			Source:     "webhook",
			Reason:     "incoming event did not match wait condition",
			EventID:    evt.EventID,
			Step:       "deterministic",
			RunStatus:  runtime.Run.Status,
			GoalStatus: runtime.Goal.Status,
			WaitKind:   wait.Kind,
			WaitID:     wait.EventID,
			Subject:    wait.Subject,
			Message:    webhookSummary(evt, info),
			PayloadRef: payloadRef,
		})
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":true,"status":"ignored","event_id":%q}`, evt.EventID)
		return
	}
	now := time.Now()
	if ok, reason := reserveAutoWakeupBudget(&entry.Runtime, "webhook:"+evt.Type, now); !ok {
		entry.Runtime.Scheduler.LastWakeupAt = now
		entry.Runtime.Scheduler.LastWakeupReason = "budget_blocked:webhook:" + evt.Type
		if evt.EventID != "" {
			entry.Runtime.Scheduler.LastWakeupEventID = evt.EventID
		}
		if wakeupKey != "" {
			entry.Runtime.Scheduler.LastWakeupKey = wakeupKey
		}
		runtime := entry.Runtime
		path := entry.Path
		d.mu.Unlock()
		if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, err), http.StatusInternalServerError)
			return
		}
		d.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       "wakeup_budget_blocked",
			Source:     "webhook",
			Reason:     reason,
			EventID:    evt.EventID,
			Step:       "deterministic",
			RunStatus:  runtime.Run.Status,
			GoalStatus: runtime.Goal.Status,
			Message:    reason,
			PayloadRef: payloadRef,
		})
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":true,"status":"budget_blocked","event_id":%q}`, evt.EventID)
		return
	}

	// Update runtime to signal the wakeup.
	entry.Runtime.Run.Status = agent.RunStatusQueued
	entry.Runtime.Run.LastWakeupReason = "webhook:" + evt.Type
	entry.Runtime.Run.ResumeCount++
	if waitingEvent {
		entry.Runtime.Wait = agent.RuntimeWaitMeta{}
	} else {
		entry.Runtime.Wait = agent.RuntimeWaitMeta{
			EventSource: "webhook:" + evt.Type,
			EventID:     evt.EventID,
		}
	}
	entry.Runtime.Scheduler.LastWakeupAt = now
	entry.Runtime.Scheduler.LastWakeupReason = "webhook:" + evt.Type
	if evt.EventID != "" {
		entry.Runtime.Scheduler.LastWakeupEventID = evt.EventID
	}
	if wakeupKey != "" {
		entry.Runtime.Scheduler.LastWakeupKey = wakeupKey
	}
	runtime := entry.Runtime
	path := entry.Path
	d.mu.Unlock()

	if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, err), http.StatusInternalServerError)
		return
	}
	d.appendTimeline(path, agent.RuntimeTimelineEvent{
		Type:       "webhook_event_received",
		Source:     "webhook",
		Reason:     "webhook:" + evt.Type,
		EventID:    evt.EventID,
		Step:       "deterministic",
		RunStatus:  runtime.Run.Status,
		GoalStatus: runtime.Goal.Status,
		Message:    webhookSummary(evt, info),
		PayloadRef: payloadRef,
	})
	d.enqueueIntent(RunIntent{
		SessionID:   evt.SessionID,
		SessionPath: path,
		Source:      "webhook",
		Reason:      "webhook:" + evt.Type,
		EventID:     evt.EventID,
		Context:     boundedWebhookContext(evt, info),
	})

	d.logger.Info("webhook event received", "type", evt.Type, "session", evt.SessionID, "event_id", evt.EventID)

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ok":true,"session_id":%q,"status":"queued","event_id":%q}`, evt.SessionID, evt.EventID)
}

func saveWebhookPayload(sessionPath string, body []byte, receivedAt time.Time) (string, error) {
	dir := webhookPayloadDir(sessionPath)
	if dir == "" {
		return "", fmt.Errorf("empty session path")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	name := fmt.Sprintf("%s-%s.json",
		receivedAt.UTC().Format("20060102T150405.000000000Z"),
		hex.EncodeToString(sum[:8]),
	)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join(filepath.Base(dir), name)), nil
}

func webhookPayloadDir(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return agent.RuntimeMetaPath(sessionPath) + ".webhooks"
}

func webhookPayloadPathFromRef(sessionPath, ref string) (string, bool) {
	ref = filepath.Clean(filepath.FromSlash(strings.TrimSpace(ref)))
	if ref == "." || filepath.IsAbs(ref) || strings.HasPrefix(ref, ".."+string(filepath.Separator)) {
		return "", false
	}
	dir := webhookPayloadDir(sessionPath)
	if filepath.Dir(ref) != filepath.Base(dir) {
		return "", false
	}
	return filepath.Join(filepath.Dir(dir), ref), true
}

func webhookMatchesWait(wait agent.RuntimeWaitMeta, evt WebhookEvent, info WebhookInfo) bool {
	if wait.EventSource != "" && !webhookSourceMatches(wait.EventSource, evt, info) {
		return false
	}
	if wait.EventID != "" && !strings.EqualFold(wait.EventID, evt.EventID) {
		return false
	}
	if wait.EventStatus != "" && !strings.EqualFold(wait.EventStatus, info.Status) {
		return false
	}
	if wait.EventConclusion != "" && !strings.EqualFold(wait.EventConclusion, info.Conclusion) {
		return false
	}
	return true
}

func webhookWakeupKey(evt WebhookEvent, info WebhookInfo) string {
	eventType := strings.ToLower(strings.TrimSpace(firstNonEmpty(evt.Type, info.Event)))
	repo := strings.ToLower(strings.TrimSpace(info.Repo))
	if eventType == "" || repo == "" {
		return ""
	}
	target := strings.ToLower(strings.TrimSpace(info.Ref))
	if info.Number > 0 {
		target = "#" + strconv.Itoa(info.Number)
	}
	state := strings.ToLower(strings.Join([]string{
		strings.TrimSpace(info.Action),
		strings.TrimSpace(info.Status),
		strings.TrimSpace(info.Conclusion),
	}, "/"))
	state = strings.Trim(state, "/")
	if target == "" || state == "" {
		return ""
	}
	return strings.Join([]string{eventType, repo, target, state}, "|")
}

func webhookShouldDiagnoseWaitFailure(wait agent.RuntimeWaitMeta, evt WebhookEvent, info WebhookInfo) bool {
	if !strings.EqualFold(wait.EventConclusion, "success") || !info.Failure {
		return false
	}
	if wait.EventSource != "" && !webhookSourceMatches(wait.EventSource, evt, info) {
		return false
	}
	if wait.EventID != "" && !strings.EqualFold(wait.EventID, evt.EventID) {
		return false
	}
	if wait.EventStatus != "" && !strings.EqualFold(wait.EventStatus, info.Status) {
		return false
	}
	return true
}

func isFailureConclusion(conclusion string) bool {
	switch strings.ToLower(strings.TrimSpace(conclusion)) {
	case "failure", "cancelled", "timed_out", "action_required", "startup_failure", "stale":
		return true
	default:
		return false
	}
}

func webhookSourceMatches(want string, evt WebhookEvent, info WebhookInfo) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return true
	}
	candidates := []string{
		evt.Type,
		"webhook:" + evt.Type,
		info.Event,
	}
	if info.Provider != "" && info.Event != "" {
		providerEvent := info.Provider + "." + info.Event
		candidates = append(candidates, providerEvent, "webhook:"+providerEvent)
	}
	for _, candidate := range candidates {
		if strings.EqualFold(want, strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

// WebhookInfo is the provider-normalized view used for routing, deduplication,
// wait matching, summaries, and failure semantics.
type WebhookInfo struct {
	Provider   string
	Event      string
	Delivery   string
	Action     string
	Repo       string
	Number     int
	Ref        string
	Status     string
	Conclusion string
	Title      string
	Failure    bool
}

type githubWebhookInfo = WebhookInfo

type githubWebhookAdapter struct{}

func (githubWebhookAdapter) Normalize(r *http.Request, body []byte, evt WebhookEvent) (WebhookEvent, WebhookInfo, bool) {
	info := extractGitHubWebhookInfo(r, body)
	if info.Event == "" {
		return evt, info, false
	}
	if evt.Type == "" {
		evt.Type = "github." + info.Event
	}
	if evt.EventID == "" {
		evt.EventID = info.Delivery
	}
	return evt, info, true
}

func extractGitHubWebhookInfo(r *http.Request, body []byte) githubWebhookInfo {
	info := githubWebhookInfo{
		Provider: "github",
		Event:    strings.TrimSpace(r.Header.Get("X-GitHub-Event")),
		Delivery: strings.TrimSpace(r.Header.Get("X-GitHub-Delivery")),
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return info
	}
	info.Action = stringField(root, "action")
	info.Ref = normalizeGitHubRef(stringField(root, "ref"))
	if repo := mapField(root, "repository"); repo != nil {
		info.Repo = stringField(repo, "full_name")
	}
	if strings.EqualFold(info.Event, "push") && info.Action == "" {
		info.Action = "push"
	}
	if pr := mapField(root, "pull_request"); pr != nil {
		info.Number = intField(pr, "number")
		info.Title = stringField(pr, "title")
	}
	if issue := mapField(root, "issue"); issue != nil && info.Number == 0 {
		info.Number = intField(issue, "number")
		info.Title = stringField(issue, "title")
	}
	if wr := mapField(root, "workflow_run"); wr != nil {
		info.Status = stringField(wr, "status")
		info.Conclusion = stringField(wr, "conclusion")
		if info.Ref == "" {
			info.Ref = stringField(wr, "head_branch")
		}
		if info.Number == 0 {
			info.Number = firstPullNumber(wr)
		}
	}
	if cr := mapField(root, "check_run"); cr != nil {
		if info.Status == "" {
			info.Status = stringField(cr, "status")
		}
		if info.Conclusion == "" {
			info.Conclusion = stringField(cr, "conclusion")
		}
		if info.Number == 0 {
			info.Number = firstPullNumber(cr)
		}
	}
	if cs := mapField(root, "check_suite"); cs != nil {
		if info.Status == "" {
			info.Status = stringField(cs, "status")
		}
		if info.Conclusion == "" {
			info.Conclusion = stringField(cs, "conclusion")
		}
		if info.Ref == "" {
			info.Ref = stringField(cs, "head_branch")
		}
		if info.Number == 0 {
			info.Number = firstPullNumber(cs)
		}
	}
	if strings.EqualFold(info.Event, "status") {
		state := stringField(root, "state")
		if info.Status == "" {
			info.Status = state
		}
		if info.Conclusion == "" {
			info.Conclusion = state
		}
		if info.Ref == "" {
			info.Ref = firstBranchName(root)
		}
		if info.Title == "" {
			info.Title = stringField(root, "context")
		}
	}
	if rel := mapField(root, "release"); rel != nil {
		if info.Ref == "" {
			info.Ref = stringField(rel, "tag_name")
		}
		if info.Title == "" {
			info.Title = firstNonEmpty(stringField(rel, "name"), stringField(rel, "tag_name"))
		}
	}
	info.Failure = isFailureConclusion(info.Conclusion)
	return info
}

func (d *Daemon) routeWebhookEvent(evt WebhookEvent, info WebhookInfo) (string, bool) {
	repo := strings.ToLower(strings.TrimSpace(info.Repo))
	if repo == "" {
		return "", false
	}
	type candidate struct {
		id    string
		score int
	}
	d.mu.RLock()
	entries := make([]*SessionEntry, 0, len(d.registry))
	for _, entry := range d.registry {
		entries = append(entries, entry)
	}
	d.mu.RUnlock()
	var candidates []candidate
	for _, entry := range entries {
		score := webhookRouteScore(entry, repo, info.Ref, info.Number, len(entries))
		if score > 0 {
			candidates = append(candidates, candidate{id: entry.ID, score: score})
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	best := candidates[0]
	tie := false
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
			tie = false
		} else if c.score == best.score {
			tie = true
		}
	}
	if tie {
		return "", false
	}
	return best.id, true
}

func webhookRouteScore(entry *SessionEntry, repo, ref string, number int, totalSessions int) int {
	meta, _, _ := agent.LoadBranchMeta(entry.Path)
	haystack := strings.ToLower(strings.Join([]string{
		entry.Runtime.Goal.Text,
		meta.TopicTitle,
		meta.TopicID,
		meta.WorkspaceRoot,
		entry.Runtime.WorkspaceRoot,
		entry.Path,
	}, "\n"))
	score := 0
	if strings.Contains(haystack, repo) || strings.Contains(haystack, filepath.Base(repo)) || workspaceMatchesRepo(firstNonEmpty(entry.Runtime.WorkspaceRoot, meta.WorkspaceRoot), repo) {
		score += 4
	}
	if number > 0 && textMentionsNumber(haystack, number) {
		score += 3
	}
	if ref != "" && textMentionsRef(haystack, ref) {
		score += 2
	}
	switch entry.Runtime.Goal.Status {
	case "running", "blocked":
		score++
	}
	if number > 0 && score > 0 && !textMentionsNumber(haystack, number) && totalSessions > 1 {
		score--
	}
	return score
}

func workspaceMatchesRepo(root, repo string) bool {
	root = strings.TrimSpace(root)
	if root == "" || repo == "" {
		return false
	}
	b, err := os.ReadFile(filepath.Join(root, ".git", "config"))
	if err != nil {
		return false
	}
	text := strings.ToLower(string(b))
	repo = strings.ToLower(strings.TrimSuffix(repo, ".git"))
	return strings.Contains(text, repo) || strings.Contains(text, repo+".git")
}

func textMentionsNumber(text string, n int) bool {
	if n <= 0 {
		return false
	}
	s := strconv.Itoa(n)
	return strings.Contains(text, "#"+s) ||
		strings.Contains(text, "pr "+s) ||
		strings.Contains(text, "pr-"+s) ||
		strings.Contains(text, "pull/"+s) ||
		strings.Contains(text, "issues/"+s)
}

func textMentionsRef(text, ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if ref == "" {
		return false
	}
	return strings.Contains(text, "branch "+ref) ||
		strings.Contains(text, "branch="+ref) ||
		strings.Contains(text, "ref "+ref) ||
		strings.Contains(text, "ref="+ref) ||
		strings.Contains(text, "tag "+ref) ||
		strings.Contains(text, "tag="+ref) ||
		strings.Contains(text, "/tree/"+ref) ||
		strings.Contains(text, "/releases/tag/"+ref)
}

func webhookSummary(evt WebhookEvent, info WebhookInfo) string {
	parts := []string{"webhook event"}
	if evt.Type != "" {
		parts = append(parts, evt.Type)
	}
	if info.Repo != "" {
		parts = append(parts, "repo="+info.Repo)
	}
	if info.Number > 0 {
		parts = append(parts, fmt.Sprintf("number=%d", info.Number))
	}
	if info.Action != "" {
		parts = append(parts, "action="+info.Action)
	}
	if info.Status != "" {
		parts = append(parts, "status="+info.Status)
	}
	if info.Conclusion != "" {
		parts = append(parts, "conclusion="+info.Conclusion)
	}
	if info.Ref != "" {
		parts = append(parts, "ref="+info.Ref)
	}
	if info.Title != "" {
		parts = append(parts, "title="+info.Title)
	}
	return strings.Join(parts, " ")
}

func boundedWebhookContext(evt WebhookEvent, info WebhookInfo) string {
	summary := strings.TrimSpace(evt.Summary)
	if summary == "" {
		summary = webhookSummary(evt, info)
	}
	if len(summary) > 2000 {
		summary = summary[:2000]
	}
	return summary
}

func boundedWebhookFailureContext(evt WebhookEvent, info WebhookInfo, wait agent.RuntimeWaitMeta) string {
	summary := boundedWebhookContext(evt, info)
	prefix := "CI finished without the awaited successful conclusion. Summarize the failure, identify likely next diagnostic steps, and keep waiting for the success condition before release or merge actions."
	if wait.Subject != "" {
		prefix += " Waiting subject: " + wait.Subject + "."
	}
	if wait.EventConclusion != "" {
		prefix += " Awaited conclusion: " + wait.EventConclusion + "."
	}
	return prefix + "\n\nEvent: " + summary
}

func mapField(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func intField(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

func firstPullNumber(m map[string]any) int {
	raw, _ := m["pull_requests"].([]any)
	for _, item := range raw {
		pr, _ := item.(map[string]any)
		if n := intField(pr, "number"); n > 0 {
			return n
		}
	}
	return 0
}

func firstBranchName(m map[string]any) string {
	raw, _ := m["branches"].([]any)
	for _, item := range raw {
		branch, _ := item.(map[string]any)
		if name := stringField(branch, "name"); name != "" {
			return normalizeGitHubRef(name)
		}
	}
	return ""
}

func normalizeGitHubRef(ref string) string {
	ref = strings.TrimSpace(ref)
	for _, prefix := range []string{"refs/heads/", "refs/tags/"} {
		if strings.HasPrefix(ref, prefix) {
			return strings.TrimPrefix(ref, prefix)
		}
	}
	return ref
}

// validateSignature checks the HMAC-SHA256 signature against the webhook secret.
// Supports both "sha256=<hex>" (GitHub style) and raw hex formats.
func (d *Daemon) validateSignature(body []byte, signature string) bool {
	if d.webhookCfg == nil || d.webhookCfg.Secret == "" {
		return false
	}
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return false
	}

	// Strip "sha256=" prefix if present.
	signature = strings.TrimPrefix(signature, "sha256=")

	mac := hmac.New(sha256.New, []byte(d.webhookCfg.Secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(signature))
}
