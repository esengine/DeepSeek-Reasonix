package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/sandbox"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

func reviewCommand(args []string) int {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	base := fs.String("base", "", "base branch/commit to diff against (defaults to HEAD — reviews uncommitted working-tree changes)")
	commit := fs.String("commit", "", "review a specific commit (shows changes introduced by that commit)")
	model := fs.String("model", "", "provider name override (default: config default_model)")
	instructions := fs.String("instructions", "", "extra review instructions appended to the prompt")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// 1. Get the diff.
	diff, err := getReviewDiff(*base, *commit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if diff == "" {
		fmt.Println("No changes to review.")
		return 0
	}

	// 2. Load config and resolve model.
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: failed to load config:", err)
		return 1
	}
	modelName := *model
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	entry, ok := cfg.ResolveModel(modelName)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: unknown model %q — check your config\n", modelName)
		return 1
	}
	if err := cfg.Validate(modelName); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	// 3. Create provider.
	prov, err := boot.NewProviderWithProxy(entry, cfg.NetworkProxySpec())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: failed to create provider:", err)
		return 1
	}

	// 4. Get the built-in review skill.
	root, _ := os.Getwd()
	skillStore := skill.New(skill.Options{ProjectRoot: root, Stderr: os.Stderr})
	reviewSk, ok := skillStore.Read("review")
	if !ok {
		fmt.Fprintln(os.Stderr, "error: built-in review skill not found")
		return 1
	}
	if reviewSk.RunAs != skill.RunSubagent {
		fmt.Fprintln(os.Stderr, "error: review skill is not a subagent skill")
		return 1
	}

	// 5. Build a review-scoped sub-agent registry.
	reg := buildReviewSubagentRegistry(reviewSk, cfg, root)

	// 6. Prepare the review prompt.
	task := buildReviewTask(diff, *instructions)

	// 7. Run the review subagent.
	ctx := context.Background()
	// Deliberately minimal Options: this one-shot CLI path has no gate, no
	// compaction, and no session, unlike the in-session sub-agent paths built
	// through TaskTool.subagentOptions / boot's subagentSkillOptions. If a new
	// Options field becomes load-bearing for sub-agents, decide explicitly
	// whether this path needs it too.
	result, err := agent.RunSubAgentWithSession(ctx, prov, reg, agent.NewSession(reviewSk.Body), task, agent.Options{
		MaxSteps:      12,
		Temperature:   cfg.Agent.Temperature,
		Pricing:       entry.Price,
		ContextWindow: entry.ContextWindow,
	}, event.Discard)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: review failed:", err)
		return 1
	}

	fmt.Print(result)
	return 0
}

func buildReviewSubagentRegistry(reviewSk skill.Skill, cfg *config.Config, root string) *tool.Registry {
	// The shared helper strips subagent-unavailable background capabilities while
	// preserving foreground bash. This direct CLI path does not go through boot,
	// so it first builds the small parent set from the review skill allow-list.
	parentReg := tool.NewRegistry()
	for _, name := range reviewSk.AllowedTools {
		if tl, ok := tool.LookupBuiltin(name); ok {
			parentReg.Add(tl)
		}
	}
	// Replace the unconfined init-time defaults with confined instances,
	// mirroring boot's addBuiltins: readers/search bound to the configured
	// forbid-read roots, bash to the OS sandbox spec plus the session-data
	// guard. The zero-value tools registered at init honor none of the user's
	// [sandbox] config, so `reasonix review` previously read forbid_read
	// paths a normal session would refuse.
	writeRoots := cfg.WriteRootsForRoot(root)
	forbidReadRoots := cfg.ForbidReadRootsForRoot(root)
	guard := builtin.NewSessionDataGuard(config.MemoryUserDir(), cfg.AllowWriteRoots())
	bashSpec := sandbox.Spec{
		Mode:            cfg.BashMode(),
		WriteRoots:      writeRoots,
		ForbidReadRoots: forbidReadRoots,
		Network:         cfg.Sandbox.Network,
	}
	searchSpec := builtin.ResolveSearch(cfg.Tools.Search.Engine, cfg.Tools.Search.RgPath, os.Stderr)
	confined := append(builtin.ConfineReaders(forbidReadRoots),
		builtin.ConfineBash(bashSpec, guard),
		builtin.ConfineSearch(searchSpec, bashSpec, forbidReadRoots))
	for _, tl := range confined {
		if _, ok := parentReg.Get(tl.Name()); ok {
			parentReg.Add(tl)
		}
	}
	if reviewSk.ReadOnly {
		// The built-in review skill declares read-only; enforce it here exactly
		// like the in-session runner does (writer tools stripped, bash under the
		// plan-mode safe policy) so `reasonix review` is not a writable backdoor.
		return agent.ReadOnlySubagentToolRegistry(parentReg, reviewSk.AllowedTools)
	}
	return agent.SubagentToolRegistry(parentReg, reviewSk.AllowedTools)
}

// getReviewDiff runs the appropriate git diff command and returns its output.
// - commit="abc": shows diff of abc^..abc
// - base="main": shows diff of main...HEAD
// - neither: shows diff of uncommitted working-tree changes
func getReviewDiff(base, commit string) (string, error) {
	cwd, _ := os.Getwd()
	ctx := context.Background()
	switch {
	case commit != "":
		return runGit(ctx, cwd, "diff", commit+"^.."+commit)
	case base != "":
		return runGit(ctx, cwd, "diff", base+"...HEAD")
	default:
		// Working tree changes: staged + unstaged.
		out, err := runGit(ctx, cwd, "diff", "HEAD")
		if err != nil {
			return "", err
		}
		if out == "" {
			// No working-tree changes; check for staged-only.
			out, err = runGit(ctx, cwd, "diff", "--cached")
		}
		return out, err
	}
}

func buildReviewTask(diff string, extra string) string {
	var b strings.Builder
	b.WriteString("Review the following changes. ")
	if extra != "" {
		b.WriteString(extra)
		b.WriteString(" ")
	}
	b.WriteString("The diff is:\n\n```diff\n")
	// Truncate huge diffs to protect the review subagent's context budget.
	const maxLen = 16000
	if len(diff) > maxLen {
		b.WriteString(diff[:maxLen])
		b.WriteString("\n```\n\n(diff truncated at ")
		fmt.Fprint(&b, maxLen)
		b.WriteString(" chars — focus on the changes shown)")
	} else {
		b.WriteString(diff)
		b.WriteString("\n```")
	}
	return b.String()
}
