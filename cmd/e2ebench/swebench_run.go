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
	permission string
	arm        ablation.Set
	runID      string
	workDir    string
	harness    string
	dataset    string
	maxSteps   int
	timeoutSec int
	workers    int
	keepImages bool
	// network is the isolated Docker network; proxyURL is the allowlisted API path.
	network  string
	proxyURL string
}

// The only benchmark config is sandbox.bash=off, required because official
// images do not ship bubblewrap. Network facts are intentionally not injected.
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
	r := result{task: task{ID: inst.InstanceID}, Profile: benchmarkProfileStandard}
	r.Arm = o.arm.Arm()

	image := swebenchImage(o.namespace, inst.InstanceID)
	container := swebenchContainer(inst.InstanceID)
	_ = dockerRun("rm", "-f", container)

	runArgs := []string{"run", "-d", "--name", container}
	if o.network != "" {
		runArgs = append(runArgs, "--network", o.network)
	}
	for _, kv := range proxyEnv(o.proxyURL) {
		runArgs = append(runArgs, "-e", kv)
	}
	runArgs = append(runArgs, image, "sleep", "infinity")
	if out, err := dockerOutput(runArgs...); err != nil {
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
	args := swebenchAgentArgs(metricsPath, o.model, o.permission, o.arm, o.maxSteps, swebenchPrompt(inst))
	agentCmd := append([]string{"exec", "-e", "REASONIX_HOME=/opt/rxhome", container},
		testbedShell("/usr/local/bin/reasonix "+shellQuoteAll(args))...)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(o.timeoutSec)*time.Second)
	defer cancel()
	startedAt := time.Now()
	runErr := dockerRunCtx(ctx, agentCmd...)
	r.WallMs = time.Since(startedAt).Milliseconds()

	// Prefer the final record; fall back to the snapshot a killed agent left.
	// The final file is authoritative and the sidecar is only ever read when it
	// is absent, so the two can never be counted together.
	r.Unaccounted = true
	for _, src := range []struct {
		path    string
		partial bool
	}{{metricsPath, false}, {metricsPath + ".partial", true}} {
		raw, err := dockerOutput("exec", container, "cat", src.path)
		if err != nil {
			continue
		}
		var m runMetrics
		if json.Unmarshal([]byte(raw), &m) != nil {
			continue
		}
		arm := r.Arm
		r.runMetrics = m
		r.Arm = arm
		r.Unaccounted = false
		r.Partial = src.partial || !m.Complete
		break
	}
	if ctx.Err() != nil {
		r.Outcome = "timeout"
	}
	if runErr != nil && r.Outcome == "" {
		r.Note = "agent: " + runErr.Error()
	}

	patch, err := extractTestbedPatch(container)
	if err != nil {
		r.Note = strings.TrimSpace(r.Note + " | diff: " + err.Error())
		return r, ""
	}
	return r, patch
}

const patchArgBudget = 16 << 10

// extractTestbedPatch drops only binary entries, which git apply cannot carry;
// all text files are retained and paths are passed as argv.
func extractTestbedPatch(container string) (string, error) {
	if out, err := dockerOutput("exec", container, "git", "-C", "/testbed", "add", "-A"); err != nil {
		return "", fmt.Errorf("add: %s", firstLine(out))
	}
	numstat, err := dockerOutput("exec", container, "git", "-C", "/testbed",
		"diff", "--cached", "--no-renames", "--numstat", "-z")
	if err != nil {
		return "", fmt.Errorf("numstat: %s", firstLine(numstat))
	}
	files := patchFileList(numstat)
	if len(files) == 0 {
		return "", nil
	}
	batches, err := patchFileBatches(container, files)
	if err != nil {
		return "", fmt.Errorf("diff args: %w", err)
	}
	var patch strings.Builder
	for _, batch := range batches {
		out, err := dockerOutput(testbedPatchDiffArgs(container, batch)...)
		if err != nil {
			return "", fmt.Errorf("diff: %s", firstLine(out))
		}
		patch.WriteString(out)
	}
	return patch.String(), nil
}

func testbedPatchDiffArgs(container string, files []string) []string {
	return append([]string{"exec", container, "git", "--literal-pathspecs", "-C", "/testbed",
		"diff", "--cached", "--no-renames", "--"}, files...)
}

func patchFileBatches(container string, files []string) ([][]string, error) {
	baseBytes := argvBytes(testbedPatchDiffArgs(container, nil))
	var batches [][]string
	current := make([]string, 0)
	currentBytes := baseBytes
	for _, path := range files {
		pathBytes := len(path) + 1
		if baseBytes+pathBytes > patchArgBudget {
			return nil, fmt.Errorf("path %q exceeds the %d-byte argv budget", path, patchArgBudget)
		}
		if len(current) > 0 && currentBytes+pathBytes > patchArgBudget {
			batches = append(batches, current)
			current = make([]string, 0)
			currentBytes = baseBytes
		}
		current = append(current, path)
		currentBytes += pathBytes
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches, nil
}

func argvBytes(args []string) int {
	total := 0
	for _, arg := range args {
		total += len(arg) + 1
	}
	return total
}

// patchFileList returns every text path from numstat, excluding binary entries.
func patchFileList(numstat string) []string {
	var files []string
	for entry := range strings.SplitSeq(numstat, "\x00") {
		if entry == "" {
			continue
		}
		fields := strings.SplitN(entry, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		added, deleted, path := fields[0], fields[1], fields[2]
		if added == "-" || deleted == "-" || path == "" {
			continue // binary: not representable in an appliable text diff
		}
		files = append(files, path)
	}
	return files
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

// preflight fails before the first container instead of after fifty. A unit
// test can only check the argv we build; it cannot know whether this binary
// accepts it. An earlier run lost a full arm to a flag that existed on the
// interactive command but not on `run`.
func preflight(o swebenchOpts) error {
	posture, err := permissionFlag(o.permission)
	if err != nil {
		return err
	}
	help, err := exec.Command(o.bin, "run", "--help").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s run --help: %w", o.bin, err)
	}
	name, _, _ := strings.Cut(strings.TrimPrefix(posture, "--"), "=")
	if !strings.Contains(string(help), "--"+name) {
		return fmt.Errorf("%s run does not accept --%s; the %q posture would fail on every instance", o.bin, name, o.permission)
	}
	if o.network == "" || o.proxyURL == "" {
		return fmt.Errorf("-network and -proxy are required: with off-box egress the agent reads the upstream fix and every solve is unearned")
	}
	return nil
}

func runSwebench(o swebenchOpts) string {
	if err := preflight(o); err != nil {
		fmt.Fprintln(os.Stderr, "preflight:", err)
		os.Exit(2)
	}
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
	posture := o.permission
	if posture == "" {
		posture = "auto"
	}
	fmt.Fprintf(&b, "<sub>model `%s` · permissions `%s` · subset `%s` · run `%s` · agent runs inside the official instance image · graded by the official harness</sub>\n\n",
		model, posture, filepath.Base(o.subset), o.runID)
	b.WriteString(renderBody(results))
	return b.String()
}

// proxyEnv sets both cases because the Go client reads the lowercase names via
// httpproxy.FromEnvironment while curl and pip inside the image read either.
func proxyEnv(url string) []string {
	if strings.TrimSpace(url) == "" {
		return nil
	}
	return []string{
		"http_proxy=" + url, "https_proxy=" + url,
		"HTTP_PROXY=" + url, "HTTPS_PROXY=" + url,
	}
}

func firstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return strings.TrimSpace(before)
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
