package fileutil

import "strings"

// VCSStore names a version control and the directory it keeps its store in.
type VCSStore struct {
	Dir  string
	Name string
}

// vcsStores is the declared table, in probe order. A store is not the work
// product — `git status` alone rewrites the index — so nothing downstream may
// read a change under one as evidence the tree was touched. Order settles the
// workspace with two: a jj repo colocated with git keeps both, and the one the
// user drives is the one a plain git checkout does not have.
var vcsStores = []VCSStore{
	{Dir: ".jj", Name: "jj"},
	{Dir: ".git", Name: "git"},
	{Dir: ".hg", Name: "hg"},
	{Dir: ".svn", Name: "svn"},
}

// VCSStores returns the known stores in probe order. Callers that must name a
// workspace's version control read this table instead of testing for `.git`.
func VCSStores() []VCSStore { return append([]VCSStore(nil), vcsStores...) }

// IsVCSStoreDir reports whether a single path element names a VCS store.
func IsVCSStoreDir(name string) bool {
	for _, s := range vcsStores {
		if s.Dir == name {
			return true
		}
	}
	return false
}

// UnderVCSStore reports whether path is a VCS store or lies inside one.
func UnderVCSStore(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	for part := range strings.SplitSeq(NormalizeSlashPath(path), "/") {
		if IsVCSStoreDir(part) {
			return true
		}
	}
	return false
}
