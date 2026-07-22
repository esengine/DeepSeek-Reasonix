package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	maxCapabilityPaths         = 4
	maxCapabilityPrefixTokens  = 8
	maxCapabilityPathBytes     = 4096
	maxCapabilityJustification = 100
)

// CapabilityUse selects whether a prepared Bash invocation stays in its base
// sandbox or uses the complete effective capability delta. Authorization is a
// separate concern; callers must never select AuthorizedDelta without it.
type CapabilityUse uint8

const (
	// BaseOnly executes with the unchanged configured sandbox.
	BaseOnly CapabilityUse = iota
	// AuthorizedDelta applies the complete sealed effective delta.
	AuthorizedDelta
)

// CapabilityState describes the result of evaluating a model-authored request.
type CapabilityState uint8

const (
	// CapabilityOmitted means sandbox_capabilities was not present.
	CapabilityOmitted CapabilityState = iota
	// CapabilityNoEffectiveDelta means the request adds no OS authority.
	CapabilityNoEffectiveDelta
	// CapabilityReady means the complete delta is valid and enforceable.
	CapabilityReady
	// CapabilitySoftDenied means the complete bundle failed closed.
	CapabilitySoftDenied
	// CapabilityBaseUnavailable preserves the legacy backend-unavailable path.
	CapabilityBaseUnavailable
)

// CapabilityPathIdentity distinguishes relocatable workspace paths from
// canonical host paths without encoding the distinction in a magic prefix.
type CapabilityPathIdentity string

const (
	// WorkspaceRelative identifies a logical path below the canonical workspace.
	WorkspaceRelative CapabilityPathIdentity = "workspace_relative"
	// CanonicalAbsolute identifies an already canonical absolute host path.
	CanonicalAbsolute CapabilityPathIdentity = "canonical_absolute"
)

// CapabilityPathKind preserves whether a grant names one exact file or a
// recursive directory scope.
type CapabilityPathKind string

const (
	// CapabilityFile grants one exact existing file.
	CapabilityFile CapabilityPathKind = "file"
	// CapabilityDirectory grants an existing directory and its recursive subtree.
	CapabilityDirectory CapabilityPathKind = "directory"
)

// CapabilityRiskLevel is deliberately small in this substrate. Later approval
// UIs may render critical findings prominently without re-deriving path risk.
type CapabilityRiskLevel string

const (
	// CapabilityRiskNormal carries no known broad or sensitive scope finding.
	CapabilityRiskNormal CapabilityRiskLevel = "normal"
	// CapabilityRiskCritical requires prominent human review.
	CapabilityRiskCritical CapabilityRiskLevel = "critical"
)

// CapabilityRiskFinding is one authoritative, machine-readable risk reason.
type CapabilityRiskFinding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CapabilityRisk is the aggregate risk carried by a normalized request.
type CapabilityRisk struct {
	Level    CapabilityRiskLevel     `json:"level"`
	Findings []CapabilityRiskFinding `json:"findings,omitempty"`
}

// CapabilityPath is one normalized, existing path scope. Path retains the
// logical identity used by the model; Canonical is the authority reviewed and
// materialized by the host.
type CapabilityPath struct {
	Identity  CapabilityPathIdentity `json:"identity"`
	Path      string                 `json:"path"`
	Canonical string                 `json:"canonical"`
	Kind      CapabilityPathKind     `json:"kind"`
}

// CapabilitySet is an atomic network/read/write authority bundle.
type CapabilitySet struct {
	Network bool             `json:"network"`
	Reads   []CapabilityPath `json:"read_paths,omitempty"`
	Writes  []CapabilityPath `json:"write_paths,omitempty"`
}

// CapabilityReview is the immutable view consumed by the future authorization
// gate. Slices returned by CapabilityAssessment.Review are defensive copies.
type CapabilityReview struct {
	Requested      bool            `json:"requested"`
	State          CapabilityState `json:"state"`
	Request        CapabilitySet   `json:"request"`
	EffectiveDelta CapabilitySet   `json:"effective_delta"`
	ArgvPrefix     []string        `json:"argv_prefix,omitempty"`
	Justification  string          `json:"justification,omitempty"`
	Risk           CapabilityRisk  `json:"risk"`
	Diagnostic     string          `json:"diagnostic,omitempty"`
}

