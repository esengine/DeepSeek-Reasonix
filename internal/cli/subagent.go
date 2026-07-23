package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"reasonix/internal/boot"
	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/skill"
)

var setupSubagentCommand = func(ctx context.Context, modelName string, maxStepsOverride int, requireKey bool, sink event.Sink, workspaceRoot string) (*control.Controller, error) {
	return setupProfile(ctx, modelName, maxStepsOverride, requireKey, sink, "", workspaceRoot)
}

const subagentUsageText = `usage:
  reasonix subagent list [--dir PATH]
  reasonix subagent create <name> --description TEXT (--prompt TEXT | --prompt-file PATH) [--scope project|global] [--model REF] [--effort LEVEL] [--tools a,b] [--color NAME] [--dir PATH]
  reasonix subagent edit <name> [--description TEXT] [--prompt TEXT | --prompt-file PATH] [--model REF] [--effort LEVEL] [--tools a,b] [--color NAME] [--dir PATH]
  reasonix subagent delete <name> --yes [--dir PATH]
  reasonix subagent try <name> [--model REF] [--max-steps N] [--dir PATH] <task>
  reasonix subagent run <name> [--model REF] [--max-steps N] [--dir PATH] <task>

Use --prompt-file - or pipe stdin to read a system prompt from stdin.
`

func subagentCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, subagentUsageText)
		return 2
	}
	switch strings.ToLower(args[0]) {
	case "list", "ls":
		return subagentListCommand(args[1:])
	case "create", "new":
		return subagentCreateCommand(args[1:])
	case "edit", "update":
		return subagentEditCommand(args[1:])
	case "delete", "remove", "rm":
		return subagentDeleteCommand(args[1:])
	case "try":
		return subagentRunCommand(args[1:], true)
	case "run":
		return subagentRunCommand(args[1:], false)
	case "help", "--help", "-h":
		fmt.Print(subagentUsageText)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown subagent command %q\n\n%s", args[0], subagentUsageText)
		return 2
	}
}

func subagentListCommand(args []string) int {
	fs := flag.NewFlagSet("subagent list", flag.ContinueOnError)
	dir := fs.String("dir", "", "project root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		fmt.Fprint(os.Stderr, subagentUsageText)
		return 2
	}
	if rc := chdirTo(*dir); rc != 0 {
		return rc
	}
	profiles := subagentProfiles(newCLISubagentStore().SlashList())
	cfg, _ := config.Load()
	if len(profiles) == 0 {
		fmt.Println("no subagent profiles found")
		return 0
	}
	for _, sk := range profiles {
		attributes := []string{string(sk.Scope)}
		if sk.Invocation == "manual" {
			attributes = append(attributes, "manual")
		}
		if sk.ReadOnly {
			attributes = append(attributes, "read-only")
		}
		model := sk.Model
		effort := sk.Effort
		if cfg != nil {
			if override := subagentOverride(cfg.Agent.SubagentModels, sk.Name); override != "" {
				model = override
			}
			if override := subagentOverride(cfg.Agent.SubagentEfforts, sk.Name); override != "" {
				effort = override
			}
		}
		if model != "" {
			attributes = append(attributes, "model="+model)
		}
		if effort != "" {
			attributes = append(attributes, "effort="+effort)
		}
		fmt.Printf("%-40s %-28s %s\n", sk.SlashName(), "["+strings.Join(attributes, ", ")+"]", sk.Description)
	}
	return 0
}

type optionalString struct {
	value string
	set   bool
}

func (v *optionalString) String() string { return v.value }
func (v *optionalString) Set(value string) error {
	v.value = value
	v.set = true
	return nil
}

type subagentProfileFlags struct {
	description optionalString
	prompt      optionalString
	promptFile  optionalString
	model       optionalString
	effort      optionalString
	tools       optionalString
	color       optionalString
	dir         string
}

func addSubagentProfileFlags(fs *flag.FlagSet, values *subagentProfileFlags) {
	fs.Var(&values.description, "description", "one-line profile description")
	fs.Var(&values.prompt, "prompt", "subagent system prompt")
	fs.Var(&values.promptFile, "prompt-file", "read system prompt from a file (- for stdin)")
	fs.Var(&values.model, "model", "per-profile model reference (empty clears on edit)")
	fs.Var(&values.effort, "effort", "per-profile reasoning effort (empty clears on edit)")
	fs.Var(&values.tools, "tools", "comma-separated allowed tools (empty means all tools)")
	fs.Var(&values.color, "color", "profile color tag (empty clears on edit)")
	fs.StringVar(&values.dir, "dir", "", "project root")
}

