package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"reasonix/internal/control"
	"reasonix/internal/secrets"
	"reasonix/internal/shellparse"
)

// This file holds the chat TUI's paste & image-attachment input layer: folding
// long pasted text into a deletable [Pasted text #N] token, turning
// dragged/pasted images and file paths into @references, and the clipboard
// commands behind them. The composer state it operates on (pastedBlocks /
// nextPasteID / pendingPastes) lives on chatTUI; these are split out of
// chat_tui.go as a self-contained concern.

func pastedLineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"), "\n") + 1
}

func foldedPasteLabel(id, lines int) string {
	return fmt.Sprintf("[Pasted text #%d · %d lines]", id, lines)
}

func renderFoldedPasteBlock(block pastedBlock) string {
	return fmt.Sprintf("%s\n\n--- Begin %s ---\n%s\n--- End %s ---", block.label, block.label, block.text, block.label)
}

func shouldFoldPastedText(s string) bool {
	return len([]rune(s)) >= foldedPasteMinChars || pastedLineCount(s) >= foldedPasteMinLines
}

func (m *chatTUI) shouldFoldPaste(s string) bool {
	return shouldFoldPastedText(s)
}

func (m *chatTUI) insertFoldedPaste(s string) {
	m.deleteComposerSelection()
	label := foldedPasteLabel(m.nextPasteID, pastedLineCount(s))
	m.nextPasteID++
	m.pastedBlocks = append(m.pastedBlocks, pastedBlock{label: label, text: s})
	m.input.InsertString(label + " ")
}

// insertImageRef puts a deletable [image #N] token in the input box (mapped to
// the saved attachment's @ref, expanded on submit) so a dragged/pasted image is
// edited and removed like any other text, not stranded in a separate tray.
func (m *chatTUI) insertImageRef(path string) {
	m.deleteComposerSelection()
	label := fmt.Sprintf("[image #%d]", m.nextPasteID)
	m.nextPasteID++
	m.pastedBlocks = append(m.pastedBlocks, pastedBlock{label: label, text: "@" + path, image: true})
	m.input.InsertString(label + " ")
	m.growInputToFit()
	m.updateCompletion()
}

func (m *chatTUI) expandPastedBlocks(displayed string) string {
	sent := displayed
	for _, block := range m.pastedBlocks {
		if !strings.Contains(sent, block.label) {
			continue
		}
		repl := renderFoldedPasteBlock(block)
		if block.image {
			repl = block.text
		}
		sent = strings.ReplaceAll(sent, block.label, repl)
	}
	return sent
}

func (m *chatTUI) pasteLabelsIn(s string) []string {
	var labels []string
	for _, block := range m.pastedBlocks {
		if strings.Contains(s, block.label) {
			labels = append(labels, block.label)
		}
	}
	return labels
}

func (m *chatTUI) clearSubmittedPastes() {
	if len(m.pendingPastes) == 0 {
		return
	}
	submitted := make(map[string]bool, len(m.pendingPastes))
	for _, label := range m.pendingPastes {
		submitted[label] = true
	}
	kept := m.pastedBlocks[:0]
	for _, block := range m.pastedBlocks {
		if !submitted[block.label] {
			kept = append(kept, block)
		}
	}
	m.pastedBlocks = kept
	m.pendingPastes = nil
}

func pasteClipboardImage() tea.Cmd {
	return func() tea.Msg {
		path, err := control.SaveClipboardImage()
		return clipboardImageMsg{path: path, err: err}
	}
}

type clipboardTextPasteMsg struct {
	text   string
	err    error
	remote bool
}

var readNativeClipboardText = clipboard.ReadAll

// pasteClipboardText backs the captured-mouse right-click path. Keyboard text
// paste still arrives from the terminal as a bracketed tea.PasteMsg; this read
// is deliberately text-only so right-click never probes for an image first.
func pasteClipboardText() tea.Cmd {
	return func() tea.Msg {
		if remoteClipboardSession() {
			return clipboardTextPasteMsg{remote: true}
		}
		text, err := readNativeClipboardText()
		return clipboardTextPasteMsg{text: text, err: err}
	}
}

func imagePasteShortcut(keyName, goos string) bool {
	if goos == "windows" {
		return keyName == "alt+v"
	}
	return keyName == "ctrl+v"
}

