package agent

import (
	"encoding/json"
	"testing"
)

func TestExternalActionClassifierRecognizesStaticCommandVariants(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "git push", command: "git push origin HEAD", want: true},
		{name: "git global directory", command: "git -C ../repo push origin HEAD", want: true},
		{name: "git global config", command: "git -c credential.helper= push origin HEAD", want: true},
		{name: "npm workspace publish", command: "npm --workspace pkg publish", want: true},
		{name: "pnpm directory deploy", command: "pnpm --dir pkg deploy", want: true},
		{name: "kubectl namespace apply", command: "kubectl -n production apply -f deploy.yaml", want: true},
		{name: "kubectl impersonation apply", command: "kubectl --as deployer apply -f deploy.yaml", want: true},
		{name: "docker context push", command: "docker --context production push image:tag", want: true},
		{name: "gh release create", command: "gh --repo owner/repo release create v1.2.3", want: true},
		{name: "env wrapper", command: "env CI=1 git -C ../repo push origin HEAD", want: true},
		{name: "shell wrapper", command: "bash -lc 'git -C ../repo push origin HEAD'", want: true},
		{name: "package script", command: "npm run deploy", want: true},
		{name: "task runner", command: "make publish", want: true},
		{name: "chained push", command: "git status && git -C ../repo push origin HEAD", want: true},
		{name: "git dynamic subcommand fails closed", command: `git "$action" origin HEAD`, want: true},
		{name: "git status", command: "git status --short"},
		{name: "npm view", command: "npm --workspace pkg view"},
		{name: "kubectl get", command: "kubectl -n production get pods"},
		{name: "gh release view", command: "gh --repo owner/repo release view v1.2.3"},
		{name: "ordinary local command", command: "go fmt ./..."},
		{name: "action word as data", command: "echo deploy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"command": tt.command})
			if err != nil {
				t.Fatal(err)
			}
			if got := isExternalActionTool("bash", "bash", args); got != tt.want {
				t.Fatalf("isExternalActionTool(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestExternalActionClassifierRecognizesResolvedToolNames(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "publish", want: true},
		{name: "mcp__vercel__deploy_project", want: true},
		{name: "mcp__registry__push_image", want: true},
		{name: "github_create_release", want: true},
		{name: "get_deployment_status"},
		{name: "list_releases"},
		{name: "read_file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isExternalActionTool(tt.name, tt.name, nil); got != tt.want {
				t.Fatalf("isExternalActionTool(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
