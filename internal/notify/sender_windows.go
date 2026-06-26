//go:build windows

package notify

import (
	"log"

	"git.sr.ht/~jackmordaunt/go-toast/v2"
)

// PlatformSender delivers notifications through the Windows Toast API.
type PlatformSender struct{}

// NewPlatformSender returns the best-effort sender for the current platform.
// It also registers app metadata in the Windows Registry on first call.
func NewPlatformSender() PlatformSender {
	if err := toast.SetAppData(toast.AppData{
		AppID: "Reasonix",
	}); err != nil {
		log.Printf("[notify] SetAppData: %v", err)
	}
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
