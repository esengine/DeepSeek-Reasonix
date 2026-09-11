package cli

import (
	"os"
	"time"

	"golang.org/x/term"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/telemetry"
	"reasonix/internal/trajectory"
)

// runSinkChain is the assembled event pipeline for one `run` invocation, with
// handles to the decorators the command must finalize after the run.
type runSinkChain struct {
	sink         event.Sink
	resultOutput *runOutputSink
	metrics      *metricsSink
	trajectory   *trajectory.Recorder
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
			renderer = newMarkdownRenderer(termW)
		}
		textSink := agent.NewTextSink(os.Stdout, renderer, termW)
		textSink.SetShowReasoning(showThinking)
		chain.sink = textSink
	}
	if metricsPath != "" {
		chain.metrics = &metricsSink{
			inner:         chain.sink,
			partialPath:   partialMetricsPath(metricsPath),
			snapshotEvery: 2 * time.Second,
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
