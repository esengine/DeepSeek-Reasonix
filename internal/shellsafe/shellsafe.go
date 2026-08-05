// Package shellsafe is the single source of truth for which shell commands are
// read-only — they don't modify filesystem, network, or process state. The
// permission auto-approve path and explicitly read-only runners share these
// tables so their command classification cannot drift.
package shellsafe

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"reasonix/internal/shellparse"
)

// ReadOnlyCommands holds single-word commands whose base name alone implies a
// read-only operation. The first word of a command (lowercased) is looked up
// here. Commands that are read-only only for certain subcommands (e.g. git) are
// in ReadOnlyPrefixes.
var ReadOnlyCommands = map[string]bool{
	"cat": true, "head": true, "tail": true, "less": true, "more": true,
	"ls": true, "find": true, "locate": true, "which": true, "whereis": true, "type": true,
	"grep": true, "egrep": true, "fgrep": true, "rg": true,
	"echo": true, "printf": true,
	"pwd": true, "cd": true, "whoami": true, "id": true, "uname": true, "hostname": true,
	"date": true, "printenv": true,
	"wc": true, "sort": true, "uniq": true, "cut": true, "tr": true,
	"stat": true, "file": true, "du": true, "df": true,
	"ps": true, "top": true, "htop": true,
	"diff": true, "cmp": true, "comm": true,
	"man": true, "info": true, "help": true,
	"true": true, "false": true, "test": true, "[": true,
	"basename": true, "dirname": true, "realpath": true, "readlink": true,
	// PowerShell inspection cmdlets. Keep this list intentionally narrow: only
	// cmdlets whose verb is intrinsically observational belong here. The parser
	// still rejects pipelines, substitutions, redirections, and command chains.
	"get-childitem": true, "get-content": true, "get-item": true,
	"get-location": true, "get-process": true, "get-command": true,
	"get-nettcpconnection": true,
	"resolve-path":         true, "select-string": true, "measure-object": true,
	"compare-object": true,
}

// workspaceNonMutatingCommands holds commands that do not write workspace
// state but are not safe to auto-allow as permission-layer readers. Keep this
// separate from ReadOnlyCommands: Test-NetConnection performs network I/O and
// must still pass through the user's permission policy even though Delivery
// does not need to serialize it behind the workspace writer.
var workspaceNonMutatingCommands = map[string]bool{
	"test-netconnection": true,
}

// ReadOnlyPrefixes maps a base command to the set of subcommands (the second
// word) that are read-only. A subcommand not listed here is treated as not
// read-only (fail-closed): write-capable subcommands (git branch/remote/config,
// go build/test, npm install, …) are deliberately absent.
var ReadOnlyPrefixes = map[string]map[string]bool{
	"git": {
		"log": true, "status": true, "diff": true, "show": true,
		"tag":   true,
		"blame": true, "grep": true, "ls-files": true, "ls-tree": true,
		"rev-parse": true, "rev-list": true, "describe": true, "reflog": true,
		"shortlog": true, "whatchanged": true, "cherry": true,
		"cat-file": true, "for-each-ref": true, "name-rev": true,
	},
	"go": {
		"vet": true, "doc": true, "list": true,
		"version": true, "env": true,
	},
	"npm": {
		"ls": true, "list": true, "view": true, "info": true,
		"outdated": true, "audit": true,
	},
	"cargo": {
		"check": true, "doc": true, "search": true,
	},
	"docker": {
		"ps": true, "images": true, "inspect": true, "logs": true,
		"stats": true, "info": true, "version": true,
	},
	"kubectl": {
		"get": true, "describe": true, "logs": true, "explain": true,
		"api-resources": true, "api-versions": true,
	},
	// Version/help probes for common runtimes (the second word is a flag).
	"node":    {"-v": true, "--version": true},
	"python":  {"--version": true, "-v": true, "-V": true},
	"python3": {"--version": true, "-v": true, "-V": true},
}

// ContainsShellSyntax reports whether a command uses shell operators or
// substitution — chaining/redirection/expansion can smuggle a write past a
// read-only base-word check, so any such command is treated as not read-only.
func ContainsShellSyntax(cmd string) bool {
	return shellparse.ContainsShellSyntax(cmd)
}

