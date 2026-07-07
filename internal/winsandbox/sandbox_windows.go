//go:build windows

package winsandbox

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"reasonix/internal/proc"

	"golang.org/x/sys/windows"
)

const (
	procThreadAttributeSecurityCapabilities = 0x00020009
	startupFlagsUseStdHandles               = windows.STARTF_USESTDHANDLES
	startupFlagsHiddenWindow                = startupFlagsUseStdHandles | windows.STARTF_USESHOWWINDOW
	hresultAlreadyExists                    = 0x800700b7
	allApplicationPackagesSID               = "S-1-15-2-1"
	allRestrictedApplicationPackagesSID     = "S-1-15-2-2"
	lowMandatoryLevelSID                    = "S-1-16-4096"
)

var (
	moduserenv                        = windows.NewLazySystemDLL("userenv.dll")
	procCreateAppContainerProfile     = moduserenv.NewProc("CreateAppContainerProfile")
	procDeriveAppContainerSidFromName = moduserenv.NewProc("DeriveAppContainerSidFromAppContainerName")
	windowsSandboxWaitMilliseconds    = uint32(windows.INFINITE)
)

// Available reports whether the native Windows sandbox backend is available.
func Available() bool {
	for _, proc := range []*windows.LazyProc{
		procCreateAppContainerProfile,
		procDeriveAppContainerSidFromName,
	} {
		if err := proc.Find(); err != nil {
			return false
		}
	}
	return true
}

// Run executes argv in the native Windows sandbox.
func Run(spec Spec, argv []string, opts RunOptions) (Result, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return Result{}, fmt.Errorf("windows sandbox command is required")
	}
	if !Available() {
		return Result{}, ErrUnsupported
	}
	code, err := runWindowsSandboxed(spec, argv, opts)
	if err != nil {
		return Result{}, err
	}
	return Result{ExitCode: code}, nil
}

// lockWaitNotice adapts the run's stderr into the lock queue-notice writer.
// The typed-nil guard matters: passing a nil *os.File through io.Writer would
// make the interface non-nil and defeat the notice==nil checks downstream.
func lockWaitNotice(opts RunOptions) io.Writer {
	if opts.Stderr == nil {
		return nil
	}
	return opts.Stderr
}

func runWindowsSandboxed(spec Spec, argv []string, opts RunOptions) (int, error) {
	if spec.Writable {
		return runWindowsRestrictedSandboxed(spec, argv, opts)
	}
	// Serialize against concurrent runs touching the same roots and clear any
	// deny residue left by a crashed run before we mutate ACLs.
	lock, err := lockWindowsRoots(windowsMutatedRootsForRun(spec, argv[0]), lockWaitNotice(opts), lockHolderLabel(argv), spec.LockWait)
	if err != nil {
		return 0, err
	}
	defer lock.release()
	sweepWindowsDenyResidue()
	residueRun := newWindowsDenyResidueRun()
	// Clear this run's residue marker last — after every grant/deny cleanup below
	// has run — so a crash at any point leaves a marker covering all ACEs still on
	// disk. Registered before the cleanup defers so it executes after them (LIFO).
	defer residueRun.clear()
	ac, err := prepareAppContainer(spec)
	if err != nil {
		return 0, err
	}
	defer ac.close()
	tempRoot, cleanupTemp, err := windowsSandboxTempRoot(spec)
	if err != nil {
		return 0, err
	}
	defer cleanupTemp()
	cleanupFS, err := grantAppContainerFilesystem(residueRun, ac.sid, spec, tempRoot)
	if err != nil {
		return 0, err
	}
	defer cleanupFS()
	cleanupExe, err := grantAppContainerExecutable(residueRun, ac.sid, argv[0])
	if err != nil {
		return 0, err
	}
	defer cleanupExe()

	job, err := sandboxJobObject()
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(job)

	childEnv := windowsSandboxEnv(spec, tempRoot, opts.Env)
	pi, err := startAppContainerProcess(ac, argv, childEnv, opts)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(pi.Process)
	defer windows.CloseHandle(pi.Thread)

	if err := windows.AssignProcessToJobObject(job, pi.Process); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		return 0, err
	}
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		return 0, fmt.Errorf("resume sandboxed process: %w", err)
	}
	event, err := windows.WaitForSingleObject(pi.Process, windowsSandboxWaitLimitMilliseconds())
	if err != nil {
		return 0, fmt.Errorf("wait for sandboxed process: %w", err)
	}
	switch event {
	case windows.WAIT_OBJECT_0:
	case uint32(windows.WAIT_TIMEOUT):
		_ = windows.TerminateJobObject(job, 1)
		_, _ = windows.WaitForSingleObject(pi.Process, 5000)
		return 0, fmt.Errorf("sandboxed process timed out")
	default:
		return 0, fmt.Errorf("wait for sandboxed process returned %#x", event)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(pi.Process, &code); err != nil {
		return 0, fmt.Errorf("get sandboxed process exit code: %w", err)
	}
	return int(code), nil
}

