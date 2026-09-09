package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

func writeEnvelopeFixture(t *testing.T, name string, lines int) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, name)
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func readEnvelope(t *testing.T, r readFile, args string) (tool.ReadResultEnvelope, string, bool) {
	t.Helper()
	out, env, err := r.ExecuteRead(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("Execute(%s): %v", args, err)
	}
	return env, out, true
}

func TestReadEnvelopeInspectDefaultsToOneBoundedPage(t *testing.T) {
	dir, _ := writeEnvelopeFixture(t, "a.go", 5)
	r := readFile{workDir: dir}
	env, _, ok := readEnvelope(t, r, `{"path":"a.go"}`)
	if !ok {
		t.Fatal("ReadEnvelope must describe an inspect read")
	}
	if env.ProtocolVersion != tool.ReadResultProtocolVersion {
		t.Fatalf("ProtocolVersion = %d, want %d", env.ProtocolVersion, tool.ReadResultProtocolVersion)
	}
	if env.Intent != tool.ReadIntentInspect {
		t.Fatalf("Intent = %q, want %q", env.Intent, tool.ReadIntentInspect)
	}
	if env.RequestedRange != nil {
		t.Fatalf("an inspect read requests no window, got %+v", env.RequestedRange)
	}
	if got, want := env.DeliveredRanges, []tool.ReadRange{{Start: 0, End: 5}}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("DeliveredRanges = %+v, want %+v", got, want)
	}
	if env.HasMore || !env.EOF || env.SourceCut != tool.ReadCutNone {
		t.Fatalf("small file must read to EOF: %+v", env)
	}
	if env.NextCursor != "" {
		t.Fatalf("no continuation cursor at EOF, got %q", env.NextCursor)
	}
	if !filepath.IsAbs(env.Source.CanonicalPath) || env.Source.Snapshot == "" {
		t.Fatalf("source identity missing: %+v", env.Source)
	}
}

func TestReadEnvelopeRangeReportsRequestedAndDeliveredWindow(t *testing.T) {
	dir, _ := writeEnvelopeFixture(t, "a.go", 10)
	r := readFile{workDir: dir}
	env, _, ok := readEnvelope(t, r, `{"path":"a.go","offset":2,"limit":3}`)
	if !ok {
		t.Fatal("ReadEnvelope must describe an explicit range")
	}
	if env.Intent != tool.ReadIntentRange {
		t.Fatalf("Intent = %q, want %q", env.Intent, tool.ReadIntentRange)
	}
	if env.RequestedRange == nil || *env.RequestedRange != (tool.ReadRange{Start: 2, End: 5}) {
		t.Fatalf("RequestedRange = %+v, want {2 5}", env.RequestedRange)
	}
	if got, want := env.DeliveredRanges, []tool.ReadRange{{Start: 2, End: 5}}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("DeliveredRanges = %+v, want %+v", got, want)
	}
	if !env.HasMore || env.EOF || env.SourceCut != tool.ReadCutPageLimit {
		t.Fatalf("paged read must report a page cut: %+v", env)
	}
	cursor, ok := tool.DecodeReadCursor(env.NextCursor)
	if !ok || cursor.NextStart != 5 {
		t.Fatalf("NextCursor = %q (cursor %+v, ok=%v), want next_start 5", env.NextCursor, cursor, ok)
	}
	if cursor.Path != env.Source.CanonicalPath || cursor.Snapshot != env.Source.Snapshot || cursor.NextStart != 5 {
		t.Fatalf("cursor %+v must name this envelope's source and position", cursor)
	}
}

func TestReadEnvelopePastEOFDeliversNothing(t *testing.T) {
	dir, _ := writeEnvelopeFixture(t, "a.go", 3)
	r := readFile{workDir: dir}
	env, _, ok := readEnvelope(t, r, `{"path":"a.go","offset":99,"limit":10}`)
	if !ok {
		t.Fatal("ReadEnvelope must describe a past-EOF read")
	}
	if len(env.DeliveredRanges) != 0 || !env.EOF || env.HasMore {
		t.Fatalf("past-EOF read delivered content: %+v", env)
	}
	if env.SourceEnd == nil || *env.SourceEnd != 3 {
		t.Fatalf("past-EOF read must still report the source end, got %+v", env.SourceEnd)
	}
	if env.Source.Snapshot == "" {
		t.Fatal("an existing file must still carry a source snapshot")
	}
}

func TestReadEnvelopeEmptyFileIsEOF(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r := readFile{workDir: dir}
	env, _, ok := readEnvelope(t, r, `{"path":"empty.txt"}`)
	if !ok || !env.EOF || env.HasMore || len(env.DeliveredRanges) != 0 {
		t.Fatalf("empty file envelope = %+v (ok=%v)", env, ok)
	}
}

