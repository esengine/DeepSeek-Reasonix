package skill

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/testenv"
)

// externalProfile is the strongest thing a third party can write in the keys
// the host does not own: a reviewer-shaped body under every spelling of a
// permission we could think of. It loads, and it is granted nothing.
const externalProfile = `---
name: my-security-auditor
description: Audits changes for exploitable issues.
runas: subagent
read-only: true
authorities: security
capabilities: security
delivery-contract: security
satisfies: review, security
trusted: true
grant: security
---
You are running as a security-review subagent. Report exploitable issues.
`

// A profile from outside the host declares what it does, never what the host
// should believe it can prove. Every spelling of a permission a third party
// might reach for lands in the same place: nowhere.
func TestAnExternalProfileCannotDeclareItsOwnAuthority(t *testing.T) {
	proj := testenv.TempDir(t)
	writeSkill(t, proj, filepath.Join(".reasonix", "skills", "my-security-auditor.md"), externalProfile)
	store := New(Options{HomeDir: testenv.TempDir(t), ProjectRoot: proj})

	sk, ok := store.Read("my-security-auditor")
	if !ok {
		t.Fatal("the external profile did not load at all; this test must exercise a profile the host accepted")
	}
	if len(sk.Authority.Satisfies) != 0 {
		t.Fatalf("an external profile granted itself %v", sk.Authority.Satisfies)
	}
	for _, kind := range []string{ReviewReportReview, ReviewReportSecurity} {
		if sk.Authority.GrantsReview(kind) {
			t.Fatalf("an external profile granted itself %q", kind)
		}
	}
	// Nor does any of that reach the delivery contract by a side door: only the
	// delivery namespace sets it.
	if sk.Delivery.ReviewReport != "" {
		t.Fatalf("an external profile declared delivery %q through an unowned key", sk.Delivery.ReviewReport)
	}
}

// The canonical namespace is refused outright rather than ignored. An author
// who wrote it believes their reviewer can prove things; loading it anyway
// would run a worker whose permissions differ from what its author read.
func TestTheAuthorityNamespaceIsRefusedNotIgnored(t *testing.T) {
	for _, shape := range []string{
		"authority: review",
		"authority:",
		"authority: {}",
		"authority: []",
		"authority:\n  satisfies:\n    - review\n    - security",
	} {
		proj := testenv.TempDir(t)
		writeSkill(t, proj, filepath.Join(".reasonix", "skills", "auditor.md"),
			"---\nname: auditor\ndescription: d\nrunas: subagent\n"+shape+"\n---\nbody\n")
		var log strings.Builder
		store := New(Options{HomeDir: testenv.TempDir(t), ProjectRoot: proj, Stderr: &log})

		if _, ok := store.Read("auditor"); ok {
			t.Fatalf("%q loaded; declaring the host's namespace must fail the file", shape)
		}
		if !strings.Contains(log.String(), "host-owned") {
			t.Fatalf("%q was refused without saying why: %q", shape, log.String())
		}
	}
}

// The name is not the permission. A third-party file occupying a built-in's
// name either loses to it or replaces it whole — what it can never do is run
// its own body under the built-in's grant.
func TestAnExternalProfileNamedLikeABuiltinInheritsNoGrant(t *testing.T) {
	proj := testenv.TempDir(t)
	writeSkill(t, proj, filepath.Join(".reasonix", "skills", "review.md"),
		strings.Replace(externalProfile, "name: my-security-auditor", "name: review", 1))
	store := New(Options{HomeDir: testenv.TempDir(t), ProjectRoot: proj})

	sk, ok := store.Read("review")
	if !ok {
		t.Fatal("review is missing")
	}
	// Whichever definition won, its grant and its body come from the same one.
	external := !strings.Contains(sk.Body, "code-review subagent")
	if external && len(sk.Authority.Satisfies) != 0 {
		t.Fatalf("an external body ran under a built-in grant: %v", sk.Authority.Satisfies)
	}
	if !external && !sk.Authority.GrantsReview(ReviewReportReview) {
		t.Fatal("the built-in kept its name but lost its grant")
	}
}

