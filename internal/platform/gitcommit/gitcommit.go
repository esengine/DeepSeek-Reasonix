// Package gitcommit reads the staged change set and records a local commit from
// it. Every git call goes through gitcmd.Repo, so no program named by the
// repository's configuration or hooks runs, and signing is off because the
// signer is such a program. Nothing here pushes, stages or rewrites history.
package gitcommit

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"reasonix/internal/base/secrets"
	"reasonix/internal/base/textutil"
	"reasonix/internal/platform/gitcmd"
)

var (
	// ErrNoRepository is a workspace that is not under version control.
	ErrNoRepository = errors.New("gitcommit: the workspace is not a git repository")
	// ErrNothingStaged is an index identical to HEAD.
	ErrNothingStaged = errors.New("gitcommit: nothing is staged")
	// ErrStagedChanged is an index that no longer matches what the person confirmed.
	ErrStagedChanged = errors.New("gitcommit: the staged changes differ from the ones confirmed")
	// ErrSensitiveStaged is a staged set holding secret-like content that was not acknowledged.
	ErrSensitiveStaged = errors.New("gitcommit: the staged changes look like they contain secrets")
	// ErrEmptyMessage is a commit message with nothing in it.
	ErrEmptyMessage = errors.New("gitcommit: the commit message is empty")
	// ErrMessageInvalid is a message with a control character or a bidirectional
	// override: text that would render differently from what it says.
	ErrMessageInvalid = errors.New("gitcommit: the commit message holds a NUL byte")
	// ErrMessageTooLong is a message past MaxMessageBytes.
	ErrMessageTooLong = errors.New("gitcommit: the commit message is too long")
	// ErrIdentityMissing is a repository with no author identity git can use.
	ErrIdentityMissing = errors.New("gitcommit: no git author identity is configured")
	// ErrCommitFailed is git refusing or failing the commit.
	ErrCommitFailed = errors.New("gitcommit: git could not record the commit")
)

const (
	// MaxMessageBytes bounds a message; a longer one is refused, never clipped.
	MaxMessageBytes = 16 * 1024
	// MaxDiffBytes bounds the diff text retained for the model; the scan is unbounded.
	MaxDiffBytes   = 1 << 20
	recentSubjects = 8
)

// File is one staged path. Insertions and Deletions are nil where git did not
// count, as for a binary file.
type File struct {
	Path       string `json:"path"`
	Status     string `json:"status"`
	Insertions *int   `json:"insertions,omitempty"`
	Deletions  *int   `json:"deletions,omitempty"`
	Sensitive  bool   `json:"sensitive,omitempty"`
}

// Snapshot is the index as it stood when read, frozen as a tree object: every
// later read and the commit itself use that tree, so an index that moves on
// cannot change what is reviewed or recorded. Fingerprint names HEAD and tree.
type Snapshot struct {
	Files          []File
	Diff           string // diff of the non-sensitive files, up to MaxDiffBytes
	Truncated      bool
	ContentSecrets bool // an added line is shaped like a credential
	Fingerprint    string
	Recent         []string // subjects of the latest commits, newest first
	head, tree     string
}

// SensitiveFiles lists the staged paths flagged by name or by a private key in
// their added lines.
func (s Snapshot) SensitiveFiles() []string {
	var out []string
	for _, f := range s.Files {
		if f.Sensitive {
			out = append(out, f.Path)
		}
	}
	return out
}

// Warned reports whether the set needs an acknowledgement before it is committed.
func (s Snapshot) Warned() bool { return s.ContentSecrets || len(s.SensitiveFiles()) > 0 }

// SensitiveError is ErrSensitiveStaged carrying what tripped it.
type SensitiveError struct {
	Files   []string
	Content bool
}

func (e *SensitiveError) Error() string        { return ErrSensitiveStaged.Error() }
func (e *SensitiveError) Is(target error) bool { return target == ErrSensitiveStaged }

