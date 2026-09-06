// unparsed.go — a config file the parser rejected, told as something a reader
// can act on rather than as a sentence about column numbers.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	fileencoding "reasonix/internal/fileutil/encoding"
)

// What a surface is showing while the file on disk cannot be read.
const (
	RecoveredSnapshot = "last-known-good"
	RecoveredDefaults = "defaults"
)

// maxEscapeRepairs bounds the repair loop. Each round fixes one escape the
// parser named, and a file needing more than this is not the mistake this
// repairs.
const maxEscapeRepairs = 256

// UnparsedFile is the identity of "this config file does not parse". A caller
// has to tell it from an ordinary rejected value — one means "fix what you
// typed", the other means "nothing here can be saved until the file is fixed" —
// and a message is where that difference goes to die.
type UnparsedFile struct {
	Path      string // the file the reader has to open
	Line      int    // 1-based; 0 when the parser reported no position
	Column    int
	Key       string // the last key the parser read, when it had one
	Excerpt   string // the offending line as written
	Repair    string // that line said the other way; empty when there is no certain repair
	Recovered string // what is on screen instead: RecoveredSnapshot or RecoveredDefaults
	err       error
}

func (e *UnparsedFile) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("config %s: line %d does not parse: %v", e.Path, e.Line, e.err)
	}
	return fmt.Sprintf("config %s does not parse: %v", e.Path, e.err)
}

func (e *UnparsedFile) Unwrap() error { return e.err }

// Repairable reports whether this file has a repair to offer.
func (e *UnparsedFile) Repairable() bool { return e != nil && e.Repair != "" }

// recoveredForEdit is what a settings surface gets when the file will not
// parse: the runtime loader's own recovery rather than bare defaults, which
// had the surface showing built-in values while the user's settings sat unread
// on disk. The save guard is unchanged - the error it carries still refuses
// every write, because the file a write would replace is one nothing read.
func recoveredForEdit(path string, err error) *Config {
	cfg := Default()
	recovered := RecoveredDefaults
	if isUserConfigPath(path) && processRoots().loadLastKnownGoodUserConfig(cfg) == nil {
		recovered = RecoveredSnapshot
	}
	normalizeConfigForEdit(cfg)
	cfg.editLoadErr = asUnparsedFile(path, err, recovered)
	return cfg
}

// Unparsed reports the file this Config could not be read from, when that is
// why it is holding recovered values instead of the user's own.
func (c *Config) Unparsed() *UnparsedFile {
	var out *UnparsedFile
	if c != nil && errors.As(c.editLoadErr, &out) {
		return out
	}
	return nil
}

// asUnparsedFile names a load failure the parser caused. Anything else — a
// missing file, a permission error — is not this condition and travels on
// unchanged, so a caller matching the identity never widens it.
func asUnparsedFile(path string, err error, recovered string) error {
	var parse toml.ParseError
	if err == nil || !errors.As(err, &parse) {
		return err
	}
	out := &UnparsedFile{
		Path:      path,
		Line:      parse.Position.Line,
		Column:    parse.Position.Col,
		Key:       parse.LastKey,
		Recovered: recovered,
		err:       err,
	}
	text, _, readErr := readConfigText(path)
	if readErr != nil {
		return out
	}
	out.Excerpt = lineAt(text, out.Line)
	if repaired, ok := repairEscapes(text); ok {
		out.Repair = lineAt(repaired, out.Line)
	}
	return out
}

// asUnparsedJSON names a JSON store the parser rejected. Same identity as the
// TOML one and for the same reason: "your file is broken" and "the disk would
// not answer" are different things to do next.
func asUnparsedJSON(path string, data []byte, err error) error {
	var syntax *json.SyntaxError
	if err == nil || !errors.As(err, &syntax) {
		return err
	}
	out := &UnparsedFile{Path: path, Line: lineOfOffset(data, syntax.Offset), Recovered: RecoveredDefaults, err: err}
	out.Excerpt = lineAt(string(data), out.Line)
	return out
}

// lineOfOffset turns a byte offset into the 1-based line holding it, which is
// what a reader opens the file to.
func lineOfOffset(data []byte, offset int64) int {
	if offset < 0 {
		return 0
	}
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	return 1 + strings.Count(string(data[:offset]), "\n")
}

func lineAt(text string, line int) string {
	lines := strings.Split(text, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return strings.TrimRight(lines[line-1], "\r")
}

// repairEscapes says the same bytes in a way TOML accepts. A Windows path
// inside a basic string is the one way a hand-written config stops parsing, and
// there is only one reading of it: the backslash the parser refused is a
// backslash. The parser locates it and the parser confirms the result — no step
// here reads the message it wrote, and a file it cannot finish is left alone.
func repairEscapes(text string) (string, bool) {
	repaired := text
	for range maxEscapeRepairs {
		var probe map[string]any
		if _, err := toml.Decode(repaired, &probe); err == nil {
			return repaired, repaired != text
		} else if next, ok := escapeDoubled(repaired, err); ok {
			repaired = next
		} else {
			return text, false
		}
	}
	return text, false
}

// escapeDoubled doubles the backslash the parser stopped on. The span it
// reports starts at the string's content and ends at the escape it refused, so
// the last backslash inside that span is the one meant literally — true for a
// plain bad escape, and for \x and \U, which fail further along at the hex
// digits they never got.
func escapeDoubled(text string, err error) (string, bool) {
	var parse toml.ParseError
	if !errors.As(err, &parse) {
		return text, false
	}
	start, end := parse.Position.Start, parse.Position.Start+parse.Position.Len
	if start < 0 || start >= len(text) {
		return text, false
	}
	at := strings.LastIndexByte(text[start:min(end, len(text))], '\\')
	if at < 0 {
		return text, false
	}
	at += start
	return text[:at] + `\` + text[at:], true
}

// RepairUnparsedConfig rewrites path with the escapes said the other way, after
// copying the original beside it. It refuses unless the result loads: a repair
// that leaves the file unusable is a second problem, not a fix.
func RepairUnparsedConfig(path string) (backup string, err error) {
	if isUserConfigPath(path) {
		if lockErr := currentUserConfigEditLockError(); lockErr != nil {
			return "", fmt.Errorf("repair user config: %w", lockErr)
		}
	}
	text, raw, err := readConfigText(path)
	if err != nil {
		return "", fmt.Errorf("repair config %s: %w", path, err)
	}
	repaired, ok := repairEscapes(text)
	if !ok {
		return "", fmt.Errorf("repair config %s: this file needs an edit no rewrite can guess", path)
	}
	if err := ValidateBytes([]byte(repaired)); err != nil {
		return "", fmt.Errorf("repair config %s: %w", path, err)
	}
	resolved, err := resolveConfigAccessPath(path, isUserConfigPath(path))
	if err != nil {
		return "", err
	}
	perm := configFilePerm(path)
	backup = resolved + ".broken-" + time.Now().Format("20060102-150405")
	if err := os.WriteFile(backup, raw, perm); err != nil {
		return "", fmt.Errorf("back up %s: %w", path, err)
	}
	if err := writeConfigFileResolved(resolved, repaired, perm); err != nil {
		return "", err
	}
	return backup, nil
}

func readConfigText(path string) (text string, raw []byte, err error) {
	resolved, exists, err := statConfigPath(path)
	if err != nil {
		return "", nil, err
	}
	if !exists {
		return "", nil, os.ErrNotExist
	}
	raw, err = fileencoding.ReadFileUTF8(resolved)
	if err != nil {
		return "", nil, err
	}
	return string(fileencoding.DecodeToUTF8(raw)), raw, nil
}
