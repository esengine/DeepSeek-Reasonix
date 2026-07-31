An implementation of sentence boundaries from [Unicode text segmentation](https://unicode.org/reports/tr29/#Sentence_Boundaries) (UAX 29), for Unicode 17.

[![Documentation](https://pkg.go.dev/badge/github.com/clipperhouse/uax29/v2/sentences.svg)](https://pkg.go.dev/github.com/clipperhouse/uax29/v2/sentences)
![Tests](https://github.com/clipperhouse/uax29/actions/workflows/gotest.yml/badge.svg)
![Fuzz](https://github.com/clipperhouse/uax29/actions/workflows/gofuzz.yml/badge.svg)

## Quick start

```
go get "github.com/clipperhouse/uax29/v2/sentences"
```

```go
import "github.com/clipperhouse/uax29/v2/sentences"

text := "Hello, 世界. Nice dog! 👍🐶"

tokens := sentences.FromString(text)

for tokens.Next() {                         // Next() returns true until end of data
	fmt.Println(tokens.Value())        // Do something with the current sentence
}
```

## Conformance

We use the Unicode [test suite](https://unicode.org/reports/tr41/tr41-36.html#Tests29).

![Tests](https://github.com/clipperhouse/uax29/actions/workflows/gotest.yml/badge.svg)
![Fuzz](https://github.com/clipperhouse/uax29/actions/workflows/gofuzz.yml/badge.svg)

## APIs

### If you have a `string`

```go
text := "Hello, 世界. Nice dog! 👍🐶"

tokens := sentences.FromString(text)

for tokens.Next() {                      // Next() returns true until end of data
	fmt.Println(tokens.Value())          // Do something with the current sentence
}
```

### If you have an `io.Reader`

`FromReader` embeds a [`bufio.Scanner`](https://pkg.go.dev/bufio#Scanner), so just use those methods.

```go
r := getYourReader()                       // from a file or network maybe
tokens := sentences.FromReader(r)

for tokens.Scan() {                        // Scan() returns true until error or EOF
	fmt.Println(tokens.Text())             // Do something with the current sentence
}

if tokens.Err() != nil {                   // Check the error
	log.Fatal(tokens.Err())
}
```

### If you have a `[]byte`

```go
b := []byte("Hello, 世界. Nice dog! 👍🐶")

tokens := sentences.FromBytes(b)

for tokens.Next() {                     // Next() returns true until end of data
	fmt.Println(tokens.Value())         // Do something with the current sentence
}
```

### Performance

On a Mac M2 laptop, we see around 35MB/s, or around 180 thousand sentences per second. You should see ~constant memory, and no allocations.

### Invalid inputs

Invalid UTF-8 input is considered undefined behavior. We test to ensure that bad inputs will not cause pathological outcomes, such as a panic or infinite loop. Callers should expect “garbage-in, garbage-out”.

Your pipeline should probably include a call to [`utf8.Valid()`](https://pkg.go.dev/unicode/utf8#Valid).
