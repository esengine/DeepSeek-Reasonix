//go:build (linux || freebsd || openbsd || netbsd) && !android

//Note that you need to have github.com/knightpp/dbus-codegen-go installed from "custom" branch
//go:generate dbus-codegen-go -prefix org.kde -package notifier -output internal/generated/notifier/status_notifier_item.go internal/StatusNotifierItem.xml
//go:generate dbus-codegen-go -prefix com.canonical -package menu -output internal/generated/menu/dbus_menu.go internal/DbusMenu.xml

package systray

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png" // used only here
	"log"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"

	"fyne.io/systray/internal/generated/menu"
	"fyne.io/systray/internal/generated/notifier"
)

const (
	path     = "/StatusNotifierItem"
	menuPath = "/StatusNotifierItem/menu"
)

var (
	// to signal quitting the internal main loop
	quitChan = make(chan struct{})

	// instance is the current instance of our DBus tray server
	instance = &tray{menu: &menuLayout{}, menuVersion: 1}
)

// SetTemplateIcon sets the systray icon as a template icon (on macOS), falling back
// to a regular icon on other platforms.
// templateIconBytes and iconBytes should be the content of .ico for windows and
// .ico/.jpg/.png for other platforms.
func SetTemplateIcon(templateIconBytes []byte, regularIconBytes []byte) {
	// TODO handle the templateIconBytes?
	SetIcon(regularIconBytes)
}

// SetIcon sets the systray icon.
// iconBytes should be the content of .ico for windows and .ico/.jpg/.png
// for other platforms.
func SetIcon(iconBytes []byte) {
	instance.lock.Lock()
	instance.iconData = iconBytes
	props := instance.props
	conn := instance.conn
	defer instance.lock.Unlock()

	if props == nil {
		return
	}

	props.SetMust("org.kde.StatusNotifierItem", "IconPixmap",
		[]PX{convertToPixels(iconBytes)})
	if conn == nil {
		return
	}

	err := notifier.Emit(conn, &notifier.StatusNotifierItem_NewIconSignal{
		Path: path,
		Body: &notifier.StatusNotifierItem_NewIconSignalBody{},
	})
	if err != nil {
		log.Printf("systray error: failed to emit new icon signal: %s\n", err)
		return
	}
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
func SetTitle(t string) {
	instance.lock.Lock()
	instance.title = t
	props := instance.props
	conn := instance.conn
	defer instance.lock.Unlock()

	if props == nil {
		return
	}
	dbusErr := props.Set("org.kde.StatusNotifierItem", "Title",
		dbus.MakeVariant(t))
	if dbusErr != nil {
		log.Printf("systray error: failed to set Title prop: %s\n", dbusErr)
		return
	}

	if conn == nil {
		return
	}

	err := notifier.Emit(conn, &notifier.StatusNotifierItem_NewTitleSignal{
		Path: path,
		Body: &notifier.StatusNotifierItem_NewTitleSignalBody{},
	})
	if err != nil {
		log.Printf("systray error: failed to emit new title signal: %s\n", err)
		return
	}
}

// SetTooltip sets the systray tooltip to display on mouse hover of the tray icon,
// only available on Mac and Windows.
func SetTooltip(tooltipTitle string) {
	instance.lock.Lock()
	instance.tooltipTitle = tooltipTitle
	props := instance.props
	conn := instance.conn
	defer instance.lock.Unlock()

	if props == nil {
		return
	}
	dbusErr := props.Set("org.kde.StatusNotifierItem", "ToolTip",
		dbus.MakeVariant(tooltip{V2: tooltipTitle}))
	if dbusErr != nil {
		log.Printf("systray error: failed to set ToolTip prop: %s\n", dbusErr)
		return
	}

	if conn == nil {
		return
	}

	err := notifier.Emit(conn, &notifier.StatusNotifierItem_NewToolTipSignal{
		Path: path,
		Body: &notifier.StatusNotifierItem_NewToolTipSignalBody{},
	})
	if err != nil {
		log.Printf("systray error: failed to emit new tooltip signal: %s\n", err)
		return
	}
}

// SetTemplateIcon sets the icon of a menu item as a template icon (on macOS). On Windows and
// Linux, it falls back to the regular icon bytes.
// templateIconBytes and regularIconBytes should be the content of .ico for windows and
// .ico/.jpg/.png for other platforms.
func (item *MenuItem) SetTemplateIcon(templateIconBytes []byte, regularIconBytes []byte) {
	item.SetIcon(regularIconBytes)
}

// SetRemovalAllowed sets whether a user can remove the systray icon or not.
// This is only supported on macOS.
func SetRemovalAllowed(allowed bool) {
}

func setInternalLoop(_ bool) {
	// nothing to action on Linux
}

func registerSystray() {
}

func nativeLoop() int {
	nativeStart()
	<-quitChan
	nativeEnd()
	return 0
}

func nativeEnd() {
	runSystrayExit()
	instance.conn.Close()
}

func quit() {
	close(quitChan)
}

func nativeStart() {
	systrayReady()
	conn, err := dbus.SessionBus()
	if err != nil {
		log.Printf("systray error: failed to connect to DBus: %v\n", err)
		return
	}
	err = notifier.ExportStatusNotifierItem(conn, path, newLeftRightNotifierItem())
	if err != nil {
		log.Printf("systray error: failed to export status notifier item: %v\n", err)
	}
	err = menu.ExportDbusmenu(conn, menuPath, instance)
	if err != nil {
		log.Printf("systray error: failed to export status notifier menu: %v\n", err)
		return
	}

	name := fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid()) // register id 1 for this process
	_, err = conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil {
		log.Printf("systray error: failed to request name: %s\n", err)
		// it's not critical error: continue
	}
	props, err := prop.Export(conn, path, instance.createPropSpec())
	if err != nil {
		log.Printf("systray error: failed to export notifier item properties to bus: %s\n", err)
		return
	}
	menuProps, err := prop.Export(conn, menuPath, createMenuPropSpec())
	if err != nil {
		log.Printf("systray error: failed to export notifier menu properties to bus: %s\n", err)
		return
	}

	node := introspect.Node{
		Name: path,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			notifier.IntrospectDataStatusNotifierItem,
		},
	}
	err = conn.Export(introspect.NewIntrospectable(&node), path,
		"org.freedesktop.DBus.Introspectable")
	if err != nil {
		log.Printf("systray error: failed to export node introspection: %s\n", err)
		return
	}
	menuNode := introspect.Node{
		Name: menuPath,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			menu.IntrospectDataDbusmenu,
		},
	}
	err = conn.Export(introspect.NewIntrospectable(&menuNode), menuPath,
		"org.freedesktop.DBus.Introspectable")
	if err != nil {
		log.Printf("systray error: failed to export menu node introspection: %s\n", err)
		return
	}

	instance.lock.Lock()
	instance.conn = conn
	instance.props = props
	instance.menuProps = menuProps
	instance.lock.Unlock()

	go stayRegistered()
}

