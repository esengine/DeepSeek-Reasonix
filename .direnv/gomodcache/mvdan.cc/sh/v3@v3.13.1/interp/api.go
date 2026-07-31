// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package interp implements an interpreter to execute shell programs
// parsed by the [syntax] package as either [syntax.LangBash]
// or [syntax.LangPOSIX], behaving like Bash as a result.
//
// The interpreter currently aims to behave like a non-interactive shell,
// which is how most shells run scripts, and is more useful to machines.
// In the future, it may gain an option to behave like an interactive shell.
package interp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// A Runner interprets shell programs. It can be reused, but it is not safe for
// concurrent use. Use [New] to build a new Runner.
//
// Note that writes to Stdout and Stderr may be concurrent if background
// commands are used. If you plan on using an [io.Writer] implementation that
// isn't safe for concurrent use, consider a workaround like hiding writes
// behind a mutex.
//
// Runner's exported fields are meant to be configured via [RunnerOption];
// once a Runner has been created, the fields should be treated as read-only.
type Runner struct {
	// Env specifies the initial environment for the interpreter, which must
	// not be nil. It can only be set via [Env].
	//
	// If it includes a TMPDIR variable describing an absolute directory,
	// it is used as the directory in which to create temporary files needed
	// for the interpreter's use, such as named pipes for process substitutions.
	// Otherwise, [os.TempDir] is used.
	Env expand.Environ

	// writeEnv overlays [Runner.Env] so that we can write environment variables
	// as an overlay.
	writeEnv expand.WriteEnviron

	// Dir specifies the working directory of the command, which must be an
	// absolute path. It can only be set via [Dir].
	Dir string

	// tempDir is either $TMPDIR from [Runner.Env], or [os.TempDir].
	tempDir string

	// Params are the current shell parameters, e.g. from running a shell
	// file or calling a function. Accessible via the $@/$* family of vars.
	// It can only be set via [Params].
	Params []string

	// Separate maps - note that bash allows a name to be both a var and a
	// func simultaneously.
	// Vars is mostly superseded by Env at this point.
	// TODO(v4): remove these

	Vars  map[string]expand.Variable
	Funcs map[string]*syntax.Stmt

	alias map[string]alias

	// callHandler is a function allowing to replace a simple command's
	// arguments. It may be nil.
	callHandler CallHandlerFunc

	// execHandler is responsible for executing programs. It must not be nil.
	execHandler ExecHandlerFunc

	// execMiddlewares grows with calls to [ExecHandlers],
	// and is used to construct execHandler when Reset is first called.
	// The slice is needed to preserve the relative order of middlewares.
	execMiddlewares []func(ExecHandlerFunc) ExecHandlerFunc

	// openHandler is a function responsible for opening files. It must not be nil.
	openHandler OpenHandlerFunc

	// readDirHandler is a function responsible for reading directories during
	// glob expansion. It must be non-nil.
	readDirHandler ReadDirHandlerFunc2

	// statHandler is a function responsible for getting file stat. It must be non-nil.
	statHandler StatHandlerFunc

	stdin  *os.File // e.g. the read end of a pipe
	stdout io.Writer
	stderr io.Writer

	ecfg *expand.Config
	ectx context.Context // just so that Runner.Subshell can use it again

	// didReset remembers whether the runner has ever been reset. This is
	// used so that Reset is automatically called when running any program
	// or node for the first time on a Runner.
	didReset bool

	usedNew bool

	filename string // only if Node was a File

	// >0 to break or continue out of N enclosing loops
	breakEnclosing, contnEnclosing int

	inLoop       bool
	inFunc       bool
	inSource     bool
	handlingTrap bool // whether we're currently in a trap callback

	// track if a sourced script set positional parameters
	sourceSetParams bool

	// noErrExit prevents failing commands from triggering [optErrExit],
	// such as the condition in a [syntax.IfClause].
	noErrExit bool

	// The current and last exit statuses. They can only be different if
	// the interpreter is in the middle of running a statement. In that
	// scenario, 'exit' is the status for the current statement being run,
	// and 'lastExit' corresponds to the previous statement that was run.
	exit     exitStatus
	lastExit exitStatus

	lastExpandExit exitStatus // used to surface exit statuses while expanding fields

	// bgProcs holds all background shells spawned by this runner.
	// Their PIDs are 1-indexed, from 1 to len(bgProcs), with a "g" prefix
	// to distinguish them from real PIDs on the host operating system.
	//
	// Note that each shell only tracks its direct children;
	// subshells do not share nor inherit the background PIDs they can wait for.
	bgProcs []bgProc

	opts runnerOpts

	origDir    string
	origParams []string
	origOpts   runnerOpts
	origStdin  *os.File
	origStdout io.Writer
	origStderr io.Writer

	// Most scripts don't use pushd/popd, so make space for the initial PWD
	// without requiring an extra allocation.
	dirStack     []string
	dirBootstrap [1]string

	optState getopts

	// keepRedirs is used so that "exec" can make any redirections
	// apply to the current shell, and not just the command.
	keepRedirs bool

	// Fake signal callbacks
	callbackErr  string
	callbackExit string
}

