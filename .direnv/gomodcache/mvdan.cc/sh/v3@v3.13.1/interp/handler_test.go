// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

func blocklistOneExec(name string) func(interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return func(ctx context.Context, args []string) error {
			if args[0] == name {
				return fmt.Errorf("%s: blocklisted program", name)
			}
			return next(ctx, args)
		}
	}
}

func blocklistAllExec(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		return fmt.Errorf("blocklisted: %s", args[0])
	}
}

func blocklistNondevOpen(ctx context.Context, path string, flags int, mode os.FileMode) (io.ReadWriteCloser, error) {
	if path != "/dev/null" {
		return nil, fmt.Errorf("non-dev: %s", path)
	}

	return interp.DefaultOpenHandler()(ctx, path, flags, mode)
}

func mockFileOpen(ctx context.Context, path string, flags int, mode os.FileMode) (io.ReadWriteCloser, error) {
	return nopWriterCloser{strings.NewReader(fmt.Sprintf("body of %s", path))}, nil
}

func blocklistGlob(ctx context.Context, path string) ([]fs.FileInfo, error) {
	return nil, fmt.Errorf("blocklisted: glob")
}

func execPrint(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		hc := interp.HandlerCtx(ctx)
		fmt.Fprintf(hc.Stdout, "would run: %s\n", args)
		return nil
	}
}

func execExitStatus5(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		return interp.ExitStatus(5)
	}
}

func execCustomError(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		return fmt.Errorf("custom error")
	}
}

func execCustomExitStatus5(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		return fmt.Errorf("custom error: %w", interp.ExitStatus(5))
	}
}

func execDotRunnerBuiltin(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		if name, ok := strings.CutPrefix(args[0], "."); ok {
			hc := interp.HandlerCtx(ctx)
			args[0] = name
			err := hc.Builtin(ctx, args)
			fmt.Fprintf(hc.Stdout, "ran builtin: %s\n", args)
			return err
		}
		return next(ctx, args)
	}
}

// runnerCtx allows us to give handler functions access to the Runner, if needed.
var runnerCtx = new(int)

func execPrintWouldExec(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		runner, ok := ctx.Value(runnerCtx).(*interp.Runner)
		if ok && runner.Exited() {
			return fmt.Errorf("would exec via builtin: %s", args)
		}
		return nil
	}
}

