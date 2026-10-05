package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

func completeAt(t *testing.T, ctrl *control.Controller, line string, cursor int) control.Completion {
	t.Helper()
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()

	q := url.Values{"line": {line}, "cursor": {strconv.Itoa(cursor)}}
	resp, err := http.Get(srv.URL + "/complete?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /complete = %d, want 200", resp.StatusCode)
	}
	var got control.Completion
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

// The composer indexes the line in UTF-16 code units, so a prompt written in
// Chinese has to come back with offsets that splice where the caret is — byte
// offsets would land mid-character and rewrite the wrong text.
func TestCompleteOffsetsAreUTF16(t *testing.T) {
	root := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctrl := control.New(control.Options{SessionDir: testenv.TempDir(t), WorkspaceRoot: root})
	defer ctrl.Close()

	line := "看一下 @no"
	// "看一下 " is 4 UTF-16 units; the token starts there and runs to the end.
	got := completeAt(t, ctrl, line, 7)
	if got.Kind != control.CompleteRef {
		t.Fatalf("kind = %q, want %q", got.Kind, control.CompleteRef)
	}
	if got.From != 4 || got.To != 7 {
		t.Fatalf("span [%d,%d), want [4,7) in UTF-16 units", got.From, got.To)
	}
	if []rune(line)[got.From] != '@' {
		t.Fatalf("From points at %q, want '@'", string([]rune(line)[got.From]))
	}
	if len(got.Items) == 0 || got.Items[0].Insert != "@notes.md" {
		t.Fatalf("items = %+v, want @notes.md", got.Items)
	}
}

// The built-in verbs Submit dispatches are the ones a windowed frontend can
// offer: a menu that lists only skills hides most of what typing already does.
func TestCompleteOffersBuiltinCommands(t *testing.T) {
	ctrl := control.New(control.Options{SessionDir: testenv.TempDir(t), WorkspaceRoot: testenv.TempDir(t)})
	defer ctrl.Close()

	got := completeAt(t, ctrl, "/comp", 5)
	if got.Kind != control.CompleteSlash {
		t.Fatalf("kind = %q, want %q", got.Kind, control.CompleteSlash)
	}
	for _, it := range got.Items {
		if it.Label == "/compact" {
			if it.Kind != "builtin" {
				t.Fatalf("/compact kind = %q, want builtin", it.Kind)
			}
			return
		}
	}
	t.Fatalf("items = %+v, want /compact", got.Items)
}

// A client of this server completes inside the workspace and nowhere else —
// the same boundary SubmitHTTP resolves its references within.
func TestCompleteRefusesPathsOutsideWorkspace(t *testing.T) {
	parent := testenv.TempDir(t)
	root := filepath.Join(parent, "work")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "secret.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctrl := control.New(control.Options{SessionDir: testenv.TempDir(t), WorkspaceRoot: root})
	defer ctrl.Close()

	if got := completeAt(t, ctrl, "@../", 4); len(got.Items) != 0 {
		t.Fatalf("completion escaped the workspace: %+v", got.Items)
	}
}

// A management verb does its work and emits a notice without ever starting a
// turn, so judging the submission by whether one began drew an error over
// every /compact and /clear the composer sent.
func TestSubmitAcceptsAVerbThatStartsNoTurn(t *testing.T) {
	ctrl := control.New(control.Options{SessionDir: testenv.TempDir(t), WorkspaceRoot: testenv.TempDir(t)})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()

	for _, verb := range []string{"/compact", "/context", "/clear"} {
		resp, err := http.Post(srv.URL+"/submit", "application/json",
			strings.NewReader(`{"input":"`+verb+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusConflict {
			t.Fatalf("POST /submit %s = 409 %s, want it accepted", verb, strings.TrimSpace(string(body)))
		}
	}
}

// An @-reference turn is stored twice: the composed text the model saw, and the
// line the person typed. Reopening a session showed the composed one, so a
// one-line prompt came back as the whole file it referenced.
func TestHistoryShowsWhatWasTypedNotWhatWasSent(t *testing.T) {
	typed := "@REASONIX.md 看一下"
	got := historyMessages([]provider.Message{
		{Role: provider.RoleUser, Content: "Referenced context: <file path=\"REASONIX.md\">…全文…</file>\n\n" + typed, RawContent: typed},
		// A turn from before RawContent existed still has to render readably.
		{Role: provider.RoleUser, Content: "老会话里的一句话"},
	})
	if got[0].Content != typed {
		t.Fatalf("history[0] = %q, want the typed line %q", got[0].Content, typed)
	}
	if got[1].Content != "老会话里的一句话" {
		t.Fatalf("history[1] = %q, want the legacy turn unchanged", got[1].Content)
	}
}

// The built-in verbs are worded by the kernel and read in a window, and the two
// need not speak the same language: a machine whose CLI is Chinese can have an
// English window on it. Reading the process's own catalogue put a Chinese menu
// under an English composer.
func TestCompleteVerbsFollowTheAskingWindow(t *testing.T) {
	ctrl := control.New(control.Options{SessionDir: testenv.TempDir(t), WorkspaceRoot: testenv.TempDir(t)})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()

	hints := func(lang string) map[string]string {
		t.Helper()
		q := url.Values{"line": {"/"}, "cursor": {"1"}}
		if lang != "" {
			q.Set("lang", lang)
		}
		resp, err := http.Get(srv.URL + "/complete?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var got control.Completion
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, item := range got.Items {
			out[item.Label] = item.Hint
		}
		return out
	}

	en, zh := hints("en"), hints("zh")
	if len(en) == 0 {
		t.Fatal("the menu came back empty; the rest proves nothing")
	}
	han := false
	for label, hint := range en {
		if strings.ContainsFunc(hint, func(r rune) bool { return r >= 0x4E00 && r <= 0x9FFF }) {
			t.Errorf("%s reads %q in an English window", label, hint)
		}
		if zh[label] != hint {
			han = true
		}
	}
	if !han {
		t.Fatal("every hint is identical in both languages; the language was not read")
	}
}
