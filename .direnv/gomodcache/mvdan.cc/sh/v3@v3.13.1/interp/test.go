// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"context"
	"fmt"
	"os"
	"regexp"

	"golang.org/x/term"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// non-empty string is true, empty string is false
func (r *Runner) bashTest(ctx context.Context, expr syntax.TestExpr, classic bool) string {
	switch x := expr.(type) {
	case *syntax.Word:
		if classic {
			// In the classic "test" mode, we already expanded and
			// split the list of words, so don't redo that work.
			return r.document(x)
		}
		return r.literal(x)
	case *syntax.ParenTest:
		return r.bashTest(ctx, x.X, classic)
	case *syntax.BinaryTest:
		switch x.Op {
		case syntax.TsMatchShort, syntax.TsMatch, syntax.TsNoMatch:
			str := r.literal(x.X.(*syntax.Word))
			yw := x.Y.(*syntax.Word)
			if classic { // test, [
				lit := r.literal(yw)
				if (str == lit) == (x.Op != syntax.TsNoMatch) {
					return "1"
				}
			} else { // [[
				pattern := r.pattern(yw)
				if match(pattern, str) == (x.Op != syntax.TsNoMatch) {
					return "1"
				}
			}
			return ""
		}
		if r.binTest(ctx, x.Op, r.bashTest(ctx, x.X, classic), r.bashTest(ctx, x.Y, classic)) {
			return "1"
		}
		return ""
	case *syntax.UnaryTest:
		if r.unTest(ctx, x.Op, r.bashTest(ctx, x.X, classic)) {
			return "1"
		}
		return ""
	}
	return ""
}

func (r *Runner) binTest(ctx context.Context, op syntax.BinTestOperator, x, y string) bool {
	switch op {
	case syntax.TsReMatch:
		re, err := regexp.Compile(y)
		if err != nil {
			r.exit.code = 2
			return false
		}
		m := re.FindStringSubmatch(x)
		if m == nil {
			return false
		}
		vr := expand.Variable{
			Set:  true,
			Kind: expand.Indexed,
			List: m,
		}
		r.setVar("BASH_REMATCH", vr)
		return true
	case syntax.TsNewer:
		info1, err1 := r.stat(ctx, x)
		info2, err2 := r.stat(ctx, y)
		if err1 != nil || err2 != nil {
			return false
		}
		return info1.ModTime().After(info2.ModTime())
	case syntax.TsOlder:
		info1, err1 := r.stat(ctx, x)
		info2, err2 := r.stat(ctx, y)
		if err1 != nil || err2 != nil {
			return false
		}
		return info1.ModTime().Before(info2.ModTime())
	case syntax.TsDevIno:
		info1, err1 := r.stat(ctx, x)
		info2, err2 := r.stat(ctx, y)
		if err1 != nil || err2 != nil {
			return false
		}
		return os.SameFile(info1, info2)
	case syntax.TsEql:
		return atoi(x) == atoi(y)
	case syntax.TsNeq:
		return atoi(x) != atoi(y)
	case syntax.TsLeq:
		return atoi(x) <= atoi(y)
	case syntax.TsGeq:
		return atoi(x) >= atoi(y)
	case syntax.TsLss:
		return atoi(x) < atoi(y)
	case syntax.TsGtr:
		return atoi(x) > atoi(y)
	case syntax.AndTest:
		return x != "" && y != ""
	case syntax.OrTest:
		return x != "" || y != ""
	case syntax.TsBefore:
		return x < y
	case syntax.TsAfter:
		return x > y
	default:
		// Should only happen if we forgot a case above.
		panic(fmt.Sprintf("unexpected binary test operator: %q", op))
	}
}

func (r *Runner) statMode(ctx context.Context, name string, mode os.FileMode) bool {
	info, err := r.stat(ctx, name)
	return err == nil && info.Mode()&mode != 0
}

// These are copied from x/sys/unix as we can't import it here.
const (
	access_R_OK = 0x4
	access_W_OK = 0x2
	access_X_OK = 0x1
)

func (r *Runner) unTest(ctx context.Context, op syntax.UnTestOperator, x string) bool {
	switch op {
	case syntax.TsExists:
		_, err := r.stat(ctx, x)
		return err == nil
	case syntax.TsRegFile:
		info, err := r.stat(ctx, x)
		return err == nil && info.Mode().IsRegular()
	case syntax.TsDirect:
		return r.statMode(ctx, x, os.ModeDir)
	case syntax.TsCharSp:
		return r.statMode(ctx, x, os.ModeCharDevice)
	case syntax.TsBlckSp:
		info, err := r.stat(ctx, x)
		return err == nil && info.Mode()&os.ModeDevice != 0 &&
			info.Mode()&os.ModeCharDevice == 0
	case syntax.TsNmPipe:
		return r.statMode(ctx, x, os.ModeNamedPipe)
	case syntax.TsSocket:
		return r.statMode(ctx, x, os.ModeSocket)
	case syntax.TsSmbLink:
		info, err := r.lstat(ctx, x)
		return err == nil && info.Mode()&os.ModeSymlink != 0
	case syntax.TsSticky:
		return r.statMode(ctx, x, os.ModeSticky)
	case syntax.TsUIDSet:
		return r.statMode(ctx, x, os.ModeSetuid)
	case syntax.TsGIDSet:
		return r.statMode(ctx, x, os.ModeSetgid)
	case syntax.TsModif:
		r.errf("unsupported unary test op: %v\n", op)
		return false
	case syntax.TsRead:
		return r.access(ctx, r.absPath(x), access_R_OK) == nil
	case syntax.TsWrite:
		return r.access(ctx, r.absPath(x), access_W_OK) == nil
	case syntax.TsExec:
		return r.access(ctx, r.absPath(x), access_X_OK) == nil
	case syntax.TsNoEmpty:
		info, err := r.stat(ctx, x)
		return err == nil && info.Size() > 0
	case syntax.TsFdTerm:
		fd := atoi(x)
		var f any
		switch fd {
		case 0:
			f = r.stdin
		case 1:
			f = r.stdout
		case 2:
			f = r.stderr
		}
		if f, ok := f.(interface{ Fd() uintptr }); ok {
			// Support [os.File.Fd] methods such as the one on [*os.File].
			return term.IsTerminal(int(f.Fd()))
		}
		// TODO: allow term.IsTerminal here too if running in the
		// "single process" mode.
		return false
	case syntax.TsEmpStr:
		return x == ""
	case syntax.TsNempStr:
		return x != ""
	case syntax.TsOptSet:
		if opt := r.posixOptByName(x); opt != nil {
			return *opt
		}
		return false
	case syntax.TsVarSet:
		return r.lookupVar(x).IsSet()
	case syntax.TsRefVar:
		return r.lookupVar(x).Kind == expand.NameRef
	case syntax.TsNot:
		return x == ""
	case syntax.TsUsrOwn, syntax.TsGrpOwn:
		return r.unTestOwnOrGrp(ctx, op, x)
	default:
		// Should only happen if we forgot a case above.
		panic(fmt.Sprintf("unexpected unary test op: %v", op))
	}
}
