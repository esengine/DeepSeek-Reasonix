package remotecloud

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"
)

// controllerGrace is how long encrypted controller sessions outlive a lost relay
// connection. Controllers stay attached at the relay while the device
// reconnects, so a quick reconnect resumes them instead of stranding each phone
// with a session only it still holds.
const controllerGrace = 2 * time.Minute

const (
	replayTTL        = 10 * time.Minute
	replayEntries    = 64
	replayEntryBytes = 2 << 20
	replayTotalBytes = 8 << 20
)

const (
	minHeartbeat = 5 * time.Second
	maxHeartbeat = time.Minute
)

// relayLink is the state that spans relay connections. Only the Run goroutine
// touches it.
type relayLink struct {
	deviceID     string
	instanceID   string
	sessions     map[string]*controllerSession
	offlineSince time.Time
	replay       replayCache
}

// instance names this process to controllers. A controller resends a
// state-changing request only to the instance it sent it to, because only that
// instance holds the answer in its replay cache.
func (h *Host) instance() string {
	if h.link.instanceID == "" {
		h.link.instanceID, _ = randomToken()
	}
	return h.link.instanceID
}

func randomToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// admit binds a command to this session. A controller that names the session
// the desktop issued in `ready` must number its commands upward, so neither a
// command from another session nor an earlier one of this session is accepted
// again. Commands naming no session come from controllers built before binding.
func (s *controllerSession) admit(command controllerCommand) error {
	if command.Session == "" {
		return nil
	}
	if command.Session != s.nonce || command.Seq <= s.lastSeq {
		return errors.New("remote cloud: replayed or foreign controller command")
	}
	s.lastSeq = command.Seq
	return nil
}

type relayHelloMessage struct {
	Type        string   `json:"type"`
	Controllers []string `json:"controllers"`
	Heartbeat   string   `json:"heartbeat"`
	HeartbeatMs int      `json:"heartbeatMs"`
}

func parseRelayHello(payload []byte) (relayHelloMessage, bool) {
	var hello relayHelloMessage
	if json.Unmarshal(payload, &hello) != nil || hello.Type != "relay_hello" {
		return relayHelloMessage{}, false
	}
	return hello, true
}

func (m relayHelloMessage) interval() time.Duration {
	interval := time.Duration(m.HeartbeatMs) * time.Millisecond
	return min(max(interval, minHeartbeat), maxHeartbeat)
}

// adopt returns the session map for this device identity, discarding sessions
// keyed to another identity: their ciphers derive from that identity's key.
func (h *Host) adopt(deviceID string) map[string]*controllerSession {
	if h.link.deviceID != deviceID {
		h.dropControllers()
		h.link.replay = replayCache{}
		h.link.deviceID = deviceID
	}
	if h.link.sessions == nil {
		h.link.sessions = make(map[string]*controllerSession)
	}
	h.link.offlineSince = time.Time{}
	return h.link.sessions
}

func (h *Host) suspend(now time.Time) {
	if h.link.offlineSince.IsZero() {
		h.link.offlineSince = now
	}
}

func (h *Host) expireSuspended(now time.Time) {
	if !h.link.offlineSince.IsZero() && now.Sub(h.link.offlineSince) >= controllerGrace {
		h.dropControllers()
	}
}

// keepControllers drops every session the relay no longer has attached.
func (h *Host) keepControllers(attached []string) {
	for id := range h.link.sessions {
		if !slices.Contains(attached, id) {
			h.forgetController(id)
		}
	}
}

func (h *Host) forgetController(id string) {
	delete(h.link.sessions, id)
	if h.presence != nil {
		h.presence.CloudControllerDisconnected(id)
	}
}

func (h *Host) dropControllers() {
	for id := range h.link.sessions {
		h.forgetController(id)
	}
	h.link.offlineSince = time.Time{}
}

// replayCache keeps the answer to each state-changing desktop request for a
// while, so a controller that resends one after a reconnect gets the original
// answer instead of running it twice. It outlives controller sessions and is
// reset only when the device identity or account changes.
type replayCache struct {
	entries map[string]replayEntry
	bytes   int
}

type replayEntry struct {
	at       time.Time
	size     int
	payloads []map[string]any
}

// replayKey binds a cached answer to the request it answered, not only its id.
func replayKey(id, method, path string, body []byte) string {
	sum := sha256.New()
	for _, part := range [][]byte{[]byte(method), []byte(path), body} {
		sum.Write([]byte{byte(len(part) >> 24), byte(len(part) >> 16), byte(len(part) >> 8), byte(len(part))})
		sum.Write(part)
	}
	return id + ":" + hex.EncodeToString(sum.Sum(nil))
}

func replayable(method string) bool {
	return method != http.MethodGet && method != http.MethodHead
}

func (c *replayCache) get(id string, now time.Time) ([]map[string]any, bool) {
	entry, ok := c.entries[id]
	if !ok || now.Sub(entry.at) >= replayTTL {
		return nil, false
	}
	return entry.payloads, true
}

func (c *replayCache) put(id string, payloads []map[string]any, size int, now time.Time) {
	if size > replayEntryBytes {
		return
	}
	if c.entries == nil {
		c.entries = make(map[string]replayEntry)
	}
	for key, entry := range c.entries {
		if now.Sub(entry.at) >= replayTTL {
			c.remove(key)
		}
	}
	c.remove(id)
	for len(c.entries) >= replayEntries || c.bytes+size > replayTotalBytes {
		oldest := ""
		for key, entry := range c.entries {
			if oldest == "" || entry.at.Before(c.entries[oldest].at) {
				oldest = key
			}
		}
		c.remove(oldest)
	}
	c.entries[id] = replayEntry{at: now, size: size, payloads: payloads}
	c.bytes += size
}

func (c *replayCache) remove(id string) {
	if entry, ok := c.entries[id]; ok {
		c.bytes -= entry.size
		delete(c.entries, id)
	}
}
