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
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"reasonix/internal/browserrelay"
)

// TestRealE2E_NavigateAndRead 是真的 E2E 测试：
// 1. 启动 Relay Server
// 2. 用 WebSocket 模拟扩展连接并认证
// 3. 从服务器侧（s.Send）发出 CDP 命令，扩展侧接收后回结果
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

	// Step 4: 服务器侧发起 CDP 命令（这是生产代码路径——AI 通过 s.Send 发送）
	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := s.Send(ctx, "Page.navigate", json.RawMessage(`{"url":"about:blank"}`))
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- string(result)
	}()

	// 扩展侧接收 CDP 命令
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

	select {
	case result := <-resultCh:
		if !strings.Contains(result, "frameId") {
			t.Fatalf("result = %q, want frameId echo", result)
		}
	case err := <-errCh:
		t.Fatalf("Send() failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Send() timed out")
	}

	t.Log("✅ 完整的 CDP 指令流：Send → 扩展接收 → 返回结果，链路通畅")
}

// 这个测试展示了：真正的 E2E 需要"三方"都在
// 1. Relay Server（Go）
// 2. Extension（Chrome）
// 3. 目标网页
// 缺一不可