// exitStatus holds the state of the shell after running one command.
// Beyond the exit status code, it also holds whether the shell should return or exit,
// as well as any Go error values that should be given back to the user.
//
// TODO(v4): consider replacing ExitStatus with a struct like this,
// so that an [ExecHandlerFunc] can e.g. mimic `exit 0` or fatal errors
// with specific exit codes.
type exitStatus struct {
	// code is the exit status code.
	// When code is zero, err must be nil.
	code uint8

	// TODO: consider an enum, as only one of these should be set at a time
	returning bool // whether the current function `return`ed
	exiting   bool // whether the current shell is exiting
	fatalExit bool // whether the current shell is exiting due to a fatal error; err below must not be nil

	// err holds the error information for a non-zero exit status code or fatal error.
	// Used so that running a single statement with a custom handler
	// which returns a non-fatal Go error, such as a Go error wrapping [NewExitStatus],
	// can be returned by [Runner.Run] without being lost entirely.
	err error
}

// clear sets the exit status code and error to zero, as long as the exit status
// was not set by `return`, `exit`, or a fatal error.
func (e *exitStatus) clear() {
	if e.returning || e.exiting || e.fatalExit {
		return
	}
	e.code = 0
	e.err = nil
}

func (e *exitStatus) ok() bool { return e.code == 0 }

// oneIf sets the exit status code to 1 if b is true.
// Note that it assumes the exit status hasn't been set yet,
// meaning that [exitStatus.code] and [exitStatus.err] are zero values.
func (e *exitStatus) oneIf(b bool) {
	if b {
		e.code = 1
	}
}

func (e *exitStatus) fatal(err error) {
	if e.fatalExit || err == nil {
		return
	}
	e.exiting = true
	e.fatalExit = true
	e.err = err
	if e.code == 0 {
		e.code = 1
	}
}

func (e *exitStatus) fromHandlerError(err error) {
	if err == nil {
		return
	}
	var exit errBuiltinExitStatus
	var es ExitStatus
	if errors.As(err, &exit) {
		*e = exitStatus(exit)
	} else if errors.As(err, &es) {
		e.err = err
		e.code = uint8(es)
	} else {
		e.fatal(err) // handler's custom fatal error
	}
}

type bgProc struct {
	// closed when the background process finishes,
	// after which point the result fields below are set.
	done chan struct{}

	exit *exitStatus
}

type alias struct {
	args  []*syntax.Word
	blank bool
}

// New creates a new Runner, applying a number of options. If applying any of
// the options results in an error, it is returned.
//
// Any unset options fall back to their defaults. For example, not supplying the
// environment falls back to the process's environment, and not supplying the
// standard output writer means that the output will be discarded.
func New(opts ...RunnerOption) (*Runner, error) {
	r := &Runner{
		usedNew:        true,
		openHandler:    DefaultOpenHandler(),
		readDirHandler: DefaultReadDirHandler2(),
		statHandler:    DefaultStatHandler(),
	}
	r.dirStack = r.dirBootstrap[:0]
	// turn "on" the default Bash options
	for i, opt := range bashOptsTable {
		r.opts[len(posixOptsTable)+i] = opt.defaultState
	}

	for _, opt := range opts {
		if err := opt(r); err != nil {
			return nil, err
		}
	}

	// Set the default fallbacks, if necessary.
	if r.Env == nil {
		Env(nil)(r)
	}
	if r.Dir == "" {
		if err := Dir("")(r); err != nil {
			return nil, err
		}
	}
	if r.stdout == nil || r.stderr == nil {
		StdIO(r.stdin, r.stdout, r.stderr)(r)
	}
	return r, nil
}

