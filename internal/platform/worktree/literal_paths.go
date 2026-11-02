package worktree

// literalPathspecs keeps admitted filenames from becoming Git selection syntax.
func literalPathspecs(paths []string) []string {
	specs := make([]string, len(paths))
	for i, path := range paths {
		specs[i] = ":(literal)" + path
	}
	return specs
}