func (m *chatTUI) beginClipboardImagePaste() tea.Cmd {
	if m.clipboardImagePending {
		return nil
	}
	m.clipboardImagePending = true
	return pasteClipboardImage()
}

var (
	readTmuxPasteBuffer       = readTmuxBuffer
	readPrimaryPasteSelection = readPrimarySelection
	newPasteCommand           = exec.Command
)

// pasteMiddleClick returns a tea.Cmd that reads from the selection owner for the
// current terminal environment and sends the result through the canonical paste
// path. tmux normally owns middle-click and pastes its current buffer, but forwards
// the event when an application enables mouse reporting; honor that same contract
// here instead of unexpectedly switching to the desktop PRIMARY selection.
func pasteMiddleClick() tea.Cmd {
	return func() tea.Msg {
		if os.Getenv("TMUX") != "" {
			text, err := readTmuxPasteBuffer()
			if err != nil || text == "" {
				return nil
			}
			return tea.PasteMsg{Content: text}
		}
		if remoteClipboardSession() {
			return clipboardTextPasteMsg{remote: true}
		}
		text, err := readPrimaryPasteSelection()
		if err != nil || text == "" {
			return nil
		}
		return tea.PasteMsg{Content: text}
	}
}

// readTmuxBuffer retrieves the current tmux buffer verbatim. The inherited TMUX
// environment variable identifies the correct server socket, just as the tmux
// client command does for an interactive shell inside the pane.
func readTmuxBuffer() (string, error) {
	cmd := newPasteCommand("tmux", "save-buffer", "-")
	cmd.Env = secrets.ProcessEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("read tmux paste buffer: %w", err)
	}
	return string(out), nil
}

// readPrimarySelection attempts to retrieve text from the PRIMARY selection by
// trying wl-paste (Wayland), xclip (X11), and xsel (X11) in order.
func readPrimarySelection() (string, error) {
	// Match the order used by SaveClipboardImage: Wayland tool first, then X11.
	for _, args := range [][]string{
		{"wl-paste", "--primary", "--type", "text", "--no-newline"},
		{"xclip", "-selection", "primary", "-o"},
		{"xsel", "--output", "--primary"},
	} {
		cmd := newPasteCommand(args[0], args[1:]...)
		cmd.Env = secrets.ProcessEnv()
		out, err := cmd.Output()
		if err == nil {
			return string(out), nil
		}
	}
	return "", fmt.Errorf("no primary selection tool found (need wl-paste, xclip, or xsel)")
}

func (m *chatTUI) attachPastedImages(text string) bool {
	sources, ok := pastedImageSources(text)
	if !ok {
		return false
	}
	attached := false
	for _, src := range sources {
		path, err := savePastedImageSource(src)
		if err != nil {
			m.notice("paste image: " + err.Error())
			continue
		}
		m.insertImageRef(path)
		attached = true
	}
	if !attached && m.validComposerSelection() && !m.composerSel.empty() {
		// A failed attachment must not replace an active selection. The notice
		// above explains the failure; callers otherwise fall back to text paste.
		return true
	}
	return attached
}

var markdownImageSourceRe = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)

type pastedImageSource struct {
	value        string
	shellDecoded bool
}

func pastedImageSources(text string) ([]pastedImageSource, bool) {
	return pastedImageSourcesForOS(text, runtime.GOOS)
}

func pastedImageSourcesForOS(text, goos string) ([]pastedImageSource, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, false
	}
	if isDataImage(trimmed) {
		return []pastedImageSource{{value: trimmed}}, true
	}
	if matches := markdownImageSourceRe.FindAllStringSubmatch(trimmed, -1); len(matches) > 0 {
		rest := strings.TrimSpace(markdownImageSourceRe.ReplaceAllString(trimmed, ""))
		if rest == "" {
			sources := make([]pastedImageSource, 0, len(matches))
			for _, m := range matches {
				sources = append(sources, pastedImageSource{value: m[1]})
			}
			return sources, true
		}
	}

	lines := nonEmptyPasteLines(trimmed)
	lineSources := rawPastedImageSources(lines)
	if len(lines) > 0 && allImageSources(lineSources, goos) {
		return lineSources, true
	}
	fields := splitPastePathTokens(trimmed)
	fieldSources := rawPastedImageSources(fields)
	if len(fields) > 1 && allImageSources(fieldSources, goos) {
		return fieldSources, true
	}
	if staticFields, malformed := shellparse.StaticFields(trimmed); malformed == "" && len(staticFields) > 1 {
		sources := make([]pastedImageSource, 0, len(staticFields))
		for _, field := range staticFields {
			sources = append(sources, pastedImageSource{value: field, shellDecoded: true})
		}
		if allImageSources(sources, goos) {
			return sources, true
		}
	}
	return nil, false
}

