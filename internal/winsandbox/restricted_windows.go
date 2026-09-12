//go:build windows

package winsandbox

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	disableMaxPrivilege      = 0x1
	luaToken                 = 0x4
	writeRestricted          = 0x8
	fileDeleteChild          = 0x40
	capabilityWriteGrantMask = (windows.FILE_GENERIC_WRITE | windows.DELETE | fileDeleteChild) &^ windows.STANDARD_RIGHTS_WRITE
	fileAllAccess            = 0x1F01FF
)

type restrictedCapability struct {
	root    string
	purpose capabilityPurpose
	sidText string
	sid     *windows.SID
	object  windowsFileIdentity
}

type windowsFileIdentity struct {
	volumeSerial uint32
	indexHigh    uint32
	indexLow     uint32
}

func prepareRestrictedCapabilities(spec Spec, tempRoot string, notice io.Writer, holderLabel string) ([]restrictedCapability, error) {
	if spec.ReadOnly {
		return nil, nil
	}

	writeRoots, err := canonicalWindowsDirectories(spec.WritableRoots)
	if err != nil {
		return nil, err
	}
	tempRoot, err = canonicalWindowsDirectory(tempRoot)
	if err != nil {
		return nil, fmt.Errorf("canonicalize session temp: %w", err)
	}
	if err := validateCapabilityBoundaries(writeRoots, tempRoot, spec.ProtectedWriteRoots); err != nil {
		return nil, err
	}

	capabilities := make([]restrictedCapability, 0, len(writeRoots)+1)
	for _, root := range writeRoots {
		capability, err := newRestrictedCapability(capabilityWorkspace, root)
		if err != nil {
			return nil, err
		}
		capabilities = append(capabilities, capability)
	}
	tempCapability, err := newRestrictedCapability(capabilitySessionTemp, tempRoot)
	if err != nil {
		return nil, err
	}
	capabilities = append(capabilities, tempCapability)

	roots := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		roots = append(roots, capability.root)
	}
	lock, err := lockWindowsRoots(roots, notice, holderLabel, spec.LockWait)
	if err != nil {
		return nil, err
	}
	defer lock.release()
	records, err := newCapabilityRecordStore(spec.ProtectedWriteRoots, roots)
	if err != nil {
		return nil, err
	}
	for _, capability := range capabilities {
		if err := records.write(capability, "preparing"); err != nil {
			return nil, fmt.Errorf("record %s capability for %q before DACL update: %w", capability.purpose, capability.root, err)
		}
		if err := grantWriteCapability(capability.root, capability.sid); err != nil {
			return nil, fmt.Errorf("materialize %s capability for %q: %w", capability.purpose, capability.root, err)
		}
		current, err := windowsDirectoryIdentity(capability.root)
		if err != nil {
			return nil, fmt.Errorf("revalidate %s capability root %q: %w", capability.purpose, capability.root, err)
		}
		if current != capability.object {
			return nil, fmt.Errorf("%s capability root %q changed identity during sandbox preparation", capability.purpose, capability.root)
		}
		if err := records.write(capability, "active"); err != nil {
			return nil, fmt.Errorf("finalize %s capability record for %q: %w", capability.purpose, capability.root, err)
		}
	}
	return capabilities, nil
}

func newRestrictedCapability(purpose capabilityPurpose, root string) (restrictedCapability, error) {
	identity := strings.ToLower(filepath.Clean(root))
	sidText := deriveCapabilitySID(purpose, identity)
	sid, err := windows.StringToSid(sidText)
	if err != nil {
		return restrictedCapability{}, fmt.Errorf("parse derived Windows capability SID %q: %w", sidText, err)
	}
	object, err := windowsDirectoryIdentity(root)
	if err != nil {
		return restrictedCapability{}, fmt.Errorf("read Windows directory identity for %q: %w", root, err)
	}
	return restrictedCapability{root: root, purpose: purpose, sidText: sidText, sid: sid, object: object}, nil
}

func windowsDirectoryIdentity(root string) (windowsFileIdentity, error) {
	path16, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return windowsFileIdentity{}, err
	}
	handle, err := windows.CreateFile(
		path16,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return windowsFileIdentity{}, err
	}
	defer windows.CloseHandle(handle)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return windowsFileIdentity{}, err
	}
	return windowsFileIdentity{
		volumeSerial: info.VolumeSerialNumber,
		indexHigh:    info.FileIndexHigh,
		indexLow:     info.FileIndexLow,
	}, nil
}

