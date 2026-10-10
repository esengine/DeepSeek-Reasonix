package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/platform/wsl"
)

func remoteWSLCLI(args []string) int {
	if len(args) == 0 || (args[0] != "list" && args[0] != "test") || (args[0] == "list" && len(args) != 1) || (args[0] == "test" && len(args) != 2) {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote wsl list | reasonix remote wsl test <distribution>")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if args[0] == "list" {
		list, err := wsl.List(ctx, wsl.System())
		if err != nil {
			return wslFailure(err)
		}
		if len(list) == 0 {
			fmt.Println("no WSL distributions are installed")
		}
		for _, d := range list {
			mark := " "
			if d.Default {
				mark = "*"
			}
			fmt.Printf("%s %s (WSL %d)\n", mark, d.Name, d.Version)
		}
		return 0
	}
	p, err := wsl.Check(ctx, wsl.System(), args[1])
	if err != nil {
		return wslFailure(err)
	}
	fmt.Printf("  distribution  %s (WSL %d)\n  machine       linux/%s, user %s\n", p.Distro.Name, p.Distro.Version, p.Arch, p.User)
	return 0
}

func wslFailure(err error) int {
	fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
	if errors.Is(err, wsl.ErrUnavailable) {
		return 3
	}
	return 1
}
