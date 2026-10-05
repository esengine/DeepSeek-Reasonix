package main

import (
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

func stepNamed(t *testing.T, job *yaml.Node, name string) *yaml.Node {
	t.Helper()
	steps := mappingValue(job, "steps")
	for _, step := range steps.Content {
		if mappingScalar(step, "name") == name {
			return step
		}
	}
	t.Fatalf("no step %q", name)
	return nil
}

func TestStudioReleaseDecidesLatestFromTheFlagsScript(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	publish := mappingValue(jobsOf(t, root), "publish")
	step := stepNamed(t, publish, "Publish GitHub release")
	run := mappingScalar(step, "run")
	env := mappingValue(step, "env")
	if got := mappingScalar(env, "CANDIDATE"); got != "${{ needs.resolve.outputs.candidate }}" {
		t.Fatalf("CANDIDATE = %q, want the resolve job's grammar-derived output", got)
	}
	if !strings.Contains(run, `scripts/studio-release-flags.sh "$TAG" "$CANDIDATE"`) {
		t.Fatal("the release flags do not come from scripts/studio-release-flags.sh")
	}
	for _, flag := range []string{"--prerelease", "--latest"} {
		if strings.Contains(run, flag) {
			t.Errorf("the publish step spells %s itself; the decision belongs to the flags script", flag)
		}
	}
	if strings.Contains(run, "gh release edit") && strings.Contains(run[strings.Index(run, "gh release edit"):], "--latest") {
		t.Error("an existing release must not have its latest flag edited")
	}
}

func TestStudioGitHubReleaseCarriesNoRootManifest(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	publish := mappingValue(jobsOf(t, root), "publish")
	run := mappingScalar(stepNamed(t, publish, "Publish GitHub release"), "run")
	if !strings.Contains(run, `= latest.json ]`) {
		t.Fatal("the publish step does not filter latest.json out of the uploaded assets")
	}
	for line := range strings.SplitSeq(run, "\n") {
		if strings.Contains(line, "gh release") && strings.Contains(line, "dist/") {
			t.Errorf("gh uploads the dist glob, root latest.json included: %s", strings.TrimSpace(line))
		}
	}
	if !strings.Contains(run, `"${assets[@]}"`) {
		t.Fatal("the release is not created from the filtered asset list")
	}
	mirror := mappingScalar(stepNamed(t, publish, "Mirror to R2 and update the Studio catalog"), "run")
	if !strings.Contains(mirror, `aws s3 cp dist/ "s3://${R2_BUCKET}/${TAG}/" --recursive`) {
		t.Fatal("the per-tag manifest must still reach R2 with the rest of dist")
	}
}
