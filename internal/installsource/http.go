package installsource

import (
	"context"
	"io"
	"net/http"
	"time"
)

// defaultFetchTimeout caps the lifetime of a single HTTP fetch. Without it a
// slow CDN can hold the agent tool call open until the user gives up. The
// value is generous (30s) so large SKILL.md bodies still load, but bounded so
// a hung server is not an open-ended wait.
const defaultFetchTimeout = 30 * time.Second

// defaultFetchLimit is the maximum body size we will accept from a remote
// manifest. SKILL.md / .mcp.json files are normally a few KB; 2 MiB is a
// safety cap that prevents an untrusted mirror from streaming gigabytes into
// our parser.
const defaultFetchLimit = 2 << 20

// fetchText performs a bounded GET on sourceURL using the tool's HTTP client.
// It applies defaultFetchTimeout unless the caller's context already has a
// tighter deadline, and never reads more than defaultFetchLimit bytes.
func (t *installSourceTool) fetchText(ctx context.Context, sourceURL string) (string, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultFetchTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", newErr(ErrSourceUnreadable, "%s: %v", sourceURL, err)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "reasonix-install/1.0")
	}
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", newErr(ErrSourceUnreadable, "%s: %v", sourceURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", newErr(ErrAuthRequired, "%s: HTTP %d", sourceURL, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", newErr(ErrSourceUnreadable, "%s: HTTP %d", sourceURL, resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, defaultFetchLimit)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", newErr(ErrSourceUnreadable, "%s: read body: %v", sourceURL, err)
	}
	return string(body), nil
}
