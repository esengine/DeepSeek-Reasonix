package browser

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"reasonix/internal/base/proc"
)

const (
	launchTimeout = 20 * time.Second
	closeTimeout  = 3 * time.Second
	// A headless tab otherwise gets a viewport too small for most layouts.
	viewportWidth  = 1280
	viewportHeight = 900
)

// LaunchSpec is what one browser process starts with. ProfileDir is its own:
// a browser never runs on the person's everyday profile.
type LaunchSpec struct {
	Executable string
	ProfileDir string
	Headless   bool
}

// usePipe is false where a child cannot inherit descriptors 3 and 4. The port
// endpoint it falls back to is reachable by any local process while it runs.
var usePipe = runtime.GOOS != "windows"

// engine is one running browser and the connection that drives it.
type engine struct {
	spec   LaunchSpec
	cmd    *exec.Cmd
	job    *proc.TrackedJob
	conn   *conn
	exited chan struct{}

	mu        sync.Mutex
	listeners map[int]func(event)
	nextID    int
	unsub     func()
}

func launchArgs(spec LaunchSpec) []string {
	args := []string{
		"--user-data-dir=" + spec.ProfileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-features=Translate,MediaRouter",
		fmt.Sprintf("--window-size=%d,%d", viewportWidth, viewportHeight),
	}
	switch runtime.GOOS {
	case "darwin":
		args = append(args, "--use-mock-keychain")
	case "linux":
		args = append(args, "--password-store=basic")
	}
	if spec.Headless {
		args = append(args, "--headless=new")
	}
	if usePipe {
		args = append(args, "--remote-debugging-pipe")
	} else {
		args = append(args, "--remote-debugging-port=0")
	}
	return append(args, "about:blank")
}

func launch(ctx context.Context, spec LaunchSpec) (*engine, error) {
	if err := os.MkdirAll(spec.ProfileDir, 0o700); err != nil {
		return nil, fail(CodeEngineFailed, "create the browser profile directory: %v", err)
	}
	ctx, cancel := context.WithTimeout(ctx, launchTimeout)
	defer cancel()

	activePort := filepath.Join(spec.ProfileDir, "DevToolsActivePort")
	cmd := exec.Command(spec.Executable, launchArgs(spec)...)
	proc.HideWindow(cmd)
	var tr transport
	var parentEnds []*os.File
	if usePipe {
		toBrowser, commands, err := os.Pipe()
		if err != nil {
			return nil, fail(CodeEngineFailed, "create the command pipe: %v", err)
		}
		replies, fromBrowser, err := os.Pipe()
		if err != nil {
			_ = toBrowser.Close()
			_ = commands.Close()
			return nil, fail(CodeEngineFailed, "create the reply pipe: %v", err)
		}
		cmd.ExtraFiles = []*os.File{toBrowser, fromBrowser}
		parentEnds = []*os.File{toBrowser, fromBrowser}
		tr = newPipeTransport(replies, commands, commands, replies)
	} else {
		_ = os.Remove(activePort)
	}
	job, err := proc.StartTracked(cmd)
	for _, f := range parentEnds {
		_ = f.Close()
	}
	if err != nil {
		if tr != nil {
			_ = tr.close()
		}
		return nil, fail(CodeEngineFailed, "start %s: %v", spec.Executable, err)
	}
	e := &engine{spec: spec, cmd: cmd, job: job, exited: make(chan struct{}), listeners: map[int]func(event){}}
	go func() {
		_ = cmd.Wait()
		close(e.exited)
	}()
	if tr == nil {
		ws, err := e.dialActivePort(ctx, activePort)
		if err != nil {
			e.kill()
			return nil, e.launchFailure(err)
		}
		tr = ws
	}
	e.conn = newConn(tr)
	e.unsub = e.conn.subscribe("", e.dispatch)
	if err := e.conn.call(ctx, "", "Browser.getVersion", nil, nil); err != nil {
		e.kill()
		return nil, e.launchFailure(err)
	}
	if err := e.conn.call(ctx, "", "Target.setDiscoverTargets", map[string]any{"discover": true}, nil); err != nil {
		e.kill()
		return nil, e.launchFailure(err)
	}
	// A download lands outside the workspace, where nothing the host observes
	// can say it happened, so none is allowed; each refusal is reported.
	if err := e.conn.call(ctx, "", "Browser.setDownloadBehavior", map[string]any{"behavior": "deny", "eventsEnabled": true}, nil); err != nil {
		e.kill()
		return nil, e.launchFailure(err)
	}
	return e, nil
}

// dialActivePort waits for the browser to write the port it chose, then
// connects to it.
func (e *engine) dialActivePort(ctx context.Context, path string) (*wsTransport, error) {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if port, wsPath, ok := readActivePort(path); ok {
			return dialWS(ctx, "ws://127.0.0.1:"+port+wsPath)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-e.exited:
			return nil, errConnClosed
		case <-tick.C:
		}
	}
}

