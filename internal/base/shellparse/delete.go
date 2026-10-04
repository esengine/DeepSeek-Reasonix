package shellparse

import "mvdan.cc/sh/v3/syntax"

type DeleteCall struct {
	Name   string   `json:"name"`
	Args   []string `json:"args"`
	Unsafe bool     `json:"unsafe"`
}

type DeleteAnalysis struct {
	Standalone  bool         `json:"standalone"`
	Calls       []DeleteCall `json:"calls"`
	SyntaxError string       `json:"syntax_error"`
	HostError   bool         `json:"host_error"`
}

func AnalyzeDeleteCalls(command string) (DeleteAnalysis, error) {
	file, err := ParseBash(command)
	if err != nil {
		return DeleteAnalysis{}, err
	}
	out := DeleteAnalysis{Standalone: true}
	safe := make(map[*syntax.CallExpr]bool)
	var sequence func(*syntax.Stmt)
	sequence = func(stmt *syntax.Stmt) {
		if stmt.Background || stmt.Negated {
			return
		}
		switch expr := stmt.Cmd.(type) {
		case *syntax.CallExpr:
			safe[expr] = len(expr.Assigns) == 0
		case *syntax.BinaryCmd:
			if expr.Op == syntax.AndStmt || expr.Op == syntax.OrStmt {
				sequence(expr.X)
				sequence(expr.Y)
			}
		}
	}
	for _, stmt := range file.Stmts {
		sequence(stmt)
	}
	syntax.Walk(file, func(node syntax.Node) bool {
		if stmt, ok := node.(*syntax.Stmt); ok {
			switch stmt.Cmd.(type) {
			case nil, *syntax.CallExpr, *syntax.BinaryCmd:
			default:
				out.Calls = append(out.Calls, DeleteCall{Unsafe: true})
			}
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}
		if len(call.Args) == 0 {
			out.Calls = append(out.Calls, DeleteCall{Unsafe: true})
			return true
		}
		name, _ := StaticWord(call.Args[0])
		parsed := DeleteCall{Name: name, Unsafe: !safe[call]}
		for _, arg := range call.Args[1:] {
			value, _ := StaticWord(arg)
			parsed.Args = append(parsed.Args, value)
		}
		out.Calls = append(out.Calls, parsed)
		return true
	})
	return out, nil
}