func runWindowsRestrictedSandboxed(spec Spec, argv []string, opts RunOptions) (int, error) {
	if !spec.Network {
		return 0, fmt.Errorf("network=false is not available for writable Windows sandbox commands")
	}
	// Serialize against concurrent runs touching the same roots and clear any
	// deny residue left by a crashed run before we mutate ACLs.
	lock, err := lockWindowsRoots(windowsMutatedRootsForRun(spec, argv[0]), lockWaitNotice(opts), lockHolderLabel(argv), spec.LockWait)
	if err != nil {
		return 0, err
	}
	defer lock.release()
	sweepWindowsDenyResidue()
	residueRun := newWindowsDenyResidueRun()
	// Clear this run's residue marker last — after every grant/deny cleanup below
	// has run — so a crash at any point leaves a marker covering all ACEs still on
	// disk. Registered before the cleanup defers so it executes after them (LIFO).
	defer residueRun.clear()
	tempRoot, cleanupTemp, err := windowsSandboxTempRoot(spec)
	if err != nil {
		return 0, err
	}
	defer cleanupTemp()
	userSID, err := currentProcessUserSID()
	if err != nil {
		return 0, err
	}
	cleanupFS, err := grantAppContainerFilesystem(residueRun, userSID, spec, tempRoot)
	if err != nil {
		return 0, err
	}
	defer cleanupFS()
	cleanupExe, err := grantAppContainerExecutable(residueRun, userSID, argv[0])
	if err != nil {
		return 0, err
	}
	defer cleanupExe()

	token, err := createLowIntegrityPrimaryToken()
	if err != nil {
		return 0, err
	}
	defer token.Close()

	job, err := sandboxJobObject()
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(job)

	childEnv := windowsSandboxEnv(spec, tempRoot, opts.Env)
	pi, err := startRestrictedTokenProcess(token, argv, childEnv, opts)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(pi.Process)
	defer windows.CloseHandle(pi.Thread)

	if err := windows.AssignProcessToJobObject(job, pi.Process); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		return 0, err
	}
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		return 0, fmt.Errorf("resume sandboxed process: %w", err)
	}
	event, err := windows.WaitForSingleObject(pi.Process, windowsSandboxWaitLimitMilliseconds())
	if err != nil {
		return 0, fmt.Errorf("wait for sandboxed process: %w", err)
	}
	switch event {
	case windows.WAIT_OBJECT_0:
	case uint32(windows.WAIT_TIMEOUT):
		_ = windows.TerminateJobObject(job, 1)
		_, _ = windows.WaitForSingleObject(pi.Process, 5000)
		return 0, fmt.Errorf("sandboxed process timed out")
	default:
		return 0, fmt.Errorf("wait for sandboxed process returned %#x", event)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(pi.Process, &code); err != nil {
		return 0, fmt.Errorf("get sandboxed process exit code: %w", err)
	}
	return int(code), nil
}

type appContainerLaunch struct {
	sid          *windows.SID
	capabilities []windows.SIDAndAttributes
}

func (a *appContainerLaunch) close() {
	if a == nil {
		return
	}
	if a.sid != nil {
		_ = windows.FreeSid(a.sid)
	}
}

func prepareAppContainer(spec Spec) (*appContainerLaunch, error) {
	name := windowsAppContainerName(spec)
	sid, err := createOrDeriveAppContainer(name)
	if err != nil {
		return nil, err
	}
	ac := &appContainerLaunch{sid: sid}
	if spec.Network {
		for _, sidType := range []windows.WELL_KNOWN_SID_TYPE{
			windows.WinCapabilityInternetClientSid,
			windows.WinCapabilityPrivateNetworkClientServerSid,
		} {
			capSID, err := windows.CreateWellKnownSid(sidType)
			if err != nil {
				ac.close()
				return nil, fmt.Errorf("create network capability SID: %w", err)
			}
			ac.capabilities = append(ac.capabilities, windows.SIDAndAttributes{Sid: capSID, Attributes: windows.SE_GROUP_ENABLED})
		}
	}
	return ac, nil
}

func windowsAppContainerName(spec Spec) string {
	var b strings.Builder
	b.WriteString("windows-sandbox")
	b.WriteByte(0)
	b.WriteString(os.Getenv("USERNAME"))
	b.WriteByte(0)
	b.WriteString(os.Getenv("USERDOMAIN"))
	b.WriteByte(0)
	b.WriteString("ro")
	for _, root := range windowsWritableRoots(spec) {
		b.WriteByte(0)
		b.WriteString(strings.ToLower(filepath.Clean(root)))
	}
	for _, root := range normalizedWindowsRoots(spec.ForbidReadRoots) {
		b.WriteByte(0)
		b.WriteString("deny=")
		b.WriteString(root)
	}
	sum := sha1.Sum([]byte(b.String()))
	return "WinSandbox." + hex.EncodeToString(sum[:10])
}

func createOrDeriveAppContainer(name string) (*windows.SID, error) {
	name16, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	var sid *windows.SID
	hr, _, _ := procCreateAppContainerProfile.Call(
		uintptr(unsafe.Pointer(name16)),
		uintptr(unsafe.Pointer(name16)),
		uintptr(unsafe.Pointer(name16)),
		0,
		0,
		uintptr(unsafe.Pointer(&sid)),
	)
	if hr == 0 {
		return sid, nil
	}
	if uint32(hr) != hresultAlreadyExists {
		return nil, fmt.Errorf("create appcontainer profile %q: HRESULT 0x%08x", name, uint32(hr))
	}
	hr, _, _ = procDeriveAppContainerSidFromName.Call(
		uintptr(unsafe.Pointer(name16)),
		uintptr(unsafe.Pointer(&sid)),
	)
	if hr != 0 {
		return nil, fmt.Errorf("derive appcontainer SID %q: HRESULT 0x%08x", name, uint32(hr))
	}
	return sid, nil
}

