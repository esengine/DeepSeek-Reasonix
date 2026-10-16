package sandbox

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ShellDiscovery is one shell resolution and how it may spend time. Zero values
// are the production behaviour; Probe, Budget and Hedge exist so a caller can
// drive discovery through a slow or hung interpreter on any OS.
type ShellDiscovery struct {
	Prefer, Path string
	// Args are launcher arguments for Path (wsl.exe -d <distro> -- /bin/bash).
	// With them the pinned program is proven by running it with them, and an
	// unusable pair falls back to auto-detection.
	Args []string
	Warn io.Writer
	// ProofDir, when set, keeps launch proofs between runs. It must sit in the
	// user's own data tree; a proof never changes a sandbox decision.
	ProofDir string
	// Probe replaces the bash launch probe. The in-process memo is private to this call; proofs still persist to ProofDir.
	Probe  func(string) bool
	Budget time.Duration
	// LookPath replaces the PATH lookup, so a caller can drive a lookup that never returns.
	LookPath func(string) (string, error)
	Hedge    time.Duration
}

// Resolve is ResolveShell with the discovery settings above.
func (d ShellDiscovery) Resolve() Shell {
	if len(d.Args) > 0 {
		return d.resolveLauncher()
	}
	memo := &provenBash
	if d.Probe != nil {
		memo = new(sync.Map)
	}
	store := newShellProofStore(d.ProofDir)
	search := &bashSearch{budget: d.Budget, hedge: d.Hedge}
	build := func() shellHost {
		h := currentHost()
		if d.LookPath != nil {
			h.lookPath = d.LookPath
		}
		h.probe = func(path string) bool {
			run := d.Probe
			if run == nil {
				if runtime.GOOS != "windows" {
					return true
				}
				run = runBashProbe
			}
			return proveBash(path, run, store, memo)
		}
		h.search = search
		return h
	}
	return resolveWithin(search, build, memo, d.Prefer, d.Path, d.Warn)
}

// resolveWithin bounds the whole resolution, host construction included: a PATH
// entry on a dead share or a registry read can stall a lookup that no probe
// budget covers. Past the bash and PowerShell budgets together the session gets
// a bash already proven in this process, else the bare name as when nothing is
// found; the abandoned lookup finishes into the proof store.
func resolveWithin(search *bashSearch, build func() shellHost, memo *sync.Map, prefer, path string, warn io.Writer) Shell {
	budget, hedge := search.timing()
	limit := 2*budget + hedge
	var mu sync.Mutex
	var log strings.Builder
	got := make(chan Shell, 1)
	go func() {
		got <- resolveOn(build(), prefer, path, lockedWriter{&mu, &log})
	}()
	var sh Shell
	select {
	case sh = <-got:
	case <-time.After(limit):
		w := lockedWriter{&mu, &log}
		if proven, ok := provenBashPath(memo); ok {
			sh = Shell{Kind: ShellBash, Path: proven}
			fmt.Fprintf(w, "warning: [tools.shell] interpreter discovery did not finish within %s; using %q, which was proven earlier in this run\n", limit, proven)
		} else {
			sh = Shell{Kind: ShellBash, Path: "bash"}
			fmt.Fprintf(w, "warning: [tools.shell] interpreter discovery did not finish within %s; falling back to %q, which may fail to start. Restarting Reasonix usually recovers\n", limit, "bash")
		}
	}
	if warn != nil {
		mu.Lock()
		_, _ = io.WriteString(warn, log.String())
		mu.Unlock()
	}
	return sh
}

// provenBashPath is the proven bash that sorts first, so the same memo always
// names the same one. Only an entry whose value is struct{}{} is a proof; any
// other record in the table (a remembered hang, say) is not.
func provenBashPath(memo *sync.Map) (string, bool) {
	best := ""
	memo.Range(func(k, v any) bool {
		if _, proven := v.(struct{}); !proven {
			return true
		}
		if id, ok := k.(bashProbeIdentity); ok && (best == "" || id.path < best) {
			best = id.path
		}
		return true
	})
	return best, best != ""
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *strings.Builder
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

type bashSearch struct{ budget, hedge time.Duration }

const (
	// bashDiscoveryBudget caps how long discovery waits for every bash candidate
	// together; the unanswered ones are reported as probe_timeout.
	bashDiscoveryBudget = 12 * time.Second
	bashHedgeDelay      = 1500 * time.Millisecond
)

func (s *bashSearch) timing() (budget, hedge time.Duration) {
	budget, hedge = bashDiscoveryBudget, bashHedgeDelay
	if s != nil && s.budget > 0 {
		budget = s.budget
	}
	if s != nil && s.hedge > 0 {
		hedge = s.hedge
	}
	return budget, hedge
}

// bashCandidates lists the bash executables worth proving, best first.
func (h shellHost) bashCandidates() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if key := strings.ToLower(filepath.Clean(p)); !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	if p, ok := h.lookShell("bash"); ok && !h.isWSL(p) {
		add(p)
	}
	for _, p := range h.winBash {
		if h.exists(p) && !h.isWSL(p) {
			add(p)
		}
	}
	// Android has no bash. `/system/bin/sh` is mksh — a POSIX shell that takes
	// `-c` like the rest — and it is the only one on the device, so without it
	// the search finds nothing, hands the session the literal name "bash", and
	// every command fails to start. That is the "Shell tool interpreter
	// fallback" a phone showed on launch, and it was not a warning about a
	// preference: the shell tool did not work at all.
	//
	// Listed after bash rather than instead of it, so a device with a real bash
	// installed (Termux, a rooted system) keeps using it.
	if h.goos == "android" {
		for _, p := range []string{"/system/bin/sh", "/vendor/bin/sh"} {
			if h.exists(p) {
				add(p)
			}
		}
	}
	return out
}

