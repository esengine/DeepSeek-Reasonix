package cli

import "github.com/spf13/pflag"

// runFlags is what `reasonix run` accepts. It is built apart from runAgent so
// the flags written before the verb can be checked against it.
type runFlags struct {
	fs                                *pflag.FlagSet
	model, profileFlag, presetFlag    *string
	metricsPath, trajectoryPath, dir  *string
	ablateFlag, foldIndexFlag, resume *string
	effort, permissionMode            *string
	outputFormat                      *string
	maxSteps                          *int
	showThinking, cont, copySession   *bool
	takeover                          *bool
	autoApprove, yolo                 *bool
	printOnly, eventsJSONL            *bool
	failOnUnverified                  *bool
	additionalDirs, allowedToolValues []string
}

func newRunFlags() *runFlags {
	fs := pflag.NewFlagSet("run", pflag.ContinueOnError)
	fs.SetInterspersed(true)
	f := &runFlags{fs: fs}
	f.model = fs.String("model", "", "provider name (default: config default_model)")
	f.profileFlag = fs.String("profile", "", "deprecated: use --preset (economy|balanced|delivery)")
	f.presetFlag = fs.String("preset", "balanced", "agent execution setting: light | balanced | delivery")
	f.maxSteps = fs.Int("max-steps", 0, "one-off max tool-call rounds (0 = automatic)")
	f.showThinking = fs.Bool("show-thinking", false, "show thinking text instead of the collapsed thinking marker")
	fs.Var(new(debugLogFlag), "debug", "write diagnostic logs (assembly timing, cache state) to stderr")
	fs.Lookup("debug").NoOptDefVal = "true"
	f.metricsPath = fs.String("metrics", "", "write a JSON token/cache/cost summary of the run to this path")
	f.trajectoryPath = fs.String("trajectory", "", "append a timestamped JSONL trajectory of the run's full event stream (tool calls, reasoning, decisions) to this path")
	f.ablateFlag, f.foldIndexFlag = registerArmFlags(fs)
	f.dir = fs.String("dir", "", "change to this directory first (project root); config, sandbox and file tools resolve from here")
	f.cont = registerContinueFlag(fs)
	f.resume = fs.String("resume", "", "resume by session file path, session ID, or machine session ID (takes precedence over --continue)")
	f.takeover = fs.Bool("takeover", false, "with --resume/--continue: accepted for 1.x command lines; a session another Reasonix process holds is still refused")
	f.copySession = fs.Bool("copy", false, "with --resume/--continue: duplicate the session and continue in the copy (escape hatch when the original is held by another Reasonix process)")
	f.effort = fs.String("effort", "", "session reasoning effort override")
	f.permissionMode = fs.String("permission-mode", "", "permission mode: read-only | manual | ask | auto | acceptEdits | dontAsk | bypassPermissions (default: auto where the OS sandbox confines writes and the folder is trusted, otherwise ask)")
	f.autoApprove, f.yolo = registerRunApprovalFlags(fs)
	f.printOnly = fs.BoolP("print", "p", false, "print only the final response")
	f.failOnUnverified = fs.Bool("fail-on-unverified", false, "exit 3 when the model finished but the host could not verify the result (final readiness unmet, or edits and commands refused because the folder is not trusted)")
	f.eventsJSONL = fs.Bool("events-jsonl", false, "emit a redacted structured event stream as JSONL")
	f.outputFormat = fs.String("output-format", "text", "output format: text | json | stream-json")
	fs.StringArrayVar(&f.additionalDirs, "add-dir", nil, "allow tool access to an additional directory (repeatable)")
	fs.StringArrayVar(&f.allowedToolValues, "allowed-tools", nil, "comma or space-separated permission rules to allow")
	fs.StringArrayVar(&f.allowedToolValues, "allowedTools", nil, "alias for --allowed-tools")
	return f
}
