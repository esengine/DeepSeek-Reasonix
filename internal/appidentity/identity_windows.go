//go:build windows

package appidentity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	clsctxInprocServer    = 0x1
	coinitApartmentThread = 0x2
	rpcEChangedMode       = 0x80010106
	slgpRawPath           = 0x4
	stgmReadWrite         = 0x2
	vtLPWSTR              = 31
	windowsPathBuffer     = 32768
)

var (
	clsidShellLink = windows.GUID{
		Data1: 0x00021401,
		Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	iidIShellLinkW = windows.GUID{
		Data1: 0x000214f9,
		Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	iidIPersistFile = windows.GUID{
		Data1: 0x0000010b,
		Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	iidIPropertyStore = windows.GUID{
		Data1: 0x886d8eeb,
		Data2: 0x8cf2,
		Data3: 0x4446,
		Data4: [8]byte{0x8d, 0x02, 0xcd, 0xba, 0x1d, 0xbd, 0xcf, 0x99},
	}
	pkeyAppUserModelID = propertyKey{
		FormatID: windows.GUID{
			Data1: 0x9f4c2855,
			Data2: 0x9f79,
			Data3: 0x4b39,
			Data4: [8]byte{0xa8, 0xd0, 0xe1, 0xd4, 0x2d, 0xe1, 0xd5, 0xf3},
		},
		PropertyID: 5,
	}

	ole32   = windows.NewLazySystemDLL("ole32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procCoCreateInstance                        = ole32.NewProc("CoCreateInstance")
	procCoInitializeEx                          = ole32.NewProc("CoInitializeEx")
	procCoUninitialize                          = ole32.NewProc("CoUninitialize")
	procPropVariantClear                        = ole32.NewProc("PropVariantClear")
	procPropVariantCopy                         = ole32.NewProc("PropVariantCopy")
	procSetCurrentProcessExplicitAppUserModelID = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
	procSHChangeNotify                          = shell32.NewProc("SHChangeNotify")

	knownFolderPath = windows.KnownFolderPath
)

type propertyKey struct {
	FormatID   windows.GUID
	PropertyID uint32
}

type propVariant struct {
	VariantType uint16
	Reserved1   uint16
	Reserved2   uint16
	Reserved3   uint16
	Value       *uint16
	Value2      uintptr
}

type unknownVTable struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}

type shellLinkW struct {
	VTable *shellLinkWVTable
}

type shellLinkWVTable struct {
	unknownVTable
	GetPath             uintptr
	GetIDList           uintptr
	SetIDList           uintptr
	GetDescription      uintptr
	SetDescription      uintptr
	GetWorkingDirectory uintptr
	SetWorkingDirectory uintptr
	GetArguments        uintptr
	SetArguments        uintptr
	GetHotkey           uintptr
	SetHotkey           uintptr
	GetShowCmd          uintptr
	SetShowCmd          uintptr
	GetIconLocation     uintptr
	SetIconLocation     uintptr
	SetRelativePath     uintptr
	Resolve             uintptr
	SetPath             uintptr
}

type persistFile struct {
	VTable *persistFileVTable
}

type persistFileVTable struct {
	unknownVTable
	GetClassID    uintptr
	IsDirty       uintptr
	Load          uintptr
	Save          uintptr
	SaveCompleted uintptr
	GetCurFile    uintptr
}

type propertyStore struct {
	VTable *propertyStoreVTable
}

type propertyStoreVTable struct {
	unknownVTable
	GetCount uintptr
	GetAt    uintptr
	GetValue uintptr
	SetValue uintptr
	Commit   uintptr
}

type loadedShortcut struct {
	link    *shellLinkW
	persist *persistFile
	store   *propertyStore
	path    string
}

func ApplyToCurrentProcess() error {
	id, err := windows.UTF16PtrFromString(AppUserModelID)
	if err != nil {
		return err
	}
	hr, _, _ := procSetCurrentProcessExplicitAppUserModelID.Call(uintptr(unsafe.Pointer(id)))
	return checkHRESULT("SetCurrentProcessExplicitAppUserModelID", hr)
}

func RepairOwnedShortcuts(installRoot string) error {
	installRoot = filepath.Clean(strings.TrimSpace(installRoot))
	if installRoot == "." || installRoot == "" {
		return nil
	}
	paths, discoveryErr := shortcutCandidates(installRoot)
	if len(paths) == 0 {
		return discoveryErr
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	uninitialize, err := initializeCOM()
	if err != nil {
		return errors.Join(discoveryErr, err)
	}
	defer uninitialize()

	var repairErr error
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			if !os.IsNotExist(err) && reasonixShortcutName(path) {
				repairErr = errors.Join(repairErr, err)
			}
			continue
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		changed, err := repairOwnedShortcut(path, installRoot)
		if err != nil {
			if reasonixShortcutName(path) {
				repairErr = errors.Join(repairErr, fmt.Errorf("%s: %w", path, err))
			}
			continue
		}
		if changed {
			notifyShortcutChanged(path)
		}
	}
	return errors.Join(discoveryErr, repairErr)
}

func shortcutCandidates(installRoot string) ([]string, error) {
	seen := make(map[string]struct{})
	paths := make([]string, 0, 8)
	add := func(path string) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || path == "" {
			return
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		paths = append(paths, path)
	}
	addReasonixLinks := func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() && reasonixShortcutName(entry.Name()) {
				add(filepath.Join(dir, entry.Name()))
			}
		}
		return nil
	}

	add(filepath.Join(installRoot, "Reasonix.lnk"))
	var resultErr error
	resultErr = errors.Join(resultErr, addReasonixLinks(installRoot))
	for _, folderID := range []*windows.KNOWNFOLDERID{windows.FOLDERID_Desktop, windows.FOLDERID_Programs} {
		folder, err := knownFolderPath(folderID, windows.KF_FLAG_DEFAULT)
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			continue
		}
		add(filepath.Join(folder, "Reasonix.lnk"))
	}
	roaming, err := knownFolderPath(windows.FOLDERID_RoamingAppData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		resultErr = errors.Join(resultErr, err)
	} else {
		pinned := filepath.Join(roaming, "Microsoft", "Internet Explorer", "Quick Launch", "User Pinned", "TaskBar")
		resultErr = errors.Join(resultErr, addReasonixLinks(pinned))
	}
	return paths, resultErr
}

