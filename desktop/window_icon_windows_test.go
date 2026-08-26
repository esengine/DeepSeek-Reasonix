//go:build windows

package main

import (
	"sync/atomic"
	"testing"
)

type windowIconCall struct {
	hwnd, selector, icon uintptr
}

func TestApplyWindowIconsFromExecutableRetriesUntilWindowAppears(t *testing.T) {
	var findCalls atomic.Int32
	var assigned atomic.Int32

	find := func() uintptr {
		findCalls.Add(1)
		if findCalls.Load() < 3 {
			return 0 // window not ready on first two attempts
		}
		return 0x1234
	}
	assign := func(hwnd uintptr) {
		if hwnd != 0x1234 {
			t.Errorf("assignWindowIcons got hwnd 0x%X, want 0x1234", hwnd)
		}
		assigned.Add(1)
	}

	oldFind, oldAssign := windowIconFindWindow, windowIconAssignWindow
	windowIconFindWindow, windowIconAssignWindow = find, assign
	defer func() {
		windowIconFindWindow, windowIconAssignWindow = oldFind, oldAssign
	}()

	applyWindowIconsFromExecutable()

	if assigned.Load() != 1 {
		t.Fatalf("assignWindowIcons called %d times, want exactly 1", assigned.Load())
	}
	if findCalls.Load() != 3 {
		t.Fatalf("findWindow called %d times, want 3 (two misses then hit)", findCalls.Load())
	}
}

func TestAssignWindowIconsSkipsWhenNoIconLoaded(t *testing.T) {
	oldLoad, oldSend, oldSetClass := windowIconLoadIcons, windowIconSendMessage, windowIconSetClassLong
	windowIconLoadIcons = func() (uintptr, uintptr) { return 0, 0 }
	windowIconSendMessage = func(uintptr, uintptr, uintptr, uintptr) {
		t.Fatal("SendMessageW called without a loaded icon")
	}
	windowIconSetClassLong = func(uintptr, uintptr, uintptr) {
		t.Fatal("SetClassLongPtrW called without a loaded icon")
	}
	t.Cleanup(func() {
		windowIconLoadIcons, windowIconSendMessage, windowIconSetClassLong = oldLoad, oldSend, oldSetClass
	})

	assignWindowIcons(0xDEAD)
}

func TestAssignWindowIconsUsesWin32BigAndSmallSelectors(t *testing.T) {
	oldLoad, oldSend, oldSetClass := windowIconLoadIcons, windowIconSendMessage, windowIconSetClassLong
	t.Cleanup(func() {
		windowIconLoadIcons, windowIconSendMessage, windowIconSetClassLong = oldLoad, oldSend, oldSetClass
	})

	windowIconLoadIcons = func() (uintptr, uintptr) { return 0xAAAA, 0xBBBB }
	var messages []windowIconCall
	windowIconSendMessage = func(hwnd, message, selector, icon uintptr) {
		if message != wmSetIcon {
			t.Fatalf("SendMessageW message = 0x%X, want WM_SETICON", message)
		}
		messages = append(messages, windowIconCall{hwnd: hwnd, selector: selector, icon: icon})
	}
	var classIcons []windowIconCall
	windowIconSetClassLong = func(hwnd, selector, icon uintptr) {
		classIcons = append(classIcons, windowIconCall{hwnd: hwnd, selector: selector, icon: icon})
	}

	assignWindowIcons(0x1234)

	wantMessages := []windowIconCall{
		{hwnd: 0x1234, selector: 1, icon: 0xAAAA}, // ICON_BIG
		{hwnd: 0x1234, selector: 0, icon: 0xBBBB}, // ICON_SMALL
	}
	assertWindowIconCalls(t, "window messages", messages, wantMessages)
	wantClassIcons := []windowIconCall{
		{hwnd: 0x1234, selector: gclpHicon, icon: 0xAAAA},
		{hwnd: 0x1234, selector: gclpHiconSm, icon: 0xBBBB},
	}
	assertWindowIconCalls(t, "class icons", classIcons, wantClassIcons)
}

func assertWindowIconCalls(t *testing.T, label string, got, want []windowIconCall) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s count = %d, want %d: %#v", label, len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %#v, want %#v", label, i, got[i], want[i])
		}
	}
}
