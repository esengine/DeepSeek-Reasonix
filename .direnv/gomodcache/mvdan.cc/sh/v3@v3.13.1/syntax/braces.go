// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"slices"
	"strconv"
	"strings"
)

var (
	litLeftBrace  = &Lit{Value: "{"}
	litComma      = &Lit{Value: ","}
	litDots       = &Lit{Value: ".."}
	litRightBrace = &Lit{Value: "}"}
)

// SplitBraces parses brace expansions within a word's literal parts.
// If any valid brace expansions are found, they are replaced with BraceExp nodes,
// and the function returns true.
// Otherwise, the word is left untouched and the function returns false.
//
// For example, a literal word "foo{bar,baz}" will result in a word containing
// the literal "foo", and a brace expansion with the elements "bar" and "baz".
//
// It does not return an error; malformed brace expansions are simply skipped.
// For example, the literal word "a{b" is left unchanged.
func SplitBraces(word *Word) bool {
	if !slices.ContainsFunc(word.Parts, func(part WordPart) bool {
		lit, ok := part.(*Lit)
		return ok && strings.Contains(lit.Value, "{")
	}) {
		// In the common case where a word has no braces, skip any allocs.
		return false
	}
	top := &Word{}
	acc := top
	var cur *BraceExp
	open := []*BraceExp{}

	pop := func() *BraceExp {
		old := cur
		open = open[:len(open)-1]
		if len(open) == 0 {
			cur = nil
			acc = top
		} else {
			cur = open[len(open)-1]
			acc = cur.Elems[len(cur.Elems)-1]
		}
		return old
	}
	addLit := func(lit *Lit) {
		acc.Parts = append(acc.Parts, lit)
	}

	for _, wp := range word.Parts {
		lit, ok := wp.(*Lit)
		if !ok {
			acc.Parts = append(acc.Parts, wp)
			continue
		}
		last := 0
		for j := 0; j < len(lit.Value); j++ {
			addlitidx := func() {
				if last == j {
					return // empty lit
				}
				l2 := *lit
				l2.Value = l2.Value[last:j]
				addLit(&l2)
			}
			switch lit.Value[j] {
			case '{':
				addlitidx()
				acc = &Word{}
				cur = &BraceExp{Elems: []*Word{acc}}
				open = append(open, cur)
			case ',':
				if cur == nil {
					continue
				}
				addlitidx()
				acc = &Word{}
				cur.Elems = append(cur.Elems, acc)
			case '.':
				if cur == nil {
					continue
				}
				if j+1 >= len(lit.Value) || lit.Value[j+1] != '.' {
					continue
				}
				addlitidx()
				cur.Sequence = true
				acc = &Word{}
				cur.Elems = append(cur.Elems, acc)
				j++
			case '}':
				if cur == nil {
					continue
				}
				addlitidx()
				br := pop()
				if len(br.Elems) == 1 {
					// return {x} to a non-brace
					addLit(litLeftBrace)
					acc.Parts = append(acc.Parts, br.Elems[0].Parts...)
					addLit(litRightBrace)
					break
				}
				if !br.Sequence {
					acc.Parts = append(acc.Parts, br)
					break
				}
				var chars [2]bool
				broken := false
				for i, elem := range br.Elems[:2] {
					val := elem.Lit()
					if _, err := strconv.Atoi(val); err == nil {
					} else if len(val) == 1 && asciiLetter(val[0]) {
						chars[i] = true
					} else {
						broken = true
					}
				}
				if len(br.Elems) == 3 {
					// increment must be a number
					val := br.Elems[2].Lit()
					if _, err := strconv.Atoi(val); err != nil {
						broken = true
					}
				}
				// are start and end both chars or
				// non-chars?
				if chars[0] != chars[1] {
					broken = true
				}
				if !broken {
					acc.Parts = append(acc.Parts, br)
					break
				}
				// return broken {x..y[..incr]} to a non-brace
				addLit(litLeftBrace)
				for i, elem := range br.Elems {
					if i > 0 {
						addLit(litDots)
					}
					acc.Parts = append(acc.Parts, elem.Parts...)
				}
				addLit(litRightBrace)
			default:
				continue
			}
			last = j + 1
		}
		if last == 0 {
			addLit(lit)
		} else {
			left := *lit
			left.Value = left.Value[last:]
			addLit(&left)
		}
	}
	// open braces that were never closed fall back to non-braces
	for acc != top {
		br := pop()
		addLit(litLeftBrace)
		for i, elem := range br.Elems {
			if i > 0 {
				if br.Sequence {
					addLit(litDots)
				} else {
					addLit(litComma)
				}
			}
			acc.Parts = append(acc.Parts, elem.Parts...)
		}
	}
	*word = *top
	return true
}