// bash proves the first usable bash candidate.
func (h shellHost) bash() (Shell, bool) {
	path, ok := h.firstProven(h.bashCandidates(), h.probe, func(p string) { bashProbeFailures.Store(p, FallbackProbeTimeout) })
	if !ok {
		return Shell{}, false
	}
	return Shell{Kind: ShellBash, Path: path}, true
}

// firstProven proves the first usable candidate. A candidate that has not
// answered within the hedge delay lets the next one start alongside it, so one
// hung launch does not serialise the rest, and the whole search stops waiting
// after the budget; every candidate still unanswered then goes to
// unanswered. Preference order decides the winner, never arrival order.
func (h shellHost) firstProven(cands []string, prove func(string) bool, unanswered func(string)) (string, bool) {
	budgetFor, hedgeFor := h.search.timing()
	answers := make([]chan bool, len(cands))
	start := func(i int) {
		if answers[i] != nil {
			return
		}
		c := make(chan bool, 1)
		answers[i] = c
		go func() { c <- prove(cands[i]) }()
	}
	budget := time.After(budgetFor)
	next := 0
	for i := range cands {
		start(i)
		next = max(next, i+1)
	wait:
		for {
			var hedge <-chan time.Time
			if next < len(cands) {
				hedge = time.After(hedgeFor)
			}
			select {
			case ok := <-answers[i]:
				if ok {
					return cands[i], true
				}
				break wait
			case <-hedge:
				start(next)
				next++
			case <-budget:
				return settleAtBudget(cands, answers, i, unanswered)
			}
		}
	}
	return "", false
}

// settleAtBudget settles a search that ran out of time: the first candidate
// from i on that already proved itself wins, and every one still unanswered is
// reported.
func settleAtBudget(cands []string, answers []chan bool, i int, unanswered func(string)) (string, bool) {
	won := ""
	for j := i; j < len(cands); j++ {
		var ok bool
		if answers[j] != nil {
			select {
			case ok = <-answers[j]:
			default:
				unanswered(cands[j])
				continue
			}
		}
		if ok && won == "" {
			won = cands[j]
		}
	}
	return won, won != ""
}

// bashProbeIdentity is the file a successful probe vouched for. The same path
// with the same size and mtime is the same executable, so it is not launched
// again; a failure is never kept, because a timeout may be transient.
type bashProbeIdentity struct {
	path    string
	size    int64
	modTime int64
}

// provenBash lives for the process. It holds successes, and for a short window
// the probes that hung: a bash that exists but never answers costs a probe
// timeout every time it is asked, and nothing about the file changed between
// asks. A probe that exited with a plain failure is cheap to repeat and not kept.
var provenBash sync.Map

// hungProbeMemory is how long a hung probe is not repeated for the same file.
var hungProbeMemory = time.Minute

type hungProbe struct{ at time.Time }

func probeBashMemo(path string, run func(string) bool) bool {
	return proveBash(path, run, nil, &provenBash)
}

// proveBash answers from this process's memo, then from a persisted proof of
// the same bytes, and only then launches the probe. The file's identity is its
// path, size and modification time.
func proveBash(path string, run func(string) bool, store *shellProofStore, memo *sync.Map) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return run(path)
	}
	id := bashProbeIdentity{path: path, size: fi.Size(), modTime: fi.ModTime().UnixNano()}
	if v, ok := memo.Load(id); ok {
		hung, wasHung := v.(hungProbe)
		if !wasHung {
			return true
		}
		if time.Since(hung.at) < hungProbeMemory {
			return false
		}
		memo.Delete(id)
	}
	if store.holds(path, fi) {
		memo.Store(id, struct{}{})
		return true
	}
	digest := ""
	if store != nil {
		digest, _ = digestFile(path)
	}
	if !run(path) {
		if bashProbeTimedOut(path) {
			memo.Store(id, hungProbe{at: time.Now()})
		}
		return false
	}
	memo.Store(id, struct{}{})
	if digest != "" {
		store.record(path, fi, digest)
	}
	return true
}
