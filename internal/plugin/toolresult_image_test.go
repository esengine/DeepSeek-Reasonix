package plugin

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func imageItem(mime, data string) string {
	b, _ := json.Marshal(map[string]string{"type": "image", "data": data, "mimeType": mime})
	return string(b)
}

func TestParseToolResultImageItem(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	res := `{"content":[{"type":"text","text":"before "},` + imageItem("image/png", payload) + `,{"type":"text","text":" after"}]}`
	text, images, err := parseToolResult(json.RawMessage(res))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if want := "before [image: image/png] after"; text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	if len(images) != 1 || images[0] != "data:image/png;base64,"+payload {
		t.Fatalf("images = %v, want one png data URL", images)
	}
}

func TestParseToolResultImageDefaultsToPNG(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("x"))
	res := `{"content":[{"type":"image","data":"` + payload + `"}]}`
	text, images, err := parseToolResult(json.RawMessage(res))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if text != "[image: image/png]" {
		t.Fatalf("text = %q, want png placeholder", text)
	}
	if len(images) != 1 || !strings.HasPrefix(images[0], "data:image/png;base64,") {
		t.Fatalf("images = %v, want png data URL", images)
	}
}

func TestParseToolResultImageNormalizesWrappedBase64(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	wrapped := payload[:4] + "\n" + payload[4:8] + " " + payload[8:]
	res := `{"content":[` + imageItem("image/png", wrapped) + `]}`
	_, images, err := parseToolResult(json.RawMessage(res))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if len(images) != 1 || images[0] != "data:image/png;base64,"+payload {
		t.Fatalf("images = %v, want whitespace stripped to %q", images, payload)
	}
}

func TestParseToolResultImageInvalidBase64Omitted(t *testing.T) {
	res := `{"content":[` + imageItem("image/png", "not-base64!!") + `]}`
	text, images, err := parseToolResult(json.RawMessage(res))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("images = %v, want none for invalid base64", images)
	}
	if !strings.Contains(text, "invalid base64") {
		t.Fatalf("text = %q, want invalid-base64 placeholder", text)
	}
}

func TestParseToolResultImageUnsupportedMimeOmitted(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("bmp"))
	res := `{"content":[` + imageItem("image/bmp", payload) + `]}`
	text, images, err := parseToolResult(json.RawMessage(res))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("images = %v, want none for unsupported mime", images)
	}
	if !strings.Contains(text, "unsupported type image/bmp") {
		t.Fatalf("text = %q, want unsupported-type placeholder", text)
	}
}

func TestParseToolResultImageOversizedOmitted(t *testing.T) {
	// Valid base64 alphabet, over the byte budget.
	big := strings.Repeat("QUFB", maxToolResultImageBytes/4+1)
	res := `{"content":[` + imageItem("image/png", big) + `]}`
	text, images, err := parseToolResult(json.RawMessage(res))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("got %d images, want none for oversized payload", len(images))
	}
	if !strings.Contains(text, "exceeds") {
		t.Fatalf("text = %q, want oversize placeholder", text)
	}
}

func TestParseToolResultImageCountCapped(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("x"))
	items := make([]string, 0, maxToolResultImages+1)
	for i := 0; i < maxToolResultImages+1; i++ {
		items = append(items, imageItem("image/png", payload))
	}
	res := `{"content":[` + strings.Join(items, ",") + `]}`
	text, images, err := parseToolResult(json.RawMessage(res))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if len(images) != maxToolResultImages {
		t.Fatalf("got %d images, want cap of %d", len(images), maxToolResultImages)
	}
	if !strings.Contains(text, "image limit reached") {
		t.Fatalf("text = %q, want limit placeholder for the overflow item", text)
	}
}

func TestParseToolResultErrorStillReturnsImages(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("x"))
	res := `{"content":[{"type":"text","text":"boom "},` + imageItem("image/png", payload) + `],"isError":true}`
	text, images, err := parseToolResult(json.RawMessage(res))
	if err == nil {
		t.Fatal("want error for isError result")
	}
	if !strings.Contains(text, "boom") || !strings.Contains(text, "[image: image/png]") {
		t.Fatalf("text = %q, want text and placeholder preserved", text)
	}
	if len(images) != 1 {
		t.Fatalf("images = %v, want the image parsed even on error", images)
	}
}
