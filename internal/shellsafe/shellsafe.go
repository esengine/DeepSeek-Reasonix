// Package shellsafe is the single source of truth for which shell commands are
// read-only — they don't modify filesystem, network, or process state. The
// permission auto-approve path and explicitly read-only runners share these
// tables so their command classification cannot drift.
package shellsafe

import (
	"strings"

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
	fields, malformed := shellparse.StaticFields(command)
	if malformed != "" || len(fields) == 0 {
		return "", "", false
	}
	base = strings.ToLower(fields[0])
	if ReadOnlyCommands[base] {
		return base, "", true
	}
	if len(fields) > 1 {
		if subs, prefixed := ReadOnlyPrefixes[base]; prefixed {
			sub = strings.ToLower(fields[1])
			if subs[sub] {
				return base, sub, true
			}
		}
	}
	return "", "", false
}
