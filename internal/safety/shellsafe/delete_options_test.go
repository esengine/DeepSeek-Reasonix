package shellsafe

import (
	"reflect"
	"testing"
)

func TestPowerShellDeleteOptionConsumption(t *testing.T) {
	for _, option := range []string{"-ErrorAction", "-ea", "-WarningAction", "-wa", "-InformationAction", "-Confirm"} {
		for _, tt := range []struct {
			suffix   string
			rest     []string
			consumed int
			unknown  bool
		}{
			{"", []string{"Stop", "tree"}, 1, false},
			{":Stop", []string{"tree"}, 0, false},
			{"", nil, 0, true},
			{"", []string{"-Recurse"}, 0, true},
		} {
			t.Run(option+tt.suffix+joinDeleteArgs(tt.rest), func(t *testing.T) {
				var extent DeleteExtent
				got := powerShellDeleteOption(&extent, option+tt.suffix, tt.rest)
				want := DeleteExtent{UnknownOption: tt.unknown}
				if got != tt.consumed || !reflect.DeepEqual(extent, want) {
					t.Fatalf("consumed %d, extent %#v; want %d, %#v", got, extent, tt.consumed, want)
				}
				args := append([]string{"-Recurse", option + tt.suffix}, tt.rest...)
				want = DeleteExtent{Targets: []string{""}, Recursive: true, UnknownOption: tt.unknown}
				if !tt.unknown {
					want.Targets = []string{"tree"}
				}
				if got := AnalyzeRecursiveDelete("Remove-Item", args, true); !reflect.DeepEqual(got, want) {
					t.Fatalf("got %#v, want %#v", got, want)
				}
			})
		}
	}
	for _, option := range []string{"-Verbose", "-Debug", "-WhatIf", "-Force", "-f", "-LiteralPath", "-Path"} {
		t.Run(option, func(t *testing.T) {
			want := DeleteExtent{Targets: []string{"tree"}, Recursive: true}
			if got := AnalyzeRecursiveDelete("ri", []string{"-Recurse", option, "tree"}, true); !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
	for _, option := range []string{"-Unknown", "-", "-recursex"} {
		t.Run(option, func(t *testing.T) {
			want := DeleteExtent{Targets: []string{"tree"}, Recursive: true, UnknownOption: true}
			if got := AnalyzeRecursiveDelete("ri", []string{"-r", option, "tree"}, true); !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
}
