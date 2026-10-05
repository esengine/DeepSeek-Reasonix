//go:build windows

package packagegrant

import (
	"encoding/binary"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Strip removes package grants from root and everything under it, parents
// before children, so a directory's inheritable entries are gone before its
// children are read. Links and reparse points are not followed: what they
// point at is not this tree.
func Strip(root string) (Report, error) {
	report := Report{Stripped: []string{}, Refused: []Failure{}, Unread: []Failure{}}
	info, err := os.Stat(root)
	if err != nil {
		return report, err
	}
	if !info.IsDir() {
		return report, fs.ErrInvalid
	}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			report.Unread = append(report.Unread, Failure{Path: path, Err: err})
			return nil
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return nil
		}
		acl, protect, changed, err := withoutGrants(path)
		if err != nil {
			report.Unread = append(report.Unread, Failure{Path: path, Err: err})
			return nil
		}
		if !changed {
			return nil
		}
		if err := writeDACL(path, acl, protect); err != nil {
			report.Refused = append(report.Refused, Failure{Path: path, Err: err})
			return nil
		}
		report.Stripped = append(report.Stripped, path)
		return nil
	})
	return report, walkErr
}

// withoutGrants reads path's DACL and returns it with the package grants gone.
func withoutGrants(path string) (acl []byte, protect, changed bool, err error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return nil, false, false, err
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		// No DACL names nobody, so there is nothing a package grant could be.
		return nil, false, false, nil
	}
	control, _, err := sd.Control()
	if err != nil {
		return nil, false, false, err
	}
	header := unsafe.Slice((*byte)(unsafe.Pointer(dacl)), aclHeaderSize)
	raw := unsafe.Slice((*byte)(unsafe.Pointer(dacl)), binary.LittleEndian.Uint16(header[2:4]))
	acl, protect, changed, err = rewrite(raw, control&windows.SE_DACL_PROTECTED != 0)
	runtime.KeepAlive(sd)
	return acl, protect, changed, err
}

func writeDACL(path string, acl []byte, protect bool) error {
	var inheritance windows.SECURITY_INFORMATION = windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	if protect {
		inheritance = windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|inheritance,
		nil, nil, (*windows.ACL)(unsafe.Pointer(&acl[0])), nil)
	runtime.KeepAlive(acl)
	return err
}
