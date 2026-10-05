package usecap

import (
	"strings"

	fileenc "reasonix/internal/base/fileutil/encoding"
)

// capabilityLeadBytes bounds one catalog row's description. Full descriptions
// were two thirds of a listing that reached 41 KB and spilled out of context
// past the 32 KiB cap — the model asked what it had and was handed a pointer to
// a file. inspect is where the whole text lives, which is what the proxy's own
// description already tells it to use before calling.
const capabilityLeadBytes = 160

// capabilityLead is the first sentence of a description, bounded. A row has to
// say enough to pick from; it does not have to say enough to call.
func capabilityLead(description string) string {
	lead := strings.TrimSpace(description)
	if lead == "" {
		return ""
	}
	if head, _, cut := strings.Cut(lead, ". "); cut {
		lead = head + "."
	}
	if len(lead) <= capabilityLeadBytes {
		return lead
	}
	bounded := lead[:capabilityLeadBytes]
	// Cut on a word so the row does not end mid-token. A description with no
	// spaces to cut on — a CJK one — falls back to the character boundary,
	// which is the same cut TrimPartialRune makes for a truncated read.
	if i := strings.LastIndexAny(bounded, " \t"); i > capabilityLeadBytes/2 {
		bounded = bounded[:i]
	} else {
		bounded = string(fileenc.TrimPartialRune([]byte(bounded)))
	}
	return strings.TrimRight(bounded, " \t,;:") + "…"
}
