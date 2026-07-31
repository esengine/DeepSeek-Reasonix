// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"math"
	"strconv"
	"strings"
)

// Node represents a syntax tree node.
type Node interface {
	// Pos returns the position of the first character of the node. Comments
	// are ignored, except if the node is a [*File].
	Pos() Pos
	// End returns the position of the character immediately after the node.
	// If the character is a newline, the line number won't cross into the
	// next line. Comments are ignored, except if the node is a [*File].
	End() Pos
}

// File represents a shell source file.
type File struct {
	Name string

	Stmts []*Stmt
	Last  []Comment
}

func (f *File) Pos() Pos { return stmtsPos(f.Stmts, f.Last) }
func (f *File) End() Pos { return stmtsEnd(f.Stmts, f.Last) }

func stmtsPos(stmts []*Stmt, last []Comment) Pos {
	if len(stmts) > 0 {
		s := stmts[0]
		sPos := s.Pos()
		if len(s.Comments) > 0 {
			if cPos := s.Comments[0].Pos(); sPos.After(cPos) {
				return cPos
			}
		}
		return sPos
	}
	if len(last) > 0 {
		return last[0].Pos()
	}
	return Pos{}
}

func stmtsEnd(stmts []*Stmt, last []Comment) Pos {
	if len(last) > 0 {
		return last[len(last)-1].End()
	}
	if len(stmts) > 0 {
		s := stmts[len(stmts)-1]
		sEnd := s.End()
		if len(s.Comments) > 0 {
			if cEnd := s.Comments[0].End(); cEnd.After(sEnd) {
				return cEnd
			}
		}
		return sEnd
	}
	return Pos{}
}

// Pos is a position within a shell source file.
type Pos struct {
	offs, lineCol uint32
}

const (
	// Offsets use 32 bits for a reasonable amount of precision.
	// We reserve a few of the highest values to represent types of invalid positions.
	// We leave some space before the real uint32 maximum so that we can easily detect
	// when arithmetic on invalid positions is done by mistake.
	offsetRecovered = math.MaxUint32 - 10
	offsetMax       = math.MaxUint32 - 11

	// We used to split line and column numbers evenly in 16 bits, but line numbers
	// are significantly more important in practice. Use more bits for them.

	lineBitSize = 18
	lineMax     = (1 << lineBitSize) - 1

	colBitSize = 32 - lineBitSize
	colMax     = (1 << colBitSize) - 1
	colBitMask = colMax
)

// TODO(v4): consider using uint32 for Offset/Line/Col to better represent bit sizes.
// Or go with int64, which more closely resembles portable "sizes" elsewhere.
// The latter is probably nicest, as then we can change the number of internal
// bits later, and we can also do overflow checks for the user in NewPos.

// NewPos creates a position with the given offset, line, and column.
//
// Note that [Pos] uses a limited number of bits to store these numbers.
// If line or column overflow their allocated space, they are replaced with 0.
func NewPos(offset, line, column uint) Pos {
	// Basic protection against offset overflow;
	// note that an offset of 0 is valid, so we leave the maximum.
	offset = min(offset, offsetMax)
	if line > lineMax {
		line = 0 // protect against overflows; rendered as "?"
	}
	if column > colMax {
		column = 0 // protect against overflows; rendered as "?"
	}
	return Pos{
		offs:    uint32(offset),
		lineCol: (uint32(line) << colBitSize) | uint32(column),
	}
}

// Offset returns the byte offset of the position in the original source file.
// Byte offsets start at 0. Invalid positions always report the offset 0.
//
// Offset has basic protection against overflows; if an input is too large,
// offset numbers will stop increasing past a very large number.
func (p Pos) Offset() uint {
	if p.offs > offsetMax {
		return 0 // invalid
	}
	return uint(p.offs)
}

// Line returns the line number of the position, starting at 1.
// Invalid positions always report the line number 0.
//
// Line is protected against overflows; if an input has too many lines, extra
// lines will have a line number of 0, rendered as "?" by [Pos.String].
func (p Pos) Line() uint { return uint(p.lineCol >> colBitSize) }

