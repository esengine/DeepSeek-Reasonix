package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"reasonix/internal/base/fileutil"
	"runtime"
	"time"
)

type workload struct {
	Workers int           `json:"workers"`
	Steps   int           `json:"steps"`
	Think   time.Duration `json:"think_ns"`
	Record  bool          `json:"record_locks"`
}

type request struct {
	Home, Root, Sessions, Scope, Mode string
	ID                                int
	Load                              workload
}

type interval struct {
	Phase string `json:"phase"`
	Start int64  `json:"start_ns"`
	End   int64  `json:"end_ns"`
}

type report struct {
	Platform   string             `json:"platform"`
	Go         string             `json:"go"`
	CPUs       int                `json:"logical_cpus"`
	Definition string             `json:"definition"`
	Load       workload           `json:"load"`
	Results    map[string][]round `json:"results"`
	Completed  bool               `json:"completed"`
	Failure    string             `json:"failure,omitempty"`
}

func main() {
	worker := flag.Bool("worker", false, "private process protocol")
	trials := flag.Int("trials", 20, "alternating paired trials per scope")
	workers := flag.Int("workers", 4, "real Controller processes")
	steps := flag.Int("steps", 3, "committed changes per session")
	think := flag.Duration("think", 100*time.Millisecond, "simulated provider latency per request")
	record := flag.Bool("record-locks", false, "collect actual OS lock bounds separately from throughput")
	output := flag.String("output", "", "JSON report destination")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var err error
	if *worker {
		err = runWorker(ctx)
	} else if *trials < 1 || *trials > 100 || *workers < 1 || *workers > 16 || *steps < 1 || *steps > 100 || *think < 0 || *think > time.Minute || *output == "" {
		err = errors.New("invalid workload or missing -output")
	} else {
		err = compare(ctx, workload{*workers, *steps, *think, *record}, *trials, *output)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func compare(ctx context.Context, load workload, trials int, output string) error {
	r := report{Platform: runtime.GOOS + "/" + runtime.GOARCH, Go: runtime.Version(), CPUs: runtime.NumCPU(), Load: load,
		Definition: "Product assembly benchmark. Each worker is a separate process with a real boot.Build Controller, session lease, actual tools and shared account coordination directory. Only the provider is simulated. Wall starts after every Controller is built, leased and ready, and includes turn execution, final snapshot, controller shutdown and process exit. Preparation is reported separately. Legacy uses the current binary's compatibility policy with no collaboration field; shared explicitly enables short transactions. Each pair uses fresh workspaces, identical source/configuration and alternating order. named uses distinct write_file targets; opaque uses Bash append to per-worker targets; same-file uses independent read_file/edit_file CAS loops. Failed edits are re-read and rescheduled, never silently overwritten. Traces include real OS locks, provider waits, tool dispatch/result and contention events; recording is separate from throughput.", Results: map[string][]round{}}
	r.Definition += " Both modes first execute the same per-worker Bash admission-marker write, so legacy can acquire workspace scope before narrower file claims. Separate failed unprimed runs demonstrate the legacy widening limitation; this report compares completing workloads."
	for _, scope := range []string{"named", "opaque", "same-file"} {
		for trial := range trials {
			modes := []string{"legacy", "shared"}
			if trial%2 == 1 {
				modes[0], modes[1] = modes[1], modes[0]
			}
			for _, mode := range modes {
				got, err := runRound(ctx, load, mode, scope)
				if err != nil {
					err = fmt.Errorf("%s/%s trial %d: %w", scope, mode, trial, err)
					r.Failure = err.Error()
					return errors.Join(err, r.save(output))
				}
				key := scope + "/" + mode
				r.Results[key] = append(r.Results[key], got)
				if err := r.save(output); err != nil {
					return err
				}
			}
		}
	}
	r.Completed = true
	return r.save(output)
}

func (r report) save(output string) error {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(output, append(raw, '\n'), 0600)
}
