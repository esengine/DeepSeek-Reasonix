package shellrun

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"

	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

const codePageLine = "FIND: 参数格式不正确\r\n"

func gbkBytes(t *testing.T, s string) []byte {
	t.Helper()
	b, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func octalPrintf(b []byte) string {
	var sb strings.Builder
	sb.WriteString("printf '")
	for _, c := range b {
		fmt.Fprintf(&sb, "\\%03o", c)
	}
	sb.WriteString("'")
	return sb.String()
}

// A Windows console tool answers in the machine's code page (cp936 on a Chinese
// install); left undecoded, JSON turns every byte of it into U+FFFD.
func TestRunForegroundDecodesCodePageOutput(t *testing.T) {
	argv, sh := shellArgv(t, octalPrintf(gbkBytes(t, codePageLine))+"; exit 3")
	if sh.Kind == sandbox.ShellPowerShell {
		t.Skip("printf octal escapes need a POSIX shell")
	}
	res := RunForeground(context.Background(), Request{Argv: argv, Timeout: 30 * time.Second})
	if res.State != tool.ShellStateFailed {
		t.Fatalf("state = %q", res.State)
	}
	if res.Combined != codePageLine {
		t.Fatalf("Combined = %q, want %q", res.Combined, codePageLine)
	}
	if res.OutputTail != codePageLine {
		t.Fatalf("OutputTail = %q, want %q", res.OutputTail, codePageLine)
	}
}

// A GBK lead byte is not a UTF-8 rune start, so output that begins with a CJK
// character must be decoded from its raw bytes; only an edge a buffer really
// cut may lose part of a character.
func TestBuffersDecodeCodePageOutputStartingWithCJK(t *testing.T) {
	const text = "参数格式不正确"
	line := gbkBytes(t, text)
	t.Run("uncut tail", func(t *testing.T) {
		c := newOutputCollector(1<<20, 1<<10)
		_, _ = c.tail.Write(line)
		if got := c.tailString(); got != text {
			t.Fatalf("OutputTail = %q, want %q", got, text)
		}
	})
	t.Run("cut tail", func(t *testing.T) {
		c := newOutputCollector(1<<20, len(line))
		_, _ = c.tail.Write(gbkBytes(t, "旧输出"))
		_, _ = c.tail.Write(line)
		if got := c.tailString(); got != text {
			t.Fatalf("OutputTail = %q, want %q", got, text)
		}
	})
	t.Run("uncut combined", func(t *testing.T) {
		c := newOutputCollector(1<<20, 1<<10)
		_, _ = c.combined.Write(line)
		if got := c.combined.String(); got != text {
			t.Fatalf("Combined = %q, want %q", got, text)
		}
	})
	t.Run("cut combined", func(t *testing.T) {
		b := &boundedBuffer{mu: &sync.Mutex{}, limit: 3*len(line) + 6 + len(line) + 3, tailLimit: len(line), marker: "..."}
		for range 10 {
			_, _ = b.Write(line)
		}
		want := strings.Repeat(text, 3) + "参数格" + "..." + text
		if got := b.String(); got != want {
			t.Fatalf("Combined = %q, want %q", got, want)
		}
	})
}
