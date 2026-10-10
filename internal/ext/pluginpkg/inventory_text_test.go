package pluginpkg

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"reasonix/internal/base/testenv"
)

const hostile = "x\x1b[2J‮​ ⠀"

func hostileValue() string { return hostile + strings.Repeat("长", 3000) }

func fillStrings(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString(hostileValue())
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.String || v.Type().Elem().Kind() == reflect.Struct {
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		}
		for i := range v.Len() {
			fillStrings(v.Index(i))
		}
	case reflect.Struct:
		for _, f := range v.Fields() {
			fillStrings(f)
		}
	}
}

func checkShown(t *testing.T, path string, v reflect.Value) {
	t.Helper()
	switch v.Kind() {
	case reflect.String:
		s := v.String()
		for _, r := range s {
			if r < 0x20 && r != '\n' || r == 0x7f || r == 0x202e || r == 0x200b || r == 0x2028 || r == 0x2800 || unicode.Is(unicode.Co, r) {
				t.Errorf("%s keeps hidden %U", path, r)
				return
			}
		}
		if utf8.RuneCountInString(s) > 1100 || s == "" {
			t.Errorf("%s: unbounded or lost (%d runes)", path, utf8.RuneCountInString(s))
		}
	case reflect.Slice:
		for i := range v.Len() {
			checkShown(t, path, v.Index(i))
		}
	case reflect.Struct:
		for sf, f := range v.Fields() {
			checkShown(t, path+"."+sf.Name, f)
		}
	}
}

func TestInventoryProjectsEveryAuthorField(t *testing.T) {
	var inv Inventory
	fillStrings(reflect.ValueOf(&inv).Elem())
	checkShown(t, "Inventory", reflect.ValueOf(inv.bounded()))
}

func TestDisplayProjectsEveryAuthorField(t *testing.T) {
	var p InstalledPlugin
	fillStrings(reflect.ValueOf(&p).Elem())
	root := p.Root
	shown := p.Display()
	if shown.Root != root {
		t.Fatal("Display must leave Root, a path this process resolves, alone")
	}
	shown.Root = "ok"
	checkShown(t, "InstalledPlugin", reflect.ValueOf(shown))

	var rt RuntimeSpec
	fillStrings(reflect.ValueOf(&rt).Elem())
	rt.Tools = nil
	checkShown(t, "RuntimeSpec", reflect.ValueOf(*rt.Display()))

	var issues []CompatibilityIssue
	fillStrings(reflect.ValueOf(&issues).Elem())
	checkShown(t, "issues", reflect.ValueOf(DisplayIssues(issues)))
	checkShown(t, "lines", reflect.ValueOf(DisplayLines([]string{hostileValue()})))
	checkShown(t, "ids", reflect.ValueOf(DisplayIdentities([]string{hostileValue()})))
	if got := DisplayLines(make([]string, 500)); len(got) != 50 {
		t.Fatalf("lines uncapped: %d", len(got))
	}
}

func TestOrdinaryInventoryAndRecordAreUnchanged(t *testing.T) {
	inv := Inventory{
		Skills:     []SkillRef{{Name: "代码审查", Description: "审查代码 and report\n第二行 🚀", Path: "/p/skills/a", Invocation: "/x:a", RunAs: "fork"}},
		Hooks:      []HookRef{{Event: "SessionStart", Match: "*", Command: "node ./h.js --flag=中文", Description: "starts"}},
		MCPServers: []MCPServerRef{{Name: "docs", DisplayName: "Docs", Transport: "stdio", Command: "npx", URL: "https://例え.jp/mcp"}},
	}
	if got := inv.bounded(); !reflect.DeepEqual(got, inv) {
		t.Fatalf("ordinary inventory changed: %+v", got)
	}
	rec := InstalledPlugin{Name: "demo", Source: "https://github.com/a/b", Root: "plugins/demo", Version: "1.0.0", Description: "说明 text", Enabled: true}
	if rec.Display() != rec {
		t.Fatalf("ordinary record changed: %+v", rec.Display())
	}
}

func TestInstalledShowTextBoundsHookAndMCPText(t *testing.T) {
	home := testenv.TempDir(t)
	root := filepath.Join(home, "plugins", "evil")
	writeTestFile(t, filepath.Join(root, NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"evil","version":"1.0.0\u001b[2J‮",
"hooks":{"SessionStart":[{"command":"run\u001b[2J‮ `+strings.Repeat("a", 3000)+`"}]},
"mcpServers":{"docs":{"command":"docs\u001b[31m‮","args":["--stdio"]}}}`)
	if err := Upsert(home, InstalledPlugin{Name: "evil", Root: "plugins/evil", ManifestKind: "reasonix", Version: "1.0.0\x1b[2J‮", Description: "d\x1b[2J", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	show, err := InstalledShowText(home, "evil")
	if err != nil {
		t.Fatal(err)
	}
	list, err := InstalledListText(home)
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"show": show, "list": list} {
		if strings.ContainsAny(text, "\x1b‮") {
			t.Errorf("%s keeps hidden characters: %q", name, text[:min(len(text), 300)])
		}
		if len(text) > 8000 {
			t.Errorf("%s unbounded: %d bytes", name, len(text))
		}
	}
	if !strings.Contains(show, `\u{1b}[2J`) || !strings.Contains(show, "…") {
		t.Fatalf("hook command must show the escape and the cut: %q", show)
	}
}

func TestInventoryIsRawAndDisplayIsASeparateProjection(t *testing.T) {
	root := testenv.TempDir(t)
	writeV2Plugin(t, root, `{"apiVersion":"reasonix.io/plugin/v2","name":"raw","version":"1.0.0",
"hooks":{"SessionStart":[{"command":"run​x"}]}}`)
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := pkg.Inventory().Hooks; len(got) != 1 || got[0].Command != "run​x" {
		t.Fatalf("Inventory must return the package's own text: %+v", got)
	}
	if got := pkg.InventoryForDisplay().Hooks; len(got) != 1 || got[0].Command != `run\u{200b}x` {
		t.Fatalf("display projection: %+v", got)
	}
}
