package agent

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"reasonix/internal/store"
)

// sessionEventLogProbe classifies whatever sits at the session's event-log
// path. Legacy imports can leave a foreign ".events.jsonl" (e.g. the v0.x
// Claude-style event transcript) at exactly the native log path; writing into
// or over it would corrupt the user's original file, so foreign logs are
// read-ignored and never touched.
type sessionEventLogProbe struct {
	size          int64
	native        bool // missing/empty, or first record is a supported schema-1 event
	dag           bool // first record is a schema-2 DAG entry
	futureSchema  bool // first record declares a newer schema than this build
	schemaVersion int
}

// sessionEventSidecarsFit reports whether the event log and index filenames
// stay within the filesystem's name limit. Overlong transcript names (from the
// pre-bounded recovery cascade, until reconcileOverlongSessionFilenames renames
// them) must run checkpoint-only: creating their sidecars would fail with
// ENAMETOOLONG mid-save.
func sessionEventSidecarsFit(sessionPath string) bool {
	logName := filepath.Base(store.SessionEventLog(sessionPath))
	indexName := filepath.Base(store.SessionEventIndex(sessionPath))
	return len(logName) <= nameMaxBytes && len(indexName) <= nameMaxBytes
}

// probeSessionEventLog inspects the first record of the event log to decide
// whether the native persistence layer owns the file. Missing or empty logs
// count as native (we may create/append); an undecodable or foreign first
// record — or a transcript name too long for the sidecars to fit — marks the
// file as not ours.
func probeSessionEventLog(sessionPath string) (sessionEventLogProbe, error) {
	return probeSessionEventLogWithLimits(sessionPath, defaultSessionReplayLimits)
}

func probeSessionEventLogWithLimits(sessionPath string, limits sessionReplayLimits) (sessionEventLogProbe, error) {
	path := store.SessionEventLog(sessionPath)
	if path == "" {
		return sessionEventLogProbe{native: true}, nil
	}
	if !sessionEventSidecarsFit(sessionPath) {
		return sessionEventLogProbe{}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return sessionEventLogProbe{native: true}, nil
		}
		return sessionEventLogProbe{}, err
	}
	if info.IsDir() {
		return sessionEventLogProbe{}, nil
	}
	if info.Size() == 0 {
		return sessionEventLogProbe{native: true}, nil
	}
	probe := sessionEventLogProbe{size: info.Size()}
	f, err := os.Open(path)
	if err != nil {
		return sessionEventLogProbe{}, err
	}
	defer f.Close()
	var schemaVersion int
	var eventType string
	var ok bool
	schemaVersion, eventType, ok = probeSessionEventHeader(f)
	if !ok && info.Size() <= limits.maxBytes {
		// Native writers put both identifying fields in the bounded prefix; for
		// other in-budget JSON a minimal struct decode keeps field order a
		// compatibility property rather than a format requirement.
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return sessionEventLogProbe{}, err
		}
		var header struct {
			SchemaVersion int    `json:"schema_version"`
			Type          string `json:"type"`
		}
		dec := json.NewDecoder(&io.LimitedReader{R: f, N: limits.maxBytes + 1})
		if err := dec.Decode(&header); err == nil {
			schemaVersion, eventType, ok = header.SchemaVersion, header.Type, true
		}
	}
	if !ok {
		// Nothing decodable at the head: not a native log this build can own.
		return probe, nil
	}
	probe.schemaVersion = schemaVersion
	switch {
	case schemaVersion == sessionEventSchemaVersion &&
		(eventType == sessionEventTypeReplace || eventType == sessionEventTypeAppend):
		probe.native = true
	case schemaVersion == sessionDAGSchemaVersion:
		probe.dag = true
	case schemaVersion > sessionDAGSchemaVersion:
		// A newer writer owns this log; ignoring or truncating it would
		// silently discard that writer's transcript.
		probe.futureSchema = true
	}
	return probe, nil
}

// probeSessionEventHeader searches a bounded prefix for the identifying fields.
// Using Decode on a partial struct still buffers the whole JSON value, so native
// writer output must take this fast path before replay's byte budget is checked.
func probeSessionEventHeader(r io.Reader) (schemaVersion int, eventType string, ok bool) {
	dec := json.NewDecoder(io.LimitReader(r, sessionEventProbeMaxBytes))
	tok, err := dec.Token()
	if err != nil {
		return 0, "", false
	}
	if delim, isDelim := tok.(json.Delim); !isDelim || delim != '{' {
		return 0, "", false
	}
	var haveSchema, haveType bool
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return 0, "", false
		}
		name, isString := key.(string)
		if !isString {
			return 0, "", false
		}
		switch name {
		case "schema_version":
			if err := dec.Decode(&schemaVersion); err != nil {
				return 0, "", false
			}
			haveSchema = true
		case "type":
			if err := dec.Decode(&eventType); err != nil {
				return 0, "", false
			}
			haveType = true
		default:
			var discard json.RawMessage
			if err := dec.Decode(&discard); err != nil {
				return 0, "", false
			}
		}
		if haveSchema && haveType {
			return schemaVersion, eventType, true
		}
	}
	return 0, "", false
}
