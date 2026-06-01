// Package cli implements reasonix's command-line entry: subcommand routing, flag
// parsing, assembly from config, and exit codes. The core is config-driven —
// providers and tools are resolved from configuration, not hardcoded.
package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
	"reasonix/internal/serve"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

// Run is the CLI entry point; it returns a process exit code.
func Run(args []string, version string) int {
	// Pick the UI language up front so even pre-config paths (the first-run
	// welcome banner) come through localized. Env-only first; if a config
	// exists and pins a language, that wins.
	i18n.DetectLanguage("")
	if cfg, err := config.Load(); err == nil && cfg.Language != "" {
		i18n.DetectLanguage(cfg.Language)
	}

	if len(args) == 0 {
		return welcome(version)
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "run":
		return runAgent(rest)
	case "chat":
		return chatREPL(rest)
	case "serve":
		return runServe(rest)
	case "setup":
		return setupConfig(rest)
	case "init":
		// Project memory (AGENTS.md) is model-generated in-session — `/init` runs
		// the codebase analysis. This CLI entry just points there (and to `setup`
		// for config), so `reasonix init` isn't a dead end.
		return initHint()
	case "acp":
		return acpCommand(rest, version)
	case "mcp":
		return mcpCommand(rest)
	case "codegraph":
		return codegraphCommand(rest)
	case "version", "--version", "-v":
		fmt.Println("reasonix", version)
		return 0
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, i18n.M.UnknownCommandFmt+"\n\n", cmd)
		usage()
		return 2
	}
}

// setup builds a ready-to-drive Controller from config via boot.Build. It is a
// thin adapter kept so the subcommands below read the same as before; the actual
// assembly (model resolution, tool registry, permission gate, two-model
// Coordinator) lives in internal/boot, shared with the desktop frontend.
// requireKey forces the executor's API key to be present (used by run); chat
// passes false so the session UI is reachable before a key is set. sink receives
// the agent's typed event stream — runAgent passes a TextSink that renders to
// stdout, the TUI passes an event-channel sink so events become tea.Msgs.
func setup(ctx context.Context, modelName string, maxStepsOverride int, requireKey bool, sink event.Sink) (*control.Controller, error) {
	return boot.Build(ctx, boot.Options{
		Model:      modelName,
		MaxSteps:   maxStepsOverride,
		RequireKey: requireKey,
		Sink:       sink,
	})
}

// setupQuiet is like setup but suppresses plugin subprocess stderr output.
// Used during model switch inside a bubbletea session to prevent plugin logs
// from corrupting the TUI's terminal raw mode.
func setupQuiet(ctx context.Context, modelName string, maxStepsOverride int, requireKey bool, sink event.Sink) (*control.Controller, error) {
	return boot.Build(ctx, boot.Options{
		Model:      modelName,
		MaxSteps:   maxStepsOverride,
		RequireKey: requireKey,
		Sink:       sink,
		Stderr:     io.Discard,
	})
}

func runAgent(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	model := fs.String("model", "", "provider name (default: config default_model)")
	maxSteps := fs.Int("max-steps", 0, "max tool-call rounds (0 = use config/default)")
	showThinking := fs.Bool("show-thinking", false, "show thinking text instead of the collapsed thinking marker")
	metricsPath := fs.String("metrics", "", "write a JSON token/cache/cost summary of the run to this path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		prompt = readStdin()
	}
	if prompt == "" {
		fmt.Fprintln(os.Stderr, i18n.M.UsageRunHint)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Live run: render the agent's event stream to stdout. Markdown post-stream
	// redraw (cursor moves) is enabled only on a TTY; piped / captured output
	// keeps the raw stream.
	var renderer agent.Renderer
	termW := 80
	if isTTY(os.Stdout) {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			termW = w
		}
		renderer = newMarkdownRenderer(termW)
	}
	textSink := agent.NewTextSink(os.Stdout, renderer, termW)
	textSink.SetShowReasoning(*showThinking)
	var sink event.Sink = textSink
	var metrics *metricsSink
	if *metricsPath != "" {
		metrics = &metricsSink{inner: textSink}
		sink = metrics
	}
	ctrl, err := setup(ctx, *model, *maxSteps, true, sink)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer ctrl.Close()

	runErr := ctrl.Run(ctx, prompt)
	if metrics != nil {
		if err := writeMetrics(*metricsPath, metrics.m); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		}
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "\n"+i18n.M.ErrorPrefix, runErr)
		return 1
	}
	return 0
}

