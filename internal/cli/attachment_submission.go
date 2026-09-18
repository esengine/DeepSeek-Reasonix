package cli

import (
	"context"

	"reasonix/internal/control"
	"reasonix/internal/session"
)

// prepareImageTurn keeps rejected local attachment input in the composer. The
// opaque candidate is consumed after TUI setup without another source read.
func (m *chatTUI) prepareImageTurn(sent, raw, restore string) (func(control.SessionAPI), bool) {
	local, ok := m.ctrl.(interface {
		PrepareSubmission(context.Context, control.SubmissionRequest) (*control.PreparedSubmission, error)
		SubmitPreparedWithSetup(context.Context, *control.PreparedSubmission, func() error) (session.SubmissionReceipt, error)
	})
	if !ok || m.ctrl.Running() {
		return func(ctrl control.SessionAPI) { ctrl.SendWithRaw(sent, raw) }, true
	}
	prepared, err := local.PrepareSubmission(context.Background(), control.SubmissionRequest{Input: sent, Display: raw})
	if err != nil {
		m.notice(err.Error())
		m.input.SetValue(restore)
		m.growInputToFit()
		return nil, false
	}
	if !prepared.HasImages() {
		return func(ctrl control.SessionAPI) { ctrl.SendWithRaw(sent, raw) }, true
	}
	return func(control.SessionAPI) {
		if _, err := local.SubmitPreparedWithSetup(context.Background(), prepared, nil); err != nil {
			m.notice(err.Error())
			m.input.SetValue(restore)
			m.growInputToFit()
		}
	}, true
}
