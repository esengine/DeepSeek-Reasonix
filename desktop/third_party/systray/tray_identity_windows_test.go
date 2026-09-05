//go:build windows

package systray

import (
	"errors"
	"golang.org/x/sys/windows"
	"testing"
	"unsafe"
)

func TestInitialGUIDFailureFallsBackWithoutDeletingOtherIcon(t *testing.T) {
	previous := shellNotifyIcon
	t.Cleanup(func() { shellNotifyIcon = previous })
	for _, failGUID := range []bool{false, true} {
		t.Run(map[bool]string{false: "signed GUID", true: "GUID rejected"}[failGUID], func(t *testing.T) {
			guid, _ := windows.GUIDFromString("{AF8B2B6E-CF17-43B9-AFB9-B0BF2695D8AC}")
			tray := winTray{useIconGUID: true, iconGUID: guid, nid: &notifyIconData{Flags: 0x21, GuidItem: guid, Wnd: 42, ID: 100}}
			calls := 0
			shellNotifyIcon = func(a ...uintptr) (uintptr, uintptr, error) {
				if a[0] != 0 {
					t.Fatalf("registration attempted operation %d, must not delete", a[0])
				}
				n := (*notifyIconData)(unsafe.Pointer(a[1]))
				calls++
				if calls == 1 && failGUID {
					return 0, 0, errors.New("GUID belongs to another path")
				}
				wantGUID := !failGUID
				if (n.Flags&0x20 != 0) != wantGUID || (n.GuidItem != windows.GUID{}) != wantGUID {
					t.Fatalf("wrong registration identity: %+v", n)
				}
				if n.Wnd != 42 || n.ID != 100 {
					t.Fatal("lost window identity")
				}
				return 1, 0, nil
			}
			if err := tray.addInitialIcon(); err != nil {
				t.Fatal(err)
			}
			if tray.useIconGUID == failGUID {
				t.Fatal("mode not retained")
			}
			// Explorer's taskbar-created handler re-adds the retained notification data.
			if err := tray.nid.add(); err != nil {
				t.Fatal(err)
			}
			if failGUID && calls != 3 || !failGUID && calls != 2 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}
