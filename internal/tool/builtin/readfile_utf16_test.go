package builtin

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// TestReadFileBOMlessUTF16LE proves a Windows-style UTF-16 source file with no
// BOM is decoded rather than rejected as binary on the NUL-byte check.
func TestReadFileBOMlessUTF16LE(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CameraManagerLogLoader.cpp")
	src := "// Created by 69431 on 2024/12/31\n#include \"alson/CameraManagerLogLoader.h\"\n"
	var b bytes.Buffer
	for _, u := range utf16.Encode([]rune(src)) {
		_ = binary.Write(&b, binary.LittleEndian, u)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"path": path})
	out, err := readFile{}.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("read rejected BOM-less UTF-16: %v", err)
	}
	if !strings.Contains(out, "Created by 69431") || !strings.Contains(out, "#include") {
		t.Fatalf("UTF-16 content not decoded:\n%s", out)
	}
	if strings.Contains(out, "\x00") {
		t.Fatal("NUL bytes leaked into decoded output")
	}
}
