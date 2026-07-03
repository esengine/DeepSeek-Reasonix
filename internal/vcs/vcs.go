package vcs

// VCSInfo holds unified version control status information.
type VCSInfo struct {
	Type      string // "git" | "jj" | ""
	Repo      string // repo root basename
	Branch    string // branch name or bookmark
	Detached  bool
	Added     int
	Removed   int
	Untracked int // jj: always 0
}

// VCSFileStatus represents a single file-level change.
type VCSFileStatus struct {
	Path    string
	OldPath string // rename/copy source
	Status  string // "Added" / "Modified" / "Deleted" / "Renamed" / "Untracked"
}

// VCSCommit is a single history entry.
type VCSCommit struct {
	Hash    string // git SHA or jj change_id
	Author  string
	Date    string
	Message string
}

// VCSCommitDetail is the detail view for a single commit.
type VCSCommitDetail struct {
	Diff  *string
	Files []string
}
