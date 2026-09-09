package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/store"
)

// sessionDAGTailRepairMinAge keeps tail repair from truncating a line another
// writer is still appending: a torn tail is only cut once the log has been
// quiet for this long.
const sessionDAGTailRepairMinAge = 2 * time.Second

type sessionDAGHeader struct {
	generation   int64
	upgradedFrom int
}

// encodeSessionDAGEntries serializes entries one per line, filling in the
// schema version and the default writer/timestamp.
func encodeSessionDAGEntries(entries []sessionDAGEntry, now time.Time) ([]byte, error) {
	var buf bytes.Buffer
	for i := range entries {
		e := &entries[i]
		e.SchemaVersion = sessionDAGSchemaVersion
		if e.At.IsZero() {
			e.At = now
		}
		if e.Writer == "" {
			e.Writer = SessionWriterID()
		}
		b, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("encode session entry: %w", err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// appendSessionDAGEntries writes entries as one contiguous batch and returns
// the log size afterwards. Callers hold the session file lock: JSON lines
// exceed PIPE_BUF, so the flock, not O_APPEND, is what keeps two writers'
// batches from interleaving.
func appendSessionDAGEntries(sessionPath string, entries []sessionDAGEntry, sync bool) (int64, error) {
	path := store.SessionEventLog(sessionPath)
	if path == "" {
		return 0, fmt.Errorf("empty session event log path")
	}
	if len(entries) == 0 {
		info, err := os.Stat(path)
		if err != nil {
			return 0, err
		}
		return info.Size(), nil
	}
	fileutil.Crash("dag-append", path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	data, err := encodeSessionDAGEntries(entries, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, fmt.Errorf("open session event log: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return 0, fmt.Errorf("protect session event log: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return 0, fmt.Errorf("append session entries: %w", err)
	}
	if sync {
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return 0, err
		}
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return 0, err
	}
	return info.Size(), f.Close()
}

// readSessionDAGHeader decodes the leading log entry without reading the rest
// of the file, so a writer can notice a rotation (new generation) cheaply.
func readSessionDAGHeader(sessionPath string) (sessionDAGHeader, bool, error) {
	path := store.SessionEventLog(sessionPath)
	if path == "" {
		return sessionDAGHeader{}, false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return sessionDAGHeader{}, false, nil
		}
		return sessionDAGHeader{}, false, err
	}
	defer f.Close()
	var e sessionDAGEntry
	if err := json.NewDecoder(io.LimitReader(f, sessionEventProbeMaxBytes)).Decode(&e); err != nil {
		return sessionDAGHeader{}, false, nil
	}
	if e.SchemaVersion != sessionDAGSchemaVersion || e.Type != sessionDAGTypeLog {
		return sessionDAGHeader{}, false, nil
	}
	return sessionDAGHeader{generation: e.Generation, upgradedFrom: e.UpgradedFrom}, true, nil
}

// repairSessionDAGTail truncates a torn tail found by replay once the log has
// been quiet long enough that no writer can still be finishing that line. The
// discarded bytes go to the .damaged sidecar first. Callers hold the file lock.
func repairSessionDAGTail(sessionPath string, st *sessionDAGState, now time.Time) (bool, error) {
	if st == nil || !st.damaged || st.lastGoodEnd >= st.size {
		return false, nil
	}
	path := store.SessionEventLog(sessionPath)
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if info.Size() != st.size || now.Sub(info.ModTime()) < sessionDAGTailRepairMinAge {
		return false, nil
	}
	if preserveErr := preserveDamagedEventLogTail(sessionPath, path, st.lastGoodEnd, st.size); preserveErr != nil {
		slog.Warn("session: could not preserve damaged log tail; truncating anyway",
			"path", path, "from", st.lastGoodEnd, "size", st.size, "err", preserveErr)
	}
	if err := os.Truncate(path, st.lastGoodEnd); err != nil {
		return false, err
	}
	if st.lastGoodEnd > 0 {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return false, err
		}
		if _, err := f.Write([]byte{'\n'}); err != nil {
			_ = f.Close()
			return false, err
		}
		if err := f.Close(); err != nil {
			return false, err
		}
	}
	st.size = st.lastGoodEnd
	if st.lastGoodEnd > 0 {
		st.size++
	}
	st.damaged = false
	return true, nil
}
