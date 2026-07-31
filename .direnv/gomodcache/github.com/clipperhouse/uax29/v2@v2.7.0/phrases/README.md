An implementation of "phrase boundaries", a variation on words boundaries from [Unicode text segmentation](https://unicode.org/reports/tr29/#Word_Boundaries) (UAX 29), for Unicode 17.

"Phrases" are not a Unicode standard, it is our definition that we think may be useful. We define it as "a series of words separated only by spaces". Punctuation breaks phrases. Emojis are treated as words.

[![Documentation](https://pkg.go.dev/badge/github.com/clipperhouse/uax29/v2/phrases.svg)](https://pkg.go.dev/github.com/clipperhouse/uax29/v2/phrases)
![Tests](https://github.com/clipperhouse/uax29/actions/workflows/gotest.yml/badge.svg)
![Fuzz](https://github.com/clipperhouse/uax29/actions/workflows/gofuzz.yml/badge.svg)

## Quick start

```
go get "github.com/clipperhouse/uax29/v2/phrases"
```

```go
import "github.com/clipperhouse/uax29/v2/phrases"

text := "Hello, 世界. Nice — and totally adorable — dog; perhaps the "best one"! 🏆 🐶"

tokens := phrases.FromString(text)

for tokens.Next() {                    // Next() returns true until end of data
	fmt.Println(tokens.Value())        // Do something with the current phrase
}
```

## APIs

### If you have a `string`

```go
text := "Hello, 世界. Nice dog! 👍🐶"

tokens := phrases.FromString(text)

for tokens.Next() {                    // Next() returns true until end of data
	fmt.Println(tokens.Value())        // Do something with the current phrase
}
```

### If you have an `io.Reader`

`FromReader` embeds a [`bufio.Scanner`](https://pkg.go.dev/bufio#Scanner), so just use those methods.

```go
r := getYourReader()                      // from a file or network maybe
tokens := phrases.FromReader(r)

for tokens.Scan() {                       // Scan() returns true until error or EOF
	fmt.Println(tokens.Text())            // Do something with the current phrase
}

if tokens.Err() != nil {                  // Check the error
	log.Fatal(tokens.Err())
}
```

### If you have a `[]byte`

```go
b := []byte("Hello, 世界. Nice dog! 👍🐶")

tokens := phrases.FromBytes(b)

for tokens.Next() {                     // Next() returns true until end of data
	fmt.Println(tokens.Value())         // Do something with the current phrase
}
```

### Performance

On a Mac M2 laptop, we see around 240MB/s, or around 30 million phrases (tokens, really) per second. You should see ~constant memory, and no allocations.

### Invalid inputs

Invalid UTF-8 input is considered undefined behavior. We test to ensure that bad inputs will not cause pathological outcomes, such as a panic or infinite loop. Callers should expect "garbage-in, garbage-out".

Your pipeline should probably include a call to [`utf8.Valid()`](https://pkg.go.dev/unicode/utf8#Valid).
