package boot

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/extension/providerext"
	"reasonix/internal/provider"
)

// Stage 7 end-to-end coverage: a fake sidecar declares and streams an
// extension-hosted provider through the merged resolver BuildRuntime exposes.

// bootWithProviderPlugin installs the fake sidecar in provider mode and
// returns the build result.
func bootWithProviderPlugin(t *testing.T, name string, runtime map[string]any) *BuildResult {
	t.Helper()
	if runtime == nil {
		runtime = map[string]any{}
	}
	if _, ok := runtime["capabilities"]; !ok {
		runtime["capabilities"] = []string{"providers"}
	}
	env := map[string]string{
		bootFakeEnvPluginName: name,
		bootFakeEnvProvider:   "1",
	}
	if extra, ok := runtime["env"].(map[string]string); ok {
		maps.Copy(env, extra)
	}
	runtime["env"] = env
	return bootWithFakePlugin(t, name, runtime)
}

func collectProviderChunks(t *testing.T, out <-chan provider.Chunk) []provider.Chunk {
	t.Helper()
	var chunks []provider.Chunk
	for {
		select {
		case chunk, ok := <-out:
			if !ok {
				return chunks
			}
			chunks = append(chunks, chunk)
		case <-time.After(10 * time.Second):
			t.Fatal("provider stream did not close")
		}
	}
}

func TestBootExtensionProviderStreamsEndToEnd(t *testing.T) {
	res := bootWithProviderPlugin(t, "providerdemo", nil)
	if res.ProviderResolver == nil {
		t.Fatal("BuildRuntime returned no ProviderResolver")
	}

	// The merged catalog carries the sidecar's provider next to the config's.
	var found *provider.Descriptor
	for _, d := range res.ProviderResolver.Catalog() {
		if d.Ref == "plugin/providerdemo/fake/x" {
			copy := d
			found = &copy
		}
	}
	if found == nil {
		t.Fatalf("merged catalog = %v, want plugin/providerdemo/fake/x", res.ProviderResolver.Catalog())
	}
	if found.DisplayName != "Boot Fake" || found.Model != "x" || !found.Tools || !found.Reasoning {
		t.Fatalf("sidecar descriptor = %+v", found)
	}

	p, err := res.ProviderResolver.Resolve(provider.Selection{Ref: "plugin/providerdemo/fake/x"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Name() != "plugin" {
		t.Fatalf("Name() = %q", p.Name())
	}
	out, err := p.Stream(context.Background(), provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "say hi"}},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collectProviderChunks(t, out)
	if len(chunks) != 3 {
		t.Fatalf("chunks = %+v, want text, text, usage", chunks)
	}
	if chunks[0].Type != provider.ChunkText || chunks[0].Text != "fake-hello " ||
		chunks[1].Type != provider.ChunkText || chunks[1].Text != "fake-world" {
		t.Fatalf("text chunks = %+v", chunks[:2])
	}
	if chunks[2].Type != provider.ChunkUsage || chunks[2].Usage == nil ||
		chunks[2].Usage.TotalTokens != 12 || chunks[2].Usage.CacheHitTokens != 2 ||
		chunks[2].Usage.ReasoningTokens != 4 || chunks[2].Usage.FinishReason != "stop" {
		t.Fatalf("usage chunk = %+v", chunks[2])
	}

	// The base resolver still serves the config's own model.
	base, err := res.ProviderResolver.Resolve(provider.Selection{Ref: "test-model/x"})
	if err != nil {
		t.Fatalf("Resolve base: %v", err)
	}
	if base.Name() != "test-model" {
		t.Fatalf("base provider name = %q", base.Name())
	}
}

// writeRuntimeFixtureWithConflictingProvider writes the shared fixture plus a
// config provider whose synthesized ref matches the fake sidecar's ref.
func writeRuntimeFixtureWithConflictingProvider(t *testing.T, dir, name string) {
	t.Helper()
	writeRuntimeFixture(t, dir)
	appendRuntimeFixture(t, dir, fmt.Sprintf(`
[[providers]]
name = "plugin"
kind = "openai"
base_url = "https://example.invalid"
model = "%s/fake/x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`, name))
}

func appendRuntimeFixture(t *testing.T, dir, extra string) {
	t.Helper()
	path := dir + "/reasonix.toml"
	existing, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := os.WriteFile(path, append(existing, []byte(extra)...), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestBootFailsOnUnclaimedExtensionProviderConflict(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	name := "conflicter"
	writeRuntimeFixtureWithConflictingProvider(t, dir, name)
	installBootFakePlugin(t, config.ReasonixHomeDir(), name, map[string]any{
		"capabilities": []string{"providers"},
		"env": map[string]string{
			bootFakeEnvPluginName: name,
			bootFakeEnvProvider:   "1",
		},
	})

	_, err := BuildRuntime(context.Background(), Options{})
	if err == nil {
		t.Fatal("BuildRuntime succeeded with an unclaimed provider conflict")
	}
	var conflictErr *providerext.ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("error %v is not a providerext.ConflictError", err)
	}
	ref := "plugin/" + name + "/fake/x"
	if !strings.Contains(err.Error(), ref) || !strings.Contains(err.Error(), `"`+name+`"`) ||
		!strings.Contains(err.Error(), "provider:"+ref) {
		t.Fatalf("conflict error = %q, want ref, plugin, and slot named", err)
	}
}

func TestBootExtensionProviderConflictWithClaimSidecarWins(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	name := "claimerdemo"
	writeRuntimeFixtureWithConflictingProvider(t, dir, name)
	ref := "plugin/" + name + "/fake/x"
	res := bootWithProviderPlugin(t, name, map[string]any{
		"replaces": []string{"provider:" + ref},
	})

	var found *provider.Descriptor
	for _, d := range res.ProviderResolver.Catalog() {
		if d.Ref == ref {
			copy := d
			found = &copy
		}
	}
	if found == nil {
		t.Fatalf("merged catalog = %v, want %s", res.ProviderResolver.Catalog(), ref)
	}
	if found.DisplayName != "Boot Fake" {
		t.Fatalf("contested descriptor = %+v, want the claiming sidecar's entry", found)
	}

	p, err := res.ProviderResolver.Resolve(provider.Selection{Ref: ref})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	out, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collectProviderChunks(t, out)
	if len(chunks) != 3 || chunks[0].Text != "fake-hello " {
		t.Fatalf("chunks = %+v, want the sidecar's stream", chunks)
	}
}
