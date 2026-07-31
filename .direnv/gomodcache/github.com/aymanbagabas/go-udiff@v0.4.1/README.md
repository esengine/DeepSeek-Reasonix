# µDiff

<p>
<a href="https://github.com/aymanbagabas/go-udiff/releases"><img src="https://img.shields.io/github/release/aymanbagabas/go-udiff.svg" alt="Latest Release"></a>
<a href="https://pkg.go.dev/github.com/aymanbagabas/go-udiff?tab=doc"><img src="https://godoc.org/github.com/golang/gddo?status.svg" alt="Go Docs"></a>
<a href="https://github.com/aymanbagabas/go-udiff/actions"><img src="https://github.com/aymanbagabas/go-udiff/workflows/build/badge.svg" alt="Build Status"></a>
<a href="https://goreportcard.com/report/github.com/aymanbagabas/go-udiff"><img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/aymanbagabas/go-udiff"></a>
</p>

Micro diff (µDiff) is a Go library that implements the
[Myers'](http://www.xmailserver.org/diff2.pdf) diffing algorithm. It aims to
provide a minimal API to compute and apply diffs with zero dependencies. It
also supports generating diffs in the [Unified Format](https://www.gnu.org/software/diffutils/manual/html_node/Unified-Format.html).
If you are looking for a way to parse unified diffs, check out
[sourcegraph/go-diff](https://github.com/sourcegraph/go-diff).

This is merely a copy of the [Golang tools internal diff package](https://github.com/golang/tools/tree/master/internal/diff)
with a few modifications to export package symbols. All credit goes to the [Go authors](https://go.dev/AUTHORS).

## Usage

You can import the package using the following command:

```bash
go get github.com/aymanbagabas/go-udiff
```

## Examples

Generate a unified diff for strings `a` and `b` with the default number of
context lines (3). Use `udiff.ToUnified` to specify the number of context
lines.

```go
package main

import (
    "fmt"

    "github.com/aymanbagabas/go-udiff"
)

func main() {
    a := "Hello, world!\n"
    b := "Hello, Go!\nSay hi to µDiff"
    unified := udiff.Unified("a.txt", "b.txt", a, b)
    fmt.Println(unified)
}
```

```
--- a.txt
+++ b.txt
@@ -1 +1,2 @@
-Hello, world!
+Hello, Go!
+Say hi to µDiff
\ No newline at end of file
```

Apply changes to a string.

```go
package main

import (
    "fmt"

    "github.com/aymanbagabas/go-udiff"
)

func main() {
    a := "Hello, world!\n"
    b := "Hello, Go!\nSay hi to µDiff"

    edits := udiff.Strings(a, b)
    final, err := udiff.Apply(a, edits)
    if err != nil {
        panic(err)
    }

    fmt.Println(final)
}
```

```
Hello, Go!
Say hi to µDiff
```

To get a line-by-line diff and edits:

```go
package main

import (
    "fmt"

    "github.com/aymanbagabas/go-udiff"
)

func main() {
    a := "Hello, world!\n"
    b := "Hello, Go!\nSay hi to µDiff"

    edits := udiff.Strings(a, b)
    d, err := udiff.ToUnifiedDiff("a.txt", "b.txt", a, edits, udiff.DefaultContextLines)
    if err != nil {
        panic(err)
    }

    for _, h := range d.Hunks {
        fmt.Printf("hunk: -%d, +%d\n", h.FromLine, h.ToLine)
        for _, l := range h.Lines {
            fmt.Printf("%s %q\n", l.Kind, l.Content)
        }
    }
}
```

```
hunk: -1, +1
delete "Hello, world!\n"
insert "Hello, Go!\n"
insert "Say hi to µDiff"
```

## Alternatives

- [sergi/go-diff](https://github.com/sergi/go-diff) No longer reliable. See [#123](https://github.com/sergi/go-diff/issues/123) and [#141](https://github.com/sergi/go-diff/pull/141).
- [hexops/gotextdiff](https://github.com/hexops/gotextdiff) Takes the same approach but looks like the project is abandoned.
- [sourcegraph/go-diff](https://github.com/sourcegraph/go-diff) It doesn't compute diffs. Great package for parsing and printing unified diffs.

## Contributing

Please send any contributions [upstream](https://github.com/golang/tools). Pull
requests made against [the upstream diff package](https://github.com/golang/tools/tree/master/internal/diff)
are welcome.

## License

[BSD 3-Clause](./LICENSE-BSD) and [MIT](./LICENSE-MIT).
