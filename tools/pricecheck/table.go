// table.go — reading a published price page as rows of cells.
package main

import (
	"io"
	"strings"

	"golang.org/x/net/html"
)

// table is one HTML table as rows of trimmed cell text. Spans are not expanded:
// a reader here locates a value by the label on its own row, never by counting
// columns, because a vendor adding a column would silently shift every index.
type table [][]string

// parseTables returns every table on the page, in document order.
func parseTables(r io.Reader) ([]table, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	var tables []table
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "table" {
			tables = append(tables, rowsOf(n))
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return tables, nil
}

func rowsOf(node *html.Node) table {
	var rows table
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			rows = append(rows, cellsOf(n))
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return rows
}

func cellsOf(node *html.Node) []string {
	var cells []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "td" || n.Data == "th") {
			cells = append(cells, strings.Join(strings.Fields(textOf(n)), " "))
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return cells
}

func textOf(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(textOf(child))
		b.WriteString(" ")
	}
	return b.String()
}

// rowWithCell finds the one row holding a cell equal to a label. Equality, not
// containment: "Cached Input" is a substring of "Uncached Input", and a reader
// that cannot tell those apart reports the wrong half of a price table.
func (t table) rowWithCell(labels ...string) ([]string, bool) {
	var found []string
	for _, row := range t {
		matched := false
		for _, cell := range row {
			for _, label := range labels {
				if strings.EqualFold(strings.TrimSpace(cell), label) {
					matched = true
				}
			}
		}
		if !matched {
			continue
		}
		if found != nil {
			return nil, false
		}
		found = row
	}
	return found, found != nil
}

// rowContaining finds the one row whose cells carry every marker. Requiring a
// single match is the point: two matching rows means the page grew a section
// this reader cannot tell apart, and guessing between them is how a checker
// starts reporting a rate nobody publishes.
func (t table) rowContaining(markers ...string) ([]string, bool) {
	var found []string
	for _, row := range t {
		joined := strings.ToLower(strings.Join(row, " "))
		matched := true
		for _, marker := range markers {
			if !strings.Contains(joined, strings.ToLower(marker)) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		if found != nil {
			return nil, false
		}
		found = row
	}
	return found, found != nil
}
