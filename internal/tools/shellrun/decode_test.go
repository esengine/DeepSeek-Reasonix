package shellrun

import (
	"context"
	"os"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"

	fileenc "reasonix/internal/base/fileutil/encoding"
)

const codePageLine = "FIND: 参数格式不正确\r\n"

func TestCodePageOutputChild(t *testing.T) {
	if os.Getenv("REASONIX_TEST_CODEPAGE_CHILD") != "1" {
		return
	}
	data := gbkBytes(t, codePageLine)
	_, _ = os.Stdout.Write(data[:8])
	_, _ = os.Stdout.Write(data[8:])
	os.Exit(0)
}

func TestForegroundCodePageProgressMatchesFinalOutput(t *testing.T) {
	var progress strings.Builder
	res := RunForeground(context.Background(), Request{
		Argv:     []string{os.Args[0], "-test.run=^TestCodePageOutputChild$"},
		Env:      append(os.Environ(), "REASONIX_TEST_CODEPAGE_CHILD=1"),
		Progress: func(chunk string) { progress.WriteString(chunk) },
	})
	if res.Err != nil {
		t.Fatalf("run: %v", res.Err)
	}
	if res.Combined != codePageLine {
		t.Fatalf("final output = %q, want %q", res.Combined, codePageLine)
	}
	if got := progress.String(); got != codePageLine {
		t.Fatalf("progress = %q, want %q", got, codePageLine)
	}
}

func TestProgressDecodesSplitCodePageAndUTF8Characters(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{
		{"GBK", gbkBytes(t, "参数\n"), "参数\n"},
		{"GBK without newline", gbkBytes(t, "参数"), "参数"},
		{"UTF-8", []byte("参数\n"), "参数\n"},
		{"UTF-8 without newline", []byte("参数"), "参数"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got strings.Builder
			w := newProgressWriter(func(s string) { got.WriteString(s) }, 1<<20, "")
			for _, b := range tc.data {
				_, _ = w.Write([]byte{b})
			}
			w.Flush()
			if got.String() != tc.want {
				t.Fatalf("progress = %q, want %q", got.String(), tc.want)
			}
		})
	}
}

func TestProgressKeepsASCIIPartialsLive(t *testing.T) {
	var got strings.Builder
	w := newProgressWriter(func(s string) { got.WriteString(s) }, 1<<20, "")
	_, _ = w.Write([]byte("building..."))
	if got.String() != "building..." {
		t.Fatalf("partial ASCII progress = %q", got.String())
	}
}

func TestProgressCapDoesNotSplitCodePageCharacter(t *testing.T) {
	var got strings.Builder
	w := newProgressWriter(func(s string) { got.WriteString(s) }, 3, "<truncated>")
	_, _ = w.Write(gbkBytes(t, "参数"))
	w.Flush()
	if got.String() != "参<truncated>" {
		t.Fatalf("bounded progress = %q", got.String())
	}
}

func gbkBytes(t *testing.T, s string) []byte {
	t.Helper()
	b, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A Windows console tool answers in the machine's code page. The bytes used to
// be kept as a Go string unchanged, and JSON then coerced every invalid one to
// U+FFFD: a run that failed four times told the model nothing about why.
func TestShellOutputInTheMachineCodePageIsReadable(t *testing.T) {
	if got := decodeShellOutput(gbkBytes(t, codePageLine), fileenc.Cut{}); got != codePageLine {
		t.Fatalf("decodeShellOutput = %q, want %q", got, codePageLine)
	}
}

// UTF-8 output is passed through, less a character the buffer cut in half —
// re-reading that as the code page would invent mojibake where truncation was
// the only problem.
func TestUTF8OutputIsNotReinterpreted(t *testing.T) {
	full := "参数格式不正确 ok\n"
	if got := decodeShellOutput([]byte(full), fileenc.Cut{}); got != full {
		t.Fatalf("valid UTF-8 was rewritten: %q", got)
	}
	if got := decodeShellOutput([]byte(full)[1:], fileenc.Cut{Head: true}); got != full[3:] {
		t.Fatalf("a front-truncated tail was reinterpreted: %q", got)
	}
	if tail := []byte(full)[:len(full)-4]; decodeShellOutput(tail, fileenc.Cut{Tail: true}) != string(tail) {
		t.Fatal("a back-truncated tail was reinterpreted")
	}
	if got := decodeShellOutput(nil, fileenc.Cut{}); got != "" {
		t.Fatalf("empty output = %q", got)
	}
	if got := decodeShellOutput([]byte("plain ascii"), fileenc.Cut{}); !strings.Contains(got, "ascii") {
		t.Fatalf("ascii = %q", got)
	}
}

// The collector is what a run's output actually passes through, so the decode
// has to sit there rather than in a helper the caller could stop using.
func TestTheCollectorDecodesWhatTheChildWrote(t *testing.T) {
	gbk := gbkBytes(t, codePageLine)
	c := newOutputCollector(1<<20, 1<<10)
	if _, err := c.combined.Write(gbk); err != nil {
		t.Fatal(err)
	}
	if _, err := c.tail.Write(gbk); err != nil {
		t.Fatal(err)
	}
	if got := c.combinedString(); got != codePageLine {
		t.Fatalf("combined = %q, want %q", got, codePageLine)
	}
	if got := c.tailString(); got != codePageLine {
		t.Fatalf("tail = %q, want %q", got, codePageLine)
	}
}

// Output cut short inside a code-page character is still code-page text. The
// cut fails the byte-for-byte round trip a whole file must pass before it may
// be rewritten, and output is only ever read.
func TestCodePageOutputCutMidCharacterIsReadable(t *testing.T) {
	gbk := gbkBytes(t, codePageLine)
	cut := gbk[:len(gbk)-3]
	if got := decodeShellOutput(cut, fileenc.Cut{Tail: true}); !strings.HasPrefix(got, "FIND: 参数格式不正") {
		t.Fatalf("decodeShellOutput = %q, want the code-page text up to the cut", got)
	}
}