// runServe exposes the controller over HTTP+SSE: events stream to the browser,
// commands arrive as JSON POSTs. The Broadcaster is the controller's event sink,
// so the same typed stream the chat TUI consumes reaches web clients — the
// transport-agnostic controller driven by a second frontend.
func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	model := fs.String("model", "", "provider name (default: config default_model)")
	maxSteps := fs.Int("max-steps", 0, "max tool-call rounds (0 = use config/default)")
	addr := fs.String("addr", "127.0.0.1:8787", "listen address")
	resume := fs.String("resume", "", "resume a saved session file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx := context.Background()
	bc := serve.NewBroadcaster()
	ctrl, err := setup(ctx, *model, *maxSteps, true, bc)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer ctrl.Close()

	// Auto-save target: reuse the resumed file, else a fresh one — same as chat.
	if *resume != "" {
		loaded, err := agent.LoadSession(*resume)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		ctrl.Resume(loaded, *resume)
	} else if ctrl.SessionDir() != "" {
		ctrl.SetSessionPath(agent.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	}

	fmt.Printf("reasonix serve — %s on http://%s\n", ctrl.Label(), *addr)
	// Use graceful shutdown so SIGINT/SIGTERM drain active connections.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := serve.New(ctrl, bc).RunGraceful(ctx, *addr); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	return 0
}

// chatREPL is an interactive session: a single persistent agent/session and a
// prompt loop that keeps conversation context across turns. Exit with
// 'exit'/'quit' or Ctrl-D.
func chatREPL(args []string) int {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	model := fs.String("model", "", "provider name (default: config default_model)")
	maxSteps := fs.Int("max-steps", 0, "max tool-call rounds (0 = use config/default)")
	cont := fs.Bool("continue", false, "resume the most recent saved session")
	fs.BoolVar(cont, "c", false, "shorthand for --continue")
	resume := fs.Bool("resume", false, "list saved sessions and pick one to resume")
	yolo := fs.Bool("dangerously-skip-permissions", false, "YOLO: auto-approve every tool call this session (deny rules still apply)")
	fs.BoolVar(yolo, "yolo", false, "alias for --dangerously-skip-permissions")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Decide whether we're starting fresh or resuming. --resume opens an
	// interactive picker; --continue / -c jumps straight into the newest.
	var resumePath string
	switch {
	case *resume:
		path, rc := pickSessionToResume()
		if rc != 0 {
			return rc
		}
		resumePath = path
	case *cont:
		sessions, err := agent.ListSessions(config.SessionDir())
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		if len(sessions) == 0 {
			fmt.Fprintln(os.Stderr, i18n.M.NoSessionToResume)
			return 1
		}
		resumePath = sessions[0].Path
	}

	ctx := context.Background()

	// Plumb the controller's typed event stream through a channel so each event
	// can become a tea.Msg inside the TUI's update loop. Buffered generously:
	// streaming bursts (tool results, long answers) shouldn't backpressure the
	// agent goroutine.
	eventCh := make(chan event.Event, 1024)

	sink := &eventSink{ch: eventCh}
	ctrl, err := setup(ctx, *model, *maxSteps, false, sink)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}

	// Decide where this conversation's auto-save lands. A resume reuses the
	// file so closing/reopening keeps appending to the same history; a fresh
	// session lands in a new file stamped with the model name.
	if resumePath != "" {
		if loaded, err := agent.LoadSession(resumePath); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		} else {
			ctrl.Resume(loaded, resumePath)
		}
	} else if ctrl.SessionDir() != "" {
		ctrl.SetSessionPath(agent.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	}

	// Surface a missing-key warning inside the TUI banner so the first message
	// failing is at least pre-announced; the user can still enter chat.
	missing := ""
	if cfg, loadErr := config.Load(); loadErr == nil {
		name := *model
		if name == "" {
			name = cfg.DefaultModel
		}
		if vErr := cfg.Validate(name); vErr != nil {
			missing = vErr.Error()
		}
	}

	// Initial terminal width — the TUI re-flows on every WindowSizeMsg so
	// this is just a starting estimate before the first resize event lands.
	termW := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		termW = w
	}

	// Route "ask" decisions to the TUI: the controller emits an ApprovalRequest
	// event and blocks until the user answers via ctrl.Approve. Sub-agents (the
	// task tool) keep their headless gate from setup — no UI to prompt through.
	ctrl.EnableInteractiveApproval()
	// YOLO: skip every approval prompt for the session (deny rules still apply).
	if *yolo {
		ctrl.SetBypass(true)
	}

	m := newChatTUI(ctrl, missing, eventCh, termW)
	if cfg, err := config.Load(); err == nil {
		m.outputStyle = cfg.Agent.OutputStyle    // shown as the active entry in /output-style
		m.statuslineCmd = cfg.Statusline.Command // custom status-line command, "" = built-in row
	}

	// /model support: a pure builder the TUI calls to rebuild on a different
	// model (carrying the conversation). It must NOT touch the running model —
	// runModelSubcommand performs the swap on the live copy. The same stable sink
	// feeds the new controller, so events keep flowing to this TUI.
	m.buildController = func(ref string, carry []provider.Message) (*control.Controller, error) {
		c, err := setupQuiet(ctx, ref, *maxSteps, false, sink)
		if err != nil {
			return nil, err
		}
		path := ""
		if dir := c.SessionDir(); dir != "" {
			path = agent.NewSessionPath(dir, c.Label())
		}
		if len(carry) > 0 {
			c.Resume(&agent.Session{Messages: carry}, path)
		} else if path != "" {
			c.SetSessionPath(path)
		}
		c.EnableInteractiveApproval()
		if *yolo {
			c.SetBypass(true)
		}
		return c, nil
	}
	if cfg, e := config.Load(); e == nil {
		name := *model
		if name == "" {
			name = cfg.DefaultModel
		}
		if entry, ok := cfg.ResolveModel(name); ok {
			m.modelRef = entry.Name + "/" + entry.Model
		}
	}

	// No alt-screen: finalized transcript lines are committed to the terminal's
	// normal buffer (via tea.Println) so native scrollback, the wheel, and copy
	// all work — the bubbletea-managed region is just the bottom input/status.
	p := tea.NewProgram(m)
	final, runErr := p.Run()
	// Close the active controller plus any retired ones from /model switches.
	// Retired controllers were stashed rather than closed at switch time
	// because Controller.Close() runs SessionEnd hooks and kills plugin
	// subprocesses — operations that corrupt bubbletea's terminal raw mode
	// when executed while the TUI is alive.
	if fm, ok := final.(chatTUI); ok {
		for _, oc := range fm.oldControllers {
			oc.Close()
		}
		if fm.ctrl != nil {
			fm.ctrl.Close()
		} else {
			ctrl.Close()
		}
	} else {
		ctrl.Close()
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, runErr)
		return 1
	}
	return 0
}

