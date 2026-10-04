package pluginpkg

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"reasonix/internal/base/fileutil"
)

func SaveState(reasonixHome string, st State) error {
	return saveStateValidated(reasonixHome, st, nil)
}

// ErrPublicationFailed identifies a registry pointer that was not published.
var ErrPublicationFailed = errors.New("plugin package publication failed")

func saveStateValidated(reasonixHome string, st State, validate func() error) error {
	if st.Version == 0 {
		st.Version = 1
	}
	slices.SortStableFunc(st.Plugins, func(a, b InstalledPlugin) int { return cmp.Compare(a.Name, b.Name) })
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := fileutil.AtomicWriteFileStrictValidated(StatePath(reasonixHome), b, 0o644, validate); err != nil {
		return fmt.Errorf("%w: %w", ErrPublicationFailed, err)
	}
	return nil
}

// UpsertValidated verifies the referenced material immediately before publishing
// the registry pointer, with the same state lock as other registry mutations.
func UpsertValidated(reasonixHome string, p InstalledPlugin, validate func() error) error {
	if !IsValidName(p.Name) {
		return fmt.Errorf("invalid plugin name %q", p.Name)
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	st, err := LoadState(reasonixHome)
	if err != nil {
		return err
	}
	for i := range st.Plugins {
		if st.Plugins[i].Name == p.Name {
			st.Plugins[i] = p
			return saveStateValidated(reasonixHome, st, validate)
		}
	}
	st.Plugins = append(st.Plugins, p)
	return saveStateValidated(reasonixHome, st, validate)
}
