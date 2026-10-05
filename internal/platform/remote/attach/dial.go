package attach

import (
	"fmt"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/remote"
)

// Prompts is the interactivity a frontend supplies — a terminal in the CLI, a
// dialog in Studio. Both may be nil: no Secret means non-interactive methods
// only, and no HostKey means a first-seen key is rejected rather than asked
// about, which is the safe direction for an unattended caller.
type Prompts struct {
	Secret  remote.SecretPrompt
	HostKey remote.HostKeyPrompt
}

// Dial resolves a configured host — layering ~/.ssh/config under it where the
// entry asks for that — and returns its supervised connection, not yet started.
func Dial(cfg *config.Config, nameOrTarget string, prompts Prompts) (*remote.Client, error) {
	sshCfg, err := remote.LoadUserSSHConfig()
	if err != nil {
		return nil, fmt.Errorf("load SSH config: %w", err)
	}
	host, err := remote.ResolveHost(cfg, nameOrTarget, sshCfg)
	if err != nil {
		return nil, err
	}
	jumps, err := remote.ResolveJumpHosts(cfg, host.ProxyJump, sshCfg)
	if err != nil {
		return nil, err
	}
	hops := make([]remote.JumpHostOptions, 0, len(jumps))
	for _, jump := range jumps {
		hops = append(hops, remote.JumpHostOptions{Host: jump, Auth: AuthFor(jump, prompts.Secret)})
	}
	// A misconfigured proxy is surfaced, not silently bypassed: a proxy is often
	// a policy requirement, and quietly dialing direct would route the
	// connection around it.
	dialer, err := netclient.NewStreamDialer(cfg.NetworkProxySpec())
	if err != nil {
		return nil, fmt.Errorf("remote: network proxy is misconfigured: %w", err)
	}
	return remote.New(remote.Options{
		Host:      host,
		Auth:      AuthFor(host, prompts.Secret),
		JumpHosts: hops,
		HostKeys:  &remote.HostKeyPolicy{Prompt: prompts.HostKey},
		Dialer:    dialer,
	})
}

// AuthFor binds a host's configured credential references to the resolver, and
// leaves the prompt as the fallback for what no env var names.
func AuthFor(host remote.ResolvedHost, prompt remote.SecretPrompt) remote.AuthOptions {
	auth := remote.AuthOptions{SecretPrompt: prompt}
	if env := host.PassphraseEnv; env != "" {
		auth.Passphrase = func() (string, error) { return config.ResolveCredential(env).Value, nil }
	}
	if env := host.PasswordEnv; env != "" {
		auth.Password = func() (string, error) { return config.ResolveCredential(env).Value, nil }
	}
	return auth
}
