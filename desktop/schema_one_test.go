package main

import (
	"testing"

	"reasonix/internal/agent"
)

// isolateDesktopUserDirsSchemaOne pins a test to the schema-1 session writer:
// it exercises recovery-copy mechanics that only that path has.
func isolateDesktopUserDirsSchemaOne(t *testing.T) {
	t.Helper()
	t.Setenv(agent.SessionLogSchemaEnv, "v1")
	isolateDesktopUserDirs(t)
}

// schemaOneTempDir pins a test to the schema-1 session writer and returns a
// temp dir for its session files.
func schemaOneTempDir(t *testing.T) string {
	t.Helper()
	t.Setenv(agent.SessionLogSchemaEnv, "v1")
	return t.TempDir()
}
