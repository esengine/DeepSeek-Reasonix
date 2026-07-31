// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell

import (
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func strEnviron(pairs ...string) func(string) string {
	return func(name string) string {
		prefix := name + "="
		for _, pair := range pairs {
			if val, ok := strings.CutPrefix(pair, prefix); ok {
				return val
			}
		}
		return ""
	}
}

var expandTests = []struct {
	in   string
	env  func(name string) string
	want string
}{
	{"foo", nil, "foo"},
	{"\nfoo\n", nil, "\nfoo\n"},
	{"a-$b-c", nil, "a--c"},
	{"${INTERP_GLOBAL:+hasOsEnv}", nil, "hasOsEnv"},
	{"a-$b-c", strEnviron(), "a--c"},
	{"a-$b-c", strEnviron("b=b_val"), "a-b_val-c"},
	{"${x//o/a}", strEnviron("x=foo"), "faa"},
	{"*.go", nil, "*.go"},
	{"~", nil, "~"},
}

func TestExpand(t *testing.T) {
	os.Setenv("INTERP_GLOBAL", "value")
	for _, tc := range expandTests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			got, err := Expand(tc.in, tc.env)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("\nwant: %q\ngot:  %q", tc.want, got)
			}
		})
	}
}

func TestUnexpectedCmdSubst(t *testing.T) {
	t.Parallel()
	want := "unexpected command substitution at 1:6"
	for _, fn := range []func() error{
		func() error {
			_, err := Expand("echo $(uname -a)", nil)
			return err
		},
		func() error {
			_, err := Fields("echo $(uname -a)", nil)
			return err
		},
	} {
		got := fmt.Sprint(fn())
		if !strings.Contains(got, want) {
			t.Fatalf("wanted error %q, got: %s", want, got)
		}
	}
}

var fieldsTests = []struct {
	in   string
	env  func(name string) string
	want []string
}{
	{"foo", nil, []string{"foo"}},
	{"\nfoo\n", nil, []string{"foo"}},
	{"foo bar", nil, []string{"foo", "bar"}},
	{"foo 'bar baz'", nil, []string{"foo", "bar baz"}},
	{"$x", strEnviron("x=foo bar"), []string{"foo", "bar"}},
	{`"$x"`, strEnviron("x=foo bar"), []string{"foo bar"}},
	{"~", strEnviron("HOME=/my/home"), []string{"/my/home"}},
	{"~/foo/bar", strEnviron("HOME=/my/home"), []string{"/my/home/foo/bar"}},
	{"~foo/file", strEnviron("HOME foo=/bar"), []string{"/bar/file"}},
	{"*.go", nil, []string{"*.go"}},

	{"~", func(name string) string {
		switch runtime.GOOS {
		case "windows":
			if name == "USERPROFILE" {
				return "/my/home"
			}
		default:
			if name == "HOME" {
				return "/my/home"
			}
		}
		return ""
	}, []string{"/my/home"}},
}

func TestFields(t *testing.T) {
	os.Setenv("INTERP_GLOBAL", "value")
	for _, tc := range fieldsTests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			got, err := Fields(tc.in, tc.env)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("\nwant: %q\ngot:  %q", tc.want, got)
			}
		})
	}
}
