package hook

import (
	"encoding/json"
	"os"
	"path/filepath"

	fileencoding "reasonix/internal/fileutil/encoding"
)

// Trust gates project hooks. A project's .reasonix/settings.json can run
// arbitrary shell commands, so cloning a repo must not silently execute its
// hooks: project hooks load only after the user explicitly trusts that project
// root. The trust flag lives in user-global state (<Reasonix home>/trust.json),
// NOT in the project file itself — an attacker controls the latter. Global
// hooks (<Reasonix home>/settings.json) are the user's own and always run.

// TrustFilename is the user-global trust store under ~/.reasonix.
const TrustFilename = "trust.json"

type trustFile struct {
	// Projects maps an absolute project root to its trust flag.
	Projects map[string]bool `json:"projects"`
}

// TrustPath is <Reasonix home>/trust.json (homeDir overrides ~ for tests and
// legacy callers).
func TrustPath(homeDir string) string {
	return filepath.Join(reasonixHome(homeDir), TrustFilename)
}

// IsTrusted reports whether projectRoot has been trusted to run its hooks.
func IsTrusted(projectRoot, homeDir string) bool {
	if projectRoot == "" {
		return false
	}
	return readTrust(homeDir).Projects[absRoot(projectRoot)]
}

// Trust marks projectRoot as trusted, persisting the flag. Idempotent.
func Trust(projectRoot, homeDir string) error {
	if projectRoot == "" {
		return nil
	}
	tf := readTrust(homeDir)
	if tf.Projects == nil {
		tf.Projects = map[string]bool{}
	}
	tf.Projects[absRoot(projectRoot)] = true
	return writeTrust(homeDir, tf)
}

func absRoot(root string) string {
	if abs, err := filepath.Abs(root); err == nil {
		return abs
	}
	return root
}

func readTrust(homeDir string) trustFile {
	var tf trustFile
	path := TrustPath(homeDir)
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		if os.IsNotExist(err) {
			if legacy := legacyTrustPath(homeDir); legacy != "" {
				if legacyBytes, legacyErr := fileencoding.ReadFileUTF8(legacy); legacyErr == nil {
					_ = json.Unmarshal(legacyBytes, &tf)
					return tf
				}
			}
		}
		return tf
	}
	_ = json.Unmarshal(b, &tf) // malformed → empty (untrusted), don't crash
	return tf
}

func writeTrust(homeDir string, tf trustFile) error {
	path := TrustPath(homeDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
