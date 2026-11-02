// Package hostframe renders data the host hands the model from someone other
// than the user, under a tag that says whose it is. The body can neither close
// its block nor open a second one, and names the sender chose are reduced to
// printable tokens before they reach an attribute.
package hostframe

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// Attr is one attribute of a block's opening tag, rendered as name="value".
type Attr struct{ Name, Value string }

var defusers sync.Map

// hostTags is the complete vocabulary whose authority belongs to the team host.
var hostTags = []string{"agent-message", "agent-reply", "group-turn", "line", "event", "agent-identity", "team-roster", "group-roster", "team-memory", "scheduled-task"}

func defuser(tag string) *regexp.Regexp {
	if re, ok := defusers.Load(tag); ok {
		return re.(*regexp.Regexp)
	}
	tags := append([]string{}, hostTags...)
	if tag != "" {
		tags = append(tags, tag)
	}
	for i := range tags {
		tags[i] = regexp.QuoteMeta(tags[i])
	}
	re, _ := defusers.LoadOrStore(tag, regexp.MustCompile(`(?i)<(\s*/?\s*(?:`+strings.Join(tags, "|")+`))\b`))
	return re.(*regexp.Regexp)
}

// Fragment is host framing or escaped data; its contents cannot be forged by callers.
type Fragment struct{ text string }

func (f Fragment) String() string { return f.text }

// Text makes arbitrary content inert with respect to the host's vocabulary.
func Text(body string) Fragment { return Fragment{defuser("").ReplaceAllString(body, `<\$1`)} }

// Block frames arbitrary content, defusing every host tag rather than only its own.
func Block(tag string, attrs []Attr, body string) string {
	return Frame(tag, attrs, Fragment{defuser(tag).ReplaceAllString(body, `<\$1`)}).String()
}

// Frame composes already framed or escaped fragments without unescaping their leaves.
func Frame(tag string, attrs []Attr, parts ...Fragment) Fragment {
	var b strings.Builder
	b.WriteString("<" + tag)
	for _, a := range attrs {
		fmt.Fprintf(&b, " %s=%q", a.Name, a.Value)
	}
	b.WriteString(">\n")
	for _, part := range parts {
		b.WriteString(part.text)
	}
	b.WriteString("\n</" + tag + ">\n")
	return Fragment{b.String()}
}

// Token reduces a name someone other than the host chose to at most limit
// letters, digits, spaces and "._-".
func Token(s string, limit int) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if n >= limit {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || strings.ContainsRune("._-", r) {
			b.WriteRune(r)
			n++
		}
	}
	return strings.TrimSpace(b.String())
}