// TODO: join with TestRunnerOpts?
var modCases = []struct {
	name string
	opts []interp.RunnerOption
	src  string
	want string
}{
	{
		name: "ExecBlocklistOne",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(blocklistOneExec("sleep")),
		},
		src:  "echo foo; sleep 1",
		want: "foo\nRunner.Run error: sleep: blocklisted program",
	},
	{
		name: "ExecBlocklistOneSubshell",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(
				blocklistOneExec("faa"),
				testExecHandler, // sed is used below
			),
		},
		src:  "a=$(echo foo | sed 's/o/a/g'); echo $a; $a args",
		want: "faa\nRunner.Run error: faa: blocklisted program",
	},
	{
		name: "ExecBlocklistAllSubshell",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(blocklistAllExec),
		},
		src:  "(malicious)",
		want: "Runner.Run error: blocklisted: malicious",
	},
	{
		name: "ExecPipe",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(blocklistAllExec),
		},
		src:  "malicious | echo foo",
		want: "foo\nRunner.Run error: blocklisted: malicious",
	},
	{
		name: "ExecPipeFail",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(blocklistAllExec),
		},
		src:  "set -o pipefail; malicious | echo foo",
		want: "foo\nRunner.Run error: blocklisted: malicious",
	},
	{
		name: "ExecCmdSubst",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(blocklistAllExec),
		},
		src:  "a=$(malicious)",
		want: "blocklisted: malicious\nRunner.Run error: blocklisted: malicious",
	},
	{
		name: "ExecBackground",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(blocklistAllExec),
		},
		src: "{ malicious; true; } & { malicious; true; } & wait",
		// Note that "wait" with no arguments always succeeds.
		want: "",
	},
	{
		name: "ExecPrintWouldExec",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execPrintWouldExec),
		},
		src:  "exec /bin/sh",
		want: "Runner.Run error: would exec via builtin: [/bin/sh]",
	},
	{
		name: "ExecPrintAndBlocklist",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(
				execPrint,
				blocklistOneExec("foo"),
			),
		},
		src:  "foo",
		want: "would run: [foo]\n",
	},
	{
		name: "ExecPrintAndBlocklistSeparate",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execPrint),
			interp.ExecHandlers(blocklistOneExec("foo")),
		},
		src:  "foo",
		want: "would run: [foo]\n",
	},
	{
		name: "ExecBlocklistAndPrint",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(
				blocklistOneExec("foo"),
				execPrint,
			),
		},
		src:  "foo",
		want: "Runner.Run error: foo: blocklisted program",
	},
	{
		name: "ExecExitStatus5",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execExitStatus5),
		},
		src:  "foo",
		want: "Runner.Run error: exit status 5",
	},
	{
		name: "ExecCustomError",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execCustomError),
		},
		src:  "foo",
		want: "Runner.Run error: custom error",
	},
	{
		name: "ExecCustomErrorContinuation",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execCustomError),
		},
		src:  "foo; echo next",
		want: "Runner.Run error: custom error",
	},
	{
		name: "ExecCustomExitStatus5",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execCustomExitStatus5),
		},
		src:  "foo",
		want: "Runner.Run error: custom error: exit status 5",
	},
	{
		name: "ExecCustomExitStatus5Continuation",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execCustomExitStatus5),
		},
		src:  "foo; echo next",
		want: "next\n",
	},
	{
		name: "ExecCustomExitStatus5Pipefail",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execCustomExitStatus5),
		},
		src:  "set -o pipefail; foo | true",
		want: "Runner.Run error: custom error: exit status 5",
	},
	{
		name: "ExecCustomExitStatus5ErrExit",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execCustomExitStatus5),
		},
		src:  "set -o errexit; foo",
		want: "Runner.Run error: custom error: exit status 5",
	},
	{
		name: "ExecCustomExitStatus5Exec",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execCustomExitStatus5),
		},
		src:  "exec foo; echo never-run",
		want: "Runner.Run error: custom error: exit status 5",
	},
	{
		name: "ExecCustomExitStatus5Negated",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execCustomExitStatus5),
		},
		src:  "! foo; echo custom-error-wiped; exit 1",
		want: "custom-error-wiped\nRunner.Run error: exit status 1",
	},
	{
		name: "ExecCustomExitStatus5CmdSubst",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execCustomExitStatus5),
		},
		src:  "x=$(foo)",
		want: "Runner.Run error: custom error: exit status 5",
	},
	{
		name: "ExecCustomExitStatus5Subshell",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execCustomExitStatus5),
		},
		src:  "(foo)",
		want: "Runner.Run error: custom error: exit status 5",
	},
	{
		name: "ExecCustomExitStatus5Wait",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execCustomExitStatus5),
		},
		src:  "foo & bg=$!; wait $bg",
		want: "Runner.Run error: custom error: exit status 5",
	},
	{
		name: "ExecCustomExitStatus5Exit",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execCustomExitStatus5),
		},
		src:  "foo; exit",
		want: "Runner.Run error: custom error: exit status 5",
	},
	{
		name: "ExecCustomExitStatus5Source",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execCustomExitStatus5),
		},
		src:  "echo 'foo' >a; source ./a",
		want: "Runner.Run error: custom error: exit status 5",
	},
	{
		name: "ExecDotRunnerBuiltin",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execDotRunnerBuiltin, execExitStatus5),
		},
		src:  ".true; foo; echo $?; .false",
		want: "ran builtin: [true]\n5\nran builtin: [false]\nRunner.Run error: exit status 1",
	},
	{
		name: "ExecDotRunnerBuiltinExiting",
		opts: []interp.RunnerOption{
			interp.ExecHandlers(execDotRunnerBuiltin, execExitStatus5),
		},
		src:  "echo before; .exit 0; echo after",
		want: "before\nran builtin: [exit 0]\n",
	},
	{
		name: "NonExecBuiltin",
		opts: []interp.RunnerOption{
			interp.CallHandler(func(ctx context.Context, args []string) ([]string, error) {
				hc := interp.HandlerCtx(ctx)
				err := hc.Builtin(ctx, append([]string{"echo"}, args...))
				return nil, err
			}),
		},
		src:  "foo; bar",
		want: "Runner.Run error: HandlerContext.Builtin can only be called via an ExecHandlerFunc",
	},
	{
		name: "OpenForbidNonDev",
		opts: []interp.RunnerOption{
			interp.OpenHandler(blocklistNondevOpen),
		},
		src:  "echo foo >/dev/null; echo bar >/tmp/x",
		want: "Runner.Run error: non-dev: /tmp/x",
	},
	{
		name: "OpenMockFile",
		opts: []interp.RunnerOption{
			interp.OpenHandler(mockFileOpen),
		},
		src:  "echo $(<foo); echo $(< <(echo bar))",
		want: "body of foo\nbar\n",
	},
	{
		name: "CallReplaceWithBlank",
		opts: []interp.RunnerOption{
			interp.OpenHandler(blocklistNondevOpen),
			interp.CallHandler(func(ctx context.Context, args []string) ([]string, error) {
				return []string{"echo", "blank"}, nil
			}),
		},
		src:  "echo foo >/dev/null; { bar; } && baz",
		want: "blank\nblank\n",
	},
	{
		name: "CallDryRun",
		opts: []interp.RunnerOption{
			interp.CallHandler(func(ctx context.Context, args []string) ([]string, error) {
				return append([]string{"echo", "run:"}, args...), nil
			}),
		},
		src:  "cd some-dir; cat foo; exit 1",
		want: "run: cd some-dir\nrun: cat foo\nrun: exit 1\n",
	},
	{
		name: "CallError",
		opts: []interp.RunnerOption{
			interp.CallHandler(func(ctx context.Context, args []string) ([]string, error) {
				if args[0] == "echo" && len(args) > 2 {
					return nil, fmt.Errorf("refusing to run echo builtin with multiple args")
				}
				return args, nil
			}),
		},
		src:  "echo foo; echo foo bar",
		want: "foo\nRunner.Run error: refusing to run echo builtin with multiple args",
	},
	{
		name: "GlobForbid",
		opts: []interp.RunnerOption{
			interp.ReadDirHandler(blocklistGlob),
		},
		src:  "echo *",
		want: "blocklisted: glob\n",
	},
}

