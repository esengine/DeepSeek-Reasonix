package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// normalizeSTTLang 归一化识别语言，空值/非法值回退 zh-CN。
func TestNormalizeSTTLang(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "zh-CN"},
		{"zh", "zh-CN"},
		{"zh-CN", "zh-CN"},
		{"zh-TW", "zh-CN"}, // 中文变体归一化到 zh-CN（沿用源项目行为）
		{"en", "en-US"},
		{"en-US", "en-US"},
		{"en-GB", "en-US"},
		{"EN-us", "en-US"},
		{"ja-JP", "ja-JP"}, // 未知语言原样保留
		{"  fr-FR  ", "fr-FR"},
	}
	for _, tc := range cases {
		if got := normalizeSTTLang(tc.in); got != tc.want {
			t.Errorf("normalizeSTTLang(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// runBridgeOnServer 在真实 httptest 服务器上挂载 bridge.handleWS，返回测试
// 服务器与渠道。这样 bridge 的 WebSocket 服务端语义（浏览器->桥接转发）被
// 端到端验证：转录/状态事件经 emit 回调发布。
func runBridgeOnServer(t *testing.T, b *sttBridge) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.handleWS(w, r)
	}))
}

// 浏览器识别页经 WS 上报 final transcript 与 listening 状态，桥接层必须
// 原样转发给 emit（前端 onSTTTranscript/onSTTState 订阅）。
func TestSTTBridgeForwardsTranscriptAndState(t *testing.T) {
	type event struct {
		name string
		data interface{}
	}
	events := make(chan event, 8)
	emit := func(name string, data ...interface{}) {
		ev := event{name: name}
		if len(data) > 0 {
			ev.data = data[0]
		}
		events <- ev
	}

	b := newSTTBridge(t.TempDir(), emit)
	srv := runBridgeOnServer(t, b)
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetPingHandler(nil)
	conn.SetReadLimit(1 << 20)

	// 模拟浏览器识别页：上报 listening 与 final 转录。
	send := func(v any) {
		t.Helper()
		if err := conn.WriteJSON(v); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	send(map[string]any{"type": "status", "state": "listening"})
	send(map[string]any{"type": "transcript", "text": "你好世界", "isFinal": true})

	// 桥接把所有事件经 emit 投递，最多等 3 秒。
	gotTranscript := false
	gotListening := false
	deadline := time.After(3 * time.Second)
	for !gotTranscript || !gotListening {
		select {
		case ev := <-events:
					switch ev.name {
					case sttTranscriptEvent:
						if p, ok := ev.data.(sttTranscriptPayload); ok && p.Text == "你好世界" && p.IsFinal {
							gotTranscript = true
						} else {
							t.Fatalf("unexpected transcript event: %#v", ev.data)
						}
					case sttStateEvent:
						if m, ok := ev.data.(map[string]any); ok {
							if v, ok := m["listening"].(bool); ok && v {
								gotListening = true
							}
						}
					}
		case <-deadline:
			t.Fatalf("timeout waiting for events: transcript=%v listening=%v", gotTranscript, gotListening)
		}
	}
	if !gotTranscript || !gotListening {
		t.Fatalf("missing events: transcript=%v listening=%v", gotTranscript, gotListening)
	}
}

// 空 transcript 过滤：Web Speech 偶发空 final 不上报，避免光标处插入空内容。
func TestSTTBridgeSkipsEmptyFinalTranscript(t *testing.T) {
	n := 0
	emit := func(name string, data ...interface{}) { n++ }
	b := newSTTBridge(t.TempDir(), emit)
	srv := runBridgeOnServer(t, b)
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	time.Sleep(200 * time.Millisecond) // 等处理器注册连接

	_ = conn.WriteJSON(map[string]any{"type": "transcript", "text": "   ", "isFinal": true})
	_ = conn.WriteJSON(map[string]any{"type": "transcript", "text": "", "isFinal": true})
	time.Sleep(300 * time.Millisecond)
	if n != 0 {
		t.Fatalf("empty finals emitted %d events, want 0", n)
	}
}

// 单客户端：新连接覆盖旧连接（识别页被重置时旧 WS 必须释放）。
func TestSTTBridgeSingleClientReplacesPrevious(t *testing.T) {
	b := newSTTBridge(t.TempDir(), nil)
	srv := runBridgeOnServer(t, b)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}

	if _, _, err := dialer.Dial(url, nil); err != nil {
			t.Fatalf("dial 1: %v", err)
		}
	time.Sleep(100 * time.Millisecond)
	b.mu.Lock()
	first := b.browserConn
	b.mu.Unlock()
	if first == nil {
		t.Fatal("first connection not registered")
	}

	second, _, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial 2: %v", err)
	}
	defer second.Close()
	time.Sleep(100 * time.Millisecond)

	b.mu.Lock()
	cur := b.browserConn
	b.mu.Unlock()
	if cur == nil || cur == first {
		t.Fatal("second connection did not replace the first")
	}
	// 旧连接应被关闭。
	_ = first.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, _, err := first.ReadMessage(); err == nil {
		t.Fatal("stale connection still readable; expected close")
	}
}

// Status 在服务未启动时返回安全的零值（前端按钮态不闪错）。
func TestSTTBridgeStatusDefault(t *testing.T) {
	b := newSTTBridge(t.TempDir(), nil)
	s := b.Status()
	if s["running"] != false || s["listening"] != false || s["connected"] != false {
		t.Fatalf("default status = %v", s)
	}
	if lang, _ := s["lang"].(string); lang != "zh-CN" {
		t.Fatalf("default lang = %v, want zh-CN", lang)
	}
}

// 转录负载序列化契约：payload 必须可被前端 JSON 解析（stt:transcript 事件）。
func TestSTTTranscriptPayloadJSON(t *testing.T) {
	p := sttTranscriptPayload{Text: "hi", IsFinal: true, Error: ""}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back sttTranscriptPayload
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Text != "hi" || !back.IsFinal || back.Error != "" {
		t.Fatalf("roundtrip = %+v", back)
	}
}