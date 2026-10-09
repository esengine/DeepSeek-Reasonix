package serve

import (
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/config"
)

func TestStatusDoesNotLoadConfiguration(t *testing.T) {
	srv := newRichProviderServer(t)

	var loads atomic.Int32
	prev := config.SetInstalledPackages(func(string) []config.InstalledPackage {
		loads.Add(1)
		return nil
	})
	t.Cleanup(func() { config.SetInstalledPackages(prev) })

	var status map[string]any
	for range 5 {
		status = nil
		getJSON(t, srv.URL+"/status", &status)
	}
	if n := loads.Load(); n != 0 {
		t.Fatalf("5 status reads loaded the full configuration %d times, want 0", n)
	}
	if status["modelRef"] != "rich/alpha" || status["effort"] == nil || status["vision"] == nil || status["visionDeclared"] == nil {
		t.Fatalf("status lost the model facts: %v", status)
	}
}
