package teamroster

import "path/filepath"

// GroupPaths returns the canonical log and round paths for a validated group id.
func (s Store) GroupPaths(group string) (log, round string, err error) {
	dir, err := s.groupDir(group)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(dir, GroupLogFile), filepath.Join(dir, RoundFile), nil
}
