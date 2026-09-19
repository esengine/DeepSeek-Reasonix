package serve

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/session"
	"reasonix/internal/store"
)

// generatedTitleWriteTimeout bounds the best-effort durable write of a title
// generated while listing; the poll must not wait on another writer's lease.
const generatedTitleWriteTimeout = 2 * time.Second

type sessionListEntry struct {
	HostID     string `json:"hostId,omitempty"`
	SessionID  string `json:"sessionId,omitempty"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Title      string `json:"title,omitempty"`
	Turns      int    `json:"turns,omitempty"`
	Current    bool   `json:"current,omitempty"`
	Running    bool   `json:"running,omitempty"`
	TakenOver  bool   `json:"takenOver,omitempty"`
	MtimeMilli int64  `json:"mtimeMilli"`
}

// sessions lists saved sessions with event-log-aware titles and turn counts.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	ctrl := s.ctl()
	dir := ctrl.SessionDir()
	if dir == "" {
		writeJSON(w, []any{})
		return
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		entries = nil
	} else if err != nil {
		writeJSON(w, []any{})
		return
	}
	current := agent.CanonicalSessionPath(ctrl.SessionPath())
	running := map[string]bool{}
	s.detachedMu.Lock()
	for path, detached := range s.detached {
		running[filepath.Clean(path)] = controllerHasActiveRuntimeWork(detached.ctrl)
	}
	s.detachedMu.Unlock()
	out := make([]sessionListEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !store.IsSessionTranscriptName(entry.Name()) {
			continue
		}
		path := agent.CanonicalSessionPath(filepath.Join(dir, entry.Name()))
		if agent.IsCleanupPending(path) {
			continue
		}
		mtime := agent.SessionContentModTime(path)
		cleanPath := agent.CanonicalSessionPath(path)
		row := sessionListEntry{
			Name:       strings.TrimSuffix(entry.Name(), ".jsonl"),
			Path:       path,
			Current:    cleanPath == current,
			Running:    running[cleanPath],
			TakenOver:  s.sessionMirrored(cleanPath) || leaseHeldByForeignRuntime(cleanPath),
			MtimeMilli: mtime.UnixMilli(),
		}
		if row.Current {
			row.Running = controllerHasActiveRuntimeWork(ctrl) && !row.TakenOver
		}
		first, turns, cached := agent.SessionPreviewCached(path)
		if !cached {
			first, turns = agent.SessionPreview(path)
		}
		if turns > 0 {
			row.Turns = turns
			row.Title = s.sessionTitle(r.Context(), entry.Name(), first, mtime.UnixNano())
		}
		out = append(out, row)
	}
	if concrete, ok := ctrl.(*control.Controller); ok {
		if service := concrete.SessionService(); service != nil {
			_, runtime, bound := concrete.SessionBinding()
			page, listErr := service.Query().List(r.Context(), "", 100)
			if listErr == nil {
				for _, info := range page.Sessions {
					row := sessionListEntry{
						HostID: info.Ref.HostID, SessionID: info.Ref.SessionID, Name: info.SessionID,
						Title: s.sessionDisplayTitle(r.Context(), service, info), Turns: info.Turns,
						MtimeMilli: info.UpdatedAt.UnixMilli(),
						Current:    bound && info.Ref == runtime.Ref(),
					}
					if live, exists := service.Runtime(info.Ref); exists {
						phase := live.Snapshot().Phase
						row.Running = phase.Busy()
					}
					out = append(out, row)
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].MtimeMilli > out[j].MtimeMilli })
	writeJSON(w, out)
}

// sessionDisplayTitle resolves what the session list shows for one stored
// session, following docs/SESSION_TITLE_OWNERSHIP.md: an explicit title in the
// session's own event log first, then a generated title, then the first authored
// message. The generated title is written through to the same event log rather
// than kept beside it, so every reader of the store sees the same name and a
// manual rename racing this generation wins on the title revision.
func (s *Server) sessionDisplayTitle(ctx context.Context, service *session.Service, info session.SessionInfo) string {
	if info.Title != "" {
		return info.Title
	}
	// A v4 session has no transcript path, so its store id keys the local
	// generation cache and its catalog preview stands in for the first message.
	if info.Preview == "" {
		return ""
	}
	title := s.sessionTitle(ctx, info.SessionID, info.Preview, info.UpdatedAt.UnixNano())
	if title == "" {
		return ""
	}
	s.persistGeneratedTitle(ctx, service, info, title)
	return title
}

// persistGeneratedTitle records a generated title in the session's own event
// log, guarded by the title revision the listing read. Best-effort: a session
// another runtime owns, or one renamed while generation ran, simply keeps its
// current title, and the local cache still serves this poll.
func (s *Server) persistGeneratedTitle(ctx context.Context, service *session.Service, info session.SessionInfo, title string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), generatedTitleWriteTimeout)
	defer cancel()
	err := service.SetTitleIfSequence(writeCtx, info.Ref, info.TitleSequence, title)
	if err != nil && !errors.Is(err, session.ErrSessionTitleChanged) {
		slog.Warn("serve: persist generated title", "session", info.SessionID, "err", err)
	}
}
