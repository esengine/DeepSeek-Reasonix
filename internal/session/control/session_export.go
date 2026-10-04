package control

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

// ErrNoWorkspace is returned when a session has no workspace to export into.
var ErrNoWorkspace = errors.New("session has no workspace root")

const maxExportNames = 100

// ExportConversation writes the conversation as markdown into the workspace
// root and returns the file's path and how many messages it holds. Zero
// messages writes nothing. The file is created exclusively and never through a
// symlink, so a planted name can neither be overwritten nor lead outside.
func (c *Controller) ExportConversation() (path string, messages int, err error) {
	doc, n := ConversationMarkdown(c.History())
	if n == 0 {
		return "", 0, nil
	}
	if c.workspaceRoot == "" {
		return "", 0, ErrNoWorkspace
	}
	root, err := os.OpenRoot(c.workspaceRoot)
	if err != nil {
		return "", 0, err
	}
	defer root.Close()
	stem := "session-" + time.Now().Format("20060102-150405")
	for i := range maxExportNames {
		name := stem + ".md"
		if i > 0 {
			name = fmt.Sprintf("%s-%d.md", stem, i+1)
		}
		f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", 0, err
		}
		_, werr := f.WriteString(doc)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return "", 0, werr
		}
		return c.workspaceRoot + string(os.PathSeparator) + name, n, nil
	}
	return "", 0, os.ErrExist
}

// ConversationMarkdown renders what the person and the model said: no system
// text, reasoning, tool traffic, steers or lines the host wrote on the
// person's behalf. n counts the messages written.
func ConversationMarkdown(msgs []provider.Message) (doc string, n int) {
	var b strings.Builder
	b.WriteString("# reasonix session\n\n")
	var last provider.Role
	for _, msg := range msgs {
		var content string
		switch msg.Role {
		case provider.RoleUser:
			if _, _, steer := sessionstore.SteerKind(msg.Content); steer || msg.HostAuthored {
				continue
			}
			content = sessionstore.UserMessageText(msg)
		case provider.RoleAssistant:
			content = msg.Content
		default:
			continue
		}
		if content = strings.TrimSpace(content); content == "" {
			continue
		}
		if msg.Role != last {
			b.WriteString("## " + map[provider.Role]string{provider.RoleUser: "User", provider.RoleAssistant: "Assistant"}[msg.Role] + "\n\n")
		}
		b.WriteString(content + "\n\n")
		last = msg.Role
		n++
	}
	return b.String(), n
}
