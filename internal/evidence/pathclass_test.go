package evidence

import "testing"

func TestClassifyPathRejectsSubstringFalsePositives(t *testing.T) {
	tests := []struct {
		path string
		want PathClass
	}{
		{"author.go", PathOrdinary},
		{"authority.md", PathDocs},
		{"docs/authority.md", PathDocs},
		{"internal/author/author.go", PathOrdinary},
		{"auth/session.go", PathAuth},
		{"internal/auth/session.go", PathAuth},
		{"internal/permission/gate.go", PathAuth},
		{"internal/oauth/client.go", PathAuth},
		{"secrets/token.go", PathSecret},
		{"internal/db/migrations/001_init.sql", PathMigration},
		{"schema/user.proto", PathSchema},
		{"api/openapi.yaml", PathPublicAPI},
		{"docs/GUIDE.md", PathDocs},
		{"locales/en.json", PathI18n},
		{"internal/agent/agent_test.go", PathTest},
		{"testdata/fixture.json", PathTest},
		{"web/app.css", PathStyle},
		{"internal/agent/agent.go", PathOrdinary},
		{"internal/agent/mutex.go", PathConcurrency},
		{"internal/agent/mutex_test.go", PathTest},
	}
	for _, tt := range tests {
		if got := ClassifyPath(tt.path, ""); got != tt.want {
			t.Fatalf("ClassifyPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestClassifyPathIgnoresPromptWords(t *testing.T) {
	if got := ClassifyPath("README.md", ""); got != PathDocs {
		t.Fatalf("readme = %v, want docs", got)
	}
	if ClassifyPath("internal/parser/parser.go", "") != PathOrdinary {
		t.Fatal("ordinary production path must not inherit prompt keywords")
	}
}

func TestPathLooksSensitiveUsesSegments(t *testing.T) {
	if pathLooksSensitive("author.go", "") {
		t.Fatal("author.go must not be sensitive")
	}
	if pathLooksSensitive("authority.md", "") {
		t.Fatal("authority.md must not be sensitive")
	}
	if !pathLooksSensitive("internal/auth/session.go", "") {
		t.Fatal("auth/session.go must be sensitive")
	}
}

func TestPathModuleSplitsTopLevelPackages(t *testing.T) {
	if got, want := PathModule("internal/agent/agent.go", ""), "internal/agent"; got != want {
		t.Fatalf("agent module = %q, want %q", got, want)
	}
	if got, want := PathModule("internal/plugin/plugin.go", ""), "internal/plugin"; got != want {
		t.Fatalf("plugin module = %q, want %q", got, want)
	}
	if PathModule("internal/agent/agent.go", "") == PathModule("internal/plugin/plugin.go", "") {
		t.Fatal("agent and plugin must be distinct modules")
	}
}
