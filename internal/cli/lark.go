package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"reasonix/internal/config"
	"reasonix/internal/larkbot"
)

func runLark(args []string) int {
	fs := flag.NewFlagSet("lark", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config load:", err)
		return 1
	}

	if !cfg.Lark.Enabled() {
		fmt.Fprintln(os.Stderr, "lark bot is not configured — set app_id and app_secret in [lark] section of reasonix.toml")
		return 1
	}

	bot, err := larkbot.New(larkbot.Options{
		AppID:     cfg.Lark.ResolvedAppID(),
		AppSecret: cfg.Lark.ResolvedAppSecret(),
		Cfg:       &cfg.Lark,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "lark bot:", err)
		return 1
	}
	defer bot.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Println("reasonix lark — connected to Lark bot")
	if err := bot.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "lark bot:", err)
		return 1
	}
	return 0
}
