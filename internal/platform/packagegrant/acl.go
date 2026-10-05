package packagegrant

import (
	"encoding/binary"
	"errors"
)

// Report is what one pass changed, what it found and could not change, and
// what it could not read. An unread object is not known to carry a grant.
type Report struct {
	Stripped []string  `json:"stripped"`
	Refused  []Failure `json:"refused"`
	Unread   []Failure `json:"unread"`
}

// Failure is one object the pass could not finish, and why.
type Failure struct {
	Path string `json:"path"`
	Err  error  `json:"-"`
}

// ErrMalformed marks an ACL whose sizes do not account for its bytes. Nothing is
// written back from one.
var ErrMalformed = errors.New("packagegrant: malformed ACL")

const (
	aclHeaderSize = 8
	aceHeaderSize = 4
	aceMaskSize   = 4

	aceTypeAccessAllowed         = 0x0
	aceTypeAccessAllowedCallback = 0x9
	aceFlagInherited             = 0x10

	sidMinSize          = 8
	appPackageAuthority = 15
	appPackageBaseRID   = 2
	// SECURITY_BUILTIN_APP_PACKAGE_RID_COUNT: the groups every package belongs to.
	builtinPackageRIDCount = 2
)

// rewrite returns acl without the allow entries that name a specific package.
// changed is false when there were none. protect says the result must be
// written as a protected DACL: a removed entry was inherited, so the object
// stops inheriting and keeps the rest of what it had, now as its own entries.
func rewrite(acl []byte, wasProtected bool) (out []byte, protect, changed bool, err error) {
	if len(acl) < aclHeaderSize {
		return nil, false, false, ErrMalformed
	}
	size := int(binary.LittleEndian.Uint16(acl[2:4]))
	count := int(binary.LittleEndian.Uint16(acl[4:6]))
	if size < aclHeaderSize || size > len(acl) {
		return nil, false, false, ErrMalformed
	}
	var kept [][]byte
	inheritedRemoved := false
	offset := aclHeaderSize
	for range count {
		if offset+aceHeaderSize > size {
			return nil, false, false, ErrMalformed
		}
		aceSize := int(binary.LittleEndian.Uint16(acl[offset+2 : offset+4]))
		if aceSize < aceHeaderSize || offset+aceSize > size {
			return nil, false, false, ErrMalformed
		}
		ace := acl[offset : offset+aceSize]
		offset += aceSize
		if grantsSpecificPackage(ace) {
			changed = true
			inheritedRemoved = inheritedRemoved || ace[1]&aceFlagInherited != 0
			continue
		}
		kept = append(kept, ace)
	}
	if !changed {
		return nil, false, false, nil
	}
	total := aclHeaderSize
	for _, ace := range kept {
		total += len(ace)
	}
	out = make([]byte, aclHeaderSize, total)
	out[0] = acl[0]
	binary.LittleEndian.PutUint16(out[2:4], uint16(total))
	binary.LittleEndian.PutUint16(out[4:6], uint16(len(kept)))
	for _, ace := range kept {
		start := len(out)
		out = append(out, ace...)
		if inheritedRemoved {
			out[start+1] &^= aceFlagInherited
		}
	}
	return out, wasProtected || inheritedRemoved, true, nil
}

// grantsSpecificPackage reports whether ace allows access to one app package
// rather than to a built-in package group. Entry types whose SID does not sit
// right after the mask are never matched, so they are always kept.
func grantsSpecificPackage(ace []byte) bool {
	if ace[0] != aceTypeAccessAllowed && ace[0] != aceTypeAccessAllowedCallback {
		return false
	}
	sid := ace[aceHeaderSize+aceMaskSize:]
	if len(sid) < sidMinSize {
		return false
	}
	subCount := int(sid[1])
	if len(sid) < sidMinSize+4*subCount || subCount <= builtinPackageRIDCount {
		return false
	}
	authority := sid[2:8]
	for _, b := range authority[:5] {
		if b != 0 {
			return false
		}
	}
	return authority[5] == appPackageAuthority && binary.LittleEndian.Uint32(sid[8:12]) == appPackageBaseRID
}
