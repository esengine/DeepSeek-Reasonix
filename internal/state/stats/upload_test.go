package stats

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func uploadClock(day string) func() time.Time {
	at, _ := time.ParseInLocation(dayLayout, day, time.Local)
	return func() time.Time { return at.Add(10 * time.Hour) }
}

func TestUploadsFromSeveralProcessesAddUpPerProviderAndDay(t *testing.T) {
	dir := testenv.TempDir(t)
	a := newUploadMeter(dir, "a", uploadClock("2026-10-26"))
	b := newUploadMeter(dir, "b", uploadClock("2026-10-26"))
	c := newUploadMeter(dir, "c", uploadClock("2026-10-27"))
	a.Add("deepseek", 1000)
	a.Add("deepseek", 500)
	b.Add("deepseek", 2500)
	b.Add("relay", 1000)
	c.Add("deepseek", 5000)
	for _, m := range []*uploadMeter{a, b, c} {
		if err := m.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	from, _ := time.ParseInLocation(dayLayout, "2026-10-25", time.Local)
	to, _ := time.ParseInLocation(dayLayout, "2026-10-27", time.Local)
	got, err := QueryUpload(dir, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bytes != 10000 || got.Requests != 5 {
		t.Fatalf("total = %d bytes in %d requests, want 10000 in 5", got.Bytes, got.Requests)
	}
	if len(got.Daily) != 3 || got.Daily[0].Day != "2026-10-25" || got.Daily[0].Bytes != 0 || got.Daily[1].Bytes != 5000 || got.Daily[2].Bytes != 5000 {
		t.Fatalf("daily = %+v, want the quiet day kept as zero so the axis is stable", got.Daily)
	}
	if len(got.Providers) != 2 || got.Providers[0].Provider != "deepseek" || got.Providers[0].Bytes != 9000 || got.Providers[0].Requests != 4 {
		t.Fatalf("providers = %+v, want deepseek first with 9000 bytes in 4 requests", got.Providers)
	}
	if p := got.Providers[0].Percent; p < 89.9 || p > 90.1 {
		t.Fatalf("deepseek percent = %v, want 90", p)
	}
}

func TestUploadQueryLeavesOutDaysOutsideTheRange(t *testing.T) {
	dir := testenv.TempDir(t)
	m := newUploadMeter(dir, "a", uploadClock("2026-09-01"))
	m.Add("p", 700)
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}
	from, _ := time.ParseInLocation(dayLayout, "2026-10-01", time.Local)
	to, _ := time.ParseInLocation(dayLayout, "2026-10-27", time.Local)
	got, err := QueryUpload(dir, from, to)
	if err != nil || got.Bytes != 0 || len(got.Providers) != 0 {
		t.Fatalf("got %+v err=%v, want nothing from September in an October range", got, err)
	}
}

func TestAMeterThatNeverUploadedLeavesNoFile(t *testing.T) {
	dir := testenv.TempDir(t)
	m := newUploadMeter(dir, "a", uploadClock("2026-10-27"))
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, uploadDirName)); err == nil {
		entries, _ := os.ReadDir(filepath.Join(dir, uploadDirName))
		if len(entries) != 0 {
			t.Fatalf("an idle process wrote %d files", len(entries))
		}
	}
}

func TestOldUploadFilesAreRemovedWhenAMeterStarts(t *testing.T) {
	dir := testenv.TempDir(t)
	old := newUploadMeter(dir, "a", uploadClock("2024-01-01"))
	old.Add("p", 1)
	if err := old.Flush(); err != nil {
		t.Fatal(err)
	}
	fresh := newUploadMeter(dir, "b", uploadClock("2026-10-27"))
	fresh.pruneBefore(fresh.now().AddDate(0, 0, -uploadKeepDays))
	entries, _ := os.ReadDir(filepath.Join(dir, uploadDirName))
	if len(entries) != 0 {
		t.Fatalf("%d files left after pruning, want the 2024 one gone", len(entries))
	}
}

func TestTornOrForeignFilesInTheUploadDirectoryAreIgnored(t *testing.T) {
	dir := testenv.TempDir(t)
	m := newUploadMeter(dir, "a", uploadClock("2026-10-27"))
	m.Add("p", 300)
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}
	up := filepath.Join(dir, uploadDirName)
	_ = os.WriteFile(filepath.Join(up, "2026-10-27.torn.json"), []byte(`{"day":"2026-10-27","provi`), 0o600)
	_ = os.WriteFile(filepath.Join(up, "notes.txt"), []byte("x"), 0o600)
	day, _ := time.ParseInLocation(dayLayout, "2026-10-27", time.Local)
	got, err := QueryUpload(dir, day, day)
	if err != nil || got.Bytes != 300 {
		t.Fatalf("got %+v err=%v, want the intact file's 300 bytes and no failure", got, err)
	}
}
