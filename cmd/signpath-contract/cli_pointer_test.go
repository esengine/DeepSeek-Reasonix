package main

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCLIPointerJobIsSwitchedSerializedAndNarrow(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	job := mappingValue(jobsOf(t, root), "cli-pointer")
	if job == nil {
		t.Fatal("cli-pointer job is missing")
	}
	if got := mappingScalar(job, "if"); got != "vars.STUDIO_PUBLISHES_CLI == 'true'" {
		t.Fatalf("cli-pointer if = %q, want the STUDIO_PUBLISHES_CLI switch", got)
	}
	needs := mappingValue(job, "needs")
	if needs == nil || needs.Kind != yaml.SequenceNode {
		t.Fatal("cli-pointer must list its needs")
	}
	seen := map[string]bool{}
	for _, n := range needs.Content {
		seen[n.Value] = true
	}
	if !seen["resolve"] || !seen["publish"] || !seen["cli-gate"] {
		t.Fatalf("cli-pointer needs %v, want resolve, publish and cli-gate", seen)
	}
	concurrency := mappingValue(job, "concurrency")
	if concurrency == nil || !strings.HasPrefix(mappingScalar(concurrency, "group"), "studio-cli-pointer-") {
		t.Fatal("cli-pointer must take its own per-channel lock, never the 1.x workflow's release-cli-<channel> group")
	}
	if mappingScalar(concurrency, "cancel-in-progress") != "false" {
		t.Fatal("cli-pointer must not cancel a run that is mid-write")
	}
	permissions := mappingValue(job, "permissions")
	if permissions == nil || permissions.Kind != yaml.MappingNode || len(permissions.Content) != 2 ||
		mappingScalar(permissions, "contents") != "write" {
		t.Fatal("cli-pointer permissions must be exactly contents: write")
	}
	if !nodeContains(job, "scripts/publish-cli-pointer.sh") {
		t.Fatal("cli-pointer must run scripts/publish-cli-pointer.sh")
	}
	if !nodeContains(job, "vars.CLI_PUBLISH_FROZEN") {
		t.Fatal("cli-pointer must pass vars.CLI_PUBLISH_FROZEN to its script")
	}
	if name, found := refersTo(job, append([]string{"RELEASE_TAG_TOKEN"}, cliChannelSecrets...)); found {
		t.Fatalf("cli-pointer reads %s; registry credentials belong to cli-channels only", name)
	}
}

func TestCLIGateBlocksEveryCLIChannel(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	jobs := jobsOf(t, root)
	gate := mappingValue(jobs, "cli-gate")
	if gate == nil {
		t.Fatal("cli-gate job is missing")
	}
	if got := mappingScalar(gate, "if"); got != "vars.STUDIO_PUBLISHES_CLI == 'true'" {
		t.Fatalf("cli-gate if = %q, want the STUDIO_PUBLISHES_CLI switch", got)
	}
	if !nodeContains(gate, "vars.CLI_PUBLISH_FROZEN") {
		t.Fatal("cli-gate must read vars.CLI_PUBLISH_FROZEN")
	}
	if nodeContains(gate, "actions/checkout") || nodeContains(gate, "secrets.") {
		t.Fatal("cli-gate must run no repository code and read no secret")
	}
	for _, name := range []string{"cli-tag", "cli-channels", "cli-pointer"} {
		needs := mappingValue(mappingValue(jobs, name), "needs")
		found := false
		for _, n := range needs.Content {
			found = found || n.Value == "cli-gate"
		}
		if !found {
			t.Fatalf("%s does not wait for cli-gate", name)
		}
	}
	for _, name := range []string{"cli-channels", "cli-pointer"} {
		found := false
		for _, n := range mappingValue(mappingValue(jobs, name), "needs").Content {
			found = found || n.Value == "cli-tag"
		}
		if !found {
			t.Fatalf("%s does not wait for cli-tag", name)
		}
	}
	found := false
	for _, n := range mappingValue(mappingValue(jobs, "cli-channels"), "needs").Content {
		found = found || n.Value == "cli-pointer"
	}
	if !found {
		t.Fatal("cli-channels does not wait for cli-pointer: the cask would name archives that may not exist yet")
	}
}

var releaseTagSecrets = []string{"RELEASE_TAG_TOKEN"}

func workflowFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, pattern := range []string{"*.yml", "*.yaml"} {
		found, err := filepath.Glob("../../.github/workflows/" + pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, found...)
	}
	if len(files) == 0 {
		t.Fatal("no workflows found")
	}
	return files
}

// The token may appear in one inline step of Studio's cli-tag job and in no
// other env scope: not the workflow's, not a job's, not another step's.
func TestReleaseTagTokenIsReadByOneInlineStep(t *testing.T) {
	holders := 0
	for _, file := range workflowFiles(t) {
		root := parseWorkflowFile(t, file)
		studio := filepath.Base(file) == "release-studio.yml"
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == "jobs" {
				continue
			}
			if what, found := refersTo(root.Content[i+1], releaseTagSecrets); found {
				t.Errorf("%s reaches %s at workflow level (%s)", file, what, root.Content[i].Value)
			}
		}
		jobs := mappingValue(root, "jobs")
		if jobs == nil {
			continue
		}
		for i := 0; i+1 < len(jobs.Content); i += 2 {
			id, job := jobs.Content[i].Value, jobs.Content[i+1]
			allowed := studio && id == "cli-tag"
			for j := 0; j+1 < len(job.Content); j += 2 {
				key, value := job.Content[j].Value, job.Content[j+1]
				if key != "steps" {
					if what, found := refersTo(value, releaseTagSecrets); found {
						t.Errorf("%s job %s reaches %s in %s; only a step of cli-tag may", file, id, what, key)
					}
					continue
				}
				for _, step := range value.Content {
					if what, found := refersTo(step, releaseTagSecrets); found {
						holders++
						if !allowed {
							t.Errorf("%s job %s reaches %s; only Studio's cli-tag step may", file, id, what)
						}
						if mappingScalar(step, "uses") != "" {
							t.Errorf("%s job %s: the token step must be inline shell, not an action", file, id)
						}
					} else if allowed && mappingScalar(step, "uses") != "" {
						t.Errorf("cli-tag must check out nothing and run no action")
					}
				}
			}
		}
	}
	if holders != 1 {
		t.Fatalf("%d steps read RELEASE_TAG_TOKEN, want exactly one", holders)
	}
}
