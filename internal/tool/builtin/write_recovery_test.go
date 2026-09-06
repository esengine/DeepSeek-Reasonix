package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	fileenc "reasonix/internal/fileutil/encoding"
	"reasonix/internal/tool"
)

func TestWriteIntentDurabilityBeforeMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("intent store unavailable")
	ctx := tool.WithWriteIntentHook(context.Background(), func(tool.FileWriteIntent) error { return failure })
	args, _ := json.Marshal(map[string]string{"path": path, "content": "after"})
	if _, err := (writeFile{}).Execute(ctx, args); !errors.Is(err, failure) {
		t.Fatalf("err=%v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "before" {
		t.Fatal("write preceded durable intent")
	}
}
func TestWriteRecoveryVerifiesActualPostcondition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	var intent tool.FileWriteIntent
	ctx := tool.WithWriteIntentHook(context.Background(), func(i tool.FileWriteIntent) error { intent = i; return nil })
	args, _ := json.Marshal(map[string]string{"path": path, "content": "after"})
	writer := writeFile{}
	if _, err := writer.Execute(ctx, args); err != nil {
		t.Fatal(err)
	}
	if got := writer.VerifyWrite(ctx, intent); got != tool.WriteSatisfied {
		t.Fatalf("got %s intent=%+v", got, intent)
	}
	if err := os.WriteFile(path, []byte("external change"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := writer.VerifyWrite(ctx, intent); got != tool.WriteConflict {
		t.Fatalf("got %s", got)
	}
	intent.Version = 99
	if got := writer.VerifyWrite(ctx, intent); got != tool.WriteUnknown {
		t.Fatalf("future version=%s", got)
	}
}

type recoverableOverlay struct {
	content   string
	available bool
	id        string
}

func (o *recoverableOverlay) ReadTextFile(context.Context, string) (string, bool) {
	return o.content, o.available
}
func (o *recoverableOverlay) WriteTextFile(_ context.Context, _ string, text string) (bool, error) {
	if !o.available {
		return false, nil
	}
	o.content = text
	return true, nil
}
func (o *recoverableOverlay) RecoveryIdentity() string { return o.id }
func TestWriteRecoveryUsesOriginalOverlay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer.txt")
	if err := os.WriteFile(path, []byte("disk"), 0600); err != nil {
		t.Fatal(err)
	}
	overlay := &recoverableOverlay{content: "unsaved", available: true, id: "transport-one"}
	writer := writeFile{overlay: overlay}
	var intent tool.FileWriteIntent
	ctx := tool.WithWriteIntentHook(context.Background(), func(i tool.FileWriteIntent) error { intent = i; return nil })
	args, _ := json.Marshal(map[string]string{"path": path, "content": "edited buffer"})
	if _, err := writer.Execute(ctx, args); err != nil {
		t.Fatal(err)
	}
	if got := writer.VerifyWrite(ctx, intent); got != tool.WriteSatisfied {
		t.Fatalf("overlay=%s", got)
	}
	overlay.available = false
	if got := writer.VerifyWrite(ctx, intent); got != tool.WriteUnknown {
		t.Fatalf("unavailable=%s", got)
	}
	overlay.available = true
	overlay.id = "replacement-transport"
	if got := writer.VerifyWrite(ctx, intent); got != tool.WriteUnknown {
		t.Fatalf("replacement=%s", got)
	}
}

func TestWriteIntentFailureDoesNotCreateDirectories(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "new", "nested", "file.txt")
	ctx := tool.WithWriteIntentHook(context.Background(), func(tool.FileWriteIntent) error { return errors.New("disk full") })
	args, _ := json.Marshal(map[string]string{"path": path, "content": "new"})
	if _, err := (writeFile{}).Execute(ctx, args); err == nil {
		t.Fatal("expected durable intent error")
	}
	if _, err := os.Stat(filepath.Join(root, "new")); !os.IsNotExist(err) {
		t.Fatalf("directory created before evidence: %v", err)
	}
}

func TestWriteRecoveryRejectsChangedSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	first, second, link := filepath.Join(root, "first"), filepath.Join(root, "second"), filepath.Join(root, "link")
	for _, p := range []string{first, second} {
		if err := os.WriteFile(p, []byte("before"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(first, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var intent tool.FileWriteIntent
	ctx := tool.WithWriteIntentHook(context.Background(), func(i tool.FileWriteIntent) error { intent = i; return nil })
	args, _ := json.Marshal(map[string]string{"path": link, "content": "after"})
	w := writeFile{}
	if _, err := w.Execute(ctx, args); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	if got := w.VerifyWrite(ctx, intent); got != tool.WriteUnknown {
		t.Fatalf("changed symlink=%s", got)
	}
}

func TestWriteRecoveryChecksEncoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "encoded.txt")
	if err := os.WriteFile(path, fileenc.Encode("before", fileenc.UTF16LE), 0600); err != nil {
		t.Fatal(err)
	}
	var intent tool.FileWriteIntent
	ctx := tool.WithWriteIntentHook(context.Background(), func(i tool.FileWriteIntent) error { intent = i; return nil })
	args, _ := json.Marshal(map[string]string{"path": path, "content": "after"})
	w := writeFile{}
	if _, err := w.Execute(ctx, args); err != nil {
		t.Fatal(err)
	}
	if got := w.VerifyWrite(ctx, intent); got != tool.WriteSatisfied {
		t.Fatalf("encoded=%s", got)
	}
	if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := w.VerifyWrite(ctx, intent); got != tool.WriteConflict {
		t.Fatalf("changed encoding=%s", got)
	}
}
