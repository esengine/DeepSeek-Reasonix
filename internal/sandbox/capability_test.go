package sandbox

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluateCapabilityOmittedDoesNoWorkspaceResolution(t *testing.T) {
	assessment := EvaluateCapability(context.Background(), CapabilityInput{
		Base:      Spec{Mode: "enforce"},
		Workspace: filepath.Join(t.TempDir(), "missing"),
	})
	review := assessment.Review()
	if review.State != CapabilityOmitted || review.Requested {
		t.Fatalf("review = %#v, want omitted request", review)
	}
}

func TestEvaluateCapabilityStrictlySoftDeniesInvalidObjects(t *testing.T) {
	workspace := t.TempDir()
	tests := map[string]string{
		"null":               `null`,
		"unknown field":      `{"network":true,"surprise":true}`,
		"duplicate field":    `{"network":true,"network":false}`,
		"unknown identity":   `{"read_paths":[{"identity":"host","path":"x"}]}`,
		"unknown path field": `{"read_paths":[{"identity":"workspace_relative","path":"x","kind":"directory"}]}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			review := EvaluateCapability(context.Background(), CapabilityInput{
				Base:      Spec{Mode: "off"},
				Workspace: workspace,
				Raw:       json.RawMessage(raw),
			}).Review()
			if review.State != CapabilitySoftDenied || review.Diagnostic == "" {
				t.Fatalf("review = %#v, want diagnosed soft denial", review)
			}
			if !capabilitySetEmpty(review.EffectiveDelta) {
				t.Fatalf("invalid request produced delta: %#v", review.EffectiveDelta)
			}
		})
	}
}

func TestEvaluateCapabilityEnforcesPreNormalizationLimits(t *testing.T) {
	workspace := t.TempDir()
	path := func(name string) map[string]string {
		return map[string]string{"identity": string(WorkspaceRelative), "path": name}
	}
	tests := map[string]map[string]any{
		"read count": {
			"read_paths": []any{path("a"), path("b"), path("c"), path("d"), path("e")},
		},
		"write count": {
			"write_paths": []any{path("a"), path("b"), path("c"), path("d"), path("e")},
		},
		"prefix count": {
			"argv_prefix": []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"},
		},
		"path bytes": {
			"read_paths": []any{path(strings.Repeat("界", maxCapabilityPathBytes/3+1))},
		},
		"justification runes": {
			"justification": strings.Repeat("界", maxCapabilityJustification+1),
		},
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			review := EvaluateCapability(context.Background(), CapabilityInput{
				Base:      Spec{Mode: "off"},
				Workspace: workspace,
				Raw:       raw,
			}).Review()
			if review.State != CapabilitySoftDenied {
				t.Fatalf("state = %v, diagnostic=%q, want soft denial", review.State, review.Diagnostic)
			}
		})
	}
}

func TestEvaluateCapabilityNormalizesIdentityAndPreservesKind(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, "data")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "output.txt")
	if err := os.WriteFile(external, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	raw := capabilityJSON(t, map[string]any{
		"read_paths": []any{
			map[string]string{"identity": string(WorkspaceRelative), "path": "data"},
			map[string]string{"identity": string(WorkspaceRelative), "path": "data/input.txt"},
		},
		"write_paths": []any{
			map[string]string{"identity": string(CanonicalAbsolute), "path": external},
		},
	})
	review := EvaluateCapability(context.Background(), CapabilityInput{
		Base:      Spec{Mode: "off"},
		Workspace: workspace,
		Raw:       raw,
	}).Review()
	if review.State != CapabilityNoEffectiveDelta {
		t.Fatalf("state = %v, diagnostic=%q", review.State, review.Diagnostic)
	}
	if got := review.Request.Reads; len(got) != 1 || got[0].Kind != CapabilityDirectory {
		t.Fatalf("normalized reads = %#v", got)
	}
	if got := review.Request.Writes; len(got) != 1 || got[0].Kind != CapabilityFile || got[0].Canonical != external {
		t.Fatalf("normalized writes = %#v", got)
	}
}

func TestEvaluateCapabilityRejectsWorkspaceEscapeAndMissingTargets(t *testing.T) {
	workspace := t.TempDir()
	external := t.TempDir()
	link := filepath.Join(workspace, "outside")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string]map[string]any{
		"lexical escape": {
			"read_paths": []any{map[string]string{"identity": string(WorkspaceRelative), "path": "../outside"}},
		},
		"symlink escape": {
			"read_paths": []any{map[string]string{"identity": string(WorkspaceRelative), "path": "outside"}},
		},
		"missing": {
			"write_paths": []any{map[string]string{"identity": string(WorkspaceRelative), "path": "missing"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			review := EvaluateCapability(context.Background(), CapabilityInput{Base: Spec{Mode: "off"}, Workspace: workspace, Raw: capabilityJSON(t, request)}).Review()
			if review.State != CapabilitySoftDenied {
				t.Fatalf("state = %v, diagnostic=%q", review.State, review.Diagnostic)
			}
		})
	}
}

func TestEffectiveCapabilityDeltaSubtractsBaseAuthority(t *testing.T) {
	workspace := t.TempDir()
	external := t.TempDir()
	allowed := CapabilityPath{Canonical: workspace, Kind: CapabilityDirectory}
	extra := CapabilityPath{Canonical: external, Kind: CapabilityDirectory}
	delta := effectiveCapabilityDelta(Spec{
		Network:    true,
		WriteRoots: []string{workspace},
	}, CapabilitySet{
		Network: true,
		Reads:   []CapabilityPath{allowed},
		Writes:  []CapabilityPath{allowed, extra},
	})
	if delta.Network || len(delta.Reads) != 0 || len(delta.Writes) != 1 || delta.Writes[0].Canonical != external {
		t.Fatalf("delta = %#v", delta)
	}
}

func TestWriteUnderForbiddenReadRequiresExplicitSufficientRead(t *testing.T) {
	secret := t.TempDir()
	child := filepath.Join(secret, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	write := CapabilityPath{Canonical: child, Kind: CapabilityDirectory}
	base := Spec{ForbidReadRoots: []string{secret}}
	if err := requireExplicitReadsForWrites(base, nil, []CapabilityPath{write}); err == nil {
		t.Fatal("write without explicit read should be rejected")
	}
	insufficient := CapabilityPath{Canonical: filepath.Join(child, "file"), Kind: CapabilityFile}
	if err := requireExplicitReadsForWrites(base, []CapabilityPath{insufficient}, []CapabilityPath{write}); err == nil {
		t.Fatal("narrower read should not cover directory write")
	}
	if err := requireExplicitReadsForWrites(base, []CapabilityPath{write}, []CapabilityPath{write}); err != nil {
		t.Fatalf("covering read rejected: %v", err)
	}
	delta := effectiveCapabilityDelta(Spec{WriteRoots: []string{secret}, ForbidReadRoots: []string{secret}}, CapabilitySet{
		Reads:  []CapabilityPath{write},
		Writes: []CapabilityPath{write},
	})
	if len(delta.Reads) != 1 || len(delta.Writes) != 1 {
		t.Fatalf("forbidden-read punch-through must retain independent read and write delta: %#v", delta)
	}
}

func TestBroadCapabilityRootsRemainValidButCritical(t *testing.T) {
	raw := capabilityJSON(t, map[string]any{
		"read_paths": []any{map[string]string{"identity": string(CanonicalAbsolute), "path": string(filepath.Separator)}},
	})
	review := EvaluateCapability(context.Background(), CapabilityInput{Base: Spec{Mode: "off"}, Workspace: t.TempDir(), Raw: raw}).Review()
	if review.State != CapabilityNoEffectiveDelta || review.Risk.Level != CapabilityRiskCritical {
		t.Fatalf("review = %#v, want valid critical root", review)
	}
	if len(review.Risk.Findings) == 0 || review.Risk.Findings[0].Code != "filesystem_root" {
		t.Fatalf("risk findings = %#v", review.Risk.Findings)
	}
}

func TestSensitiveCapabilityPathsCarryCriticalRisk(t *testing.T) {
	workspace := t.TempDir()
	forbidden := filepath.Join(workspace, "private")
	if err := os.Mkdir(forbidden, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(workspace, ".env"),
		filepath.Join(workspace, ".git-credentials"),
		filepath.Join(workspace, ".netrc"),
		filepath.Join(workspace, "client.pem"),
		filepath.Join(workspace, "private", "notes.txt"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			request := CapabilitySet{Reads: []CapabilityPath{{Canonical: path, Kind: CapabilityFile}}}
			risk := classifyCapabilityRisk(Spec{ForbidReadRoots: []string{forbidden}}, request)
			if risk.Level != CapabilityRiskCritical || len(risk.Findings) == 0 || risk.Findings[0].Code != "sensitive_path" {
				t.Fatalf("risk = %#v, want critical sensitive_path", risk)
			}
		})
	}
}

func TestBroadPseudoFilesystemScopesCarryCriticalRisk(t *testing.T) {
	for _, target := range []string{"/dev", "/dev/dri", "/proc", "/proc/1", "/sys", "/sys/kernel"} {
		t.Run(target, func(t *testing.T) {
			request := CapabilitySet{Reads: []CapabilityPath{{Canonical: target, Kind: CapabilityDirectory}}}
			risk := classifyCapabilityRisk(Spec{}, request)
			if risk.Level != CapabilityRiskCritical || len(risk.Findings) == 0 || risk.Findings[0].Code != "broad_system_path" {
				t.Fatalf("risk = %#v, want critical broad_system_path", risk)
			}
		})
	}
}

func capabilityJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
