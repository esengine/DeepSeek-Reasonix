package sessionv4

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/state/sessionv4/v4fixture"
)

func roles(t Transcript) string {
	var b strings.Builder
	for _, m := range t.Messages {
		b.WriteString(string(m.Role)[:1])
	}
	return b.String()
}

// The transcript is the fold of the message events in order: complete
// appends, upsert replaces by id, retract removes, and a history replace
// starts over; the title and model come from their own events.
func TestTranscriptFoldsMessageEvents(t *testing.T) {
	s := v4fixture.New(t)
	dir := s.Session("0123456789abcdef0123456789abcdef", 3)
	s.Batch(dir, v4fixture.Ended,
		v4fixture.Event{Kind: "session/config", Payload: map[string]any{"modelRef": "deepseek/deepseek-flash"}},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m1", "system", "sys")},
		v4fixture.Event{Kind: "turn/start", Payload: map[string]any{}},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m2", "user", "<ctx>", map[string]any{"origin": "host"})},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m3", "user", "hello", map[string]any{"origin": "user"})},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m4", "assistant", "draft")},
	)
	s.Batch(dir, v4fixture.Ended,
		v4fixture.Event{Kind: "message/upsert", Payload: v4fixture.Msg("m4", "assistant", "hi")},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m5", "assistant", "extra")},
		v4fixture.Event{Kind: "message/retract", Payload: map[string]any{"messageIds": []string{"m5"}}},
		v4fixture.Event{Kind: "session/title", Payload: map[string]any{"title": "Greeting"}},
		v4fixture.Event{Kind: "a/future-optional-kind", Payload: map[string]any{}},
	)
	sess, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := sess.Transcript()
	if err != nil {
		t.Fatal(err)
	}
	if roles(tr) != "suua" || tr.Messages[3].Content != "hi" || tr.Title != "Greeting" || tr.ModelRef != "deepseek/deepseek-flash" {
		t.Fatalf("transcript = %+v", tr)
	}
	if !tr.Messages[1].HostAuthored || tr.Messages[2].HostAuthored {
		t.Fatalf("origin not carried: %+v", tr.Messages[1:3])
	}

	s.Batch(dir, v4fixture.Ended, v4fixture.Event{Kind: "history/replace", Payload: map[string]any{"messages": []any{
		v4fixture.Msg("m1", "system", "sys")["message"], v4fixture.Msg("m9", "user", "fresh")["message"],
	}}})
	sess, _ = Open(dir)
	if tr, err = sess.Transcript(); err != nil || roles(tr) != "su" || tr.Messages[1].Content != "fresh" {
		t.Fatalf("history replace: %+v %v", tr, err)
	}
}

// A payload over the inline limit and an image both live in the content pool;
// the image reaches this build as a data URL.
func TestTranscriptResolvesContentPool(t *testing.T) {
	s := v4fixture.New(t)
	dir := s.Session("11111111111111111111111111111111", 3)
	big, _ := json.Marshal(v4fixture.Msg("m1", "user", strings.Repeat("x", 70<<10)))
	img := s.Object([]byte("PNGDATA"), "image/png")
	s.Batch(dir, v4fixture.Ended,
		v4fixture.Event{Kind: "message/complete", Ref: s.Object(big, "application/json")},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m2", "user", "look", map[string]any{
			"image_inputs": []any{map[string]any{"kind": "attachment", "attachment": map[string]any{"v": 1, "content": img}}},
		})},
	)
	sess, _ := Open(dir)
	tr, err := sess.Transcript()
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Messages[0].Content) != 70<<10 || len(tr.Messages[1].Images) != 1 || tr.Messages[1].Images[0] != "data:image/png;base64,UE5HREFUQQ==" {
		t.Fatalf("pool content not resolved: %d chars, images %v", len(tr.Messages[0].Content), tr.Messages[1].Images)
	}
}

