package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type round struct {
	WallNS  int64            `json:"wall_ns"`
	Workers []measuredWorker `json:"workers"`
}

type child struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	decoder *json.Decoder
	log     *os.File
	waited  bool
	waitErr error
}

func runRound(ctx context.Context, load workload, mode, scope string) (got round, err error) {
	directory, err := os.MkdirTemp("", "reasonix-product-concurrency-")
	if err != nil {
		return got, err
	}
	defer os.RemoveAll(directory)
	r := request{Home: filepath.Join(directory, "home"), Root: filepath.Join(directory, "workspace"), Sessions: filepath.Join(directory, "sessions"), Scope: scope, Mode: mode, Load: load}
	for _, path := range []string{r.Home, r.Root, r.Sessions} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return got, err
		}
	}
	configuration := "[codegraph]\nenabled=false\n[agent]\n"
	if mode == "shared" {
		configuration += "collaboration='shared'\n"
	}
	if err := os.WriteFile(filepath.Join(r.Home, "config.toml"), []byte(configuration), 0600); err != nil {
		return got, err
	}
	if err := os.WriteFile(filepath.Join(r.Root, "go.mod"), []byte("module reasonixbenchmark\n\ngo 1.26.0\n"), 0600); err != nil {
		return got, err
	}
	if err := os.WriteFile(filepath.Join(r.Root, "worker_test.go"), []byte(verificationFixture), 0600); err != nil {
		return got, err
	}
	if scope == "same-file" {
		var body strings.Builder
		for id := range load.Workers {
			fmt.Fprintf(&body, "worker-%d=0\n", id)
		}
		if err := os.WriteFile(filepath.Join(r.Root, "shared.txt"), []byte(body.String()), 0600); err != nil {
			return got, err
		}
	}
	var children []*child
	defer func() {
		var closing sync.WaitGroup
		for _, c := range children {
			closing.Go(func() { c.stop() })
		}
		closing.Wait()
	}()
	for id := range load.Workers {
		r.ID = id
		c, err := startChild(ctx, r, directory)
		if err != nil {
			return got, err
		}
		children = append(children, c)
	}
	for _, c := range children {
		var ready string
		if err := c.decoder.Decode(&ready); err != nil || ready != "ready" {
			return got, c.failure("preparation", errors.Join(err, errors.New("worker did not become ready")))
		}
	}
	started := time.Now()
	for _, c := range children {
		if err := json.NewEncoder(c.in).Encode("start"); err != nil {
			return got, err
		}
	}
	for _, c := range children {
		var measurement measuredWorker
		if err := c.decoder.Decode(&measurement); err != nil {
			return got, c.failure("execution", err)
		}
		got.Workers = append(got.Workers, measurement)
		c.waitErr = c.cmd.Wait()
		c.waited = true
		if c.waitErr != nil {
			return got, c.failure("exit", c.waitErr)
		}
	}
	got.WallNS = time.Since(started).Nanoseconds()
	return got, verifyFiles(r)
}

func (c *child) stop() {
	defer c.log.Close()
	c.in.Close()
	if c.waited {
		return
	}
	done := make(chan struct{})
	go func() { c.waitErr = c.cmd.Wait(); close(done) }()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		_ = c.cmd.Process.Kill()
		<-done
	}
	c.waited = true
}

func (c *child) failure(phase string, cause error) error {
	c.stop()
	body, _ := os.ReadFile(c.log.Name())
	exitErr := c.waitErr
	if exitErr != nil {
		exitErr = fmt.Errorf("process exit: %w", exitErr)
	}
	return fmt.Errorf("worker %s: %w: %s", phase, errors.Join(cause, exitErr), body)
}

const verificationFixture = `package fixture
import("fmt";"os";"strconv";"strings";"testing")
func TestWorker(t *testing.T){
 id,e:=strconv.Atoi(os.Getenv("BENCH_WORKER"));if e!=nil{t.Fatal(e)}
 steps,e:=strconv.Atoi(os.Getenv("BENCH_STEPS"));if e!=nil{t.Fatal(e)}
 read:=func(path string)string{t.Helper();b,e:=os.ReadFile(path);if e!=nil{t.Fatal(e)};return string(b)}
 switch os.Getenv("BENCH_SCOPE"){
 case "same-file":if strings.Count(read("shared.txt"),fmt.Sprintf("worker-%d=%d\n",id,steps))!=1{t.Fatal("missing final worker marker")}
 case "opaque":want:="";for step:=0;step<steps;step++{want+=fmt.Sprintf("step-%d\n",step)};if read(fmt.Sprintf("worker-%d.txt",id))!=want{t.Fatal("append mismatch")}
 default:for step:=0;step<steps;step++{name:=fmt.Sprintf("worker-%d-step-%d",id,step);if read(name+".txt")!=name+"\n"{t.Fatal("write mismatch")}}
 }
}
`

func startChild(ctx context.Context, r request, directory string) (*child, error) {
	cmd := exec.CommandContext(ctx, os.Args[0], "-worker")
	cmd.Dir = r.Root
	cmd.Env = append(os.Environ(), "REASONIX_HOME="+r.Home, "REASONIX_STATE_HOME="+r.Home, "REASONIX_CACHE_HOME="+filepath.Join(r.Home, "cache"))
	log, err := os.Create(filepath.Join(directory, fmt.Sprintf("worker-%d.log", r.ID)))
	if err != nil {
		return nil, err
	}
	cmd.Stderr = log
	in, err := cmd.StdinPipe()
	if err != nil {
		log.Close()
		return nil, err
	}
	cmd.Cancel = func() error { _ = in.Close(); return nil }
	cmd.WaitDelay = 10 * time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		log.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		in.Close()
		out.Close()
		log.Close()
		return nil, err
	}
	if err := json.NewEncoder(in).Encode(r); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		in.Close()
		out.Close()
		log.Close()
		return nil, err
	}
	return &child{cmd: cmd, in: in, decoder: json.NewDecoder(out), log: log}, nil
}

func verifyFiles(r request) error {
	if r.Scope == "same-file" {
		var want strings.Builder
		for id := range r.Load.Workers {
			fmt.Fprintf(&want, "worker-%d=%d\n", id, r.Load.Steps)
		}
		return requireFile(filepath.Join(r.Root, "shared.txt"), want.String())
	}
	for id := range r.Load.Workers {
		if r.Scope == "opaque" {
			var want strings.Builder
			for step := range r.Load.Steps {
				fmt.Fprintf(&want, "step-%d\n", step)
			}
			if err := requireFile(filepath.Join(r.Root, fmt.Sprintf("worker-%d.txt", id)), want.String()); err != nil {
				return err
			}
		} else {
			for step := range r.Load.Steps {
				name := fmt.Sprintf("worker-%d-step-%d", id, step)
				if err := requireFile(filepath.Join(r.Root, name+".txt"), name+"\n"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func requireFile(path, want string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(body) != want {
		return fmt.Errorf("mutation lost, duplicated or unexpected: %s: got %q, want %q", path, body, want)
	}
	return nil
}
