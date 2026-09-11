package config

import (
	"fmt"
	"strings"
)

func renderRemoteConfig(b *strings.Builder, c *Config, scope RenderScope) {
	if scope == RenderScopeProject || (!c.Remote.ImportSSHConfig && len(c.Remote.Hosts) == 0 && len(c.Remote.Projects) == 0) {
		return
	}
	b.WriteString("[remote]   # SSH remote hosts; user/global only, ./reasonix.toml cannot override\n")
	if c.Remote.ImportSSHConfig {
		b.WriteString("import_ssh_config = true   # surface ~/.ssh/config aliases in `reasonix remote import`\n")
	}
	for _, host := range c.Remote.Hosts {
		renderRemoteHost(b, host)
	}
	for _, project := range c.Remote.Projects {
		b.WriteString("\n[[remote.projects]]\n")
		fmt.Fprintf(b, "host_id = %q   # referenced [[remote.hosts]] name\n", project.HostID)
		fmt.Fprintf(b, "workspace = %q\n", project.Workspace)
		if project.Title != "" {
			fmt.Fprintf(b, "title = %q\n", project.Title)
		}
	}
	b.WriteString("\n")
}

func renderRemoteHost(b *strings.Builder, host RemoteHostEntry) {
	b.WriteString("\n[[remote.hosts]]\n")
	fmt.Fprintf(b, "name = %q\n", host.Name)
	fmt.Fprintf(b, "host = %q\n", host.Host)
	if host.Port > 0 {
		fmt.Fprintf(b, "port = %d\n", host.Port)
	}
	if host.User != "" {
		fmt.Fprintf(b, "user = %q\n", host.User)
	}
	if host.IdentityFile != "" {
		fmt.Fprintf(b, "identity_file = %q   # key file path; Reasonix never stores key material\n", host.IdentityFile)
	}
	if host.PassphraseEnv != "" {
		fmt.Fprintf(b, "passphrase_env = %q   # env var name; value lives in Reasonix's global .env\n", host.PassphraseEnv)
	}
	if host.PasswordEnv != "" {
		fmt.Fprintf(b, "password_env = %q   # env var name; value lives in Reasonix's global .env\n", host.PasswordEnv)
	}
	if host.ProxyJump != "" {
		fmt.Fprintf(b, "proxy_jump = %q   # OpenSSH ProxyJump chain\n", host.ProxyJump)
	}
	if host.Workspace != "" {
		fmt.Fprintf(b, "workspace = %q   # default remote workspace dir\n", host.Workspace)
	}
	if host.ServeInstall != "" {
		fmt.Fprintf(b, "serve_install = %q   # auto|npm|upload|never\n", host.ServeInstall)
	}
	if host.CredentialMode != "" {
		fmt.Fprintf(b, "credential_mode = %q   # remote (key on the host) | local-proxy (desktop holds the key; calls tunnel back)\n", host.CredentialMode)
	}
	if host.UseSSHConfig {
		b.WriteString("use_ssh_config = true   # layer ~/.ssh/config values under unset fields\n")
	}
	for _, forward := range host.Forwards {
		b.WriteString("\n[[remote.hosts.forwards]]\n")
		fmt.Fprintf(b, "type = %q   # local (-L) | remote (-R)\n", forward.Type)
		fmt.Fprintf(b, "bind = %q\n", forward.Bind)
		fmt.Fprintf(b, "target = %q\n", forward.Target)
	}
}
