package remotecloud

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/account"
)

type countingDesktop struct {
	calls int
}

func (d *countingDesktop) CloudDesktop(_ context.Context, request DesktopRequest, _ string) (DesktopResponse, error) {
	d.calls++
	return DesktopResponse{Status: http.StatusOK, ContentType: "text/plain", Body: []byte(request.Method)}, nil
}

type presenceLog struct {
	disconnected []string
}

func (p *presenceLog) CloudControllerConnected(string, string) int { return 1 }
func (p *presenceLog) CloudControllerSeen(string)                  {}
func (p *presenceLog) CloudControllerDisconnected(id string) {
	p.disconnected = append(p.disconnected, id)
}

type relayRound struct {
	device *websocket.Conn
}

// fakeRelay accepts one device connection per round and hands it to the test.
func fakeRelay(t *testing.T) (string, chan relayRound) {
	t.Helper()
	rounds := make(chan relayRound, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		rounds <- relayRound{device: conn}
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http"), rounds
}

func controllerHello(t *testing.T, device *ecdh.PrivateKey, deviceID string) (string, *sessionCipher) {
	t.Helper()
	controller, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	greeting := mustJSON(t, hello{
		Version: protocolVersion, Type: "hello",
		PublicKey: base64.RawURLEncoding.EncodeToString(controller.PublicKey().Bytes()),
		Salt:      base64.RawURLEncoding.EncodeToString(salt),
	})
	mirror := mustJSON(t, hello{
		Version: protocolVersion, Type: "hello",
		PublicKey: base64.RawURLEncoding.EncodeToString(device.PublicKey().Bytes()),
		Salt:      base64.RawURLEncoding.EncodeToString(salt),
	})
	channel, err := newSessionCipher(controller, deviceID, string(mirror))
	if err != nil {
		t.Fatal(err)
	}
	return string(greeting), channel
}

func sendController(t *testing.T, relay *websocket.Conn, connectionID, payload string) {
	t.Helper()
	if err := relay.WriteMessage(websocket.TextMessage, mustJSON(t, gatewayMessage{
		Type: "controller_message", ConnectionID: connectionID,
		Scopes: []account.RemoteCapability{account.RemoteDesktop}, Payload: payload,
	})); err != nil {
		t.Fatal(err)
	}
}

func TestControllerSessionsSurviveADeviceReconnect(t *testing.T) {
	url, rounds := fakeRelay(t)
	device, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	deviceID := strings.Repeat("d", 64)
	saved := &identity{DeviceID: deviceID, DeviceCredential: strings.Repeat("e", 64)}
	presence := &presenceLog{}
	desktop := &countingDesktop{}
	host := &Host{
		dialer: websocket.DefaultDialer, relayURL: url, token: func() string { return "token" },
		desktop: desktop, presence: presence, disconnect: make(chan controllerDisconnect),
	}
	connectionID := strings.Repeat("c", 32)
	greeting, channel := controllerHello(t, device, deviceID)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first := make(chan error, 1)
	go func() { first <- host.connect(ctx, "token", saved, device) }()
	relay := (<-rounds).device
	sendController(t, relay, connectionID, greeting)
	ready := readDirected(t, relay, channel)
	if ready["type"] != "ready" || ready["session"] == "" || ready["instance"] != host.instance() {
		t.Fatalf("ready = %+v", ready)
	}
	sessionNonce, _ := ready["session"].(string)
	relay.Close()
	<-first
	host.suspend(time.Now())

	second := make(chan error, 1)
	go func() { second <- host.connect(ctx, "token", saved, device) }()
	relay = (<-rounds).device
	defer relay.Close()
	if err := relay.WriteMessage(websocket.TextMessage, mustJSON(t, map[string]any{
		"type": "relay_hello", "controllers": []string{connectionID},
		"heartbeat": `{"type":"heartbeat"}`, "heartbeatMs": 5000,
	})); err != nil {
		t.Fatal(err)
	}
	request, err := channel.seal(controllerCommand{Version: 1, Type: "desktop.request", ID: "post-1", Method: http.MethodPost, Path: "/send", Session: sessionNonce, Seq: 1})
	if err != nil {
		t.Fatal(err)
	}
	sendController(t, relay, connectionID, request)
	answer := readDirected(t, relay, channel)
	if answer["type"] != "desktop.response" || answer["id"] != "post-1" {
		t.Fatalf("answer after reconnect = %+v", answer)
	}
	resent, err := channel.seal(controllerCommand{Version: 1, Type: "desktop.request", ID: "post-1", Method: http.MethodPost, Path: "/send", Session: sessionNonce, Seq: 2})
	if err != nil {
		t.Fatal(err)
	}
	sendController(t, relay, connectionID, resent)
	again := readDirected(t, relay, channel)
	if again["body"] != answer["body"] || desktop.calls != 1 {
		t.Fatalf("resent request ran %d times; answers %+v / %+v", desktop.calls, answer, again)
	}
	if len(presence.disconnected) != 0 {
		t.Fatalf("controller was reported gone across a reconnect: %v", presence.disconnected)
	}

	if err := relay.SetReadDeadline(time.Now().Add(8 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, beat, err := relay.ReadMessage()
	if err != nil || string(beat) != `{"type":"heartbeat"}` {
		t.Fatalf("heartbeat = %q, %v", beat, err)
	}
	cancel()
	<-second
}

func TestRelayHelloDropsControllersTheRelayNoLongerHolds(t *testing.T) {
	presence := &presenceLog{}
	host := &Host{presence: presence}
	sessions := host.adopt("device-a")
	sessions["kept"] = &controllerSession{}
	sessions["gone"] = &controllerSession{}

	host.keepControllers([]string{"kept"})

	if _, ok := sessions["kept"]; !ok || len(sessions) != 1 {
		t.Fatalf("sessions = %v", sessions)
	}
	if len(presence.disconnected) != 1 || presence.disconnected[0] != "gone" {
		t.Fatalf("disconnected = %v", presence.disconnected)
	}
}

func TestSuspendedControllersExpireAfterTheGracePeriod(t *testing.T) {
	presence := &presenceLog{}
	host := &Host{presence: presence}
	host.adopt("device-a")["phone"] = &controllerSession{}
	start := time.Now()
	host.suspend(start)
	host.suspend(start.Add(time.Minute))

	host.expireSuspended(start.Add(controllerGrace - time.Second))
	if len(host.link.sessions) != 1 {
		t.Fatal("controller dropped inside the grace period")
	}
	host.expireSuspended(start.Add(controllerGrace))
	if len(host.link.sessions) != 0 || len(presence.disconnected) != 1 {
		t.Fatalf("sessions = %v, disconnected = %v", host.link.sessions, presence.disconnected)
	}
}

func TestANewDeviceIdentityDiscardsOldSessions(t *testing.T) {
	host := &Host{}
	host.adopt("device-a")["phone"] = &controllerSession{}
	if sessions := host.adopt("device-b"); len(sessions) != 0 {
		t.Fatalf("sessions kept across identities: %v", sessions)
	}
}

func TestReplayCacheIsBounded(t *testing.T) {
	var cache replayCache
	now := time.Now()
	for index := range replayEntries + 5 {
		cache.put(string(rune('a'+index)), nil, 1, now.Add(time.Duration(index)*time.Millisecond))
	}
	if len(cache.entries) > replayEntries {
		t.Fatalf("entries = %d", len(cache.entries))
	}
	cache.put("big", nil, replayEntryBytes+1, now)
	if _, ok := cache.get("big", now); ok {
		t.Fatal("oversized answer was kept")
	}
	cache.put("old", nil, 1, now)
	if _, ok := cache.get("old", now.Add(replayTTL)); ok {
		t.Fatal("expired answer was replayed")
	}
	for index := range 5 {
		cache.put("large"+string(rune('0'+index)), nil, replayEntryBytes, now)
	}
	if cache.bytes > replayTotalBytes {
		t.Fatalf("cache holds %d bytes", cache.bytes)
	}
}

func TestRelayHelloIntervalIsClamped(t *testing.T) {
	for ms, want := range map[int]time.Duration{0: minHeartbeat, 20_000: 20 * time.Second, 10_000_000: maxHeartbeat} {
		if got := (relayHelloMessage{HeartbeatMs: ms}).interval(); got != want {
			t.Errorf("interval(%d) = %v, want %v", ms, got, want)
		}
	}
	if _, ok := parseRelayHello(mustJSON(t, map[string]any{"type": "controller_connected"})); ok {
		t.Fatal("presence frame parsed as relay hello")
	}
}

func TestBoundCommandsCannotBeReplayedIntoOrWithinASession(t *testing.T) {
	session := &controllerSession{nonce: "current"}
	if err := session.admit(controllerCommand{Session: "current", Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if err := session.admit(controllerCommand{Session: "current", Seq: 1}); err == nil {
		t.Fatal("a repeated sequence number was accepted")
	}
	if err := session.admit(controllerCommand{Session: "recorded-earlier", Seq: 9}); err == nil {
		t.Fatal("a command bound to another session was accepted")
	}
	if err := session.admit(controllerCommand{Session: "current", Seq: 5}); err != nil {
		t.Fatalf("a later command after a gap was refused: %v", err)
	}
	if err := session.admit(controllerCommand{}); err != nil {
		t.Fatalf("an unbound command from an older controller was refused: %v", err)
	}
}

func TestReplayAnswersOutliveControllerSessions(t *testing.T) {
	host := &Host{}
	host.adopt("device-a")["phone"] = &controllerSession{}
	key := replayKey("post-1", http.MethodPost, "/send", []byte("{}"))
	host.link.replay.put(key, []map[string]any{{"id": "post-1"}}, 1, time.Now())
	start := time.Now()
	host.suspend(start)
	host.expireSuspended(start.Add(controllerGrace))
	if _, ok := host.link.replay.get(key, time.Now()); !ok {
		t.Fatal("the grace period discarded an answer a reconnecting phone may still ask for")
	}
	if _, ok := host.link.replay.get(replayKey("post-1", http.MethodPost, "/send", []byte("{\"x\":1}")), time.Now()); ok {
		t.Fatal("a different request under the same id got the cached answer")
	}
	host.adopt("device-b")
	if _, ok := host.link.replay.get(key, time.Now()); ok {
		t.Fatal("answers survived a change of device identity")
	}
}

func TestRelayHandshakeCarriesTheClientIdentity(t *testing.T) {
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("User-Agent")
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err == nil {
			conn.Close()
		}
	}))
	defer server.Close()
	device, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	saved := &identity{DeviceID: strings.Repeat("d", 64), DeviceCredential: strings.Repeat("e", 64)}
	for _, tc := range []struct {
		name   string
		client *account.Client
		want   string
	}{
		{"account identity", account.New("", "reasonix-studio/9.9.9", nil), "reasonix-studio/9.9.9"},
		{"no account client", nil, provider.ClientUserAgent()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := &Host{
				client: tc.client, dialer: websocket.DefaultDialer, relayURL: "ws" + strings.TrimPrefix(server.URL, "http"),
				token: func() string { return "token" }, disconnect: make(chan controllerDisconnect),
			}
			_ = host.connect(t.Context(), "token", saved, device)
			if got := <-seen; got != tc.want {
				t.Fatalf("handshake User-Agent = %q, want %q", got, tc.want)
			}
		})
	}
}
