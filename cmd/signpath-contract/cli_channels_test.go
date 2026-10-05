package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var cliChannelSecrets = []string{"NPM_TOKEN", "HOMEBREW_TAP_TOKEN"}

func parseWorkflowFile(t *testing.T, path string) *yaml.Node {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(document.Content) != 1 {
		t.Fatalf("%s: not a single YAML document", path)
	}
	return document.Content[0]
}

func jobsOf(t *testing.T, root *yaml.Node) *yaml.Node {
	t.Helper()
	jobs := mappingValue(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		t.Fatal("workflow has no jobs mapping")
	}
	return jobs
}

func permissionsIssue(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind != yaml.MappingNode {
		return "permissions is a " + node.Value + " shorthand, not a mapping"
	}
	return ""
}

// secretsRefs reports every way a workflow can reach a named secret: dotted
// and subscripted access, and the whole-context forms that expose them all.
var secretsWholeContext = regexp.MustCompile(`\bsecrets\b\s*(?:[^.\[\w\s]|$)|toJSON\(\s*secrets\s*\)|\bsecrets\s*\[\s*[^'"\s]`)

func secretRefPattern(name string) *regexp.Regexp {
	return regexp.MustCompile(`\bsecrets\s*(?:\.\s*` + name + `\b|\[\s*['"]` + name + `['"]\s*\])`)
}

func scalarsOf(node *yaml.Node, out *[]string) {
	if node == nil {
		return
	}
	if node.Kind == yaml.ScalarNode {
		*out = append(*out, node.Value)
	}
	for _, child := range node.Content {
		scalarsOf(child, out)
	}
}

func refersTo(node *yaml.Node, names []string) (string, bool) {
	var scalars []string
	scalarsOf(node, &scalars)
	for _, text := range scalars {
		for _, name := range names {
			if secretRefPattern(name).MatchString(text) {
				return name, true
			}
		}
		if secretsWholeContext.MatchString(text) && regexp.MustCompile(`\$\{\{`).MatchString(text) {
			return "the whole secrets context", true
		}
	}
	return "", false
}

func TestCLIChannelsJobIsGatedAndAttested(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	jobs := jobsOf(t, root)
	job := mappingValue(jobs, "cli-channels")
	if job == nil {
		t.Fatal("cli-channels job is missing")
	}
	if mappingValue(job, "environment") != nil {
		t.Fatal("cli-channels must not declare an environment; the switch and the tag rule are its controls")
	}
	if got := mappingScalar(job, "if"); got != "vars.STUDIO_PUBLISHES_CLI == 'true'" {
		t.Fatalf("cli-channels if = %q, want the STUDIO_PUBLISHES_CLI switch", got)
	}
	permissions := mappingValue(job, "permissions")
	if permissions == nil || permissions.Kind != yaml.MappingNode {
		t.Fatal("cli-channels must declare its permissions as a mapping")
	}
	want := map[string]string{"contents": "read", "id-token": "write"}
	if len(permissions.Content) != 2*len(want) {
		t.Fatalf("cli-channels permissions has %d entries, want exactly %v", len(permissions.Content)/2, want)
	}
	for scope, level := range want {
		if got := mappingScalar(permissions, scope); got != level {
			t.Fatalf("cli-channels permission %s = %q, want %q", scope, got, level)
		}
	}
	for _, name := range cliChannelSecrets {
		if !secretRefPattern(name).MatchString(scalarString(job)) {
			t.Fatalf("cli-channels does not read secrets.%s", name)
		}
	}
}

func scalarString(node *yaml.Node) string {
	var scalars []string
	scalarsOf(node, &scalars)
	return strings.Join(scalars, "\n")
}

func TestReleaseStudioGrantsNoWriteAll(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	if top := mappingValue(root, "permissions"); top != nil && top.Kind == yaml.ScalarNode && top.Value == "write-all" {
		t.Error("release-studio.yml grants write-all")
	}
	jobs := jobsOf(t, root)
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		name, job := jobs.Content[i].Value, jobs.Content[i+1]
		if p := mappingValue(job, "permissions"); p != nil && p.Kind == yaml.ScalarNode && p.Value == "write-all" {
			t.Errorf("release-studio.yml job %s grants write-all", name)
		}
	}
}

func TestNoWorkflowGrantsIDTokenOrBroadPermissionsOutsideCLIChannels(t *testing.T) {
	files, err := filepath.Glob("../../.github/workflows/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows found: %v", err)
	}
	for _, file := range files {
		root := parseWorkflowFile(t, file)
		if top := mappingValue(root, "permissions"); top != nil {
			if msg := permissionsIssue(top); msg != "" {
				t.Errorf("%s: workflow-level %s", file, msg)
			} else if mappingScalar(top, "id-token") != "" {
				t.Errorf("%s: id-token at workflow level", file)
			}
		}
		jobs := mappingValue(root, "jobs")
		if jobs == nil {
			continue
		}
		for i := 0; i+1 < len(jobs.Content); i += 2 {
			name, job := jobs.Content[i].Value, jobs.Content[i+1]
			permissions := mappingValue(job, "permissions")
			if permissions == nil {
				continue
			}
			if msg := permissionsIssue(permissions); msg != "" {
				t.Errorf("%s job %s: %s", file, name, msg)
				continue
			}
			if mappingScalar(permissions, "id-token") != "" && !(filepath.Base(file) == "release-studio.yml" && name == "cli-channels") {
				t.Errorf("%s job %s requests id-token; only release-studio.yml cli-channels may", file, name)
			}
		}
	}
}

func TestOnlyCLIChannelsReferencesRegistrySecrets(t *testing.T) {
	files, err := filepath.Glob("../../.github/workflows/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows found: %v", err)
	}
	guarded := cliChannelSecrets
	for _, file := range files {
		root := parseWorkflowFile(t, file)
		jobs := mappingValue(root, "jobs")
		if jobs == nil {
			continue
		}
		if filepath.Base(file) == "release-studio.yml" {
			for i := 0; i+1 < len(jobs.Content); i += 2 {
				name, job := jobs.Content[i].Value, jobs.Content[i+1]
				if name == "cli-channels" {
					continue
				}
				if what, found := refersTo(job, guarded); found {
					t.Errorf("release-studio.yml job %s reaches %s; only cli-channels may", name, what)
				}
			}
			if what, found := refersTo(mappingValue(root, "env"), guarded); found {
				t.Errorf("release-studio.yml workflow env reaches %s", what)
			}
			continue
		}
		if name, found := refersTo(root, cliChannelSecrets); found {
			t.Errorf("%s reaches %s, which only release-studio.yml cli-channels may read", file, name)
		}
	}
}

func TestSecretReferenceDetectionCoversBypassForms(t *testing.T) {
	for _, text := range []string{
		"${{ secrets.NPM_TOKEN }}",
		"${{ secrets['NPM_TOKEN'] }}",
		`${{ secrets["NPM_TOKEN"] }}`,
		"${{ toJSON(secrets) }}",
		"${{ secrets[format('NPM_{0}', 'TOKEN')] }}",
	} {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte("run: \""+regexp.MustCompile(`"`).ReplaceAllString(text, `\"`)+"\"\n"), &doc); err != nil {
			t.Fatal(err)
		}
		if _, found := refersTo(doc.Content[0], cliChannelSecrets); !found {
			t.Errorf("reference form not detected: %s", text)
		}
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("run: echo ${{ secrets.R2_BUCKET }}\n"), &doc); err != nil {
		t.Fatal(err)
	}
	if _, found := refersTo(doc.Content[0], cliChannelSecrets); found {
		t.Error("an unrelated secret was reported")
	}
}
