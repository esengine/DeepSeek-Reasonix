package winres

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tc-hib/winres/version"
)

func TestMain(m *testing.M) {
	getTestData()
	os.Exit(m.Run())
}

func getTestData() {
	exec.Command("git", "clone", "https://github.com/tc-hib/winres-testdata.git", "testdata").Run()

	cmd := exec.Command("git", "fetch", "--all")
	cmd.Dir = "./testdata"
	cmd.Run()

	cmd = exec.Command("git", "reset", "--hard", "origin/main")
	cmd.Dir = "./testdata"
	cmd.Run()
}

func TestErrors(t *testing.T) {
	r := &ResourceSet{}
	var err error

	err = r.Set(RT_RCDATA, ID(0), 0, []byte{})
	if err == nil || err.Error() != errZeroID {
		t.Fail()
	}

	err = r.Set(ID(0), ID(1), 0, []byte{})
	if err == nil || err.Error() != errZeroID {
		t.Fail()
	}

	if r.Set(RT_RCDATA, ID(0xFFFF), 0, []byte{}) != nil {
		t.Fail()
	}

	err = r.Set(RT_RCDATA, Name(""), 0, []byte{})
	if err == nil || err.Error() != errEmptyName {
		t.Fail()
	}

	err = r.Set(Name(""), ID(1), 0, []byte{})
	if err == nil || err.Error() != errEmptyName {
		t.Fail()
	}

	if r.Set(RT_RCDATA, Name("look, i'm not a nice resource name"), 0, []byte{}) != nil {
		t.Fail()
	}

	err = r.Set(RT_RCDATA, Name("IAMNICER\x00"), 0, []byte{})
	if err == nil || err.Error() != errNameContainsNUL {
		t.Fail()
	}

	if r.Set(Name("look, i'm not a nice type name"), ID(1), 0, []byte{}) != nil {
		t.Fail()
	}

	err = r.Set(Name("IAMNICER\x00"), ID(42), 0, []byte{})
	if err == nil || err.Error() != errNameContainsNUL {
		t.Fail()
	}

	err = r.WriteObject(io.Discard, "*")
	if err == nil || err.Error() != errUnknownArch {
		t.Fail()
	}
}

func TestEmpty(t *testing.T) {
	rs := &ResourceSet{}
	checkResourceSet(t, rs, ArchI386)
}

func TestWinRes1(t *testing.T) {
	r := &ResourceSet{}

	r.Set(RT_MANIFEST, ID(1), LCIDDefault, []byte(manifest1))
	r.Set(Name("CUSTOM TYPE"), Name("CUSTOM RESOURCE"), 1033, []byte("Hello World!"))
	r.Set(Name("CUSTOM TYPE"), Name("CUSTOM RESOURCE"), 1036, []byte("Bonjour Monde !"))
	r.Set(Name("CUSTOM TYPE"), ID(42), 1033, []byte("# Hello World!"))
	r.Set(Name("CUSTOM TYPE"), ID(42), 1036, []byte("# Bonjour Monde !"))
	r.Set(RT_RCDATA, ID(1), 1033, []byte("## Hello World!"))
	r.Set(RT_RCDATA, ID(1), 1036, []byte("## Bonjour Monde !"))
	r.Set(RT_RCDATA, ID(42), 1033, []byte("### Hello World!"))
	r.Set(RT_RCDATA, ID(42), 1036, []byte("### Bonjour Monde !"))
	icon1 := loadICOFile(t, "icon.ico")
	cursor := loadCURFile(t, "cursor.cur")
	icon2 := loadPNGFileAsIcon(t, "cur-64x128.png", nil)
	icon3 := loadPNGFileAsIcon(t, "cur-32x64.png", []int{48, 16})
	icon4 := loadPNGFileAsIcon(t, "img.png", []int{128})
	r.SetIconTranslation(ID(1), 0, icon1)
	r.SetIconTranslation(ID(1), 1033, icon2)
	r.SetIconTranslation(ID(1), 1036, icon3)
	r.SetIconTranslation(Name("SUPERB ICON"), 0, icon4)
	r.SetIcon(ID(2), icon2)
	r.SetCursor(ID(1), cursor)
	v := version.Info{
		ProductVersion: [4]uint16{5, 6, 7, 8},
	}
	v.Set(1036, "Custom Info", "Very important information")
	v.Set(1036, version.ProductName, "A test for winres")
	v.Set(1036, version.ProductVersion, "0.0.0.0-αlpha-")
	v.Set(1036, version.CompanyName, "Test Corporation ltd")
	v.Flags.SpecialBuild = true
	v.FileVersion = [4]uint16{4, 42, 424, 4242}
	v.Timestamp = time.Date(1979, 7, 3, 0, 15, 0, 0, time.UTC)
	r.SetVersionInfo(v)

	checkResourceSet(t, r, ArchAMD64)
}