func canonicalWindowsDirectories(roots []string) ([]string, error) {
	out := make([]string, 0, len(roots))
	seen := make(map[string]bool, len(roots))
	for _, root := range roots {
		canonical, err := canonicalWindowsDirectory(root)
		if err != nil {
			return nil, fmt.Errorf("canonicalize writable root %q: %w", root, err)
		}
		key := strings.ToLower(filepath.Clean(canonical))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, canonical)
	}
	return out, nil
}

func canonicalWindowsDirectory(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("empty directory")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory")
	}
	path16, err := windows.UTF16PtrFromString(real)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		path16,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", fmt.Errorf("open directory for final-path validation: %w", err)
	}
	defer windows.CloseHandle(handle)

	needed, err := windows.GetFinalPathNameByHandle(handle, nil, 0, 0)
	if err != nil {
		return "", fmt.Errorf("query final directory path: %w", err)
	}
	if needed == 0 {
		return "", fmt.Errorf("query final directory path returned an empty path")
	}
	buffer := make([]uint16, needed+1)
	written, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return "", fmt.Errorf("read final directory path: %w", err)
	}
	if written == 0 || written >= uint32(len(buffer)) {
		return "", fmt.Errorf("read final directory path returned invalid length %d", written)
	}
	return filepath.Clean(normalizeWindowsFinalPath(windows.UTF16ToString(buffer[:written]))), nil
}

