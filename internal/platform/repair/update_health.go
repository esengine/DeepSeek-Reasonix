package repair

// UpdateHealthWitness is one launch's authority to retire the update it booted
// from. The identity is read once at startup, before anything can rewrite the
// pending transaction, so acknowledging health later commits that exact
// transaction or nothing.
type UpdateHealthWitness struct {
	createdAt     string
	transactionID string
}

// CaptureUpdateHealth reads the pending transaction this launch is the target
// of. A launch that did not boot from an update captures nothing, and its
// acknowledgement is inert — an application cannot vouch for a replacement it
// is not running.
func CaptureUpdateHealth(runningVersion string) *UpdateHealthWitness {
	tx, err := ReadPendingUpdate()
	if err != nil || tx == nil || !UpdateVersionsEqual(tx.ToVersion, runningVersion) {
		return nil
	}
	return &UpdateHealthWitness{
		createdAt:     tx.CreatedAt,
		transactionID: UpdateTransactionID(tx),
	}
}

// Acknowledge retires the captured transaction. The updater performs the swap;
// only the application that boots from it can declare the replacement healthy,
// which is why this carries no path or version of its own to be told.
func (w *UpdateHealthWitness) Acknowledge(runningVersion string) error {
	if w == nil {
		return nil
	}
	return MarkUpdateHealthyExact(runningVersion, w.createdAt, w.transactionID)
}