func reasonixShortcutName(path string) bool {
	name := filepath.Base(strings.TrimSpace(path))
	return strings.EqualFold(filepath.Ext(name), ".lnk") &&
		strings.HasPrefix(strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name))), "reasonix")
}

func repairOwnedShortcut(path, installRoot string) (bool, error) {
	shortcut, err := loadShortcut(path, stgmReadWrite)
	if err != nil {
		return false, err
	}
	defer shortcut.release()

	target, err := shortcut.targetPath()
	if err != nil {
		return false, err
	}
	if !ownedShortcutTarget(target, installRoot) {
		return false, nil
	}
	currentID, err := shortcut.appUserModelID()
	if err != nil {
		return false, err
	}
	if currentID == AppUserModelID {
		return false, nil
	}
	if err := shortcut.setAppUserModelID(AppUserModelID); err != nil {
		return false, err
	}
	return true, nil
}

func ownedShortcutTarget(target, installRoot string) bool {
	target = filepath.Clean(strings.TrimSpace(target))
	installRoot = filepath.Clean(strings.TrimSpace(installRoot))
	if target == "." || target == "" || installRoot == "." || installRoot == "" {
		return false
	}
	for _, candidate := range []string{
		filepath.Join(installRoot, "reasonix-launcher.exe"),
		filepath.Join(installRoot, "Reasonix.exe"),
		filepath.Join(installRoot, "reasonix-desktop.exe"),
	} {
		if sameWindowsPathOrFile(target, candidate) {
			return true
		}
	}
	rel, err := filepath.Rel(installRoot, target)
	if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) == 3 && strings.EqualFold(parts[0], "versions") &&
			strings.EqualFold(parts[2], "reasonix-desktop.exe") {
			return true
		}
	}
	versionDir := filepath.Dir(target)
	versionsDir := filepath.Dir(versionDir)
	return strings.EqualFold(filepath.Base(target), "reasonix-desktop.exe") &&
		strings.EqualFold(filepath.Base(versionsDir), "versions") &&
		sameWindowsFile(filepath.Dir(versionsDir), installRoot)
}

func sameWindowsPathOrFile(left, right string) bool {
	if strings.EqualFold(filepath.Clean(left), filepath.Clean(right)) {
		return true
	}
	if !strings.EqualFold(filepath.Base(left), filepath.Base(right)) {
		return false
	}
	return sameWindowsFile(left, right)
}

func sameWindowsFile(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func loadShortcut(path string, mode uint32) (*loadedShortcut, error) {
	var link *shellLinkW
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIShellLinkW)),
		uintptr(unsafe.Pointer(&link)),
	)
	if err := checkHRESULT("CoCreateInstance(CLSID_ShellLink)", hr); err != nil {
		return nil, err
	}
	shortcut := &loadedShortcut{link: link, path: path}
	if err := queryInterface(unsafe.Pointer(link), &iidIPersistFile, unsafe.Pointer(&shortcut.persist)); err != nil {
		shortcut.release()
		return nil, err
	}
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		shortcut.release()
		return nil, err
	}
	hr, _, _ = syscall.SyscallN(
		shortcut.persist.VTable.Load,
		uintptr(unsafe.Pointer(shortcut.persist)),
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(mode),
	)
	if err := checkHRESULT("IPersistFile.Load", hr); err != nil {
		shortcut.release()
		return nil, err
	}
	if err := queryInterface(unsafe.Pointer(link), &iidIPropertyStore, unsafe.Pointer(&shortcut.store)); err != nil {
		shortcut.release()
		return nil, err
	}
	return shortcut, nil
}

