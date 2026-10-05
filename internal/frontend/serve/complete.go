package serve

import (
	"net/http"
	"strconv"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// maxCompleteLine caps the line a client may ask about. The composer sends one
// prompt line per keystroke; anything past this is not a line being edited.
const maxCompleteLine = 8 << 10

// complete answers the composer's menu. The grammar lives in control.Complete,
// so this frontend's "@" and "/" mean what the terminal's mean. Offsets cross
// here in UTF-16 code units, which is how a browser indexes the string it sent:
// the kernel counts bytes, and a Chinese prompt would splice at the wrong
// place.
func (s *Server) complete(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	line := q.Get("line")
	if len(line) > maxCompleteLine {
		refuse(w, http.StatusRequestEntityTooLarge, "complete.line_too_long", "that line is too long to complete", nil)
		return
	}
	cursor := len(line)
	if v := q.Get("cursor"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			refuse(w, http.StatusBadRequest, codeBadValue, "cursor must be a number", map[string]any{"field": "cursor", "allowed": []string{"a number"}})
			return
		}
		cursor = byteOffset(line, n)
	}
	// The kernel words the built-in verbs and a window reads them, in a
	// language that is not this process's. The window is the authority on what
	// it draws; the config answers only for a client that says nothing.
	lang := q.Get("lang")
	if lang == "" {
		if cfg, err := config.Load(); err == nil {
			lang = cfg.DesktopLanguage()
		}
	}
	data := s.ctl().CompletionData(lang)
	// A client of this server completes inside the workspace and nowhere else,
	// which is the same boundary SubmitHTTP resolves its references within.
	data.Scoped = true
	out := control.Complete(line, cursor, data)
	out.From = utf16Offset(line, out.From)
	out.To = utf16Offset(line, out.To)
	writeJSON(w, out)
}

// utf16Offset converts a byte offset into s to the UTF-16 code-unit offset a
// JavaScript client indexes the same string by.
func utf16Offset(s string, b int) int {
	if b <= 0 {
		return 0
	}
	if b > len(s) {
		b = len(s)
	}
	n := 0
	for _, r := range s[:b] {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}

// byteOffset is utf16Offset reversed: the byte index of a client's caret.
func byteOffset(s string, u16 int) int {
	if u16 <= 0 {
		return 0
	}
	n := 0
	for i, r := range s {
		if n >= u16 {
			return i
		}
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return len(s)
}
