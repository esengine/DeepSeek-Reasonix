package clipimage

import (
	"context"
	"encoding/base64"
	"strings"
)

// Windows PowerShell 5.1 ships with every Windows and reaches the GUI
// clipboard; PowerShell 7 has no Get-Clipboard -Format Image. The PNG comes
// back as base64 on stdout, and an empty answer means there was no image.
const script = `Add-Type -AssemblyName System.Drawing
$img = Get-Clipboard -Format Image
if ($null -eq $img) { exit 0 }
$ms = New-Object System.IO.MemoryStream
$img.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
[Convert]::ToBase64String($ms.ToArray())`

func read(ctx context.Context) ([]byte, error) {
	out, err := run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return nil, ErrNoImage
	}
	return base64.StdEncoding.DecodeString(text)
}
