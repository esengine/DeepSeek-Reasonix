package gitcmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
)

// ErrObjectNotLocal reports that objects a command needs are absent from a
// partial clone. Host git never fetches them (GIT_NO_LAZY_FETCH).
var ErrObjectNotLocal = errors.New("gitcmd: git objects are not present locally")

// ObjectsMissing reports whether any object in the trees of revs is absent
// locally, as git itself lists them (--missing=print), without fetching. A rev
// that does not resolve — a root commit's parent — is skipped.
func (r Repo) ObjectsMissing(ctx context.Context, revs ...string) bool {
	for _, rev := range revs {
		out, err := r.Command(ctx, "rev-list", "--objects", "--no-walk", "--missing=print", "--end-of-options", rev, "--").Output()
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(bytes.NewReader(out))
		for sc.Scan() {
			if bytes.HasPrefix(sc.Bytes(), []byte("?")) {
				return true
			}
		}
	}
	return false
}
