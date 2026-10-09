package pluginpkg

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/testenv"
)

func countSkillReads(t *testing.T) func() map[string]int {
	t.Helper()
	var mu sync.Mutex
	reads := map[string]int{}
	prev := readSkillFile
	readSkillFile = func(path string) ([]byte, error) {
		b, err := fileencoding.ReadFileUTF8(path)
		if err == nil {
			mu.Lock()
			reads[path]++
			mu.Unlock()
		}
		return b, err
	}
	t.Cleanup(func() { readSkillFile = prev })
	return func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		return maps.Clone(reads)
	}
}

func writeSkills(t *testing.T, root string, n int) {
	t.Helper()
	for i := range n {
		writeTestFile(t, filepath.Join(root, "skills", fmt.Sprintf("skill-%03d", i), "SKILL.md"), "---\ndescription: d\n---\nbody")
	}
}

func TestParseDirOpensEachSkillFileOnce(t *testing.T) {
	const n = 12
	for _, tc := range []struct{ label, manifest, body string }{
		{"claude", ClaudeManifest, `{"name":"scan-kit"}`},
		{"codex", CodexManifest, `{"name":"scan-kit","skills":"skills"}`},
		{"native v2", NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"scan-kit","contributes":{"skills":["skills"]}}`},
	} {
		t.Run(tc.label, func(t *testing.T) {
			root := testenv.TempDir(t)
			writeTestFile(t, filepath.Join(root, tc.manifest), tc.body)
			writeSkills(t, root, n)
			snapshot := countSkillReads(t)
			pkg, _, err := ParseDir(root)
			if err != nil {
				t.Fatal(err)
			}
			reads := snapshot()
			if len(reads) != n {
				t.Fatalf("opened %d distinct skill files, want %d", len(reads), n)
			}
			for path, c := range reads {
				if c != 1 {
					t.Errorf("%s opened %d times in one ParseDir", path, c)
				}
			}
			if got, _, _, _ := pkg.CapabilityCounts(); got != n {
				t.Fatalf("CapabilityCounts skills=%d, want %d", got, n)
			}
			if pkg.skills != nil {
				t.Fatal("ParseDir leaked its scan into the returned Package")
			}
		})
	}
}

func TestParsedPackageRescansDisk(t *testing.T) {
	root := testenv.TempDir(t)
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name":"scan-kit"}`)
	writeSkills(t, root, 2)
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "skills", "skill-000")); err != nil {
		t.Fatal(err)
	}
	if got, _, _, _ := pkg.CapabilityCounts(); got != 1 {
		t.Fatalf("CapabilityCounts after removal = %d, want 1", got)
	}
}

func TestParseDirKeepsSkillNameWarnings(t *testing.T) {
	root := testenv.TempDir(t)
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name":"scan-kit"}`)
	writeTestFile(t, filepath.Join(root, "skills", "a", "SKILL.md"), "---\nname: same\n---\nx")
	writeTestFile(t, filepath.Join(root, "skills", "b", "SKILL.md"), "---\nname: same\n---\nx")
	_, warnings, err := ParseDir(root)
	if err != nil || len(warnings) != 1 {
		t.Fatalf("warnings=%v err=%v, want one shadow warning", warnings, err)
	}
}

