package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"

	"reasonix/internal/proc"
)

// wslClipboardWriteScript reads stdin as UTF-8 and lets Windows PowerShell
// perform the UTF-16 clipboard write. Passing text through clip.exe instead
// makes the Windows program decode the raw UTF-8 bytes using the active OEM
// code page (CP936 on Chinese systems), producing mojibake such as 中文 -> 涓枃.
const wslClipboardWriteScript = `$stdin = [Console]::OpenStandardInput()
$reader = [IO.StreamReader]::new($stdin, [Text.UTF8Encoding]::new($false))
Set-Clipboard -Value $reader.ReadToEnd()`

// writeClipboardText uses the normal platform clipboard outside WSL. WSL is a
// Linux process: preserve its clipboard utilities, but replace the clip.exe
// fallback that would otherwise decode UTF-8 using a Windows code page.
func writeClipboardText(text string) error {
	if isWSL() {
		return writeWSLClipboardText(text)
	}
	return clipboard.WriteAll(text)
}

func isWSL() bool {
	return isWSLFor(runtime.GOOS, os.Getenv)
}

func isWSLFor(goos string, getenv func(string) string) bool {
	if goos != "linux" {
		return false
	}
	return getenv("WSL_DISTRO_NAME") != "" || getenv("WSL_INTEROP") != ""
}

func writeWSLClipboardText(text string) error {
	cmd := newWSLClipboardCommand(text)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return fmt.Errorf("write WSL clipboard: %w: %s", err, detail)
		}
		return fmt.Errorf("write WSL clipboard: %w", err)
	}
	return nil
}

func newWSLClipboardCommand(text string) *exec.Cmd {
	args := []string{
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		wslClipboardWriteScript,
	}
	// Keep native Linux clipboard support when Windows interop is unavailable.
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "" && clipboardCommandAvailable("wl-copy"):
		args = []string{"wl-copy"}
	case clipboardCommandAvailable("xclip"):
		args = []string{"xclip", "-in", "-selection", "clipboard"}
	case clipboardCommandAvailable("xsel"):
		args = []string{"xsel", "--input", "--clipboard"}
	}
	cmd := proc.Command(args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(text)
	return cmd
}

func clipboardCommandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
