package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/config"
)

func TestSaveProviderCredentialRefreshesRouteWhileConfigEditLocked(t *testing.T) {
	isolateDesktopUserDirs(t)
	const keyEnv = "TEST_PROXY_CONFIG_LOCK_KEY"
	setDesktopTestCredential(t, keyEnv, "sk-before-lock")
	auth := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth <- r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	modelRef, _ := configureCredentialProxyTestModels(t, upstream.URL, keyEnv)
	a := &App{}
	t.Cleanup(a.closeCredentialProxy)
	route, err := a.applyCredentialProxyModel("box", "~/app", modelRef)
	if err != nil {
		t.Fatal(err)
	}

	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		_, err := a.saveProviderCredential(keyEnv, "sk-after-lock")
		return err
	}(); err != nil {
		t.Fatal(err)
	}
	requestCredentialProxy(t, route.port, route.token)
	if got := <-auth; got != "Bearer sk-after-lock" {
		t.Fatalf("refreshed upstream auth = %q, want rotated credential", got)
	}
}
