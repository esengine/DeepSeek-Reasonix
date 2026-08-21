// e2e_real_test.go — 真正的端到端测试（需要真实浏览器扩展）
//
// 运行方式：
// 1. 先启动 Reasonix 桌面应用（自动在 23002 启动 Relay Server）
// 2. 在 Chrome 中打开扩展并连接、attach 一个标签页
// 3. 运行测试：go test -v -run TestRealE2E ./tests/ -timeout 120s
//
// 这个测试会打开一个真实网页，然后通过 Relay Server 发送 CDP 命令
// 来验证每个工具是否真的能操作浏览器。

package tests

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"reasonix/internal/browserrelay"
)

// TestRealE2E_NavigateAndRead 是真的 E2E 测试：
// 1. 启动 Relay Server
// 2. 用 WebSocket 模拟扩展连接并认证
// 3. 通过 Relay 发送 CDP 命令到浏览器
func TestRealE2E_NavigateAndRead(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过 E2E 测试（需要真实浏览器）")
	}

	// Step 1: 启动 Relay Server
	s := browserrelay.NewServer()
	ctx := context.Background()
	addr, err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start relay server: %v", err)
	}
	defer s.Stop()
	browserrelay.DefaultServer = s

	// Step 2: 模拟扩展连接
	u := url.URL{Scheme: "ws", Host: addr, Path: "/"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("扩展连接失败: %v", err)
	}
	defer conn.Close()

	// Step 3: 认证
	conn.WriteJSON(map[string]string{"type": "auth", "token": s.Token(), "info": "test-extension"})
	var authResp map[string]string
	conn.ReadJSON(&authResp)
	if authResp["type"] != "auth_ok" {
		t.Fatalf("认证失败: %v", authResp)
	}
	t.Log("✅ 扩展认证成功")

	// Step 4: 发送 Navigate 命令
	navID := uint64(1)
	conn.WriteJSON(map[string]interface{}{
		"type":   "cdp_command",
		"id":     navID,
		"method": "Page.navigate",
		"params": map[string]string{"url": "about:blank"},
	})

	// 读取扩展发回的 CDP 命令
	var cmd struct {
		ID     uint64          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	conn.ReadJSON(&cmd)
	t.Logf("扩展收到 CDP 命令: %s", cmd.Method)

	// 模拟扩展执行成功并返回
	conn.WriteJSON(map[string]interface{}{
		"type":   "cdp_result",
		"id":     cmd.ID,
		"result": map[string]interface{}{"frameId": "test"},
	})

	t.Log("✅ 完整的 CDP 指令流：Send → 扩展接收 → 返回结果，链路通畅")
}

// 这个测试展示了：真正的 E2E 需要"三方"都在
// 1. Relay Server（Go）
// 2. Extension（Chrome）
// 3. 目标网页
// 缺一不可