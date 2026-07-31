// Package systray is a cross-platform Go library to place an icon and menu in the notification area.
package systray

import (
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
)

var (
	systrayReady, systrayExit func()
	tappedLeft, tappedRight   func()
	systrayExitCalled         bool
	menuItems                 = make(map[uint32]*MenuItem)
	menuItemsLock             sync.RWMutex

	initialMenuBuilt sync.WaitGroup
	currentID        atomic.Uint32
	quitOnce         sync.Once

	// TrayOpenedCh receives an entry each time the system tray menu is opened.
	TrayOpenedCh = make(chan struct{})
)

// This helper function allows us to call systrayExit only once,
// without accidentally calling it twice in the same lifetime.
func runSystrayExit() {
	if !systrayExitCalled {
		systrayExitCalled = true
		systrayExit()
	}
}

func init() {
	runtime.LockOSThread()
}

// MenuItem is used to keep track each menu item of systray.
// Don't create it directly, use the one systray.AddMenuItem() returned
type MenuItem struct {
	// ClickedCh is the channel which will be notified when the menu item is clicked
	ClickedCh chan struct{}

	// id uniquely identify a menu item, not supposed to be modified
	id uint32
	// title is the text shown on menu item
	title string
	// tooltip is the text shown when pointing to menu item
	tooltip string
	// disabled menu item is grayed out and has no effect when clicked
	disabled bool
	// checked menu item has a tick before the title
	checked bool
	// has the menu item a checkbox (Linux)
	isCheckable bool
	// parent item, for sub menus
	parent *MenuItem
}

func (item *MenuItem) String() string {
	if item.parent == nil {
		return fmt.Sprintf("MenuItem[%d, %q]", item.id, item.title)
	}
	return fmt.Sprintf("MenuItem[%d, parent %d, %q]", item.id, item.parent.id, item.title)
}

// newMenuItem returns a populated MenuItem object
func newMenuItem(title string, tooltip string, parent *MenuItem) *MenuItem {
	item := &MenuItem{
		ClickedCh:   make(chan struct{}),
		id:          currentID.Add(1),
		title:       title,
		tooltip:     tooltip,
		disabled:    false,
		checked:     false,
		isCheckable: false,
		parent:      parent,
	}

	menuItemsLock.Lock()
	menuItems[item.id] = item
	menuItemsLock.Unlock()

	return item
}

// Run initializes GUI and starts the event loop, then invokes the onReady
// callback. It blocks until systray.Quit() is called.
func Run(onReady, onExit func()) {
	setInternalLoop(true)
	Register(onReady, onExit)

	nativeLoop()
}

// RunWithExternalLoop allows the system tray module to operate with other toolkits.
// The returned start and end functions should be called by the toolkit when the application has started and will end.
func RunWithExternalLoop(onReady, onExit func()) (start, end func()) {
	Register(onReady, onExit)

	return nativeStart, func() {
		nativeEnd()
		Quit()
	}
}

// Register initializes GUI and registers the callbacks but relies on the
// caller to run the event loop somewhere else. It's useful if the program
// needs to show other UI elements, for example, webview.
// To overcome some OS weirdness, On macOS versions before Catalina, calling
// this does exactly the same as Run().
func Register(onReady func(), onExit func()) {
	if onReady == nil {
		systrayReady = func() {}
	} else {
		// Run onReady on separate goroutine to avoid blocking event loop
		readyCh := make(chan interface{})
		initialMenuBuilt.Add(1)
		go func() {
			<-readyCh
			onReady()
			initialMenuBuilt.Done()
		}()
		systrayReady = func() {
			close(readyCh)
		}
	}
	// unlike onReady, onExit runs in the event loop to make sure it has time to
	// finish before the process terminates
	if onExit == nil {
		onExit = func() {}
	}
	systrayExit = onExit
	systrayExitCalled = false
	registerSystray()
}

// ResetMenu will remove all menu items
func ResetMenu() {
	menuItemsLock.Lock()
	id := currentID.Load()
	items := make([]*MenuItem, 0, len(menuItems))
	for _, item := range menuItems {
		items = append(items, item)
	}
	menuItemsLock.Unlock()
	for _, item := range items {
		if item.id <= id && item.parent == nil {
			item.Remove()
		}
	}
	resetMenu()
}

// Quit the systray
func Quit() {
	quitOnce.Do(quit)
}

func SetOnTapped(f func()) {
	tappedLeft = f
}

func SetOnSecondaryTapped(f func()) {
	tappedRight = f
}

