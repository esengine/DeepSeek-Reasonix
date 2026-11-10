//go:build windows

package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

type winHarness struct {
	t             *testing.T
	book          *GrantBook
	ws, out, temp string
	state         string
	spec          Spec
}

// newWinHarness grants a workspace through the real book and returns a spec
// that runs commands under the real launcher. It needs the interactive desktop
// session: a network logon has no logon SID to restrict with.
func newWinHarness(t *testing.T) *winHarness {
	t.Helper()
	base := testenv.TempDir(t)
	h := &winHarness{t: t, ws: filepath.Join(base, "ws"), out: filepath.Join(base, "outside")}
	for _, d := range []string{h.ws, h.out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A file that exists before the grant: the sandbox must be able to edit it.
	if err := os.WriteFile(filepath.Join(h.ws, "before.txt"), []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A session temp directory as the host makes one: new, under the temp root,
	// outside every write root, granted by the launcher when it starts.
	temp, err := os.MkdirTemp("", sessionTempPrefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temp) })
	h.temp = temp
	h.state = filepath.Join(base, "state")
	book, err := OpenGrantBook(h.state, WindowsGrantOps())
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Ensure(h.ws); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = book.Revoke(h.ws) })
	h.book = book
	h.spec = Spec{Mode: "enforce", WriteRoots: []string{h.ws}, GrantSID: book.SID(), SessionTemp: h.temp}
	return h
}

func (h *winHarness) run(cmdline string) (string, int) {
	h.t.Helper()
	return h.runArgv([]string{"cmd", "/c", cmdline})
}

