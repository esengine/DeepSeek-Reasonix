package builtin

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/safety/sandbox"
)

// A pipeline hands the shell only its last stage's status, so `go test | tail`
// exits zero on a failing suite and the host cannot say whether the check
// passed. bash keeps every stage's status in PIPESTATUS; asking for it costs
// one line and changes no semantics, where `set -o pipefail` would change what
// `| head` means.
type pipeStatusProbe struct{ nonce string }

func newPipeStatusProbe(sh sandbox.Shell, command string, background bool) pipeStatusProbe {
	if background || sh.Kind != sandbox.ShellBash || !evidence.PipeStatusCanDecideVerification(command) {
		return pipeStatusProbe{}
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return pipeStatusProbe{}
	}
	return pipeStatusProbe{nonce: hex.EncodeToString(raw[:])}
}

func (p pipeStatusProbe) active() bool { return p.nonce != "" }

// wrap appends the report and restores the status the command would have
// exited with, so nothing downstream sees a different result. A command that
// exits or execs never reaches the report, which simply leaves it unreported.
func (p pipeStatusProbe) wrap(command string) string {
	if !p.active() {
		return command
	}
	return command + "\n__rx_ps=(\"${PIPESTATUS[@]}\"); printf '%s %s\\n' '" + p.marker() +
		"' \"${__rx_ps[*]}\" >&2; exit \"${__rx_ps[${#__rx_ps[@]}-1]:-0}\"\n"
}

func (p pipeStatusProbe) marker() string {
	if !p.active() {
		return ""
	}
	return "__rx_pipestatus_" + p.nonce
}

// harvest moves the report out of everything the model can see and onto the
// execution record. Output truncation or a killed process drops the line,
// which reads as no report rather than a wrong one.
func (p pipeStatusProbe) harvest(out string, ex *tool.ShellExecution) string {
	if !p.active() || ex == nil {
		return out
	}
	out, ex.PipeStatus = p.read(out)
	ex.OutputTail, _ = p.read(ex.OutputTail)
	return out
}

func (p pipeStatusProbe) read(out string) (string, []int) {
	if !p.active() || !strings.Contains(out, p.marker()) {
		return out, nil
	}
	var kept []string
	var status []int
	for line := range strings.SplitSeq(out, "\n") {
		rest, found := strings.CutPrefix(strings.TrimSpace(line), p.marker())
		if !found {
			kept = append(kept, line)
			continue
		}
		status = parsePipeStatus(rest)
	}
	return strings.TrimRight(strings.Join(kept, "\n"), "\n"), status
}

func parsePipeStatus(fields string) []int {
	var status []int
	for field := range strings.FieldsSeq(fields) {
		code, err := strconv.Atoi(field)
		if err != nil {
			return nil
		}
		status = append(status, code)
	}
	return status
}
