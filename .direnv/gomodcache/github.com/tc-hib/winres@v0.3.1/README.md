# winres

[![Go Reference](https://pkg.go.dev/badge/github.com/tc-hib/winres.svg)](https://pkg.go.dev/github.com/tc-hib/winres)
[![codecov](https://codecov.io/gh/tc-hib/winres/branch/master/graph/badge.svg?token=JURCYAY1N1)](https://codecov.io/gh/tc-hib/winres)
[![Go Report](https://goreportcard.com/badge/github.com/tc-hib/winres)](https://goreportcard.com/report/github.com/tc-hib/winres)

Package winres provides functions for embedding resources in a Windows executable built with Go.

Most often, you'll want to embed an application icon, a manifest, and "version information", which is what you can see
in the Details tab of file properties.

## Command line tool

If you are looking for a command line tool, please head to [go-winres](https://github.com/tc-hib/go-winres).

## Alternatives

This project is similar to [akavel/rsrc](https://www.github.com/akavel/rsrc/)
and [josephspurrier/goversioninfo](https://github.com/josephspurrier/goversioninfo).

## Limitations

This is not a real resource compiler, which means it won't help you embed these UI definitions:

* `ACCELERATORS`
* `DIALOGEX`
* `MENUEX`
* `POPUP`

If you ever need them, which is unlikely, use one of those tools instead:

* `rc.exe` and `cvtres.exe` from Visual Studio
* `windres` from GNU Binary Utilities
* `llvm-rc` and `llvm-cvtres` from LLVM tools

See [Resource Compiler](https://docs.microsoft.com/en-us/windows/win32/menurc/resource-compiler) for more information.

## Usage

To embed resources, you need an `.rsrc` section in your executable. Winres provides functions to compile this `.rsrc`
section into a COFF object file.

Put this file in your project directory, name it "something.syso" or, preferably,
"something_windows_amd64.syso", and you're done :
the `go build` command will detect it and automatically use it.

You should have a look at the [command line tool](https://github.com/tc-hib/go-winres) to try it. Using the library
gives you more control, though.

Here is a quick example:

```go
package main

import (
	"io/ioutil"
	"os"

	"github.com/tc-hib/winres"
)

func main() {
	// Start by creating an empty resource set
	rs := winres.ResourceSet{}

	// Add resources
	// This is a cursor named ID(1)
	cursorData, _ := ioutil.ReadFile("cursor.cur")
	rs.Set(winres.RT_CURSOR, winres.ID(1), 0, cursorData)

	// This is a custom data type, translated in english (0x409) and french (0x40C)
	// You can find more language IDs by searching for LCID
	rs.Set(winres.Name("CUSTOM"), winres.Name("COOLDATA"), 0x409, []byte("Hello World"))
	rs.Set(winres.Name("CUSTOM"), winres.Name("COOLDATA"), 0x40C, []byte("Bonjour Monde"))

	// Compile to a COFF object file
	// It is recommended to use the target suffix "_window_amd64"
	// so that `go build` knows when not to include it.
	out, _ := os.Create("rsrc_windows_amd64.syso")
	rs.WriteObject(out, winres.ArchAMD64)
}
```

## Thanks

Many thanks to [akavel](https://github.com/akavel) for his help.

This project uses these very helpful libs:

* [nfnt/resize](https://github.com/nfnt/resize) - pure Go image resizing
