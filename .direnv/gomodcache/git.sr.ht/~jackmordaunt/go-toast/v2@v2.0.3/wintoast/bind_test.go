//go:build windows

package wintoast

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

// TestSetAppData ensures correct control flow.
func TestSetAppData(t *testing.T) {

	t.Run("set data", func(t *testing.T) {
		var didEarlyOut = true

		setAppDataFunc = func(data AppData) error {
			didEarlyOut = false
			return nil
		}

		appData = AppData{}

		input := AppData{AppID: "test-id"}

		if err := SetAppData(input); err != nil {
			t.Fatalf("error: %v", err)
		}

		if appData != input {
			t.Fatalf("want=%v, got %v", input, appData)
		}

		if didEarlyOut {
			t.Fatalf("expected to manipulate registry, instead early out")
		}
	})

	t.Run("avoid setting empty data", func(t *testing.T) {
		var didEarlyOut = true

		setAppDataFunc = func(data AppData) error {
			didEarlyOut = false
			return nil
		}

		appData = AppData{AppID: "test-id"}

		input := AppData{}

		if err := SetAppData(input); err != nil {
			t.Fatalf("error: %v", err)
		}

		if appData == input {
			t.Fatalf("want=%v, got %v", appData, input)
		}

		if !didEarlyOut {
			t.Fatal("expected early out, instead registry was manipulated")
		}
	})

	t.Run("cancel on error", func(t *testing.T) {
		setAppDataFunc = func(data AppData) error {
			return fmt.Errorf("fake error")
		}

		appData = AppData{}

		input := AppData{AppID: "test-id"}

		if err := SetAppData(input); err == nil {
			t.Fatalf("expected error, got nil")
		}

		if appData == input {
			t.Fatalf("want=%v, got %v", appData, input)
		}
	})

	t.Run("expect registry keys", func(t *testing.T) {

		t.Run("minimal keys", func(t *testing.T) {
			setAppDataFunc = setAppDataImpl

			record := map[string]string{}

			// Capture what would be written out to the registry.
			writeStringValue = func(path, name, value string) error {
				record[filepath.Join(path, name)] = value
				return nil
			}

			input := AppData{
				AppID: "test-id",
			}

			if err := SetAppData(input); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			expect := map[string]string{
				filepath.Join(appKeyRoot, input.AppID, "CustomActivator"): GUID_ImplNotificationActivationCallback.String(),
				filepath.Join(appKeyRoot, input.AppID, "DisplayName"):     input.AppID,
			}

			if !reflect.DeepEqual(expect, record) {
				t.Fatalf("\nwant=%v \ngot =%v\n", expect, record)
			}
		})

		t.Run("all keys", func(t *testing.T) {
			setAppDataFunc = setAppDataImpl

			record := map[string]string{}

			// Capture what would be written out to the registry.
			writeStringValue = func(path, name, value string) error {
				record[filepath.Join(path, name)] = value
				return nil
			}

			input := AppData{
				AppID:               "test-id",
				IconPath:            "path/to/icon.ico",
				IconBackgroundColor: "#FFFFFF",
				ActivationExe:       "path/to/exe",
			}

			if err := SetAppData(input); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			expect := map[string]string{
				filepath.Join(appKeyRoot, input.AppID, "CustomActivator"):     GUID_ImplNotificationActivationCallback.String(),
				filepath.Join(appKeyRoot, input.AppID, "DisplayName"):         input.AppID,
				filepath.Join(appKeyRoot, input.AppID, "IconUri"):             input.IconPath,
				filepath.Join(appKeyRoot, input.AppID, "IconBackgroundColor"): input.IconBackgroundColor,
				filepath.Join(activationKey):                                  input.ActivationExe,
			}

			if !reflect.DeepEqual(expect, record) {
				t.Fatalf("\nwant=%v \ngot =%v\n", expect, record)
			}
		})
	})
}
