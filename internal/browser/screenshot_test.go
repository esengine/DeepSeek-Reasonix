package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

func TestScreenshotReturnsPNGImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeExecutor{screenshot: Screenshot{Path: path, Width: 2, Height: 2}}
	shot := toolByName(t, fake, "browser_screenshot")
	it, ok := shot.(tool.ImageTool)
	if !ok {
		t.Fatal("browser_screenshot does not implement ImageTool")
	}
	args := json.RawMessage(`{"tabId":"t1","ref":"e4","fullPage":true}`)
	text, images, err := it.ExecuteWithImages(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || !strings.HasPrefix(images[0], "data:image/png;base64,") {
		t.Fatalf("images = %v", images)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(images[0], "data:image/png;base64,"))
	if err != nil || !bytes.Equal(decoded, buf.Bytes()) {
		t.Fatalf("image payload does not round-trip: err = %v", err)
	}
	if !strings.Contains(text, "[image: image/png, 2x2]") || !strings.Contains(text, "tab t1") || !strings.Contains(text, path) {
		t.Fatalf("text = %q", text)
	}
	if !reflect.DeepEqual(fake.shots, []ScreenshotRequest{{TabID: "t1", Ref: "e4", FullPage: true}}) {
		t.Fatalf("screenshot requests = %+v", fake.shots)
	}
	out, err := shot.Execute(context.Background(), args)
	if err != nil || !strings.Contains(out, "structured image channel") || strings.Contains(out, "base64") {
		t.Fatalf("Execute out = %q, err = %v", out, err)
	}
}

func TestScreenshotRefusesOversizeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(screenshotMaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	fake := &fakeExecutor{screenshot: Screenshot{Path: path}}
	text, images, err := toolByName(t, fake, "browser_screenshot").(tool.ImageTool).ExecuteWithImages(context.Background(), json.RawMessage(`{"tabId":"t1"}`))
	if err != nil || len(images) != 0 || !strings.Contains(text, "over the 8 MiB limit") {
		t.Fatalf("text = %q, images = %d, err = %v", text, len(images), err)
	}
}

func TestScreenshotMissingFileIsError(t *testing.T) {
	fake := &fakeExecutor{screenshot: Screenshot{Path: filepath.Join(t.TempDir(), "missing.png")}}
	_, _, err := toolByName(t, fake, "browser_screenshot").(tool.ImageTool).ExecuteWithImages(context.Background(), json.RawMessage(`{"tabId":"t1"}`))
	if err == nil || !strings.Contains(err.Error(), "read screenshot") {
		t.Fatalf("err = %v", err)
	}
	if _, blocked := tool.BlockedMessage(err); blocked {
		t.Fatalf("missing file rendered as blocked: %v", err)
	}
}
