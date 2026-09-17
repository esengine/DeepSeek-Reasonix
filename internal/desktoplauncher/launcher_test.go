package desktoplauncher

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/installlayout"
)

func TestStripLegacyLaunchArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "plain args passthrough",
			args: []string{"--verbose", "value"},
			want: []string{"--verbose", "value"},
		},
		{
			name: "empty input",
			args: nil,
			want: []string{},
		},
		{
			name: "launch token stripped",
			args: []string{"launch", "reasonix-desktop"},
			want: []string{"reasonix-desktop"},
		},
		{
			name: "detach token stripped",
			args: []string{"--detach", "reasonix-desktop"},
			want: []string{"reasonix-desktop"},
		},
		{
			name: "safe mode token stripped",
			args: []string{"--safe-mode"},
			want: []string{},
		},
		{
			name: "short safe mode token stripped",
			args: []string{"-safe-mode", "reasonix-desktop"},
			want: []string{"reasonix-desktop"},
		},
		{
			name: "app with separate value stripped",
			args: []string{"--app", "1.2.3"},
			want: []string{},
		},
		{
			name: "app with equals value stripped",
			args: []string{"--app=1.2.3"},
			want: []string{},
		},
		{
			name: "app with empty equals value stripped",
			args: []string{"--app="},
			want: []string{},
		},
		{
			name: "app as last arg stripped",
			args: []string{"--app"},
			want: []string{},
		},
		{
			name: "app value not mistaken for standalone arg",
			args: []string{"--app", "--detach"},
			want: []string{},
		},
		{
			name: "combined legacy tokens",
			args: []string{"launch", "--detach", "--safe-mode", "reasonix-desktop", "--app", "9.9.9"},
			want: []string{"reasonix-desktop"},
		},
		{
			name: "legacy tokens removed between kept args",
			args: []string{"--verbose", "launch", "x"},
			want: []string{"--verbose", "x"},
		},
		{
			name: "double dash preserves everything after",
			args: []string{"--detach", "--", "launch", "--app", "1.2.3"},
			want: []string{"--", "launch", "--app", "1.2.3"},
		},
		{
			name: "double dash first preserves whole input",
			args: []string{"--", "--safe-mode"},
			want: []string{"--", "--safe-mode"},
		},
		{
			name: "app value after double dash preserved",
			args: []string{"--app", "1.2.3", "--", "--app", "4.5.6"},
			want: []string{"--", "--app", "4.5.6"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripLegacyLaunchArgs(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("StripLegacyLaunchArgs(%q) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestSiblingDesktop(t *testing.T) {
	desktopName := installlayout.DesktopBinaryName()

	t.Run("regular file found", func(t *testing.T) {
		root := t.TempDir()
		desktop := filepath.Join(root, desktopName)
		if err := os.WriteFile(desktop, []byte("desktop"), 0o755); err != nil {
			t.Fatal(err)
		}
		got := siblingDesktop(root)
		if got != desktop {
			t.Fatalf("siblingDesktop() = %q, want %q", got, desktop)
		}
	})

	t.Run("missing desktop", func(t *testing.T) {
		got := siblingDesktop(t.TempDir())
		if got != "" {
			t.Fatalf("siblingDesktop() = %q, want empty", got)
		}
	})

	t.Run("desktop is a directory", func(t *testing.T) {
		root := t.TempDir()
		desktop := filepath.Join(root, desktopName)
		if err := os.Mkdir(desktop, 0o755); err != nil {
			t.Fatal(err)
		}
		got := siblingDesktop(root)
		if got != "" {
			t.Fatalf("siblingDesktop() = %q, want empty", got)
		}
	})

	t.Run("desktop is a symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink privilege varies on Windows CI")
		}
		root := t.TempDir()
		target := filepath.Join(t.TempDir(), desktopName)
		if err := os.WriteFile(target, []byte("desktop"), 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, desktopName)
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		got := siblingDesktop(root)
		if got != "" {
			t.Fatalf("siblingDesktop() = %q, want empty for symlink", got)
		}
	})
}

func TestResolveDesktopPath(t *testing.T) {
	desktopName := installlayout.DesktopBinaryName()

	t.Run("flat sibling fallback without current.json", func(t *testing.T) {
		root := t.TempDir()
		desktop := filepath.Join(root, desktopName)
		if err := os.WriteFile(desktop, []byte("desktop"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := ResolveDesktopPath(root)
		if err != nil {
			t.Fatal(err)
		}
		if got != desktop {
			t.Fatalf("ResolveDesktopPath() = %q, want %q", got, desktop)
		}
	})

	t.Run("no current.json and no sibling is an error", func(t *testing.T) {
		_, err := ResolveDesktopPath(t.TempDir())
		if err == nil {
			t.Fatal("ResolveDesktopPath() succeeded, want error")
		}
		if !strings.Contains(err.Error(), "missing current.json") {
			t.Fatalf("ResolveDesktopPath() error = %q, want mention of missing current.json", err)
		}
	})

	t.Run("current.json pointer wins over sibling", func(t *testing.T) {
		root := t.TempDir()
		// A flat sibling must be ignored once current.json exists.
		if err := os.WriteFile(filepath.Join(root, desktopName), []byte("flat"), 0o755); err != nil {
			t.Fatal(err)
		}
		version := "v1.20.0"
		dir := filepath.Join(root, "versions", version)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, desktopName), []byte("active"), 0o755); err != nil {
			t.Fatal(err)
		}
		ptr := installlayout.CurrentPointer{SchemaVersion: 1, ActiveVersion: version, ActiveDir: installlayout.VersionDirRelative(version)}
		if err := installlayout.WriteCurrent(root, ptr); err != nil {
			t.Fatal(err)
		}
		got, err := ResolveDesktopPath(root)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(dir, desktopName)
		if got != want {
			t.Fatalf("ResolveDesktopPath() = %q, want %q", got, want)
		}
	})
}