// setupConfig runs the configuration wizard (the `reasonix setup` command),
// writing reasonix.toml (+ .env). Project memory is a separate concern — the
// in-session `/init` skill generates AGENTS.md (see initHint).
func setupConfig(args []string) int {
	path := "reasonix.toml"
	if len(args) > 0 {
		path = args[0]
	}
	if _, err := os.Stat(path); err == nil {
		// Non-interactive must not clobber an existing config silently.
		if !isInteractive() {
			fmt.Fprintf(os.Stderr, i18n.M.NotOverwritingFmt+"\n", path)
			return 1
		}
		in := bufio.NewScanner(os.Stdin)
		ans := ask(in, os.Stdout, fmt.Sprintf(i18n.M.ConfirmReconfigureFmt, path), "N")
		if ans != "y" && ans != "Y" {
			fmt.Println(i18n.M.KeepingExisting)
			return 0
		}
	}

	// Interactive wizard on a TTY; fall back to the annotated default when piped.
	if isInteractive() {
		rc := interactiveSetup(path)
		if rc == 0 {
			fmt.Printf(i18n.M.TryHintFmt+"\n", bold("reasonix chat"))
		}
		return rc
	}
	return writeDefaultConfig(path)
}

func writeDefaultConfig(path string) int {
	if err := config.Default().WriteFile(path); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.WriteConfigErr, err)
		return 1
	}
	fmt.Printf(i18n.M.WroteFileFmt+"\n", path)
	fmt.Println(i18n.M.NextHint)
	return 0
}

