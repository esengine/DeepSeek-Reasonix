[![API Reference](https://img.shields.io/badge/api-reference-blue.svg)](https://pkg.go.dev/github.com/minio/selfupdate?tab=doc) [![Apache V2 License](https://img.shields.io/badge/license-Apache%20V2-blue.svg)](https://github.com/minio/selfupdate/blob/master/LICENSE)

# selfupdate: Build self-updating Go programs

> NOTE: Original work at github.com/inconshreveable/go-update, modified for the needs within MinIO project

Package update provides functionality to implement secure, self-updating Go programs (or other single-file targets)
A program can update itself by replacing its executable file with a new version.

It provides the flexibility to implement different updating user experiences
like auto-updating, or manual user-initiated updates. It also boasts
advanced features like binary patching and code signing verification.

Example of updating from a URL:

```go
import (
    "fmt"
    "net/http"

    "github.com/minio/selfupdate"
)

func doUpdate(url string) error {
    resp, err := http.Get(url)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    err := selfupdate.Apply(resp.Body, update.Options{})
    if err != nil {
        // error handling
    }
    return err
}
```

## Features

- Cross platform support (Windows too!)
- Binary patch application
- Checksum verification
- Code signing verification
- Support for updating arbitrary files

## License
This SDK is distributed under the [Apache License, Version 2.0](http://www.apache.org/licenses/LICENSE-2.0), see LICENSE for more information. Original work was also distributed under the same license.
