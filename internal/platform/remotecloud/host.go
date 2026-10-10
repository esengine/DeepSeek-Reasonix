// Package remotecloud keeps a signed-in Studio reachable through the Reasonix
// message relay. Payloads are encrypted between the controller and Studio; the
// relay sees only routing identities, capability scopes and ciphertext.
package remotecloud

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/account"
)

const DefaultRelayURL = "wss://remote.reasonix.io"

const desktopResponseChunk = 24 << 10

var (
	errAccountChanged = errors.New("remote cloud: account changed")
	errRelayRefused   = errors.New("remote cloud: relay refused the connection")
)

type unavailableReason string

const (
	unavailableSignedOut        unavailableReason = "signed_out"
	unavailableRelayUnreachable unavailableReason = "relay_unreachable"
	unavailableRelayRefused     unavailableReason = "relay_refused"
)

type Status struct {
	DeviceID string            `json:"deviceId,omitempty"`
	Name     string            `json:"name,omitempty"`
	Online   bool              `json:"online"`
	Error    string            `json:"error,omitempty"`
	Reason   unavailableReason `json:"reason,omitempty"`
}

type hostState struct {
	status Status
}

type Host struct {
	client     *account.Client
	dialer     *websocket.Dialer
	relayURL   string
	version    string
	name       string
	token      func() string
	tasks      TaskService
	desktop    DesktopService
	presence   ControllerPresence
	disconnect chan controllerDisconnect

	link relayLink

	mu    sync.RWMutex
	state hostState
}

type controllerDisconnect struct {
	id   string
	done chan error
}

type TaskService interface {
	CloudTasks(context.Context) (any, error)
	CloudTask(context.Context, string) (any, error)
	CloudSubmit(context.Context, string, string, string, int) error
}

type DesktopRequest struct {
	Method  string
	Path    string
	Body    []byte
	Ordinal int
}

type DesktopResponse struct {
	Status      int
	ContentType string
	ETag        string
	Body        []byte
}

type DesktopService interface {
	CloudDesktop(context.Context, DesktopRequest, string) (DesktopResponse, error)
}

type ControllerPresence interface {
	CloudControllerConnected(string, string) int
	CloudControllerSeen(string)
	CloudControllerDisconnected(string)
}

type controllerSession struct {
	cipher  *sessionCipher
	ordinal int
	nonce   string
	lastSeq int64
}

func New(client *account.Client, dialer *websocket.Dialer, relayURL, version string, tasks ...TaskService) *Host {
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	if strings.TrimSpace(relayURL) == "" {
		relayURL = DefaultRelayURL
	}
	name, _ := os.Hostname()
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Reasonix Studio"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	host := &Host{
		client: client, dialer: dialer, relayURL: strings.TrimRight(relayURL, "/"),
		version: version, name: name, token: account.Token, disconnect: make(chan controllerDisconnect),
	}
	if len(tasks) > 0 {
		host.tasks = tasks[0]
		host.desktop, _ = tasks[0].(DesktopService)
		host.presence, _ = tasks[0].(ControllerPresence)
	}
	return host
}

// DisconnectController asks the relay to close one authenticated controller.
// The live connection owns WebSocket writes, so callers hand the request to it
// instead of writing concurrently from the local HTTP handler.
func (h *Host) DisconnectController(id string) error {
	id = strings.TrimSpace(id)
	if len(id) != 32 {
		return errors.New("remote cloud: invalid controller identity")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return errors.New("remote cloud: invalid controller identity")
	}
	request := controllerDisconnect{id: id, done: make(chan error, 1)}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case h.disconnect <- request:
	case <-timer.C:
		return errors.New("remote cloud: device is offline")
	}
	select {
	case err := <-request.done:
		return err
	case <-timer.C:
		return errors.New("remote cloud: disconnect timed out")
	}
}

func (h *Host) Status() Status {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.state.status
}

func (h *Host) publish(status Status) {
	h.mu.Lock()
	h.state.status = status
	h.mu.Unlock()
}

