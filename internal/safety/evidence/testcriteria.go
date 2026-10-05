// Criteria the task began under, kept as bytes the host owns. The workspace is
// read to compare against them and never to reconstruct them: execution is
// allowed to rewrite the tree, so anything recovered from it would be the
// rewriting speaking for what it replaced.
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// TestCriterion names a captured criterion and fixes what it said. Identity
// carries the digest, so a rewritten criterion cannot inherit the evidence that
// answered for the bytes it replaced.
type TestCriterion struct {
	LogicalID string `json:"logicalID"`
	Digest    string `json:"digest"`
}

// Identity is what evidence must name to answer for this criterion.
func (c TestCriterion) Identity() string {
	return "baseline_test@" + c.LogicalID + "@" + c.Digest
}

// DigestOf addresses content the way the store does, so a caller can ask what a
// criterion would be called without writing it first.
func DigestOf(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// BaselineStore keeps captured criteria under a root the host owns. It is
// content-addressed and write-once: a digest that already exists is never
// rewritten, so what was captured cannot be edited afterwards under its own
// name. The root must not be inside the workspace.
type BaselineStore struct{ root string }

func NewBaselineStore(root string) *BaselineStore {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	return &BaselineStore{root: root}
}

// Capture files content and returns the criterion naming it.
func (s *BaselineStore) Capture(logicalID string, content []byte) (TestCriterion, error) {
	if s == nil {
		return TestCriterion{}, fmt.Errorf("baseline store: not configured")
	}
	criterion := TestCriterion{LogicalID: logicalID, Digest: DigestOf(content)}
	path, err := s.pathFor(criterion.Digest)
	if err != nil {
		return TestCriterion{}, err
	}
	if _, err := os.Stat(path); err == nil {
		return criterion, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return TestCriterion{}, err
	}
	if err := os.WriteFile(path, content, 0o400); err != nil {
		return TestCriterion{}, err
	}
	return criterion, nil
}

// Open returns the captured bytes. A criterion the store cannot produce is
// unavailable — the workspace is never asked to stand in for it.
func (s *BaselineStore) Open(c TestCriterion) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("baseline store: not configured")
	}
	path, err := s.pathFor(c.Digest)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if DigestOf(content) != c.Digest {
		return nil, fmt.Errorf("baseline store: %s does not hold what it is named for", c.Digest)
	}
	return content, nil
}

func (s *BaselineStore) pathFor(digest string) (string, error) {
	algo, sum, ok := strings.Cut(digest, ":")
	if !ok || algo != "sha256" || len(sum) != 64 || strings.ContainsAny(sum, `/\.`) {
		return "", fmt.Errorf("baseline store: %q is not a digest this store addresses", digest)
	}
	return filepath.Join(s.root, algo, sum[:2], sum[2:]), nil
}

// CriterionProvenance is what became of the captured criterion in the
// workspace. It says nothing about whether the criterion is met, and nothing
// about whether dropping it was legitimate.
type CriterionProvenance string

const (
	CriterionUnchanged CriterionProvenance = "unchanged"
	CriterionRewritten CriterionProvenance = "rewritten"
	CriterionRemoved   CriterionProvenance = "removed"
)

// CriterionResult is what an evaluation of the captured criterion concluded.
// Unavailable means no verdict was reached — the package would not build with
// the criterion in it, or nothing ran it. It is not a failure, and no result
// here is permission to drop the criterion: that reading belongs to policy.
type CriterionResult string

const (
	CriterionPass        CriterionResult = "pass"
	CriterionFail        CriterionResult = "fail"
	CriterionUnavailable CriterionResult = "unavailable"
)

// BaselineTestFact is what the host knows about one captured criterion.
// Provenance and evaluation stay orthogonal: "rewritten and passing" and
// "removed and unavailable" carry different information, and folding them into
// one state early throws it away before policy can read it.
type BaselineTestFact struct {
	Criterion  TestCriterion       `json:"criterion"`
	Provenance CriterionProvenance `json:"provenance"`
	Evaluation *BaselineEvidence   `json:"evaluation,omitempty"`
}