// CommandIsReadOnly reports whether the command's base/subcommand is in the
// read-only tables, ignoring argument rigor (which each consumer applies). It
// returns the base and subcommand so callers can run their own arg checks.
// ok is false when the command contains shell syntax or the base/subcommand is
// not a known read-only operation.
func CommandIsReadOnly(command string) (base, sub string, ok bool) {
	base, sub, _, ok = ClassifyReadOnlyCommand(command)
	return base, sub, ok
}

// ClassifyReadOnlyCommand returns the resolved argument fields as well as the
// command classification. Dynamic fields are opaque placeholders and are
// returned only for the narrowly verified substitution shape above.
func ClassifyReadOnlyCommand(command string) (base, sub string, fields []string, ok bool) {
	fields, malformed := shellparse.StaticFields(command)
	if malformed != "" {
		var dynamic bool
		fields, dynamic, ok = resolvedReadOnlyFields(command, false)
		if !ok || !dynamic {
			return "", "", nil, false
		}
	}
	if len(fields) == 0 {
		return "", "", nil, false
	}
	base = strings.ToLower(fields[0])
	if ReadOnlyCommands[base] {
		if hasResolvedSubstitution(fields) && !substitutionSafeCommands[base] {
			return "", "", nil, false
		}
		return base, "", fields, true
	}
	if len(fields) > 1 {
		if subs, prefixed := ReadOnlyPrefixes[base]; prefixed {
			sub = strings.ToLower(fields[1])
			if subs[sub] {
				return base, sub, fields, true
			}
		}
	}
	return "", "", nil, false
}

const resolvedSubstitutionPlaceholder = "__reasonix_read_only_substitution__"

var substitutionSafeCommands = map[string]bool{
	"cat": true, "head": true, "tail": true, "ls": true,
	"grep": true, "egrep": true, "fgrep": true, "rg": true,
	"echo": true, "printf": true, "pwd": true, "whoami": true,
	"id": true, "uname": true, "hostname": true, "wc": true,
	"stat": true, "file": true, "du": true, "df": true,
	"cmp": true, "comm": true, "true": true, "false": true,
	"test": true, "[": true, "basename": true, "dirname": true,
	"realpath": true, "readlink": true,
}

func hasResolvedSubstitution(fields []string) bool {
	for _, field := range fields {
		if strings.Contains(field, resolvedSubstitutionPlaceholder) {
			return true
		}
	}
	return false
}

// resolvedReadOnlyFields accepts one narrow dynamic shape: a command from the
// read-only table with a double-quoted command substitution whose nested
// command is itself a single, static, argument-safe read-only command. It never
// evaluates output. Unquoted substitutions, parameters, arithmetic, process
// substitutions, redirects, assignments, chains, and background jobs remain
// fail-closed.
func resolvedReadOnlyFields(command string, nested bool) ([]string, bool, bool) {
	file, err := shellparse.ParseBash(command)
	if err != nil || len(file.Stmts) != 1 {
		return nil, false, false
	}
	return resolvedReadOnlyStmt(file.Stmts[0], nested)
}

func resolvedReadOnlyStmt(stmt *syntax.Stmt, nested bool) ([]string, bool, bool) {
	if stmt == nil || stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown || len(stmt.Redirs) > 0 {
		return nil, false, false
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) > 0 || len(call.Args) == 0 {
		return nil, false, false
	}
	fields := make([]string, 0, len(call.Args))
	dynamic := false
	for _, word := range call.Args {
		field, wordDynamic, ok := resolvedReadOnlyWord(word)
		if !ok {
			return nil, false, false
		}
		fields = append(fields, field)
		dynamic = dynamic || wordDynamic
	}
	base, sub, tableOK := readOnlyFields(fields)
	if !tableOK || (nested && (!substitutionSafeCommands[base] || !nestedReadOnlyArgsSafe(base, sub, fields))) {
		return nil, false, false
	}
	return fields, dynamic, true
}

