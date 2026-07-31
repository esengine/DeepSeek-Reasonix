# Go Images

[![Go Reference](https://pkg.go.dev/badge/golang.org/x/image.svg)](https://pkg.go.dev/golang.org/x/image)

This repository holds supplementary Go image packages.

## Security Considerations

The packages in this repository have the same security model as the standard
library [`image`](https://pkg.go.dev/image) package. Specifically, when
operating on arbitrary images, DecodeConfig should be called before Decode, so
that the program can decide whether the image, as defined in the returned
header, can be safely decoded with the available resources. A call to Decode
which produces an extremely large image, as defined in the header returned by
DecodeConfig, is not considered a security issue, regardless of whether the
image is itself malformed or not.

## Report Issues / Send Patches

This repository uses Gerrit for code changes. To learn how to submit changes to
this repository, see https://go.dev/doc/contribute.

The git repository is https://go.googlesource.com/image.

The main issue tracker for the image repository is located at
https://go.dev/issues. Prefix your issue with "x/image:" in the
subject line, so it is easy to find.
