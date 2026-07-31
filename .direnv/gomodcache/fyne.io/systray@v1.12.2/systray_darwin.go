//go:build !ios

package systray

/*
#cgo darwin CFLAGS: -DDARWIN -x objective-c -fobjc-arc
#cgo darwin LDFLAGS: -framework Cocoa

#include <stdbool.h>
#include "systray.h"

void setInternalLoop(bool);
*/
import "C"

import (
	"fmt"
	"os"
	"unsafe"
)

// SetTemplateIcon sets the systray icon as a template icon (on Mac), falling back
// to a regular icon on other platforms.
// templateIconBytes and regularIconBytes should be the content of .ico for windows and
// .ico/.jpg/.png for other platforms.
func SetTemplateIcon(templateIconBytes []byte, regularIconBytes []byte) {
	cstr := (*C.char)(unsafe.Pointer(&templateIconBytes[0]))
	C.setIcon(cstr, (C.int)(len(templateIconBytes)), true)
}

// SetIcon sets the icon of a menu item. Only works on macOS and Windows.
// iconBytes should be the content of .ico/.jpg/.png
func (item *MenuItem) SetIcon(iconBytes []byte) {
	cstr := (*C.char)(unsafe.Pointer(&iconBytes[0]))
	C.setMenuItemIcon(cstr, (C.int)(len(iconBytes)), C.int(item.id), false)
}

// SetIconFromFilePath sets the icon of a menu item from a file path.
// iconFilePath should be the path to a .ico for windows and .ico/.jpg/.png for other platforms.
func (item *MenuItem) SetIconFromFilePath(iconFilePath string) error {
	iconBytes, err := os.ReadFile(iconFilePath)
	if err != nil {
		return fmt.Errorf("failed to read icon file: %v", err)
	}
	item.SetIcon(iconBytes)
	return nil
}

// SetTemplateIcon sets the icon of a menu item as a template icon (on macOS). On Windows, it
// falls back to the regular icon bytes and on Linux it does nothing.
// templateIconBytes and regularIconBytes should be the content of .ico for windows and
// .ico/.jpg/.png for other platforms.
func (item *MenuItem) SetTemplateIcon(templateIconBytes []byte, regularIconBytes []byte) {
	cstr := (*C.char)(unsafe.Pointer(&templateIconBytes[0]))
	C.setMenuItemIcon(cstr, (C.int)(len(templateIconBytes)), C.int(item.id), true)
}

// SetRemovalAllowed sets whether a user can remove the systray icon or not.
// This is only supported on macOS.
func SetRemovalAllowed(allowed bool) {
	C.setRemovalAllowed((C.bool)(allowed))
}

func registerSystray() {
	C.registerSystray()
}

func nativeLoop() {
	C.nativeLoop()
}

func nativeEnd() {
	C.nativeEnd()
}

func nativeStart() {
	C.nativeStart()
}

func quit() {
	C.quit()
}

func setInternalLoop(internal bool) {
	C.setInternalLoop(C.bool(internal))
}

// SetIcon sets the systray icon.
// iconBytes should be the content of .ico for windows and .ico/.jpg/.png
// for other platforms.
func SetIcon(iconBytes []byte) {
	cstr := (*C.char)(unsafe.Pointer(&iconBytes[0]))
	C.setIcon(cstr, (C.int)(len(iconBytes)), false)
}

// SetIconFromFilePath sets the systray icon from a file path.
// iconFilePath should be the path to a .ico for windows and .ico/.jpg/.png for other platforms.
func SetIconFromFilePath(iconFilePath string) error {
	bytes, err := os.ReadFile(iconFilePath)
	if err != nil {
		return fmt.Errorf("failed to read icon file: %v", err)
	}
	SetIcon(bytes)
	return nil
}

// SetTitle sets the systray title, only available on Mac and Linux.
func SetTitle(title string) {
	C.setTitle(C.CString(title))
}

// SetTooltip sets the systray tooltip to display on mouse hover of the tray icon,
// only available on Mac and Windows.
func SetTooltip(tooltip string) {
	C.setTooltip(C.CString(tooltip))
}

func addOrUpdateMenuItem(item *MenuItem) {
	var disabled C.short
	if item.disabled {
		disabled = 1
	}
	var checked C.short
	if item.checked {
		checked = 1
	}
	var isCheckable C.short
	if item.isCheckable {
		isCheckable = 1
	}
	var parentID uint32 = 0
	if item.parent != nil {
		parentID = item.parent.id
	}
	C.add_or_update_menu_item(
		C.int(item.id),
		C.int(parentID),
		C.CString(item.title),
		C.CString(item.tooltip),
		disabled,
		checked,
		isCheckable,
	)
}

func addSeparator(id uint32, parent uint32) {
	C.add_separator(C.int(id), C.int(parent))
}

func hideMenuItem(item *MenuItem) {
	C.hide_menu_item(
		C.int(item.id),
	)
}

func showMenuItem(item *MenuItem) {
	C.show_menu_item(
		C.int(item.id),
	)
}

func removeMenuItem(item *MenuItem) {
	C.remove_menu_item(
		C.int(item.id),
	)
}

func resetMenu() {
	C.reset_menu()
}

//export systray_left_click
func systray_left_click() {
	if fn := tappedLeft; fn != nil {
		fn()
		return
	}

	C.show_menu()
}

//export systray_right_click
func systray_right_click() {
	if fn := tappedRight; fn != nil {
		fn()
		return
	}

	C.show_menu()
}

//export systray_ready
func systray_ready() {
	systrayReady()
}

//export systray_on_exit
func systray_on_exit() {
	runSystrayExit()
}

//export systray_menu_item_selected
func systray_menu_item_selected(cID C.int) {
	systrayMenuItemSelected(uint32(cID))
}

//export systray_menu_will_open
func systray_menu_will_open() {
	select {
	case TrayOpenedCh <- struct{}{}:
	default:
	}
}
