package isolation

import (
	"fmt"
	"os"
	"path/filepath"
)

// The entry lock serializes ID allocation across all workspaces in this root.
func (s *Store) reserveIdentity(id string) error {
	present, err := s.containsResultID(id)
	if err != nil {
		return err
	}
	if present {
		return fmt.Errorf("isolated identity already exists: %s", id)
	}
	f, err := os.OpenFile(filepath.Join(s.managedRoot, "."+id+".allocated"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	return f.Close()
}
