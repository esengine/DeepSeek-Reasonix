package protocol

import (
	"fmt"
	"regexp"
	"strings"
)

const ProtocolVersion = "1"

var (
	revisionPattern   = regexp.MustCompile(`^[0-9a-f]{40}(\+dirty)?$`)
	schemaHashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type BuildID struct {
	ProductVersion  string `json:"productVersion"`
	SourceRevision  string `json:"sourceRevision"`
	ProtocolVersion string `json:"protocolVersion"`
	SchemaHash      string `json:"schemaHash"`
}

func (id BuildID) Validate() error {
	if strings.TrimSpace(id.ProductVersion) == "" {
		return fmt.Errorf("productVersion is required")
	}
	if !revisionPattern.MatchString(id.SourceRevision) {
		return fmt.Errorf("sourceRevision must be a full lowercase commit hash with an optional +dirty suffix")
	}
	if id.ProtocolVersion == "" {
		return fmt.Errorf("protocolVersion is required")
	}
	if !schemaHashPattern.MatchString(id.SchemaHash) {
		return fmt.Errorf("schemaHash must be sha256 followed by 64 lowercase hex characters")
	}
	return nil
}

func NewBuildID(productVersion, sourceRevision string) (BuildID, error) {
	id := BuildID{
		ProductVersion: strings.TrimSpace(productVersion), SourceRevision: strings.TrimSpace(sourceRevision),
		ProtocolVersion: ProtocolVersion, SchemaHash: SchemaHash(),
	}
	return id, id.Validate()
}

type BuildIDField string

const (
	BuildProductVersion  BuildIDField = "productVersion"
	BuildSourceRevision  BuildIDField = "sourceRevision"
	BuildProtocolVersion BuildIDField = "protocolVersion"
	BuildSchemaHash      BuildIDField = "schemaHash"
)

type BuildIDMismatch struct {
	Field    BuildIDField
	Expected string
	Actual   string
}

func (m BuildIDMismatch) Error() string {
	return fmt.Sprintf("build ID %s mismatch", m.Field)
}

func CompareBuildID(expected, actual BuildID) error {
	if err := expected.Validate(); err != nil {
		return fmt.Errorf("expected build ID: %w", err)
	}
	if err := actual.Validate(); err != nil {
		return fmt.Errorf("actual build ID: %w", err)
	}
	checks := []struct {
		field BuildIDField
		want  string
		got   string
	}{
		{BuildProductVersion, expected.ProductVersion, actual.ProductVersion},
		{BuildSourceRevision, expected.SourceRevision, actual.SourceRevision},
		{BuildProtocolVersion, expected.ProtocolVersion, actual.ProtocolVersion},
		{BuildSchemaHash, expected.SchemaHash, actual.SchemaHash},
	}
	for _, check := range checks {
		if check.want != check.got {
			return &BuildIDMismatch{Field: check.field, Expected: check.want, Actual: check.got}
		}
	}
	return nil
}
