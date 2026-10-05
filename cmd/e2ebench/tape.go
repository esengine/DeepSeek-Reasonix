package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// A tape is every provider exchange of one run, in request order, recorded at
// the meter. Replayed, it answers the run from disk instead of the provider,
// and the requests the harness sends are held against the recorded ones: the
// first request that differs is where a kernel change altered what the model
// is asked, which is how a prefix meant to stay byte-stable is caught moving.
type tapeMode int

const (
	tapeOff tapeMode = iota
	tapeRecord
	tapeReplay
)

type tapeConfig struct {
	mode tapeMode
	root string
}

func registerTapeFlags() (record, replay *string) {
	record = flag.String("tape-record", "", "suite mode: record every provider exchange under this directory, one tape per task and trial. Requires -meter")
	replay = flag.String("tape-replay", "", "suite mode: answer each run from tapes under this directory instead of the provider, and report the first request that differs from the recorded one. Requires -meter")
	return record, replay
}

func tapeSettings(record, replay, meterConfig string) tapeConfig {
	record, replay = strings.TrimSpace(record), strings.TrimSpace(replay)
	fail := func(msg string) tapeConfig {
		fmt.Fprintln(os.Stderr, "tape:", msg)
		os.Exit(2)
		return tapeConfig{}
	}
	switch {
	case record == "" && replay == "":
		return tapeConfig{}
	case record != "" && replay != "":
		return fail("-tape-record and -tape-replay cannot be combined")
	case strings.TrimSpace(meterConfig) == "":
		return fail("tapes are made at the meter; pass -meter")
	case record != "":
		return tapeConfig{mode: tapeRecord, root: record}
	default:
		return tapeConfig{mode: tapeReplay, root: replay}
	}
}

// taskWorkdir is where a run's copy of the task lives. The workspace path is
// part of what the model is asked, so a taped run needs the same path on every
// recording and replay of that task and trial; any other run gets a fresh one.
func taskWorkdir(cfg suiteConfig, taskID string) (string, error) {
	if cfg.tape.mode == tapeOff {
		return os.MkdirTemp("", "e2ebench-"+taskID+"-")
	}
	dir := filepath.Join(os.TempDir(), "e2ebench-tape", fmt.Sprintf("%s-trial-%d", taskID, max(cfg.trial, 1)))
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	return dir, os.MkdirAll(dir, 0o755)
}

// tapeEpoch is the time every seeded file carries in a taped run. A listing the
// agent takes shows modification times, and a copy made a minute later would
// otherwise answer the same command differently on replay.
var tapeEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

func pinTapeTimes(cfg suiteConfig, work string) {
	if cfg.tape.mode == tapeOff {
		return
	}
	_ = filepath.WalkDir(work, func(path string, _ os.DirEntry, err error) error {
		if err == nil {
			_ = os.Chtimes(path, tapeEpoch, tapeEpoch)
		}
		return nil
	})
	// ".." in a listing is the tape root, whose time moves with every run made
	// under it.
	_ = os.Chtimes(filepath.Dir(work), tapeEpoch, tapeEpoch)
}

// tape is one run's recording directory.
type tape struct {
	mode tapeMode
	dir  string
}

func (c tapeConfig) forRun(taskID string, trial int) (*tape, error) {
	if c.mode == tapeOff {
		return nil, nil
	}
	dir := filepath.Join(c.root, taskID, fmt.Sprintf("trial-%d", max(trial, 1)))
	if c.mode == tapeRecord {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("no tape for %s: %w", taskID, err)
	}
	return &tape{mode: c.mode, dir: dir}, nil
}

type tapeResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
}

func (t *tape) file(index int, suffix string) string {
	return filepath.Join(t.dir, fmt.Sprintf("%04d.%s", index, suffix))
}

func (t *tape) save(index int, request []byte, status int, contentType string, body []byte) {
	meta, _ := json.Marshal(tapeResponse{Status: status, ContentType: contentType})
	_ = os.WriteFile(t.file(index, "request.json"), request, 0o644)
	_ = os.WriteFile(t.file(index, "response.json"), meta, 0o644)
	_ = os.WriteFile(t.file(index, "response.body"), body, 0o644)
}

