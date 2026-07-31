package winres

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/template"
)

// AppManifest describes an application manifest.
//
// Its zero value corresponds to the most common case.
type AppManifest struct {
	Identity                          AssemblyIdentity `json:"identity"`
	Description                       string           `json:"description"`
	Compatibility                     SupportedOS      `json:"minimum-os"`
	ExecutionLevel                    ExecutionLevel   `json:"execution-level"`
	UIAccess                          bool             `json:"ui-access"` // Require access to other applications' UI elements
	AutoElevate                       bool             `json:"auto-elevate"`
	DPIAwareness                      DPIAwareness     `json:"dpi-awareness"`
	DisableTheming                    bool             `json:"disable-theming"`
	DisableWindowFiltering            bool             `json:"disable-window-filtering"`
	HighResolutionScrollingAware      bool             `json:"high-resolution-scrolling-aware"`
	UltraHighResolutionScrollingAware bool             `json:"ultra-high-resolution-scrolling-aware"`
	LongPathAware                     bool             `json:"long-path-aware"`
	PrinterDriverIsolation            bool             `json:"printer-driver-isolation"`
	GDIScaling                        bool             `json:"gdi-scaling"`
	SegmentHeap                       bool             `json:"segment-heap"`
	UseCommonControlsV6               bool             `json:"use-common-controls-v6"` // Application requires Common Controls V6 (V5 remains the default)
}

// AssemblyIdentity defines the side-by-side assembly identity of the executable.
//
// It should not be needed unless another assembly depends on this one.
//
// If the Name field is empty, the <assemblyIdentity> element will be omitted.
type AssemblyIdentity struct {
	Name    string
	Version [4]uint16
}

// DPIAwareness is an enumeration which corresponds to the <dpiAware> and the <dpiAwareness> elements.
//
// When it is set to DPIPerMonitorV2, it will fallback to DPIAware if the OS does not support it.
//
// DPIPerMonitor would not scale windows on secondary monitors.
type DPIAwareness int

const (
	DPIAware DPIAwareness = iota
	DPIUnaware
	DPIPerMonitor
	DPIPerMonitorV2
)

// SupportedOS is an enumeration that provides a simplified way to fill the
// compatibility element in an application manifest, by only setting a minimum OS.
//
// Its zero value is Win7AndAbove, which matches Go's requirements.
//
// https://github.com/golang/go/wiki/MinimumRequirements#windows
type SupportedOS int

const (
	WinVistaAndAbove SupportedOS = iota - 1
	Win7AndAbove
	Win8AndAbove
	Win81AndAbove
	Win10AndAbove
)

// ExecutionLevel is used in an AppManifest to set the required execution level.
type ExecutionLevel int

const (
	AsInvoker ExecutionLevel = iota
	HighestAvailable
	RequireAdministrator
)

const (
	osWin10    = "{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"
	osWin81    = "{1f676c76-80e1-4239-95bb-83d0f6d0da78}"
	osWin8     = "{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}"
	osWin7     = "{35138b9a-5d96-4fbd-8e2d-a2440225f93a}"
	osWinVista = "{e2011457-1546-43c5-a5fe-008deee3d3f0}"
)

// language=GoTemplate
var manifestTemplate = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
{{- if .AssemblyName}}

  <assemblyIdentity type="win32" name="{{.AssemblyName | html}}" version="{{.AssemblyVersion}}" processorArchitecture="*"/>
{{- end}}
{{- if .Description}}
  <description>{{.Description | html}}</description>
{{- end}}

  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      {{- range $osID := .SupportedOS}}
      <supportedOS Id="{{$osID}}"/>
      {{- end}}
    </application>
  </compatibility>

  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">{{.DPIAware}}</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">{{.DPIAwareness}}</dpiAwareness>
      {{- if .AutoElevate}}
      <autoElevate xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</autoElevate>
      {{- end}}
      {{- if .DisableTheming}}
      <disableTheming xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</disableTheming>
      {{- end}}
      {{- if .DisableWindowFiltering}}
      <disableWindowFiltering xmlns="http://schemas.microsoft.com/SMI/2011/WindowsSettings">true</disableWindowFiltering>
      {{- end}}
      {{- if .HighResolutionScrollingAware}}
      <highResolutionScrollingAware xmlns="http://schemas.microsoft.com/SMI/2013/WindowsSettings">true</highResolutionScrollingAware>
      {{- end}}
      {{- if .PrinterDriverIsolation}}
      <printerDriverIsolation xmlns="http://schemas.microsoft.com/SMI/2011/WindowsSettings">true</printerDriverIsolation>
      {{- end}}
      {{- if .UltraHighResolutionScrollingAware}}
      <ultraHighResolutionScrollingAware xmlns="http://schemas.microsoft.com/SMI/2013/WindowsSettings">true</ultraHighResolutionScrollingAware>
      {{- end}}
      {{- if .LongPathAware}}
      <longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware>
      {{- end}}
      {{- if .GDIScaling}}
      <gdiScaling xmlns="http://schemas.microsoft.com/SMI/2017/WindowsSettings">true</gdiScaling>
      {{- end}}
      {{- if .SegmentHeap}}
      <heapType xmlns="http://schemas.microsoft.com/SMI/2020/WindowsSettings">SegmentHeap</heapType>
      {{- end}}
    </windowsSettings>
  </application>

  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="{{.ExecutionLevel}}" uiAccess="{{if .UIAccess}}true{{else}}false{{end}}"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  {{- if .UseCommonControlsV6}}

  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  {{- end}}

