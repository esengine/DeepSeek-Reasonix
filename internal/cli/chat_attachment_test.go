package cli

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/control"
)

func TestExpandPastedBlocksImage(t *testing.T) {
	m := &chatTUI{pastedBlocks: []pastedBlock{
		{label: "[image #1]", text: "@.reasonix/attachments/clipboard-20260601-010203.000001.png", image: true},
		{label: "[Pasted text #2 · 3 lines]", text: "a\nb\nc"},
	}}
	got := m.expandPastedBlocks("look at [image #1] and [Pasted text #2 · 3 lines]")
	want := "look at @.reasonix/attachments/clipboard-20260601-010203.000001.png and " +
		renderFoldedPasteBlock(m.pastedBlocks[1])
	if got != want {
		t.Fatalf("expandPastedBlocks = %q, want %q", got, want)
	}
	if displayLineForImageRefs(got) != "look at [image1] and "+renderFoldedPasteBlock(m.pastedBlocks[1]) {
		t.Fatalf("image ref should collapse to a label in the bubble: %q", displayLineForImageRefs(got))
	}
}

func TestDisplayLineForImageRefs(t *testing.T) {
	got := displayLineForImageRefs("describe @.reasonix/attachments/clipboard-20260601-010203.000001.png @.reasonix/attachments/clipboard-20260601-010204.000002-000002.jpg")
	want := "describe [image1] [image2]"
	if got != want {
		t.Fatalf("displayLineForImageRefs = %q, want %q", got, want)
	}
}

func TestPastedFileRef(t *testing.T) {
	dir := t.TempDir()
	pdf := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, ok := pastedFileRef(pdf); !ok || got != "@"+filepath.Clean(pdf) {
		t.Fatalf("pastedFileRef(existing pdf) = %q, %v", got, ok)
	}
	if got, ok := pastedFileRef(`"` + pdf + `"`); !ok || got != "@"+filepath.Clean(pdf) {
		t.Fatalf("pastedFileRef(quoted pdf) = %q, %v", got, ok)
	}
	if _, ok := pastedFileRef("just-a-word"); ok {
		t.Fatal("a bare word with no separator must not be a file ref")
	}
	if _, ok := pastedFileRef(filepath.Join(dir, "missing.pdf")); ok {
		t.Fatal("a non-existent path must not be a file ref")
	}
	if _, ok := pastedFileRef(dir); ok {
		t.Fatal("a directory must not be a file ref")
	}
}

func TestPastedFileRefShellEscapedSpaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell-escaped paths are not decoded on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Application Support", "report 2026.pdf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	escaped := strings.ReplaceAll(path, " ", `\ `)

	// The returned ref keeps whitespace escaped so it survives @-token parsing
	// on submit (control.parseRefTokens unescapes it back to the real path).
	want := "@" + control.EscapeRefPath(filepath.Clean(path))
	if got, ok := pastedFileRef(escaped); !ok || got != want {
		t.Fatalf("pastedFileRef(shell escaped pdf) = %q, %v; want %s", got, ok, want)
	}
}

func TestPastedImageSources(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		want      []string
		ok        bool
		posixOnly bool
	}{
		{
			name: "data URL",
			text: "data:image/png;base64,aaa",
			want: []string{"data:image/png;base64,aaa"},
			ok:   true,
		},
		{
			name: "markdown images",
			text: "![a](/tmp/a.png)\n![b](file:///tmp/b.jpg)",
			want: []string{"/tmp/a.png", "file:///tmp/b.jpg"},
			ok:   true,
		},
		{
			name:      "shell escaped path with spaces",
			text:      `/Users/jawa/Library/Application\ Support/CleanShot/media/CleanShot\ 2026-07-06\ at\ 11.33.14@2x.png`,
			want:      []string{`/Users/jawa/Library/Application\ Support/CleanShot/media/CleanShot\ 2026-07-06\ at\ 11.33.14@2x.png`},
			ok:        true,
			posixOnly: true,
		},
		{
			name: "shell escaped path without whitespace",
			text: `/tmp/capture\(1\).png`,
			want: []string{`/tmp/capture\(1\).png`},
			ok:   true,
		},
		{
			name:      "multiple shell escaped paths on one line",
			text:      `/tmp/first\ image.png /tmp/second\ image.jpg`,
			want:      []string{`/tmp/first\ image.png`, `/tmp/second\ image.jpg`},
			ok:        true,
			posixOnly: true,
		},
		{
			name: "multiple quoted paths on one line",
			text: `'/tmp/first image.png' "/tmp/second image.jpg"`,
			want: []string{`'/tmp/first image.png'`, `"/tmp/second image.jpg"`},
			ok:   true,
		},
		{
			name: "sentence with image path remains text",
			text: `see /tmp/CleanShot\ 2026.png`,
			ok:   false,
		},
		{
			name: "plain text",
			text: "hello /tmp/a.png",
			ok:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.posixOnly && runtime.GOOS == "windows" {
				t.Skip("POSIX shell-escaped paths are not decoded on Windows")
			}
			sources, ok := pastedImageSources(c.text)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			var got []string
			if sources != nil {
				got = make([]string, 0, len(sources))
				for _, source := range sources {
					got = append(got, source.value)
				}
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("sources = %v, want %v", got, c.want)
			}
		})
	}
}

