package config

import (
	"fmt"

	"github.com/BurntSushi/toml"

	fileencoding "reasonix/internal/base/fileutil/encoding"
)

// mergeFileSnapshot decodes one immutable read of a TOML file onto cfg and
// returns metadata from those exact bytes. Callers that derive source or
// precedence decisions from metadata must use this result instead of reading
// the path again: a config file may be atomically replaced between reads.
func mergeFileSnapshot(cfg *Config, path string) (toml.MetaData, error) {
	return configReadPaths(nil).mergeFileSnapshot(cfg, path)
}

func readTOMLFileBytes(path string) ([]byte, error) {
	resolved, err := resolveConfigReadPath(path)
	if err != nil {
		return nil, err
	}
	return fileencoding.ReadFileUTF8(resolved)
}

func decodeTOMLFile(path string, v any) (toml.MetaData, error) {
	data, err := readTOMLFileBytes(path)
	if err != nil {
		return toml.MetaData{}, err
	}
	return decodeTOMLBytes(data, v)
}

func mergeFileSnapshotWithRead(cfg *Config, path string, readFile func(string) ([]byte, error)) (toml.MetaData, error) {
	return configReadPaths(nil).mergeFileSnapshotWithRead(cfg, path, readFile)
}

func decodeTOMLBytes(data []byte, v any) (toml.MetaData, error) {
	text := string(fileencoding.DecodeToUTF8(data))
	meta, err := toml.Decode(text, v)
	if err != nil {
		return meta, err
	}
	if cfg, ok := v.(*Config); ok {
		if err := isolateDecodedProviders(text, meta, cfg); err != nil {
			return meta, err
		}
	}
	return meta, nil
}

// isolateDecodedProviders stops one vendor's defaults from being written onto
// another vendor's provider entry.
//
// TOML array-of-tables decoding is positional: BurntSushi/toml unifies
// [[providers]][i] onto whatever the destination slice already holds at index i
// instead of onto a zero value. Every config load seeds from Default(), which
// ships two official DeepSeek entries, so a user's first two [[providers]]
// inherit every DeepSeek field they did not set themselves — balance_url,
// the USD price table and context_window = 1000000 (#7357, #7358).
//
// Re-decoding the same bytes onto an empty slice makes a declared provider list
// carry only what the file declares. Defaults that are genuinely correct for an
// entry are then reapplied by the explicit backfill helpers, which key on the
// endpoint rather than on list position.
func isolateDecodedProviders(text string, meta toml.MetaData, cfg *Config) error {
	if cfg == nil || len(cfg.Providers) == 0 || !meta.IsDefined("providers") {
		return nil
	}
	var isolated struct {
		Providers []ProviderEntry `toml:"providers"`
	}
	if _, err := toml.Decode(text, &isolated); err != nil {
		return fmt.Errorf("decode providers: %w", err)
	}
	cfg.Providers = isolated.Providers
	return nil
}

func (readPaths configReadPaths) mergeFileSnapshot(cfg *Config, path string) (toml.MetaData, error) {
	if readPaths == nil {
		return readPaths.mergeFileSnapshotWithRead(cfg, path, fileencoding.ReadFileUTF8)
	}
	return readPaths.mergeFileSnapshotWithRead(cfg, path, nil)
}

func (readPaths configReadPaths) mergeFileSnapshotWithRead(cfg *Config, path string, readFile func(string) ([]byte, error)) (toml.MetaData, error) {
	resolved, exists, err := readPaths.stat(path)
	if err != nil {
		return toml.MetaData{}, err
	}
	if !exists {
		return toml.MetaData{}, nil
	}
	var data []byte
	if readFile == nil {
		data, err = readPaths.readFile(path)
	} else {
		data, err = readFile(resolved)
	}
	if err != nil {
		return toml.MetaData{}, fmt.Errorf("config %s: %w", path, err)
	}
	// Validate before merging identical bytes: TOML decoding can mutate earlier
	// fields before a later type error. Metadata and source provenance must use
	// the same transactional snapshot.
	var validated Config
	if _, err := decodeTOMLBytes(data, &validated); err != nil {
		return toml.MetaData{}, fmt.Errorf("config %s: %w", path, err)
	}
	meta, err := decodeTOMLBytes(data, cfg)
	if err != nil {
		return toml.MetaData{}, fmt.Errorf("config %s: %w", path, err)
	}
	if meta.IsDefined("providers") {
		var persisted Config
		if _, err := decodeTOMLBytes(data, &persisted); err != nil {
			return toml.MetaData{}, fmt.Errorf("config %s: %w", path, err)
		}
		markPersistedDeepSeekOfficialPricing(&persisted)
		markers := map[string]string{}
		for i := range persisted.Providers {
			markers[providerMergeKey(persisted.Providers[i])] = persisted.Providers[i].persistedOfficialCurrency
		}
		for i := range cfg.Providers {
			cfg.Providers[i].persistedOfficialCurrency = markers[providerMergeKey(cfg.Providers[i])]
		}
	}
	return meta, nil
}
