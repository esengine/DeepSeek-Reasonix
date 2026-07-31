//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

// From https://github.com/blevesearch/segment/blob/master/segment_words_test.go

package words_test

import (
	"bytes"
	"testing"

	"github.com/clipperhouse/uax29/v2/internal/iterators/filter"
	"github.com/clipperhouse/uax29/v2/words"
)

var letter filter.Func = func(token []byte) bool {
	return filter.AlphaNumeric(token) && !words.BleveNumeric(token)
}

var number filter.Func = words.BleveNumeric
var ideo filter.Func = words.BleveIdeographic

var none filter.Func = func(token []byte) bool {
	return !filter.AlphaNumeric(token) && !words.BleveNumeric(token) && !words.BleveIdeographic(token)
}

func TestAdhocSegmentsWithType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input       []byte
		output      [][]byte
		outputTypes []filter.Func
	}{
		{
			input: []byte("Now  is the.\n End."),
			output: [][]byte{
				[]byte("Now"),
				// Known difference that bleve segment splits whitespace individually, where
				// this package concatenates spaces into a single token. This difference
				// is presumed to be irrelevant.
				// []byte(" "),
				[]byte("  "),
				[]byte("is"),
				[]byte(" "),
				[]byte("the"),
				[]byte("."),
				[]byte("\n"),
				[]byte(" "),
				[]byte("End"),
				[]byte("."),
			},
			outputTypes: []filter.Func{
				letter,
				none,
				letter,
				none,
				letter,
				none,
				none,
				none,
				letter,
				none,
			},
		},
		{
			input: []byte("3.5"),
			output: [][]byte{
				[]byte("3.5"),
			},
			outputTypes: []filter.Func{
				number,
			},
		},
		{
			input: []byte("age 25"),
			output: [][]byte{
				[]byte("age"),
				[]byte(" "),
				[]byte("25"),
			},
			outputTypes: []filter.Func{
				letter,
				none,
				number,
			},
		},
		{
			input: []byte("cat3.5"),
			output: [][]byte{
				[]byte("cat3.5"),
			},
			outputTypes: []filter.Func{
				letter,
			},
		},
		{
			input: []byte("c"),
			output: [][]byte{
				[]byte("c"),
			},
			outputTypes: []filter.Func{
				letter,
			},
		},
		{
			input: []byte("こんにちは世界"),
			output: [][]byte{
				[]byte("こ"),
				[]byte("ん"),
				[]byte("に"),
				[]byte("ち"),
				[]byte("は"),
				[]byte("世"),
				[]byte("界"),
			},
			outputTypes: []filter.Func{
				ideo,
				ideo,
				ideo,
				ideo,
				ideo,
				ideo,
				ideo,
			},
		},
		{
			input: []byte("你好世界"),
			output: [][]byte{
				[]byte("你"),
				[]byte("好"),
				[]byte("世"),
				[]byte("界"),
			},
			outputTypes: []filter.Func{
				ideo,
				ideo,
				ideo,
				ideo,
			},
		},
		{
			input: []byte("サッカ"),
			output: [][]byte{
				[]byte("サッカ"),
			},
			outputTypes: []filter.Func{
				ideo,
			},
		},
		// test for wb7b/wb7c
		{
			input: []byte(`א"א`),
			output: [][]byte{
				[]byte(`א"א`),
			},
			outputTypes: []filter.Func{
				letter,
			},
		},
	}

	for _, test := range tests {
		tokens := words.FromBytes(test.input)

		i := 0
		for tokens.Next() {
			got := tokens.Value()
			expected := test.output[i]
			if !bytes.Equal(expected, got) {
				t.Fatalf("expected %q, got %q", expected, got)
			}
			i++
		}
		if i != len(test.output) {
			t.Fatalf("missed a token in %q", test.input)
		}

		for i, f := range test.outputTypes {
			expected := test.output[i]
			if !f(expected) {
				t.Logf("input: %q, expected: %q\n", test.input, expected)
				t.Logf("Letter: %t\n", letter(expected))
				t.Logf("Ideo: %t\n", ideo(expected))
				t.Logf("Number: %t\n", number(expected))
				t.Logf("None: %t\n", none(expected))
				t.Fatal("nope")
			}
		}
	}

	for _, test := range tests {
		tokens := words.FromReader(bytes.NewReader(test.input))

		i := 0
		for tokens.Scan() {
			got := tokens.Bytes()
			expected := test.output[i]
			if !bytes.Equal(expected, got) {
				t.Fatalf("expected %q, got %q", expected, got)
			}
			i++
		}
		if i != len(test.output) {
			t.Fatalf("missed a token in %q", test.input)
		}
		if err := tokens.Err(); err != nil {
			t.Fatal(err)
		}

		for i, f := range test.outputTypes {
			output := test.output[i]
			if !f(output) {
				t.Logf("input: %q, expected: %q\n", test.input, output)
				t.Logf("Letter: %t\n", letter(output))
				t.Logf("Ideo: %t\n", ideo(output))
				t.Logf("Number: %t\n", number(output))
				t.Logf("None: %t\n", none(output))
				t.Fatal("nope")
			}
		}
	}
}
