package isolation

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const settlementFenceDirectory = ".settlements-v1"

// This reader never interprets settlement bytes. Their presence fences ordinary
// legacy operations until a compatible recovery owner settles the record.
func (s *Store) checkSettlementFence(id string) error {
	managed, err := os.OpenRoot(s.managedRoot)
	if err != nil {
		return fmt.Errorf("inspect settlement namespace: %w", err)
	}
	defer managed.Close()
	info, err := managed.Lstat(settlementFenceDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect settlement namespace: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("settlement namespace is not a directory; files retained: %w", os.ErrInvalid)
	}
	records, err := managed.OpenRoot(settlementFenceDirectory)
	if err != nil {
		return fmt.Errorf("open settlement namespace: %w", err)
	}
	defer records.Close()
	name := strings.TrimSuffix(resultLockName(id), ".lock") + ".json"
	if _, err := records.Lstat(name); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect settlement record: %w", err)
	}
	return fmt.Errorf("isolated result has a durable settlement record; compatible recovery required, files retained")
}
