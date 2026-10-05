// Command workgroupassay measures how much of ordinary use has execution worth
// folding. It reads durable wire logs, keeps the turns the frozen protocol
// admits, and refuses to answer until the sample is whole.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/eventwire"
)

// turn is one authored turn: the frames it owns and where in time it sits.
type turn struct {
	session  string
	at       time.Time
	index    int
	authored int
	frames   []eventwire.Event
}

func main() {
	root := flag.String("root", "", "session root to scan (default: the configured one)")
	flag.Parse()
	dirs, scratch := scanRoots(*root)
	turns, skipped := collect(dirs)
	skipped.scratchWorkspaces = scratch
	sort.Slice(turns, func(i, j int) bool {
		if !turns[i].at.Equal(turns[j].at) {
			return turns[i].at.Before(turns[j].at)
		}
		return turns[i].index < turns[j].index
	})
	report(turns, skipped)
}

func scanRoots(root string) ([]string, int) {
	if strings.TrimSpace(root) != "" {
		return []string{root}, 0
	}
	projects, scratch := projectSessionDirs()
	seen := map[string]bool{}
	var out []string
	for _, d := range append([]string{config.SessionDir()}, projects...) {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out, scratch
}

// projectSessionDirs finds the per-project session directories a Studio window
// writes into. Workspaces under a temp root are left out: a run driven from a
// scratch directory is an experiment, and the protocol excludes those by
// construction rather than by hoping none happen after the freeze.
func projectSessionDirs() ([]string, int) {
	home := config.ReasonixHomeDir()
	if home == "" {
		return nil, 0
	}
	entries, err := os.ReadDir(filepath.Join(home, "projects"))
	if err != nil {
		return nil, 0
	}
	scratch := scratchSlugs()
	var out []string
	skipped := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if underScratch(e.Name(), scratch) {
			skipped++
			continue
		}
		out = append(out, filepath.Join(home, "projects", e.Name(), "sessions"))
	}
	return out, skipped
}

// scratchSlugs is what a session directory's name starts with when its
// workspace lives under a temp root. Built with the host's own slug function,
// so this reads the encoding the host wrote rather than guessing at names.
func scratchSlugs() []string {
	var out []string
	for _, root := range []string{os.TempDir(), "/tmp", "/private/tmp"} {
		for _, p := range []string{root, resolved(root)} {
			if abs, err := filepath.Abs(p); err == nil && abs != "" && abs != string(filepath.Separator) {
				if slug := config.WorkspaceSlug(abs); slug != "" {
					out = append(out, slug)
				}
			}
		}
	}
	return out
}

func resolved(path string) string {
	if p, err := filepath.EvalSymlinks(path); err == nil {
		return p
	}
	return path
}

func underScratch(name string, slugs []string) bool {
	for _, s := range slugs {
		if strings.HasPrefix(name, s) {
			return true
		}
	}
	return false
}

// skipReasons counts what the protocol turned away, so a small sample can say
// why rather than look like nothing happened.
type skipReasons struct {
	beforeFreeze      int
	truncatedLogs     int
	unnamedTurns      int
	scratchWorkspaces int
}

func collect(dirs []string) ([]turn, skipReasons) {
	var out []turn
	var skipped skipReasons
	frozen := frozenTime()
	for _, dir := range dirs {
		logs, _ := filepath.Glob(filepath.Join(dir, "*.wire.jsonl"))
		for _, path := range logs {
			at, ok := sessionStart(path)
			if !ok || at.Before(frozen) {
				skipped.beforeFreeze++
				continue
			}
			if truncated(path) {
				skipped.truncatedLogs++
				continue
			}
			turns, unnamed := turnsIn(path, at)
			skipped.unnamedTurns += unnamed
			out = append(out, turns...)
		}
	}
	return out, skipped
}

// sessionStart reads when a session began from its file name, which is the only
// clock the durable log carries for frames that never ran a tool. The stamp is
// UTC (sessionstore.NewSessionPath writes it that way); reading it as local time moves
// every session by the machine's offset and lets one from before the freeze
// pass the filter.
func sessionStart(path string) (time.Time, bool) {
	name := filepath.Base(path)
	if i := strings.Index(name, "-"); i > 0 && len(name) > i+7 {
		if t, err := time.ParseInLocation("20060102-150405", name[:i+7], time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// truncated reads the log's own record of what it does not contain. A prefix
// cannot answer a question about a whole turn.
func truncated(logPath string) bool {
	meta := strings.TrimSuffix(logPath, ".wire.jsonl") + ".wire.meta.json"
	body, err := os.ReadFile(meta)
	if err != nil {
		return false
	}
	var m struct {
		Truncated bool `json:"truncated"`
	}
	return json.Unmarshal(body, &m) == nil && m.Truncated
}

// turnsIn splits a log into authored turns. A turn_started with no authoredTurn
// is a synthetic continuation of one the host already named, never a turn of
// its own; it is counted as skipped and its frames stay with nothing.
func turnsIn(path string, at time.Time) ([]turn, int) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, 0
	}
	var out []turn
	var cur *turn
	unnamed := 0
	idx := 0
	for line := range strings.SplitSeq(strings.TrimRight(string(body), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e eventwire.Event
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e.Kind == "turn_started" {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = nil
			if e.AuthoredTurn == nil {
				unnamed++
				continue
			}
			idx++
			cur = &turn{session: filepath.Base(path), at: at, index: idx, authored: *e.AuthoredTurn}
		}
		if cur != nil {
			cur.frames = append(cur.frames, e)
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out, unnamed
}

func report(turns []turn, skipped skipReasons) {
	fmt.Printf("P2-2e natural-value assay\n")
	fmt.Printf("  protocol frozen at %s; sample is the first %d eligible authored turns after it\n",
		frozenAt, sampleSize)
	fmt.Printf("  eligible so far: %d\n", len(turns))
	fmt.Printf("  turned away: %d session(s) before the freeze, %d truncated, %d unnamed turns,\n",
		skipped.beforeFreeze, skipped.truncatedLogs, skipped.unnamedTurns)
	fmt.Printf("               %d workspace(s) under a temp root — experiments, not ordinary use\n",
		skipped.scratchWorkspaces)
	if len(turns) < sampleSize {
		fmt.Printf("\nNo result yet. %d more eligible authored turn(s) are owed.\n", sampleSize-len(turns))
		fmt.Printf("Reading a partial sample is the sequential decision this protocol exists to prevent,\n")
		fmt.Printf("so nothing further is printed until the sample is whole.\n")
		return
	}
	measure(turns[:sampleSize])
}
