package agent

import (
	"encoding/json"
	"testing"
)

// TestSemanticPruner 测试语义上下文裁剪
func TestSemanticPruner(t *testing.T) {
	pruner := NewSemanticPruner()

	// 错误内容应获得高评分
	score := pruner.ScoreMessage(0, "assistant", "Error: undefined variable x")
	if score.Importance < 0.7 {
		t.Fatalf("error should have high importance, got %.2f", score.Importance)
	}
	if score.Category != "error" {
		t.Fatalf("expected category error, got %s", score.Category)
	}

	// 代码定义应获得高评分
	score2 := pruner.ScoreMessage(1, "assistant", "func main() {\n  fmt.Println()")
	if score2.Importance < 0.7 {
		t.Fatalf("code should have high importance, got %.2f", score2.Importance)
	}

	// 普通对话应获得中低评分
	score3 := pruner.ScoreMessage(2, "user", "hello")
	if score3.Importance > 0.65 {
		t.Fatalf("chat should have medium-low importance, got %.2f", score3.Importance)
	}

	// 裁剪候选应只返回低重要性消息
	candidates := pruner.GetPruningCandidates(100)
	for _, idx := range candidates {
		s := pruner.GetScore(idx)
		if s != nil && s.Importance >= 0.4 {
			t.Fatalf("pruning candidate %d should have low importance, got %.2f", idx, s.Importance)
		}
	}
}

// TestPrefetchPredictor 测试预测性预取
func TestPrefetchPredictor(t *testing.T) {
	predictor := NewPrefetchPredictor()

	// read_file 后应预测 grep
	predictions := predictor.Predict("read_file", `{"path":"main.go"}`)
	if len(predictions) == 0 {
		t.Fatal("should predict next tools after read_file")
	}

	found := false
	for _, p := range predictions {
		if p.PredictedTool == "grep" {
			found = true
		}
	}
	if !found {
		t.Fatal("should predict grep after read_file")
	}

	// 检查预取命中
	hit := predictor.CheckPrefetchHit("grep")
	if !hit {
		t.Fatal("grep should hit prefetch queue")
	}

	// 未预取的工具不应命中
	hit2 := predictor.CheckPrefetchHit("web_search")
	if hit2 {
		t.Fatal("web_search should not hit prefetch queue")
	}

	// 记录序列用于学习
	predictor.RecordSequence([]string{"read_file", "grep", "edit_file", "bash"})
	predictions2 := predictor.Predict("read_file", "{}")
	if len(predictions2) == 0 {
		t.Fatal("should still predict after learning")
	}
}

// TestContextWindowPredictor 测试上下文窗口预测器
func TestContextWindowPredictor(t *testing.T) {
	predictor := NewContextWindowPredictor(128000)

	// 记录消耗
	predictor.RecordConsumption(0, 10000, 2000)
	predictor.RecordConsumption(1, 15000, 3000)
	predictor.RecordConsumption(2, 20000, 2500)

	// 预测
	result := predictor.Predict()
	if result == nil {
		t.Fatal("prediction should not be nil")
	}
	if result.CurrentUsage <= 0 {
		t.Fatal("current usage should be positive")
	}
	if result.RemainingTokens <= 0 {
		t.Fatal("remaining tokens should be positive")
	}

	// 模拟高使用率
	for i := 3; i < 20; i++ {
		predictor.RecordConsumption(i, 5000+i*3000, 2000)
	}
	result2 := predictor.Predict()
	if result2 == nil {
		t.Fatal("prediction should not be nil")
	}
	// 在高使用率时应建议压缩
	if result2.PredictedUsage < 0.5 {
		t.Fatalf("predicted usage should be high, got %.1f%%", result2.PredictedUsage*100)
	}
}

// TestCostEstimator 测试 token 成本估算器
func TestCostEstimator(t *testing.T) {
	estimator := NewCostEstimator("deepseek")

	// 估算一次请求的成本
	cost := estimator.EstimateCost(10000, 2000, 8000, 1000)
	if cost.TotalCost <= 0 {
		t.Fatal("total cost should be positive")
	}
	if cost.InputCost <= 0 {
		t.Fatal("input cost should be positive")
	}
	if cost.OutputCost <= 0 {
		t.Fatal("output cost should be positive")
	}

	// 缓存节省
	savings := estimator.EstimateSavings(8000, 1000)
	if savings <= 0 {
		t.Fatal("cache savings should be positive")
	}

	// 设置较低的预算用于测试
	estimator.SetDailyBudget(0.50) // $0.50/day

	// 模拟超预算（100 次大请求）
	for i := 0; i < 100; i++ {
		estimator.EstimateCost(50000, 10000, 40000, 5000)
	}
	status2 := estimator.CheckBudget()
	if status2 == BudgetStatusOK {
		t.Fatal("should not be OK after exceeding budget")
	}
}

// TestToolSchemaLazyLoader 测试工具 schema 懒加载
func TestToolSchemaLazyLoader(t *testing.T) {
	t.Skip("requires tool package import")

	// This test is in the tool package, tested separately
	_ = json.RawMessage(`{}`)
}
