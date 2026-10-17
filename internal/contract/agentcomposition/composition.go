// Package agentcomposition defines immutable session capability compositions.
package agentcomposition

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const SchemaVersion = 1
const Inherit = "inherit"
const Select = "select"

var ErrInvalid = errors.New("invalid agent composition")
var ErrUnavailable = errors.New("agent composition unavailable")
var ErrLocked = errors.New("agent composition locked")
var identifier = regexp.MustCompile(`^(builtin|user|project):[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

type Persona struct {
	Prefix                string `json:"prefix"`
	Suffix                string `json:"suffix"`
	Complete              bool   `json:"complete"`
	IncludeRuntimeContext bool   `json:"include_runtime_context"`
}

type Module struct {
	ID         string          `json:"id"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

type Selection struct {
	Mode    string   `json:"mode"`
	Members []Module `json:"members"`
}

type Definition struct {
	SchemaVersion int       `json:"schemaVersion"`
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Persona       Persona   `json:"persona"`
	Tools         Selection `json:"tools"`
	Skills        Selection `json:"skills"`
	MCP           Selection `json:"mcp"`
	Extensions    Selection `json:"extensions"`
	Hooks         Selection `json:"hooks"`
	Commands      Selection `json:"commands"`
}

type Version struct {
	Definition Definition `json:"definition"`
	Revision   string     `json:"revision"`
}

type Diagnostic struct {
	Code   string `json:"code"`
	Module string `json:"module,omitempty"`
	Detail string `json:"detail"`
}

// Resource captures a module's data without retaining a mutable source path.
type Resource struct {
	ID   string          `json:"id"`
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

type Binding struct {
	Resolved         Definition `json:"resolved"`
	Started          bool       `json:"started,omitempty"`
	Version          Version    `json:"version"`
	Digest           string     `json:"digest"`
	Workspace        string     `json:"workspace,omitempty"`
	Resources        []Resource `json:"resources"`
	SystemPrompt     string     `json:"systemPrompt,omitempty"`
	ToolSchemaDigest string     `json:"toolSchemaDigest,omitempty"`
	ToolSurface      []string   `json:"toolSurface"`
}

func (b *Binding) Seal() {
	b.Digest = ""
	started := b.Started
	b.Started = false
	b.Digest = Digest(b)
	b.Started = started
}

func (b *Binding) Validate() error {
	if b == nil {
		return ErrInvalid
	}
	if err := b.Version.Definition.Validate(); err != nil {
		return err
	}
	if b.Version.Revision != Revision(b.Version.Definition) {
		return ErrInvalid
	}
	if err := b.Resolved.Validate(); err != nil {
		return err
	}
	if b.Resolved.ID != b.Version.Definition.ID {
		return ErrInvalid
	}
	total := len(b.SystemPrompt)
	seen := map[string]bool{}
	for _, r := range b.Resources {
		key := r.Kind + ":" + r.ID
		if r.ID == "" || r.Kind == "" || seen[key] || !json.Valid(r.Data) {
			return ErrInvalid
		}
		seen[key] = true
		total += len(r.Data)
		if total > 128<<20 || len(b.Resources) > 20000 {
			return fmt.Errorf("%w: retained resource limit", ErrInvalid)
		}
	}
	copy := Clone(b)
	copy.Seal()
	if copy.Digest != b.Digest {
		return fmt.Errorf("%w: binding digest", ErrInvalid)
	}
	return nil
}

func (d Definition) Validate() error {
	if d.SchemaVersion != SchemaVersion || !identifier.MatchString(d.ID) || strings.TrimSpace(d.Name) == "" || len(d.Name) > 1024 || len(d.Description) > 16384 {
		return fmt.Errorf("%w: schemaVersion, id or name", ErrInvalid)
	}
	if len(d.Persona.Prefix) > 32768 || len(d.Persona.Suffix) > 32768 || d.Persona.Complete && strings.TrimSpace(d.Persona.Prefix) == "" {
		return fmt.Errorf("%w: persona", ErrInvalid)
	}
	for _, s := range []Selection{d.Tools, d.Skills, d.MCP, d.Extensions, d.Hooks, d.Commands} {
		if len(s.Members) > 10000 {
			return ErrInvalid
		}
		if s.Mode != Inherit && s.Mode != Select || s.Mode == Inherit && len(s.Members) != 0 {
			return fmt.Errorf("%w: selection mode", ErrInvalid)
		}
		seen := map[string]bool{}
		for _, m := range s.Members {
			if m.ID == "" || len(m.ID) > 1024 || seen[m.ID] {
				return fmt.Errorf("%w: duplicate or empty module %q", ErrInvalid, m.ID)
			}
			seen[m.ID] = true
			if len(m.Parameters) > 0 {
				var obj map[string]json.RawMessage
				if err := json.Unmarshal(m.Parameters, &obj); err != nil || obj == nil {
					return fmt.Errorf("%w: module parameters", ErrInvalid)
				}
			}
		}
	}
	return d.validateModuleParameters()
}

func Decode(data []byte) (Definition, error) {
	var d Definition
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return d, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return d, fmt.Errorf("%w: trailing data", ErrInvalid)
	}
	return d, d.Validate()
}

func Revision(d Definition) string { return Digest(d) }

func Digest(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	var normalized any
	if err := json.Unmarshal(b, &normalized); err != nil {
		return ""
	}
	b, err = json.Marshal(normalized)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func Clone[T any](v T) T {
	b, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(b, &out)
	return out
}
