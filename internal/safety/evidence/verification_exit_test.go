package evidence

import (
	"encoding/json"
	"testing"
)

func TestVerificationExitConclusive(t *testing.T) {
	cases := []struct {
		command    string
		conclusive bool
	}{
		{"go test ./...", true},
		{"go test ./... 2>&1", true},
		{"cd /w && go vet ./... && go test ./...", true},
		// `head` reports the status, and it succeeds on a failing suite.
		{"go test ./... 2>&1 | head -100", false},
		// `git status` reports the status, and it fails outside a repository.
		{"go test ./... | tail -20 && git diff --stat; git status --short", false},
		{"go test ./... || echo done", false},
		{"go test ./... &", false},
		// The suite reports the status even though a `;` precedes it.
		{`git status --short 2>/dev/null || echo "no repo"; go test ./... 2>&1`, true},
		// A trailing reporter takes the status back over.
		{"go test ./...; echo $?", false},
		// `&&` short-circuits, so a zero status still means the suite passed.
		{"go test ./... && echo done", true},
		// The suite is still short-circuited even when a later stage is piped.
		{"go test ./... && go vet ./... 2>&1 | head -20", true},
		{"go test ./... || go test ./pkg", false},
	}
	for _, tc := range cases {
		if got := VerificationExitConclusive(tc.command); got != tc.conclusive {
			t.Errorf("VerificationExitConclusive(%q) = %v, want %v", tc.command, got, tc.conclusive)
		}
	}
}

// The wrapper here is plumbing: a redirect, and stages after the check that
// only carry its output. Where the check ran is not wrapper — see
// TestVerificationIdentityKeepsExecutionContext.
func TestVerificationIdentityIgnoresTheWrapper(t *testing.T) {
	same := []string{
		"go test ./...",
		"go test ./... 2>&1",
		"go test ./... 2>&1 | head -100",
		"go test ./... | tail -20 | grep -v vendor",
	}
	want := VerificationIdentity(same[0])
	for _, command := range same[1:] {
		if got := VerificationIdentity(command); got != want {
			t.Errorf("VerificationIdentity(%q) = %q, want %q", command, got, want)
		}
	}
}

func TestVerificationIdentitySeparatesDifferentChecks(t *testing.T) {
	if VerificationIdentity("go test ./...") == VerificationIdentity("go vet ./...") {
		t.Fatal("VerificationIdentity: distinct checks must not share an identity")
	}
}

// A command no static pass can decompose keeps its own text, so two unrelated
// unparseable commands never merge into one item.
func TestVerificationIdentityFallsBackToTheCommand(t *testing.T) {
	command := "go test ./... $(cat"
	if got := VerificationIdentity(command); got != command {
		t.Fatalf("VerificationIdentity(%q) = %q, want the command itself", command, got)
	}
}

// The post-change verification floor must not be satisfied by a suite whose
// verdict the shell hid: that is exactly the shape that exits 0 while red.
func TestVerificationFloorRejectsAnUnreadableRun(t *testing.T) {
	piped := NewLedger()
	piped.Record(Receipt{
		ToolName:     "bash",
		Command:      "go test ./... | head -100",
		Success:      true,
		Verification: VerificationInconclusive,
	})
	if piped.HasSuccessfulVerificationCommandAfter(-1) {
		t.Fatal("a piped suite satisfied the verification floor; its exit status proves nothing")
	}

	clean := NewLedger()
	clean.Record(Receipt{
		ToolName:     "bash",
		Command:      "go test ./...",
		Success:      true,
		Verification: VerificationPassed,
	})
	if !clean.HasSuccessfulVerificationCommandAfter(-1) {
		t.Fatal("a readable passing run did not satisfy the verification floor")
	}
}

// A check is not discarded for its company: the host cannot prove `sed` leaves
// the tree alone — it writes through a script language nothing here parses —
// but `&&` still proves the suite that ran before it passed. Delivery sign-off
// keeps asking the stricter question.
func TestVerificationIsNotDiscardedForItsCompany(t *testing.T) {
	const mixed = "go test ./... && go vet ./... && sed -n '1p' out.txt"
	if !CommandRunsVerification(mixed) {
		t.Fatal("a suite in a chain with an unclassified command stopped counting as verification")
	}
	if !VerificationExitConclusive(mixed) {
		t.Fatal("an && chain hides no failure, so the suite's verdict is readable")
	}
	if IsDeliveryVerificationCommand(mixed) {
		t.Fatal("delivery sign-off must keep rejecting a command it cannot prove read-only")
	}
}

