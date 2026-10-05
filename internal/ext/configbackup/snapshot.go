package configbackup

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// FormatVersion is the snapshot schema this build writes. A reader refuses a
// newer one rather than guessing at fields it does not know.
const FormatVersion = 1

// Category is one box the user ticks at export.
type Category string

const (
	CategorySettings   Category = "settings"
	CategoryExtensions Category = "extensions"
	CategoryMemory     Category = "memory"
	CategoryAutomation Category = "automation"
	CategorySecrets    Category = "secrets"
)

// CategoryInfo is what an export form needs to draw one box.
type CategoryInfo struct {
	ID        Category `json:"id"`
	DefaultOn bool     `json:"defaultOn"`
	// Consent means restoring its items asks per item, whatever the box said.
	Consent bool `json:"consent"`
}

// Categories lists every category in display order.
func Categories() []CategoryInfo {
	return []CategoryInfo{
		{ID: CategorySettings, DefaultOn: true},
		{ID: CategoryExtensions, DefaultOn: true},
		{ID: CategoryMemory, DefaultOn: true},
		{ID: CategoryAutomation, DefaultOn: true, Consent: true},
		{ID: CategorySecrets},
	}
}

func knownCategory(c Category) bool {
	return slices.ContainsFunc(Categories(), func(info CategoryInfo) bool { return info.ID == c })
}

// Item kinds. The kind decides how Data is read and where it is written back.
const (
	KindGeneral    = "general"
	KindProvider   = "provider"
	KindInterface  = "interface"
	KindSkill      = "skill"
	KindPlugin     = "plugin"
	KindMCP        = "mcp"
	KindMemory     = "memory"
	KindHook       = "hook"
	KindStatusline = "statusline"
	KindSecret     = "secret"
)

// Snapshot is the plaintext inside a sealed backup.
type Snapshot struct {
	Format     int        `json:"format"`
	CreatedAt  time.Time  `json:"createdAt"`
	AppVersion string     `json:"appVersion,omitempty"`
	Platform   string     `json:"platform"`
	Categories []Category `json:"categories"`
	Items      []Item     `json:"items"`
	Omitted    []Omission `json:"omitted,omitempty"`
}

// Item is one restorable unit. ID is "<kind>:<name>" and unique in a snapshot.
type Item struct {
	ID       string          `json:"id"`
	Category Category        `json:"category"`
	Kind     string          `json:"kind"`
	Name     string          `json:"name"`
	Data     json.RawMessage `json:"data"`
}

// Omission records something export left out, so the restoring side can say
// what it will not find rather than let it look lost.
type Omission struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Omission reasons.
const (
	OmitTooLarge   = "too_large"
	OmitUnreadable = "unreadable"
	OmitNotFile    = "not_regular_file"
	OmitHidden     = "hidden"
)

// Sentinels a caller tells apart. Each carries its own identity so a frontend
// maps it to a code instead of reading the message.
var (
	ErrUnsupportedFormat = errors.New("configbackup: snapshot format is newer than this build")
	ErrMalformed         = errors.New("configbackup: snapshot is malformed")
	ErrTooLarge          = errors.New("configbackup: snapshot exceeds the size limit")
	ErrNoCategories      = errors.New("configbackup: no category selected")
	ErrUnknownCategory   = errors.New("configbackup: unknown category")
)

const (
	maxSnapshotBytes = 32 << 20
	maxItems         = 5000
)

func itemID(kind, name string) string { return kind + ":" + name }

// encode renders a snapshot as gzipped JSON, the plaintext Seal encrypts.
func encode(s *Snapshot) ([]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decode is encode's inverse. The decompressed size is bounded because the
// bytes came from a network and gzip expands by up to three orders of magnitude.
func decode(compressed []byte) (*Snapshot, error) {
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	raw, err := io.ReadAll(io.LimitReader(zr, maxSnapshotBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if len(raw) > maxSnapshotBytes {
		return nil, ErrTooLarge
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *Snapshot) validate() error {
	if s.Format < 1 {
		return fmt.Errorf("%w: missing format", ErrMalformed)
	}
	if s.Format > FormatVersion {
		return ErrUnsupportedFormat
	}
	if len(s.Items) > maxItems {
		return ErrTooLarge
	}
	seen := make(map[string]bool, len(s.Items))
	for _, it := range s.Items {
		if it.ID != itemID(it.Kind, it.Name) || seen[it.ID] || !knownCategory(it.Category) {
			return fmt.Errorf("%w: item %q", ErrMalformed, it.ID)
		}
		if categoryOfKind(it.Kind) != it.Category {
			return fmt.Errorf("%w: item %q is filed under %q", ErrMalformed, it.ID, it.Category)
		}
		seen[it.ID] = true
	}
	return s.validateRestorable()
}

// categoryOfKind is fixed per kind, so a snapshot cannot file a hook under
// settings to dodge the consent automation carries.
func categoryOfKind(kind string) Category {
	switch kind {
	case KindGeneral, KindProvider, KindInterface:
		return CategorySettings
	case KindSkill, KindPlugin, KindMCP:
		return CategoryExtensions
	case KindMemory:
		return CategoryMemory
	case KindHook, KindStatusline:
		return CategoryAutomation
	case KindSecret:
		return CategorySecrets
	}
	return ""
}

// ParseCategories validates a requested set and returns it in display order.
func ParseCategories(raw []string) ([]Category, error) {
	want := map[Category]bool{}
	for _, r := range raw {
		c := Category(strings.TrimSpace(r))
		if !knownCategory(c) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownCategory, r)
		}
		want[c] = true
	}
	if want[CategorySecrets] {
		want[CategorySettings] = true
	}
	var out []Category
	for _, info := range Categories() {
		if want[info.ID] {
			out = append(out, info.ID)
		}
	}
	if len(out) == 0 {
		return nil, ErrNoCategories
	}
	return out, nil
}
