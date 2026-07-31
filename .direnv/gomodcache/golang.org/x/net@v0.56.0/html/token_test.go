// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package html

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html/atom"
)

// https://github.com/golang/go/issues/58246
const issue58246 = `<!--[if gte mso 12]>
  <xml>
      <o:OfficeDocumentSettings>
      <o:AllowPNG/>
      <o:PixelsPerInch>96</o:PixelsPerInch>
      </o:OfficeDocumentSettings>
    </xml>
<![endif]-->`

type tokenTest struct {
	// A short description of the test case.
	desc string
	// The HTML to parse.
	html string
	// The string representations of the expected tokens, joined by '$'.
	golden string
}

var tokenTests = []tokenTest{
	{
		"empty",
		"",
		"",
	},
	// A single text node. The tokenizer should not break text nodes on whitespace,
	// nor should it normalize whitespace within a text node.
	{
		"text",
		"foo  bar",
		"foo  bar",
	},
	// An entity.
	{
		"entity",
		"one &lt; two",
		"one &lt; two",
	},
	// A start, self-closing and end tag. The tokenizer does not care if the start
	// and end tokens don't match; that is the job of the parser.
	{
		"tags",
		"<a>b<c/>d</e>",
		"<a>$b$<c/>$d$</e>",
	},
	// Angle brackets that aren't a tag.
	{
		"not a tag #0",
		"<",
		"&lt;",
	},
	{
		"not a tag #1",
		"</",
		"&lt;/",
	},
	{
		"not a tag #2",
		"</>",
		"<!---->",
	},
	{
		"not a tag #3",
		"a</>b",
		"a$<!---->$b",
	},
	{
		"not a tag #4",
		"</ >",
		"<!-- -->",
	},
	{
		"not a tag #5",
		"</.",
		"<!--.-->",
	},
	{
		"not a tag #6",
		"</.>",
		"<!--.-->",
	},
	{
		"not a tag #7",
		"a < b",
		"a &lt; b",
	},
	{
		"not a tag #8",
		"<.>",
		"&lt;.&gt;",
	},
	{
		"not a tag #9",
		"a<<<b>>>c",
		"a&lt;&lt;$<b>$&gt;&gt;c",
	},
	{
		"not a tag #10",
		"if x<0 and y < 0 then x*y>0",
		"if x&lt;0 and y &lt; 0 then x*y&gt;0",
	},
	{
		"not a tag #11",
		"<<p>",
		"&lt;$<p>",
	},
	// EOF in a tag name.
	{
		"tag name eof #0",
		"<a",
		"",
	},
	{
		"tag name eof #1",
		"<a ",
		"",
	},
	{
		"tag name eof #2",
		"a<b",
		"a",
	},
	{
		"tag name eof #3",
		"<a><b",
		"<a>",
	},
	{
		"tag name eof #4",
		`<a x`,
		``,
	},
	// Some malformed tags that are missing a '>'.
	{
		"malformed tag #0",
		`<p</p>`,
		`<p< p="">`,
	},
	{
		"malformed tag #1",
		`<p </p>`,
		`<p <="" p="">`,
	},
	{
		"malformed tag #2",
		`<p id`,
		``,
	},
	{
		"malformed tag #3",
		`<p id=`,
		``,
	},
	{
		"malformed tag #4",
		`<p id=>`,
		`<p id="">`,
	},
	{
		"malformed tag #5",
		`<p id=0`,
		``,
	},
	{
		"malformed tag #6",
		`<p id=0</p>`,
		`<p id="0&lt;/p">`,
	},
	{
		"malformed tag #7",
		`<p id="0</p>`,
		``,
	},
	{
		"malformed tag #8",
		`<p id="0"</p>`,
		`<p id="0" <="" p="">`,
	},
	{
		"malformed tag #9",
		`<p></p id`,
		`<p>`,
	},
	// Raw text and RCDATA.
	{
		"basic raw text",
		"<script><a></b></script>",
		"<script>$&lt;a&gt;&lt;/b&gt;$</script>",
	},
	{
		"unfinished script end tag",
		"<SCRIPT>a</SCR",
		"<script>$a&lt;/SCR",
	},
	{
		"broken script end tag",
		"<SCRIPT>a</SCR ipt>",
		"<script>$a&lt;/SCR ipt&gt;",
	},
	{
		"EOF in script end tag",
		"<SCRIPT>a</SCRipt",
		"<script>$a&lt;/SCRipt",
	},
	{
		"scriptx end tag",
		"<SCRIPT>a</SCRiptx",
		"<script>$a&lt;/SCRiptx",
	},
	{
		"' ' completes script end tag",
		"<SCRIPT>a</SCRipt ",
		"<script>$a",
	},
	{
		"'>' completes script end tag",
		"<SCRIPT>a</SCRipt>",
		"<script>$a$</script>",
	},
	{
		"self-closing script end tag",
		"<SCRIPT>a</SCRipt/>",
		"<script>$a$</script>",
	},
	{
		"nested script tag",
		"<SCRIPT>a</SCRipt<script>",
		"<script>$a&lt;/SCRipt&lt;script&gt;",
	},
	{
		"script end tag after unfinished",
		"<SCRIPT>a</SCRipt</script>",
		"<script>$a&lt;/SCRipt$</script>",
	},
	{
		"script/style mismatched tags",
		"<script>a</style>",
		"<script>$a&lt;/style&gt;",
	},
	{
		"style element with entity",
		"<style>&apos;",
		"<style>$&amp;apos;",
	},
	{
		"textarea with tag",
		"<textarea><div></textarea>",
		"<textarea>$&lt;div&gt;$</textarea>",
	},
	{
		"title with tag and entity",
		"<title><b>K&amp;R C</b></title>",
		"<title>$&lt;b&gt;K&amp;R C&lt;/b&gt;$</title>",
	},
	{
		"title with trailing '&lt;' entity",
		"<title>foobar<</title>",
		"<title>$foobar&lt;$</title>",
	},
	// DOCTYPE tests.
	{
		"Proper DOCTYPE",
		"<!DOCTYPE html>",
		"<!DOCTYPE html>",
	},
	{
		"DOCTYPE with no space",
		"<!doctypehtml>",
		"<!DOCTYPE html>",
	},
	{
		"DOCTYPE with two spaces",
		"<!doctype  html>",
		"<!DOCTYPE html>",
	},
	{
		"looks like DOCTYPE but isn't",
		"<!DOCUMENT html>",
		"<!--DOCUMENT html-->",
	},
	{
		"DOCTYPE at EOF",
		"<!DOCtype",
		"<!DOCTYPE >",
	},
	// XML processing instructions.
	{
		"XML processing instruction",
		"<?xml?>",
		"<!--?xml?-->",
	},
	// Comments. See also func TestComments.
	{
		"comment0",
		"abc<b><!-- skipme --></b>def",
		"abc$<b>$<!-- skipme -->$</b>$def",
	},
	{
		"comment1",
		"a<!-->z",
		"a$<!---->$z",
	},
	{
		"comment2",
		"a<!--->z",
		"a$<!---->$z",
	},
	{
		"comment3",
		"a<!--x>-->z",
		"a$<!--x>-->$z",
	},
	{
		"comment4",
		"a<!--x->-->z",
		"a$<!--x-&gt;-->$z",
	},
	{
		"comment5",
		"a<!>z",
		"a$<!---->$z",
	},
	{
		"comment6",
		"a<!->z",
		"a$<!----->$z",
	},
	{
		"comment7",
		"a<!---<>z",
		"a$<!---<>z-->",
	},
	{
		"comment8",
		"a<!--z",
		"a$<!--z-->",
	},
	{
		"comment9",
		"a<!--z-",
		"a$<!--z-->",
	},
	{
		"comment10",
		"a<!--z--",
		"a$<!--z-->",
	},
	{
		"comment11",
		"a<!--z---",
		"a$<!--z--->",
	},
	{
		"comment12",
		"a<!--z----",
		"a$<!--z---->",
	},
	{
		"comment13",
		"a<!--x--!>z",
		"a$<!--x-->$z",
	},
	{
		"comment14",
		"a<!--!-->z",
		"a$<!--!-->$z",
	},
	{
		"comment15",
		"a<!-- !-->z",
		"a$<!-- !-->$z",
	},
	{
		"comment16",
		"a<!--i\x00j-->z",
		"a$<!--i\uFFFDj-->$z",
	},
	{
		"comment17",
		"a<!--\x00",
		"a$<!--\uFFFD-->",
	},
	{
		"comment18",
		"a<!--<!-->z",
		"a$<!--<!-->$z",
	},
	{
		"comment19",
		"a<!--<!--",
		"a$<!--<!-->",
	},
	{
		"comment20",
		"a<!--ij--kl-->z",
		"a$<!--ij--kl-->$z",
	},
	{
		"comment21",
		"a<!--ij--kl--!>z",
		"a$<!--ij--kl-->$z",
	},
	{
		"comment22",
		"a<!--!--!<--!-->z",
		"a$<!--!--!<--!-->$z",
	},
	{
		"comment23",
		"a<!--&gt;-->z",
		"a$<!--&gt;-->$z",
	},
	{
		"comment24",
		"a<!--&gt;>x",
		"a$<!--&gt;>x-->",
	},
	{
		"comment25",
		"a<!--&gt;&gt;",
		"a$<!--&gt;>-->",
	},
	{
		"comment26",
		"a<!--&gt;&gt;-",
		"a$<!--&gt;>-->",
	},
	{
		"comment27",
		"a<!--&gt;&gt;-->z",
		"a$<!--&gt;>-->$z",
	},
	{
		"comment28",
		"a<!--&amp;&gt;-->z",
		"a$<!--&amp;>-->$z",
	},
	{
		"comment29",
		"a<!--&amp;gt;-->z",
		"a$<!--&amp;gt;-->$z",
	},
	{
		"comment30",
		"a<!--&nosuchentity;-->z",
		"a$<!--&amp;nosuchentity;-->$z",
	},
	{
		"comment31",
		"a<!--i>>j-->z",
		"a$<!--i>>j-->$z",
	},
	{
		"comment32",
		"a<!--i!>>j-->z",
		"a$<!--i!&gt;>j-->$z",
	},
	// https://stackoverflow.design/email/base/mso/#targeting-specific-outlook-versions
	// says "[For] Windows Outlook 2003 and above... conditional comments allow
	// us to add bits of HTML that are only read by the Word-based versions of
	// Outlook". These comments (with angle brackets) should pass through
	// unchanged (by this Go package) when rendering.
	//
	// We should also still escape ">" as "&gt;" when necessary.
	// https://github.com/golang/go/issues/48237
	//
	// The "your code" example below comes from that stackoverflow.design link
	// above but note that it can contain angle-bracket-rich XML.
	// https://github.com/golang/go/issues/58246
	{
		"issue48237CommentWithAmpgtsemi1",
		"a<!--<p></p>&lt;!--[video]--&gt;-->z",
		"a$<!--<p></p><!--[video]--&gt;-->$z",
	},
	{
		"issue48237CommentWithAmpgtsemi2",
		"a<!--<p></p>&lt;!--[video]--!&gt;-->z",
		"a$<!--<p></p><!--[video]--!&gt;-->$z",
	},
	{
		"issue58246MicrosoftOutlookComment1",
		"a<!--[if mso]> your code <![endif]-->z",
		"a$<!--[if mso]> your code <![endif]-->$z",
	},
	{
		"issue58246MicrosoftOutlookComment2",
		"a" + issue58246 + "z",
		"a$" + issue58246 + "$z",
	},
	// An attribute with a backslash.
	{
		"backslash",
		`<p id="a\"b">`,
		`<p id="a\" b"="">`,
	},
	// Entities, tag name and attribute key lower-casing, and whitespace
	// normalization within a tag.
	{
		"tricky",
		"<p \t\n iD=\"a&quot;B\"  foo=\"bar\"><EM>te&lt;&amp;;xt</em></p>",
		`<p id="a&#34;B" foo="bar">$<em>$te&lt;&amp;;xt$</em>$</p>`,
	},
	// A nonexistent entity. Tokenizing and converting back to a string should
	// escape the "&" to become "&amp;".
	{
		"noSuchEntity",
		`<a b="c&noSuchEntity;d">&lt;&alsoDoesntExist;&`,
		`<a b="c&amp;noSuchEntity;d">$&lt;&amp;alsoDoesntExist;&amp;`,
	},
	{
		"entity without semicolon",
		`&notit;&notin;<a b="q=z&amp=5&notice=hello&not;=world">`,
		`¬it;∉$<a b="q=z&amp;amp=5&amp;notice=hello¬=world">`,
	},
	{
		"entity with digits",
		"&frac12;",
		"½",
	},
	// Attribute tests:
	// http://dev.w3.org/html5/pf-summary/Overview.html#attributes
	{
		"Empty attribute",
		`<input disabled FOO>`,
		`<input disabled="" foo="">`,
	},
	{
		"Empty attribute, whitespace",
		`<input disabled FOO >`,
		`<input disabled="" foo="">`,
	},
	{
		"Unquoted attribute value",
		`<input value=yes FOO=BAR>`,
		`<input value="yes" foo="BAR">`,
	},
	{
		"Unquoted attribute value, spaces",
		`<input value = yes FOO = BAR>`,
		`<input value="yes" foo="BAR">`,
	},
	{
		"Unquoted attribute value, trailing space",
		`<input value=yes FOO=BAR >`,
		`<input value="yes" foo="BAR">`,
	},
	{
		"Single-quoted attribute value",
		`<input value='yes' FOO='BAR'>`,
		`<input value="yes" foo="BAR">`,
	},
	{
		"Single-quoted attribute value, trailing space",
		`<input value='yes' FOO='BAR' >`,
		`<input value="yes" foo="BAR">`,
	},
	{
		"Double-quoted attribute value",
		`<input value="I'm an attribute" FOO="BAR">`,
		`<input value="I&#39;m an attribute" foo="BAR">`,
	},
	{
		"Attribute name characters",
		`<meta http-equiv="content-type">`,
		`<meta http-equiv="content-type">`,
	},
	{
		"Mixed attributes",
		`a<P V="0 1" w='2' X=3 y>z`,
		`a$<p v="0 1" w="2" x="3" y="">$z`,
	},
	{
		"Attributes with a solitary single quote",
		`<p id=can't><p id=won't>`,
		`<p id="can&#39;t">$<p id="won&#39;t">`,
	},
	// WHATWG 13.2.5.32 equals sign before attribute name state
	{
		"equals sign before attribute name",
		`<p  =>`,
		`<p =="">`,
	},
	{
		"equals sign before attribute name, extra cruft",
		`<p  =asd>`,
		`<p =asd="">`,
	},
	{
		"forward slash before attribute name",
		`<p/=">`,
		`<p ="="">`,
	},
	{
		"forward slash before attribute name with spaces around",
		`<p / =">`,
		`<p ="="">`,
	},
	{
		"forward slash after attribute name followed by a character",
		`<p a/ ="">`,
		`<p a="" =""="">`,
	},
	{
		"slash at end of unquoted attribute value",
		`<p a="\">`,
		`<p a="\">`,
	},
	{
		"self-closing tag with attribute",
		`<p a=/>`,
		`<p a="/">`,
	},
	{
		"duplicate attributes",
		`<p foo="bar" foo="baz">`,
		`<p foo="bar">`,
	},
	{
		"duplicate attributes, different case",
		`<p FOO="bar" foo="baz">`,
		`<p foo="bar">`,
	},
	{
		"partial doctype",
		`<!doc`,
		`<!--doc-->`,
	},
	{
		"partial cdata",
		`<![CDA`,
		`<!--[CDA-->`,
	},
	{
		"partial comment",
		`<!comment`,
		`<!--comment-->`,
	},
}

