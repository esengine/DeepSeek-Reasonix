package builtin

import (
	"path/filepath"
	"strings"

	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/sessiontemp"
)

// refuseLinkedGitMetadata stops a confined launch the sandbox cannot make safe
// and releases its lease; see sandbox.CheckGitMetadata.
func refuseLinkedGitMetadata(wrapped bool, spec sandbox.Spec, lease *sessiontemp.Lease) error {
	if !wrapped {
		return nil
	}
	err := sandbox.CheckGitMetadata(spec)
	if err != nil && lease != nil {
		lease.Release()
	}
	return err
}

// confinementNotes appends the host's account of what the sandbox refused a
// foreground command: protected Git metadata, dropped write roots, then egress.
func (b bash) confinementNotes(out string, wrapped bool, egressToken string) string {
	out = appendSessionDataHint(out, b.gitMetadataNote(out, wrapped))
	out = appendSessionDataHint(out, b.writeRootNote(wrapped))
	return appendSessionDataHint(out, b.egressNote(egressToken))
}

// gitMetadataNote is the host's account of a confined command refused at Git
// metadata the sandbox protects. The OS error alone reads as an ordinary
// permission problem, and git reports some refused config writes yet exits 0,
// so the note rides on the protected path's identity, not the exit status.
func (b bash) gitMetadataNote(out string, wrapped bool) string {
	if !wrapped {
		return ""
	}
	named := gitMetadataNamedIn(out, b.sb, b.workDir)
	if len(named) == 0 {
		return ""
	}
	return "[host] " + sandbox.GitMetadataDeniedCode + ": " + strings.Join(named, ", ") +
		" is Git configuration or hooks that the host's own git reads, so the sandbox keeps it read-only; a write there did not happen even if the command exited 0. Everything else under .git stays writable. If the user asked for exactly this change, tell them it has to be made outside the sandbox."
}

// gitMetadataNamedIn returns the protected Git metadata paths a failed
// command's output names, spelled as it names them. The paths are the host's
// own identities; nothing here reads the wording around them.
func gitMetadataNamedIn(output string, spec sandbox.Spec, workDir string) []string {
	var named []string
	for _, path := range sandbox.GitMetadataPaths(spec) {
		for _, spelling := range gitMetadataSpellings(path, workDir) {
			if containsPathToken(output, spelling) {
				named = append(named, spelling)
				break
			}
		}
	}
	return named
}

func gitMetadataSpellings(path, workDir string) []string {
	out := []string{path}
	base, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		return out
	}
	if rel, err := filepath.Rel(base, path); err == nil && filepath.IsLocal(rel) {
		if strings.HasSuffix(path, string(filepath.Separator)) {
			rel += string(filepath.Separator)
		}
		out = append(out, rel)
	}
	return out
}

// containsPathToken reports whether path occurs in output as a whole path, not
// as the tail or prefix of a longer one: `.git` inside `main/.git/objects` is
// not the workspace's `.git`. A directory spelling ends in a separator.
func containsPathToken(output, path string) bool {
	dirSpelling := strings.HasSuffix(path, string(filepath.Separator))
	for from := 0; ; {
		i := strings.Index(output[from:], path)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(path)
		if (start == 0 || !isPathByte(output[start-1])) && (dirSpelling || end == len(output) || !isPathByte(output[end])) {
			return true
		}
		from = start + 1
	}
}

func isPathByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("._-/\\~", c) >= 0
}
