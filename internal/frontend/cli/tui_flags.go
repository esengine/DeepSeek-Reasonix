package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/pflag"

	"reasonix/internal/base/i18n"
)

// tuiFlags are the options the terminal UI takes, which are also the ones a
// bare `reasonix` took in 1.x, so a 1.x command line starts the same session.
type tuiFlags struct {
	fs             *pflag.FlagSet
	model, preset  *string
	profile, dir   *string
	resume, effort *string
	permissionMode *string
	inline, cont   *bool
	yolo, copy     *bool
	maxSteps       *int
	addDirs        []string
	allowedValues  []string
}

func newTUIFlags() *tuiFlags {
	fs := pflag.NewFlagSet("tui", pflag.ContinueOnError)
	f := &tuiFlags{fs: fs}
	f.model = fs.String("model", "", "provider name (default: config default_model)")
	f.preset = fs.String("preset", "balanced", "agent execution setting: light | balanced | delivery")
	f.profile = fs.String("profile", "", "deprecated: use --preset")
	f.dir = fs.String("dir", "", "change to this directory first (project root)")
	f.inline = fs.Bool("inline", false, "write the conversation into the terminal's scrollback instead of taking the full screen")
	f.cont = registerContinueFlag(fs)
	f.resume = fs.StringP("resume", "r", "", "resume by session file path, session ID, or machine session ID; bare -r picks one (takes precedence over --continue)")
	fs.Lookup("resume").NoOptDefVal = resumePickerSentinel
	f.copy = fs.Bool("copy", false, "with --resume/--continue: duplicate the session and continue in the copy")
	f.effort = fs.String("effort", "", "session reasoning effort override")
	f.permissionMode = fs.String("permission-mode", "", "permission mode: read-only | manual | ask | auto | acceptEdits | dontAsk | plan | bypassPermissions (default: auto where the OS sandbox confines writes and the folder is trusted, otherwise ask)")
	f.yolo = fs.Bool("yolo", false, "skip approval prompts; the sandbox, network policy and deny rules still apply (alias for --permission-mode bypassPermissions)")
	fs.BoolVar(f.yolo, "dangerously-skip-permissions", false, "alias for --yolo")
	f.maxSteps = fs.Int("max-steps", 0, "one-off max tool-call rounds (0 = automatic)")
	fs.StringArrayVar(&f.addDirs, "add-dir", nil, "allow tool access to an additional directory (repeatable)")
	fs.StringArrayVar(&f.allowedValues, "allowed-tools", nil, "comma or space-separated permission rules to allow")
	fs.StringArrayVar(&f.allowedValues, "allowedTools", nil, "alias for --allowed-tools")
	return f
}

// runtimeProfile is the execution setting, from --preset or the retired
// --profile spelling.
func (f *tuiFlags) runtimeProfile() (string, error) {
	raw := strings.TrimSpace(*f.profile)
	if raw != "" {
		fmt.Fprintln(os.Stderr, "warning: --profile is deprecated; use --preset light|balanced|delivery")
	} else {
		raw = strings.TrimSpace(*f.preset)
	}
	return parseRuntimeProfile(raw)
}

// permissions is the approval posture the flags ask for; set is false when
// none was named, so the configured default stands.
func (f *tuiFlags) permissions() (mode cliPermissionMode, allowed []string, set bool, err error) {
	value := *f.permissionMode
	if *f.yolo {
		if f.fs.Changed("permission-mode") {
			return mode, nil, false, errors.New("--yolo cannot be combined with --permission-mode")
		}
		value = "bypassPermissions"
	}
	if mode, err = parsePermissionMode(value); err != nil {
		return mode, nil, false, err
	}
	if allowed, err = splitAllowedToolRules(f.allowedValues); err != nil {
		return mode, nil, false, err
	}
	return mode, uniqueStrings(append(allowed, mode.allow...)), strings.TrimSpace(value) != "", nil
}

// effortOverride is the --effort value, or nil when it was not given.
func (f *tuiFlags) effortOverride() *string {
	if strings.TrimSpace(*f.effort) == "" {
		return nil
	}
	return f.effort
}

func tuiUsageError(err error) int {
	fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
	return 2
}
