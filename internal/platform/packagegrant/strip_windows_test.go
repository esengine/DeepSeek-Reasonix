//go:build windows

package packagegrant

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

const testPackageSID = "S-1-15-2-1658335055-2853777128-3014724382-1760870658-2056114279-2893136470-138490047"

func icacls(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("icacls", args...).CombinedOutput(); err != nil {
		t.Fatalf("icacls %v: %v\n%s", args, err, out)
	}
}

// entries lists what path's DACL says about the test package, as "allow",
// "allow-inherited" or "deny", plus whether the DACL is protected.
func entries(t *testing.T, path string) ([]string, bool) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatalf("dacl %s: %v", path, err)
	}
	control, _, _ := sd.Control()
	want, _ := windows.StringToSid(testPackageSID)
	var got []string
	for i := range uint32(dacl.AceCount) {
		var entry *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &entry); err != nil {
			t.Fatalf("ace %d: %v", i, err)
		}
		if !(*windows.SID)(unsafe.Pointer(&entry.SidStart)).Equals(want) {
			continue
		}
		switch {
		case entry.Header.AceType == aceTypeAccessAllowed && entry.Header.AceFlags&aceFlagInherited != 0:
			got = append(got, "allow-inherited")
		case entry.Header.AceType == aceTypeAccessAllowed:
			got = append(got, "allow")
		default:
			got = append(got, "deny")
		}
	}
	return got, control&windows.SE_DACL_PROTECTED != 0
}

func tree(t *testing.T) (parent, root, dll string) {
	parent = t.TempDir()
	root = filepath.Join(parent, "app")
	dll = filepath.Join(root, "ffmpeg.dll")
	if err := os.MkdirAll(filepath.Join(root, "resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dll, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return parent, root, dll
}

func TestAGrantOnTheRootLeavesEveryObjectUnderIt(t *testing.T) {
	_, root, dll := tree(t)
	icacls(t, root, "/grant", "*"+testPackageSID+":(OI)(CI)(RX)")
	if got, _ := entries(t, dll); !slices.Equal(got, []string{"allow-inherited"}) {
		t.Fatalf("setup: the dll carries %v", got)
	}

	report, err := Strip(root)
	if err != nil || len(report.Refused) != 0 {
		t.Fatalf("strip: %+v, %v", report, err)
	}
	for _, path := range []string{root, filepath.Join(root, "resources"), dll} {
		if got, protected := entries(t, path); len(got) != 0 || protected {
			t.Errorf("%s still carries %v (protected=%v)", path, got, protected)
		}
	}
	if !slices.Equal(report.Stripped, []string{root}) {
		t.Errorf("stripped %v, want only the root that held the entry", report.Stripped)
	}
	if _, err := os.ReadFile(dll); err != nil {
		t.Fatalf("the pass took this account's own access: %v", err)
	}
	if again, err := Strip(root); err != nil || len(again.Stripped) != 0 {
		t.Fatalf("a second pass changed %v, %v", again.Stripped, err)
	}
}

func TestAGrantFromAboveTheTreeIsCutAtTheRootAndTheParentIsUntouched(t *testing.T) {
	parent, root, dll := tree(t)
	icacls(t, parent, "/grant", "*"+testPackageSID+":(OI)(CI)(RX)")

	if _, err := Strip(root); err != nil {
		t.Fatal(err)
	}
	if got, _ := entries(t, parent); !slices.Equal(got, []string{"allow"}) {
		t.Fatalf("the parent is not this tree's to change, and now carries %v", got)
	}
	if got, protected := entries(t, root); len(got) != 0 || !protected {
		t.Fatalf("root carries %v, protected=%v", got, protected)
	}
	if got, _ := entries(t, dll); len(got) != 0 {
		t.Fatalf("dll still carries %v", got)
	}
	if _, err := os.ReadFile(dll); err != nil {
		t.Fatalf("the entries root inherited were not kept as its own: %v", err)
	}
}

func TestAGrantOnOneFileIsRemovedAndADenyStays(t *testing.T) {
	_, root, dll := tree(t)
	other := filepath.Join(root, "resources", "other.dll")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	icacls(t, dll, "/grant", "*"+testPackageSID+":(RX)")
	icacls(t, other, "/deny", "*"+testPackageSID+":(RX)")

	report, err := Strip(root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(report.Stripped, []string{dll}) {
		t.Fatalf("stripped %v, want only %s", report.Stripped, dll)
	}
	if got, protected := entries(t, dll); len(got) != 0 || protected {
		t.Fatalf("dll carries %v, protected=%v", got, protected)
	}
	if got, _ := entries(t, other); !slices.Equal(got, []string{"deny"}) {
		t.Fatalf("a deny narrows access and must stay; other.dll carries %v", got)
	}
}

func TestStripRefusesSomethingThatIsNotADirectory(t *testing.T) {
	_, _, dll := tree(t)
	if _, err := Strip(dll); err == nil {
		t.Fatal("a file was accepted as the tree to strip")
	}
}

func TestTheTestSIDIsAPackageByTheJudgement(t *testing.T) {
	s, err := windows.StringToSid(testPackageSID)
	if err != nil {
		t.Fatal(err)
	}
	raw := unsafe.Slice((*byte)(unsafe.Pointer(s)), windows.GetLengthSid(s))
	entry := binary.LittleEndian.AppendUint32([]byte{aceTypeAccessAllowed, 0, 0, 0}, 0x1200a9)
	entry = append(entry, raw...)
	binary.LittleEndian.PutUint16(entry[2:4], uint16(len(entry)))
	if !grantsSpecificPackage(entry) {
		t.Fatal("the SID these tests grant is not one the judgement recognises, so they prove nothing")
	}
}