func TestRunnerHandlers(t *testing.T) {
	t.Parallel()

	p := syntax.NewParser()
	for _, tc := range modCases {
		t.Run(tc.name, func(t *testing.T) {
			skipIfUnsupported(t, tc.src)
			file := parse(t, p, tc.src)
			tdir := t.TempDir()
			var cb concBuffer
			r, err := interp.New(interp.Dir(tdir), interp.StdIO(nil, &cb, &cb))
			if err != nil {
				t.Fatal(err)
			}
			for _, opt := range tc.opts {
				opt(r)
			}
			ctx := context.WithValue(context.Background(), runnerCtx, r)
			if err := r.Run(ctx, file); err != nil {
				fmt.Fprintf(&cb, "Runner.Run error: %v", err)
			}
			got := cb.String()
			if got != tc.want {
				t.Fatalf("want:\n%q\ngot:\n%q", tc.want, got)
			}
		})
	}
}

type readyBuffer struct {
	buf       bytes.Buffer
	seenReady sync.WaitGroup
}

func (b *readyBuffer) Write(p []byte) (n int, err error) {
	if string(p) == "ready\n" {
		b.seenReady.Done()
		return len(p), nil
	}
	return b.buf.Write(p)
}

func TestKillTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("sleeps and timeouts are slow")
	}
	if runtime.GOOS == "windows" {
		t.Skip("skipping trap tests on windows")
	}
	t.Parallel()

	tests := []struct {
		src         string
		want        string
		killTimeout time.Duration
		forcedKill  bool
	}{
		// killed immediately
		{
			`sh -c "trap 'echo trapped; exit 0' INT; echo ready; for i in \$(seq 1 100); do sleep 0.01; done"`,
			"",
			-1,
			true,
		},
		// interrupted first, and stops itself in time
		{
			`sh -c "trap 'echo trapped; exit 0' INT; echo ready; for i in \$(seq 1 100); do sleep 0.01; done"`,
			"trapped\n",
			time.Second,
			false,
		},
		// interrupted first, but does not stop itself in time
		{
			`sh -c "trap 'echo trapped; for i in \$(seq 1 100); do sleep 0.01; done' INT; echo ready; for i in \$(seq 1 100); do sleep 0.01; done"`,
			"trapped\n",
			20 * time.Millisecond,
			true,
		},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			file := parse(t, nil, test.src)
			attempt := 0
			for {
				var rbuf readyBuffer
				rbuf.seenReady.Add(1)
				ctx, cancel := context.WithCancel(context.Background())
				r, err := interp.New(
					interp.StdIO(nil, &rbuf, &rbuf),
					interp.ExecHandler(interp.DefaultExecHandler(test.killTimeout)),
				)
				if err != nil {
					t.Fatal(err)
				}
				go func() {
					rbuf.seenReady.Wait()
					cancel()
				}()
				err = r.Run(ctx, file)
				if test.forcedKill {
					if errors.As(err, new(interp.ExitStatus)) || err == nil {
						t.Error("command was not force-killed")
					}
				} else {
					if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
						t.Errorf("execution errored: %v", err)
					}
				}
				got := rbuf.buf.String()
				if got != test.want {
					if attempt < 3 && got == "" && test.killTimeout > 0 {
						attempt++
						test.killTimeout *= 2
						continue
					}
					t.Fatalf("want:\n%s\ngot:\n%s", test.want, got)
				}
				break
			}
		})
	}
}

func TestKillSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping signal tests on windows")
	}
	tests := []struct {
		signal os.Signal
		want   error
	}{
		{syscall.SIGINT, interp.ExitStatus(130)},  // 128 + 2
		{syscall.SIGKILL, interp.ExitStatus(137)}, // 128 + 9
		{syscall.SIGTERM, interp.ExitStatus(143)}, // 128 + 15
	}

	// pid_and_hang is implemented in TestMain; we use it to have the
	// interpreter spawn a process, and easily grab its PID to send it a
	// signal directly. The program prints its PID and hangs forever.
	file := parse(t, nil, "GOSH_CMD=pid_and_hang $GOSH_PROG")
	for _, test := range tests {
		t.Run(fmt.Sprintf("signal-%d", test.signal), func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			outReader, outWriter := io.Pipe()
			stderr := new(bytes.Buffer)
			r, _ := interp.New(interp.StdIO(nil, outWriter, stderr))
			errch := make(chan error, 1)
			go func() {
				errch <- r.Run(ctx, file)
				outWriter.Close()
			}()

			br := bufio.NewReader(outReader)
			line, err := br.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil {
				t.Fatal(err)
			}

			proc, err := os.FindProcess(pid)
			if err != nil {
				t.Fatal(err)
			}
			if err := proc.Signal(test.signal); err != nil {
				t.Fatal(err)
			}
			if got := <-errch; got != test.want {
				t.Fatalf("want error %v, got %v. stderr: %s", test.want, got, stderr)
			}
		})
	}
}
