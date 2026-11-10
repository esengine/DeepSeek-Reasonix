package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

type resourceTransport struct {
	mu        sync.Mutex
	listed    []string
	templates []string
	text      string
	listErr   error
	readErr   error
	readCalls []string
}

func (t *resourceTransport) call(_ context.Context, method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch method {
	case "resources/list":
		if t.listErr != nil {
			return nil, t.listErr
		}
		items := make([]map[string]string, 0, len(t.listed))
		for _, u := range t.listed {
			items = append(items, map[string]string{"uri": u, "name": u})
		}
		return json.Marshal(map[string]any{"resources": items})
	case "resources/templates/list":
		items := make([]map[string]string, 0, len(t.templates))
		for _, u := range t.templates {
			items = append(items, map[string]string{"uriTemplate": u, "name": u})
		}
		return json.Marshal(map[string]any{"resourceTemplates": items})
	case "resources/read":
		uri := params.(map[string]any)["uri"].(string)
		t.readCalls = append(t.readCalls, uri)
		if t.readErr != nil {
			return nil, t.readErr
		}
		return json.Marshal(map[string]any{"contents": []map[string]string{{"uri": uri, "text": t.text}}})
	}
	return json.RawMessage(`{}`), nil
}
func (t *resourceTransport) notify(context.Context, string, any) error { return nil }
func (t *resourceTransport) close()                                    {}

func resourceHost(tr *resourceTransport, declares bool) *Host {
	h := NewHost()
	h.clients = append(h.clients, &Client{name: "docs", t: tr, spec: Spec{Name: "docs"}, hasResources: declares})
	return h
}

func refusalCode(t *testing.T, err error) string {
	t.Helper()
	var r tool.Refusal
	if !errors.As(err, &r) {
		t.Fatalf("error %v carries no refusal identity", err)
	}
	return r.Code
}

// The host is what stops a model that was talked into asking for an address
// the server never published: the server is not even asked to read it.
func TestReadDeclaredResourceRefusesWhatTheServerDidNotListWithoutAskingIt(t *testing.T) {
	tr := &resourceTransport{listed: []string{"doc://a"}, text: "A"}
	h := resourceHost(tr, true)

	got, err := h.ReadDeclaredResource(context.Background(), "docs", "doc://a", 0)
	if err != nil || got.Text != "A" || got.Truncated {
		t.Fatalf("a listed resource: %+v, %v", got, err)
	}
	_, err = h.ReadDeclaredResource(context.Background(), "docs", "secret://keys", 0)
	if code := refusalCode(t, err); code != CodeResourceNotDeclared {
		t.Fatalf("code = %q, want %q", code, CodeResourceNotDeclared)
	}
	if len(tr.readCalls) != 1 || tr.readCalls[0] != "doc://a" {
		t.Fatalf("the server was asked to read %v; only the listed address may be read", tr.readCalls)
	}
}

// Each way the call cannot proceed has its own code, so the model and the
// window can tell a wrong name from a server that offers nothing from a
// server that failed.
func TestReadDeclaredResourceNamesWhyItCannotProceed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		host   *Host
		server string
		want   string
	}{
		{"unknown server", resourceHost(&resourceTransport{}, true), "nope", CodeResourceNoServer},
		{"server declares no resources", resourceHost(&resourceTransport{}, false), "docs", CodeResourceUnsupported},
		{"listing fails", resourceHost(&resourceTransport{listErr: errors.New("boom")}, true), "docs", CodeResourceListFailed},
		{"read fails", resourceHost(&resourceTransport{listed: []string{"doc://a"}, readErr: errors.New("boom")}, true), "docs", CodeResourceReadFailed},
	} {
		_, err := tc.host.ReadDeclaredResource(context.Background(), tc.server, "doc://a", 0)
		if code := refusalCode(t, err); code != tc.want {
			t.Errorf("%s: code = %q, want %q", tc.name, code, tc.want)
		}
	}
}

