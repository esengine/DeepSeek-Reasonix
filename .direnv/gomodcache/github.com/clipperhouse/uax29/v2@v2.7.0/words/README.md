An implementation of word boundaries from [Unicode text segmentation](https://unicode.org/reports/tr29/#Word_Boundaries) (UAX 29), for Unicode 17.

[![Documentation](https://pkg.go.dev/badge/github.com/clipperhouse/uax29/v2/words.svg)](https://pkg.go.dev/github.com/clipperhouse/uax29/v2/words)
![Tests](https://github.com/clipperhouse/uax29/actions/workflows/gotest.yml/badge.svg)
![Fuzz](https://github.com/clipperhouse/uax29/actions/workflows/gofuzz.yml/badge.svg)

## Quick start

```
go get "github.com/clipperhouse/uax29/v2/words"
```

```go
import "github.com/clipperhouse/uax29/v2/words"

text := "Hello, 世界. Nice dog! 👍🐶"

tokens := words.FromString(text)

for tokens.Next() {                     // Next() returns true until end of data or error
	fmt.Println(tokens.Value())         // Do something with the current token
}
```

_Note: this package returns all tokens, including whitespace and punctuation. It's not strictly "words" in the common sense, it's "split on word boundaries"._

## Conformance

We use the Unicode [test suite](https://unicode.org/reports/tr41/tr41-36.html#Tests29).

![Tests](https://github.com/clipperhouse/uax29/actions/workflows/gotest.yml/badge.svg)
![Fuzz](https://github.com/clipperhouse/uax29/actions/workflows/gofuzz.yml/badge.svg)

## APIs

#### If you have a `string`

```go
text := "Hello, 世界. Nice dog! 👍🐶"

tokens := words.FromString(text)

for tokens.Next() {                          // Next() returns true until end of data
	fmt.Println(tokens.Value())        // Do something with the current word
}
```

#### If you have an `io.Reader`

`FromReader` embeds a [`bufio.Scanner`](https://pkg.go.dev/bufio#Scanner), so just use those methods.

```go
r := getYourReader()                   // from a file or network maybe
tokens := words.FromReader(r)

for tokens.Scan() {                    // Scan() returns true until end of data or error
	fmt.Println(tokens.Text())         // Do something with the current word
}

if tokens.Err() != nil {               // Check the error
	log.Fatal(tokens.Err())
}
```

#### If you have a `[]byte`

```go
b := []byte("Hello, 世界. Nice dog! 👍🐶")

tokens := words.FromBytes(b)

for tokens.Next() {                     // Next() returns true until end of data
	fmt.Println(tokens.Value())         // Do something with the current word
}
```

### Benchmarks

```
goos: darwin
goarch: arm64
pkg: github.com/clipperhouse/uax29/words/comparative
cpu: Apple M2

BenchmarkWordsMultilingual/clipperhouse/uax29-8      164760 ns/op	  212.20 MB/s	       0 B/op	   0 allocs/op
BenchmarkWordsMultilingual/blevesearch/segment-8     593147 ns/op	   58.94 MB/s	   40960 B/op	   1 allocs/op

BenchmarkWordsASCII/clipperhouse/uax29-8             224518 ns/op	  282.38 MB/s	       0 B/op	   0 allocs/op
BenchmarkWordsASCII/blevesearch/segment-8           1387461 ns/op	   45.69 MB/s	   65537 B/op	   1 allocs/op
```

### Invalid inputs

Invalid UTF-8 input is considered undefined behavior. We test to ensure that bad inputs will not cause pathological outcomes, such as a panic or infinite loop. Callers should expect “garbage-in, garbage-out”.

Your pipeline should probably include a call to [`utf8.Valid()`](https://pkg.go.dev/unicode/utf8#Valid).

### Joiners

By default, the UAX #29 standard will split words on hyphens, slashes, @ and other punctuation. You might wish those characters not to break words, by specifying joiners.

```go
text := "Hello, 世界. Tell me about your super-cool .com. I'm .01% interested and 3/4 of a mile away. Email me at foo@example.biz. #winning"
joiners := &words.Joiners{
	Middle:  []rune("@-/"), // appearing in the middle of a word
	Leading: []rune("#."),  // appearing at the front of a word
}

tokens := words.FromString(text)
tokens.Joiners(joiners)

for tokens.Next() {
	fmt.Println(tokens.Value())
}
```

### Limitations

This package follows the basic UAX #29 specification. For more idiomatic treatment of words across languages, there is more that can be done, scroll down to the [“Notes:” section of the standard](https://unicode.org/reports/tr29/#Word_Boundary_Rules):

> It is not possible to provide a uniform set of rules that resolves all issues across languages or that handles all ambiguous situations within a given language. The goal for the specification presented in this annex is to provide a workable default; tailored implementations can be more sophisticated.
