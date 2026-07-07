//go:build windows

package notify

import "git.sr.ht/~jackmordaunt/go-toast/v2"

// PlatformSender delivers notifications through the Windows Toast API.
type PlatformSender struct{}

// NewPlatformSender returns the best-effort sender for the current platform.
func NewPlatformSender() PlatformSender {
	_ = toast.SetAppData(toast.AppData{AppID: "Reasonix"})
	return PlatformSender{}
}

func (PlatformSender) Send(m Message) error {
	notification := toast.Notification{
		AppID: "Reasonix",
		Title: m.Title,
		Body:  m.Body,
	}
	return notification.Push()
}
