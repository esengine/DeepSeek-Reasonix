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
// reading a matrix-valued runs-on through the matrix it names.
func (j ciJob) runsOnWindows() bool {
	label := strings.TrimSpace(j.RunsOn)
	if inner, ok := strings.CutPrefix(label, "${{"); ok {
		key, _ := strings.CutSuffix(inner, "}}")
		key, ok = strings.CutPrefix(strings.TrimSpace(key), "matrix.")
		if !ok {
			return false
		}
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

// A root-module package list run on Windows with go test's result cache on
// pays, before and after each package, one filepath.EvalSymlinks per path its
// binary recorded outside the module. internal/assembly/boot records ~1.9M, so
// the step spends minutes printing nothing and runs into its job timeout.
func TestWindowsCILegsBypassTheTestResultCache(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var wf ciWorkflow
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatal(err)
	}
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