// RunnerOption can be passed to [New] to alter a [Runner]'s behaviour.
// It can also be applied directly on an existing Runner,
// such as interp.Params("-e")(runner).
// Note that options cannot be applied once Run or Reset have been called.
type RunnerOption func(*Runner) error

// TODO: enforce the rule above via didReset.

// Env sets the interpreter's environment. If nil, a copy of the current
// process's environment is used.
func Env(env expand.Environ) RunnerOption {
	return func(r *Runner) error {
		if env == nil {
			env = expand.ListEnviron(os.Environ()...)
		}
		r.Env = env
		return nil
	}
}

// Dir sets the interpreter's working directory. If empty, the process's current
// directory is used.
func Dir(path string) RunnerOption {
	return func(r *Runner) error {
		if path == "" {
			path, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("could not get current dir: %w", err)
			}
			r.Dir = path
			return nil
		}
		path, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("could not get absolute dir: %w", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("could not stat: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", path)
		}
		r.Dir = path
		return nil
	}
}

// Interactive configures the interpreter to behave like an interactive shell,
// akin to Bash. Currently, this only enables the expansion of aliases,
// but later on it should also change other behavior.
func Interactive(enabled bool) RunnerOption {
	return func(r *Runner) error {
		r.opts[optExpandAliases] = enabled
		return nil
	}
}

// Params populates the shell options and parameters. For example, Params("-e",
// "--", "foo") will set the "-e" option and the parameters ["foo"], and
// Params("+e") will unset the "-e" option and leave the parameters untouched.
//
// This is similar to what the interpreter's "set" builtin does.
func Params(args ...string) RunnerOption {
	return func(r *Runner) error {
		fp := flagParser{remaining: args}
		for fp.more() {
			flag := fp.flag()
			if flag == "-" {
				// TODO: implement "The -x and -v options are turned off."
				if args := fp.args(); len(args) > 0 {
					r.Params = args
				}
				return nil
			}
			enable := flag[0] == '-'
			if flag[1] != 'o' {
				opt := r.posixOptByFlag(flag[1])
				if opt == nil {
					return fmt.Errorf("invalid option: %q", flag)
				}
				*opt = enable
				continue
			}
			value := fp.value()
			if value == "" && enable {
				for i, opt := range &posixOptsTable {
					r.printOptLine(opt.name, r.opts[i], true)
				}
				continue
			}
			if value == "" && !enable {
				for i, opt := range &posixOptsTable {
					setFlag := "+o"
					if r.opts[i] {
						setFlag = "-o"
					}
					r.outf("set %s %s\n", setFlag, opt.name)
				}
				continue
			}
			opt := r.posixOptByName(value)
			if opt == nil {
				return fmt.Errorf("invalid option: %q", value)
			}
			*opt = enable
		}
		if args := fp.args(); args != nil {
			// If "--" wasn't given and there were zero arguments,
			// we don't want to override the current parameters.
			r.Params = args

			// Record whether a sourced script sets the parameters.
			if r.inSource {
				r.sourceSetParams = true
			}
		}
		return nil
	}
}

// CallHandler sets the call handler. See [CallHandlerFunc] for more info.
func CallHandler(f CallHandlerFunc) RunnerOption {
	return func(r *Runner) error {
		r.callHandler = f
		return nil
	}
}

// ExecHandler sets one command execution handler,
// which replaces [DefaultExecHandler](2 * time.Second).
//
// Deprecated: use [ExecHandlers] instead, which allows chaining handlers more easily
// like middleware functions.
func ExecHandler(f ExecHandlerFunc) RunnerOption {
	return func(r *Runner) error {
		r.execHandler = f
		return nil
	}
}