// A batch without its end record is a write still in progress and is not part
// of the conversation; a checksum that does not match, or a batch out of
// sequence, is damage.
func TestTornTailIsInvisibleAndDamageIsReported(t *testing.T) {
	s := v4fixture.New(t)
	dir := s.Session("22222222222222222222222222222222", 2)
	s.Batch(dir, v4fixture.Ended, v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m1", "user", "kept")})
	s.Batch(dir, v4fixture.Torn, v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m2", "user", "torn")})
	sess, _ := Open(dir)
	if tr, err := sess.Transcript(); err != nil || roles(tr) != "u" {
		t.Fatalf("torn tail: %+v %v", tr, err)
	}

	bad := s.Session("33333333333333333333333333333333", 2)
	s.Batch(bad, v4fixture.BadSum, v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m1", "user", "a")})
	if sess, _ := Open(bad); !errors.Is(func() error { _, err := sess.Transcript(); return err }(), ErrDamaged) {
		t.Fatal("a wrong checksum was not reported as damage")
	}

	gap := s.Session("44444444444444444444444444444444", 2)
	s.Batch(gap, v4fixture.Ended, v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m1", "user", "a")})
	s.Seq = 1
	s.Batch(gap, v4fixture.Ended, v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m2", "user", "b")})
	if sess, _ := Open(gap); !errors.Is(func() error { _, err := sess.Transcript(); return err }(), ErrDamaged) {
		t.Fatal("a batch out of sequence was not reported as damage")
	}
}

// Only directories with a readable, supported manifest named after them are
// sessions; 1.x's dot entries and unknown versions are skipped.
func TestListSkipsWhatIsNotASession(t *testing.T) {
	s := v4fixture.New(t)
	s.Session("55555555555555555555555555555555", 1)
	s.Session("66666666666666666666666666666666", 9)
	for _, name := range []string{".content-v1", ".query-cache", "notes"} {
		if err := os.MkdirAll(filepath.Join(s.Root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	got := List(s.Root)
	if len(got) != 1 || got[0].Manifest.SessionID != "55555555555555555555555555555555" {
		t.Fatalf("listed %+v", got)
	}
}

func TestOpenBoundsTheManifestSize(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	for name, tc := range map[string]struct {
		size int
		ok   bool
	}{
		"at the cap":   {maxManifestBytes, true},
		"over the cap": {maxManifestBytes + 1, false},
	} {
		t.Run(name, func(t *testing.T) {
			s := v4fixture.New(t)
			dir := s.Session(id, 3)
			raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			padded := append(raw, []byte(strings.Repeat(" ", tc.size-len(raw)))...)
			if err := os.WriteFile(filepath.Join(dir, "manifest.json"), padded, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = Open(dir)
			if tc.ok && err != nil {
				t.Fatalf("Open: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrDamaged) {
				t.Fatalf("Open = %v, want ErrDamaged", err)
			}
		})
	}
}

func TestOpenInRefusesASessionLinkedFromOutsideTheStore(t *testing.T) {
	s := v4fixture.New(t)
	const id = "0123456789abcdef0123456789abcdef"
	outside := s.Session(id, 3)
	store := filepath.Join(t.TempDir(), "store")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(store, id)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got := List(store); len(got) != 0 {
		t.Fatalf("listed %d sessions through a symlink out of the store", len(got))
	}
	if _, err := Open(filepath.Join(store, id)); err == nil {
		t.Fatal("Open followed a symlink out of the store")
	}
}

func TestPathBasedReadsKeepNoDirectoryHandle(t *testing.T) {
	s := v4fixture.New(t)
	const id = "0123456789abcdef0123456789abcdef"
	dir := s.Session(id, 3)
	s.Batch(dir, v4fixture.Ended, v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m1", "user", "hi")})

	var roots []*os.Root
	rootOpened = func(r *os.Root) { roots = append(roots, r) }
	t.Cleanup(func() { rootOpened = nil })

	listed := List(s.Root)
	opened, err := Open(dir)
	if err != nil || len(listed) != 1 {
		t.Fatalf("Open: %v, listed %d", err, len(listed))
	}
	if _, err := opened.Transcript(); err != nil {
		t.Fatal(err)
	}
	opened.CachedListing()
	if len(roots) == 0 {
		t.Fatal("no directory handle was opened")
	}
	for i, r := range roots {
		if _, err := r.Stat("."); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("directory handle %d is still open after reads (stat: %v)", i, err)
		}
	}
	if err := os.Rename(s.Root, s.Root+"-moved"); err != nil {
		t.Fatalf("store cannot be renamed after reading it: %v", err)
	}
}
