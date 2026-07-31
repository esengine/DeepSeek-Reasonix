// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package internal

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/pattern"
)

// ExtendedPatternMatcher returns a [regexp.Regexp.MatchString]-like function
// to support !(pattern-list) extended patterns where possible.
// It can be used instead of [pattern.Regexp] for narrow use cases.
func ExtendedPatternMatcher(pat string, mode pattern.Mode) (func(string) bool, error) {
	if mode&pattern.ExtendedOperators != 0 && mode&pattern.EntireString == 0 {
		// In the future we could try to support !(pattern) without matching
		// the entire input, ensuring we add enough test cases.
		panic("ExtendedOperators is only supported with EntireString")
	}

	// Extended pattern matching operators are always on outside of pathname expansion.
	expr, err := pattern.Regexp(pat, mode)
	if err != nil {
		// Handle !(pattern-list) negation: when Regexp returns NegExtglobError,
		// match the inner pattern and negate the result.
		var negErr *pattern.NegExtGlobError
		if !errors.As(err, &negErr) {
			return nil, err
		}
		return extNegatedMatcher(pat, negErr.Groups)
	}
	rx := regexp.MustCompile(expr)
	return rx.MatchString, nil
}

// extNegatedMatcher handles !(pattern-list) extglob negation.
// Only a single !(...) group with fixed-string prefix and suffix is supported.
func extNegatedMatcher(pat string, groups []pattern.NegExtGlobGroup) (func(string) bool, error) {
	if len(groups) != 1 {
		return nil, fmt.Errorf("multiple extglob !(...) groups are not supported yet")
	}
	g := groups[0]
	prefix := pat[:g.Start]
	suffix := pat[g.End:]

	if pattern.HasMeta(prefix, 0) || pattern.HasMeta(suffix, 0) {
		return nil, fmt.Errorf("extglob !(...) is only supported with a fixed prefix and suffix")
	}

	// Use @(inner) to compile the pattern list, then negate the match.
	inner := pat[g.Start+len("!(") : g.End-len(")")]
	expr, err := pattern.Regexp("@("+inner+")", pattern.EntireString|pattern.ExtendedOperators)
	if err != nil {
		return nil, err
	}
	rx := regexp.MustCompile(expr)

	return func(name string) bool {
		if !strings.HasPrefix(name, prefix) {
			return false
		}
		if !strings.HasSuffix(name, suffix) {
			return false
		}
		end := len(name) - len(suffix)
		if end < len(prefix) {
			return false // prefix and suffix overlap in name
		}
		middle := name[len(prefix):end]

		return !rx.MatchString(middle)
	}, nil
}
