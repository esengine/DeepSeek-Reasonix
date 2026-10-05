package market

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/config"
)

// Record is one market install: which approved version, pinned to which
// digest, and what it put on disk.
type Record struct {
	Slug        string `json:"slug"`
	Kind        string `json:"kind"`
	Version     string `json:"version"`
	ContentHash string `json:"contentHash"`
	// Unreviewed: pinned to the confirmed preview's digest, not a reviewer's.
	Unreviewed bool        `json:"unreviewed,omitempty"`
	Items      []Installed `json:"items"`
	At         string      `json:"at"`
}

// Installed is one thing an install left behind. Target is a path for a skill
// or a plugin; for an MCP server it is the config file that lists Name.
type Installed struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Target string `json:"target"`
}

type ledgerFile struct {
	Records []Record `json:"records"`
}

var ledgerMu sync.Mutex

func ledgerPath(home string) string { return filepath.Join(home, "market", "installed.json") }

func loadLedger(home string) (ledgerFile, error) {
	var out ledgerFile
	raw, err := os.ReadFile(ledgerPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return ledgerFile{}, err
	}
	return out, nil
}

func saveRecord(home string, rec Record) error {
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	file, err := loadLedger(home)
	if err != nil {
		return err
	}
	file.Records = slices.DeleteFunc(file.Records, func(r Record) bool { return r.Slug == rec.Slug })
	file.Records = append(file.Records, rec)
	slices.SortFunc(file.Records, func(a, b Record) int { return strings.Compare(a.Slug, b.Slug) })
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ledgerPath(home)), 0o755); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(ledgerPath(home), raw, 0o644)
}

// InstalledRecords answers which slugs are installed now: a record whose every
// item is still where the install put it. Removing a package elsewhere is
// enough to make its record stop counting, with nobody told.
func InstalledRecords(home string) map[string]Record {
	ledgerMu.Lock()
	file, err := loadLedger(home)
	ledgerMu.Unlock()
	out := map[string]Record{}
	if err != nil {
		return out
	}
	for _, rec := range file.Records {
		if len(rec.Items) > 0 && !slices.ContainsFunc(rec.Items, func(it Installed) bool { return !present(it) }) {
			out[rec.Slug] = rec
		}
	}
	return out
}

func present(it Installed) bool {
	if it.Target == "" {
		return false
	}
	if it.Kind != "mcp" {
		_, err := os.Lstat(it.Target)
		return err == nil
	}
	cfg := config.LoadForEdit(it.Target)
	return cfg != nil && slices.ContainsFunc(cfg.Plugins, func(p config.PluginEntry) bool { return p.Name == it.Name })
}
