package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"reasonix/internal/i18n"
)

// errCancelled is returned by selectOne when the user aborts (q or Ctrl-C).
var errCancelled = errors.New("selection cancelled")

type menuItem struct {
	name string
	desc string
}

// termHeight returns the terminal's row count, falling back to 24 on error.
func termHeight(fd int) int {
	_, h, err := term.GetSize(fd)
	if err != nil || h <= 0 {
		return 24
	}
	return h
}

// maxViewport calculates how many menu rows fit in the terminal after subtracting
// the header (2 lines) and the hint/footer (1 line), leaving at least 5 rows.
func maxViewport(totalItems, termRows int) int {
	avail := termRows - 3 // header + blank + footer
	if avail < 5 {
		avail = 5
	}
	if totalItems < avail {
		return totalItems
	}
	return avail
}

// renderSearchBar draws the search input line when searching is active.
func renderSearchBar(w *os.File, query string) {
	fmt.Fprintf(w, "\r\033[K%s %s\n", accent("🔍"), query+"_")
}

// filterMenuItems returns items whose name or desc contain the query (case-insensitive).
func filterMenuItems(items []menuItem, query string) []menuItem {
	if query == "" {
		return items
	}
	lq := strings.ToLower(query)
	var out []menuItem
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.name), lq) || strings.Contains(strings.ToLower(it.desc), lq) {
			out = append(out, it)
		}
	}
	return out
}

// selectOne renders an interactive single-choice menu navigated with the arrow
// keys (or j/k), confirmed with Enter, aborted with q or Ctrl-C. It puts the
// terminal in raw mode, so it requires a TTY (callers gate on isInteractive).
// When the item list exceeds the terminal height, only a viewport-sized window
// is shown, with scroll indicators. Pressing '/' enters search mode to filter
// items by keyword.
func selectOne(label string, items []menuItem) (int, error) {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return 0, err
	}
	defer term.Restore(fd, old)

	w := os.Stdout
	th := termHeight(fd)

	// search state
	searching := false
	searchQuery := ""
	filtered := items                    // indices into original items
	filterIdx := make([]int, len(items)) // maps filtered position → original index
	for i := range items {
		filterIdx[i] = i
	}

	sel := 0
	scroll := 0

	render := func() {
		n := len(filtered)
		if n == 0 {
			return
		}
		// clamp viewport
		vp := maxViewport(n, th)
		// adjust scroll to keep sel visible
		if sel < scroll {
			scroll = sel
		}
		if sel >= scroll+vp {
			scroll = sel - vp + 1
		}
		if scroll < 0 {
			scroll = 0
		}

		// scroll-up indicator
		if scroll > 0 {
			fmt.Fprintf(w, "\r\033[K%s\n", dim(fmt.Sprintf(i18n.M.SelectMoreAboveFmt, scroll)))
		} else {
			fmt.Fprintf(w, "\r\033[K\r\n")
		}

		// menu rows
		end := scroll + vp
		if end > n {
			end = n
		}
		for i := scroll; i < end; i++ {
			it := filtered[i]
			name := fmt.Sprintf("%-10s", it.name)
			if i == sel {
				fmt.Fprintf(w, "\r\033[K%s\r\n", reverse(fmt.Sprintf(" ❯ %s %s ", name, it.desc)))
			} else {
				fmt.Fprintf(w, "\r\033[K   %s %s\r\n", name, dim(it.desc))
			}
		}

		// scroll-down indicator
		if end < n {
			fmt.Fprintf(w, "\r\033[K%s\n", dim(fmt.Sprintf(i18n.M.SelectMoreBelowFmt, n-end)))
		} else {
			fmt.Fprintf(w, "\r\033[K\r\n")
		}
	}

	// initial header + search bar area
	drawHeader := func() {
		if searching {
			fmt.Fprintf(w, "\r\033[K%s %s  %s\r\n\r\n", accent("▌"), bold(label), dim(i18n.M.SelectSearchHint))
			renderSearchBar(w, searchQuery)
		} else {
			fmt.Fprintf(w, "\r\033[K%s %s  %s\r\n\r\n", accent("▌"), bold(label), dim(i18n.M.SelectOneHint))
		}
	}

	// total lines to move up for redraw: header(2) + searchbar(if searching)(1) + viewport items + scroll indicators
	totalLines := func() int {
		vp := maxViewport(len(filtered), th)
		lines := 2 + vp + 2 // header + blank + items + top/bottom scroll indicators
		if searching {
			lines++ // search bar
		}
		return lines
	}

	drawHeader()
	render()

	buf := make([]byte, 8)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return 0, err
		}
		k := buf[:n]

		if searching {
			switch {
			case k[0] == 27: // Esc — exit search
				searching = false
				searchQuery = ""
				filtered = items
				filterIdx = make([]int, len(items))
				for i := range items {
					filterIdx[i] = i
				}
				sel = 0
				scroll = 0
				// redraw: move up and redraw everything
				fmt.Fprintf(w, "\033[%dA", totalLines())
				drawHeader()
				render()
			case k[0] == '\r' || k[0] == '\n':
				if len(filtered) > 0 {
					fmt.Fprint(w, "\r\n")
					return filterIdx[sel], nil
				}
			case k[0] == 127 || k[0] == 8: // backspace
				if len(searchQuery) > 0 {
					searchQuery = searchQuery[:len(searchQuery)-1]
					filtered = filterMenuItems(items, searchQuery)
					filterIdx = filterIndices(items, searchQuery)
					sel = 0
					scroll = 0
					fmt.Fprintf(w, "\033[%dA", totalLines())
					drawHeader()
					render()
				}
			case k[0] == 3: // Ctrl-C
				fmt.Fprint(w, "\r\n")
				return 0, errCancelled
			case k[0] >= 32 && k[0] < 127: // printable
				searchQuery += string(k[0])
				filtered = filterMenuItems(items, searchQuery)
				filterIdx = filterIndices(items, searchQuery)
				sel = 0
				scroll = 0
				fmt.Fprintf(w, "\033[%dA", totalLines())
				drawHeader()
				render()
			default:
				continue
			}
			continue
		}

		switch {
		case k[0] == '\r' || k[0] == '\n':
			fmt.Fprint(w, "\r\n")
			return filterIdx[sel], nil
		case k[0] == 3 || k[0] == 'q': // Ctrl-C or q
			fmt.Fprint(w, "\r\n")
			return 0, errCancelled
		case k[0] == '/': // enter search mode
			searching = true
			searchQuery = ""
			fmt.Fprintf(w, "\033[%dA", totalLines())
			drawHeader()
			render()
		case len(k) >= 3 && k[0] == 27 && k[1] == '[' && k[2] == 'A': // up
			if sel > 0 {
				sel--
			}
		case len(k) >= 3 && k[0] == 27 && k[1] == '[' && k[2] == 'B': // down
			if sel < len(filtered)-1 {
				sel++
			}
		case k[0] == 'k':
			if sel > 0 {
				sel--
			}
		case k[0] == 'j':
			if sel < len(filtered)-1 {
				sel++
			}
		default:
			continue // ignore other keys, no redraw
		}
		fmt.Fprintf(w, "\033[%dA", totalLines())
		drawHeader()
		render()
	}
}

