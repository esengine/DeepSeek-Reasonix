package version

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func ExampleInfo_Set() {
	vi := Info{}
	// 0x409 is en-US, and the default language
	vi.Set(0x409, ProductName, "Good Product")
	// 0x40C is fr-FR
	vi.Set(0x40C, ProductName, "Bon Produit")
	// 0 is neutral
	vi.Set(0, "Smile", "😀")
}

func ExampleInfo() {
	vi := Info{}
	// Set some info in the fixed structure
	vi.ProductVersion = [4]uint16{1, 0, 0, 1}
	// Set some info in the string table
	vi.Set(0x409, ProductName, "Good Product")
	vi.Set(0x40C, ProductName, "Bon Produit")
	// Once it's complete, make a resource
	// resourceData := vi.Bytes()
	// ...
}

func TestMergeTranslations(t *testing.T) {
	trans := map[uint16]*Info{}

	var (
		v000, v401, v402, v409 Info
		vMerged                *Info
		b                      []byte
	)

	vMerged = MergeTranslations(trans)
	if vMerged == nil || !reflect.DeepEqual(vMerged, &Info{}) {
		t.Fail()
	}

	trans[0x401] = &v401
	trans[0x402] = &v402

	v401.FileVersion = [4]uint16{0, 4, 0, 1}
	v401.Set(0x000, "k1", "v1.401.000")
	v401.Set(0x401, "k3", "v3.401")

	v402.FileVersion = [4]uint16{0, 4, 0, 2}
	v402.Set(0x401, "k1", "v1.402.401")
	v402.Set(0x000, "k2", "v2.402.000")

	vMerged = MergeTranslations(trans)
	b, _ = json.MarshalIndent(vMerged, "", "  ")
	//language=JSON
	if string(b) != `{
  "fixed": {
    "file_version": "0.4.0.1"
  },
  "info": {
    "0401": {
      "k3": "v3.401"
    },
    "0402": {
      "k2": "v2.402.000"
    }
  }
}` {
		t.Fail()
	}

	trans[0x409] = &v409
	v409.FileVersion = [4]uint16{0, 4, 0, 9}
	v401.lt[0x401] = nil
	v401.Set(0x402, "k3", "v3.401.402")
	delete(*v402.lt[0x000], "k2")
	v402.Set(0x401, "k2", "v2.402.401")
	vMerged = MergeTranslations(trans)
	b, _ = json.MarshalIndent(vMerged, "", "  ")
	//language=JSON
	if string(b) != `{
  "fixed": {
    "file_version": "0.4.0.9"
  },
  "info": {
    "0401": {
      "k1": "v1.401.000"
    },
    "0402": {
      "k1": "v1.402.401",
      "k2": "v2.402.401"
    }
  }
}` {
		t.Fail()
	}

	trans[0x000] = &v000
	v000.FileVersion = [4]uint16{}
	v402.Set(0x403, "k2", "v2.402.403")
	vMerged = MergeTranslations(trans)
	b, _ = json.MarshalIndent(vMerged, "", "  ")
	//language=JSON
	if string(b) != `{
  "info": {
    "0401": {
      "k1": "v1.401.000"
    }
  }
}` {
		fmt.Println(string(b))
		t.Fail()
	}
}

func TestFromBytes1(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(b, vi.Bytes()) {
		t.Fail()
	}
}

func TestFromBytes2(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(b, vi.Bytes()) {
		t.Fail()
	}
}

func TestFromBytes_DLL(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(b, vi.Bytes()) || vi.Type != DLL {
		t.Fail()
	}
}

func TestFromBytes_Empty(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(b, vi.Bytes()) {
		t.Fail()
	}
}

func TestFromBytes_ErrCodePage(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err == nil || vi != nil {
		t.Fail()
		return
	}
	if err.Error() != errUnhandledCodePage {
		t.Fail()
	}
}

func TestFromBytes_ErrEOF1(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err != io.ErrUnexpectedEOF || vi != nil {
		t.Fail()
	}
}

func TestFromBytes_ErrEOF2(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err != io.ErrUnexpectedEOF || vi != nil {
		t.Fail()
	}
}

