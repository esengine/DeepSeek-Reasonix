package main

import (
	"sync"
	"testing"
	"time"
)

// TestPhantomRegistryRegisterUnregister 验证注册和注销
func TestPhantomRegistryRegisterUnregister(t *testing.T) {
	reg := NewPhantomRegistry()
	defer reg.Stop()

	// 注册
	reg.Register("session-1", "Alpha", "/workspace/alpha", "tab-1")
	reg.Register("session-2", "Beta", "/workspace/beta", "tab-2")

	entries := reg.GetEntries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// 验证按 Name 排序
	if entries[0].Name != "Alpha" {
		t.Fatalf("expected first entry to be Alpha, got %s", entries[0].Name)
	}
	if entries[1].Name != "Beta" {
		t.Fatalf("expected second entry to be Beta, got %s", entries[1].Name)
	}

	// 注销
	reg.Unregister("session-1")
	entries = reg.GetEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after unregister, got %d", len(entries))
	}
	if entries[0].SessionID != "session-2" {
		t.Fatalf("expected remaining entry to be session-2, got %s", entries[0].SessionID)
	}
}

// TestPhantomRegistryStatusUpdate 验证状态更新（零 token）
func TestPhantomRegistryStatusUpdate(t *testing.T) {
	reg := NewPhantomRegistry()
	defer reg.Stop()

	reg.Register("s1", "Test", "/ws", "t1")

	// 更新状态为活跃
	reg.UpdateStatus("s1", PhantomActive, 3)

	entry := reg.GetEntry("s1")
	if entry == nil {
		t.Fatal("entry not found")
	}
	if entry.Status != PhantomActive {
		t.Fatalf("expected status active, got %s", entry.Status)
	}
	if entry.TurnCount != 3 {
		t.Fatalf("expected turnCount 3, got %d", entry.TurnCount)
	}

	// 更新状态为失败
	reg.UpdateStatus("s1", PhantomFailed, 4)
	entry = reg.GetEntry("s1")
	if entry.Status != PhantomFailed {
		t.Fatalf("expected status failed, got %s", entry.Status)
	}
}

// TestPhantomRegistryConclusion 验证结论更新
func TestPhantomRegistryConclusion(t *testing.T) {
	reg := NewPhantomRegistry()
	defer reg.Stop()

	reg.Register("s1", "Test", "/ws", "t1")

	// 更新结论
	reg.UpdateConclusion("s1", "修改文件: main.go", "已就绪", 5)

	entry := reg.GetEntry("s1")
	if entry.Conclusion == nil {
		t.Fatal("conclusion not set")
	}
	if entry.Conclusion.Summary != "修改文件: main.go" {
		t.Fatalf("unexpected summary: %s", entry.Conclusion.Summary)
	}
	if entry.Conclusion.Status != "已就绪" {
		t.Fatalf("unexpected status: %s", entry.Conclusion.Status)
	}
	if entry.Conclusion.SourceTurn != 5 {
		t.Fatalf("expected sourceTurn 5, got %d", entry.Conclusion.SourceTurn)
	}
}

// TestPhantomRegistryCommBadge 验证交流计数
func TestPhantomRegistryCommBadge(t *testing.T) {
	reg := NewPhantomRegistry()
	defer reg.Stop()

	reg.Register("s1", "Test", "/ws", "t1")

	// 模拟收到交流
	reg.IncrementComm("s1", CommSummary, false)
	reg.IncrementComm("s1", CommSignal, false)
	reg.IncrementComm("s1", CommSummary, true)

	entry := reg.GetEntry("s1")
	if entry.CommBadge.TotalCount != 3 {
		t.Fatalf("expected total 3, got %d", entry.CommBadge.TotalCount)
	}
	if entry.CommBadge.RecvCount != 2 {
		t.Fatalf("expected recv 2, got %d", entry.CommBadge.RecvCount)
	}
	if entry.CommBadge.SentCount != 1 {
		t.Fatalf("expected sent 1, got %d", entry.CommBadge.SentCount)
	}
	if !entry.CommBadge.Unread {
		t.Fatal("should be unread")
	}
	if entry.CommBadge.PendingCount != 2 {
		t.Fatalf("expected pending 2, got %d", entry.CommBadge.PendingCount)
	}

	// 标记已读
	reg.MarkCommRead("s1")
	entry = reg.GetEntry("s1")
	if entry.CommBadge.Unread {
		t.Fatal("should be read after MarkCommRead")
	}
	if entry.CommBadge.PendingCount != 0 {
		t.Fatalf("expected pending 0 after read, got %d", entry.CommBadge.PendingCount)
	}
}

