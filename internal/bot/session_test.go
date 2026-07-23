package bot

import (
	"testing"
	"time"
)

func TestBuildSessionKey(t *testing.T) {
	tests := []struct {
		name string
		src  SessionSource
		// DM 同 chat 不同 user 应返回相同 key
		wantSame bool
		src2     SessionSource
	}{
		{
			name:     "dm same chat different user",
			src:      SessionSource{Platform: PlatformQQ, ChatType: ChatDM, ChatID: "user123", UserID: "a"},
			src2:     SessionSource{Platform: PlatformQQ, ChatType: ChatDM, ChatID: "user123", UserID: "b"},
			wantSame: true,
		},
		{
			name:     "dm different chat",
			src:      SessionSource{Platform: PlatformQQ, ChatType: ChatDM, ChatID: "user123", UserID: "a"},
			src2:     SessionSource{Platform: PlatformQQ, ChatType: ChatDM, ChatID: "user456", UserID: "a"},
			wantSame: false,
		},
		{
			name:     "direct same chat different user",
			src:      SessionSource{Platform: PlatformQQ, ChatType: ChatDirect, ChatID: "guild123", UserID: "a"},
			src2:     SessionSource{Platform: PlatformQQ, ChatType: ChatDirect, ChatID: "guild123", UserID: "b"},
			wantSame: true,
		},
		{
			name:     "direct distinct from dm",
			src:      SessionSource{Platform: PlatformQQ, ChatType: ChatDirect, ChatID: "shared", UserID: "a"},
			src2:     SessionSource{Platform: PlatformQQ, ChatType: ChatDM, ChatID: "shared", UserID: "a"},
			wantSame: false,
		},
		{
			name:     "group same chat different user",
			src:      SessionSource{Platform: PlatformFeishu, ChatType: ChatGroup, ChatID: "group1", UserID: "a"},
			src2:     SessionSource{Platform: PlatformFeishu, ChatType: ChatGroup, ChatID: "group1", UserID: "b"},
			wantSame: false,
		},
		{
			name:     "group same user different chat",
			src:      SessionSource{Platform: PlatformFeishu, ChatType: ChatGroup, ChatID: "group1", UserID: "a"},
			src2:     SessionSource{Platform: PlatformFeishu, ChatType: ChatGroup, ChatID: "group2", UserID: "a"},
			wantSame: false,
		},
		{
			name:     "thread shared",
			src:      SessionSource{Platform: PlatformQQ, ChatType: ChatThread, ChatID: "ch1", ThreadID: "th1", UserID: "a"},
			src2:     SessionSource{Platform: PlatformQQ, ChatType: ChatThread, ChatID: "ch1", ThreadID: "th1", UserID: "b"},
			wantSame: true,
		},
		{
			name:     "different platform same ids",
			src:      SessionSource{Platform: PlatformQQ, ChatType: ChatDM, ChatID: "123", UserID: "u1"},
			src2:     SessionSource{Platform: PlatformFeishu, ChatType: ChatDM, ChatID: "123", UserID: "u1"},
			wantSame: false,
		},
		{
			name:     "same platform different connection",
			src:      SessionSource{Platform: PlatformFeishu, ConnectionID: "feishu-feishu", ChatType: ChatDM, ChatID: "123", UserID: "u1"},
			src2:     SessionSource{Platform: PlatformFeishu, ConnectionID: "feishu-lark", ChatType: ChatDM, ChatID: "123", UserID: "u1"},
			wantSame: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k1 := BuildSessionKey(tt.src)
			k2 := BuildSessionKey(tt.src2)
			if tt.wantSame && k1 != k2 {
				t.Errorf("want same key, got %s != %s", k1, k2)
			}
			if !tt.wantSame && k1 == k2 {
				t.Errorf("want different keys, got %s == %s", k1, k2)
			}
		})
	}
}

func TestIsSlashBypass(t *testing.T) {
	tests := []struct {
		text   string
		bypass bool
	}{
		{"/stop", true},
		{"/stop  extra args", true},
		{"/new", true},
		{"/reset", true},
		{"/approve", true},
		{"/deny", true},
		{"/yolo", true},
		{"/yolo on", true},
		{"/mode yolo", true},
		{"/status", true},
		{"/help", true},
		{"hello", false},
		{"/unknown", false},
		{"", false},
		{" /stop", false}, // leading space means not a slash command
	}

	for _, tt := range tests {
		got := IsSlashBypass(tt.text)
		if got != tt.bypass {
			t.Errorf("IsSlashBypass(%q) = %v, want %v", tt.text, got, tt.bypass)
		}
	}
}

