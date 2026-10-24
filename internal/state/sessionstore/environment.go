package sessionstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/state/store"
)

var (
	ErrExecutionEnvironmentProtocol = errors.New("unsupported session execution environment; binding retained")
	ErrExecutionEnvironmentMismatch = errors.New("session requires its bound execution environment; reopen that environment")
)

// ExecutionEnvironmentBinding is immutable for one transcript. Unknown formats
// block execution instead of turning an isolated conversation into a source writer.
type ExecutionEnvironmentBinding struct {
	Version         int    `json:"version"`
	SessionPath     string `json:"session_path"`
	EnvironmentID   string `json:"environment_id"`
	Generation      string `json:"generation"`
	SourceWorkspace string `json:"source_workspace"`
	WorkspaceRoot   string `json:"workspace_root"`
	WorkingCopyRoot string `json:"working_copy_root"`
	ArchiveOnly     bool   `json:"archive_only,omitempty"`
}

func ExecutionEnvironmentPath(sessionPath string) string {
	if strings.TrimSpace(sessionPath) == "" {
		return ""
	}
	return store.SessionEnvironment(canonicalSessionSavePath(sessionPath))
}

func LoadExecutionEnvironment(sessionPath string) (ExecutionEnvironmentBinding, bool, error) {
	return loadExecutionEnvironment(sessionPath)
}

func loadExecutionEnvironmentSidecar(sessionPath string) (ExecutionEnvironmentBinding, bool, error) {
	var r ExecutionEnvironmentBinding
	if strings.TrimSpace(sessionPath) == "" {
		return r, false, nil
	}
	if len(filepath.Base(ExecutionEnvironmentPath(sessionPath))) > nameMaxBytes {
		return r, false, nil
	}
	raw, err := os.ReadFile(ExecutionEnvironmentPath(sessionPath))
	if errors.Is(err, os.ErrNotExist) {
		return r, false, nil
	}
	if err != nil {
		return r, true, err
	}
	if json.Unmarshal(raw, &r) != nil || r.Version != 1 || r.SessionPath != canonicalSessionSavePath(sessionPath) ||
		r.EnvironmentID == "" || r.Generation == "" || !filepath.IsAbs(r.SourceWorkspace) ||
		!filepath.IsAbs(r.WorkspaceRoot) || !filepath.IsAbs(r.WorkingCopyRoot) {
		return r, true, ErrExecutionEnvironmentProtocol
	}
	return r, true, nil
}

// BindExecutionEnvironment requires the transcript's current session authority.
// An existing binding is retained byte-for-byte, including unknown fields.
func BindExecutionEnvironment(sessionPath string, binding ExecutionEnvironmentBinding, authority *SessionWriteAuthority) error {
	end, err := authority.BeginSave(sessionPath)
	if err != nil {
		return err
	}
	defer end()
	return WithSessionPublication(sessionPath, func(p *SessionPublication) error {
		return p.BindExecutionEnvironment(binding, authority)
	})
}

func bindExecutionEnvironmentLocked(sessionPath string, binding ExecutionEnvironmentBinding) error {
	binding.SessionPath = canonicalSessionSavePath(sessionPath)
	if binding.Version != 1 || binding.EnvironmentID == "" || binding.Generation == "" ||
		!filepath.IsAbs(binding.SourceWorkspace) || !filepath.IsAbs(binding.WorkspaceRoot) || !filepath.IsAbs(binding.WorkingCopyRoot) {
		return ErrExecutionEnvironmentProtocol
	}
	if current, exists, err := loadExecutionEnvironmentSidecar(sessionPath); err != nil {
		return err
	} else if exists {
		if !reflect.DeepEqual(current, binding) {
			return ErrExecutionEnvironmentMismatch
		}
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	if previous, readErr := os.ReadFile(ExecutionEnvironmentPath(sessionPath)); readErr == nil {
		raw = previous
	}
	if err := publishEnvironmentFence(sessionPath, raw, binding); err != nil {
		return err
	}
	if _, exists, err := loadExecutionEnvironmentSidecar(sessionPath); err != nil || exists {
		return err
	}
	if err := fileutil.AtomicCreateFile(ExecutionEnvironmentPath(sessionPath), raw, 0600); err != nil {
		if current, exists, loadErr := LoadExecutionEnvironment(sessionPath); loadErr == nil && exists && reflect.DeepEqual(current, binding) {
			return nil
		}
		return err
	}
	return nil
}

func CheckExecutionEnvironment(sessionPath, workspaceRoot string) error {
	binding, exists, err := LoadExecutionEnvironment(sessionPath)
	if err != nil || !exists {
		return err
	}
	if binding.ArchiveOnly {
		return ErrExecutionEnvironmentMismatch
	}
	expected, err := filepath.EvalSymlinks(binding.WorkspaceRoot)
	if err != nil {
		return errors.Join(ErrExecutionEnvironmentMismatch, err)
	}
	actual, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return errors.Join(ErrExecutionEnvironmentMismatch, err)
	}
	if CanonicalSessionPath(expected) != CanonicalSessionPath(actual) {
		return ErrExecutionEnvironmentMismatch
	}
	info, err := os.Stat(expected)
	if err != nil || !info.IsDir() {
		return errors.Join(ErrExecutionEnvironmentMismatch, err)
	}
	return nil
}