// load returns the recorded answer to request index, and where the request
// the harness sent now first differs from the recorded one ("" when equal).
func (t *tape) load(index int, request []byte) (resp tapeResponse, body []byte, diff string, err error) {
	meta, err := os.ReadFile(t.file(index, "response.json"))
	if err != nil {
		return resp, nil, "", err
	}
	if err = json.Unmarshal(meta, &resp); err != nil {
		return resp, nil, "", err
	}
	if body, err = os.ReadFile(t.file(index, "response.body")); err != nil {
		return resp, nil, "", err
	}
	recorded, _ := os.ReadFile(t.file(index, "request.json"))
	return resp, body, requestDiff(recorded, request), nil
}

// replay answers request index from the tape. The recorded body goes through
// the same readers a live one does, so the report's usage matches the run
// that made the tape.
func (m *meter) replay(w http.ResponseWriter, index int, body []byte) {
	resp, data, diff, err := m.tape.load(index, body)
	m.mu.Lock()
	m.Replayed++
	if err != nil {
		m.TapeMissing++
		m.mu.Unlock()
		http.Error(w, fmt.Sprintf("e2ebench tape has no response for request %d", index), http.StatusBadGateway)
		return
	}
	if diff != "" && m.DivergedAt == 0 {
		m.DivergedAt, m.Divergence = index, diff
		// Kept beside the recorded one, so the difference can be read.
		_ = os.WriteFile(m.tape.file(index, "replayed.json"), body, 0o644)
	}
	m.mu.Unlock()
	if resp.ContentType != "" {
		w.Header().Set("Content-Type", resp.ContentType)
	}
	w.WriteHeader(resp.Status)
	if strings.Contains(resp.ContentType, "event-stream") {
		m.pipeStream(w, bytes.NewReader(data))
		return
	}
	m.pipeJSON(w, bytes.NewReader(data))
}

// requestDiff names the first place two chat requests differ: a message by
// index and role, the tool list, or another field. Messages come first because
// they are where a prefix change shows.
func requestDiff(recorded, got []byte) string {
	if bytes.Equal(recorded, got) {
		return ""
	}
	var a, b map[string]json.RawMessage
	if json.Unmarshal(recorded, &a) != nil || json.Unmarshal(got, &b) != nil {
		return "request body"
	}
	var ma, mb []json.RawMessage
	_ = json.Unmarshal(a["messages"], &ma)
	_ = json.Unmarshal(b["messages"], &mb)
	for i := range min(len(ma), len(mb)) {
		if !bytes.Equal(ma[i], mb[i]) {
			var m struct {
				Role string `json:"role"`
			}
			_ = json.Unmarshal(mb[i], &m)
			return fmt.Sprintf("message %d (%s)", i+1, m.Role)
		}
	}
	if len(ma) != len(mb) {
		return fmt.Sprintf("message count %d -> %d", len(ma), len(mb))
	}
	if !bytes.Equal(a["tools"], b["tools"]) {
		return "tools"
	}
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range slices.Compact(keys) {
		if !bytes.Equal(a[k], b[k]) {
			return "field " + k
		}
	}
	return "request body"
}

// renderTapeReplay reports what replay found: how many runs matched their
// tape request for request, and where each other one first departed.
func renderTapeReplay(results []result) string {
	replayed, matched := 0, 0
	var diverged []string
	for _, r := range results {
		if r.Skipped || r.Meter == nil || r.Meter.Replayed == 0 {
			continue
		}
		replayed++
		switch {
		case r.Meter.DivergedAt > 0:
			diverged = append(diverged, fmt.Sprintf("`%s` at request %d, %s", r.ID, r.Meter.DivergedAt, r.Meter.Divergence))
		case r.Meter.TapeMissing > 0:
			diverged = append(diverged, fmt.Sprintf("`%s` made %d requests past the end of its tape", r.ID, r.Meter.TapeMissing))
		default:
			matched++
		}
	}
	if replayed == 0 {
		return ""
	}
	line := fmt.Sprintf("**Replay** (%d runs): %d matched their tape request for request", replayed, matched)
	if len(diverged) > 0 {
		line += " · diverged: " + strings.Join(diverged, " · ")
	}
	return line + "\n\n"
}
