package serve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// ErrShareAddress refuses an address the window does not own on a private
// network. Binding a wildcard or a public address is not a sharing decision
// this surface makes.
var ErrShareAddress = errors.New("that address is not a private address on this machine")

// ErrShareClosed is an offer asked of a share that is not listening.
var ErrShareClosed = errors.New("sharing is off")

// ErrShareUnattached is a share opened before the host named what it serves.
var ErrShareUnattached = errors.New("sharing has nothing to serve yet")

// tailnetPrefix is the CGNAT range Tailscale hands out; its traffic is already
// encrypted end to end, which makes it the one routed network worth offering.
var tailnetPrefix = netip.MustParsePrefix("100.64.0.0/10")

// DeviceShare is a window's second door: a listener on a private address that
// paired devices reach the window's hub through, behind a device gate. It is
// shut until the person at the window opens it, and shutting it unpairs every
// device.
type DeviceShare struct {
	registry *DeviceRegistry
	cloud    *cloudPresence
	page     fs.FS
	// addresses lists what may be bound; a variable seam so tests need no NIC.
	addresses func() []ShareAddress
	// turn serialises Open and Close, so two opens cannot leave one listener
	// running that nothing will ever stop.
	turn sync.Mutex

	mu              sync.Mutex
	handler         http.Handler
	live            *shareListener
	cloudDisconnect func(string) error
	cloudStatus     func() CloudRemoteStatus
	port            int
	persistPort     func(int) error
	// persistState records whether the share is open and on what address, so the
	// next process can reopen it. Nil leaves the share in memory only, which is
	// what every test builds.
	persistState func(persistedShareState) error
	// remember reports whether this install keeps paired phones across restarts.
	// Nil is the historical behaviour: reopening the share or ending the process
	// unpairs every device, which is what a directly built share gets.
	remember func() bool
}

// remembers reports whether paired phones outlive the door that paired them.
func (s *DeviceShare) remembers() bool {
	return s.remember != nil && s.remember()
}

type shareListener struct {
	origin string
	stop   context.CancelFunc
	done   chan struct{}
}

// ShareAddress is one address a share can listen on.
type ShareAddress struct {
	Interface string      `json:"interface"`
	IP        string      `json:"ip"`
	Kind      AddressKind `json:"kind"`
}

// AddressKind says what reaches an address, read from the interface itself.
type AddressKind string

const (
	// AddressLAN is a broadcast-capable adapter with a hardware address: Wi-Fi
	// or Ethernet, which a phone on the same network can reach.
	AddressLAN AddressKind = "lan"
	// AddressTailnet is a Tailscale address, reachable from anywhere on the
	// tailnet and encrypted on the way.
	AddressTailnet AddressKind = "tailnet"
	// AddressVirtual is anything else: a VPN or proxy tunnel, a VM switch.
	// Listed, because it may be what someone wants, but never the default.
	AddressVirtual AddressKind = "virtual"
)

var addressRank = map[AddressKind]int{AddressTailnet: 0, AddressLAN: 1, AddressVirtual: 2}

// ShareStatus is the whole state a window draws its sharing panel from.
type ShareStatus struct {
	Open         bool                  `json:"open"`
	Port         int                   `json:"port,omitempty"`
	Origin       string                `json:"origin,omitempty"`
	Addresses    []ShareAddress        `json:"addresses"`
	Devices      []DeviceView          `json:"devices"`
	CloudDevices []CloudControllerView `json:"cloudDevices"`
	CloudRemote  CloudRemoteStatus     `json:"cloudRemote"`
	OfferExpires *time.Time            `json:"offerExpires,omitempty"`
}

// CloudRemoteStatus is this Studio's own reachability through the encrypted
// Internet relay. The device identity routes a connection; it is not a
// credential, and the account service still verifies ownership before access.
type CloudRemoteStatus struct {
	DeviceID string `json:"deviceId,omitempty"`
	Name     string `json:"name,omitempty"`
	Online   bool   `json:"online"`
	Error    string `json:"error,omitempty"`
}

type CloudShareOffer struct {
	URL string `json:"url"`
	QR  string `json:"qr"`
}

// ShareOffer is a pairing code as a device receives it: a link that carries the
// code in its fragment, and that link drawn as a QR code.
type ShareOffer struct {
	URL     string    `json:"url"`
	QR      string    `json:"qr"`
	Expires time.Time `json:"expires"`
}