type securityCapabilities struct {
	AppContainerSid *windows.SID
	Capabilities    *windows.SIDAndAttributes
	CapabilityCount uint32
	Reserved        uint32
}

func createLowIntegrityPrimaryToken() (windows.Token, error) {
	var existing windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_DUPLICATE|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_QUERY|windows.TOKEN_ADJUST_DEFAULT|windows.TOKEN_ADJUST_SESSIONID, &existing); err != nil {
		return 0, fmt.Errorf("open process token: %w", err)
	}
	defer existing.Close()
	var token windows.Token
	if err := windows.DuplicateTokenEx(existing, windows.TOKEN_ALL_ACCESS, nil, windows.SecurityImpersonation, windows.TokenPrimary, &token); err != nil {
		return 0, fmt.Errorf("duplicate low-integrity token: %w", err)
	}
	lowSID, err := windows.StringToSid(lowMandatoryLevelSID)
	if err != nil {
		token.Close()
		return 0, fmt.Errorf("create low integrity SID: %w", err)
	}
	label := windows.Tokenmandatorylabel{
		Label: windows.SIDAndAttributes{
			Sid:        lowSID,
			Attributes: windows.SE_GROUP_INTEGRITY,
		},
	}
	if err := windows.SetTokenInformation(token, uint32(windows.TokenIntegrityLevel), (*byte)(unsafe.Pointer(&label)), label.Size()); err != nil {
		token.Close()
		return 0, fmt.Errorf("set low token integrity: %w", err)
	}
	return token, nil
}

func startRestrictedTokenProcess(token windows.Token, argv []string, env []string, opts RunOptions) (windows.ProcessInformation, error) {
	var pi windows.ProcessInformation
	resolvedArgv := append([]string(nil), argv...)
	exe, err := resolveWindowsExecutable(argv[0])
	if err != nil {
		return pi, err
	}
	resolvedArgv[0] = exe
	appName, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return pi, err
	}
	cmdLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(resolvedArgv))
	if err != nil {
		return pi, err
	}
	var cwd *uint16
	if wd := opts.Dir; wd != "" {
		cwd, _ = windows.UTF16PtrFromString(wd)
	} else if wd, err := os.Getwd(); err == nil {
		cwd, _ = windows.UTF16PtrFromString(wd)
	}
	envBlock, err := windowsEnvironmentBlock(env)
	if err != nil {
		return pi, err
	}
	var envp *uint16
	if len(envBlock) > 0 {
		envp = &envBlock[0]
	}

	handles, err := inheritableStdHandles(opts.Stdin, opts.Stdout, opts.Stderr)
	if err != nil {
		return pi, err
	}
	defer closeHandles(handles)

	attrList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return pi, fmt.Errorf("create startup attribute list: %w", err)
	}
	defer attrList.Delete()
	inheritedHandles := uniqueNonZeroHandles(handles[:])
	if len(inheritedHandles) > 0 {
		if err := attrList.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&inheritedHandles[0]), uintptr(len(inheritedHandles))*unsafe.Sizeof(inheritedHandles[0])); err != nil {
			return pi, fmt.Errorf("set inherited handle list: %w", err)
		}
	}

	si := windowsSandboxStartupInfo(handles, attrList)

	flags := windowsSandboxProcessCreationFlags()
	if err := windows.CreateProcessAsUser(token, appName, cmdLine, nil, nil, true, flags, envp, cwd, &si.StartupInfo, &pi); err != nil {
		return pi, fmt.Errorf("create restricted process: %w", err)
	}
	return pi, nil
}

func startAppContainerProcess(ac *appContainerLaunch, argv []string, env []string, opts RunOptions) (windows.ProcessInformation, error) {
	var pi windows.ProcessInformation
	resolvedArgv := append([]string(nil), argv...)
	exe, err := resolveWindowsExecutable(argv[0])
	if err != nil {
		return pi, err
	}
	resolvedArgv[0] = exe
	appName, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return pi, err
	}
	cmdLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(resolvedArgv))
	if err != nil {
		return pi, err
	}
	var cwd *uint16
	if wd := opts.Dir; wd != "" {
		cwd, _ = windows.UTF16PtrFromString(wd)
	} else if wd, err := os.Getwd(); err == nil {
		cwd, _ = windows.UTF16PtrFromString(wd)
	}
	envBlock, err := windowsEnvironmentBlock(env)
	if err != nil {
		return pi, err
	}
	var envp *uint16
	if len(envBlock) > 0 {
		envp = &envBlock[0]
	}

	handles, err := inheritableStdHandles(opts.Stdin, opts.Stdout, opts.Stderr)
	if err != nil {
		return pi, err
	}
	defer closeHandles(handles)

	attrList, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return pi, fmt.Errorf("create startup attribute list: %w", err)
	}
	defer attrList.Delete()

	caps := securityCapabilities{AppContainerSid: ac.sid, CapabilityCount: uint32(len(ac.capabilities))}
	if len(ac.capabilities) > 0 {
		caps.Capabilities = &ac.capabilities[0]
	}
	if err := attrList.Update(procThreadAttributeSecurityCapabilities, unsafe.Pointer(&caps), unsafe.Sizeof(caps)); err != nil {
		return pi, fmt.Errorf("set appcontainer security capabilities: %w", err)
	}
	inheritedHandles := uniqueNonZeroHandles(handles[:])
	if len(inheritedHandles) > 0 {
		if err := attrList.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&inheritedHandles[0]), uintptr(len(inheritedHandles))*unsafe.Sizeof(inheritedHandles[0])); err != nil {
			return pi, fmt.Errorf("set inherited handle list: %w", err)
		}
	}

	si := windowsSandboxStartupInfo(handles, attrList)

	flags := windowsSandboxProcessCreationFlags()
	if err := windows.CreateProcess(appName, cmdLine, nil, nil, true, flags, envp, cwd, &si.StartupInfo, &pi); err != nil {
		return pi, fmt.Errorf("create appcontainer process: %w", err)
	}
	return pi, nil
}

