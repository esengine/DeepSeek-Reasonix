package cli

import (
	"io"
	"os"
	"time"

	"golang.org/x/term"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/frontend/termrender"
	"reasonix/internal/platform/telemetry"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/trajectory"
)

// runSinkChain is the assembled event pipeline for one `run` invocation, with
// handles to the decorators the command must finalize after the run.
type runSinkChain struct {
	sink         event.Sink
	resultOutput *runOutputSink
	metrics      *metricsSink
	trajectory   *trajectory.Recorder
	// tally is set for a text run that prints no result object.
	tally *runDenialTally
}

// buildRunSink assembles `run`'s sink chain: stdout rendering innermost, then
// metrics accumulation, then trajectory recording, then notifications and the
// telemetry reporter outermost. Markdown post-stream redraw (cursor moves) is
// enabled only on a TTY; piped / captured output keeps the raw stream.
func buildRunSink(format runOutputFormat, printOnly, showThinking bool, metricsPath, trajectoryPath string, cfg *config.Config, reporter *telemetry.Reporter) (runSinkChain, error) {
	var chain runSinkChain
	if printOnly || format != runOutputText {
		chain.resultOutput = newRunOutputSink(os.Stdout, format)
		chain.sink = chain.resultOutput
	} else {
		var renderer agent.Renderer
		termW := 80
		if isTTY(os.Stdout) {
			if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
				termW = w
			}
			renderer = termrender.NewMarkdownRenderer(termW)
		}
		textSink := agent.NewTextSink(os.Stdout, renderer, termW)
		textSink.SetShowReasoning(showThinking)
		chain.tally = newRunDenialTally(textSink)
		chain.sink = chain.tally
	}
	if metricsPath != "" {
		chain.metrics = &metricsSink{
			AuditForwarder: event.AuditForwarder{Inner: chain.sink},
			inner:          chain.sink,
			partialPath:    partialMetricsPath(metricsPath),
			snapshotEvery:  2 * time.Second,
		}
		chain.sink = chain.metrics
	}
	if trajectoryPath != "" {
		rec, err := trajectory.New(chain.sink, trajectoryPath, nil)
		if err != nil {
			return runSinkChain{}, err
		}
		chain.trajectory = rec
		chain.sink = rec
	}
	chain.sink = withNotifications(chain.sink, cfg)
	chain.sink = reporter.Wrap(chain.sink)
	return chain, nil
}

// begin marks the controller bound and the prompt about to be submitted.
func (c runSinkChain) begin(ctrl *control.Controller, version, prompt string) {
	recordTrajectoryHeader(c.trajectory, ctrl, version)
	if c.resultOutput != nil {
		c.resultOutput.BeginTurn(sessionstore.BranchID(ctrl.SessionPath()), prompt)
	}
}

// recordTrajectoryHeader persists the request-side prefix the event stream
// cannot carry. The hashes come from the same capture the cache diagnostics
// report, so a reader can prove the header belongs to the rounds it precedes
// instead of assuming the run it was found next to.
func recordTrajectoryHeader(rec *trajectory.Recorder, ctrl *control.Controller, version string) {
	if rec == nil || ctrl == nil {
		return
	}
	schemas := ctrl.ToolSchemas()
	prompt := ctrl.SystemPrompt()
	shape := agent.CaptureShape(prompt, schemas, 0)
	build := BuildInfo{Version: version}.withDefaults()
	caps, capsHash := capabilityVersions(build.Version+"@"+build.GitCommit, shape.SystemHash, schemas, ctrl.Skills())
	rec.RecordRunHeader(trajectory.RunHeader{
		ModelRef:      ctrl.ModelRef(),
		WorkspaceRoot: ctrl.WorkspaceRoot(),
		System:        prompt,
		SystemHash:    shape.SystemHash,
		Tools:         agent.NormalizedToolSchemas(schemas),
		ToolsHash:     shape.ToolsHash,
		PrefixHash:    shape.PrefixHash,

		Capabilities:     caps,
		CapabilitiesHash: capsHash,
	})
}

// settlePosture opens the run on its approval mode and tells the sinks which
// folder a refusal is about. With no mode named the build's default decides.
func (c runSinkChain) settlePosture(ctrl *control.Controller, defaulted bool, named string) string {
	mode := named
	if defaulted {
		mode = ctrl.ApplyDefaultHeadlessApprovalMode()
	} else {
		ctrl.ApplyHeadlessApprovalMode(named)
	}
	c.resultOutput.SetPermissionMode(mode)
	note := newFolderNote(ctrl)
	c.resultOutput.setFolderNote(note)
	if c.tally != nil {
		c.tally.setNote(note)
	}
	return mode
}

// refusedByFolderTrust reports whether an edit or command was refused because
// the folder is untrusted. A text run with no result object has not yet said
// what it was refused, so it says so on w now.
func (c runSinkChain) refusedByFolderTrust(w io.Writer) bool {
	if c.tally == nil {
		return c.resultOutput.refusedByFolderTrust()
	}
	denials := c.tally.snapshot()
	writeDenialWarning(w, denials)
	return refusedByFolderTrust(denials)
}
