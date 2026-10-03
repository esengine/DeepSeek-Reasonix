package skill

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginWarningsUseProfileDeliveryRules(t *testing.T) {
	for _, tc := range []struct {
		name, declaration, warning string
	}{
		{"absent", "", ""},
		{"review", "delivery:\n  review-report: review", ""},
		{"security", "delivery:\n  review-report: SECURITY", ""},
		{"legacy", "review-report: review", ""},
		{"unknown", "delivery:\n  typo: review", `unknown delivery field "typo"`},
		{"scalar", "delivery: review", "`delivery:` must be a block"},
		{"sequence", "delivery:\n  review-report: [review, security]", "takes one value"},
		{"unsupported", "delivery:\n  review-report: unsupported", "accepted: review, security"},
		{"authority", "authority:\n  baseline: accepted", "`authority:` is host-owned"},
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
			if tc.warning == "" {
				if len(warnings) != 0 || !loaded {
					t.Fatalf("warnings=%v, loaded=%v", warnings, loaded)
				}
				return
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0], "profiles/review.md: ") || !strings.Contains(warnings[0], tc.warning) || loaded {
				t.Fatalf("warnings=%v, loaded=%v, want %q once and rejected profile", warnings, loaded, tc.warning)
			}
		})
	}
}

func TestPluginWarningsCoverRuntimeDirectoryAndNestedProfiles(t *testing.T) {
	root := testenv.TempDir(t)
	writeSkill(t, root, pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"profiles","contributes":{"skills":["skills"],"agents":["agents","agents"]}}`)
	const rejected = "---\ndescription: Review files\nauthority:\n  baseline: approved\n---\nReview."
	paths := []string{"agents/review/SKILL.md", "agents/group/nested/SKILL.md", "agents/group/flat.md", "skills/group/check/SKILL.md", "agents/group/deep/extra/review5/SKILL.md"}
	for _, path := range paths {
		writeSkill(t, root, path, rejected)
	}
	writeSkill(t, root, "agents/scripts/ignored.md", rejected)
	writeSkill(t, root, "outside/ignored.md", rejected)
	pkg, _, err := pluginpkg.ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var runtimeWarnings strings.Builder
	st := New(Options{HomeDir: testenv.TempDir(t), CustomPaths: append(pkg.SkillRoots(), pkg.AgentRoots()...), MaxDepth: 5, DisableBuiltins: true, Stderr: &runtimeWarnings})
	if got := st.List(); len(got) != 0 {
		t.Fatalf("rejected runtime profiles loaded: %+v", got)
	}
	warnings := PluginWarnings(pkg)
	for _, path := range paths {
		if !strings.Contains(runtimeWarnings.String(), filepath.Join(root, filepath.FromSlash(path))) {
			t.Errorf("runtime did not reject %s: %s", path, runtimeWarnings.String())
		}
		matches := 0
		for _, warning := range warnings {
			if strings.HasPrefix(warning, path+": ") && strings.Contains(warning, "`authority:` is host-owned") {
				matches++
			}
		}
		if matches != 1 {
			t.Errorf("%s warnings=%v, want one runtime rejection diagnostic", path, warnings)
		}
	}
	if len(warnings) != len(paths) {
		t.Errorf("warnings=%v, want only declared runtime profiles", warnings)
	}
}
