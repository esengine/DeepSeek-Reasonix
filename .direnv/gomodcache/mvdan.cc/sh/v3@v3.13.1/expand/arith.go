// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"fmt"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// TODO(v4): the arithmetic APIs should return int64 for portability with 32-bit systems,
// even if Bash only supports native int sizes.

func Arithm(cfg *Config, expr syntax.ArithmExpr) (int, error) {
	switch expr := expr.(type) {
	case *syntax.Word:
		str, err := Literal(cfg, expr)
		if err != nil {
			return 0, err
		}
		// recursively fetch vars
		i := 0
		for syntax.ValidName(str) {
			val := cfg.envGet(str)
			if val == "" {
				break
			}
			if i++; i >= maxNameRefDepth {
				break
			}
			str = val
		}
		// default to 0
		return int(atoi(str)), nil
	case *syntax.ParenArithm:
		return Arithm(cfg, expr.X)
	case *syntax.UnaryArithm:
		switch expr.Op {
		case syntax.Inc, syntax.Dec:
			name := expr.X.(*syntax.Word).Lit()
			old := atoi(cfg.envGet(name))
			val := old
			if expr.Op == syntax.Inc {
				val++
			} else {
				val--
			}
			if err := cfg.envSet(name, strconv.FormatInt(val, 10)); err != nil {
				return 0, err
			}
			if expr.Post {
				return int(old), nil
			}
			return int(val), nil
		}
		val, err := Arithm(cfg, expr.X)
		if err != nil {
			return 0, err
		}
		switch expr.Op {
		case syntax.Not:
			return oneIf(val == 0), nil
		case syntax.BitNegation:
			return ^val, nil
		case syntax.Plus:
			return val, nil
		case syntax.Minus:
			return -val, nil
		default:
			return 0, fmt.Errorf("unsupported unary arithmetic operator: %q", expr.Op)
		}
	case *syntax.BinaryArithm:
		switch expr.Op {
		case syntax.Assgn, syntax.AddAssgn, syntax.SubAssgn,
			syntax.MulAssgn, syntax.QuoAssgn, syntax.RemAssgn,
			syntax.AndAssgn, syntax.OrAssgn, syntax.XorAssgn,
			syntax.ShlAssgn, syntax.ShrAssgn:
			return cfg.assgnArit(expr)
		case syntax.TernQuest: // TernColon can't happen here
			cond, err := Arithm(cfg, expr.X)
			if err != nil {
				return 0, err
			}
			b2 := expr.Y.(*syntax.BinaryArithm) // must have Op==TernColon
			if cond == 1 {
				return Arithm(cfg, b2.X)
			}
			return Arithm(cfg, b2.Y)
		}
		left, err := Arithm(cfg, expr.X)
		if err != nil {
			return 0, err
		}
		right, err := Arithm(cfg, expr.Y)
		if err != nil {
			return 0, err
		}
		return binArit(expr.Op, left, right)
	default:
		panic(fmt.Sprintf("unexpected arithm expr: %T", expr))
	}
}

func oneIf(b bool) int {
	if b {
		return 1
	}
	return 0
}

// atoi is like [strconv.ParseInt](s, 10, 64), but it ignores errors and trims whitespace.
func atoi(s string) int64 {
	s = strings.TrimSpace(s)
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func (cfg *Config) assgnArit(b *syntax.BinaryArithm) (int, error) {
	name := b.X.(*syntax.Word).Lit()
	val := atoi(cfg.envGet(name))
	arg_, err := Arithm(cfg, b.Y)
	if err != nil {
		return 0, err
	}
	arg := int64(arg_)
	switch b.Op {
	case syntax.Assgn:
		val = arg
	case syntax.AddAssgn:
		val += arg
	case syntax.SubAssgn:
		val -= arg
	case syntax.MulAssgn:
		val *= arg
	case syntax.QuoAssgn:
		if arg == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		val /= arg
	case syntax.RemAssgn:
		if arg == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		val %= arg
	case syntax.AndAssgn:
		val &= arg
	case syntax.OrAssgn:
		val |= arg
	case syntax.XorAssgn:
		val ^= arg
	case syntax.ShlAssgn:
		val <<= uint(arg)
	case syntax.ShrAssgn:
		val >>= uint(arg)
	}
	if err := cfg.envSet(name, strconv.FormatInt(val, 10)); err != nil {
		return 0, err
	}
	return int(val), nil
}

func intPow(a, b int) int {
	p := 1
	for b > 0 {
		if b&1 != 0 {
			p *= a
		}
		b >>= 1
		a *= a
	}
	return p
}

func binArit(op syntax.BinAritOperator, x, y int) (int, error) {
	switch op {
	case syntax.Add:
		return x + y, nil
	case syntax.Sub:
		return x - y, nil
	case syntax.Mul:
		return x * y, nil
	case syntax.Quo:
		if y == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return x / y, nil
	case syntax.Rem:
		if y == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return x % y, nil
	case syntax.Pow:
		return intPow(x, y), nil
	case syntax.Eql:
		return oneIf(x == y), nil
	case syntax.Gtr:
		return oneIf(x > y), nil
	case syntax.Lss:
		return oneIf(x < y), nil
	case syntax.Neq:
		return oneIf(x != y), nil
	case syntax.Leq:
		return oneIf(x <= y), nil
	case syntax.Geq:
		return oneIf(x >= y), nil
	case syntax.And:
		return x & y, nil
	case syntax.Or:
		return x | y, nil
	case syntax.Xor:
		return x ^ y, nil
	case syntax.Shr:
		return x >> uint(y), nil
	case syntax.Shl:
		return x << uint(y), nil
	case syntax.AndArit:
		return oneIf(x != 0 && y != 0), nil
	case syntax.OrArit:
		return oneIf(x != 0 || y != 0), nil
	case syntax.Comma:
		// x is executed but its result discarded
		return y, nil
	default:
		return 0, fmt.Errorf("unsupported binary arithmetic operator: %q", op)
	}
}