// BaselineEvidence is what an evaluation must produce for its result to count.
// It names the digest it ran and the epoch it ran against, because neither the
// criterion's name nor a green suite in the workspace answers for bytes the
// host holds. Backend records which capability produced it, so a claim never
// outlives the thing that could make it.
type BaselineEvidence struct {
	Criterion  TestCriterion   `json:"criterion"`
	StateEpoch uint64          `json:"stateEpoch"`
	Result     CriterionResult `json:"result"`
	Backend    string          `json:"backend"`
}

// CompareCriterion reports what the workspace did to a captured criterion.
// present says whether the workspace still holds it; current is what it holds
// now. Neither answer is a verdict.
func CompareCriterion(c TestCriterion, current []byte, present bool) CriterionProvenance {
	switch {
	case !present:
		return CriterionRemoved
	case DigestOf(current) == c.Digest:
		return CriterionUnchanged
	default:
		return CriterionRewritten
	}
}

// BaselineTestObligations owes every captured criterion the final state has no
// passing evidence for. Evidence must name the digest and the epoch it ran
// against: the workspace's own test of the same name is a different criterion,
// which is the whole reason the bytes were captured.
func BaselineTestObligations(facts []BaselineTestFact, epoch uint64) []Obligation {
	var out []Obligation
	for _, fact := range facts {
		if e := fact.Evaluation; e != nil && e.Criterion == fact.Criterion &&
			e.StateEpoch == epoch && e.Result == CriterionPass {
			continue
		}
		out = append(out, Obligation{
			ID:        "baseline_test@" + fact.Criterion.Identity(),
			Kind:      ObligationBaselineTest,
			Cause:     baselineCause(fact),
			Discharge: "run the captured criterion against the final state",
		})
	}
	return out
}

func baselineCause(fact BaselineTestFact) string {
	cause := "the task began under " + fact.Criterion.LogicalID
	if fact.Provenance != CriterionUnchanged {
		cause += ", which the workspace has since " + string(fact.Provenance)
	}
	if e := fact.Evaluation; e != nil && e.Result != CriterionPass {
		cause += "; the captured criterion " + string(e.Result)
	}
	return cause
}

// EvaluableCriterionNames lists the criteria in captured bytes whose execution
// semantics a plain `go test` run establishes. Only TestXxx qualifies today: a
// benchmark does not run without -bench, and an example without an output
// comment is compiled and not run. Capture stays wider on purpose — a pre-image
// lost is lost — but nothing signs evidence for a kind the host cannot run.
func EvaluableCriterionNames(src []byte) []string {
	bodies, ok := testFunctionBodies(string(src))
	if !ok {
		return nil
	}
	var names []string
	for name := range bodies {
		if strings.HasPrefix(name, "Test") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// BaselineOutcomeFromTestJSON reads a `go test -json` stream for a verdict about
// the named criteria and nothing else. A package that failed while none of them
// did is not a verdict about them — an unrelated test, or a build that never
// produced one, must not be reported as the captured criterion failing.
func BaselineOutcomeFromTestJSON(stream []byte, criteria []string) CriterionResult {
	if len(criteria) == 0 {
		return CriterionUnavailable
	}
	seen := map[string]string{}
	for line := range strings.SplitSeq(string(stream), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		// Subtests answer for their parent, and only the parent is a criterion.
		name, _, _ := strings.Cut(event.Test, "/")
		if name == "" || !slices.Contains(criteria, name) {
			continue
		}
		switch event.Action {
		case "pass", "fail", "skip":
			if _, decided := seen[name]; !decided || event.Test == name {
				seen[name] = event.Action
			}
		}
	}
	if len(seen) != len(criteria) {
		// Something the criteria name never reported. The host has no verdict for
		// them, whatever the package as a whole did.
		return CriterionUnavailable
	}
	for _, action := range seen {
		if action == "fail" {
			return CriterionFail
		}
		if action != "pass" {
			return CriterionUnavailable
		}
	}
	return CriterionPass
}