func TestReadEnvelopeVersionTokenFollowsDeliveredContent(t *testing.T) {
	dir, path := writeEnvelopeFixture(t, "a.go", 4)
	r := readFile{workDir: dir}
	before, _, ok := readEnvelope(t, r, `{"path":"a.go"}`)
	if !ok {
		t.Fatal("first read must produce an envelope")
	}
	if err := os.WriteFile(path, []byte("line 1\nline 2 changed\nline 3\nline 4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, _, ok := readEnvelope(t, r, `{"path":"a.go"}`)
	if !ok {
		t.Fatal("second read must produce an envelope")
	}
	if before.Source.Snapshot == after.Source.Snapshot {
		t.Fatal("an edited line must change the content version token")
	}
	if before.DeliveredRanges[0] != after.DeliveredRanges[0] {
		t.Fatalf("window geometry changed unexpectedly: %+v vs %+v", before.DeliveredRanges, after.DeliveredRanges)
	}
}

// TestReadEnvelopeSeparatesSourceIdentityFromWindowDigest pins the v2 split: a
// line-terminator rewrite moves the source snapshot (the file changed) while the
// decoded window digest stays put, so neither can stand in for the other.
func TestReadEnvelopeSeparatesSourceIdentityFromWindowDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("alpha\r\nbeta\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := readFile{workDir: dir}
	crlf, _, ok := readEnvelope(t, r, `{"path":"a.txt"}`)
	if !ok {
		t.Fatal("crlf read must produce an envelope")
	}
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lf, _, ok := readEnvelope(t, r, `{"path":"a.txt"}`)
	if !ok {
		t.Fatal("lf read must produce an envelope")
	}
	if crlf.Source.Snapshot == lf.Source.Snapshot {
		t.Fatalf("a raw byte change must move the source snapshot: %q", crlf.Source.Snapshot)
	}
	if crlf.WindowDigest != lf.WindowDigest {
		t.Fatalf("decoded window digest changed unexpectedly: %q vs %q", crlf.WindowDigest, lf.WindowDigest)
	}
	if crlf.DeliveredRanges[0] != lf.DeliveredRanges[0] {
		t.Fatalf("decoded windows differ: %+v vs %+v", crlf.DeliveredRanges, lf.DeliveredRanges)
	}
}

func TestReadEnvelopeKeepsUnicodeWindowsIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "u.go")
	body := "设置面板 → 标题\n第二行\nemoji 🙂 结束\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r := readFile{workDir: dir}
	env, out, ok := readEnvelope(t, r, `{"path":"u.go","offset":1,"limit":2}`)
	if !ok {
		t.Fatal("unicode read must produce an envelope")
	}
	if got, want := env.DeliveredRanges, []tool.ReadRange{{Start: 1, End: 3}}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("DeliveredRanges = %+v, want %+v", got, want)
	}
	window, ok := tool.ParseReadWindow(out)
	if !ok || len(window.Lines) != 2 {
		t.Fatalf("rendered window = %+v (ok=%v)", window, ok)
	}
	if window.Lines[0] != "第二行" {
		t.Fatalf("first delivered line = %q, want 第二行", window.Lines[0])
	}
	if want := tool.WindowDigest(env.Source.CanonicalPath, window); env.WindowDigest != want {
		t.Fatalf("WindowDigest = %q, want %q", env.WindowDigest, want)
	}
	again, _, _ := readEnvelope(t, r, `{"path":"u.go","offset":1,"limit":2}`)
	if again.Source.Snapshot != env.Source.Snapshot {
		t.Fatal("identical unicode window must produce a stable version token")
	}
	observed, ok := r.ObserveModelText(json.RawMessage(`{"path":"u.go","offset":1,"limit":2}`), out)
	if !ok || observed.Version != env.WindowDigest {
		t.Fatalf("observation version %q must match the envelope window digest %q (ok=%v)", observed.Version, env.WindowDigest, ok)
	}
}

func TestReadIntentDefaultsAndExplicitForms(t *testing.T) {
	dir, _ := writeEnvelopeFixture(t, "a.go", 10)
	r := readFile{workDir: dir}
	cases := []struct {
		args       string
		wantIntent tool.ReadIntent
		wantWindow bool
	}{
		{`{"path":"a.go"}`, tool.ReadIntentInspect, false},
		{`{"path":"a.go","offset":1}`, tool.ReadIntentRange, true},
		{`{"path":"a.go","limit":5}`, tool.ReadIntentRange, true},
		{`{"path":"a.go","intent":"full"}`, tool.ReadIntentFull, false},
		{`{"path":"a.go","intent":"inspect","offset":1,"limit":5}`, tool.ReadIntentInspect, true},
	}
	for _, tc := range cases {
		t.Run(tc.args, func(t *testing.T) {
			env, _, ok := readEnvelope(t, r, tc.args)
			if !ok {
				t.Fatalf("ReadEnvelope(%s) reported no envelope", tc.args)
			}
			if env.Intent != tc.wantIntent {
				t.Fatalf("Intent = %q, want %q", env.Intent, tc.wantIntent)
			}
			if (env.RequestedRange != nil) != tc.wantWindow {
				t.Fatalf("RequestedRange = %+v, want present=%v", env.RequestedRange, tc.wantWindow)
			}
		})
	}
}

