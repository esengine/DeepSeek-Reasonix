package crashreport

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"
)

const (
	hostFatalPrefix  = "host-"
	keepFatalLogs    = 5
	maxFatalLogBytes = 256 << 10
)

// InstallFatalLog mirrors what the Go runtime prints when it kills the process
// into dir/host-<utc time>-<version>-<pid>.log. Unlike InstallFatalOutput the
// file is for people to read and for the shell to quote, so it survives the
// next launch until the retention bounds drop it. release removes the file of a
// run that ended cleanly.
func InstallFatalLog(dir, version string) (release func()) {
	release = func() {}
	if strings.TrimSpace(dir) == "" {
		return release
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return release
	}
	pruneFatalLogs(dir)
	name := fmt.Sprintf("%s%s-%s-%d%s", hostFatalPrefix, time.Now().UTC().Format("20060102T150405Z"), pathSafe(version), os.Getpid(), fatalSuffix)
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return release
	}
	installErr := debug.SetCrashOutput(f, debug.CrashOptions{})
	_ = f.Close()
	if installErr != nil {
		_ = os.Remove(path)
		return release
	}
	return func() {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		_ = os.Remove(path)
	}
}

// pruneFatalLogs drops empty files, keeps the newest keepFatalLogs-1 (room is left for
// the file about to be opened) and cuts each survivor to its last
// maxFatalLogBytes, which is where the cause of a crash is printed.
func pruneFatalLogs(dir string) {
	names, _ := filepath.Glob(filepath.Join(dir, hostFatalPrefix+"*"+fatalSuffix))
	type entry struct {
		path string
		at   time.Time
	}
	var kept []entry
	for _, path := range names {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() == 0 {
			_ = os.Remove(path)
			continue
		}
		kept = append(kept, entry{path, info.ModTime()})
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].at.After(kept[j].at) })
	for i, e := range kept {
		if i >= keepFatalLogs-1 {
			_ = os.Remove(e.path)
			continue
		}
		cutToTail(e.path)
	}
}

func cutToTail(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= maxFatalLogBytes {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	tail := make([]byte, maxFatalLogBytes)
	n, _ := f.ReadAt(tail, info.Size()-maxFatalLogBytes)
	_ = f.Close()
	if os.WriteFile(path, tail[:n], 0o600) == nil {
		_ = os.Chtimes(path, info.ModTime(), info.ModTime())
	}
}

func pathSafe(s string) string {
	out := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.':
			return r
		}
		return '-'
	}, strings.TrimSpace(s))
	if out == "" {
		return "unknown"
	}
	if len(out) > 32 {
		out = out[:32]
	}
	return out
}
