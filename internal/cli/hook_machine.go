package cli

import (
	"io"
	"os"
	"sort"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/hook"
)

type machineHook struct {
	Event  string `json:"event"`
	Match  string `json:"match,omitempty"`
	Scope  string `json:"scope"`
	Status string `json:"status"`
}

type machineHookSource struct {
	Scope     string `json:"scope"`
	Status    string `json:"status"`
	HookCount int    `json:"hook_count"`
}

type machineHookList struct {
	SchemaVersion int           `json:"schema_version"`
	Command       string        `json:"command"`
	Hooks         []machineHook `json:"hooks"`
}

type machineHookStatus struct {
	SchemaVersion  int                 `json:"schema_version"`
	Command        string              `json:"command"`
	TrustedProject bool                `json:"trusted_project"`
	ProjectDefines bool                `json:"project_defines"`
	Sources        []machineHookSource `json:"sources"`
}

type hookMachineOptions struct {
	projectRoot string
	homeDir     string
	json        bool
}

func hookCommand(args []string) int {
	return runHookCommand(args, os.Stdout)
}

func runHookCommand(args []string, out io.Writer) int {
	command := "hook"
	if len(args) == 0 {
		return writeMachineError(out, command, "invalid_argument", "a hook operation is required")
	}
	operation := args[0]
	command = "hook." + operation
	if operation != "list" && operation != "status" {
		return writeMachineError(out, command, "unknown_command", "unknown hook operation")
	}
	options, code, message := parseHookMachineOptions(args[1:])
	if code != "" {
		return writeMachineError(out, command, code, message)
	}
	if !options.json {
		return writeMachineError(out, command, "invalid_argument", "--json is required")
	}
	if options.projectRoot == "" {
		options.projectRoot, _ = os.Getwd()
	}
	if options.homeDir == "" {
		options.homeDir = config.ReasonixHomeDir()
	}
	inspection := hook.Inspect(hook.LoadOptions{
		ProjectRoot: options.projectRoot,
		HomeDir:     options.homeDir,
	})
	if operation == "list" {
		items := make([]machineHook, 0, len(inspection.Entries))
		for _, entry := range inspection.Entries {
			status := machineHookEntryStatus(entry)
			match := entry.Match
			if match == "" {
				match = "*"
			}
			items = append(items, machineHook{Event: string(entry.Event), Match: match, Scope: string(entry.Scope), Status: status})
		}
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Event != items[j].Event {
				return items[i].Event < items[j].Event
			}
			if items[i].Scope != items[j].Scope {
				return items[i].Scope < items[j].Scope
			}
			return items[i].Match < items[j].Match
		})
		return writeMachineJSON(out, machineHookList{SchemaVersion: machineSchemaVersion, Command: command, Hooks: items})
	}
	sources := make([]machineHookSource, 0, len(inspection.Sources))
	for _, source := range inspection.Sources {
		sources = append(sources, machineHookSource{Scope: string(source.Scope), Status: source.Status, HookCount: source.HookCount})
	}
	sort.SliceStable(sources, func(i, j int) bool { return sources[i].Scope < sources[j].Scope })
	return writeMachineJSON(out, machineHookStatus{
		SchemaVersion:  machineSchemaVersion,
		Command:        command,
		TrustedProject: inspection.TrustedProject,
		ProjectDefines: inspection.ProjectDefines,
		Sources:        sources,
	})
}

func machineHookEntryStatus(entry hook.Entry) string {
	if !hook.IsKnownEvent(string(entry.Event)) {
		return "invalid"
	}
	command := strings.TrimSpace(entry.Command)
	contextFile := strings.TrimSpace(entry.ContextFile)
	if entry.Scope == hook.ScopePlugin {
		if command == "" && contextFile == "" {
			return "invalid"
		}
		if contextFile != "" && !hook.ContextFileUsable(contextFile) {
			return "invalid"
		}
	} else if command == "" {
		// Native project/global loading requires a command; contextFile is an
		// internal plugin-only execution path.
		return "invalid"
	}
	if hook.UsesToolMatcher(entry.Event) && hook.ValidateMatcher(entry.Match) != "" {
		return "invalid"
	}
	return "active"
}

func parseHookMachineOptions(args []string) (hookMachineOptions, string, string) {
	var options hookMachineOptions
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			options.json = true
		case "--project-root", "--dir":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return options, "invalid_argument", "project root requires a value"
			}
			i++
			options.projectRoot = args[i]
		case "--home-dir":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return options, "invalid_argument", "home directory requires a value"
			}
			i++
			options.homeDir = args[i]
		case "--help", "-h":
			return options, "invalid_argument", "use the documented machine interface"
		default:
			return options, "invalid_argument", "unknown hook option"
		}
	}
	return options, "", ""
}
