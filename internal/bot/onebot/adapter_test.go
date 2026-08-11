package onebot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reasonix/internal/bot"

	"golang.org/x/net/websocket"
)

func TestValidateEndpointRequiresTokenOutsideLoopback(t *testing.T) {
	if err := validateEndpoint("ws://127.0.0.1:3001", ""); err != nil {
		t.Fatalf("loopback endpoint rejected without token: %v", err)
	}
	if err := validateEndpoint("ws://192.0.2.10:3001", ""); err == nil {
		t.Fatal("non-loopback endpoint accepted without token")
	}
	if err := validateEndpoint("ws://192.0.2.10:3001", "secret"); err == nil {
		t.Fatal("public plaintext websocket accepted")
	}
	if err := validateEndpoint("wss://192.0.2.10:3001", "secret"); err != nil {
		t.Fatalf("secure non-loopback endpoint rejected: %v", err)
	}
}

func TestAppendOneBotMediaParsesURLAndBase64Segments(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte("voice"))
	var msg bot.InboundMessage
	appendOneBotMedia(&msg, json.RawMessage(`[{"type":"image","data":{"url":"https://cdn.example/image.png"}},{"type":"record","data":{"file":"base64://`+data+`","name":"reply.wav"}}]`))
	if len(msg.MediaURLs) != 1 || msg.MediaURLs[0] != "https://cdn.example/image.png" {
		t.Fatalf("media urls = %+v", msg.MediaURLs)
	}
	if len(msg.Media) != 1 || msg.Media[0].Kind != "audio" || string(msg.Media[0].Data) != "voice" {
		t.Fatalf("media = %+v", msg.Media)
	}
}

func TestConnectAndServeStartsReaderBeforeCapabilityRPC(t *testing.T) {
	actions := make(chan string, 2)
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()
		decoder := json.NewDecoder(ws)
		encoder := json.NewEncoder(ws)
		for {
			var request struct {
				Action string `json:"action"`
				Echo   string `json:"echo"`
			}
			if err := decoder.Decode(&request); err != nil {
				return
			}
			actions <- request.Action
			data := map[string]any{}
			if request.Action == "get_login_info" {
				data["user_id"] = 123456789
			}
			_ = encoder.Encode(map[string]any{"status": "ok", "retcode": 0, "echo": request.Echo, "data": data})
			if request.Action == "get_login_info" {
				return
			}
		}
	}))
	defer srv.Close()

	a := &adapter{cfg: Config{WebSocketURL: "ws" + strings.TrimPrefix(srv.URL, "http")}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.connectAndServe(ctx); err == nil {
		t.Fatal("connectAndServe returned nil after server close")
	}
	close(actions)
	var got []string
	for action := range actions {
		got = append(got, action)
	}
	if len(got) != 2 || got[0] != "get_version_info" || got[1] != "get_login_info" {
		t.Fatalf("capability actions = %v, want version/login probes", got)
	}
	if selfID := a.currentSelfID(); selfID != "123456789" {
		t.Fatalf("self id = %q, want login account id", selfID)
	}
}

func TestOneBotEventAcceptsNumericAndStringIDs(t *testing.T) {
	var numeric oneBotEvent
	if err := json.Unmarshal([]byte(`{"post_type":"message","message_type":"group","user_id":123,"group_id":456,"message_id":789}`), &numeric); err != nil {
		t.Fatalf("numeric ids: %v", err)
	}
	if numeric.UserID.String() != "123" || numeric.GroupID.String() != "456" || numeric.MessageID.String() != "789" {
		t.Fatalf("numeric event = %+v", numeric)
	}
	var quoted oneBotEvent
	if err := json.Unmarshal([]byte(`{"user_id":"123","group_id":"456","message_id":"789"}`), &quoted); err != nil {
		t.Fatalf("quoted ids: %v", err)
	}
	if quoted.UserID.String() != "123" || quoted.GroupID.String() != "456" || quoted.MessageID.String() != "789" {
		t.Fatalf("quoted event = %+v", quoted)
	}
}