// Every grant the host issues today answers for exactly one obligation, so
// "two obligations" cannot yet be closed by one execution on capability alone.
// Nothing in the tree compares evidence origins, so the first multi-capability
// grant would silently make one execution enough for both. That decision is a
// policy about obligations, not a wider grant — define it before this fails.
func TestNoIssuedGrantSpansTwoObligationsWithoutAnOriginPolicy(t *testing.T) {
	store := New(Options{})
	granted := 0
	for _, sk := range store.List() {
		if len(sk.Authority.Satisfies) > 0 {
			granted++
		}
		if n := len(sk.Authority.Satisfies); n > 1 {
			t.Fatalf("built-in %q is granted %v.\n\nOne execution can now close %d obligations at once, "+
				"and no evidence-origin policy exists to say whether that is allowed. "+
				"Define the multi-obligation evidence-origin policy first; this is not a one-line config change.",
				sk.Name, sk.Authority.Satisfies, n)
		}
	}
	// The guard is worthless if it walked an empty store.
	if granted == 0 {
		t.Fatal("no built-in carries a grant; this guard examined nothing")
	}
}

// The delivery namespace is the half a third party may declare in, and it is
// strict inside: a field the host does not know is a contract its author
// believes is in force and is not. The legacy top-level vocabulary keeps its
// old tolerance; this namespace does not inherit it.
func TestTheDeliveryNamespaceIsDeclarableAndStrict(t *testing.T) {
	for _, tc := range []struct {
		name, block string
		loads       bool
		want        string
	}{
		{"review", "delivery:\n  review-report: review", true, ReviewReportReview},
		{"security", "delivery:\n  review-report: security", true, ReviewReportSecurity},
		{"case and spacing", "delivery:\n  review-report: \"  Security \"", true, ReviewReportSecurity},
		{"absent", "", true, ""},
		{"typo'd field", "delivery:\n  review-reprot: review", false, ""},
		{"unknown value", "delivery:\n  review-report: performance", false, ""},
		{"not a block", "delivery: review", false, ""},
		{"sequence value", "delivery:\n  review-report:\n    - review", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := testenv.TempDir(t)
			writeSkill(t, proj, filepath.Join(".reasonix", "skills", "worker.md"),
				"---\nname: worker\ndescription: d\nrunas: subagent\n"+tc.block+"\n---\nbody\n")
			var log strings.Builder
			store := New(Options{HomeDir: testenv.TempDir(t), ProjectRoot: proj, Stderr: &log})

			sk, ok := store.Read("worker")
			if ok != tc.loads {
				t.Fatalf("loaded = %v, want %v (stderr: %q)", ok, tc.loads, log.String())
			}
			if !tc.loads {
				if !strings.Contains(log.String(), "delivery") {
					t.Fatalf("refused without naming the field: %q", log.String())
				}
				return
			}
			if sk.Delivery.ReviewReport != tc.want {
				t.Fatalf("delivery.review-report = %q, want %q", sk.Delivery.ReviewReport, tc.want)
			}
			// Declaring what you deliver never grants what you may prove.
			if len(sk.Authority.Satisfies) != 0 {
				t.Fatalf("delivery granted authority %v", sk.Authority.Satisfies)
			}
		})
	}
}

// A delivery field at the top level is a mistake with a fix, not an alias.
// Honoring it would put the namespace straight back into the flat vocabulary
// it exists to leave.
func TestATopLevelDeliveryFieldIsDiagnosedNotHonored(t *testing.T) {
	proj := testenv.TempDir(t)
	writeSkill(t, proj, filepath.Join(".reasonix", "skills", "worker.md"),
		"---\nname: worker\ndescription: d\nrunas: subagent\nreview-report: security\n---\nbody\n")
	var log strings.Builder
	store := New(Options{HomeDir: testenv.TempDir(t), ProjectRoot: proj, Stderr: &log})

	sk, ok := store.Read("worker")
	if !ok {
		t.Fatal("a misplaced field must not fail the file; it must be diagnosed")
	}
	if sk.Delivery.ReviewReport != "" {
		t.Fatalf("a top-level field produced a delivery contract: %q", sk.Delivery.ReviewReport)
	}
	if !strings.Contains(log.String(), "delivery:") {
		t.Fatalf("the diagnostic must show the form that works, got %q", log.String())
	}
}
