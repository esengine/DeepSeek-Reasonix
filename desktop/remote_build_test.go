package main

import (
	"strings"
	"testing"

	"reasonix/internal/buildinfo"
	"reasonix/internal/remote/protocol"
)

func TestCurrentDesktopRemoteBuildIDUsesExactCoordinatedIdentity(t *testing.T) {
	oldVersion, oldRevision := version, buildinfo.SourceRevision
	t.Cleanup(func() {
		version = oldVersion
		buildinfo.SourceRevision = oldRevision
	})
	version = "v9.8.7"
	buildinfo.SourceRevision = strings.Repeat("a", 40)

	id, err := currentDesktopRemoteBuildID()
	if err != nil {
		t.Fatal(err)
	}
	if id.ProductVersion != version || id.SourceRevision != buildinfo.SourceRevision ||
		id.ProtocolVersion != protocol.ProtocolVersion || id.SchemaHash != protocol.SchemaHash() {
		t.Fatalf("Desktop Remote Build ID = %#v", id)
	}
}

func TestCurrentDesktopRemoteBuildIDHasNoRevisionFallback(t *testing.T) {
	oldVersion, oldRevision := version, buildinfo.SourceRevision
	t.Cleanup(func() {
		version = oldVersion
		buildinfo.SourceRevision = oldRevision
	})
	version = "v9.8.7"
	buildinfo.SourceRevision = "not-an-exact-revision"

	if _, err := currentDesktopRemoteBuildID(); err == nil {
		t.Fatal("Desktop accepted an inexact Remote source revision")
	}
}