// CapabilityInput contains the complete immutable basis for one evaluation.
// A nil Raw value means sandbox_capabilities was omitted.
type CapabilityInput struct {
	Base      Spec
	Workspace string
	Raw       json.RawMessage
}

// CapabilityAssessment is a sealed evaluation. Its authority-bearing fields
// are intentionally private so callers cannot manufacture a partial delta.
type CapabilityAssessment struct {
	base      Spec
	workspace string
	review    CapabilityReview
}

// Review returns a defensive snapshot suitable for policy and presentation.
func (a CapabilityAssessment) Review() CapabilityReview {
	r := a.review
	r.Request = cloneCapabilitySet(r.Request)
	r.EffectiveDelta = cloneCapabilitySet(r.EffectiveDelta)
	r.ArgvPrefix = append([]string(nil), r.ArgvPrefix...)
	r.Risk.Findings = append([]CapabilityRiskFinding(nil), r.Risk.Findings...)
	return r
}

type capabilityPathRequest struct {
	Identity CapabilityPathIdentity `json:"identity"`
	Path     string                 `json:"path"`
}

type capabilityRequest struct {
	Network       bool                    `json:"network"`
	ReadPaths     []capabilityPathRequest `json:"read_paths"`
	WritePaths    []capabilityPathRequest `json:"write_paths"`
	ArgvPrefix    []string                `json:"argv_prefix"`
	Justification string                  `json:"justification"`
}

// EvaluateCapability strictly validates and normalizes one complete request,
// subtracts authority already present in the base sandbox, classifies risk, and
// probes whether the current platform can materialize the remaining bundle.
// Capability faults are represented as soft denial, never as execution errors.
func EvaluateCapability(ctx context.Context, in CapabilityInput) CapabilityAssessment {
	a := CapabilityAssessment{base: cloneSpec(in.Base)}
	a.review.Risk.Level = CapabilityRiskNormal
	if in.Raw == nil {
		a.review.State = CapabilityOmitted
		return a
	}
	a.review.Requested = true

	req, err := decodeCapabilityRequest(in.Raw)
	if err != nil {
		return a.softDeny(err)
	}
	workspace, err := canonicalWorkspace(in.Workspace)
	if err != nil {
		return a.softDeny(fmt.Errorf("resolve workspace: %w", err))
	}
	a.workspace = workspace

	reads, err := normalizeCapabilityPaths(workspace, req.ReadPaths)
	if err != nil {
		return a.softDeny(fmt.Errorf("read paths: %w", err))
	}
	writes, err := normalizeCapabilityPaths(workspace, req.WritePaths)
	if err != nil {
		return a.softDeny(fmt.Errorf("write paths: %w", err))
	}
	a.review.Request = CapabilitySet{Network: req.Network, Reads: reads, Writes: writes}
	a.review.ArgvPrefix = append([]string(nil), req.ArgvPrefix...)
	a.review.Justification = req.Justification
	a.review.Risk = classifyCapabilityRisk(a.base, a.review.Request)

	if err := requireExplicitReadsForWrites(a.base, reads, writes); err != nil {
		return a.softDeny(err)
	}
	a.review.EffectiveDelta = effectiveCapabilityDelta(a.base, a.review.Request)
	if capabilitySetEmpty(a.review.EffectiveDelta) {
		a.review.State = CapabilityNoEffectiveDelta
		return a
	}
	if !a.base.Enforce() || capabilityPlatformNoDelta() {
		a.review.EffectiveDelta = CapabilitySet{}
		a.review.State = CapabilityNoEffectiveDelta
		return a
	}
	if !Available() {
		a.review.State = CapabilityBaseUnavailable
		a.review.Diagnostic = BackendUnavailableReason()
		return a
	}
	if ok, reason := capabilityPlatformSupports(ctx, a.base, a.review.EffectiveDelta); !ok {
		return a.softDeny(fmt.Errorf("sandbox capability bundle is unsupported: %s", reason))
	}
	a.review.State = CapabilityReady
	return a
}