// AddMenuItem adds a menu item with the designated title and tooltip.
// It can be safely invoked from different goroutines.
// Created menu items are checkable on Windows and OSX by default. For Linux you have to use AddMenuItemCheckbox
func AddMenuItem(title string, tooltip string) *MenuItem {
	item := newMenuItem(title, tooltip, nil)
	item.update()
	return item
}

// AddMenuItemCheckbox adds a menu item with the designated title and tooltip and a checkbox for Linux.
// On other platforms there will be a check indicated next to the item if `checked` is true.
// It can be safely invoked from different goroutines.
func AddMenuItemCheckbox(title string, tooltip string, checked bool) *MenuItem {
	item := newMenuItem(title, tooltip, nil)
	item.isCheckable = true
	item.checked = checked
	item.update()
	return item
}

// AddSeparator adds a separator bar to the menu
func AddSeparator() {
	addSeparator(currentID.Add(1), 0)
}

// AddSeparator adds a separator bar to the submenu
func (item *MenuItem) AddSeparator() {
	addSeparator(currentID.Add(1), item.id)
}

// AddSubMenuItem adds a nested sub-menu item with the designated title and tooltip.
// It can be safely invoked from different goroutines.
// Created menu items are checkable on Windows and OSX by default. For Linux you have to use AddSubMenuItemCheckbox
func (item *MenuItem) AddSubMenuItem(title string, tooltip string) *MenuItem {
	child := newMenuItem(title, tooltip, item)
	child.update()
	return child
}

// AddSubMenuItemCheckbox adds a nested sub-menu item with the designated title and tooltip and a checkbox for Linux.
// It can be safely invoked from different goroutines.
// On Windows and OSX this is the same as calling AddSubMenuItem
func (item *MenuItem) AddSubMenuItemCheckbox(title string, tooltip string, checked bool) *MenuItem {
	child := newMenuItem(title, tooltip, item)
	child.isCheckable = true
	child.checked = checked
	child.update()
	return child
}

// SetTitle set the text to display on a menu item
func (item *MenuItem) SetTitle(title string) {
	item.title = title
	item.update()
}

// SetTooltip set the tooltip to show when mouse hover
func (item *MenuItem) SetTooltip(tooltip string) {
	item.tooltip = tooltip
	item.update()
}

// Disabled checks if the menu item is disabled
func (item *MenuItem) Disabled() bool {
	return item.disabled
}

// Enable a menu item regardless if it's previously enabled or not
func (item *MenuItem) Enable() {
	item.disabled = false
	item.update()
}

// Disable a menu item regardless if it's previously disabled or not
func (item *MenuItem) Disable() {
	item.disabled = true
	item.update()
}

// Hide hides a menu item
func (item *MenuItem) Hide() {
	hideMenuItem(item)
}

// Remove removes a menu item
func (item *MenuItem) Remove() {
	menuItemsLock.RLock()
	var childList []*MenuItem
	for _, child := range menuItems {
		if child.parent == item {
			childList = append(childList, child)
		}
	}
	menuItemsLock.RUnlock()
	for _, child := range childList {
		child.Remove()
	}
	removeMenuItem(item)
	menuItemsLock.Lock()
	defer menuItemsLock.Unlock()
	delete(menuItems, item.id)
	if item.ClickedCh == nil {
		return
	}
	select {
	case _, ok := <-item.ClickedCh:
		if !ok {
			return
		}
	default:
	}
	close(item.ClickedCh)
}

// Show shows a previously hidden menu item
func (item *MenuItem) Show() {
	showMenuItem(item)
}

// Checked returns if the menu item has a check mark
func (item *MenuItem) Checked() bool {
	return item.checked
}

// Check a menu item regardless if it's previously checked or not
func (item *MenuItem) Check() {
	item.checked = true
	item.update()
}

// Uncheck a menu item regardless if it's previously unchecked or not
func (item *MenuItem) Uncheck() {
	item.checked = false
	item.update()
}

// update propagates changes on a menu item to systray
func (item *MenuItem) update() {
	menuItemsLock.Lock()
	_, exists := menuItems[item.id]
	menuItemsLock.Unlock()

	if !exists {
		return
	}
	addOrUpdateMenuItem(item)
}

func systrayMenuItemSelected(id uint32) {
	menuItemsLock.RLock()
	item, ok := menuItems[id]
	menuItemsLock.RUnlock()
	if !ok {
		log.Printf("systray error: no menu item with ID %d\n", id)
		return
	}
	select {
	case item.ClickedCh <- struct{}{}:
	// in case no one waiting for the channel
	default:
	}
}
