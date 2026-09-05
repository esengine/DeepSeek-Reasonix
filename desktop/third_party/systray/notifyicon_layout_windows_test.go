//go:build windows

package systray

import (
	"testing"
	"unsafe"
)

func TestNotifyIconDataMatchesWindowsABI(t *testing.T) {
	// shellapi.h declares uTimeout/uVersion as a single UINT union. A second
	// UINT shifts guidItem even when mocked Shell_NotifyIcon calls succeed.
	var n notifyIconData
	wantSize, wantGUID, wantBalloon := uintptr(956), uintptr(936), uintptr(952)
	if unsafe.Sizeof(n.Wnd) == 8 {
		wantSize, wantGUID, wantBalloon = 976, 952, 968
	}
	if got := unsafe.Offsetof(n.InfoTitle) - unsafe.Offsetof(n.TimeoutOrVersion); got != 4 {
		t.Fatalf("timeout/version union occupies %d bytes, want 4", got)
	}
	if unsafe.Sizeof(n) != wantSize || unsafe.Offsetof(n.GuidItem) != wantGUID || unsafe.Offsetof(n.BalloonIcon) != wantBalloon {
		t.Fatalf("native layout: size=%d GUID=%d balloon=%d; want %d/%d/%d", unsafe.Sizeof(n), unsafe.Offsetof(n.GuidItem), unsafe.Offsetof(n.BalloonIcon), wantSize, wantGUID, wantBalloon)
	}
}