func TestSessionManager_TryAcquire(t *testing.T) {
	sm := NewSessionManager(100 * time.Millisecond)

	msg := InboundMessage{Text: "hello", Platform: PlatformQQ, ChatType: ChatDM, ChatID: "c1", UserID: "u1"}
	key := BuildSessionKey(msg.Session())

	// 第一次获取成功
	acquired, merged := sm.TryAcquire(key, msg)
	if !acquired || merged {
		t.Error("first acquire should succeed")
	}

	// 第二次获取应该排队
	acquired, merged = sm.TryAcquire(key, InboundMessage{Text: "world"})
	if acquired || !merged {
		t.Error("second acquire should merge into queue")
	}

	// slash bypass 命令应绕过
	acquired, merged = sm.TryAcquire(key, InboundMessage{Text: "/stop"})
	if !acquired || merged {
		t.Error("slash bypass should acquire immediately")
	}

	// 第一次 Release 返回排队消息
	next := sm.Release(key)
	if next == nil {
		t.Fatal("expected queued message after first release")
	}
	if next.Text != "world" {
		t.Errorf("merged text = %q, want %q", next.Text, "world")
	}
}

func TestSessionManager_Debounce(t *testing.T) {
	sm := NewSessionManager(200 * time.Millisecond)

	msg := InboundMessage{Text: "first", Platform: PlatformQQ, ChatType: ChatDM, ChatID: "c1", UserID: "u1"}
	key := BuildSessionKey(msg.Session())

	acquired, _ := sm.TryAcquire(key, msg)
	if !acquired {
		t.Fatal("first acquire should succeed")
	}

	// 同 session 消息应合并
	sm.TryAcquire(key, InboundMessage{Text: "second"})
	// 在 debounce 窗口内发第三条
	sm.TryAcquire(key, InboundMessage{Text: "third"})

	next := sm.Release(key)
	if next == nil {
		t.Fatal("expected queued message after release")
	}
	// "second" 和 "third" 合并在队列里（"first" 已作为 active 被处理）
	if next.Text != "second\nthird" {
		t.Errorf("merged = %q, want %q", next.Text, "second\nthird")
	}
}

func TestSessionManager_ForceRelease(t *testing.T) {
	sm := NewSessionManager(100 * time.Millisecond)

	msg := InboundMessage{Text: "test", Platform: PlatformQQ, ChatType: ChatDM, ChatID: "c1", UserID: "u1"}
	key := BuildSessionKey(msg.Session())

	sm.TryAcquire(key, msg)
	if !sm.IsActive(key) {
		t.Error("should be active")
	}

	sm.ForceRelease(key)
	if sm.IsActive(key) {
		t.Error("should not be active after force release")
	}
}

func TestSessionManagerRunIfIdleSerializesNewAdmission(t *testing.T) {
	sm := NewSessionManager(100 * time.Millisecond)
	msg := InboundMessage{Text: "test", Platform: PlatformQQ, ChatType: ChatDM, ChatID: "c1", UserID: "u1"}
	key := BuildSessionKey(msg.Session())
	entered := make(chan struct{})
	release := make(chan struct{})
	switchDone := make(chan bool, 1)
	go func() {
		switchDone <- sm.runIfIdle(key, func() bool {
			close(entered)
			<-release
			return true
		})
	}()
	<-entered

	admitted := make(chan bool, 1)
	go func() {
		acquired, _ := sm.TryAcquire(key, msg)
		admitted <- acquired
	}()
	select {
	case acquired := <-admitted:
		t.Fatalf("message admission completed during runtime switch: acquired=%v", acquired)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if !<-switchDone {
		t.Fatal("idle runtime switch was rejected")
	}
	select {
	case acquired := <-admitted:
		if !acquired {
			t.Fatal("message was not admitted after runtime switch")
		}
	case <-time.After(time.Second):
		t.Fatal("message admission remained blocked after runtime switch")
	}
}

func TestHashID(t *testing.T) {
	h1 := hashID("user_12345")
	h2 := hashID("user_12345")
	h3 := hashID("user_67890")

	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
	if h1 == h3 {
		t.Error("different inputs should produce different hashes")
	}
	if hashID("") != "" {
		t.Error("empty input should produce empty hash")
	}
}

func TestInboundMessage_Session(t *testing.T) {
	msg := InboundMessage{
		Platform:     PlatformQQ,
		ConnectionID: "qq-main",
		Domain:       "qq",
		ChatType:     ChatDM,
		ChatID:       "chat1",
		UserID:       "user1",
		ThreadID:     "thread1",
	}

	src := msg.Session()
	if src.Platform != PlatformQQ || src.ConnectionID != "qq-main" || src.Domain != "qq" || src.ChatType != ChatDM || src.ChatID != "chat1" || src.UserID != "user1" || src.ThreadID != "thread1" {
		t.Error("Session() should copy all fields")
	}
}