func (a CapabilityAssessment) softDeny(err error) CapabilityAssessment {
	a.review.State = CapabilitySoftDenied
	a.review.EffectiveDelta = CapabilitySet{}
	a.review.Diagnostic = err.Error()
	return a
}

func decodeCapabilityRequest(raw json.RawMessage) (capabilityRequest, error) {
	var req capabilityRequest
	if !utf8.Valid(raw) {
		return req, fmt.Errorf("sandbox_capabilities must be valid UTF-8 JSON")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return req, fmt.Errorf("sandbox_capabilities must be an object")
	}
	if err := rejectDuplicateCapabilityFields(raw); err != nil {
		return req, fmt.Errorf("invalid sandbox_capabilities: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, fmt.Errorf("invalid sandbox_capabilities: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return req, fmt.Errorf("invalid sandbox_capabilities: trailing JSON value")
		}
		return req, fmt.Errorf("invalid sandbox_capabilities: %w", err)
	}
	if len(req.ReadPaths) > maxCapabilityPaths {
		return req, fmt.Errorf("read_paths exceeds %d entries", maxCapabilityPaths)
	}
	if len(req.WritePaths) > maxCapabilityPaths {
		return req, fmt.Errorf("write_paths exceeds %d entries", maxCapabilityPaths)
	}
	if len(req.ArgvPrefix) > maxCapabilityPrefixTokens {
		return req, fmt.Errorf("argv_prefix exceeds %d tokens", maxCapabilityPrefixTokens)
	}
	for _, token := range req.ArgvPrefix {
		if token == "" || !utf8.ValidString(token) || strings.ContainsRune(token, '\x00') {
			return req, fmt.Errorf("argv_prefix tokens must be non-empty UTF-8 without NUL")
		}
	}
	if !utf8.ValidString(req.Justification) || utf8.RuneCountInString(req.Justification) > maxCapabilityJustification {
		return req, fmt.Errorf("justification exceeds %d Unicode characters", maxCapabilityJustification)
	}
	for _, p := range append(append([]capabilityPathRequest(nil), req.ReadPaths...), req.WritePaths...) {
		if len(p.Path) > maxCapabilityPathBytes {
			return req, fmt.Errorf("path exceeds %d bytes", maxCapabilityPathBytes)
		}
		if p.Path == "" || !utf8.ValidString(p.Path) || strings.ContainsRune(p.Path, '\x00') {
			return req, fmt.Errorf("paths must be non-empty UTF-8 without NUL")
		}
	}
	return req, nil
}

func rejectDuplicateCapabilityFields(raw json.RawMessage) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("object key is not a string")
				}
				if seen[key] {
					return fmt.Errorf("duplicate field %q", key)
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delim)
		}
	}
	return walk()
}

func canonicalWorkspace(workspace string) (string, error) {
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", workspace)
	}
	return filepath.Clean(real), nil
}