func TestTokenizer(t *testing.T) {
	for _, tt := range tokenTests {
		t.Run(tt.desc, func(t *testing.T) {
			z := NewTokenizer(strings.NewReader(tt.html))
			z.AllowCDATA(true)
			if tt.golden != "" {
				for i, s := range strings.Split(tt.golden, "$") {
					if z.Next() == ErrorToken {
						t.Errorf("%s token %d: want %q got error %v", tt.desc, i, s, z.Err())
						return
					}
					actual := z.Token().String()
					if s != actual {
						t.Errorf("%s token %d: want %q got %q", tt.desc, i, s, actual)
						return
					}
				}
			}
			z.Next()
			if z.Err() != io.EOF {
				t.Errorf("%s: want EOF got %q", tt.desc, z.Err())
			}
		})
	}
}

func TestMaxBuffer(t *testing.T) {
	// Exceeding the maximum buffer size generates ErrBufferExceeded.
	z := NewTokenizer(strings.NewReader("<" + strings.Repeat("t", 10)))
	z.SetMaxBuf(5)
	tt := z.Next()
	if got, want := tt, ErrorToken; got != want {
		t.Fatalf("token type: got: %v want: %v", got, want)
	}
	if got, want := z.Err(), ErrBufferExceeded; got != want {
		t.Errorf("error type: got: %v want: %v", got, want)
	}
	if got, want := string(z.Raw()), "<tttt"; got != want {
		t.Fatalf("buffered before overflow: got: %q want: %q", got, want)
	}
}

