package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/account"
)

// An account is for the parts of Reasonix that are inherently networked — the
// forum, following up a crash report, publishing a skill. Running the agent
// never needs one, so these commands are the only place it comes up.

func accountClient(version string) (*account.Client, error) {
	// Same proxy the user configured for models: a machine that needs one to
	// reach DeepSeek needs it to reach id.reasonix.io too.
	spec := netclient.ProxySpec{Mode: netclient.ModeAuto}
	if cfg, err := config.Load(); err == nil && cfg != nil {
		spec = cfg.NetworkProxySpec()
	}
	httpClient, err := netclient.NewHTTPClient(spec, netclient.TransportOptions{})
	if err != nil {
		return nil, err
	}
	return account.New(os.Getenv("REASONIX_ACCOUNTS_URL"), "reasonix-cli/"+version, httpClient), nil
}

// accountCommand is the one dispatch seam for the account verbs, so the CLI's
// top-level switch grows by a single branch.
func accountCommand(cmd string, args []string, version string) int {
	switch cmd {
	case "login":
		return loginCommand(args, version)
	case "whoami":
		return whoamiCommand(args, version)
	default:
		return logoutCommand(args, version)
	}
}

func loginCommand(args []string, version string) int {
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "login: unexpected argument %q\n", args[0])
		return 2
	}
	client, err := accountClient(version)
	if err != nil {
		fmt.Fprintln(os.Stderr, "login:", err)
		return 1
	}
	// Ctrl-C at the waiting prompt cancels the sign-in, not the shell.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if token := account.Token(); token != "" {
		if user, err := client.Me(ctx, token); err == nil {
			fmt.Printf("Already signed in as %s <%s>.\n", user.Label(), user.Email)
			fmt.Println("Run `reasonix logout` first to sign in as someone else.")
			return 0
		}
	}

	grant, err := client.StartDevice(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "login:", err)
		return 1
	}
	fmt.Printf("Open %s and enter this code:\n\n    %s\n\n", grant.VerificationURI, grant.UserCode)
	fmt.Println("Waiting for approval… (Ctrl+C to cancel)")

	token, user, err := client.WaitForApproval(ctx, grant)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "\nlogin: cancelled.")
			return 130
		}
		fmt.Fprintln(os.Stderr, "login:", err)
		return 1
	}
	path, err := account.SaveToken(token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "login: signed in but could not store the token:", err)
		return 1
	}
	who := "your account"
	if user != nil {
		who = fmt.Sprintf("%s <%s>", user.Label(), user.Email)
	}
	fmt.Printf("Signed in as %s\n", who)
	if path != "" {
		fmt.Printf("Token stored in %s\n", path)
	}
	return 0
}

func whoamiCommand(args []string, version string) int {
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "whoami: unexpected argument %q\n", args[0])
		return 2
	}
	token := account.Token()
	if token == "" {
		fmt.Fprintln(os.Stderr, "Not signed in. Run `reasonix login` — only needed for the forum and crash follow-ups.")
		return 1
	}
	client, err := accountClient(version)
	if err != nil {
		fmt.Fprintln(os.Stderr, "whoami:", err)
		return 1
	}
	user, err := client.Me(context.Background(), token)
	if err != nil {
		// A dead identity service must not look like a signed-out account:
		// the user would "fix" it by signing in again, which also fails.
		if errors.Is(err, account.ErrUnauthorized) {
			fmt.Fprintln(os.Stderr, "Your session has expired. Run `reasonix login` again.")
			return 1
		}
		fmt.Fprintln(os.Stderr, "whoami: cannot reach the identity service:", err)
		return 1
	}
	fmt.Printf("%s <%s>\n", user.Label(), user.Email)
	if handle := strings.TrimSpace(user.Handle); handle != "" && handle != user.Label() {
		fmt.Printf("Handle: %s\n", handle)
	}
	return 0
}

func logoutCommand(args []string, version string) int {
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "logout: unexpected argument %q\n", args[0])
		return 2
	}
	token := account.Token()
	if token == "" {
		fmt.Println("Not signed in.")
		return 0
	}
	// Revoking server-side is best effort: the local token must go regardless,
	// or an offline machine could never sign out.
	if client, err := accountClient(version); err == nil {
		if err := client.Logout(context.Background(), token); err != nil {
			fmt.Fprintln(os.Stderr, "logout: could not revoke the session remotely:", err)
		}
	}
	if err := account.ClearToken(); err != nil {
		fmt.Fprintln(os.Stderr, "logout:", err)
		return 1
	}
	fmt.Println("Signed out. Sessions, memory and configuration are untouched.")
	return 0
}
