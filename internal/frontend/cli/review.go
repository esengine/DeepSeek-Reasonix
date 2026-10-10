package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"reasonix/internal/state/sessionstore"
	"strings"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/skill"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/tools/builtin"
)

func reviewCommand(args []string) int {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	base := fs.String("base", "", "base branch/commit to diff against (defaults to HEAD — reviews uncommitted working-tree changes)")
	commit := fs.String("commit", "", "review a specific commit (shows changes introduced by that commit)")
	model := fs.String("model", "", "provider name override (default: config default_model)")
	instructions := fs.String("instructions", "", "extra review instructions appended to the prompt")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
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

	// 2. Load config and resolve model. resolveModelForCLI transparently
	// falls through a keyless default to the next configured provider
	// (issue #6996), so a user whose default_model no longer has a key
	// (e.g. they migrated providers) does not have to add --model to
	// every `reasonix review` invocation.
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: failed to load config:", err)
		return 1
	}
	modelName, _, err := resolveModelForCLI(*model, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
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

	// 4. The checkout under review is untrusted: what runs — the review skill,
	// the tools and their binaries — comes from the user's scope only.
	root, _ := os.Getwd()
	userCfg, err := cfg.Roots().LoadUserConfigReadOnly()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: failed to load user config:", err)
		return 1
	}
	skillStore := skill.New(skill.Options{Stderr: os.Stderr})
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
	reg, checkTargetAccess := buildReviewSubagentRegistry(reviewSk, userCfg, root)

	// 6. Prepare the review prompt.
	task := buildReviewTask(diff, *instructions)

	// 7. Run the review subagent.
	// The reviewed checkout is untrusted input: its own hooks never run here.
	hooks := boot.NewUserHookRunner(cfg, root, os.Stderr)
	hooks.SetSessionID(sessionstore.BranchID(sessionstore.NewSessionPath("", "review")))
	ctx := context.Background()
	// This one-shot path has no gate or compaction, but admission must use
	// the same user-only path policy as its tools.
	result, err := agent.RunReadOnlySubAgentWithSession(ctx, prov, reg, sessionstore.NewSession(reviewSk.Body), task, agent.Options{
		MaxSteps:          12,
		Hooks:             hooks,
		CheckTargetAccess: checkTargetAccess,
		Temperature:       cfg.Agent.Temperature,
		Pricing:           entry.Price,
		ContextWindow:     entry.ContextWindow,
	}, event.Discard)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: review failed:", err)
		return 1
	}

	fmt.Print(result)
	return 0
}

// buildReviewSubagentRegistry takes the user-only config: the search binary and
// the sandbox are settings a reviewed checkout must not choose. Bind tool
// confinement and admission together so both enforce the same policy.
func buildReviewSubagentRegistry(reviewSk skill.Skill, cfg *config.Config, root string) (*tool.Registry, tool.TargetAccessCheck) {
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
	forbidReadRoots := boot.RuntimeForbidReadRoots(cfg, root)
	guard := builtin.NewSessionDataGuard(config.MemoryUserDir(), cfg.AllowWriteRoots())
	checkTargetAccess := (builtin.Workspace{
		WriteRoots: writeRoots, ForbidReadRoots: forbidReadRoots, SessionGuard: guard,
	}).TargetAccessCheck()
	bashSpec := sandbox.Spec{
		Mode:            cfg.BashMode(),
		WriteRoots:      writeRoots,
		Pins:            sandbox.PinWriteRoots(writeRoots),
		ForbidReadRoots: forbidReadRoots,
		Network:         cfg.Sandbox.Network,
		HostAuthorities: sandbox.ParseAuthorities(cfg.Sandbox.HostAuthorities),
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
		// permission-classified read-only policy) so `reasonix review` is not a
		// writable backdoor.
		return agent.ReadOnlySubagentToolRegistry(parentReg, reviewSk.AllowedTools), checkTargetAccess
	}
	return agent.SubagentToolRegistry(parentReg, reviewSk.AllowedTools), checkTargetAccess
}

// getReviewDiff runs the appropriate git diff command and returns its output.
// - commit="abc": shows diff of abc^..abc
// - base="main": shows diff of main...HEAD
// - neither: shows diff of uncommitted working-tree changes
func getReviewDiff(base, commit string) (string, error) {
	cwd, _ := os.Getwd()
	ctx := context.Background()
	// Resolved before the review agent runs, and only once.
	repo, err := gitcmd.Open(ctx, cwd)
	if err != nil {
		return "", err
	}
	switch {
	case commit != "":
		out, err := runGit(ctx, repo, "diff", commit+"^.."+commit)
		if err != nil && repo.ObjectsMissing(ctx, commit, commit+"^") {
			// A partial clone's unfetched objects; host git does not fetch them.
			return "", fmt.Errorf("commit %s: %w", commit, gitcmd.ErrObjectNotLocal)
		}
		return out, err
	case base != "":
		return runGit(ctx, repo, "diff", base+"...HEAD")
	default:
		// Working tree changes: staged + unstaged.
		out, err := runGit(ctx, repo, "diff", "HEAD")
		if err != nil {
			return "", err
		}
		if out == "" {
			// No working-tree changes; check for staged-only.
			out, err = runGit(ctx, repo, "diff", "--cached")
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
