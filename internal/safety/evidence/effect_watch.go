package evidence

import (
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"reasonix/internal/base/shellparse"
)

// EffectWatch answers one question per round: did anything happen that the
// host itself can observe. Issuing a call is not an effect — a command with
// new arguments that changes nothing and reads nothing answers no — so the
// judgement reads receipts, never what a call was named or what it printed.
type EffectWatch struct {
	reads  map[string]bool
	checks map[string]bool
}

func NewEffectWatch() *EffectWatch {
	return &EffectWatch{reads: map[string]bool{}, checks: map[string]bool{}}
}

// Exists reports whether a shell argument names something on disk. It is how a
// command's operand is told apart from text: only the host can stat it.
type Exists func(arg string) (key string, ok bool)

// RoundHadEffect folds one round's receipts in and reports whether any of them
// changed state, moved a check, read something not read before in this watch,
// or brought a delegated result back.
func (w *EffectWatch) RoundHadEffect(receipts []Receipt, exists Exists) bool {
	if w == nil {
		return false
	}
	effect := false
	for _, r := range receipts {
		if w.receiptEffect(r, exists) {
			effect = true
		}
	}
	return effect
}

func (w *EffectWatch) receiptEffect(r Receipt, exists Exists) bool {
	if !r.Success {
		return w.checkMoved(r)
	}
	switch {
	case r.Mutation || r.Write:
		return true
	case r.StepProof || r.TodoStep != nil:
		return true
	case r.ToolName == "task" || r.ToolName == "parallel_tasks" || r.ToolName == "fleet":
		return true
	}
	moved := w.checkMoved(r)
	if r.OutputBytes == 0 {
		return moved
	}
	if strings.TrimSpace(r.Command) != "" {
		return w.shellReadsNew(r.Command, exists) || moved
	}
	if r.Read {
		// The request is the identity: another slice of a file, a new query, or
		// a new URL each return content this run had not been given.
		return w.firstRead(r.ToolName+"\x00"+string(r.Args)) || moved
	}
	return moved
}

// checkMoved reports a verification whose outcome differs from the last time
// the same check ran. A first run is a baseline, not a change.
func (w *EffectWatch) checkMoved(r Receipt) bool {
	command := strings.TrimSpace(r.Command)
	if command == "" || !IsDeliveryVerificationCommand(command) {
		return false
	}
	key, passed := VerificationIdentity(command), verificationPassed(r)
	was, seen := w.checks[key]
	w.checks[key] = passed
	return seen && was != passed
}

func (w *EffectWatch) firstRead(key string) bool {
	if w.reads[key] {
		return false
	}
	w.reads[key] = true
	return true
}

// shellReadsNew reports a command that named an existing path this watch had
// not seen named before. The operand has to exist on disk, so text a command
// merely prints never counts however much it varies.
func (w *EffectWatch) shellReadsNew(command string, exists Exists) bool {
	if exists == nil {
		return false
	}
	file, err := shellparse.ParseBash(command)
	if err != nil {
		return false
	}
	fresh := false
	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}
		for _, arg := range call.Args[min(1, len(call.Args)):] {
			word, static := shellparse.StaticWord(arg)
			if !static || word == "" || strings.HasPrefix(word, "-") {
				continue
			}
			if key, ok := exists(word); ok && w.firstRead("path\x00"+key) {
				fresh = true
			}
		}
		return true
	})
	return fresh
}

// ExistsUnder resolves shell operands against root and stats them.
func ExistsUnder(root string, stat func(string) bool) Exists {
	return func(arg string) (string, bool) {
		p := arg
		if !filepath.IsAbs(p) {
			if root == "" {
				return "", false
			}
			p = filepath.Join(root, p)
		}
		p = filepath.Clean(p)
		if !stat(p) {
			return "", false
		}
		return NormalizePath(p), true
	}
}
