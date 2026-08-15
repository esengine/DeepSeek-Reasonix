package taskpolicy

import (
	"strings"
	"unicode"
)

// ExecutionPolicyBlock renders the transient user-turn policy freeze. Callers
// persist it in Message Content and keep the original user text in RawContent.
func ExecutionPolicyBlock(p TaskPolicy) string {
	var b strings.Builder
	b.WriteString(`<execution-policy version="`)
	b.WriteString(itoa(p.PolicyVersion))
	b.WriteString(`">`)
	b.WriteByte('\n')
	b.WriteString("route=")
	b.WriteString(routeName(p.Route))
	b.WriteString(" risk=")
	b.WriteString(riskName(p.Risk))
	b.WriteString(" verification=")
	b.WriteString(verifyName(p.Verification))
	b.WriteString(" review=")
	b.WriteString(reviewName(p.Review))
	b.WriteString(" evidence=")
	b.WriteString(evidenceName(p.Evidence))
	if p.Constraints.ForbidMutation {
		b.WriteString("\nconstraint=no-mutation")
	}
	if p.Constraints.ForbidTests {
		b.WriteString("\nconstraint=no-tests")
	}
	if p.Constraints.ForbidExternal {
		b.WriteString("\nconstraint=no-external")
	}
	if len(p.Constraints.AllowedChecks) > 0 {
		b.WriteString("\nconstraint=only-checks:")
		b.WriteString(strings.Join(p.Constraints.AllowedChecks, ","))
	}
	if p.Constraints.PlanModeReadOnly {
		b.WriteString("\nconstraint=plan-mode-read-only")
	}
	b.WriteString("\n</execution-policy>")
	return b.String()
}

func routeName(r Route) string {
	switch r {
	case RouteLightPlan:
		return "light-plan"
	case RouteFullPlan:
		return "full-plan"
	default:
		return "direct"
	}
}

func riskName(r Risk) string {
	switch r {
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	default:
		return "low"
	}
}

func verifyName(v Verification) string {
	switch v {
	case VerifyTargeted:
		return "targeted"
	case VerifyFull:
		return "full"
	default:
		return "none"
	}
}

func reviewName(r Review) string {
	switch r {
	case ReviewConditional:
		return "conditional"
	case ReviewForced:
		return "forced"
	case ReviewForcedSecurity:
		return "forced-security"
	default:
		return "none"
	}
}

func evidenceName(e Evidence) string {
	switch e {
	case EvidenceTargeted:
		return "targeted"
	case EvidenceClosedLoop:
		return "closed-loop"
	default:
		return "none"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// HasInstructionalContent reports whether s has non-space runes.
func HasInstructionalContent(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		return true
	}
	return false
}
