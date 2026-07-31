// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore

package main

// This file contains code that is common between the generation code and the
// package's test code.

import (
	"log"

	"golang.org/x/text/internal/ucd"
)

func catFromEntry(p *ucd.Parser) (cat category) {
	// Note: As of Unicode 16, IdnaMappingTable.txt no longer includes
	// disallowed_STD3_valid and disallowed_STD3_mapped.
	// It is up to us to bring them back with our definition of disallowedSTD3,
	// which is exactly the runes from Unicode 15.
	r := p.Rune(0)
	idna2008status := p.String(3)
	switch s := p.String(1); s {
	case "valid":
		cat = valid
	case "disallowed":
		cat = disallowed
	case "disallowed_STD3_valid":
		cat = disallowedSTD3Valid
	case "disallowed_STD3_mapped":
		cat = disallowedSTD3Mapped
	case "mapped":
		cat = mapped
	case "deviation":
		cat = deviation
	case "ignored":
		cat = ignored
	default:
		log.Fatalf("%U: Unknown category %q", r, s)
	}
	if s := idna2008status; s != "" {
		if cat != valid {
			log.Fatalf(`%U: %s defined for %q/%v; want "valid"`, r, s, p.String(1), cat)
		}
		switch s {
		case "NV8":
			cat = validNV8
		case "XV8":
			cat = validXV8
		default:
			log.Fatalf("%U: Unexpected exception %q", r, s)
		}
	}
	return cat
}

var joinType = map[string]info{
	"L": joiningL,
	"D": joiningD,
	"T": joiningT,
	"R": joiningR,
}
