package agent

import (
	"strings"

	"reasonix/internal/evidence"
)

func (a *Agent) scratchRoots() []string {
	if a == nil || a.svc.sessionTemp == nil {
		return nil
	}
	dir := strings.TrimSpace(a.svc.sessionTemp.Dir())
	if dir == "" {
		return nil
	}
	return []string{dir}
}

func (a *Agent) stampReceiptDeliveryScope(rec *evidence.Receipt) {
	if a == nil || rec == nil || !rec.Success || !(rec.Mutation || rec.Write) {
		return
	}
	roots := a.scratchRoots()
	if len(rec.Paths) > 0 {
		for _, path := range rec.Paths {
			if evidence.ClassifyWriteScope(path, a.writeWorkspaceRoot, roots) != evidence.WriteScopeScratch {
				return
			}
		}
		rec.DeliveryScope = evidence.WriteScopeScratch
		return
	}
}