func TestMaxBufferReconstruction(t *testing.T) {
	// Exceeding the maximum buffer size at any point while tokenizing permits
	// reconstructing the original input.
tests:
	for _, test := range tokenTests {
		for maxBuf := 1; ; maxBuf++ {
			r := strings.NewReader(test.html)
			z := NewTokenizer(r)
			z.SetMaxBuf(maxBuf)
			var tokenized bytes.Buffer
			for {
				tt := z.Next()
				tokenized.Write(z.Raw())
				if tt == ErrorToken {
					if err := z.Err(); err != io.EOF && err != ErrBufferExceeded {
						t.Errorf("%s: unexpected error: %v", test.desc, err)
					}
					break
				}
			}
			// Anything tokenized along with untokenized input or data left in the reader.
			assembled, err := io.ReadAll(io.MultiReader(&tokenized, bytes.NewReader(z.Buffered()), r))
			if err != nil {
				t.Errorf("%s: ReadAll: %v", test.desc, err)
				continue tests
			}
			if got, want := string(assembled), test.html; got != want {
				t.Errorf("%s: reassembled html:\n got: %q\nwant: %q", test.desc, got, want)
				continue tests
			}
			// EOF indicates that we completed tokenization and hence found the max
			// maxBuf that generates ErrBufferExceeded, so continue to the next test.
			if z.Err() == io.EOF {
				break
			}
		} // buffer sizes
	} // tests
}

