package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"reasonix/internal/agent"
)

type cliCompletionValueKind uint8

const (
	cliCompletionNoValue cliCompletionValueKind = iota
	cliCompletionStaticValue
	cliCompletionModelValue
	cliCompletionSessionValue
)

type cliCompletionFlag struct {
	names  []string
	kind   cliCompletionValueKind
	values []string
}

type cliCompletionSpec struct {
	name        string
	aliases     []string
	flags       []cliCompletionFlag
	subcommands []cliCompletionSpec
}

func completionFlag(names string, kind cliCompletionValueKind, values ...string) cliCompletionFlag {
	return cliCompletionFlag{names: strings.Fields(names), kind: kind, values: values}
}

func completionSpec(name string, flags []cliCompletionFlag, subcommands ...cliCompletionSpec) cliCompletionSpec {
	return cliCompletionSpec{name: name, flags: flags, subcommands: subcommands}
}

func completionSpecWithAliases(name string, aliases []string, flags []cliCompletionFlag, subcommands ...cliCompletionSpec) cliCompletionSpec {
	return cliCompletionSpec{name: name, aliases: aliases, flags: flags, subcommands: subcommands}
}

func cliCompletionRootSpec() cliCompletionSpec {
	profile := completionFlag("--profile", cliCompletionStaticValue, "economy", "balanced", "delivery")
	model := completionFlag("--model", cliCompletionModelValue)
	resume := completionFlag("--resume -r", cliCompletionSessionValue)
	effort := completionFlag("--effort", cliCompletionStaticValue, "auto", "low", "medium", "high", "max")
	permissionMode := completionFlag("--permission-mode", cliCompletionStaticValue,
		"manual", "ask", "auto", "acceptEdits", "dontAsk", "plan", "bypassPermissions")
	help := completionFlag("--help -h", cliCompletionNoValue)

	interactiveFlags := []cliCompletionFlag{
		model, profile,
		completionFlag("--max-steps", cliCompletionStaticValue),
		completionFlag("--continue -c", cliCompletionNoValue),
		resume,
		completionFlag("--copy", cliCompletionNoValue),
		completionFlag("--dangerously-skip-permissions --yolo", cliCompletionNoValue),
		completionFlag("--dir", cliCompletionStaticValue),
		effort, permissionMode,
		completionFlag("--add-dir", cliCompletionStaticValue),
		completionFlag("--allowed-tools --allowedTools", cliCompletionStaticValue),
		help,
	}
	runFlags := []cliCompletionFlag{
		model, profile,
		completionFlag("--max-steps", cliCompletionStaticValue),
		completionFlag("--show-thinking", cliCompletionNoValue),
		completionFlag("--metrics", cliCompletionStaticValue),
		completionFlag("--dir", cliCompletionStaticValue),
		completionFlag("--continue -c", cliCompletionNoValue),
		completionFlag("--resume", cliCompletionSessionValue),
		completionFlag("--copy", cliCompletionNoValue),
		effort, permissionMode,
		completionFlag("--auto -y", cliCompletionNoValue),
		completionFlag("--print -p", cliCompletionNoValue),
		completionFlag("--events-jsonl", cliCompletionNoValue),
		completionFlag("--output-format", cliCompletionStaticValue, "text", "json", "stream-json"),
		completionFlag("--add-dir", cliCompletionStaticValue),
		completionFlag("--allowed-tools --allowedTools", cliCompletionStaticValue),
		help,
	}

	root := cliCompletionSpec{name: "reasonix", flags: append([]cliCompletionFlag{
		model,
		completionFlag("--max-steps", cliCompletionStaticValue),
		completionFlag("--print -p", cliCompletionNoValue),
		completionFlag("--continue -c", cliCompletionNoValue),
		resume,
		completionFlag("--copy", cliCompletionNoValue),
		completionFlag("--dangerously-skip-permissions --yolo", cliCompletionNoValue),
		permissionMode,
		effort,
		completionFlag("--dir", cliCompletionStaticValue),
		completionFlag("--add-dir", cliCompletionStaticValue),
		completionFlag("--allowed-tools --allowedTools", cliCompletionStaticValue),
		completionFlag("--acp", cliCompletionNoValue),
		completionFlag("--version -v", cliCompletionNoValue),
	}, help)}

	root.subcommands = []cliCompletionSpec{
		completionSpec("run", runFlags),
		completionSpecWithAliases("chat", []string{"code"}, interactiveFlags),
		completionSpec("serve", []cliCompletionFlag{
			model, profile,
			completionFlag("--max-steps", cliCompletionStaticValue),
			completionFlag("--addr", cliCompletionStaticValue),
			completionFlag("--resume", cliCompletionSessionValue),
			completionFlag("--auth", cliCompletionStaticValue, "none", "token", "password"),
			completionFlag("--token --password --port-file --token-file --pid-file", cliCompletionStaticValue),
			completionFlag("--hash-password --behind-proxy", cliCompletionNoValue), help,
		}),
		completionSpec("setup", []cliCompletionFlag{completionFlag("--local -l", cliCompletionNoValue), help}),
		completionSpec("config", []cliCompletionFlag{help},
			completionSpec("auto-plan", []cliCompletionFlag{completionFlag("--local", cliCompletionNoValue), help}),
			completionSpec("reasoning-language", []cliCompletionFlag{completionFlag("--local", cliCompletionNoValue), help}),
			completionSpec("compact-ratio", []cliCompletionFlag{completionFlag("--local", cliCompletionNoValue), help}),
			completionSpec("currency", []cliCompletionFlag{completionFlag("--local", cliCompletionNoValue), help}),
			completionSpec("telemetry", []cliCompletionFlag{help}),
		),
		completionSpec("init", []cliCompletionFlag{help}),
		completionSpec("acp", []cliCompletionFlag{
			model, profile,
			completionFlag("--planner", cliCompletionStaticValue, "auto", "off"),
			completionFlag("--sandbox-network", cliCompletionStaticValue, "auto", "on", "off"),
			completionFlag("--sandbox-bash", cliCompletionStaticValue, "auto", "enforce"),
			completionFlag("--workspace-only", cliCompletionNoValue), help,
		}),
		completionSpec("mcp", []cliCompletionFlag{help},
			completionSpecWithAliases("list", []string{"ls"}, []cliCompletionFlag{help}),
			completionSpec("get", []cliCompletionFlag{help}),
			completionSpec("add", []cliCompletionFlag{
				completionFlag("--http --streamable-http --sse --env --header", cliCompletionStaticValue), help,
			}),
			completionSpec("enable", []cliCompletionFlag{help}),
			completionSpec("disable", []cliCompletionFlag{help}),
			completionSpecWithAliases("retry", []string{"connect"}, []cliCompletionFlag{help}),
			completionSpec("update", []cliCompletionFlag{help}),
			completionSpec("import", []cliCompletionFlag{help}),
			completionSpecWithAliases("browse", []string{"search"}, []cliCompletionFlag{
				completionFlag("--limit", cliCompletionStaticValue), completionFlag("--json", cliCompletionNoValue), help,
			}),
			completionSpec("install", []cliCompletionFlag{completionFlag("--as", cliCompletionStaticValue), help}),
			completionSpecWithAliases("remove", []string{"rm"}, []cliCompletionFlag{help}),
		),
		completionSpec("remote", []cliCompletionFlag{help},
			completionSpec("add", []cliCompletionFlag{
				completionFlag("--identity --jump --workspace --serve-install --passphrase-env --password-env", cliCompletionStaticValue),
				completionFlag("--use-ssh-config", cliCompletionNoValue), help,
			}),
			completionSpecWithAliases("list", []string{"ls"}, []cliCompletionFlag{help}),
			completionSpecWithAliases("remove", []string{"rm"}, []cliCompletionFlag{help}),
			completionSpec("import", []cliCompletionFlag{completionFlag("--all", cliCompletionNoValue), help}),
			completionSpec("test", []cliCompletionFlag{help}),
			completionSpecWithAliases("connect", []string{"open"}, []cliCompletionFlag{
				completionFlag("--workspace --local-port", cliCompletionStaticValue),
				completionFlag("--no-serve --open --forward-only", cliCompletionNoValue), help,
			}),
			completionSpec("status", []cliCompletionFlag{help}),
			completionSpec("forward", []cliCompletionFlag{help},
				completionSpec("add", []cliCompletionFlag{help}),
				completionSpecWithAliases("remove", []string{"rm"}, []cliCompletionFlag{help}),
				completionSpecWithAliases("list", []string{"ls"}, []cliCompletionFlag{help}),
			),
			completionSpec("serve", []cliCompletionFlag{help},
				completionSpec("start", []cliCompletionFlag{completionFlag("--workspace -n", cliCompletionStaticValue), help}),
				completionSpec("stop", []cliCompletionFlag{completionFlag("--workspace -n", cliCompletionStaticValue), help}),
				completionSpec("status", []cliCompletionFlag{completionFlag("--workspace -n", cliCompletionStaticValue), help}),
				completionSpec("logs", []cliCompletionFlag{completionFlag("--workspace -n", cliCompletionStaticValue), help}),
			),
			completionSpec("fs", []cliCompletionFlag{help},
				completionSpecWithAliases("list", []string{"ls"}, []cliCompletionFlag{help}),
				completionSpec("get", []cliCompletionFlag{help}),
				completionSpec("put", []cliCompletionFlag{help}),
			),
			completionSpec("attach-workspace", []cliCompletionFlag{completionFlag("--stdio", cliCompletionNoValue), completionFlag("--workspace", cliCompletionStaticValue), help}),
		),
		completionSpec("plugin", []cliCompletionFlag{help},
			completionSpec("install", []cliCompletionFlag{
				completionFlag("--yes --dry-run --link --replace", cliCompletionNoValue),
				completionFlag("--name", cliCompletionStaticValue), help,
			}),
			completionSpec("list", []cliCompletionFlag{help}),
			completionSpec("show", []cliCompletionFlag{help}),
			completionSpecWithAliases("remove", []string{"uninstall"}, []cliCompletionFlag{completionFlag("--yes", cliCompletionNoValue), help}),
			completionSpec("enable", []cliCompletionFlag{help}),
			completionSpec("disable", []cliCompletionFlag{help}),
			completionSpec("doctor", []cliCompletionFlag{help}),
		),
		completionSpec("subagent", []cliCompletionFlag{help},
			completionSpecWithAliases("list", []string{"ls"}, []cliCompletionFlag{completionFlag("--dir", cliCompletionStaticValue), help}),
			completionSpecWithAliases("create", []string{"new"}, append(subagentCompletionFlags(model, effort, help), completionFlag("--scope", cliCompletionStaticValue, "project", "global"))),
			completionSpecWithAliases("edit", []string{"update"}, subagentCompletionFlags(model, effort, help)),
			completionSpecWithAliases("delete", []string{"remove", "rm"}, []cliCompletionFlag{
				completionFlag("--yes", cliCompletionNoValue), completionFlag("--dir", cliCompletionStaticValue), help,
			}),
			completionSpec("try", []cliCompletionFlag{model, completionFlag("--max-steps --dir", cliCompletionStaticValue), help}),
			completionSpec("run", []cliCompletionFlag{model, completionFlag("--max-steps --dir", cliCompletionStaticValue), help}),
		),
		completionSpec("doctor", []cliCompletionFlag{completionFlag("--json", cliCompletionNoValue), help},
			completionSpec("repair", []cliCompletionFlag{
				completionFlag("--root", cliCompletionStaticValue), completionFlag("--apply --project --json", cliCompletionNoValue), help,
			}),
			completionSpec("quality", []cliCompletionFlag{completionFlag("--json", cliCompletionNoValue), help}),
			completionSpec("session", []cliCompletionFlag{completionFlag("--zip", cliCompletionNoValue), completionFlag("--out", cliCompletionStaticValue), help}),
			completionSpec("redact-sessions", []cliCompletionFlag{completionFlag("--dry-run --json", cliCompletionNoValue), completionFlag("--dir", cliCompletionStaticValue), help}),
			completionSpec("capabilities", []cliCompletionFlag{
				completionFlag("--root --timeout", cliCompletionStaticValue), completionFlag("--json --live", cliCompletionNoValue), help,
			}),
		),
		completionSpec("report", []cliCompletionFlag{help},
			completionSpec("list", []cliCompletionFlag{help}),
			completionSpec("show", []cliCompletionFlag{help}),
			completionSpec("send", []cliCompletionFlag{help}),
			completionSpec("delete", []cliCompletionFlag{help}),
		),
		completionSpec("session", []cliCompletionFlag{help},
			completionSpec("list", machineSessionCompletionFlags(help)),
			completionSpec("show", machineSessionCompletionFlags(help)),
			completionSpec("status", machineSessionCompletionFlags(help)),
			completionSpec("recovery", machineSessionCompletionFlags(help)),
		),
		completionSpecWithAliases("hook", []string{"hooks"}, []cliCompletionFlag{help},
			completionSpec("list", hookCompletionFlags(help)),
			completionSpec("status", hookCompletionFlags(help)),
		),
		completionSpec("task", []cliCompletionFlag{help},
			completionSpec("list", taskCompletionFlags(help)),
			completionSpec("show", taskCompletionFlags(help)),
		),
		completionSpec("review", []cliCompletionFlag{
			completionFlag("--base --commit --instructions", cliCompletionStaticValue), model, help,
		}),
		completionSpec("bot", []cliCompletionFlag{help},
			completionSpec("start", []cliCompletionFlag{completionFlag("--channels --dir", cliCompletionStaticValue), model, help}),
			completionSpec("doctor", []cliCompletionFlag{completionFlag("--json --deep", cliCompletionNoValue), help}),
			completionSpec("pairing", []cliCompletionFlag{help},
				completionSpec("list", []cliCompletionFlag{help}),
				completionSpec("approve", []cliCompletionFlag{help}),
				completionSpecWithAliases("reject", []string{"deny"}, []cliCompletionFlag{help}),
			),
			completionSpec("weixin-login", []cliCompletionFlag{completionFlag("--timeout", cliCompletionStaticValue), help}),
		),
		completionSpecWithAliases("upgrade", []string{"update"}, []cliCompletionFlag{
			completionFlag("--check --force", cliCompletionNoValue), completionFlag("--channel", cliCompletionStaticValue), help,
		}),
		completionSpec("completion", []cliCompletionFlag{help},
			completionSpec("bash", nil),
			completionSpec("zsh", nil),
			completionSpec("fish", nil),
		),
		completionSpec("version", []cliCompletionFlag{help}),
		completionSpec("help", []cliCompletionFlag{help}),
	}
	return root
}