// initHint handles `reasonix init`. Unlike a config scaffold, project memory is
// model-generated by analyzing the codebase, so it lives as the in-session
// `/init` skill rather than a CLI command. This entry just points the user there
// (and to `reasonix setup` for config) so the verb isn't a dead end.
func initHint() int {
	fmt.Println(i18n.M.InitHint)
	return 0
}

// interactiveSetup runs the setup wizard, then writes reasonix.toml (plus .env for any
// keys entered). The wizard is intentionally minimal: pick language, pick
// provider, enter API keys. Language is asked first so every subsequent prompt
// is already in the user's language even when env auto-detection got it wrong.
// Two-model collaboration is left as a manual config edit (planner_model) so
// first-run never confronts newcomers with advanced choices.
func interactiveSetup(path string) int {
	// Seed from the existing config when reconfiguring, so a re-run to fix a key
	// preserves the user's providers / agent settings instead of resetting to
	// defaults. First run (no file) falls back to the built-in defaults.
	cfg := config.LoadForEdit(path)
	prevDefault := cfg.DefaultModel

	lang, err := selectLanguage()
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nsetup cancelled.")
		return 1
	}
	cfg.Language = lang
	i18n.DetectLanguage(lang)

	// Now that the catalogue matches the user's choice, show the welcome banner
	// in their language before any substantive prompt.
	fmt.Println()
	fmt.Print(boxed([]string{
		accent("◆") + " " + fmt.Sprintf(i18n.M.WelcomeTitleFmt, bold("reasonix")),
		"",
		dim(i18n.M.NoConfigYet),
	}))
	fmt.Println()

	enabled, err := selectEnabledProviders(cfg.Providers)
	if err != nil {
		fmt.Fprintln(os.Stderr, "\n"+i18n.M.SetupCancelled)
		return 1
	}

	envLines := configureKeys(enabled, os.Stdin, os.Stdout)

	cfg.Providers = enabled
	// Keep the previous default model if it's still enabled; otherwise fall back
	// to the first selected provider.
	cfg.DefaultModel = enabled[0].Name
	for _, p := range enabled {
		if p.Name == prevDefault {
			cfg.DefaultModel = prevDefault
			break
		}
	}

	if err := cfg.WriteFile(path); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.WriteConfigErr, err)
		return 1
	}
	fmt.Printf("\n%s %s\n", green("✓"), fmt.Sprintf(i18n.M.WroteFileFmt, path))

	if len(envLines) > 0 {
		if err := appendEnv(".env", envLines); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.WriteEnvErr, err)
			return 1
		}
		fmt.Printf("%s %s\n", green("✓"), fmt.Sprintf(i18n.M.WroteFileFmt, ".env"))
	}

	fmt.Printf("\n%s %s\n", accent("◆"), i18n.M.SetupComplete)
	return 0
}