func TestPassthrough(t *testing.T) {
	// Accumulating the raw output for each parse event should reconstruct the
	// original input.
	for _, test := range tokenTests {
		z := NewTokenizer(strings.NewReader(test.html))
		var parsed bytes.Buffer
		for {
			tt := z.Next()
			parsed.Write(z.Raw())
			if tt == ErrorToken {
				break
			}
		}
		if got, want := parsed.String(), test.html; got != want {
			t.Errorf("%s: parsed output:\n got: %q\nwant: %q", test.desc, got, want)
		}
	}
}

func TestBufAPI(t *testing.T) {
	s := "0<a>1</a>2<b>3<a>4<a>5</a>6</b>7</a>8<a/>9"
	z := NewTokenizer(bytes.NewBufferString(s))
	var result bytes.Buffer
	depth := 0
loop:
	for {
		tt := z.Next()
		switch tt {
		case ErrorToken:
			if z.Err() != io.EOF {
				t.Error(z.Err())
			}
			break loop
		case TextToken:
			if depth > 0 {
				result.Write(z.Text())
			}
		case StartTagToken, EndTagToken:
			tn, _ := z.TagName()
			if len(tn) == 1 && tn[0] == 'a' {
				if tt == StartTagToken {
					depth++
				} else {
					depth--
				}
			}
		}
	}
	u := "14567"
	v := string(result.Bytes())
	if u != v {
		t.Errorf("TestBufAPI: want %q got %q", u, v)
	}
}

