// browser_credential.go — typing a secret into a site.
package permission

import "strings"

// BrowserCredentialPrefix marks a browser subject that enters a secret — a
// password, a one-time code, card details — on the site after it. The host
// writes it; only a rule naming that exact subject answers it, never a site
// grant or a glob, and auto does not.
const BrowserCredentialPrefix = "credential:"

// BrowserScriptPrefix marks arbitrary page JavaScript that only an exact grant answers.
const BrowserScriptPrefix = "script:"

// BrowserSubjectRequiresExplicitApproval reports a browser subject a person
// has to answer unless YOLO or an exact grant already did.
func BrowserSubjectRequiresExplicitApproval(subject string) bool {
	return strings.HasPrefix(subject, BrowserCredentialPrefix) || strings.HasPrefix(subject, BrowserScriptPrefix)
}

// browserSubjects is every subject a browser call answers to: a credential
// entry is also a visit to its site, so a rule refusing the site refuses it.
func browserSubjects(origin string) []string {
	for _, prefix := range []string{BrowserCredentialPrefix, BrowserScriptPrefix} {
		if site, ok := strings.CutPrefix(origin, prefix); ok && site != "" {
			return []string{origin, site}
		}
	}
	return []string{origin}
}

func (p Policy) decideBrowserCredential(toolName, subject string) (Decision, bool) {
	if !IsBrowserTool(toolName) || !BrowserSubjectRequiresExplicitApproval(subject) {
		return 0, false
	}
	switch {
	case matchAny(p.Deny, toolName, subject):
		return Deny, true
	case matchAnyExact(p.SessionAllow, toolName, subject), matchAnyExact(p.Allow, toolName, subject):
		return Allow, true
	case p.Mode == Deny:
		return Deny, true
	}
	return Ask, true
}
