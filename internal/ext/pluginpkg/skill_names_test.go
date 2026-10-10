package pluginpkg_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/ext/skill"
)

func TestPackageSkillNamesMatchRuntime(t *testing.T) {
	for _, tc := range []struct {
		label, stem, frontName, want string
		flat                         bool
	}{
		{label: "Chinese folder", stem: "审查", want: "审查"},
		{label: "Japanese flat", stem: "点検", want: "点検", flat: true},
		{label: "decomposed folder", stem: "cafe\u0301", want: "café"},
		{label: "Unicode override", stem: "审查", frontName: "点検", want: "点検"},
		{label: "decomposed override", stem: "审查", frontName: "re\u0301vision", want: "révision"},
		{label: "ASCII stem compatibility", stem: "review", frontName: "审查", want: "review"},
		{label: "ASCII override", stem: "review", frontName: "inspect", want: "inspect"},
		{label: "spaced override", stem: "review", frontName: `" inspect "`, want: "inspect"},
		{label: "64 runes", stem: strings.Repeat("审", 64), want: strings.Repeat("审", 64)},
		{label: "65 runes", stem: strings.Repeat("审", 65), flat: true},
		{label: "leading mark", stem: "\u0301review", flat: true},
		{label: "invisible stem", stem: "rev\u200diew", flat: true},
		{label: "variation selector", stem: "审\ufe0f", flat: true},
		{label: "invalid override", stem: "审查", frontName: "bad/name", want: "审查"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			root, home := testenv.TempDir(t), testenv.TempDir(t)
			write := func(rel, body string) {
				t.Helper()
				path := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write(pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"names-kit","contributes":{"skills":["skills"]}}`)
			path := filepath.Join("skills", tc.stem, "SKILL.md")
			if tc.flat {
				path = filepath.Join("skills", tc.stem+".md")
			}
			write(path, "---\nname: "+tc.frontName+"\ndescription: Skill name fixture\n---\nBODY")
			pkg, warnings, err := pluginpkg.ParseDir(root)
			if err != nil || len(warnings) != 0 {
				t.Fatalf("ParseDir: warnings=%v err=%v", warnings, err)
			}
			runtime := skill.New(skill.Options{HomeDir: home, CustomPaths: pkg.SkillRoots(), DisableBuiltins: true}).List()
			inventory := pkg.Inventory().Skills
			count, _, _, _ := pkg.CapabilityCounts()
			if tc.want == "" {
				if len(runtime) != 0 || len(inventory) != 0 || count != 0 {
					t.Fatalf("invalid stem accepted: runtime=%+v inventory=%+v count=%d", runtime, inventory, count)
				}
				return
			}
			if len(runtime) != 1 || runtime[0].Name != tc.want {
				t.Fatalf("runtime=%+v, want %q", runtime, tc.want)
			}
			if len(inventory) != 1 || inventory[0].Name != tc.want || inventory[0].Invocation != "/"+tc.want || count != 1 {
				t.Fatalf("inventory=%+v count=%d, want /%s", inventory, count, tc.want)
			}
			if inventory[0].Path != runtime[0].Path {
				t.Fatalf("inventory path=%q runtime path=%q", inventory[0].Path, runtime[0].Path)
			}
		})
	}
}

func TestPackageCanonicalSkillCollisionsMatchRuntimeWinner(t *testing.T) {
	for _, tc := range []struct {
		label, roots, first, second string
		frontNames                  bool
	}{
		{"frontmatter names", `["skills"]`, "skills/第一/SKILL.md", "skills/第二/SKILL.md", true},
		{"directory names across roots", `["second","first"]`, "first/café/SKILL.md", "second/cafe\u0301/SKILL.md", false},
		{"flat names across roots", `["second","first"]`, "first/café.md", "second/cafe\u0301.md", false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			root, home := testenv.TempDir(t), testenv.TempDir(t)
			write := func(rel, body string) {
				t.Helper()
				path := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write(pluginpkg.NativeManifest, fmt.Sprintf(`{"apiVersion":"reasonix.io/plugin/v2","name":"names-kit","contributes":{"skills":%s}}`, tc.roots))
			firstBody, secondBody := "---\ndescription: First fixture\n---\nFIRST", "---\ndescription: Second fixture\n---\nSECOND"
			if tc.frontNames {
				firstBody = "---\nname: café\ndescription: First fixture\n---\nFIRST"
				secondBody = "---\nname: cafe\u0301\ndescription: Second fixture\n---\nSECOND"
			}
			write(tc.first, firstBody)
			write(tc.second, secondBody)
			pkg, warnings, err := pluginpkg.ParseDir(root)
			if err != nil {
				t.Fatalf("runtime-compatible package refused: %v", err)
			}
			store := skill.New(skill.Options{HomeDir: home, CustomPaths: pkg.SkillRoots(), DisableBuiltins: true})
			runtime := store.SlashList()
			if len(runtime) != 1 || runtime[0].Name != "café" || runtime[0].Path != filepath.Join(root, tc.first) || runtime[0].Body != "FIRST" {
				t.Fatalf("runtime winner = %+v", runtime)
			}
			if winner, ok := store.Read("café"); !ok || winner.Path != runtime[0].Path {
				t.Fatalf("Read winner = %+v, ok=%t", winner, ok)
			}
			inventory := pkg.Inventory().Skills
			count, _, _, _ := pkg.CapabilityCounts()
			if len(inventory) != 1 || count != 1 || inventory[0].Path != runtime[0].Path || inventory[0].Invocation != "/café" {
				t.Fatalf("inventory=%+v count=%d, runtime winner=%+v", inventory, count, runtime[0])
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0], `skill name "café"`) || !strings.Contains(warnings[0], tc.first) || !strings.Contains(warnings[0], tc.second) {
				t.Fatalf("collision warning = %v", warnings)
			}
		})
	}
}

func TestPackageAllowsOverlappingSkillRoots(t *testing.T) {
	root := testenv.TempDir(t)
	path := filepath.Join(root, "skills", "审查", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\ndescription: Overlapping root fixture\n---\nBODY"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `{"apiVersion":"reasonix.io/plugin/v2","name":"names-kit","contributes":{"skills":["skills","skills/审查"]}}`
	if err := os.WriteFile(filepath.Join(root, pluginpkg.NativeManifest), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, warnings, err := pluginpkg.ParseDir(root)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("overlapping roots: warnings=%v err=%v", warnings, err)
	}
	if inventory := pkg.Inventory().Skills; len(inventory) != 1 || inventory[0].Name != "审查" || inventory[0].Path != path {
		t.Fatalf("overlapping roots inventory = %+v", inventory)
	}
}