func TestConvertNewlines(t *testing.T) {
	testCases := map[string]string{
		"Mac\rDOS\r\nUnix\n":    "Mac\nDOS\nUnix\n",
		"Unix\nMac\rDOS\r\n":    "Unix\nMac\nDOS\n",
		"DOS\r\nDOS\r\nDOS\r\n": "DOS\nDOS\nDOS\n",
		"":                      "",
		"\n":                    "\n",
		"\n\r":                  "\n\n",
		"\r":                    "\n",
		"\r\n":                  "\n",
		"\r\n\n":                "\n\n",
		"\r\n\r":                "\n\n",
		"\r\n\r\n":              "\n\n",
		"\r\r":                  "\n\n",
		"\r\r\n":                "\n\n",
		"\r\r\n\n":              "\n\n\n",
		"\r\r\r\n":              "\n\n\n",
		"\r \n":                 "\n \n",
		"xyz":                   "xyz",
	}
	for in, want := range testCases {
		if got := string(convertNewlines([]byte(in))); got != want {
			t.Errorf("input %q: got %q, want %q", in, got, want)
		}
	}
}

func TestReaderEdgeCases(t *testing.T) {
	const s = "<p>An io.Reader can return (0, nil) or (n, io.EOF).</p>"
	testCases := []io.Reader{
		&zeroOneByteReader{s: s},
		&eofStringsReader{s: s},
		&stuckReader{},
	}
	for i, tc := range testCases {
		got := []TokenType{}
		z := NewTokenizer(tc)
		for {
			tt := z.Next()
			if tt == ErrorToken {
				break
			}
			got = append(got, tt)
		}
		if err := z.Err(); err != nil && err != io.EOF {
			if err != io.ErrNoProgress {
				t.Errorf("i=%d: %v", i, err)
			}
			continue
		}
		want := []TokenType{
			StartTagToken,
			TextToken,
			EndTagToken,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("i=%d: got %v, want %v", i, got, want)
			continue
		}
	}
}

