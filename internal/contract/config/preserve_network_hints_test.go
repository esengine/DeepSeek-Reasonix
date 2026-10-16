package config

import (
	"os"
	"strings"
	"testing"
)

// A config written before the resolver settings existed carries neither `dns`
// nor `doh`, and a save that edits it rewrites only the keys whose values
// changed — a comment is never added. So the section stayed bare release after
// release while the file itself was rewritten, and the person who opened it saw
// neither key and concluded the feature was not there. The examples now follow
// a save that touches [network].
func TestSavingNetworkSettingsTeachesTheResolverKeys(t *testing.T) {
	seed := "config_version = 12\n[network]\nproxy_mode = \"auto\"\n\n[network.proxy]\ntype = \"socks5\"\n"
	_, saved := saveInPlace(t, seed, func(c *Config) error {
		c.Network.ProxyMode = "off"
		return nil
	})
	for _, want := range networkResolverHints {
		if !strings.Contains(saved, want) {
			t.Fatalf("the resolver example %q is missing:\n%s", want, saved)
		}
	}
	// Inside [network] and before the sub-table it belongs to: an example that
	// lands under [network.proxy] documents the wrong table.
	net := strings.Index(saved, "[network]\n")
	proxy := strings.Index(saved, "[network.proxy]")
	first := strings.Index(saved, networkResolverHints[0])
	if net < 0 || proxy < 0 || !(net < first && first < proxy) {
		t.Fatalf("the examples are not inside [network]:\n%s", saved)
	}
}

// A save that leaves [network] alone must not touch it. This is the contract
// the 1.x-preservation tests pin from the other side: a file is rewritten only
// where the edit changed it.
func TestASaveThatLeavesNetworkAloneAddsNothingToIt(t *testing.T) {
	seed := "config_version = 12\n[network]\nproxy_mode = \"auto\"\n\n[desktop]\nfuture = 1\n"
	_, saved := saveInPlace(t, seed, darkTheme)
	if strings.Contains(saved, "dns") || strings.Contains(saved, "doh") {
		t.Fatalf("an unrelated save touched [network]:\n%s", saved)
	}
}

// Added once, not once per save. A second edit to [network] finds the examples
// already there and leaves the file as it stands.
func TestResolverExamplesAreAddedOnce(t *testing.T) {
	seed := "config_version = 12\n[network]\nproxy_mode = \"auto\"\n"
	path := seedUserConfigText(t, seed)
	if err := EditConfigFile(path, func(c *Config) error { c.Network.ProxyMode = "off"; return nil }); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	// A different [network] field, so this save does take the hinting path.
	if err := EditConfigFile(path, func(c *Config) error { c.Network.NoProxy = "localhost"; return nil }); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if strings.Count(string(second), networkResolverHints[0]) != 1 ||
		strings.Count(string(second), networkResolverHints[1]) != 1 {
		t.Fatalf("an example was duplicated:\n--- after one save ---\n%s\n--- after two ---\n%s", first, second)
	}
}

// A resolver the person set is theirs. The example is not added over it, and
// the setting that still has no line of its own gets one.
func TestAResolverThePersonSetIsNotDuplicated(t *testing.T) {
	seed := "config_version = 12\n[network]\nproxy_mode = \"auto\"\ndns = [\"8.8.8.8\"]\n"
	_, saved := saveInPlace(t, seed, func(c *Config) error {
		c.Network.ProxyMode = "off"
		return nil
	})
	if !strings.Contains(saved, `dns = ["8.8.8.8"]`) {
		t.Fatalf("the person's resolver was lost:\n%s", saved)
	}
	if strings.Contains(saved, networkResolverHints[0]) {
		t.Fatalf("the dns example was added over a real setting:\n%s", saved)
	}
	if !strings.Contains(saved, networkResolverHints[1]) {
		t.Fatalf("the doh example is missing:\n%s", saved)
	}
}

// A commented example someone already has counts as present, so a save does not
// append a second copy of the same two lines.
func TestAnExampleAlreadyInTheFileIsNotRepeated(t *testing.T) {
	seed := "config_version = 12\n[network]\nproxy_mode = \"auto\"\n" +
		networkResolverHints[0] + "\n" + networkResolverHints[1] + "\n"
	_, saved := saveInPlace(t, seed, func(c *Config) error {
		c.Network.ProxyMode = "off"
		return nil
	})
	if strings.Count(saved, networkResolverHints[0]) != 1 || strings.Count(saved, networkResolverHints[1]) != 1 {
		t.Fatalf("the examples were repeated:\n%s", saved)
	}
}

// A section that does not exist is not invented: the renderer writes [network]
// when it has something to say about the network, and a save is not the place
// to add a table the file never had.
func TestNoNetworkSectionMeansNoExamples(t *testing.T) {
	seed := "config_version = 12\n[desktop]\nfuture = 1\n"
	_, saved := saveInPlace(t, seed, darkTheme)
	if strings.Contains(saved, "dns") || strings.Contains(saved, "doh") {
		t.Fatalf("a [network] section was invented:\n%s", saved)
	}
}

// A multiline string may quote a config example — a system prompt explaining
// `dns` is the ordinary case. Its contents are text, not settings, so they
// neither satisfy the check nor get a line added inside them.
func TestAResolverExampleInsideAMultilineStringIsText(t *testing.T) {
	seed := "config_version = 12\n[network]\nproxy_mode = \"auto\"\n\n" +
		"[agent]\nsystem_prompt = \"\"\"\nset dns = [\"1.1.1.1\"] to override\n\"\"\"\n"
	_, saved := saveInPlace(t, seed, func(c *Config) error {
		c.Network.ProxyMode = "off"
		return nil
	})
	for _, want := range networkResolverHints {
		if !strings.Contains(saved, want) {
			t.Fatalf("the example %q was suppressed by a string's contents:\n%s", want, saved)
		}
	}
	if !strings.Contains(saved, `set dns = ["1.1.1.1"] to override`) {
		t.Fatalf("the multiline value was rewritten:\n%s", saved)
	}
}
