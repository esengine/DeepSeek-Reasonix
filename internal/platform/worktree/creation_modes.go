package worktree

// newFileMode and newDirMode are what a writer in this process creates by
// default, so a new entry carrying them is reproducible by publication.
func newFileMode() uint32 { return 0o666 &^ processUmask }

func newDirMode() uint32 { return 0o777 &^ processUmask }