func subagentCreateCommand(args []string) int {
	name, rest, ok := namedSubagentArgs(args)
	if !ok {
		fmt.Fprint(os.Stderr, subagentUsageText)
		return 2
	}
	fs := flag.NewFlagSet("subagent create", flag.ContinueOnError)
	var values subagentProfileFlags
	addSubagentProfileFlags(fs, &values)
	scopeText := fs.String("scope", "", "project or global (default: project)")
	if err := fs.Parse(rest); err != nil || len(fs.Args()) != 0 {
		return 2
	}
	if rc := chdirTo(values.dir); rc != 0 {
		return rc
	}
	if !values.description.set || strings.TrimSpace(values.description.value) == "" {
		fmt.Fprintln(os.Stderr, "subagent create: --description is required")
		return 2
	}
	prompt, changed, err := resolveSubagentPrompt(values.prompt, values.promptFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "subagent create:", err)
		return 2
	}
	if !changed {
		prompt = readStdin()
		changed = strings.TrimSpace(prompt) != ""
	}
	if !changed || strings.TrimSpace(prompt) == "" {
		fmt.Fprintln(os.Stderr, "subagent create: --prompt or --prompt-file is required")
		return 2
	}
	store := newCLISubagentStore()
	scope, err := profileCreateScope(*scopeText, store.HasProjectScope())
	if err != nil {
		fmt.Fprintln(os.Stderr, "subagent create:", err)
		return 2
	}
	if err := refuseSubagentNameCollision(store.List(), name); err != nil {
		fmt.Fprintln(os.Stderr, "subagent create:", err)
		return 1
	}
	content := renderCLIProfile(name, values.description.value, prompt, values.model.value, values.effort.value, parseToolList(values.tools.value), values.color.value, false)
	path, err := store.CreateWithContent(name, scope, content)
	if err != nil {
		fmt.Fprintln(os.Stderr, "subagent create:", err)
		return 1
	}
	fmt.Printf("created subagent profile %q at %s\n", name, path)
	return 0
}

func subagentEditCommand(args []string) int {
	name, rest, ok := namedSubagentArgs(args)
	if !ok {
		fmt.Fprint(os.Stderr, subagentUsageText)
		return 2
	}
	fs := flag.NewFlagSet("subagent edit", flag.ContinueOnError)
	var values subagentProfileFlags
	addSubagentProfileFlags(fs, &values)
	if err := fs.Parse(rest); err != nil || len(fs.Args()) != 0 {
		return 2
	}
	if rc := chdirTo(values.dir); rc != 0 {
		return rc
	}
	if !profileFlagsChanged(values) {
		fmt.Fprintln(os.Stderr, "subagent edit: provide at least one field to update")
		return 2
	}
	store := newCLISubagentStore()
	sk, ok := store.Read(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "subagent edit: unknown profile %q\n", name)
		return 1
	}
	if sk.Scope == skill.ScopeBuiltin {
		if err := editBuiltinSubagentProfile(sk, values); err != nil {
			fmt.Fprintln(os.Stderr, "subagent edit:", err)
			return 1
		}
		fmt.Printf("updated built-in subagent profile %q overrides\n", sk.Name)
		return 0
	}
	if err := skill.ValidateEditableSubagentProfile(sk); err != nil {
		fmt.Fprintln(os.Stderr, "subagent edit:", err)
		return 1
	}
	prompt, promptChanged, err := resolveSubagentPrompt(values.prompt, values.promptFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "subagent edit:", err)
		return 2
	}
	description, body := sk.Description, sk.Body
	model, effort, color := sk.Model, sk.Effort, sk.Color
	tools := append([]string(nil), sk.AllowedTools...)
	if values.description.set {
		description = values.description.value
	}
	if promptChanged {
		body = prompt
	}
	if values.model.set {
		model = values.model.value
	}
	if values.effort.set {
		effort = values.effort.value
	}
	if values.tools.set {
		tools = parseToolList(values.tools.value)
	}
	if values.color.set {
		color = values.color.value
	}
	if strings.TrimSpace(description) == "" || strings.TrimSpace(body) == "" {
		fmt.Fprintln(os.Stderr, "subagent edit: description and prompt cannot be empty")
		return 2
	}
	content := renderCLIProfile(sk.Name, description, body, model, effort, tools, color, sk.ReadOnly)
	if err := store.UpdateContent(sk.Name, sk.Scope, content); err != nil {
		fmt.Fprintln(os.Stderr, "subagent edit:", err)
		return 1
	}
	fmt.Printf("updated subagent profile %q\n", sk.Name)
	return 0
}