func TestSelfClosingTagValueConfusion(t *testing.T) {
	z := NewTokenizer(strings.NewReader(`<p a=/>`))
	tok := z.Next()
	if tok != StartTagToken {
		t.Fatalf("unexpected token type: got %s, want %s", tok, StartTagToken)
	}
}

// zeroOneByteReader is like a strings.Reader that alternates between
// returning 0 bytes and 1 byte at a time.
type zeroOneByteReader struct {
	s string
	n int
}

func (r *zeroOneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.s) == 0 {
		return 0, io.EOF
	}
	r.n++
	if r.n%2 != 0 {
		return 0, nil
	}
	p[0], r.s = r.s[0], r.s[1:]
	return 1, nil
}

// eofStringsReader is like a strings.Reader but can return an (n, err) where
// n > 0 && err != nil.
type eofStringsReader struct {
	s string
}

func (r *eofStringsReader) Read(p []byte) (int, error) {
	n := copy(p, r.s)
	r.s = r.s[n:]
	if r.s != "" {
		return n, nil
	}
	return n, io.EOF
}

// stuckReader is an io.Reader that always returns no data and no error.
type stuckReader struct{}

func (*stuckReader) Read(p []byte) (int, error) {
	return 0, nil
}

const (
	rawLevel = iota
	lowLevel
	highLevel
)

func benchmarkTokenizer(b *testing.B, level int) {
	buf, err := os.ReadFile("testdata/go1.html")
	if err != nil {
		b.Fatalf("could not read testdata/go1.html: %v", err)
	}
	b.SetBytes(int64(len(buf)))
	runtime.GC()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		z := NewTokenizer(bytes.NewBuffer(buf))
		for {
			tt := z.Next()
			if tt == ErrorToken {
				if err := z.Err(); err != nil && err != io.EOF {
					b.Fatalf("tokenizer error: %v", err)
				}
				break
			}
			switch level {
			case rawLevel:
				// Calling z.Raw just returns the raw bytes of the token. It does
				// not unescape &lt; to <, or lower-case tag names and attribute keys.
				z.Raw()
			case lowLevel:
				// Calling z.Text, z.TagName and z.TagAttr returns []byte values
				// whose contents may change on the next call to z.Next.
				switch tt {
				case TextToken, CommentToken, DoctypeToken:
					z.Text()
				case StartTagToken, SelfClosingTagToken:
					_, more := z.TagName()
					for more {
						_, _, more = z.TagAttr()
					}
				case EndTagToken:
					z.TagName()
				}
			case highLevel:
				// Calling z.Token converts []byte values to strings whose validity
				// extend beyond the next call to z.Next.
				z.Token()
			}
		}
	}
}

