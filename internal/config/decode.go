package config

import (
	"fmt"

	"github.com/BurntSushi/toml"

	fileencoding "reasonix/internal/fileutil/encoding"
)

func decodeTOMLFile(path string, v any) (toml.MetaData, error) {
	resolved, err := resolveConfigReadPath(path)
	if err != nil {
		return toml.MetaData{}, err
	}
	return decodeTOMLFileResolved(resolved, v)
}

func decodeTOMLFileResolved(path string, v any) (toml.MetaData, error) {
	data, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return toml.MetaData{}, err
	}
	return decodeTOMLBytes(data, v)
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
