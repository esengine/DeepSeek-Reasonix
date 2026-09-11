// Frontmatter serialization for saved facts: the YAML shape render emits and
// the legacy type-routing compatibility it preserves.
package memory

import (
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// memoryFrontmatter is the YAML shape render emits, mirroring the auto-memory
// shape (name / description / metadata.type) so the files are interchangeable
// with that ecosystem and re-readable by loadMemory. Marshaled by yaml.v3 so a
// value containing ": ", '#', or quotes is escaped instead of corrupting the
// block; plain values render byte-identically to the previous hand-built form.
type memoryFrontmatter struct {
	ID             string `yaml:"id,omitempty"`
	Revision       int    `yaml:"revision,omitempty"`
	CreatedAt      string `yaml:"created_at,omitempty"`
	UpdatedAt      string `yaml:"updated_at,omitempty"`
	Name           string `yaml:"name"`
	Title          string `yaml:"title,omitempty"`
	Desc           string `yaml:"description"`
	Keywords       string `yaml:"keywords,omitempty"`
	Activation     string `yaml:"activation,omitempty"`
	Volatility     string `yaml:"volatility,omitempty"`
	SubjectKey     string `yaml:"subject_key,omitempty"`
	ExpiresAt      string `yaml:"expires_at,omitempty"`
	LastVerifiedAt string `yaml:"last_verified_at,omitempty"`
	Metadata       struct {
		Type     string `yaml:"type"`
		FactType string `yaml:"fact_type,omitempty"`
		Scope    string `yaml:"scope"`
	} `yaml:"metadata"`
}

// render serializes a memory to frontmatter + body.
func render(m Memory, name string) string {
	fm := memoryFrontmatter{
		ID: m.ID, Revision: m.Revision, Name: name, Title: oneLine(m.Title), Desc: oneLine(m.Description),
		Keywords: oneLine(m.Keywords), Activation: string(NormalizeActivation(string(m.Activation))),
		Volatility: string(NormalizeVolatility(string(m.Volatility))),
		SubjectKey: NormalizeSubjectKey(m.SubjectKey),
	}
	if !m.CreatedAt.IsZero() {
		fm.CreatedAt = m.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !m.UpdatedAt.IsZero() {
		fm.UpdatedAt = m.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !m.ExpiresAt.IsZero() {
		fm.ExpiresAt = m.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if !m.LastVerifiedAt.IsZero() {
		fm.LastVerifiedAt = m.LastVerifiedAt.UTC().Format(time.RFC3339Nano)
	}
	actualType := NormalizeType(string(m.Type))
	scope := NormalizeFactScope(string(m.Scope))
	compatType := previousReleaseRoutingType(actualType, scope)
	fm.Metadata.Type = string(compatType)
	if compatType != actualType {
		fm.Metadata.FactType = string(actualType)
	}
	fm.Metadata.Scope = string(scope)
	var b strings.Builder
	b.WriteString("---\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	// Encoding a flat struct of strings cannot fail.
	_ = enc.Encode(fm)
	_ = enc.Close()
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(m.Body))
	b.WriteString("\n")
	return b.String()
}

// previousReleaseRoutingType keeps scope safe when an older Reasonix binary
// shares the same state directory. Previous releases routed user/feedback to
// GlobalDir and project/reference to Dir, so metadata.type remains a compatible
// routing hint while metadata.fact_type preserves the independent new category.
func previousReleaseRoutingType(actual Type, scope FactScope) Type {
	if scope == FactScopeGlobal {
		if actual == TypeUser || actual == TypeFeedback {
			return actual
		}
		return TypeUser
	}
	if actual == TypeProject || actual == TypeReference {
		return actual
	}
	return TypeProject
}
