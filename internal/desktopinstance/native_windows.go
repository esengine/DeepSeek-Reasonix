//go:build windows

package desktopinstance

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var user32 = windows.NewLazySystemDLL("user32.dll")

type process struct {
	pid           uint32
	parent        uint32
	handle        windows.Handle
	image         string
	created       windows.Filetime
	status        *Status
	legacyProfile string
}

func canonical(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func sameUser(handle windows.Handle) (bool, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(handle, windows.TOKEN_QUERY, &token); err != nil {
		return false, err
	}
	defer token.Close()
	theirs, err := token.GetTokenUser()
	if err != nil {
		return false, err
	}
	ours, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false, err
	}
	return theirs.User.Sid.Equals(ours.User.Sid), nil
}

func openProcess(pid, parent uint32) (*process, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return nil, err
	}
	p := &process{pid: pid, parent: parent, handle: h}
	ok := false
	defer func() {
		if !ok {
			windows.CloseHandle(h)
		}
	}()
	own, err := sameUser(h)
	if err != nil {
		return nil, fmt.Errorf("cannot verify process user: %w", err)
	}
	if !own {
		return nil, errors.New("cannot verify process user")
	}
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(h, 0, &buffer[0], &size); err != nil {
		return nil, err
	}
	p.image, err = canonical(windows.UTF16ToString(buffer[:size]))
	if err != nil {
		return nil, err
	}
	var exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &p.created, &exited, &kernel, &user); err != nil {
		return nil, err
	}
	if !p.alive() {
		return nil, errors.New("process exited during inspection")
	}
	ok = true
	return p, nil
}

func (p *process) alive() bool {
	result, err := windows.WaitForSingleObject(p.handle, 0)
	return err == nil && result == uint32(windows.WAIT_TIMEOUT)
}
func (p *process) close() { windows.CloseHandle(p.handle) }

func ordinaryProduct(image string) bool {
	size, err := windows.GetFileVersionInfoSize(image, nil)
	if err != nil || size == 0 || size > 1024*1024 {
		return false
	}
	data := make([]byte, size)
	if windows.GetFileVersionInfo(image, 0, size, unsafe.Pointer(&data[0])) != nil {
		return false
	}
	var translations *uint16
	var count uint32
	if windows.VerQueryValue(unsafe.Pointer(&data[0]), `\VarFileInfo\Translation`, unsafe.Pointer(&translations), &count) != nil || count < 4 {
		return false
	}
	values := unsafe.Slice(translations, int(count/2))
	for i := 0; i+1 < len(values); i += 2 {
		var value *uint16
		var length uint32
		key := fmt.Sprintf(`\StringFileInfo\%04x%04x\ProductName`, values[i], values[i+1])
		if windows.VerQueryValue(unsafe.Pointer(&data[0]), key, unsafe.Pointer(&value), &length) == nil && value != nil && length > 0 {
			name := windows.UTF16ToString(unsafe.Slice(value, int(length)))
			runtime.KeepAlive(data)
			return name == "Reasonix"
		}
	}
	return false
}

func (p *process) terminate() error {
	if !p.alive() {
		return nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, p.pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return err
	}
	if created != p.created || !p.alive() {
		return outcome(UnknownOwner, "process identity changed")
	}
	return windows.TerminateProcess(h, 1)
}

