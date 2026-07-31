<p align="center" style="text-align: center">
   <img src="logo.png" width="50%"><br/>
</p>

<p align="center">
   <code>embed.FS</code> wrapper providing additional functionality<br/><br/>
   <a href="https://github.com/leaanthony/debme/blob/master/LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg"></a>
   <a href="https://goreportcard.com/report/github.com/leaanthony/debme"><img src="https://goreportcard.com/badge/github.com/leaanthony/debme"/></a>
   <a href="https://godoc.org/github.com/leaanthony/debme"><img src="https://img.shields.io/badge/godoc-reference-blue.svg"/></a>
   <a href="https://www.codefactor.io/repository/github/leaanthony/debme"><img src="https://www.codefactor.io/repository/github/leaanthony/debme/badge" alt="CodeFactor" /></a>
   <a href="https://github.com/leaanthony/debme/issues"><img src="https://img.shields.io/badge/contributions-welcome-brightgreen.svg?style=flat" alt="CodeFactor" /></a>
   <a href="https://app.fossa.io/projects/git%2Bgithub.com%2Fleaanthony%2Fdebme?ref=badge_shield" alt="FOSSA Status"><img src="https://app.fossa.io/api/projects/git%2Bgithub.com%2Fleaanthony%2Fdebme.svg?type=shield"/></a>
   <a href="https://github.com/avelino/awesome-go"><img src="https://awesome.re/mentioned-badge.svg" /></a>
</p>

## Features

  * Get an `embed.FS` from an embedded subdirectory
  * Handy `Copy(sourcePath, targetPath)` method to copy an embedded file to the filesystem
  * 100% `embed.FS` compatible
  * 100% code coverage

## Example

```go
package main

import (
	"embed"
	"github.com/leaanthony/debme"
	"io/fs"
)

// Example Filesystem:
//
// fixtures/
// ├── test1
// |   └── onefile.txt
// └── test2
//     └── inner
//         ├── deeper
//         |   └── three.txt
//         ├── one.txt
//         └── two.txt

//go:embed fixtures
var fixtures embed.FS

func main() {
	root, _ := debme.FS(fixtures, "fixtures")

	// Anchor to "fixtures/test1"
	test1, _ := root.FS("test1")
	files1, _ := test1.ReadDir(".")

	println(len(files1)) // 1
	println(files1[0].Name()) // "onefile.txt"

	// Anchor to "fixtures/test2/inner"
	inner, _ := root.FS("test2/inner")
	one, _ := inner.ReadFile("one.txt")

	println(string(one)) // "1"

	// Fully compatible FS
	fs.WalkDir(inner, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		println("Path:", path, " Name:", d.Name())
		return nil
	})

	/*
		Path: .  Name: inner
		Path: deeper  Name: deeper
		Path: deeper/three.txt  Name: three.txt
		Path: one.txt  Name: one.txt
		Path: two.txt  Name: two.txt
	*/
	
	// Go deeper
	deeper, _ := inner.FS("deeper")
	deeperFiles, _ := deeper.ReadDir(".")

	println(len(deeperFiles)) // 1
	println(files1[0].Name()) // "three.txt"
	
	// Copy files
	err := deeper.Copy("three.txt", "/path/to/target.txt")
}
```

## Why

Go's new embed functionality is awesome! The only thing I found a little frustrating was the need to manage base paths.
This module was created out of the need to embed multiple templates in the [Wails](https://github.com/wailsapp/wails) CLI.