func windowsSandboxStartupInfo(handles [3]windows.Handle, attrList *windows.ProcThreadAttributeListContainer) windows.StartupInfoEx {
	si := windows.StartupInfoEx{}
	si.StartupInfo.Cb = uint32(unsafe.Sizeof(si))
	si.StartupInfo.Flags = startupFlagsHiddenWindow
	si.StartupInfo.ShowWindow = uint16(windows.SW_HIDE)
	si.StartupInfo.StdInput = handles[0]
	si.StartupInfo.StdOutput = handles[1]
	si.StartupInfo.StdErr = handles[2]
	if attrList != nil {
		si.ProcThreadAttributeList = attrList.List()
	}
	return si
}

func windowsSandboxProcessCreationFlags() uint32 {
	return uint32(windows.CREATE_UNICODE_ENVIRONMENT |
		windows.EXTENDED_STARTUPINFO_PRESENT |
		windows.CREATE_SUSPENDED |
		windows.CREATE_NO_WINDOW)
}

func resolveWindowsExecutable(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("command is required")
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
		return name, nil
	}
	exe, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("resolve command %q: %w", name, err)
	}
	return exe, nil
}

func sandboxJobObject() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create job object: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return 0, fmt.Errorf("configure job object limits: %w", err)
	}
	ui := windows.JOBOBJECT_BASIC_UI_RESTRICTIONS{
		UIRestrictionsClass: windows.JOB_OBJECT_UILIMIT_DESKTOP |
			windows.JOB_OBJECT_UILIMIT_DISPLAYSETTINGS |
			windows.JOB_OBJECT_UILIMIT_EXITWINDOWS |
			windows.JOB_OBJECT_UILIMIT_GLOBALATOMS |
			windows.JOB_OBJECT_UILIMIT_HANDLES |
			windows.JOB_OBJECT_UILIMIT_READCLIPBOARD |
			windows.JOB_OBJECT_UILIMIT_SYSTEMPARAMETERS |
			windows.JOB_OBJECT_UILIMIT_WRITECLIPBOARD,
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectBasicUIRestrictions, uintptr(unsafe.Pointer(&ui)), uint32(unsafe.Sizeof(ui))); err != nil {
		windows.CloseHandle(job)
		return 0, fmt.Errorf("configure job object UI restrictions: %w", err)
	}
	return job, nil
}

func windowsSandboxWaitLimitMilliseconds() uint32 {
	if raw := os.Getenv("WINDOWS_SANDBOX_WAIT_MS"); raw != "" {
		ms, err := strconv.ParseUint(raw, 10, 32)
		if err == nil && ms > 0 {
			return uint32(ms)
		}
	}
	return windowsSandboxWaitMilliseconds
}

func inheritableStdHandles(stdin *os.File, stdout *os.File, stderr *os.File) ([3]windows.Handle, error) {
	files := [3]*os.File{stdin, stdout, stderr}
	var out [3]windows.Handle
	for i, f := range files {
		if f == nil {
			continue
		}
		dup, err := duplicateInheritableHandle(windows.Handle(f.Fd()))
		if err != nil {
			closeHandles(out)
			return out, err
		}
		out[i] = dup
	}
	return out, nil
}

func duplicateInheritableHandle(h windows.Handle) (windows.Handle, error) {
	var dup windows.Handle
	current := windows.CurrentProcess()
	if err := windows.DuplicateHandle(current, h, current, &dup, 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return 0, fmt.Errorf("duplicate stdio handle: %w", err)
	}
	return dup, nil
}

func closeHandles(handles [3]windows.Handle) {
	for _, h := range handles {
		if h != 0 {
			_ = windows.CloseHandle(h)
		}
	}
}

