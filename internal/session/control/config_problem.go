// config_problem.go — the config file the settings surfaces could not read.
package control

import (
	"errors"

	"reasonix/internal/contract/config"
)

// ConfigProblem is what a settings surface shows in place of a save it cannot
// perform: which file, which line, what is on screen instead, and the one
// repair certain enough to offer. It carries no wording of its own — a frontend
// says this in the reader's language from the code the refusal travels under.
type ConfigProblem struct {
	Path      string `json:"path"`
	Line      int    `json:"line,omitempty"`
	Key       string `json:"key,omitempty"`
	Excerpt   string `json:"excerpt,omitempty"`
	Repair    string `json:"repair,omitempty"`
	Recovered string `json:"recovered,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// ConfigProblem reports the user config file this session could not read, or
// nil when it read it.
func (c *Controller) ConfigProblem() *ConfigProblem {
	return ConfigProblemOf(config.LoadForEdit(config.UserConfigPath()).Unparsed())
}

// RepairConfigFile rewrites the unreadable user config and reports where the
// original was kept. The caller rebuilds: the runtime is still assembled from
// whatever it booted with.
func (c *Controller) RepairConfigFile() (string, error) {
	return config.RepairUnparsedConfig(config.UserConfigPath())
}

// ConfigProblemFromError finds the identity inside a save failure, so the panel
// that was refused can show the same thing the banner does.
func ConfigProblemFromError(err error) *ConfigProblem {
	var unparsed *config.UnparsedFile
	if !errors.As(err, &unparsed) {
		return nil
	}
	return ConfigProblemOf(unparsed)
}

// ConfigProblemOf converts the config package's identity into what crosses the
// wire. A nil file is no problem, and reports as one.
func ConfigProblemOf(unparsed *config.UnparsedFile) *ConfigProblem {
	if unparsed == nil {
		return nil
	}
	return &ConfigProblem{
		Path:      unparsed.Path,
		Line:      unparsed.Line,
		Key:       unparsed.Key,
		Excerpt:   unparsed.Excerpt,
		Repair:    unparsed.Repair,
		Recovered: unparsed.Recovered,
		Detail:    unparsed.Error(),
	}
}
