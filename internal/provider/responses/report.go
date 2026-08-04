package responses

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 结构化报告合成（deep-research Phase 5 代码化）：把一次/多次检索得到的
// KnowledgeEntry 合成带引用、置信度、时效与冲突标注的结构化报告。

// ReportSection is one theme/fact cluster in a synthesized report.
type ReportSection struct {
	Title   string
	Facts   []string
	Sources []Source
	// Confidence aggregates the credibility of the section's sources.
	Confidence float64
}

// KnowledgeReport is the synthesized structured report.
type KnowledgeReport struct {
	Topic         string
	GeneratedAt   time.Time
	Summary       string
	Sections      []ReportSection
	AllSources    []Source
	AvgConfidence float64
	HasConflict   bool // P4 event conflict detected
	StaleNote     string
}

// SynthesizeReport merges one or more knowledge entries into a structured
// report: dedupes facts, groups by key-fact overlap, aggregates confidence,
// flags conflicts, and notes staleness. Empty entries yield a minimal report.
func SynthesizeReport(topic string, entries []*KnowledgeEntry) KnowledgeReport {
	r := KnowledgeReport{Topic: topic, GeneratedAt: time.Now()}
	if len(entries) == 0 {
		return r
	}

	seenFact := map[string]bool{}
	var factList []string
	sourceSet := map[string]Source{}
	var totalConf float64
	sourceCount := 0

	for _, e := range entries {
		if e == nil {
			continue
		}
		if e.AnswerSummary != "" && r.Summary == "" {
			r.Summary = e.AnswerSummary
		}
		if e.ConflictDetected {
			r.HasConflict = true
		}
		if e.NeedsRefresh(time.Now()) {
			r.StaleNote = "部分信息来自过期缓存，建议联网刷新后核实。"
		}
		for _, f := range e.KeyFacts {
			key := strings.TrimSpace(f)
			if key != "" && !seenFact[key] {
				seenFact[key] = true
				factList = append(factList, key)
			}
		}
		for _, s := range e.Sources {
			if s.URL == "" {
				continue
			}
			if _, ok := sourceSet[s.URL]; !ok {
				sourceSet[s.URL] = s
			}
			totalConf += s.Credibility
			sourceCount++
		}
	}

	// 单个事实段 + 来源段
	var section ReportSection
	section.Title = "关键事实"
	section.Facts = factList
	for _, s := range sourceSet {
		section.Sources = append(section.Sources, s)
		section.Confidence += s.Credibility
		r.AllSources = append(r.AllSources, s)
	}
	if len(section.Sources) > 0 {
		section.Confidence /= float64(len(section.Sources))
	}
	if len(section.Facts) > 0 || len(section.Sources) > 0 {
		r.Sections = append(r.Sections, section)
	}

	if sourceCount > 0 {
		r.AvgConfidence = totalConf / float64(sourceCount)
	}
	sort.Slice(r.AllSources, func(i, j int) bool {
		return r.AllSources[i].Credibility > r.AllSources[j].Credibility
	})
	return r
}

// Render produces the markdown report (deep-research Phase 5 style).
func (r KnowledgeReport) Render() string {
	var b strings.Builder
	b.WriteString("# " + r.Topic + " — 信息报告\n\n")
	fmt.Fprintf(&b, "> 生成时间: %s | 平均可信度: %.2f\n\n", r.GeneratedAt.Format("2006-01-02 15:04"), r.AvgConfidence)
	if r.HasConflict {
		b.WriteString("> ⚠️ 检测到多源矛盾（冲突信号），以下结论需谨慎。\n\n")
	}
	if r.StaleNote != "" {
		b.WriteString("> ⚠️ " + r.StaleNote + "\n\n")
	}
	if r.Summary != "" {
		b.WriteString("## 摘要\n\n" + r.Summary + "\n\n")
	}
	for _, s := range r.Sections {
		b.WriteString("## " + s.Title + "\n\n")
		if len(s.Facts) > 0 {
			for _, f := range s.Facts {
				b.WriteString("- " + f + "\n")
			}
			b.WriteString("\n")
		}
		if len(s.Sources) > 0 {
			b.WriteString("**来源**（可信度排序）：\n")
			sorted := append([]Source(nil), s.Sources...)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].Credibility > sorted[j].Credibility })
			for _, src := range sorted {
				fmt.Fprintf(&b, "- %s（%.2f）%s\n", src.Title, src.Credibility, src.URL)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// RenderSummary produces the concise first-pass report (摘要优先交互)：
// 结论摘要 + 关键指标 + 来源标注 + 详细问询提示。用户确认后再 Render()
// 出完整报告。
func (r KnowledgeReport) RenderSummary() string {
	var b strings.Builder
	b.WriteString("# " + r.Topic + " — 信息摘要\n\n")
	if r.Summary != "" {
		// 摘要截断到 ~300 字符（用户可要求详细）
		runes := []rune(r.Summary)
		if len(runes) > 300 {
			b.WriteString(string(runes[:300]) + "…\n\n")
		} else {
			b.WriteString(r.Summary + "\n\n")
		}
	}
	fmt.Fprintf(&b, "> 平均可信度: %.2f | 关键事实: %d 条 | 来源: %d 个\n",
		r.AvgConfidence, len(r.MergedFactCount()), len(r.AllSources))
	if len(r.AllSources) > 0 {
		b.WriteString("\n**信息来源**：\n")
		for i, s := range r.AllSources {
			if i >= 5 {
				fmt.Fprintf(&b, "- 等 %d 个来源…\n", len(r.AllSources)-5)
				break
			}
			fmt.Fprintf(&b, "- %s（%s）", s.Title, s.Domain)
			if s.URL != "" {
				b.WriteString(" " + s.URL)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n> 回复「详细」获取完整报告数据。\n")
	return b.String()
}

// MergedFactCount returns the total number of facts across sections.
func (r KnowledgeReport) MergedFactCount() []string {
	var out []string
	for _, s := range r.Sections {
		out = append(out, s.Facts...)
	}
	return out
}
