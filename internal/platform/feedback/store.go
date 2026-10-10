package feedback

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
)

const (
	stateFile     = "feedback.json"
	maxLocalItems = 50
)

// state is everything kept on this machine. The install token authorises
// reading this install's own reports, so the file is private to the user.
type state struct {
	InstallID    string   `json:"installId,omitempty"`
	InstallToken string   `json:"installToken,omitempty"`
	DisplayName  string   `json:"displayName,omitempty"`
	Items        []Item   `json:"items,omitempty"`
	Pending      *pending `json:"pending,omitempty"`
	// Profile is the last standing the service confirmed, shown labelled stale
	// while it is unreachable.
	Profile *Profile `json:"profile,omitempty"`
	// Seen is, per receipt, the newest reply id the person has been shown.
	Seen map[string]ReplyID `json:"seen,omitempty"`
}

// pending is the idempotency key of a send whose outcome this machine never
// learned, with a hash of what it carried. Only the hash is kept, never the text.
type pending struct {
	Key         string    `json:"key"`
	Fingerprint string    `json:"fingerprint"`
	At          time.Time `json:"at"`
}

const pendingTTL = 24 * time.Hour

type store struct {
	path string
	mu   sync.Mutex
}

func newStore(home string) *store { return &store{path: filepath.Join(home, stateFile)} }

// update runs fn on the current state and writes the result back atomically,
// under a lock that also excludes another process on the same home.
func (s *store) update(fn func(*state) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	release, err := filelock.Acquire(context.Background(), s.path+".lock")
	if err != nil {
		return err
	}
	defer release()
	st, err := s.read()
	if err != nil {
		return err
	}
	if err := fn(&st); err != nil {
		return err
	}
	body, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(s.path, body, 0o600)
}

func (s *store) load() (state, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

func (s *store) read() (state, error) {
	var st state
	body, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if json.Unmarshal(body, &st) != nil {
		return state{}, nil
	}
	return st, nil
}

// begin settles what a send goes out under: the install identity, minted on
// first use, and the idempotency key. A caller key wins; otherwise an unfinished
// send of the same content keeps its key.
func (s *store) begin(key, fingerprint string) (id, token, resolved string, err error) {
	err = s.update(func(st *state) error {
		if err := st.mint(); err != nil {
			return err
		}
		if key == "" {
			if p := st.Pending; p != nil && p.Fingerprint == fingerprint && now().Sub(p.At) < pendingTTL {
				key = p.Key
			} else if key, err = newKey(); err != nil {
				return err
			}
			st.Pending = &pending{Key: key, Fingerprint: fingerprint, At: now()}
		}
		id, token, resolved = st.InstallID, st.InstallToken, key
		return nil
	})
	return
}

// rotate replaces an identity the service no longer accepts. The service cannot
// issue a token twice, so the old reports stay listed but unreadable.
func (s *store) rotate() (id string, err error) {
	err = s.update(func(st *state) error {
		st.retire()
		if err := st.mint(); err != nil {
			return err
		}
		id = st.InstallID
		return nil
	})
	return id, err
}

func (st *state) mint() error {
	if st.InstallID != "" {
		return nil
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	st.InstallID, st.InstallToken = base64.RawURLEncoding.EncodeToString(raw), ""
	return nil
}

// remember merges reports into the local list, newest first, without ever
// shrinking what a fuller answer already recorded.
func (st *state) remember(items ...Item) {
	byReceipt := map[string]Item{}
	for _, it := range append(append([]Item{}, st.Items...), items...) {
		it.UnreadReplies = 0
		byReceipt[it.Receipt] = it
	}
	merged := make([]Item, 0, len(byReceipt))
	for _, it := range byReceipt {
		merged = append(merged, it)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].CreatedAt.After(merged[j].CreatedAt) })
	st.Items = merged[:min(len(merged), maxLocalItems)]
	kept := map[string]bool{}
	for _, it := range st.Items {
		kept[it.Receipt] = true
	}
	for receipt := range st.Seen {
		if !kept[receipt] {
			delete(st.Seen, receipt)
		}
	}
}

// retire drops the install identity a service no longer recognises. Reports
// sent under it stay listed, marked as unreadable, because the service lists
// only the current identity's rows and cannot issue a token twice.
func (st *state) retire() {
	st.InstallID, st.InstallToken, st.Profile = "", "", nil
	for i := range st.Items {
		st.Items[i].StatusUnavailable = true
	}
}

func newKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	const hexd = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for i, v := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hexd[v>>4], hexd[v&0x0f])
	}
	return string(out), nil
}

var now = time.Now
