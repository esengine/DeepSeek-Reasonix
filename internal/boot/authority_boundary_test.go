package boot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/skill"
	"reasonix/internal/testenv"
)

// Delivering a report and being believed by it are separate permissions. This
// is the shape a third-party reviewer takes: it owes a typed verdict, its
// findings reach the parent, and it closes nothing. The declaration path that
// lets an external file ask for the delivery half does not exist yet; this
// holds the contract so it cannot arrive carrying the other half.
func TestDeliveryWithoutAuthorityReportsButProvesNothing(t *testing.T) {
	external := skill.Skill{
		Name: "my-security-auditor", Description: "third-party auditor",
		Body: "You audit changes for exploitable issues.", RunAs: skill.RunSubagent, ReadOnly: true,
		Delivery: skill.DeliveryContract{ReviewReport: skill.ReviewReportSecurity},
	}
	builtin, ok := skill.New(skill.Options{}).Read("security-review")
	if !ok {
		t.Fatal("built-in security-review is missing")
	}
	builtin.AllowedTools = nil

	for _, tc := range []struct {
		name   string
		sk     skill.Skill
		proves bool
	}{
		{"external, delivery only", external, false},
		{"built-in, delivery and grant", builtin, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := runProfileForReport(t, tc.sk, "pass")

			var report *evidence.Receipt
			for _, r := range parent.Summary().Receipts {
				if r.ToolName == "review_report" && r.Success {
					report = &r
				}
			}
			if report == nil {
				t.Fatal("the worker owed a typed report and none reached the parent")
			}
			// Delivery holds on both: the report was produced and accepted.
			if report.ReportKind != evidence.ReviewKindSecurity {
				t.Fatalf("report kind = %q", report.ReportKind)
			}
			if got := evidence.ReceiptProves(*report, evidence.ReviewKindSecurity); got != tc.proves {
				t.Fatalf("proves security = %v, want %v (authority=%v)", got, tc.proves, report.ReviewAuthority)
			}
			// Neither may reach the obligation it was never granted.
			if evidence.ReceiptProves(*report, evidence.ReviewKindReview) {
				t.Fatal("a security worker proved the review obligation")
			}
			sec, _, _ := parent.HasStructuredReviewAfter(evidence.ReviewKindSecurity, 0, []string{"a.go"})
			if sec != tc.proves {
				t.Fatalf("closes the security obligation = %v, want %v", sec, tc.proves)
			}
		})
	}
}

// The half a third party keeps: it cannot certify, but it can refuse. A block
// is a verdict about what the reviewer did look at, and delivery honors it
// without asking who was authorized to say it.
func TestAnUngrantedWorkerCanStillBlockDelivery(t *testing.T) {
	external := skill.Skill{
		Name: "my-security-auditor", Description: "third-party auditor",
		Body: "You audit changes.", RunAs: skill.RunSubagent, ReadOnly: true,
		Delivery: skill.DeliveryContract{ReviewReport: skill.ReviewReportSecurity},
	}
	parent := runProfileForReport(t, external, "block")
	if _, ok := parent.BlockingReviewAfter(0); !ok {
		t.Fatal("an ungranted worker's block must still reach delivery")
	}
	if got := parent.ReviewProofGapFor(evidence.ReviewKindSecurity); got != evidence.ReviewProofUngranted {
		t.Fatalf("proof gap = %v, want ungranted so the host can say why it did not count", got)
	}
}

func runProfileForReport(t *testing.T, sk skill.Skill, verdict string) *evidence.Ledger {
	t.Helper()
	return runProfileForReportKind(t, sk, verdict, "security")
}

func runProfileForReportKind(t *testing.T, sk skill.Skill, verdict, kind string) *evidence.Ledger {
	t.Helper()
	w := newReviewWorldWith(t, &reportingProvider{verdict: verdict, kind: kind}, sk)
	parent := evidence.NewLedger()
	parent.Record(evidence.ReceiptFromToolCall("edit_file",
		json.RawMessage(`{"path":"a.go"}`), true, evidence.ToolFacts{}))
	ctx := evidence.WithLedger(w.ctx, parent)
	args, _ := json.Marshal(map[string]any{"prompt": "audit it", "profile": sk.Name})
	if _, err := w.tasks.Execute(ctx, json.RawMessage(args)); err != nil {
		t.Fatalf("run %s: %v", sk.Name, err)
	}
	return parent
}

// Taking a built-in's name takes its job, not its standing. A workspace that
// ships its own review.md wins the name — and the reviewer that runs there can
// no longer close the review obligation, because the grant stayed with the
// definition the host wrote. Fail-closed, and the host says which of the two
// happened rather than repeating a demand the turn believes it met.
func TestShadowingABuiltinReviewerLosesItsGrant(t *testing.T) {
	proj := testenv.TempDir(t)
	dir := filepath.Join(proj, ".reasonix", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review.md"), []byte(
		"---\nname: review\ndescription: our own reviewer\nrunas: subagent\nread-only: true\ndelivery:\n  review-report: review\n---\nYou review our way.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := skill.New(skill.Options{HomeDir: testenv.TempDir(t), ProjectRoot: proj})
	shadowed, ok := store.Read("review")
	if !ok {
		t.Fatal("review is missing")
	}
	if shadowed.Scope != skill.ScopeProject {
		t.Skipf("the workspace copy did not win the name (scope=%s); shadowing is what this test is about", shadowed.Scope)
	}

	if shadowed.Delivery.ReviewReport != skill.ReviewReportReview {
		t.Fatalf("the workspace copy declared delivery %q", shadowed.Delivery.ReviewReport)
	}
	if len(shadowed.Authority.Satisfies) != 0 {
		t.Fatalf("a shadowing profile inherited the built-in grant: %v", shadowed.Authority.Satisfies)
	}

	parent := runProfileForReportKind(t, shadowed, "pass", "review")
	var report *evidence.Receipt
	for _, r := range parent.Summary().Receipts {
		if r.ToolName == "review_report" && r.Success {
			report = &r
		}
	}
	if report == nil {
		t.Fatal("it owes a report and delivers one; only the proof is withheld")
	}
	if evidence.ReceiptProves(*report, evidence.ReviewKindReview) {
		t.Fatalf("a shadowing profile proved the review obligation: %v", report.ReviewAuthority)
	}
	rev, _, _ := parent.HasStructuredReviewAfter(evidence.ReviewKindReview, 0, []string{"a.go"})
	if rev {
		t.Fatal("a shadowing profile closed the review obligation")
	}
	if got := parent.ReviewProofGapFor(evidence.ReviewKindReview); got != evidence.ReviewProofUngranted {
		t.Fatalf("proof gap = %v, want ungranted so the host can say why the report did not count", got)
	}
}