// Col returns the column number of the position, starting at 1. It counts in
// bytes. Invalid positions always report the column number 0.
//
// Col is protected against overflows; if an input line has too many columns,
// extra columns will have a column number of 0, rendered as "?" by [Pos.String].
func (p Pos) Col() uint { return uint(p.lineCol & colBitMask) }

func (p Pos) String() string {
	var b strings.Builder
	if line := p.Line(); line > 0 {
		b.WriteString(strconv.FormatUint(uint64(line), 10))
	} else {
		b.WriteByte('?')
	}
	b.WriteByte(':')
	if col := p.Col(); col > 0 {
		b.WriteString(strconv.FormatUint(uint64(col), 10))
	} else {
		b.WriteByte('?')
	}
	return b.String()
}

// IsValid reports whether the position contains useful position information.
// Some positions returned via [Parse] may be invalid: for example, [Stmt.Semicolon]
// will only be valid if a statement contained a closing token such as ';'.
//
// Recovered positions, as reported by [Pos.IsRecovered], are not considered valid
// given that they don't contain position information.
func (p Pos) IsValid() bool {
	return p.offs <= offsetMax && p.lineCol != 0
}

var recoveredPos = Pos{offs: offsetRecovered}

// IsRecovered reports whether the position that the token or node belongs to
// was missing in the original input and recovered via [RecoverErrors].
func (p Pos) IsRecovered() bool { return p == recoveredPos }

// After reports whether the position p is after p2. It is a more expressive
// version of p.Offset() > p2.Offset().
// It always returns false if p is an invalid position.
func (p Pos) After(p2 Pos) bool {
	if !p.IsValid() {
		return false
	}
	return p.offs > p2.offs
}

func posAddCol(p Pos, n int) Pos {
	if !p.IsValid() {
		return p
	}
	// TODO: guard against overflows
	p.lineCol += uint32(n)
	p.offs += uint32(n)
	return p
}

func posMax(p1, p2 Pos) Pos {
	if p2.After(p1) {
		return p2
	}
	return p1
}

// Comment represents a single comment on a single line.
type Comment struct {
	Hash Pos
	Text string
}

func (c *Comment) Pos() Pos { return c.Hash }
func (c *Comment) End() Pos { return posAddCol(c.Hash, 1+len(c.Text)) }

// Stmt represents a statement, also known as a "complete command". It is
// compromised of a command and other components that may come before or after
// it.
type Stmt struct {
	Comments   []Comment
	Cmd        Command
	Position   Pos
	Semicolon  Pos  // position of ';', '&', or '|&', if any
	Negated    bool // ! stmt
	Background bool // stmt &
	Coprocess  bool // mksh's |&
	Disown     bool // zsh's &| or &!

	Redirs []*Redirect // stmt >a <b
}

func (s *Stmt) Pos() Pos { return s.Position }
func (s *Stmt) End() Pos {
	if s.Semicolon.IsValid() {
		end := posAddCol(s.Semicolon, 1) // ';' or '&'
		if s.Coprocess || s.Disown {
			end = posAddCol(end, 1) // '|&' or '&|' or '&!'
		}
		return end
	}
	end := s.Position
	if s.Negated {
		end = posAddCol(end, 1)
	}
	if s.Cmd != nil {
		end = s.Cmd.End()
	}
	if len(s.Redirs) > 0 {
		end = posMax(end, s.Redirs[len(s.Redirs)-1].End())
	}
	return end
}

// Command represents all nodes that are simple or compound commands, including
// function declarations.
//
// These are [*CallExpr], [*IfClause], [*WhileClause], [*ForClause], [*CaseClause],
// [*Block], [*Subshell], [*BinaryCmd], [*FuncDecl], [*ArithmCmd], [*TestClause],
// [*DeclClause], [*LetClause], [*TimeClause], and [*CoprocClause].
type Command interface {
	Node
	commandNode()
}

func (*CallExpr) commandNode()     {}
func (*IfClause) commandNode()     {}
func (*WhileClause) commandNode()  {}
func (*ForClause) commandNode()    {}
func (*CaseClause) commandNode()   {}
func (*Block) commandNode()        {}
func (*Subshell) commandNode()     {}
func (*BinaryCmd) commandNode()    {}
func (*FuncDecl) commandNode()     {}
func (*ArithmCmd) commandNode()    {}
func (*TestClause) commandNode()   {}
func (*DeclClause) commandNode()   {}
func (*LetClause) commandNode()    {}
func (*TimeClause) commandNode()   {}
func (*CoprocClause) commandNode() {}
func (*TestDecl) commandNode()     {}

