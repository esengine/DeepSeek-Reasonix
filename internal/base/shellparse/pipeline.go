package shellparse

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// TerminalPipelines returns every pipeline that could be the last thing the
// command runs, each as its stage sources left to right. bash's PIPESTATUS
// describes whichever one actually ran, and a caller can tell which when the
// candidates differ in stage count. `go vet ./... && go test ./... | tail` has
// candidates of one and two stages: two statuses can only be the second.
func TerminalPipelines(command string) ([][]string, bool) {
	if strings.TrimSpace(command) == "" {
		return nil, false
	}
	file, err := ParseBash(command)
	if err != nil || HasHereDoc(file) || len(file.Stmts) == 0 {
		return nil, false
	}
	// Only the last `;`-separated statement can be the last thing to run;
	// earlier ones always continue into it.
	return terminalPipelinesOf(command, file.Stmts[len(file.Stmts)-1])
}

func terminalPipelinesOf(source string, stmt *syntax.Stmt) ([][]string, bool) {
	if stmt == nil || stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown {
		return nil, false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		if cmd.Op == syntax.Pipe || cmd.Op == syntax.PipeAll {
			var stages []string
			if !appendPipelineStages(source, stmt, &stages) {
				return nil, false
			}
			return [][]string{stages}, true
		}
		// `&&` and `||` both stop early, so either side can be the last to run.
		left, okLeft := terminalPipelinesOf(source, cmd.X)
		right, okRight := terminalPipelinesOf(source, cmd.Y)
		if !okLeft || !okRight {
			return nil, false
		}
		return append(left, right...), true
	case *syntax.CallExpr:
		return [][]string{{sourceForStmt(source, stmt)}}, true
	default:
		return nil, false
	}
}

// PipelineForStatus picks the candidate whose stage count matches a captured
// PIPESTATUS. A tie means two pipelines of the same width could have produced
// it, and guessing between them would attribute a status to a command that
// never ran.
func PipelineForStatus(command string, statusLen int) ([]string, bool) {
	if statusLen <= 0 {
		return nil, false
	}
	candidates, ok := TerminalPipelines(command)
	if !ok {
		return nil, false
	}
	var match []string
	for _, stages := range candidates {
		if len(stages) != statusLen {
			continue
		}
		if match != nil {
			return nil, false
		}
		match = stages
	}
	return match, match != nil
}

func appendPipelineStages(source string, stmt *syntax.Stmt, stages *[]string) bool {
	if stmt == nil || stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown {
		return false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		if cmd.Op != syntax.Pipe && cmd.Op != syntax.PipeAll {
			return false
		}
		return appendPipelineStages(source, cmd.X, stages) &&
			appendPipelineStages(source, cmd.Y, stages)
	case *syntax.CallExpr:
		*stages = append(*stages, sourceForStmt(source, stmt))
		return true
	default:
		return false
	}
}
