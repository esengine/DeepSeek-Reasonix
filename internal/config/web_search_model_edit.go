package config

import (
	"fmt"
	"os"
	fileencoding "reasonix/internal/fileutil/encoding"
)

// SaveWebSearchModelTo preserves comments and unknown fields in an existing user
// file. The caller holds the same config edit lock used by all Desktop setters.
func (c *Config) SaveWebSearchModelTo(path string) error {
	if c == nil {
		return fmt.Errorf("save search model: nil config")
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
	body := upsertTOMLSectionKey(string(raw), "agent", "web_search_model", fmt.Sprintf("web_search_model = %q", c.Agent.WebSearchModel))
	return writeConfigFileResolved(resolved, body, configFilePerm(path))
}