func (h *Host) Run(ctx context.Context) {
	defer h.dropControllers()
	backoff := time.Second
	for ctx.Err() == nil {
		h.expireSuspended(time.Now())
		token := strings.TrimSpace(h.token())
		if token == "" {
			h.dropControllers()
			h.publish(Status{Reason: unavailableSignedOut})
			if !wait(ctx, time.Second) {
				return
			}
			continue
		}
		saved, private, err := h.ensureIdentity(ctx, token)
		if err == nil {
			err = h.connect(ctx, token, saved, private)
		}
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errAccountChanged) {
			h.dropControllers()
			h.link.replay = replayCache{}
		}
		h.suspend(time.Now())
		status := h.Status()
		wasOnline := status.Online
		status.Online = false
		status.Error = err.Error()
		status.Reason = unavailableReasonFor(err)
		h.publish(status)
		if wasOnline {
			backoff = time.Second
		}
		if !wait(ctx, backoff) {
			return
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func unavailableReasonFor(err error) unavailableReason {
	if errors.Is(err, account.ErrUnauthorized) {
		return unavailableSignedOut
	}
	if errors.Is(err, errRelayRefused) {
		return unavailableRelayRefused
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) && closeErr.Code == websocket.ClosePolicyViolation {
		return unavailableRelayRefused
	}
	return unavailableRelayUnreachable
}

func (h *Host) ensureIdentity(ctx context.Context, token string) (*identity, *ecdh.PrivateKey, error) {
	user, err := h.client.Me(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	saved, err := loadIdentity()
	if err == nil && saved != nil && saved.OwnerID == user.ID {
		private, keyErr := privateKey(saved)
		if keyErr == nil && h.identityCurrent(ctx, token, saved) {
			return saved, private, nil
		}
		if keyErr == nil {
			return h.registerIdentity(ctx, token, user.ID, private)
		}
	}
	private, public, err := generatePrivateKey()
	if err != nil {
		return nil, nil, err
	}
	return h.registerIdentityWithPublic(ctx, token, user.ID, private, public)
}

func (h *Host) identityCurrent(ctx context.Context, token string, saved *identity) bool {
	devices, err := h.client.RemoteDevices(ctx, token)
	if err != nil {
		return true
	}
	for _, device := range devices {
		if device.ID == saved.DeviceID {
			return slices.Contains(device.Capabilities, account.RemoteDesktop)
		}
	}
	return false
}

func (h *Host) registerIdentity(ctx context.Context, token string, ownerID int64, private *ecdh.PrivateKey) (*identity, *ecdh.PrivateKey, error) {
	public := base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes())
	return h.registerIdentityWithPublic(ctx, token, ownerID, private, public)
}

func (h *Host) registerIdentityWithPublic(ctx context.Context, token string, ownerID int64, private *ecdh.PrivateKey, public string) (*identity, *ecdh.PrivateKey, error) {
	registered, err := h.client.RegisterRemoteDevice(ctx, token, account.RemoteDeviceRegistration{
		Name: h.name, Platform: platformName(runtime.GOOS), PublicKey: public,
		Capabilities: []account.RemoteCapability{
			account.RemoteTasks, account.RemoteLogs, account.RemoteFiles, account.RemoteDesktop,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	saved := &identity{
		OwnerID: ownerID, DeviceID: registered.Device.ID,
		DeviceCredential: registered.DeviceCredential,
		PrivateKey:       base64.RawURLEncoding.EncodeToString(private.Bytes()),
	}
	if err := saveIdentity(saved); err != nil {
		return nil, nil, err
	}
	return saved, private, nil
}

func platformName(goos string) string {
	if goos == "darwin" {
		return "macos"
	}
	return goos
}

type gatewayMessage struct {
	Type         string                     `json:"type"`
	ConnectionID string                     `json:"connectionId"`
	Scopes       []account.RemoteCapability `json:"scopes"`
	Payload      string                     `json:"payload"`
}

type controllerCommand struct {
	Version int    `json:"v"`
	Type    string `json:"type"`
	ID      string `json:"id"`
	TaskID  string `json:"taskId,omitempty"`
	Text    string `json:"text,omitempty"`
	Method  string `json:"method,omitempty"`
	Path    string `json:"path,omitempty"`
	Body    string `json:"body,omitempty"`
	Session string `json:"session,omitempty"`
	Seq     int64  `json:"seq,omitempty"`
}

func (h *Host) connect(ctx context.Context, token string, saved *identity, private *ecdh.PrivateKey) error {
	url := h.relayURL + "/v1/devices/" + saved.DeviceID + "/connect"
	headers := http.Header{
		"Authorization": []string{"Bearer " + saved.DeviceCredential},
		"User-Agent":    []string{h.userAgent()},
	}
	conn, response, err := h.dialer.DialContext(ctx, url, headers)
	if err != nil {
		return classifyRelayDialError(response, err)
	}
	defer conn.Close()
	conn.SetReadLimit(64 << 10)
	h.publish(Status{DeviceID: saved.DeviceID, Name: h.name, Online: true})

	done := make(chan struct{})
	defer close(done)
	messages := make(chan []byte, 1)
	readErr := make(chan error, 1)
	go func() {
		for {
			kind, payload, err := conn.ReadMessage()
			if err != nil {
				select {
				case readErr <- err:
				case <-done:
				}
				return
			}
			if kind != websocket.TextMessage {
				select {
				case readErr <- errors.New("remote cloud: relay sent a binary message"):
				case <-done:
				}
				return
			}
			select {
			case messages <- payload:
			case <-done:
				return
			}
		}
	}()

	sessions := h.adopt(saved.DeviceID)
	pingTicker := time.NewTicker(20 * time.Second)
	tokenTicker := time.NewTicker(time.Second)
	heartbeat := ""
	defer pingTicker.Stop()
	defer tokenTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			return err
		case <-tokenTicker.C:
			if strings.TrimSpace(h.token()) != token {
				return errAccountChanged
			}
		case <-pingTicker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return err
			}
			if heartbeat != "" {
				if err := conn.WriteMessage(websocket.TextMessage, []byte(heartbeat)); err != nil {
					return err
				}
			}
		case request := <-h.disconnect:
			if _, ok := sessions[request.id]; !ok {
				request.done <- errors.New("remote cloud: controller is no longer connected")
				continue
			}
			wire, err := json.Marshal(map[string]string{
				"type": "disconnect_controller", "connectionId": request.id,
			})
			if err == nil {
				err = conn.WriteMessage(websocket.TextMessage, wire)
			}
			if err == nil {
				h.forgetController(request.id)
			}
			request.done <- err
		case payload := <-messages:
			if hello, ok := parseRelayHello(payload); ok {
				h.keepControllers(hello.Controllers)
				if hello.Heartbeat != "" {
					heartbeat = hello.Heartbeat
					pingTicker.Reset(hello.interval())
				}
				continue
			}
			if err := h.handle(ctx, conn, saved, private, sessions, payload); err != nil {
				continue
			}
		}
	}
}

func classifyRelayDialError(response *http.Response, err error) error {
	if response == nil {
		return err
	}
	if response.StatusCode == http.StatusUnauthorized {
		_ = clearIdentity()
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		return fmt.Errorf("%w: %w", errRelayRefused, err)
	}
	return err
}

func (h *Host) handle(
	ctx context.Context,
	conn *websocket.Conn,
	saved *identity,
	private *ecdh.PrivateKey,
	sessions map[string]*controllerSession,
	payload []byte,
) error {
	var message gatewayMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return err
	}
	switch message.Type {
	case "controller_disconnected":
		if h.presence != nil {
			h.presence.CloudControllerDisconnected(message.ConnectionID)
		}
		delete(sessions, message.ConnectionID)
		return nil
	case "controller_connected", "heartbeat_ack":
		return nil
	case "controller_message":
	default:
		return errors.New("remote cloud: unknown relay message")
	}
	session := sessions[message.ConnectionID]
	if session == nil {
		created, err := newSessionCipher(private, saved.DeviceID, message.Payload)
		if err != nil {
			return err
		}
		ordinal := 0
		if h.presence != nil {
			ordinal = h.presence.CloudControllerConnected(message.ConnectionID, "Web Studio")
		}
		nonce, err := randomToken()
		if err != nil {
			return err
		}
		sessions[message.ConnectionID] = &controllerSession{cipher: created, ordinal: ordinal, nonce: nonce}
		ready, err := created.seal(map[string]any{
			"v": protocolVersion, "type": "ready", "deviceId": saved.DeviceID,
			"name": h.name, "platform": platformName(runtime.GOOS), "version": h.version,
			"features": []string{"replay"}, "session": nonce, "instance": h.instance(),
		})
		if err != nil {
			return err
		}
		return writeDirected(conn, message.ConnectionID, ready)
	}
	var command controllerCommand
	if h.presence != nil {
		h.presence.CloudControllerSeen(message.ConnectionID)
	}
	if err := session.cipher.open(message.Payload, &command); err != nil {
		return err
	}
	if command.Version != protocolVersion || command.ID == "" {
		return errors.New("remote cloud: unsupported controller command")
	}
	if err := session.admit(command); err != nil {
		return err
	}
	var response map[string]any
	switch command.Type {
	case "ping":
		response = map[string]any{"v": protocolVersion, "type": "pong", "id": command.ID, "at": time.Now().UTC().Format(time.RFC3339Nano)}
	case "tasks.list", "tasks.get", "tasks.send":
		response = h.taskCommand(ctx, "cloud:"+message.ConnectionID, session.ordinal, message.Scopes, command)
	case "desktop.request":
		return h.desktopCommand(ctx, conn, session.cipher, "cloud:"+message.ConnectionID, session.ordinal, message.ConnectionID, message.Scopes, command)
	default:
		return errors.New("remote cloud: unsupported controller command")
	}
	reply, err := session.cipher.seal(response)
	if err != nil {
		return err
	}
	return writeDirected(conn, message.ConnectionID, reply)
}

func (h *Host) desktopCommand(
	ctx context.Context,
	conn *websocket.Conn,
	session *sessionCipher,
	deviceID string,
	ordinal int,
	connectionID string,
	scopes []account.RemoteCapability,
	command controllerCommand,
) error {
	if h.desktop == nil || !hasScope(scopes, account.RemoteDesktop) {
		return h.writeDesktopError(conn, session, connectionID, command.ID, "desktop access is unavailable")
	}
	body, err := base64.RawURLEncoding.DecodeString(command.Body)
	if err != nil {
		return h.writeDesktopError(conn, session, connectionID, command.ID, "desktop request body is invalid")
	}
	replay := replayable(command.Method)
	key := replayKey(command.ID, command.Method, command.Path, body)
	if replay {
		if payloads, ok := h.link.replay.get(key, time.Now()); ok {
			return writeSealed(conn, session, connectionID, payloads)
		}
	}
	response, err := h.desktop.CloudDesktop(ctx, DesktopRequest{
		Method: command.Method, Path: command.Path, Body: body, Ordinal: ordinal,
	}, deviceID)
	if err != nil {
		payloads := []map[string]any{{"v": protocolVersion, "type": "error", "id": command.ID, "error": err.Error()}}
		if replay {
			h.link.replay.put(key, payloads, len(err.Error()), time.Now())
		}
		return writeSealed(conn, session, connectionID, payloads)
	}
	payloads := desktopPayloads(command.ID, response)
	if replay {
		h.link.replay.put(key, payloads, len(response.Body), time.Now())
	}
	return writeSealed(conn, session, connectionID, payloads)
}

func desktopPayloads(id string, response DesktopResponse) []map[string]any {
	chunks := max((len(response.Body)+desktopResponseChunk-1)/desktopResponseChunk, 1)
	payloads := make([]map[string]any, 0, chunks)
	for index := range chunks {
		start := index * desktopResponseChunk
		end := min(start+desktopResponseChunk, len(response.Body))
		chunk := ""
		if start < len(response.Body) {
			chunk = base64.RawURLEncoding.EncodeToString(response.Body[start:end])
		}
		payloads = append(payloads, map[string]any{
			"v": protocolVersion, "type": "desktop.response", "id": id,
			"status": response.Status, "contentType": response.ContentType, "etag": response.ETag,
			"index": index, "done": index == chunks-1, "body": chunk,
		})
	}
	return payloads
}

func writeSealed(conn *websocket.Conn, session *sessionCipher, connectionID string, payloads []map[string]any) error {
	for _, payload := range payloads {
		reply, err := session.seal(payload)
		if err != nil {
			return err
		}
		if err := writeDirected(conn, connectionID, reply); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) writeDesktopError(conn *websocket.Conn, session *sessionCipher, connectionID, id, message string) error {
	reply, err := session.seal(map[string]any{
		"v": protocolVersion, "type": "error", "id": id, "error": message,
	})
	if err != nil {
		return err
	}
	return writeDirected(conn, connectionID, reply)
}

func (h *Host) taskCommand(ctx context.Context, deviceID string, ordinal int, scopes []account.RemoteCapability, command controllerCommand) map[string]any {
	response := map[string]any{"v": protocolVersion, "id": command.ID}
	if h.tasks == nil || !hasScope(scopes, account.RemoteTasks) {
		response["type"] = "error"
		response["error"] = "tasks access is unavailable"
		return response
	}
	var value any
	var err error
	switch command.Type {
	case "tasks.list":
		value, err = h.tasks.CloudTasks(ctx)
		response["type"] = "tasks.list"
		response["tasks"] = value
	case "tasks.get":
		value, err = h.tasks.CloudTask(ctx, command.TaskID)
		response["type"] = "tasks.get"
		response["snapshot"] = value
	case "tasks.send":
		err = h.tasks.CloudSubmit(ctx, command.TaskID, command.Text, deviceID, ordinal)
		response["type"] = "tasks.sent"
	}
	if err != nil {
		response["type"] = "error"
		response["error"] = err.Error()
	}
	return response
}

func hasScope(scopes []account.RemoteCapability, want account.RemoteCapability) bool {
	return slices.Contains(scopes, want)
}

func writeDirected(conn *websocket.Conn, connectionID, payload string) error {
	wire, err := json.Marshal(map[string]string{"to": connectionID, "payload": payload})
	if err != nil {
		return err
	}
	if len(wire) > 64<<10 {
		return fmt.Errorf("remote cloud: reply exceeds relay limit")
	}
	return conn.WriteMessage(websocket.TextMessage, wire)
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// userAgent is the identity the account client already signs in with, so the
// relay handshake and the sign-in reach the same edge as the same caller.
func (h *Host) userAgent() string {
	if h.client != nil && h.client.UserAgent != "" {
		return h.client.UserAgent
	}
	return provider.ClientUserAgent()
}