func normalizeCapabilityPaths(workspace string, requests []capabilityPathRequest) ([]CapabilityPath, error) {
	out := make([]CapabilityPath, 0, len(requests))
	for _, req := range requests {
		logical := req.Path
		var candidate string
		switch req.Identity {
		case WorkspaceRelative:
			if filepath.IsAbs(logical) || !filepath.IsLocal(logical) {
				return nil, fmt.Errorf("workspace_relative path %q must stay inside the workspace", logical)
			}
			candidate = filepath.Join(workspace, logical)
		case CanonicalAbsolute:
			if !filepath.IsAbs(logical) || filepath.Clean(logical) != logical {
				return nil, fmt.Errorf("canonical_absolute path %q must be clean and absolute", logical)
			}
			candidate = logical
		default:
			return nil, fmt.Errorf("unknown path identity %q", req.Identity)
		}
		real, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return nil, fmt.Errorf("path %q must already exist: %w", logical, err)
		}
		real = filepath.Clean(real)
		if req.Identity == WorkspaceRelative && !pathAtOrBelow(real, workspace) {
			return nil, fmt.Errorf("workspace_relative path %q escapes the workspace", logical)
		}
		if req.Identity == CanonicalAbsolute && real != logical {
			return nil, fmt.Errorf("canonical_absolute path %q resolves to %q", logical, real)
		}
		info, err := os.Stat(real)
		if err != nil {
			return nil, fmt.Errorf("stat path %q: %w", logical, err)
		}
		kind, err := capabilityPathKind(info)
		if err != nil {
			return nil, fmt.Errorf("path %q: %w", logical, err)
		}
		normalized := CapabilityPath{Identity: req.Identity, Path: logical, Canonical: real, Kind: kind}
		covered := false
		for _, existing := range out {
			if capabilityPathCovers(existing, normalized) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		kept := out[:0]
		for _, existing := range out {
			if !capabilityPathCovers(normalized, existing) {
				kept = append(kept, existing)
			}
		}
		out = append(kept, normalized)
	}
	return out, nil
}

func effectiveCapabilityDelta(base Spec, request CapabilitySet) CapabilitySet {
	delta := CapabilitySet{Network: request.Network && !base.Network}
	for _, p := range request.Reads {
		if capabilityReadCoveredByBase(base, p) {
			continue
		}
		delta.Reads = append(delta.Reads, p)
	}
	writeRoots := capabilityBaseWriteRoots(base)
	for _, p := range request.Writes {
		if capabilityWriteCoveredByBase(base, p, writeRoots) {
			continue
		}
		delta.Writes = append(delta.Writes, p)
	}
	return delta
}

func capabilityWriteCoveredByBase(base Spec, requested CapabilityPath, writeRoots []string) bool {
	if !capabilityPathCoveredByRoots(requested, writeRoots) {
		return false
	}
	for _, root := range base.ForbidReadRoots {
		forbidden, ok := existingCapabilityRoot(root)
		if ok && capabilityPathsIntersect(requested, forbidden) {
			return false
		}
	}
	return true
}

func capabilityReadCoveredByBase(base Spec, requested CapabilityPath) bool {
	for _, root := range base.ForbidReadRoots {
		forbidden, ok := existingCapabilityRoot(root)
		if ok && capabilityPathsIntersect(requested, forbidden) {
			return false
		}
	}
	return capabilityBaseReadCovers(base, requested)
}

func requireExplicitReadsForWrites(base Spec, reads, writes []CapabilityPath) error {
	for _, write := range writes {
		intersectsForbidden := false
		for _, root := range base.ForbidReadRoots {
			forbidden, ok := existingCapabilityRoot(root)
			if ok && capabilityPathsIntersect(write, forbidden) {
				intersectsForbidden = true
				break
			}
		}
		if !intersectsForbidden {
			continue
		}
		covered := false
		for _, read := range reads {
			if capabilityPathCovers(read, write) {
				covered = true
				break
			}
		}
		if !covered {
			return fmt.Errorf("write path %q intersects forbidden-read policy and requires an explicit covering read path", write.Canonical)
		}
	}
	return nil
}

func existingCapabilityRoot(root string) (CapabilityPath, bool) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return CapabilityPath{}, false
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return CapabilityPath{}, false
	}
	info, err := os.Stat(real)
	if err != nil {
		return CapabilityPath{}, false
	}
	kind, err := capabilityPathKind(info)
	if err != nil {
		return CapabilityPath{}, false
	}
	return CapabilityPath{Canonical: filepath.Clean(real), Kind: kind}, true
}

func capabilityPathKind(info os.FileInfo) (CapabilityPathKind, error) {
	if info.IsDir() {
		return CapabilityDirectory, nil
	}
	if info.Mode().IsRegular() {
		return CapabilityFile, nil
	}
	return "", fmt.Errorf("target must be a regular file or directory, got mode %s", info.Mode().Type())
}

func capabilityPathCoveredByRoots(path CapabilityPath, roots []string) bool {
	for _, root := range roots {
		r, ok := existingCapabilityRoot(root)
		if ok && capabilityPathCovers(r, path) {
			return true
		}
	}
	return false
}

