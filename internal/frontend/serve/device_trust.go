package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/config"
)

// persistedDevice is one paired device as it survives a restart. The digest is
// sha256 of the credential the device holds in its cookie — the credential
// itself is never written, so the file hands out nothing to whoever reads it.
type persistedDevice struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Digest   string    `json:"digest"`
	PairedAt time.Time `json:"pairedAt"`
	LastSeen time.Time `json:"lastSeen"`
}

// deviceTrustPath is where this machine keeps the devices it has paired. It
// rides the Reasonix state directory, so REASONIX_HOME isolation holds and a
// test instance never adopts the real one's devices.
func deviceTrustPath() string {
	if !rememberPairedDevices() {
		return ""
	}
	home := strings.TrimSpace(config.ReasonixHomeDir())
	if home == "" {
		return ""
	}
	return filepath.Join(home, "state", "device-trust.json")
}

// loadDeviceTrust reads what a previous process had paired. A missing, empty or
// unreadable file reads as "no devices": the restart then asks for a code, which
// is the behaviour this file exists to avoid, not one to fail the share over.
func loadDeviceTrust(path string) []persistedDevice {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var saved []persistedDevice
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil
	}
	return saved
}

// saveDeviceTrust writes the paired set atomically, and removes the file once
// nothing is paired. A failure is the caller's to log: the pairing already
// happened, and a share that kept running without its file is better than one
// that refused a phone over a disk.
func saveDeviceTrust(path string, devices []persistedDevice) error {
	if path == "" {
		return nil
	}
	if len(devices) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	raw, err := json.Marshal(devices)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, raw, 0o600)
}

// snapshotLocked copies the paired set into its persisted shape. The caller
// holds d.mu.
func (d *DeviceRegistry) snapshotLocked() []persistedDevice {
	out := make([]persistedDevice, 0, len(d.devices))
	for _, dev := range d.devices {
		out = append(out, persistedDevice{
			ID:       dev.id,
			Name:     dev.name,
			Digest:   hex.EncodeToString(dev.digest[:]),
			PairedAt: dev.paired,
			LastSeen: dev.seen,
		})
	}
	return out
}

// flush hands the persisted set to the hook, outside the lock so a disk write
// never holds up a request.
func (d *DeviceRegistry) flush(snapshot []persistedDevice) {
	if d.persist != nil {
		d.persist(snapshot)
	}
}

// Restore adopts the devices a previous process had paired, so a phone that
// scanned once is still paired after a restart. Entries that do not carry a
// usable digest are dropped rather than half-adopted.
func (d *DeviceRegistry) Restore(saved []persistedDevice) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, one := range saved {
		digest, err := hex.DecodeString(one.Digest)
		if err != nil || len(digest) != sha256.Size || strings.TrimSpace(one.ID) == "" {
			continue
		}
		if _, exists := d.devices[one.ID]; exists {
			continue
		}
		var sum [sha256.Size]byte
		copy(sum[:], digest)
		d.devices[one.ID] = &pairedDevice{
			id:      one.ID,
			name:    one.Name,
			digest:  sum,
			paired:  one.PairedAt,
			seen:    one.LastSeen,
			streams: map[*deviceStream]struct{}{},
		}
	}
}

// rememberPairedDevices reports whether this install keeps phone access across
// restarts: the share reopens where it was, and the phones that paired against it
// are adopted. Off, the share is memory-only, so closing it or ending the process
// unpairs every device, which is what shipped.
func rememberPairedDevices() bool {
	cfg, err := config.Load()
	return err == nil && cfg.Serve.RememberPairedDevices
}
