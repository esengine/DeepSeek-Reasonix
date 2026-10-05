package packagegrant

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

const aceTypeAccessDenied = 0x1

// sid encodes S-1-<authority>-<subs...> the way Windows lays one out in an ACE.
func sid(authority byte, subs ...uint32) []byte {
	b := []byte{1, byte(len(subs)), 0, 0, 0, 0, 0, authority}
	for _, s := range subs {
		b = binary.LittleEndian.AppendUint32(b, s)
	}
	return b
}

func ace(kind, flags byte, s []byte) []byte {
	b := []byte{kind, flags, 0, 0}
	b = binary.LittleEndian.AppendUint32(b, 0x1200a9)
	b = append(b, s...)
	binary.LittleEndian.PutUint16(b[2:4], uint16(len(b)))
	return b
}

func acl(aces ...[]byte) []byte {
	b := make([]byte, aclHeaderSize)
	b[0] = 2
	for _, a := range aces {
		b = append(b, a...)
	}
	binary.LittleEndian.PutUint16(b[2:4], uint16(len(b)))
	binary.LittleEndian.PutUint16(b[4:6], uint16(len(aces)))
	return b
}

var (
	user         = sid(5, 21, 1111111111, 2222222222, 3333333333, 1001)
	system       = sid(5, 18)
	allPackages  = sid(15, 2, 1)
	allRestrict  = sid(15, 2, 2)
	capability   = sid(15, 3, 1024, 1, 2, 3, 4, 5, 6, 7)
	livePackage  = sid(15, 2, 1658335055, 2853777128, 3014724382, 1760870658, 2056114279, 2893136470, 138490047)
	shortPackage = sid(15, 2, 999, 999, 999)
)

func TestOnlySpecificPackageAllowEntriesAreRemoved(t *testing.T) {
	keep := [][]byte{
		ace(aceTypeAccessAllowed, 0, user),
		ace(aceTypeAccessAllowed, 0, system),
		ace(aceTypeAccessAllowed, 0, allPackages),
		ace(aceTypeAccessAllowed, 0, allRestrict),
		ace(aceTypeAccessAllowed, 0, capability),
		ace(aceTypeAccessDenied, 0, livePackage),
	}
	for name, grant := range map[string][]byte{
		"a package whose SID has the full hash": ace(aceTypeAccessAllowed, 0, livePackage),
		"a package SID of any other length":     ace(aceTypeAccessAllowed, 0, shortPackage),
		"a callback allow entry":                ace(aceTypeAccessAllowedCallback, 0, livePackage),
	} {
		in := acl(append(append([][]byte{}, keep...), grant)...)
		out, protect, changed, err := rewrite(in, false)
		if err != nil || !changed || protect {
			t.Fatalf("%s: changed=%v protect=%v err=%v", name, changed, protect, err)
		}
		if want := acl(keep...); !bytes.Equal(out, want) {
			t.Fatalf("%s: kept entries differ\n got %x\nwant %x", name, out, want)
		}
	}
}

// A pass narrows access: a deny naming a package stays, and so does every
// entry the judgement has no reason to read.
func TestAnACLWithoutPackageGrantsIsLeftAlone(t *testing.T) {
	in := acl(
		ace(aceTypeAccessDenied, 0, livePackage),
		ace(aceTypeAccessAllowed, aceFlagInherited, allPackages),
		ace(aceTypeAccessAllowed, 0, capability),
		ace(0x5, 0, livePackage),
	)
	out, protect, changed, err := rewrite(in, true)
	if err != nil || changed || protect || out != nil {
		t.Fatalf("out=%x protect=%v changed=%v err=%v", out, protect, changed, err)
	}
}

func TestAnInheritedGrantProtectsTheObjectAndKeepsWhatItInherited(t *testing.T) {
	in := acl(
		ace(aceTypeAccessAllowed, 0, user),
		ace(aceTypeAccessAllowed, aceFlagInherited|0x3, livePackage),
		ace(aceTypeAccessAllowed, aceFlagInherited|0x3, system),
	)
	out, protect, changed, err := rewrite(in, false)
	if err != nil || !changed || !protect {
		t.Fatalf("changed=%v protect=%v err=%v", changed, protect, err)
	}
	want := acl(ace(aceTypeAccessAllowed, 0, user), ace(aceTypeAccessAllowed, 0x3, system))
	if !bytes.Equal(out, want) {
		t.Fatalf("inherited entries were not kept as the object's own\n got %x\nwant %x", out, want)
	}
}

func TestAProtectedDACLStaysProtected(t *testing.T) {
	in := acl(ace(aceTypeAccessAllowed, 0, user), ace(aceTypeAccessAllowed, 0, livePackage))
	if _, protect, changed, err := rewrite(in, true); err != nil || !changed || !protect {
		t.Fatalf("changed=%v protect=%v err=%v", changed, protect, err)
	}
}

func TestSizesThatDoNotAccountForTheBytesAreRefused(t *testing.T) {
	good := acl(ace(aceTypeAccessAllowed, 0, livePackage))
	cases := map[string][]byte{
		"shorter than a header": good[:4],
		"size past the buffer":  func() []byte { b := bytes.Clone(good); binary.LittleEndian.PutUint16(b[2:4], 0xffff); return b }(),
		"count past the size":   func() []byte { b := bytes.Clone(good); binary.LittleEndian.PutUint16(b[4:6], 2); return b }(),
		"entry past the size":   func() []byte { b := bytes.Clone(good); binary.LittleEndian.PutUint16(b[10:12], 0xff); return b }(),
		"entry shorter than its header": func() []byte {
			b := bytes.Clone(good)
			binary.LittleEndian.PutUint16(b[10:12], 2)
			return b
		}(),
	}
	for name, in := range cases {
		if _, _, _, err := rewrite(in, false); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: err=%v, want ErrMalformed", name, err)
		}
	}
}

func TestATruncatedSIDIsNeverAPackage(t *testing.T) {
	entry := ace(aceTypeAccessAllowed, 0, livePackage)
	truncated := entry[:aceHeaderSize+aceMaskSize+sidMinSize+4]
	binary.LittleEndian.PutUint16(truncated[2:4], uint16(len(truncated)))
	if grantsSpecificPackage(truncated) {
		t.Fatal("a SID shorter than its sub-authority count was read as a package")
	}
}
