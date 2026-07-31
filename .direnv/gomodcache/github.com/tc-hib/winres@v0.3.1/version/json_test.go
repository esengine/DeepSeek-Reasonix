package version

import (
	"encoding/json"
	"testing"
	"time"
)

func TestInfo_MarshalJSON(t *testing.T) {
	if marshal(t, nil) != `null` {
		t.Fail()
	}

	vi := &Info{}
	checkMarshal(t, vi, `{}`)

	vi.FileVersion = [4]uint16{19, 79, 7, 3}
	checkMarshal(t, vi, `{"fixed":{"file_version":"19.79.7.3"}}`)

	vi.ProductVersion = [4]uint16{0xFFFF, 1, 0x7FFF, 2}
	vi.Type = DLL
	vi.Timestamp = time.Date(1979, 7, 3, 00, 45, 20, 0, time.UTC)
	checkMarshal(t, vi, `{"fixed":{"file_version":"19.79.7.3","product_version":"65535.1.32767.2","type":"DLL","timestamp":"1979-07-03T00:45:20Z"}}`)

	vi.Flags.SpecialBuild = true
	vi.Flags.PrivateBuild = true
	vi.Flags.Patched = true
	vi.Flags.Prerelease = true
	vi.Flags.Debug = true
	checkMarshal(t, vi, `{"fixed":{"file_version":"19.79.7.3","product_version":"65535.1.32767.2","flags":"Debug,Prerelease,Patched,PrivateBuild,SpecialBuild","type":"DLL","timestamp":"1979-07-03T00:45:20Z"}}`)

	vi.FileVersion = [4]uint16{}
	vi.ProductVersion = [4]uint16{}
	vi.Type = 0
	vi.Timestamp = time.Time{}
	vi.Flags.SpecialBuild = false
	vi.Flags.Patched = false
	vi.Flags.Debug = false
	checkMarshal(t, vi, `{"fixed":{"flags":"Prerelease,PrivateBuild"}}`)

	vi.Set(0, CompanyName, "Company name")
	vi.Set(0, ProductVersion, "Product version")
	checkMarshal(t, vi, `{"fixed":{"flags":"Prerelease,PrivateBuild"},"info":{"0000":{"CompanyName":"Company name","ProductVersion":"Product version"}}}`)

	vi.Flags.PrivateBuild = false
	vi.Flags.Prerelease = false
	checkMarshal(t, vi, `{"info":{"0000":{"CompanyName":"Company name","ProductVersion":"Product version"}}}`)

	vi.Set(0x409, CompanyName, "Company name EN")
	vi.Set(0x40C, CompanyName, "Company name FR")
	checkMarshal(t, vi, `{"info":{"0000":{"CompanyName":"Company name","ProductVersion":"Product version"},"0409":{"CompanyName":"Company name EN"},"040C":{"CompanyName":"Company name FR"}}}`)

	vi.Type = Unknown
	checkMarshal(t, vi, `{"fixed":{"type":"Unknown"},"info":{"0000":{"CompanyName":"Company name","ProductVersion":"Product version"},"0409":{"CompanyName":"Company name EN"},"040C":{"CompanyName":"Company name FR"}}}`)

	vi.Type = 42
	vi.lt = LangTable{}
	checkMarshal(t, vi, `{"fixed":{"type":"Unknown"}}`)

	vi.Type = App
	vi.lt = make(LangTable)
	checkMarshal(t, vi, `{}`)
}

func TestInfo_UnmarshalJSON(t *testing.T) {
	var vi Info
	if vi.UnmarshalJSON([]byte(` {,}`)) == nil {
		t.Fail()
	}
}

func marshal(t *testing.T, vi *Info) string {
	b, err := json.Marshal(vi)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func unmarshal(t *testing.T, b string) *Info {
	vi := &Info{}
	err := json.Unmarshal([]byte(b), vi)
	if err != nil {
		t.Fatal(err)
	}
	return vi
}

func checkMarshal(t *testing.T, vi *Info, expected string) {
	got := marshal(t, vi)
	if got != expected {
		t.Errorf("marshal:\nexpected:\n%s\ngot:\n%s", expected, got)
	}

	got2 := marshal(t, unmarshal(t, got))
	if got2 != expected {
		t.Errorf("unmarshal:\nexpected:\n%s\ngot:\n%s", expected, got2)
	}
}
