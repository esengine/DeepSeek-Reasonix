package plancontract

import (
	"slices"
	"strings"
	"testing"
)

// listItems returns the markdown list items in text, mirroring the rule a
// markdown task-list reader applies: a bullet or numbered marker after any
// indentation. Render's structural promise is that these are exactly the steps.
func listItems(text string) []string {
	var out []string
	for raw := range strings.SplitSeq(text, "\n") {
		line := strings.TrimLeft(raw, " \t")
		matched := false
		for _, marker := range []string{"- ", "* ", "+ "} {
			if rest, ok := strings.CutPrefix(line, marker); ok {
				out = append(out, strings.TrimSpace(rest))
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		digits := 0
		for digits < len(line) && line[digits] >= '0' && line[digits] <= '9' {
			digits++
		}
		if digits > 0 && digits+1 < len(line) && (line[digits] == '.' || line[digits] == ')') && line[digits+1] == ' ' {
			out = append(out, strings.TrimSpace(line[digits+2:]))
		}
	}
	return out
}

func richPlan() Plan {
	return Plan{
		Objective:   "make the cache key model-aware",
		Assumptions: []Assumption{{Text: "- warm caches are disposable", Confirm: "rg cacheKey internal/provider"}},
		NonGoals:    []string{"1. rewriting the retry loop"},
		Steps: []Step{
			{
				ID: "p1", Title: "thread the model ref through",
				VerifiedFiles:  []string{"internal/provider/cache.go"},
				CandidateFiles: []string{"internal/boot/boot.go"},
				Risks:          []string{"existing warm caches invalidate once"},
			},
			{
				ID: "s1", ParentID: "p1", Title: "extend cacheKey",
				Acceptance:   []Criterion{{Text: "two model refs never share an entry"}, {Text: "existing hits keep hitting", Regression: true}},
				Verification: []Verification{{Command: "go test ./internal/provider/", Expect: "all green"}},
			},
			{ID: "p2", Title: "record the hit rate"},
		},
	}
}

func TestRenderEmitsOnlyStepsAsListItems(t *testing.T) {
	got := listItems(Render(richPlan()))
	want := []string{
		"thread the model ref through",
		"extend cacheKey",
		"record the hit rate",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("list items = %v, want exactly the steps %v", got, want)
	}
}

func TestRenderListItemsMatchTheProjectedTodos(t *testing.T) {
	p := richPlan()
	items := listItems(Render(p))
	todos := ProjectTodos(p)
	if len(items) != len(todos) {
		t.Fatalf("rendered %d list items but projected %d todos", len(items), len(todos))
	}
	for i := range todos {
		if items[i] != todos[i].Content {
			t.Errorf("item %d = %q, todo = %q", i, items[i], todos[i].Content)
		}
	}
}

func TestRenderKeepsStepDetailOffTheList(t *testing.T) {
	out := Render(richPlan())
	for _, want := range []string{
		"   verified: internal/provider/cache.go",
		"   candidate: internal/boot/boot.go",
		"   risk: existing warm caches invalidate once",
		"     accept [c1]: two model refs never share an entry",
		"     regression [c2]: existing hits keep hitting",
		"     verify: go test ./internal/provider/ — all green",
	} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("rendered plan missing detail line %q:\n%s", want, out)
		}
	}
}

func TestRenderOmitsEmptySections(t *testing.T) {
	out := Render(Plan{Objective: "just do it", Steps: []Step{{Title: "do it"}}})
	for _, absent := range []string{"Assumptions", "Non-goals", "verified:", "verify:", "risk:"} {
		if strings.Contains(out, absent) {
			t.Errorf("rendered plan should omit %q:\n%s", absent, out)
		}
	}
	if !strings.Contains(out, "**Objective** — just do it") || !strings.Contains(out, "1. do it") {
		t.Fatalf("rendered plan lost its content:\n%s", out)
	}
}

func TestRenderIsEmptyForAnEmptyPlan(t *testing.T) {
	if got := Render(Plan{}); got != "" {
		t.Fatalf("Render(empty) = %q, want empty", got)
	}
}

func TestRenderNumbersPhasesInProjectionOrder(t *testing.T) {
	out := Render(Plan{Objective: "o", Steps: []Step{
		{ID: "late", Title: "second", DependsOn: []string{"early"}},
		{ID: "early", Title: "first"},
	}})
	first := strings.Index(out, "1. first")
	second := strings.Index(out, "2. second")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("phases not renumbered in dependency order:\n%s", out)
	}
}