func capabilityPathCovers(parent, child CapabilityPath) bool {
	if parent.Canonical == child.Canonical {
		return parent.Kind == CapabilityDirectory || parent.Kind == child.Kind
	}
	return parent.Kind == CapabilityDirectory && pathAtOrBelow(child.Canonical, parent.Canonical)
}

func capabilityPathsIntersect(a, b CapabilityPath) bool {
	return capabilityPathCovers(a, b) || capabilityPathCovers(b, a)
}

func pathAtOrBelow(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func classifyCapabilityRisk(base Spec, set CapabilitySet) CapabilityRisk {
	risk := CapabilityRisk{Level: CapabilityRiskNormal}
	home, _ := os.UserHomeDir()
	if home != "" {
		home, _ = filepath.EvalSymlinks(home)
	}
	for _, p := range append(append([]CapabilityPath(nil), set.Reads...), set.Writes...) {
		code := ""
		message := ""
		switch {
		case filepath.Clean(p.Canonical) == string(filepath.Separator):
			code, message = "filesystem_root", "scope includes the filesystem root"
		case home != "" && capabilityPathCovers(p, CapabilityPath{Canonical: home, Kind: CapabilityDirectory}):
			code, message = "home_root", "scope includes the user's home directory"
		case capabilityIntersectsRoots(p, base.ForbidReadRoots):
			code, message = "sensitive_path", "scope intersects the configured forbidden-read policy"
		case sensitiveCapabilityPath(p.Canonical):
			code, message = "sensitive_path", "scope includes a credential or security-sensitive path"
		case broadCapabilityPath(p.Canonical):
			code, message = "broad_system_path", "scope includes a broad system directory"
		}
		if code != "" {
			risk.Level = CapabilityRiskCritical
			risk.Findings = append(risk.Findings, CapabilityRiskFinding{Code: code, Message: message})
		}
	}
	return risk
}

func capabilityIntersectsRoots(path CapabilityPath, roots []string) bool {
	for _, root := range roots {
		candidate, ok := existingCapabilityRoot(root)
		if ok && capabilityPathsIntersect(path, candidate) {
			return true
		}
	}
	return false
}

func sensitiveCapabilityPath(path string) bool {
	clean := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	name := filepath.Base(clean)
	switch name {
	case ".env", ".git-credentials", ".netrc":
		return true
	}
	for _, ext := range []string{".pem", ".key", ".p12", ".pfx"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	for _, marker := range []string{"/.ssh", "/.gnupg", "/.aws", "/.kube", "/.config/gcloud", "/etc/shadow", "/etc/sudoers"} {
		if clean == marker || strings.HasSuffix(clean, marker) || strings.Contains(clean, marker+"/") {
			return true
		}
	}
	return false
}

func broadCapabilityPath(path string) bool {
	clean := filepath.Clean(path)
	for _, broad := range []string{"/dev", "/proc", "/sys", "/etc", "/var", "/usr", "/opt", "/home", "/Users", "/root"} {
		broad = filepath.Clean(broad)
		if clean == broad || pathAtOrBelow(clean, broad) {
			return true
		}
	}
	return false
}

func capabilitySetEmpty(set CapabilitySet) bool {
	return !set.Network && len(set.Reads) == 0 && len(set.Writes) == 0
}

func cloneCapabilitySet(in CapabilitySet) CapabilitySet {
	return CapabilitySet{
		Network: in.Network,
		Reads:   append([]CapabilityPath(nil), in.Reads...),
		Writes:  append([]CapabilityPath(nil), in.Writes...),
	}
}

func cloneSpec(in Spec) Spec {
	out := in
	out.WriteRoots = append([]string(nil), in.WriteRoots...)
	out.ReadRoots = append([]string(nil), in.ReadRoots...)
	out.AppContainerWriteRoots = append([]string(nil), in.AppContainerWriteRoots...)
	out.ForbidReadRoots = append([]string(nil), in.ForbidReadRoots...)
	return out
}