func rawPastedImageSources(values []string) []pastedImageSource {
	sources := make([]pastedImageSource, 0, len(values))
	for _, value := range values {
		sources = append(sources, pastedImageSource{value: value})
	}
	return sources
}

// splitPastePathTokens splits pasted text into path tokens the way a shell
// would hand them to a program: unescaped, unquoted whitespace separates
// tokens, while backslash escapes and token-leading quotes keep a path with
// spaces together. Tokens keep their original escapes/quotes so each one
// round-trips through pastedImagePath. Quotes only open at the start of a
// token, so an apostrophe inside a word ("it's") never swallows the rest of
// the text.
func splitPastePathTokens(s string) []string {
	var tokens []string
	var b strings.Builder
	var quote byte
	escaped := false
	flush := func() {
		if b.Len() > 0 {
			tokens = append(tokens, b.String())
			b.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case escaped:
			b.WriteByte(ch)
			escaped = false
		case quote != 0:
			b.WriteByte(ch)
			if ch == quote {
				quote = 0
			}
		case ch == '\\':
			b.WriteByte(ch)
			escaped = true
		case (ch == '\'' || ch == '"') && b.Len() == 0:
			b.WriteByte(ch)
			quote = ch
		case ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n':
			flush()
		default:
			b.WriteByte(ch)
		}
	}
	flush()
	return tokens
}

func nonEmptyPasteLines(text string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func allImageSources(sources []pastedImageSource, goos string) bool {
	if len(sources) == 0 {
		return false
	}
	for _, src := range sources {
		if !looksLikeImageSource(src, goos) {
			return false
		}
	}
	return true
}

func looksLikeImageSource(src pastedImageSource, goos string) bool {
	if isDataImage(strings.TrimSpace(src.value)) {
		return true
	}
	for _, path := range pastedPathCandidates(src.value, goos, src.shellDecoded) {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp":
			return true
		}
	}
	return false
}

func savePastedImageSource(src pastedImageSource) (string, error) {
	value := strings.TrimSpace(src.value)
	if isDataImage(value) {
		return control.SaveImageDataURL(value)
	}
	var lastErr error
	for _, path := range pastedPathCandidates(value, runtime.GOOS, src.shellDecoded) {
		if !looksLikeImagePath(path) {
			continue
		}
		saved, err := control.SaveImageFile(path)
		if err == nil {
			return saved, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("unsupported pasted image source")
}

func looksLikeImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func isDataImage(src string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(src)), "data:image/")
}

// pastedImagePathForOS returns the preferred syntactic candidate with the OS
// injected so platform-specific path handling is testable everywhere.
func pastedImagePathForOS(src, goos string) (string, bool) {
	candidates := pastedPathCandidates(src, goos, false)
	if len(candidates) == 0 {
		return "", false
	}
	return candidates[0], true
}

func hasUnescapedPathWhitespace(s string) bool {
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			return true
		}
	}
	return false
}

// unescapeShellPath applies POSIX backslash semantics to an unquoted pasted
// path: a backslash makes the next byte literal, whatever it is — zsh and
// bash escape any byte they consider special that way (space, parens, ^,
// comma, $, ...), so a whitelist would always lag behind. A trailing
// backslash stays literal. Quoted paths and Windows paths never reach here.
func unescapeShellPath(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// pastedFileRef turns a dragged/pasted non-image file path into an @reference so
// it attaches instead of landing as literal text (and, for a POSIX path, being
// misread as a slash command). Images are handled earlier; only path-shaped
// content (a separator) that points at a real file qualifies, so an ordinary
// pasted word is left alone. Whitespace in the path is escaped so the ref
// survives @-token parsing on submit.
func pastedFileRef(content string) (string, bool) {
	path, ok := resolveExistingPastedPath(content, runtime.GOOS, false, pastedPathExists)
	if !ok || !strings.ContainsAny(path, `/\`) {
		return "", false
	}
	return "@" + control.EscapeRefPath(path), true
}
