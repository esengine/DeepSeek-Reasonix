package browserrelay

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestMain isolates the persisted token file for every test: without this,
// Start() writes the real ~/.reasonix/browser-relay.json on the dev machine.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "browserrelay-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	os.Setenv("REASONIX_HOME", tmp)
	code := m.Run()
	os.Unsetenv("REASONIX_HOME")
	os.Exit(code)
}

func TestNewServer(t *testing.T) {
	s := NewServer()
	if s == nil {
		t.Fatal("NewServer() returned nil")
	}
	if s.state != StateDisconnected {
		t.Fatalf("initial state = %v, want disconnected", s.state)
	}
}

func TestServerStartStop(t *testing.T) {
	s := NewServer()
	ctx := context.Background()

	addr, err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	if addr == "" {
		t.Fatal("Start() returned empty address")
	}

	status := s.Status()
	if !status.Running {
		t.Fatal("server should be running after Start()")
	}
	if status.State != "disconnected" {
		t.Fatalf("state = %q, want disconnected", status.State)
	}
	if status.Addr != addr {
		t.Fatalf("addr = %q, want %q", status.Addr, addr)
	}

	token := s.Token()
	if len(token) < 32 {
		t.Fatalf("token too short: %d chars", len(token))
	}

	// Stop and verify.
	s.Stop()
	status = s.Status()
	if status.Running {
		t.Fatal("server should not be running after Stop()")
	}
}

func TestTokenGeneration(t *testing.T) {
	for i := 0; i < 3; i++ {
		s := NewServer()
		ctx := context.Background()
		_, err := s.Start(ctx)
		if err != nil {
			t.Fatalf("Start() failed: %v", err)
		}
		tok := s.Token()
		if len(tok) < 32 {
			t.Fatalf("token too short: %d chars", len(tok))
		}
		s.Stop()
	}
}

func TestSendBeforeAuthorized(t *testing.T) {
	s := NewServer()
	ctx := context.Background()
	_, err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer s.Stop()

	_, err = s.Send(ctx, "Page.navigate", json.RawMessage(`{"url":"about:blank"}`))
	if err == nil {
		t.Fatal("Send() should fail when not authorized")
	}
}

func TestDoubleStop(t *testing.T) {
	s := NewServer()
	ctx := context.Background()
	_, err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	s.Stop()
	s.Stop() // must not panic
}

// TestExtensionAuth verifies the full extension auth flow over real WebSocket.
func TestExtensionAuth(t *testing.T) {
	s := NewServer()
	ctx := context.Background()
	addr, err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer s.Stop()

	// Connect like an extension would.
	u := url.URL{Scheme: "ws", Host: addr, Path: "/"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Server should see us as "connected".
	status := s.Status()
	if status.State != "connected" {
		t.Fatalf("state = %q, want connected", status.State)
	}

	// Send auth with wrong token.
	writeMsg(t, conn, message{Type: "auth", Token: "wrong-token"})
	var resp message
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("read auth response: %v", err)
	}
	if resp.Type != "auth_error" {
		t.Fatalf("response type = %q, want auth_error", resp.Type)
	}

	// Wait for connection to close after failed auth.
	conn.Close()

	// Reconnect with correct token.
	conn2, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("re-dial failed: %v", err)
	}
	defer conn2.Close()

	writeMsg(t, conn2, message{Type: "auth", Token: s.Token(), Info: "test-extension-v1"})

	var authResp message
	if err := conn2.ReadJSON(&authResp); err != nil {
		t.Fatalf("read auth response: %v", err)
	}
	if authResp.Type != "auth_ok" {
		t.Fatalf("response type = %q, want auth_ok", authResp.Type)
	}

	// Server should now be authorized.
	status = s.Status()
	if status.State != "authorized" {
		t.Fatalf("state = %q, want authorized", status.State)
	}
	if status.ExtensionInfo != "test-extension-v1" {
		t.Fatalf("extension info = %q, want test-extension-v1", status.ExtensionInfo)
	}
}