</assembly>
`

func makeManifest(manifest AppManifest) []byte {
	vars := struct {
		AppManifest
		AssemblyName    string
		AssemblyVersion string
		SupportedOS     []string
		DPIAware        string
		DPIAwareness    string
		ExecutionLevel  string
	}{AppManifest: manifest}

	if manifest.Identity.Name != "" {
		vars.AssemblyName = manifest.Identity.Name
		v := manifest.Identity.Version
		vars.AssemblyVersion = fmt.Sprintf("%d.%d.%d.%d", v[0], v[1], v[2], v[3])
	}

	vars.SupportedOS = []string{
		osWin10,
		osWin81,
		osWin8,
		osWin7,
		osWinVista,
	}
	switch manifest.Compatibility {
	case Win7AndAbove:
		vars.SupportedOS = vars.SupportedOS[:4]
	case Win8AndAbove:
		vars.SupportedOS = vars.SupportedOS[:3]
	case Win81AndAbove:
		vars.SupportedOS = vars.SupportedOS[:2]
	case Win10AndAbove:
		vars.SupportedOS = vars.SupportedOS[:1]
	}

	switch manifest.ExecutionLevel {
	case RequireAdministrator:
		vars.ExecutionLevel = "requireAdministrator"
	case HighestAvailable:
		vars.ExecutionLevel = "highestAvailable"
	default:
		vars.ExecutionLevel = "asInvoker"
	}

	switch manifest.DPIAwareness {
	case DPIAware:
		vars.DPIAware = "true"
		vars.DPIAwareness = "system"
	case DPIPerMonitor:
		vars.DPIAware = "true/pm"
		vars.DPIAwareness = "permonitor"
	case DPIPerMonitorV2:
		// PerMonitorV2 fixes the scale on secondary monitors
		// If not available, the closest option seems to be System
		vars.DPIAware = "true"
		vars.DPIAwareness = "permonitorv2,system"
	case DPIUnaware:
		vars.DPIAware = "false"
		vars.DPIAwareness = "unaware"
	}

	buf := &bytes.Buffer{}
	tmpl := template.Must(template.New("manifest").Parse(manifestTemplate))
	err := tmpl.Execute(buf, vars)
	if err != nil {
		panic(err)
	}

	return buf.Bytes()
}

type appManifestXML struct {
	Identity struct {
		Name    string `xml:"name,attr"`
		Version string `xml:"version,attr"`
	} `xml:"assemblyIdentity"`
	Description   string `xml:"description"`
	Compatibility struct {
		Application struct {
			SupportedOS []struct {
				Id string `xml:"Id,attr"`
			} `xml:"supportedOS"`
		} `xml:"application"`
	} `xml:"compatibility"`
	Application struct {
		WindowsSettings struct {
			DPIAware                          string `xml:"dpiAware"`
			DPIAwareness                      string `xml:"dpiAwareness"`
			AutoElevate                       string `xml:"autoElevate"`
			DisableTheming                    string `xml:"disableTheming"`
			DisableWindowFiltering            string `xml:"disableWindowFiltering"`
			HighResolutionScrollingAware      string `xml:"highResolutionScrollingAware"`
			PrinterDriverIsolation            string `xml:"printerDriverIsolation"`
			UltraHighResolutionScrollingAware string `xml:"ultraHighResolutionScrollingAware"`
			LongPathAware                     string `xml:"longPathAware"`
			GDIScaling                        string `xml:"gdiScaling"`
			HeapType                          string `xml:"heapType"`
		} `xml:"windowsSettings"`
	} `xml:"application"`
	TrustInfo struct {
		Security struct {
			RequestedPrivileges struct {
				RequestedExecutionLevel struct {
					Level    string `xml:"level,attr"`
					UIAccess string `xml:"uiAccess,attr"`
				} `xml:"requestedExecutionLevel"`
			} `xml:"requestedPrivileges"`
		} `xml:"security"`
	} `xml:"trustInfo"`
	Dependency struct {
		DependentAssembly []struct {
			Identity struct {
				Name           string `xml:"name,attr"`
				Version        string `xml:"version,attr"`
				PublicKeyToken string `xml:"publicKeyToken,attr"`
			} `xml:"assemblyIdentity"`
		} `xml:"dependentAssembly"`
	} `xml:"dependency"`
}

// AppManifestFromXML makes an AppManifest from an xml manifest,
// trying to retrieve as much valid information as possible.
//
// If the xml contains other data, they are ignored.
//
// This function can only return xml syntax errors, other errors are ignored.
func AppManifestFromXML(data []byte) (AppManifest, error) {
	x := appManifestXML{}
	err := xml.Unmarshal(data, &x)
	if err != nil {
		return AppManifest{}, err
	}
	var m AppManifest

	m.Identity.Name = x.Identity.Name
	v := strings.Split(x.Identity.Version, ".")
	if len(v) > 4 {
		v = v[:4]
	}
	for i := range v {
		n, _ := strconv.ParseUint(v[i], 10, 16)
		m.Identity.Version[i] = uint16(n)
	}
	m.Description = x.Description

	m.Compatibility = Win10AndAbove + 1
	for _, os := range x.Compatibility.Application.SupportedOS {
		c := osIDToEnum(os.Id)
		if c < m.Compatibility {
			m.Compatibility = c
		}
	}
	if m.Compatibility > Win10AndAbove {
		m.Compatibility = Win7AndAbove
	}

	settings := x.Application.WindowsSettings
	m.DPIAwareness = readDPIAwareness(settings.DPIAware, settings.DPIAwareness)
	m.AutoElevate = manifestBool(settings.AutoElevate)
	m.DisableTheming = manifestBool(settings.DisableTheming)
	m.DisableWindowFiltering = manifestBool(settings.DisableWindowFiltering)
	m.HighResolutionScrollingAware = manifestBool(settings.HighResolutionScrollingAware)
	m.PrinterDriverIsolation = manifestBool(settings.PrinterDriverIsolation)
	m.UltraHighResolutionScrollingAware = manifestBool(settings.UltraHighResolutionScrollingAware)
	m.LongPathAware = manifestBool(settings.LongPathAware)
	m.GDIScaling = manifestBool(settings.GDIScaling)
	m.SegmentHeap = manifestString(settings.HeapType) == "segmentheap"

	for _, dep := range x.Dependency.DependentAssembly {
		if manifestString(dep.Identity.Name) == "microsoft.windows.common-controls" &&
			strings.HasPrefix(manifestString(dep.Identity.Version), "6.") &&
			manifestString(dep.Identity.PublicKeyToken) == "6595b64144ccf1df" {
			m.UseCommonControlsV6 = true
		}
	}

	m.UIAccess = manifestBool(x.TrustInfo.Security.RequestedPrivileges.RequestedExecutionLevel.UIAccess)
	switch manifestString(x.TrustInfo.Security.RequestedPrivileges.RequestedExecutionLevel.Level) {
	case "requireadministrator":
		m.ExecutionLevel = RequireAdministrator
	case "highestavailable":
		m.ExecutionLevel = HighestAvailable
	}

	return m, nil
}

func readDPIAwareness(dpiAware string, dpiAwareness string) DPIAwareness {
	for _, s := range strings.Split(dpiAwareness, ",") {
		switch manifestString(s) {
		case "permonitorv2":
			return DPIPerMonitorV2
		case "permonitor":
			return DPIPerMonitor
		case "system":
			return DPIAware
		case "unaware":
			return DPIUnaware
		}
	}
	switch manifestString(dpiAware) {
	case "true":
		return DPIAware
	case "true/pm":
		return DPIPerMonitor
	}
	return DPIUnaware
}

func manifestString(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func manifestBool(s string) bool {
	return manifestString(s) == "true"
}

func osIDToEnum(osID string) SupportedOS {
	switch osID {
	case osWinVista:
		return WinVistaAndAbove
	case osWin7:
		return Win7AndAbove
	case osWin8:
		return Win8AndAbove
	case osWin81:
		return Win81AndAbove
	}
	return Win10AndAbove
}

// JSON marshalling:

func (os SupportedOS) MarshalText() ([]byte, error) {
	switch os {
	case WinVistaAndAbove:
		return []byte("vista"), nil
	case Win7AndAbove:
		return []byte("win7"), nil
	case Win8AndAbove:
		return []byte("win8"), nil
	case Win81AndAbove:
		return []byte("win8.1"), nil
	case Win10AndAbove:
		return []byte("win10"), nil
	}
	return nil, errors.New(errUnknownSupportedOS)
}

func (os *SupportedOS) UnmarshalText(b []byte) error {
	switch strings.ToLower(strings.TrimSpace(string(b))) {
	case "vista":
		*os = WinVistaAndAbove
		return nil
	case "win7":
		*os = Win7AndAbove
		return nil
	case "win8":
		*os = Win8AndAbove
		return nil
	case "win8.1":
		*os = Win81AndAbove
		return nil
	case "win10":
		*os = Win10AndAbove
		return nil
	}
	return errors.New(errUnknownSupportedOS)
}

func (a DPIAwareness) MarshalText() ([]byte, error) {
	switch a {
	case DPIAware:
		return []byte("system"), nil
	case DPIUnaware:
		return []byte("unaware"), nil
	case DPIPerMonitor:
		return []byte("per monitor"), nil
	case DPIPerMonitorV2:
		return []byte("per monitor v2"), nil
	}
	return nil, errors.New(errUnknownDPIAwareness)
}

func (a *DPIAwareness) UnmarshalText(b []byte) error {
	switch strings.ToLower(strings.TrimSpace(string(b))) {
	case "system", "true", "":
		*a = DPIAware
		return nil
	case "unaware", "false":
		*a = DPIUnaware
		return nil
	case "per monitor", "permonitor", "true/pm":
		*a = DPIPerMonitor
		return nil
	case "per monitor v2", "permonitorv2":
		*a = DPIPerMonitorV2
		return nil
	}
	return errors.New(errUnknownDPIAwareness)
}

func (level ExecutionLevel) MarshalText() ([]byte, error) {
	switch level {
	case AsInvoker:
		return []byte(""), nil
	case HighestAvailable:
		return []byte("highest"), nil
	case RequireAdministrator:
		return []byte("administrator"), nil
	}
	return nil, errors.New(errUnknownExecLevel)
}

func (level *ExecutionLevel) UnmarshalText(b []byte) error {
	switch strings.ToLower(strings.TrimSpace(string(b))) {
	case "", "as invoker", "asinvoker":
		*level = AsInvoker
		return nil
	case "highest", "highest available", "highestavailable":
		*level = HighestAvailable
		return nil
	case "administrator", "require administrator", "requireadministrator":
		*level = RequireAdministrator
		return nil
	}
	return errors.New(errUnknownExecLevel)
}

type assemblyIdentityJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (ai AssemblyIdentity) MarshalJSON() ([]byte, error) {
	if ai.Name == "" {
		return []byte(`{}`), nil
	}
	j := assemblyIdentityJSON{}
	j.Name = ai.Name
	if ai.Name != "" {
		j.Version = fmt.Sprintf("%d.%d.%d.%d", ai.Version[0], ai.Version[1], ai.Version[2], ai.Version[3])
	}
	return json.Marshal(j)
}

func (ai *AssemblyIdentity) UnmarshalJSON(b []byte) error {
	j := assemblyIdentityJSON{}
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	ai.Name = j.Name
	j.Version = strings.TrimSpace(j.Version)
	if j.Version == "" {
		return nil
	}
	v := strings.Split(j.Version, ".")
	if len(v) > 4 {
		return errors.New(errInvalidVersion)
	}
	for i := range v {
		n, err := strconv.ParseUint(v[i], 10, 16)
		if err != nil {
			return errors.New(errInvalidVersion)
		}
		ai.Version[i] = uint16(n)
	}
	return nil
}