func readStatus(p *process) (Status, error) {
	var zero Status
	name, _ := windows.UTF16PtrFromString(fmt.Sprintf(`\\.\pipe\reasonix-shell-v1-%d`, p.pid))
	pipe, err := windows.CreateFile(name, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	connectDeadline := time.Now().Add(2 * time.Second)
	for errors.Is(err, windows.ERROR_PIPE_BUSY) && time.Now().Before(connectDeadline) {
		time.Sleep(20 * time.Millisecond)
		pipe, err = windows.CreateFile(name, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	}
	if err != nil {
		return zero, err
	}
	defer windows.CloseHandle(pipe)
	var owner uint32
	if err := windows.GetNamedPipeServerProcessId(pipe, &owner); err != nil {
		return zero, err
	}
	if owner != p.pid || !p.alive() {
		return zero, outcome(UnknownOwner, "status pipe owner changed")
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return zero, err
	}
	defer windows.CloseHandle(event)
	data := make([]byte, 0, StatusLimit+1)
	deadline := time.Now().Add(2 * time.Second)
	for len(data) <= StatusLimit {
		buffer := make([]byte, StatusLimit+1-len(data))
		var n uint32
		if err := windows.ResetEvent(event); err != nil {
			return zero, err
		}
		ov := windows.Overlapped{HEvent: event}
		err = windows.ReadFile(pipe, buffer, &n, &ov)
		if errors.Is(err, windows.ERROR_IO_PENDING) {
			remaining := max(time.Until(deadline), 0)
			wait, waitErr := windows.WaitForSingleObject(event, uint32(remaining.Milliseconds()))
			if waitErr != nil || wait != windows.WAIT_OBJECT_0 {
				_ = windows.CancelIoEx(pipe, &ov)
				_, _ = windows.WaitForSingleObject(event, windows.INFINITE)
				return zero, errors.New("shell status read timeout")
			}
			err = windows.GetOverlappedResult(pipe, &ov, &n, false)
		}
		if n > 0 {
			data = append(data, buffer[:n]...)
		}
		if bytes.Contains(data, []byte{'\n'}) {
			break
		}
		if err != nil {
			return zero, err
		}
		if n == 0 {
			return zero, errors.New("empty shell status")
		}
	}
	if !p.alive() {
		return zero, errors.New("shell exited during status read")
	}
	return DecodeStatus(bytes.TrimSpace(data), p.pid)
}

func processList() ([]windows.ProcessEntry32, error) {
	h, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	var entries []windows.ProcessEntry32
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(h, &e); err == nil; err = windows.Process32Next(h, &e) {
		entries = append(entries, e)
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return entries, nil
}

func messageProfile(pid uint32) string {
	class, _ := windows.UTF16PtrFromString("Chrome_MessageWindow")
	find := user32.NewProc("FindWindowExW")
	var after uintptr
	for {
		hwnd, _, _ := find.Call(^uintptr(2), after, uintptr(unsafe.Pointer(class)), 0)
		if hwnd == 0 {
			return ""
		}
		after = hwnd
		var owner uint32
		user32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner != pid {
			continue
		}
		text := make([]uint16, 32768)
		n, _, _ := user32.NewProc("GetWindowTextW").Call(hwnd, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
		if n > 0 {
			return windows.UTF16ToString(text[:n])
		}
	}
}

func closeWindows(p *process) {
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var owner uint32
		user32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner == p.pid && p.alive() {
			user32.NewProc("PostMessageW").Call(hwnd, 0x0010, 0, 0)
		}
		return 1
	})
	user32.NewProc("EnumWindows").Call(callback, 0)
}

func focusLegacyWindow(p *process) bool {
	shown := false
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var owner uint32
		user32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner != p.pid || !p.alive() {
			return 1
		}
		n, _, _ := user32.NewProc("GetWindowTextLengthW").Call(hwnd)
		if n == 0 {
			return 1
		}
		user32.NewProc("ShowWindow").Call(hwnd, 9) // SW_RESTORE also unhides tray windows.
		user32.NewProc("SetForegroundWindow").Call(hwnd)
		shown = true
		return 0
	})
	user32.NewProc("EnumWindows").Call(callback, 0)
	return shown
}

func lockInstall(root string) (func(), error) {
	token, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	key := ProfileKey(root + "|" + token.User.Sid.String())
	name, _ := windows.UTF16PtrFromString(`Local\Reasonix-Recovery-` + key)
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, err
	}
	runtime.LockOSThread()
	result, err := windows.WaitForSingleObject(h, 120000)
	if err != nil || (result != windows.WAIT_OBJECT_0 && result != windows.WAIT_ABANDONED) {
		windows.CloseHandle(h)
		runtime.UnlockOSThread()
		return nil, outcome(ExitTimeout, "another install or recovery is still running")
	}
	return func() { _ = windows.ReleaseMutex(h); windows.CloseHandle(h); runtime.UnlockOSThread() }, nil
}

func Notify(err error) {
	title, _ := windows.UTF16PtrFromString("Reasonix 启动 / Startup")
	text, _ := windows.UTF16PtrFromString("Reasonix 未能完成启动或更新，请查看日志后重试。\nReasonix could not finish startup or update.\n\n" + err.Error())
	user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x30)
}

