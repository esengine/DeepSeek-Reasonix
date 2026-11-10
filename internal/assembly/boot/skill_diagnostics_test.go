package boot

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/ext/skill"
)

func TestSkillDiagnosticsReachRendererAcrossRediscovery(t *testing.T) {
	root := testenv.TempDir(t)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	var raw bytes.Buffer
	var notices []event.Event
	opts := Options{Stderr: &raw, DiagnosticSink: event.FuncSink(func(e event.Event) { notices = append(notices, e) })}
	assembly := buildSkillAssembly(opts, &config.Config{}, root, true, "")
	notices = nil
	raw.Reset()
	dir := filepath.Join(root, ".reasonix", "skills", "new-warning")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("---\nname: "+name+"\n---\nA runtime skill.\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("new-warning")
	assembly.store.SlashList()
	if raw.Len() != 0 {
		t.Fatalf("runtime diagnostic overwrote terminal stderr: %s", raw.String())
	}
	if len(notices) != 1 || notices[0].Kind != event.Notice || notices[0].Level != event.LevelWarn || !strings.Contains(notices[0].Text, "new-warning") {
		t.Fatalf("renderer notices = %+v", notices)
	}
	assembly.store.SlashList()
	if len(notices) != 1 {
		t.Fatalf("identical diagnostic repeated: %+v", notices)
	}
	write("changed-warning")
	time.Sleep(2100 * time.Millisecond)
	assembly.store.SlashList()
	if len(notices) != 2 || !strings.Contains(notices[1].Text, "changed-warning") {
		t.Fatalf("changed diagnostic lost: %+v", notices)
	}
	assembly.store.ConfigureDiscoverySource(func() *skill.Store {
		return skill.New(skill.Options{ProjectRoot: root, ReasonixHomeDir: home, Stderr: io.Discard})
	})
	assembly.store.SlashList()
	if len(notices) != 2 {
		t.Fatalf("live source repeated diagnostic: %+v", notices)
	}
	write("live-warning")
	assembly.store.SlashList()
	if len(notices) != 3 || !strings.Contains(notices[2].Text, "live-warning") {
		t.Fatalf("live diagnostic lost: %+v", notices)
	}
}

func TestSkillDiagnosticDestinationsAtStartup(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		for _, suppressed := range []bool{false, true} {
			t.Run(fmt.Sprintf("renderer=%t/suppressed=%t", interactive, suppressed), func(t *testing.T) {
				root := testenv.TempDir(t)
				t.Setenv("REASONIX_HOME", testenv.TempDir(t))
				dir := filepath.Join(root, ".reasonix", "skills", "startup-warning")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: startup-warning\n---\nBody.\n"), 0600); err != nil {
					t.Fatal(err)
				}
				var raw bytes.Buffer
				var notices []event.Event
				opts := Options{Stderr: &raw}
				if interactive {
					opts.DiagnosticSink = event.FuncSink(func(e event.Event) { notices = append(notices, e) })
				}
				cfg := &config.Config{}
				cfg.Skills.SuppressWarnings = suppressed
				assembly := buildSkillAssembly(opts, cfg, root, true, "")
				assembly.store.SlashList()
				if interactive && raw.Len() != 0 {
					t.Fatalf("raw stderr: %s", raw.String())
				}
				if suppressed {
					if len(notices) != 0 || raw.Len() != 0 {
						t.Fatalf("suppressed diagnostic emitted: %+v %q", notices, raw.String())
					}
				} else if interactive {
					if len(notices) != 1 {
						t.Fatalf("startup notices = %+v", notices)
					}
				} else if !strings.Contains(raw.String(), "startup-warning") {
					t.Fatalf("missing noninteractive stderr: %q", raw.String())
				}
			})
		}
	}
}
