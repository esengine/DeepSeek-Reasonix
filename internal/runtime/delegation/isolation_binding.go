package delegation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

// A separate record survives preceding metadata writers that drop unknown fields.
type isolationTranscriptBinding struct {
	Version   int    `json:"version"`
	Ref       string `json:"ref"`
	Parent    string `json:"parent"`
	Workspace string `json:"workspace"`
	Isolation string `json:"isolation"`
	Grant     string `json:"grant"`
	Schema    string `json:"schema"`
}

func (s *SubagentStore) restoreIsolationBinding(meta *sessionstore.SubagentMeta) error {
	path := store.SessionIsolationBinding(s.sessionPath(meta.Ref))
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(path)
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return fmt.Errorf("invalid isolated transcript binding")
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return err
	}
	var binding isolationTranscriptBinding
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if len(data) > 64<<10 || d.Decode(&binding) != nil || d.Decode(new(any)) != io.EOF || binding.Version != 1 {
		return fmt.Errorf("unsupported or malformed isolated transcript binding; preserved")
	}
	if binding.Ref != meta.Ref || binding.Parent != meta.ParentSession || binding.Workspace != meta.WorkspaceRoot || binding.Schema != meta.ToolSchemaHash || binding.Isolation == "" || binding.Grant == "" {
		return fmt.Errorf("isolated transcript binding identity changed")
	}
	if (meta.IsolationRef != "" && meta.IsolationRef != binding.Isolation) || (meta.IsolationGrant != "" && meta.IsolationGrant != binding.Grant) {
		return fmt.Errorf("isolated transcript binding disagrees with metadata")
	}
	meta.IsolationRef, meta.IsolationGrant = binding.Isolation, binding.Grant
	return nil
}

func (s *SubagentStore) preserveIsolationBinding(meta sessionstore.SubagentMeta) error {
	if meta.IsolationRef == "" {
		return s.restoreIsolationBinding(&meta)
	}
	binding := isolationTranscriptBinding{1, meta.Ref, meta.ParentSession, meta.WorkspaceRoot, meta.IsolationRef, meta.IsolationGrant, meta.ToolSchemaHash}
	data, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	err = fileutil.AtomicCreateFile(store.SessionIsolationBinding(s.sessionPath(meta.Ref)), data, 0600)
	if errors.Is(err, os.ErrExist) {
		return s.restoreIsolationBinding(&meta)
	}
	return err
}
