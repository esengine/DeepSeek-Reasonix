package computer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"sync"

	"reasonix/internal/base/proc"
)

// Helper is the native process that carries operations out. It starts on first
// use and again after it exits; a request pending when it goes fails as
// computer.failed rather than waiting forever.
type Helper struct {
	path string

	mu    sync.Mutex
	live  *helperProcess
	stops uint64          // how many times the person has asked the agent to stop
	asked map[string]bool // permissions already requested from the system
}

// NewHelper returns a helper that runs the executable at path when needed.
func NewHelper(path string) *Helper { return &Helper{path: path} }

type helperProcess struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	wmu     sync.Mutex
	mu      sync.Mutex
	next    int64
	pending map[int64]chan helperReply
	done    chan struct{}
}

type helperReply struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Event string `json:"event"`
}

var errHelperGone = errors.New("the computer-use helper exited")

func (h *Helper) process() (*helperProcess, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.live != nil {
		select {
		case <-h.live.done:
		default:
			return h.live, nil
		}
	}
	cmd := exec.Command(h.path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	proc.HideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &helperProcess{cmd: cmd, stdin: stdin, pending: map[int64]chan helperReply{}, done: make(chan struct{})}
	go h.read(p, stdout)
	h.live = p
	return p, nil
}

func (h *Helper) read(p *helperProcess, stdout io.Reader) {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var r helperReply
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		if r.Event == "stop" {
			h.mu.Lock()
			h.stops++
			h.mu.Unlock()
			continue
		}
		p.mu.Lock()
		ch := p.pending[r.ID]
		delete(p.pending, r.ID)
		p.mu.Unlock()
		if ch != nil {
			ch <- r
		}
	}
	_ = p.cmd.Wait()
	p.mu.Lock()
	close(p.done)
	for id, ch := range p.pending {
		close(ch)
		delete(p.pending, id)
	}
	p.mu.Unlock()
}

// stopMark is where stoppedSince measures from. A count rather than a clock:
// Windows advances time.Now by timer tick, so a stop inside the tick a run
// started in would compare equal to its start and go unseen.
func (h *Helper) stopMark() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stops
}

// stoppedSince reports whether the person asked the agent to stop after mark.
func (h *Helper) stoppedSince(mark uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stops != mark
}

// firstAsk reports whether permission has not been requested yet, and records
// that it now has been.
func (h *Helper) firstAsk(permission string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.asked[permission] {
		return false
	}
	if h.asked == nil {
		h.asked = map[string]bool{}
	}
	h.asked[permission] = true
	return true
}

// Call sends one request and decodes its result into out.
func (h *Helper) Call(ctx context.Context, method string, params, out any) error {
	p, err := h.process()
	if err != nil {
		return fail(CodeUnavailable, "the computer-use helper could not start: %v", err)
	}
	reply := make(chan helperReply, 1)
	p.mu.Lock()
	select {
	case <-p.done:
		p.mu.Unlock()
		return fail(CodeFailed, "%v", errHelperGone)
	default:
	}
	p.next++
	id := p.next
	p.pending[id] = reply
	p.mu.Unlock()
	line, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	p.wmu.Lock()
	_, err = p.stdin.Write(append(line, '\n'))
	p.wmu.Unlock()
	if err != nil {
		return fail(CodeFailed, "%v", errHelperGone)
	}
	select {
	case r, ok := <-reply:
		if !ok {
			return fail(CodeFailed, "%v", errHelperGone)
		}
		if r.Error != nil {
			return &Failure{Code: Code(r.Error.Code), Detail: r.Error.Message}
		}
		if out != nil && len(r.Result) > 0 {
			return json.Unmarshal(r.Result, out)
		}
		return nil
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return ctx.Err()
	}
}
