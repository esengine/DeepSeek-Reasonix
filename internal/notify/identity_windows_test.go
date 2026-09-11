//go:build windows

package notify

import (
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"

	"git.sr.ht/~jackmordaunt/go-toast/v2"
	"golang.org/x/sys/windows/registry"
)

func TestWindowsNotificationsKeepDesktopIdentityAndReadableName(t *testing.T) {
	registered := false
	err := registerDesktopNotifications(func(data toast.AppData) error {
		if data.AppID != "io.reasonix.desktop" {
			t.Fatalf("registered ID = %q", data.AppID)
		}
		registered = true
		return nil
	}, func(id, name string) error {
		if !registered || id != "io.reasonix.desktop" || name != "Reasonix" {
			t.Fatalf("display name update = %q/%q, registered=%t", id, name, registered)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	n := desktopNotification(Message{Title: "Done", Body: "Task completed"})
	if n.AppID != "io.reasonix.desktop" || n.Title != "Done" || n.Body != "Task completed" {
		t.Fatalf("notification = %+v", n)
	}
}

func TestWindowsNotificationRegistrationFailureDoesNotWriteMetadata(t *testing.T) {
	want := errors.New("registration refused")
	err := registerDesktopNotifications(func(toast.AppData) error { return want }, func(string, string) error {
		t.Fatal("metadata written after registration failed")
		return nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("registration error = %v, want %v", err, want)
	}
}

func TestNotificationDisplayNamePreservesOtherRegistrations(t *testing.T) {
	prefix := "Reasonix.IdentityTest." + rand.Text()
	ids := []string{prefix + ".Legacy", prefix + ".Desktop"}
	for _, id := range ids {
		path := filepath.Join("Software", "Classes", "AppUserModelId", id)
		key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = registry.DeleteKey(registry.CURRENT_USER, path) })
		if err := key.SetStringValue("DisplayName", id); err != nil {
			t.Fatal(err)
		}
		if err := key.SetStringValue("CustomActivator", "preserve-activator"); err != nil {
			t.Fatal(err)
		}
		key.Close()
	}
	if err := setNotificationDisplayName(ids[1], "Reasonix"); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		key, err := registry.OpenKey(registry.CURRENT_USER, filepath.Join("Software", "Classes", "AppUserModelId", id), registry.QUERY_VALUE)
		if err != nil {
			t.Fatal(err)
		}
		name, _, nameErr := key.GetStringValue("DisplayName")
		activator, _, activatorErr := key.GetStringValue("CustomActivator")
		key.Close()
		want := id
		if i == 1 {
			want = "Reasonix"
		}
		if nameErr != nil || activatorErr != nil || name != want || activator != "preserve-activator" {
			t.Fatalf("registration %s changed unexpectedly: name=%q (%v), activator=%q (%v)", id, name, nameErr, activator, activatorErr)
		}
	}
}
