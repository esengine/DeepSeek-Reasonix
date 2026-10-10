package skill

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/frontmatter"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginWarningsUseProfileDeliveryRules(t *testing.T) {
	for _, tc := range []struct{ name, declaration, code string }{
		{"absent", "", ""},
		{"review", "delivery:\n  review-report: review", ""},
		{"security", "delivery:\n  review-report: SECURITY", ""},
		{"legacy", "review-report: review", ""},
		{"unknown", "delivery:\n  private_key_marker: review", "profile.delivery.unknown_field"},
		{"scalar", "delivery: private_value_marker", "profile.delivery.mapping_required"},
		{"sequence", "delivery:\n  review-report: [private_value_marker]", "profile.delivery.scalar_required"},
		{"unsupported", "delivery:\n  review-report: private_value_marker", "profile.delivery.unsupported_report"},
		{"authority", "authority:\n  private_key_marker: private_value_marker", "profile.authority.host_owned"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testenv.TempDir(t)
			writeSkill(t, root, pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"profiles","contributes":{"skills":["profiles"],"agents":["profiles"]}}`)
			body := "---\nname: review\ndescription: Review files\n" + tc.declaration + "\n---\nReview the files."
			writeSkill(t, root, "profiles/review.md", body)
			writeSkill(t, root, "ignored/rejected.md", "---\nauthority: claimed\n---\nIgnored.")
			pkg, _, err := pluginpkg.ParseDir(root)
			if err != nil {
				t.Fatal(err)
			}
			warnings := PluginWarnings(pkg)
			st := New(Options{HomeDir: testenv.TempDir(t), CustomPaths: []string{filepath.Join(root, "profiles")}, DisableBuiltins: true, Stderr: io.Discard})
			_, loaded := st.Read("review")
			if tc.code == "" {
				if len(warnings) != 0 || !loaded {
					t.Fatalf("warnings=%v, loaded=%v", warnings, loaded)
				}
				return
			}
			if len(warnings) != 1 || loaded {
				t.Fatalf("warnings=%v, loaded=%v", warnings, loaded)
			}
			assertProfileCode(t, warnings[0], tc.code)
			doc, _ := frontmatter.Parse(body)
			_, err = deliveryFromDocument(doc)
			assertProfileCode(t, err, tc.code)
			if !strings.HasPrefix(warnings[0].Error(), "profiles/review.md: ") {
				t.Fatalf("path projection=%v", warnings[0])
			}
			for _, rejected := range []string{"private_key_marker", "private_value_marker"} {
				if strings.Contains(err.Error(), rejected) || strings.Contains(warnings[0].Error(), rejected) {
					t.Errorf("source echoed in diagnostic: %v / %v", err, warnings[0])
				}
			}
		})
	}
}

func assertProfileCode(t *testing.T, err error, code string) {
	t.Helper()
	var identified interface{ Code() string }
	if !errors.As(err, &identified) || identified.Code() != code {
		t.Errorf("error=%v, want producer identity %s", err, code)
	}
	if err == nil || !strings.Contains(err.Error(), "["+code+"]") {
		t.Errorf("error=%v, missing projected identity %s", err, code)
	}
}

func TestPluginWarningsCoverRuntimeDirectoryAndNestedProfiles(t *testing.T) {
	root := testenv.TempDir(t)
	writeSkill(t, root, pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"profiles","contributes":{"skills":["skills"],"agents":["agents","agents"]}}`)
	const rejected = "---\ndescription: Review files\nauthority:\n  baseline: approved\n---\nReview."
	paths := []string{"agents/review/SKILL.md", "agents/group/nested/SKILL.md", "agents/group/flat.md", "skills/group/check/SKILL.md", "agents/group/deep/review3/SKILL.md", "agents/review/child.md"}
	for _, path := range paths {
		writeSkill(t, root, path, rejected)
	}
	for _, path := range []string{"agents/group/deep/extra/review4/SKILL.md", "agents/scripts/ignored.md", "outside/ignored.md", "agents/valid/hidden.md"} {
		writeSkill(t, root, path, rejected)
	}
	writeSkill(t, root, "agents/valid/SKILL.md", "---\ndescription: Valid stop\n---\nBODY")
	pkg, _, err := pluginpkg.ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var runtimeWarnings strings.Builder
	st := New(Options{HomeDir: testenv.TempDir(t), CustomPaths: append(pkg.SkillRoots(), pkg.AgentRoots()...), DisableBuiltins: true, Stderr: &runtimeWarnings})
	if got := st.List(); len(got) != 1 || got[0].Name != "valid" {
		t.Fatalf("runtime profiles=%+v", got)
	}
	warnings := PluginWarnings(pkg)
	for _, path := range paths {
		if !strings.Contains(runtimeWarnings.String(), filepath.Join(root, filepath.FromSlash(path))) {
			t.Errorf("runtime did not reject %s", path)
		}
		matches := 0
		for _, warning := range warnings {
			if strings.HasPrefix(warning.Error(), path+": ") {
				matches++
				assertProfileCode(t, warning, "profile.authority.host_owned")
			}
		}
		if matches != 1 {
			t.Errorf("%s warnings=%v, want one diagnostic", path, warnings)
		}
	}
	if len(warnings) != len(paths) {
		t.Errorf("warnings=%v, want only declared default-depth profiles", warnings)
	}
	for _, depth := range []int{-1, 0, 1, 3, 5, 9} {
		want := (&config.Config{Skills: config.SkillsConfig{MaxDepth: depth}}).SkillMaxDepth()
		if got := New(Options{MaxDepth: depth}).maxDepth; got != want {
			t.Errorf("depth=%d got=%d owner=%d", depth, got, want)
		}
	}
}

