package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type benchProcess struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	decoder  *json.Decoder
	stderr   bytes.Buffer
	waitOnce sync.Once
	waitErr  error
}

func (p *benchProcess) wait() error {
	p.waitOnce.Do(func() { p.waitErr = p.cmd.Wait() })
	return p.waitErr
}

func startWorker(ctx context.Context, request workerRequest) (*benchProcess, error) {
	program, err := os.Executable()
	if err != nil {
		return nil, err
	}
	p := &benchProcess{cmd: exec.CommandContext(ctx, program, "-worker")}
	p.cmd.Stderr = &p.stderr
	p.stdin, err = p.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		_ = p.stdin.Close()
		return nil, err
	}
	p.decoder = json.NewDecoder(stdout)
	if err := p.cmd.Start(); err != nil {
		_ = p.stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	if err := json.NewEncoder(p.stdin).Encode(request); err != nil {
		_ = p.cmd.Process.Kill()
		_ = p.wait()
		return nil, err
	}
	return p, nil
}

func runRound(ctx context.Context, load workload, mode, scope string) (m measurement, resultErr error) {
	root, err := os.MkdirTemp("", "reasonix-coordination-bench-")
	if err != nil {
		return m, err
	}
	defer os.RemoveAll(root)
	ctx, cancel := context.WithCancel(ctx)
	var workers []*benchProcess
	defer func() {
		cancel()
		for _, p := range workers {
			_ = p.stdin.Close()
			if err := p.wait(); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("worker exit: %w: %s", err, p.stderr.String()))
			}
		}
	}()
	request := workerRequest{Root: filepath.Join(root, "workspace"), State: filepath.Join(root, "state"), Mode: mode, Scope: scope, Load: load}
	if err := os.MkdirAll(request.Root, 0700); err != nil {
		return m, err
	}
	for id := range load.Workers {
		request.ID = id
		if err := os.WriteFile(requestPath(request), nil, 0600); err != nil {
			return m, err
		}
	}
	for id := range load.Workers {
		request.ID = id
		p, err := startWorker(ctx, request)
		if err != nil {
			return m, err
		}
		workers = append(workers, p)
	}
	if err := startReadyWorkers(workers); err != nil {
		return m, err
	}
	started := time.Now()
	for _, p := range workers {
		if err := json.NewEncoder(p.stdin).Encode("start"); err != nil {
			return m, err
		}
	}
	for _, p := range workers {
		var child measurement
		if err := p.decoder.Decode(&child); err != nil {
			return m, err
		}
		if err := p.wait(); err != nil {
			return m, err
		}
		m.add(child)
	}
	m.WallNS = time.Since(started).Nanoseconds()
	return m, verifyOutput(request)
}

func startReadyWorkers(workers []*benchProcess) error {
	for _, p := range workers {
		var phase string
		if err := p.decoder.Decode(&phase); err != nil {
			return err
		}
		if phase != "ready" {
			return errors.New("worker did not reach ready barrier")
		}
	}
	return nil
}

func verifyOutput(request workerRequest) error {
	var actual, expected []string
	for id := range request.Load.Workers {
		request.ID = id
		if id == 0 || request.Scope != "same-file" {
			raw, err := os.ReadFile(requestPath(request))
			if err != nil {
				return err
			}
			actual = append(actual, strings.Fields(string(raw))...)
		}
		for step := range request.Load.Steps {
			expected = append(expected, fmt.Sprintf("%d:%d", id, step))
		}
	}
	slices.Sort(actual)
	slices.Sort(expected)
	if !slices.Equal(actual, expected) {
		return errors.New("writes were lost, duplicated or changed")
	}
	return nil
}