// pickSessionToResume scans the session dir, takes the 10 most recent, and
// shows a single-choice menu with timestamp + turn count + first user
// message so the user can pick one. Returns the chosen path and a process
// exit code (non-zero when there's nothing to pick or the user cancelled).
func pickSessionToResume() (string, int) {
	sessions, err := agent.ListSessions(config.SessionDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return "", 1
	}
	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, i18n.M.NoSessionToResume)
		return "", 1
	}
	if !isInteractive() {
		fmt.Fprintln(os.Stderr, i18n.M.ResumeRequiresTTY)
		return "", 1
	}
	const cap = 10
	if len(sessions) > cap {
		sessions = sessions[:cap]
	}
	items := make([]menuItem, len(sessions))
	for i, s := range sessions {
		when := s.ModTime.Local().Format("01-02 15:04")
		preview := s.Preview
		if preview == "" {
			preview = "(no user message yet)"
		}
		items[i] = menuItem{
			name: when,
			desc: fmt.Sprintf("%d turns · %s", s.Turns, preview),
		}
	}
	idx, err := selectOne(i18n.M.PickSessionLabel, items)
	if err != nil {
		return "", 1
	}
	return sessions[idx].Path, 0
}

// selectLanguage is the wizard's first prompt: it shows the two UI languages
// in their native form and pre-selects the env-detected one (so a single Enter
// confirms the auto-detection, a single arrow + Enter picks the other). The
// label is bilingual because we don't yet know which catalogue to trust.
func selectLanguage() (string, error) {
	detected := i18n.DetectLanguage("")
	items := []menuItem{{name: "English"}, {name: "中文 (简体)"}}
	tags := []string{"en", "zh"}
	if detected == "zh" {
		items[0], items[1] = items[1], items[0]
		tags[0], tags[1] = tags[1], tags[0]
	}
	idx, err := selectOne("Language · 语言", items)
	if err != nil {
		return "", err
	}
	return tags[idx], nil
}

// selectEnabledProviders prompts a single multi-select of provider families
// (DeepSeek / MiMo / …) and returns every SKU of every chosen family. Picking
// a family enables all of its SKUs; users who want to exclude a specific SKU
// edit reasonix.toml afterward — keeping first-run a single decision instead of two.
func selectEnabledProviders(providers []config.ProviderEntry) ([]config.ProviderEntry, error) {
	famOrder, famMembers, famInfo := groupByFamily(providers)

	famItems := make([]menuItem, len(famOrder))
	for i, k := range famOrder {
		famItems[i] = menuItem{name: famInfo[k].name, desc: famInfo[k].desc}
	}
	famIdxs, err := selectMany(i18n.M.SelectProvidersLabel, famItems)
	if err != nil {
		return nil, err
	}

	var enabled []config.ProviderEntry
	for _, fi := range famIdxs {
		for _, mi := range famMembers[famOrder[fi]] {
			enabled = append(enabled, providers[mi])
		}
	}
	return enabled, nil
}

// providerFamily is a wizard-only grouping of provider SKUs by vendor; it does
// not exist in config because users editing reasonix.toml deal with SKU names
// directly. Keys mirror the SKU name prefix (deepseek-*, mimo) so adding a new
// preset only requires a familyOf case.
type providerFamily struct {
	key  string
	name string
	desc string
}

func familyOf(name string) providerFamily {
	switch {
	case strings.HasPrefix(name, "deepseek"):
		return providerFamily{key: "deepseek", name: "DeepSeek", desc: "fast & cheap, plus a stronger Pro SKU"}
	case strings.HasPrefix(name, "mimo"):
		return providerFamily{key: "mimo", name: "MiMo (Xiaomi)", desc: "long-horizon agentic"}
	default:
		return providerFamily{key: name, name: name}
	}
}

func groupByFamily(providers []config.ProviderEntry) ([]string, map[string][]int, map[string]providerFamily) {
	var order []string
	members := map[string][]int{}
	info := map[string]providerFamily{}
	for i, p := range providers {
		f := familyOf(p.Name)
		if _, seen := members[f.key]; !seen {
			order = append(order, f.key)
			info[f.key] = f
		}
		members[f.key] = append(members[f.key], i)
	}
	return order, members, info
}