func TestWinRes2(t *testing.T) {
	r := &ResourceSet{}

	r.Set(RT_MANIFEST, ID(1), LCIDDefault, []byte(manifest1))

	j, _ := os.ReadFile(filepath.Join(testDataDir, "vi.json"))
	v := version.Info{}
	json.Unmarshal(j, &v)
	r.SetVersionInfo(v)

	r.SetIcon(ID(1), loadICOFile(t, "icon.ico"))

	checkResourceSet(t, r, ArchI386)
}

func TestWinRes3(t *testing.T) {
	rs := &ResourceSet{}
	rs.SetCursor(ID(1), loadCURFile(t, "cursor.cur"))
	checkResourceSet(t, rs, ArchARM)
}

func TestWinRes4(t *testing.T) {
	rs := &ResourceSet{}
	rs.SetCursor(ID(1), loadPNGFileAsCursor(t, "cur-32x64.png", 10, 7))
	rs.SetIcon(ID(1), loadPNGFileAsIcon(t, "cur-32x64.png", []int{1, 7, 11, 15, 22, 255, 256}))
	checkResourceSet(t, rs, ArchARM64)
}

func TestResourceSet_Count(t *testing.T) {
	rs := &ResourceSet{}
	rs.SetManifest(AppManifest{})
	rs.SetManifest(AppManifest{Identity: AssemblyIdentity{Name: "Hello"}})
	rs.Set(RT_RCDATA, ID(42), 0x40C, make([]byte, 8))
	rs.Set(RT_RCDATA, ID(42), 0x40C, make([]byte, 9))
	rs.Set(RT_RCDATA, Name("Data"), 0x40C, make([]byte, 6))
	rs.Set(RT_RCDATA, ID(42), 0x409, make([]byte, 7))
	rs.Set(RT_VERSION, ID(1), 0x409, make([]byte, 9))
	rs.Set(RT_CURSOR, ID(42), 0x409, make([]byte, 5))
	rs.Set(Name("1"), ID(1), 0x409, make([]byte, 1))
	if rs.Count() != 7 {
		t.Fail()
	}
}

func TestResourceSet_SetManifest(t *testing.T) {
	rs := &ResourceSet{}
	rs.SetManifest(AppManifest{})
	checkResourceSet(t, rs, ArchARM64)
}

func TestResourceSet_SetVersionInfo(t *testing.T) {
	rs := &ResourceSet{}
	vi := version.Info{}
	vi.FileVersion = [4]uint16{1, 2, 3, 4}
	vi.ProductVersion = [4]uint16{1, 2, 3, 4}
	vi.Set(0x0409, version.ProductName, "Good product")
	vi.Set(0x040C, version.ProductName, "Bon produit")
	rs.SetVersionInfo(vi)
	checkResourceSet(t, rs, ArchAMD64)
}

func TestResourceSet_Walk(t *testing.T) {
	rs := ResourceSet{}
	b := &bytes.Buffer{}

	walker := func(typeID, resID Identifier, langID uint16, data []byte) bool {
		fmt.Fprintf(b, "%T(%v) -> %T(%v) -> 0x%04X -> [%d]byte\n", typeID, typeID, resID, resID, langID, len(data))
		return resID != Name("STOP")
	}

	rs.Walk(walker)
	if b.String() != "" {
		t.Fail()
	}

	rs.Set(RT_RCDATA, ID(42), 0x40C, make([]byte, 8))
	rs.Set(RT_RCDATA, Name("Data"), 0x40C, make([]byte, 6))
	rs.Set(RT_RCDATA, ID(42), 0x409, make([]byte, 7))
	rs.Set(RT_VERSION, ID(1), 0x409, make([]byte, 9))
	rs.Set(RT_CURSOR, ID(42), 0x409, make([]byte, 5))
	rs.Set(Name("1"), ID(1), 0x409, make([]byte, 1))
	rs.Set(Name("1"), ID(2), 0x409, make([]byte, 2))
	rs.Set(Name("Hi"), ID(2), 0x409, make([]byte, 3))
	rs.Set(Name("hey"), ID(2), 0x409, make([]byte, 4))
	rs.Set(ID(99), Name("STOP"), 0x409, make([]byte, 4))
	rs.Set(ID(99), Name("TOO FAR"), 0x409, make([]byte, 4))
	rs.Walk(walker)
	expected := `winres.Name(1) -> winres.ID(1) -> 0x0409 -> [1]byte
winres.Name(1) -> winres.ID(2) -> 0x0409 -> [2]byte
winres.Name(Hi) -> winres.ID(2) -> 0x0409 -> [3]byte
winres.Name(hey) -> winres.ID(2) -> 0x0409 -> [4]byte
winres.ID(1) -> winres.ID(42) -> 0x0409 -> [5]byte
winres.ID(10) -> winres.Name(Data) -> 0x040C -> [6]byte
winres.ID(10) -> winres.ID(42) -> 0x0409 -> [7]byte
winres.ID(10) -> winres.ID(42) -> 0x040C -> [8]byte
winres.ID(16) -> winres.ID(1) -> 0x0409 -> [9]byte
winres.ID(99) -> winres.Name(STOP) -> 0x0409 -> [4]byte
`
	if b.String() != expected {
		t.Fail()
	}
}

