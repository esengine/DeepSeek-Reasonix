package cli

import (
	"flag"
	"testing"

	"reasonix/internal/remote/bootstrap"
)

func TestServeBrowserBrokerFromEnv(t *testing.T) {
	env := func(pairs map[string]string) func(string) string {
		return func(k string) string { return pairs[k] }
	}
	if broker, err := serveBrowserBrokerFromEnv(env(nil)); err != nil || broker != nil {
		t.Fatalf("empty env = %v, %v; want nil, nil", broker, err)
	}
	if _, err := serveBrowserBrokerFromEnv(env(map[string]string{bootstrap.BrowserBrokerEnv: "http://127.0.0.1:9"})); err == nil {
		t.Fatal("endpoint without token accepted")
	}
	if _, err := serveBrowserBrokerFromEnv(env(map[string]string{bootstrap.BrowserTokenEnv: "t"})); err == nil {
		t.Fatal("token without endpoint accepted")
	}
	if _, err := serveBrowserBrokerFromEnv(env(map[string]string{
		bootstrap.BrowserBrokerEnv: "http://10.0.0.1:9", bootstrap.BrowserTokenEnv: "t",
	})); err == nil {
		t.Fatal("non-loopback endpoint accepted")
	}
	broker, err := serveBrowserBrokerFromEnv(env(map[string]string{
		bootstrap.BrowserBrokerEnv: "http://127.0.0.1:9", bootstrap.BrowserTokenEnv: "t",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if broker == nil || broker.Endpoint() != "http://127.0.0.1:9" {
		t.Fatalf("broker = %+v", broker)
	}
}

func TestServeHelpAdvertisesBrowserBrokerMarker(t *testing.T) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	registerServeCapabilityFlags(fs)
	seen := false
	fs.VisitAll(func(f *flag.Flag) { seen = seen || f.Name == bootstrap.ServeBrowserBrokerMarker })
	if !seen {
		t.Fatalf("serve flags lack the %s capability marker", bootstrap.ServeBrowserBrokerMarker)
	}
}
