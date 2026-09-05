// Package instanceidentity owns the Desktop data-home identity across processes.
package instanceidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"reasonix/internal/pathidentity"
)

const Prefix = "com.reasonix.desktop"
const UpdateEnvironmentKey = "REASONIX_UPDATE_INSTANCE_ID"

var trayNamespace = uuid.MustParse("af8b2b6e-cf17-43b9-afb9-b0bf2695d8ac")

func CanonicalHome(home string) string {
	home = strings.TrimSpace(home)
	if home == "" {
		return ""
	}
	return filepath.Dir(pathidentity.Canonical(filepath.Join(home, ".reasonix-home.identity")))
}

func ForHome(home string) string {
	home = CanonicalHome(home)
	if home == "" {
		return Prefix
	}
	sum := sha256.Sum256([]byte(home))
	return Prefix + "." + hex.EncodeToString(sum[:8])
}

func Valid(id string) bool {
	suffix, ok := strings.CutPrefix(id, Prefix+".")
	if !ok || len(suffix) != 16 {
		return false
	}
	_, err := hex.DecodeString(suffix)
	return err == nil && suffix == strings.ToLower(suffix)
}

func TrayGUID(id string) string { return "{" + uuid.NewSHA1(trayNamespace, []byte(id)).String() + "}" }

// UpdateEnvironment freezes relative data homes before the helper changes cwd.
func UpdateEnvironment(base []string, home string) []string {
	home = CanonicalHome(home)
	env := make([]string, 0, len(base)+2)
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "REASONIX_HOME") && !strings.EqualFold(key, UpdateEnvironmentKey) {
			env = append(env, entry)
		}
	}
	return append(env, "REASONIX_HOME="+home, UpdateEnvironmentKey+"="+ForHome(home))
}

func UpdateID() string { return os.Getenv(UpdateEnvironmentKey) }