func uniqueNonZeroHandles(handles []windows.Handle) []windows.Handle {
	out := make([]windows.Handle, 0, len(handles))
	seen := map[windows.Handle]bool{}
	for _, h := range handles {
		if h == 0 || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}

func grantAppContainerFilesystem(residueRun *windowsResidueRun, sid *windows.SID, spec Spec, extraWritableRoots ...string) (func(), error) {
	objectSIDStrs := appContainerObjectAccessSIDStrings(sid)
	writableSIDStrs := appContainerWritableAccessSIDStrings(sid)
	var cleanup []func()
	for _, root := range windowsWritableRoots(spec, extraWritableRoots...) {
		restore, _, err := snapshotPathSecurity(root, spec.Writable)
		if err != nil {
			runCleanup(cleanup)()
			return func() {}, err
		}
		cleanup = append(cleanup, restore)
		restoreIndex := len(cleanup) - 1
		perm := "RX"
		if spec.Writable {
			perm = "F"
		}
		if err := grantAppContainerSIDs(root, writableSIDStrs, perm); err != nil {
			runCleanup(cleanup)()
			return func() {}, err
		}
		var restoreLabel func()
		if spec.Writable {
			if err := setLowIntegrity(root); err != nil {
				runCleanup(cleanup)()
				return func() {}, err
			}
			// setLowIntegrity stamps an *explicit* Low integrity label on every
			// descendant via `/T`, but the DACL/label snapshot only captures the
			// root. Without a recursive reset the subtree keeps its Low label
			// after cleanup, so any low-integrity process on the host (a browser
			// renderer, say) could write into the user's files later — a durable
			// security regression. Always recursively reset the subtree to Medium
			// on cleanup so no Low residue survives. The root's own label is
			// restored from the snapshot first; resetting it to explicit Medium
			// afterward is harmless because Medium is the default effective level
			// for user files.
			restoreLabel = func() { _ = icacls(root, "/setintegritylevel", "(OI)(CI)M", "/T", "/C") }
		}
		removeAdded := func() { removeGrantedAppContainerSIDs(root, writableSIDStrs) }
		cleanup[restoreIndex] = cleanupPathSecurity(restore, removeAdded, restoreLabel)
	}
	for _, root := range normalizedWindowsRoots(spec.ForbidReadRoots) {
		// forbid_read must also cover single files (e.g. ~/.netrc, .npmrc), not
		// just directories. Skip only truly-absent paths; deny ACEs apply to
		// files as well as directories. pathExists distinguishes "missing" from
		// "is a file", so a forbid_read entry pointing at a real file is still
		// protected instead of being silently ignored.
		if !pathExists(root) {
			continue
		}
		restore, _, err := snapshotPathSecurity(root, false)
		if err != nil {
			runCleanup(cleanup)()
			return func() {}, err
		}
		cleanup = append(cleanup, restore)
		restoreIndex := len(cleanup) - 1
		denySIDStrs := forbidReadDenySIDStrings(objectSIDStrs)
		// Record the deny in the crash-residue marker *before* applying it, and
		// fail closed if the marker cannot be written. Otherwise a crash between
		// applying the deny and recording it — or a silent marker write failure —
		// would leave the user's SID denied on this path with no marker for the
		// next run to sweep, locking them out of e.g. ~/.ssh permanently.
		if err := residueRun.recordBeforeApply(residueDeny, root); err != nil {
			runCleanup(cleanup)()
			return func() {}, err
		}
		// Directories need inheritance so the deny covers existing and new
		// children; a plain file has no children, so inheritance flags are
		// meaningless (and icacls rejects (OI)(CI) on a file).
		if err := denyAppContainerSIDsWithInheritance(root, denySIDStrs, "RX", dirExists(root)); err != nil {
			runCleanup(cleanup)()
			return func() {}, err
		}
		removeAdded := func() { removeDeniedAppContainerSIDs(root, denySIDStrs) }
		cleanup[restoreIndex] = cleanupPathSecurity(restore, removeAdded, nil)
	}
	// The residue marker is written incrementally above and cleared centrally by
	// the run once every cleanup (filesystem and executable) has completed.
	return runCleanup(cleanup), nil
}

func forbidReadDenySIDStrings(base []string) []string {
	out := append([]string(nil), base...)
	if userSID, err := currentProcessUserSIDString(); err == nil && userSID != "" {
		out = append(out, userSID)
	}
	return dedupeSIDStrings(out)
}

func grantAppContainerExecutable(residueRun *windowsResidueRun, sid *windows.SID, exe string) (func(), error) {
	objectSIDStrs := appContainerObjectAccessSIDStrings(sid)
	var cleanup []func()
	for _, dir := range windowsMutableExecutableGrantRoots(exe) {
		// Only non-system tool directories reach here; system locations already
		// grant AppContainer read/execute through built-in package SIDs and are
		// excluded upstream so a crash can never leave residue that a later sweep
		// would strip from a system directory. Treat the remaining local-tool
		// grants as best-effort convenience instead of failing before the OS can
		// decide.
		restore, _, err := snapshotPathSecurity(dir, false)
		if err != nil {
			continue
		}
		// Record the grant before applying it so a crash cannot leave a widened
		// tool-directory ACL with no marker for the next run to sweep. A marker
		// write failure aborts this dir's grant (restore and skip) rather than
		// applying an untracked grant.
		if err := residueRun.recordBeforeApply(residueGrant, dir); err != nil {
			restore()
			continue
		}
		if err := grantAppContainerSIDs(dir, objectSIDStrs, "RX"); err != nil {
			restore()
			continue
		}
		removeAdded := func() { removeGrantedAppContainerSIDs(dir, objectSIDStrs) }
		cleanup = append(cleanup, cleanupPathSecurity(restore, removeAdded, nil))
	}
	// The residue marker is cleared centrally by the run after all cleanups.
	return runCleanup(cleanup), nil
}

func windowsExecutableGrantDir(exe string) string {
	roots := windowsExecutableGrantRoots(exe)
	if len(roots) == 0 {
		return ""
	}
	return roots[0]
}

func windowsExecutableGrantRoots(exe string) []string {
	if strings.TrimSpace(exe) == "" {
		return nil
	}
	if resolved, err := resolveWindowsExecutable(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	if dir == "." || !dirExists(dir) {
		return nil
	}
	roots := []string{dir}
	if gitRoot := windowsGitInstallRoot(exe); gitRoot != "" && !sameWindowsRoot(gitRoot, dir) {
		roots = append(roots, gitRoot)
	}
	return dedupeWindowsRoots(roots)
}

// windowsMutableExecutableGrantRoots is the subset of the run's executable grant
// roots whose ACLs the sandbox will actually mutate: the non-system ones. System
// tool directories (System32, the Program Files variants) already grant
// AppContainer read/execute through Windows' built-in package SIDs, so an
// AppContainer child can execute a system shell without any sandbox grant. They
// are deliberately excluded so the sandbox never snapshots, grants, or records
// crash-residue for them: a residue entry on a system directory would let a later
// sweep run icacls /remove:g for the broad built-in package SIDs and strip the
// directory's factory ACEs. This is the single source of truth shared by both the
// per-root lock set (windowsMutatedRootsForRun) and the grant loop
// (grantAppContainerExecutable), so the paths that are locked and the paths that
// are mutated can never drift apart. Membership is by path, not a write probe, so
// it stays stable when the process is elevated (an admin can create a file under
// System32, which a probe would misread as writable and wrongly pull in).
func windowsMutableExecutableGrantRoots(exe string) []string {
	roots := windowsExecutableGrantRoots(exe)
	out := make([]string, 0, len(roots))
	for _, dir := range roots {
		if !isWindowsSystemRoot(dir) {
			out = append(out, dir)
		}
	}
	return out
}

func windowsGitInstallRoot(exe string) string {
	for dir := filepath.Dir(filepath.Clean(exe)); dir != "."; dir = filepath.Dir(dir) {
		if strings.EqualFold(filepath.Base(dir), "Git") && dirExists(dir) {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}
	return ""
}

func appContainerPackageSIDStrings(sid *windows.SID) []string {
	if sid == nil {
		return nil
	}
	return []string{sid.String()}
}

func appContainerObjectAccessSIDStrings(sid *windows.SID) []string {
	out := appContainerPackageSIDStrings(sid)
	if len(out) == 0 {
		return nil
	}
	// AppContainer file access is evaluated with the package SID plus Windows'
	// built-in app package groups. Grant the broad groups only on paths whose
	// descriptors we snapshot and restore; ancestor traversal stays package-SID
	// only to avoid disturbing existing system directory ACLs.
	return append(out, allApplicationPackagesSID, allRestrictedApplicationPackagesSID)
}

func appContainerWritableAccessSIDStrings(sid *windows.SID) []string {
	out := append([]string(nil), appContainerObjectAccessSIDStrings(sid)...)
	if userSID, err := currentProcessUserSIDString(); err == nil && userSID != "" {
		out = append(out, userSID)
	}
	return dedupeSIDStrings(out)
}

func currentProcessUserSIDString() (string, error) {
	sid, err := currentProcessUserSID()
	if err != nil || sid == nil {
		return "", err
	}
	return sid.String(), nil
}

func currentProcessUserSID() (*windows.SID, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	if user == nil || user.User.Sid == nil {
		return nil, nil
	}
	return user.User.Sid.Copy()
}

func dedupeSIDStrings(sids []string) []string {
	out := make([]string, 0, len(sids))
	seen := map[string]bool{}
	for _, sid := range sids {
		if sid == "" || seen[sid] {
			continue
		}
		seen[sid] = true
		out = append(out, sid)
	}
	return out
}

func snapshotPathSecurity(path string, includeLabel bool) (func(), bool, error) {
	info := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION)
	if includeLabel {
		info |= windows.LABEL_SECURITY_INFORMATION
	}
	restoreDACL, cleanupDACL, err := snapshotPathDACLWithICACLS(path)
	if err != nil {
		restoreDACL = nil
		cleanupDACL = nil
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, info)
	if err != nil {
		if cleanupDACL != nil {
			cleanupDACL()
		}
		if includeLabel && errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			restore, _, daclErr := snapshotPathSecurity(path, false)
			if daclErr == nil {
				return restore, false, nil
			}
		}
		return nil, false, fmt.Errorf("snapshot security descriptor %q: %w", path, err)
	}
	if sd == nil {
		return func() {}, includeLabel, nil
	}
	sddl := sd.String()
	if sddl == "" {
		return nil, false, fmt.Errorf("snapshot security descriptor %q: empty SDDL", path)
	}
	return func() {
		if restoreDACL != nil {
			_ = restoreDACL()
		} else {
			_ = restorePathSecurity(path, sddl, windows.DACL_SECURITY_INFORMATION)
		}
		if includeLabel {
			_ = restorePathSecurity(path, sddl, windows.LABEL_SECURITY_INFORMATION)
		}
		if cleanupDACL != nil {
			cleanupDACL()
		}
	}, includeLabel, nil
}

func snapshotPathDACLWithICACLS(path string) (func() error, func(), error) {
	f, err := os.CreateTemp("", "windows-sandbox-acl-*.txt")
	if err != nil {
		return nil, nil, err
	}
	snapshot := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(snapshot)
		return nil, nil, err
	}
	if err := icacls(path, "/save", snapshot, "/C"); err != nil {
		_ = os.Remove(snapshot)
		return nil, nil, err
	}
	restore := func() error {
		return icacls(windowsACLRestoreRoot(path), "/restore", snapshot, "/C")
	}
	cleanup := func() { _ = os.Remove(snapshot) }
	return restore, cleanup, nil
}

