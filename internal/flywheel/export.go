package flywheel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Export（轨迹数据复用，docs/MODELLING_ENGINEERING.md §5）
//
// 把沉淀的轨迹（task→verify→judge）按条件导出为标准化 JSONL 集——Co-training
// 基础素材（借鉴 Muse Code：工具调用/多智能体协作轨迹融入模型训练）。导出是
// 只读聚合，不改动原轨迹。
// ---------------------------------------------------------------------------

// ExportFilter 导出过滤条件（空值 = 不限制）。
type ExportFilter struct {
	// MinScore 只导出 judge.score ≥ MinScore 的轨迹（如 0.7 只导优质/成功轨迹；
	// ≤0 或未设则导出全部）。
	MinScore float64 `json:"min_score,omitempty"`
	// Repo 只导出指定项目（按 Trajectory.Repo 精确匹配；空 = 全部）。
	Repo string `json:"repo,omitempty"`
	// Task 只导出任务描述包含该子串的轨迹（空 = 全部）。
	Task string `json:"task,omitempty"`
	// Label 只导出指定 judge 标签（excellent/good/partial/failed；空 = 全部）。
	Label string `json:"label,omitempty"`
}

// Export writes every trajectory matching f into dir as one file per
// trajectory (<id>.jsonl) plus a manifest.jsonl summary line per export.
// Returns the number of exported trajectories.
func (s *Store) Export(dir string, f ExportFilter) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	trajs, err := s.LoadTrajectories()
	if err != nil {
		return 0, err
	}
	manifest, err := os.OpenFile(filepath.Join(dir, "manifest.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer manifest.Close()

	count := 0
	for _, t := range trajs {
		if !matchesFilter(t, f) {
			continue
		}
		if err := writeExportTrajectory(dir, t); err != nil {
			return count, err
		}
		// manifest line: id + label + score（供训练脚本筛选）。
		line := struct {
			ID    string  `json:"id"`
			Label string  `json:"label,omitempty"`
			Score float64 `json:"score,omitempty"`
			Repo  string  `json:"repo,omitempty"`
		}{ID: t.ID, Repo: t.Repo}
		if t.Judge != nil {
			line.Label = t.Judge.Name
			line.Score = t.Judge.Score
		}
		b, _ := json.Marshal(line)
		if _, err := manifest.Write(append(b, '\n')); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func matchesFilter(t *Trajectory, f ExportFilter) bool {
	if f.MinScore > 0 {
		if t.Judge == nil || t.Judge.Score < f.MinScore {
			return false
		}
	}
	if f.Repo != "" && t.Repo != f.Repo {
		return false
	}
	if f.Task != "" && !strings.Contains(t.Task, f.Task) {
		return false
	}
	if f.Label != "" {
		if t.Judge == nil || t.Judge.Name != f.Label {
			return false
		}
	}
	return true
}

func writeExportTrajectory(dir string, t *Trajectory) error {
	if err := validateStoredID(t.ID); err != nil {
		return err
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, t.ID+".jsonl"), append(b, '\n'), 0o644)
}

// ---------------------------------------------------------------------------
// ContextHints（轻量模式上下文裁剪，docs/MODELLING_ENGINEERING.md §3/§6）
//
// 给定当前任务 query，从轨迹库检索 top-N 相关轨迹并组装为可注入的摘要提示
// （含 id/task/judge 标签 + 失败教训引用）。轻量模式用它替代"全量历史"注入，
// 只带相关片段 → 省 token。独立纯函数，不侵入 agent 核心路径。
// ---------------------------------------------------------------------------

// ContextHints returns a compact injection-ready hint block for query.
// limit ≤0 defaults to 3. Empty store → empty string (caller skips injection).
func (s *Store) ContextHints(query string, limit int) (string, error) {
	hits, err := s.SearchTrajectories(query, limit)
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("<context-hints>\n")
	for _, h := range hits {
		t := h.Trajectory
		label := ""
		if t.Judge != nil {
			label = t.Judge.Name
		}
		b.WriteString("- traj ")
		b.WriteString(t.ID)
		b.WriteString(" (")
		b.WriteString(label)
		b.WriteString("): ")
		b.WriteString(truncateHint(t.Task, 120))
		b.WriteByte('\n')
	}
	b.WriteString("</context-hints>")
	return b.String(), nil
}

func truncateHint(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
