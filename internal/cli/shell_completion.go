package cli

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"reasonix/internal/agent"
)

type cliCompletionValueKind uint8

const (
	cliCompletionNoValue cliCompletionValueKind = iota
	cliCompletionStaticValue
	cliCompletionOptionalValue // next token may be a value or another flag (--resume [QUERY])
	cliCompletionPathValue     // path/file; backend returns empty so shells use default file completion
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

func catalogCompletionSpec(help cliCompletionFlag) cliCompletionSpec {
	reindex := make([]cliCompletionSpec, 0, len(catalogCommands))
	for _, command := range catalogCommands {
		flags := append(slices.Clone(command.completionFlags), help)
		reindex = append(reindex, completionSpec(command.name, flags))
	}
	return completionSpec("catalogs", []cliCompletionFlag{help}, completionSpec("reindex", []cliCompletionFlag{help}, reindex...))
}

func cliCompletionRootSpec() cliCompletionSpec {
	model := completionFlag("--model", cliCompletionModelValue)
	resume := completionFlag("--resume -r", cliCompletionOptionalValue) // optional QUERY
	effort := completionFlag("--effort", cliCompletionStaticValue, "auto", "low", "medium", "high", "max")
	permissionMode := completionFlag("--permission-mode", cliCompletionStaticValue,
		"manual", "ask", "auto", "acceptEdits", "dontAsk", "plan", "bypassPermissions")
	help := completionFlag("--help -h", cliCompletionNoValue)

	interactiveFlags := []cliCompletionFlag{
		model,
		completionFlag("--max-steps", cliCompletionStaticValue),
		completionFlag("--continue -c", cliCompletionNoValue),
		resume,
		completionFlag("--copy", cliCompletionNoValue),
		completionFlag("--dangerously-skip-permissions --yolo", cliCompletionNoValue),
		completionFlag("--dir", cliCompletionPathValue),
		effort, permissionMode,
		completionFlag("--add-dir", cliCompletionPathValue),
		completionFlag("--allowed-tools --allowedTools", cliCompletionStaticValue),
		help,
	}
	// run/serve --resume require a value; only interactive root --resume [QUERY] is optional.
	runResume := completionFlag("--resume", cliCompletionSessionValue)
	runFlags := []cliCompletionFlag{
		model,
		completionFlag("--max-steps", cliCompletionStaticValue),
		completionFlag("--show-thinking", cliCompletionNoValue),
		completionFlag("--metrics", cliCompletionPathValue),
		completionFlag("--dir", cliCompletionPathValue),
		completionFlag("--continue -c", cliCompletionNoValue),
		runResume,
		completionFlag("--copy", cliCompletionNoValue),
		effort, permissionMode,
		completionFlag("--auto -y", cliCompletionNoValue),
		completionFlag("--print -p", cliCompletionNoValue),
		completionFlag("--events-jsonl", cliCompletionNoValue),
		completionFlag("--output-format", cliCompletionStaticValue, "text", "json", "stream-json"),
		completionFlag("--add-dir", cliCompletionPathValue),
		completionFlag("--allowed-tools --allowedTools", cliCompletionStaticValue),
		completionFlag("--ablate", cliCompletionStaticValue, "none", "all", "evidence", "planner", "subagent", "retrieval", "compaction"),
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
		completionFlag("--dir", cliCompletionPathValue),
		completionFlag("--add-dir", cliCompletionPathValue),
		completionFlag("--allowed-tools --allowedTools", cliCompletionStaticValue),
		completionFlag("--acp", cliCompletionNoValue),
		completionFlag("--version -v", cliCompletionNoValue),
	}, help)}

	serveFlags := cliServeCompletionFlags(model, help)

	root.subcommands = []cliCompletionSpec{
		completionSpec("run", runFlags),
		completionSpecWithAliases("chat", []string{"code"}, interactiveFlags),
		completionSpec("serve", serveFlags),
		completionSpec("web", serveFlags),
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
			model,
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
			completionSpecWithAliases("list", []string{"ls"}, []cliCompletionFlag{completionFlag("--dir", cliCompletionPathValue), help}),
			completionSpecWithAliases("create", []string{"new"}, append(subagentCompletionFlags(model, effort, help), completionFlag("--scope", cliCompletionStaticValue, "project", "global"))),
			completionSpecWithAliases("edit", []string{"update"}, subagentCompletionFlags(model, effort, help)),
			completionSpecWithAliases("delete", []string{"remove", "rm"}, []cliCompletionFlag{
				completionFlag("--yes", cliCompletionNoValue), completionFlag("--dir", cliCompletionPathValue), help,
			}),
			completionSpec("try", []cliCompletionFlag{model, completionFlag("--max-steps --dir", cliCompletionStaticValue), help}),
			completionSpec("run", []cliCompletionFlag{model, completionFlag("--max-steps --dir", cliCompletionStaticValue), help}),
		),
		doctorCompletionSpec(help),
		completionSpec("sessions", []cliCompletionFlag{help},
			completionSpec("reindex", []cliCompletionFlag{
				completionFlag("--json", cliCompletionNoValue), completionFlag("--dir", cliCompletionPathValue), help,
			}),
			completionSpec("diagnose", []cliCompletionFlag{
				completionFlag("--json", cliCompletionNoValue), completionFlag("--dir", cliCompletionPathValue), help,
			}),
			completionSpec("cleanup", []cliCompletionFlag{
				completionFlag("--json --apply", cliCompletionNoValue), completionFlag("--dir", cliCompletionPathValue), help,
			}),
		),
		catalogCompletionSpec(help),
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
			// Machine list/show: --json --dir --project-root --session (task_machine.go).
			completionSpec("list", completionTaskMachineListFlags(help)),
			completionSpec("show", completionTaskMachineShowFlags(help)),
			// Live monitor surface (task.go FlagSets).
			completionSpec("monitor", []cliCompletionFlag{help},
				completionSpec("list", completionTaskListFlags(help)),
				completionSpec("status", completionTaskStatusFlags(help)),
				completionSpec("events", completionTaskEventsFlags(help)),
				completionSpec("stop", completionTaskControlFlags(help)),
				completionSpec("cancel", completionTaskControlFlags(help)),
				completionSpec("requeue", completionTaskRequeueFlags(help)),
				completionSpec("open-session", completionTaskOpenSessionFlags(help)),
			),
			completionSpec("status", completionTaskStatusFlags(help)),
			completionSpec("events", completionTaskEventsFlags(help)),
			completionSpec("stop", completionTaskControlFlags(help)),
			completionSpec("cancel", completionTaskControlFlags(help)),
			completionSpec("requeue", completionTaskRequeueFlags(help)),
			completionSpec("open-session", completionTaskOpenSessionFlags(help)),
			completionSpec("tmux", []cliCompletionFlag{help},
				completionSpec("attach", completionTaskTmuxAttachFlags(help)),
				completionSpec("status", completionTaskTmuxFlags(help)),
				completionSpec("open", completionTaskTmuxFlags(help)),
				completionSpec("detach", completionTaskTmuxFlags(help)),
			),
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
		completionSpec("version", []cliCompletionFlag{
			completionFlag("--verbose --json", cliCompletionNoValue), help,
		}),
		completionSpec("docs-manifest", []cliCompletionFlag{help}),
		completionSpec("help", []cliCompletionFlag{help}),
	}
	return root
}

