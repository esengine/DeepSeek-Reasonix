package winres

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func Test_makeManifest(t *testing.T) {
	type args struct {
		manifest AppManifest
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "empty",
			args: struct{ manifest AppManifest }{},
			// language=manifest
			want: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">

  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
      <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
      <supportedOS Id="{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}"/>
      <supportedOS Id="{35138b9a-5d96-4fbd-8e2d-a2440225f93a}"/>
    </application>
  </compatibility>

  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">system</dpiAwareness>
    </windowsSettings>
  </application>

  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>

</assembly>
`},
		{
			name: "full",
			args: struct{ manifest AppManifest }{AppManifest{
				Identity: AssemblyIdentity{
					Name:    "<app.name>",
					Version: [4]uint16{1, 2, 3, 4},
				},
				Description:                       "<Application Description>",
				UIAccess:                          true,
				AutoElevate:                       true,
				DisableTheming:                    true,
				DisableWindowFiltering:            true,
				HighResolutionScrollingAware:      true,
				UltraHighResolutionScrollingAware: true,
				LongPathAware:                     true,
				PrinterDriverIsolation:            true,
				GDIScaling:                        true,
				SegmentHeap:                       true,
				UseCommonControlsV6:               true,
				ExecutionLevel:                    HighestAvailable,
				Compatibility:                     WinVistaAndAbove,
				DPIAwareness:                      DPIAware,
			}},
			// language=manifest
			want: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">

  <assemblyIdentity type="win32" name="&lt;app.name&gt;" version="1.2.3.4" processorArchitecture="*"/>
  <description>&lt;Application Description&gt;</description>

  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
      <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
      <supportedOS Id="{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}"/>
      <supportedOS Id="{35138b9a-5d96-4fbd-8e2d-a2440225f93a}"/>
      <supportedOS Id="{e2011457-1546-43c5-a5fe-008deee3d3f0}"/>
    </application>
  </compatibility>

  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">system</dpiAwareness>
      <autoElevate xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</autoElevate>
      <disableTheming xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</disableTheming>
      <disableWindowFiltering xmlns="http://schemas.microsoft.com/SMI/2011/WindowsSettings">true</disableWindowFiltering>
      <highResolutionScrollingAware xmlns="http://schemas.microsoft.com/SMI/2013/WindowsSettings">true</highResolutionScrollingAware>
      <printerDriverIsolation xmlns="http://schemas.microsoft.com/SMI/2011/WindowsSettings">true</printerDriverIsolation>
      <ultraHighResolutionScrollingAware xmlns="http://schemas.microsoft.com/SMI/2013/WindowsSettings">true</ultraHighResolutionScrollingAware>
      <longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware>
      <gdiScaling xmlns="http://schemas.microsoft.com/SMI/2017/WindowsSettings">true</gdiScaling>
      <heapType xmlns="http://schemas.microsoft.com/SMI/2020/WindowsSettings">SegmentHeap</heapType>
    </windowsSettings>
  </application>

  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="highestAvailable" uiAccess="true"/>
      </requestedPrivileges>
    </security>
  </trustInfo>

  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>

</assembly>
`},
		{
			name: "win10admin",
			args: struct{ manifest AppManifest }{AppManifest{
				Identity: AssemblyIdentity{
					// No name, no identity (empty name is forbidden)
					Version: [4]uint16{1, 2, 3, 4},
				},
				Description:                       "Application Description",
				UIAccess:                          false,
				AutoElevate:                       true,
				DisableTheming:                    false,
				DisableWindowFiltering:            true,
				HighResolutionScrollingAware:      false,
				UltraHighResolutionScrollingAware: true,
				LongPathAware:                     false,
				PrinterDriverIsolation:            true,
				GDIScaling:                        false,
				SegmentHeap:                       true,
				ExecutionLevel:                    RequireAdministrator,
				Compatibility:                     Win10AndAbove,
				DPIAwareness:                      DPIUnaware,
			}},
			// language=manifest
			want: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <description>Application Description</description>

  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    </application>
  </compatibility>

  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">false</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">unaware</dpiAwareness>
      <autoElevate xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</autoElevate>
      <disableWindowFiltering xmlns="http://schemas.microsoft.com/SMI/2011/WindowsSettings">true</disableWindowFiltering>
      <printerDriverIsolation xmlns="http://schemas.microsoft.com/SMI/2011/WindowsSettings">true</printerDriverIsolation>
      <ultraHighResolutionScrollingAware xmlns="http://schemas.microsoft.com/SMI/2013/WindowsSettings">true</ultraHighResolutionScrollingAware>
      <heapType xmlns="http://schemas.microsoft.com/SMI/2020/WindowsSettings">SegmentHeap</heapType>
    </windowsSettings>
  </application>

  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="requireAdministrator" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>

</assembly>
`},
		{
			name: "win8highest",
			args: struct{ manifest AppManifest }{AppManifest{
				Identity: AssemblyIdentity{
					Name: "app.name",
					// No version -> 0.0.0.0
				},
				Description:                       "Application Description",
				UIAccess:                          true,
				AutoElevate:                       true,
				DisableTheming:                    true,
				DisableWindowFiltering:            true,
				HighResolutionScrollingAware:      true,
				UltraHighResolutionScrollingAware: false,
				LongPathAware:                     false,
				PrinterDriverIsolation:            false,
				GDIScaling:                        false,
				SegmentHeap:                       false,
				ExecutionLevel:                    HighestAvailable,
				Compatibility:                     Win8AndAbove,
				DPIAwareness:                      DPIPerMonitor,
			}},
			// language=manifest
			want: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">

  <assemblyIdentity type="win32" name="app.name" version="0.0.0.0" processorArchitecture="*"/>
  <description>Application Description</description>

  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
      <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
      <supportedOS Id="{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}"/>
    </application>
  </compatibility>

  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">permonitor</dpiAwareness>
      <autoElevate xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</autoElevate>
      <disableTheming xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</disableTheming>
      <disableWindowFiltering xmlns="http://schemas.microsoft.com/SMI/2011/WindowsSettings">true</disableWindowFiltering>
      <highResolutionScrollingAware xmlns="http://schemas.microsoft.com/SMI/2013/WindowsSettings">true</highResolutionScrollingAware>
    </windowsSettings>
  </application>

  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="highestAvailable" uiAccess="true"/>
      </requestedPrivileges>
    </security>
  </trustInfo>

</assembly>
`},
		{
			name: "win81",
			args: struct{ manifest AppManifest }{AppManifest{
				Identity: AssemblyIdentity{
					Name:    "app.name",
					Version: [4]uint16{0xFFFF, 65535, 0xFFFF, 65535},
				},
				Description:                       "Application™\nDescription",
				UIAccess:                          false,
				AutoElevate:                       false,
				DisableTheming:                    false,
				DisableWindowFiltering:            false,
				HighResolutionScrollingAware:      false,
				UltraHighResolutionScrollingAware: true,
				LongPathAware:                     true,
				PrinterDriverIsolation:            true,
				GDIScaling:                        true,
				SegmentHeap:                       true,
				ExecutionLevel:                    AsInvoker,
				Compatibility:                     Win81AndAbove,
				DPIAwareness:                      DPIPerMonitorV2,
			}},
			// language=manifest
			want: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">

  <assemblyIdentity type="win32" name="app.name" version="65535.65535.65535.65535" processorArchitecture="*"/>
  <description>Application™
Description</description>

  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
      <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
    </application>
  </compatibility>

  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">permonitorv2,system</dpiAwareness>
      <printerDriverIsolation xmlns="http://schemas.microsoft.com/SMI/2011/WindowsSettings">true</printerDriverIsolation>
      <ultraHighResolutionScrollingAware xmlns="http://schemas.microsoft.com/SMI/2013/WindowsSettings">true</ultraHighResolutionScrollingAware>
      <longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware>
      <gdiScaling xmlns="http://schemas.microsoft.com/SMI/2017/WindowsSettings">true</gdiScaling>
      <heapType xmlns="http://schemas.microsoft.com/SMI/2020/WindowsSettings">SegmentHeap</heapType>
    </windowsSettings>
  </application>

  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>

</assembly>
`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := makeManifest(tt.args.manifest); string(got) != tt.want {
				t.Errorf("*** makeManifest():\n%v###\n*** want:\n%v###", string(got), tt.want)
			}
		})
	}
}

func TestMakeManifest_Bug(t *testing.T) {
	bak := manifestTemplate
	defer func() {
		recover()
		manifestTemplate = bak
	}()

	manifestTemplate = "{{.bobby}}"
	makeManifest(AppManifest{})

	t.Error("should have panicked")
}

func TestAppManifestFromXML(t *testing.T) {
	tests := []struct {
		name    string
		xml     string
		want    AppManifest
		wantErr bool
	}{
		{
			name: "zero", xml: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly></assembly>`,
			want:    AppManifest{DPIAwareness: DPIUnaware},
			wantErr: false,
		},
		{
			name: "longVersion", xml: // language=manifest
			`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly>
  <assemblyIdentity type="win32" name=" a.name" version="-1.2.3.4.5.6.7" processorArchitecture="x86"/>
</assembly>`,
			want: AppManifest{
				Identity: AssemblyIdentity{
					Name:    " a.name",
					Version: [4]uint16{0, 2, 3, 4},
				},
				DPIAwareness: DPIUnaware,
			},
			wantErr: false,
		},
		{
			name: "shortVersion", xml: // language=manifest
			`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly>
  <assemblyIdentity type="win32" name="💨" version="42.5💨" processorArchitecture="x86"/>
</assembly>`,
			want: AppManifest{
				Identity: AssemblyIdentity{
					Name:    "💨",
					Version: [4]uint16{42},
				},
				DPIAwareness: DPIUnaware,
			},
			wantErr: false,
		},
		{
			name: "dpiAware", xml: // language=manifest
			`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware> TrUe </dpiAware>
    </windowsSettings>
  </application>
</assembly>`,
			want:    AppManifest{},
			wantErr: false,
		},
		{
			name: "manifest1", xml: // language=manifest
			`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAwareness> something,system, </dpiAwareness>

      <autoElevate>true</autoElevate>
      <disableTheming> tRue</disableTheming>
      <disableWindowFiltering>true</disableWindowFiltering>
      
      <printerDriverIsolation>false</printerDriverIsolation>
      <ultraHighResolutionScrollingAware> err</ultraHighResolutionScrollingAware>
      <longPathAware> yes</longPathAware>
      <gdiScaling>true</gdiScaling>
      <heapType attr="x">segmentHeap</heapType>
    </windowsSettings>
    <windowsSettings>
      <highResolutionScrollingAware>true</highResolutionScrollingAware>
    </windowsSettings>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="5.6.6.6" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="06595b64144ccf1df" language="*"/>
    </dependentAssembly>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls6" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  </application>
  <description>This is a 
  description</description>
</assembly>`,
			want: AppManifest{
				Description:                  "This is a \n  description",
				AutoElevate:                  true,
				DisableTheming:               true,
				DisableWindowFiltering:       true,
				GDIScaling:                   true,
				HighResolutionScrollingAware: true,
				SegmentHeap:                  true,
			},
			wantErr: false,
		},
		{
			name: "manifest2", xml: // language=manifest
			`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly>
  <trustInfo>
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="require administrator" uiAccess=" True "/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAwareness>system</dpiAwareness>
      <ultraHighResolutionScrollingAware>true</ultraHighResolutionScrollingAware>
      <printerDriverIsolation>true</printerDriverIsolation>
      <longPathAware >false</longPathAware>
      <longPathAware >true</longPathAware>
      <gdiScaling>true</gdiScaling>
    </windowsSettings>

  </application>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="a" version="5.6.6.6" processorArchitecture="*" publicKeyToken="42" language="*"/>
    </dependentAssembly>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="  6.0.0.0 " processorArchitecture="*" publicKeyToken=" 6595B64144CCF1DF " language="*"/>
    </dependentAssembly>
  </dependency>
</assembly>`,
			want: AppManifest{
				UIAccess:                          true,
				PrinterDriverIsolation:            true,
				UltraHighResolutionScrollingAware: true,
				LongPathAware:                     true,
				GDIScaling:                        true,
				UseCommonControlsV6:               true,
			},
			wantErr: false,
		},
		{
			name: "admin", xml: // language=manifest
			`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly>
  <trustInfo>
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="RequireAdministrator" uiAccess="yes"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAwareness>system</dpiAwareness>
    </windowsSettings>
  </application>
</assembly>`,
			want:    AppManifest{ExecutionLevel: RequireAdministrator},
			wantErr: false,
		},
		{
			name: "highest", xml: // language=manifest
			`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly>
  <trustInfo>
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="HIGHESTAVAILABLE" uiAccess="no thanks"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="etzetezteztzertz"/>
      <supportedOS Id=" {8E0F7A12-BFB3-4FE8-B9A5-48FD50A15A9A} "/>
    </application>
  </compatibility>
  <application>
    <windowsSettings>
      <dpiAware>true</dpiAware>
    </windowsSettings>
  </application>
</assembly>`,
			want: AppManifest{
				ExecutionLevel: HighestAvailable,
				Compatibility:  Win10AndAbove,
			},
			wantErr: false,
		},
		{
			name: "parseError", xml: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><assembly></assemble>`,
			want:    AppManifest{},
			wantErr: true,
		},
		{
			name: "os", xml: // language=manifest
			`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly>
  <trustInfo>
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="*" uiAccess="no thanks"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
      <supportedOS Id=" {8E0F7A12-BFB3-4FE8-B9A5-48FD50A15A9A} "/>
      <supportedOS Id="{e2011457-1546-43c5-a5fe-008deee3d3f0}"/>
      <supportedOS Id="{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}"/>
      <supportedOS Id="{35138b9a-5d96-4fbd-8e2d-a2440225f93a}"/>
    </application>
  </compatibility>
  <application>
    <windowsSettings>
      <dpiAware>true</dpiAware>
    </windowsSettings>
  </application>
</assembly>`,
			want:    AppManifest{Compatibility: WinVistaAndAbove},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := AppManifestFromXML([]byte(tt.xml))
			if (err != nil) != tt.wantErr {
				t.Errorf("AppManifestFromXML() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AppManifestFromXML() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_readDPIAwareness(t *testing.T) {
	type args struct {
		dpiAware     string
		dpiAwareness string
	}
	tests := []struct {
		name string
		args args
		want DPIAwareness
	}{
		{args: args{dpiAware: "", dpiAwareness: "true, systeM , permonitor"}, want: DPIAware},
		{args: args{dpiAware: "true", dpiAwareness: "perMonitorv2,system"}, want: DPIPerMonitorV2},
		{args: args{dpiAware: "true", dpiAwareness: ""}, want: DPIAware},
		{args: args{dpiAware: " true/PM ", dpiAwareness: ""}, want: DPIPerMonitor},
		{args: args{dpiAware: "false", dpiAwareness: "system"}, want: DPIAware},
		{args: args{dpiAware: "true / PM", dpiAwareness: "per monitor"}, want: DPIUnaware},
		{args: args{dpiAware: "true", dpiAwareness: "perMonitorv2,system"}, want: DPIPerMonitorV2},
		{args: args{dpiAware: "true", dpiAwareness: "PerMonitor"}, want: DPIPerMonitor},
		{args: args{dpiAware: "true", dpiAwareness: " unaWarE"}, want: DPIUnaware},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readDPIAwareness(tt.args.dpiAware, tt.args.dpiAwareness); got != tt.want {
				t.Errorf("readDPIAwareness() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_AppManifest_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		manifest AppManifest
		want     string
		wantErr  string
	}{
		{
			manifest: AppManifest{},
			want:// language=json
			`{"identity":{},"description":"","minimum-os":"win7","execution-level":"","ui-access":false,"auto-elevate":false,"dpi-awareness":"system","disable-theming":false,"disable-window-filtering":false,"high-resolution-scrolling-aware":false,"ultra-high-resolution-scrolling-aware":false,"long-path-aware":false,"printer-driver-isolation":false,"gdi-scaling":false,"segment-heap":false,"use-common-controls-v6":false}`,
		},
		{
			manifest: AppManifest{
				Identity: AssemblyIdentity{
					Name:    "app.name",
					Version: [4]uint16{1, 2, 3, 65535},
				},
				Description:                  "app desc",
				Compatibility:                WinVistaAndAbove,
				ExecutionLevel:               RequireAdministrator,
				UIAccess:                     true,
				DPIAwareness:                 DPIPerMonitorV2,
				DisableTheming:               true,
				HighResolutionScrollingAware: true,
				LongPathAware:                true,
				GDIScaling:                   true,
				UseCommonControlsV6:          true,
			},
			want:// language=json
			`{"identity":{"name":"app.name","version":"1.2.3.65535"},"description":"app desc","minimum-os":"vista","execution-level":"administrator","ui-access":true,"auto-elevate":false,"dpi-awareness":"per monitor v2","disable-theming":true,"disable-window-filtering":false,"high-resolution-scrolling-aware":true,"ultra-high-resolution-scrolling-aware":false,"long-path-aware":true,"printer-driver-isolation":false,"gdi-scaling":true,"segment-heap":false,"use-common-controls-v6":true}`,
		},
		{
			manifest: AppManifest{
				Identity: AssemblyIdentity{
					Name:    "",
					Version: [4]uint16{1, 2, 3, 4},
				},
				Compatibility:          Win8AndAbove,
				ExecutionLevel:         HighestAvailable,
				AutoElevate:            true,
				DPIAwareness:           DPIPerMonitor,
				LongPathAware:          true,
				PrinterDriverIsolation: true,
				GDIScaling:             true,
				SegmentHeap:            true,
			},
			want:// language=json
			`{"identity":{},"description":"","minimum-os":"win8","execution-level":"highest","ui-access":false,"auto-elevate":true,"dpi-awareness":"per monitor","disable-theming":false,"disable-window-filtering":false,"high-resolution-scrolling-aware":false,"ultra-high-resolution-scrolling-aware":false,"long-path-aware":true,"printer-driver-isolation":true,"gdi-scaling":true,"segment-heap":true,"use-common-controls-v6":false}`,
		},
		{
			manifest: AppManifest{
				Identity: AssemblyIdentity{
					Name: "a",
				},
				Compatibility:                     Win81AndAbove,
				ExecutionLevel:                    AsInvoker,
				AutoElevate:                       true,
				DPIAwareness:                      DPIUnaware,
				DisableTheming:                    true,
				DisableWindowFiltering:            true,
				HighResolutionScrollingAware:      true,
				UltraHighResolutionScrollingAware: true,
			},
			want:// language=json
			`{"identity":{"name":"a","version":"0.0.0.0"},"description":"","minimum-os":"win8.1","execution-level":"","ui-access":false,"auto-elevate":true,"dpi-awareness":"unaware","disable-theming":true,"disable-window-filtering":true,"high-resolution-scrolling-aware":true,"ultra-high-resolution-scrolling-aware":true,"long-path-aware":false,"printer-driver-isolation":false,"gdi-scaling":false,"segment-heap":false,"use-common-controls-v6":false}`,
		},
		{
			manifest: AppManifest{
				Compatibility:                     Win10AndAbove,
				UIAccess:                          true,
				DPIAwareness:                      DPIAware,
				DisableWindowFiltering:            true,
				UltraHighResolutionScrollingAware: true,
				PrinterDriverIsolation:            true,
				SegmentHeap:                       true,
			},
			want:// language=json
			`{"identity":{},"description":"","minimum-os":"win10","execution-level":"","ui-access":true,"auto-elevate":false,"dpi-awareness":"system","disable-theming":false,"disable-window-filtering":true,"high-resolution-scrolling-aware":false,"ultra-high-resolution-scrolling-aware":true,"long-path-aware":false,"printer-driver-isolation":true,"gdi-scaling":false,"segment-heap":true,"use-common-controls-v6":false}`,
		},
		{
			manifest: AppManifest{Compatibility: 42},
			wantErr:  errUnknownSupportedOS,
		},
		{
			manifest: AppManifest{DPIAwareness: 42},
			wantErr:  errUnknownDPIAwareness,
		},
		{
			manifest: AppManifest{ExecutionLevel: 42},
			wantErr:  errUnknownExecLevel,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.manifest)
			if !isErr(err, tt.wantErr) {
				t.Errorf("json.Marshal(AppManifest) error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && string(got) != tt.want {
				t.Errorf("json.Marshal(AppManifest):\n%s\nwant:\n%s", string(got), tt.want)
			}
		})
	}
}

func isErr(err error, msg string) bool {
	if err == nil && msg == "" {
		return true
	}
	if msg == "*" {
		return err != nil
	}
	for err != nil {
		if err.Error() == msg {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

func Test_AppManifest_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    AppManifest
		wantErr string
	}{
		{
			json:// language=json
			`{}`,
			want: AppManifest{},
		},
		{
			json:// language=json
			`{"identity":{},"description":"","minimum-os":"win7","execution-level":"","ui-access":false,"auto-elevate":false,"dpi-awareness":"system","disable-theming":false,"disable-window-filtering":false,"high-resolution-scrolling-aware":false,"ultra-high-resolution-scrolling-aware":false,"long-path-aware":false,"printer-driver-isolation":false,"gdi-scaling":false,"segment-heap":false,"use-common-controls-v6":false}`,
			want: AppManifest{},
		},
		{
			json:// language=json
			`{"identity":{},"description":"","minimum-os":"win10","execution-level":"","ui-access":true,"auto-elevate":false,"dpi-awareness":"system","disable-theming":false,"disable-window-filtering":true,"high-resolution-scrolling-aware":false,"ultra-high-resolution-scrolling-aware":true,"long-path-aware":false,"printer-driver-isolation":true,"gdi-scaling":false,"segment-heap":true,"use-common-controls-v6":false}`,
			want: AppManifest{
				Compatibility:                     Win10AndAbove,
				UIAccess:                          true,
				DPIAwareness:                      DPIAware,
				DisableWindowFiltering:            true,
				UltraHighResolutionScrollingAware: true,
				PrinterDriverIsolation:            true,
				SegmentHeap:                       true,
			},
		},
		{
			json:// language=json
			`{"identity":{"name":"app.name","version":" 65535.65535.65535.65535 "},"description":"app desc","minimum-os":" Vista","execution-level":"administrator","ui-access":true,"auto-elevate":false,"dpi-awareness":"per monitor v2","disable-theming":true,"disable-window-filtering":false,"high-resolution-scrolling-aware":true,"ultra-high-resolution-scrolling-aware":false,"long-path-aware":true,"printer-driver-isolation":false,"gdi-scaling":true,"segment-heap":false,"use-common-controls-v6":true}`,
			want: AppManifest{
				Identity: AssemblyIdentity{
					Name:    "app.name",
					Version: [4]uint16{65535, 65535, 65535, 65535},
				},
				Description:                  "app desc",
				Compatibility:                WinVistaAndAbove,
				ExecutionLevel:               RequireAdministrator,
				UIAccess:                     true,
				DPIAwareness:                 DPIPerMonitorV2,
				DisableTheming:               true,
				HighResolutionScrollingAware: true,
				LongPathAware:                true,
				GDIScaling:                   true,
				UseCommonControlsV6:          true,
			},
		},
		{
			json:// language=json
			`{"identity":{"name":"app.name","version":" 1.2"},"minimum-os":"win8","execution-level":"highest","auto-elevate":true,"dpi-awareness":"per monitor","disable-window-filtering":true,"high-resolution-scrolling-aware":false,"ultra-high-resolution-scrolling-aware":true,"long-path-aware":false,"printer-driver-isolation":true,"gdi-scaling":false,"segment-heap":true,"use-common-controls-v6":false}`,
			want: AppManifest{
				Identity: AssemblyIdentity{
					Name:    "app.name",
					Version: [4]uint16{1, 2},
				},
				Compatibility:                     Win8AndAbove,
				ExecutionLevel:                    HighestAvailable,
				DPIAwareness:                      DPIPerMonitor,
				AutoElevate:                       true,
				DisableWindowFiltering:            true,
				UltraHighResolutionScrollingAware: true,
				PrinterDriverIsolation:            true,
				SegmentHeap:                       true,
			},
		},
		{
			json:// language=json
			`{"identity":{"version":"1"},"dpi-awareness":"unaware","minimum-os":"win8.1","execution-level":"as invoker","segment-heap":true,"use-common-controls-v6":true}`,
			want: AppManifest{
				Identity: AssemblyIdentity{
					Version: [4]uint16{1},
				},
				Compatibility:       Win81AndAbove,
				ExecutionLevel:      AsInvoker,
				DPIAwareness:        DPIUnaware,
				SegmentHeap:         true,
				UseCommonControlsV6: true,
			},
		},
		{
			json:// language=json
			`{"identity":{"version":"1.65536.1.1"}}`,
			wantErr: errInvalidVersion,
		},
		{
			json:// language=json
			`{"identity":{"version":"1.1.1.1.0"}}`,
			wantErr: errInvalidVersion,
		},
		{
			json:// language=json
			`{"identity":{"version":"1.1.1. 1"}}`,
			wantErr: errInvalidVersion,
		},
		{
			json:// language=json
			`{"identity":{"version":"-1.1.1.1"}}`,
			wantErr: errInvalidVersion,
		},
		{
			json:// language=json
			`{"minimum-os":"linux"}`,
			wantErr: errUnknownSupportedOS,
		},
		{
			json:// language=json
			`{"execution-level":"root"}`,
			wantErr: errUnknownExecLevel,
		},
		{
			json:// language=json
			`{"dpi-awareness":"mostly"}`,
			wantErr: errUnknownDPIAwareness,
		},
		{
			json:    `{"":""]`,
			wantErr: "*",
		},
		{
			json:// language=json
			`{"identity":["app.name"]}`,
			wantErr: "*",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got AppManifest
			err := json.Unmarshal([]byte(tt.json), &got)
			if (tt.wantErr != "") != (err != nil) || (err != nil && tt.wantErr != "*" && err.Error() != tt.wantErr) {
				t.Errorf("json.Unmarshal(AppManifest) error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("json.Unmarshal(AppManifest) got = %v, want %v", got, tt.want)
			}
		})
	}
}
