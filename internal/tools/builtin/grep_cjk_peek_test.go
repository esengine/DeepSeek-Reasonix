package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A UTF-8 file bigger than grep's 8 KiB peek used to be detected by that peek
// alone, and over CJK text a fixed window lands mid-character about two times
// in three: utf8.Valid failed on the cut byte, GB18030 accepted the bytes, and
// every matched line came back as mojibake.
func TestGrepReadsALargeCJKFileAsWhatItIs(t *testing.T) {
	dir := t.TempDir()
	const needle = "skin_manifest"
	body := strings.Repeat("美术给的那包图 "+needle+"\n", 2000)
	if len(body) <= 8*1024 {
		t.Fatalf("fixture must exceed the peek: %d bytes", len(body))
	}
	if err := os.WriteFile(filepath.Join(dir, "进度.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	args, err := json.Marshal(map[string]any{"pattern": needle, "path": dir})
	if err != nil {
		t.Fatal(err)
	}
	out, err := grepTool{workDir: dir}.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	head, _, _ := strings.Cut(out, "\n")
	if !strings.Contains(out, "美术给的那包图") {
		t.Fatalf("grep returned the file in the wrong charset: %s", head)
	}
}
