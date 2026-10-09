package remotehost

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/platform/remote/bootstrap"
)

func TestServeOccupancyFailuresCarryTheirOwnCode(t *testing.T) {
	kernel, err := os.ReadFile(filepath.Join("..", "..", "..", "desktop", "frontend-next", "src", "i18n", "kernel.ts"))
	if err != nil {
		t.Skipf("no frontend catalogue: %v", err)
	}
	for _, tc := range []struct {
		err  error
		code string
	}{
		{bootstrap.ErrServeProviderMismatch, "remote.serve_provider_mismatch"},
		{bootstrap.ErrServeNotAttachable, "remote.serve_not_attachable"},
	} {
		if got := installFailureCode(fmt.Errorf("pid 7: %w", tc.err)); got != tc.code {
			t.Errorf("code for %v = %q, want %q", tc.err, got, tc.code)
		}
		if !strings.Contains(string(kernel), `"`+tc.code+`":`) {
			t.Errorf("%s has no wording in kernel.ts", tc.code)
		}
	}
}
