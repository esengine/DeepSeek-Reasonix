//go:build !windows

package execfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/remote/execfs/fstest"
)

// local runs the script here, the way wsl.exe --exec does: argv is passed
// straight to the program, so no word is ever parsed by a shell on the way.
type local struct{ env []string }

func (l local) Script(ctx context.Context, stdin io.Reader, body string, args ...string) (Result, error) {
	return runArgv(ctx, stdin, l.env, append([]string{"/bin/sh", "-c", body, "sh"}, args...))
}

// viaCommandLine runs the script as a transport that only takes a command
// string does: CommandLine builds it, and a shell parses it before /bin/sh does.
type viaCommandLine struct{}

func (viaCommandLine) Script(ctx context.Context, stdin io.Reader, body string, args ...string) (Result, error) {
	return runArgv(ctx, stdin, nil, []string{"/bin/sh", "-c", CommandLine(body, args...)})
}

func runArgv(ctx context.Context, stdin io.Reader, env []string, argv []string) (Result, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var out, errb bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Env = stdin, &out, &errb, env
	err := cmd.Run()
	res := Result{Stdout: out.Bytes(), Stderr: errb.Bytes()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode, err = ee.ExitCode(), nil
	}
	return res, err
}

func needGNUStat(t *testing.T) {
	t.Helper()
	if err := exec.Command("stat", "-c", "%s", "/").Run(); err != nil {
		t.Skip("needs GNU stat")
	}
}

func TestFileLayerThroughArgvAgainstALocalPOSIXShell(t *testing.T) {
	needGNUStat(t)
	fstest.RunScenario(t, New(local{}), testenv.TempDir(t))
}

func TestFileLayerThroughACommandLineAgainstALocalPOSIXShell(t *testing.T) {
	needGNUStat(t)
	fstest.RunScenario(t, New(viaCommandLine{}), testenv.TempDir(t))
}

func TestAMissingToolIsNamedNotSwallowed(t *testing.T) {
	needGNUStat(t)
	fsys := New(local{env: []string{"PATH=/nonexistent"}})
	_, err := fsys.Stat(context.Background(), "/")
	if !errors.Is(err, ErrToolMissing) || !strings.Contains(err.Error(), "stat") {
		t.Fatalf("Stat without stat = %v, want ErrToolMissing naming it", err)
	}
	if err := fsys.WriteFileAtomic(context.Background(), "/tmp/x", []byte("x"), 0o600); !errors.Is(err, ErrToolMissing) {
		t.Fatalf("write without cat = %v, want ErrToolMissing", err)
	}
}

func TestAFileOverTheTransferLimitIsRefusedByTypeBothWays(t *testing.T) {
	needGNUStat(t)
	old := transferCap
	transferCap = 1024
	t.Cleanup(func() { transferCap = old })
	ctx := context.Background()
	dir := testenv.TempDir(t)
	fsys := New(local{})

	if _, err := fsys.UploadAtomic(ctx, dir+"/up", bytes.NewReader(make([]byte, 1025)), 0o600); !errors.Is(err, ErrTooLarge) {
		t.Errorf("upload over the limit = %v, want ErrTooLarge", err)
	}
	if _, err := fsys.Stat(ctx, dir+"/up"); err == nil {
		t.Error("an upload that was refused left a file behind")
	}
	if err := fsys.WriteFileAtomic(ctx, dir+"/small", make([]byte, 1024), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fsys.WriteFileAtomic(ctx, dir+"/big", make([]byte, 1024), 0o600); err != nil {
		t.Fatal(err)
	}
	transferCap = 100
	var sink bytes.Buffer
	if _, err := fsys.Download(ctx, dir+"/big", &sink); !errors.Is(err, ErrTooLarge) || sink.Len() != 0 {
		t.Errorf("download over the limit = %v with %d bytes written, want ErrTooLarge and none", err, sink.Len())
	}
}

func TestATruncatedUploadIsNotPublished(t *testing.T) {
	needGNUStat(t)
	ctx := context.Background()
	dir := testenv.TempDir(t)
	fsys := New(shortWrite{local{}})
	if err := fsys.WriteFileAtomic(ctx, dir+"/f", []byte("complete"), 0o600); err == nil {
		t.Fatal("a write whose bytes did not all arrive was published")
	}
	if _, err := fsys.Stat(ctx, dir+"/f"); err == nil {
		t.Fatal("the truncated file is in place")
	}
}

// shortWrite drops the tail of stdin, as a connection cut mid-transfer would
// while still ending the stream cleanly.
type shortWrite struct{ local }

func (s shortWrite) Script(ctx context.Context, stdin io.Reader, body string, args ...string) (Result, error) {
	if stdin == nil {
		return s.local.Script(ctx, nil, body, args...)
	}
	data, _ := io.ReadAll(stdin)
	return s.local.Script(ctx, bytes.NewReader(data[:len(data)/2]), body, args...)
}

func TestAWriteWillNotFollowALinkPlantedAtTheTempName(t *testing.T) {
	needGNUStat(t)
	dir := testenv.TempDir(t)
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := tempNonce
	tempNonce = func() string { return "fixed" }
	defer func() { tempNonce = old }()
	target := filepath.Join(dir, "out")
	for name, link := range map[string]string{"existing": victim, "dangling": filepath.Join(dir, "nowhere")} {
		tmp := filepath.Join(dir, ".out.reasonix-tmp-fixed")
		if err := os.Symlink(link, tmp); err != nil {
			t.Fatal(err)
		}
		err := New(local{}).WriteFileAtomic(context.Background(), target, []byte("evil"), 0o600)
		if err == nil {
			t.Fatalf("%s: write through a planted link succeeded", name)
		}
		if b, _ := os.ReadFile(victim); string(b) != "keep" {
			t.Fatalf("%s: the link's target was written: %q", name, b)
		}
		if _, err := os.Lstat(link); name == "dangling" && err == nil {
			t.Fatalf("dangling target was created")
		}
		if _, err := os.Stat(target); err == nil {
			t.Fatalf("%s: target was published", name)
		}
		os.Remove(tmp)
	}
}
