package browser

// maxRequests is how many requests one tab remembers. A page that polls makes
// thousands of them, and what an agent reads is the recent ones.
const maxRequests = 200

// Request is one thing a page asked the network for, as far as the browser has
// said. Headers and bodies are deliberately not kept: a credential travels in
// exactly those, and keeping them would put them in the transcript of every
// session that read this list.
type Request struct {
	Method string
	URL    string
	Kind   string // document, script, xhr, fetch, image, …
	Status int    // 0 until a response arrives
	Mime   string
	Bytes  int64
	Failed string // what the browser said when nothing arrived
	Millis int64

	// Page is which document of this tab asked: the list outlives a navigation,
	// and a 404 the page before this one collected is not this page's.
	Page int64

	id    string
	start float64 // the browser's own clock, in seconds
}

// Requests answers what a tab asked the network for, oldest first.
func (s *Session) Requests(tabID string) ([]Request, TabInfo, error) {
	t, err := s.tab(tabID)
	if err != nil {
		return nil, TabInfo{}, err
	}
	t.mu.Lock()
	out := make([]Request, 0, len(t.netlog))
	for _, r := range t.netlog {
		out = append(out, *r)
	}
	t.mu.Unlock()
	return out, t.info(false), nil
}

func (t *tab) requestStarted(id, method, url, kind string, at float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r := &Request{Method: method, URL: url, Kind: kind, Page: t.navigated, id: id, start: at}
	t.requests[id] = r
	t.netlog = append(t.netlog, r)
	if len(t.netlog) > maxRequests {
		delete(t.requests, t.netlog[0].id)
		t.netlog = t.netlog[1:]
	}
}

func (t *tab) requestResponded(id string, status int, mime string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if r, ok := t.requests[id]; ok {
		r.Status, r.Mime = status, mime
	}
}

func (t *tab) requestEnded(id string, at float64, size int64, failed string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.requests[id]
	if !ok {
		return
	}
	r.Failed = failed
	if size > 0 {
		r.Bytes = size
	}
	if at > r.start {
		r.Millis = int64((at - r.start) * 1000)
	}
}

// requestName is how a request is named in a page's log, which is written for a
// person to read rather than for this list.
func (t *tab) requestName(id, fallback string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if r, ok := t.requests[id]; ok {
		return r.Method + " " + r.URL
	}
	return fallback
}