func BenchmarkRawLevelTokenizer(b *testing.B)  { benchmarkTokenizer(b, rawLevel) }
func BenchmarkLowLevelTokenizer(b *testing.B)  { benchmarkTokenizer(b, lowLevel) }
func BenchmarkHighLevelTokenizer(b *testing.B) { benchmarkTokenizer(b, highLevel) }

type h5libTest struct {
	Description   string
	Input         string
	InitialStates []string
	Output        []Token
	Errors        []struct{ Code string }
}

var unicodeRegexp = regexp.MustCompile(`\\u([0-9a-fA-F]{4})`)

const (
	tx = 0b10000000
	t2 = 0b11000000
	t3 = 0b11100000
	t4 = 0b11110000

	maskx = 0b00111111

	rune1Max = 1<<7 - 1
	rune2Max = 1<<11 - 1
	rune3Max = 1<<16 - 1

	surrogateMin = 0xD800
	surrogateMax = 0xDFFF
)

func unescapeUnicode(s string) string {
	return unicodeRegexp.ReplaceAllStringFunc(s, func(match string) string {
		// match is something like "\u0000"
		// Extract the 4 hex digits
		hex := match[2:]

		// Parse the hex digits into an integer
		n, err := strconv.ParseInt(hex, 16, 32)
		if err != nil {
			panic(err)
		}

		// The following is a loose copy of unicode/utf8.AppendRune, which ignores
		// some of the error checking, which is necessary to support some of the
		// test characters.

		if uint32(n) <= rune1Max {
			return string(byte(n))
		}

		// Convert the integer to a string
		switch i := uint32(n); {
		case i <= rune2Max:
			return string([]byte{t2 | byte(n>>6), tx | byte(n)&maskx})
		case i <= rune3Max:
			return string([]byte{t3 | byte(n>>12), tx | byte(n>>6)&maskx, tx | byte(n)&maskx})
		case i > rune3Max: // && i <= MaxRune:
			return string([]byte{t4 | byte(n>>18), tx | byte(n>>12)&maskx, tx | byte(n>>6)&maskx, tx | byte(n)&maskx})
		default:
			panic(fmt.Sprintf("unsupported rune %x", n))
		}
	})
}

func (t *h5libTest) UnmarshalJSON(data []byte) error {
	var test struct {
		Description   string
		Input         string
		DoubleEscaped bool
		InitialStates []string
		Output        [][]any
		Errors        []struct{ Code string }
	}
	if err := json.Unmarshal(data, &test); err != nil {
		return err
	}
	*t = h5libTest{
		Description:   test.Description,
		Input:         test.Input,
		InitialStates: test.InitialStates,
		Errors:        test.Errors,
	}

	if test.DoubleEscaped {
		t.Input = unescapeUnicode(t.Input)
	}

	for _, testToken := range test.Output {
		token := Token{}
		switch testToken[0].(string) {
		case "DOCTYPE":
			token.Type = DoctypeToken
			if testToken[1] != nil {
				token.Data = testToken[1].(string)
			}
			// TODO: public/system id, we don't really support this?
		case "StartTag":
			if len(testToken) == 4 && testToken[3].(bool) == true {
				token.Type = SelfClosingTagToken
			} else {
				token.Type = StartTagToken
			}
			token.Data = testToken[1].(string)
		case "EndTag":
			token.Type = EndTagToken
			token.Data = testToken[1].(string)
		case "Comment":
			token.Type = CommentToken
			token.Data = testToken[1].(string)
		case "Character":
			token.Type = TextToken
			token.Data = testToken[1].(string)
		default:
			return fmt.Errorf("unknown token type %s", testToken[0])
		}

		if test.DoubleEscaped {
			token.Data = unescapeUnicode(token.Data)
		}

		if testToken[0] == "DOCTYPE" || testToken[0] == "StartTag" || testToken[0] == "EndTag" {
			token.DataAtom = atom.Lookup([]byte(token.Data))
		}

		if (testToken[0] == "StartTag" || testToken[0] == "EndTag") && len(testToken) > 2 {
			for k, v := range testToken[2].(map[string]any) {
				token.Attr = append(token.Attr, Attribute{
					Key: k,
					Val: v.(string),
				})
			}
		}

		t.Output = append(t.Output, token)
	}

	return nil
}

