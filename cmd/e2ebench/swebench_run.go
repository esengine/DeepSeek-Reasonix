package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/ablation"
	fileencoding "reasonix/internal/fileutil/encoding"
)

type swebenchOpts struct {
	bin        string
	subset     string
	namespace  string
	model      string
	profile    string
	arm        ablation.Set
	runID      string
	workDir    string
	harness    string
	dataset    string
	maxSteps   int
	timeoutSec int
	workers    int
	keepImages bool
}

// The agent runs unconfined inside the instance container: the container is
// already the isolation boundary, and Reasonix refuses to run bash at all when
// it cannot find bubblewrap, which the official images do not ship.
const swebenchAgentConfig = "[sandbox]\nbash = \"off\"\n"

func loadSwebenchSubset(path string) ([]swebenchInstance, error) {
	data, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return nil, err
	}
	var out []swebenchInstance
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no instances", path)
	}
	return out, nil
}

// runSwebenchInstance drives one instance: start its evaluation container, run
// the agent inside it against /testbed, and take whatever the working tree
// became as the candidate patch. Grading happens later, in one batch.
func runSwebenchInstance(o swebenchOpts, inst swebenchInstance) (result, string) {
	r := result{task: task{ID: inst.InstanceID}, Profile: o.profile}
	r.Arm = o.arm.Arm()

	image := swebenchImage(o.namespace, inst.InstanceID)
	container := swebenchContainer(inst.InstanceID)
	_ = dockerRun("rm", "-f", container)

	if out, err := dockerOutput("run", "-d", "--name", container, image, "sleep", "infinity"); err != nil {
		r.Note = "start container: " + firstLine(out)
		r.Outcome = "container_error"
		return r, ""
	}
	defer func() {
		_ = dockerRun("rm", "-f", container)
		if !o.keepImages {
			_ = dockerRun("rmi", "-f", image)
		}
	}()

	if err := provisionAgent(o, container); err != nil {
		r.Note = "provision agent: " + err.Error()
		r.Outcome = "container_error"
		return r, ""
	}

	metricsPath := "/tmp/reasonix-metrics.json"
	args := swebenchAgentArgs(metricsPath, o.model, o.profile, o.arm, o.maxSteps, swebenchPrompt(inst))
	agentCmd := append([]string{"exec", "-e", "REASONIX_HOME=/opt/rxhome", container},
		testbedShell("/usr/local/bin/reasonix "+shellQuoteAll(args))...)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(o.timeoutSec)*time.Second)
	defer cancel()
	startedAt := time.Now()
	runErr := dockerRunCtx(ctx, agentCmd...)
	r.WallMs = time.Since(startedAt).Milliseconds()

	if raw, err := dockerOutput("exec", container, "cat", metricsPath); err == nil {
		var m runMetrics
		if json.Unmarshal([]byte(raw), &m) == nil {
			arm := r.Arm
			r.runMetrics = m
			r.Arm = arm
		}
	}
	if ctx.Err() != nil {
		r.Outcome = "timeout"
	}
	if runErr != nil && r.Outcome == "" {
		r.Note = "agent: " + runErr.Error()
	}

	patch, err := dockerOutput("exec", container, "bash", "-lc",
		"cd /testbed && git add -A >/dev/null 2>&1; git diff --cached")
	if err != nil {
		r.Note = strings.TrimSpace(r.Note + " | diff: " + firstLine(patch))
		return r, ""
	}
	return r, patch
}

// provisionAgent copies the binary and a credential-free home into the
// container. The API key is streamed in rather than baked into an image layer
// or an argv, so it never lands anywhere a later docker inspect can read it.
func provisionAgent(o swebenchOpts, container string) error {
	if err := dockerRun("cp", o.bin, container+":/usr/local/bin/reasonix"); err != nil {
		return err
	}
	if err := dockerRun("exec", container, "mkdir", "-p", "/opt/rxhome"); err != nil {
		return err
	}
	if err := dockerPipe(swebenchAgentConfig, "exec", "-i", container,
		"bash", "-c", "umask 077 && cat > /opt/rxhome/config.toml"); err != nil {
		return err
	}
	env, err := os.ReadFile(filepath.Join(os.Getenv("REASONIX_HOME"), ".env"))
	if err != nil {
		return fmt.Errorf("read credentials from $REASONIX_HOME/.env: %w", err)
	}
	return dockerPipe(string(env), "exec", "-i", container,
		"bash", "-c", "umask 077 && cat > /opt/rxhome/.env")
}

