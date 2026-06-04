package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
	"reasonix/internal/netclient"
	"reasonix/internal/update"
)

const updateHTTPTimeout = 30 * time.Second

func updateCommand(args []string, version string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "check only, do not download or apply")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Build an HTTP client that honours the user's proxy settings.
	c, err := updateHTTPClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), updateHTTPTimeout)
	defer cancel()

	info := update.Check(ctx, c, version)
	if info.Err != "" {
		fmt.Fprintf(os.Stderr, "%s %s\n", i18n.M.UpdateCheckFailed, info.Err)
		return 1
	}

	if !info.Available {
		fmt.Println(i18n.M.UpdateAlreadyLatest)
		return 0
	}

	fmt.Printf("%s %s → %s\n", i18n.M.UpdateAvailable, info.Current, info.Latest)
	if info.Prerelease {
		fmt.Println(i18n.M.UpdatePrereleaseHint)
	}

	if *dryRun {
		return 0
	}

	// Perform the update with progress output.
	fmt.Println(i18n.M.UpdateDownloading)
	err = update.Apply(ctx, c, func(phase string, received, total int64) {
		switch phase {
		case "downloading":
			if total > 0 {
				fmt.Fprintf(os.Stderr, "\r%s %d/%d KiB", i18n.M.UpdateProgress, received/1024, total/1024)
			} else {
				fmt.Fprintf(os.Stderr, "\r%s %d KiB", i18n.M.UpdateProgress, received/1024)
			}
		case "verifying":
			fmt.Fprintf(os.Stderr, "\r%s\n", i18n.M.UpdateVerifying)
		case "applying update":
			fmt.Fprintf(os.Stderr, "%s\n", i18n.M.UpdateApplying)
		case "done":
			fmt.Fprintf(os.Stderr, "\r%s\n", i18n.M.UpdateDone)
		}
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n%s %v\n", i18n.M.UpdateFailed, err)
		return 1
	}

	fmt.Println(i18n.M.UpdateSuccess)
	return 0
}

// updateHTTPClient builds an HTTP client respecting the user's proxy config.
func updateHTTPClient() (*http.Client, error) {
	spec := netclient.ProxySpec{Mode: netclient.ModeAuto}
	if cfg, err := config.Load(); err == nil {
		spec = cfg.NetworkProxySpec()
	}
	return netclient.NewHTTPClient(spec, updateHTTPTimeout, netclient.TransportOptions{})
}
