package responses

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 信息帧数据建模（2026-08-03 用户要求）：并行子代理检索 → 各帧 → 信息
// 拼图合成完整认知。每帧是一个场景/人群/语言视角的检索结果切片；拼图
// 合并多帧为完整信息面。

// InfoFrame is one retrieval slice: a scene/audience/language viewport.
type InfoFrame struct {
	Domain      InfoDomain    `json:"domain"`
	Audience    SceneAudience `json:"audience,omitempty"`
	Language    string        `json:"language"`
	Topic       string        `json:"topic"`
	Facts       []string      `json:"facts,omitempty"`
	Sources     []Source      `json:"sources,omitempty"`
	Confidence  float64       `json:"confidence"`
	RetrievedAt time.Time     `json:"retrieved_at"`
}

// NewInfoFrame builds a frame with retrieval timestamp.
func NewInfoFrame(domain InfoDomain, audience SceneAudience, lang, topic string) *InfoFrame {
	return &InfoFrame{
		Domain: domain, Audience: audience, Language: lang, Topic: topic,
		RetrievedAt: time.Now(),
	}
}

// FrameView is the merged result of many frames (信息拼图).
type FrameView struct {
	Topic         string
	Frames        []InfoFrame
	MergedFacts   []string
	AllSources    []Source
	AvgConfidence float64
	Languages     []string
	Domains       []InfoDomain
}

// MergeFrames assembles the complete information picture from parallel
// retrieval frames: dedupes facts, aggregates sources, averages confidence,
// and reports coverage (languages × domains).
func MergeFrames(topic string, frames []*InfoFrame) FrameView {
	v := FrameView{Topic: topic}
	if len(frames) == 0 {
		return v
	}
	seenFact := map[string]bool{}
	seenSrc := map[string]bool{}
	langSet := map[string]bool{}
	domSet := map[InfoDomain]bool{}
	var total float64
	n := 0

	for _, f := range frames {
		if f == nil {
			continue
		}
		v.Frames = append(v.Frames, *f)
		langSet[f.Language] = true
		domSet[f.Domain] = true
		for _, fact := range f.Facts {
			key := strings.TrimSpace(fact)
			if key != "" && !seenFact[key] {
				seenFact[key] = true
				v.MergedFacts = append(v.MergedFacts, key)
			}
		}
		for _, s := range f.Sources {
			if s.URL == "" || seenSrc[s.URL] {
				continue
			}
			seenSrc[s.URL] = true
			v.AllSources = append(v.AllSources, s)
		}
		if f.Confidence > 0 {
			total += f.Confidence
			n++
		}
	}
	if n > 0 {
		v.AvgConfidence = total / float64(n)
	}
	for l := range langSet {
		v.Languages = append(v.Languages, l)
	}
	for d := range domSet {
		v.Domains = append(v.Domains, d)
	}
	sort.Strings(v.Languages)
	sort.Slice(v.Domains, func(i, j int) bool { return v.Domains[i] < v.Domains[j] })
	sort.Slice(v.AllSources, func(i, j int) bool {
		return v.AllSources[i].Credibility > v.AllSources[j].Credibility
	})
	return v
}

// Render produces the markdown 信息拼图 report.
func (v FrameView) Render() string {
	if v.Topic == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("# " + v.Topic + " — 信息拼图\n\n")
	b.WriteString("> 覆盖语言: " + joinOrDash(v.Languages) + " | 覆盖场景: " + joinDomains(v.Domains) + "\n\n")
	if len(v.MergedFacts) > 0 {
		b.WriteString("## 拼图事实\n\n")
		for _, f := range v.MergedFacts {
			b.WriteString("- " + f + "\n")
		}
		b.WriteString("\n")
	}
	if len(v.AllSources) > 0 {
		b.WriteString("## 来源（可信度排序）\n\n")
		for _, s := range v.AllSources {
			fmt.Fprintf(&b, "- %s（%s，%s）%s\n", s.Title, s.Domain, formatConf(s.Credibility), s.URL)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func joinOrDash(items []string) string {
	if len(items) == 0 {
		return "—"
	}
	return strings.Join(items, "/")
}

func joinDomains(ds []InfoDomain) string {
	items := make([]string, 0, len(ds))
	for _, d := range ds {
		items = append(items, string(d))
	}
	return joinOrDash(items)
}

func formatConf(c float64) string {
	if c <= 0 {
		return "未评分"
	}
	return fmt.Sprintf("%.2f", c)
}