// Assign represents an assignment to a variable.
//
// Here and elsewhere, Index can mean either an index expression into an indexed
// array, or a string key into an associative array.
//
// If Index is non-nil, the value will be a word and not an array as nested
// arrays are not allowed.
//
// If Naked is true and Name is nil, the assignment is part of a [DeclClause] and
// the argument (in the Value field) will be evaluated at run-time. This
// includes parameter expansions, which may expand to assignments or options.
type Assign struct {
	Append bool       // +=
	Naked  bool       // without '='
	Name   *Lit       // must be a valid name
	Index  ArithmExpr // [i], ["k"]
	Value  *Word      // =val
	Array  *ArrayExpr // =(arr)
}

func (a *Assign) Pos() Pos {
	if a.Name == nil {
		return a.Value.Pos()
	}
	return a.Name.Pos()
}

func (a *Assign) End() Pos {
	if a.Value != nil {
		return a.Value.End()
	}
	if a.Array != nil {
		return a.Array.End()
	}
	if a.Index != nil {
		return posAddCol(a.Index.End(), 2)
	}
	if a.Naked {
		return a.Name.End()
	}
	return posAddCol(a.Name.End(), 1)
}

// Redirect represents an input/output redirection.
type Redirect struct {
	OpPos Pos
	Op    RedirOperator
	N     *Lit  // fd>, or {varname}> in Bash
	Word  *Word // >word
	Hdoc  *Word // here-document body
}

func (r *Redirect) Pos() Pos {
	if r.N != nil {
		return r.N.Pos()
	}
	return r.OpPos
}

func (r *Redirect) End() Pos {
	if r.Hdoc != nil {
		return r.Hdoc.End()
	}
	return r.Word.End()
}

// CallExpr represents a command execution or function call, otherwise known as
// a "simple command".
//
// If Args is empty, Assigns apply to the shell environment. Otherwise, they are
// variables that cannot be arrays and which only apply to the call.
type CallExpr struct {
	Assigns []*Assign // a=x b=y args
	Args    []*Word
}

func (c *CallExpr) Pos() Pos {
	if len(c.Assigns) > 0 {
		return c.Assigns[0].Pos()
	}
	return c.Args[0].Pos()
}

func (c *CallExpr) End() Pos {
	if len(c.Args) == 0 {
		return c.Assigns[len(c.Assigns)-1].End()
	}
	return c.Args[len(c.Args)-1].End()
}

// Subshell represents a series of commands that should be executed in a nested
// shell environment.
type Subshell struct {
	Lparen, Rparen Pos

	Stmts []*Stmt
	Last  []Comment
}

func (s *Subshell) Pos() Pos { return s.Lparen }
func (s *Subshell) End() Pos { return posAddCol(s.Rparen, 1) }

// Block represents a series of commands that should be executed in a nested
// scope. It is essentially a list of statements within curly braces.
type Block struct {
	Lbrace, Rbrace Pos

	Stmts []*Stmt
	Last  []Comment
}

func (b *Block) Pos() Pos { return b.Lbrace }
func (b *Block) End() Pos { return posAddCol(b.Rbrace, 1) }

// IfClause represents an if statement.
type IfClause struct {
	Position Pos // position of the starting "if", "elif", or "else" token
	ThenPos  Pos // position of "then", empty if this is an "else"
	FiPos    Pos // position of "fi", shared with .Else if non-nil

	Cond     []*Stmt
	CondLast []Comment
	Then     []*Stmt
	ThenLast []Comment

	Else *IfClause // if non-nil, an "elif" or an "else"

	Last []Comment // comments on the first "elif", "else", or "fi"
}

func (c *IfClause) Pos() Pos { return c.Position }
func (c *IfClause) End() Pos { return posAddCol(c.FiPos, 2) }

// WhileClause represents a while or an until clause.
type WhileClause struct {
	WhilePos, DoPos, DonePos Pos
	Until                    bool

	Cond     []*Stmt
	CondLast []Comment
	Do       []*Stmt
	DoLast   []Comment
}

