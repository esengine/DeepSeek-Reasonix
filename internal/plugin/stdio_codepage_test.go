package plugin

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const codePageNotFound = "'read-only-mysql-mcp-server' 不是内部或外部命令，也不是可运行的程序"

// cmd.exe on a Chinese Windows reports a missing command in the console code
// page, not UTF-8; the failure the settings page shows must still be readable.
func TestStdioFailureDecodesCodePageStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	host, _ := StartAvailable(ctx, []Spec{{
		Name:    "codepage",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestCodePageStderrHelper", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_STDERR_CODEPAGE": "1"},
	}})
	defer host.Close()

	failures := host.Failures()
	if len(failures) != 1 {
		t.Fatalf("failures = %+v, want one", failures)
	}
	if !strings.Contains(failures[0].Error, codePageNotFound) {
		t.Fatalf("failure should carry the decoded stderr, got %q", failures[0].Error)
	}
}

// TestCodePageStderrHelper is not a real test: as a child it writes a GBK
// line to stderr and exits, the way cmd.exe reports a missing command.
func TestCodePageStderrHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_STDERR_CODEPAGE") != "1" {
		return
	}
	b, _ := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(codePageNotFound))
	os.Stderr.Write(b)
	os.Exit(1)
}

func gbk(t *testing.T, s string) []byte {
	t.Helper()
	b, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A GBK lead byte is not a UTF-8 rune start, so a tail that begins with a CJK
// character must be decoded whole; only a tail the limit really cut may lose
// part of one.
func TestStderrTailDecodesTheServersCodePage(t *testing.T) {
	const line = "参数格式不正确，服务器无法启动"
	whole := gbk(t, line)
	for _, tc := range []struct {
		name  string
		limit int
		write [][]byte
		want  string
	}{
		{"uncut GBK starting with CJK", 16 * 1024, [][]byte{whole}, line},
		{"cut GBK starting with CJK", len(whole), [][]byte{gbk(t, "旧输出"), whole}, line},
		{"UTF-8 cut mid-character", len(line) - 1, [][]byte{[]byte(line)}, line[len("参"):]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &tailBuffer{limit: tc.limit}
			for _, p := range tc.write {
				_, _ = b.Write(p)
			}
			if got := b.String(); got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
