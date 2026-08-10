package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// serveState is one /api/state response: the per-task live digest the
// dashboard polls while a bench run is still writing trajectories.
type serveState struct {
	Dir   string      `json:"dir"`
	Now   int64       `json:"now"`
	Suite []string    `json:"suite,omitempty"`
	Tasks []serveTask `json:"tasks"`
}

type serveTask struct {
	ID          string          `json:"id"`
	Records     int             `json:"records"`
	SpanMs      int64           `json:"span_ms"`
	ModelRounds int             `json:"model_rounds"`
	ToolMs      int64           `json:"tool_ms"`
	AgoMs       int64           `json:"ago_ms"`
	NoProgress  int             `json:"no_progress"`
	Outcome     *outcomeSummary `json:"outcome,omitempty"`
	Rounds      []serveRound    `json:"rounds"`
}

type serveRound struct {
	TS           int64 `json:"t,omitempty"`
	Exploration  int   `json:"e,omitempty"`
	Verification int   `json:"v,omitempty"`
	Objective    int   `json:"o,omitempty"`
	Regression   int   `json:"r,omitempty"`
	Churn        int   `json:"c,omitempty"`
	Legacy       int   `json:"g,omitempty"`
}

func runServeMode(dir, suite, addr string) error {
	if dir == "" {
		return fmt.Errorf("serve mode needs -trajectories <dir>")
	}
	var suiteIDs []string
	if tasks, err := loadTasks(suite); err == nil {
		for _, t := range tasks {
			suiteIDs = append(suiteIDs, t.ID)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, _ *http.Request) {
		state, err := collectServeState(dir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		state.Suite = suiteIDs
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, serveHTML)
	})
	fmt.Printf("e2ebench live dashboard: http://%s (watching %s)\n", addr, dir)
	return http.ListenAndServe(addr, mux)
}

// collectServeState re-summarizes every trajectory on each poll. Files are
// small and flushed per record, so live reads see every completed line.
func collectServeState(dir string) (*serveState, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.trajectory.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	state := &serveState{Dir: dir, Now: time.Now().UnixMilli(), Tasks: []serveTask{}}
	for _, path := range paths {
		scan, err := scanTrajectoryFile(path)
		if err != nil {
			continue
		}
		s := scan.finish()
		t := serveTask{
			ID:          strings.TrimSuffix(filepath.Base(path), ".trajectory.jsonl"),
			Records:     s.Records,
			SpanMs:      s.SpanMs,
			ModelRounds: s.ModelRounds,
			ToolMs:      s.toolWall(),
			AgoMs:       -1,
			NoProgress:  s.NoProgressSignals,
			Outcome:     s.Outcome,
			Rounds:      make([]serveRound, 0, len(scan.outcomePoints)),
		}
		if fi, err := os.Stat(path); err == nil {
			t.AgoMs = time.Since(fi.ModTime()).Milliseconds()
		}
		for _, p := range scan.outcomePoints {
			t.Rounds = append(t.Rounds, serveRound{
				TS:          p.ts,
				Exploration: p.exploration, Verification: p.verification,
				Objective: p.objective, Regression: p.regression,
				Churn: p.churn, Legacy: p.legacyGain,
			})
		}
		state.Tasks = append(state.Tasks, t)
	}
	return state, nil
}