func (w *WhileClause) Pos() Pos { return w.WhilePos }
func (w *WhileClause) End() Pos { return posAddCol(w.DonePos, 4) }

// ForClause represents a for or a select clause. The latter is only present in
// Bash.
type ForClause struct {
	ForPos, DoPos, DonePos Pos
	Select                 bool
	Braces                 bool // deprecated form with { } instead of do/done
	Loop                   Loop

	Do     []*Stmt
	DoLast []Comment
}

func (f *ForClause) Pos() Pos { return f.ForPos }
func (f *ForClause) End() Pos { return posAddCol(f.DonePos, 4) }

// Loop holds either [*WordIter] or [*CStyleLoop].
type Loop interface {
	Node
	loopNode()
}

func (*WordIter) loopNode()   {}
func (*CStyleLoop) loopNode() {}

// WordIter represents the iteration of a variable over a series of words in a
// for clause. If InPos is an invalid position, the "in" token was missing, so
// the iteration is over the shell's positional parameters.
type WordIter struct {
	Name  *Lit
	InPos Pos // position of "in"
	Items []*Word
}

func (w *WordIter) Pos() Pos { return w.Name.Pos() }
func (w *WordIter) End() Pos {
	if len(w.Items) > 0 {
		return wordLastEnd(w.Items)
	}
	return posMax(w.Name.End(), posAddCol(w.InPos, 2))
}

// CStyleLoop represents the behavior of a for clause similar to the C
// language.
//
// This node will only appear with [LangBash].
type CStyleLoop struct {
	Lparen, Rparen Pos
	// Init, Cond, Post can each be nil, if the for loop construct omits it.
	Init, Cond, Post ArithmExpr
}

func (c *CStyleLoop) Pos() Pos { return c.Lparen }
func (c *CStyleLoop) End() Pos { return posAddCol(c.Rparen, 2) }

// BinaryCmd represents a binary expression between two statements.
type BinaryCmd struct {
	OpPos Pos
	Op    BinCmdOperator
	X, Y  *Stmt
}

func (b *BinaryCmd) Pos() Pos { return b.X.Pos() }
func (b *BinaryCmd) End() Pos { return b.Y.End() }

// FuncDecl represents the declaration of a function.
type FuncDecl struct {
	Position Pos
	RsrvWord bool // non-posix "function f" style
	Parens   bool // with () parentheses, can only be false when RsrvWord==true

	// Only one of these is set at a time.
	// Neither is set when declaring an anonymous func with [LangZsh].
	// TODO(v4): join these, even if it's mildly annoying to non-Zsh users.
	Name  *Lit
	Names []*Lit // When declaring many func names with [LangZsh].

	Body *Stmt
}

func (f *FuncDecl) Pos() Pos { return f.Position }
func (f *FuncDecl) End() Pos { return f.Body.End() }

// Word represents a shell word, containing one or more word parts contiguous to
// each other. The word is delimited by word boundaries, such as spaces,
// newlines, semicolons, or parentheses.
type Word struct {
	Parts []WordPart
}

func (w *Word) Pos() Pos { return w.Parts[0].Pos() }
func (w *Word) End() Pos { return w.Parts[len(w.Parts)-1].End() }

// Lit returns the word as a string when it is a simple literal,
// made up of [*Lit] word parts only.
// An empty string is returned otherwise.
//
// For example, the word "foo" will return "foo",
// but the word "foo${bar}" will return "".
func (w *Word) Lit() string {
	// In the usual case, we'll have either a single part that's a literal,
	// or one of the parts being a non-literal. Using strings.Join instead
	// of a strings.Builder avoids extra work in these cases, since a single
	// part is a shortcut, and many parts don't incur string copies.
	lits := make([]string, 0, 1)
	for _, part := range w.Parts {
		lit, ok := part.(*Lit)
		if !ok {
			return ""
		}
		lits = append(lits, lit.Value)
	}
	return strings.Join(lits, "")
}

