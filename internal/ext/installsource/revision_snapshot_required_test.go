package installsource

import (
	"errors"
	"reasonix/internal/ext/pluginpkg"
	"testing"
)

func plannedPluginCopy(t *testing.T, tool *Tool, source string) action {
	t.Helper()
	pkg, _, err := pluginpkg.ParseDir(source)
	if err != nil {
		t.Fatal(err)
	}
	act, err := tool.pluginPackageAction(request{Mode: "copy"}, pkg, source)
	if err != nil {
		t.Fatal(err)
	}
	return act
}

func TestRevisionCopyRequiresApprovedSnapshot(t *testing.T) {
	tool, source := revisionPlugin(t)
	act := action{Name: "approved", Source: source, Mode: "copy"}
	if err := tool.applyInstallPluginPackage(t.Context(), request{}, &act); !errors.Is(err, ErrApprovalDenied) {
		t.Fatalf("unbound copy accepted: %v", err)
	}
}
