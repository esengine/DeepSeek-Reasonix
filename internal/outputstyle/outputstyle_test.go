package outputstyle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	fileencoding "reasonix/internal/fileutil/encoding"
)

func TestResolveBuiltin(t *testing.T) {
	st, ok := Resolve("explanatory", nil)
	if !ok {
		t.Fatal("explanatory built-in should resolve")
	}
	if !st.Builtin || !st.KeepCoding || strings.TrimSpace(st.Body) == "" {
		t.Errorf("unexpected built-in shape: %+v", st)
	}
	// Case-insensitive.
	if _, ok := Resolve("LEARNING", nil); !ok {
		t.Error("resolve should be case-insensitive")
	}
}

func TestResolveDefaultIsNone(t *testing.T) {
	for _, name := range []string{"", "  ", "default"} {
		if _, ok := Resolve(name, nil); ok {
			t.Errorf("Resolve(%q) should be no-style", name)
		}
	}
}

func TestApply(t *testing.T) {
	append1 := Apply("BASE", OutputStyle{Body: "X", KeepCoding: true})
	if append1 != "BASE\n\nX" {
		t.Errorf("keep-coding append = %q", append1)
	}
	replace := Apply("BASE", OutputStyle{Body: "X", KeepCoding: false})
	if replace != "X" {
		t.Errorf("replace = %q, want X", replace)
	}
	if got := Apply("BASE", OutputStyle{Body: "   "}); got != "BASE" {
		t.Errorf("empty body should leave base untouched, got %q", got)
	}
}

func TestListIncludesBuiltinsSorted(t *testing.T) {
	got := List(nil)
	if len(got) < 3 {
		t.Fatalf("expected at least the 3 built-ins, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Name > got[i].Name {
			t.Errorf("not sorted by name: %q before %q", got[i-1].Name, got[i].Name)
		}
	}
}

func TestCustomFileOverridesBuiltinAndParses(t *testing.T) {
	dir := t.TempDir()
	// Override the built-in "explanatory" with a custom replace-style file.
	md := "---\ndescription: my persona\nkeep-coding-instructions: false\n---\nYou are a pirate. Answer in pirate speak.\n"
	if err := os.WriteFile(filepath.Join(dir, "explanatory.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	st, ok := Resolve("explanatory", []string{dir})
	if !ok {
		t.Fatal("custom explanatory should resolve")
	}
	if st.Builtin {
		t.Error("custom file should override the built-in (Builtin=false)")
	}
	if st.KeepCoding {
		t.Error("keep-coding-instructions: false should disable KeepCoding")
	}
	if st.Description != "my persona" || !strings.Contains(st.Body, "pirate") {
		t.Errorf("frontmatter/body not parsed: %+v", st)
	}
	if got := Apply("CODING PROMPT", st); got != st.Body {
		t.Errorf("a replace-style should drop the base prompt, got %q", got)
	}
}

func TestResolveDecodesGB18030CustomFile(t *testing.T) {
	dir := t.TempDir()
	body := "---\nname: concise-cn\ndescription: 中文风格\n---\n请用中文简洁回答。"
	if err := os.WriteFile(filepath.Join(dir, "concise-cn.md"), fileencoding.Encode(body, fileencoding.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}

	st, ok := Resolve("concise-cn", []string{dir})
	if !ok {
		t.Fatal("custom style should resolve")
	}
	if st.Description != "中文风格" || st.Body != "请用中文简洁回答。" {
		t.Fatalf("decoded style = %+v", st)
	}
}

func TestParseFileNameFromFilename(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "snappy.md"), []byte("Be snappy."), 0o644); err != nil {
		t.Fatal(err)
	}
	st, ok := Resolve("snappy", []string{dir})
	if !ok || st.Name != "snappy" || !st.KeepCoding { // default keep-coding when unspecified
		t.Errorf("filename-derived style wrong: %+v ok=%v", st, ok)
	}
}