// WordPart represents all nodes that can form part of a word.
//
// These are [*Lit], [*SglQuoted], [*DblQuoted], [*ParamExp], [*CmdSubst], [*ArithmExp],
// [*ProcSubst], and [*ExtGlob].
type WordPart interface {
	Node
	wordPartNode()
}

func (*Lit) wordPartNode()       {}
func (*SglQuoted) wordPartNode() {}
func (*DblQuoted) wordPartNode() {}
func (*ParamExp) wordPartNode()  {}
func (*CmdSubst) wordPartNode()  {}
func (*ArithmExp) wordPartNode() {}
func (*ProcSubst) wordPartNode() {}
func (*ExtGlob) wordPartNode()   {}
func (*BraceExp) wordPartNode()  {}

// Lit represents a string literal.
//
// Note that a parsed string literal may not appear as-is in the original source
// code, as it is possible to split literals by escaping newlines. The splitting
// is lost, but the end position is not.
type Lit struct {
	ValuePos, ValueEnd Pos
	Value              string
}

func (l *Lit) Pos() Pos { return l.ValuePos }
func (l *Lit) End() Pos { return l.ValueEnd }

// SglQuoted represents a string within single quotes.
type SglQuoted struct {
	Left, Right Pos
	Dollar      bool // $''
	Value       string
}

func (q *SglQuoted) Pos() Pos { return q.Left }
func (q *SglQuoted) End() Pos { return posAddCol(q.Right, 1) }

// DblQuoted represents a list of nodes within double quotes.
type DblQuoted struct {
	Left, Right Pos
	Dollar      bool // $""
	Parts       []WordPart
}

func (q *DblQuoted) Pos() Pos { return q.Left }
func (q *DblQuoted) End() Pos { return posAddCol(q.Right, 1) }

// CmdSubst represents a command substitution.
type CmdSubst struct {
	Left, Right Pos

	Stmts []*Stmt
	Last  []Comment

	Backquotes bool // deprecated `foo`
	TempFile   bool // mksh's ${ foo;}
	ReplyVar   bool // mksh's ${|foo;}
}

func (c *CmdSubst) Pos() Pos { return c.Left }
func (c *CmdSubst) End() Pos { return posAddCol(c.Right, 1) }

// ParamExp represents a parameter expansion.
type ParamExp struct {
	Dollar, Rbrace Pos

	// TODO(v4): replace Short for !Rbrace.IsValid()

	Short bool // $a instead of ${a}

	Flags *Lit // ${(flags)a} with [LangZsh]

	// Only one of these is set at a time.
	// TODO(v4): perhaps use an Operator token here,
	// given how we've grown the number of booleans
	// TODO(v4): rename Excl to reflect its purpose
	Excl   bool // ${!a}
	Length bool // ${#a}
	Width  bool // mksh's ${%a}
	IsSet  bool // ${+a} with [LangZsh]

	// Only one of these is set at a time,
	// or neither with [LangZsh] when the name is omitted.
	// TODO(v4): consider joining Param and NestedParam into a single field,
	// even if that would be mildly annoying to non-Zsh users.
	Param *Lit
	// A nested parameter expression in the form of [*ParamExp] or [*CmdSubst],
	// or either of those in a [*DblQuoted]. Only possible with [LangZsh].
	NestedParam WordPart

	// TODO(v4): rename Index to Subscript, which better matches bash and zsh terminology

	Index ArithmExpr // ${a[i]}, ${a["k"]}, or a ${a[i,j]} slice with [LangZsh]

	// Only one of these is set at a time.
	// TODO(v4): consider joining these in a single "expansion" field/type,
	// because it should be impossible for multiple to be set at once,
	// and a flat structure like this takes up more space.
	Modifiers []*Lit           // ${a:h2} with [LangZsh]
	Slice     *Slice           // ${a:x:y}
	Repl      *Replace         // ${a/x/y}
	Names     ParNamesOperator // ${!prefix*} or ${!prefix@}
	Exp       *Expansion       // ${a:-b}, ${a#b}, etc
}

// simple returns true if the parameter expansion is of the form $name or ${name},
// only expanding a name without any further logic.
func (p *ParamExp) simple() bool {
	return p.Param != nil && p.Flags == nil &&
		!p.Excl && !p.Length && !p.Width && !p.IsSet &&
		p.NestedParam == nil && p.Index == nil &&
		len(p.Modifiers) == 0 && p.Slice == nil &&
		p.Repl == nil && p.Names == 0 && p.Exp == nil
}

