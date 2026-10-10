package permission

import (
	"encoding/json"
	"errors"
	"strings"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/contract/planmode"
	"reasonix/internal/safety/shellsafe"
)

// BashCommandIsReadOnly reports whether a bash tool call is a known foreground
// read-only command. Capability-restricted runners use this directly instead of
// depending on Plan mode: Plan is a collaboration workflow, while this check is
// an execution permission boundary.
func BashCommandIsReadOnly(args json.RawMessage) bool {
	readOnly, _ := BashReadOnlyProof(args)
	return readOnly
}

// BashReadOnlyProof is BashCommandIsReadOnly with the structural reason a call
// was not proven read-only. The proof is zero when the call is read-only or the
// arguments are not a bash invocation at all.
func BashReadOnlyProof(args json.RawMessage) (bool, planmode.Proof) {
	var p struct {
		Command                     string `json:"command"`
		RunInBackground             bool   `json:"run_in_background"`
		PreserveBackgroundProcesses bool   `json:"preserve_background_processes"`
	}
	if err := json.Unmarshal(args, &p); err != nil || strings.TrimSpace(p.Command) == "" {
		return false, planmode.Proof{}
	}
	if p.RunInBackground || p.PreserveBackgroundProcesses {
		return false, planmode.Proof{Why: planmode.WhyBackground, Subject: p.Command}
	}
	readOnly, proof := bashSubjectProof(p.Command)
	proof.Subject = p.Command
	return readOnly, proof
}

// isReadOnlyBashSubject returns true when a bash command is a known read-only
// operation. The subject is the JSON arg value extracted by Subject() — for bash
// it is the raw command string. Both halves — which commands can be readers,
// and whether these arguments keep this call one — come from shellsafe, so
// permission and evidence cannot answer differently.
func isReadOnlyBashSubject(subject string) bool {
	readOnly, _ := bashSubjectProof(subject)
	return readOnly
}

func bashSubjectProof(subject string) (bool, planmode.Proof) {
	if shellsafe.OperandsNameNetworkPath(subject) {
		return false, planmode.Proof{Why: planmode.WhyNetworkPath}
	}
	if normalized, ok := normalizeBashSafeRedirectsForMatch(subject); ok {
		subject = normalized
	}
	base, sub, fields, ok := shellsafe.ClassifyReadOnlyCommand(subject)
	if ok {
		return argsProof(base, sub, fields)
	}
	// A compound statement is not one classifiable command, but every
	// command it runs is; read-only leaves make the whole thing read-only.
	leaves, why, readable := shellparse.CompoundLeaves(subject)
	if readable {
		return leavesProof(leaves)
	}
	if why == "" {
		why = singleCommandRejection(subject)
	}
	if why != "" {
		return false, planmode.Proof{Why: planmode.WhyShellConstruct, Detail: string(why)}
	}
	if argv, malformed := shellparse.StaticFields(subject); malformed == "" && len(argv) > 0 {
		return false, planmode.Proof{Why: planmode.WhyUnknownProgram, Detail: programLabel(argv)}
	}
	return false, planmode.Proof{}
}

func argsProof(base, sub string, fields []string) (bool, planmode.Proof) {
	if shellsafe.ArgsMakeReadOnlyCommandWrite(base, sub, fields) {
		return false, planmode.Proof{Why: planmode.WhyWriteArguments, Detail: programLabel(fields)}
	}
	return true, planmode.Proof{}
}

func leavesProof(leaves [][]string) (bool, planmode.Proof) {
	for _, argv := range leaves {
		base, sub, fields, classified := shellsafe.ClassifyReadOnlyFields(argv)
		if !classified {
			if len(argv) > 1 && shellparse.IsDynamicArg(argv[1]) {
				return false, planmode.Proof{Why: planmode.WhyShellConstruct, Detail: string(shellparse.StaticRejectExpansion)}
			}
			return false, planmode.Proof{Why: planmode.WhyUnknownProgram, Detail: programLabel(argv)}
		}
		if readOnly, proof := argsProof(base, sub, fields); !readOnly {
			return false, proof
		}
	}
	return true, planmode.Proof{}
}

func singleCommandRejection(subject string) shellparse.StaticRejectReason {
	_, err := shellparse.ParseStaticCommand(subject, shellparse.StaticCommandPolicy{})
	var reject *shellparse.StaticRejectError
	if errors.As(err, &reject) {
		return reject.Reason
	}
	return ""
}

// programLabel names the program a classifier judged: the command word, plus the
// subcommand for the programs read-only only by subcommand.
func programLabel(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	base := strings.ToLower(shellparse.WordBase(argv[0]))
	if _, tabled := shellsafe.ReadOnlyPrefixes[base]; tabled && len(argv) > 1 {
		return base + " " + argv[1]
	}
	return base
}

// containsShellSyntax delegates to the shared classifier; retained for the other
// permission call sites (permission.go).
func containsShellSyntax(cmd string) bool {
	return shellsafe.ContainsShellSyntax(cmd)
}

// dangerousBashPatterns are glob-like patterns that match destructive
// commands. Used only for a UI warning — the deny list is the actual
// enforcement mechanism.
var dangerousBashPatterns = []struct {
	pattern string
	label   string
}{
	{"rm -rf*", "recursive delete"},
	{"rm -r *", "recursive delete"},
	{"rm -fr*", "recursive delete"},
	{"git push*--force*", "force push"},
	{"git push*-f*", "force push"},
	{"git reset --hard*", "hard reset"},
	{"git clean -f*", "force clean"},
	{"git restore*", "discards uncommitted changes"},
	{"git checkout -- *", "discards uncommitted changes"},
	{"git checkout .*", "discards uncommitted changes"},
	{"git stash drop*", "drops stashed changes"},
	{"git stash clear*", "drops stashed changes"},
	{"chmod 777*", "world-writable"},
	{"chmod -R 777*", "world-writable recursive"},
	{"chown *", "ownership change"},
	{"sudo *", "superuser"},
	{"mkfs*", "filesystem format"},
	{"dd if=*", "raw device write"},
	{"fdisk*", "partition table"},
	{"> /dev/*", "device overwrite"},
}

// BashDangerWarning returns a short label if subject matches a known
// dangerous pattern, or "" when the command looks safe. This is a visual
// hint only — the Policy rules are the authority.
func BashDangerWarning(subject string) string {
	s := strings.TrimSpace(subject)
	for _, d := range dangerousBashPatterns {
		if matchGlob(d.pattern, s) {
			return d.label
		}
	}
	return ""
}