func normalizeWindowsFinalPath(path string) string {
	const (
		extendedPrefix = `\\?\`
		uncPrefix      = `\\?\UNC\`
	)
	if strings.HasPrefix(strings.ToUpper(path), strings.ToUpper(uncPrefix)) {
		return `\\` + path[len(uncPrefix):]
	}
	if strings.HasPrefix(path, extendedPrefix) {
		return path[len(extendedPrefix):]
	}
	return path
}

func validateCapabilityBoundaries(writeRoots []string, tempRoot string, protectedRoots []string) error {
	for _, root := range writeRoots {
		if windowsPathsOverlap(root, tempRoot) {
			return fmt.Errorf("session temp %q overlaps writable root %q", tempRoot, root)
		}
	}
	for _, protected := range protectedRoots {
		protected = strings.TrimSpace(protected)
		if protected == "" {
			continue
		}
		abs, err := canonicalWindowsDirectory(protected)
		if err != nil {
			return fmt.Errorf("canonicalize protected root %q: %w", protected, err)
		}
		if windowsPathsOverlap(tempRoot, abs) {
			return fmt.Errorf("session temp %q overlaps protected state root %q", tempRoot, abs)
		}
		for _, root := range writeRoots {
			// A capability is placed on the exact writable directory, not on its
			// ancestors. It is therefore safe for the desktop's explicit
			// global-workspace to live below the Reasonix state root. The reverse
			// relationship is unsafe: granting a workspace that contains the
			// protected root would make the protected data inherit the write ACE.
			if windowsPathWithin(abs, root) {
				if isWindowsGlobalWorkspaceException(abs, root) {
					continue
				}
				return fmt.Errorf("writable root %q is inside protected state root %q", root, abs)
			}
			if windowsPathWithin(root, abs) {
				return fmt.Errorf("writable root %q contains protected state root %q", root, abs)
			}
		}
	}
	return nil
}

func isWindowsGlobalWorkspaceException(protectedRoot, writableRoot string) bool {
	return strings.EqualFold(filepath.Base(writableRoot), "global-workspace") &&
		strings.EqualFold(filepath.Clean(filepath.Dir(writableRoot)), filepath.Clean(protectedRoot))
}

func windowsPathsOverlap(a, b string) bool {
	a = strings.ToLower(filepath.Clean(a))
	b = strings.ToLower(filepath.Clean(b))
	return windowsPathWithin(a, b) || windowsPathWithin(b, a)
}

func windowsPathWithin(root, candidate string) bool {
	root = strings.ToLower(filepath.Clean(root))
	candidate = strings.ToLower(filepath.Clean(candidate))
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func grantWriteCapability(root string, sid *windows.SID) error {
	present, err := hasExactWriteCapability(root, sid)
	if err != nil {
		return err
	}
	if present {
		return nil
	}

	// A directory capability is represented by one inheritable ACE. Do not add
	// the second, non-inheritable entry used by the legacy AppContainer helper:
	// the restricted token needs the same precise descriptor on the root and its
	// descendants so the exact-ACE check remains stable across launches.
	var pinner runtime.Pinner
	pinner.Pin(sid)
	defer pinner.Unpin()
	entry := windows.EXPLICIT_ACCESS{
		AccessPermissions: windows.ACCESS_MASK(capabilityWriteGrantMask),
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_UNKNOWN,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
	sd, err := windows.GetNamedSecurityInfo(root, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("get DACL %q: %w", root, err)
	}
	var oldDACL *windows.ACL
	if sd != nil {
		oldDACL, _, err = sd.DACL()
		if err != nil && !errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) {
			return fmt.Errorf("read DACL %q: %w", root, err)
		}
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{entry}, oldDACL)
	if err != nil {
		return fmt.Errorf("build capability DACL %q: %w", root, err)
	}
	if err := windows.SetNamedSecurityInfo(root, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		return fmt.Errorf("set capability DACL %q: %w", root, err)
	}
	runtime.KeepAlive(sd)
	runtime.KeepAlive(acl)
	return nil
}

func hasExactWriteCapability(root string, sid *windows.SID) (bool, error) {
	sd, err := windows.GetNamedSecurityInfo(root, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, fmt.Errorf("get DACL %q: %w", root, err)
	}
	if sd == nil {
		return false, nil
	}
	acl, _, err := sd.DACL()
	if errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) || acl == nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read DACL %q: %w", root, err)
	}
	for index := range uint32(acl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, index, &ace); err != nil {
			return false, fmt.Errorf("read DACL ACE %d for %q: %w", index, root, err)
		}
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE ||
			ace.Header.AceFlags != windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT ||
			ace.Mask != windows.ACCESS_MASK(capabilityWriteGrantMask) {
			continue
		}
		aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if windows.EqualSid(aceSID, sid) {
			runtime.KeepAlive(sd)
			return true, nil
		}
	}
	runtime.KeepAlive(sd)
	return false, nil
}

func createWriteRestrictedPrimaryToken(readOnly bool, capabilities []restrictedCapability) (windows.Token, error) {
	if readOnly && len(capabilities) != 0 {
		return 0, fmt.Errorf("read-only restricted token must not carry write capabilities")
	}
	if !readOnly && len(capabilities) == 0 {
		return 0, fmt.Errorf("workspace-write restricted token requires a directory capability")
	}

	var current windows.Token
	access := uint32(windows.TOKEN_QUERY | windows.TOKEN_DUPLICATE | windows.TOKEN_ASSIGN_PRIMARY | windows.TOKEN_ADJUST_DEFAULT)
	if err := windows.OpenProcessToken(windows.CurrentProcess(), access, &current); err != nil {
		return 0, fmt.Errorf("open current process token: %w", err)
	}
	defer current.Close()
	logonSID, err := currentProcessLogonSID(current)
	if err != nil {
		return 0, err
	}
	worldSID, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		return 0, fmt.Errorf("create Everyone SID: %w", err)
	}

	restricting := []windows.SIDAndAttributes{{Sid: logonSID}, {Sid: worldSID}}
	if !readOnly {
		for _, capability := range capabilities {
			restricting = append(restricting, windows.SIDAndAttributes{Sid: capability.sid})
		}
	}
	var token windows.Token
	r1, _, callErr := procCreateRestrictedToken.Call(
		uintptr(current),
		disableMaxPrivilege|luaToken|writeRestricted,
		0, 0,
		0, 0,
		uintptr(len(restricting)), uintptr(unsafe.Pointer(&restricting[0])),
		uintptr(unsafe.Pointer(&token)),
	)
	runtime.KeepAlive(restricting)
	runtime.KeepAlive(capabilities)
	if r1 == 0 {
		if errors.Is(callErr, windows.ERROR_SUCCESS) {
			return 0, fmt.Errorf("CreateRestrictedToken failed without a Windows error code")
		}
		return 0, fmt.Errorf("CreateRestrictedToken: %w", callErr)
	}
	if token == 0 {
		return 0, fmt.Errorf("CreateRestrictedToken returned a null token")
	}

	defaultSID := worldSID
	if !readOnly {
		defaultSID = capabilities[0].sid
		for _, capability := range capabilities {
			if capability.purpose == capabilitySessionTemp {
				defaultSID = capability.sid
				break
			}
		}
	}
	if err := setRestrictedTokenDefaultDACL(token, defaultSID); err != nil {
		token.Close()
		return 0, err
	}
	return token, nil
}

func currentProcessLogonSID(token windows.Token) (*windows.SID, error) {
	groups, err := token.GetTokenGroups()
	if err != nil {
		return nil, fmt.Errorf("read process token groups: %w", err)
	}
	for _, group := range groups.AllGroups() {
		if group.Sid == nil || group.Attributes&windows.SE_GROUP_LOGON_ID != windows.SE_GROUP_LOGON_ID {
			continue
		}
		copy, err := group.Sid.Copy()
		if err != nil {
			return nil, fmt.Errorf("copy process logon SID: %w", err)
		}
		return copy, nil
	}
	return nil, fmt.Errorf("CreateRestrictedToken prerequisite failed: process token has no logon SID")
}

type tokenDefaultDACL struct {
	DefaultDACL *windows.ACL
}

func setRestrictedTokenDefaultDACL(token windows.Token, sid *windows.SID) error {
	var needed uint32
	err := windows.GetTokenInformation(token, windows.TokenDefaultDacl, nil, 0, &needed)
	if err != nil && !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
		return fmt.Errorf("query restricted token default DACL size: %w", err)
	}
	if needed < uint32(unsafe.Sizeof(tokenDefaultDACL{})) {
		return fmt.Errorf("restricted token default DACL has invalid size %d", needed)
	}
	buffer := make([]byte, needed)
	if err := windows.GetTokenInformation(token, windows.TokenDefaultDacl, &buffer[0], uint32(len(buffer)), &needed); err != nil {
		return fmt.Errorf("read restricted token default DACL: %w", err)
	}
	current := (*tokenDefaultDACL)(unsafe.Pointer(&buffer[0])).DefaultDACL
	if current == nil {
		return fmt.Errorf("restricted token has no default DACL")
	}

	var pin runtime.Pinner
	pin.Pin(sid)
	defer pin.Unpin()
	entry := windows.EXPLICIT_ACCESS{
		AccessPermissions: windows.ACCESS_MASK(fileAllAccess),
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_UNKNOWN,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{entry}, current)
	if err != nil {
		return fmt.Errorf("merge restricted token default DACL: %w", err)
	}
	info := tokenDefaultDACL{DefaultDACL: acl}
	if err := windows.SetTokenInformation(token, windows.TokenDefaultDacl, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return fmt.Errorf("set restricted token default DACL: %w", err)
	}
	runtime.KeepAlive(buffer)
	runtime.KeepAlive(acl)
	return nil
}

func existingWindowsForbidReadRoots(spec Spec) []string {
	var roots []string
	for _, root := range normalizedWindowsRoots(spec.ForbidReadRoots) {
		if pathExists(root) {
			roots = append(roots, root)
		}
	}
	return roots
}

func applyRestrictedForbidRead(residueRun *windowsResidueRun, roots []string) (func(), error) {
	var cleanup []func()
	for _, root := range normalizedWindowsRoots(roots) {
		if !pathExists(root) {
			continue
		}
		restore, _, err := snapshotPathSecurity(root, false)
		if err != nil {
			runCleanup(cleanup)()
			return func() {}, err
		}
		cleanup = append(cleanup, restore)
		restoreIndex := len(cleanup) - 1
		denySIDs := forbidReadDenySIDStrings(nil)
		if len(denySIDs) == 0 {
			runCleanup(cleanup)()
			return func() {}, fmt.Errorf("resolve current user SID for forbid_read %q", root)
		}
		if err := residueRun.recordBeforeApply(residueDeny, root); err != nil {
			runCleanup(cleanup)()
			return func() {}, err
		}
		if err := denyAppContainerSIDsWithInheritance(root, denySIDs, "RX", dirExists(root)); err != nil {
			runCleanup(cleanup)()
			return func() {}, err
		}
		removeAdded := func() { removeDeniedAppContainerSIDs(root, denySIDs) }
		cleanup[restoreIndex] = cleanupPathSecurity(restore, removeAdded, nil)
	}
	return runCleanup(cleanup), nil
}