// Staged reads the index against HEAD. A repository with no commits compares
// against the empty tree, which is what git does for the first commit.
func Staged(ctx context.Context, repo gitcmd.Repo) (Snapshot, error) {
	if !repo.Valid() {
		return Snapshot{}, ErrNoRepository
	}
	top := repo.Top()
	head := ""
	if out, err := top.Command(ctx, "rev-parse", "-q", "--verify", "HEAD^{commit}").Output(); err == nil {
		head = strings.TrimSpace(string(out))
	}
	treeOut, err := top.Command(ctx, "write-tree").Output()
	if err != nil {
		return Snapshot{}, fmt.Errorf("gitcommit: freezing the index: %w", err)
	}
	tree := strings.TrimSpace(string(treeOut))
	base := head
	if base == "" {
		cmd := top.Command(ctx, "hash-object", "-t", "tree", "--stdin")
		cmd.Stdin = strings.NewReader("")
		out, err := cmd.Output()
		if err != nil {
			return Snapshot{}, fmt.Errorf("gitcommit: reading the empty tree: %w", err)
		}
		base = strings.TrimSpace(string(out))
	}
	raw, err := top.Command(ctx, "diff", "--raw", "--no-abbrev", "--no-renames", "-z", base, tree).Output()
	if err != nil {
		return Snapshot{}, fmt.Errorf("gitcommit: reading the staged files: %w", err)
	}
	files := parseRaw(raw)
	if len(files) == 0 {
		return Snapshot{}, ErrNothingStaged
	}
	snap := Snapshot{Files: files, Fingerprint: head + ":" + tree, head: head, tree: tree}
	if numstat, err := top.Command(ctx, "diff", "--numstat", "--no-renames", "-z", base, tree).Output(); err == nil {
		applyNumstat(snap.Files, numstat)
	}
	if err := scanDiff(ctx, top, base, tree, &snap); err != nil {
		return Snapshot{}, err
	}
	var recentSecret bool
	snap.Recent, recentSecret = recentLog(ctx, top)
	snap.ContentSecrets = snap.ContentSecrets || recentSecret
	return snap, nil
}

func parseRaw(raw []byte) []File {
	fields := bytes.Split(raw, []byte{0})
	var out []File
	for i := 0; i+1 < len(fields); i += 2 {
		meta := strings.Fields(string(fields[i]))
		if len(meta) < 5 {
			continue
		}
		p := string(fields[i+1])
		out = append(out, File{Path: p, Status: meta[4][:1], Sensitive: secrets.SensitiveFileName(path.Base(p))})
	}
	return out
}

func applyNumstat(files []File, raw []byte) {
	counts := map[string][2]*int{}
	for line := range bytes.SplitSeq(raw, []byte{0}) {
		f := strings.SplitN(string(line), "\t", 3)
		if len(f) == 3 {
			counts[f[2]] = [2]*int{atoi(f[0]), atoi(f[1])}
		}
	}
	for i := range files {
		if c, ok := counts[files[i].Path]; ok {
			files[i].Insertions, files[i].Deletions = c[0], c[1]
		}
	}
}

func atoi(s string) *int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	return &n
}

var privateKeyMarker = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)

const (
	maxScanLine = 64 << 10
	scanOverlap = 4096
)

// scanDiff streams the whole frozen diff. Sections follow the file list one to
// one, so a section is attributed by position: a file named as a secret holder,
// or whose added lines carry a private key, is flagged and its text dropped
// before anything is retained for a model. Past MaxDiffBytes it keeps judging
// and retains nothing.
func scanDiff(ctx context.Context, top gitcmd.Repo, base, tree string, snap *Snapshot) error {
	cmd := top.Command(ctx, "diff", "--no-color", "--no-renames", base, tree)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("gitcommit: reading the staged diff: %w", err)
	}
	var (
		out     strings.Builder
		section strings.Builder
		idx     = -1
		inHunk  bool
		pem     bool
	)
	flush := func() {
		if idx < 0 || idx >= len(snap.Files) {
			return
		}
		if pem {
			snap.Files[idx].Sensitive = true
		}
		if snap.Files[idx].Sensitive {
			return
		}
		if room := MaxDiffBytes - out.Len(); section.Len() > room {
			out.WriteString(section.String()[:room])
			snap.Truncated = true
		} else {
			out.WriteString(section.String())
		}
	}
	rd := bufio.NewReaderSize(pipe, 64<<10)
	for {
		line, overlong, err := readLine(rd)
		if len(line) > 0 || err == nil {
			if strings.HasPrefix(line, "diff --git ") {
				flush()
				section.Reset()
				idx++
				inHunk, pem = false, false
			} else if strings.HasPrefix(line, "@@") {
				inHunk = true
			}
			if inHunk && strings.HasPrefix(line, "+") {
				if privateKeyMarker.MatchString(line) {
					pem = true
				}
				if overlong || secrets.Redact(line) != line {
					snap.ContentSecrets = true
				}
			}
			if idx >= 0 && idx < len(snap.Files) && !snap.Files[idx].Sensitive && !pem && section.Len() <= MaxDiffBytes {
				section.WriteString(line + "\n")
			}
		}
		if err != nil {
			break
		}
	}
	flush()
	_, _ = io.Copy(io.Discard, pipe)
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("gitcommit: reading the staged diff: %w", err)
	}
	if idx+1 != len(snap.Files) {
		snap.ContentSecrets = true
		out.Reset()
		snap.Truncated = true
	}
	snap.Diff = out.String()
	return nil
}

