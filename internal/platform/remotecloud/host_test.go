package remotecloud

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"reasonix/internal/platform/account"
)

type taskStub struct {
	sentTask string
	sentText string
	ordinal  int
}

type presenceStub struct {
	connected    string
	seen         string
	disconnected string
}

func (s *presenceStub) CloudControllerConnected(id, _ string) int { s.connected = id; return 4 }
func (s *presenceStub) CloudControllerSeen(id string)             { s.seen = id }
func (s *presenceStub) CloudControllerDisconnected(id string)     { s.disconnected = id }

type desktopStub struct {
	body []byte
}

func (s *desktopStub) CloudDesktop(_ context.Context, request DesktopRequest, deviceID string) (DesktopResponse, error) {
	if request.Method != http.MethodGet || request.Path != "/history" || request.Ordinal != 4 || deviceID == "" {
		return DesktopResponse{}, errors.New("unexpected desktop request")
	}
	return DesktopResponse{Status: http.StatusOK, ContentType: "application/json", Body: s.body}, nil
}

func (s *taskStub) CloudTasks(context.Context) (any, error) {
	return []map[string]string{{"id": "r1", "name": "project"}}, nil
}
func (s *taskStub) CloudTask(context.Context, string) (any, error) { return nil, nil }
func (s *taskStub) CloudSubmit(_ context.Context, task, text, _ string, ordinal int) error {
	s.sentTask, s.sentText, s.ordinal = task, text, ordinal
	return nil
}

func TestTaskCommandsRequireScopeAndReachTypedBackend(t *testing.T) {
	stub := &taskStub{}
	host := &Host{tasks: stub}
	denied := host.taskCommand(t.Context(), "device", 3, nil, controllerCommand{Type: "tasks.list", ID: "1"})
	if denied["type"] != "error" {
		t.Fatalf("unscoped response = %+v", denied)
	}
	response := host.taskCommand(t.Context(), "device", 3, []account.RemoteCapability{account.RemoteTasks}, controllerCommand{
		Type: "tasks.send", ID: "2", TaskID: "r1", Text: "run tests",
	})
	if response["type"] != "tasks.sent" || stub.sentTask != "r1" || stub.sentText != "run tests" || stub.ordinal != 3 {
		t.Fatalf("response = %+v, stub = %+v", response, stub)
	}
}

func TestDisconnectControllerHandsRequestToLiveConnection(t *testing.T) {
	host := &Host{disconnect: make(chan controllerDisconnect)}
	id := strings.Repeat("c", 32)
	go func() {
		request := <-host.disconnect
		if request.id != id {
			request.done <- errors.New("wrong controller")
			return
		}
		request.done <- nil
	}()
	if err := host.DisconnectController(id); err != nil {
		t.Fatal(err)
	}
	if err := host.DisconnectController("not-a-controller"); err == nil {
		t.Fatal("invalid controller identity was accepted")
	}
	if err := host.DisconnectController(strings.Repeat("g", 32)); err == nil {
		t.Fatal("non-hex controller identity was accepted")
	}
}