func subagentCompletionFlags(model, effort, help cliCompletionFlag) []cliCompletionFlag {
	return []cliCompletionFlag{
		completionFlag("--description --prompt --prompt-file --tools --color --dir", cliCompletionStaticValue),
		model, effort, help,
	}
}

func machineSessionCompletionFlags(help cliCompletionFlag) []cliCompletionFlag {
	return []cliCompletionFlag{
		completionFlag("--json", cliCompletionNoValue),
		completionFlag("--dir --project-root", cliCompletionStaticValue), help,
	}
}

func hookCompletionFlags(help cliCompletionFlag) []cliCompletionFlag {
	return []cliCompletionFlag{
		completionFlag("--json", cliCompletionNoValue),
		completionFlag("--dir --project-root --home-dir", cliCompletionStaticValue), help,
	}
}

func taskCompletionFlags(help cliCompletionFlag) []cliCompletionFlag {
	return []cliCompletionFlag{
		completionFlag("--json", cliCompletionNoValue),
		completionFlag("--dir --project-root --session", cliCompletionStaticValue), help,
	}
}

func completionCommand(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		completionUsage(os.Stdout)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	if args[0] == "__complete" {
		return completionCandidateCommand(args[1:])
	}
	if len(args) != 1 {
		completionUsage(os.Stderr)
		return 2
	}
	var script string
	switch args[0] {
	case "bash":
		script = bashCompletionScript
	case "zsh":
		script = zshCompletionScript
	case "fish":
		script = fishCompletionScript
	default:
		fmt.Fprintf(os.Stderr, "unknown shell %q (want bash, zsh, or fish)\n", args[0])
		return 2
	}
	fmt.Print(script)
	return 0
}

