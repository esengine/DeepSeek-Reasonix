package boot

import "reasonix/internal/safety/permission"

// mediaDecoders are the tools that run a program over untrusted media bytes.
var mediaDecoders = []string{"read_video", "check_video"}

// askWithUnsandboxedMedia is the ask list the permission policy is built from.
// A media decoder parses untrusted bytes; where an OS sandbox wraps it it is
// bounded, and where none does (Windows, or the sandbox turned off) the first
// use is sent for approval. A rule the user wrote about a decoder, in any list,
// is theirs and leaves that decoder alone.
func askWithUnsandboxedMedia(allow, ask, deny []string, sandboxed bool) []string {
	out := append([]string(nil), ask...)
	if sandboxed {
		return out
	}
	for _, name := range mediaDecoders {
		if !userRuleNames(name, allow, ask, deny) {
			out = append(out, name)
		}
	}
	return out
}

func userRuleNames(name string, lists ...[]string) bool {
	for _, list := range lists {
		for _, rule := range list {
			if permission.RuleMatchesString(rule, name, "") {
				return true
			}
		}
	}
	return false
}
