package cli

import (
	"flag"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestParseCommandFlagsReportsErrors(t *testing.T) {
	tests := []struct {
		name       string
		newFlagSet func() commandFlagSet
		args       []string
		want       string
	}{
		{
			name: "unknown pflag",
			newFlagSet: func() commandFlagSet {
				return pflag.NewFlagSet("test", pflag.ContinueOnError)
			},
			args: []string{"--unknown"},
			want: "unknown flag: --unknown",
		},
		{
			name: "invalid pflag value",
			newFlagSet: func() commandFlagSet {
				fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
				fs.Int("count", 0, "item count")
				return fs
			},
			args: []string{"--count=invalid"},
			want: "invalid argument \"invalid\" for \"--count\" flag",
		},
		{
			name: "missing pflag value",
			newFlagSet: func() commandFlagSet {
				fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
				fs.String("model", "", "model name")
				return fs
			},
			args: []string{"--model"},
			want: "flag needs an argument: --model",
		},
		{
			name: "unknown standard flag",
			newFlagSet: func() commandFlagSet {
				return flag.NewFlagSet("test", flag.ContinueOnError)
			},
			args: []string{"--unknown"},
			want: "flag provided but not defined: -unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var code int
			var proceed bool
			stderr := captureStderr(t, func() {
				code, proceed = parseCommandFlags(tt.newFlagSet(), tt.args)
			})
			if code != 2 || proceed {
				t.Fatalf("parseCommandFlags(%q) = (%d, %v), want (2, false)", tt.args, code, proceed)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Fatalf("stderr = %q, want %q", stderr, tt.want)
			}
			if strings.Contains(stderr, "Usage of") {
				t.Fatalf("parse error should be concise, got usage in stderr:\n%s", stderr)
			}
		})
	}
}

func TestParseCommandFlagsTreatsHelpAsSuccess(t *testing.T) {
	tests := []struct {
		name       string
		newFlagSet func() commandFlagSet
	}{
		{
			name: "pflag",
			newFlagSet: func() commandFlagSet {
				fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
				fs.String("model", "", "model name")
				return fs
			},
		},
		{
			name: "standard flag",
			newFlagSet: func() commandFlagSet {
				fs := flag.NewFlagSet("test", flag.ContinueOnError)
				fs.String("model", "", "model name")
				return fs
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var code int
			var proceed bool
			stdout, stderr := captureCLIOutput(t, func() {
				code, proceed = parseCommandFlags(tt.newFlagSet(), []string{"--help"})
			})
			if code != 0 || proceed {
				t.Fatalf("parseCommandFlags(--help) = (%d, %v), want (0, false)", code, proceed)
			}
			if !strings.Contains(stdout, "Usage of test:") || !strings.Contains(stdout, "model name") {
				t.Fatalf("help output missing usage:\n%s", stdout)
			}
			if stderr != "" {
				t.Fatalf("help wrote stderr: %q", stderr)
			}
			if strings.Contains(stdout, "Error:") || strings.Contains(stdout, "flag: help requested") {
				t.Fatalf("help should not be reported as an error:\n%s", stdout)
			}
		})
	}
}

func TestParseCommandFlagsSuccessProceedsSilently(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	name := fs.String("name", "", "name")
	var code int
	var proceed bool
	stderr := captureStderr(t, func() {
		code, proceed = parseCommandFlags(fs, []string{"--name", "reasonix"})
	})
	if code != 0 || !proceed || *name != "reasonix" {
		t.Fatalf("parseCommandFlags success = (%d, %v, %q), want (0, true, reasonix)", code, proceed, *name)
	}
	if stderr != "" {
		t.Fatalf("successful parse wrote stderr: %q", stderr)
	}
}
