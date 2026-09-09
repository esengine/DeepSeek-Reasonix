package acp

import (
	"testing"

	"reasonix/internal/agent"
)

// schemaOneTempDir pins a test to the schema-1 session writer: it exercises
// file-based branch transitions that only that path has.
func schemaOneTempDir(t *testing.T) string {
	t.Helper()
	t.Setenv(agent.SessionLogSchemaEnv, "v1")
	return t.TempDir()
}
