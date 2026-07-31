package normalize

import (
	"fmt"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"strings"
)

// ComposeMentionsTextPrefix builds a text prefix that renders as real Feishu mentions when prepended
// to a text-type outbound message (the <at ...> tag form).
func ComposeMentionsTextPrefix(mentions []types.Mention) string {
	if len(mentions) == 0 {
		return ""
	}
	var parts []string
	for _, m := range mentions {
		if m.UserID == "" {
			continue
		}
		name := m.Name
		parts = append(parts, fmt.Sprintf(`<at user_id="%s">%s</at>`, m.UserID, name))
	}
	if len(parts) > 0 {
		return strings.Join(parts, " ") + " "
	}
	return ""
}

// ComposePostMentionElements produces `at` elements to prepend to the first paragraph of a post body.
func ComposePostMentionElements(mentions []types.Mention) []postElement {
	if len(mentions) == 0 {
		return nil
	}
	var out []postElement
	for _, m := range mentions {
		if m.UserID == "" {
			continue
		}
		out = append(out, postElement{
			Tag:      "at",
			UserID:   m.UserID,
			UserName: m.Name,
		})
	}
	return out
}
