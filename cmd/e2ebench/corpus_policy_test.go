package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	fileencoding "reasonix/internal/base/fileutil/encoding"
)

// benchmarksRoot holds every committed task corpus. Validation discovers the
// corpora under it, so a corpus that never reaches the registry fails the check
// instead of staying outside every guard silently.
const benchmarksRoot = "../../benchmarks"

// authoringRule names one of the authoring guards a corpus answers to.
type authoringRule string

const (
	rulePristineSeed authoringRule = "pristine-seed"
	ruleReference    authoringRule = "reference-solution"
	ruleStopPolicy   authoringRule = "stop-policy"
)

// allRules is the validator's closed vocabulary: a registry key outside it is an
// unknown rule, not a silently ignored one.
var allRules = []authoringRule{rulePristineSeed, ruleReference, ruleStopPolicy}

// ruleMode records how a rule applies to one corpus. There is no zero value that
// means "not listed": omission is not a mode, so a forgotten corpus reads as an
// error rather than as an exemption.
type ruleMode string

const (
	// ruleEnforce applies the whole rule.
	ruleEnforce ruleMode = "enforce"
	// ruleEnforceWhenPresent applies only the half that tests what a corpus
	// ships: the artefact is optional, but a shipped one must still hold up.
	// Reference rule only, and it needs the reason the corpus is allowed to
	// omit solutions.
	ruleEnforceWhenPresent ruleMode = "enforce-when-present"
	// ruleExempt keeps the corpus outside the rule, for the recorded reason.
	ruleExempt ruleMode = "exempt"
)

type ruleDecision struct {
	Mode   ruleMode
	Reason string
}

type corpusPolicy struct {
	Dir   string
	Rules map[authoringRule]ruleDecision
}

// corpusPolicies is the single registry the authoring guards read. Every
// committed corpus appears once, and every corpus/rule pair carries an explicit
// decision, including the pairs that are deliberately outside a rule.
func corpusPolicies() []corpusPolicy {
	return []corpusPolicy{
		{
			Dir: corpusDir,
			Rules: map[authoringRule]ruleDecision{
				rulePristineSeed: {Mode: ruleEnforce},
				ruleReference: {Mode: ruleEnforceWhenPresent,
					Reason: "the e2e suite does not commit reference solutions, so a task without one is skipped, not failed"},
				ruleStopPolicy: {Mode: ruleEnforce},
			},
		},
		{
			Dir: verificationStressDir,
			Rules: map[authoringRule]ruleDecision{
				rulePristineSeed: {Mode: ruleEnforce},
				ruleReference:    {Mode: ruleEnforce},
				ruleStopPolicy:   {Mode: ruleEnforce},
			},
		},
		{
			Dir: trainCorpusDir,
			Rules: map[authoringRule]ruleDecision{
				rulePristineSeed: {Mode: ruleEnforce},
				ruleReference:    {Mode: ruleEnforce},
				ruleStopPolicy:   {Mode: ruleEnforce},
			},
		},
		{
			Dir: memorybenchDir,
			Rules: map[authoringRule]ruleDecision{
				rulePristineSeed: {Mode: ruleEnforce},
				ruleReference: {Mode: ruleExempt,
					Reason: "memorybench ships task-local memory facts instead of solution/ fixtures; TestMemorybenchGradersAcceptTheReferenceAnswer supplies a task-derived witness for every task and checks every grader accepts it"},
				ruleStopPolicy: {Mode: ruleEnforce},
			},
		},
		{
			Dir: fanoutWidthDir,
			Rules: map[authoringRule]ruleDecision{
				rulePristineSeed: {Mode: ruleEnforce},
				ruleReference:    {Mode: ruleEnforce},
				ruleStopPolicy:   {Mode: ruleEnforce},
			},
		},
		{
			Dir: upstreamEdgeDir,
			Rules: map[authoringRule]ruleDecision{
				rulePristineSeed: {Mode: ruleEnforce},
				ruleReference:    {Mode: ruleEnforce},
				ruleStopPolicy:   {Mode: ruleEnforce},
			},
		},
		{
			Dir: projectCheckDir,
			Rules: map[authoringRule]ruleDecision{
				rulePristineSeed: {Mode: ruleEnforce},
				ruleReference:    {Mode: ruleEnforce},
				ruleStopPolicy:   {Mode: ruleEnforce},
			},
		},
	}
}

