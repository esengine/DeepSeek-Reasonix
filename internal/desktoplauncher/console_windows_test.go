//go:build windows

package desktoplauncher

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"reasonix/internal/installlayout"
)

const consoleTestEnv = "REASONIX_LAUNCHER_CONSOLE_TEST"

//go:embed testdata/console-probe/main.go
var consoleProbeSource string

// TestMain lets a GUI copy of the test executable run the installed launch path.
func TestMain(m *testing.M) {
	root := os.Getenv(consoleTestEnv)
	if root == "" {
		os.Exit(m.Run())
	}
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	name := filepath.Base(exe)
	kernel := syscall.NewLazyDLL("kernel32.dll")
	cp, _, _ := kernel.NewProc("GetConsoleCP").Call()
	window, _, _ := kernel.NewProc("GetConsoleWindow").Call()
	fmt.Printf("console-state %s %d %d\n", name, cp, window)
	switch name {
	case "reasonix-launcher.exe", "Reasonix.exe":
		os.Exit(Run(os.Args[1:], "test"))
	default:
		panic("unexpected console test executable: " + name)
	}
}

func TestLauncherDoesNotCreateConsoleWindow(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executableBytes, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	// Match the shipped -H windowsgui launcher while keeping its children CUI.
	// A CREATE_NO_WINDOW console parent can pass an invisible console onward,
	// masking the missing creation flag in the launcher under test.
	guiBytes := bytes.Clone(executableBytes)
	peOffset := binary.LittleEndian.Uint32(guiBytes[0x3c:0x40])
	binary.LittleEndian.PutUint16(guiBytes[peOffset+24+68:], 2)
	probeBytes := buildConsoleProbe(t)
	for _, entry := range []string{"reasonix-launcher.exe", "Reasonix.exe"} {
		for _, legacy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/legacy=%t", entry, legacy), func(t *testing.T) {
				root := t.TempDir()
				active := filepath.Join(root, "versions", "v1.0.0")
				if err := os.MkdirAll(active, 0o755); err != nil {
					t.Fatal(err)
				}
				paths := []string{filepath.Join(root, entry), filepath.Join(active, "reasonix-desktop.exe")}
				if legacy {
					paths = append(paths, filepath.Join(root, "reasonix-guard.exe"))
				} else if err := writeConsoleTestPointer(root); err != nil {
					t.Fatal(err)
				}
				for _, path := range paths {
					data := probeBytes
					if path == paths[0] {
						data = guiBytes
					}
					if err := os.WriteFile(path, data, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, paths[0])
				cmd.Env = append(os.Environ(), consoleTestEnv+"="+root)
				cmd.WaitDelay = 5 * time.Second
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("launcher: %v\n%s", err, output)
				}
				seen := make(map[string]bool)
				for line := range bytes.SplitSeq(output, []byte{'\n'}) {
					if !bytes.HasPrefix(line, []byte("console-state ")) {
						continue
					}
					var name string
					var cp, window uint64
					if _, err := fmt.Sscanf(string(line), "console-state %s %d %d", &name, &cp, &window); err != nil {
						t.Fatal(err)
					}
					seen[name] = true
					// CREATE_NO_WINDOW still permits a console code page; the
					// regression is creating a window, not having console I/O.
					if window != 0 {
						t.Errorf("%s created a console window: codepage=%d window=%d", name, cp, window)
					}
				}
				for _, path := range paths {
					if !seen[filepath.Base(path)] {
						t.Errorf("missing process probe for %s: %s", filepath.Base(path), output)
					}
				}
				if legacy {
					if _, err := os.Stat(filepath.Join(root, "reasonix-guard.exe")); !os.IsNotExist(err) {
						t.Errorf("completed legacy migrator was not removed: %v", err)
					}
				}
				if strings.Contains(string(output), "error:") {
					t.Fatalf("launcher reported an error: %s", output)
				}
			})
		}
	}
}

func writeConsoleTestPointer(root string) error {
	return installlayout.WriteCurrent(root, installlayout.CurrentPointer{
		SchemaVersion: 1, ActiveVersion: "v1.0.0", ActiveDir: "versions/v1.0.0",
	})
}

func buildConsoleProbe(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(consoleProbeSource), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", "probe.exe", "main.go")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0", "GO111MODULE=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build console probe: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(dir, "probe.exe"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
