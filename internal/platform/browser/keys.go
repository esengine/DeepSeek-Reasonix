package browser

import (
	"strings"
	"unicode/utf8"
)

type keyDef struct {
	key  string
	code string
	vk   int
	text string
}

var namedKeys = map[string]keyDef{
	"enter":      {"Enter", "Enter", 13, "\r"},
	"tab":        {"Tab", "Tab", 9, ""},
	"escape":     {"Escape", "Escape", 27, ""},
	"backspace":  {"Backspace", "Backspace", 8, ""},
	"delete":     {"Delete", "Delete", 46, ""},
	"space":      {" ", "Space", 32, " "},
	"arrowleft":  {"ArrowLeft", "ArrowLeft", 37, ""},
	"arrowup":    {"ArrowUp", "ArrowUp", 38, ""},
	"arrowright": {"ArrowRight", "ArrowRight", 39, ""},
	"arrowdown":  {"ArrowDown", "ArrowDown", 40, ""},
	"home":       {"Home", "Home", 36, ""},
	"end":        {"End", "End", 35, ""},
	"pageup":     {"PageUp", "PageUp", 33, ""},
	"pagedown":   {"PageDown", "PageDown", 34, ""},
}

var modifierBits = map[string]int{"alt": 1, "control": 2, "ctrl": 2, "meta": 4, "cmd": 4, "shift": 8}

// parseKey reads a chord such as "Enter", "Shift+Tab" or "Control+a".
func parseKey(chord string) (keyDef, int, bool) {
	parts := strings.Split(chord, "+")
	modifiers := 0
	for _, m := range parts[:len(parts)-1] {
		bit, ok := modifierBits[strings.ToLower(strings.TrimSpace(m))]
		if !ok {
			return keyDef{}, 0, false
		}
		modifiers |= bit
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if def, ok := namedKeys[strings.ToLower(last)]; ok {
		return def, modifiers, true
	}
	if utf8.RuneCountInString(last) != 1 {
		return keyDef{}, 0, false
	}
	r, _ := utf8.DecodeRuneInString(last)
	def := keyDef{key: last, text: last}
	switch {
	case r >= 'a' && r <= 'z':
		def.code, def.vk = "Key"+strings.ToUpper(last), int(r-'a'+'A')
	case r >= 'A' && r <= 'Z':
		def.code, def.vk = "Key"+last, int(r)
	case r >= '0' && r <= '9':
		def.code, def.vk = "Digit"+last, int(r)
	}
	if modifiers&(2|4) != 0 {
		def.text = ""
	}
	return def, modifiers, true
}