func TestFromBytes_ErrEOF3(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err != io.ErrUnexpectedEOF || vi != nil {
		t.Fail()
	}
}

func TestFromBytes_ErrInvalidLangID(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err == nil || vi != nil {
		t.Fail()
		return
	}
	if err.Error() != errInvalidLangID {
		t.Fail()
	}
}

func TestFromBytes_ErrInvalidSignature(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err == nil || vi != nil {
		t.Fail()
		return
	}
	if err.Error() != errInvalidSignature {
		t.Fail()
	}
}

func TestFromBytes_ErrLength1(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err == nil || vi != nil {
		t.Fail()
		return
	}
	if err.Error() != errInvalidLength {
		t.Fail()
	}
}

func TestFromBytes_ErrLength2(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err == nil || vi != nil {
		t.Fail()
		return
	}
	if err.Error() != errInvalidLength {
		t.Fail()
	}
}

func TestFromBytes_ErrStringLength(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err == nil || vi != nil {
		t.Fail()
		return
	}
	if err.Error() != errInvalidStringLength {
		t.Fail()
	}
}

func TestFromBytes_ErrTruncFixed(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err != io.ErrUnexpectedEOF || vi != nil {
		t.Fail()
	}
}

func TestFromBytes_OtherType(t *testing.T) {
	b := loadGolden(t)
	vi, err := FromBytes(b)
	if err != nil {
		t.Fatal(err)
	}

	if vi.Type != Unknown {
		t.Fail()
	}
}

func TestInfo_Bytes1(t *testing.T) {
	vi := &Info{}
	// 0x409 is en-US, and the default language
	vi.Set(0x409, ProductName, "Good Product")
	// 0x40C is fr-FR
	vi.Set(0x40C, ProductName, "Bon Produit")
	// 0 is neutral
	vi.Set(0, "Smile", "😀")

	vi.Flags.Prerelease = true
	vi.Flags.Patched = true
	vi.Flags.PrivateBuild = true
	vi.Flags.SpecialBuild = true
	vi.Flags.Debug = true

	vi.Timestamp = time.Date(1979, 7, 3, 0, 15, 42, 100, time.UTC)

	vi.ProductVersion = [4]uint16{0xFFF5, 0xFFF6, 0xFFF7, 0xFFF8}
	vi.FileVersion = [4]uint16{0xFFF1, 0xFFF2, 0xFFF3, 0xFFF4}

	checkBytes(t, vi)
}

func TestInfo_Bytes2(t *testing.T) {
	vi := &Info{}
	vi.Set(LangDefault, "Custom Info", "Very important information")
	vi.Set(LangDefault, Comments, "This is a test...\n\nLook, I even skipped a line!\n")
	vi.Set(LangDefault, CompanyName, "Some Company")
	vi.Set(LangDefault, FileDescription, "This great product does...\r\n\r\nNothing!\n\n")
	vi.Set(LangDefault, FileVersion, "V. 421.422.423.424")
	vi.Set(LangDefault, InternalName, "secret-product")
	vi.Set(LangDefault, LegalCopyright, "© Some Company")
	vi.Set(LangDefault, LegalTrademarks, "™ Some Trademarks")
	vi.Set(LangDefault, OriginalFilename, "hey_there.dll")
	vi.Set(LangDefault, PrivateBuild, "True")
	vi.Set(LangDefault, ProductName, "Some Product")
	vi.Set(LangDefault, ProductVersion, "v. 1.2.3.42 (private)")
	vi.Set(LangDefault, SpecialBuild, "False")

	vi.Flags.Prerelease = true
	vi.Flags.Patched = false
	vi.Flags.PrivateBuild = true
	vi.Flags.SpecialBuild = false
	vi.Flags.Debug = true

	vi.ProductVersion = [4]uint16{8, 4, 2, 1}

	checkBytes(t, vi)
}

func TestInfo_Bytes_DLL(t *testing.T) {
	checkBytes(t, &Info{
		Type: DLL,
	})
}

func TestInfo_Bytes_Empty(t *testing.T) {
	var nilInfo *Info

	if len(nilInfo.Bytes()) > 0 {
		t.Fail()
	}

	checkBytes(t, &Info{})
}