// TestPhantomRegistryIsolationFilter 验证隔离级别过滤
func TestPhantomRegistryIsolationFilter(t *testing.T) {
	reg := NewPhantomRegistry()
	defer reg.Stop()

	reg.Register("s1", "Test", "/ws", "t1")
	reg.UpdateConclusion("s1", "详细结论内容", "已就绪", 1)

	// Merged: 显示完整结论
	reg.UpdateIsolation("s1", IsolationMerged)
	entry := reg.GetEntry("s1")
	if entry.Conclusion == nil || entry.Conclusion.Summary != "详细结论内容" {
		t.Fatal("merged should show full conclusion")
	}

	// Observed: 截断到 50 字符
	reg.UpdateIsolation("s1", IsolationObserved)
	longSummary := "这是一个非常长的结论摘要内容用于测试隔离级别过滤功能是否正确截断到五十个字符以内"
	reg.UpdateConclusion("s1", longSummary, "已就绪", 2)
	entry = reg.GetEntry("s1")
	if entry.Conclusion == nil {
		t.Fatal("observed should show conclusion")
	}
	if len(entry.Conclusion.Summary) > 53 { // 50 + "..."
		t.Fatalf("observed summary should be truncated, got %d chars", len(entry.Conclusion.Summary))
	}

	// Zoned: 隐藏摘要内容
	reg.UpdateIsolation("s1", IsolationZoned)
	entry = reg.GetEntry("s1")
	if entry.Conclusion != nil && entry.Conclusion.Summary != "" {
		t.Fatal("zoned should hide summary content")
	}

	// Sandbox: 完全隐藏结论
	reg.UpdateIsolation("s1", IsolationSandbox)
	entry = reg.GetEntry("s1")
	if entry.Conclusion != nil {
		t.Fatal("sandbox should hide conclusion entirely")
	}
}

// TestPhantomRegistrySubscribe 验证事件订阅（零 token 推送）
func TestPhantomRegistrySubscribe(t *testing.T) {
	reg := NewPhantomRegistry()
	defer reg.Stop()

	sub := reg.Subscribe()

	// 触发更新
	reg.Register("s1", "Test", "/ws", "t1")
	reg.UpdateStatus("s1", PhantomActive, 1)

	// 等待事件（带超时）
	received := make([]PhantomUpdate, 0)
	done := make(chan struct{})
	go func() {
		timeout := time.After(500 * time.Millisecond)
		for {
			select {
			case <-timeout:
				close(done)
				return
			case u := <-sub:
				received = append(received, u)
				if len(received) >= 2 {
					close(done)
					return
				}
			}
		}
	}()
	<-done

	if len(received) < 2 {
		t.Fatalf("expected at least 2 updates, got %d", len(received))
	}
	if received[0].Type != "added" {
		t.Fatalf("expected first update type 'added', got %s", received[0].Type)
	}
	if received[1].Type != "status" {
		t.Fatalf("expected second update type 'status', got %s", received[1].Type)
	}
}

// TestPhantomRegistryConcurrent 并发安全测试
func TestPhantomRegistryConcurrent(t *testing.T) {
	reg := NewPhantomRegistry()
	defer reg.Stop()

	var wg sync.WaitGroup
	// 10 个 goroutine 并发注册和更新
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sessionID := "session-" + intToStr(n)
			reg.Register(sessionID, "Test"+intToStr(n), "/ws", "tab-"+intToStr(n))
			reg.UpdateStatus(sessionID, PhantomActive, 1)
			reg.UpdateConclusion(sessionID, "test conclusion", "已就绪", 1)
			reg.IncrementComm(sessionID, CommSignal, false)
		}(i)
	}
	wg.Wait()

	entries := reg.GetEntries()
	if len(entries) != 10 {
		t.Fatalf("expected 10 entries, got %d", len(entries))
	}

	// 验证排序
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Name > entries[i].Name {
			t.Fatalf("entries not sorted: %s > %s", entries[i-1].Name, entries[i].Name)
		}
	}
}

// TestExtractConclusionFromTurn 验证结论提取（不调用 LLM）
func TestExtractConclusionFromTurn(t *testing.T) {
	// 测试 bash 工具调用
	calls := []toolCallSummary{
		{Name: "bash", Command: "go build ./...", Success: true},
	}
	result := extractConclusionFromTurn(calls, true)
	if result != "执行命令: go build ./..." {
		t.Fatalf("unexpected conclusion: %s", result)
	}

	// 测试文件修改
	calls = []toolCallSummary{
		{Name: "write_file", FilePath: "main.go", Success: true},
	}
	result = extractConclusionFromTurn(calls, true)
	if result != "修改文件: main.go" {
		t.Fatalf("unexpected conclusion: %s", result)
	}

	// 测试失败
	calls = []toolCallSummary{
		{Name: "bash", Command: "make test", Success: false},
	}
	result = extractConclusionFromTurn(calls, false)
	if result != "执行失败" {
		t.Fatalf("unexpected conclusion: %s", result)
	}

	// 测试纯文本回复
	result = extractConclusionFromTurn(nil, true)
	if result != "纯文本回复" {
		t.Fatalf("unexpected conclusion: %s", result)
	}
}