func register() bool {
	obj := instance.conn.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher")
	call := obj.Call("org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem", 0, path)
	if call.Err != nil {
		log.Printf("systray error: failed to register: %v\n", call.Err)
		return false
	}

	return true
}

func stayRegistered() {
	register()

	conn := instance.conn
	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/DBus"),
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchSender("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, "org.kde.StatusNotifierWatcher"),
	); err != nil {
		log.Printf("systray error: failed to register signal matching: %v\n", err)
		// If we can't monitor signals, there is no point in
		// us being here. we're either registered or not (per
		// above) and will roll the dice from here...
		return
	}

	sc := make(chan *dbus.Signal, 10)
	conn.Signal(sc)

	for {
		select {
		case sig := <-sc:
			if sig == nil {
				return // We get a nil signal when closing the window.
			} else if len(sig.Body) < 3 {
				return // malformed signal?
			}

			// sig.Body has the args, which are [name old_owner new_owner]
			if s, ok := sig.Body[2].(string); ok && s != "" {
				register()
			}
		case <-quitChan:
			return
		}
	}
}

// tray is a basic type that handles the dbus functionality
type tray struct {
	// the DBus connection that we will use
	conn *dbus.Conn

	// icon data for the main systray icon
	iconData []byte
	// title and tooltip state
	title, tooltipTitle string

	lock             sync.Mutex
	menu             *menuLayout
	menuLock         sync.RWMutex
	props, menuProps *prop.Properties
	menuVersion      uint32
}

func (t *tray) createPropSpec() map[string]map[string]*prop.Prop {
	t.lock.Lock()
	defer t.lock.Unlock()
	id := t.title
	if id == "" {
		id = fmt.Sprintf("systray_%d", os.Getpid())
	}
	return map[string]map[string]*prop.Prop{
		"org.kde.StatusNotifierItem": {
			"Status": {
				Value:    "Active", // Passive, Active or NeedsAttention
				Writable: false,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"Title": {
				Value:    t.title,
				Writable: true,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"Id": {
				Value:    id,
				Writable: false,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"Category": {
				Value:    "ApplicationStatus",
				Writable: false,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"IconName": {
				Value:    "",
				Writable: false,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"IconPixmap": {
				Value:    []PX{convertToPixels(t.iconData)},
				Writable: true,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"IconThemePath": {
				Value:    "",
				Writable: false,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"ItemIsMenu": {
				Value:    tappedLeft == nil && tappedRight == nil,
				Writable: false,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"Menu": {
				Value:    dbus.ObjectPath(menuPath),
				Writable: true,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"ToolTip": {
				Value:    tooltip{V2: t.tooltipTitle},
				Writable: true,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
		}}
}

// PX is picture pix map structure with width and high
type PX struct {
	W, H int
	Pix  []byte
}

// tooltip is our data for a tooltip property.
// Param names need to match the generated code...
type tooltip = struct {
	V0 string // name
	V1 []PX   // icons
	V2 string // title
	V3 string // description
}

func convertToPixels(data []byte) PX {
	if len(data) == 0 {
		return PX{}
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		log.Printf("Failed to read icon format %v", err)
		return PX{}
	}

	return PX{
		img.Bounds().Dx(), img.Bounds().Dy(),
		argbForImage(img),
	}
}

func argbForImage(img image.Image) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	data := make([]byte, w*h*4)
	i := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			data[i] = byte(a)
			data[i+1] = byte(r)
			data[i+2] = byte(g)
			data[i+3] = byte(b)
			i += 4
		}
	}
	return data
}
