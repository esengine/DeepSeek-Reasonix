package appupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/platform/update"
)

// The handover ends this process, so whether a move landed is only knowable by
// the next launch. The record says what was attempted; the swap helper adds why
// it gave up, since it is detached and has nobody else to tell.
const (
	moveRecordName  = "move.json"
	swapOutcomeName = "swap-outcome.txt"
)

type pendingMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// recordMove is written only for a move forward. The build it lands on then
// carries this code and consumes the record; a rollback may land on a build
// that predates it, which would leave the record to be misread later.
func recordMove(dir string, m pendingMove) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, moveRecordName), b, 0o600)
}

// settleLastMove consumes what the previous launch handed over and reports the
// failure this launch is evidence of: still running the build it moved from.
func settleLastMove(dir, running string) (update.Progress, bool) {
	recordPath, outcomePath := filepath.Join(dir, moveRecordName), filepath.Join(dir, swapOutcomeName)
	b, err := os.ReadFile(recordPath)
	why, _ := os.ReadFile(outcomePath)
	_ = os.Remove(recordPath)
	_ = os.Remove(outcomePath)
	if err != nil {
		return update.Progress{}, false
	}
	var m pendingMove
	if json.Unmarshal(b, &m) != nil || m.To == "" || !update.SameVersion(m.From, running) {
		return update.Progress{}, false
	}
	return update.Progress{Version: m.To, Phase: update.PhaseFailed, Code: FailNotApplied, Err: strings.TrimSpace(string(why))}, true
}
