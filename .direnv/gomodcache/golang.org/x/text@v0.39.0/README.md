# Go Text

[![Go Reference](https://pkg.go.dev/badge/golang.org/x/text.svg)](https://pkg.go.dev/golang.org/x/text)

This repository holds supplementary Go packages for text processing,
many involving Unicode.

## CLDR Versioning

It is important that the Unicode version used in `x/text` matches the one used
by your Go compiler. The `x/text` repository supports multiple versions of
Unicode and will match the version of Unicode to that of the Go compiler. At the
moment this is supported for Go compilers from version 1.7.

## Contribute

To submit changes to this repository, see http://go.dev/doc/contribute.

The git repository is https://go.googlesource.com/text.

To generate the tables in this repository (except for the encoding tables),
run go generate from this directory. By default tables are generated for the
Unicode version in core and the CLDR version defined in
golang.org/x/text/unicode/cldr.

Running go generate will as a side effect create a DATA subdirectory in this
directory, which holds all files that are used as a source for generating the
tables. This directory will also serve as a cache.

## Testing

Run

    go test ./...

from this directory to run all tests. Add the "-tags icu" flag to also run
ICU conformance tests (if available). This requires that you have the correct
ICU version installed on your system.

TODO:
- updating unversioned source files.

## Generating Tables

To generate the tables in this repository (except for the encoding
tables), run `go generate` from this directory. By default tables are
generated for the Unicode version in core and the CLDR version defined in
golang.org/x/text/unicode/cldr.

Running go generate will as a side effect create a DATA subdirectory in this
directory which holds all files that are used as a source for generating the
tables. This directory will also serve as a cache.

## Versions

To update a Unicode version run

    UNICODE_VERSION=x.x.x go generate

where `x.x.x` must correspond to a directory in https://www.unicode.org/Public/.
If this version is newer than the version in core it will also update the
relevant packages there. The idna package in x/net will always be updated.

To update a CLDR version run

    CLDR_VERSION=version go generate

where `version` must correspond to a directory in
https://www.unicode.org/Public/cldr/.

Note that the code gets adapted over time to changes in the data and that
backwards compatibility is not maintained.
So updating to a different version may not work.

The files in DATA/{iana|icu|w3|whatwg} are currently not versioned.

## Report Issues

The main issue tracker for the text repository is located at
https://go.dev/issues. Prefix your issue with "x/text:" in the
subject line, so it is easy to find.
