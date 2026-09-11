//go:build windows

package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"
	"unsafe"

	"fyne.io/systray"
	"golang.org/x/sys/windows"

	"reasonix/desktop/internal/instanceidentity"
)

// Opt-in subprocess acceptance: production signature selection and tray loop,
// checked against Explorer, including independently signed or moved fixtures.
func TestNativeTrayAcceptanceWorker(t *testing.T) {
	marker := os.Getenv("REASONIX_TEST_TRAY_MARKER")
	if marker == "" {
		t.Skip("native acceptance subprocess only")
	}
	ready := make(chan struct{})
	quit := startDesktopTray(func() {
		systray.SetIcon(trayIconBytes)
		systray.SetTooltip("Reasonix isolated acceptance")
		systray.AddMenuItem("Open", "Acceptance menu")
		close(ready)
	}, func() {})
	defer quit()
	select {
	case <-ready:
	case <-time.After(15 * time.Second):
		t.Fatal("tray failed to initialize")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	signed := verifyTraySignature(exe) == nil
	guid, err := windows.GUIDFromString(instanceidentity.TrayGUID(singleInstanceID()))
	if err != nil {
		t.Fatal(err)
	}
	user := windows.NewLazySystemDLL("user32.dll")
	find := user.NewProc("FindWindowExW")
	getPID := user.NewProc("GetWindowThreadProcessId")
	class, _ := windows.UTF16PtrFromString("SystrayClass")
	var hwnd uintptr
	for {
		hwnd, _, _ = find.Call(0, hwnd, uintptr(unsafe.Pointer(class)), 0)
		if hwnd == 0 {
			t.Fatal("tray window not found")
		}
		var pid uint32
		getPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == uint32(os.Getpid()) {
			break
		}
	}
	identifier := struct {
		Size   uint32
		Window uintptr
		ID     uint32
		GUID   windows.GUID
	}{Window: hwnd, ID: 100}
	identifier.Size = uint32(unsafe.Sizeof(identifier))
	if signed {
		identifier.Window = 0
		identifier.ID = 0
		identifier.GUID = guid
	}
	var rect struct{ Left, Top, Right, Bottom int32 }
	query := windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconGetRect")
	result, _, _ := query.Call(uintptr(unsafe.Pointer(&identifier)), uintptr(unsafe.Pointer(&rect)))
	if int32(result) < 0 {
		t.Fatalf("Explorer cannot resolve tray identity: HRESULT %#x (signed=%v)", result, signed)
	}
	data, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "signed": signed, "guid": guid.String(), "rect": rect, "version": version})
	if err := os.WriteFile(marker, data, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker + ".stop"); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("acceptance worker timed out")
}
