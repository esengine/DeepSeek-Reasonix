/*

Package winres provides functions to create a resource section for Windows executables.
This is where the application's icon, manifest, and version information are stored.

Basic usage

Create an empty ResourceSet, call the Set method to add resources to it
and then use the WriteObject method to produce an object file.

Each resource must be named, so it can later be accessed with FindResource, LoadImage, etc.
To name a resource, you may use an int that you cast to winres.ID, or a string that you cast to winres.Name.

 rs := winres.ResourceSet{}
 rs.Set(winres.RT_RCDATA, winres.Name("MYDATA"), 0, 0, []byte("some data"))
 rs.WriteObject(someFile, winres.ArchAMD64)
 rs.WriteObject(anotherFile, winres.ArchI386)

Embedding resources in a Go built application

winres produces a linkable COFF object.
Save it in your project's root directory with extension ``syso''
and it will be automatically included by ``go build''.

It is recommended to name this object with target suffixes,
so that the ``go build'' command automatically links the proper object for each target.

For example:
 rsrc_windows_amd64.syso
 rsrc_windows_386.syso

Example

Embedding an icon, version information, and a manifest:

 import (
	"image"
	"log"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
 )

 func main() {
	// First create an empty resource set
	rs := winres.ResourceSet{}

	// Make an icon group from a png file
	f, err := os.Open("icon.png")
	if err != nil {
		log.Fatalln(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		log.Fatalln(err)
	}
	f.Close()
	icon, _ := winres.NewIconFromResizedImage(img, nil)

	// Add the icon to the resource set, as "APPICON"
	rs.SetIcon(winres.Name("APPICON"), icon)

	// Make a VersionInfo structure
	vi := version.Info{
		FileVersion:    [4]uint16{1, 0, 0, 0},
		ProductVersion: [4]uint16{1, 0, 0, 1},
	}
	vi.Set(0, version.ProductName, "A Windows Application")
	vi.Set(0, version.ProductVersion, "v1.0.0.1")

	// Add the VersionInfo to the resource set
	rs.SetVersionInfo(vi)

	// Add a manifest
	rs.SetManifest(winres.AppManifest{
		ExecutionLevel:      RequireAdministrator,
		DPIAwareness:        DPIPerMonitorV2,
		UseCommonControlsV6: true,
	})

	// Create an object file for amd64
	out, err := os.Create("rsrc_windows_amd64.syso")
	defer out.Close()
	if err != nil {
		log.Fatalln(err)
	}
	err = rs.WriteObject(out, winres.ArchAMD64)
	if err != nil {
		log.Fatalln(err)
	}
 }

Localization

You can provide different resources depending on the user's langage.

To do so, you should provide a language code identifier (LCID) to the Set method.

A list of LCIDs is available there: https://docs.microsoft.com/en-us/openspecs/windows_protocols/ms-lcid/

As an example, the LCID for en-US is 0x0409.

Other functions

winres can do a few more things: extract resources from an executable, replace resources in an executable,
export cursors or icons...

*/
package winres
