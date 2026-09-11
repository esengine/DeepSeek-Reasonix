//go:build windows

package notify

import (
	"path/filepath"

	"git.sr.ht/~jackmordaunt/go-toast/v2"
	"golang.org/x/sys/windows/registry"

	"reasonix/internal/appidentity"
)

// PlatformSender delivers notifications through the Windows Toast API.
type PlatformSender struct{}

// NewPlatformSender returns the best-effort sender for the current platform.
func NewPlatformSender() PlatformSender {
	_ = registerDesktopNotifications(toast.SetAppData, setNotificationDisplayName)
	return PlatformSender{}
}

func (PlatformSender) Send(m Message) error {
	notification := desktopNotification(m)
	return notification.Push()
}

func desktopNotification(m Message) toast.Notification {
	return toast.Notification{
		AppID: appidentity.AppUserModelID,
		Title: m.Title,
		Body:  m.Body,
	}
}

func registerDesktopNotifications(register func(toast.AppData) error, displayName func(string, string) error) error {
	if err := register(toast.AppData{AppID: appidentity.AppUserModelID}); err != nil {
		return err
	}
	return displayName(appidentity.AppUserModelID, appidentity.DisplayName)
}

// go-toast defaults DisplayName to the ID. Change only our new registration;
// the old "Reasonix" registration still belongs to installed Studio releases.
func setNotificationDisplayName(id, name string) error {
	path := filepath.Join("Software", "Classes", "AppUserModelId", id)
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue("DisplayName", name)
}
