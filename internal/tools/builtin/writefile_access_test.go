package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	fileenc "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

type writeSourceProbeOverlay struct {
	reads, writes int
}

func (o *writeSourceProbeOverlay) ReadTextFile(context.Context, string) (string, bool) {
	o.reads++
	return "overlay-canary", true
}
func (o *writeSourceProbeOverlay) WriteTextFile(context.Context, string, string) (bool, error) {
	o.writes++
	return false, nil
}

func TestWriteFileCreatesWithoutReadPermission(t *testing.T) {
	withProtectSensitiveFiles(t, true)
	root := testenv.TempDir(t)
	path := filepath.Join(root, "server.key")
	probe := &writeSourceProbeOverlay{}
	var hadPrior bool
	var prior []byte
	ws := Workspace{Dir: root, ForbidReadRoots: []string{root}, FileOverlay: probe, FileWriteReceipt: func(_ string, old bool, content []byte) { hadPrior, prior = old, content }}
	writer := ws.Tools("write_file")[0]
	args := argsJSON(t, map[string]any{"path": path, "content": "new data\n"})
	if err := ws.TargetAccessCheck()(context.Background(), writer, args); err != nil {
		t.Fatalf("new file incorrectly requires reading: %v", err)
	}
	change, err := writer.(tool.Previewer).Preview(context.Background(), args)
	if err != nil || change.OldText != "" || change.NewText != "new data\n" {
		t.Fatalf("new preview=%+v error=%v", change, err)
	}
	if _, err := writer.Execute(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "new data\n" {
		t.Fatalf("file=%q error=%v", got, err)
	}
	if probe.reads != 0 || hadPrior || len(prior) != 0 {
		t.Fatalf("new file accessed prior content: reads=%d prior=%q", probe.reads, prior)
	}
}

func TestWriteFileRejectsForbiddenExistingSource(t *testing.T) {
	root := testenv.TempDir(t)
	path := filepath.Join(root, "existing.txt")
	const original = "EXISTING_CANARY_DO_NOT_READ\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := &writeSourceProbeOverlay{}
	ws := Workspace{Dir: root, ForbidReadRoots: []string{path}, FileOverlay: probe}
	writer := ws.Tools("write_file")[0]
	args := argsJSON(t, map[string]any{"path": path, "content": "replacement"})
	for _, operation := range []string{"admission", "preview", "execute"} {
		t.Run(operation, func(t *testing.T) {
			var err error
			switch operation {
			case "admission":
				err = ws.TargetAccessCheck()(context.Background(), writer, args)
			case "preview":
				_, err = writer.(tool.Previewer).Preview(context.Background(), args)
			case "execute":
				_, err = writer.Execute(context.Background(), args)
			}
			var refusal tool.Refusal
			if !errors.As(err, &refusal) || refusal.Code != "workspace.overwrite_read_forbidden" || refusal.Message != "Cannot overwrite target file: permission to read existing contents is required." || errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected read-policy refusal, got %v", err)
			}
			if probe.reads != 0 || probe.writes != 0 {
				t.Fatalf("forbidden source reached overlay: %+v", probe)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != original {
				t.Fatalf("protected file=%q error=%v", got, err)
			}
		})
	}
}

func TestWriteFileAllowedSourcePreservesEncodingAndReceipt(t *testing.T) {
	root := testenv.TempDir(t)
	path := filepath.Join(root, "existing.txt")
	const original = "old content\n"
	encoded, err := fileenc.Encode(original, fileenc.UTF16LE)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	hadPrior, prior := false, ""
	ws := Workspace{Dir: root, FileWriteReceipt: func(_ string, old bool, content []byte) { hadPrior, prior = old, string(content) }}
	writer := ws.Tools("write_file")[0]
	args := argsJSON(t, map[string]any{"path": path, "content": "updated content\n"})
	if err := ws.TargetAccessCheck()(context.Background(), writer, args); err != nil {
		t.Fatal(err)
	}
	change, err := writer.(tool.Previewer).Preview(context.Background(), args)
	if err != nil || change.OldText != original {
		t.Fatalf("existing preview=%+v error=%v", change, err)
	}
	if _, err := writer.Execute(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	content, kind, err := readFileEncoded(path)
	if err != nil || kind != fileenc.UTF16LE || content != "updated content\n" || !hadPrior || prior != original {
		t.Fatalf("content=%q kind=%v prior=%q error=%v", content, kind, prior, err)
	}
}

func TestWriteFileRejectsExistingSourceOutsideReadScope(t *testing.T) {
	root := testenv.TempDir(t)
	path := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := Workspace{Dir: root, ReadRoots: []string{testenv.TempDir(t)}}
	writer := ws.Tools("write_file")[0]
	args := argsJSON(t, map[string]any{"path": path, "content": "replacement"})
	for name, operation := range map[string]func() error{
		"admission": func() error { return ws.TargetAccessCheck()(context.Background(), writer, args) },
		"preview":   func() error { _, err := writer.(tool.Previewer).Preview(context.Background(), args); return err },
		"execute":   func() error { _, err := writer.Execute(context.Background(), args); return err },
	} {
		t.Run(name, func(t *testing.T) {
			var refusal tool.Refusal
			err := operation()
			if !errors.As(err, &refusal) || refusal.Code != CodeOverwriteReadOutsideScope || refusal.Message != overwriteReadAccessDeniedMessage {
				t.Fatalf("wrong overwrite scope refusal: %v", err)
			}
		})
	}
}