// ExecHandlers appends middlewares to handle command execution.
// The middlewares are chained from first to last, and the first is called by the runner.
// Each middleware is expected to call the "next" middleware at most once.
//
// For example, a middleware may implement only some commands.
// For those commands, it can run its logic and avoid calling "next".
// For any other commands, it can call "next" with the original parameters.
//
// Another common example is a middleware which always calls "next",
// but runs custom logic either before or after that call.
// For instance, a middleware could change the arguments to the "next" call,
// or it could print log lines before or after the call to "next".
//
// The last exec handler is always [DefaultExecHandler](2 * time.Second).
func ExecHandlers(middlewares ...func(next ExecHandlerFunc) ExecHandlerFunc) RunnerOption {
	return func(r *Runner) error {
		r.execMiddlewares = append(r.execMiddlewares, middlewares...)
		return nil
	}
}

// TODO: consider porting the middleware API in [ExecHandlers] to [OpenHandler],
// [ReadDirHandler2], and [StatHandler].

// TODO(v4): now that [ExecHandlers] allows calling a next handler with changed
// arguments, one of the two advantages of [CallHandler] is gone. The other is the
// ability to work with builtins; if we make [ExecHandlers] work with builtins, we
// could join both APIs.

// OpenHandler sets file open handler. See [OpenHandlerFunc] for more info.
func OpenHandler(f OpenHandlerFunc) RunnerOption {
	return func(r *Runner) error {
		r.openHandler = f
		return nil
	}
}

// ReadDirHandler sets the read directory handler. See [ReadDirHandlerFunc] for more info.
//
// Deprecated: use [ReadDirHandler2].
func ReadDirHandler(f ReadDirHandlerFunc) RunnerOption {
	return func(r *Runner) error {
		r.readDirHandler = func(ctx context.Context, path string) ([]fs.DirEntry, error) {
			infos, err := f(ctx, path)
			if err != nil {
				return nil, err
			}
			entries := make([]fs.DirEntry, len(infos))
			for i, info := range infos {
				entries[i] = fs.FileInfoToDirEntry(info)
			}
			return entries, nil
		}
		return nil
	}
}

// ReadDirHandler2 sets the read directory handler. See [ReadDirHandlerFunc2] for more info.
func ReadDirHandler2(f ReadDirHandlerFunc2) RunnerOption {
	return func(r *Runner) error {
		r.readDirHandler = f
		return nil
	}
}

// StatHandler sets the stat handler. See [StatHandlerFunc] for more info.
func StatHandler(f StatHandlerFunc) RunnerOption {
	return func(r *Runner) error {
		r.statHandler = f
		return nil
	}
}

func stdinFile(r io.Reader) (*os.File, error) {
	switch r := r.(type) {
	case *os.File:
		return r, nil
	case nil:
		return nil, nil
	default:
		pr, pw, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		go func() {
			io.Copy(pw, r)
			pw.Close()
		}()
		return pr, nil
	}
}

// StdIO configures an interpreter's standard input, standard output, and
// standard error. If out or err are nil, they default to a writer that discards
// the output.
//
// Note that providing a non-nil standard input other than [*os.File] will require
// an [os.Pipe] and spawning a goroutine to copy into it,
// as an [os.File] is the only way to share a reader with subprocesses.
// This may cause the interpreter to consume the entire reader.
// See [os/exec.Cmd.Stdin].
//
// When providing an [*os.File] as standard input, consider using an [os.Pipe]
// as it has the best chance to support cancellable reads via [os.File.SetReadDeadline],
// so that cancelling the runner's context can stop a blocked standard input read.
func StdIO(in io.Reader, out, err io.Writer) RunnerOption {
	return func(r *Runner) error {
		stdin, _err := stdinFile(in)
		if _err != nil {
			return _err
		}
		r.stdin = stdin
		if out == nil {
			out = io.Discard
		}
		r.stdout = out
		if err == nil {
			err = io.Discard
		}
		r.stderr = err
		return nil
	}
}

func (r *Runner) posixOptByName(name string) *bool {
	for i, opt := range &posixOptsTable {
		if opt.name == name {
			return &r.opts[i]
		}
	}
	return nil
}

func (r *Runner) posixOptByFlag(flag byte) *bool {
	for i, opt := range &posixOptsTable {
		if opt.flag == flag {
			return &r.opts[i]
		}
	}
	return nil
}

func (r *Runner) bashOptByName(name string) (status *bool, supported bool) {
	for i, opt := range bashOptsTable {
		if opt.name == name {
			index := len(posixOptsTable) + i
			return &r.opts[index], opt.supported
		}
	}
	return nil, false
}

