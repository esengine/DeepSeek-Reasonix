package diskhealth

import (
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

const gib = int64(1) << 30

type disks map[string]Volume

func (d disks) read(dir string) (Volume, bool) {
	v, ok := d[dir]
	return v, ok
}

func TestLowIsAShortageOfAbsoluteOrRelativeSpace(t *testing.T) {
	for name, c := range map[string]struct {
		v    Volume
		want bool
	}{
		"plenty":                 {Volume{Free: 200 * gib, Total: 500 * gib}, false},
		"under five gigabytes":   {Volume{Free: 4 * gib, Total: 500 * gib}, true},
		"under three percent":    {Volume{Free: 20 * gib, Total: 1000 * gib}, true},
		"small disk, ample room": {Volume{Free: 30 * gib, Total: 64 * gib}, false},
		"unknown total":          {Volume{Free: 0, Total: 0}, false},
	} {
		if got := Low(c.v); got != c.want {
			t.Errorf("%s: Low(%+v) = %v, want %v", name, c.v, got, c.want)
		}
	}
}

func TestOneNoticeForALowVolumeAndNoneWhileItStaysQuiet(t *testing.T) {
	m := NewMonitor()
	d := disks{"/w": {Free: 2 * gib, Total: 500 * gib}}
	t0 := time.Unix(1_000_000, 0)
	got := m.Check(t0, []string{"/w"}, d.read)
	if len(got) != 1 || got[0].Dir != "/w" || got[0].Free != 2*gib {
		t.Fatalf("first check = %+v, want one notice for /w", got)
	}
	if again := m.Check(t0.Add(time.Hour), []string{"/w"}, d.read); len(again) != 0 {
		t.Fatalf("an hour later = %+v, want silence", again)
	}
	if later := m.Check(t0.Add(QuietFor+time.Minute), []string{"/w"}, d.read); len(later) != 1 {
		t.Fatalf("after the quiet period = %+v, want the reminder", later)
	}
}

func TestARecoveredVolumeWarnsAgainWhenItFillsAgain(t *testing.T) {
	m := NewMonitor()
	d := disks{"/w": {Free: 2 * gib, Total: 500 * gib}}
	t0 := time.Unix(1_000_000, 0)
	m.Check(t0, []string{"/w"}, d.read)
	d["/w"] = Volume{Free: 100 * gib, Total: 500 * gib}
	if got := m.Check(t0.Add(time.Minute), []string{"/w"}, d.read); len(got) != 0 {
		t.Fatalf("recovered volume = %+v, want no notice", got)
	}
	d["/w"] = Volume{Free: 1 * gib, Total: 500 * gib}
	if got := m.Check(t0.Add(2*time.Minute), []string{"/w"}, d.read); len(got) != 1 {
		t.Fatalf("filled again = %+v, want a fresh notice well inside the quiet period", got)
	}
}

func TestAVolumeHoveringAtTheLineDoesNotFlap(t *testing.T) {
	m := NewMonitor()
	d := disks{"/w": {Free: 4 * gib, Total: 500 * gib}}
	t0 := time.Unix(1_000_000, 0)
	m.Check(t0, []string{"/w"}, d.read)
	d["/w"] = Volume{Free: 6 * gib, Total: 500 * gib}
	m.Check(t0.Add(time.Minute), []string{"/w"}, d.read)
	d["/w"] = Volume{Free: 4 * gib, Total: 500 * gib}
	if got := m.Check(t0.Add(2*time.Minute), []string{"/w"}, d.read); len(got) != 0 {
		t.Fatalf("hovering = %+v, want silence until it clearly recovered", got)
	}
}

func TestTwoFoldersOnOneVolumeAreOneNotice(t *testing.T) {
	m := NewMonitor()
	same := Volume{Free: 2 * gib, Total: 500 * gib}
	d := disks{"/w": same, "/state": same}
	if got := m.Check(time.Unix(1_000_000, 0), []string{"/w", "/state"}, d.read); len(got) != 1 {
		t.Fatalf("two folders, one volume = %+v, want one notice", got)
	}
}

func TestAnUnreadableFolderIsLeftAlone(t *testing.T) {
	m := NewMonitor()
	if got := m.Check(time.Unix(1_000_000, 0), []string{"/gone"}, disks{}.read); len(got) != 0 {
		t.Fatalf("unreadable = %+v, want nothing said about a volume nobody could read", got)
	}
}

func TestReadAnswersForARealFolder(t *testing.T) {
	v, ok := Read(testenv.TempDir(t))
	if !ok || v.Total <= 0 || v.Free < 0 || v.Free > v.Total {
		t.Fatalf("Read(temp dir) = %+v, %v, want a volume with a total and a free count within it", v, ok)
	}
}
