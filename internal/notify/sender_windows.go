//go:build windows

package notify

import "os/exec"

// PlatformSender delivers notifications through the host OS.
type PlatformSender struct{}

// NewPlatformSender returns the best-effort sender for the current platform.
func NewPlatformSender() PlatformSender { return PlatformSender{} }

func (PlatformSender) Send(m Message) error {
	cmd := exec.Command("powershell", "-NoProfile", "-Command", `
if (Get-Command New-BurntToastNotification -ErrorAction SilentlyContinue) {
  New-BurntToastNotification -Text $args[0], $args[1]
} elseif (Get-Command msg -ErrorAction SilentlyContinue) {
  msg $env:USERNAME ($args[0] + ': ' + $args[1])
}
`, m.Title, m.Body)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