// The company must not launder an unreadable verdict either: here the suite is
// piped and a later stage owns the status, so nothing is proven.
func TestCompanyDoesNotLaunderAPipedVerdict(t *testing.T) {
	const laundered = "go test ./... | head -50 && gofmt -l ."
	if !CommandRunsVerification(laundered) {
		t.Fatal("the command still runs a verification and must carry a verdict")
	}
	if VerificationExitConclusive(laundered) {
		t.Fatal("a piped suite proves nothing even when a later stage succeeds")
	}
}

// Receipts from before host classification existed keep their old meaning.
func TestVerificationFloorKeepsUnclassifiedReceipts(t *testing.T) {
	ledger := NewLedger()
	ledger.Record(Receipt{ToolName: "bash", Command: "go test ./...", Success: true})
	if !ledger.HasSuccessfulVerificationCommandAfter(-1) {
		t.Fatal("an unclassified successful run must keep counting as verification")
	}
}

// A substring scan over the raw argument JSON accepts every case below;
// the classifier parses the command and accepts only the first.
func TestIsVerificationToolCallReadsCommandNotSubstrings(t *testing.T) {
	cases := []struct {
		name string
		args string
		want bool
	}{
		{"runs the verifier", `{"command":"go test ./..."}`, true},
		{"reads a file named after one", `{"command":"cat notes-about-go-test.md"}`, false},
		{"prints advice about one", `{"command":"echo 'npm test is what you should run'"}`, false},
		{"searches for one", `{"command":"grep -rn 'pytest' ."}`, false},
		{"non-bash tool", `{"command":"go test ./..."}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := "bash"
			if tc.name == "non-bash tool" {
				tool = "read_file"
			}
			if got := IsVerificationToolCall(tool, json.RawMessage(tc.args)); got != tc.want {
				t.Fatalf("IsVerificationToolCall(%q, %s) = %v, want %v", tool, tc.args, got, tc.want)
			}
		})
	}
}

// Writing a file with a here-doc and running the checks in the same call is the
// ordinary shape of a turn's last step. It used to read as no verification at
// all, so the turn that ran its tests was reported as having skipped them.
func TestVerificationSurvivesAHereDocBesideIt(t *testing.T) {
	cases := []struct {
		name           string
		command        string
		wantRuns       bool
		wantConclusive bool
	}{
		{
			name:           "checks after a written file",
			command:        "cat > a.txt <<'EOF'\nx\nEOF\ngo test ./...",
			wantRuns:       true,
			wantConclusive: true,
		},
		{
			name:           "the here-doc decides the status",
			command:        "go test ./...\ncat > a.txt <<'EOF'\nx\nEOF",
			wantRuns:       true,
			wantConclusive: false,
		},
		{
			name:           "a body alone verifies nothing",
			command:        "cat > a.txt <<'EOF'\ngo test ./...\nEOF",
			wantRuns:       false,
			wantConclusive: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CommandRunsVerification(tc.command); got != tc.wantRuns {
				t.Fatalf("CommandRunsVerification = %v, want %v", got, tc.wantRuns)
			}
			if got := VerificationExitConclusive(tc.command); got != tc.wantConclusive {
				t.Fatalf("VerificationExitConclusive = %v, want %v", got, tc.wantConclusive)
			}
		})
	}
}

// A check's identity is the verifier it ran, so the same check folds with
// itself across calls. Reading a here-doc command's whole text instead would
// key it on the body — two runs of one check would never fold together.
func TestVerificationIdentityIgnoresAHereDocBody(t *testing.T) {
	first := "cat > fixture.json <<'EOF'\n{\"a\":1}\nEOF\ngo test ./internal/cache/"
	second := "cat > fixture.json <<'EOF'\n{\"a\":2}\nEOF\ngo test ./internal/cache/"
	if VerificationIdentity(first) != VerificationIdentity(second) {
		t.Fatalf("identities differ: %q vs %q", VerificationIdentity(first), VerificationIdentity(second))
	}
	if got, want := VerificationIdentity(first), VerificationIdentity("go test ./internal/cache/"); got != want {
		t.Fatalf("identity = %q, want the bare check %q", got, want)
	}
}

// Delivery blocks inline interpreter source because the host cannot audit it.
// A here-doc feeding the same interpreter is the same program arriving the same
// way, so the block has to see it too — shellparse's own note on StdinHereDoc
// says a here-doc into an interpreter is -c by another spelling.
func TestOpaqueInterpreterSeesAHereDocFedProgram(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    bool
	}{
		{"python -c", `python -c "print(1)"`, true},
		{"python fed by a here-doc", "python - <<'PY'\nprint(1)\nPY", true},
		{"node fed by a here-doc", "node <<'JS'\nconsole.log(1)\nJS", true},
		{"here-doc after a cd", "cd work && python - <<'PY'\nprint(1)\nPY", true},
		{"a here-doc into a file is not a program", "cat > notes.md <<'EOF'\nhello\nEOF", false},
		// A script operand names a file the host can read, so the body is that
		// script's input and not its source. Blocking it would stop a turn over
		// something this gate can already account for.
		{"a here-doc as data for a script", "python script.py <<'IN'\n1\nIN", false},
		{"a module operand is a program too", "python -m tool <<'IN'\n1\nIN", false},
		{"an explicit stdin marker still hands over the program", "python - <<'PY'\nprint(1)\nPY", true},
		{"a shell told to read stdin hands over the program", "sh -s <<'SH'\necho 1\nSH", true},
		{"no here-doc at all", "go test ./...", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"command": tc.command})
			if err != nil {
				t.Fatal(err)
			}
			if got := BashToolCallUsesOpaqueInlineInterpreter(args); got != tc.want {
				t.Fatalf("BashToolCallUsesOpaqueInlineInterpreter = %v, want %v", got, tc.want)
			}
		})
	}
}

// Canonicalisation removes spelling and plumbing, never context. A check run
// somewhere else, or under a different environment, is a different check:
// folding them would let a green run at the root clear a red one in a
// subdirectory, and let a baseline criterion be settled by a check it never
// named.
func TestVerificationIdentityKeepsExecutionContext(t *testing.T) {
	root := VerificationIdentity("go test ./...")

	distinct := []string{
		"cd backend && go test ./...",
		"cd backend && cd frontend && go test ./...",
		"GOFLAGS=-race go test ./...",
		"go test ./... -race",
	}
	for _, command := range distinct {
		if got := VerificationIdentity(command); got == root {
			t.Errorf("VerificationIdentity(%q) = %q, want a distinct criterion", command, got)
		}
	}

	// What sits after the check is where its output went, which cannot change
	// what it already proved.
	same := []string{"go test ./... 2>&1 | tail -10", "go test ./... | head -30", "  go   test ./...  "}
	for _, command := range same {
		if got := VerificationIdentity(command); got != root {
			t.Errorf("VerificationIdentity(%q) = %q, want %q", command, got, root)
		}
	}
}

// Identity and verdict answer different questions. Folding a check with its own
// output plumbing is right; concluding from the plumbing's exit status that the
// check passed is not. The three below keep the two apart.

func TestTrailingPlumbingDoesNotChangeVerificationIdentity(t *testing.T) {
	want := VerificationIdentity("go test ./...")
	for _, command := range []string{"go test ./... 2>&1 | tail -10", "go test ./... | head -30"} {
		if got := VerificationIdentity(command); got != want {
			t.Fatalf("VerificationIdentity(%q) = %q, want %q", command, got, want)
		}
	}
}

// Without pipefail the suite can be red while the pipeline exits 0, so a zero
// the host cannot attribute to the check proves nothing either way.
func TestAZeroExitFromTrailingPlumbingDoesNotProveAFailedVerifier(t *testing.T) {
	for _, command := range []string{
		"go test ./... 2>&1 | tail -10",
		"go test ./... || true",
		"go test ./...; exit 0",
	} {
		outcome := VerificationOutcome(Receipt{ToolName: "bash", Command: command, Success: true})
		if outcome != "" {
			t.Errorf("VerificationOutcome(%q, exit 0) = %q, want no verdict", command, outcome)
		}
	}
	// The same command whose status the host could read still answers.
	direct := VerificationOutcome(Receipt{ToolName: "bash", Command: "go test ./...", Success: true})
	if direct != VerificationPassed {
		t.Fatalf("VerificationOutcome(direct run) = %q, want passed", direct)
	}
}

// The sharp one: outcomes fold per identity, so an aggregate exit the host
// cannot attribute must not be allowed to settle a failure recorded under that
// same identity.
func TestAFailedVerifierCannotBeClearedByTheSameIdentityWithAnUntrustedAggregateExit(t *testing.T) {
	failed := Receipt{ToolName: "bash", Command: "go test ./...", Success: false, Verification: VerificationFailed}

	laundered := NewLedger()
	laundered.Record(failed)
	laundered.Record(Receipt{ToolName: "bash", Command: "go test ./... 2>&1 | tail -10", Success: true})
	if !laundered.HasFailedVerificationAfter(-1) {
		t.Fatal("a pipeline's exit status cleared the failure it was hiding")
	}

	// A real re-run the host can read still clears it — the fold is not broken,
	// only the laundering is.
	rerun := NewLedger()
	rerun.Record(failed)
	rerun.Record(Receipt{ToolName: "bash", Command: "go test ./...", Success: true, Verification: VerificationPassed})
	if rerun.HasFailedVerificationAfter(-1) {
		t.Fatal("re-running the check until it passes no longer clears it")
	}
}