func windowsACLRestoreRoot(path string) string {
	if volume := filepath.VolumeName(path); volume != "" {
		return volume + string(os.PathSeparator)
	}
	return filepath.Dir(path)
}

func cleanupPathSecurity(restore func(), removeAddedACEs func(), afterRestore func()) func() {
	return func() {
		if removeAddedACEs != nil {
			removeAddedACEs()
		}
		if restore != nil {
			restore()
		}
		if afterRestore != nil {
			afterRestore()
		}
	}
}

func restorePathSecurity(path, sddl string, info windows.SECURITY_INFORMATION) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("parse security descriptor snapshot %q: %w", path, err)
	}
	absolute, err := sd.ToAbsolute()
	if err != nil {
		return fmt.Errorf("prepare security descriptor snapshot %q: %w", path, err)
	}
	if info&windows.DACL_SECURITY_INFORMATION != 0 {
		dacl, _, err := absolute.DACL()
		if err != nil {
			return fmt.Errorf("restore security descriptor DACL %q: %w", path, err)
		}
		if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
			return fmt.Errorf("restore security descriptor DACL %q: %w", path, err)
		}
	}
	var sacl *windows.ACL
	if info&windows.LABEL_SECURITY_INFORMATION != 0 {
		sacl, _, err = absolute.SACL()
		if err != nil {
			return fmt.Errorf("restore security descriptor SACL %q: %w", path, err)
		}
		if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION, nil, nil, nil, sacl); err != nil {
			return fmt.Errorf("restore security descriptor label %q: %w", path, err)
		}
	}
	return nil
}

