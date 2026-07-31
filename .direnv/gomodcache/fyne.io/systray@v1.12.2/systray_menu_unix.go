//go:build (linux || freebsd || openbsd || netbsd) && !android

package systray

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"fyne.io/systray/internal/generated/menu"
)

// SetIcon sets the icon of a menu item.
// iconBytes should be the content of .ico/.jpg/.png
func (item *MenuItem) SetIcon(iconBytes []byte) {
	instance.menuLock.Lock()
	defer instance.menuLock.Unlock()
	m, exists := findLayout(int32(item.id))
	if exists {
		m.V1["icon-data"] = dbus.MakeVariant(iconBytes)
		emitItemPropertiesUpdated(int32(item.id), m.V1)
		// Property-only change; per spec LayoutUpdated isn't required here,
		// but kept as a fallback for clients that only watch LayoutUpdated.
		refresh()
	}
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

// copyLayout makes full copy of layout
func copyLayout(in *menuLayout, depth int32) *menuLayout {
	out := menuLayout{
		V0: in.V0,
		V1: make(map[string]dbus.Variant, len(in.V1)),
	}
	for k, v := range in.V1 {
		out.V1[k] = v
	}
	if depth != 0 {
		depth--
		out.V2 = make([]dbus.Variant, len(in.V2))
		for i, v := range in.V2 {
			out.V2[i] = dbus.MakeVariant(copyLayout(v.Value().(*menuLayout), depth))
		}
	} else {
		out.V2 = []dbus.Variant{}
	}
	return &out
}

// firstGetLayoutDone tracks whether the initial GetLayout response has been served
// since the last menu reset. libdbusmenu-gtk3 (used by Cinnamon/Xfce/GNOME) requests
// GetGroupProperties for grandchildren before parents in the first call, causing children
// to be silently dropped (blank submenus) because parent GtkMenu containers don't exist
// yet. Returning depth=1 on the first call ensures parents get their GtkMenu containers
// before grandchildren are introduced in subsequent calls, where the order is correct.
// After serving depth=1, a goroutine automatically triggers a second GetLayout cycle so
// submenus populate without requiring user interaction. Multiple goroutines from rapid
// resets are harmless — they just emit extra LayoutUpdated signals.
var firstGetLayoutDone bool

// GetLayout is com.canonical.dbusmenu.GetLayout method.
func (t *tray) GetLayout(parentID int32, recursionDepth int32, propertyNames []string) (revision uint32, layout menuLayout, err *dbus.Error) {
	initialMenuBuilt.Wait()
	instance.menuLock.Lock()
	defer instance.menuLock.Unlock()
	if m, ok := findLayout(parentID); ok {
		depth := recursionDepth
		if !firstGetLayoutDone {
			firstGetLayoutDone = true
			depth = 1
			go func() {
				time.Sleep(150 * time.Millisecond)
				refresh()
			}()
		}
		// return copy of menu layout to prevent panic from cuncurrent access to layout
		return instance.menuVersion, *copyLayout(m, depth), nil
	}
	return
}

// GetGroupProperties is com.canonical.dbusmenu.GetGroupProperties method.
func (t *tray) GetGroupProperties(ids []int32, propertyNames []string) (properties []struct {
	V0 int32
	V1 map[string]dbus.Variant
}, err *dbus.Error) {
	instance.menuLock.Lock()
	defer instance.menuLock.Unlock()
	for _, id := range ids {
		if m, ok := findLayout(id); ok {
			p := struct {
				V0 int32
				V1 map[string]dbus.Variant
			}{
				V0: m.V0,
				V1: make(map[string]dbus.Variant, len(m.V1)),
			}
			for k, v := range m.V1 {
				p.V1[k] = v
			}
			properties = append(properties, p)
		}
	}
	return
}

// GetProperty is com.canonical.dbusmenu.GetProperty method.
func (t *tray) GetProperty(id int32, name string) (value dbus.Variant, err *dbus.Error) {
	instance.menuLock.Lock()
	defer instance.menuLock.Unlock()
	if m, ok := findLayout(id); ok {
		if p, ok := m.V1[name]; ok {
			return p, nil
		}
	}
	return
}

// Event is com.canonical.dbusmenu.Event method.
func (t *tray) Event(id int32, eventID string, data dbus.Variant, timestamp uint32) (err *dbus.Error) {
	switch eventID {
	case "clicked":
		systrayMenuItemSelected(uint32(id))
	case "opened":
		t.menuLock.RLock()
		rootMenuID := t.menu.V0
		t.menuLock.RUnlock()

		if id == rootMenuID {
			select {
			case TrayOpenedCh <- struct{}{}:
			default:
			}
		}
	}
	return
}

// EventGroup is com.canonical.dbusmenu.EventGroup method.
func (t *tray) EventGroup(events []struct {
	V0 int32
	V1 string
	V2 dbus.Variant
	V3 uint32
}) (idErrors []int32, err *dbus.Error) {
	for _, event := range events {
		if event.V1 == "clicked" {
			systrayMenuItemSelected(uint32(event.V0))
		}
	}
	return
}

// AboutToShow is com.canonical.dbusmenu.AboutToShow method.
func (t *tray) AboutToShow(id int32) (needUpdate bool, err *dbus.Error) {
	return
}

// AboutToShowGroup is com.canonical.dbusmenu.AboutToShowGroup method.
func (t *tray) AboutToShowGroup(ids []int32) (updatesNeeded []int32, idErrors []int32, err *dbus.Error) {
	instance.menuLock.RLock()
	defer instance.menuLock.RUnlock()
	for _, id := range ids {
		if m, ok := findLayout(id); ok && len(m.V2) > 0 {
			updatesNeeded = append(updatesNeeded, id)
		}
	}
	return
}

func createMenuPropSpec() map[string]map[string]*prop.Prop {
	instance.menuLock.Lock()
	defer instance.menuLock.Unlock()
	return map[string]map[string]*prop.Prop{
		"com.canonical.dbusmenu": {
			"Version": {
				Value:    instance.menuVersion,
				Writable: true,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"TextDirection": {
				Value:    "ltr",
				Writable: false,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"Status": {
				Value:    "normal",
				Writable: false,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"IconThemePath": {
				Value:    []string{},
				Writable: false,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
		},
	}
}

// menuLayout is a named struct to map into generated bindings. It represents the layout of a menu item
type menuLayout = struct {
	V0 int32                   // the unique ID of this item
	V1 map[string]dbus.Variant // properties for this menu item layout
	V2 []dbus.Variant          // child menu item layouts
}

func addOrUpdateMenuItem(item *MenuItem) {
	var layout *menuLayout
	var parentForChildrenDisplayUpdate *menuLayout
	instance.menuLock.Lock()
	defer instance.menuLock.Unlock()
	m, exists := findLayout(int32(item.id))
	if exists {
		layout = m
	} else {
		layout = &menuLayout{
			V0: int32(item.id),
			V1: map[string]dbus.Variant{},
			V2: []dbus.Variant{},
		}

		parent := instance.menu
		if item.parent != nil {
			m, ok := findLayout(int32(item.parent.id))
			if ok {
				parent = m
				if _, already := parent.V1["children-display"]; !already {
					parent.V1["children-display"] = dbus.MakeVariant("submenu")
					if parent.V0 != 0 {
						parentForChildrenDisplayUpdate = parent
					}
				}
			}
		}
		parent.V2 = append(parent.V2, dbus.MakeVariant(layout))
	}

	applyItemToLayout(item, layout)
	if exists {
		emitItemPropertiesUpdated(int32(item.id), layout.V1)
		// Property-only change on an existing item; per spec LayoutUpdated isn't required here,
		// but kept as a fallback for clients that only watch LayoutUpdated.
		refresh()
	} else {
		// We've added "children-display", that's a property change
		if parentForChildrenDisplayUpdate != nil {
			emitItemPropertiesUpdated(parentForChildrenDisplayUpdate.V0, parentForChildrenDisplayUpdate.V1)
		}
		// New item appended to a parent's children,
		// that's a structural change, so LayoutUpdated signal is required
		refresh()
	}
}

func addSeparator(id uint32, parent uint32) {
	menu, _ := findLayout(int32(parent))

	instance.menuLock.Lock()
	defer instance.menuLock.Unlock()
	layout := &menuLayout{
		V0: int32(id),
		V1: map[string]dbus.Variant{
			"type": dbus.MakeVariant("separator"),
		},
		V2: []dbus.Variant{},
	}
	menu.V2 = append(menu.V2, dbus.MakeVariant(layout))
	refresh()
}

func applyItemToLayout(in *MenuItem, out *menuLayout) {
	out.V1["enabled"] = dbus.MakeVariant(!in.disabled)
	out.V1["label"] = dbus.MakeVariant(in.title)

	if in.isCheckable {
		out.V1["toggle-type"] = dbus.MakeVariant("checkmark")
		if in.checked {
			out.V1["toggle-state"] = dbus.MakeVariant(1)
		} else {
			out.V1["toggle-state"] = dbus.MakeVariant(0)
		}
	} else {
		out.V1["toggle-type"] = dbus.MakeVariant("")
		out.V1["toggle-state"] = dbus.MakeVariant(0)
	}
}

func findLayout(id int32) (*menuLayout, bool) {
	if id == 0 {
		return instance.menu, true
	}
	return findSubLayout(id, instance.menu.V2)
}

func findSubLayout(id int32, vals []dbus.Variant) (*menuLayout, bool) {
	for _, i := range vals {
		item := i.Value().(*menuLayout)
		if item.V0 == id {
			return item, true
		}

		if len(item.V2) > 0 {
			child, ok := findSubLayout(id, item.V2)
			if ok {
				return child, true
			}
		}
	}

	return nil, false
}

func removeSubLayout(id int32, vals []dbus.Variant) ([]dbus.Variant, bool) {
	for idx, i := range vals {
		item := i.Value().(*menuLayout)
		if item.V0 == id {
			return append(vals[:idx], vals[idx+1:]...), true
		}

		if len(item.V2) > 0 {
			if child, removed := removeSubLayout(id, item.V2); removed {
				return child, true
			}
		}
	}

	return vals, false
}

func removeMenuItem(item *MenuItem) {
	instance.menuLock.Lock()
	defer instance.menuLock.Unlock()

	parent := instance.menu
	if item.parent != nil {
		m, ok := findLayout(int32(item.parent.id))
		if !ok {
			return
		}
		parent = m
	}

	if items, removed := removeSubLayout(int32(item.id), parent.V2); removed {
		parent.V2 = items
		refresh()
	}
}

func hideMenuItem(item *MenuItem) {
	instance.menuLock.Lock()
	defer instance.menuLock.Unlock()
	m, exists := findLayout(int32(item.id))
	if exists {
		m.V1["visible"] = dbus.MakeVariant(false)
		emitItemPropertiesUpdated(int32(item.id), m.V1)
		// Property-only change; per spec LayoutUpdated isn't required here,
		// but kept as a fallback for clients that only watch LayoutUpdated.
		refresh()
	}
}

func showMenuItem(item *MenuItem) {
	instance.menuLock.Lock()
	defer instance.menuLock.Unlock()
	m, exists := findLayout(int32(item.id))
	if exists {
		m.V1["visible"] = dbus.MakeVariant(true)
		emitItemPropertiesUpdated(int32(item.id), m.V1)
		// Property-only change; per spec LayoutUpdated isn't required here,
		// but kept as a fallback for clients that only watch LayoutUpdated.
		refresh()
	}
}

// emitItemPropertiesUpdated emits the com.canonical.dbusmenu.ItemsPropertiesUpdated
// signal so desktop clients refresh per-item state (label, enabled, toggle-state,
// visible, icon-data) without re-querying the whole layout.
func emitItemPropertiesUpdated(id int32, props map[string]dbus.Variant) {
	instance.lock.Lock()
	conn := instance.conn
	instance.lock.Unlock()
	if conn == nil {
		return
	}
	err := menu.Emit(conn, &menu.Dbusmenu_ItemsPropertiesUpdatedSignal{
		Path: menuPath,
		Body: &menu.Dbusmenu_ItemsPropertiesUpdatedSignalBody{
			UpdatedProps: []struct {
				V0 int32
				V1 map[string]dbus.Variant
			}{{V0: id, V1: props}},
		},
	})
	if err != nil {
		log.Printf("systray error: failed to emit items properties updated signal: %v\n", err)
	}
}

func refresh() {
	instance.lock.Lock()
	defer instance.lock.Unlock()
	if instance.conn == nil || instance.menuProps == nil {
		return
	}
	instance.menuVersion++
	dbusErr := instance.menuProps.Set("com.canonical.dbusmenu", "Version",
		dbus.MakeVariant(instance.menuVersion))
	if dbusErr != nil {
		log.Printf("systray error: failed to update menu version: %v\n", dbusErr)
		return
	}
	err := menu.Emit(instance.conn, &menu.Dbusmenu_LayoutUpdatedSignal{
		Path: menuPath,
		Body: &menu.Dbusmenu_LayoutUpdatedSignalBody{
			Revision: instance.menuVersion,
		},
	})
	if err != nil {
		log.Printf("systray error: failed to emit layout updated signal: %v\n", err)
	}
}

func resetMenu() {
	instance.menuLock.Lock()
	defer instance.menuLock.Unlock()
	instance.menu = &menuLayout{}
	instance.menuVersion++
	firstGetLayoutDone = false
	refresh()
}