func (s *loadedShortcut) targetPath() (string, error) {
	buffer := make([]uint16, windowsPathBuffer)
	hr, _, _ := syscall.SyscallN(
		s.link.VTable.GetPath,
		uintptr(unsafe.Pointer(s.link)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		0,
		slgpRawPath,
	)
	if err := checkHRESULT("IShellLinkW.GetPath", hr); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buffer), nil
}

func (s *loadedShortcut) appUserModelID() (string, error) {
	var value propVariant
	hr, _, _ := syscall.SyscallN(
		s.store.VTable.GetValue,
		uintptr(unsafe.Pointer(s.store)),
		uintptr(unsafe.Pointer(&pkeyAppUserModelID)),
		uintptr(unsafe.Pointer(&value)),
	)
	if err := checkHRESULT("IPropertyStore.GetValue", hr); err != nil {
		return "", err
	}
	defer clearPropVariant(&value)
	if value.VariantType != vtLPWSTR || value.Value == nil {
		return "", nil
	}
	return windows.UTF16PtrToString(value.Value), nil
}

func (s *loadedShortcut) setAppUserModelID(id string) error {
	idPtr, err := windows.UTF16PtrFromString(id)
	if err != nil {
		return err
	}
	source := propVariant{VariantType: vtLPWSTR, Value: idPtr}
	var value propVariant
	hr, _, _ := procPropVariantCopy.Call(
		uintptr(unsafe.Pointer(&value)),
		uintptr(unsafe.Pointer(&source)),
	)
	runtime.KeepAlive(idPtr)
	if err := checkHRESULT("PropVariantCopy", hr); err != nil {
		return err
	}
	defer clearPropVariant(&value)
	hr, _, _ = syscall.SyscallN(
		s.store.VTable.SetValue,
		uintptr(unsafe.Pointer(s.store)),
		uintptr(unsafe.Pointer(&pkeyAppUserModelID)),
		uintptr(unsafe.Pointer(&value)),
	)
	if err := checkHRESULT("IPropertyStore.SetValue", hr); err != nil {
		return err
	}
	hr, _, _ = syscall.SyscallN(s.store.VTable.Commit, uintptr(unsafe.Pointer(s.store)))
	if err := checkHRESULT("IPropertyStore.Commit", hr); err != nil {
		return err
	}
	pathPtr, err := windows.UTF16PtrFromString(s.path)
	if err != nil {
		return err
	}
	hr, _, _ = syscall.SyscallN(
		s.persist.VTable.Save,
		uintptr(unsafe.Pointer(s.persist)),
		uintptr(unsafe.Pointer(pathPtr)),
		1,
	)
	return checkHRESULT("IPersistFile.Save", hr)
}

func (s *loadedShortcut) release() {
	if s.store != nil {
		releaseInterface(unsafe.Pointer(s.store))
		s.store = nil
	}
	if s.persist != nil {
		releaseInterface(unsafe.Pointer(s.persist))
		s.persist = nil
	}
	if s.link != nil {
		releaseInterface(unsafe.Pointer(s.link))
		s.link = nil
	}
}

func initializeCOM() (func(), error) {
	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThread)
	if uint32(hr) == rpcEChangedMode {
		return func() {}, nil
	}
	if err := checkHRESULT("CoInitializeEx", hr); err != nil {
		return nil, err
	}
	return func() { procCoUninitialize.Call() }, nil
}

func queryInterface(object unsafe.Pointer, iid *windows.GUID, result unsafe.Pointer) error {
	vtable := (*unknownVTable)(*(*unsafe.Pointer)(object))
	hr, _, _ := syscall.SyscallN(
		vtable.QueryInterface,
		uintptr(object),
		uintptr(unsafe.Pointer(iid)),
		uintptr(result),
	)
	return checkHRESULT("IUnknown.QueryInterface", hr)
}

func releaseInterface(object unsafe.Pointer) {
	vtable := (*unknownVTable)(*(*unsafe.Pointer)(object))
	_, _, _ = syscall.SyscallN(vtable.Release, uintptr(object))
}

func clearPropVariant(value *propVariant) {
	_, _, _ = procPropVariantClear.Call(uintptr(unsafe.Pointer(value)))
}

func notifyShortcutChanged(path string) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	const (
		shcneUpdateItem = 0x00002000
		shcnfFlush      = 0x1000
		shcnfPathW      = 0x0005
	)
	_, _, _ = procSHChangeNotify.Call(shcneUpdateItem, shcnfPathW|shcnfFlush, uintptr(unsafe.Pointer(pathPtr)), 0)
}

func checkHRESULT(operation string, result uintptr) error {
	if int32(uint32(result)) >= 0 {
		return nil
	}
	return fmt.Errorf("%s failed with HRESULT 0x%08X", operation, uint32(result))
}
