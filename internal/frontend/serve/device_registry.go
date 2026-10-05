package serve

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// pairingTTL bounds how long a displayed code is worth anything: long
	// enough to find a phone, short enough that a photo of the screen is stale.
	pairingTTL = 5 * time.Minute
	// deviceNameMax caps the label a device is listed under.
	deviceNameMax = 80
)

// ErrPairingInvalid is every way a code can fail to pair: unknown, used,
// expired or superseded. The caller learns nothing about which.
var ErrPairingInvalid = errors.New("pairing code is not valid")

// DeviceRegistry holds which devices may reach a kernel and the one pairing
// code that can add another. Only digests are kept: a registry dumped from
// memory hands out no credential. It lives for the process, so a restart
// unpairs every device.
type DeviceRegistry struct {
	mu      sync.Mutex
	now     func() time.Time
	pending pairingOffer
	devices map[string]*pairedDevice
}

// pairingOffer is the code on screen. A zero digest is no offer.
type pairingOffer struct {
	digest  [sha256.Size]byte
	expires time.Time
}

type pairedDevice struct {
	id     string
	name   string
	digest [sha256.Size]byte
	paired time.Time
	seen   time.Time
	// streams are the event streams this device holds open. Each can be cut,
	// because a credential checked when a stream opened says nothing about
	// whether the device is still paired an hour into it.
	streams map[*deviceStream]struct{}
}

type deviceStream struct{ cancel context.CancelFunc }

// DeviceView is a paired device as the host lists it.
type DeviceView struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	PairedAt time.Time `json:"pairedAt"`
	LastSeen time.Time `json:"lastSeen"`
	// Online is whether the device holds an event stream open right now: a
	// page on screen, not a page that was last opened some time ago.
	Online bool `json:"online"`
}

// NewDeviceRegistry returns an empty registry with no code on offer.
func NewDeviceRegistry() *DeviceRegistry {
	return newDeviceRegistryAt(time.Now)
}

func newDeviceRegistryAt(now func() time.Time) *DeviceRegistry {
	return &DeviceRegistry{now: now, devices: map[string]*pairedDevice{}}
}

// Offer mints the code a new device pairs with, replacing any earlier one.
func (d *DeviceRegistry) Offer() (code string, expires time.Time) {
	code = randomSecret()
	d.mu.Lock()
	defer d.mu.Unlock()
	expires = d.now().Add(pairingTTL)
	d.pending = pairingOffer{digest: sha256.Sum256([]byte(code)), expires: expires}
	return code, expires
}

// Withdraw takes the code off offer without pairing anything.
func (d *DeviceRegistry) Withdraw() {
	d.mu.Lock()
	d.pending = pairingOffer{}
	d.mu.Unlock()
}

// OfferExpires reports when the code on offer lapses, if one is.
func (d *DeviceRegistry) OfferExpires() (time.Time, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.offerLiveLocked() {
		return time.Time{}, false
	}
	return d.pending.expires, true
}

func (d *DeviceRegistry) offerLiveLocked() bool {
	return d.pending.digest != [sha256.Size]byte{} && d.now().Before(d.pending.expires)
}

// Redeem spends the code on offer for a device credential. A code pairs once:
// whoever presents it first is the device, and the offer is gone either way.
func (d *DeviceRegistry) Redeem(code, name string) (credential string, view DeviceView, err error) {
	sum := sha256.Sum256([]byte(code))
	d.mu.Lock()
	defer d.mu.Unlock()
	if code == "" || !d.offerLiveLocked() || subtle.ConstantTimeCompare(sum[:], d.pending.digest[:]) != 1 {
		return "", DeviceView{}, ErrPairingInvalid
	}
	d.pending = pairingOffer{}
	credential = randomSecret()
	now := d.now()
	dev := &pairedDevice{
		id:      deviceID(),
		name:    deviceLabel(name),
		digest:  sha256.Sum256([]byte(credential)),
		paired:  now,
		seen:    now,
		streams: map[*deviceStream]struct{}{},
	}
	d.devices[dev.id] = dev
	return credential, dev.view(), nil
}

// Authenticate names the device a credential belongs to, and records that it
// was seen.
func (d *DeviceRegistry) Authenticate(credential string) (string, bool) {
	if credential == "" {
		return "", false
	}
	sum := sha256.Sum256([]byte(credential))
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, dev := range d.devices {
		if subtle.ConstantTimeCompare(sum[:], dev.digest[:]) == 1 {
			dev.seen = d.now()
			return dev.id, true
		}
	}
	return "", false
}

// Devices lists what is paired, earliest first.
func (d *DeviceRegistry) Devices() []DeviceView {
	d.mu.Lock()
	out := make([]DeviceView, 0, len(d.devices))
	for _, dev := range d.devices {
		out = append(out, dev.view())
	}
	d.mu.Unlock()
	slices.SortFunc(out, func(a, b DeviceView) int {
		if c := a.PairedAt.Compare(b.PairedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// Revoke unpairs one device. Its next request is refused and every stream it
// holds open is cut now.
func (d *DeviceRegistry) Revoke(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	dev, ok := d.devices[id]
	if !ok {
		return false
	}
	delete(d.devices, id)
	dev.cut()
	return true
}

// RevokeAll unpairs every device, cuts their streams and withdraws the code on
// offer.
func (d *DeviceRegistry) RevokeAll() {
	d.mu.Lock()
	for _, dev := range d.devices {
		dev.cut()
	}
	d.devices = map[string]*pairedDevice{}
	d.pending = pairingOffer{}
	d.mu.Unlock()
}

// holdStream registers a stream the device opened and derives the context it
// runs under, which unpairing the device cancels. The returned func ends the
// hold; a device already unpaired gets a context that is done.
func (d *DeviceRegistry) holdStream(ctx context.Context, id string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	defer d.mu.Unlock()
	dev, ok := d.devices[id]
	if !ok {
		cancel()
		return ctx, func() {}
	}
	s := &deviceStream{cancel: cancel}
	dev.streams[s] = struct{}{}
	return ctx, func() {
		d.mu.Lock()
		delete(dev.streams, s)
		d.mu.Unlock()
		cancel()
	}
}

// Self is the device as it sees itself: its view and its place in the list
// the host draws, so both name it the same.
func (d *DeviceRegistry) Self(id string) (DeviceView, int, bool) {
	for i, v := range d.Devices() {
		if v.ID == id {
			return v, i + 1, true
		}
	}
	return DeviceView{}, 0, false
}

func (p *pairedDevice) cut() {
	for s := range p.streams {
		s.cancel()
	}
}

func (p *pairedDevice) view() DeviceView {
	return DeviceView{ID: p.id, Name: p.name, PairedAt: p.paired, LastSeen: p.seen, Online: len(p.streams) > 0}
}

// deviceLabel is display text only; nothing decides on it.
func deviceLabel(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > deviceNameMax {
		name = string(r[:deviceNameMax])
	}
	return name
}

func randomSecret() string {
	b := make([]byte, tokenByteLen)
	if _, err := rand.Read(b); err != nil {
		panic("serve/device: crypto/rand.Read failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func deviceID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic("serve/device: crypto/rand.Read failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