// NewDeviceShare returns a closed share serving page to devices.
func NewDeviceShare(page fs.FS) *DeviceShare {
	registry := NewDeviceRegistry()
	// Devices paired before a restart are adopted here, and every later change
	// writes them back: a phone that scanned once keeps its cookie across
	// restarts, until the share is closed or that device is removed.
	if trust := deviceTrustPath(); trust != "" {
		registry.Restore(loadDeviceTrust(trust))
		registry.persist = func(devices []persistedDevice) { _ = saveDeviceTrust(trust, devices) }
	}
	return &DeviceShare{registry: registry, cloud: newCloudPresence(), page: page, addresses: PrivateAddresses, persistPort: persistSharePort, remember: rememberPairedDevices}
}

// Attach names the handler devices reach. The hub is built after the share it
// registers routes for, so this closes that loop before anything opens.
func (s *DeviceShare) Attach(h http.Handler) {
	s.mu.Lock()
	s.handler = h
	s.mu.Unlock()
}

// Open listens on ip, one of the addresses Status lists, on the configured port
// or one the system picks. A share already open stops listening first; the phones
// already paired stay paired, so reopening — after a restart, say — does not ask
// for a new code.
func (s *DeviceShare) Open(ip string) (ShareStatus, error) {
	if !slices.ContainsFunc(s.addresses(), func(a ShareAddress) bool { return a.IP == ip }) {
		return s.Status(), ErrShareAddress
	}
	s.turn.Lock()
	defer s.turn.Unlock()
	// An install that does not remember paired devices gets the share it always
	// had: opening it again is a fresh door, and the phones of the old one go
	// with it.
	if s.remembers() {
		s.stopListeningLocked()
	} else {
		s.closeLocked()
	}
	s.mu.Lock()
	handler := s.handler
	s.mu.Unlock()
	if handler == nil {
		return s.Status(), ErrShareUnattached
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(ip, s.listenPort()))
	if err != nil {
		if addrInUse(err) {
			return s.Status(), fmt.Errorf("%w: %w", ErrSharePortInUse, err)
		}
		return s.Status(), err
	}
	origin := "http://" + ln.Addr().String()
	ctx, stop := context.WithCancel(context.Background())
	live := &shareListener{origin: origin, stop: stop, done: make(chan struct{})}
	gate := NewDeviceGate(handler, DeviceGateOptions{Registry: s.registry, Origin: origin, Page: s.page, Machine: machineName()})
	s.mu.Lock()
	s.live = live
	s.mu.Unlock()
	// Recorded only after the listener is real: the next process should reopen an
	// address that worked, never one that failed to bind.
	s.recordState(true, ip)
	go func() {
		defer close(live.done)
		if err := runGracefulListener(ctx, ln, gate); err != nil {
			slog.Warn("serve: device share stopped", "origin", origin, "err", err)
		}
	}()
	return s.Status(), nil
}

// Reopen reopens the share where it last listened. When that address is gone —
// another network, another adapter — it falls back to the first address this
// machine now offers, because a share that cannot reopen where it was is still
// better off open than silently shut. Phones paired on the old origin have to
// scan again in that case; one that kept its address does not.
func (s *DeviceShare) Reopen(address string) (ShareStatus, error) {
	if strings.TrimSpace(address) != "" {
		if st, err := s.Open(address); err == nil {
			return st, nil
		}
	}
	list := s.addresses()
	if len(list) == 0 {
		return s.Status(), ErrShareAddress
	}
	return s.Open(list[0].IP)
}

// RestoreState adopts what the previous process remembered — whether the share
// was open, and where — and wires the hook that records every later change. A
// share with no state file starts closed, which is the shipping default.
func (s *DeviceShare) RestoreState() persistedShareState {
	path := shareStatePath()
	if path == "" {
		return persistedShareState{}
	}
	s.mu.Lock()
	s.persistState = func(st persistedShareState) error { return saveShareState(path, st) }
	s.mu.Unlock()
	return loadShareState(path)
}

// recordState hands the current state to the hook, outside the lock so a disk
// write never holds up a request. The hook itself is read under the lock — it is
// set once, but the race detector cannot know that.
func (s *DeviceShare) recordState(open bool, address string) {
	s.mu.Lock()
	persist := s.persistState
	s.mu.Unlock()
	if persist != nil {
		if err := persist(persistedShareState{Open: open, Address: address}); err != nil {
			slog.Warn("serve: phone-access state was not recorded", "err", err)
		}
	}
}

// Close stops listening and unpairs every device — the user closing the share.
func (s *DeviceShare) Close() {
	s.turn.Lock()
	defer s.turn.Unlock()
	s.closeLocked()
}

// Shutdown stops listening when the process is going away. An install that
// remembers paired devices keeps them, so the next process adopts the same phones
// rather than ask for a new code; one that does not ends up as Close, which is
// what shipped.
func (s *DeviceShare) Shutdown() {
	if !s.remembers() {
		s.Close()
		return
	}
	s.turn.Lock()
	defer s.turn.Unlock()
	s.stopListeningLocked()
}

