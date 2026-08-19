package taskcontract

import "reasonix/internal/evidence"

// Mapping is the fixed action-to-obligation table for one successful writer.
type Mapping struct {
	Preconditions []Obligation
	PostSuccess   []Obligation
}

// MapWriter builds the fixed preconditions and post-success duties for a
// concrete effect profile. Failed or denied writers must not call this.
func MapWriter(profile evidence.EffectProfile, seq int, workspaceRoot string, testsForbidden bool) Mapping {
	if profile.ReadOnly && !profile.MutatesState() {
		return Mapping{}
	}
	targets := profile.TargetKeys()
	class := classifyWriter(profile, workspaceRoot)
	pre, verifyKind, origin, verifyEnf, reviews := writerDuties(class)
	if testsForbidden {
		if verifyEnf != EnforcementAdvisory {
			verifyEnf = EnforcementAdvisory
		}
	}
	var mapping Mapping
	for _, kind := range pre {
		mapping.Preconditions = append(mapping.Preconditions, newObligation(kind, EnforcementRecoverable, origin, seq, targets))
	}
	if verifyKind != "" {
		mapping.PostSuccess = append(mapping.PostSuccess, newObligation(verifyKind, verifyEnf, origin, seq, targets))
	}
	for _, kind := range reviews {
		enf := EnforcementRecoverable
		if verifyEnf == EnforcementStrict || class == writerAuth || class == writerSchema || class == writerDestructive || class == writerOpaque {
			enf = EnforcementStrict
		}
		if testsForbidden && (kind == ObligationTargetedVerify || kind == ObligationFullVerify) {
			enf = EnforcementAdvisory
		}
		mapping.PostSuccess = appendScopedReviewObligations(mapping.PostSuccess, kind, enf, origin, seq, targets)
	}
	return mapping
}

func appendScopedReviewObligations(dst []Obligation, kind ObligationKind, enf Enforcement, origin ReasonCode, seq int, targets []evidence.TargetKey) []Obligation {
	switch kind {
	case ObligationDiffReview, ObligationIndependentReview, ObligationSecurityReview:
		if len(targets) > 1 {
			for _, target := range targets {
				dst = append(dst, newObligation(kind, enf, origin, seq, []evidence.TargetKey{target}))
			}
			return dst
		}
	}
	return append(dst, newObligation(kind, enf, origin, seq, targets))
}

type writerClass uint8

const (
	writerNone writerClass = iota
	writerDocs
	writerSingle
	writerMulti
	writerSchema
	writerAuth
	writerDestructive
	writerOpaque
)

func classifyWriter(profile evidence.EffectProfile, workspaceRoot string) writerClass {
	if isScratchWriter(profile, workspaceRoot) {
		return writerNone
	}
	// MCP/unknown tools carry ReasonOpaqueWriter. Unproven bash is fail-closed
	// at permission and the shell contract, not as an MCP-style opaque writer.
	if profile.Reason == evidence.ReasonOpaqueWriter {
		return writerOpaque
	}
	if profile.OpaqueWriter() {
		if profile.Destructive || profile.Irreversible || profile.ExternalState || profile.HostState {
			return writerDestructive
		}
		return writerSingle
	}
	if profile.HostState && !profile.WorkspaceWrite && !profile.ExternalState && !profile.Destructive {
		return writerNone
	}
	if profile.Destructive || profile.Irreversible || profile.ExternalState || profile.HostState {
		return writerDestructive
	}
	if len(profile.Targets) == 0 && profile.MutatesState() {
		return writerSingle
	}
	var (
		docsOnly  = true
		sawProd   bool
		sensitive writerClass
	)
	for _, t := range profile.Targets {
		switch evidence.ClassifyPath(t.Path, workspaceRoot) {
		case evidence.PathAuth, evidence.PathSecret:
			sensitive = writerAuth
			docsOnly = false
		case evidence.PathSchema, evidence.PathMigration, evidence.PathPublicAPI:
			if sensitive != writerAuth {
				sensitive = writerSchema
			}
			docsOnly = false
		case evidence.PathDocs, evidence.PathI18n, evidence.PathTest, evidence.PathStyle:
		default:
			docsOnly = false
			sawProd = true
		}
	}
	if sensitive != 0 {
		return sensitive
	}
	if docsOnly && !sawProd {
		return writerDocs
	}
	if len(profile.Targets) > 1 {
		return writerMulti
	}
	return writerSingle
}

func workspaceProofTarget(profile evidence.EffectProfile, workspaceRoot string) bool {
	if isScratchWriter(profile, workspaceRoot) {
		return false
	}
	return profile.WorkspaceWrite || profile.RepoMetadata
}

func isScratchWriter(profile evidence.EffectProfile, workspaceRoot string) bool {
	if profile.Reason == evidence.ReasonScratch {
		return true
	}
	if len(profile.Targets) == 0 {
		return false
	}
	for _, target := range profile.Targets {
		if target.Path == "" {
			return false
		}
		if evidence.ClassifyWriteScope(target.Path, workspaceRoot, nil) != evidence.WriteScopeScratch {
			return false
		}
	}
	return true
}

func writerDuties(class writerClass) (pre []ObligationKind, verify ObligationKind, origin ReasonCode, verifyEnf Enforcement, reviews []ObligationKind) {
	switch class {
	case writerDocs:
		return nil, ObligationTargetedVerify, ReasonDocsEdit, EnforcementAdvisory, nil
	case writerSingle:
		return nil, ObligationTargetedVerify, ReasonProductionEdit, EnforcementRecoverable, []ObligationKind{ObligationDiffReview}
	case writerMulti:
		return []ObligationKind{ObligationTodo, ObligationCriteria}, ObligationTargetedVerify, ReasonMultiFile, EnforcementRecoverable, []ObligationKind{ObligationDiffReview}
	case writerSchema:
		return []ObligationKind{ObligationTodo, ObligationCriteria}, ObligationFullVerify, ReasonSchemaPath, EnforcementStrict, []ObligationKind{ObligationDiffReview, ObligationIndependentReview, ObligationSignoff}
	case writerAuth:
		return []ObligationKind{ObligationTodo, ObligationCriteria}, ObligationFullVerify, ReasonAuthPath, EnforcementStrict, []ObligationKind{ObligationSecurityReview, ObligationSignoff}
	case writerDestructive:
		return []ObligationKind{ObligationTodo, ObligationCriteria}, ObligationActionReceipt, ReasonDestructive, EnforcementStrict, []ObligationKind{ObligationDiffReview}
	case writerOpaque:
		return []ObligationKind{ObligationTodo, ObligationCriteria}, ObligationFullVerify, ReasonOpaqueWriter, EnforcementStrict, []ObligationKind{ObligationDiffReview, ObligationIndependentReview, ObligationSignoff}
	default:
		return nil, "", "", 0, nil
	}
}

func newObligation(kind ObligationKind, enf Enforcement, origin ReasonCode, seq int, targets []evidence.TargetKey) Obligation {
	return Obligation{
		Kind:        kind,
		Enforcement: enf,
		Origin:      origin,
		Since:       seq,
		Targets:     copyTargetKeys(targets),
	}
}