// TestRotateToken verifies token rotation: a new token is persisted to disk,
// the old session is dropped, and the previous token no longer authorizes.
func TestRotateToken(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir()) // isolate the persisted token file

	s := NewServer()
	ctx := context.Background()
	addr, err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer s.Stop()

	old := s.Token()
	if old == "" {
		t.Fatal("token empty after start")
	}

	// Connect and authorize like an extension would.
	u := url.URL{Scheme: "ws", Host: addr, Path: "/"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	writeMsg(t, conn, message{Type: "auth", Token: old, Info: "test-extension-v1"})
	var authResp message
	if err := conn.ReadJSON(&authResp); err != nil {
		t.Fatalf("read auth response: %v", err)
	}
	if authResp.Type != "auth_ok" {
		t.Fatalf("response type = %q, want auth_ok", authResp.Type)
	}

	// Rotate the token.
	rotated, err := s.RotateToken()
	if err != nil {
		t.Fatalf("RotateToken() failed: %v", err)
	}
	if rotated == "" || rotated == old {
		t.Fatalf("rotated token = %q, want non-empty and different from %q", rotated, old)
	}
	if s.Token() != rotated {
		t.Fatalf("server token = %q, want %q", s.Token(), rotated)
	}

	// The old connection must have been dropped and state reset.
	if st := s.Status(); st.State != "disconnected" {
		t.Fatalf("state = %q, want disconnected after rotation", st.State)
	}

	// The persisted file must contain the new token.
	data, err := os.ReadFile(tokenFilePath())
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	var stored struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("parse token file: %v", err)
	}
	if stored.Token != rotated {
		t.Fatalf("persisted token = %q, want %q", stored.Token, rotated)
	}

	// A fresh connection using the old token must be rejected.
	conn2, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("re-dial failed: %v", err)
	}
	defer conn2.Close()

	writeMsg(t, conn2, message{Type: "auth", Token: old})
	if err := conn2.ReadJSON(&authResp); err != nil {
		t.Fatalf("read auth response: %v", err)
	}
	if authResp.Type != "auth_error" {
		t.Fatalf("response type = %q, want auth_error", authResp.Type)
	}
}

func TestCDPCommandFlow(t *testing.T) {
	s := NewServer()
	ctx := context.Background()
	addr, err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer s.Stop()

	// Connect and authorize.
	u := url.URL{Scheme: "ws", Host: addr, Path: "/"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	writeMsg(t, conn, message{Type: "auth", Token: s.Token()})
	var authResp message
	if err := conn.ReadJSON(&authResp); err != nil {
		t.Fatalf("read auth response: %v", err)
	}

	// Send a CDP command from the AI side.
	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := s.Send(ctx, "Runtime.evaluate", nil)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- string(result)
	}()

	// Extension receives the CDP command.
	var cmdMsg message
	if err := conn.ReadJSON(&cmdMsg); err != nil {
		t.Fatalf("read CDP command: %v", err)
	}
	if cmdMsg.Type != "cdp_command" {
		t.Fatalf("message type = %q, want cdp_command", cmdMsg.Type)
	}
	if cmdMsg.Method != "Runtime.evaluate" {
		t.Fatalf("method = %q, want Runtime.evaluate", cmdMsg.Method)
	}

	// Extension sends back a result.
	writeMsg(t, conn, message{
		Type:   "cdp_result",
		ID:     cmdMsg.ID,
		Result: []byte(`{"result":{"type":"string","value":"hello"}}`),
	})

	select {
	case result := <-resultCh:
		if result == "" {
			t.Fatal("got empty result")
		}
	case err := <-errCh:
		t.Fatalf("Send() failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Send() timed out")
	}
}

