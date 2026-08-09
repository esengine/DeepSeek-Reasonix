package cli

import (
	"strings"

	"reasonix/internal/event"
)

func (m *chatTUI) collapseFinalToolOutput(tool event.Tool) {
	// Live shell progress is capped; restore the bounded final head+tail result
	// so Ctrl+B keeps the command's ending diagnostics.
	if strings.HasPrefix(tool.ID, "shell-") {
		if tool.Output == "" {
			delete(m.shellOutputs, tool.ID)
		} else {
			m.shellOutputs[tool.ID] = tool.Output
		}
	}
	m.collapseToolOutput(tool.ID, tool.Output)
}
