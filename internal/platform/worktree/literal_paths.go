package worktree

import "strings"

// literalPathspecs keeps admitted filenames from becoming Git selection syntax.
func literalPathspecs(paths []string) []string {
	specs := make([]string, len(paths))
	for i, path := range paths {
		specs[i] = ":(literal)" + path
	}
	return specs
}

// hashPathInput uses Git's byte-oriented C quoting, including embedded newlines.
func hashPathInput(paths []string) string {
	const octal = "01234567"
	var input strings.Builder
	for _, path := range paths {
		input.WriteByte('"')
		for i := range len(path) {
			b := path[i]
			switch {
			case b == '\\' || b == '"':
				input.WriteByte('\\')
				input.WriteByte(b)
			case b < 0x20 || b >= 0x7f:
				input.WriteByte('\\')
				input.WriteByte(octal[b>>6])
				input.WriteByte(octal[(b>>3)&7])
				input.WriteByte(octal[b&7])
			default:
				input.WriteByte(b)
			}
		}
		input.WriteString("\"\n")
	}
	return input.String()
}
