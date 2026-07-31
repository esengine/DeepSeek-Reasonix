// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package json

import (
	"strings"
	"testing"
)

func TestScan(t *testing.T) {
	tCases := []struct {
		data   string
		length int
		ok     bool
	}{
		{`foo`, 2, false},
		{`}{`, 1, false},
		{`{]`, 2, false},
		{`{}`, 2, true},
		{`[]`, 2, true},
		{`"foo"`, 5, true},
		{`120`, 3, true},
		{`1e20`, 4, true},
		{`0.120`, 5, true},
		{`null`, 4, true},
		{`{"foo":"bar"}`, 13, true},
		{`{"foo":"21\t\u0009 \u1234","bar":{"baz":["qux"]}`, 48, false},
		{`{"foo":"bar","bar":{"baz":["qux"]}}`, 35, true},
		{`{"foo":-1,"bar":{"baz":[true, false, null, 100, 0.123]}}`, 56, true},
		{`{"foo":-1,"bar":{"baz":[tru]}}`, 28, false},
		{`{"foo":-1,"bar":{"baz":[nul]}}`, 28, false},
		{`{"foo":-1,"bar":{"baz":[314e+1]}}`, 33, true},
	}
	for _, st := range tCases {
		scanned, err := Scan([]byte(st.data))
		if scanned != st.length {
			t.Errorf("Scan length error: expected: %d; got: %d; input: %s",
				st.length, scanned, st.data)
		}

		if err != nil && st.ok {
			t.Errorf("Scan failed with err: %s; input: %s", err, st.data)
		}

		if err == nil && !st.ok {
			t.Errorf("Scan should fail for input: %s", st.data)
		}
	}
}

func TestScannerMaxDepth(t *testing.T) {
	tCases := []struct {
		name        string
		data        string
		errMaxDepth bool
	}{
		{
			name:        "ArrayUnderMaxNestingDepth",
			data:        `{"a":` + strings.Repeat(`[`, 10000-1) + strings.Repeat(`]`, 10000-1) + `}`,
			errMaxDepth: false,
		},
		{
			name:        "ArrayOverMaxNestingDepth",
			data:        `{"a":` + strings.Repeat(`[`, 10000) + strings.Repeat(`]`, 10000) + `}`,
			errMaxDepth: true,
		},
		{
			name:        "ObjectUnderMaxNestingDepth",
			data:        `{"a":` + strings.Repeat(`{"a":`, 10000-1) + `0` + strings.Repeat(`}`, 10000-1) + `}`,
			errMaxDepth: false,
		},
		{
			name:        "ObjectOverMaxNestingDepth",
			data:        `{"a":` + strings.Repeat(`{"a":`, 10000) + `0` + strings.Repeat(`}`, 10000) + `}`,
			errMaxDepth: true,
		},
	}

	for _, tt := range tCases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Scan([]byte(tt.data))
			if !tt.errMaxDepth {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing 'exceeded max depth', got none")
				} else if !strings.Contains(err.Error(), "exceeded max depth") {
					t.Errorf("expected error containing 'exceeded max depth', got: %v", err)
				}
			}
		})
	}
}