// readLine returns one line without its newline, clipped to maxScanLine, and
// whether a credential-shaped value sits anywhere in it: the part past the clip
// is judged chunk by chunk, with an overlap so a value on a boundary is whole.
func readLine(rd *bufio.Reader) (string, bool, error) {
	var b []byte
	var tail string
	hit := false
	for {
		part, isPrefix, err := rd.ReadLine()
		if len(b) < maxScanLine {
			b = append(b, part...)
		} else if !hit {
			chunk := tail + string(part)
			hit = secrets.Redact(chunk) != chunk
		}
		if len(b) >= maxScanLine {
			tail = string(append([]byte(tail), part...))
			if n := len(tail); n > scanOverlap {
				tail = tail[n-scanOverlap:]
			}
		}
		if err != nil || !isPrefix {
			return string(b), hit, err
		}
	}
}

func recentLog(ctx context.Context, top gitcmd.Repo) (subjects []string, credential bool) {
	out, err := top.Command(ctx, "log", "-n", strconv.Itoa(recentSubjects), "--format=%s", "--no-show-signature").Output()
	if err != nil {
		return nil, false
	}
	for l := range strings.SplitSeq(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if masked := secrets.Redact(l); masked != l {
				credential, l = true, masked
			}
			subjects = append(subjects, l)
		}
	}
	return subjects, credential
}

// Result is the commit that was recorded.
type Result struct {
	Hash    string
	Subject string
}

// afterCheck runs between the confirmation checks and the write; tests use it
// to move the index at the worst moment.
var afterCheck func()

// Commit records exactly the tree the person reviewed as one local commit.
// fingerprint must be the one Staged reported; a set that holds secret-like
// content is refused unless acknowledged. The commit is written from the tree
// object and HEAD is moved only if it is still where it was, so an index that
// changes after the check cannot enter it. Hooks and signing never run.
func Commit(ctx context.Context, repo gitcmd.Repo, message, fingerprint string, acknowledged bool) (Result, error) {
	message = strings.TrimSpace(strings.ReplaceAll(message, "\r\n", "\n"))
	switch {
	case message == "":
		return Result{}, ErrEmptyMessage
	case len(message) > MaxMessageBytes:
		return Result{}, ErrMessageTooLong
	case textutil.HasHiddenControls(message):
		return Result{}, ErrMessageInvalid
	}
	snap, err := Staged(ctx, repo)
	if err != nil {
		return Result{}, err
	}
	if fingerprint == "" || fingerprint != snap.Fingerprint {
		return Result{}, ErrStagedChanged
	}
	if snap.Warned() && !acknowledged {
		return Result{}, &SensitiveError{Files: snap.SensitiveFiles(), Content: snap.ContentSecrets}
	}
	top := repo.Top()
	if top.Command(ctx, "var", "GIT_AUTHOR_IDENT").Run() != nil {
		return Result{}, ErrIdentityMissing
	}
	if top.Command(ctx, "rev-parse", "-q", "--verify", "MERGE_HEAD").Run() == nil {
		return Result{}, fmt.Errorf("%w: a merge is in progress", ErrCommitFailed)
	}
	if afterCheck != nil {
		afterCheck()
	}
	args := []string{"commit-tree", snap.tree, "--no-gpg-sign"}
	old := strings.Repeat("0", len(snap.tree))
	if snap.head != "" {
		args = append(args, "-p", snap.head)
		old = snap.head
	}
	cmd := top.CommandWithConfig(ctx, []string{"commit.gpgsign=false"}, append(args, "-F", "-")...)
	cmd.Stdin = strings.NewReader(message + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return Result{}, fmt.Errorf("%w: %s", ErrCommitFailed, secrets.RedactCredentials(strings.TrimSpace(stderr.String())))
	}
	hash := strings.TrimSpace(string(out))
	subject, _, _ := strings.Cut(message, "\n")
	upd := top.Command(ctx, "update-ref", "-m", "commit: "+strings.TrimSpace(subject), "HEAD", hash, old)
	stderr.Reset()
	upd.Stderr = &stderr
	if err := upd.Run(); err != nil {
		return Result{}, fmt.Errorf("%w: HEAD moved while committing: %s", ErrStagedChanged, secrets.RedactCredentials(strings.TrimSpace(stderr.String())))
	}
	return Result{Hash: hash, Subject: strings.TrimSpace(subject)}, nil
}
