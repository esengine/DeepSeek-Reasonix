package shellsafe

// withoutGitLocationOptions drops the git global options that only choose
// where git looks or how it pages, so `git -C dir status` classifies as the
// `status` it runs. `-C dir` is `cd dir &&`, which already classifies. Every
// other global option (-c, --git-dir, --exec-path, --config-env, …) can make
// git run a program, so it stays in place and fails the subcommand lookup.
func withoutGitLocationOptions(base string, fields []string) []string {
	if base != "git" {
		return fields
	}
	i := 1
	for i < len(fields) {
		switch fields[i] {
		case "--no-pager", "-P":
			i++
		case "-C":
			if i+1 >= len(fields) {
				return fields
			}
			i += 2
		default:
			if i == 1 {
				return fields
			}
			return append([]string{fields[0]}, fields[i:]...)
		}
	}
	return fields
}
