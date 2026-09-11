package taskcontract

import "reasonix/internal/evidence"

// Enforcement is how strictly an obligation binds the host stop decision.
type Enforcement uint8

const (
	EnforcementAdvisory Enforcement = iota
	EnforcementRecoverable
	EnforcementStrict
)

// ObligationKind names one host-owned duty created from facts.
type ObligationKind string

const (
	ObligationTodo              ObligationKind = "todo"
	ObligationCriteria          ObligationKind = "acceptance_criteria"
	ObligationTargetedVerify    ObligationKind = "targeted_verification"
	ObligationFullVerify        ObligationKind = "full_verification"
	ObligationDiffReview        ObligationKind = "diff_review"
	ObligationIndependentReview ObligationKind = "independent_review"
	ObligationSecurityReview    ObligationKind = "security_review"
	ObligationSignoff           ObligationKind = "signoff"
	ObligationActionReceipt     ObligationKind = "action_receipt"
)

// ReasonCode is a privacy-safe origin or decision code.
type ReasonCode string

const (
	ReasonApprovedPlan   ReasonCode = "approved_plan"
	ReasonActiveGoal     ReasonCode = "active_goal"
	ReasonTodo           ReasonCode = "todo"
	ReasonProjectCheck   ReasonCode = "project_check"
	ReasonReceipt        ReasonCode = "receipt"
	ReasonFirstWriter    ReasonCode = "first_writer"
	ReasonDocsEdit       ReasonCode = "docs_edit"
	ReasonProductionEdit ReasonCode = "production_edit"
	ReasonMultiFile      ReasonCode = "multi_file"
	ReasonSchemaPath     ReasonCode = "schema_path"
	ReasonAuthPath       ReasonCode = "auth_path"
	ReasonDestructive    ReasonCode = "destructive"
	ReasonOpaqueWriter   ReasonCode = "opaque_writer"
	ReasonUserConstraint ReasonCode = "user_constraint"
	ReasonPlanBoundary   ReasonCode = "plan_boundary"
	ReasonTestsForbidden ReasonCode = "tests_forbidden"
	ReasonPolicyFloor    ReasonCode = "policy_floor"
	ReasonConflict       ReasonCode = "constraint_conflict"
)

// Obligation is one host duty. Obligations can only be added, satisfied, or
// invalidated by a later related write — never deleted by another policy.
type Obligation struct {
	Kind             ObligationKind
	Enforcement      Enforcement
	Origin           ReasonCode
	Since            int
	Targets          []evidence.TargetKey
	SatisfiedBy      []int
	RecoveryAttempts int
}

// StopDisposition is the host's terminal stance for the current contract.
type StopDisposition uint8

const (
	StopReady StopDisposition = iota
	StopContinue
	StopPartial
	StopBlocked
)

func (d StopDisposition) String() string {
	switch d {
	case StopReady:
		return "ready"
	case StopContinue:
		return "continue"
	case StopPartial:
		return "partial"
	case StopBlocked:
		return "blocked"
	default:
		return "ready"
	}
}

// StopOptions supplies host facts the contract cannot observe itself.
type StopOptions struct {
	EnvUnavailable   bool
	PermissionDenied bool
	RecoveryLimit    bool
	LoopGuard        bool
}

func copyTargetKeys(in []evidence.TargetKey) []evidence.TargetKey {
	if len(in) == 0 {
		return nil
	}
	return append([]evidence.TargetKey(nil), in...)
}

func copyInts(in []int) []int {
	if len(in) == 0 {
		return nil
	}
	return append([]int(nil), in...)
}

func cloneObligation(o Obligation) Obligation {
	o.Targets = copyTargetKeys(o.Targets)
	o.SatisfiedBy = copyInts(o.SatisfiedBy)
	return o
}
