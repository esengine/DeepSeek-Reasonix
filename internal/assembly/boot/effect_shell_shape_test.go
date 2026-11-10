package boot

import (
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

// The host used to refuse commands by their shape — a ';' or a pipe behind a
// declared check, a mutation beside a check, a trailing echo of $? — and tell the
// model to rewrite them, spending a round and skipping the rest of the batch.
// They run now; the command's own exit status is all that speaks for it.
func TestEffectShellShapesThatUsedToBeRefusedNowRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the commands below are POSIX shell")
	}
	cases := map[string]provider.ToolCall{
		"declared check behind a ';'":      {ID: "c1", Name: "bash", Arguments: `{"command":"echo first; echo second","verifies":"it prints"}`},
		"declared check through a pipe":    {ID: "c2", Name: "bash", Arguments: `{"command":"echo piped | cat","verifies":"it prints"}`},
		"mutation beside a check":          {ID: "c3", Name: "bash", Arguments: `{"command":"mkdir -p built && ls built && echo done"}`},
		"trailing echo of the exit status": {ID: "c4", Name: "bash", Arguments: `{"command":"true; echo $?"}`},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			run := buildPostureRun(t, "[sandbox]\nbash = \"off\"\n", call)
			run.runIn(t, control.ToolApprovalYolo)
			saw := run.modelSaw(t, call.ID)
			if strings.Contains(saw, "blocked") || strings.Contains(saw, "declares a check") {
				t.Fatalf("the command was refused for its shape: %q", saw)
			}
			if run.results[call.ID].Output == "" {
				t.Fatalf("nothing ran: %+v", run.results[call.ID])
			}
		})
	}
}
