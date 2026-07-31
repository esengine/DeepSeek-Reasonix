package words_test

import (
	"testing"

	"github.com/clipperhouse/uax29/v2/words"
)

var joinersInput = []byte("Hello, 世界. Tell me about your super-cool .com. I'm .01% interested and 3/4 of a mile away. Email me at foo@example.biz. #winning")
var joiners = &words.Joiners[[]byte]{
	Middle:  []rune("@-/"),
	Leading: []rune("#."),
}

type joinersTest struct {
	input string
	// word should be found in standard iterator
	found1 bool
	// word should be found in iterator with joiners
	found2 bool
}

var joinersTests = []joinersTest{
	{"Hello", true, true},
	{"世", true, true},
	{"super", true, false},
	{"-", true, false},
	{"cool", true, false},
	{"super-cool", false, true},
	{"com", true, false}, // ".com" should usually be split, but joined with config
	{".com", false, true},
	{"01", true, false},
	{".01", false, true},
	{"3", true, false},
	{"3/4", false, true},
	{"foo", true, false},
	{"@", true, false},
	{"example.biz", true, false},
	{"foo@example.biz", false, true},
	{"#", true, false},
	{"winning", true, false},
	{"#winning", false, true},
}

func TestGenericIteratorWithJoiners(t *testing.T) {
	t.Parallel()

	// Test with []byte and joiners
	text := []byte("Hello, 世界. Tell me about your super-cool .com. I'm .01% interested and 3/4 of a mile away. Email me at foo@example.biz. #winning")
	iter := words.FromBytes(text)

	joiners := &words.Joiners[[]byte]{
		Middle:  []rune("@-/"),
		Leading: []rune("#."),
	}
	iter.Joiners(joiners)

	var results []string
	for iter.Next() {
		results = append(results, string(iter.Value()))
	}

	// Check that some specific joined tokens exist
	expectedJoined := []string{"super-cool", ".com", ".01", "3/4", "foo@example.biz", "#winning"}
	for _, expected := range expectedJoined {
		found := false
		for _, result := range results {
			if result == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected joined token %q not found in results", expected)
		}
	}

	// Test with string and joiners
	textStr := string(text)
	iterStr := words.FromString(textStr)

	joinersStr := &words.Joiners[string]{
		Middle:  []rune("@-/"),
		Leading: []rune("#."),
	}
	iterStr.Joiners(joinersStr)

	var resultsStr []string
	for iterStr.Next() {
		resultsStr = append(resultsStr, iterStr.Value())
	}

	// Check that some specific joined tokens exist
	for _, expected := range expectedJoined {
		found := false
		for _, result := range resultsStr {
			if result == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected joined token %q not found in string results", expected)
		}
	}
}