// runnerOpts contains all POSIX Shell and Bash options as one contiguous table.
type runnerOpts [len(posixOptsTable) + len(bashOptsTable)]bool

type posixOpt struct {
	flag byte   // one-character flag form for this option; a space if none exists
	name string // full name of the option
}

type bashOpt struct {
	name         string
	defaultState bool // Bash's default value for this option
	supported    bool // whether we support the option's non-default state
}

var posixOptsTable = [...]posixOpt{
	// sorted alphabetically by name
	{'a', "allexport"},
	{'e', "errexit"},
	{'n', "noexec"},
	{'f', "noglob"},
	{'u', "nounset"},
	{'x', "xtrace"},
	{' ', "pipefail"},
}

var bashOptsTable = [...]bashOpt{
	// supported options, sorted alphabetically by name
	{
		name:         "dotglob",
		defaultState: false,
		supported:    true,
	},
	{
		name:         "expand_aliases",
		defaultState: false,
		supported:    true,
	},
	{
		name:         "extglob",
		defaultState: false,
		supported:    true,
	},
	{
		name:         "globstar",
		defaultState: false,
		supported:    true,
	},
	{
		name:         "nocaseglob",
		defaultState: false,
		supported:    true,
	},
	{
		name:         "nullglob",
		defaultState: false,
		supported:    true,
	},
	// unsupported options, sorted alphabetically by name
	{name: "assoc_expand_once"},
	{name: "autocd"},
	{name: "cdable_vars"},
	{name: "cdspell"},
	{name: "checkhash"},
	{name: "checkjobs"},
	{
		name:         "checkwinsize",
		defaultState: true,
	},
	{
		name:         "cmdhist",
		defaultState: true,
	},
	{name: "compat31"},
	{name: "compat32"},
	{name: "compat40"},
	{name: "compat41"},
	{name: "compat42"},
	{name: "compat44"},
	{name: "compat43"},
	{name: "compat44"},
	{
		name:         "complete_fullquote",
		defaultState: true,
	},
	{name: "direxpand"},
	{name: "dirspell"},
	{name: "execfail"},
	{name: "extdebug"},
	{
		name:         "extquote",
		defaultState: true,
	},
	{name: "failglob"},
	{
		name:         "force_fignore",
		defaultState: true,
	},
	{name: "globasciiranges"},
	{name: "gnu_errfmt"},
	{name: "histappend"},
	{name: "histreedit"},
	{name: "histverify"},
	{
		name:         "hostcomplete",
		defaultState: true,
	},
	{name: "huponexit"},
	{
		name:         "inherit_errexit",
		defaultState: true,
	},
	{
		name:         "interactive_comments",
		defaultState: true,
	},
	{name: "lastpipe"},
	{name: "lithist"},
	{name: "localvar_inherit"},
	{name: "localvar_unset"},
	{name: "login_shell"},
	{name: "mailwarn"},
	{name: "no_empty_cmd_completion"},
	{name: "nocasematch"},
	{
		name:         "progcomp",
		defaultState: true,
	},
	{name: "progcomp_alias"},
	{
		name:         "promptvars",
		defaultState: true,
	},
	{name: "restricted_shell"},
	{name: "shift_verbose"},
	{
		name:         "sourcepath",
		defaultState: true,
	},
	{name: "xpg_echo"},
}

// To access the shell options arrays without a linear search when we
// know which option we're after at compile time. First come the shell options,
// then the bash options.
const (
	// These correspond to indexes in [shellOptsTable]
	optAllExport = iota
	optErrExit
	optNoExec
	optNoGlob
	optNoUnset
	optXTrace
	optPipeFail

	// These correspond to indexes (offset by the above seven items) of
	// supported options in [bashOptsTable]
	optDotGlob
	optExpandAliases
	optExtGlob
	optGlobStar
	optNoCaseGlob
	optNullGlob
)

