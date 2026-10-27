package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fileenc "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/contract/tool"
)

type changingReadOverlay struct {
	reads  int
	writes int
}

func (o *changingReadOverlay) ReadTextFile(context.Context, string) (string, bool) {
	o.reads++
	if o.reads == 1 {
		return "before\n", true
	}
	return "another writer\n", true
}

func (o *changingReadOverlay) WriteTextFile(context.Context, string, string) (bool, error) {
	o.writes++
	return true, nil
}

func TestFileViewTracksTheReturnedReadRatherThanASecondRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	views, overlay := NewFileViews(), &changingReadOverlay{}
	if err := runViewTool(t, readFile{views: views, overlay: overlay}, map[string]string{"path": path}); err != nil {
		t.Fatal(err)
	}
	err := runViewTool(t, writeFile{views: views, overlay: overlay}, map[string]string{"path": path, "content": "overwrite\n"})
	if !errors.Is(err, ErrFileChangedSinceSeen) || overlay.writes != 0 {
		t.Fatalf("stale read certified a later version: %v; writes=%d", err, overlay.writes)
	}
}

func TestPagedReadDoesNotCertifyUnseenFileContent(t *testing.T) {
	for _, page := range []string{
		`{"path":"a.txt","limit":1}`,
		`{"path":"a.txt","offset":1}`,
		`{"path":"a.txt","offset":99}`,
	} {
		t.Run(page, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "a.txt")
			if err := os.WriteFile(path, []byte("header\noriginal tail\n"), 0600); err != nil {
				t.Fatal(err)
			}
			views := NewFileViews()
			reader := readFile{workDir: root, views: views}
			if _, err := reader.Execute(t.Context(), json.RawMessage(`{"path":"a.txt"}`)); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("header\nother writer's tail\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := reader.Execute(t.Context(), json.RawMessage(page)); err != nil {
				t.Fatal(err)
			}
			_, err := (writeFile{workDir: root, views: views}).Execute(t.Context(), json.RawMessage(`{"path":"a.txt","content":"edited header\noriginal tail\n"}`))
			if !errors.Is(err, ErrFileChangedSinceSeen) {
				t.Fatalf("paged read authorized stale overwrite: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != "header\nother writer's tail\n" {
				t.Fatalf("lost unseen content: %q; %v", got, err)
			}
		})
	}
}

