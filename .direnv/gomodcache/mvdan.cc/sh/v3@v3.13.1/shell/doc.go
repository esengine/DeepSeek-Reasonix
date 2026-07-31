// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package shell contains high-level features that use the syntax, expand, and
// interp packages under the hood.
//
// Please note that this package uses POSIX Shell syntax. As such, path names on
// Windows need to use double backslashes or be within single quotes when given
// to functions like Fields. For example:
//
//	shell.Fields("echo /foo/bar")     // on Unix-like
//	shell.Fields("echo C:\\foo\\bar") // on Windows
//	shell.Fields("echo 'C:\foo\bar'") // on Windows, with quotes
package shell
