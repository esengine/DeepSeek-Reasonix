package cli

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"reasonix/internal/control"
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
	label := foldedPasteLabel(m.nextPasteID, pastedLineCount(s))
	m.nextPasteID++
	m.pastedBlocks = append(m.pastedBlocks, pastedBlock{label: label, text: s})
	m.input.InsertString(label + " ")
}

// insertImageRef puts a deletable [image #N] token in the input box (mapped to
// the saved attachment's @ref, expanded on submit) so a dragged/pasted image is
// edited and removed like any other text, not stranded in a separate tray.
func (m *chatTUI) insertImageRef(path string) {
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

func pasteClipboard() tea.Cmd {
	return func() tea.Msg {
		path, imageErr := control.SaveClipboardImage()
		if imageErr == nil {
			return clipboardPasteMsg{path: path}
		}
		text, textErr := clipboard.ReadAll()
		if textErr == nil && text != "" {
			return clipboardPasteMsg{text: text}
		}
		if textErr != nil {
			return clipboardPasteMsg{err: fmt.Errorf("%v; text paste failed: %w", imageErr, textErr)}
		}
		return clipboardPasteMsg{err: imageErr}
	}
}

func (m *chatTUI) attachPastedImages(text string) bool {
	sources, ok := pastedImageSources(text)
	if !ok {
		return false
	}
	for _, src := range sources {
		path, err := savePastedImageSource(src)
		if err != nil {
			m.notice("paste image: " + err.Error())
			continue
		}
		m.insertImageRef(path)
	}
	return true
}

var markdownImageSourceRe = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)

func pastedImageSources(text string) ([]string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, false
	}
	if isDataImage(trimmed) {
		return []string{trimmed}, true
	}
	if matches := markdownImageSourceRe.FindAllStringSubmatch(trimmed, -1); len(matches) > 0 {
		rest := strings.TrimSpace(markdownImageSourceRe.ReplaceAllString(trimmed, ""))
		if rest == "" {
			sources := make([]string, 0, len(matches))
			for _, m := range matches {
				sources = append(sources, m[1])
			}
			return sources, true
		}
	}

	lines := nonEmptyPasteLines(trimmed)
	if len(lines) > 0 && allImageSources(lines) {
		return lines, true
	}
	fields := strings.Fields(trimmed)
	if len(fields) > 1 && allImageSources(fields) {
		return fields, true
	}
	return nil, false
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

func allImageSources(sources []string) bool {
	if len(sources) == 0 {
		return false
	}
	for _, src := range sources {
		if !looksLikeImageSource(src) {
			return false
		}
	}
	return true
}

func looksLikeImageSource(src string) bool {
	if isDataImage(strings.TrimSpace(src)) {
		return true
	}
	path, ok := pastedImagePath(src)
	if !ok {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

func savePastedImageSource(src string) (string, error) {
	src = strings.TrimSpace(src)
	if isDataImage(src) {
		return control.SaveImageDataURL(src)
	}
	path, ok := pastedImagePath(src)
	if !ok {
		return "", fmt.Errorf("unsupported pasted image source")
	}
	return control.SaveImageFile(path)
}

func isDataImage(src string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(src)), "data:image/")
}

func pastedImagePath(src string) (string, bool) {
	src = strings.TrimSpace(src)
	src = strings.TrimPrefix(src, "@")
	quoted := (strings.HasPrefix(src, `"`) && strings.HasSuffix(src, `"`)) || (strings.HasPrefix(src, `'`) && strings.HasSuffix(src, `'`))
	src = strings.Trim(src, "\"'")
	if src == "" {
		return "", false
	}
	if !quoted && strings.ContainsAny(src, " \t\r\n") {
		return "", false
	}
	lower := strings.ToLower(src)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return "", false
	}
	if strings.HasPrefix(lower, "file://") {
		u, err := url.Parse(src)
		if err != nil || u.Path == "" {
			return "", false
		}
		src = u.Path
	}
	if strings.HasPrefix(src, "~/") {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			src = filepath.Join(home, strings.TrimPrefix(src, "~/"))
		}
	}
	return filepath.Clean(src), true
}

// pastedFileRef turns a dragged/pasted non-image file path into an @reference so
// it attaches instead of landing as literal text (and, for a POSIX path, being
// misread as a slash command). Images are handled earlier; only path-shaped
// content (a separator) that points at a real file qualifies, so an ordinary
// pasted word is left alone.
func pastedFileRef(content string) (string, bool) {
	path, ok := pastedImagePath(content)
	if !ok || !strings.ContainsAny(path, `/\`) {
		return "", false
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", false
	}
	return "@" + path, true
}