func runCleanup(cleanup []func()) func() {
	return func() {
		for i := len(cleanup) - 1; i >= 0; i-- {
			cleanup[i]()
		}
	}
}

// icacls default timeouts. The non-recursive default is generous because
// icacls can be slow on busy or antivirus-scanned volumes; the recursive
// default is far larger because `/T` walks the whole subtree, and a large
// workspace (e.g. one with node_modules) can take much longer than a few
// seconds. Both are overridable through WINDOWS_SANDBOX_ICACLS_TIMEOUT_MS so
// operators on pathological trees can raise the ceiling without a rebuild.
const (
	defaultICACLSTimeout          = 30 * time.Second
	defaultICACLSRecursiveTimeout = 10 * time.Minute
)

// systemRootTool resolves a Windows system tool to its absolute path under
// %SystemRoot%\System32 so the sandbox never invokes a PATH-resolved binary
// (which an attacker who controls the workspace or environment could shadow).
// It falls back to the bare name only when the expected file is missing, so
// oddly-configured hosts still function instead of failing outright.
func systemRootTool(name string) string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = os.Getenv("windir")
	}
	if root == "" {
		root = `C:\Windows`
	}
	full := filepath.Join(root, "System32", name)
	if _, err := os.Stat(full); err == nil {
		return full
	}
	return name
}

// windowsICACLSTimeout returns the configured icacls timeout, honoring
// WINDOWS_SANDBOX_ICACLS_TIMEOUT_MS when set to a positive value and otherwise
// falling back to the caller-provided default.
func windowsICACLSTimeout(fallback time.Duration) time.Duration {
	if raw := os.Getenv("WINDOWS_SANDBOX_ICACLS_TIMEOUT_MS"); raw != "" {
		if ms, err := strconv.ParseUint(raw, 10, 63); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return fallback
}

// icaclsTimeoutForArgs picks the recursive budget whenever the invocation
// includes `/T`, so any recursive operation automatically gets the larger
// ceiling without every call site having to know.
func icaclsTimeoutForArgs(args []string) time.Duration {
	for _, a := range args {
		if strings.EqualFold(a, "/T") {
			return windowsICACLSTimeout(defaultICACLSRecursiveTimeout)
		}
	}
	return windowsICACLSTimeout(defaultICACLSTimeout)
}

func icacls(root string, args ...string) error {
	all := append([]string{root}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), icaclsTimeoutForArgs(args))
	defer cancel()
	cmd := hiddenWindowsSystemCommandContext(ctx, "icacls.exe", all...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("icacls %q %s: %w", root, strings.Join(args, " "), err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("icacls %q %s: %w: %s", root, strings.Join(args, " "), err, strings.TrimSpace(out.String()))
		}
		return nil
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = hiddenWindowsSystemCommand("taskkill.exe", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		if ctx.Err() != nil {
			return fmt.Errorf("icacls %q %s: %w", root, strings.Join(args, " "), ctx.Err())
		}
		return fmt.Errorf("icacls %q %s: timed out", root, strings.Join(args, " "))
	}
}

func hiddenWindowsSystemCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, systemRootTool(name), args...)
	proc.HideWindow(cmd)
	return cmd
}

func hiddenWindowsSystemCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(systemRootTool(name), args...)
	proc.HideWindow(cmd)
	return cmd
}

func setLowIntegrity(root string) error {
	if err := icacls(root, "/setintegritylevel", "L", "/T", "/C"); err != nil {
		return fmt.Errorf("set low integrity label %q: %w", root, err)
	}
	return nil
}

func grantAppContainerSIDs(root string, sidStrs []string, perm string) error {
	mask, err := windowsACLAccessMask(perm)
	if err != nil {
		return err
	}
	return applyWindowsACLEntries(root, sidStrs, windows.GRANT_ACCESS, mask, true)
}

func denyAppContainerSIDs(root string, sidStrs []string, perm string) error {
	return denyAppContainerSIDsWithInheritance(root, sidStrs, perm, true)
}

