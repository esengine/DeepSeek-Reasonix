package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LastKnownGoodConfigPath is the fixed path of the most recent verified user
// config snapshot. Written by repair.RecordHealthyConfig after a successful
// desktop boot; used only as an in-memory recovery source when the live file
// cannot be parsed. The original user config is never overwritten by a load.
func LastKnownGoodConfigPath() string { return processRoots().lastKnownGoodConfigPath() }

func (r Roots) lastKnownGoodConfigPath() string {
	root := r.MemoryUserDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "repair", "config.toml.last-known-good")
}

// loadLastKnownGoodUserConfig merges a validated LKG snapshot into cfg.
// Returns an error when no usable snapshot exists.
func (r Roots) loadLastKnownGoodUserConfig(cfg *Config) error {
	path := r.lastKnownGoodConfigPath()
	if path == "" {
		return fmt.Errorf("last-known-good path unavailable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := ValidateBytes(data); err != nil {
		return err
	}
	if _, err := decodeTOMLBytes(data, cfg); err != nil {
		return fmt.Errorf("last-known-good: %w", err)
	}
	return nil
}

// lkgClock is the seam the age tests drive; the snapshot's own mtime is the
// record time because every write of it is atomic.
var lkgClock = time.Now

// lastKnownGoodProvenance describes the snapshot a recovery is about to trust.
// A snapshot is only as good as the config it was taken from, so a recovery
// that silently substitutes a months-old file is the failure this reports.
// Empty means no usable snapshot, and callers say so instead of dating one.
func lastKnownGoodProvenance() string {
	path := LastKnownGoodConfigPath()
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	recorded := info.ModTime()
	age := max(lkgClock().Sub(recorded), 0)
	return fmt.Sprintf("recorded %s, %s ago", recorded.Format(time.DateOnly), humanizeAge(age))
}

// humanizeAge renders a coarse age; the decision this informs is "is this
// snapshot close enough to my config to trust", which days answer and
// sub-second precision does not.
func humanizeAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "moments"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	default:
		return plural(int(d.Hours()/24), "day")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// describeLastKnownGood names the substitute a recovery is about to trust,
// dated. A stale snapshot silently reverts every setting changed since it was
// taken, so the age belongs in the warning rather than only on disk.
func describeLastKnownGood() string {
	if prov := lastKnownGoodProvenance(); prov != "" {
		return "last-known-good snapshot (" + prov + ")"
	}
	return "last-known-good snapshot"
}
