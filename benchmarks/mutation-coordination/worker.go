package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/workspacelease"
)

func runWorker(ctx context.Context) error {
	decoder := json.NewDecoder(bufio.NewReader(os.Stdin))
	var request workerRequest
	if err := decoder.Decode(&request); err != nil {
		return err
	}
	probe, err := startWorkerProbe(request.Load.RecordLocks)
	if err != nil {
		return err
	}
	defer probe.close()
	prepared := probe.phase("preparation")
	var old *workspacelease.Owner
	var coordinator *mutation.Coordinator
	if request.Mode == "legacy" {
		old, err = workspacelease.New(request.Root, request.State, nil)
	} else {
		coordinator, err = mutation.New(request.Root, request.State, probe.notice)
	}
	if err != nil {
		return err
	}
	prepared()
	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode("ready"); err != nil {
		return err
	}
	var start string
	if err := decoder.Decode(&start); err != nil || start != "start" {
		return errors.Join(err, errors.New("missing start barrier"))
	}
	var measured measurement
	if old != nil {
		measured, err = runLegacy(ctx, request, old, probe)
	} else {
		measured, err = runTransactions(ctx, request, coordinator, probe)
	}
	if err != nil {
		return err
	}
	if err := probe.finish(request.ID, request.State, &measured); err != nil {
		return err
	}
	if request.Load.RecordLocks && request.Mode == "transaction" && measured.InferenceWithResourceNS != 0 {
		return errors.New("transaction retained a resource OS lock during model inference")
	}
	return encoder.Encode(measured)
}

func requestPath(request workerRequest) string {
	if request.Scope == "same-file" {
		return filepath.Join(request.Root, "shared")
	}
	return filepath.Join(request.Root, fmt.Sprintf("worker-%d", request.ID))
}

func runLegacy(ctx context.Context, request workerRequest, owner *workspacelease.Owner, probe *workerProbe) (m measurement, err error) {
	m.probe = probe
	owner.OnRelease(func(st workspacelease.Stats) { m.ResourceWaitNS += st.Waited.Nanoseconds() })
	owner.BeginRun()
	var held time.Time
	defer func() {
		owner.EndRun()
		if !held.IsZero() {
			m.HeldNS = time.Since(held).Nanoseconds()
		}
	}()
	started := time.Now()
	acquired := probe.phase("acquisition")
	if request.Scope == "opaque" {
		err = owner.AcquireWrite(ctx)
	} else {
		err = owner.AcquirePaths(ctx, []string{requestPath(request)})
	}
	m.WaitNS = time.Since(started).Nanoseconds()
	acquired()
	if err != nil {
		return m, err
	}
	held = time.Now()
	for step := range request.Load.Steps {
		if err := appendOperation(ctx, request, step, &m); err != nil {
			return m, err
		}
		if err := modelWait(ctx, request.Load.Think, &m); err != nil {
			return m, err
		}
	}
	return m, nil
}

func runTransactions(ctx context.Context, request workerRequest, coordinator *mutation.Coordinator, probe *workerProbe) (m measurement, err error) {
	m.probe = probe
	for step := range request.Load.Steps {
		if err := transactionOperation(ctx, request, coordinator, step, &m); err != nil {
			return m, err
		}
		if err := modelWait(ctx, request.Load.Think, &m); err != nil {
			return m, err
		}
	}
	return m, nil
}

func transactionOperation(ctx context.Context, request workerRequest, c *mutation.Coordinator, step int, m *measurement) error {
	path := requestPath(request)
	for range 128 {
		version, err := fileutil.ReadContentVersion(ctx, path)
		if err != nil {
			return err
		}
		intent := mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{path}, Expected: map[string]mutation.ContentVersion{path: version}}
		if request.Scope == "opaque" {
			intent.Effect = mutation.Workspace
		}
		started := time.Now()
		acquired := m.probe.phase("acquisition")
		lease, err := c.Acquire(ctx, intent)
		acquired()
		m.WaitNS += time.Since(started).Nanoseconds()
		if errors.Is(err, mutation.ErrVersionConflict) {
			m.Conflicts++
			m.Rescheduled++
			continue
		}
		if err != nil {
			return err
		}
		held := time.Now()
		err = appendOperation(ctx, request, step, m)
		outcome := mutation.Committed
		if err != nil {
			outcome = mutation.Unknown
		}
		lease.End(outcome)
		m.HeldNS += time.Since(held).Nanoseconds()
		return err
	}
	return errors.New("CAS retry workload exhausted")
}

func appendOperation(ctx context.Context, request workerRequest, step int, m *measurement) error {
	defer m.probe.phase("execution")()
	started := time.Now()
	defer func() { m.ExecutingNS += time.Since(started).Nanoseconds() }()
	if err := pause(ctx, request.Load.Write); err != nil {
		return err
	}
	path := requestPath(request)
	before, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	after := append(before, fmt.Appendf(nil, "%d:%d\n", request.ID, step)...)
	return fileutil.AtomicWriteFileStrict(path, after, 0600)
}

func modelWait(ctx context.Context, duration time.Duration, m *measurement) error {
	defer m.probe.phase("inference")()
	started := time.Now()
	err := pause(ctx, duration)
	m.InferenceNS += time.Since(started).Nanoseconds()
	return err
}

func pause(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
