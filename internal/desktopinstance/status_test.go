package desktopinstance

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func TestExitCodesPreserveWrappedBlockers(t *testing.T) {
	for _, tc := range []struct {
		code Code
		exit int
	}{
		{ConfirmationRequired, 1618}, {Cancelled, 1602}, {UnknownOwner, 1618}, {OtherInstallation, 1618}, {ExitTimeout, 1618}, {StartupFailed, 1603},
	} {
		if got := ExitCode(fmt.Errorf("activation: %w", outcome(tc.code, "test"))); got != tc.exit {
			t.Errorf("%s: got %d", tc.code, got)
		}
	}
}

func TestStatusRequiresRendererAndHealthyVisibleWindow(t *testing.T) {
	s := Status{SchemaVersion: 1, Product: "com.reasonix.desktop", PID: 10, Version: "v1.38.7", Generation: "g", HomeKey: ProfileKey(`C:\Users\Test\reasonix\desktop-shell`), Lifecycle: "ready", Service: "ready", ServicePID: 11, Visible: true, RendererVersion: "v1.38.7", Healthy: true}
	data, _ := json.Marshal(s)
	got, err := DecodeStatus(data, 10)
	if err != nil || !got.Ready("v1.38.7") {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := DecodeStatus(data, 12); err == nil {
		t.Fatal("accepted another PID")
	}
	s.Healthy = false
	if s.Ready("") {
		t.Fatal("accepted unconfirmed frontend")
	}
	s.Healthy = true
	s.RendererVersion = "v1.38.5"
	if s.Ready("") {
		t.Fatal("accepted mismatched renderer")
	}
	s.RendererVersion = s.Version
	s.Visible = false
	if s.Ready("") {
		t.Fatal("accepted invisible window")
	}
}

func TestImageRoleExcludesOtherProductsAndInstallations(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"versions/v1.38.5/app/Reasonix.exe", "versions/v1.38.5/reasonix-desktop.exe"} {
		if ImageRole(root, filepath.Join(root, rel)) == "" {
			t.Fatal(rel)
		}
	}
	for _, rel := range []string{"../Studio/app/Reasonix.exe", "Reasonix Studio.exe", "versions/.staging/app/Reasonix.exe", "versions/v1.38.5/reasonix-cli.exe"} {
		if ImageRole(root, filepath.Join(root, rel)) != "" {
			t.Fatal(rel)
		}
	}
}

func TestStatusRejectsFutureProtocolAndOversizedMessages(t *testing.T) {
	if _, err := DecodeStatus(make([]byte, StatusLimit+1), 1); err == nil {
		t.Fatal("unbounded status")
	}
	if _, err := DecodeStatus([]byte(`{"schemaVersion":2}`), 1); err == nil {
		t.Fatal("future protocol")
	}
	if ProfileKey(`C:\Users\TEST\Profile`) != ProfileKey(`c:/users/test/profile`) {
		t.Fatal("Windows aliases differ")
	}
}