func TestOversizeReadDoesNotForgetAnEarlierFileView(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	views := NewFileViews()
	reader := readFile{workDir: root, views: views}
	if _, err := reader.Execute(t.Context(), json.RawMessage(`{"path":"a.txt"}`)); err != nil {
		t.Fatal(err)
	}
	current := strings.Repeat("external writer\n", maxViewBytes/16+1)
	if err := os.WriteFile(path, []byte(current), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Execute(t.Context(), json.RawMessage(`{"path":"a.txt","limit":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := views.checkOverwrite(path, current); !errors.Is(err, ErrFileChangedSinceSeen) {
		t.Fatalf("oversize read forgot the previous view: %v", err)
	}
}

func TestWholeReadRefreshesTheFileView(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		enc     fileenc.Kind
		nested  bool
	}{
		{name: "empty"},
		{name: "final newline", content: "new\n"},
		{name: "no final newline", content: "new"},
		{name: "CRLF", content: "new\r\n"},
		{name: "UTF8 BOM", content: "new\n", enc: fileenc.UTF8BOM},
		{name: "UTF16", content: "new\n", enc: fileenc.UTF16LE},
		{name: "nested verbatim", content: "new\r\n", nested: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "a.txt")
			if err := writeFileEncoded(path, tc.content, tc.enc); err != nil {
				t.Fatal(err)
			}
			views := NewFileViews()
			views.saw(path, "before\n")
			ctx := t.Context()
			if tc.nested {
				ctx = tool.MarkNested(ctx)
			}
			args, err := json.Marshal(map[string]any{"path": path, "limit": 1})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (readFile{views: views}).Execute(ctx, args); err != nil {
				t.Fatal(err)
			}
			if err := runViewTool(t, writeFile{views: views}, map[string]string{"path": path, "content": "edited\n"}); err != nil {
				t.Fatalf("whole returned read did not refresh the view: %v", err)
			}
		})
	}
}

func TestFileViewCaptureKeepsPagedReadsStreaming(t *testing.T) {
	content := strings.Repeat("line\n", maxViewBytes)
	src := strings.NewReader(content)
	views := NewFileViews()
	views.saw("a.txt", "before\n")
	out, err := views.captureRead("a.txt", (readFile{}).scan)(src, 0, 1)
	if err != nil || !strings.Contains(out, "1→line") {
		t.Fatalf("read page: %q; %v", out, err)
	}
	if consumed := len(content) - src.Len(); consumed > readFileDetectSample {
		t.Fatalf("recording a page consumed %d unseen bytes", consumed)
	}
	if err := views.checkOverwrite("a.txt", content); !errors.Is(err, ErrFileChangedSinceSeen) {
		t.Fatalf("streamed page certified unread content: %v", err)
	}
}

func TestFirstIncompleteReadCannotPermitALaterStaleOverwrite(t *testing.T) {
	for _, args := range []string{
		`{"path":"a.txt","limit":1}`,
		`{"path":"a.txt","offset":1}`,
	} {
		t.Run(args, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "a.txt")
			if err := os.WriteFile(path, []byte("before\ntail\n"), 0600); err != nil {
				t.Fatal(err)
			}
			views := NewFileViews()
			if _, err := (readFile{workDir: root, views: views}).Execute(t.Context(), json.RawMessage(args)); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("user edit\ntail\n"), 0600); err != nil {
				t.Fatal(err)
			}
			err := runViewTool(t, writeFile{workDir: root, views: views}, map[string]string{"path": path, "content": "stale rewrite"})
			if !errors.Is(err, ErrFileChangedSinceSeen) {
				t.Fatalf("first incomplete read permitted a stale overwrite: %v", err)
			}
		})
	}
}

func TestFirstPartialReadRequiresAWholeReread(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("header\nunseen tail\n"), 0600); err != nil {
		t.Fatal(err)
	}
	views := NewFileViews()
	read := readFile{workDir: root, views: views}
	write := writeFile{workDir: root, views: views}
	if _, err := read.Execute(t.Context(), json.RawMessage(`{"path":"a.txt","limit":1}`)); err != nil {
		t.Fatal(err)
	}
	err := runViewTool(t, write, map[string]string{"path": path, "content": "replacement\n"})
	if !errors.Is(err, ErrFileChangedSinceSeen) {
		t.Fatalf("unseen tail was certified for replacement: %v", err)
	}
	if _, err := read.Execute(t.Context(), json.RawMessage(`{"path":"a.txt"}`)); err != nil {
		t.Fatal(err)
	}
	if err := runViewTool(t, write, map[string]string{"path": path, "content": "replacement\n"}); err != nil {
		t.Fatalf("whole reread did not permit replacement: %v", err)
	}
}

func TestDefaultLimitedReadNamesTheReadThatCertifiesReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "long.txt")
	lines := readFileDefaultLimit + 500
	if err := os.WriteFile(path, []byte(strings.Repeat("line\n", lines)), 0600); err != nil {
		t.Fatal(err)
	}
	views := NewFileViews()
	read := readFile{workDir: root, views: views}
	write := writeFile{workDir: root, views: views}
	if _, err := read.Execute(t.Context(), json.RawMessage(`{"path":"long.txt"}`)); err != nil {
		t.Fatal(err)
	}
	err := runViewTool(t, write, map[string]string{"path": path, "content": "short\n"})
	if !errors.Is(err, ErrFileNotFullySeen) || !strings.Contains(err.Error(), "limit >= 2500") {
		t.Fatalf("a default-limited read was refused without naming the read that certifies it: %v", err)
	}
	if _, err := read.Execute(t.Context(), json.RawMessage(`{"path":"long.txt","limit":2500}`)); err != nil {
		t.Fatal(err)
	}
	if err := runViewTool(t, write, map[string]string{"path": path, "content": "short\n"}); err != nil {
		t.Fatalf("the named read did not certify replacement: %v", err)
	}
}
