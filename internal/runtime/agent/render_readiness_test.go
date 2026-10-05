package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/taskpolicy"
	"reasonix/internal/safety/evidence"
)

func renderWrite(path string) evidence.Receipt {
	args, _ := json.Marshal(map[string]string{"path": path})
	return evidence.Receipt{ToolName: "write_file", Args: args, Success: true, Write: true, Paths: []string{path}}
}

func balancedTurn() turnRuntime {
	return turnRuntime{policySet: true, policy: taskpolicy.TaskPolicy{Verification: taskpolicy.VerifyTargeted}}
}

// A page the turn wrote is owed a look where there is a browser to open it in
// and a model that can see it, and nowhere else.
func TestUnseenRenderGapOwesALookOnlyWhereOneCanBeTaken(t *testing.T) {
	root := testenv.TempDir(t)
	write := renderWrite("pelican.svg")

	seeing := &Agent{task: taskRuntime{ledger: readinessLedger(write)}, turn: balancedTurn(), agentConfig: agentConfig{renderRoot: root}}
	got := seeing.finalReadinessCheckFor()
	if got.missingRender != 1 || !slices.Contains(got.missingIDs(), "render") {
		t.Fatalf("readiness = %+v, want one render gap", got)
	}
	if url := evidence.FileURL(filepath.Join(root, "pelican.svg")); !strings.Contains(got.reason, url) {
		t.Fatalf("reason %q does not tell the model to open %s", got.reason, url)
	}

	blind := &Agent{task: taskRuntime{ledger: readinessLedger(write)}, turn: balancedTurn()}
	if got := blind.finalReadinessCheckFor(); got.missingRender != 0 {
		t.Fatalf("an agent with nowhere to look was owed one: %+v", got)
	}
}

func TestUnseenRenderGapClosesOnALookOrADeclaredBlock(t *testing.T) {
	root := testenv.TempDir(t)
	write := renderWrite("pelican.svg")
	look := evidence.Receipt{ToolName: "browser_read", Success: true, Read: true,
		Viewed: []string{evidence.ViewedPath(evidence.FileURL(filepath.Join(root, "pelican.svg")))}}
	blocked := evidence.Receipt{ToolName: "conclude_blocked", Success: true}

	for name, ledger := range map[string]*evidence.Ledger{
		"looked":  readinessLedger(write, look),
		"blocked": readinessLedger(write, blocked),
	} {
		a := &Agent{task: taskRuntime{ledger: ledger}, turn: balancedTurn(), agentConfig: agentConfig{renderRoot: root}}
		if got := a.finalReadinessCheckFor(); got.missingRender != 0 {
			t.Errorf("%s: still owed a look: %+v", name, got)
		}
	}
}

// Only a screenshot the browser reports as a local file is a look; acting on a
// page, or a screenshot of a site, names no written file.
func TestOnlyALocalScreenshotRecordsAView(t *testing.T) {
	file := evidence.FileURL(filepath.Join(testenv.TempDir(t), "pelican.svg"))
	cases := []struct {
		tool, url string
		want      bool
	}{
		{"browser_read", file, true},
		{"browser_act", file, false},
		{"browser_read", "https://example.com/pelican.svg", false},
	}
	for _, c := range cases {
		rec := evidence.Receipt{ToolName: c.tool}
		decorateExecutionReceipt(&rec, "ok", &tool.ShellExecution{Kind: browserExecutionKind, State: tool.ShellStateCompleted, Subject: c.url})
		if got := len(rec.Viewed) == 1; got != c.want {
			t.Errorf("%s on %s: viewed = %v, want %v", c.tool, c.url, rec.Viewed, c.want)
		}
	}
}

// End to end over a real run: the model writes a page and, in the result of
// that very call, is told which URL to open and that a screenshot settles it.
func TestAWrittenPageTellsTheModelToLookAtIt(t *testing.T) {
	root := testenv.TempDir(t)
	page := filepath.Join(root, "pelican.svg")
	args, _ := json.Marshal(map[string]string{"path": page})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("draw", "write_file", string(args)), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, evidenceRegistry(), sessionstore.NewSession(""), Options{RenderRoot: root}, event.Discard)
	_ = a.Run(context.Background(), "draw a pelican riding a bicycle")
	got := toolResultByID(a.sess.conversation, "draw")
	for _, want := range []string{string(evidence.ObligationUnseenRender), evidence.FileURL(page), "browser_read what=screenshot"} {
		if !strings.Contains(got, want) {
			t.Fatalf("result = %q, want it to carry %q", got, want)
		}
	}

	blind := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("draw", "write_file", string(args)), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	b := New(blind, evidenceRegistry(), sessionstore.NewSession(""), Options{}, event.Discard)
	_ = b.Run(context.Background(), "draw a pelican riding a bicycle")
	if got := toolResultByID(b.sess.conversation, "draw"); strings.Contains(got, string(evidence.ObligationUnseenRender)) {
		t.Fatalf("an agent with nowhere to look was told to look: %q", got)
	}
}

// Drawing a pelican and looking at it is the whole check; the model is not
// sent off to run a command that would only say the file parses.
func TestALookIsTheCheckForATaskThatOnlyDrew(t *testing.T) {
	root := testenv.TempDir(t)
	look := evidence.Receipt{ToolName: "browser_read", Success: true, Read: true,
		Viewed: []string{evidence.ViewedPath(evidence.FileURL(filepath.Join(root, "pelican.svg")))}}
	gapFor := func(receipts ...evidence.Receipt) finalReadinessCheck {
		a := New(&scriptedProvider{name: "p"}, evidenceRegistry(), sessionstore.NewSession(""), Options{RenderRoot: root}, event.Discard)
		a.task.ledger = readinessLedger(receipts...)
		a.turn = balancedTurn()
		return a.finalReadinessCheckFor()
	}
	if got := gapFor(renderWrite("pelican.svg"), look); got.missingVerification != 0 || got.missingRender != 0 {
		t.Fatalf("drawn and seen: %+v, want nothing owed", got)
	}
	if got := gapFor(renderWrite("main.go"), renderWrite("pelican.svg"), look); got.missingVerification == 0 {
		t.Fatalf("code changed too: %+v, want a verification still owed", got)
	}
}
