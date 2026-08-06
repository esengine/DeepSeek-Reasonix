package mcpregistry

import "testing"

func TestNpmPackageVersion(t *testing.T) {
	cases := []struct {
		identifier, version, want string
	}{
		{"mcp-server", "", "mcp-server"},
		{"mcp-server", "1.2.3", "mcp-server@1.2.3"},
		{"@scope/mcp-server", "", "@scope/mcp-server"},
		{"@scope/mcp-server", "2.0.0-beta.1", "@scope/mcp-server@2.0.0-beta.1"},
	}
	for _, c := range cases {
		if got := npmPackageVersion(c.identifier, c.version); got != c.want {
			t.Errorf("npmPackageVersion(%q, %q) = %q, want %q", c.identifier, c.version, got, c.want)
		}
	}
}

func TestPythonPackageVersion(t *testing.T) {
	cases := []struct {
		identifier, version, want string
	}{
		{"mcp-server", "", "mcp-server"},
		{"mcp-server", "1.2.3", "mcp-server==1.2.3"},
		{"mcp-server", "1.0.0rc1", "mcp-server==1.0.0rc1"},
		{"mcp-server[postgres]", "0.9.0", "mcp-server[postgres]==0.9.0"},
	}
	for _, c := range cases {
		if got := pythonPackageVersion(c.identifier, c.version); got != c.want {
			t.Errorf("pythonPackageVersion(%q, %q) = %q, want %q", c.identifier, c.version, got, c.want)
		}
	}
}

func TestCacheKey(t *testing.T) {
	// The key is the lowercased, trimmed query, a NUL separator, and the
	// decimal limit, so the query and limit cannot be confused with each other.
	if got, want := cacheKey("Demo", 10), "demo\x0010"; got != want {
		t.Fatalf("cacheKey(\"Demo\", 10) = %q, want %q", got, want)
	}

	// Case and surrounding whitespace normalize to the same key.
	for _, pair := range [][2]string{
		{cacheKey("Demo", 10), cacheKey("  demo  ", 10)},
		{cacheKey("MCP Registry", 20), cacheKey("mcp registry", 20)},
	} {
		if pair[0] != pair[1] {
			t.Errorf("normalized inputs produced different keys: %q != %q", pair[0], pair[1])
		}
	}

	// Distinct (query, limit) inputs must never share a key. The last three
	// pairs would collide under naive query+limit concatenation, and an
	// embedded NUL in a query must not be able to forge a limit.
	for _, pair := range [][2]string{
		{cacheKey("demo", 10), cacheKey("demo", 20)},
		{cacheKey("demo", 10), cacheKey("demo1", 0)},
		{cacheKey("demo\x0010", 5), cacheKey("demo", 105)},
		{cacheKey("demo", 10), cacheKey("demo\x00", 10)},
	} {
		if pair[0] == pair[1] {
			t.Errorf("distinct inputs collided: %q == %q", pair[0], pair[1])
		}
	}
}
