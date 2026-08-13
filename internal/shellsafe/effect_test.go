package shellsafe

import (
	"encoding/json"
	"os"
	"testing"
)

type sharedEffectCase struct {
	Name, Command                                    string
	Certainty                                        string
	Writes                                           []string
	PermissionReader, ExecutesCode, UsesNetwork      bool
	TaskPolicyBlocked, ContentMutation, BatchBarrier bool
}

func loadSharedEffectCases(t *testing.T, path string) []sharedEffectCase {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []sharedEffectCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestSharedCommandEffectMatrix(t *testing.T) {
	for _, tc := range loadSharedEffectCases(t, "testdata/command_effects.json") {
		t.Run(tc.Name, func(t *testing.T) {
			got := ClassifyBash(tc.Command)
			wantCertainty := EffectKnown
			if tc.Certainty == "unknown" {
				wantCertainty = EffectUnknown
			}
			var wantWrites WriteDomain
			for _, domain := range tc.Writes {
				switch domain {
				case "content":
					wantWrites |= WriteWorkspaceContent
				case "repository":
					wantWrites |= WriteRepositoryMetadata
				case "host":
					wantWrites |= WriteHostState
				case "external":
					wantWrites |= WriteExternalState
				}
			}
			if got.Certainty != wantCertainty || got.Writes != wantWrites || got.IsPermissionReader() != tc.PermissionReader ||
				got.ExecutesCode != tc.ExecutesCode || got.UsesNetwork != tc.UsesNetwork || got.ContentMutation() != tc.ContentMutation {
				t.Fatalf("ClassifyBash(%q) = %+v, matrix=%+v", tc.Command, got, tc)
			}
		})
	}
}

func TestClassifyBashCommandEffects(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		certainty  Certainty
		writes     WriteDomain
		permission bool
		executes   bool
		network    bool
		family     string
	}{
		{name: "branch all", command: "git branch -a", certainty: EffectKnown, permission: true, family: "git branch"},
		{name: "branch remotes", command: "git branch --remotes", certainty: EffectKnown, permission: true, family: "git branch"},
		{name: "branch filtered list", command: "git branch --list 'release/*'", certainty: EffectKnown, permission: true, family: "git branch"},
		{name: "branch show current", command: "git branch --show-current", certainty: EffectKnown, permission: true, family: "git branch"},
		{name: "branch create", command: "git branch feature/new", certainty: EffectKnown, writes: WriteRepositoryMetadata, family: "git branch"},
		{name: "branch delete", command: "git branch -D feature/old", certainty: EffectKnown, writes: WriteRepositoryMetadata, family: "git branch"},
		{name: "branch upstream", command: "git branch --set-upstream-to origin/main", certainty: EffectKnown, writes: WriteRepositoryMetadata, family: "git branch"},

		{name: "tag list", command: "git tag --list 'v1.*'", certainty: EffectKnown, permission: true, family: "git tag"},
		{name: "tag create", command: "git tag v1.2.3", certainty: EffectKnown, writes: WriteRepositoryMetadata, family: "git tag"},
		{name: "tag delete", command: "git tag -d v1.2.3", certainty: EffectKnown, writes: WriteRepositoryMetadata, family: "git tag"},

		{name: "remote list", command: "git remote -v", certainty: EffectKnown, permission: true, family: "git remote"},
		{name: "remote get url", command: "git remote get-url origin", certainty: EffectKnown, permission: true, family: "git remote"},
		{name: "remote add", command: "git remote add origin example.invalid/repo", certainty: EffectKnown, writes: WriteRepositoryMetadata, family: "git remote"},

		{name: "config list", command: "git config --list", certainty: EffectKnown, permission: true, family: "git config"},
		{name: "config get", command: "git config --get user.name", certainty: EffectKnown, permission: true, family: "git config"},
		{name: "config legacy get", command: "git config user.name", certainty: EffectKnown, permission: true, family: "git config"},
		{name: "config local set", command: "git config user.name example", certainty: EffectKnown, writes: WriteRepositoryMetadata, family: "git config"},
		{name: "config global set", command: "git config --global user.name example", certainty: EffectKnown, writes: WriteHostState, family: "git config"},
		{name: "config edit", command: "git config --edit", certainty: EffectKnown, writes: WriteRepositoryMetadata, family: "git config"},
		{name: "config global edit", command: "git config --global -e", certainty: EffectKnown, writes: WriteHostState, family: "git config"},
		{name: "env prefixed status", command: "GOROOT=/x git status", certainty: EffectKnown, family: "git status"},
		{name: "env utility status", command: "env GOROOT=/x git status", certainty: EffectKnown, family: "git status"},
		{name: "env flags fail closed", command: "env -i git status", certainty: EffectUnknown, family: "env"},

		{name: "worktree list", command: "git worktree list", certainty: EffectKnown, permission: true, family: "git worktree"},
		{name: "worktree add", command: "git worktree add ../wt feature", certainty: EffectKnown, writes: WriteWorkspaceContent | WriteRepositoryMetadata, family: "git worktree"},
		{name: "stash list", command: "git stash list", certainty: EffectKnown, permission: true, family: "git stash"},
		{name: "stash show", command: "git stash show -p", certainty: EffectKnown, permission: true, family: "git stash"},
		{name: "stash pop", command: "git stash pop", certainty: EffectKnown, writes: WriteWorkspaceContent | WriteRepositoryMetadata, family: "git stash"},
		{name: "clean dry run", command: "git clean -ndx", certainty: EffectKnown, permission: true, family: "git clean"},
		{name: "clean force", command: "git clean -fd", certainty: EffectKnown, writes: WriteWorkspaceContent, family: "git clean"},
		{name: "submodule status", command: "git submodule status", certainty: EffectKnown, permission: true, family: "git submodule"},
		{name: "submodule update", command: "git submodule update --init", certainty: EffectKnown, writes: WriteWorkspaceContent | WriteRepositoryMetadata, family: "git submodule"},

		{name: "pure commit", command: "git commit -q -m 'checkpoint'", certainty: EffectKnown, writes: WriteRepositoryMetadata, family: "git commit"},
		{name: "commit all", command: "git commit -am 'checkpoint'", certainty: EffectKnown, writes: WriteWorkspaceContent | WriteRepositoryMetadata, family: "git commit"},
		{name: "commit amend", command: "git commit --amend -m 'checkpoint'", certainty: EffectKnown, writes: WriteWorkspaceContent | WriteRepositoryMetadata, family: "git commit"},

		{name: "diff reader", command: "git diff --check", certainty: EffectKnown, permission: true, family: "git diff"},
		{name: "diff output", command: "git diff --output=changes.patch", certainty: EffectKnown, writes: WriteWorkspaceContent, family: "git diff"},
		{name: "diff external helper", command: "git diff --ext-diff", certainty: EffectUnknown, executes: true, family: "git diff"},
		{name: "grep pager", command: "git grep --open-files-in-pager=vim needle", certainty: EffectUnknown, executes: true, family: "git grep"},

		{name: "date display", command: "date -u +%s", certainty: EffectKnown, permission: true, family: "date"},
		{name: "date set GNU", command: "date --set tomorrow", certainty: EffectKnown, writes: WriteHostState, family: "date"},
		{name: "date set BSD", command: "date 081122302026", certainty: EffectKnown, writes: WriteHostState, family: "date"},
		{name: "npm audit", command: "npm audit", certainty: EffectKnown, permission: true, network: true, family: "npm audit"},
		{name: "npm audit fix", command: "npm audit fix", certainty: EffectKnown, writes: WriteWorkspaceContent, network: true, family: "npm audit"},
		{name: "go list", command: "go list ./...", certainty: EffectKnown, permission: true, family: "go list"},
		{name: "go list readonly modules", command: "go list -mod=readonly ./...", certainty: EffectKnown, permission: true, family: "go list"},
		{name: "go list module update", command: "go list -mod=mod ./...", certainty: EffectKnown, writes: WriteWorkspaceContent, family: "go list"},
		{name: "cargo check", command: "cargo check", certainty: EffectKnown, writes: WriteWorkspaceContent, executes: true, family: "cargo check"},

		{name: "safe pipeline", command: "git branch -a | head -10", certainty: EffectKnown, permission: true, family: "git branch"},
		{name: "safe fd redirect", command: "git branch -a 2>&1", certainty: EffectKnown, permission: true, family: "git branch"},
		{name: "file redirect", command: "git status > status.txt", certainty: EffectKnown, writes: WriteWorkspaceContent, family: "git status"},
		{name: "reader then writer", command: "git status && git branch -D old", certainty: EffectKnown, writes: WriteRepositoryMetadata, family: "git branch"},
		{name: "dynamic expansion", command: "echo $HOME", certainty: EffectUnknown, family: "echo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyBash(tt.command)
			if got.Certainty != tt.certainty || got.Writes != tt.writes || got.PermissionSafe != tt.permission ||
				got.ExecutesCode != tt.executes || got.UsesNetwork != tt.network || got.CommandFamily != tt.family {
				t.Fatalf("ClassifyBash(%q) = %+v, want certainty=%v writes=%v permission=%t executes=%t network=%t family=%q",
					tt.command, got, tt.certainty, tt.writes, tt.permission, tt.executes, tt.network, tt.family)
			}
		})
	}
}

func TestCommandEffectProjectionsFailClosed(t *testing.T) {
	unknown := CommandEffect{Certainty: EffectUnknown}
	if !unknown.AnyMutation() || !unknown.WorkspaceMutation() || !unknown.ContentMutation() {
		t.Fatalf("unknown projections must fail closed: %+v", unknown)
	}
	if unknown.RepositoryMutation() || unknown.IsPermissionReader() {
		t.Fatalf("unknown effect must not invent a repository classification or permission trust: %+v", unknown)
	}

	repo := CommandEffect{Certainty: EffectKnown, Writes: WriteRepositoryMetadata}
	if !repo.AnyMutation() || !repo.WorkspaceMutation() || repo.ContentMutation() || !repo.RepositoryMutation() {
		t.Fatalf("repository-only projections are inconsistent: %+v", repo)
	}
}