func TestHTML5LibTests(t *testing.T) {
	skipTests := map[string]bool{
		// We emit a comment token here, instead of no token. This is a specification
		// divergence that we may want to fix.
		"test1.test/Empty end tag":                           true,
		"test2.test/Empty end tag with following characters": true,
		"test2.test/Empty end tag with following tag":        true,
		"test2.test/Empty end tag with following comment":    true,
		"test2.test/Empty end tag with following end tag":    true,
		"test3.test/</>":                                 true,
		"test4.test/CR EOF after doctype name":           true,
		"test4.test/Doctype public case-sensitivity (1)": true,
		"test4.test/Doctype public case-sensitivity (2)": true,
		"test4.test/Doctype system case-sensitivity (1)": true,
		"test4.test/Doctype system case-sensitivity (2)": true,
	}

	var tests struct {
		Tests []h5libTest
	}
	testFiles, err := filepath.Glob("testdata/html5lib-tests/tokenizer/*.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, testFile := range testFiles {
		data, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &tests); err != nil {
			t.Fatal(err)
		}

		base := filepath.Base(testFile)

		for _, tc := range tests.Tests {
			name := fmt.Sprintf("%s/%s", base, tc.Description)
			t.Run(name, func(t *testing.T) {
				if skipTests[name] {
					t.Skip("skipping, known failure")
				}
				if len(tc.InitialStates) > 0 {
					t.Skip("Initial states not supported yet")
				}
				if strings.Contains(tc.Input, "<!DOCTYPE") {
					t.Skip("Skipping DOCTYPE")
				}
				z := NewTokenizer(strings.NewReader(tc.Input))
				var tokens []Token
				for {
					if z.Next() == ErrorToken {
						if z.Err() == io.EOF {
							break
						}
						t.Fatalf("Error: %v", z.Err())
					}
					tokens = append(tokens, z.Token())
				}
				sortTokenAttributes(tokens)
				sortTokenAttributes(tc.Output)
				if !reflect.DeepEqual(tokens, tc.Output) {
					t.Errorf("\nInput: %s\nGot:\t%#v\nWant:\t%#v\nParse Errors: %s\n", tc.Input, tokens, tc.Output, tc.Errors)
				}
			})
		}
	}
}

func sortTokenAttributes(tokens []Token) {
	for _, token := range tokens {
		slices.SortFunc(token.Attr, func(a, b Attribute) int {
			return strings.Compare(a.Namespace+a.Key+a.Val, b.Namespace+b.Key+b.Val)
		})
	}
}

func TestUnicodeAttributeCase(t *testing.T) {
	// <div a="1" A="1"> is resolved to <div a="1"> because a and A are considered
	// duplicate attribute names. Different unicode cases are not considered equal
	// though, so <div ä="1" Ä="1"> is tokenized as <div ä="1" Ä="1">.
	f := `<div ä="1" Ä="1">`
	z := NewTokenizer(strings.NewReader(f))
	if tt := z.Next(); tt != StartTagToken {
		t.Fatalf("expected StartTagToken, got %s", tt)
	}
	tok := z.Token()
	if len(tok.Attr) != 2 {
		t.Fatalf("expected 2 attributes, got %d", len(tok.Attr))
	}
	if tok.Attr[0].Key != "ä" {
		t.Errorf("expected attribute key to be 'ä', got %s", tok.Attr[0].Key)
	}
	if tok.Attr[1].Key != "Ä" {
		t.Errorf("expected attribute key to be 'Ä', got %s", tok.Attr[1].Key)
	}
}
