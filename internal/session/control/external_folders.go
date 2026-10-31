package control

import (
	"path/filepath"
	"strings"
	"sync"
)

// externalFolders owns session-scoped external path grants. Directory grants
// include descendants; selecting a file registers only that file's token.
type externalFolders struct {
	mu       sync.RWMutex
	byToken  map[string]string
	toolRefs externalFolderToolRefs
}

// register records root under its token and authorizes it on the tool side.
func (f *externalFolders) register(token, root string) {
	f.mu.Lock()
	if f.byToken == nil {
		f.byToken = map[string]string{}
	}
	f.byToken[token] = root
	f.mu.Unlock()
	if f.toolRefs != nil {
		f.toolRefs.RegisterReadRoot(token, root)
	}
}

// resolve answers a normalized token with the registered root it names and the
// path under it: an exact match is the root itself, and a registered prefix
// carries the remainder as a subpath. A remainder that does not clean to a
// path inside the root answers nothing rather than the root.
func (f *externalFolders) resolve(key string) (rootToken, rel, abs string, ok bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if abs, ok := f.byToken[key]; ok {
		return key, ".", abs, true
	}
	for registered, abs := range f.byToken {
		if !strings.HasPrefix(key, registered+"/") {
			continue
		}
		sub, ok := cleanExternalFolderSubpath(strings.TrimPrefix(key, registered+"/"))
		if !ok {
			return "", "", "", false
		}
		return registered, sub, abs, true
	}
	return "", "", "", false
}

// roots snapshots what is registered, so a caller sorts and filters off the lock.
func (f *externalFolders) roots() []externalRootRef {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]externalRootRef, 0, len(f.byToken))
	for token, abs := range f.byToken {
		out = append(out, externalRootRef{token: token, abs: abs})
	}
	return out
}

// readableUnder answers where an absolute path may be read from, among the
// folders this session was given. A path in none of them answers nothing: the
// grant is what makes a file readable here, and a file nobody brought in is
// not readable at all.
func (f *externalFolders) readableUnder(abs string) (root string, rel string, ok bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, registered := range f.byToken {
		if registered == abs {
			// The grant is the file itself: its folder is the base and its
			// name the path under it.
			return filepath.Dir(registered), filepath.Base(registered), true
		}
		if r, rel, ok := pathUnder(registered, abs); ok {
			return r, rel, true
		}
	}
	return "", "", false
}
