package responses

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 因果线结构化（情报模式）：把"信号→推断→行动→后果"四阶段链从检索结果
// 中提炼成可查询、可计算预警窗口、可渲染的数据结构。对应美伊战争情报
// 案例（加油机东调→E-4B→B-2集结→打击）的代码化。

// CausalStage is one phase of a causal chain.
type CausalStage string

const (
	// StageSignal is an observable/trackable signal (tanker movement, E-4B
	// sortie, carrier redirect).
	StageSignal CausalStage = "signal"
	// StageInference is the analyst conclusion drawn from signals.
	StageInference CausalStage = "inference"
	// StageAction is the military/political action.
	StageAction CausalStage = "action"
	// StageConsequence is the aftermath.
	StageConsequence CausalStage = "consequence"
)

// CausalEvent is one node in a causal chain.
type CausalEvent struct {
	Stage  CausalStage
	Time   string // free-form timestamp ("6月14日" / "2025-06-17 18:00")
	Detail string
	// Signal names the trackable artifact when Stage==signal.
	Signal string
	Source string // provenance URL when known
}

// CausalChain is the full signal→inference→action→consequence timeline.
type CausalChain struct {
	Topic  string
	Events []CausalEvent
	// WarningWindow is the signal→action lead time (the OSINT 预警窗口:
	// 油机东调到实际打击约 7 天).
	WarningWindow time.Duration
	Confidence    float64
	Sources       []string
}

// NewCausalChain builds an empty chain for a topic.
func NewCausalChain(topic string) *CausalChain {
	return &CausalChain{Topic: topic}
}

// Add appends an event, keeping stage order stable (signal < inference <
// action < consequence) at insertion time.
func (c *CausalChain) Add(e CausalEvent) {
	if c == nil {
		return
	}
	c.Events = append(c.Events, e)
	sort.SliceStable(c.Events, func(i, j int) bool {
		return stageRank(c.Events[i].Stage) < stageRank(c.Events[j].Stage)
	})
}

// FromEventStream derives a causal chain from an event-tracked KnowledgeEntry
// (P4 integration): the entry's event chain + key facts become signal-stage
// nodes; updates become action/consequence. Returns nil on empty input.
func FromEventStream(topic string, e *KnowledgeEntry) *CausalChain {
	if e == nil {
		return nil
	}
	c := NewCausalChain(topic)
	c.Confidence = e.Confidence
	// Key facts of the entry are the observable signals.
	for _, f := range e.KeyFacts {
		c.Add(CausalEvent{Stage: StageSignal, Time: e.CreatedAt.Format("2006-01-02"), Detail: f, Signal: f})
	}
	// Linked events are the surrounding context (inference/consequence).
	for _, linked := range e.EventChain {
		c.Add(CausalEvent{Stage: StageInference, Time: "", Detail: "关联事件: " + linked})
	}
	for _, s := range e.Sources {
		if s.URL != "" {
			c.Sources = append(c.Sources, s.URL)
		}
	}
	return c
}

// ComputeWarningWindow estimates the signal→action lead time from the chain:
// the duration between the earliest signal-stage event and the first action
// stage. Returns 0 when either stage is missing or timestamps are unparsable.
func (c *CausalChain) ComputeWarningWindow() time.Duration {
	if c == nil {
		return 0
	}
	var firstSignal, firstAction time.Time
	for _, e := range c.Events {
		t, err := parseChainTime(e.Time)
		if err != nil {
			continue
		}
		if e.Stage == StageSignal && (firstSignal.IsZero() || t.Before(firstSignal)) {
			firstSignal = t
		}
		if e.Stage == StageAction && (firstAction.IsZero() || t.Before(firstAction)) {
			firstAction = t
		}
	}
	if firstSignal.IsZero() || firstAction.IsZero() || !firstAction.After(firstSignal) {
		return 0
	}
	return firstAction.Sub(firstSignal)
}

// parseChainTime accepts "2006-01-02", "2006-01-02 15:04", or short forms
// like "6月14日" (assumed within the current year).
func parseChainTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02", "2006/01/02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	// 6月14日 / 6月14日 18:00（用 rune 索引避免 UTF-8 字节偏移）
	runes := []rune(s)
	if i := strings.IndexRune(s, '月'); i > 0 {
		monthStr := s[:i]
		rest := string(runes[len([]rune(monthStr))+1:])
		var month, day, hour, min int
		if j := strings.IndexRune(rest, '日'); j > 0 {
			if _, err := fmt.Sscanf(rest[:j], "%d", &day); err != nil {
				return time.Time{}, err
			}
			tail := rest[j+1:]
			_, _ = fmt.Sscanf(tail, "%d:%d", &hour, &min)
		}
		if _, err := fmt.Sscanf(monthStr, "%d", &month); err != nil {
			return time.Time{}, err
		}
		if month < 1 || month > 12 || day < 1 || day > 31 {
			return time.Time{}, fmt.Errorf("invalid month/day")
		}
		return time.Date(time.Now().Year(), time.Month(month), day, hour, min, 0, 0, time.Local), nil
	}
	return time.Time{}, fmt.Errorf("unparsable: %q", s)
}

func stageRank(s CausalStage) int {
	switch s {
	case StageSignal:
		return 0
	case StageInference:
		return 1
	case StageAction:
		return 2
	case StageConsequence:
		return 3
	default:
		return 4
	}
}

// Render produces a markdown causal-chain report.
func (c *CausalChain) Render() string {
	if c == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("# " + c.Topic + " — 因果线\n\n")
	if c.WarningWindow > 0 {
		fmt.Fprintf(&b, "> 预警窗口（信号→行动）: %s\n\n", c.WarningWindow.Round(time.Hour))
	}
	if c.Confidence > 0 {
		fmt.Fprintf(&b, "> 置信度: %.2f\n\n", c.Confidence)
	}
	stageLabels := map[CausalStage]string{
		StageSignal:      "🔍 信号",
		StageInference:   "🧠 推断",
		StageAction:      "⚡ 行动",
		StageConsequence: "📉 后果",
	}
	// Group by stage for a readable chain.
	for _, st := range []CausalStage{StageSignal, StageInference, StageAction, StageConsequence} {
		var staged []CausalEvent
		for _, e := range c.Events {
			if e.Stage == st {
				staged = append(staged, e)
			}
		}
		if len(staged) == 0 {
			continue
		}
		b.WriteString("## " + stageLabels[st] + "\n\n")
		for _, e := range staged {
			t := e.Time
			if t == "" {
				t = "—"
			}
			fmt.Fprintf(&b, "- [%s] %s", t, e.Detail)
			if e.Signal != "" && e.Signal != e.Detail {
				fmt.Fprintf(&b, "（信号: %s）", e.Signal)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(c.Sources) > 0 {
		b.WriteString("## 来源\n\n")
		for _, s := range c.Sources {
			b.WriteString("- " + s + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}
