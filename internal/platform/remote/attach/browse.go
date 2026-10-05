package attach

import (
	"context"
	"errors"
	"path"
	"slices"
	"strings"
	"time"

	"reasonix/internal/platform/remote/sftpfs"
)

// browseLinger keeps a connection the picker opened alive past its answer.
// Stepping into a folder is a second call, and a link dropped between the two
// re-dials — which on a passphrase-protected key means asking for it again.
const browseLinger = 45 * time.Second

// browseCap bounds one answer. A home directory on a shared box holds tens of
// thousands of entries, and a picker showing the first few hundred with a note
// is the one that stays usable; silently cutting it would read as "that folder
// is not there".
const browseCap = 500

// Listing is one directory on the far machine, in that machine's own spelling.
// Parent is carried rather than cut from Path: neither side's path rules
// describe the other's, and a Windows host answers a mac with a drive letter.
type Listing struct {
	Path      string
	Parent    string
	Folders   []Folder
	Truncated bool
}

// Folder is one directory inside a Listing. The name comes from the machine
// that owns it for the same reason the parent does.
type Folder struct {
	Name string
	Path string
}

// Browse lists the directories under dir on host, resolving an empty dir to the
// login home. It holds the connection and nothing above it: no reasonix is
// installed and no serve is started, so choosing a folder costs one dial even
// on a machine this build has never run on.
func (p *Pool) Browse(ctx context.Context, host, dir string) (Listing, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return Listing{}, errors.New("attach: no host named")
	}
	l, dialer := p.holdLink(host)
	if dialer {
		p.dial(l, Call{})
	}
	if err := l.wait(ctx); err != nil {
		p.dropLink(l)
		return Listing{}, err
	}
	// Released on a timer, not at return — see browseLinger.
	defer func() { time.AfterFunc(browseLinger, func() { p.dropLink(l) }) }()

	fsys, err := l.client.SFTP()
	if err != nil {
		return Listing{}, err
	}
	if strings.TrimSpace(dir) == "" {
		dir = "~"
	}
	// The far machine resolves it: ~ belongs to the account that logged in, and
	// a symlinked project must be listed under the path it really has, or
	// stepping up from it would leave the tree it was reached through.
	at, err := fsys.RealPath(ctx, dir)
	if err != nil {
		return Listing{}, err
	}
	entries, err := fsys.List(ctx, at)
	if err != nil {
		return Listing{}, err
	}
	return listingOf(at, entries), nil
}

func listingOf(at string, entries []sftpfs.Entry) Listing {
	out := Listing{Path: at}
	if up := path.Dir(at); up != at {
		out.Parent = up
	}
	for _, e := range entries {
		if e.IsDir {
			out.Folders = append(out.Folders, Folder{Name: e.Name, Path: e.Path})
		}
	}
	// SFTP answers in whatever order the far filesystem holds them.
	slices.SortFunc(out.Folders, func(a, b Folder) int { return strings.Compare(a.Name, b.Name) })
	if len(out.Folders) > browseCap {
		out.Folders, out.Truncated = out.Folders[:browseCap], true
	}
	return out
}