func (s *DeviceShare) closeLocked() {
	s.stopListeningLocked()
	s.registry.RevokeAll()
	s.recordState(false, "")
}

func (s *DeviceShare) stopListeningLocked() {
	s.mu.Lock()
	live := s.live
	s.live = nil
	s.mu.Unlock()
	if live != nil {
		live.stop()
		<-live.done
	}
}

// Offer mints a pairing code for the open share.
func (s *DeviceShare) Offer() (ShareOffer, error) {
	s.mu.Lock()
	live := s.live
	s.mu.Unlock()
	if live == nil {
		return ShareOffer{}, ErrShareClosed
	}
	code, expires := s.registry.Offer()
	link := live.origin + "/#pair=" + code
	svg, err := QRSVG(link)
	if err != nil {
		s.registry.Withdraw()
		return ShareOffer{}, err
	}
	return ShareOffer{URL: link, QR: svg, Expires: expires}, nil
}

// Revoke unpairs one LAN device or disconnects one authenticated Internet
// controller. Both are host-only actions exposed by the same device list.
func (s *DeviceShare) Revoke(id string) bool {
	if s.registry.Revoke(id) {
		return true
	}
	s.mu.Lock()
	disconnect := s.cloudDisconnect
	s.mu.Unlock()
	return s.cloud.contains(id) && disconnect != nil && disconnect(id) == nil
}

func (s *DeviceShare) setCloudDisconnect(disconnect func(string) error) {
	s.mu.Lock()
	s.cloudDisconnect = disconnect
	s.mu.Unlock()
}

func (s *DeviceShare) setCloudStatus(status func() CloudRemoteStatus) {
	s.mu.Lock()
	s.cloudStatus = status
	s.mu.Unlock()
}

// CloudOffer draws a public, account-gated link for this Studio. The link
// deliberately carries no token: a phone must sign in as the same owner, and
// the platform checks that the target is currently online before connecting.
func (s *DeviceShare) CloudOffer() (CloudShareOffer, error) {
	s.mu.Lock()
	status := s.cloudStatus
	s.mu.Unlock()
	if status == nil {
		return CloudShareOffer{}, errors.New("Internet remote access is not ready")
	}
	current := status()
	if !current.Online || strings.TrimSpace(current.DeviceID) == "" {
		return CloudShareOffer{}, errors.New("this Studio is not online for Internet remote access")
	}
	link := "https://reasonix.io/remote/?device=" + url.QueryEscape(current.DeviceID)
	svg, err := QRSVG(link)
	if err != nil {
		return CloudShareOffer{}, err
	}
	return CloudShareOffer{URL: link, QR: svg}, nil
}

// Status reports what is open, what could be, and who is paired.
func (s *DeviceShare) Status() ShareStatus {
	s.mu.Lock()
	live := s.live
	cloudStatus := s.cloudStatus
	port := s.port
	s.mu.Unlock()
	st := ShareStatus{Port: port, Addresses: s.addresses(), Devices: s.registry.Devices(), CloudDevices: s.cloud.views()}
	if cloudStatus != nil {
		st.CloudRemote = cloudStatus()
	}
	if live != nil {
		st.Open, st.Origin = true, live.origin
	}
	if exp, ok := s.registry.OfferExpires(); ok {
		st.OfferExpires = &exp
	}
	return st
}

// machineName is what a device is told it is driving: this computer's name,
// or a plain word where the system will not say.
func machineName() string {
	if name, err := os.Hostname(); err == nil && strings.TrimSpace(name) != "" {
		return name
	}
	return "this computer"
}

// OffInternet reports whether plain HTTP to ip stays off the public internet:
// loopback, a private range, or a tailnet, whose traffic is already encrypted.
// Only there may a credential ride an unencrypted link.
func OffInternet(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate() || tailnetPrefix.Contains(ip)
}

// PrivateAddresses is every private IPv4 address on an up interface, the ones
// a phone is likeliest to reach first: the first entry is the default.
func PrivateAddresses() []ShareAddress {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := []ShareAddress{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			pfx, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			ip := pfx.Addr().Unmap()
			if !ip.Is4() {
				continue
			}
			kind := AddressVirtual
			switch {
			case tailnetPrefix.Contains(ip):
				kind = AddressTailnet
			case !ip.IsPrivate():
				continue
			case len(iface.HardwareAddr) > 0 && iface.Flags&net.FlagBroadcast != 0:
				kind = AddressLAN
			}
			out = append(out, ShareAddress{Interface: iface.Name, IP: ip.String(), Kind: kind})
		}
	}
	slices.SortStableFunc(out, func(a, b ShareAddress) int { return addressRank[a.Kind] - addressRank[b.Kind] })
	return out
}