func resolvedReadOnlyWord(word *syntax.Word) (string, bool, bool) {
	if static, ok := shellparse.StaticWord(word); ok {
		return static, false, true
	}
	if word == nil {
		return "", false, false
	}
	var out strings.Builder
	dynamic := false
	for _, part := range word.Parts {
		value, partDynamic, ok := resolvedReadOnlyWordPart(part, false)
		if !ok {
			return "", false, false
		}
		out.WriteString(value)
		dynamic = dynamic || partDynamic
	}
	return out.String(), dynamic, true
}

func resolvedReadOnlyWordPart(part syntax.WordPart, doubleQuoted bool) (string, bool, bool) {
	switch value := part.(type) {
	case *syntax.Lit:
		return value.Value, false, true
	case *syntax.SglQuoted:
		return value.Value, false, true
	case *syntax.DblQuoted:
		var out strings.Builder
		dynamic := false
		for _, nested := range value.Parts {
			text, partDynamic, ok := resolvedReadOnlyWordPart(nested, true)
			if !ok {
				return "", false, false
			}
			out.WriteString(text)
			dynamic = dynamic || partDynamic
		}
		return out.String(), dynamic, true
	case *syntax.CmdSubst:
		if !doubleQuoted || value.TempFile || value.ReplyVar || len(value.Stmts) != 1 {
			return "", false, false
		}
		if _, _, ok := resolvedReadOnlyStmt(value.Stmts[0], true); !ok {
			return "", false, false
		}
		return resolvedSubstitutionPlaceholder, true, true
	default:
		return "", false, false
	}
}

func readOnlyFields(fields []string) (base, sub string, ok bool) {
	if len(fields) == 0 {
		return "", "", false
	}
	base = strings.ToLower(fields[0])
	if ReadOnlyCommands[base] {
		return base, "", true
	}
	if len(fields) > 1 {
		sub = strings.ToLower(fields[1])
		if ReadOnlyPrefixes[base][sub] {
			return base, sub, true
		}
	}
	return "", "", false
}

func nestedReadOnlyArgsSafe(base, sub string, fields []string) bool {
	args := fields[1:]
	if sub != "" && len(args) > 0 {
		args = args[1:]
	}
	for _, arg := range args {
		switch {
		case base == "find" && (arg == "-exec" || arg == "-execdir" || arg == "-delete" || arg == "-ok" || arg == "-okdir" || arg == "-fls" || arg == "-fprint" || arg == "-fprint0" || arg == "-fprintf"):
			return false
		case base == "sed" && (strings.HasPrefix(arg, "-i") || strings.HasPrefix(arg, "--in-place")):
			return false
		case base == "sort" && (strings.HasPrefix(arg, "-o") || arg == "--output" || strings.HasPrefix(arg, "--output=")):
			return false
		case base == "git" && (sub == "diff" || sub == "show" || sub == "log") && (arg == "--output" || strings.HasPrefix(arg, "--output=")):
			return false
		case base == "go" && sub == "env" && (arg == "-w" || arg == "-u"):
			return false
		}
	}
	return base != "git" || sub != "tag" || len(args) == 0 || args[0] == "-l" || args[0] == "--list"
}

// CommandIsWorkspaceNonMutating reports commands that Delivery can execute
// without acquiring the workspace writer. Permission-safe readers are a
// subset; network probes live only in workspaceNonMutatingCommands so this
// classification cannot silently widen approval or read-only subagent access.
func CommandIsWorkspaceNonMutating(command string) (base, sub string, ok bool) {
	base, sub, _, ok = ClassifyWorkspaceNonMutatingCommand(command)
	return base, sub, ok
}

// ClassifyWorkspaceNonMutatingCommand is the field-carrying form used by
// Delivery mutation accounting.
func ClassifyWorkspaceNonMutatingCommand(command string) (base, sub string, fields []string, ok bool) {
	if base, sub, fields, ok = ClassifyReadOnlyCommand(command); ok {
		return base, sub, fields, true
	}
	fields, malformed := shellparse.StaticFields(command)
	if malformed != "" || len(fields) == 0 {
		return "", "", nil, false
	}
	base = strings.ToLower(fields[0])
	if workspaceNonMutatingCommands[base] {
		return base, "", fields, true
	}
	return "", "", nil, false
}
