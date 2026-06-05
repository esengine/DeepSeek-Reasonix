package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDefaultSpecsInvariants(t *testing.T) {
	seen := map[string]string{}
	for lang, s := range DefaultSpecs() {
		if s.Command == "" || s.LanguageID == "" || len(s.Extensions) == 0 {
			t.Errorf("lang %q: incomplete spec %+v", lang, s)
		}
		for _, ext := range s.Extensions {
			if prev, dup := seen[ext]; dup {
				t.Errorf("extension %q claimed by both %q and %q", ext, prev, lang)
			}
			seen[ext] = lang
		}
	}
	if seen[".go"] != "go" || seen[".rs"] != "rust" || seen[".cpp"] != "cpp" || seen[".cs"] != "csharp" {
		t.Errorf("unexpected routing: %v", seen)
	}
}

func TestExtensionRouting(t *testing.T) {
	m := NewManager(t.TempDir(), map[string]ServerSpec{
		"elixir": {Command: "no-such-elixir-ls-xyz", LanguageID: "elixir", Extensions: []string{".ex", ".exs"}, InstallHint: "mix archive.install"},
	})
	defer m.Close()

	if _, err := m.resolve("a.ex"); !errors.As(err, new(*notInstalledError)) {
		t.Fatalf("configured-but-missing language should yield notInstalledError, got %v", err)
	}
	_, err := m.resolve("a.go")
	if err == nil || !strings.Contains(err.Error(), "no language server") {
		t.Fatalf("unconfigured extension should report no server, got %v", err)
	}
}

func TestConnBidirectional(t *testing.T) {
	caR, caW := io.Pipe()
	acR, acW := io.Pipe()
	// Close the writers at the end so both readLoop goroutines see EOF and exit
	// (in production the subprocess pipe EOFs on kill; here nothing else closes it).
	defer caW.Close()
	defer acW.Close()

	notif := make(chan string, 4)
	var client *conn
	client = newConn(caW, acR,
		func(method string, _ json.RawMessage) { notif <- method },
		func(id int64, _ string, _ json.RawMessage) { _ = client.reply(id, map[string]any{"ok": true}) })

	var server *conn
	server = newConn(acW, caR,
		func(string, json.RawMessage) {},
		func(id int64, method string, _ json.RawMessage) { _ = server.reply(id, map[string]any{"echo": method}) })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := client.call(ctx, "ping", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("client call: %v", err)
	}
	if !strings.Contains(string(res), `"echo":"ping"`) {
		t.Fatalf("unexpected response: %s", res)
	}

	if err := server.notify("textDocument/publishDiagnostics", map[string]any{}); err != nil {
		t.Fatalf("server notify: %v", err)
	}
	select {
	case m := <-notif:
		if m != "textDocument/publishDiagnostics" {
			t.Fatalf("notify method = %q", m)
		}
	case <-ctx.Done():
		t.Fatal("notification not delivered")
	}

	sres, err := server.call(ctx, "workspace/configuration", nil)
	if err != nil {
		t.Fatalf("server→client call: %v", err)
	}
	if !strings.Contains(string(sres), `"ok":true`) {
		t.Fatalf("server→client reply: %s", sres)
	}
}

func TestReadFrame(t *testing.T) {
	in := "Content-Length: 17\r\nContent-Type: x\r\n\r\n" + `{"jsonrpc":"2.0"}` + "Content-Length: 2\r\n\r\n{}"
	r := bufio.NewReader(strings.NewReader(in))
	first, err := readFrame(r)
	if err != nil || string(first) != `{"jsonrpc":"2.0"}` {
		t.Fatalf("first frame = %q, err %v", first, err)
	}
	second, err := readFrame(r)
	if err != nil || string(second) != `{}` {
		t.Fatalf("second frame = %q, err %v", second, err)
	}
	if _, err := readFrame(r); err == nil {
		t.Fatal("expected EOF on third read")
	}
}

func TestURIRoundtrip(t *testing.T) {
	paths := []string{"/home/u/a b.go", "/x/y.rs"}
	if runtime.GOOS == "windows" {
		paths = []string{`C:\Users\u\a b.go`, `D:\x\y.rs`}
	}
	for _, p := range paths {
		uri := pathToURI(p)
		if !strings.HasPrefix(uri, "file://") {
			t.Errorf("%q → %q is not a file URI", p, uri)
		}
		if got := uriToPath(uri); got != p {
			t.Errorf("roundtrip %q → %q", p, got)
		}
	}
}

func TestLocateEncoding(t *testing.T) {
	content := "package x\nαβ foo()\n" // line 2 has two 2-byte runes then a space
	u16, err := locate(content, 2, "foo", encodingUTF16)
	if err != nil {
		t.Fatal(err)
	}
	if u16.Line != 1 || u16.Character != 3 {
		t.Errorf("utf16 pos = %+v, want line 1 char 3", u16)
	}
	u8, err := locate(content, 2, "foo", encodingUTF8)
	if err != nil {
		t.Fatal(err)
	}
	if u8.Character != 5 {
		t.Errorf("utf8 char = %d, want 5", u8.Character)
	}
	if _, err := locate(content, 2, "missing", encodingUTF16); err == nil {
		t.Error("expected not-found error")
	}
}

func TestParseLocations(t *testing.T) {
	single := `{"uri":"file:///a","range":{"start":{"line":1,"character":0},"end":{"line":1,"character":2}}}`
	if got := parseLocations(json.RawMessage(single)); len(got) != 1 || got[0].URI != "file:///a" {
		t.Errorf("single: %+v", got)
	}
	arr := `[{"uri":"file:///a","range":{}},{"uri":"file:///b","range":{}}]`
	if got := parseLocations(json.RawMessage(arr)); len(got) != 2 {
		t.Errorf("array: %+v", got)
	}
	link := `[{"targetUri":"file:///c","targetRange":{"start":{"line":2,"character":0},"end":{"line":2,"character":1}}}]`
	got := parseLocations(json.RawMessage(link))
	if len(got) != 1 || got[0].URI != "file:///c" || got[0].Range.Start.Line != 2 {
		t.Errorf("locationlink: %+v", got)
	}
	if parseLocations(json.RawMessage("null")) != nil {
		t.Error("null should yield nil")
	}
}

func TestParseHover(t *testing.T) {
	markup := `{"contents":{"kind":"markdown","value":"func F()"}}`
	if got := parseHover(json.RawMessage(markup)); got != "func F()" {
		t.Errorf("markup hover = %q", got)
	}
	marked := `{"contents":[{"language":"go","value":"func F()"},"docs"]}`
	if got := parseHover(json.RawMessage(marked)); got != "func F()\ndocs" {
		t.Errorf("marked array hover = %q", got)
	}
	if got := parseHover(json.RawMessage(`{"contents":""}`)); got != "" {
		t.Errorf("empty hover = %q", got)
	}
}