func (h *winHarness) runArgv(args []string) (string, int) {
	h.t.Helper()
	argv, wrapped := CommandArgs(h.spec, args)
	if !wrapped {
		h.t.Fatal("the command was not wrapped")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	out, err := cmd.CombinedOutput()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		h.t.Fatalf("launcher did not run: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out)), code
}

// profileEnv moves the profile a spec's caches are looked up in to temporary
// directories, so a test never grants a folder of the machine it runs on.
func profileEnv(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{"LOCALAPPDATA": testenv.TempDir(t), "USERPROFILE": testenv.TempDir(t)}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// What the host observes, not exit codes: a denied write leaves no file.
func TestRestrictedCommandsWriteOnlyWhereTheSIDWasGranted(t *testing.T) {
	h := newWinHarness(t)
	existing := filepath.Join(h.out, "existing.txt")
	if err := os.WriteFile(existing, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(h.ws, "jn")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, h.out).CombinedOutput(); err != nil {
		t.Skipf("cannot make a junction here: %v %s", err, out)
	}
	hkcu := `HKCU\Software\rxsandbox-probe`
	cmds := []string{
		`echo ok> ` + filepath.Join(h.ws, "inside.txt"),
		`echo no> ` + filepath.Join(h.out, "outside.txt"),
		`echo no>> ` + existing,
		`mkdir ` + filepath.Join(h.out, "newdir"),
		`echo no> ` + filepath.Join(junction, "via-junction.txt"),
		`reg add ` + hkcu + ` /v a /d b /f`,
		`cmd /c "echo nested> ` + filepath.Join(h.out, "nested.txt") + `"`,
	}
	t.Cleanup(func() { _ = exec.Command("reg", "delete", hkcu, "/f").Run() })
	for _, c := range cmds {
		out, code := h.run(c)
		if b, err := os.ReadFile(existing); err != nil || string(b) != "secret" {
			t.Errorf("after %q (exit %d, %q) the file outside the root is %q (%v)", c, code, out, b, err)
			_ = os.WriteFile(existing, []byte("secret"), 0o644)
		}
	}
	if !exists(filepath.Join(h.ws, "inside.txt")) {
		t.Error("a write inside the granted root did not land")
	}
	for _, p := range []string{"outside.txt", "newdir", "via-junction.txt", "nested.txt"} {
		if exists(filepath.Join(h.out, p)) {
			t.Errorf("a restricted command created %s outside the root", p)
		}
	}
	if b, err := os.ReadFile(existing); err != nil || string(b) != "secret" {
		t.Errorf("a restricted command changed or removed a file outside the root: %q %v", b, err)
	}
	if err := exec.Command("reg", "query", hkcu).Run(); err == nil {
		t.Error("a restricted command wrote to HKCU")
	}
}

// Reads and the network are not claimed, and must keep working.
func TestRestrictedCommandsStillReadOutsideAndReportTheirExitStatus(t *testing.T) {
	h := newWinHarness(t)
	if err := os.WriteFile(filepath.Join(h.out, "readable.txt"), []byte("hello-outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := h.run(`type ` + filepath.Join(h.out, "readable.txt")); code != 0 || !strings.Contains(out, "hello-outside") {
		t.Errorf("read outside: code %d out %q", code, out)
	}
	if _, code := h.run(`exit 7`); code != 7 {
		t.Errorf("exit status = %d, want 7", code)
	}
	if out, _ := h.run(`echo %TEMP%`); !strings.EqualFold(out, h.temp) {
		t.Errorf("TEMP = %q, want the session temp %q", out, h.temp)
	}
}

// The job ends the whole tree when the launcher goes, so a child cannot outlive
// a cancelled command.
func TestCancellingTheLauncherEndsTheTree(t *testing.T) {
	h := newWinHarness(t)
	late := filepath.Join(h.ws, "late.txt")
	argv, _ := CommandArgs(h.spec, []string{"cmd", "/c", `ping -n 7 127.0.0.1 >nul & echo late> ` + late})
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_ = exec.CommandContext(ctx, argv[0], argv[1:]...).Run()
	time.Sleep(8 * time.Second)
	if exists(late) {
		t.Fatal("a child outlived its cancelled launcher and finished its work")
	}
}

// Inside a granted root the tools a developer uses still work under the token.
func TestRestrictedCommandsRunGitAndPowerShellInsideTheRoot(t *testing.T) {
	h := newWinHarness(t)
	if out, code := h.run(`cd /d ` + h.ws + ` && git init -q g && echo gitok`); code != 0 || !strings.Contains(out, "gitok") || !exists(filepath.Join(h.ws, "g", ".git")) {
		t.Errorf("git init: code %d out %q", code, out)
	}
	target := filepath.Join(h.ws, "ps.txt")
	if out, code := h.runArgv([]string{"powershell", "-NoProfile", "-Command", "Set-Content -Path '" + target + "' -Value x; 'psok'"}); code != 0 || !strings.Contains(out, "psok") || !exists(target) {
		t.Errorf("powershell write: code %d out %q", code, out)
	}
}

// GrantSpec prepares an enforcing spec through the real book: roots granted,
// token proven, SID set; and a spec that does not enforce is left alone.
func TestGrantSpecPreparesAnEnforcingSpecAndLeavesAnOffSpecAlone(t *testing.T) {
	state := testenv.TempDir(t)
	ws := testenv.TempDir(t)
	env := profileEnv(t)
	off := GrantSpec(Spec{Mode: "off", WriteRoots: []string{ws}, ShellEnv: env}, state)
	if off.GrantSID != "" || off.GrantFault != nil {
		t.Fatalf("an off spec was touched: %+v", off)
	}
	on := GrantSpec(Spec{Mode: "enforce", WriteRoots: []string{ws}, ShellEnv: env}, state)
	if on.GrantFault != nil || on.GrantSID == "" {
		t.Fatalf("enforce spec = fault %v sid %q", on.GrantFault, on.GrantSID)
	}
	if ok, _ := WindowsGrantOps().Granted(ws, on.GrantSID); !ok {
		t.Fatal("the workspace root was not granted")
	}
	if !AvailableFor(on) || bwrapFaultFor(on) != nil {
		t.Fatal("a prepared spec reports itself unavailable")
	}
	t.Cleanup(func() {
		if b, err := windowsBook(state); err == nil {
			_ = b.Revoke(ws)
		}
	})
}

// A damaged record is a typed fault with its own stable code, never a fresh start.
func TestGrantSpecReportsADamagedRecordAsATypedFault(t *testing.T) {
	state := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(state, "windows-grants.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := GrantSpec(Spec{Mode: "enforce", WriteRoots: []string{testenv.TempDir(t)}, ShellEnv: profileEnv(t)}, state)
	if !errors.Is(spec.GrantFault, ErrWinGrantUnreadable) || spec.GrantSID != "" {
		t.Fatalf("spec = fault %v sid %q", spec.GrantFault, spec.GrantSID)
	}
	if AvailableFor(spec) {
		t.Fatal("an unprepared enforcing spec reports itself available")
	}
	if got := UnavailableCodeFor(spec); got != "sandbox.win_grant_unreadable" {
		t.Fatalf("code = %q", got)
	}
	if _, wrapped := CommandArgs(spec, []string{"cmd"}); wrapped {
		t.Fatal("a command was wrapped without a prepared spec")
	}
}

// Inside a root the sandbox edits and removes files that were already there.
func TestRestrictedCommandsEditAndRemoveExistingFilesInsideTheRoot(t *testing.T) {
	h := newWinHarness(t)
	before := filepath.Join(h.ws, "before.txt")
	h.run(`echo more>> ` + before)
	if b, _ := os.ReadFile(before); !strings.Contains(string(b), "more") {
		t.Errorf("the sandbox could not edit a file that existed before the grant: %q", b)
	}
	h.run(`del ` + before)
	if exists(before) {
		t.Error("the sandbox could not remove a file inside its own root")
	}
}

// A known gap, pinned so the documentation cannot drift from it: Windows does
// not consult the restricting SIDs for a delete or a rename, so a restricted
// command can remove or move away anything the user may outside its roots. If
// this test starts failing the bound has grown; update the guide when it does.
func TestKnownGapDeleteOutsideTheRootIsNotBounded(t *testing.T) {
	h := newWinHarness(t)
	victim := filepath.Join(h.out, "victim.txt")
	if err := os.WriteFile(victim, []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.run(`del ` + victim)
	if exists(victim) {
		t.Skip("a delete outside the root was refused on this machine; the documented gap did not show here")
	}
	h.run(`echo moved> ` + filepath.Join(h.out, "still-denied.txt"))
	if exists(filepath.Join(h.out, "still-denied.txt")) {
		t.Fatal("the gap is wider than documented: a create outside the root succeeded")
	}
}

func TestGrantedRootsListsWhatGrantSpecGrantedAndRevokeGrantsTakesItBack(t *testing.T) {
	state, ws := testenv.TempDir(t), testenv.TempDir(t)
	spec := GrantSpec(Spec{Mode: "enforce", WriteRoots: []string{ws}, ShellEnv: profileEnv(t)}, state)
	if spec.GrantFault != nil {
		t.Fatal(spec.GrantFault)
	}
	sid, roots, err := GrantedRoots(state)
	if err != nil || sid != spec.GrantSID || len(roots) == 0 {
		t.Fatalf("GrantedRoots = %q %v %v", sid, roots, err)
	}
	// A fresh process would open the record again; this one holds the book, so drop it.
	windowsBooksMu.Lock()
	delete(windowsBooks, state)
	windowsBooksMu.Unlock()
	done, failed, err := RevokeGrants(state)
	if err != nil || len(failed) != 0 || len(done) != len(roots) {
		t.Fatalf("RevokeGrants = %v %v %v", done, failed, err)
	}
	if ok, _ := WindowsGrantOps().Granted(ws, sid); ok {
		t.Fatal("the SID is still on the workspace after RevokeGrants")
	}
	if _, left, _ := GrantedRoots(state); len(left) != 0 {
		t.Fatalf("the record still lists %v", left)
	}
}

// A workspace that contains the grant record would let a command rewrite what the
// host grants next, so enforcement is refused with its own typed fault.
func TestGrantSpecRefusesARecordInsideAWritableRoot(t *testing.T) {
	home := testenv.TempDir(t)
	state := filepath.Join(home, "AppData", "reasonix", "sandbox")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	spec := GrantSpec(Spec{Mode: "enforce", WriteRoots: []string{home}, ShellEnv: profileEnv(t)}, state)
	if !errors.Is(spec.GrantFault, ErrWinRecordExposed) || spec.GrantSID != "" {
		t.Fatalf("spec = fault %v sid %q", spec.GrantFault, spec.GrantSID)
	}
	if got := UnavailableCodeFor(spec); got != "sandbox.win_record_exposed" {
		t.Fatalf("code = %q", got)
	}
	if ok, _ := WindowsGrantOps().Granted(home, newSyntheticSID()); ok {
		t.Fatal("something was granted before the refusal")
	}
	beside := GrantSpec(Spec{Mode: "enforce", WriteRoots: []string{testenv.TempDir(t)}, ShellEnv: profileEnv(t)}, state)
	if beside.GrantFault != nil {
		t.Fatalf("a workspace beside the record was refused: %v", beside.GrantFault)
	}
	t.Cleanup(func() {
		if b, err := windowsBook(state); err == nil {
			for _, r := range b.Roots() {
				_ = b.Revoke(r)
			}
		}
	})
}
