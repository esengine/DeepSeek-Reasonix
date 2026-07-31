This package tokenizes (splits) words, sentences and graphemes, based on [Unicode text segmentation](https://unicode.org/reports/tr29/) (UAX #29), for Unicode 17. Details and usage are in the respective packages:

[uax29/graphemes](https://github.com/clipperhouse/uax29/tree/master/graphemes)

[uax29/words](https://github.com/clipperhouse/uax29/tree/master/words)

[uax29/phrases](https://github.com/clipperhouse/uax29/tree/master/phrases)

[uax29/sentences](https://github.com/clipperhouse/uax29/tree/master/sentences)

### Why tokenize?

Any time our code operates on individual words, we are tokenizing. Often, we do it ad hoc, such as splitting on spaces, which gives inconsistent results. The Unicode standard is better: it is multi-lingual, and handles punctuation, special characters, etc.

### Uses

The uax29 module has 4 tokenizers. In decreasing granularity: sentences → phrases → words → graphemes. Words and graphemes are the most common uses.

You might use `words` for inverted indexes, full-text search, TF-IDF, BM25, embeddings, etc.

If you're doing embeddings, the definition of “meaningful unit” will depend on your application. You might choose sentences, phrases, words, or a combination.

### Conformance

We use the official [Unicode test suites](https://unicode.org/reports/tr41/tr41-36.html#Tests29). Status:

![Go](https://github.com/clipperhouse/uax29/actions/workflows/gotest.yml/badge.svg)

## Quick start

```
go get "github.com/clipperhouse/uax29/v2/words"
```

```go
import "github.com/clipperhouse/uax29/v2/words"

text := "Hello, 世界. Nice dog! 👍🐶"

tokens := words.FromString(text)

for tokens.Next() {                     // Next() returns true until end of data
	fmt.Println(tokens.Value())         // Do something with the current token
}
```

### See also

[jargon](https://github.com/clipperhouse/jargon), a text pipelines package for CLI and Go, which consumes this package.

### Prior art

[blevesearch/segment](https://github.com/blevesearch/segment)

[rivo/uniseg](https://github.com/rivo/uniseg)

### Other language implementations

[C#](https://github.com/clipperhouse/uax29.net) (also by me)

[JavaScript](https://github.com/tc39/proposal-intl-segmenter)

[Rust](https://unicode-rs.github.io/unicode-segmentation/unicode_segmentation/trait.UnicodeSegmentation.html)

[Java](https://lucene.apache.org/core/6_5_0/core/org/apache/lucene/analysis/standard/StandardTokenizer.html)

[Python](https://uniseg-python.readthedocs.io/en/latest/)
