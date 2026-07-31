// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package html

import (
	"strings"
	"testing"
)

func TestNode_ChildNodes(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"<a></a>", "a"},
		{"a", "a"},
		{"<a></a><!--b-->", "a b"},
		{"a<b></b>c", "a b c"},
		{"a<b><!--c--></b>d", "a b d"},
		{"<a><b>c<!--d-->e</b></a>f<!--g--><h>i</h>", "a f g h"},
	}
	for _, test := range tests {
		doc, err := Parse(strings.NewReader(test.in))
		if err != nil {
			t.Fatal(err)
		}
		// Drill to <html><head></head><body>
		n := doc.FirstChild.FirstChild.NextSibling
		var results []string
		for c := range n.ChildNodes() {
			results = append(results, c.Data)
		}
		if got := strings.Join(results, " "); got != test.want {
			t.Errorf("ChildNodes = %q, want %q", got, test.want)
		}
	}
}

func TestNode_Descendants(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"<a></a>", "a"},
		{"<a><b></b></a>", "a b"},
		{"<a>b</a>", "a b"},
		{"<a><!--b--></a>", "a b"},
		{"<a>b<c></c>d</a>", "a b c d"},
		{"<a>b<c><!--d--></c>e</a>", "a b c d e"},
		{"<a><b><c>d<!--e-->f</c></b>g<!--h--><i>j</i></a>", "a b c d e f g h i j"},
	}
	for _, test := range tests {
		doc, err := Parse(strings.NewReader(test.in))
		if err != nil {
			t.Fatal(err)
		}
		// Drill to <html><head></head><body>
		n := doc.FirstChild.FirstChild.NextSibling
		var results []string
		for c := range n.Descendants() {
			results = append(results, c.Data)
		}
		if got := strings.Join(results, " "); got != test.want {
			t.Errorf("Descendants = %q; want: %q", got, test.want)
		}
	}
}

func TestNode_Ancestors(t *testing.T) {
	for _, size := range []int{0, 1, 2, 10, 100, 10_000} {
		n := buildChain(size)
		nParents := 0
		for _ = range n.Ancestors() {
			nParents++
		}
		if nParents != size {
			t.Errorf("number of Ancestors = %d; want: %d", nParents, size)
		}
	}
}

func buildChain(size int) *Node {
	child := new(Node)
	for range size {
		parent := child
		child = new(Node)
		parent.AppendChild(child)
	}
	return child
}
