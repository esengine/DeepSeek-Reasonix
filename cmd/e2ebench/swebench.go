package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/ablation"
)

// SWE-bench mode runs the agent inside the official per-instance evaluation
// container, so it can execute the repo's tests exactly like the harnesses it
// is compared against, then hands the resulting patch to the official grader.

type swebenchInstance struct {
	InstanceID string `json:"instance_id"`
	Repo       string `json:"repo"`
	BaseCommit string `json:"base_commit"`
	Problem    string `json:"problem_statement"`
	Difficulty string `json:"difficulty"`
}

// swebenchImage builds the registry name of an instance's evaluation image.
// The harness mangles "__" to "_1776_" only when a namespace is set, so the
// local image key it prints is not the name you can pull.
func swebenchImage(namespace, instanceID string) string {
	return fmt.Sprintf("%s/sweb.eval.x86_64.%s:latest",
		namespace, strings.ReplaceAll(instanceID, "__", "_1776_"))
}

// swebenchContainer is the throwaway container name for one attempt. It is
// distinct from the grader's own container so a stale agent container can never
// be mistaken for an evaluation in progress.
func swebenchContainer(instanceID string) string {
	return "rxagent." + strings.ReplaceAll(instanceID, "__", ".")
}

// testbedShell wraps a command so it runs against the instance's conda
// environment. The images ship miniconda with the repo's dependencies in an env
// named "testbed"; a bare `docker exec` misses it and every import fails.
func testbedShell(command string) []string {
	return []string{"bash", "-lc",
		"source /opt/miniconda3/bin/activate && conda activate testbed && cd /testbed && " + command}
}

// permissionFlag maps a benchmark permission posture onto the CLI flag. auto is
// the unattended default. The alternative posture exists because comparable
// harnesses run without the dynamic-shell gate, so measuring against them under
// the gate measures our permission policy rather than the agent.
func permissionFlag(mode string) (string, error) {
	switch mode {
	case "", "auto":
		return "--permission-mode=auto", nil
	case "yolo":
		return "--permission-mode=bypassPermissions", nil
	default:
		return "", fmt.Errorf("unknown permission mode %q (want auto or yolo)", mode)
	}
}

func swebenchAgentArgs(metricsPath, model, profile, permission string, arm ablation.Set, maxSteps int, prompt string) []string {
	posture, err := permissionFlag(permission)
	if err != nil {
		panic(err) // validated at flag-parse time; reaching here is a wiring bug
	}
	args := []string{"run", posture, "--metrics", metricsPath}
	if model != "" {
		args = append(args, "--model", model)
	}
	if maxSteps > 0 {
		args = append(args, "--max-steps", fmt.Sprint(maxSteps))
	}
	args = appendBenchmarkProfileArgs(args, profile)
	if !arm.Empty() {
		args = append(args, "--ablate", arm.String())
	}
	return append(args, prompt)
}

// swebenchPrompt is the task text the agent sees. It carries the issue and the
// working rules, and deliberately withholds the test patch and the
// FAIL_TO_PASS list — those are the answer key.
func swebenchPrompt(inst swebenchInstance) string {
	var b strings.Builder
	b.WriteString("Resolve the following issue in the repository at /testbed.\n\n")
	b.WriteString("<issue>\n")
	b.WriteString(strings.TrimSpace(inst.Problem))
	b.WriteString("\n</issue>\n\n")
	b.WriteString("The repository is a git checkout at the commit where the issue reproduces. ")
	b.WriteString("Edit the source to fix it, and run the project's own tests to check your work. ")
	b.WriteString("Do not commit, and do not modify any test file — the fix is graded by tests you cannot see.\n")
	return b.String()
}

// swebenchPrediction is one line of the predictions file the official grader
// reads. Field names are the harness's, not ours.
type swebenchPrediction struct {
	InstanceID string `json:"instance_id"`
	Model      string `json:"model_name_or_path"`
	Patch      string `json:"model_patch"`
}

func encodePredictions(model string, patches map[string]string, order []string) (string, error) {
	var b strings.Builder
	for _, id := range order {
		patch, ok := patches[id]
		if !ok {
			continue
		}
		line, err := json.Marshal(swebenchPrediction{InstanceID: id, Model: model, Patch: patch})
		if err != nil {
			return "", err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// swebenchReport is the subset of the grader's JSON summary we consume. Unknown
// fields are ignored so a harness upgrade that adds counters does not break us.
type swebenchReport struct {
	ResolvedIDs   []string `json:"resolved_ids"`
	UnresolvedIDs []string `json:"unresolved_ids"`
	ErrorIDs      []string `json:"error_ids"`
	EmptyPatchIDs []string `json:"empty_patch_ids"`
	IncompleteIDs []string `json:"incomplete_ids"`
}

// swebenchReportPath is where run_evaluation writes its summary: the model name
// from the predictions file joined with the run id, in the working directory.
func swebenchReportPath(model, runID string) string {
	return model + "." + runID + ".json"
}

// gradedClass maps one instance's grader outcome onto our published failure
// taxonomy. An id the grader never mentions is reported as unknown rather than
// silently counted as unsolved.
func (r swebenchReport) gradedClass(instanceID string) string {
	if slices.Contains(r.ResolvedIDs, instanceID) {
		return "solved"
	}
	if slices.Contains(r.EmptyPatchIDs, instanceID) {
		return "no_patch"
	}
	if slices.Contains(r.ErrorIDs, instanceID) {
		return "grader_error"
	}
	if slices.Contains(r.IncompleteIDs, instanceID) {
		return "eval_timeout"
	}
	if slices.Contains(r.UnresolvedIDs, instanceID) {
		return "wrong_patch"
	}
	return "ungraded"
}
