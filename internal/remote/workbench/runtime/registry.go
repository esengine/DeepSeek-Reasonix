package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/fileutil"
	"reasonix/internal/remote/protocol"
)

const runtimeSessionRegistryVersion = 1

type runtimeSessionRegistry struct {
	Version   int                    `json:"version"`
	Workspace string                 `json:"workspace"`
	Sessions  []runtimeSessionRecord `json:"sessions"`
}

type runtimeSessionRecord struct {
	ID            protocol.SessionID         `json:"id"`
	Path          string                     `json:"path"`
	Model         string                     `json:"model"`
	Effort        string                     `json:"effort,omitempty"`
	Collaboration protocol.CollaborationMode `json:"collaborationMode,omitempty"`
	TokenMode     protocol.TokenMode         `json:"tokenMode,omitempty"`
	ToolApproval  protocol.ToolApprovalMode  `json:"toolApprovalMode,omitempty"`
	TopicID       protocol.TopicID           `json:"topicId"`
	Title         string                     `json:"title,omitempty"`
	CreatedAt     int64                      `json:"createdAtMs,omitempty"`
	UpdatedAt     int64                      `json:"updatedAtMs,omitempty"`
}

func (s *Server) sessionDir() string {
	if dir := strings.TrimSpace(s.opts.SessionDir); dir != "" {
		return dir
	}
	return config.SessionDir()
}

func (s *Server) sessionRegistryPath() string {
	if path := strings.TrimSpace(s.opts.RegistryPath); path != "" {
		return path
	}
	s.mu.Lock()
	socket := s.socket
	s.mu.Unlock()
	if strings.TrimSpace(socket) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(socket), "sessions.json")
}

func (s *Server) ensureSessionsRestored(ctx context.Context) error {
	path := s.sessionRegistryPath()
	if path == "" {
		return nil
	}
	s.registryMu.Lock()
	defer s.registryMu.Unlock()
	if !s.registryRead {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read Remote session registry: %w", err)
		}
		if err == nil {
			var registry runtimeSessionRegistry
			if err := json.Unmarshal(data, &registry); err != nil {
				return fmt.Errorf("decode Remote session registry: %w", err)
			}
			if registry.Version != runtimeSessionRegistryVersion {
				return fmt.Errorf("unsupported Remote session registry version %d", registry.Version)
			}
			if !sameRegistryWorkspace(registry.Workspace, s.opts.Workspace) {
				return fmt.Errorf("Remote session registry belongs to another workspace")
			}
			for _, record := range registry.Sessions {
				if err := s.validateSessionRecord(record); err != nil {
					s.logRegistryError("ignore invalid session record", err)
					continue
				}
				s.dormant[record.ID] = record
			}
		}
		s.registryRead = true
	}

	for id, record := range s.dormant {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		s.mu.Lock()
		_, exists := s.sessions[id]
		s.mu.Unlock()
		if exists {
			delete(s.dormant, id)
			continue
		}
		restored, err := s.restoreSessionRecord(ctx, record)
		if err != nil {
			s.logRegistryError("defer session restore", err)
			continue
		}
		s.mu.Lock()
		duplicate := false
		if _, exists := s.sessions[id]; !exists {
			s.sessions[id] = restored
		} else {
			duplicate = true
		}
		s.mu.Unlock()
		if duplicate {
			closeRuntimeSession(restored)
		}
		delete(s.dormant, id)
	}
	return nil
}

func (s *Server) restoreSessionRecord(ctx context.Context, record runtimeSessionRecord) (*session, error) {
	effort := record.Effort
	sink := &sessionSink{server: s, sessionID: record.ID}
	ctrl, err := s.buildController(ctx, record.Model, &effort, sink, normalizedTokenMode(record.TokenMode))
	if err != nil {
		if ctrl != nil {
			ctrl.Close()
		}
		return nil, fmt.Errorf("restore session %s controller: %w", record.ID, err)
	}
	if ctrl == nil {
		return nil, fmt.Errorf("restore session %s controller: builder returned nil", record.ID)
	}
	leases := control.NewSessionLeaseKeeper()
	if err := leases.Rebind(record.Path); err != nil {
		ctrl.Close()
		leases.Release()
		return nil, fmt.Errorf("restore session %s lease: %w", record.ID, err)
	}
	loaded, loadErr := agent.LoadSession(record.Path)
	switch {
	case loadErr == nil && loaded != nil:
		ctrl.AdoptHistory(loaded.Messages, record.Path)
	case os.IsNotExist(loadErr):
		ctrl.SetSessionPath(record.Path)
	case loadErr != nil:
		ctrl.Close()
		leases.Release()
		return nil, fmt.Errorf("restore session %s transcript: %w", record.ID, loadErr)
	default:
		ctrl.SetSessionPath(record.Path)
	}
	now := time.Now().UnixMilli()
	createdAt, updatedAt := record.CreatedAt, record.UpdatedAt
	if createdAt <= 0 {
		createdAt = now
	}
	if updatedAt <= 0 {
		updatedAt = createdAt
	}
	title := strings.TrimSpace(record.Title)
	if title == "" {
		title = "New session"
	}
	restored := &session{
		id: record.ID, ctrl: ctrl, leases: leases, model: ctrl.ModelRef(), effort: effort,
		collaboration: normalizedCollaboration(record.Collaboration), tokenMode: normalizedTokenMode(record.TokenMode),
		toolApproval: normalizedToolApproval(record.ToolApproval), topicID: record.TopicID, title: title,
		runtimeEpoch: protocol.RuntimeEpoch("runtime_" + randomHex(12)), createdAt: createdAt, updatedAt: updatedAt, sink: sink,
	}
	applyControllerProfile(ctrl, restored.collaboration, restored.toolApproval)
	return restored, nil
}