// What the model is given is capped, the cut never splits a character, and the
// model is told how much was left out.
func TestReadDeclaredResourceCapsTheTextAtACharacterBoundary(t *testing.T) {
	tr := &resourceTransport{listed: []string{"doc://big"}, text: strings.Repeat("é", 10)}
	h := resourceHost(tr, true)
	got, err := h.ReadDeclaredResource(context.Background(), "docs", "doc://big", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || got.Bytes != 20 || got.Text != "éé" {
		t.Fatalf("capped read = %+v, want two whole characters of a 20-byte resource", got)
	}
	tr.text = strings.Repeat("x", defaultResourceReadBytes+10)
	out, err := (&resourceReadTool{host: h}).Execute(context.Background(), json.RawMessage(`{"server":"docs","uri":"doc://big"}`))
	if err != nil || !strings.Contains(out, "[truncated:") || !strings.Contains(out, "65546 bytes") {
		t.Fatalf("an oversized read did not say what was left out: %.60q... %v", out, err)
	}
	tr.text = "short"
	out, err = (&resourceReadTool{host: h}).Execute(context.Background(), json.RawMessage(`{"server":"docs","uri":"doc://big"}`))
	if err != nil || out != "short" {
		t.Fatalf("a read under the cap = %q %v", out, err)
	}
}

func TestResourceReadToolRefusesBadArguments(t *testing.T) {
	tool := &resourceReadTool{host: resourceHost(&resourceTransport{}, true)}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`not json`)); err == nil {
		t.Fatal("malformed arguments were accepted")
	}
	if tool.ReadOnly() {
		t.Fatal("reading from a third party's server must not skip the approval policy")
	}
}

// A server that has not connected yet is known by what its last handshake
// cached, and every cache writer must keep what the server declared.
func TestDeclaresResourcesReadsTheLiveClientThenTheCache(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	spec := Spec{Name: "cached-docs", Type: "http", URL: "https://example.test/mcp"}
	h := NewHost()
	if h.DeclaresResources(spec) {
		t.Fatal("an unknown server declares resources")
	}
	live := NewHost()
	live.clients = append(live.clients, &Client{name: spec.Name, hasTools: true, hasResources: true})
	if err := SaveCachedSchema(spec.Name, CachedSchema{CacheKey: SchemaCacheKey(spec), Capabilities: live.cachedCapabilities(spec.Name, true)}); err != nil {
		t.Fatal(err)
	}
	if !h.DeclaresResources(spec) {
		t.Fatal("a cached handshake that declared resources was not honoured")
	}
	if got := h.cachedCapabilities("absent", true); got["resources"] || !got["tools"] {
		t.Fatalf("a server with no live client = %v, want tools only", got)
	}
	if !live.DeclaresResources(spec) {
		t.Fatal("the live client's declaration was not honoured")
	}
}

// A server may declare addresses it builds on demand. An address is admitted
// only when a declared template can produce it, and the host never reads an
// operator it does not understand as permission.
func TestUriMatchesTemplate(t *testing.T) {
	for _, tc := range []struct {
		template, uri string
		want          bool
	}{
		{"doc://{id}", "doc://42", true},
		{"doc://{id}", "doc://42/extra", false},
		{"doc://{id}", "doc://", false},
		{"doc://{id}/page/{n}", "doc://abc/page/3", true},
		{"doc://{id}/page/{n}", "doc://abc/page", false},
		{"file:///{+path}", "file:///src/a/b.go", true},
		{"file:///{+path}", "file:///src/a b.go", false},
		{"file:///{+path}", "http://evil.example/x", false},
		{"doc://{id}", "doc://a\nb", false},
		{"doc://{id}", "other://42", false},
		{"doc://{id}", "xdoc://42", false},
		{"doc://{id}", "doc://42#frag", false},
		{"doc://{id}{?q}", "doc://42?q=1", false},
		{"doc://{/id}", "doc://x", false},
		{"doc://{id", "doc://42", false},
		{"doc://a.b/{id}", "doc://aXb/1", false},
		{"", "doc://x", false},
	} {
		if got := uriMatchesTemplate(tc.template, tc.uri); got != tc.want {
			t.Errorf("template %q, uri %q: %v, want %v", tc.template, tc.uri, got, tc.want)
		}
	}
}

func TestReadDeclaredResourceAdmitsWhatADeclaredTemplateBuilds(t *testing.T) {
	tr := &resourceTransport{listed: []string{"doc://index"}, templates: []string{"doc://item/{id}"}, text: "T"}
	h := resourceHost(tr, true)
	got, err := h.ReadDeclaredResource(context.Background(), "docs", "doc://item/7", 0)
	if err != nil || got.Text != "T" {
		t.Fatalf("an address a declared template builds: %+v, %v", got, err)
	}
	_, err = h.ReadDeclaredResource(context.Background(), "docs", "doc://secret/7", 0)
	if code := refusalCode(t, err); code != CodeResourceNotDeclared {
		t.Fatalf("code = %q, want %q", code, CodeResourceNotDeclared)
	}
	if len(tr.readCalls) != 1 || tr.readCalls[0] != "doc://item/7" {
		t.Fatalf("the server was asked to read %v", tr.readCalls)
	}
}
