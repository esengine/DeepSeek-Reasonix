package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDeviceTrustSurvivesARestart is the whole point of the file: a phone that
// scanned once still holds a credential the next process accepts.
func TestDeviceTrustSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "device-trust.json")
	first := newDeviceRegistryAt(time.Now)
	first.persist = func(devices []persistedDevice) { _ = saveDeviceTrust(path, devices) }

	code, _ := first.Offer()
	credential, view, err := first.Redeem(code, "phone")
	if err != nil {
		t.Fatal(err)
	}

	second := newDeviceRegistryAt(time.Now)
	second.Restore(loadDeviceTrust(path))
	id, ok := second.Authenticate(credential)
	if !ok || id != view.ID {
		t.Fatalf("a restarted registry refused the paired credential: id=%q ok=%v want %q", id, ok, view.ID)
	}
	if got := second.Devices(); len(got) != 1 || got[0].Name != view.Name {
		t.Fatalf("restored device list = %+v, want one entry named %q", got, view.Name)
	}
}

// TestUnpairingForgetsTheDeviceOnDisk holds the decision the switch was asked
// for: closing the share unpairs every device, and removing one leaves nothing
// behind to be adopted at the next start.
func TestUnpairingForgetsTheDeviceOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device-trust.json")
	reg := newDeviceRegistryAt(time.Now)
	reg.persist = func(devices []persistedDevice) { _ = saveDeviceTrust(path, devices) }

	code, _ := reg.Offer()
	_, view, err := reg.Redeem(code, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("pairing did not write the trust file: %v", err)
	}

	if !reg.Revoke(view.ID) {
		t.Fatal("revoking a paired device reported no such device")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("removing the last device left its file behind (stat err = %v)", err)
	}

	code, _ = reg.Offer()
	if _, _, err := reg.Redeem(code, "phone"); err != nil {
		t.Fatal(err)
	}
	reg.RevokeAll()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("closing the share left the file behind (stat err = %v)", err)
	}
}

// TestTrustFileWithoutUsableDigestsIsIgnored keeps a damaged or hand-edited file
// from pairing something it cannot authenticate: it reads as no devices.
func TestTrustFileWithoutUsableDigestsIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device-trust.json")
	body := `[{"id":"a","digest":"not-hex"},{"id":"","digest":"00"},{"id":"b"},{"id":"c","digest":"0011"}]`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := newDeviceRegistryAt(time.Now)
	reg.Restore(loadDeviceTrust(path))
	if got := reg.Devices(); len(got) != 0 {
		t.Fatalf("unusable entries were adopted: %+v", got)
	}
}

// TestMissingTrustFileReadsAsNoDevices covers the first run, where nothing has
// been written yet.
func TestMissingTrustFileReadsAsNoDevices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "device-trust.json")
	reg := newDeviceRegistryAt(time.Now)
	reg.Restore(loadDeviceTrust(path))
	if got := reg.Devices(); len(got) != 0 {
		t.Fatalf("a missing file produced devices: %+v", got)
	}
}

// TestShutdownKeepsTheDevicesCloseForgets holds the two exits apart: the process
// going away keeps the phones this machine adopted, while the user closing the
// share withdraws them.
func TestShutdownKeepsTheDevicesCloseForgets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device-trust.json")
	share := NewDeviceShare(nil)
	share.remember = func() bool { return true }
	share.registry.persist = func(devices []persistedDevice) { _ = saveDeviceTrust(path, devices) }

	code, _ := share.registry.Offer()
	credential, _, err := share.registry.Redeem(code, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("pairing did not write the trust file: %v", err)
	}

	share.Shutdown()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("shutdown forgot the paired devices: %v", err)
	}
	next := newDeviceRegistryAt(time.Now)
	next.Restore(loadDeviceTrust(path))
	if _, ok := next.Authenticate(credential); !ok {
		t.Fatal("the next process did not accept the credential a shutdown left behind")
	}

	share.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("closing the share left the file behind (stat err = %v)", err)
	}
}

// TestReopeningAShareKeepsThePairedDevices is the restart path: opening the share
// again stops the old listener but leaves the phones paired, so nobody scans a
// second time.
func TestReopeningAShareKeepsThePairedDevices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device-trust.json")
	share := NewDeviceShare(nil)
	share.remember = func() bool { return true }
	share.registry.persist = func(devices []persistedDevice) { _ = saveDeviceTrust(path, devices) }
	share.Attach(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	share.addresses = func() []ShareAddress { return []ShareAddress{{Interface: "lo", IP: "127.0.0.1", Kind: AddressLAN}} }
	t.Cleanup(share.Close)

	code, _ := share.registry.Offer()
	credential, _, err := share.registry.Redeem(code, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := share.Open("127.0.0.1"); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, ok := share.registry.Authenticate(credential); !ok {
		t.Fatal("reopening the share unpaired the phone that had scanned")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("reopening the share forgot the devices on disk: %v", err)
	}
}

// TestWithoutTheSwitchShutdownStillForgets is the shipping default: an install that
// does not remember paired phones keeps the behaviour it always had, so a process
// going away unpairs the phones it was holding.
func TestWithoutTheSwitchShutdownStillForgets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device-trust.json")
	share := NewDeviceShare(nil)
	share.registry.persist = func(devices []persistedDevice) { _ = saveDeviceTrust(path, devices) }

	code, _ := share.registry.Offer()
	if _, _, err := share.registry.Redeem(code, "phone"); err != nil {
		t.Fatal(err)
	}
	share.Shutdown()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the default install kept devices across a shutdown (stat err = %v)", err)
	}
}
