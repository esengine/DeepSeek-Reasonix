// Package extensioncontract holds the leaf capability identity types shared by
// the plugin manifest parser and the extension kernel. It must stay free of
// business imports so both layers can depend on it without cycles.
package extensioncontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// CapabilityKey is a namespaced capability identity. Namespace + Kind + ID
// form the stable address used by dependency resolution and conflict reports.
type CapabilityKey struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	ID        string `json:"id"`
}

// String returns the canonical wire form namespace/kind/id.
func (k CapabilityKey) String() string {
	return k.Namespace + "/" + k.Kind + "/" + k.ID
}

// Validate rejects empty or whitespace-bearing key parts.
func (k CapabilityKey) Validate() error {
	if strings.TrimSpace(k.Namespace) == "" {
		return fmt.Errorf("capability namespace is required")
	}
	if strings.TrimSpace(k.Kind) == "" {
		return fmt.Errorf("capability kind is required")
	}
	if strings.TrimSpace(k.ID) == "" {
		return fmt.Errorf("capability id is required")
	}
	if strings.ContainsAny(k.Namespace, " \t\n") || strings.ContainsAny(k.Kind, " \t\n") || strings.ContainsAny(k.ID, " \t\n") {
		return fmt.Errorf("capability key parts must not contain whitespace")
	}
	return nil
}

// Capability is a concrete provided capability with a version and optional
// schema hash. Provider, tool, and UI capabilities require a stable schema hash.
type Capability struct {
	Key        CapabilityKey `json:"key"`
	Version    string        `json:"version"`
	SchemaHash string        `json:"schemaHash,omitempty"`
}

// Validate checks key shape and version/schema rules for the capability kind.
func (c Capability) Validate() error {
	if err := c.Key.Validate(); err != nil {
		return err
	}
	if !semver.IsValid(normalizeVersion(c.Version)) {
		return fmt.Errorf("capability %s: invalid version %q", c.Key, c.Version)
	}
	if requiresSchemaHash(c.Key.Kind) && strings.TrimSpace(c.SchemaHash) == "" {
		return fmt.Errorf("capability %s: schemaHash is required for kind %q", c.Key, c.Key.Kind)
	}
	return nil
}

// CanonicalHash fingerprints the capability identity used by epochs and
// snapshot cache metadata.
func (c Capability) CanonicalHash() string {
	type wire struct {
		Namespace  string `json:"namespace"`
		Kind       string `json:"kind"`
		ID         string `json:"id"`
		Version    string `json:"version"`
		SchemaHash string `json:"schemaHash"`
	}
	raw, err := json.Marshal(wire{
		Namespace:  c.Key.Namespace,
		Kind:       c.Key.Kind,
		ID:         c.Key.ID,
		Version:    normalizeVersion(c.Version),
		SchemaHash: strings.TrimSpace(c.SchemaHash),
	})
	if err != nil {
		// json.Marshal only fails on unsupported types; wire is plain strings.
		return ""
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Requirement is a dependency on a capability, with an optional version range.
type Requirement struct {
	Capability
	VersionRange string `json:"versionRange,omitempty"`
	Optional     bool   `json:"optional,omitempty"`
}

// Validate checks the requirement identity and, when set, the version range.
func (r Requirement) Validate() error {
	if err := r.Key.Validate(); err != nil {
		return err
	}
	if v := strings.TrimSpace(r.Version); v != "" && !semver.IsValid(normalizeVersion(v)) {
		return fmt.Errorf("requirement %s: invalid version %q", r.Key, r.Version)
	}
	if rangeExpr := strings.TrimSpace(r.VersionRange); rangeExpr != "" {
		if err := validateVersionRange(rangeExpr); err != nil {
			return fmt.Errorf("requirement %s: %w", r.Key, err)
		}
	}
	return nil
}

// SatisfiedBy reports whether provided meets this requirement (key match,
// optional schema hash pin, and semver range / exact version).
func (r Requirement) SatisfiedBy(provided Capability) bool {
	if r.Key != provided.Key {
		return false
	}
	if pin := strings.TrimSpace(r.SchemaHash); pin != "" && pin != strings.TrimSpace(provided.SchemaHash) {
		return false
	}
	pv := normalizeVersion(provided.Version)
	if !semver.IsValid(pv) {
		return false
	}
	if exact := strings.TrimSpace(r.Version); exact != "" {
		return semver.Compare(pv, normalizeVersion(exact)) == 0
	}
	rangeExpr := strings.TrimSpace(r.VersionRange)
	if rangeExpr == "" {
		return true
	}
	return matchVersionRange(rangeExpr, pv)
}

func requiresSchemaHash(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "provider", "tool", "ui", "uiaction":
		return true
	default:
		return false
	}
}

// normalizeVersion ensures a leading "v" for golang.org/x/mod/semver.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "v") || strings.HasPrefix(v, "V") {
		return "v" + strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	}
	return "v" + v
}

// validateVersionRange accepts a simple comma-separated set of comparisons
// such as ">=1.0.0", ">=1.0.0,<2.0.0".
func validateVersionRange(expr string) error {
	for part := range strings.SplitSeq(expr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return fmt.Errorf("empty version range clause")
		}
		op, ver, ok := splitRangeClause(part)
		if !ok {
			return fmt.Errorf("invalid version range clause %q", part)
		}
		if op == "" || !semver.IsValid(normalizeVersion(ver)) {
			return fmt.Errorf("invalid version range clause %q", part)
		}
	}
	return nil
}

func matchVersionRange(expr, version string) bool {
	for part := range strings.SplitSeq(expr, ",") {
		part = strings.TrimSpace(part)
		op, ver, ok := splitRangeClause(part)
		if !ok {
			return false
		}
		target := normalizeVersion(ver)
		cmp := semver.Compare(version, target)
		switch op {
		case ">=", "":
			if cmp < 0 {
				return false
			}
		case ">":
			if cmp <= 0 {
				return false
			}
		case "<=":
			if cmp > 0 {
				return false
			}
		case "<":
			if cmp >= 0 {
				return false
			}
		case "=", "==":
			if cmp != 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func splitRangeClause(part string) (op, ver string, ok bool) {
	for _, candidate := range []string{">=", "<=", "==", ">", "<", "="} {
		if strings.HasPrefix(part, candidate) {
			return candidate, strings.TrimSpace(part[len(candidate):]), true
		}
	}
	// Bare version means exact match.
	if semver.IsValid(normalizeVersion(part)) {
		return "=", part, true
	}
	return "", "", false
}
