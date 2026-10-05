// render_remote.go — the [remote] section of a rendered config: the machines
// this user can reach and, under each, the projects worked on over there.
package config

import (
	"fmt"
	"strings"
)

// renderRemoteSection writes [remote] and its hosts. Split out because the
// section is one subject, and because the renderer it came from is already the
// longest function in the package.
func renderRemoteSection(b *strings.Builder, r RemoteConfig) {
	if !r.ImportSSHConfig && len(r.Hosts) == 0 {
		return
	}
	b.WriteString("[remote]   # SSH remote hosts; user/global only, ./reasonix.toml cannot override\n")
	if r.ImportSSHConfig {
		b.WriteString("import_ssh_config = true   # surface ~/.ssh/config aliases in `reasonix remote import`\n")
	}
	for _, h := range r.Hosts {
		b.WriteString("\n[[remote.hosts]]\n")
		fmt.Fprintf(b, "name = %q\n", h.Name)
		fmt.Fprintf(b, "host = %q\n", h.Host)
		if h.Port > 0 {
			fmt.Fprintf(b, "port = %d\n", h.Port)
		}
		if h.User != "" {
			fmt.Fprintf(b, "user = %q\n", h.User)
		}
		if h.IdentityFile != "" {
			fmt.Fprintf(b, "identity_file = %q   # key file path; Reasonix never stores key material\n", h.IdentityFile)
		}
		if h.PassphraseEnv != "" {
			fmt.Fprintf(b, "passphrase_env = %q   # env var name; value lives in Reasonix's global .env\n", h.PassphraseEnv)
		}
		if h.PasswordEnv != "" {
			fmt.Fprintf(b, "password_env = %q   # env var name; value lives in Reasonix's global .env\n", h.PasswordEnv)
		}
		if h.ProxyJump != "" {
			fmt.Fprintf(b, "proxy_jump = %q   # OpenSSH ProxyJump chain\n", h.ProxyJump)
		}
		if h.Workspace != "" {
			fmt.Fprintf(b, "workspace = %q   # default remote workspace dir\n", h.Workspace)
		}
		if len(h.Workspaces) > 0 {
			fmt.Fprintf(b, "workspaces = %s   # the other projects on this machine\n", renderStringArray(h.Workspaces))
		}
		if h.ServeInstall != "" {
			fmt.Fprintf(b, "serve_install = %q   # auto|npm|upload|never\n", h.ServeInstall)
		}
		if h.UseSSHConfig {
			b.WriteString("use_ssh_config = true   # layer ~/.ssh/config values under unset fields\n")
		}
		if h.Disabled {
			b.WriteString("disabled = true   # kept in the book but not dialed\n")
		}
		for _, f := range h.Forwards {
			b.WriteString("\n[[remote.hosts.forwards]]\n")
			fmt.Fprintf(b, "type = %q   # local (-L) | remote (-R)\n", f.Type)
			fmt.Fprintf(b, "bind = %q\n", f.Bind)
			fmt.Fprintf(b, "target = %q\n", f.Target)
		}
	}
	b.WriteString("\n")
}
