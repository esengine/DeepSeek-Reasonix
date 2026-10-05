//go:build windows

package winuninstall

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestDeleteInstallLocationIfOwned(t *testing.T) {
	tests := []struct {
		name       string
		location   string
		wantDelete bool
	}{
		{name: "same root", location: `D:\Reasonix`, wantDelete: true},
		{name: "quoted case variant", location: `"d:\reasonix\"`, wantDelete: true},
		{name: "separate legacy install", location: `C:\Legacy\Reasonix`, wantDelete: false},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyPath := fmt.Sprintf(`Software\ReasonixTests\winuninstall-%d-%d`, os.Getpid(), i)
			key, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE|registry.SET_VALUE)
			if err != nil {
				t.Fatal(err)
			}
			if err := key.SetStringValue("", tt.location); err != nil {
				key.Close()
				t.Fatal(err)
			}
			if err := key.SetStringValue("Installer Language", "1033"); err != nil {
				key.Close()
				t.Fatal(err)
			}
			if err := key.Close(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = registry.DeleteKey(registry.CURRENT_USER, keyPath)
			})

			if err := deleteInstallLocationIfOwned(keyPath, `D:\Reasonix`); err != nil {
				t.Fatal(err)
			}

			key, err = registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
			if err != nil {
				t.Fatal(err)
			}
			defer key.Close()
			gotLocation, _, locationErr := key.GetStringValue("")
			if tt.wantDelete {
				if !errors.Is(locationErr, registry.ErrNotExist) {
					t.Fatalf("default install location still exists: value=%q err=%v", gotLocation, locationErr)
				}
			} else if locationErr != nil || gotLocation != tt.location {
				t.Fatalf("separate install location = %q, %v; want %q", gotLocation, locationErr, tt.location)
			}
			if language, _, err := key.GetStringValue("Installer Language"); err != nil || language != "1033" {
				t.Fatalf("named value = %q, %v; want preserved", language, err)
			}
		})
	}
}

func TestDeleteInstallLocationIfOwnedIgnoresMissingKey(t *testing.T) {
	keyPath := fmt.Sprintf(`Software\ReasonixTests\missing-%d`, os.Getpid())
	_ = registry.DeleteKey(registry.CURRENT_USER, keyPath)
	if err := deleteInstallLocationIfOwned(keyPath, `D:\Reasonix`); err != nil {
		t.Fatal(err)
	}
}
