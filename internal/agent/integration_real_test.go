package agent

import (
	"context"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestIntegrationSceneClassifierReal 验证场景分类器在 Agent 中实际初始化和分类
func TestIntegrationSceneClassifierReal(t *testing.T) {
	prov := &mockProvider{}
	tools := tool.NewRegistry()
	agent := New(prov, tools, NewSession(""), Options{
		MaxSteps: 1,
	}, event.Discard)

	if agent.sceneClassifier == nil {
		t.Fatal("sceneClassifier not initialized on Agent")
	}
	if agent.scenePolicyProvider == nil {
		t.Fatal("scenePolicyProvider not initialized on Agent")
	}

	// 实际执行场景分类
	result, err := agent.sceneClassifier.Classify(context.Background(), "write a function to sort an array")
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if result.Scene == "" {
		t.Fatal("scene classification returned empty scene")
	}
	t.Logf("scene=%s complexity=%d needsTools=%v needsThink=%v",
		result.Scene, result.Complexity, result.NeedsTools, result.NeedsThink)

	// 验证场景策略
	policy := agent.scenePolicyProvider.GetPolicy(result.Scene)
	if policy.MaxPrideSignals < 0 {
		t.Fatal("invalid MaxPrideSignals")
	}
	t.Logf("policy: maxPrideSignals=%d thinkMode=%s reasoningEffort=%s",
		policy.MaxPrideSignals, policy.ThinkMode, policy.ReasoningEffort)
}

// TestIntegrationIncrementalCacheReal 验证增量缓存在 Agent 中实际工作
func TestIntegrationIncrementalCacheReal(t *testing.T) {
	prov := &mockProvider{}
	tools := tool.NewRegistry()
	agent := New(prov, tools, NewSession(""), Options{
		MaxSteps:               1,
		EnableIncrementalCache: true,
	}, event.Discard)

	if agent.incrementalCache == nil {
		t.Fatal("incrementalCache not initialized on Agent")
	}

	// 模拟流式追加
	agent.incrementalCache.AppendContent("Hello, ")
	agent.incrementalCache.AppendContent("world!")
	agent.incrementalCache.AppendReasoning("thinking step 1")

	if agent.incrementalCache.Content() != "Hello, world!" {
		t.Fatalf("content mismatch: got %q", agent.incrementalCache.Content())
	}
	if agent.incrementalCache.Reasoning() != "thinking step 1" {
		t.Fatalf("reasoning mismatch: got %q", agent.incrementalCache.Reasoning())
	}
	if !agent.incrementalCache.IsPartial() {
		t.Fatal("IsPartial should be true after content appended")
	}

	// 验证恢复提示
	recovery := agent.incrementalCache.RecoveryPrompt()
	if recovery == "" {
		t.Fatal("RecoveryPrompt should not be empty")
	}
	t.Logf("recovery prompt: %s", recovery)

	// 验证重置
	agent.incrementalCache.Reset()
	if agent.incrementalCache.Content() != "" {
		t.Fatal("content should be empty after Reset")
	}
}

// TestIntegrationSideEffectTrackerReal 验证副作用追踪器在 Agent 中实际工作
func TestIntegrationSideEffectTrackerReal(t *testing.T) {
	prov := &mockProvider{}
	tools := tool.NewRegistry()
	agent := New(prov, tools, NewSession(""), Options{
		MaxSteps:                1,
		EnableSideEffectTracking: true,
	}, event.Discard)

	if agent.sideEffectTracker == nil {
		t.Fatal("sideEffectTracker not initialized on Agent")
	}

	// 记录副作用
	agent.sideEffectTracker.Record(&SideEffect{
		ID:           "test-1",
		Type:         SideEffectToolCall,
		Description:  "test tool call",
		Compensation: "undo test tool call",
		Reversible:   false,
	})

	// 验证恢复报告
	report := agent.sideEffectTracker.RecoveryReport()
	if report == "" {
		t.Fatal("RecoveryReport should not be empty")
	}
	t.Logf("recovery report:\n%s", report)

	// 验证未补偿副作用
	uncompensated := agent.sideEffectTracker.UncompensatedEffects()
	if len(uncompensated) != 1 {
		t.Fatalf("expected 1 uncompensated effect, got %d", len(uncompensated))
	}

	// 验证重置
	agent.sideEffectTracker.Reset()
	if len(agent.sideEffectTracker.UncompensatedEffects()) != 0 {
		t.Fatal("should have 0 uncompensated effects after Reset")
	}
}

// TestIntegrationDedupProviderReal 验证去重 Provider 包装器实际工作
func TestIntegrationDedupProviderReal(t *testing.T) {
	mock := &mockProvider{}
	dedupProv := provider.NewDeduplicatingProvider(mock)

	if dedupProv.Name() != mock.Name() {
		t.Fatalf("name mismatch: got %q want %q", dedupProv.Name(), mock.Name())
	}

	// 验证 Stream 方法存在且可调用
	// (不实际发送请求，只验证接口实现)
	var _ provider.Provider = dedupProv
}

// TestIntegrationPrideDetectionReal 验证骄傲信号检测实际工作
func TestIntegrationPrideDetectionReal(t *testing.T) {
	prov := &mockProvider{}
	tools := tool.NewRegistry()
	agent := New(prov, tools, NewSession(""), Options{
		MaxSteps: 1,
	}, event.Discard)

	// 检测骄傲信号
	detected := agent.scenePolicyProvider.DetectPride("This is the perfect solution and the best approach, it's 100% correct")
	if len(detected) == 0 {
		t.Fatal("expected pride signals to be detected")
	}
	t.Logf("detected pride signals: %v", detected)
}

// TestIntegrationPhantomUIReal 验证虚空UI面板在 Agent 中实际初始化
func TestIntegrationPhantomUIReal(t *testing.T) {
	prov := &mockProvider{}
	tools := tool.NewRegistry()
	agent := New(prov, tools, NewSession(""), Options{
		MaxSteps:         1,
		EnablePhantomUI:  true,
		EnableReviewGate: true,
	}, event.Discard)

	if agent.phantomUI == nil {
		t.Fatal("phantomUI not initialized on Agent")
	}
	if agent.eyeTracker == nil {
		t.Fatal("eyeTracker not initialized on Agent")
	}
	if agent.gazeIntegrator == nil {
		t.Fatal("gazeIntegrator not initialized on Agent")
	}
	if agent.reviewGate == nil {
		t.Fatal("reviewGate not initialized on Agent")
	}

	// 验证 accessor 方法
	if agent.GetPhantomUI() == nil {
		t.Fatal("GetPhantomUI returned nil")
	}
	if agent.GetEyeTracker() == nil {
		t.Fatal("GetEyeTracker returned nil")
	}
	if agent.GetReviewGate() == nil {
		t.Fatal("GetReviewGate returned nil")
	}

	// 验证审核门控可以提交变更
	item := agent.SubmitCodeChange(CodeChange{
		ID:         "change-1",
		FilePath:   "test.go",
		ChangeType: ChangeModify,
		Content:    "modified code",
		Intent:     "test code change",
	})
	if item == nil {
		t.Fatal("SubmitCodeChange returned nil")
	}
	t.Logf("review item: changeID=%s level=%d status=%d",
		item.Change.ID, item.Level, item.Status)

	// 停止后台 goroutine
	agent.phantomUI.Stop()
	agent.eyeTracker.Stop()
	agent.gazeIntegrator.Stop()
}
