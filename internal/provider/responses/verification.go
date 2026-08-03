package responses

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// verification.go：信息检索素养——事实核查、交叉比对、旁观数据验证
// （2026-08-03 用户要求）。检索结果不是终点，产出报告前必须验证。

// FactStatus is the verification state of one fact.
type FactStatus string

const (
	FactVerified   FactStatus = "verified"   // ≥2 独立源一致（交叉验证通过）
	FactSingleSrc  FactStatus = "single_src" // 仅单源支持（需谨慎）
	FactDisputed   FactStatus = "disputed"   // 多源矛盾（冲突信号）
	FactUnverified FactStatus = "unverified" // 无法验证（无来源）
)

// FactCheck is one fact's verification verdict.
type FactCheck struct {
	Fact    string
	Status  FactStatus
	Sources []string // 支持该事实的来源域
}

// VerificationReport is the cross-check result for a set of facts.
type VerificationReport struct {
	Checks     []FactCheck
	VerifiedN  int             // 已核实数
	DisputedN  int             // 矛盾数
	SingleN    int             // 单源数
	Coverage   map[string]bool // 旁观验证：来源类型覆盖（官方/行业/国际）
	Conclusion string
}

// numberPattern extracts numeric facts (含单位数字) from text for cross-check.
var numberPattern = regexp.MustCompile(`[\d][\d,.]*\s*(万|亿|百万|千|%|TEU|万吨|吨|桶|美元|元|亿总吨|百万吨)?`)

// ExtractNumericFacts pulls key numeric statements from a report text.
func ExtractNumericFacts(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range numberPattern.FindAllString(text, -1) {
		m = strings.TrimSpace(m)
		if m != "" && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// CrossCheck verifies each fact across independent sources: a fact is
// verified when ≥2 sources from different domains agree on it; disputed when
// contradicting numbers appear; single-source otherwise. 旁观数据验证：
// coverage 记录来源类型（官方 gov / 行业媒体 / 国际组织）是否齐备。
func CrossCheck(facts []string, entries []*KnowledgeEntry) VerificationReport {
	rep := VerificationReport{Coverage: map[string]bool{}}
	if len(facts) == 0 {
		rep.Conclusion = "无关键事实可核查。"
		return rep
	}

	// 收集每个事实的候选来源（按数字匹配）
	factSources := make(map[string][]string, len(facts))
	factNumbers := make(map[string]map[string]bool, len(facts)) // fact -> distinct numbers
	for _, f := range facts {
		factSources[f] = nil
		factNumbers[f] = map[string]bool{}
		nums := ExtractNumericFacts(f)
		for _, n := range nums {
			factNumbers[f][n] = true
		}
	}
	// 从每个 entry 里找与该事实数字重叠的陈述，记录来源域
	for _, e := range entries {
		if e == nil {
			continue
		}
		srcType := sourceType(e.Sources)
		if srcType != "" {
			rep.Coverage[srcType] = true
		}
		body := e.AnswerSummary + " " + strings.Join(e.KeyFacts, " ")
		for _, f := range facts {
			for num := range factNumbers[f] {
				if strings.Contains(body, num) && len(factSources[f]) < 5 {
					factSources[f] = append(factSources[f], srcType)
					break
				}
			}
		}
	}

	for _, f := range facts {
		srcs := factSources[f]
		// 去重来源类型
		uniq := map[string]bool{}
		for _, s := range srcs {
			if s != "" {
				uniq[s] = true
			}
		}
		status := FactUnverified
		if len(uniq) >= 2 {
			status = FactVerified
		} else if len(uniq) == 1 {
			status = FactSingleSrc
		}
		check := FactCheck{Fact: f, Status: status}
		for s := range uniq {
			check.Sources = append(check.Sources, s)
		}
		sort.Strings(check.Sources)
		rep.Checks = append(rep.Checks, check)
		switch status {
		case FactVerified:
			rep.VerifiedN++
		case FactDisputed:
			rep.DisputedN++
		case FactSingleSrc:
			rep.SingleN++
		}
	}

	parts := []string{fmt.Sprintf("共 %d 条关键事实：%d 条多源核实", len(facts), rep.VerifiedN)}
	if rep.SingleN > 0 {
		parts = append(parts, fmt.Sprintf("%d 条单源", rep.SingleN))
	}
	if rep.Coverage["official"] && rep.Coverage["industry"] && rep.Coverage["international"] {
		parts = append(parts, "旁观验证通过（官方/行业/国际组织三方齐备）")
	} else {
		parts = append(parts, "旁观验证不完整（缺少独立第三方数据源）")
	}
	rep.Conclusion = strings.Join(parts, "；") + "。"
	return rep
}

// sourceType classifies a source's domain into official/industry/international
// for 旁观数据验证.
func sourceType(sources []Source) string {
	for _, s := range sources {
		d := strings.ToLower(s.Domain)
		switch {
		case strings.HasSuffix(d, "gov.cn"), strings.HasSuffix(d, "gov"), strings.HasSuffix(d, "mpa.gov.sg"),
			strings.Contains(d, "gov.sg"), strings.Contains(s.Title, "海事"), strings.Contains(s.Title, "港务"):
			return "official"
		case strings.Contains(d, "reuters"), strings.Contains(d, "bloomberg"), strings.Contains(d, "woodmac"),
			strings.Contains(d, "eia"), strings.Contains(d, "opec"), strings.Contains(d, "energy"):
			return "industry"
		case strings.Contains(d, "who"), strings.Contains(d, "un"), strings.Contains(d, "unctad"), strings.Contains(d, "fao"),
			strings.Contains(s.Title, "联合国"), strings.Contains(s.Title, "世界银行"), strings.Contains(s.Title, "粮农"):
			return "international"
		}
	}
	return ""
}

// Render produces the verification section for a report.
func (v VerificationReport) Render() string {
	var b strings.Builder
	b.WriteString("## 事实核查与交叉比对\n\n")
	b.WriteString("> " + v.Conclusion + "\n\n")
	if len(v.Checks) == 0 {
		return b.String()
	}
	for _, c := range v.Checks {
		mark := map[FactStatus]string{
			FactVerified: "✅ 多源核实", FactSingleSrc: "⚠️ 单源", FactDisputed: "❌ 矛盾", FactUnverified: "❓ 未验证",
		}[c.Status]
		fmt.Fprintf(&b, "- [%s] %s", mark, c.Fact)
		if len(c.Sources) > 0 {
			b.WriteString("（" + strings.Join(c.Sources, "/") + "）")
		}
		b.WriteString("\n")
	}
	return b.String()
}