// promptMissingKeys re-runs the wizard's key-entry step for any enabled
// provider whose api_key_env is unset. Newly entered values are appended to
// .env so the chat session that follows picks them up via config.Load. The
// user can hit Enter to skip — the chat banner falls back to a one-line
// warning so they still see what's missing. Returns a non-zero exit code only
// when writing .env fails.
func promptMissingKeys(cfg *config.Config) int {
	missing := providersWithMissingKeys(cfg)
	if len(missing) == 0 {
		return 0
	}
	fmt.Println()
	fmt.Println(dim("  " + i18n.M.MissingKeyIntro))
	envLines := configureKeys(missing, os.Stdin, os.Stdout)
	if len(envLines) == 0 {
		return 0
	}
	if err := appendEnv(".env", envLines); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.WriteEnvErr, err)
		return 1
	}
	fmt.Printf("%s %s\n", green("✓"), fmt.Sprintf(i18n.M.WroteFileFmt, ".env"))
	return 0
}

// providersWithMissingKeys returns the subset of cfg.Providers whose api_key_env
// is declared but not currently set in the environment. configureKeys dedupes
// shared envs, so duplicates are fine to leave in.
func providersWithMissingKeys(cfg *config.Config) []config.ProviderEntry {
	var out []config.ProviderEntry
	for _, p := range cfg.Providers {
		if p.APIKeyEnv != "" && os.Getenv(p.APIKeyEnv) == "" {
			out = append(out, p)
		}
	}
	return out
}

// configureKeys asks for the API key of each distinct api_key_env among the
// selected providers — shared keys (e.g. both DeepSeek models) are asked once.
// Reads answers from r, writes prompts to w; returns KEY=value lines entered.
func configureKeys(selected []config.ProviderEntry, r io.Reader, w io.Writer) []string {
	in := bufio.NewScanner(r)
	fmt.Fprintln(w, "\n"+i18n.M.EnterAPIKeysHeader)

	seen := map[string]bool{}
	var envLines []string
	for _, p := range selected {
		if p.APIKeyEnv == "" || seen[p.APIKeyEnv] {
			continue
		}
		seen[p.APIKeyEnv] = true
		if key := ask(in, w, "  "+p.APIKeyEnv, ""); key != "" {
			envLines = append(envLines, p.APIKeyEnv+"="+key)
		}
	}
	return envLines
}

