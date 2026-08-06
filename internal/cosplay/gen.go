package cosplay

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// TestGenerator produces discriminating test cases. Implementations may be
// model-backed (LLM generates adversarial inputs) or template-backed (offline
// smoke + example-derived assertions). The verifier needs at least one test
// to build a matrix; more discriminating tests make repair and consensus
// sharper.
type TestGenerator interface {
	// Generate returns up to n test cases for the task/candidate.
	Generate(ctx context.Context, task Task, code string, n int) []TestCase
}

// Repairer fixes a failing candidate. It receives the code, the current
// failure set, and the task; it returns repaired code (empty or unchanged
// means "no repair possible" and stops the loop).
type Repairer interface {
	Repair(ctx context.Context, task Task, code string, failures []Failure, language string) string
}

// TemplateGenerator is a fully offline TestGenerator. It has no model and no
// network:
//
//  1. For every task Example it emits an assertion test (the highest-value
//     discriminating signal — a candidate that passes smoke but fails an
//     example is exactly what co-evolution exists to catch).
//  2. It then fills up to n with language smoke tests (compile/import check).
//
// This keeps the entire co-evolution cycle runnable in CI without any model,
// while a ModelGenerator (added by the host) can inject stronger adversarial
// cases at runtime.
type TemplateGenerator struct{}

// Generate implements TestGenerator.
func (TemplateGenerator) Generate(ctx context.Context, task Task, code string, n int) []TestCase {
	if n <= 0 {
		n = 4
	}
	var out []TestCase
	// 1. Example-derived assertion tests — the discriminating core.
	for i, ex := range task.Examples {
		if len(out) >= n {
			break
		}
		if body := assertionBody(task, ex); body != "" {
			out = append(out, TestCase{
				ID:       fmt.Sprintf("ex-%d", i),
				Language: task.Language,
				Body:     body,
				Source:   "template",
				Input:    ex.Input,
				Expected: ex.Expected,
			})
		}
	}
	// 2. Smoke tests fill the remaining budget.
	for i := len(out); i < n; i++ {
		if body := smokeBody(task.Language); body != "" {
			out = append(out, TestCase{
				ID:       fmt.Sprintf("smoke-%d", i),
				Language: task.Language,
				Body:     body,
				Source:   "template",
			})
		}
	}
	return out
}

// assertionBody renders a runnable assertion for one example, per language.
// Returns "" when the language has no template (the verifier then relies on
// smoke tests and candidate tests).
func assertionBody(task Task, ex Example) string {
	fn := task.Function
	call := fmt.Sprintf("%s(%s)", fn, ex.Input)
	if fn == "" {
		call = ex.Input // caller supplied a full invocation expression
	}
	expected := lit(ex.Expected)
	switch strings.ToLower(task.Language) {
	case "go":
		return fmt.Sprintf(`package main
import "fmt"
func main() {
	want := %s
	got := %s
	fmt.Printf("GOT:%%v", got)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		fmt.Printf("EXPECTED:%%v", want)
	}
}`, expected, call)
	case "python":
		return fmt.Sprintf(`import sys
def main():
    want = %s
    got = %s
    print("GOT:", got)
    if str(got) != str(want):
        print("EXPECTED:", want)
    else:
        print("PASS")
main()
`, expected, call)
	case "javascript":
		return fmt.Sprintf(`const want = %s;
const got = %s;
console.log("GOT:", got);
if (JSON.stringify(got) !== JSON.stringify(want)) {
  console.log("EXPECTED:", JSON.stringify(want));
} else {
  console.log("PASS");
}`, expected, call)
	default:
		return ""
	}
}

// smokeBody emits a minimal compile/import smoke test per language.
func smokeBody(lang string) string {
	switch strings.ToLower(lang) {
	case "go":
		return `package main
import "fmt"
func main() { fmt.Println("SMOKE") }
`
	case "python":
		return "print(\"SMOKE\")\n"
	case "javascript":
		return "console.log(\"SMOKE\");\n"
	default:
		return ""
	}
}

// --- literal quoting helpers ---

// lit renders a literal for the target language: numeric-looking values are
// emitted bare (so 42 compares as a number), everything else is quoted.
func lit(s string) string {
	if isNumeric(s) {
		return s
	}
	return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