func subagentDeleteCommand(args []string) int {
	name, rest, ok := namedSubagentArgs(args)
	if !ok {
		fmt.Fprint(os.Stderr, subagentUsageText)
		return 2
	}
	fs := flag.NewFlagSet("subagent delete", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm deletion")
	dir := fs.String("dir", "", "project root")
	if err := fs.Parse(rest); err != nil || len(fs.Args()) != 0 {
		return 2
	}
	if !*yes {
		fmt.Fprintln(os.Stderr, "subagent delete: pass --yes to confirm")
		return 2
	}
	if rc := chdirTo(*dir); rc != 0 {
		return rc
	}
	store := newCLISubagentStore()
	sk, ok := store.Read(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "subagent delete: unknown profile %q\n", name)
		return 1
	}
	if err := skill.ValidateEditableSubagentProfile(sk); err != nil {
		fmt.Fprintln(os.Stderr, "subagent delete:", err)
		return 1
	}
	if err := store.Delete(sk.Name, sk.Scope); err != nil {
		fmt.Fprintln(os.Stderr, "subagent delete:", err)
		return 1
	}
	fmt.Printf("deleted subagent profile %q\n", sk.Name)
	return 0
}

func subagentRunCommand(args []string, readOnly bool) int {
	name, rest, ok := namedSubagentArgs(args)
	if !ok {
		fmt.Fprint(os.Stderr, subagentUsageText)
		return 2
	}
	verb := "run"
	if readOnly {
		verb = "try"
	}
	fs := flag.NewFlagSet("subagent "+verb, flag.ContinueOnError)
	model := fs.String("model", "", "default model reference")
	maxSteps := fs.Int("max-steps", 0, "max tool-call rounds")
	dir := fs.String("dir", "", "project root")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if rc := chdirTo(*dir); rc != 0 {
		return rc
	}
	workspaceRoot, err := workspaceRootForDir(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "subagent %s: %v\n", verb, err)
		return 1
	}
	task := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if task == "" {
		task = readStdin()
	}
	if task == "" {
		fmt.Fprintf(os.Stderr, "subagent %s: task is required\n", verb)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctrl, err := setupSubagentCommand(ctx, *model, *maxSteps, true, event.Discard, workspaceRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "subagent %s: %v\n", verb, err)
		return 1
	}
	defer ctrl.Close()
	answer, err := ctrl.RunSubagentProfile(ctx, name, task, readOnly)
	if err != nil {
		fmt.Fprintf(os.Stderr, "subagent %s: %v\n", verb, err)
		return 1
	}
	fmt.Println(answer)
	return 0
}

func namedSubagentArgs(args []string) (string, []string, bool) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", nil, false
	}
	name := strings.TrimSpace(args[0])
	return name, args[1:], name != ""
}

func newCLISubagentStore() *skill.Store {
	cwd, _ := os.Getwd()
	var custom, excluded []string
	var pluginPaths, pluginAgentPaths map[string][]string
	maxDepth := 3
	if cfg, err := config.Load(); err == nil {
		custom = cfg.SkillCustomPaths()
		excluded = cfg.SkillExcludedPaths()
		pluginPaths = cfg.PluginPackageSkillOwners()
		pluginAgentPaths = cfg.PluginPackageAgentOwners()
		maxDepth = cfg.SkillMaxDepth()
	}
	return skill.New(skill.Options{
		ProjectRoot: cwd, CustomPaths: custom, PluginPaths: pluginPaths,
		PluginAgentPaths: pluginAgentPaths, ExcludedPaths: excluded, MaxDepth: maxDepth,
	})
}

func subagentProfiles(skills []skill.Skill) []skill.Skill {
	profiles := make([]skill.Skill, 0, len(skills))
	for _, sk := range skills {
		if sk.RunAs == skill.RunSubagent {
			profiles = append(profiles, sk)
		}
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles
}

func profileCreateScope(raw string, hasProject bool) (skill.Scope, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		if hasProject {
			return skill.ScopeProject, nil
		}
		return skill.ScopeGlobal, nil
	case "project":
		if !hasProject {
			return "", fmt.Errorf("project scope requires a workspace")
		}
		return skill.ScopeProject, nil
	case "global":
		return skill.ScopeGlobal, nil
	default:
		return "", fmt.Errorf("unsupported scope %q; use project or global", raw)
	}
}

