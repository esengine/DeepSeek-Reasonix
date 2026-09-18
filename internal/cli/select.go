package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"reasonix/internal/i18n"
)

// errCancelled is returned by selectOne when the user aborts (q or Ctrl-C).
var errCancelled = errors.New("selection cancelled")

type menuItem struct {
	name string
	desc string
}

// termSize returns the terminal's column and row counts, falling back to the
// classic 80×24 for any dimension the terminal fails to report.
func termSize(fd int) (cols, rows int) {
	cols, rows, err := term.GetSize(fd)
	if err != nil || cols <= 0 {
		cols = 80
	}
	if err != nil || rows <= 0 {
		rows = 24
	}
	return cols, rows
}

// fixedLines returns the number of non-item lines rendered each frame:
// header label, blank separator, scroll-up indicator, scroll-down indicator.
// When searching is true the search bar adds one more line.
func fixedLines(searching bool) int {
	n := 4 // header + blank + scroll-up + scroll-down
	if searching {
		n++ // search bar
	}
	return n
}

// maxViewport calculates how many menu item rows fit after subtracting the
// fixed lines from the available terminal rows, leaving at least 1 row.
func maxViewport(totalItems, termRows int, searching bool) int {
	avail := max(termRows-fixedLines(searching), 1)
	if totalItems < avail {
		return totalItems
	}
	return avail
}

// menuFrame writes the lines of one selectOne/selectMany frame. Redraw moves
// the cursor up by the number of lines written, so every line must occupy
// exactly one terminal row: it starts on a cleared row at column 0, ends with
// CR LF, and is clipped to the terminal width so it cannot soft-wrap. A line
// that wraps takes an extra row, the cursor-up stops short of the frame's top,
// and every keypress leaves the previous frame's first rows on screen.
type menuFrame struct {
	w    io.Writer
	cols int // terminal width in cells; 0 leaves lines unclipped
}

// line prints s on a cleared row, clipped to the frame width with an ellipsis
// marking the cut.
func (f menuFrame) line(s string) {
	if f.cols > 0 {
		s = ansi.Truncate(s, f.cols, "…")
	}
	fmt.Fprintf(f.w, "\r\033[K%s\r\n", s)
}

// header prints the title row and the blank separator under it.
func (f menuFrame) header(label, hint string) {
	f.line(accent("▌") + " " + bold(label) + "  " + dim(hint))
	f.line("")
}

// searchBar prints the query being typed while search mode is active.
func (f menuFrame) searchBar(query string) {
	f.line(accent("🔍") + " " + query + "_")
}

// row prints one entry: the current one in reverse video behind a ❯ marker,
// the others indented with a dimmed description.
func (f menuFrame) row(current bool, name, desc string) {
	if current {
		f.line(reverse(" ❯ " + name + " " + desc + " "))
		return
	}
	f.line("   " + name + " " + dim(desc))
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
	cols, th := termSize(fd)
	f := menuFrame{w: w, cols: cols}

	// search state
	searching := false
	searchQuery := ""
	filtered := items
	filterIdx := make([]int, len(items))
	for i := range items {
		filterIdx[i] = i
	}

	sel := 0
	scroll := 0
	prevLines := 0 // lines printed in the previous frame; 0 = first frame

	render := func() {
		n := len(filtered)
		vp := maxViewport(n, th, searching)
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

		// scroll-up indicator (always 1 line)
		if n > 0 && scroll > 0 {
			f.line(dim(fmt.Sprintf(i18n.M.SelectMoreAboveFmt, scroll)))
		} else {
			f.line("")
		}

		// menu rows
		end := min(scroll+vp, n)
		for i := scroll; i < end; i++ {
			it := filtered[i]
			f.row(i == sel, fmt.Sprintf("%-10s", it.name), it.desc)
		}
		// if fewer items than viewport, pad with blank lines so the frame
		// height stays constant
		for i := end - scroll; i < vp; i++ {
			f.line("")
		}

		// scroll-down indicator (always 1 line)
		if n > 0 && end < n {
			f.line(dim(fmt.Sprintf(i18n.M.SelectMoreBelowFmt, n-end)))
		} else {
			f.line("")
		}
	}

	drawHeader := func() {
		if searching {
			f.header(label, i18n.M.SelectSearchHint)
			f.searchBar(searchQuery)
		} else {
			f.header(label, i18n.M.SelectOneHint)
		}
	}

	redraw := func() {
		if prevLines > 0 {
			fmt.Fprintf(w, "\033[%dA", prevLines)
		}
		drawHeader()
		render()
		// Clear everything below the current frame so stale rows from a taller
		// previous frame don't linger.
		fmt.Fprint(w, "\033[J")
		prevLines = fixedLines(searching) + maxViewport(len(filtered), th, searching)
	}

	redraw() // initial draw

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
				redraw()
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
					redraw()
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
				redraw()
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
			redraw()
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
		redraw()
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
	cols, th := termSize(fd)
	f := menuFrame{w: w, cols: cols}

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
	prevLines := 0

	render := func() {
		n := len(filtered)
		vp := maxViewport(n, th, searching)
		if cur < scroll {
			scroll = cur
		}
		if cur >= scroll+vp {
			scroll = cur - vp + 1
		}
		if scroll < 0 {
			scroll = 0
		}

		if n > 0 && scroll > 0 {
			f.line(dim(fmt.Sprintf(i18n.M.SelectMoreAboveFmt, scroll)))
		} else {
			f.line("")
		}

		end := min(scroll+vp, n)
		for i := scroll; i < end; i++ {
			it := filtered[i]
			origIdx := filterIdx[i]
			box := "[ ]"
			if checked[origIdx] {
				box = "[x]"
			}
			f.row(i == cur, box+" "+fmt.Sprintf("%-14s", it.name), it.desc)
		}
		for i := end - scroll; i < vp; i++ {
			f.line("")
		}

		if n > 0 && end < n {
			f.line(dim(fmt.Sprintf(i18n.M.SelectMoreBelowFmt, n-end)))
		} else {
			f.line("")
		}
	}

	drawHeader := func() {
		if searching {
			f.header(label, i18n.M.SelectSearchHint)
			f.searchBar(searchQuery)
		} else {
			f.header(label, i18n.M.SelectManyHint)
		}
	}

	redraw := func() {
		if prevLines > 0 {
			fmt.Fprintf(w, "\033[%dA", prevLines)
		}
		drawHeader()
		render()
		fmt.Fprint(w, "\033[J")
		prevLines = fixedLines(searching) + maxViewport(len(filtered), th, searching)
	}

	redraw()

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
				redraw()
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
					redraw()
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
				redraw()
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
			redraw()
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
			redraw()
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
		redraw()
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

// FrameLines is exported for testing. It returns the total number of terminal
// lines that selectOne/selectMany will print for the given state.
func FrameLines(filteredLen, termRows int, searching bool) int {
	return fixedLines(searching) + maxViewport(filteredLen, termRows, searching)
}