func confirmProcesses(list []*process) bool {
	var text strings.Builder
	text.WriteString("旧版 Reasonix 尚未退出。结束进程可能丢失未保存内容。\n\nEnd these old Reasonix processes and continue? Unsaved work may be lost.\n")
	for _, p := range list {
		fmt.Fprintf(&text, "\nPID %d: %s", p.pid, p.image)
	}
	title, _ := windows.UTF16PtrFromString("Reasonix 恢复 / Recovery")
	body, _ := windows.UTF16PtrFromString(text.String())
	// Label the standard dialog's buttons explicitly; the negative action is
	// still IDNO and remains the default even on non-Chinese Windows systems.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	continueText, _ := windows.UTF16PtrFromString("结束旧进程并继续")
	cancelText, _ := windows.UTF16PtrFromString("取消")
	callback := syscall.NewCallback(func(code int32, hwnd, param uintptr) uintptr {
		if code == 5 { // HCBT_ACTIVATE
			user32.NewProc("SetDlgItemTextW").Call(hwnd, 6, uintptr(unsafe.Pointer(continueText)))
			user32.NewProc("SetDlgItemTextW").Call(hwnd, 7, uintptr(unsafe.Pointer(cancelText)))
		}
		next, _, _ := user32.NewProc("CallNextHookEx").Call(0, uintptr(code), hwnd, param)
		return next
	})
	hook, _, _ := user32.NewProc("SetWindowsHookExW").Call(5, callback, 0, uintptr(windows.GetCurrentThreadId()))
	if hook == 0 {
		return false
	}
	defer user32.NewProc("UnhookWindowsHookEx").Call(hook)
	result, _, _ := user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), 0x4|0x30|0x100)
	return result == 6
}

func inspect(root, profile string, all bool) ([]*process, error) {
	entries, err := processList()
	if err != nil {
		return nil, err
	}
	var found []*process
	fail := func(err error) ([]*process, error) {
		for _, p := range found {
			p.close()
		}
		return nil, err
	}
	for _, e := range entries {
		name := strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))
		if name != "reasonix.exe" && name != "reasonix-desktop.exe" {
			continue
		}
		p, err := openProcess(e.ProcessID, e.ParentProcessID)
		if err != nil {
			if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				return fail(outcome(UnknownOwner, "access denied while identifying candidate PID %d", e.ProcessID))
			}
			continue
		}
		role := ImageRole(root, p.image)
		if role != "" && !ordinaryProduct(p.image) {
			p.close()
			return fail(outcome(UnknownOwner, "product identity could not be verified for PID %d", e.ProcessID))
		}
		if name == "reasonix.exe" {
			status, statusErr := readStatus(p)
			if statusErr == nil {
				p.status = &status
				if status.HomeKey == ProfileKey(profile) && role == "" {
					p.close()
					return fail(outcome(OtherInstallation, "another Reasonix installation owns this data home"))
				}
			} else {
				if role != "" && !errors.Is(statusErr, windows.ERROR_FILE_NOT_FOUND) {
					p.close()
					return fail(outcome(UnknownOwner, "shell status could not be verified for PID %d: %v", e.ProcessID, statusErr))
				}
				p.legacyProfile = messageProfile(p.pid)
				if p.legacyProfile != "" {
					if real, err := canonical(p.legacyProfile); err == nil && strings.EqualFold(real, profile) && role == "" {
						p.close()
						return fail(outcome(OtherInstallation, "another legacy Reasonix installation owns this data home"))
					}
				}
			}
		}
		if role == "" {
			p.close()
			continue
		}
		if !all {
			matches := p.status != nil && p.status.HomeKey == ProfileKey(profile)
			if !matches && p.legacyProfile != "" {
				if real, err := canonical(p.legacyProfile); err == nil {
					matches = strings.EqualFold(real, profile)
				}
			}
			if !matches {
				p.close()
				continue
			}
		}
		found = append(found, p)
	}
	return found, nil
}

func preparePaths(root, home string) (string, string, error) {
	root, err := canonical(root)
	if err != nil {
		return "", "", err
	}
	profile := filepath.Join(home, "desktop-shell")
	if err := os.MkdirAll(profile, 0700); err != nil {
		return "", "", err
	}
	profile, err = canonical(profile)
	return root, profile, err
}
