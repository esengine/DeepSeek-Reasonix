package cli

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/term"

	"reasonix/internal/i18n"
)

// errCancelled is returned by selectOne when the user aborts (q or Ctrl-C).
var errCancelled = errors.New("selection cancelled")

type menuItem struct {
	name string
	desc string
}

// selectOne renders an interactive single-choice menu navigated with the arrow
// keys (or j/k), confirmed with Enter, aborted with q or Ctrl-C. It puts the
// terminal in raw mode, so it requires a TTY (callers gate on isInteractive).
func selectOne(label string, items []menuItem) (int, error) {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return 0, err
	}
	defer term.Restore(fd, old)

	w := os.Stdout
	fmt.Fprintf(w, "%s %s  %s\r\n\r\n", accent("▌"), bold(label), dim(i18n.M.SelectOneHint))

	sel := 0
	render := func() {
		for i, it := range items {
			name := fmt.Sprintf("%-10s", it.name)
			if i == sel {
				fmt.Fprintf(w, "\r\033[K%s\r\n", reverse(fmt.Sprintf(" ❯ %s %s ", name, it.desc)))
			} else {
				fmt.Fprintf(w, "\r\033[K   %s %s\r\n", name, dim(it.desc))
			}
		}
	}
	render()

	buf := make([]byte, 8)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return 0, err
		}
		k := buf[:n]
		switch {
		case k[0] == '\r' || k[0] == '\n':
			fmt.Fprint(w, "\r\n")
			return sel, nil
		case k[0] == 3 || k[0] == 'q': // Ctrl-C or q
			fmt.Fprint(w, "\r\n")
			return 0, errCancelled
		case len(k) >= 3 && k[0] == 27 && k[1] == '[' && k[2] == 'A': // up
			if sel > 0 {
				sel--
			}
		case len(k) >= 3 && k[0] == 27 && k[1] == '[' && k[2] == 'B': // down
			if sel < len(items)-1 {
				sel++
			}
		case k[0] == 'k':
			if sel > 0 {
				sel--
			}
		case k[0] == 'j':
			if sel < len(items)-1 {
				sel++
			}
		default:
			continue // ignore other keys, no redraw
		}
		fmt.Fprintf(w, "\033[%dA", len(items)) // move back up to the first item
		render()
	}
}

// selectMany renders an interactive multi-choice menu: arrow keys (or j/k) move,
// Space toggles, Enter confirms (at least one required), q/Ctrl-C aborts. It
// returns the checked indices in order and requires a TTY.
func selectMany(label string, items []menuItem) ([]int, error) {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	defer term.Restore(fd, old)

	w := os.Stdout
	fmt.Fprintf(w, "%s %s  %s\r\n\r\n", accent("▌"), bold(label), dim(i18n.M.SelectManyHint))

	cur := 0
	checked := make([]bool, len(items))
	render := func() {
		for i, it := range items {
			box := "[ ]"
			if checked[i] {
				box = "[x]"
			}
			name := fmt.Sprintf("%-14s", it.name)
			if i == cur {
				fmt.Fprintf(w, "\r\033[K%s\r\n", reverse(fmt.Sprintf(" ❯ %s %s %s ", box, name, it.desc)))
			} else {
				fmt.Fprintf(w, "\r\033[K   %s %s %s\r\n", box, name, dim(it.desc))
			}
		}
	}
	render()

	buf := make([]byte, 8)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return nil, err
		}
		k := buf[:n]
		switch {
		case k[0] == '\r' || k[0] == '\n':
			var out []int
			for i, c := range checked {
				if c {
					out = append(out, i)
				}
			}
			if len(out) == 0 {
				continue // need at least one selection
			}
			fmt.Fprint(w, "\r\n")
			return out, nil
		case k[0] == 3 || k[0] == 'q':
			fmt.Fprint(w, "\r\n")
			return nil, errCancelled
		case k[0] == ' ':
			checked[cur] = !checked[cur]
		case len(k) >= 3 && k[0] == 27 && k[1] == '[' && k[2] == 'A':
			if cur > 0 {
				cur--
			}
		case len(k) >= 3 && k[0] == 27 && k[1] == '[' && k[2] == 'B':
			if cur < len(items)-1 {
				cur++
			}
		case k[0] == 'k':
			if cur > 0 {
				cur--
			}
		case k[0] == 'j':
			if cur < len(items)-1 {
				cur++
			}
		default:
			continue
		}
		fmt.Fprintf(w, "\033[%dA", len(items))
		render()
	}
}