func TestPluginWarningsSkipUnusableSources(t *testing.T) {
	root := testenv.TempDir(t)
	const rejected = "---\ndelivery:\n  review-report: private_value_marker\n---\nBODY"
	writeSkill(t, root, "agents/ordinary.md", rejected)
	writeSkill(t, root, "agents/large.md", rejected+strings.Repeat("x", 1<<20))
	writeSkill(t, root, "agents/bad name.md", rejected)
	writeSkill(t, root, "agents/bad name/SKILL.md", rejected)
	writeSkill(t, root, "agents/nonregular.md/notes.txt", "BODY")
	if runtime.GOOS != "windows" {
		outside := testenv.TempDir(t)
		writeSkill(t, outside, "profile.md", rejected)
		writeSkill(t, root, "resident/source.md", rejected)
		for _, link := range []struct{ name, target string }{
			{"outside.md", filepath.Join(outside, "profile.md")},
			{"relative-outside.md", filepath.Join("..", "..", filepath.Base(outside), "profile.md")},
			{"absolute.md", filepath.Join(root, "resident/source.md")},
			{"resident.md", filepath.Join("..", "resident/source.md")},
			{"broken.md", "missing.md"},
			{"cycle", "."},
		} {
			if err := os.Symlink(link.target, filepath.Join(root, "agents", link.name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	pkg := pluginpkg.Package{Root: root, Manifest: pluginpkg.Manifest{Agents: []string{"agents", "agents"}}}
	warnings := PluginWarnings(pkg)
	want := 2
	if runtime.GOOS != "windows" {
		want++
	}
	if len(warnings) != want {
		t.Fatalf("warnings=%v, want %d usable sources", warnings, want)
	}
	for _, warning := range warnings {
		if !strings.HasPrefix(warning.Error(), "agents/ordinary.md: ") && !strings.HasPrefix(warning.Error(), "agents/resident.md: ") && !strings.HasPrefix(warning.Error(), "agents/bad name/SKILL.md: ") {
			t.Errorf("unexpected source warning=%v", warning)
		}
		assertProfileCode(t, warning, "profile.delivery.unsupported_report")
		if strings.Contains(warning.Error(), "private_value_marker") {
			t.Errorf("source value echoed: %v", warning)
		}
	}
}

func TestPluginWarningsDeclaredFlatSkillRoot(t *testing.T) {
	root := testenv.TempDir(t)
	writeSkill(t, root, "review.md", "---\nauthority: claimed\n---\nBODY")
	pkg := pluginpkg.Package{Root: root, Manifest: pluginpkg.Manifest{Skills: []string{"review.md"}, Agents: []string{"review.md"}}}
	warnings := PluginWarnings(pkg)
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0].Error(), "review.md: ") {
		t.Fatalf("warnings=%v", warnings)
	}
}

func TestPluginWarningsDecodeProfileSource(t *testing.T) {
	for _, encoding := range []fileencoding.Kind{fileencoding.UTF8BOM, fileencoding.UTF16LE, fileencoding.UTF16BE, fileencoding.UTF16LENoBOM} {
		t.Run(fmt.Sprint(encoding), func(t *testing.T) {
			root := testenv.TempDir(t)
			raw, err := fileencoding.Encode("---\r\ndescription: 审阅\r\ndelivery:\r\n  review-report: private_marker\r\n---\r\nBODY", encoding)
			if err != nil {
				t.Fatal(err)
			}
			writeSkillBytes(t, root, "agents/profile.md", raw)
			pkg := pluginpkg.Package{Root: root, Manifest: pluginpkg.Manifest{Agents: []string{"agents"}}}
			warnings := PluginWarnings(pkg)
			if len(warnings) != 1 {
				t.Fatalf("warnings=%v", warnings)
			}
			assertProfileCode(t, warnings[0], "profile.delivery.unsupported_report")
		})
	}
}
