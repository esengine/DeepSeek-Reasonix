package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"

	"reasonix/internal/base/proc"
	"reasonix/internal/base/secrets"
	"reasonix/internal/base/shellparse"
	"reasonix/internal/safety/sandbox"
)

var errDeleteParser = errors.New("host shell parser unavailable")

type deleteParserProcess struct {
	cmd    *exec.Cmd
	input  io.WriteCloser
	output *bufio.Reader
}

type deleteParserWorker struct {
	token   chan struct{}
	process *deleteParserProcess
}

var deleteParsers = struct {
	sync.Mutex
	workers map[string]*deleteParserWorker
}{workers: make(map[string]*deleteParserWorker)}

func analyzePowerShellDelete(ctx context.Context, sh sandbox.Shell, command string) (shellparse.DeleteAnalysis, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	deleteParsers.Lock()
	worker := deleteParsers.workers[sh.Path]
	if worker == nil {
		worker = &deleteParserWorker{token: make(chan struct{}, 1)}
		worker.token <- struct{}{}
		deleteParsers.workers[sh.Path] = worker
	}
	deleteParsers.Unlock()
	select {
	case <-ctx.Done():
		return shellparse.DeleteAnalysis{}, ctx.Err()
	case <-worker.token:
	}
	defer func() { worker.token <- struct{}{} }()
	if ctx.Err() != nil {
		return shellparse.DeleteAnalysis{}, ctx.Err()
	}
	if worker.process == nil {
		p, err := startDeleteParser(sh)
		if err != nil {
			return shellparse.DeleteAnalysis{}, errDeleteParser
		}
		worker.process = p
	}
	p := worker.process
	type response struct {
		data []byte
		err  error
	}
	done := make(chan response, 1)
	go func() {
		data, err := json.Marshal(command)
		if err == nil {
			_, err = p.input.Write(append(data, '\n'))
		}
		if err == nil {
			// The persistent protocol needs exactly one stdout line per request
			// so diagnostics cannot be mistaken for the next analysis response.
			data, err = p.output.ReadBytes('\n')
		}
		done <- response{data, err}
	}()
	var answer response
	select {
	case <-ctx.Done():
		stopDeleteParser(p)
		worker.process = nil
		<-done
		return shellparse.DeleteAnalysis{}, ctx.Err()
	case answer = <-done:
	}
	var analysis shellparse.DeleteAnalysis
	if answer.err != nil || json.Unmarshal(answer.data, &analysis) != nil || analysis.HostError {
		stopDeleteParser(p)
		worker.process = nil
		return shellparse.DeleteAnalysis{}, errDeleteParser
	}
	return analysis, nil
}

func startDeleteParser(sh sandbox.Shell) (*deleteParserProcess, error) {
	cmd := exec.Command(sh.Path, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", sandbox.PowerShellUTF8Script(shellparse.PowerShellDeleteServer))
	cmd.Env = secrets.ProcessEnv()
	proc.HideWindow(cmd)
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, err
	}
	return &deleteParserProcess{cmd: cmd, input: input, output: bufio.NewReader(output)}, nil
}

func stopDeleteParser(p *deleteParserProcess) {
	_ = p.input.Close()
	_ = p.cmd.Process.Kill()
	_ = p.cmd.Wait()
}
