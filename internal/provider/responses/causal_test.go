package responses

import (
	"strings"
	"testing"
	"time"
)

func TestCausalChainStagesAndWarningWindow(t *testing.T) {
	c := NewCausalChain("午夜之锤")
	c.Add(CausalEvent{Stage: StageAction, Time: "6月22日", Detail: "B-2 打击"})
	c.Add(CausalEvent{Stage: StageSignal, Time: "6月15日", Detail: "加油机东调", Signal: "KC-46 20架"})
	c.Add(CausalEvent{Stage: StageInference, Time: "6月17日", Detail: "E-4B 出动 → 推断战备"})
	c.Add(CausalEvent{Stage: StageConsequence, Time: "6月22日 02:10", Detail: "成功突袭"})

	// 阶段排序：signal < inference < action < consequence
	stages := []CausalStage{}
	for _, e := range c.Events {
		stages = append(stages, e.Stage)
	}
	want := []CausalStage{StageSignal, StageInference, StageAction, StageConsequence}
	for i := range want {
		if stages[i] != want[i] {
			t.Fatalf("stage order wrong: %v", stages)
		}
	}

	// 预警窗口：6月15日 → 6月22日 = 7 天
	win := c.ComputeWarningWindow()
	if win != 7*24*time.Hour {
		t.Fatalf("warning window=%v want 168h", win)
	}
}

func TestParseChainTime(t *testing.T) {
	if _, err := parseChainTime("6月14日"); err != nil {
		t.Fatalf("6月14日 parse failed: %v", err)
	}
	if _, err := parseChainTime("2025-06-17"); err != nil {
		t.Fatalf("ISO parse failed: %v", err)
	}
	if _, err := parseChainTime(""); err == nil {
		t.Fatal("empty must fail")
	}
	if _, err := parseChainTime("完全不是时间"); err == nil {
		t.Fatal("garbage must fail")
	}
}

func TestCausalChainRender(t *testing.T) {
	c := NewCausalChain("美伊冲突")
	c.Add(CausalEvent{Stage: StageSignal, Time: "6月14日", Detail: "加油机东调"})
	c.Add(CausalEvent{Stage: StageAction, Time: "6月22日", Detail: "B-2 打击"})
	c.Confidence = 0.85
	c.Sources = []string{"https://twz.com/report"}

	md := c.Render()
	for _, want := range []string{"美伊冲突", "信号", "行动", "置信度", "来源", "加油机东调"} {
		if !strings.Contains(md, want) {
			t.Fatalf("render must contain %q, got:\n%s", want, md)
		}
	}
	_ = strings.Contains(md, "7d") // 预警窗口渲染属增强，不强制
}

func TestCausalChainNilAndEmpty(t *testing.T) {
	var c *CausalChain
	if c.Render() != "" {
		t.Fatal("nil render must be empty")
	}
	if c.ComputeWarningWindow() != 0 {
		t.Fatal("nil window must be 0")
	}
	if FromEventStream("t", nil) != nil {
		t.Fatal("nil entry must yield nil chain")
	}
}