func readActivePort(path string) (port, wsPath string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var lines []string
	for sc.Scan() {
		lines = append(lines, strings.TrimSpace(sc.Text()))
	}
	if sc.Err() != nil || len(lines) < 2 || lines[0] == "" || !strings.HasPrefix(lines[1], "/devtools/browser/") {
		return "", "", false
	}
	return lines[0], lines[1], true
}

// launchFailure names why a start did not reach a working connection. A
// browser that exits at once on a profile another browser holds leaves that
// browser's lock behind, which is the only cause the host can see.
func (e *engine) launchFailure(err error) error {
	if _, statErr := os.Lstat(filepath.Join(e.spec.ProfileDir, "SingletonLock")); statErr == nil {
		select {
		case <-e.exited:
			return fail(CodeProfileBusy, "another browser is already running on %s", e.spec.ProfileDir)
		default:
		}
	}
	return fail(CodeEngineFailed, "%s did not accept a debugging connection: %v", e.spec.Executable, err)
}

func (e *engine) alive() bool {
	select {
	case <-e.conn.closed():
		return false
	case <-e.exited:
		return false
	default:
		return true
	}
}

// listen registers a handler for browser-level events, which every session on
// this engine shares.
func (e *engine) listen(handle func(event)) (stop func()) {
	e.mu.Lock()
	id := e.nextID
	e.nextID++
	e.listeners[id] = handle
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		delete(e.listeners, id)
		e.mu.Unlock()
	}
}

func (e *engine) dispatch(ev event) {
	e.mu.Lock()
	handlers := make([]func(event), 0, len(e.listeners))
	for _, h := range e.listeners {
		handlers = append(handlers, h)
	}
	e.mu.Unlock()
	for _, h := range handlers {
		h(ev)
	}
}

// close asks the browser to quit and kills it when it does not. A hosted
// browser is the host's to keep; closing asks it to drop what this profile
// opened, and ends the connection.
func (e *engine) close() {
	if e.conn != nil && e.alive() {
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		_ = e.conn.call(ctx, "", "Browser.close", nil, nil)
		cancel()
		if e.cmd != nil {
			select {
			case <-e.exited:
			case <-time.After(closeTimeout):
			}
		}
	}
	e.kill()
}

func (e *engine) kill() {
	if e.unsub != nil {
		e.unsub()
	}
	if e.cmd != nil {
		e.job.Kill(e.cmd)
	}
	if e.conn != nil {
		e.conn.shutdown()
	}
}

// Pool shares one browser per profile among the sessions that use it: a
// profile directory admits one browser process at a time. A pool with a host
// endpoint reaches the host's browser instead of launching one.
type Pool struct {
	mu      sync.Mutex
	engines map[string]*pooledEngine
	dial    EndpointDialer
}

// SetEndpoint routes every later browser start through a host. Engines already
// running keep the browser they have.
func (p *Pool) SetEndpoint(dial EndpointDialer) {
	p.mu.Lock()
	p.dial = dial
	p.mu.Unlock()
}

type pooledEngine struct {
	eng  *engine
	refs int
}

func (p *Pool) acquire(ctx context.Context, spec LaunchSpec) (*engine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.engines == nil {
		p.engines = map[string]*pooledEngine{}
	}
	if pe := p.engines[spec.ProfileDir]; pe != nil {
		if pe.eng.alive() {
			pe.refs++
			return pe.eng, nil
		}
		delete(p.engines, spec.ProfileDir)
		go pe.eng.kill()
	}
	eng, err := p.start(ctx, spec)
	if err != nil {
		return nil, err
	}
	p.engines[spec.ProfileDir] = &pooledEngine{eng: eng, refs: 1}
	return eng, nil
}

// start reaches the host's browser when one is attached, and otherwise
// launches one found on this machine.
func (p *Pool) start(ctx context.Context, spec LaunchSpec) (*engine, error) {
	if p.dial != nil {
		ep, err := p.dial(ctx, spec.ProfileDir)
		switch {
		case err == nil:
			return attachEndpoint(ctx, spec, ep)
		case !errors.Is(err, ErrNoHost):
			return nil, fail(CodeEngineFailed, "the host browser refused a connection: %v", err)
		}
	}
	exe, err := Discover(spec.Executable)
	if err != nil {
		return nil, err
	}
	spec.Executable = exe
	return launch(ctx, spec)
}

func (p *Pool) release(eng *engine) {
	p.mu.Lock()
	pe := p.engines[eng.spec.ProfileDir]
	last := false
	if pe != nil && pe.eng == eng {
		pe.refs--
		if pe.refs <= 0 {
			delete(p.engines, eng.spec.ProfileDir)
			last = true
		}
	}
	p.mu.Unlock()
	if last || pe == nil || pe.eng != eng {
		eng.close()
	}
}
