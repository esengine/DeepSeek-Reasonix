package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/base/textutil"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/ext/pluginspec"
)

// mcpTrustForCLI connects a server and records the tool definitions it holds
// back as approved; a variable so a test can stand in for the connection.
var mcpTrustForCLI = func(spec plugin.Spec, digest string) ([]string, error) {
	return trustHeldMCPTools(spec, digest, os.Stdin, os.Stdout, isInteractive())
}

func mcpTrustCLI(args []string) int {
	var digest string
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--digest" && i+1 < len(args) {
			i++
			digest = strings.TrimSpace(args[i])
			continue
		}
		rest = append(rest, args[i])
	}
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: reasonix mcp trust <name> [--digest <hex>]")
		return 2
	}
	name, workspace := strings.TrimSpace(rest[0]), mcpCLIWorkspaceRoot()
	cfg, err := config.LoadForRoot(workspace)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var entry config.PluginEntry
	found := false
	for _, configured := range cfg.Plugins {
		if configured.Name == name {
			entry, found = configured, true
			break
		}
	}
	if !found {
		fmt.Fprintf(os.Stderr, "no MCP server named %q in config\n", name)
		return 1
	}
	client, err := netclient.NewHTTPClient(cfg.NetworkProxySpec(), netclient.TransportOptions{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	specs := pluginspec.ForRootWithOptions([]config.PluginEntry{entry}, workspace, pluginspec.Options{
		DefaultCallTimeout: 30 * time.Second, ConfigSource: string(entry.Source),
		StateHome: config.ReasonixHomeDir(), Network: true, OAuthHTTPClient: client,
	})
	if len(specs) != 1 {
		fmt.Fprintf(os.Stderr, "could not build MCP specification for %q\n", name)
		return 1
	}
	accepted, err := mcpTrustForCLI(specs[0], digest)
	switch {
	case errors.Is(err, plugin.ErrNoHeldTools):
		fmt.Printf("MCP server %q has no held tool definitions\n", name)
		return 0
	case err != nil:
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("trusted %d tool definition(s) of MCP server %q: %s — reconnect it in the current session or start a new one\n",
		len(accepted), name, textutil.ShownLocator(strings.Join(accepted, ", ")))
	return 0
}

func trustHeldMCPTools(spec plugin.Spec, digest string, in io.Reader, out io.Writer, interactive bool) ([]string, error) {
	if !spec.ServerAuthorized() {
		return nil, fmt.Errorf("MCP server %q is not approved to run yet; run `reasonix mcp enable %s` first", spec.Name, spec.Name)
	}
	host := plugin.NewHost()
	defer host.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if _, err := host.EnsureConnected(ctx, spec); err != nil {
		return nil, err
	}
	held, _ := host.HeldTools(spec.Name)
	if len(held) == 0 {
		return nil, plugin.ErrNoHeldTools
	}
	set := plugin.HeldDigest(held)
	fmt.Fprintf(out, "MCP server %q holds %d tool definition(s):\n", textutil.ShownLocator(spec.Name), len(held))
	for _, h := range held {
		fmt.Fprintf(out, "  %s  %s  %s\n", textutil.ShownLocator(h.RawName), h.Class, h.Digest)
	}
	fmt.Fprintf(out, "digest: %s\n", set)
	switch {
	case digest != "":
		if digest != set {
			return nil, plugin.ErrHeldDigestMismatch
		}
	case interactive:
		fmt.Fprint(out, "Trust these definitions? [y/N] ")
		line, _ := bufio.NewReader(in).ReadString('\n')
		if answer := strings.ToLower(strings.TrimSpace(line)); answer != "y" && answer != "yes" {
			return nil, errors.New("not trusted")
		}
	default:
		return nil, errors.New("trusting needs a terminal to confirm, or --digest <hex> naming the digest shown above")
	}
	return host.AcceptHeldTools(spec.Name, set)
}