func validNativePlugin(t *testing.T, home, name string) InstalledPlugin {
	t.Helper()
	rel := filepath.Join("plugins", name)
	root := filepath.Join(home, rel)
	writeTestFile(t, filepath.Join(root, NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"`+name+`","contributes":{"skills":["skills"]}}`)
	writeTestFile(t, filepath.Join(root, "skills", "one", "SKILL.md"), "---\ndescription: d\n---\nbody")
	return InstalledPlugin{Name: name, Root: rel, Enabled: true}
}

func brokenPlugin(t *testing.T, home, name string) InstalledPlugin {
	t.Helper()
	rel := filepath.Join("plugins", name)
	writeTestFile(t, filepath.Join(home, rel, NativeManifest), `{not json`)
	return InstalledPlugin{Name: name, Root: rel, Enabled: true}
}

func TestLoadInstalledDoesNotHoldStateLockWhileParsing(t *testing.T) {
	home := testenv.TempDir(t)
	if err := Upsert(home, validNativePlugin(t, home, "slow")); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	prev := readSkillFile
	readSkillFile = func(path string) ([]byte, error) {
		once.Do(func() { close(entered); <-release })
		return fileencoding.ReadFileUTF8(path)
	}
	t.Cleanup(func() { readSkillFile = prev })

	loaded := make(chan []InstalledPackage, 1)
	go func() { out, _ := LoadInstalled(home); loaded <- out }()
	<-entered
	writes := make(chan error, 1)
	go func() { writes <- Upsert(home, validNativePlugin(t, home, "late")) }()
	select {
	case err := <-writes:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("Upsert blocked behind a LoadInstalled that was still parsing")
	}
	close(release)
	out := <-loaded
	if len(out) != 1 || out[0].Installed.Name != "slow" {
		t.Fatalf("load overlapping an install must describe its snapshot, got %+v", out)
	}
	if out, _ := LoadInstalled(home); len(out) != 2 {
		t.Fatalf("next load must see the install, got %d packages", len(out))
	}
}

func TestLoadInstalledDoesNotApplyStatusToReplacedEntry(t *testing.T) {
	home := testenv.TempDir(t)
	if err := Upsert(home, brokenPlugin(t, home, "a-bad")); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(home, validNativePlugin(t, home, "z-slow")); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	prev := readSkillFile
	readSkillFile = func(path string) ([]byte, error) {
		once.Do(func() { close(entered); <-release })
		return fileencoding.ReadFileUTF8(path)
	}
	t.Cleanup(func() { readSkillFile = prev })

	done := make(chan struct{})
	go func() { LoadInstalled(home); close(done) }()
	<-entered
	replaced := validNativePlugin(t, home, "a-good")
	replaced.Name = "a-bad"
	if err := Upsert(home, replaced); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	<-done
	st, err := LoadState(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range st.Plugins {
		if p.Name == "a-bad" && (p.Status != "" || p.StatusReason != "" || p.Root != replaced.Root) {
			t.Fatalf("verdict about the old root reached the replacement: %+v", p)
		}
	}
}

func TestLoadInstalledPersistsStatusAndSurvivesConcurrentInstalls(t *testing.T) {
	home := testenv.TempDir(t)
	if err := Upsert(home, brokenPlugin(t, home, "broken")); err != nil {
		t.Fatal(err)
	}
	const n = 8
	var wg sync.WaitGroup
	for i := range n {
		name := fmt.Sprintf("p-%02d", i)
		plugin := validNativePlugin(t, home, name)
		wg.Go(func() {
			if err := Upsert(home, plugin); err != nil {
				t.Errorf("Upsert: %v", err)
			}
			if err := SetEnabled(home, name, true); err != nil {
				t.Errorf("SetEnabled: %v", err)
			}
		})
		wg.Go(func() { LoadInstalled(home) })
	}
	wg.Wait()
	LoadInstalled(home)
	st, err := LoadState(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Plugins) != n+1 {
		t.Fatalf("got %d plugins, want %d", len(st.Plugins), n+1)
	}
	for _, p := range st.Plugins {
		switch {
		case p.Name == "broken" && p.Status != PluginStatusDisabledIncompatible:
			t.Errorf("broken plugin status = %q", p.Status)
		case p.Name != "broken" && (p.Status != "" || !p.Enabled):
			t.Errorf("%s: status=%q enabled=%v", p.Name, p.Status, p.Enabled)
		}
	}
}

func TestLoadInstalledDoesNotApplyStatusToReinstallOnSameRoot(t *testing.T) {
	home := testenv.TempDir(t)
	if err := Upsert(home, brokenPlugin(t, home, "a-bad")); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(home, validNativePlugin(t, home, "z-slow")); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	prev := readSkillFile
	readSkillFile = func(path string) ([]byte, error) {
		once.Do(func() { close(entered); <-release })
		return fileencoding.ReadFileUTF8(path)
	}
	t.Cleanup(func() { readSkillFile = prev })

	done := make(chan struct{})
	go func() { LoadInstalled(home); close(done) }()
	<-entered
	fixed := validNativePlugin(t, home, "a-bad")
	if err := Upsert(home, fixed); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	<-done
	st, err := LoadState(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range st.Plugins {
		if p.Name == "a-bad" && (p.Status != "" || p.StatusReason != "") {
			t.Fatalf("verdict about the old install reached the reinstall: %+v", p)
		}
	}
}
