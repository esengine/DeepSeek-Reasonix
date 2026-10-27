package permission

// WithSessionAllow returns a copy of p with additional ephemeral allow rules.
// Malformed entries are ignored consistently with New.
func (p Policy) WithSessionAllow(rules []string) Policy {
	p.SessionAllow = append(append([]Rule(nil), p.SessionAllow...), parseRules(rules)...)
	return p
}

// WithAllowDynamicBashFallback enables the explicit advanced override for
// dynamic shell shapes. Deny, ask, and exact allow rules retain precedence.
func (p Policy) WithAllowDynamicBashFallback(enabled bool) Policy {
	p.AllowDynamicBash = enabled
	return p
}
