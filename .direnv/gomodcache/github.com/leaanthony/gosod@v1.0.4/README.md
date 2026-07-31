<p align="center" style="text-align: center">
   <img src="logo.png" width="50%"><br/>
</p>

<p align="center">
	Scaffolding simplified<br/><br/>
   <a href="https://github.com/leaanthony/gosod/blob/master/LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg"></a>
   <a href="https://goreportcard.com/report/github.com/leaanthony/gosod"><img src="https://goreportcard.com/badge/github.com/leaanthony/gosod"/></a>
   <a href="https://godoc.org/github.com/leaanthony/gosod"><img src="https://img.shields.io/badge/godoc-reference-blue.svg"/></a>
   <a href="https://github.com/leaanthony/gosod/issues"><img src="https://img.shields.io/badge/contributions-welcome-brightgreen.svg?style=flat" alt="CodeFactor" /></a>
   <a href="https://app.fossa.io/projects/git%2Bgithub.com%2Fleaanthony%2Fgosod?ref=badge_shield" alt="FOSSA Status"><img src="https://app.fossa.io/api/projects/git%2Bgithub.com%2Fleaanthony%2Fgosod.svg?type=shield"/></a>
</p>


Features:
  - Scaffold out project directories from templates
  - Uses Go's native templating engine
  - Uses `fs.FS` for input, so it works well with `go:embed` and [debme](https://github.com/leaanthony/debme)
  - Go alternative to [cookiecutter](https://github.com/cookiecutter/cookiecutter)

## Installation 

`go get github.com/leaanthony/gosod`

## Usage

  1. Define a template directory
  2. Define some data
  3. Extract to a target directory

```go
package main

import (
	"log"

	"github.com/leaanthony/gosod"
)

type config struct {
	Name string
}


// mytemplate/
// ├── custom.filtername.txt
// ├── ignored.txt
// ├── subdir
// │   ├── included.txt
// │   └── sub.tmpl.go
// └── test.tmpl.go
//go:embed mytemplate/*
var mytemplate embed.FS

func main() {

	// Define a new Template directory
	basic, err := gosod.New(mytemplate)
	if err != nil {
		log.Fatal(err)
	}

	// Make some config data
	myConfig := &config{
		Name: "Mat",
	}
		
	// Ignore files
	basic.IgnoreFile("ignored.txt")
	
	// Custom template filters
	basic.SetTemplateFilters([]string{ ".filtername", ".tmpl" })

	// Create a new directory using the template and config
	err = basic.Extract("./generated", myConfig)
	if err != nil {
		log.Fatal(err)
	}
	
	// Ouput FS:
	// generated/
	// ├── custom.txt
	// ├── subdir
	// │   ├── included.txt
	// │   └── sub.go
	// └── test.go
}
```

## Template Directories

A template directory is simply a directory structure contianing files you wish to copy. The algorithm for copying is:

  * Categorise all files into one of: directory, standard file and template files
	* Create the directory structure
	* Copy standard files
	* Copy template files, assembled using the given data

Template files, by default, are any file with ".tmpl" in their filename. To change this, use `SetTemplateFilters([]string)`. This allows you to set any number of filters.

Files may also be ignored by using the `IgnoreFilename(string)` method.

## What's with the name?

Google is your [friend](https://translate.google.com/?sl=cy&tl=en&text=gosod&op=translate)
