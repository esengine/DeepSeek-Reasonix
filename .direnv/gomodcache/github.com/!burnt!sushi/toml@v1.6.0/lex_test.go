package toml

import "testing"

func TestPositionString(t *testing.T) {
	tests := []struct {
		in   Position
		want string
	}{
		{Position{}, "at line 0; start 0; length 0"},
		{Position{Line: 1, Col: 1, Len: 1, Start: 1}, "at line 1; start 1; length 1"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			have := tt.in.String()
			if have != tt.want {
				t.Errorf("\nhave: %q\nwant: %q", have, tt.want)
			}
		})
	}
}

func TestItemTypeString(t *testing.T) {
	for _, it := range []itemType{itemError, itemEOF, itemText,
		itemString, itemStringEsc, itemRawString, itemMultilineString,
		itemRawMultilineString, itemBool, itemInteger, itemFloat, itemDatetime,
		itemArray, itemArrayEnd, itemTableStart, itemTableEnd, itemArrayTableStart,
		itemArrayTableEnd, itemKeyStart, itemKeyEnd, itemCommentStart,
		itemInlineTableStart, itemInlineTableEnd,
	} {
		have := it.String()
		if have == "" {
			t.Errorf("empty String for %T", it)
		}
	}
}

func TestItemString(t *testing.T) {
	it := item{typ: itemString, val: "xxx", pos: Position{Line: 42, Col: 3}}
	have := it.String()
	want := "(String, xxx)"
	if have != want {
		t.Errorf("\nhave: %q\nwant: %q", have, want)
	}
}

func TestStateFnString(t *testing.T) {
	{
		var fn stateFn = lexString
		have, want := fn.String(), "lexString()"
		if have != want {
			t.Errorf("\nhave: %q\nwant: %q", have, want)
		}
	}
	{
		var fn stateFn
		have, want := fn.String(), "<nil>"
		if have != want {
			t.Errorf("\nhave: %q\nwant: %q", have, want)
		}
	}
}
