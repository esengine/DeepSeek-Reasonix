package version

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type jsonFixed struct {
	FileVersion    string     `json:"file_version,omitempty"`
	ProductVersion string     `json:"product_version,omitempty"`
	Flags          string     `json:"flags,omitempty"`
	Type           string     `json:"type,omitempty"`
	Timestamp      *time.Time `json:"timestamp,omitempty"`
}

type jsonVersionInfo struct {
	Fixed *jsonFixed              `json:"fixed,omitempty"`
	Info  map[string]*StringTable `json:"info,omitempty"`
}

func (vi *Info) MarshalJSON() ([]byte, error) {
	jvi := jsonVersionInfo{}
	jf := jsonFixed{}
	if vi.FileVersion != [4]uint16{} {
		jf.FileVersion = fmt.Sprintf("%d.%d.%d.%d", vi.FileVersion[0], vi.FileVersion[1], vi.FileVersion[2], vi.FileVersion[3])
	}
	if vi.ProductVersion != [4]uint16{} {
		jf.ProductVersion = fmt.Sprintf("%d.%d.%d.%d", vi.ProductVersion[0], vi.ProductVersion[1], vi.ProductVersion[2], vi.ProductVersion[3])
	}
	if vi.Flags.Debug {
		jf.Flags += "Debug,"
	}
	if vi.Flags.Prerelease {
		jf.Flags += "Prerelease,"
	}
	if vi.Flags.Patched {
		jf.Flags += "Patched,"
	}
	if vi.Flags.PrivateBuild {
		jf.Flags += "PrivateBuild,"
	}
	if vi.Flags.SpecialBuild {
		jf.Flags += "SpecialBuild,"
	}
	jf.Flags = strings.TrimRight(jf.Flags, ",")
	switch vi.Type {
	case App:
		// This is the default, omit it
	case DLL:
		jf.Type = "DLL"
	default:
		jf.Type = "Unknown"
	}
	if !vi.Timestamp.IsZero() {
		jf.Timestamp = &vi.Timestamp
	}
	if jf.FileVersion != "" || jf.ProductVersion != "" || jf.Flags != "" || jf.Type != "" || jf.Timestamp != nil {
		jvi.Fixed = &jf
	}

	jvi.Info = make(map[string]*StringTable)
	for k, v := range vi.lt {
		if v != nil {
			jvi.Info[fmt.Sprintf("%04X", k)] = v
		}
	}

	return json.Marshal(jvi)
}

func (vi *Info) UnmarshalJSON(b []byte) error {
	jvi := &jsonVersionInfo{}
	if err := json.Unmarshal(b, jvi); err != nil {
		return err
	}
	*vi = Info{}
	jf := jvi.Fixed
	if jf != nil {
		fmt.Sscanf(jf.FileVersion, "%d.%d.%d.%d", &vi.FileVersion[0], &vi.FileVersion[1], &vi.FileVersion[2], &vi.FileVersion[3])
		fmt.Sscanf(jf.ProductVersion, "%d.%d.%d.%d", &vi.ProductVersion[0], &vi.ProductVersion[1], &vi.ProductVersion[2], &vi.ProductVersion[3])
		f := strings.ToLower(jf.Flags)
		vi.Flags.Debug = strings.Contains(f, "debug")
		vi.Flags.Prerelease = strings.Contains(f, "prerelease")
		vi.Flags.Patched = strings.Contains(f, "patched")
		vi.Flags.PrivateBuild = strings.Contains(f, "privatebuild")
		vi.Flags.SpecialBuild = strings.Contains(f, "specialbuild")
		switch strings.ToLower(jf.Type) {
		case "app", "":
			vi.Type = App
		case "dll":
			vi.Type = DLL
		default:
			vi.Type = Unknown
		}
		if jf.Timestamp != nil {
			vi.Timestamp = *jf.Timestamp
		}
	}

	vi.lt = make(LangTable)
	for h, v := range jvi.Info {
		var k uint16
		_, err := fmt.Sscanf(h, "%X", &k)
		if err == nil {
			vi.lt[k] = v
		}
	}

	return nil
}