func TestHandshakeAndPingCrossTheDirectedEncryptedChannel(t *testing.T) {
	connections := make(chan *websocket.Conn, 1)
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		connections <- conn
		<-done
	}))
	defer server.Close()
	controllerWire, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer controllerWire.Close()
	deviceWire := <-connections
	defer func() {
		deviceWire.Close()
		close(done)
	}()

	device, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	deviceID := strings.Repeat("d", 64)
	connectionID := strings.Repeat("c", 32)
	greeting, _ := json.Marshal(hello{
		Version: protocolVersion, Type: "hello",
		PublicKey: base64.RawURLEncoding.EncodeToString(controller.PublicKey().Bytes()),
		Salt:      base64.RawURLEncoding.EncodeToString(salt),
	})
	desktopBody := bytes.Repeat([]byte("x"), desktopResponseChunk+17)
	presence := &presenceStub{}
	host := &Host{name: "Home Mac", version: "2.20.1", desktop: &desktopStub{body: desktopBody}, presence: presence}
	sessions := make(map[string]*controllerSession)
	if err := host.handle(t.Context(), deviceWire, &identity{DeviceID: deviceID}, device, sessions, mustJSON(t, gatewayMessage{
		Type: "controller_message", ConnectionID: connectionID, Payload: string(greeting),
	})); err != nil {
		t.Fatal(err)
	}
	controllerGreeting, _ := json.Marshal(hello{
		Version: protocolVersion, Type: "hello",
		PublicKey: base64.RawURLEncoding.EncodeToString(device.PublicKey().Bytes()),
		Salt:      base64.RawURLEncoding.EncodeToString(salt),
	})
	controllerCipher, err := newSessionCipher(controller, deviceID, string(controllerGreeting))
	if err != nil {
		t.Fatal(err)
	}
	ready := readDirected(t, controllerWire, controllerCipher)
	if ready["type"] != "ready" || ready["name"] != "Home Mac" {
		t.Fatalf("ready = %+v", ready)
	}
	if presence.connected != connectionID {
		t.Fatalf("connected = %q, want %q", presence.connected, connectionID)
	}

	ping, err := controllerCipher.seal(controllerCommand{Version: 1, Type: "ping", ID: "probe-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.handle(t.Context(), deviceWire, &identity{DeviceID: deviceID}, device, sessions, mustJSON(t, gatewayMessage{
		Type: "controller_message", ConnectionID: connectionID, Payload: ping,
	})); err != nil {
		t.Fatal(err)
	}
	pong := readDirected(t, controllerWire, controllerCipher)
	if pong["type"] != "pong" || pong["id"] != "probe-1" || pong["at"] == "" {
		t.Fatalf("pong = %+v", pong)
	}
	if presence.seen != connectionID {
		t.Fatalf("seen = %q, want %q", presence.seen, connectionID)
	}

	desktopRequest, err := controllerCipher.seal(controllerCommand{
		Version: 1, Type: "desktop.request", ID: "desktop-1", Method: http.MethodGet, Path: "/history",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.handle(t.Context(), deviceWire, &identity{DeviceID: deviceID}, device, sessions, mustJSON(t, gatewayMessage{
		Type: "controller_message", ConnectionID: connectionID, Scopes: []account.RemoteCapability{account.RemoteDesktop}, Payload: desktopRequest,
	})); err != nil {
		t.Fatal(err)
	}
	first := readDirected(t, controllerWire, controllerCipher)
	second := readDirected(t, controllerWire, controllerCipher)
	if first["type"] != "desktop.response" || first["done"] != false || second["done"] != true {
		t.Fatalf("desktop chunks = %+v / %+v", first, second)
	}
	firstBody, err := base64.RawURLEncoding.DecodeString(first["body"].(string))
	if err != nil {
		t.Fatal(err)
	}
	secondBody, err := base64.RawURLEncoding.DecodeString(second["body"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(firstBody, secondBody...), desktopBody) {
		t.Fatal("desktop response body changed across chunks")
	}
}

func TestRegistrationPlatformUsesTheAccountContract(t *testing.T) {
	for input, want := range map[string]string{"darwin": "macos", "windows": "windows", "linux": "linux"} {
		if got := platformName(input); got != want {
			t.Errorf("platformName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUnavailableReasonIsTypedInsteadOfReadFromErrorText(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want unavailableReason
	}{
		{"signed out", account.ErrUnauthorized, unavailableSignedOut},
		{"relay refused", fmt.Errorf("%w: bad handshake", errRelayRefused), unavailableRelayRefused},
		{"relay policy close", &websocket.CloseError{Code: websocket.ClosePolicyViolation}, unavailableRelayRefused},
		{"unreachable", errors.New("dial tcp: network is unreachable"), unavailableRelayUnreachable},
		{"same words are not an identity", errors.New("relay refused the connection"), unavailableRelayUnreachable},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := unavailableReasonFor(c.err); got != c.want {
				t.Fatalf("reason = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRelayHandshakeRefusalCarriesTheTypedIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer server.Close()
	host := &Host{dialer: websocket.DefaultDialer, relayURL: "ws" + strings.TrimPrefix(server.URL, "http")}
	err := host.connect(t.Context(), "token", &identity{DeviceID: "device", DeviceCredential: "credential"}, nil)
	if !errors.Is(err, errRelayRefused) {
		t.Fatalf("connect error = %v, want relay refusal identity", err)
	}
}

func TestRelayServerFailureRemainsUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "later", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	host := &Host{dialer: websocket.DefaultDialer, relayURL: "ws" + strings.TrimPrefix(server.URL, "http")}
	err := host.connect(t.Context(), "token", &identity{DeviceID: "device", DeviceCredential: "credential"}, nil)
	if got := unavailableReasonFor(err); got != unavailableRelayUnreachable {
		t.Fatalf("reason = %q, want %q", got, unavailableRelayUnreachable)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func readDirected(t *testing.T, conn *websocket.Conn, channel *sessionCipher) map[string]any {
	t.Helper()
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var directed struct {
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(raw, &directed); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := channel.open(directed.Payload, &body); err != nil {
		t.Fatal(err)
	}
	return body
}
