package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

// TestOpenChannelSessionGlobalBotAcrossDirs 回归：bot/channel 会话可能位于
// 全局 session dir（global scope），而 tab 的 controller 位于项目 dir。
// OpenChannelSession* 必须放行跨目录的 channel 会话，否则前端报
// 「加载会话历史失败」（session path outside session dir）。
func TestOpenChannelSessionGlobalBotAcrossDirs(t *testing.T) {
	isolateDesktopUserDirs(t)
	globalDir := config.SessionDir()
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir global: %v", err)
	}
	botPath := filepath.Join(globalDir, "bot-abc123.jsonl")
	if err := os.WriteFile(botPath, []byte(`{"role":"user","content":"from channel"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write bot session: %v", err)
	}

	// tab 的 controller 位于"项目"session dir，与全局 bot 会话不同目录。
	projDir := filepath.Join(globalDir, "..", "project-sessions")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}
	app := NewApp()
	ctrl := control.New(control.Options{SessionDir: projDir, SessionPath: filepath.Join(projDir, "active.jsonl"), Label: "test"})
	app.setTestCtrl(ctrl, "")
	defer app.activeCtrl().Close()

	if _, err := app.OpenChannelSessionForTab("test", botPath); err != nil {
		t.Fatalf("open global bot session from project tab: %v", err)
	}
	if _, err := app.OpenChannelSessionPageForTab("test", botPath, 60); err != nil {
		t.Fatalf("open paged global bot session: %v", err)
	}
}