func (p *ParamExp) Pos() Pos {
	if p.Dollar.IsValid() {
		return p.Dollar
	}
	return p.Param.Pos()
}
func (p *ParamExp) End() Pos {
	if !p.Short {
		return posAddCol(p.Rbrace, 1)
	}
	// In short mode, we can only end in either an index or a simple name.
	if p.Index != nil {
		return posAddCol(p.Index.End(), 1)
	}
	return p.Param.End()
}

func (p *ParamExp) nakedIndex() bool {
	// A naked index is arr[x] inside arithmetic, without a leading '$'.
	// In that case Dollar is unset, unlike $arr[x] where it holds the '$' position.
	return p.Short && p.Index != nil && !p.Dollar.IsValid()
}

// Slice represents a character slicing expression inside a [ParamExp].
//
// This node will only appear with [LangBash] and [LangMirBSDKorn].
// [LangZsh] uses a [BinaryArithm] with [Comma] in [ParamExp.Index] instead.
type Slice struct {
	Offset, Length ArithmExpr
}

// Replace represents a search and replace expression inside a [ParamExp].
type Replace struct {
	All        bool
	Orig, With *Word
}

// Expansion represents string manipulation in a [ParamExp] other than those
// covered by [Replace].
type Expansion struct {
	Op   ParExpOperator
	Word *Word
}

// ArithmExp represents an arithmetic expansion.
type ArithmExp struct {
	Left, Right Pos
	Bracket     bool // deprecated $[expr] form
	Unsigned    bool // mksh's $((# expr))

	X ArithmExpr
}

func (a *ArithmExp) Pos() Pos { return a.Left }
func (a *ArithmExp) End() Pos {
	if a.Bracket {
		return posAddCol(a.Right, 1)
	}
	return posAddCol(a.Right, 2)
}

// ArithmCmd represents an arithmetic command.
//
// This node will only appear with [LangBash] and [LangMirBSDKorn].
type ArithmCmd struct {
	Left, Right Pos
	Unsigned    bool // mksh's ((# expr))

	X ArithmExpr
}

func (a *ArithmCmd) Pos() Pos { return a.Left }
func (a *ArithmCmd) End() Pos { return posAddCol(a.Right, 2) }

// ArithmExpr represents all nodes that form arithmetic expressions.
//
// These are [*BinaryArithm], [*UnaryArithm], [*ParenArithm], [*FlagsArithm], and [*Word].
type ArithmExpr interface {
	Node
	arithmExprNode()
}

func (*BinaryArithm) arithmExprNode() {}
func (*UnaryArithm) arithmExprNode()  {}
func (*ParenArithm) arithmExprNode()  {}
func (*FlagsArithm) arithmExprNode()  {}
func (*Word) arithmExprNode()         {}

// BinaryArithm represents a binary arithmetic expression.
//
// If Op is any assign operator, X will be a word with a single [*Lit] whose value
// is a valid name.
//
// Ternary operators like "a ? b : c" are fit into this structure. Thus, if
// Op==[TernQuest], Y will be a [*BinaryArithm] with Op==[TernColon].
// [TernColon] does not appear in any other scenario.
type BinaryArithm struct {
	OpPos Pos
	Op    BinAritOperator
	X, Y  ArithmExpr
}

func (b *BinaryArithm) Pos() Pos { return b.X.Pos() }
func (b *BinaryArithm) End() Pos { return b.Y.End() }

// UnaryArithm represents an unary arithmetic expression. The unary operator
// may come before or after the sub-expression.
//
// If Op is [Inc] or [Dec], X will be a word with a single [*Lit] whose value is a
// valid name.
type UnaryArithm struct {
	OpPos Pos
	Op    UnAritOperator
	Post  bool
	X     ArithmExpr
}

func (u *UnaryArithm) Pos() Pos {
	if u.Post {
		return u.X.Pos()
	}
	return u.OpPos
}

func (u *UnaryArithm) End() Pos {
	if u.Post {
		return posAddCol(u.OpPos, 2)
	}
	return u.X.End()
}

