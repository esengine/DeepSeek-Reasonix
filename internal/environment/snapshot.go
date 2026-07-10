package environment

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
)

// Probe snapshots persist across process restarts so the environment section —
// which sits inside the provider-cached system-prompt prefix — stays
// byte-stable between rebuilds and relaunches. Live probes are point-in-time
// observations: a 2s timeout flips a slow tool between "found" and "timeout",
// a GUI-launched desktop resolves a different PATH than a login shell, a cold
// docker daemon reports an exit error. Re-observing on every rebuild rewrote
// the prefix and invalidated every session's provider cache (10x miss pricing)
// for no user-visible reason. Persisting one snapshot per probe fingerprint
// under the shared cache root keeps rebuilds — and the CLI and desktop on the
// same machine — on identical bytes until the snapshot ages out.
const probeSnapshotTTL = 24 * time.Hour

const probeSnapshotVersion = 1

type probeSnapshot struct {
	Version     int           `json:"version"`
	Fingerprint string        `json:"fingerprint"`
	StoredAt    time.Time     `json:"stored_at"`
	Results     []ProbeResult `json:"results"`
}

func probeSnapshotPath(dir, fingerprint string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fingerprint))
	return filepath.Join(dir, "environment", fmt.Sprintf("probes-%x.json", sum[:8]))
}

// loadProbeSnapshot returns the persisted snapshot for fingerprint, however
// stale. Callers decide whether it is fresh enough to serve directly or only
// usable as the flap-merge reference.
func loadProbeSnapshot(dir, fingerprint string) (probeSnapshot, bool) {
	path := probeSnapshotPath(dir, fingerprint)
	if path == "" {
		return probeSnapshot{}, false
	}
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return probeSnapshot{}, false
	}
	var snap probeSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return probeSnapshot{}, false
	}
	if snap.Version != probeSnapshotVersion || snap.Fingerprint != fingerprint {
		return probeSnapshot{}, false
	}
	return snap, true
}

func saveProbeSnapshot(dir, fingerprint string, results []ProbeResult, now time.Time) {
	path := probeSnapshotPath(dir, fingerprint)
	if path == "" {
		return
	}
	b, err := json.Marshal(probeSnapshot{
		Version:     probeSnapshotVersion,
		Fingerprint: fingerprint,
		StoredAt:    now,
		Results:     results,
	})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".probes.*.tmp")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return
	}
	if err := fileutil.ReplaceFile(tmpPath, path); err != nil {
		os.Remove(tmpPath)
	}
}

// transientProbeFailure reports whether a probe result describes a failure
// that says nothing definitive about whether the tool exists: a timeout (slow
// first run, cold daemon) or a nonzero exit (daemon down, transient state).
// "not found" and "not trusted" are definitive — the binary is gone from PATH
// or rejected by policy — and must be adopted, not papered over.
func transientProbeFailure(r ProbeResult) bool {
	if r.Found {
		return false
	}
	return r.Error == "timeout" || strings.HasPrefix(r.Error, "exit ")
}

// mergeProbeSnapshot overlays fresh results onto the previous snapshot's:
// a fresh transient failure keeps the previous successful observation for the
// same probe, so a slow or flaky tool cannot flip the rendered environment
// section — and with it the whole cached prompt prefix — between rebuilds.
// Definitive results (found, not found, not trusted) always win.
func mergeProbeSnapshot(previous, fresh []ProbeResult) []ProbeResult {
	if len(previous) == 0 {
		return fresh
	}
	prevByCommand := make(map[string]ProbeResult, len(previous))
	for _, r := range previous {
		if r.Found {
			prevByCommand[r.Command] = r
		}
	}
	merged := append([]ProbeResult(nil), fresh...)
	for i, r := range merged {
		if !transientProbeFailure(r) {
			continue
		}
		if prev, ok := prevByCommand[r.Command]; ok {
			merged[i] = prev
		}
	}
	sortResults(merged)
	return merged
}