// selectMany renders an interactive multi-choice menu: arrow keys (or j/k) move,
// Space toggles, Enter confirms (at least one required), q/Ctrl-C aborts. It
// returns the checked indices in order and requires a TTY. When the item list
// exceeds the terminal height, only a viewport-sized window is shown. Pressing
// '/' enters search mode to filter items by keyword.
func selectMany(label string, items []menuItem) ([]int, error) {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	defer term.Restore(fd, old)

	w := os.Stdout
	th := termHeight(fd)

	// search state
	searching := false
	searchQuery := ""
	filtered := items
	filterIdx := make([]int, len(items))
	for i := range items {
		filterIdx[i] = i
	}

	cur := 0
	checked := make([]bool, len(items))
	scroll := 0

	render := func() {
		n := len(filtered)
		if n == 0 {
			return
		}
		vp := maxViewport(n, th)
		if cur < scroll {
			scroll = cur
		}
		if cur >= scroll+vp {
			scroll = cur - vp + 1
		}
		if scroll < 0 {
			scroll = 0
		}

		if scroll > 0 {
			fmt.Fprintf(w, "\r\033[K%s\n", dim(fmt.Sprintf(i18n.M.SelectMoreAboveFmt, scroll)))
		} else {
			fmt.Fprintf(w, "\r\033[K\r\n")
		}

		end := scroll + vp
		if end > n {
			end = n
		}
		for i := scroll; i < end; i++ {
			it := filtered[i]
			origIdx := filterIdx[i]
			box := "[ ]"
			if checked[origIdx] {
				box = "[x]"
			}
			name := fmt.Sprintf("%-14s", it.name)
			if i == cur {
				fmt.Fprintf(w, "\r\033[K%s\r\n", reverse(fmt.Sprintf(" ❯ %s %s %s ", box, name, it.desc)))
			} else {
				fmt.Fprintf(w, "\r\033[K   %s %s %s\r\n", box, name, dim(it.desc))
			}
		}

		if end < n {
			fmt.Fprintf(w, "\r\033[K%s\n", dim(fmt.Sprintf(i18n.M.SelectMoreBelowFmt, n-end)))
		} else {
			fmt.Fprintf(w, "\r\033[K\r\n")
		}
	}

	totalLines := func() int {
		vp := maxViewport(len(filtered), th)
		lines := 2 + vp + 2
		if searching {
			lines++
		}
		return lines
	}

	drawHeader := func() {
		if searching {
			fmt.Fprintf(w, "\r\033[K%s %s  %s\r\n\r\n", accent("▌"), bold(label), dim(i18n.M.SelectSearchHint))
			renderSearchBar(w, searchQuery)
		} else {
			fmt.Fprintf(w, "\r\033[K%s %s  %s\r\n\r\n", accent("▌"), bold(label), dim(i18n.M.SelectManyHint))
		}
	}

	drawHeader()
	render()

	buf := make([]byte, 8)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return nil, err
		}
		k := buf[:n]

		if searching {
			switch {
			case k[0] == 27: // Esc — exit search
				searching = false
				searchQuery = ""
				filtered = items
				filterIdx = make([]int, len(items))
				for i := range items {
					filterIdx[i] = i
				}
				cur = 0
				scroll = 0
				fmt.Fprintf(w, "\033[%dA", totalLines())
				drawHeader()
				render()
			case k[0] == '\r' || k[0] == '\n':
				var out []int
				for i, c := range checked {
					if c {
						out = append(out, i)
					}
				}
				if len(out) == 0 {
					continue
				}
				fmt.Fprint(w, "\r\n")
				return out, nil
			case k[0] == ' ':
				if len(filtered) > 0 {
					origIdx := filterIdx[cur]
					checked[origIdx] = !checked[origIdx]
				}
			case k[0] == 127 || k[0] == 8: // backspace
				if len(searchQuery) > 0 {
					searchQuery = searchQuery[:len(searchQuery)-1]
					filtered = filterMenuItems(items, searchQuery)
					filterIdx = filterIndices(items, searchQuery)
					cur = 0
					scroll = 0
					fmt.Fprintf(w, "\033[%dA", totalLines())
					drawHeader()
					render()
				}
			case k[0] == 3: // Ctrl-C
				fmt.Fprint(w, "\r\n")
				return nil, errCancelled
			case k[0] >= 32 && k[0] < 127:
				searchQuery += string(k[0])
				filtered = filterMenuItems(items, searchQuery)
				filterIdx = filterIndices(items, searchQuery)
				cur = 0
				scroll = 0
				fmt.Fprintf(w, "\033[%dA", totalLines())
				drawHeader()
				render()
			case len(k) >= 3 && k[0] == 27 && k[1] == '[' && k[2] == 'A':
				if cur > 0 {
					cur--
				}
			case len(k) >= 3 && k[0] == 27 && k[1] == '[' && k[2] == 'B':
				if cur < len(filtered)-1 {
					cur++
				}
			case k[0] == 'k':
				if cur > 0 {
					cur--
				}
			case k[0] == 'j':
				if cur < len(filtered)-1 {
					cur++
				}
			default:
				continue
			}
			fmt.Fprintf(w, "\033[%dA", totalLines())
			drawHeader()
			render()
			continue
		}

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
		case k[0] == '/': // enter search mode
			searching = true
			searchQuery = ""
			fmt.Fprintf(w, "\033[%dA", totalLines())
			drawHeader()
			render()
		case k[0] == ' ':
			if len(filtered) > 0 {
				origIdx := filterIdx[cur]
				checked[origIdx] = !checked[origIdx]
			}
		case len(k) >= 3 && k[0] == 27 && k[1] == '[' && k[2] == 'A':
			if cur > 0 {
				cur--
			}
		case len(k) >= 3 && k[0] == 27 && k[1] == '[' && k[2] == 'B':
			if cur < len(filtered)-1 {
				cur++
			}
		case k[0] == 'k':
			if cur > 0 {
				cur--
			}
		case k[0] == 'j':
			if cur < len(filtered)-1 {
				cur++
			}
		default:
			continue
		}
		fmt.Fprintf(w, "\033[%dA", totalLines())
		drawHeader()
		render()
	}
}

// filterIndices returns the original indices of items matching query.
func filterIndices(items []menuItem, query string) []int {
	if query == "" {
		out := make([]int, len(items))
		for i := range items {
			out[i] = i
		}
		return out
	}
	lq := strings.ToLower(query)
	var out []int
	for i, it := range items {
		if strings.Contains(strings.ToLower(it.name), lq) || strings.Contains(strings.ToLower(it.desc), lq) {
			out = append(out, i)
		}
	}
	return out
}