// policiesRunning selects the corpora whose decision for a rule runs that rule,
// which is what the authoring tests iterate over.
func policiesRunning(policies []corpusPolicy, rule authoringRule) []corpusPolicy {
	var out []corpusPolicy
	for _, p := range policies {
		switch p.Rules[rule].Mode {
		case ruleEnforce, ruleEnforceWhenPresent:
			out = append(out, p)
		}
	}
	return out
}

// committedCorpora lists the directories under root that carry tasks/.
func committedCorpora(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if dirExists(filepath.Join(root, e.Name(), "tasks")) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// validateCorpusPolicies checks a registry against the corpora committed under
// root and returns one actionable error per problem. Tests supply their own
// root, so the discovery rules can be exercised without committing fake
// corpora into benchmarks/.
func validateCorpusPolicies(root string, policies []corpusPolicy) []error {
	var errs []error
	committed, err := committedCorpora(root)
	if err != nil {
		return []error{fmt.Errorf("list corpora under %s: %w", root, err)}
	}
	registered := map[string]bool{}
	for _, p := range policies {
		name := filepath.Base(p.Dir)
		if registered[name] {
			errs = append(errs, fmt.Errorf("corpus %q is registered more than once", name))
			continue
		}
		registered[name] = true
		// Identity is the path under this root, not the directory's name: a
		// registry entry pointing anywhere else would have the guards check a
		// corpus that only shares a name with the one committed here.
		want := filepath.Clean(filepath.Join(root, name))
		if got := filepath.Clean(p.Dir); got != want {
			errs = append(errs, fmt.Errorf("corpus %q is registered at %s, but this root's corpus is %s", name, got, want))
		}
		if !dirExists(filepath.Join(p.Dir, "tasks")) {
			errs = append(errs, fmt.Errorf("corpus %q is registered but has no tasks/ directory at %s", name, p.Dir))
		}
		for rule := range p.Rules {
			known := false
			for _, r := range allRules {
				if rule == r {
					known = true
					break
				}
			}
			if !known {
				errs = append(errs, fmt.Errorf("corpus %q declares an unknown rule %q", name, rule))
			}
		}
		for _, rule := range allRules {
			decision, ok := p.Rules[rule]
			if !ok {
				errs = append(errs, fmt.Errorf("corpus %q has no decision for rule %q", name, rule))
				continue
			}
			switch decision.Mode {
			case ruleEnforce:
				// The full rule applies; no reason is needed or read.
			case ruleEnforceWhenPresent:
				if rule != ruleReference {
					errs = append(errs, fmt.Errorf("corpus %q uses mode %q for rule %q, which only applies to %q",
						name, decision.Mode, rule, ruleReference))
				}
				if strings.TrimSpace(decision.Reason) == "" {
					errs = append(errs, fmt.Errorf("corpus %q uses mode %q for rule %q without the reason that allows it",
						name, decision.Mode, rule))
				}
			case ruleExempt:
				if strings.TrimSpace(decision.Reason) == "" {
					errs = append(errs, fmt.Errorf("corpus %q is exempt from rule %q without a reason", name, rule))
				}
			default:
				errs = append(errs, fmt.Errorf("corpus %q declares an unknown mode %q for rule %q", name, decision.Mode, rule))
			}
		}
	}
	for _, name := range committed {
		if !registered[name] {
			errs = append(errs, fmt.Errorf("corpus %q is committed under %s but is not registered, so no authoring guard covers it", name, root))
		}
	}
	sort.Slice(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
	return errs
}

// taskBounds is a task's own declaration of its bounds. Pointers keep a missing
// key distinct from a declared zero.
type taskBounds struct {
	MaxSteps   *int `toml:"max_steps"`
	TimeoutSec *int `toml:"timeout_sec"`
}

// stopPolicyViolations reports the tasks in one corpus that break the stop
// rule. It reads each task.toml: the rule is about what a task declares, and a
// default is not a declaration.
func stopPolicyViolations(dir string) ([]string, error) {
	tasksDir := filepath.Join(dir, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		data, err := fileencoding.ReadFileUTF8(filepath.Join(tasksDir, id, "task.toml"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		var bounds taskBounds
		if _, err := toml.Decode(string(data), &bounds); err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		if bounds.MaxSteps != nil && *bounds.MaxSteps > 0 {
			out = append(out, fmt.Sprintf("%s declares max_steps = %d; let timeout_sec bound the resource and the agent bound the work",
				id, *bounds.MaxSteps))
		}
		switch {
		case bounds.TimeoutSec == nil:
			out = append(out, fmt.Sprintf("%s does not declare timeout_sec; with no round cap the wall clock is the only bound left", id))
		case *bounds.TimeoutSec <= 0:
			out = append(out, fmt.Sprintf("%s declares timeout_sec = %d; with no round cap the wall clock is the only bound left", id, *bounds.TimeoutSec))
		}
	}
	sort.Strings(out)
	return out, nil
}

// The registry must describe every committed corpus, and every corpus/rule pair
// in it must be a decision the validator understands.
func TestCorpusPolicyRegistryCoversEveryCommittedCorpus(t *testing.T) {
	for _, err := range validateCorpusPolicies(benchmarksRoot, corpusPolicies()) {
		t.Error(err)
	}
	committed, err := committedCorpora(benchmarksRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(committed) == 0 {
		t.Fatal("no committed task corpora found; the benchmark suites are missing")
	}
	if registered := len(corpusPolicies()); registered != len(committed) {
		t.Errorf("registry holds %d corpora while %d are committed", registered, len(committed))
	}
}

// The registry holds one decision per corpus and rule, so the enforcement sets
// cannot drift apart silently.
func TestCorpusPolicyEnforcementSets(t *testing.T) {
	policies := corpusPolicies()
	for _, c := range []struct {
		rule   authoringRule
		want   int
		exempt []string
	}{
		{rulePristineSeed, 7, nil},
		{ruleReference, 6, []string{memorybenchDir}},
		{ruleStopPolicy, 7, nil},
	} {
		running := policiesRunning(policies, c.rule)
		if len(running) != c.want {
			t.Errorf("rule %q runs for %d corpora, want %d", c.rule, len(running), c.want)
		}
		for _, dir := range c.exempt {
			for _, p := range running {
				if p.Dir == dir {
					t.Errorf("rule %q runs for %s, which the registry exempts", c.rule, filepath.Base(dir))
				}
			}
			found := false
			for _, p := range policies {
				if p.Dir == dir && p.Rules[c.rule].Mode == ruleExempt {
					found = true
				}
			}
			if !found {
				t.Errorf("%s is not recorded as exempt from rule %q", filepath.Base(dir), c.rule)
			}
		}
	}
}

// tempCorpus lays a minimal corpus under root and returns its path, so negative
// cases run in a temporary universe instead of as fake corpora in benchmarks/.
func tempCorpus(t *testing.T, root, name, task, taskToml string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	taskDir := filepath.Join(dir, "tasks", task)
	if err := os.MkdirAll(filepath.Join(taskDir, "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if taskToml != "" {
		if err := os.WriteFile(filepath.Join(taskDir, "task.toml"), []byte(taskToml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func fullDecisions() map[authoringRule]ruleDecision {
	return map[authoringRule]ruleDecision{
		rulePristineSeed: {Mode: ruleEnforce},
		ruleReference:    {Mode: ruleEnforce},
		ruleStopPolicy:   {Mode: ruleEnforce},
	}
}

// Every way a registry can fall out of step with the tree is reported against
// the corpus and rule it concerns.
func TestCorpusPolicyValidationCatchesGaps(t *testing.T) {
	universe := func(t *testing.T, corpora ...string) (string, []corpusPolicy) {
		t.Helper()
		root := t.TempDir()
		var policies []corpusPolicy
		for _, name := range corpora {
			dir := tempCorpus(t, root, name, "t-"+name, "")
			policies = append(policies, corpusPolicy{Dir: dir, Rules: fullDecisions()})
		}
		return root, policies
	}

	cases := []struct {
		name     string
		universe func(t *testing.T) (string, []corpusPolicy)
		want     string
	}{
		{
			name: "unregistered corpus",
			universe: func(t *testing.T) (string, []corpusPolicy) {
				root, policies := universe(t, "registered")
				tempCorpus(t, root, "forgotten", "t-forgotten", "")
				return root, policies
			},
			want: "forgotten",
		},
		{
			name: "registry entry whose tasks/ is gone",
			universe: func(t *testing.T) (string, []corpusPolicy) {
				root, policies := universe(t, "registered")
				if err := os.RemoveAll(filepath.Join(root, "registered")); err != nil {
					t.Fatal(err)
				}
				return root, policies
			},
			want: "no tasks/ directory",
		},
		{
			name: "corpus with no decision for a rule",
			universe: func(t *testing.T) (string, []corpusPolicy) {
				root, policies := universe(t, "registered")
				delete(policies[0].Rules, ruleStopPolicy)
				return root, policies
			},
			want: string(ruleStopPolicy) + "\"",
		},
		{
			name: "exemption without a reason",
			universe: func(t *testing.T) (string, []corpusPolicy) {
				root, policies := universe(t, "registered")
				policies[0].Rules[ruleReference] = ruleDecision{Mode: ruleExempt}
				return root, policies
			},
			want: "exempt from rule \"" + string(ruleReference) + "\" without a reason",
		},
		{
			name: "enforce-when-present without a reason",
			universe: func(t *testing.T) (string, []corpusPolicy) {
				root, policies := universe(t, "registered")
				policies[0].Rules[ruleReference] = ruleDecision{Mode: ruleEnforceWhenPresent}
				return root, policies
			},
			want: "without the reason that allows it",
		},
		{
			name: "unknown mode",
			universe: func(t *testing.T) (string, []corpusPolicy) {
				root, policies := universe(t, "registered")
				policies[0].Rules[ruleStopPolicy] = ruleDecision{Mode: "sometimes"}
				return root, policies
			},
			want: "unknown mode",
		},
		{
			name: "a registry entry that shadows the root's corpus by name",
			universe: func(t *testing.T) (string, []corpusPolicy) {
				root, policies := universe(t, "registered")
				elsewhere := tempCorpus(t, t.TempDir(), "registered", "t-impostor", "")
				policies[0].Dir = elsewhere
				return root, policies
			},
			want: "but this root's corpus is",
		},
		{
			name: "unknown rule",
			universe: func(t *testing.T) (string, []corpusPolicy) {
				root, policies := universe(t, "registered")
				policies[0].Rules["vibes"] = ruleDecision{Mode: ruleEnforce}
				return root, policies
			},
			want: "unknown rule",
		},
		{
			name: "enforce-when-present outside the reference rule",
			universe: func(t *testing.T) (string, []corpusPolicy) {
				root, policies := universe(t, "registered")
				policies[0].Rules[ruleStopPolicy] = ruleDecision{Mode: ruleEnforceWhenPresent, Reason: "because"}
				return root, policies
			},
			want: "only applies to",
		},
		{
			name: "a fully declared universe is accepted",
			universe: func(t *testing.T) (string, []corpusPolicy) {
				return universe(t, "one", "two")
			},
			want: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, policies := c.universe(t)
			errs := validateCorpusPolicies(root, policies)
			if c.want == "" {
				if len(errs) != 0 {
					t.Fatalf("valid registry reported %v", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatalf("no error reported, want one mentioning %q", c.want)
			}
			joined := ""
			for _, err := range errs {
				joined += err.Error() + "\n"
			}
			if !strings.Contains(joined, c.want) {
				t.Fatalf("errors do not mention %q:\n%s", c.want, joined)
			}
		})
	}
}

// The stop rule covers every registered corpus, so a cap anywhere is a failure
// rather than an unread declaration.
func TestStopPolicyAppliesToEveryRegisteredCorpus(t *testing.T) {
	for _, p := range policiesRunning(corpusPolicies(), ruleStopPolicy) {
		if p.Rules[ruleStopPolicy].Mode != ruleEnforce {
			t.Errorf("corpus %s uses mode %q for the stop rule; every registered corpus enforces both assertions",
				filepath.Base(p.Dir), p.Rules[ruleStopPolicy].Mode)
		}
	}

	root := t.TempDir()
	for _, c := range []struct {
		name   string
		corpus string
		toml   string
		want   []string
	}{
		{"a cap is rejected", "train", "max_steps = 8\ntimeout_sec = 300\n", []string{"max_steps = 8"}},
		{"an omitted wall clock is rejected", "upstream-edge", "max_steps = 0\n", []string{"does not declare timeout_sec"}},
		{"a zero wall clock is rejected", "fanout-width", "timeout_sec = 0\n", []string{"declares timeout_sec = 0"}},
		{"a negative wall clock is rejected", "project-check", "timeout_sec = -1\n", []string{"declares timeout_sec = -1"}},
		{"a declared bound is accepted", "memorybench", "timeout_sec = 300\n", nil},
		{"no cap and no declaration of one is accepted", "verification-stress", "max_steps = 0\ntimeout_sec = 300\n", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := tempCorpus(t, root, c.corpus, "t-case", c.toml)
			violations, err := stopPolicyViolations(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(c.want) == 0 {
				if len(violations) != 0 {
					t.Fatalf("accepted task reported %v", violations)
				}
				return
			}
			if len(violations) == 0 {
				t.Fatalf("no violation reported, want one mentioning %q", c.want[0])
			}
			if !strings.Contains(violations[0], c.want[0]) {
				t.Fatalf("violation %q does not mention %q", violations[0], c.want[0])
			}
		})
	}
}