// ParenArithm represents an arithmetic expression within parentheses.
type ParenArithm struct {
	Lparen, Rparen Pos

	X ArithmExpr
}

func (p *ParenArithm) Pos() Pos { return p.Lparen }
func (p *ParenArithm) End() Pos { return posAddCol(p.Rparen, 1) }

// FlagsArithm represents zsh subscript flags attached to an arithmetic expression,
// such as ${array[(flags)expr]}.
//
// This node will only appear with [LangZsh].
type FlagsArithm struct {
	Flags *Lit
	X     ArithmExpr
}

func (z *FlagsArithm) Pos() Pos { return posAddCol(z.Flags.Pos(), -1) }
func (z *FlagsArithm) End() Pos {
	if z.X != nil {
		return z.X.End()
	}
	return posAddCol(z.Flags.End(), 1) // closing paren
}

// CaseClause represents a case (switch) clause.
type CaseClause struct {
	Case, In, Esac Pos
	Braces         bool // deprecated mksh form with braces instead of in/esac

	Word  *Word
	Items []*CaseItem
	Last  []Comment
}

func (c *CaseClause) Pos() Pos { return c.Case }
func (c *CaseClause) End() Pos { return posAddCol(c.Esac, 4) }

// CaseItem represents a pattern list (case) within a [CaseClause].
type CaseItem struct {
	Op       CaseOperator
	OpPos    Pos // unset if it was finished by "esac"
	Comments []Comment
	Patterns []*Word

	Stmts []*Stmt
	Last  []Comment
}

func (c *CaseItem) Pos() Pos { return c.Patterns[0].Pos() }
func (c *CaseItem) End() Pos {
	if c.OpPos.IsValid() {
		return posAddCol(c.OpPos, len(c.Op.String()))
	}
	return stmtsEnd(c.Stmts, c.Last)
}

// TestClause represents a Bash extended test clause.
//
// This node will only appear with [LangBash] and [LangMirBSDKorn].
type TestClause struct {
	Left, Right Pos

	X TestExpr
}

func (t *TestClause) Pos() Pos { return t.Left }
func (t *TestClause) End() Pos { return posAddCol(t.Right, 2) }

// TestExpr represents all nodes that form test expressions.
//
// These are [*BinaryTest], [*UnaryTest], [*ParenTest], and [*Word].
type TestExpr interface {
	Node
	testExprNode()
}

func (*BinaryTest) testExprNode() {}
func (*UnaryTest) testExprNode()  {}
func (*ParenTest) testExprNode()  {}
func (*Word) testExprNode()       {}

// BinaryTest represents a binary test expression.
type BinaryTest struct {
	OpPos Pos
	Op    BinTestOperator
	X, Y  TestExpr
}

func (b *BinaryTest) Pos() Pos { return b.X.Pos() }
func (b *BinaryTest) End() Pos { return b.Y.End() }

// UnaryTest represents a unary test expression. The unary operator may come
// before or after the sub-expression.
type UnaryTest struct {
	OpPos Pos
	Op    UnTestOperator
	X     TestExpr
}

func (u *UnaryTest) Pos() Pos { return u.OpPos }
func (u *UnaryTest) End() Pos { return u.X.End() }

// ParenTest represents a test expression within parentheses.
type ParenTest struct {
	Lparen, Rparen Pos

	X TestExpr
}

func (p *ParenTest) Pos() Pos { return p.Lparen }
func (p *ParenTest) End() Pos { return posAddCol(p.Rparen, 1) }

// DeclClause represents a Bash declare clause.
//
// Args can contain a mix of regular and naked assignments. The naked
// assignments can represent either options or variable names.
//
// This node will only appear with [LangBash].
type DeclClause struct {
	// Variant is one of "declare", "local", "export", "readonly",
	// "typeset", or "nameref".
	Variant *Lit
	Args    []*Assign
}

func (d *DeclClause) Pos() Pos { return d.Variant.Pos() }
func (d *DeclClause) End() Pos {
	if len(d.Args) > 0 {
		return d.Args[len(d.Args)-1].End()
	}
	return d.Variant.End()
}

