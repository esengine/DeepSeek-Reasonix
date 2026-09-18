package builtin

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reasonix/internal/tool"
	"strings"
	"testing"
)

func TestViewImageWorkspaceAndConfinement(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "picture.bin")
	if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	ts := (Workspace{Dir: dir}).Tools("view_image")
	if len(ts) != 1 {
		t.Fatalf("tools: %v", ts)
	}
	out, images, err := ts[0].(tool.ImageTool).ExecuteWithImages(context.Background(), argsJSON(t, map[string]any{"path": "picture.bin"}))
	if err != nil || !strings.Contains(out, "2x3") || len(images) != 1 || images[0] != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(b.Bytes()) {
		t.Fatalf("output=%s images=%v err=%v", out, images, err)
	}
	alias := filepath.Join(t.TempDir(), "alias.png")
	if err := os.Symlink(path, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, p := range []string{path, alias} {
		_, images, err := (viewImage{forbidRoots: realRoots([]string{dir})}).ExecuteWithImages(context.Background(), argsJSON(t, map[string]any{"path": p}))
		if err == nil || len(images) != 0 {
			t.Fatalf("forbidden image read: %s", p)
		}
	}
}

func TestViewImageAttachmentUsesWorkspaceInsteadOfProcessCWD(t *testing.T) {
	workspace := t.TempDir()
	processDir := t.TempDir()
	rel := filepath.Join(".reasonix", "attachments", "shared.png")
	for root, bounds := range map[string]image.Rectangle{
		workspace:  image.Rect(0, 0, 2, 3),
		processDir: image.Rect(0, 0, 7, 9),
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		var data bytes.Buffer
		if err := png.Encode(&data, image.NewRGBA(bounds)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(processDir)
	tool := (Workspace{Dir: workspace}).Tools("view_image")[0].(tool.ImageTool)
	out, _, err := tool.ExecuteWithImages(context.Background(), argsJSON(t, map[string]any{"path": filepath.ToSlash(rel)}))
	if err != nil || !strings.Contains(out, "2x3") {
		t.Fatalf("output=%q err=%v, want workspace image dimensions 2x3", out, err)
	}
}

func TestViewImageRejectsInvalidInputs(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string][]byte{"text.png": []byte("not an image"), "large.png": make([]byte, viewImageMaxBytes+1)} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
		_, images, err := (viewImage{}).ExecuteWithImages(context.Background(), argsJSON(t, map[string]any{"path": p}))
		if err == nil || len(images) != 0 {
			t.Fatalf("accepted %s", name)
		}
	}
	for _, p := range []string{"", dir, filepath.Join(dir, "missing")} {
		_, _, err := (viewImage{}).ExecuteWithImages(context.Background(), argsJSON(t, map[string]any{"path": p}))
		if err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (viewImage{}).ExecuteWithImages(ctx, argsJSON(t, map[string]any{"path": "x"})); !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v", err)
	}
}

func TestViewImageExternalAlias(t *testing.T) {
	external := t.TempDir()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "x.png"), b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	resolver := NewPathResolver()
	token := "__reasonix_external_folder/test/Images"
	resolver.RegisterReadRoot(token, external)
	ts := (Workspace{Dir: t.TempDir(), ReadPaths: resolver}).Tools("view_image")
	for _, name := range []string{"x.png", "missing.png"} {
		out, _, err := ts[0].(tool.ImageTool).ExecuteWithImages(context.Background(), argsJSON(t, map[string]any{"path": token + "/" + name}))
		if name == "x.png" && err != nil {
			t.Fatal(err)
		}
		if err != nil {
			out += err.Error()
		}
		if strings.Contains(out, external) || !strings.Contains(out, token) {
			t.Fatalf("alias not preserved: %s", out)
		}
	}
}