func completionUsage(w *os.File) {
	fmt.Fprintln(w, `Usage:
  reasonix completion bash
  reasonix completion zsh
  reasonix completion fish

The command prints a completion script to stdout. Source it directly or save it
in your shell's completion directory.`)
}

func completionCandidateCommand(args []string) int {
	if len(args) < 1 {
		return 2
	}
	cword, err := strconv.Atoi(args[0])
	if err != nil || cword < 0 {
		return 2
	}
	for _, candidate := range cliCompletionCandidates(cword, args[1:]) {
		if candidate == "" || strings.ContainsAny(candidate, "\r\n") {
			continue
		}
		fmt.Println(candidate)
	}
	return 0
}

func cliCompletionCandidates(cword int, words []string) []string {
	return cliCompletionCandidatesWithValues(cliCompletionRootSpec(), cword, words, runtimeCompletionValues)
}

func cliCompletionCandidatesWithValues(root cliCompletionSpec, cword int, words []string, dynamic func(cliCompletionValueKind) []string) []string {
	if cword < 0 {
		return nil
	}
	current := ""
	if cword < len(words) {
		current = words[cword]
	}
	limit := cword
	if limit > len(words) {
		limit = len(words)
	}

	ctx := &root
	positionalSeen := false
	var awaiting *cliCompletionFlag
	for i := 1; i < limit; i++ {
		token := words[i]
		if awaiting != nil {
			awaiting = nil
			continue
		}
		if flag, inline := cliCompletionLookupFlag(ctx, token); flag != nil {
			if flag.kind != cliCompletionNoValue && !inline {
				awaiting = flag
			}
			continue
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		if !positionalSeen {
			if child := cliCompletionLookupSubcommand(ctx, token); child != nil {
				ctx = child
				continue
			}
		}
		positionalSeen = true
	}

	if awaiting != nil {
		return filterCompletionPrefix(completionFlagValues(*awaiting, dynamic), current)
	}
	if name, value, ok := strings.Cut(current, "="); ok {
		if flag, _ := cliCompletionLookupFlag(ctx, name); flag != nil && flag.kind != cliCompletionNoValue {
			values := completionFlagValues(*flag, dynamic)
			out := make([]string, 0, len(values))
			for _, candidate := range filterCompletionPrefix(values, value) {
				out = append(out, name+"="+candidate)
			}
			return out
		}
	}
	if strings.HasPrefix(current, "-") {
		var out []string
		for _, flag := range ctx.flags {
			out = append(out, flag.names...)
		}
		return filterCompletionPrefix(stableUniqueCompletionValues(out), current)
	}
	if positionalSeen {
		return nil
	}
	var out []string
	for i := range ctx.subcommands {
		out = append(out, ctx.subcommands[i].name)
	}
	return filterCompletionPrefix(stableUniqueCompletionValues(out), current)
}

func cliCompletionLookupFlag(spec *cliCompletionSpec, token string) (*cliCompletionFlag, bool) {
	name := token
	inline := false
	if before, _, ok := strings.Cut(token, "="); ok {
		name = before
		inline = true
	}
	for i := range spec.flags {
		for _, candidate := range spec.flags[i].names {
			if candidate == name {
				return &spec.flags[i], inline
			}
		}
	}
	return nil, inline
}

func cliCompletionLookupSubcommand(spec *cliCompletionSpec, token string) *cliCompletionSpec {
	for i := range spec.subcommands {
		child := &spec.subcommands[i]
		if child.name == token {
			return child
		}
		for _, alias := range child.aliases {
			if alias == token {
				return child
			}
		}
	}
	return nil
}

func completionFlagValues(flag cliCompletionFlag, dynamic func(cliCompletionValueKind) []string) []string {
	switch flag.kind {
	case cliCompletionStaticValue:
		return stableUniqueCompletionValues(flag.values)
	case cliCompletionModelValue, cliCompletionSessionValue:
		return stableUniqueCompletionValues(dynamic(flag.kind))
	default:
		return nil
	}
}

func runtimeCompletionValues(kind cliCompletionValueKind) []string {
	switch kind {
	case cliCompletionModelValue:
		return modelRefs()
	case cliCompletionSessionValue:
		return completionSessionIDs()
	default:
		return nil
	}
}

func completionSessionIDs() []string {
	ordered, err := agent.ListSessionOrder(resolveCLISessionDir())
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(ordered))
	for _, session := range ordered {
		out = append(out, agent.BranchID(session.Path))
	}
	return stableUniqueCompletionValues(out)
}

func filterCompletionPrefix(values []string, prefix string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			out = append(out, value)
		}
	}
	return out
}

func stableUniqueCompletionValues(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

const bashCompletionScript = `# bash completion for reasonix
_reasonix_completion() {
  local line
  COMPREPLY=()
  while IFS= read -r line; do
    COMPREPLY+=("$line")
  done < <(command reasonix completion __complete "$COMP_CWORD" "${COMP_WORDS[@]}" 2>/dev/null)
}
complete -o default -F _reasonix_completion reasonix
`

const zshCompletionScript = `#compdef reasonix
_reasonix_completion() {
  local -a candidates
  candidates=("${(@f)$(command reasonix completion __complete "$((CURRENT - 1))" "${words[@]}" 2>/dev/null)}")
  if (( ${#candidates[@]} )); then
    compadd -- "${candidates[@]}"
  else
    _default
  fi
}
compdef _reasonix_completion reasonix
`

const fishCompletionScript = `function __reasonix_completion
    set -l tokens (commandline -opc)
    set -a tokens (commandline -ct)
    set -l current_index (math (count $tokens) - 1)
    command reasonix completion __complete $current_index $tokens 2>/dev/null
end
complete -c reasonix -f -a '(__reasonix_completion)'
`
