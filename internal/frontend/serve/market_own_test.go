package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"reasonix/internal/ext/market"
)

var ownRequest = map[string]any{"slug": "me/kit", "version": "1.0.0", "planId": "low:sha256:x"}

func ownedSkill(status string) *market.OwnDetail {
	return &market.OwnDetail{
		Package: market.Package{Kind: "skill", Slug: "me/kit", Status: status, LatestVersion: "1.0.0"},
		Latest:  &market.Version{Version: "1.0.0", Source: "https://example.test/SKILL.md"},
	}
}

// Installing an own package spends the session exactly as publishing does.
func TestMarketOwnRoutesNeedTheAccountGrantAndASession(t *testing.T) {
	pub := &fakePublisher{owned: ownedSkill("private")}
	base := publishServer(t, false, "tok", pub)
	for _, path := range []string{"/market/mine/plan", "/market/mine/install", "/market/mine/me/kit/submit"} {
		if code := marketCode(t, postPublish(t, base+path, ownRequest)); code != "account.signin_disabled" {
			t.Fatalf("%s without the grant = %q", path, code)
		}
	}
	base = publishServer(t, true, "", pub)
	for _, path := range []string{"/market/mine/plan", "/market/mine/install", "/market/mine/me/kit/submit"} {
		if code := marketCode(t, postPublish(t, base+path, ownRequest)); code != "market.signed_out" {
			t.Fatalf("%s signed out = %q", path, code)
		}
	}
	if pub.token != "" {
		t.Fatal("the registry was asked without a session")
	}
}

// Which package is installed is the registry's answer for this account; one it
// will not vouch for is refused before anything is planned.
func TestMarketOwnInstallAsksTheRegistryAsTheAccount(t *testing.T) {
	pub := &fakePublisher{}
	base := publishServer(t, true, "tok", pub)
	withSource := map[string]any{"slug": "bob/kit", "version": "1.0.0", "digest": "sha256:" + fmt.Sprintf("%064d", 0), "source": "https://evil.example/SKILL.md"}
	if code := marketCode(t, postPublish(t, base+"/market/mine/install", withSource)); code != "market.not_yours" {
		t.Fatalf("someone else's = %q", code)
	}
	if pub.token != "tok" {
		t.Fatalf("token = %q", pub.token)
	}
}

func TestMarketOwnInstallRequiresTheShownVersionAndItsPreviewDigest(t *testing.T) {
	pub := &fakePublisher{owned: ownedSkill("pending")}
	base := publishServer(t, true, "tok", pub)
	if code := marketCode(t, postPublish(t, base+"/market/mine/install", map[string]any{"slug": "me/kit"})); code != "request.missing_field" {
		t.Fatalf("no version = %q", code)
	}
	if code := marketCode(t, postPublish(t, base+"/market/mine/install", ownRequest)); code != "market.unpreviewed" {
		t.Fatalf("no digest = %q", code)
	}
}

func TestMarketOwnSubmitMovesThePrivatePackage(t *testing.T) {
	pub := &fakePublisher{}
	base := publishServer(t, true, "tok", pub)
	resp := postPublish(t, base+"/market/mine/me/kit/submit", map[string]any{})
	defer resp.Body.Close()
	var out struct {
		Package market.Package `json:"package"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusOK || out.Package.Status != "pending" || pub.submitted != "me/kit" || pub.token != "tok" {
		t.Fatalf("status=%d out=%+v submitted=%q", resp.StatusCode, out, pub.submitted)
	}
}

func TestMarketOwnRefusalsCarryTheirCause(t *testing.T) {
	for err, want := range map[error]string{
		market.ErrNotFound:                            "market.not_yours",
		market.ErrNotPrivate:                          "market.not_private",
		market.ErrSignedOut:                           "market.signed_out",
		market.ErrRateLimited:                         "market.rate_limited",
		fmt.Errorf("%w: down", market.ErrUnreachable): "market.unreachable",
	} {
		base := publishServer(t, true, "tok", &fakePublisher{err: err})
		if code := marketCode(t, postPublish(t, base+"/market/mine/me/kit/submit", map[string]any{})); code != want {
			t.Errorf("%v: code = %q, want %q", err, code, want)
		}
	}
}
