//go:build windows

package wintoast

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"unsafe"

	"git.sr.ht/~jackmordaunt/go-toast/v2/internal/winrt/data/xml/dom"
	"git.sr.ht/~jackmordaunt/go-toast/v2/internal/winrt/ui/notifications"
	"git.sr.ht/~jackmordaunt/go-toast/v2/tmpl"
	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

func pushPowershell(xml string) error {
	f, err := os.CreateTemp("", "*.ps1")
	if err != nil {
		return fmt.Errorf("creating temporary script file: %w", err)
	}

	defer func() { err = errors.Join(err, os.Remove(f.Name())) }()

	// This BOM ensures we can support non-ascii characters in the toast content.
	bomUtf8 := []byte{0xef, 0xbb, 0xbf}
	if _, err := f.Write(bomUtf8); err != nil {
		return fmt.Errorf("writing utf8 byte marker: %w", err)
	}

	if err := buildPowershell(xml, f); err != nil {
		return fmt.Errorf("generating powershell script: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("closing script file: %w", err)
	}

	cmd := exec.Command("PowerShell", "-ExecutionPolicy", "Bypass", "-File", f.Name())
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("executing powershell: %q: %w", string(out), err)
	}

	return nil
}

func buildPowershell(xml string, w io.Writer) error {
	type scriptData struct {
		AppID string
		XML   string
	}
	return tmpl.ScriptTemplate.Execute(w, scriptData{AppID: appData.AppID, XML: xml})
}

// HRESULT E_NOINTERFACE
const errNoInterface = 0x80004002

var comDisabled atomic.Bool

func pushCOM(appID, xml string) (err error) {
	if comDisabled.Load() {
		return nil
	}

	defer func() {
		// On Windows 7 WinRT interfaces can be stubbed out, and fail to produce
		// error values. This leads to a panic when trying to use the interface.
		// This recover transforms such panics back into an error value for the
		// caller.
		//
		// If the error is "interface not supported" we will permanently disable
		// this API henceforth.
		if v := recover(); v != nil {
			if verr, ok := v.(error); ok {
				err = verr
			}
			if oleErr, ok := v.(*ole.OleError); ok {
				if oleErr.Code() == errNoInterface {
					comDisabled.Store(true)
				}
			}
		}
	}()

	if err := initialize(); err != nil {
		return err
	}

	if err := registerClassFactory(ClassFactory); err != nil {
		return fmt.Errorf("registering class factory: %w", err)
	}

	doc, err := dom.NewXmlDocument()
	if err != nil {
		return fmt.Errorf("dom.NewXmlDocument(): %w", err)
	}

	defer doc.Release()

	if err := doc.LoadXml(xml); err != nil {
		return fmt.Errorf("doc.LoadXml(tmpl): %w", err)
	}

	manager, err := notifications.GetDefault()
	if err != nil {
		return fmt.Errorf("notifications.GetDefault(): %w", err)
	}

	defer manager.Release()

	notifier, err := manager.CreateToastNotifierWithId(appID)
	if err != nil {
		return fmt.Errorf("manager.CreateToastNotifier(%q): %w", appID, err)
	}

	defer notifier.Release()

	toast, err := notifications.CreateToastNotification(doc)
	if err != nil {
		return fmt.Errorf("notifications.CreateToastNotification(doc): %w", err)
	}

	defer toast.Release()

	if err := notifier.Show(toast); err != nil {
		return fmt.Errorf("notifier.Show(): %w", err)
	}

	return nil
}

func setAppData(data AppData) (err error) {
	appDataMu.Lock()
	defer appDataMu.Unlock()

	// Early out if we have already set this data.
	//
	// In the case the data is empty, we don't want to overrite
	// all of the registry entries to empty.
	//
	// This allows the caller to either globally set the app data
	// or provide it per notification.
	if appData == data || data.AppID == "" {
		return nil
	}

	if data.GUID != "" {
		GUID_ImplNotificationActivationCallback = ole.NewGUID(data.GUID)
	}

	// Keep a copy of the saved data for later.
	defer func() {
		if err == nil {
			appData = data
		}
	}()

	if err := setAppDataFunc(data); err != nil {
		return err
	}

	return nil
}

var initialized atomic.Bool

// initialize attempts to initialize the Windows Runtime.
// Each invocation will retry RoInitialize until a successful initialization
// is achieved. Once initialized, we avoid invoking RoInitialize since subsequent
// reinitialization generates errors.
func initialize() (err error) {
	if initialized.CompareAndSwap(false, true) {
		if err := ole.RoInitialize(1); err != nil {
			return fmt.Errorf("RoInitialize: %w", err)
		}
	}
	return nil
}

// sliceUserDataFromUnsafe builds a slice of UserData out of an unsafe pointer.
func sliceUserDataFromUnsafe(ptr unsafe.Pointer, count int) []UserData {
	// Layout mirrors the memory layout of the C struct that contains this data.
	// I'm not sure if there's special alignment or packing - though I don't notice
	// anything in the definition to indicate as such.
	type layout struct {
		Key   unsafe.Pointer
		Value unsafe.Pointer
	}

	// Create a new slice with the appropriate length
	out := make([]UserData, count)

	// Create a slice with the unsafe data layout.
	tmp := unsafe.Slice((*layout)(ptr), count)

	// Convert the unsafe layout to safe strings.
	for ii, it := range tmp {
		out[ii] = UserData{
			Key:   windows.UTF16PtrToString((*uint16)(it.Key)),
			Value: windows.UTF16PtrToString((*uint16)(it.Value)),
		}
	}

	return out
}
