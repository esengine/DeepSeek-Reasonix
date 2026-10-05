package evidence

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// rendersInBrowser names the documents a browser draws on its own. A .tsx or
// .vue source needs a dev server before there is anything to look at, so it is
// left to the page that server answers with.
var rendersInBrowser = map[string]bool{".html": true, ".htm": true, ".svg": true}

// RendersInBrowser reports whether a written file is a page or image whose
// result is seen rather than run. The extension decides; the task's wording
// never does.
func RendersInBrowser(path string) bool {
	return rendersInBrowser[strings.ToLower(filepath.Ext(strings.TrimSpace(path)))]
}

// FileURL is the address a browser opens a local file at.
func FileURL(abs string) string {
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// ViewedPath is the workspace file a browser page is, or "" for any page that
// is not a local file. The URL is the one the browser reports, never the one a
// model asked for.
func ViewedPath(pageURL string) string {
	u, err := url.Parse(strings.TrimSpace(pageURL))
	if err != nil || !strings.EqualFold(u.Scheme, "file") || u.Path == "" {
		return ""
	}
	p := u.Path
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return normalizePath(filepath.FromSlash(p))
}

// UnseenRenders owes a look at every page or image written since a screenshot
// last showed it. root resolves a relative path the way the tool that wrote it
// did; the obligation names the file's URL, which is what settling it takes.
func (l *Ledger) UnseenRenders(root string) []Obligation {
	if l == nil {
		return nil
	}
	type render struct {
		at      int
		display string
	}
	written := map[string]render{}
	var order []string
	viewed := map[string]int{}
	l.mu.Lock()
	for i, r := range l.receipts {
		for _, p := range r.Viewed {
			viewed[p] = i
		}
		for key, display := range renderedBy(r, root) {
			if _, ok := written[key]; !ok {
				order = append(order, key)
			}
			written[key] = render{at: i, display: display}
		}
	}
	l.mu.Unlock()
	var out []Obligation
	for _, key := range order {
		w := written[key]
		if at, ok := viewed[key]; ok && at > w.at {
			continue
		}
		link := FileURL(w.display)
		out = append(out, Obligation{
			ID:        fmt.Sprintf("unseen_render@%s#%d", key, w.at),
			Kind:      ObligationUnseenRender,
			Cause:     w.display,
			Discharge: fmt.Sprintf("open %s with browser_open, then take browser_read what=screenshot and check that it looks as intended", link),
		})
	}
	return out
}

// renderedBy maps each page or image a successful receipt wrote to the path it
// is shown under: the spelling the call used, made absolute against root.
func renderedBy(r Receipt, root string) map[string]string {
	out := map[string]string{}
	add := func(p string) {
		if !RendersInBrowser(p) {
			return
		}
		abs := p
		if !filepath.IsAbs(abs) && root != "" {
			abs = filepath.Join(root, abs)
		}
		out[normalizePath(abs)] = filepath.Clean(abs)
	}
	if r.Success && r.Write {
		spelled := originalPaths(r.Args)
		for _, p := range r.Paths {
			if orig, ok := spelled[p]; ok {
				p = orig
			}
			add(p)
		}
	}
	if r.Success {
		for _, p := range r.Created {
			add(p)
		}
	}
	return out
}

// originalPaths recovers the spelling a call's arguments used for each path the
// ledger folded, so a URL keeps the case the file was written under.
func originalPaths(args json.RawMessage) map[string]string {
	var fields map[string]json.RawMessage
	if len(args) == 0 || json.Unmarshal(args, &fields) != nil {
		return nil
	}
	out := map[string]string{}
	for _, p := range extractPaths(fields) {
		out[normalizePath(p)] = p
	}
	return out
}

// OnlyRendersSeen reports that every change the ledger holds wrote a page or
// image, and each has been looked at since its last write. For such a task the
// look is the check. Any other file, or a change whose paths the host could not
// establish, leaves verification to a command, however it was drawn.
func (l *Ledger) OnlyRendersSeen(root string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	wroteRender := false
	for _, r := range l.receipts {
		if !r.Success || (!r.Write && !r.Mutation) {
			continue
		}
		paths := append(append([]string(nil), r.Paths...), r.Created...)
		if len(paths) == 0 {
			l.mu.Unlock()
			return false
		}
		for _, p := range paths {
			if !RendersInBrowser(p) {
				l.mu.Unlock()
				return false
			}
		}
		wroteRender = true
	}
	l.mu.Unlock()
	return wroteRender && len(l.UnseenRenders(root)) == 0
}
