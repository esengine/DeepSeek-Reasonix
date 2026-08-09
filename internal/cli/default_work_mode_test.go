package cli

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"reasonix/internal/config"
)

// writeDefaultWorkModeConfig writes a user config.toml that sets the top-level
// default_work_mode key inside the isolated config home.
func writeDefaultWorkModeConfig(t *testing.T, value string) {
	t.Helper()
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "config_version = 2\n"
	if value != "" {
		body += "default_work_mode = " + quoteTOML(value) + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func quoteTOML(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func TestResolveRuntimeProfileUsesConfigDefault(t *testing.T) {
	isolateCLIConfigHome(t)
	writeDefaultWorkModeConfig(t, "delivery")

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	profileFlag := fs.String("profile", "balanced", "runtime profile")

	got, err := resolveRuntimeProfile(fs.Changed("profile"), profileFlag)
	if err != nil {
		t.Fatalf("resolveRuntimeProfile: %v", err)
	}
	if got != "delivery" {
		t.Fatalf("profile = %q, want delivery from default_work_mode", got)
	}
}

func TestResolveRuntimeProfileDefaultsBalancedWithoutConfig(t *testing.T) {
	isolateCLIConfigHome(t)

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	profileFlag := fs.String("profile", "balanced", "runtime profile")

	got, err := resolveRuntimeProfile(fs.Changed("profile"), profileFlag)
	if err != nil {
		t.Fatalf("resolveRuntimeProfile: %v", err)
	}
	if got != "full" {
		t.Fatalf("profile = %q, want full (balanced default)", got)
	}
}

func TestResolveRuntimeProfileExplicitFlagWinsOverConfig(t *testing.T) {
	isolateCLIConfigHome(t)
	writeDefaultWorkModeConfig(t, "delivery")

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	profileFlag := fs.String("profile", "balanced", "runtime profile")
	if err := fs.Set("profile", "balanced"); err != nil {
		t.Fatal(err)
	}

	got, err := resolveRuntimeProfile(fs.Changed("profile"), profileFlag)
	if err != nil {
		t.Fatalf("resolveRuntimeProfile: %v", err)
	}
	if got != "full" {
		t.Fatalf("profile = %q, want full: explicit --profile must override default_work_mode", got)
	}
}

func TestResolveRuntimeProfileStdlibFlagSet(t *testing.T) {
	// runServe and acpCommand register --profile on the stdlib flag set; the
	// same precedence must apply there.
	isolateCLIConfigHome(t)
	writeDefaultWorkModeConfig(t, "economy")

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	profileFlag := fs.String("profile", "balanced", "runtime profile")

	got, err := resolveRuntimeProfile(stdFlagChanged(fs, "profile"), profileFlag)
	if err != nil {
		t.Fatalf("resolveRuntimeProfile: %v", err)
	}
	if got != "economy" {
		t.Fatalf("profile = %q, want economy from default_work_mode", got)
	}
}

func TestResolveRuntimeProfileInvalidConfigValueFails(t *testing.T) {
	isolateCLIConfigHome(t)
	writeDefaultWorkModeConfig(t, "bogus")

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	profileFlag := fs.String("profile", "balanced", "runtime profile")

	if _, err := resolveRuntimeProfile(fs.Changed("profile"), profileFlag); err == nil {
		t.Fatal("resolveRuntimeProfile succeeded with invalid default_work_mode, want error")
	}
}

func TestResolveRuntimeProfileReadsProjectConfigAfterChdir(t *testing.T) {
	// runAgent/chatREPL resolve the profile after chdirTo, so a project-level
	// reasonix.toml (as --dir would land in) must feed default_work_mode.
	isolateCLIConfigHome(t)
	cwd := mustGetwd(t)
	if err := os.WriteFile(filepath.Join(cwd, "reasonix.toml"), []byte("default_work_mode = \"delivery\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	profileFlag := fs.String("profile", "balanced", "runtime profile")

	got, err := resolveRuntimeProfile(fs.Changed("profile"), profileFlag)
	if err != nil {
		t.Fatalf("resolveRuntimeProfile: %v", err)
	}
	if got != "delivery" {
		t.Fatalf("profile = %q, want delivery from project reasonix.toml", got)
	}
}

func TestResolveRuntimeProfileInvalidFlagValueStillFails(t *testing.T) {
	isolateCLIConfigHome(t)
	writeDefaultWorkModeConfig(t, "delivery")

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	profileFlag := fs.String("profile", "balanced", "runtime profile")
	if err := fs.Set("profile", "bogus"); err != nil {
		t.Fatal(err)
	}

	if _, err := resolveRuntimeProfile(fs.Changed("profile"), profileFlag); err == nil {
		t.Fatal("resolveRuntimeProfile succeeded with invalid --profile, want error")
	}
}
