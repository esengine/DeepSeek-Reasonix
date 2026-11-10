package serve

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/agentpreset"
	"reasonix/internal/state/sessionstore"
)

func TestLiveCompositionSwitchResourceCost(t *testing.T) {
	if os.Getenv("REASONIX_PRESET_BENCHMARK") != "1" {
		t.Skip("opt-in repeated measurement; deterministic resource guards run separately")
	}
	for _, files := range []int{0, 4096} {
		t.Run(fmt.Sprintf("files-%d", files), func(t *testing.T) {
			home, root := testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			t.Chdir(root)
			closeSharedCatalogsOnCleanup(t)
			skillRoot := filepath.Join(home, "skills", "templates")
			writePluginFile(t, filepath.Join(skillRoot, "SKILL.md"), "---\nname: templates\ndescription: template library\n---\nLoad individual templates when needed.")
			writePluginFile(t, filepath.Join(root, "reasonix.toml"), "[codegraph]\nenabled=false\n[environment]\nenabled=false\n")
			if _, err := config.RootsForHome(home).ApproveWorkspacePrograms(root); err != nil {
				t.Fatal(err)
			}
			payload := make([]byte, 16<<10)
			for i := range files {
				writePluginFile(t, filepath.Join(skillRoot, "templates", fmt.Sprintf("%04d.bin", i)), string(payload))
			}
			d := agentcomposition.Builtins()[0]
			d.ID = "user:performance"
			if _, err := agentpreset.New(home, root).Save(d, ""); err != nil {
				t.Fatal(err)
			}
			resolver := &provider.StaticResolver{
				Descriptors: []provider.Descriptor{{Ref: "neutral/fixture", Model: "fixture", Default: true}},
				Providers:   map[string]provider.Provider{"neutral/fixture": credentialPreviewProvider{}},
			}
			for sample := range 20 {
				bc := NewBroadcaster()
				initial, err := boot.BuildRuntime(t.Context(), boot.Options{Home: home, WorkspaceRoot: root, Model: "neutral/fixture", ProviderResolver: resolver, Sink: bc})
				if err != nil {
					t.Fatal(err)
				}
				s := New(initial.Controller, bc, config.ServeConfig{})
				s.resolver = resolver
				s.AdoptRuntime(initial)
				leases := control.NewSessionLeaseKeeper()
				s.SetSessionLeases(leases)
				for _, phase := range []string{"first-switch", "repeat-switch"} {
					runtime.GC()
					var before, after runtime.MemStats
					runtime.ReadMemStats(&before)
					start := time.Now()
					if err := s.ctl().SelectAgentComposition(t.Context(), d.ID); err != nil {
						t.Fatal(err)
					}
					elapsed := time.Since(start)
					runtime.ReadMemStats(&after)
					binding := s.ctl().CompositionBinding()
					if !binding.Live() || len(binding.Resources) != 0 {
						t.Fatal("switch retained resource payload")
					}
					data, _ := json.Marshal(binding)
					var persisted int64
					for _, path := range []string{s.ctl().SessionPath(), sessionstore.SessionEventLogPath(s.ctl().SessionPath())} {
						info, err := os.Stat(path)
						if err != nil {
							t.Fatal(err)
						}
						persisted += info.Size()
					}
					t.Logf("PRESET_COST files=%d sample=%d phase=%s ms=%.3f allocated_bytes=%d binding_bytes=%d persisted_bytes=%d", files, sample, phase, float64(elapsed)/float64(time.Millisecond), after.TotalAlloc-before.TotalAlloc, len(data), persisted)
					if phase == "first-switch" {
						if err := s.ctl().SelectAgentComposition(t.Context(), "builtin:general"); err != nil {
							t.Fatal(err)
						}
					}
				}
				target, ok := s.lastBuild.Assembly.Registry.Get("read_skill")
				if !ok {
					t.Fatal("skill entry missing")
				}
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				start := time.Now()
				if _, err := target.Execute(t.Context(), json.RawMessage(`{"name":"templates"}`)); err != nil {
					t.Fatal(err)
				}
				elapsed := time.Since(start)
				runtime.ReadMemStats(&after)
				t.Logf("PRESET_COST files=%d sample=%d phase=first-use ms=%.3f allocated_bytes=%d", files, sample, float64(elapsed)/float64(time.Millisecond), after.TotalAlloc-before.TotalAlloc)
				s.ctl().Close()
				leases.Release()
			}
		})
	}
}
