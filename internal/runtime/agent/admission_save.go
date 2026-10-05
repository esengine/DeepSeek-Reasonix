package agent

import "context"

// AdmissionSaver makes a turn's user message durable once it has landed, so a
// process killed before the turn's next save still has the prompt on reopen.
type AdmissionSaver interface {
	SaveAdmittedMessage(ctx context.Context)
}

// SetAdmissionSaver installs the host's save for admitted user messages; nil
// leaves durability to the host's own save points.
func (a *Agent) SetAdmissionSaver(s AdmissionSaver) { a.svc.admissionSaver = s }