func (s *Server) validateSessionRecord(record runtimeSessionRecord) error {
	if !strings.HasPrefix(string(record.ID), "session_") || strings.TrimSpace(string(record.TopicID)) == "" {
		return fmt.Errorf("invalid Remote session identity")
	}
	if strings.TrimSpace(record.Model) == "" {
		return fmt.Errorf("session %s has no model", record.ID)
	}
	path, err := containedSessionPath(s.sessionDir(), record.Path)
	if err != nil {
		return fmt.Errorf("session %s path: %w", record.ID, err)
	}
	if path != record.Path {
		return fmt.Errorf("session %s path is not canonical", record.ID)
	}
	return nil
}

func containedSessionPath(dir, path string) (string, error) {
	dir = strings.TrimSpace(dir)
	path = strings.TrimSpace(path)
	if dir == "" || path == "" || !strings.EqualFold(filepath.Ext(path), ".jsonl") {
		return "", errors.New("path must be a JSONL transcript in the session directory")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("path escapes the session directory")
	}
	// Remote runtime transcripts are minted directly in SessionDir. Rejecting
	// nested paths also prevents an edited registry from traversing an in-tree
	// directory symlink to a file outside the persistence root.
	if filepath.Dir(absPath) != absDir {
		return "", errors.New("path must be a direct child of the session directory")
	}
	if info, err := os.Lstat(absPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("session transcript may not be a symbolic link")
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return absPath, nil
}

func (s *Server) persistSessionRegistry() error {
	path := s.sessionRegistryPath()
	if path == "" {
		return nil
	}
	s.registryMu.Lock()
	defer s.registryMu.Unlock()
	if !s.registryRead {
		// A runtime that never listed or created sessions must not replace a registry
		// it never loaded, especially during an early startup failure.
		return nil
	}
	records := make(map[protocol.SessionID]runtimeSessionRecord, len(s.dormant))
	for id, record := range s.dormant {
		records[id] = record
	}
	type activeRecord struct {
		record runtimeSessionRecord
		ctrl   SessionController
	}
	s.mu.Lock()
	active := make([]activeRecord, 0, len(s.sessions))
	for _, sess := range s.sessions {
		if sess == nil || sess.ctrl == nil {
			continue
		}
		active = append(active, activeRecord{
			ctrl: sess.ctrl,
			record: runtimeSessionRecord{
				ID: sess.id, Model: sess.model, Effort: sess.effort,
				Collaboration: sess.collaboration, TokenMode: sess.tokenMode, ToolApproval: sess.toolApproval,
				TopicID: sess.topicID, Title: sess.title, CreatedAt: sess.createdAt, UpdatedAt: sess.updatedAt,
			},
		})
	}
	s.mu.Unlock()
	for _, item := range active {
		path, err := containedSessionPath(s.sessionDir(), item.ctrl.SessionPath())
		if err != nil {
			return fmt.Errorf("persist session %s: %w", item.record.ID, err)
		}
		item.record.Path = path
		records[item.record.ID] = item.record
	}
	ordered := make([]runtimeSessionRecord, 0, len(records))
	for _, record := range records {
		ordered = append(ordered, record)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].UpdatedAt != ordered[j].UpdatedAt {
			return ordered[i].UpdatedAt > ordered[j].UpdatedAt
		}
		return ordered[i].ID < ordered[j].ID
	})
	body, err := json.MarshalIndent(runtimeSessionRegistry{
		Version: runtimeSessionRegistryVersion, Workspace: canonicalRegistryWorkspace(s.opts.Workspace), Sessions: ordered,
	}, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, append(body, '\n'), 0o600)
}

func canonicalRegistryWorkspace(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if abs, err := filepath.Abs(workspace); err == nil {
		workspace = abs
	}
	return filepath.Clean(workspace)
}

func sameRegistryWorkspace(a, b string) bool {
	return canonicalRegistryWorkspace(a) == canonicalRegistryWorkspace(b)
}

func normalizedCollaboration(value protocol.CollaborationMode) protocol.CollaborationMode {
	switch value {
	case protocol.CollaborationPlan, protocol.CollaborationGoal:
		return value
	default:
		return protocol.CollaborationNormal
	}
}

func normalizedTokenMode(value protocol.TokenMode) protocol.TokenMode {
	switch value {
	case protocol.TokenEconomy, protocol.TokenDelivery:
		return value
	default:
		return protocol.TokenFull
	}
}

func normalizedToolApproval(value protocol.ToolApprovalMode) protocol.ToolApprovalMode {
	switch value {
	case protocol.ToolApprovalAuto, protocol.ToolApprovalYOLO:
		return value
	default:
		return protocol.ToolApprovalAsk
	}
}

func (s *Server) logRegistryError(action string, err error) {
	if err == nil || s.opts.Logger == nil {
		return
	}
	_, _ = fmt.Fprintf(s.opts.Logger, "Remote Workbench %s: %v\n", action, err)
}