func doctorCompletionSpec(help cliCompletionFlag) cliCompletionSpec {
	return completionSpec("doctor", []cliCompletionFlag{completionFlag("--json", cliCompletionNoValue), help},
		completionSpec("sessions", []cliCompletionFlag{completionFlag("--json", cliCompletionNoValue), help}),
		completionSpec("repair", []cliCompletionFlag{
			completionFlag("--root", cliCompletionStaticValue), completionFlag("--apply --project --json", cliCompletionNoValue), help,
		}),
		completionSpec("quality", []cliCompletionFlag{completionFlag("--json", cliCompletionNoValue), help}),
		completionSpec("session", []cliCompletionFlag{completionFlag("--zip", cliCompletionNoValue), completionFlag("--out", cliCompletionStaticValue), help}),
		completionSpec("redact-sessions", []cliCompletionFlag{completionFlag("--dry-run --json", cliCompletionNoValue), completionFlag("--dir", cliCompletionPathValue), help}),
		completionSpec("capabilities", []cliCompletionFlag{
			completionFlag("--root --timeout", cliCompletionStaticValue), completionFlag("--json --live", cliCompletionNoValue), help,
		}),
	)
}

func cliServeCompletionFlags(model, help cliCompletionFlag) []cliCompletionFlag {
	return []cliCompletionFlag{
		model,
		completionFlag("--max-steps --addr", cliCompletionStaticValue),
		// Serve/Web resume accepts file paths, not branch IDs.
		completionFlag("--resume", cliCompletionPathValue),
		completionFlag("--auth", cliCompletionStaticValue, "none", "token", "password"),
		completionFlag("--token --password --port-file --token-file --pid-file", cliCompletionStaticValue),
		completionFlag("--hash-password --behind-proxy --open --no-open", cliCompletionNoValue), help,
	}
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

// completionTask*Flags mirror the real FlagSets in task.go / task_machine.go.
// Names are prefixed with completion to avoid clashing with task.go helpers.

func completionTaskMachineListFlags(help cliCompletionFlag) []cliCompletionFlag {
	return []cliCompletionFlag{
		completionFlag("--json", cliCompletionNoValue),
		completionFlag("--dir --project-root", cliCompletionPathValue),
		completionFlag("--session", cliCompletionStaticValue), help,
	}
}

func completionTaskMachineShowFlags(help cliCompletionFlag) []cliCompletionFlag {
	return completionTaskMachineListFlags(help)
}

func completionTaskListFlags(help cliCompletionFlag) []cliCompletionFlag {
	return []cliCompletionFlag{
		completionFlag("--json", cliCompletionNoValue),
		completionFlag("--dir", cliCompletionPathValue), help,
	}
}

func completionTaskStatusFlags(help cliCompletionFlag) []cliCompletionFlag {
	return completionTaskListFlags(help)
}

func completionTaskEventsFlags(help cliCompletionFlag) []cliCompletionFlag {
	return []cliCompletionFlag{
		completionFlag("--json --jsonl --follow", cliCompletionNoValue),
		completionFlag("--dir", cliCompletionPathValue),
		completionFlag("--after", cliCompletionStaticValue), help,
	}
}

func completionTaskControlFlags(help cliCompletionFlag) []cliCompletionFlag {
	return []cliCompletionFlag{
		completionFlag("--json", cliCompletionNoValue),
		completionFlag("--dir", cliCompletionPathValue),
		completionFlag("--expected-version --reason --idempotency-key", cliCompletionStaticValue), help,
	}
}

func completionTaskRequeueFlags(help cliCompletionFlag) []cliCompletionFlag {
	return []cliCompletionFlag{
		completionFlag("--json", cliCompletionNoValue),
		completionFlag("--dir", cliCompletionPathValue),
		completionFlag("--expected-version --idempotency-key", cliCompletionStaticValue), help,
	}
}

func completionTaskOpenSessionFlags(help cliCompletionFlag) []cliCompletionFlag {
	return completionTaskListFlags(help)
}

func completionTaskTmuxFlags(help cliCompletionFlag) []cliCompletionFlag {
	return completionTaskListFlags(help)
}

func completionTaskTmuxAttachFlags(help cliCompletionFlag) []cliCompletionFlag {
	return []cliCompletionFlag{
		completionFlag("--json", cliCompletionNoValue),
		completionFlag("--dir", cliCompletionPathValue),
		completionFlag("--session", cliCompletionStaticValue), help,
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
	limit := min(cword, len(words))

	ctx := &root
	positionalSeen := false
	var awaiting *cliCompletionFlag
	for i := 1; i < limit; i++ {
		token := words[i]
		if awaiting != nil {
			// Optional values may be omitted: a following flag-looking token is
			// treated as the next flag rather than the optional value.
			if awaiting.kind == cliCompletionOptionalValue && strings.HasPrefix(token, "-") && !strings.HasPrefix(token, "---") {
				awaiting = nil
				// fall through and parse token as a flag/subcommand
			} else {
				awaiting = nil
				continue
			}
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
		if awaiting.kind == cliCompletionPathValue {
			// Empty candidates → shell default file completion (bash -o default, zsh _default, fish without -f).
			return nil
		}
		if awaiting.kind == cliCompletionOptionalValue && strings.HasPrefix(current, "-") {
			// Completing another flag after optional-value flag with no value typed.
			var out []string
			for _, flag := range ctx.flags {
				out = append(out, flag.names...)
			}
			return filterCompletionPrefix(stableUniqueCompletionValues(out), current)
		}
		return filterCompletionPrefix(stableUniqueCompletionValues(completionFlagValues(*awaiting, dynamic)), current)
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
		if slices.Contains(spec.flags[i].names, name) {
			return &spec.flags[i], inline
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
		if slices.Contains(child.aliases, token) {
			return child
		}
	}
	return nil
}

func completionFlagValues(flag cliCompletionFlag, dynamic func(cliCompletionValueKind) []string) []string {
	switch flag.kind {
	case cliCompletionStaticValue:
		return stableUniqueCompletionValues(flag.values)
	case cliCompletionOptionalValue:
		// Interactive --resume [QUERY]: static values (if any) plus dynamic session IDs.
		// Used for both separated and --resume=QUERY forms.
		out := append([]string{}, flag.values...)
		out = append(out, dynamic(cliCompletionSessionValue)...)
		return stableUniqueCompletionValues(out)
	case cliCompletionPathValue:
		return nil
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
complete -c reasonix -a '(__reasonix_completion)'
`
