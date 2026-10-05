// Package clipimage reads an image off the system clipboard of the machine
// the terminal runs on, through the tool each platform ships: osascript on
// macOS, Windows PowerShell on Windows, wl-paste or xclip on Linux.
package clipimage

import (
	"context"
	"errors"
	"os/exec"
	"time"

	"reasonix/internal/base/proc"
	"reasonix/internal/base/secrets"
)

// ErrNoImage means the clipboard holds nothing this package can read as an
// image; a paste that wanted one falls back to text.
var ErrNoImage = errors.New("clipboard does not contain an image")

const readTimeout = 10 * time.Second

// Read returns the clipboard's image bytes.
func Read(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	return read(ctx)
}

// run starts a clipboard tool without the credentials Reasonix loaded, and
// without a console window on Windows.
func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = secrets.ProcessEnv()
	proc.HideWindow(cmd)
	return cmd.Output()
}