func TestResourceSet_WalkType(t *testing.T) {
	rs := ResourceSet{}
	b := &bytes.Buffer{}

	walker := func(resID Identifier, langID uint16, data []byte) bool {
		fmt.Fprintf(b, "%T(%v) -> 0x%04X -> [%d]byte\n", resID, resID, langID, len(data))
		return resID != ID(999)
	}

	rs.WalkType(RT_RCDATA, walker)
	if b.String() != "" {
		t.Fail()
	}

	rs.Set(RT_RCDATA, ID(42), 0x401, make([]byte, 8))
	rs.Set(RT_RCDATA, Name("Data"), 0x402, make([]byte, 6))
	rs.Set(RT_RCDATA, ID(42), 0x403, make([]byte, 7))
	rs.Set(RT_RCDATA, ID(999), 0x404, make([]byte, 4))
	rs.Set(RT_RCDATA, ID(1000), 0x405, make([]byte, 4))
	rs.Set(RT_VERSION, ID(1), 0x409, make([]byte, 9))
	rs.Set(RT_CURSOR, ID(42), 0x409, make([]byte, 5))
	rs.Set(Name("1"), ID(1), 0x409, make([]byte, 1))
	rs.Set(Name("1"), ID(2), 0x409, make([]byte, 2))
	rs.Set(Name("Hi"), ID(2), 0x409, make([]byte, 3))
	rs.Set(Name("hey"), ID(2), 0x409, make([]byte, 4))
	rs.WalkType(RT_RCDATA, walker)
	expected := `winres.Name(Data) -> 0x0402 -> [6]byte
winres.ID(42) -> 0x0401 -> [8]byte
winres.ID(42) -> 0x0403 -> [7]byte
winres.ID(999) -> 0x0404 -> [4]byte
`

	if b.String() != expected {
		t.Fail()
	}
}

// language=manifest
const manifest1 = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">

<assemblyIdentity
	version="1.0.0.0"
	processorArchitecture="*"
	name="An App"
	type="win32"
/>

<application xmlns="urn:schemas-microsoft-com:asm.v3">
	<windowsSettings>
		<dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/PM</dpiAware>
	</windowsSettings>
</application>

<trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
	<security>
		<requestedPrivileges>
			<requestedExecutionLevel
				level="asInvoker"
				uiAccess="false"
			/>
		</requestedPrivileges>
	</security>
</trustInfo>

<compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
	<application>
		<supportedOS Id="{e2011457-1546-43c5-a5fe-008deee3d3f0}"/>
		<supportedOS Id="{35138b9a-5d96-4fbd-8e2d-a2440225f93a}"/>
		<supportedOS Id="{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}"/>
		<supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
		<supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
	</application>
</compatibility>

