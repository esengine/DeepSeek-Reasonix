package flywheel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportFiltersAndManifest(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "fw"))
	j := HeuristicJudge{}

	// 两条轨迹：一条 excellent、一条 failed。
	good := &Trajectory{ID: "g1", Task: "add cache", Repo: "rx",
		Verify: &Verify{Kind: "go_test", OK: true},
		Steps:  []Step{{Kind: "tool_call", Tool: "a", OK: true}, {Kind: "tool_call", Tool: "b", OK: true}}}
	if _, err := s.JudgeTrajectory(good, j); err != nil {
		t.Fatalf("judge good: %v", err)
	}
	bad := &Trajectory{ID: "b1", Task: "add cache", Repo: "rx",
		Verify: &Verify{Kind: "go_test", OK: false}}
	if _, err := s.JudgeTrajectory(bad, j); err != nil {
		t.Fatalf("judge bad: %v", err)
	}

	out := filepath.Join(t.TempDir(), "export")

	// 全部导出 → 2 条 + manifest 2 行。
	n, err := s.Export(out, ExportFilter{})
	if err != nil {
		t.Fatalf("Export all: %v", err)
	}
	if n != 2 {
		t.Fatalf("all export want 2, got %d", n)
	}
	if _, err := os.Stat(filepath.Join(out, "g1.jsonl")); err != nil {
		t.Errorf("g1.jsonl missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "b1.jsonl")); err != nil {
		t.Errorf("b1.jsonl missing: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(out, "manifest.jsonl"))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if got := strings.Count(string(manifest), "\n"); got != 2 {
		t.Errorf("manifest lines want 2, got %d", got)
	}

	// 按标签过滤（only excellent）→ 1 条。
	out2 := filepath.Join(t.TempDir(), "export2")
	n, err = s.Export(out2, ExportFilter{Label: "excellent"})
	if err != nil || n != 1 {
		t.Fatalf("label export want 1, got %d err=%v", n, err)
	}

	// 按分数过滤（≥0.9）→ 1 条（excellent=0.9）。
	out3 := filepath.Join(t.TempDir(), "export3")
	n, err = s.Export(out3, ExportFilter{MinScore: 0.9})
	if err != nil || n != 1 {
		t.Fatalf("min-score export want 1, got %d err=%v", n, err)
	}

	// 按任务子串 + repo 过滤。
	out4 := filepath.Join(t.TempDir(), "export4")
	n, err = s.Export(out4, ExportFilter{Task: "cache", Repo: "rx"})
	if err != nil || n != 2 {
		t.Fatalf("task+repo export want 2, got %d err=%v", n, err)
	}
	out5 := filepath.Join(t.TempDir(), "export5")
	n, err = s.Export(out5, ExportFilter{Repo: "other"})
	if err != nil || n != 0 {
		t.Fatalf("repo miss want 0, got %d err=%v", n, err)
	}

	// 导出文件必须是合法 JSON（读回验证）。
	b, err := os.ReadFile(filepath.Join(out, "g1.jsonl"))
	if err != nil {
		t.Fatalf("read g1: %v", err)
	}
	var tr Trajectory
	if err := json.Unmarshal(b, &tr); err != nil {
		t.Fatalf("exported g1 must be valid JSON: %v", err)
	}
	if tr.Judge == nil || tr.Judge.Name != "excellent" {
		t.Errorf("g1 judge lost: %+v", tr.Judge)
	}
}