// Reset returns a runner to its initial state, right before the first call to
// Run or Reset.
//
// Typically, this function only needs to be called if a runner is reused to run
// multiple programs non-incrementally. Not calling Reset between each run will
// mean that the shell state will be kept, including variables, options, and the
// current directory.
func (r *Runner) Reset() {
	if !r.usedNew {
		panic("use interp.New to construct a Runner")
	}
	if !r.didReset {
		r.origDir = r.Dir
		r.origParams = r.Params
		r.origOpts = r.opts
		r.origStdin = r.stdin
		r.origStdout = r.stdout
		r.origStderr = r.stderr

		if r.execHandler != nil && len(r.execMiddlewares) > 0 {
			panic("interp.ExecHandler should be replaced with interp.ExecHandlers, not mixed")
		}
		if r.execHandler == nil {
			r.execHandler = DefaultExecHandler(2 * time.Second)
		}
		// Middlewares are chained from first to last, and each can call the
		// next in the chain, so we need to construct the chain backwards.
		for _, mw := range slices.Backward(r.execMiddlewares) {
			r.execHandler = mw(r.execHandler)
		}
		// Fill tempDir; only need to do this once given that Env will not change.
		if dir := r.Env.Get("TMPDIR").String(); filepath.IsAbs(dir) {
			r.tempDir = dir
		} else {
			r.tempDir = os.TempDir()
		}
		// Clean it as we will later do a string prefix match.
		r.tempDir = filepath.Clean(r.tempDir)
	}
	// reset the internal state
	*r = Runner{
		Env:            r.Env,
		tempDir:        r.tempDir,
		callHandler:    r.callHandler,
		execHandler:    r.execHandler,
		openHandler:    r.openHandler,
		readDirHandler: r.readDirHandler,
		statHandler:    r.statHandler,

		// These can be set by functions like [Dir] or [Params], but
		// builtins can overwrite them; reset the fields to whatever the
		// constructor set up.
		Dir:    r.origDir,
		Params: r.origParams,
		opts:   r.origOpts,
		stdin:  r.origStdin,
		stdout: r.origStdout,
		stderr: r.origStderr,

		origDir:    r.origDir,
		origParams: r.origParams,
		origOpts:   r.origOpts,
		origStdin:  r.origStdin,
		origStdout: r.origStdout,
		origStderr: r.origStderr,

		// emptied below, to reuse the space
		Vars: r.Vars,

		dirStack: r.dirStack[:0],
		usedNew:  r.usedNew,
	}
	// Ensure we stop referencing any pointers before we reuse bgProcs.
	clear(r.bgProcs)
	r.bgProcs = r.bgProcs[:0]

	if r.Vars == nil {
		r.Vars = make(map[string]expand.Variable)
	} else {
		clear(r.Vars)
	}
	// TODO(v4): Use the supplied Env directly if it implements enough methods.
	r.writeEnv = &overlayEnviron{parent: r.Env}
	if !r.writeEnv.Get("HOME").IsSet() {
		home, _ := os.UserHomeDir()
		r.setVarString("HOME", home)
	}
	if !r.writeEnv.Get("UID").IsSet() {
		r.setVar("UID", expand.Variable{
			Set:      true,
			Kind:     expand.String,
			ReadOnly: true,
			Str:      strconv.Itoa(os.Getuid()),
		})
	}
	if !r.writeEnv.Get("EUID").IsSet() {
		r.setVar("EUID", expand.Variable{
			Set:      true,
			Kind:     expand.String,
			ReadOnly: true,
			Str:      strconv.Itoa(os.Geteuid()),
		})
	}
	if !r.writeEnv.Get("GID").IsSet() {
		r.setVar("GID", expand.Variable{
			Set:      true,
			Kind:     expand.String,
			ReadOnly: true,
			Str:      strconv.Itoa(os.Getgid()),
		})
	}
	r.setVarString("PWD", r.Dir)
	r.setVarString("IFS", " \t\n")
	r.setVarString("OPTIND", "1")

	r.dirStack = append(r.dirStack, r.Dir)

	r.didReset = true
}

// ExitStatus is a non-zero status code resulting from running a shell node.
type ExitStatus uint8

func (s ExitStatus) Error() string { return fmt.Sprintf("exit status %d", s) }

// NewExitStatus creates an error which contains the specified exit status code.
//
// Deprecated: use [ExitStatus] directly.
//
//go:fix inline
func NewExitStatus(status uint8) error {
	return ExitStatus(status)
}

