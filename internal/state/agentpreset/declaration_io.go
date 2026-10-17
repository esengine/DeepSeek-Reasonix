package agentpreset

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/agentcomposition"
)

const maxDefinitionBytes = 4 << 20

func (s *Store) lockDeclaration(id, path string) (func(), error) {
	if strings.HasPrefix(id, "project:") {
		path = filepath.Join(s.directory("user"), "project-locks", agentcomposition.Digest(path))
	}
	return lock(path)
}

func readDeclarationFile(file *os.File) ([]byte, error) {
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxDefinitionBytes {
		return nil, agentcomposition.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDefinitionBytes+1))
	if len(data) > maxDefinitionBytes {
		return nil, agentcomposition.ErrInvalid
	}
	return data, err
}

func (s *Store) readDeclaration(id, path string) ([]byte, error) {
	if strings.HasPrefix(id, "project:") {
		root, err := os.OpenRoot(s.Workspace)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		rel, err := filepath.Rel(s.Workspace, path)
		if err != nil {
			return nil, err
		}
		file, err := root.Open(rel)
		if err != nil {
			return nil, err
		}
		return readDeclarationFile(file)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return readDeclarationFile(file)
}

func (s *Store) writeDeclaration(path string, d agentcomposition.Definition) error {
	if !strings.HasPrefix(d.ID, "project:") {
		return writeJSON(path, d)
	}
	root, err := os.OpenRoot(s.Workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	rel, err := filepath.Rel(s.Workspace, path)
	if err != nil {
		return err
	}
	if err := root.MkdirAll(filepath.Dir(rel), 0o700); err != nil {
		return err
	}
	temp := filepath.Join(filepath.Dir(rel), ".preset-"+rand.Text())
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temp) }()
	data, err := json.MarshalIndent(d, "", "  ")
	if err == nil {
		_, err = file.Write(append(data, '\n'))
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(temp, rel)
}

func (s *Store) removeDeclaration(id, path string) error {
	if !strings.HasPrefix(id, "project:") {
		return os.Remove(path)
	}
	root, err := os.OpenRoot(s.Workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	rel, err := filepath.Rel(s.Workspace, path)
	if err != nil {
		return err
	}
	return root.Remove(rel)
}

func (s *Store) checkProjectDeclaration(path string) error {
	rel, err := filepath.Rel(s.Workspace, path)
	if err != nil || !filepath.IsLocal(rel) {
		return agentcomposition.ErrInvalid
	}
	current := s.Workspace
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: project preset path crosses a symbolic link", agentcomposition.ErrInvalid)
		}
	}
	return nil
}