func refuseSubagentNameCollision(skills []skill.Skill, name string) error {
	occupied := make([]string, 0, len(skills))
	for _, sk := range skills {
		occupied = append(occupied, sk.Name, sk.SlashName())
	}
	cwd, _ := os.Getwd()
	commands, _ := command.LoadRoots(config.CommandRootsForRoot(cwd)...)
	for _, custom := range commands {
		occupied = append(occupied, custom.Name)
	}
	return skill.ValidateSubagentProfileName(name, occupied)
}

func editBuiltinSubagentProfile(sk skill.Skill, values subagentProfileFlags) error {
	if values.description.set || values.prompt.set || values.promptFile.set || values.tools.set || values.color.set {
		return fmt.Errorf("built-in profile %q only supports --model and --effort overrides", sk.Name)
	}
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	if values.model.set {
		deleteSubagentOverrideAliases(cfg.Agent.SubagentModels, sk.Name)
		ref := strings.TrimSpace(values.model.value)
		if ref != "" {
			entry, ok := cfg.ResolveModel(ref)
			if !ok {
				return fmt.Errorf("unknown model %q", ref)
			}
			if cfg.Agent.SubagentModels == nil {
				cfg.Agent.SubagentModels = map[string]string{}
			}
			cfg.Agent.SubagentModels[sk.Name] = entry.Name + "/" + entry.Model
		}
	}
	if values.effort.set {
		deleteSubagentOverrideAliases(cfg.Agent.SubagentEfforts, sk.Name)
		level := strings.TrimSpace(values.effort.value)
		if level != "" && level != "auto" {
			model := subagentOverride(cfg.Agent.SubagentModels, sk.Name)
			if model == "" {
				model = strings.TrimSpace(cfg.Agent.SubagentModel)
			}
			if model == "" {
				model = cfg.DefaultModel
			}
			entry, ok := cfg.ResolveModel(model)
			if !ok {
				return fmt.Errorf("unknown subagent model %q", model)
			}
			effort, err := config.NormalizeEffort(entry, level)
			if err != nil {
				return err
			}
			if cfg.Agent.SubagentEfforts == nil {
				cfg.Agent.SubagentEfforts = map[string]string{}
			}
			cfg.Agent.SubagentEfforts[sk.Name] = effort
		}
	}
	return cfg.SaveTo(path)
}

func subagentOverride(overrides map[string]string, name string) string {
	for _, key := range boot.SubagentModelKeys(name) {
		if value := strings.TrimSpace(overrides[key]); value != "" {
			return value
		}
	}
	return ""
}

func deleteSubagentOverrideAliases(overrides map[string]string, name string) {
	for _, key := range boot.SubagentModelKeys(name) {
		delete(overrides, key)
	}
}

func resolveSubagentPrompt(prompt, promptFile optionalString) (string, bool, error) {
	if prompt.set && promptFile.set {
		return "", false, fmt.Errorf("use only one of --prompt and --prompt-file")
	}
	if prompt.set {
		return prompt.value, true, nil
	}
	if !promptFile.set {
		return "", false, nil
	}
	if promptFile.value == "-" {
		return readStdin(), true, nil
	}
	path, err := filepath.Abs(promptFile.value)
	if err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

func profileFlagsChanged(values subagentProfileFlags) bool {
	return values.description.set || values.prompt.set || values.promptFile.set || values.model.set ||
		values.effort.set || values.tools.set || values.color.set
}

func parseToolList(raw string) []string {
	seen := map[string]bool{}
	var tools []string
	for _, item := range strings.Split(raw, ",") {
		name := strings.TrimSpace(item)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		tools = append(tools, name)
	}
	return tools
}

func renderCLIProfile(name, description, prompt, model, effort string, tools []string, color string, readOnly bool) string {
	return skill.RenderSkillFile(skill.SkillFileOptions{
		Name:         strings.TrimSpace(name),
		Description:  strings.TrimSpace(description),
		Body:         strings.TrimSpace(prompt),
		RunAs:        skill.RunSubagent,
		Model:        strings.TrimSpace(model),
		Effort:       strings.TrimSpace(effort),
		AllowedTools: tools,
		ReadOnly:     readOnly,
		Color:        strings.TrimSpace(color),
		Invocation:   "manual",
	})
}