// IsExitStatus checks whether error contains an exit status and returns it.
//
// Deprecated: use [errors.As] with [ExitStatus] directly.
//
//go:fix inline
func IsExitStatus(err error) (status uint8, ok bool) {
	var es ExitStatus
	if errors.As(err, &es) {
		return uint8(es), true
	}
	return 0, false
}

// Run interprets a node, which can be a [*File], [*Stmt], or [Command]. If a non-nil
// error is returned, it will typically contain a command's exit status, which
// can be retrieved with [IsExitStatus].
//
// Run can be called multiple times synchronously to interpret programs
// incrementally. To reuse a [Runner] without keeping the internal shell state,
// call Reset.
//
// Calling Run on an entire [*File] implies an exit, meaning that an exit trap may
// run.
func (r *Runner) Run(ctx context.Context, node syntax.Node) error {
	if !r.didReset {
		r.Reset()
	}
	r.fillExpandConfig(ctx)
	r.exit = exitStatus{}
	r.filename = ""
	switch node := node.(type) {
	case *syntax.File:
		r.filename = node.Name
		r.stmts(ctx, node.Stmts)
	case *syntax.Stmt:
		r.stmt(ctx, node)
	case syntax.Command:
		r.cmd(ctx, node)
	default:
		return fmt.Errorf("node can only be File, Stmt, or Command: %T", node)
	}
	r.trapCallback(ctx, r.callbackExit, "exit")
	maps.Insert(r.Vars, r.writeEnv.Each)
	// Return the first of: a fatal error, a non-fatal handler error, or the exit code.
	if err := r.exit.err; err != nil {
		if r.exit.code == 0 {
			// This should never happen; too much code relies on checking [exitStatus.code]
			// to see if the last command succeeded or failed. [exitStatus.err] should only be
			// additional information, so fail loudly if the invariant is broken.
			panic("ended up with a non-nil exitStatus.err but a zero exitStatus.code")
		}
		return err
	}
	if code := r.exit.code; code != 0 {
		return ExitStatus(code)
	}
	return nil
}

// Exited reports whether the last Run call should exit an entire shell. This
// can be triggered by the "exit" built-in command, for example.
//
// Note that this state is overwritten at every Run call, so it should be
// checked immediately after each Run call.
func (r *Runner) Exited() bool {
	return r.exit.exiting
}

// Subshell makes a copy of the given [Runner], suitable for use concurrently
// with the original. The copy will have the same environment, including
// variables and functions, but they can all be modified without affecting the
// original.
//
// Subshell is not safe to use concurrently with [Run]. Orchestrating this is
// left up to the caller; no locking is performed.
//
// To replace e.g. stdin/out/err, do [StdIO](r.stdin, r.stdout, r.stderr)(r) on
// the copy.
func (r *Runner) Subshell() *Runner {
	return r.subshell(true)
}

// subshell is like [Runner.subshell], but allows skipping some allocations and copies
// when creating subshells which will not be used concurrently with the parent shell.
// TODO(v4): we should expose this, e.g. SubshellForeground and SubshellBackground.
func (r *Runner) subshell(background bool) *Runner {
	if !r.didReset {
		r.Reset()
	}
	// Keep in sync with the Runner type. Manually copy fields, to not copy
	// sensitive ones like [errgroup.Group], and to do deep copies of slices.
	r2 := &Runner{
		Dir:            r.Dir,
		tempDir:        r.tempDir,
		Params:         r.Params,
		callHandler:    r.callHandler,
		execHandler:    r.execHandler,
		openHandler:    r.openHandler,
		readDirHandler: r.readDirHandler,
		statHandler:    r.statHandler,
		stdin:          r.stdin,
		stdout:         r.stdout,
		stderr:         r.stderr,
		filename:       r.filename,
		opts:           r.opts,
		usedNew:        r.usedNew,
		exit:           r.exit,
		lastExit:       r.lastExit,

		origStdout: r.origStdout, // used for process substitutions
	}
	r2.writeEnv = newOverlayEnviron(r.writeEnv, background)
	// Funcs are copied, since they might be modified.
	r2.Funcs = maps.Clone(r.Funcs)
	r2.Vars = make(map[string]expand.Variable)
	r2.alias = maps.Clone(r.alias)

	r2.dirStack = append(r2.dirBootstrap[:0], r.dirStack...)
	r2.fillExpandConfig(r.ectx)
	r2.didReset = true
	return r2
}