// gradeSwebench writes the predictions file and hands it to the official
// harness, then reads back its report. We never decide resolution ourselves.
func gradeSwebench(o swebenchOpts, patches map[string]string, order []string) (swebenchReport, error) {
	var report swebenchReport
	predictions, err := encodePredictions("reasonix", patches, order)
	if err != nil {
		return report, err
	}
	path := filepath.Join(o.workDir, "predictions.jsonl")
	if err := os.WriteFile(path, []byte(predictions), 0o600); err != nil {
		return report, err
	}

	args := []string{"-m", "swebench.harness.run_evaluation",
		"--dataset_name", o.dataset,
		"--predictions_path", path,
		"--run_id", o.runID,
		"--max_workers", fmt.Sprint(o.workers),
		"--instance_ids"}
	args = append(args, order...)
	cmd := exec.Command(o.harness, args...)
	cmd.Dir = o.workDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return report, fmt.Errorf("run_evaluation: %w", err)
	}

	raw, err := fileencoding.ReadFileUTF8(filepath.Join(o.workDir, swebenchReportPath("reasonix", o.runID)))
	if err != nil {
		return report, err
	}
	return report, json.Unmarshal(raw, &report)
}

func runSwebench(o swebenchOpts) string {
	instances, err := loadSwebenchSubset(o.subset)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load subset:", err)
		os.Exit(1)
	}

	patches := map[string]string{}
	order := make([]string, 0, len(instances))
	results := make([]result, 0, len(instances))
	for i, inst := range instances {
		fmt.Fprintf(os.Stderr, "\n=== [%d/%d] %s ===\n", i+1, len(instances), inst.InstanceID)
		r, patch := runSwebenchInstance(o, inst)
		order = append(order, inst.InstanceID)
		if strings.TrimSpace(patch) != "" {
			patches[inst.InstanceID] = patch
		}
		results = append(results, r)
	}

	report, err := gradeSwebench(o, patches, order)
	if err != nil {
		fmt.Fprintln(os.Stderr, "grade:", err)
	}
	for i := range results {
		class := report.gradedClass(results[i].ID)
		results[i].Passed = class == "solved"
		// A guard that stopped the agent explains the failure better than the
		// grader's generic "unresolved", so an agent-side outcome wins.
		if results[i].Outcome == "" || results[i].Outcome == "success" {
			results[i].Outcome = class
		}
	}
	return renderSwebench(results, o)
}

func renderSwebench(results []result, o swebenchOpts) string {
	model := o.model
	if strings.TrimSpace(model) == "" {
		model = "config default"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## SWE-bench Verified — Reasonix (arm `%s`)\n\n", o.arm.Arm())
	fmt.Fprintf(&b, "<sub>model `%s` · subset `%s` · run `%s` · agent runs inside the official instance image · graded by the official harness</sub>\n\n",
		model, filepath.Base(o.subset), o.runID)
	b.WriteString(renderBody(results))
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// shellQuoteAll renders argv for a `bash -lc` string. Prompts carry newlines,
// quotes and backticks straight from a GitHub issue, so nothing may be passed
// unquoted.
func shellQuoteAll(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

func dockerRun(args ...string) error {
	return exec.Command("docker", args...).Run()
}

func dockerRunCtx(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = 10 * time.Second
	return cmd.Run()
}

func dockerOutput(args ...string) (string, error) {
	out, err := exec.Command("docker", args...).CombinedOutput()
	return string(out), err
}

func dockerPipe(stdin string, args ...string) error {
	cmd := exec.Command("docker", args...)
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Run()
}