// ArrayExpr represents a Bash array expression.
//
// This node will only appear with [LangBash].
type ArrayExpr struct {
	Lparen, Rparen Pos

	Elems []*ArrayElem
	Last  []Comment
}

func (a *ArrayExpr) Pos() Pos { return a.Lparen }
func (a *ArrayExpr) End() Pos { return posAddCol(a.Rparen, 1) }

// ArrayElem represents a Bash array element.
//
// Index can be nil; for example, declare -a x=(value).
// Value can be nil; for example, declare -A x=([index]=).
// Finally, neither can be nil; for example, declare -A x=([index]=value)
type ArrayElem struct {
	Index    ArithmExpr
	Value    *Word
	Comments []Comment
}

func (a *ArrayElem) Pos() Pos {
	if a.Index != nil {
		return a.Index.Pos()
	}
	return a.Value.Pos()
}

func (a *ArrayElem) End() Pos {
	if a.Value != nil {
		return a.Value.End()
	}
	return posAddCol(a.Index.Pos(), 1)
}

// TODO(v4): the expand package has to stringify ExtGlob again,
// and we don't gain much from a WordPart node anyway;
// make these opaque literals like we did for zsh glob qualifiers.

// ExtGlob represents a Bash extended globbing expression. Note that these are
// parsed independently of whether or not `shopt -s extglob` has been used,
// as the parser runs statically and independently of any interpreter.
//
// This node will only appear with [LangBash] and [LangMirBSDKorn].
type ExtGlob struct {
	OpPos   Pos
	Op      GlobOperator
	Pattern *Lit
}

func (e *ExtGlob) Pos() Pos { return e.OpPos }
func (e *ExtGlob) End() Pos { return posAddCol(e.Pattern.End(), 1) }

// ProcSubst represents a Bash process substitution.
//
// This node will only appear with [LangBash].
type ProcSubst struct {
	OpPos, Rparen Pos
	Op            ProcOperator

	Stmts []*Stmt
	Last  []Comment
}

func (s *ProcSubst) Pos() Pos { return s.OpPos }
func (s *ProcSubst) End() Pos { return posAddCol(s.Rparen, 1) }

// TimeClause represents a Bash time clause. PosixFormat corresponds to the -p
// flag.
//
// This node will only appear with [LangBash] and [LangMirBSDKorn].
type TimeClause struct {
	Time        Pos
	PosixFormat bool
	Stmt        *Stmt
}

func (c *TimeClause) Pos() Pos { return c.Time }
func (c *TimeClause) End() Pos {
	if c.Stmt == nil {
		return posAddCol(c.Time, 4)
	}
	return c.Stmt.End()
}

// CoprocClause represents a Bash coproc clause.
//
// This node will only appear with [LangBash].
type CoprocClause struct {
	Coproc Pos
	Name   *Word
	Stmt   *Stmt
}

func (c *CoprocClause) Pos() Pos { return c.Coproc }
func (c *CoprocClause) End() Pos { return c.Stmt.End() }

// LetClause represents a Bash let clause.
//
// This node will only appear with [LangBash] and [LangMirBSDKorn].
type LetClause struct {
	Let   Pos
	Exprs []ArithmExpr
}

func (l *LetClause) Pos() Pos { return l.Let }
func (l *LetClause) End() Pos { return l.Exprs[len(l.Exprs)-1].End() }

// BraceExp represents a Bash brace expression, such as "{a,f}" or "{1..10}".
//
// This node will only appear as a result of [SplitBraces].
type BraceExp struct {
	Sequence bool // {x..y[..incr]} instead of {x,y[,...]}
	Elems    []*Word
}

func (b *BraceExp) Pos() Pos {
	return posAddCol(b.Elems[0].Pos(), -1)
}

func (b *BraceExp) End() Pos {
	return posAddCol(wordLastEnd(b.Elems), 1)
}

// TestDecl represents the declaration of a Bats test function.
type TestDecl struct {
	Position    Pos
	Description *Word
	Body        *Stmt
}

func (f *TestDecl) Pos() Pos { return f.Position }
func (f *TestDecl) End() Pos { return f.Body.End() }

func wordLastEnd(ws []*Word) Pos {
	if len(ws) == 0 {
		return Pos{}
	}
	return ws[len(ws)-1].End()
}
