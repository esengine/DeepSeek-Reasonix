package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Every spelling below names the real System32 stub and the real Store alias;
// none may be told apart from them by how the path is written.
func TestWSLLauncherPathSurvivesAlternateSpellings(t *testing.T) {
	win := os.Getenv("SystemRoot")
	stub := filepath.Join(win, "System32", "bash.exe")
	if _, err := os.Stat(stub); err != nil {
		t.Skip("no WSL launcher on this machine")
	}
	vol := filepath.VolumeName(win)
	rest := strings.TrimPrefix(win, vol)
	spellings := map[string]string{
		"trailing dot": filepath.Join(vol+`\`, strings.TrimPrefix(rest, `\`)+".", "System32", "bash.exe"),
		"extended":     `\\?\` + stub,
		"admin share":  `\\localhost\` + strings.TrimSuffix(vol, ":") + `$` + rest + `\System32\bash.exe`,
		"lower case":   strings.ToLower(stub),
		"dot segments": filepath.Join(win, "System32", "..", "System32", "bash.exe"),
	}
	for name, p := range spellings {
		if !isWSLLauncherPath(p, win) {
			t.Errorf("%s: %q not judged WSL", name, p)
		}
	}
	alias := filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WindowsApps", "bash.exe")
	if _, err := os.Lstat(alias); err != nil {
		return
	}
	aliasPtr, err := syscall.UTF16PtrFromString(alias)
	if err != nil {
		t.Fatal(err)
	}
	short := make([]uint16, 512)
	n, _ := syscall.GetShortPathName(aliasPtr, &short[0], uint32(len(short)))
	for name, p := range map[string]string{
		"alias":        alias,
		"alias short":  syscall.UTF16ToString(short[:n]),
		"alias dotted": strings.Replace(alias, "WindowsApps", "WindowsApps.", 1),
	} {
		if p != "" && !isWSLLauncherPath(p, win) {
			t.Errorf("%s: %q not judged WSL", name, p)
		}
	}
}

func TestWSLLauncherPathFollowsJunction(t *testing.T) {
	win, _ := wslFixture(t)
	junction := filepath.Join(t.TempDir(), "j")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, filepath.Join(win, "System32")).CombinedOutput(); err != nil {
		t.Skip("cannot create a junction:", string(out))
	}
	if !isWSLLauncherPath(filepath.Join(junction, "bash.exe"), win) {
		t.Error("bash.exe through a junction to System32 not judged WSL")
	}
}