</assembly>
`

func Test_ResourceSet_set(t *testing.T) {
	rs := ResourceSet{}
	rs.set(ID(1), ID(2), 1, nil)
	rs.set(ID(1), ID(2), 3, []byte{})
	rs.set(ID(1), ID(1), 4, nil)
	rs.set(ID(1), ID(2), 4, nil)
	if rs.Count() != 1 {
		t.Fail()
	}
	rs.set(ID(1), ID(2), 3, nil)
	if len(rs.types) != 0 {
		t.Fail()
	}
	rs.set(Name("A"), Name("B"), 1, []byte{})
	rs.set(Name("A"), Name("B"), 2, []byte{})
	rs.set(Name("A"), Name("b"), 1, []byte{})
	if rs.Count() != 3 {
		t.Fail()
	}
	rs.set(Name("A"), Name("B"), 1, nil)
	if rs.Count() != 2 {
		t.Fail()
	}
	rs.set(Name("A"), Name("B"), 2, nil)
	if rs.Count() != 1 {
		t.Fail()
	}
	if _, exists := rs.types[Name("A")].resources[Name("B")]; exists {
		t.Fail()
	}
	rs.set(Name("A"), Name("b"), 1, nil)
	if len(rs.types) != 0 {
		t.Fail()
	}
	rs.set(RT_ICON, ID(4), 1, []byte{})
	rs.set(RT_ICON, ID(42), 2, []byte{})
	rs.set(RT_ICON, ID(1), 1, []byte{})
	rs.set(RT_ICON, Name("420"), 3, []byte{})
	if rs.lastIconID != 42 {
		t.Fail()
	}
	rs.set(RT_CURSOR, ID(2), 1, []byte{})
	rs.set(RT_CURSOR, ID(24), 2, []byte{})
	rs.set(RT_CURSOR, ID(1), 1, []byte{})
	rs.set(RT_CURSOR, Name("420"), 3, []byte{})
	if rs.lastCursorID != 24 {
		t.Fail()
	}
	rs.set(RT_ICON, ID(42), 2, nil)
	rs.set(RT_CURSOR, ID(24), 2, nil)
	if rs.lastIconID != 42 || rs.lastCursorID != 24 {
		t.Fatal("delete is not supposed to rollback lastCursorID/lastIconID")
	}
	rs.set(RT_CURSOR, ID(2), 1, nil)
	rs.set(RT_CURSOR, ID(1), 1, nil)
	rs.set(RT_CURSOR, Name("420"), 3, nil)
	if len(rs.types) != 1 {
		t.Fail()
	}
}

func Test_ResourceSet_firstLang(t *testing.T) {
	rs := ResourceSet{}

	rs.set(ID(1), ID(2), 3, []byte{1})
	if rs.types[ID(1)].resources[ID(2)].orderedKeys != nil {
		t.Fail()
	}
	rs.order(&state{})
	if len(rs.types[ID(1)].resources[ID(2)].orderedKeys) != 1 {
		t.Fail()
	}
	if rs.firstLang(ID(1), ID(2)) != 3 {
		t.Fail()
	}
	rs.set(ID(1), ID(2), 2, []byte{2})
	if rs.types[ID(1)].resources[ID(2)].orderedKeys != nil {
		t.Fail()
	}
	rs.set(ID(1), ID(2), 1, []byte{3})
	rs.set(ID(1), ID(2), 4, []byte{4})
	if rs.firstLang(ID(1), ID(2)) != 1 {
		t.Fail()
	}
	rs.set(ID(1), ID(2), 0, []byte{5})
	if rs.firstLang(ID(1), ID(2)) != 0 {
		t.Fail()
	}
	rs.set(ID(1), ID(2), 0, nil)
	rs.set(ID(1), ID(2), 1, nil)
	if rs.firstLang(ID(1), ID(2)) != 2 {
		t.Fail()
	}
	rs.set(ID(1), ID(2), 2, nil)
	if rs.firstLang(ID(1), ID(2)) != 3 {
		t.Fail()
	}
	// Make up impossible case
	delete(rs.types[ID(1)].resources[ID(2)].data, 3)
	delete(rs.types[ID(1)].resources[ID(2)].data, 4)
	if rs.firstLang(ID(1), ID(2)) != 0 {
		t.Fail()
	}
}

func TestResourceSet_WriteToEXE_VS(t *testing.T) {
	exe, _ := os.Open(filepath.Join(testDataDir, "vs.exe"))
	defer exe.Close()

	rs, err := LoadFromEXE(exe)
	if err != nil {
		t.Fatal(err)
	}

	rs.Set(Name("AAA"), Name("AAA"), 0x409, []byte{1})
	ico := loadPNGFileAsIcon(t, "img.png", nil)
	rs.SetIcon(Name("aAA"), ico)
	rs.SetIcon(Name("APPICON"), loadICOFile(t, "icon.ico"))

	buf := bytes.Buffer{}
	err = rs.WriteToEXE(&buf, exe)
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestResourceSet_WriteToEXE_VS0(t *testing.T) {
	exe, _ := os.Open(filepath.Join(testDataDir, "vs0.exe"))
	defer exe.Close()

	rs, err := LoadFromEXE(exe)
	if rs == nil {
		t.Fatal(err)
	}

	rs.Set(Name("AAA"), Name("AAA"), 0x409, []byte{1})
	ico := loadPNGFileAsIcon(t, "img.png", nil)
	rs.SetIcon(Name("aAA"), ico)
	rs.SetIcon(Name("APPICON"), loadICOFile(t, "icon.ico"))
	rs.SetManifest(AppManifest{
		Identity: AssemblyIdentity{
			Name:    "Some app",
			Version: [4]uint16{1, 2, 3, 4},
		},
		Description:         "This is an application",
		SegmentHeap:         true,
		UseCommonControlsV6: true,
		Compatibility:       Win81AndAbove,
		DPIAwareness:        DPIPerMonitorV2,
	})

	buf := bytes.Buffer{}
	err = rs.WriteToEXE(&buf, exe)
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestResourceSet_WriteToEXE_VS032(t *testing.T) {
	exe, _ := os.Open(filepath.Join(testDataDir, "vs032.exe"))
	defer exe.Close()

	rs, err := LoadFromEXE(exe)
	if rs == nil {
		t.Fatal(err)
	}

	rs.SetManifest(AppManifest{})

	buf := writeSeeker{}
	err = rs.WriteToEXE(&buf, exe)
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestResourceSet_WriteToEXE_VS32(t *testing.T) {
	exe, _ := os.Open(filepath.Join(testDataDir, "vs32.exe"))
	defer exe.Close()

	rs, err := LoadFromEXE(exe)
	if err != nil {
		t.Fatal(err)
	}

	rs.SetIcon(Name("APPICON"), loadICOFile(t, "icon.ico"))

	buf := bytes.Buffer{}
	err = rs.WriteToEXE(&buf, exe)
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestResourceSet_WriteToEXEWithCheckSum_VS32(t *testing.T) {
	exe, _ := os.Open(filepath.Join(testDataDir, "vs32.exe"))
	defer exe.Close()

	rs, err := LoadFromEXE(exe)
	if err != nil {
		t.Fatal(err)
	}

	rs.SetIcon(Name("APPICON"), loadICOFile(t, "icon.ico"))

	buf := bytes.Buffer{}
	err = rs.WriteToEXE(&buf, onlyReadSeeker{exe}, ForceCheckSum())
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestResourceSet_WriteToEXEWithCheckSum_VS(t *testing.T) {
	exe, _ := os.Open(filepath.Join(testDataDir, "vs.exe"))
	defer exe.Close()

	rs, err := LoadFromEXE(exe)
	if err != nil {
		t.Fatal(err)
	}

	rs.Set(Name("AAA"), Name("AAA"), 0x409, []byte{1})
	ico := loadPNGFileAsIcon(t, "img.png", nil)
	rs.SetIcon(Name("aAA"), ico)
	rs.SetIcon(Name("APPICON"), loadICOFile(t, "icon.ico"))

	buf := bytes.Buffer{}
	err = rs.WriteToEXE(&buf, exe, ForceCheckSum())
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestResourceSet_WriteToEXE_End(t *testing.T) {
	exe, _ := os.Open(filepath.Join(testDataDir, "end.exe"))
	defer exe.Close()

	rs, err := LoadFromEXE(exe)
	if err != nil {
		t.Fatal(err)
	}

	rs.SetIcon(Name("APPICON"), loadICOFile(t, "icon.ico"))

	buf := bytes.Buffer{}
	err = rs.WriteToEXE(&buf, exe)
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestResourceSet_WriteToEXE_NotEnd(t *testing.T) {
	exe, _ := os.Open(filepath.Join(testDataDir, "notend.exe"))
	defer exe.Close()

	rs, err := LoadFromEXE(exe)
	if err != nil {
		t.Fatal(err)
	}

	rs.SetIcon(Name("APPICON"), loadICOFile(t, "icon.ico"))

	buf := bytes.Buffer{}
	err = rs.WriteToEXE(&buf, exe)
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestResourceSet_WriteToEXE_SignedErr(t *testing.T) {
	exe, _ := os.Open(filepath.Join(testDataDir, "signed.exe"))
	defer exe.Close()

	rs, err := LoadFromEXE(exe)
	if err != nil {
		t.Fatal(err)
	}

	buf := bytes.Buffer{}
	err = rs.WriteToEXE(&buf, exe)
	if err != ErrSignedPE {
		t.Fatal("expected error:\n", ErrSignedPE, "\ngot:\n", err)
	}
}

func TestResourceSet_WriteToEXE_IgnoreSignature(t *testing.T) {
	exe, _ := os.Open(filepath.Join(testDataDir, "signed.exe"))
	defer exe.Close()

	rs, err := LoadFromEXE(exe)
	if err != nil {
		t.Fatal(err)
	}

	rs.SetIcon(Name("APPICON"), loadICOFile(t, "icon.ico"))

	buf := bytes.Buffer{}
	err = rs.WriteToEXE(&buf, exe, WithAuthenticode(IgnoreSignature))
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestResourceSet_WriteToEXE_RemoveSignature(t *testing.T) {
	exe, _ := os.Open(filepath.Join(testDataDir, "signed.exe"))
	defer exe.Close()

	rs, err := LoadFromEXE(exe)
	if err != nil {
		t.Fatal(err)
	}

	buf := bytes.Buffer{}
	err = rs.WriteToEXE(&buf, exe, WithAuthenticode(RemoveSignature))
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestLoadFromEXESingleType(t *testing.T) {
	exe, err := os.Open(filepath.Join(testDataDir, "rh.exe"))
	if err != nil {
		t.Fatal(err)
	}
	defer exe.Close()

	rs, err := LoadFromEXESingleType(exe, RT_GROUP_ICON)
	if err != nil {
		t.Fatal(err)
	}

	if rs.Count() != 21 || rs.lastIconID != 18 {
		t.Fail()
	}

	buf := bytes.Buffer{}
	rs.WriteObject(&buf, ArchAMD64)
	checkBinary(t, buf.Bytes())
}

func TestLoadFromEXESingleType_Err(t *testing.T) {
	exe, err := os.Open(filepath.Join(testDataDir, "rh.exe"))
	if err != nil {
		t.Fatal(err)
	}
	exe.Close()

	rs, err := LoadFromEXESingleType(exe, RT_GROUP_ICON)
	if err == nil || rs != nil {
		t.Fail()
	}
	rs, err = LoadFromEXESingleType(exe, ID(0))
	if err == nil || rs != nil || err.Error() != errZeroID {
		t.Fail()
	}

	exe, err = os.Open(filepath.Join(testDataDir, "invalid_rsrc.exe"))
	if err != nil {
		t.Fatal(err)
	}
	defer exe.Close()

	rs, err = LoadFromEXESingleType(exe, RT_GROUP_ICON)
	if err != io.ErrUnexpectedEOF || rs != nil {
		t.Fail()
	}
}

func TestResourceSet_LoadFromEXE_Err(t *testing.T) {
	rs, err := LoadFromEXE(bytes.NewReader([]byte{'N', 'Z', 0x40: 0}))
	if err == nil || rs != nil || err.Error() != errNotPEImage {
		t.Error(err)
	}

	rs, err = LoadFromEXE(bytes.NewReader([]byte{'M', 'Z'}))
	if err != io.ErrUnexpectedEOF || rs != nil {
		t.Error(err)
	}

	rs, err = LoadFromEXE(bytes.NewReader([]byte{'M', 'Z'}))
	if err != io.ErrUnexpectedEOF || rs != nil {
		t.Error(err)
	}

	b := loadBinary(t, "vs.exe")
	b = b[:0x160]
	rs, err = LoadFromEXE(bytes.NewReader(b))
	if err != io.ErrUnexpectedEOF || rs != nil {
		t.Error(err)
	}

	b = loadBinary(t, "vs.exe")
	b[0x3C]++ // corrupt offset to PE signature
	rs, err = LoadFromEXE(bytes.NewReader(b))
	if err == nil || rs != nil || err.Error() != errNotPEImage {
		t.Error(err)
	}

	b = loadBinary(t, "vs.exe")
	b[0x191] = 0x80 // corrupt resource directory address
	rs, err = LoadFromEXE(bytes.NewReader(b))
	if err == nil || rs != nil || err.Error() != errRSRCNotFound {
		t.Error(err)
	}

	b = loadBinary(t, "vs.exe")
	b[0x2B1] = 0x06 // add 0x400 to the size of the resource section
	rs, err = LoadFromEXE(bytes.NewReader(b))
	if err == nil || rs != nil || err.Error() != errSectionTooFar {
		t.Error(err)
	}

	b = loadBinary(t, "vs.exe")
	b[0x110]++ // PE magic
	rs, err = LoadFromEXE(bytes.NewReader(b))
	if err == nil || rs != nil || err.Error() != errUnknownPE {
		t.Error(err)
	}
	b[0x110]--
	b[0x111]++
	rs, err = LoadFromEXE(bytes.NewReader(b))
	if err == nil || rs != nil || err.Error() != errUnknownPE {
		t.Error(err)
	}

	b = loadBinary(t, "vs32.exe")
	b[0x10C] = 0x5F
	rs, err = LoadFromEXE(bytes.NewReader(b))
	if err != io.ErrUnexpectedEOF || rs != nil {
		t.Error(err)
	}

	b = loadBinary(t, "vs32.exe")
	b[0x16C] = 0x05
	rs, err = LoadFromEXE(bytes.NewReader(b))
	if err == nil || rs != nil || err.Error() != errUnknownPE {
		t.Error(err)
	}

	b = loadBinary(t, "vs32.exe")
	b[0x10C] = 0xDF
	rs, err = LoadFromEXE(bytes.NewReader(b))
	if err == nil || rs != nil || err.Error() != errNotPEImage {
		t.Error(err)
	}

	b = loadBinary(t, "vs32.exe")
	br := badReader{
		br:     bytes.NewReader(b),
		errPos: 0x1EF,
	}
	rs, err = LoadFromEXE(&br)
	if !isExpectedReadErr(err) || rs != nil {
		t.Fatal("expected read error, got", err)
	}

	b = loadBinary(t, "vs32.exe")
	br = badReader{
		br:     bytes.NewReader(b),
		errPos: 0x201,
	}
	rs, err = LoadFromEXE(&br)
	if !isExpectedReadErr(err) || rs != nil {
		t.Fatal("expected read error, got", err)
	}

	b = loadBinary(t, "vs0.exe")
	rs, err = LoadFromEXE(bytes.NewReader(b))
	if err == nil || rs == nil || rs.types != nil || err.Error() != errNoRSRC {
		t.Error(err)
	}

	b = loadBinary(t, "vs.exe")
	br = badReader{
		br:     bytes.NewReader(b),
		errPos: 0x2A60,
	}
	rs, err = LoadFromEXE(&br)
	if !isExpectedReadErr(err) || rs != nil {
		t.Fatal("expected read error, got", err)
	}
}

func TestResourceSet_WriteToEXE_Err(t *testing.T) {
	data := loadBinary(t, "vs.exe")
	data0 := loadBinary(t, "vs0.exe")

	tt := []struct {
		w       io.Writer
		data    []byte
		errMsg  string
		poke    []poke
		badSeek int
	}{
		{w: newBadWriter(252), data: data, errMsg: errWrite},
		{w: io.Discard, data: data, errMsg: errRSRCTwice, poke: []poke{{off: 0x25D, val: 0x60}}},
		{w: io.Discard, data: data, errMsg: errRelocTwice, poke: []poke{{off: 0x25D, val: 0x70}}},
		{w: &writeSeeker{bad: 0xA0}, data: data, errMsg: errWrite},
		{w: &writeSeeker{bad: 0xFE}, data: data, errMsg: errWrite},
		{w: &writeSeeker{bad: 0x140}, data: data, errMsg: errWrite},
		{w: &writeSeeker{bad: 0x180}, data: data, errMsg: errWrite},
		{w: &writeSeeker{bad: 0x200}, data: data, errMsg: errWrite},
		{w: &writeSeeker{bad: 0x300}, data: data, errMsg: errWrite},
		{w: newBadWriter(0x300), data: data, errMsg: errWrite},
		{w: &writeSeeker{bad: 0x27E0}, data: data, errMsg: errWrite},
		{w: &writeSeeker{bad: 0x2BF8}, data: data0[:0x2B00], errMsg: errWrite, poke: []poke{{off: 0x2A1, val: 0x01}}},
		{w: &writeSeeker{bad: 0x2A08}, data: data, errMsg: errWrite},
		{w: &writeSeeker{bad: 0x2A18}, data: data, errMsg: errWrite},
		{w: &writeSeeker{bad: 0x2C08}, data: data, errMsg: errWrite},
		{w: io.Discard, data: data, badSeek: 5, errMsg: errSeek},
		{w: io.Discard, data: data, badSeek: 6, errMsg: errSeek},
		{w: io.Discard, data: data, badSeek: 7, errMsg: errSeek},
		{w: io.Discard, data: data, badSeek: 8, errMsg: errSeek},
		{
			w:      io.Discard,
			data:   data0,
			errMsg: errNoRoomForRSRC,
			poke: []poke{
				{off: 0x205, val: 0x02},
				{off: 0x204, val: 0xB8},
			},
		},
	}

	for i := range tt {
		rs := ResourceSet{}

		b := append([]byte{}, tt[i].data...)
		for _, p := range tt[i].poke {
			b[p.off] = p.val
		}

		var r io.ReadSeeker = bytes.NewReader(b)
		if tt[i].badSeek > 0 {
			r = &badSeeker{
				br:      bytes.NewReader(b),
				errIter: tt[i].badSeek,
			}
		}
		err := rs.WriteToEXE(tt[i].w, r)

		if err == nil || err.Error() != tt[i].errMsg {
			t.Error(i, "got:", err, "\nexpected:", tt[i].errMsg)
		}
	}
}

func TestResourceSet_WriteToEXE_Delete(t *testing.T) {
	exe, _ := os.Open(filepath.Join(testDataDir, "notend.exe"))
	defer exe.Close()

	rs, err := LoadFromEXE(exe)
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 12; i++ {
		rs.Set(RT_ICON, ID(i), 0, nil)
		rs.Set(RT_CURSOR, ID(i), 0, nil)
	}
	rs.Set(Name("PNG"), Name("CUR-16X8"), 0, nil)
	rs.Set(RT_RCDATA, ID(1), 0x409, []byte{})

	buf := bytes.Buffer{}
	err = rs.WriteToEXE(&buf, exe)
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestResourceSet_WriteToEXE_SFX(t *testing.T) {
	// Self extracting archives have data after the last section of the executable
	exe, _ := os.Open(filepath.Join(testDataDir, "sfx.exe"))
	defer exe.Close()

	rs, err := LoadFromEXE(exe)
	if err != nil {
		t.Fatal(err)
	}

	// Replace file properties
	vi, err := version.FromBytes(rs.Get(RT_VERSION, ID(1), LCIDDefault))
	if err != nil {
		t.Fatal(err)
	}
	vi.SetProductVersion("1.0.0.42")
	vi.Set(LCIDDefault, version.ProductName, "My Archive")
	vi.Set(LCIDDefault, version.CompanyName, "My Company")
	vi.Set(LCIDDefault, version.LegalCopyright, "My copyright (but thanks to 7z author)")
	rs.SetVersionInfo(*vi)
	// Replace the icon
	rs.Set(RT_ICON, ID(1), LCIDDefault, nil)
	rs.Set(RT_ICON, ID(2), LCIDDefault, nil)
	rs.SetIconTranslation(ID(1), LCIDDefault, loadICOFile(t, "en.ico"))
	rs.SetIconTranslation(ID(1), 0x40C, loadICOFile(t, "fr.ico"))
	// Add a manifest for a better GUI on high DPI
	rs.SetManifest(AppManifest{
		DPIAwareness:        DPIPerMonitorV2,
		UseCommonControlsV6: true,
	})

	buf := bytes.Buffer{}
	err = rs.WriteToEXE(&buf, exe)
	if err != nil {
		t.Fatal(err)
	}

	checkBinary(t, buf.Bytes())
}

func TestResourceSet_WriteToEXE_EOF(t *testing.T) {
	data := loadBinary(t, "vs.exe")

	tt := []struct {
		data []byte
	}{
		{data: []byte{'M', 'Z', 0x3C: 0x40, 0x40: 'P', 'E'}},
		{data: []byte{'M', 'Z', 0x3C: 0x40, 0x40: 'P', 'E', 0x44: 0}},
		{data: data[:len(data)-0x1FF]},
	}

	for i := range tt {
		rs := ResourceSet{}

		err := rs.WriteToEXE(io.Discard, bytes.NewReader(tt[i].data))

		if err != io.ErrUnexpectedEOF {
			t.Error(i, err)
		}
	}
}

func TestIsSignedEXE_False(t *testing.T) {
	f, err := os.Open(filepath.Join(testDataDir, "sfx.exe"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	s, err := IsSignedEXE(f)
	if err != nil {
		t.Fatal(err)
	}
	if s {
		t.Fatal("expected IsSignedEXE to return false, got true")
	}
}

func TestIsSignedEXE_True(t *testing.T) {
	f, err := os.Open(filepath.Join(testDataDir, "signed.exe"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	f.Seek(42, io.SeekStart)
	s, err := IsSignedEXE(f)
	if err != nil {
		t.Fatal(err)
	}
	if !s {
		t.Fatal("expected IsSignedEXE to return true, got false")
	}
	p, _ := f.Seek(0, io.SeekCurrent)
	if p != 42 {
		t.Fatal("expected IsSignedEXE to restore reader's position, but it didn't")
	}
}

func TestIsSignedEXE_Error(t *testing.T) {
	r := bytes.NewReader([]byte{'N', 'Z', 0x40: 0})
	r.Seek(2, io.SeekStart)
	_, err := IsSignedEXE(r)
	if err == nil {
		t.Fatal("expected an error, didn't get one")
	}
	p, _ := r.Seek(0, io.SeekCurrent)
	if p != 2 {
		t.Fatal("expected IsSignedEXE to restore reader's position, but it didn't")
	}
}

type onlyReadSeeker struct {
	io.ReadSeeker
}

type poke struct {
	off int
	val byte
}

type writeSeeker struct {
	buf bytes.Buffer
	pos int64
	end int64
	bad int64
}

func (ws *writeSeeker) Write(data []byte) (int, error) {
	if ws.bad > 0 && ws.end <= ws.bad && ws.bad < ws.pos+int64(len(data)) {
		return 0, errors.New(errWrite)
	}
	if ws.pos < int64(ws.buf.Len()) {
		ws.buf.Truncate(int(ws.pos))
	} else if ws.pos > int64(ws.buf.Len()) {
		ws.buf.Write(make([]byte, ws.pos-int64(ws.buf.Len())))
	}
	n, err := ws.buf.Write(data)
	ws.pos += int64(n)
	if ws.end < ws.pos {
		ws.end = ws.pos
	}
	return n, err
}

func (ws *writeSeeker) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		ws.pos = off
	case io.SeekCurrent:
		ws.pos = int64(ws.buf.Len()) + off
	case io.SeekEnd:
		ws.pos = ws.end + off
	}
	return ws.pos, nil
}

func (ws writeSeeker) Bytes() []byte {
	return ws.buf.Bytes()
}