func TestCDPErrorFlow(t *testing.T) {
	s := NewServer()
	ctx := context.Background()
	addr, err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer s.Stop()

	// Connect and authorize.
	u := url.URL{Scheme: "ws", Host: addr, Path: "/"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	writeMsg(t, conn, message{Type: "auth", Token: s.Token()})
	var authResp message
	if err := conn.ReadJSON(&authResp); err != nil {
		t.Fatalf("read auth response: %v", err)
	}

	// Send a CDP command.
	resultCh := make(chan error, 1)
	go func() {
		_, err := s.Send(ctx, "Page.navigate", nil)
		resultCh <- err
	}()

	// Receive command.
	var cmdMsg message
	if err := conn.ReadJSON(&cmdMsg); err != nil {
		t.Fatalf("read CDP command: %v", err)
	}

	// Extension sends back an error.
	writeMsg(t, conn, message{
		Type:  "cdp_result",
		ID:    cmdMsg.ID,
		Error: "navigation failed: blocked",
	})

	select {
	case err := <-resultCh:
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "command error: navigation failed: blocked" {
			t.Fatalf("error = %q, want command error: navigation failed: blocked", err.Error())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send() timed out")
	}
}

func TestExtensionDisconnectFailsPending(t *testing.T) {
	s := NewServer()
	ctx := context.Background()
	addr, err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer s.Stop()

	// Connect and authorize.
	u := url.URL{Scheme: "ws", Host: addr, Path: "/"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	writeMsg(t, conn, message{Type: "auth", Token: s.Token()})
	var authResp message
	if err := conn.ReadJSON(&authResp); err != nil {
		t.Fatalf("read auth response: %v", err)
	}

	// Send a CDP command (will hang waiting for response).
	resultCh := make(chan error, 1)
	go func() {
		_, err := s.Send(ctx, "Runtime.evaluate", nil)
		resultCh <- err
	}()

	// Receive the command so it's pending.
	var cmdMsg message
	if err := conn.ReadJSON(&cmdMsg); err != nil {
		t.Fatalf("read CDP command: %v", err)
	}

	// Extension disconnects.
	conn.Close()

	// Pending command should fail.
	select {
	case err := <-resultCh:
		if err == nil {
			t.Fatal("expected error after disconnect, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send() did not return after disconnect")
	}

	// Server should be disconnected.
	status := s.Status()
	if status.State != "disconnected" {
		t.Fatalf("state = %q, want disconnected", status.State)
	}
}

func TestCDPResultWithNonObjectValue(t *testing.T) {
	// Simulates the browser_click coordinate extraction scenario: the extension
	// returns a Runtime.evaluate result whose value is a primitive (e.g. null,
	// undefined, or a string) rather than the expected {x, y} object. The server
	// must propagate the raw result faithfully so the tool layer can detect the
	// mismatch and return an error (rather than silently pretending the click
	// succeeded).
	s := NewServer()
	ctx := context.Background()
	addr, err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer s.Stop()

	u := url.URL{Scheme: "ws", Host: addr, Path: "/"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	writeMsg(t, conn, message{Type: "auth", Token: s.Token()})
	var authResp message
	if err := conn.ReadJSON(&authResp); err != nil {
		t.Fatalf("read auth response: %v", err)
	}

	// Send a Runtime.evaluate from the AI side.
	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := s.Send(ctx, "Runtime.evaluate", nil)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- string(result)
	}()

	var cmdMsg message
	if err := conn.ReadJSON(&cmdMsg); err != nil {
		t.Fatalf("read CDP command: %v", err)
	}

	// Extension returns a result where the value is a string, not an object.
	// This is what happens when document.querySelector returns null.
	writeMsg(t, conn, message{
		Type:   "cdp_result",
		ID:     cmdMsg.ID,
		Result: []byte(`{"result":{"type":"string","value":"element not found"}}`),
	})

	select {
	case result := <-resultCh:
		if result == "" {
			t.Fatal("got empty result")
		}
		// Verify the raw JSON is preserved — tool layer must parse and detect.
		if !strings.Contains(result, `"element not found"`) {
			t.Fatalf("result does not contain expected value: %s", result)
		}
	case err := <-errCh:
		t.Fatalf("Send() should not error on non-object value: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Send() timed out")
	}
}

func writeMsg(t *testing.T, conn *websocket.Conn, msg message) {
	t.Helper()
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("write message: %v", err)
	}
}
