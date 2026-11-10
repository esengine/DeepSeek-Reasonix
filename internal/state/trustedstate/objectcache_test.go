package trustedstate

import (
	"errors"
	"io/fs"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// settled backdates an object's modification time past the racy window, so the
// cache treats its stat as meaningful, the way a file written a moment ago is not.
func settled(t *testing.T, path string) fs.FileInfo {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func countingStore(t *testing.T) (*Store, *atomic.Int32) {
	s := Open(testenv.TempDir(t), nil)
	var reads atomic.Int32
	s.readFile = func(p string) ([]byte, error) { reads.Add(1); return os.ReadFile(p) }
	return s, &reads
}

func TestRepeatedObjectReadsOfAnUnchangedFileReadItOnce(t *testing.T) {
	s, reads := countingStore(t)
	d, err := s.PutObject([]byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	path, _ := s.objectPath(d)
	settled(t, path)
	for range 5 {
		got, err := s.Object(d)
		if err != nil || string(got) != "payload" {
			t.Fatalf("Object = %q, %v", got, err)
		}
	}
	if n := reads.Load(); n != 1 {
		t.Fatalf("file read %d times for 5 reads of one object, want 1", n)
	}
}

func TestAFileChangedAfterAHitIsTamperedNotServedFromMemory(t *testing.T) {
	s, _ := countingStore(t)
	d, _ := s.PutObject([]byte("payload"))
	path, _ := s.objectPath(d)
	settled(t, path)
	if _, err := s.Object(d); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(){
		"different size": func() { _ = os.WriteFile(path, []byte("rewritten payload"), 0o600) },
		"same size":      func() { _ = os.WriteFile(path, []byte("PAYLOAD"), 0o600) },
	} {
		edit()
		if _, err := s.Object(d); !errors.Is(err, ErrTampered) {
			t.Fatalf("%s: Object = %v, want ErrTampered after the file changed", name, err)
		}
		_ = os.WriteFile(path, []byte("payload"), 0o600)
		settled(t, path)
		if _, err := s.Object(d); err != nil {
			t.Fatalf("%s: restoring the bytes should read clean: %v", name, err)
		}
	}
}

func TestVerifyReadsBytesEvenWhenSizeAndTimeAreRestored(t *testing.T) {
	s, _ := countingStore(t)
	appendN(t, s, "ws", 2)
	rec, err := s.Record(mustHead(t, s, "ws").Record)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := s.objectPath(rec.Payload)
	info := settled(t, path)
	if _, err := s.Object(rec.Payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte{'Z'}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify("ws"); !errors.Is(err, ErrTampered) {
		t.Fatalf("Verify = %v, want ErrTampered although size and mtime were restored", err)
	}
}

func TestARecentlyWrittenObjectIsNotTrustedByItsTimestamp(t *testing.T) {
	s, reads := countingStore(t)
	d, _ := s.PutObject([]byte("payload"))
	// The clock and the file's time are both pinned: a file system stamps from
	// its own clock, which can lag the process clock by more than the window.
	path, _ := s.objectPath(d)
	mod := time.Now().Truncate(time.Second).Add(123 * time.Millisecond)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return mod.Add(time.Millisecond) }
	for range 3 {
		if _, err := s.Object(d); err != nil {
			t.Fatal(err)
		}
	}
	if n := reads.Load(); n != 3 {
		t.Fatalf("read %d times, want every read to hit the file while its mtime is inside the racy window", n)
	}
}

func TestCallerMutationDoesNotReachTheCache(t *testing.T) {
	s, _ := countingStore(t)
	d, _ := s.PutObject([]byte("payload"))
	path, _ := s.objectPath(d)
	settled(t, path)
	first, _ := s.Object(d)
	first[0] = 'X'
	second, _ := s.Object(d)
	third, _ := s.Object(d)
	second[1] = 'Y'
	if string(third) != "payload" {
		t.Fatalf("a caller's edit changed what later reads see: %q", third)
	}
}

func TestACoarseTimestampKeepsTheLongRacyWindow(t *testing.T) {
	s, reads := countingStore(t)
	d, _ := s.PutObject([]byte("payload"))
	path, _ := s.objectPath(d)
	coarse := time.Now().Add(-time.Second).Truncate(time.Second)
	if err := os.Chtimes(path, coarse, coarse); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := s.Object(d); err != nil {
			t.Fatal(err)
		}
	}
	if n := reads.Load(); n != 3 {
		t.Fatalf("read %d times, want every read to reach the file while a whole-second mtime is this recent", n)
	}
}

func TestAnEditThatRestoresSizeAndModificationTimeIsStillSeenThroughTheChangeTime(t *testing.T) {
	s, _ := countingStore(t)
	d, _ := s.PutObject([]byte("payload"))
	path, _ := s.objectPath(d)
	info := settled(t, path)
	if !fileIdentity(info).known() {
		t.Skip("this platform exposes no file identity beyond size and time")
	}
	if _, err := s.Object(d); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("PAYLOAD"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Object(d); !errors.Is(err, ErrTampered) {
		t.Fatalf("Object = %v, want ErrTampered: the change time moved although size and mtime were restored", err)
	}
}

func TestAReplacedFileIsNotServedFromMemory(t *testing.T) {
	s, reads := countingStore(t)
	d, _ := s.PutObject([]byte("payload"))
	path, _ := s.objectPath(d)
	info := settled(t, path)
	if _, err := s.Object(d); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	before := reads.Load()
	if got, err := s.Object(d); err != nil || string(got) != "payload" {
		t.Fatalf("Object = %q, %v", got, err)
	}
	if fileIdentity(info).known() && reads.Load() == before {
		t.Fatal("a file replaced behind the cache was answered from memory")
	}
}

// Past its limits the cache empties and starts over; every read stays right.
func TestTheCacheStaysCorrectPastItsLimits(t *testing.T) {
	s, _ := countingStore(t)
	var digests []Digest
	for i := range maxCachedCount + 50 {
		d, err := s.PutIndexObject([]byte("object-" + string(rune('a'+i%26)) + string(rune('A'+i/26%26)) + string(rune('0'+i/676))))
		if err != nil {
			t.Fatal(err)
		}
		path, _ := s.objectPath(d)
		settled(t, path)
		digests = append(digests, d)
	}
	for round := range 2 {
		for _, d := range digests {
			got, err := s.Object(d)
			if err != nil || DigestOf(got) != d {
				t.Fatalf("round %d: Object(%s) = %q, %v", round, d, got, err)
			}
		}
	}
	if len(s.objects.byPath) > maxCachedCount {
		t.Fatalf("cache holds %d entries, limit %d", len(s.objects.byPath), maxCachedCount)
	}
	big := make([]byte, maxCachedObject+1)
	d, _ := s.PutObject(big)
	path, _ := s.objectPath(d)
	settled(t, path)
	for range 2 {
		if got, err := s.Object(d); err != nil || len(got) != len(big) {
			t.Fatalf("oversized object: %d bytes, %v", len(got), err)
		}
	}
}