func TestInfo_Bytes_OtherType(t *testing.T) {
	checkBytes(t, &Info{
		Type: 42,
	})

	checkBytes(t, &Info{
		Type: Unknown,
	})
}

func TestInfo_Set(t *testing.T) {
	var (
		key   string
		value string
		err   error
		vi    Info
	)

	key, value = "", "Hi"
	err = vi.Set(0x409, key, value)
	if err == nil || err.Error() != errEmptyKey {
		t.Fail()
	}

	key, value = "Hey\x00", "Hi"
	err = vi.Set(0x409, key, value)
	if err == nil || err.Error() != errKeyContainsNUL {
		t.Fail()
	}

	key, value = "Hey", "Hi\x00"
	err = vi.Set(0x409, key, value)
	if err == nil || err.Error() != errValueContainsNUL {
		t.Fail()
	}

	key, value = "Hey", "\x00Hi"
	err = vi.Set(0x409, key, value)
	if err == nil || err.Error() != errValueContainsNUL {
		t.Fail()
	}

	if vi.Set(0, "😀", "") != nil {
		t.Fail()
	}
	if vi.Set(0, "😀", "\n\n") != nil {
		t.Fail()
	}
}

func TestInfo_SetFileVersion(t *testing.T) {
	vi := &Info{}
	// 0x409 is en-US, and the default language
	vi.Set(0x409, ProductName, "Good Product")
	// 0x40C is fr-FR
	vi.Set(0x40C, ProductName, "Bon Produit")
	// 0 is neutral
	vi.Set(0, "Smile", "😀")

	vers := "v-1.65536.65537.65538beta"
	vi.SetFileVersion(vers)

	if (*vi.lt[0x409])[FileVersion] != vers {
		t.Fail()
	}
	if (*vi.lt[0x40C])[FileVersion] != vers {
		t.Fail()
	}
	if (*vi.lt[0])[FileVersion] != vers {
		t.Fail()
	}

	checkBytes(t, vi)
}

func TestInfo_SetFileVersion_Empty(t *testing.T) {
	vi := &Info{}
	vi.SetFileVersion("... 1.42.42.42 ...")

	if len(vi.lt) != 1 || (*vi.lt[0])[FileVersion] == "" {
		t.Fail()
	}

	checkBytes(t, vi)
}

func TestInfo_SetProductVersion(t *testing.T) {
	vi := &Info{}
	// 0x409 is en-US, and the default language
	vi.Set(0x409, ProductName, "Good Product")
	// 0x40C is fr-FR
	vi.Set(0x40C, ProductName, "Bon Produit")
	// 0 is neutral
	vi.Set(0, "Smile", "😀")

	vers := "v.65531.65532.65533.65534-alpha"
	vi.SetProductVersion(vers)

	if (*vi.lt[0x409])[ProductVersion] != vers {
		t.Fail()
	}
	if (*vi.lt[0x40C])[ProductVersion] != vers {
		t.Fail()
	}
	if (*vi.lt[0])[ProductVersion] != vers {
		t.Fail()
	}

	checkBytes(t, vi)
}

func TestInfo_SetProductVersion_Empty(t *testing.T) {
	vi := &Info{}
	vi.SetProductVersion("v 1.2.3.4 beta")

	if len(vi.lt) != 1 || (*vi.lt[0])[ProductVersion] == "" {
		t.Fail()
	}

	checkBytes(t, vi)
}

