# Go Tree-sitter

[![CI][ci]](https://github.com/tree-sitter/py-tree-sitter/actions/workflows/ci.yml)
[![Go version][go version]](https://github.com/tree-sitter/go-tree-sitter/blob/master/go.mod)
[![Version][version]](https://github.com/tree-sitter/go-tree-sitter/tags)
[![Docs][docs]](https://pkg.go.dev/github.com/tree-sitter/go-tree-sitter)

This repository contains Go bindings for the [Tree-sitter](https://tree-sitter.github.io/tree-sitter/) parsing library.

To use this in your Go project, run:

```sh
go get github.com/tree-sitter/go-tree-sitter@latest
```

Example usage:

```go
package main

import (
    "fmt"

    tree_sitter "github.com/tree-sitter/go-tree-sitter"
    tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
)

func main() {
    code := []byte("const foo = 1 + 2")

    parser := tree_sitter.NewParser()
    defer parser.Close()
    parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_javascript.Language()))

    tree := parser.Parse(code, nil)
    defer tree.Close()

    root := tree.RootNode()
    fmt.Println(root.ToSexp())
}
```

By default, none of the grammars are included in this package.
This way, you can only bring in what you need, but it's at the slight cost of having to call `go get` n times.

In the example above, to fetch the JavaScript grammar, you can run the following:

```sh
go get github.com/tree-sitter/tree-sitter-javascript@latest
```

Alternatively you can also load grammars at runtime from a shared library via [purego](https://github.com/ebitengine/purego).

The example below shows how to load the JavaScript grammar from a shared library (`libtree-sitter-PARSER_NAME.so`) at runtime on Linux & macOS:

For more information on other platforms, see the [purego documentation](https://github.com/ebitengine/purego#supported-platforms)

```go
package main

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	"github.com/ebitengine/purego"
)

func main() {
	path := "/path/to/your/parser.so"
	lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
        // handle error
    }

	var javascriptLanguage func() uintptr
	purego.RegisterLibFunc(&javascriptLanguage, lib, "tree_sitter_javascript")

	language := tree_sitter.NewLanguage(unsafe.Pointer(javascriptLanguage()))
}
```

> [!NOTE]
> Due to [bugs with `runtime.SetFinalizer` and CGO](https://groups.google.com/g/golang-nuts/c/LIWj6Gl--es), you must always call `Close`
> on an object that allocates memory from C. This must be done for the `Parser`, `Tree`, `TreeCursor`, `Query`, `QueryCursor`, and `LookaheadIterator` objects.

For more information, see the [documentation](https://pkg.go.dev/github.com/tree-sitter/go-tree-sitter).

[ci]: https://img.shields.io/github/actions/workflow/status/tree-sitter/go-tree-sitter/ci.yml?logo=github&label=CI
[go version]: https://img.shields.io/github/go-mod/go-version/tree-sitter/go-tree-sitter
[version]: https://img.shields.io/github/v/tag/tree-sitter/go-tree-sitter?label=version
[docs]: https://pkg.go.dev/badge/github.com/tree-sitter/go-tree-sitter.svg?style=flat-square