// ask prints a prompt to w and returns the entered line, or def if input is empty.
func ask(in *bufio.Scanner, w io.Writer, label, def string) string {
	if def != "" {
		fmt.Fprintf(w, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(w, "%s: ", label)
	}
	if !in.Scan() {
		return def
	}
	if v := strings.TrimSpace(in.Text()); v != "" {
		return v
	}
	return def
}

// isInteractive reports whether we're attached to a real terminal on both
// stdin and stdout — required for prompting. Redirected or piped I/O is not
// interactive, so wizards never block or auto-default in scripts and CI.
func isInteractive() bool {
	return isTTY(os.Stdin) && isTTY(os.Stdout)
}

func isTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// appendEnv merges KEY=value lines into a .env file. Existing assignments of
// any key that's about to be written are dropped first, then the new values
// are appended — so re-running `reasonix setup` with a corrected key replaces the
// stale one instead of stacking duplicates (loadDotEnv is first-wins, so a
// naive append would leave the old key in effect). The new values are also
// pinned into the current process env so a chat session started right after
// init picks up the fresh keys without a restart.
func appendEnv(path string, lines []string) error {
	target := map[string]bool{}
	for _, l := range lines {
		if k, _, ok := strings.Cut(l, "="); ok {
			target[strings.TrimSpace(k)] = true
		}
	}

	var kept []string
	if data, err := os.ReadFile(path); err == nil {
		for _, raw := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(raw)
			check := strings.TrimPrefix(trimmed, "export ")
			if k, _, ok := strings.Cut(check, "="); ok && target[strings.TrimSpace(k)] {
				continue
			}
			kept = append(kept, raw)
		}
		// strings.Split on a string ending with \n leaves a trailing empty
		// element; trim it so we don't grow a blank line on every rewrite.
		if n := len(kept); n > 0 && kept[n-1] == "" {
			kept = kept[:n-1]
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	var b strings.Builder
	for _, l := range kept {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
		if k, v, ok := strings.Cut(l, "="); ok {
			os.Setenv(strings.TrimSpace(k), v)
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// readStdin reads piped input if present; an interactive terminal yields "".
func readStdin() string {
	stat, err := os.Stdin.Stat()
	if err != nil || stat.Mode()&os.ModeCharDevice != 0 {
		return ""
	}
	data, _ := io.ReadAll(os.Stdin)
	return strings.TrimSpace(string(data))
}

// welcome is the zero-arg landing screen: it reports config and key readiness,
// then guides the user to the next concrete step.
func welcome(version string) int {
	src := config.SourcePath()

	// First run on an interactive terminal: actively guide setup rather than
	// printing a static screen and exiting. interactiveSetup owns the language
	// prompt and welcome banner so every prompt the user sees is already
	// localized to their choice.
	if src == "" && isInteractive() {
		if rc := interactiveSetup("reasonix.toml"); rc != 0 {
			return rc
		}
		// Config just written; reload so .env (and any pinned language) is
		// picked up. If the chosen provider's key is ready, drop into chat.
		if cfg, err := config.Load(); err == nil && cfg.Validate(cfg.DefaultModel) == nil {
			if cfg.Language != "" {
				i18n.DetectLanguage(cfg.Language)
			}
			fmt.Printf("\n"+i18n.M.StartingChatFmt+"\n\n", bold("reasonix chat"))
			return chatREPL(nil)
		}
		fmt.Println("\n" + i18n.M.SetKeyHint)
		return 0
	}

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		cfg = config.Default()
	}

	// reasonix.toml exists and parses on a terminal: go into chat. If any enabled
	// provider's key isn't set yet, re-run the wizard's key-entry step inline
	// — first run already chose language and providers, so we don't re-ask
	// those. Skipping the prompts is still fine; the chat banner falls back to
	// a one-line warning.
	if src != "" && cfgErr == nil && isInteractive() {
		if rc := promptMissingKeys(cfg); rc != 0 {
			return rc
		}
		return chatREPL(nil)
	}

	var b strings.Builder
	b.WriteString(boxed([]string{
		accent("◆") + " " + bold("reasonix") + "  " + dim(version),
		dim(i18n.M.Subtitle),
	}))

	switch {
	case src == "":
		fmt.Fprintf(&b, "\n  %s %s\n", padRight(i18n.M.ConfigLabel, 8), dim(i18n.M.ConfigNotFound))
	case cfgErr != nil:
		fmt.Fprintf(&b, "\n  %s %s\n", padRight(i18n.M.ConfigLabel, 8), yellow(fmt.Sprintf(i18n.M.ConfigErrorFmt, src, cfgErr)))
	default:
		fmt.Fprintf(&b, "\n  %s %s\n", padRight(i18n.M.ConfigLabel, 8), src)
	}

	ready := 0
	for i, p := range cfg.Providers {
		label := i18n.M.ModelsLabel
		if i > 0 {
			label = ""
		}
		dot, status := yellow("●"), dim(i18n.M.NoKey)
		if p.APIKey() != "" {
			dot, status = green("●"), green(i18n.M.Ready)
			ready++
		}
		fmt.Fprintf(&b, "  %s %s %s%s\n", padRight(label, 8), dot, padRight(p.Name, 16), status)
	}

	fmt.Fprintf(&b, "\n  %s %s\n", accent("▌"), bold(i18n.M.GetStarted))
	n := 1
	step := func(cmd, desc string) {
		fmt.Fprintf(&b, "    %s  %s %s\n", accent(fmt.Sprint(n)), padRight(cmd, 16), dim(desc))
		n++
	}
	if src == "" {
		step("reasonix setup", i18n.M.StepScaffold)
	}
	if ready == 0 {
		step(i18n.M.StepSetKey, i18n.M.StepSetKeyHint)
	}
	step("reasonix chat", i18n.M.StepChatDesc)
	step(`reasonix run "task"`, i18n.M.StepRunDesc)

	fmt.Fprintf(&b, "\n  %s\n", dim(i18n.M.HelpFooter))

	fmt.Print(b.String())
	return 0
}

func usage() {
	fmt.Print(i18n.M.UsageBody)
}
