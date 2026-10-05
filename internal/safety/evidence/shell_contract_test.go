package evidence

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBashToolCallUsesNonTerminalInlineInterpreter(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{command: `python3 -c 'open("x","w").write("y")' ; node verify_frontend_logic.js`, want: true},
		{command: `node -e 'console.log(1)' || go test ./...`, want: true},
		{command: `node -e 'console.log(1)' | tee out.txt`, want: true},
		// `&&` short-circuits: a failing interpreter is still the call's exit
		// status, so nothing is hidden and the shape stays allowed.
		{command: `node -e 'console.log(1)' && go test ./...`, want: false},
		{command: `python3 -c 'print(1)'`, want: false},
		{command: `go test ./...`, want: false},
		{command: `node --check app.js`, want: false},
	}
	for _, tt := range tests {
		args, err := json.Marshal(map[string]string{"command": tt.command})
		if err != nil {
			t.Fatal(err)
		}
		if got := BashToolCallUsesNonTerminalInlineInterpreter(args); got != tt.want {
			t.Errorf("%q => %v, want %v", tt.command, got, tt.want)
		}
	}
}

func TestOrdinaryModeShellContractClassifiers(t *testing.T) {
	// deliveryMixed is the broad receipt-integrity classifier Delivery keeps;
	// ordinaryMixed additionally requires that the earlier failure can be hidden.
	cases := []struct {
		command       string
		deliveryMixed bool
		ordinaryMixed bool
		mask          bool
		inline        bool
	}{
		// Arbitrary node scripts are not host-recognized verifiers; the
		// non-terminal inline interpreter rule still blocks this shape.
		{command: `python3 -c 'open("/tmp/x","w").write("x")' ; node verify_frontend_logic.js`, inline: true},
		// `;` lets the verifier's status stand in for the generate step's.
		{command: `go generate ./... ; go test ./...`, deliveryMixed: true, ordinaryMixed: true},
		// `&&` reports the failing step, so ordinary mode has nothing to protect.
		{command: `go generate ./... && go test ./...`, deliveryMixed: true},
		{command: `go build ./... && go test ./...`, deliveryMixed: true},
		{command: `npm install && npm test`, deliveryMixed: true},
		{command: `cargo build && cargo clippy`, deliveryMixed: true},
		// Masked exit is also mixed (echo is not a verifier); agent checks mask first.
		{command: `go test ./...; echo $?`, deliveryMixed: true, ordinaryMixed: true, mask: true},
		{command: `python3 -c 'print(1)'`},
		{command: `go test ./...`},
		{command: `tail -n +1 file | node --check -`},
	}
	for _, tt := range cases {
		args, _ := json.Marshal(map[string]string{"command": tt.command})
		if got := BashToolCallMixesMutationAndVerification(args); got != tt.deliveryMixed {
			t.Errorf("deliveryMixed(%q)=%v want %v", tt.command, got, tt.deliveryMixed)
		}
		if got := BashToolCallMixesMutationAndMaskableVerification(args); got != tt.ordinaryMixed {
			t.Errorf("ordinaryMixed(%q)=%v want %v", tt.command, got, tt.ordinaryMixed)
		}
		if got := BashToolCallMasksVerificationExit(args); got != tt.mask {
			t.Errorf("mask(%q)=%v want %v", tt.command, got, tt.mask)
		}
		if got := BashToolCallUsesNonTerminalInlineInterpreter(args); got != tt.inline {
			t.Errorf("inlineNT(%q)=%v want %v", tt.command, got, tt.inline)
		}
	}
}

// TestOrdinaryModeAllowsShortCircuitBuildAndVerify pins the regression that
// motivated the narrow ordinary-mode classifier: the everyday
// "build, then verify" chain must stay runnable outside Delivery.
func TestOrdinaryModeAllowsShortCircuitBuildAndVerify(t *testing.T) {
	allowed := []string{
		`go build ./... && go test ./...`,
		`npm install && npm test`,
		`pnpm install && pnpm test`,
		`cargo build && cargo test`,
		`make build && make test`,
		`git pull && go test ./...`,
		`mkdir -p out && go test ./...`,
		`go mod tidy && go test ./...`,
	}
	for _, command := range allowed {
		args, _ := json.Marshal(map[string]string{"command": command})
		if BashToolCallMixesMutationAndMaskableVerification(args) {
			t.Errorf("ordinary mode must allow %q: bash reports the failing step's status", command)
		}
		if !BashToolCallMixesMutationAndVerification(args) {
			t.Errorf("delivery mode should still classify %q as mixed", command)
		}
	}
}

// A way out the block names has to survive being followed. Told only to "chain
// with '&&'", a run writes `python3 -c "…" && echo found || echo missing` and
// earns the same block again, because the '||' hands the status on exactly like
// the ';' did. So the block names the segment that took the exit status, and
// says which separators do that.
func TestInlineNonTerminalBlockNamesTheSegmentThatTookTheStatus(t *testing.T) {
	args, err := json.Marshal(map[string]string{
		"command": `python3 -c "import acmeconfig" 2>&1; pip3 show acmeconfig | head -5`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !BashToolCallUsesNonTerminalInlineInterpreter(args) {
		t.Fatal("this shape must still be blocked; the message is what changed")
	}
	msg := ShellContractInlineNonTerminalMessage(args)
	for _, want := range []string{
		`python3 -c "import acmeconfig" 2>&1`,
		"pip3 show acmeconfig",
		"';' and '||'",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("block should name %q, got: %s", want, msg)
		}
	}
}

// An unparseable or single-segment command has no pair to name; the block must
// still say something rather than quote an empty segment back.
func TestInlineNonTerminalBlockFallsBackWhenNothingToName(t *testing.T) {
	args, err := json.Marshal(map[string]string{"command": `python3 -c "import acmeconfig"`})
	if err != nil {
		t.Fatal(err)
	}
	msg := ShellContractInlineNonTerminalMessage(args)
	if msg != ShellContractPreflightMessage("inline_nonterminal") {
		t.Fatalf("message = %q, want the generic block when there is no follower to name", msg)
	}
}
