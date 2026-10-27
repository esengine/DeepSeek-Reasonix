package isolation

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

// bindRecord treats stored paths as claims. Every file-content read and identity
// stat is relative to a host-opened source, common, slot or candidate directory.
func (s *Store) bindRecord(slot *os.Root, d durableEntry) (durableEntry, bool) {
	source := s.repo.Top()
	if source.Verify() != nil {
		return d, false
	}
	src, err := openRecordDirectory(source.WorkTree)
	if err != nil {
		return d, false
	}
	defer src.root.Close()
	common, err := openRecordDirectory(source.CommonDir)
	if err != nil {
		return d, false
	}
	defer common.root.Close()
	prefix := filepath.FromSlash(d.Snap.Prefix)
	if prefix != "" && !filepath.IsLocal(prefix) {
		return d, false
	}
	if prefix == "" {
		prefix = "."
	}
	sourceGitDir, ok := common.local(source.GitDir)
	if !ok || !src.same(d.Snap.RepoRoot, ".") || !src.same(d.Workspace, prefix) ||
		!src.same(s.workspace, prefix) || !repoFieldsMatch(d.Snap.Repo, src, common, sourceGitDir) {
		return d, false
	}
	name := filepath.Base(filepath.Clean(d.Cand.WorktreeRoot))
	if !filepath.IsAbs(d.Cand.WorktreeRoot) || !filepath.IsLocal(name) || name == "." || !recordDirectoryEntry(slot, name) {
		return d, false
	}
	candidateRoot, err := slot.OpenRoot(name)
	if err != nil {
		return d, false
	}
	defer candidateRoot.Close()
	cand, err := wrapRecordDirectory(candidateRoot)
	if err != nil || !cand.same(d.Cand.WorktreeRoot, ".") || !cand.same(d.Cand.WorkspaceRoot, prefix) {
		return d, false
	}
	candidate, gitDir, ok := linkedCandidateIdentity(cand, common, sourceGitDir)
	if !ok || !repoFieldsMatch(d.Cand.Repo, cand, common, gitDir) {
		return d, false
	}
	// Use host-derived identities without rewriting the serialized record.
	d.Workspace, d.Snap.RepoRoot, d.Snap.Repo = s.workspace, source.WorkTree, source
	d.Cand.WorktreeRoot, d.Cand.Repo = candidateRoot.Name(), candidate
	d.Cand.WorkspaceRoot = filepath.Join(candidateRoot.Name(), prefix)
	return d, true
}

// The marker supplies a claim; the opened common directory bounds the backlink
// read before any record-supplied administrative path is used.
func linkedCandidateIdentity(cand, common recordDirectory, sourceGitDir string) (gitcmd.Repo, string, bool) {
	if !recordRegularFile(cand.root, ".git") {
		return gitcmd.Repo{}, "", false
	}
	raw, err := cand.root.ReadFile(".git")
	if err != nil {
		return gitcmd.Repo{}, "", false
	}
	admin, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
	if !ok || admin == "" {
		return gitcmd.Repo{}, "", false
	}
	admin = filepath.FromSlash(admin)
	if !filepath.IsAbs(admin) {
		admin = filepath.Join(cand.root.Name(), admin)
	}
	local, ok := common.local(admin)
	if !ok {
		return gitcmd.Repo{}, "", false
	}
	back, err := common.root.ReadFile(filepath.Join(local, "gitdir"))
	if err != nil {
		return gitcmd.Repo{}, "", false
	}
	backPath := filepath.FromSlash(strings.TrimSpace(string(back)))
	if !filepath.IsAbs(backPath) {
		backPath = filepath.Join(common.root.Name(), local, backPath)
	}
	// A shared .git inode is insufficient: the backlink must name this parent.
	if !cand.same(filepath.Dir(backPath), ".") || common.same(admin, sourceGitDir) {
		return gitcmd.Repo{}, "", false
	}
	repo := gitcmd.Repo{Dir: cand.root.Name(), WorkTree: cand.root.Name(),
		GitDir: filepath.Join(common.root.Name(), local), CommonDir: common.root.Name()}
	return repo, local, repo.Verify() == nil
}

func repoFieldsMatch(stored gitcmd.Repo, tree, common recordDirectory, gitDir string) bool {
	if stored == (gitcmd.Repo{}) {
		return true // legacy record; identity comes from the actual marker
	}
	return stored.Valid() && stored.Dir != "" && tree.same(stored.Dir, ".") &&
		tree.same(stored.WorkTree, ".") && common.same(stored.GitDir, gitDir) &&
		common.same(stored.CommonDir, ".")
}
