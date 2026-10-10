package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/base/i18n"
)

func (k *recordingKernel) gitReply() map[string]any {
	return map[string]any{"repo": true, "name": "demo", "branch": "feat/x", "added": 4, "removed": 1, "untracked": 3}
}

func footerPlain(m *model) []string {
	rows := m.statusBlock()
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ansi.Strip(r)
	}
	return out
}

func TestFooterShowsWorkspaceBranchAndDirtBesideTheTelemetry(t *testing.T) {
	m, k := testModel(t)
	k.git = true
	m.status.Used, m.status.Window = 12000, 64000
	m.compaction = Compaction{Ratio: 0.8}
	run(m, m.fetchMeters())
	if !strings.Contains(strings.Join(k.seen(), "\n"), "GET /workspace/git") {
		t.Fatal("the footer never asked the kernel for the work tree")
	}
	rows := footerPlain(m)
	last := rows[len(rows)-1]
	if !strings.HasPrefix(last, footerIndent+"demo@feat/x  +4 -1 ?3") {
		t.Fatalf("git identity leads the data row, got %q", last)
	}
	if !strings.HasSuffix(last, strings.TrimSpace(strings.Join(footerPlainGroups(m), "  "))) {
		t.Fatalf("telemetry sits at the right edge of the same row, got %q", last)
	}
}

func footerPlainGroups(m *model) []string {
	var out []string
	for _, g := range m.telemetry() {
		out = append(out, ansi.Strip(g))
	}
	return out
}

func TestFooterCompactionReadsAsCompactLeftToTheThreshold(t *testing.T) {
	m, _ := testModel(t)
	m.status.Used, m.status.Window = 12800, 64000
	m.compaction = Compaction{Ratio: 0.8}
	got := strings.Join(footerPlainGroups(m), "  ")
	want := i18n.M.ChatStatusCompactLabel + " 60%"
	if !strings.Contains(got, want) {
		t.Fatalf("telemetry %q lacks %q", got, want)
	}
	if strings.Contains(i18n.M.ChatStatusCompactLabel, "TO ") || strings.Contains(i18n.M.ChatStatusCompactLabel, "距") {
		t.Fatalf("the label is 1.x's bare word, got %q", i18n.M.ChatStatusCompactLabel)
	}
}

func TestFooterWithoutAGitWorkTreeKeepsTheTelemetryAlone(t *testing.T) {
	m, _ := testModel(t)
	m.status.Used, m.status.Window = 12000, 64000
	run(m, m.fetchMeters())
	for _, r := range footerPlain(m) {
		if strings.Contains(r, "@") {
			t.Fatalf("no repository, no identity: %q", r)
		}
	}
}

func TestFooterDirtMarkersOmitZeroes(t *testing.T) {
	m, _ := testModel(t)
	m.git = GitInfo{Repo: true, Name: "demo", Branch: "main"}
	if got := ansi.Strip(m.gitText()); got != "demo@main" {
		t.Fatalf("clean tree: %q", got)
	}
	m.git.Untracked = 2
	if got := ansi.Strip(m.gitText()); got != "demo@main  ?2" {
		t.Fatalf("untracked only: %q", got)
	}
}
