package cli

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

var kindMath = ast.NewNodeKind("Math")

type mathNode struct {
	ast.BaseInline
	value   string
	display bool
}

func (n *mathNode) Kind() ast.NodeKind         { return kindMath }
func (n *mathNode) Dump(src []byte, level int) { ast.DumpHelper(n, src, level, nil, nil) }

type mathParser struct{}

func (p *mathParser) Trigger() []byte { return []byte{'$'} }

func (p *mathParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) == 0 || line[0] != '$' {
		return nil
	}
	display := len(line) >= 2 && line[1] == '$'
	delim := 1
	if display {
		delim = 2
	}

	rest := line[delim:]
	var closeAt int
	if display {
		closeAt = bytes.Index(rest, []byte("$$"))
	} else {
		closeAt = bytes.IndexByte(rest, '$')
	}
	if closeAt < 0 {
		return nil
	}
	inner := rest[:closeAt]
	if len(bytes.TrimSpace(inner)) == 0 {
		return nil
	}

	// Currency guard (markdown-it-texmath rule): a single-$ span only counts as
	// math when the open isn't followed by space, the close isn't preceded by
	// space, and the char after the close isn't a digit — so "$5 and $10" stays
	// prose. Display $$ is unambiguous and skips the check.
	if !display {
		after := closeAt + 1
		if inner[0] == ' ' || inner[len(inner)-1] == ' ' ||
			(after < len(rest) && rest[after] >= '0' && rest[after] <= '9') {
			return nil
		}
	}

	block.Advance(delim + closeAt + delim)
	return &mathNode{value: latexToUnicode(string(bytes.TrimSpace(inner))), display: display}
}
