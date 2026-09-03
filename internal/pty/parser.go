package pty

import (
	"bytes"
	"strings"
	"sync"
)

// MaxCommandOutputBufferBytes is the maximum captured output size for a single write_line command (4 MiB).
const MaxCommandOutputBufferBytes = 4 * 1024 * 1024

// CompletionParser performs streaming detection of completion sentinels from raw PTY bytes.
// It runs in the OutputPump goroutine, completely decoupled from user read buffers.
type CompletionParser struct {
	mu        sync.Mutex
	active    bool
	marker    string
	driver    ShellDriver
	notify    chan struct{}
	closeOnce sync.Once

	matched   bool
	exitCode  int

	lineBuf   bytes.Buffer
	cmdBuf    bytes.Buffer
	truncated bool
}

// NewCompletionParser allocates an un-armed completion parser.
func NewCompletionParser() *CompletionParser {
	return &CompletionParser{}
}

// Arm arms the parser to look for the specified sentinel marker using the given driver.
func (cp *CompletionParser) Arm(marker string, driver ShellDriver) <-chan struct{} {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	cp.active = true
	cp.marker = marker
	cp.driver = driver
	cp.notify = make(chan struct{})
	cp.closeOnce = sync.Once{}
	cp.matched = false
	cp.exitCode = 0
	cp.lineBuf.Reset()
	cp.cmdBuf.Reset()
	cp.truncated = false

	return cp.notify
}

// Disarm cancels active sentinel parsing.
func (cp *CompletionParser) Disarm() {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.active = false
}

// Feed processes newly arrived raw bytes from the low-level PTY.
func (cp *CompletionParser) Feed(p []byte) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if !cp.active || cp.matched {
		return
	}

	for _, b := range p {
		cp.lineBuf.WriteByte(b)
		if b == '\n' {
			line := cp.lineBuf.String()
			cp.lineBuf.Reset()

			cleanLine := CleanTerminalOutput(line)
			if cp.driver != nil && cp.marker != "" {
				if code, ok := cp.driver.ParseSentinel(cleanLine, cp.marker); ok {
					cp.matched = true
					cp.exitCode = code
					cp.closeOnce.Do(func() {
						close(cp.notify)
					})
					return
				}
			}

			// Exclude the command echo line that printed the printf sentinel itself
			if cp.marker != "" && strings.Contains(cleanLine, cp.marker) {
				continue
			}

			// Append line to command output buffer up to maximum size
			if cp.cmdBuf.Len() < MaxCommandOutputBufferBytes {
				cp.cmdBuf.WriteString(cleanLine)
				if !strings.HasSuffix(cleanLine, "\n") {
					cp.cmdBuf.WriteByte('\n')
				}
			} else {
				cp.truncated = true
			}
		}
	}
}

// Result returns the captured output, exit code, and whether the sentinel matched.
func (cp *CompletionParser) Result() (output string, exitCode int, matched bool) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	out := strings.TrimSpace(cp.cmdBuf.String())
	if cp.truncated {
		out += "\n(output truncated at 4 MiB)"
	}
	return out, cp.exitCode, cp.matched
}
