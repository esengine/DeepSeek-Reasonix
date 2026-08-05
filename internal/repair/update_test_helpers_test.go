package repair

// overwritePendingUpdateForTest simulates an uncooperative on-disk rewrite.
// Production code can only create a new immutable pending transaction.
func overwritePendingUpdateForTest(tx *UpdateTransaction) error {
	return writePendingUpdate(tx, false)
}
