package doctor

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

// The built-in review skills allow use_capability, which assembly always
// registers; doctor warned that it was missing on every install.
func TestDoctorDoesNotFlagTheCapabilityProxyAsMissing(t *testing.T) {
	t.Setenv("REASONIX_HOME", filepath.Join(testenv.TempDir(t), "reasonix"))
	t.Chdir(testenv.TempDir(t))
	report := Collect(Options{Version: "test", Config: config.Default()})
	for _, w := range report.Warnings {
		if strings.Contains(w, "use_capability") {
			t.Fatalf("doctor warned about the always-registered proxy: %s", w)
		}
	}
}