func TestReadIntentRejectsConflictingParameters(t *testing.T) {
	dir, _ := writeEnvelopeFixture(t, "a.go", 3)
	r := readFile{workDir: dir}
	cases := []struct {
		name string
		args string
		want string
	}{
		{"full with a window", `{"path":"a.go","intent":"full","offset":1}`, "intent=full cannot be combined"},
		{"full with a limit", `{"path":"a.go","intent":"full","limit":10}`, "intent=full cannot be combined"},
		{"range without a window", `{"path":"a.go","intent":"range"}`, "intent=range requires"},
		{"unknown intent", `{"path":"a.go","intent":"peek"}`, "intent must be inspect, range, or full"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.Execute(context.Background(), json.RawMessage(tc.args))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Execute(%s) error = %v, want one containing %q", tc.args, err, tc.want)
			}
			if _, ok := r.ReadEnvelope(context.Background(), json.RawMessage(tc.args), "  1→a\n"); ok {
				t.Fatalf("ReadEnvelope(%s) must not describe a rejected call", tc.args)
			}
		})
	}
}

func TestReadEnvelopeRejectsCallsWithoutAPath(t *testing.T) {
	r := readFile{workDir: t.TempDir()}
	if _, ok := r.ReadEnvelope(context.Background(), json.RawMessage(`{"offset":1}`), "   1→a\n"); ok {
		t.Fatal("a call without a path has no envelope")
	}
	if _, ok := r.ReadEnvelope(context.Background(), json.RawMessage(`not json`), "   1→a\n"); ok {
		t.Fatal("malformed args have no envelope")
	}
}

func TestReadEnvelopeNeverAppearsInProviderText(t *testing.T) {
	dir, _ := writeEnvelopeFixture(t, "a.go", 10)
	r := readFile{workDir: dir}
	_, out, _ := readEnvelope(t, r, `{"path":"a.go","offset":2,"limit":3}`)
	for _, leak := range []string{"protocol_version", "version_token", "delivered_ranges", "next_cursor", "read_id"} {
		if strings.Contains(out, leak) {
			t.Fatalf("host-only envelope field %q leaked into reader output:\n%s", leak, out)
		}
	}
}

func TestReadEnvelopeNamesTheServingStore(t *testing.T) {
	dir, _ := writeEnvelopeFixture(t, "a.go", 10)
	r := readFile{workDir: dir}
	first, _, ok := readEnvelope(t, r, `{"path":"a.go","offset":0,"limit":4}`)
	if !ok {
		t.Fatal("first page must produce an envelope")
	}
	if first.Source.Kind != tool.ReadSourceDisk || first.Source.Identity == "" || first.Source.Snapshot == "" {
		t.Fatalf("source identity missing: %+v", first.Source)
	}
	second, _, ok := readEnvelope(t, r, `{"path":"a.go","offset":4,"limit":4}`)
	if !ok {
		t.Fatal("second page must produce an envelope")
	}
	if second.Source.Snapshot != first.Source.Snapshot {
		t.Fatalf("one read's snapshot must not move between pages: %q vs %q", first.Source.Snapshot, second.Source.Snapshot)
	}
	if second.WindowDigest == first.WindowDigest {
		t.Fatal("different windows must have different digests")
	}
}

func TestReadEnvelopeSourceEndIsTrustworthy(t *testing.T) {
	dir, _ := writeEnvelopeFixture(t, "a.go", 3)
	r := readFile{workDir: dir}
	complete, _, ok := readEnvelope(t, r, `{"path":"a.go"}`)
	if !ok || complete.SourceEnd == nil || *complete.SourceEnd != 3 {
		t.Fatalf("a complete read must report the source end: %+v", complete.SourceEnd)
	}
	pastEOF, _, ok := readEnvelope(t, r, `{"path":"a.go","offset":99,"limit":10}`)
	if !ok || pastEOF.SourceEnd == nil || *pastEOF.SourceEnd != 3 {
		t.Fatalf("a past-EOF read must report the source end: %+v", pastEOF.SourceEnd)
	}
	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, "empty.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	er := readFile{workDir: empty}
	env, _, ok := readEnvelope(t, er, `{"path":"empty.txt"}`)
	if !ok || env.SourceEnd == nil || *env.SourceEnd != 0 {
		t.Fatalf("an empty file must report a zero source end: %+v", env.SourceEnd)
	}
}
