package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"
)

type workload struct {
	Workers     int           `json:"workers"`
	Steps       int           `json:"steps"`
	Think       time.Duration `json:"think_ns"`
	Write       time.Duration `json:"write_ns"`
	RecordLocks bool          `json:"record_locks"`
}

type measurement struct {
	WallNS                  int64         `json:"wall_ns"`
	WaitNS                  int64         `json:"wait_ns"`
	HeldNS                  int64         `json:"held_ns"`
	ExecutingNS             int64         `json:"executing_ns"`
	InferenceNS             int64         `json:"inference_ns"`
	Conflicts               int           `json:"conflicts"`
	Rescheduled             int           `json:"rescheduled"`
	ResourceHeldLowerNS     int64         `json:"resource_held_lower_ns"`
	ResourceHeldUpperNS     int64         `json:"resource_held_upper_ns"`
	PreparationNS           int64         `json:"preparation_ns"`
	RegistryHeldNS          int64         `json:"registry_held_ns"`
	ResourceWaitNS          int64         `json:"resource_wait_ns"`
	InferenceWithResourceNS int64         `json:"inference_with_resource_ns"`
	Traces                  []workerTrace `json:"traces,omitempty"`
	probe                   *workerProbe
}

type workerRequest struct {
	Root, State, Mode, Scope string
	ID                       int
	Load                     workload
}

func main() {
	worker := flag.Bool("worker", false, "run the private worker protocol")
	trials := flag.Int("trials", 20, "paired trials per scope")
	workers := flag.Int("workers", 4, "independent writer processes")
	steps := flag.Int("steps", 3, "writes per process")
	think := flag.Duration("think", 20*time.Millisecond, "simulated model wait after each write")
	write := flag.Duration("write", 2*time.Millisecond, "simulated active operation time before atomic write")
	output := flag.String("output", "", "optional JSON report path")
	recordLocks := flag.Bool("record-locks", false, "record OS acquisition/release bounds and execution phases in memory")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var err error
	if *worker {
		err = runWorker(ctx)
	} else if *trials < 1 || *trials > 1000 || *workers < 1 || *workers > 32 || *steps < 1 || *steps > 1000 || *think < 0 || *write < 0 {
		err = fmt.Errorf("invalid workload limits")
	} else {
		err = runComparison(ctx, workload{*workers, *steps, *think, *write, *recordLocks}, *trials, *output)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type report struct {
	Platform       string            `json:"platform"`
	Go             string            `json:"go"`
	CPUs           int               `json:"logical_cpus"`
	Load           workload          `json:"load"`
	Definition     string            `json:"definition"`
	LockDefinition string            `json:"lock_definition,omitempty"`
	Results        map[string]result `json:"results"`
}

func runComparison(ctx context.Context, load workload, trials int, output string) error {
	r := report{Platform: runtime.GOOS + "/" + runtime.GOARCH, Go: runtime.Version(), CPUs: runtime.NumCPU(), Load: load,
		Definition: "Coordination-owner benchmark, not boot/model/UI acceptance. Real child processes and files. Wall starts after every child is ready and ends when all exit; startup/preparation excluded. Acquisition includes admission/version checking. API-owned spans run from successful Acquire return through release completion; internal grant time is excluded. Per-worker spans are summed and may exceed wall. Legacy holds its claim across simulated model waits. Paired mode order alternates. Opaque writes coordinate the whole workspace in both modes; named writes use independent files; same-file appends use transaction CAS retries.", Results: map[string]result{}}
	if load.RecordLocks {
		r.LockDefinition = "OS success boundaries are sampled in memory: lower hold spans syscall-success to before-unlock, upper spans before-lock to after-close. Resource bounds union exclusive workspace/claim-lock spans within each worker, including brief stale-record probes; shared compatibility, registry, preparation and liveness locks remain visible in raw traces. Registry/preparation summaries union their observed success spans. Worker preparation, each acquisition attempt, execution and model inference have separate monotonic phase intervals. Trace overflow, unsettled OS holds or transaction resource holds overlapping inference fail the run. API acquisition still includes admission and queueing, and is not a pure wait metric."
	}
	for _, scope := range []string{"opaque", "named", "same-file"} {
		for trial := range trials {
			modes := []string{"legacy", "transaction"}
			if trial%2 != 0 {
				modes[0], modes[1] = modes[1], modes[0]
			}
			for _, mode := range modes {
				m, err := runRound(ctx, load, mode, scope)
				if err != nil {
					return fmt.Errorf("%s/%s trial %d: %w", scope, mode, trial, err)
				}
				key := scope + "/" + mode
				row := r.Results[key]
				row.Samples = append(row.Samples, m)
				r.Results[key] = row
			}
		}
	}
	for key, row := range r.Results {
		row.summarize()
		r.Results[key] = row
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if output != "" {
		return os.WriteFile(output, raw, 0600)
	}
	_, err = os.Stdout.Write(raw)
	return err
}
