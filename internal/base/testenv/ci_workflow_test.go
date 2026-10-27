package testenv

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type ciWorkflow struct {
	Jobs map[string]ciJob `yaml:"jobs"`
}

type ciJob struct {
	RunsOn   string `yaml:"runs-on"`
	Strategy struct {
		Matrix map[string]any `yaml:"matrix"`
	} `yaml:"strategy"`
	Defaults struct {
		Run struct {
			WorkingDirectory string `yaml:"working-directory"`
		} `yaml:"run"`
	} `yaml:"defaults"`
	Steps []ciStep `yaml:"steps"`
}

type ciStep struct {
	Name             string `yaml:"name"`
	If               string `yaml:"if"`
	Run              string `yaml:"run"`
	WorkingDirectory string `yaml:"working-directory"`
}

// runsOnWindows reports whether any leg of the job lands on a Windows runner,
// reading a runs-on expression through the matrix it names. A pull request may
// substitute an ubuntu runner for a leg, but the matrix entry is still the
// Windows leg that runs its steps on pushes, so it is held to the same rule.
func (j ciJob) runsOnWindows() bool {
	label := strings.TrimSpace(j.RunsOn)
	if inner, ok := strings.CutPrefix(label, "${{"); ok {
		_, rest, ok := strings.Cut(inner, "matrix.")
		if !ok {
			return false
		}
		key := rest[:len(rest)-len(strings.TrimLeft(rest, "abcdefghijklmnopqrstuvwxyz_0123456789"))]
		values, _ := j.Strategy.Matrix[key].([]any)
		return slices.ContainsFunc(values, func(v any) bool {
			s, _ := v.(string)
			return strings.HasPrefix(s, "windows-")
		})
	}
	return strings.HasPrefix(label, "windows-")
}

func (j ciJob) rootModule(s ciStep) bool {
	dir := s.WorkingDirectory
	if dir == "" {
		dir = j.Defaults.Run.WorkingDirectory
	}
	return dir == "" || filepath.Clean(dir) == "."
}

// goTestArgs returns the arguments of every `go test` the script runs, each
// cut at the first shell operator.
func goTestArgs(script string) [][]string {
	var out [][]string
	for line := range strings.SplitSeq(script, "\n") {
		fields := strings.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] != "go" || fields[i+1] != "test" {
				continue
			}
			var args []string
			for _, f := range fields[i+2:] {
				if f == "&&" || f == "||" || f == "|" || f == ";" {
					break
				}
				args = append(args, f)
			}
			out = append(out, args)
		}
	}
	return out
}

func readCIWorkflow(t *testing.T) ciWorkflow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var wf ciWorkflow
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatal(err)
	}
	return wf
}

// A root-module package list run on Windows with go test's result cache on
// pays, before and after each package, one filepath.EvalSymlinks per path its
// binary recorded outside the module. internal/assembly/boot records ~1.9M, so
// the step spends minutes printing nothing and runs into its job timeout.
func TestWindowsCILegsBypassTheTestResultCache(t *testing.T) {
	wf := readCIWorkflow(t)
	checked := 0
	for jobName, job := range wf.Jobs {
		if !job.runsOnWindows() {
			continue
		}
		for _, step := range job.Steps {
			if strings.Contains(step.If, "runner.os != 'Windows'") || !job.rootModule(step) {
				continue
			}
			for _, args := range goTestArgs(step.Run) {
				checked++
				if !slices.Contains(args, "-count=1") {
					t.Errorf("ci.yml %s / %q runs go test on Windows without -count=1: %s", jobName, step.Name, strings.Join(args, " "))
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("ci.yml has no root-module go test step that runs on Windows; this guard no longer reads the workflow's shape")
	}
}

func prOwnerTestArgs(wf ciWorkflow, windows bool) [][]string {
	job := wf.Jobs["race"]
	if windows {
		job = wf.Jobs["test"]
		if !job.runsOnWindows() {
			return nil
		}
	}
	var commands [][]string
	for _, step := range job.Steps {
		if !job.rootModule(step) || strings.Contains(step.If, "github.event_name != 'pull_request'") ||
			(windows && strings.Contains(step.If, "runner.os != 'Windows'")) {
			continue
		}
		for _, args := range goTestArgs(step.Run) {
			if (!windows && !slices.Contains(args, "-race")) || slices.ContainsFunc(args, func(arg string) bool {
				return arg == "-run" || strings.HasPrefix(arg, "-run=") || arg == "-list" || strings.HasPrefix(arg, "-list=")
			}) {
				continue
			}
			commands = append(commands, args)
		}
	}
	return commands
}

func coversPackage(args []string, target string) bool {
	return slices.ContainsFunc(args, func(pattern string) bool {
		if pattern == target || pattern == "./..." {
			return true
		}
		prefix, recursive := strings.CutSuffix(pattern, "/...")
		return recursive && (target == prefix || strings.HasPrefix(target, prefix+"/"))
	})
}

func TestPRCIExecutesIsolationOwnersOnWindowsAndRace(t *testing.T) {
	wf := readCIWorkflow(t)
	for _, windows := range []bool{true, false} {
		t.Run(map[bool]string{true: "native_windows", false: "race"}[windows], func(t *testing.T) {
			commands := prOwnerTestArgs(wf, windows)
			for _, owner := range []string{"./internal/runtime/isolation", "./internal/platform/worktree", "./internal/state/mutation"} {
				t.Run(owner, func(t *testing.T) {
					if !slices.ContainsFunc(commands, func(args []string) bool { return coversPackage(args, owner) }) {
						t.Fatalf("PR CI does not execute the full owning package %s in this matrix leg", owner)
					}
				})
			}
		})
	}
}