func denyAppContainerSIDsWithInheritance(root string, sidStrs []string, perm string, includeInheritance bool) error {
	mask, err := windowsACLAccessMask(perm)
	if err != nil {
		return err
	}
	return applyWindowsACLEntries(root, sidStrs, windows.DENY_ACCESS, mask, includeInheritance)
}

func windowsACLAccessMask(perm string) (windows.ACCESS_MASK, error) {
	switch perm {
	case "F":
		return windows.ACCESS_MASK(windows.GENERIC_ALL), nil
	case "RX":
		return windows.ACCESS_MASK(windows.GENERIC_READ | windows.GENERIC_EXECUTE), nil
	default:
		return 0, fmt.Errorf("unsupported Windows ACL permission %q", perm)
	}
}

func applyWindowsACLEntries(root string, sidStrs []string, mode windows.ACCESS_MODE, mask windows.ACCESS_MASK, includeInheritance bool) error {
	entries := make([]windows.EXPLICIT_ACCESS, 0, len(sidStrs)*2)
	parsedSIDs := make([]*windows.SID, 0, len(sidStrs))
	for _, sidStr := range sidStrs {
		sid, err := windows.StringToSid(sidStr)
		if err != nil {
			return fmt.Errorf("parse SID %q: %w", sidStr, err)
		}
		parsedSIDs = append(parsedSIDs, sid)
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: mask,
			AccessMode:        mode,
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_UNKNOWN,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		})
		if includeInheritance {
			entries = append(entries, windows.EXPLICIT_ACCESS{
				AccessPermissions: mask,
				AccessMode:        mode,
				Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
				Trustee: windows.TRUSTEE{
					TrusteeForm:  windows.TRUSTEE_IS_SID,
					TrusteeType:  windows.TRUSTEE_IS_UNKNOWN,
					TrusteeValue: windows.TrusteeValueFromSID(sid),
				},
			})
		}
	}
	sd, err := windows.GetNamedSecurityInfo(root, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("get DACL %q: %w", root, err)
	}
	var oldDACL *windows.ACL
	if sd != nil {
		oldDACL, _, err = sd.DACL()
		if err != nil && !errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) {
			return fmt.Errorf("read DACL %q: %w", root, err)
		}
	}
	acl, err := windows.ACLFromEntries(entries, oldDACL)
	runtime.KeepAlive(parsedSIDs)
	if err != nil {
		return fmt.Errorf("build DACL %q: %w", root, err)
	}
	if err := windows.SetNamedSecurityInfo(root, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		return fmt.Errorf("set DACL %q: %w", root, err)
	}
	return nil
}

func removeGrantedAppContainerSIDs(root string, sidStrs []string) {
	for _, sidStr := range sidStrs {
		_ = icacls(root, "/remove:g", "*"+sidStr, "/C")
	}
}

func removeDeniedAppContainerSIDs(root string, sidStrs []string) {
	for _, sidStr := range sidStrs {
		_ = icacls(root, "/remove:d", "*"+sidStr, "/C")
	}
}

func normalizedWindowsRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	seen := map[string]bool{}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		key := strings.ToLower(filepath.Clean(abs))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, abs)
	}
	return out
}

func windowsSandboxTempRoot(spec Spec) (string, func(), error) {
	prefix := spec.TempPrefix
	if strings.TrimSpace(prefix) == "" {
		prefix = "windows-sandbox-"
	}
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", nil, fmt.Errorf("create sandbox temp root: %w", err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

func windowsSandboxEnv(spec Spec, tempRoot string, env []string) []string {
	if env == nil {
		env = os.Environ()
	}
	env = append([]string(nil), env...)
	if tempRoot == "" {
		return env
	}
	return setWindowsEnv(env, map[string]string{
		"TEMP":   tempRoot,
		"TMP":    tempRoot,
		"TMPDIR": tempRoot,
	})
}

func setWindowsEnv(env []string, values map[string]string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(env)+len(values))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			out = append(out, entry)
			continue
		}
		key := strings.ToUpper(name)
		if value, replace := values[key]; replace {
			out = append(out, name+"="+value)
			seen[key] = true
			continue
		}
		out = append(out, entry)
	}
	for key, value := range values {
		if !seen[key] {
			out = append(out, key+"="+value)
		}
	}
	return out
}

func windowsEnvironmentBlock(env []string) ([]uint16, error) {
	if len(env) == 0 {
		return nil, nil
	}
	env = append([]string(nil), env...)
	sort.SliceStable(env, func(i, j int) bool {
		return strings.ToUpper(env[i]) < strings.ToUpper(env[j])
	})
	var block []uint16
	for _, entry := range env {
		if strings.ContainsRune(entry, 0) {
			return nil, fmt.Errorf("environment entry contains NUL")
		}
		u, err := windows.UTF16FromString(entry)
		if err != nil {
			return nil, fmt.Errorf("encode environment entry: %w", err)
		}
		block = append(block, u...)
	}
	block = append(block, 0)
	return block, nil
}

func windowsWritableRoots(spec Spec, extraRoots ...string) []string {
	var dirs []string
	dirs = append(dirs, spec.WritableRoots...)
	dirs = append(dirs, extraRoots...)
	return dedupeWindowsRoots(dirs)
}

func dedupeWindowsRoots(dirs []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		if !dirExists(abs) {
			continue
		}
		key := strings.ToLower(filepath.Clean(abs))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, abs)
	}
	return out
}

func sameWindowsRoot(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// pathExists reports whether path exists at all (file or directory). forbid_read
// entries may point at single files, so directory-only checks would silently
// skip them.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