func TestPasteShellEscapedImagePathInsertsImageToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell-escaped paths are not decoded on Windows")
	}
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "Library", "Application Support", "CleanShot", "CleanShot 2026-07-06 at 11.33.14@2x.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: strings.ReplaceAll(path, " ", `\ `)})
	updated := next.(chatTUI)

	if got := updated.input.Value(); got != "[image #1] " {
		t.Fatalf("input after paste = %q, want image token", got)
	}
	if len(updated.pastedBlocks) != 1 || !updated.pastedBlocks[0].image {
		t.Fatalf("pastedBlocks = %+v, want one image block", updated.pastedBlocks)
	}
	if text := updated.pastedBlocks[0].text; !strings.HasPrefix(text, "@.reasonix/attachments/clipboard-") || !strings.HasSuffix(text, ".png") {
		t.Fatalf("image block text = %q, want saved attachment ref", text)
	}
}

func TestPasteShellEscapedImagePathWithoutWhitespaceInsertsImageToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell-escaped paths are not decoded on Windows")
	}
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "capture^(1),x.png")
	raw, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	escaped := strings.NewReplacer("(", `\(`, ")", `\)`, "^", `\^`, ",", `\,`).Replace(path)

	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: escaped})
	updated := next.(chatTUI)

	if got := updated.input.Value(); got != "[image #1] " {
		t.Fatalf("input after paste = %q, want image token", got)
	}
	if len(updated.pastedBlocks) != 1 || !updated.pastedBlocks[0].image {
		t.Fatalf("pastedBlocks = %+v, want one image block", updated.pastedBlocks)
	}
}

func TestPasteMultipleShellEscapedImagePathsInsertsImageTokens(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell-escaped paths are not decoded on Windows")
	}
	root := t.TempDir()
	t.Chdir(root)
	raw, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "first image.png")
	second := filepath.Join(root, "second image.png")
	for _, p := range []string{first, second} {
		if err := os.WriteFile(p, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	content := strings.ReplaceAll(first, " ", `\ `) + " " + strings.ReplaceAll(second, " ", `\ `)

	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: content})
	updated := next.(chatTUI)

	if got := updated.input.Value(); got != "[image #1] [image #2] " {
		t.Fatalf("input after paste = %q, want two image tokens", got)
	}
	if len(updated.pastedBlocks) != 2 || !updated.pastedBlocks[0].image || !updated.pastedBlocks[1].image {
		t.Fatalf("pastedBlocks = %+v, want two image blocks", updated.pastedBlocks)
	}
}

func TestMissingPastedImagePathRemainsText(t *testing.T) {
	content := `/definitely-missing/reasonix-image.png`
	if runtime.GOOS == "windows" {
		content = `C:/definitely-missing/reasonix-image.png`
	}

	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: content})
	updated := next.(chatTUI)
	if got := updated.input.Value(); got != content {
		t.Fatalf("input after missing image paste = %q, want original %q", got, content)
	}
	if len(updated.pastedBlocks) != 0 {
		t.Fatalf("pastedBlocks = %+v, want no image attachment", updated.pastedBlocks)
	}
}

func TestPastedImagePathShellUnescape(t *testing.T) {
	cases := []struct {
		name string
		src  string
		goos string
		want string
		ok   bool
	}{
		{
			name: "posix escaped parens without whitespace",
			src:  `/tmp/capture\(1\).png`,
			goos: "linux",
			want: "/tmp/capture(1).png",
			ok:   true,
		},
		{
			name: "posix escaped spaces",
			src:  `/tmp/first\ image.png`,
			goos: "linux",
			want: "/tmp/first image.png",
			ok:   true,
		},
		{
			name: "posix escaped caret and comma",
			src:  `/tmp/capture\^1\,a.png`,
			goos: "linux",
			want: "/tmp/capture^1,a.png",
			ok:   true,
		},
		{
			name: "posix escaped literal backslash",
			src:  `/tmp/a\\b.png`,
			goos: "linux",
			want: `/tmp/a\b.png`,
			ok:   true,
		},
		{
			name: "posix unescaped space rejected",
			src:  "/tmp/first image.png",
			goos: "linux",
			ok:   false,
		},
		{
			name: "windows backslash separators preserved",
			src:  `C:\Users\me\shot(1).png`,
			goos: "windows",
			want: `C:\Users\me\shot(1).png`,
			ok:   true,
		},
		{
			name: "windows dollar directory preserved",
			src:  `C:\$Recycle.Bin\shot.png`,
			goos: "windows",
			want: `C:\$Recycle.Bin\shot.png`,
			ok:   true,
		},
		{
			name: "windows unquoted space rejected",
			src:  `C:\Program Files\shot.png`,
			goos: "windows",
			ok:   false,
		},
		{
			name: "windows quoted path with space preserved",
			src:  `"C:\my dir\shot.png"`,
			goos: "windows",
			want: `C:\my dir\shot.png`,
			ok:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := pastedImagePathForOS(c.src, c.goos)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if c.ok && got != c.want {
				t.Fatalf("path = %q, want %q", got, c.want)
			}
		})
	}
}
