package control

import (
	"encoding/json"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/state/memory"
)

// RememberApproval is the remember-confirmation switch as the user file holds it
// beside what this workspace runs with: an absent key reads as false, because
// asking is what ships.
type RememberApproval struct {
	ProjectAutoConfirm bool   `json:"projectAutoConfirm"`
	ProjectEffective   bool   `json:"projectEffective"`
	Path               string `json:"path"`
}

// RememberApprovalSettings reads [memory] auto_confirm_project_remember from the
// user file and from the merge in force for this workspace. The switch covers a
// project-scoped write only: a global fact reaches every project, so it keeps
// asking whatever this says.
func (c *Controller) RememberApprovalSettings() RememberApproval {
	path := config.UserConfigPath()
	user := config.LoadForEdit(path).Memory
	out := RememberApproval{
		ProjectAutoConfirm: user.AutoConfirmProjectRemember,
		Path:               path,
	}
	out.ProjectEffective = out.ProjectAutoConfirm
	if merged, err := config.LoadForRootReadOnly(c.WorkspaceRoot()); err == nil {
		out.ProjectEffective = merged.Memory.AutoConfirmProjectRemember
	}
	return out
}

// SaveRememberApproval persists the switch to the user file. The caller rebuilds:
// the confirmation decision belongs to the gate the runtime was built with, so a
// live session keeps its own answer until it is replaced.
func (c *Controller) SaveRememberApproval(projectAutoConfirm bool) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	cfg.Memory.AutoConfirmProjectRemember = projectAutoConfirm
	return cfg.SaveTo(path)
}

// allowRememberByScope is the switch's answer for this write. Only a
// project-scoped write is covered, and only one that keeps every bound the
// low-risk path keeps: a global fact, a preference, a sensitive body and a target
// that cannot be resolved all keep asking. The assessment travels so the caller
// can name the fact in the receipt.
func (c *Controller) allowRememberByScope(args json.RawMessage) memory.RememberAssessment {
	mem := c.Memory()
	// This call owns the mark from here on: anything left by an earlier one
	// is stale by definition.
	c.memory.clearReceipt()
	if !c.autoConfirmProjectRemember || mem == nil {
		return memory.RememberAssessment{Reason: "the switch is off"}
	}
	return memory.AssessRememberSkip(mem.Store, args)
}

// noticeRememberSavedUnasked leaves the receipt a skipped confirmation owes: the
// name of the fact that was saved, which /forget takes back.
func (c *Controller) noticeRememberSavedUnasked(name string) {
	text := "memory saved without asking"
	if name != "" {
		text += ": " + name
	}
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeMemorySavedUnasked,
		Text: text, Detail: name})
}
