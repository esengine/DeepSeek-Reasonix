//go:build windows

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"

	"reasonix/internal/base/testenv"
)

// The token the suite runs under decides what these measure: elevated means the
// backup, restore and take-ownership privileges are present to be stripped.
func TestHardeningSuiteSaysWhichTokenItRanUnder(t *testing.T) {
	t.Logf("elevated token: %v", windows.GetCurrentProcessToken().IsElevated())
}

// Backup semantics bypass the DACL for a token that holds the backup privilege;
// the launcher removes every privilege, so a /B copy cannot land outside a root.
func TestBackupSemanticsCannotWriteOutsideTheRoot(t *testing.T) {
	h := newWinHarness(t)
	src := filepath.Join(h.ws, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "payload.txt"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(h.out, "copy")
	h.runArgv([]string{"robocopy", src, dst, "payload.txt", "/B", "/R:0", "/W:0"})
	if exists(filepath.Join(dst, "payload.txt")) || exists(dst) {
		t.Fatalf("a backup-semantics copy wrote outside the root (elevated: %v)", windows.GetCurrentProcessToken().IsElevated())
	}
	// A plain copy inside the root lands, so the refusal above is the bound's and
	// not a broken command line.
	in := filepath.Join(h.ws, "copy")
	h.runArgv([]string{"robocopy", src, in, "payload.txt", "/R:0", "/W:0"})
	if !exists(filepath.Join(in, "payload.txt")) {
		t.Error("a copy inside the root did not land")
	}
}

// A request the host did not make: a sandboxed command runs the launcher with a
// Grant naming a folder outside the roots. It must fail, and the folder must not
// gain the SID.
func TestASandboxedCommandCannotGrantItselfAFolderThroughTheLauncher(t *testing.T) {
	h := newWinHarness(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	forged := winLaunchArgv(self, winLaunch{SID: h.book.SID(), Grant: []string{h.out}}, []string{"cmd", "/c", "echo x> " + filepath.Join(h.out, "forged.txt")})
	h.runArgv(forged)
	if ok, _ := WindowsGrantOps().Granted(h.out, h.book.SID()); ok {
		t.Error("the forged request granted the SID on a folder outside the roots")
	}
	if exists(filepath.Join(h.out, "forged.txt")) {
		t.Error("the forged request ran a command that wrote outside the roots")
	}
	// The refusal is the token's: inside the sandbox the launcher says why.
	out, code := h.runArgv(forged)
	if code != 126 || !strings.Contains(out, "restricted process") {
		t.Errorf("the launcher in the sandbox answered %d %q, want a refusal for its restricted token", code, out)
	}
}

// What a child creates without an inherited ACL takes the token's default DACL;
// it has to be openable by the child itself.
func TestAChildCanOpenWhatItCreatedUnderTheNarrowedDefaultDACL(t *testing.T) {
	h := newWinHarness(t)
	script := `$e = New-Object Threading.EventWaitHandle($false,'ManualReset','Local\rxprobe-` + strconv.Itoa(os.Getpid()) + `'); [Threading.EventWaitHandle]::OpenExisting('Local\rxprobe-` + strconv.Itoa(os.Getpid()) + `') | Out-Null; 'opened'`
	out, code := h.runArgv([]string{"powershell", "-NoProfile", "-NonInteractive", "-Command", script})
	if code != 0 || !strings.Contains(out, "opened") {
		t.Fatalf("the child could not open its own event (exit %d): %s", code, out)
	}
}

// An inheritable handle in the launcher must not reach the child: an inherited
// handle is not checked against the token.
func TestOnlyTheStandardHandlesReachTheChild(t *testing.T) {
	h := newWinHarness(t)
	f, err := os.Create(filepath.Join(h.out, "held.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := windows.SetHandleInformation(windows.Handle(f.Fd()), windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	argv, wrapped := CommandArgs(h.spec, []string{self, "-test.run=TestProbeInheritedHandle"})
	if !wrapped {
		t.Fatal("the command was not wrapped")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "RX_PROBE_HANDLE="+strconv.FormatUint(uint64(f.Fd()), 10))
	cmd.SysProcAttr = &syscall.SysProcAttr{AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(f.Fd())}}
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "handle-absent") {
		t.Fatalf("the extra handle reached the child: %s", out)
	}
}

func TestProbeInheritedHandle(t *testing.T) {
	v := os.Getenv("RX_PROBE_HANDLE")
	if v == "" {
		t.Skip("helper for TestOnlyTheStandardHandlesReachTheChild")
	}
	n, _ := strconv.ParseUint(v, 10, 64)
	if _, err := windows.GetFileType(windows.Handle(n)); err != nil || windows.GetLastError() != nil {
		os.Stdout.WriteString("handle-absent\n")
		return
	}
	os.Stdout.WriteString("handle-present\n")
}

// Granting follows the tree of a root but not a link out of it, and a root that
// is itself a link is refused.
func TestGrantsDoNotFollowLinksOutOfARoot(t *testing.T) {
	base := testenv.TempDir(t)
	ws, out := filepath.Join(base, "ws"), filepath.Join(base, "out")
	for _, d := range []string{ws, out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	victim := filepath.Join(out, "victim.txt")
	if err := os.WriteFile(victim, []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(ws, "jn")
	if o, err := exec.Command("cmd", "/c", "mklink", "/J", junction, out).CombinedOutput(); err != nil {
		t.Skipf("cannot make a junction here: %v %s", err, o)
	}
	symlink := filepath.Join(ws, "sl")
	hasSymlink := os.Symlink(out, symlink) == nil
	book, err := OpenGrantBook(filepath.Join(base, "state"), WindowsGrantOps())
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Ensure(ws); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = book.Revoke(ws) })
	for _, p := range []string{out, victim} {
		if ok, _ := WindowsGrantOps().Granted(p, book.SID()); ok {
			t.Errorf("the grant reached %s through a link", p)
		}
	}
	if !hasSymlink {
		t.Log("no symbolic link could be made here; only the junction was measured")
	}
	if err := book.Ensure(junction); err == nil {
		_ = book.Revoke(junction)
		t.Error("a root that is a junction was granted")
	}
}

// Services a restricted command can ask to act for it. A task or a service made
// that way runs later with the user's own token, so each one that succeeds is a
// line in the "not bounded" list. The outcomes are pinned so the text cannot drift.
func TestWhichSystemServicesARestrictedCommandCanAskToActForIt(t *testing.T) {
	h := newWinHarness(t)
	id := "rxprobe" + strconv.Itoa(os.Getpid())
	t.Cleanup(func() {
		_ = exec.Command("schtasks", "/delete", "/tn", id, "/f").Run()
		_ = exec.Command("sc", "delete", id).Run()
		_ = exec.Command("reg", "delete", `HKLM\Software\`+id, "/f").Run()
	})
	h.run(`schtasks /create /tn ` + id + ` /tr "cmd /c exit" /sc once /st 23:59 /f`)
	taskMade := exec.Command("schtasks", "/query", "/tn", id).Run() == nil
	h.run(`sc create ` + id + ` binPath= "cmd /c exit"`)
	serviceMade := exec.Command("sc", "query", id).Run() == nil
	h.run(`reg add HKLM\Software\` + id + ` /v a /d b /f`)
	hklmWritten := exec.Command("reg", "query", `HKLM\Software\`+id).Run() == nil
	t.Logf("elevated=%v scheduledTask=%v service=%v HKLM=%v", windows.GetCurrentProcessToken().IsElevated(), taskMade, serviceMade, hklmWritten)
	if serviceMade || hklmWritten {
		t.Errorf("a restricted command made a service (%v) or wrote HKLM (%v): the guide says neither", serviceMade, hklmWritten)
	}
	if taskMade {
		t.Log("a restricted command can create a scheduled task, which later runs with the user's full token: the setting's description lists it")
	}
}