func TestInfo_SplitTranslations(t *testing.T) {
	var vi *Info

	if len(vi.SplitTranslations()) > 0 {
		t.Fail()
	}

	vi = &Info{
		FileVersion:    [4]uint16{1, 2, 3, 4},
		ProductVersion: [4]uint16{1, 2, 3, 4},
		Flags: versionFlags{
			Debug:        true,
			Prerelease:   true,
			Patched:      true,
			PrivateBuild: true,
			SpecialBuild: true,
		},
		Type:      DLL,
		Timestamp: time.Date(2020, 1, 1, 12, 30, 42, 0, time.UTC),
	}
	vi.Set(0x000, "k1", "v1.000")
	vi.Set(0x409, "k1", "v1.409")

	vi.Set(0x401, "k2", "v2.401")
	vi.Set(0x409, "k2", "v2.409")

	vi.Set(0x401, "k3", "v3.401")
	vi.Set(0x402, "k3", "v3.402")

	vi.Set(0x403, "k0", "v0.403")
	delete(*vi.lt[0x403], "k0")

	trans := vi.SplitTranslations()

	for k, v := range trans {
		switch k {
		case 0x000:
			b, _ := json.MarshalIndent(v, "", "  ")
			//language=JSON
			if string(b) != `{
  "fixed": {
    "file_version": "1.2.3.4",
    "product_version": "1.2.3.4",
    "flags": "Debug,Prerelease,Patched,PrivateBuild,SpecialBuild",
    "type": "DLL",
    "timestamp": "2020-01-01T12:30:42Z"
  },
  "info": {
    "0000": {
      "k1": "v1.000",
      "k2": "v2.409",
      "k3": "v3.401"
    }
  }
}` {
				t.Fail()
			}

		case 0x401:
			b, _ := json.MarshalIndent(v, "", "  ")
			//language=JSON
			if string(b) != `{
  "fixed": {
    "file_version": "1.2.3.4",
    "product_version": "1.2.3.4",
    "flags": "Debug,Prerelease,Patched,PrivateBuild,SpecialBuild",
    "type": "DLL",
    "timestamp": "2020-01-01T12:30:42Z"
  },
  "info": {
    "0401": {
      "k1": "v1.000",
      "k2": "v2.401",
      "k3": "v3.401"
    }
  }
}` {
				t.Fail()
			}

		case 0x402:
			b, _ := json.MarshalIndent(v, "", "  ")
			//language=JSON
			if string(b) != `{
  "fixed": {
    "file_version": "1.2.3.4",
    "product_version": "1.2.3.4",
    "flags": "Debug,Prerelease,Patched,PrivateBuild,SpecialBuild",
    "type": "DLL",
    "timestamp": "2020-01-01T12:30:42Z"
  },
  "info": {
    "0402": {
      "k1": "v1.000",
      "k2": "v2.409",
      "k3": "v3.402"
    }
  }
}` {
				t.Fail()
			}

		case 0x409:
			b, _ := json.MarshalIndent(v, "", "  ")
			//language=JSON
			if string(b) != `{
  "fixed": {
    "file_version": "1.2.3.4",
    "product_version": "1.2.3.4",
    "flags": "Debug,Prerelease,Patched,PrivateBuild,SpecialBuild",
    "type": "DLL",
    "timestamp": "2020-01-01T12:30:42Z"
  },
  "info": {
    "0409": {
      "k1": "v1.409",
      "k2": "v2.409",
      "k3": "v3.401"
    }
  }
}` {
				t.Fail()
			}

		case 0x403:
			b, _ := json.MarshalIndent(v, "", "  ")
			//language=JSON
			if string(b) != `{
  "fixed": {
    "file_version": "1.2.3.4",
    "product_version": "1.2.3.4",
    "flags": "Debug,Prerelease,Patched,PrivateBuild,SpecialBuild",
    "type": "DLL",
    "timestamp": "2020-01-01T12:30:42Z"
  },
  "info": {
    "0403": {
      "k1": "v1.000",
      "k2": "v2.409",
      "k3": "v3.401"
    }
  }
}` {
				t.Fail()
			}

		default:
			t.Fail()
		}
	}
}

func checkBytes(t *testing.T, vi *Info) {
	b := vi.Bytes()

	refFile := golden(t)
	ref, _ := os.ReadFile(refFile)

	if !bytes.Equal(ref, b) {
		t.Error(t.Name() + " output is different")
		bugFile := refFile[:len(refFile)-7] + ".bug"
		err := os.WriteFile(bugFile, b, 0666)
		if err != nil {
			t.Error(err)
			return
		}
		t.Log("dumped output to", bugFile)
	}
}

func loadGolden(t *testing.T) []byte {
	refFile := golden(t)
	ref, err := os.ReadFile(refFile)
	if err != nil {
		t.Error(err)
		return nil
	}
	return ref
}

func golden(t *testing.T) string {
	return filepath.Join("testdata", t.Name()+".golden")
}
