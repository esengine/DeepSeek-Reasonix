package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/muesli/cancelreader"
	"golang.org/x/term"

	"reasonix/internal/base/textutil"
	"reasonix/internal/platform/remote"
)

func terminalChallengePrompt(ctx context.Context, q remote.Challenge) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("remote: keyboard-interactive authentication requires a terminal")
	}
	q.Question = renderTerminalChallenge(os.Stderr, q)
	if !q.AnswerRequired {
		return "", nil
	}
	reader, err := cancelreader.NewReader(os.Stdin)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, state)
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			reader.Cancel()
		case <-stop:
		}
	}()
	defer func() { close(stop); <-stopped }()
	terminal := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{reader, os.Stderr}, q.Question)
	var answer string
	if q.Echo {
		answer, err = terminal.ReadLine()
	} else {
		answer, err = terminal.ReadPassword(q.Question)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if errors.Is(err, io.EOF) {
		return "", context.Canceled
	}
	return answer, err
}

func renderTerminalChallenge(w io.Writer, q remote.Challenge) string {
	fmt.Fprintln(w, textutil.SanitizeDisplay(q.Host))
	if q.Name != "" {
		fmt.Fprintln(w, textutil.StripHiddenControls(q.Name))
	}
	if q.Instruction != "" {
		fmt.Fprintln(w, textutil.StripHiddenControls(q.Instruction))
	}
	return textutil.StripHiddenControls(q.Question)
}
