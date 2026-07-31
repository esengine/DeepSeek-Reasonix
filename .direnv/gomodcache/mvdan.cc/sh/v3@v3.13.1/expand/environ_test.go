// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"reflect"
	"testing"
)

func TestListEnviron(t *testing.T) {
	tests := []struct {
		name        string
		insensitive bool
		pairs       []string
		want        []string
	}{
		{
			name:  "Empty",
			pairs: nil,
			want:  nil,
		},
		{
			name:  "Simple",
			pairs: []string{"A=b", "c="},
			want:  []string{"A=b", "c="},
		},
		{
			name:  "MissingEqual",
			pairs: []string{"A=b", "invalid", "c="},
			want:  []string{"A=b", "c="},
		},
		{
			name:  "DuplicateNames",
			pairs: []string{"A=x", "A=b", "c=", "c=y"},
			want:  []string{"A=b", "c=y"},
		},
		{
			name:  "NoName",
			pairs: []string{"=b", "=c"},
			want:  []string{},
		},
		{
			name:  "EmptyElements",
			pairs: []string{"A=b", "", "", "c="},
			want:  []string{"A=b", "c="},
		},
		{
			name:  "MixedCaseNoInsensitive",
			pairs: []string{"A=b1", "Path=foo", "a=b2"},
			want:  []string{"A=b1", "Path=foo", "a=b2"},
		},
		{
			name:        "MixedCaseInsensitive",
			insensitive: true,
			pairs:       []string{"A=b1", "Path=foo", "a=b2"},
			want:        []string{"a=b2", "Path=foo"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotEnv := listEnviron_(tc.insensitive, tc.pairs...)
			got := gotEnv.(listEnviron).pairs
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ListEnviron(%t, %q) wanted %#v, got %#v",
					tc.insensitive, tc.pairs, tc.want, got)
			}
		})
	}
}

func TestGetWithSameSubPrefix(t *testing.T) {
	gotEnv := ListEnviron("GREETING=text1", "GREETING2=text2")
	got := gotEnv.Get("GREETING2").String()
	if got != "text2" {
		t.Fatalf("ListEnviron.Get(GREETING2) wanted text2, got %q", got)
	}
	got = gotEnv.Get("GREETING").String()
	if got != "text1" {
		t.Fatalf("ListEnviron.Get(GREETING) wanted text1, got %q", got)
	}
}
