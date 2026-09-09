package config

import (
	"bytes"
	"fmt"
	"os"
	"reflect"

	"github.com/BurntSushi/toml"
	fileencoding "reasonix/internal/fileutil/encoding"
)

// ModelSettingsBaseline must be captured before editing under the config lock.
func (c *Config) ModelSettingsBaseline() string { return RenderTOMLForScope(c, RenderScopeUser) }

// SaveModelSettingsTo applies only the typed changes to the original document.
// Unknown top-level and provider fields survive, including nested future fields.
// A new file still receives the standard annotated template.
func (c *Config) SaveModelSettingsTo(path, baseline string) error {
	if c == nil {
		return fmt.Errorf("save model settings: nil config")
	}
	if c.editLoadErr != nil {
		return c.editLoadErr
	}
	if err := currentUserConfigEditLockError(); err != nil {
		return err
	}
	resolved, err := resolveConfigAccessPath(path, true)
	if err != nil {
		return err
	}
	raw, err := fileencoding.ReadFileUTF8(resolved)
	if os.IsNotExist(err) {
		return c.SaveTo(path)
	}
	if err != nil {
		return err
	}
	doc, before, after := map[string]any{}, map[string]any{}, map[string]any{}
	for _, input := range []struct {
		body string
		dest *map[string]any
	}{{string(raw), &doc}, {baseline, &before}, {c.ModelSettingsBaseline(), &after}} {
		if _, err := toml.Decode(input.body, input.dest); err != nil {
			return err
		}
	}
	mergeModelSettingsDelta(doc, before, after)
	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(doc); err != nil {
		return err
	}
	return writeConfigFileResolved(resolved, encoded.String(), configFilePerm(path))
}

func mergeModelSettingsDelta(doc, before, after map[string]any) {
	for key, previous := range before {
		next, exists := after[key]
		if !exists {
			delete(doc, key)
			continue
		}
		if reflect.DeepEqual(previous, next) {
			continue
		}
		oldTable, oldOK := previous.(map[string]any)
		newTable, newOK := next.(map[string]any)
		if oldOK && newOK {
			target, ok := doc[key].(map[string]any)
			if !ok {
				target = map[string]any{}
			}
			mergeModelSettingsDelta(target, oldTable, newTable)
			doc[key] = target
			continue
		}
		if key == "providers" {
			oldEntries, oldOK := previous.([]map[string]any)
			newEntries, newOK := next.([]map[string]any)
			if oldOK && newOK {
				rawEntries, _ := doc[key].([]map[string]any)
				doc[key] = mergeModelProviderEntries(rawEntries, oldEntries, newEntries)
				continue
			}
		}
		doc[key] = next
	}
	for key, next := range after {
		if _, exists := before[key]; !exists {
			doc[key] = next
		}
	}
}

func mergeModelProviderEntries(raw, before, after []map[string]any) []map[string]any {
	index := func(entries []map[string]any) map[string]map[string]any {
		result := map[string]map[string]any{}
		for _, entry := range entries {
			if name, ok := entry["name"].(string); ok {
				result[name] = entry
			}
		}
		return result
	}
	rawByName, beforeByName := index(raw), index(before)
	result := make([]map[string]any, 0, len(after))
	for _, entry := range after {
		name, _ := entry["name"].(string)
		target, exists := rawByName[name]
		if !exists {
			target = map[string]any{}
		}
		mergeModelSettingsDelta(target, beforeByName[name], entry)
		// A default provider newly materialized by an edit needs its identity.
		if !exists {
			target = entry
		}
		result = append(result, target)
	}
	return result
}
