package main

import (
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var windowsArchitectures = []string{"amd64", "arm64"}

func matrixArches(t *testing.T, job *yaml.Node) []string {
	t.Helper()
	matrix := mappingValue(mappingValue(job, "strategy"), "matrix")
	if matrix == nil {
		t.Fatal("job has no matrix")
	}
	var arches []string
	if list := mappingValue(matrix, "arch"); list != nil {
		for _, n := range list.Content {
			arches = append(arches, n.Value)
		}
	}
	if include := mappingValue(matrix, "include"); include != nil {
		for _, entry := range include.Content {
			if arch := mappingScalar(entry, "arch"); arch != "" {
				arches = append(arches, arch)
			}
		}
	}
	slices.Sort(arches)
	return arches
}

// One tree per job: each Windows stage runs once for every architecture the
// build produces, so no architecture reaches publish without passing through
// signing, packaging and verification.
func TestEveryWindowsSigningStageCoversEveryArchitecture(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	jobs := jobsOf(t, root)
	for _, name := range []string{"windows-sign-payload", "windows-package", "windows-verify-package", "windows-sign-installer", "windows-install-smoke"} {
		if got := matrixArches(t, mappingValue(jobs, name)); !slices.Equal(got, windowsArchitectures) {
			t.Errorf("%s covers %v, want %v", name, got, windowsArchitectures)
		}
	}
	build := mappingValue(mappingValue(mappingValue(mappingValue(jobs, "build"), "strategy"), "matrix"), "include")
	var built []string
	for _, entry := range build.Content {
		if goarch := mappingScalar(entry, "goarch"); goarch != "" {
			built = append(built, goarch)
		}
	}
	slices.Sort(built)
	if !slices.Equal(built, windowsArchitectures) {
		t.Errorf("build produces Windows bundles for %v, want %v", built, windowsArchitectures)
	}
}

// Certum's group holds one running and one waiting job, and a newer waiting job
// cancels the older. Legs of one matrix would race for that single slot, so the
// signing stages run them strictly one after another.
func TestWindowsSigningLegsRunOneAtATime(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	jobs := jobsOf(t, root)
	for _, name := range []string{"windows-sign-payload", "windows-sign-installer"} {
		job := mappingValue(jobs, name)
		strategy := mappingValue(job, "strategy")
		if got := mappingScalar(strategy, "max-parallel"); got != "1" {
			t.Errorf("%s max-parallel = %q, want 1", name, got)
		}
		if got := mappingScalar(strategy, "fail-fast"); got != "false" {
			t.Errorf("%s fail-fast = %q; a failed leg must leave the others to finish so only it reruns", name, got)
		}
		group := mappingScalar(mappingValue(job, "concurrency"), "group")
		if group != "certum-signing" {
			t.Errorf("%s concurrency group = %q, want certum-signing", name, group)
		}
		if mappingScalar(job, "environment") != "studio-release" {
			t.Errorf("%s lost its studio-release environment", name)
		}
	}
}

// Packaging, package verification and the install smoke parse archives and run
// installers, so none of them may hold an environment or a secret.
func TestWindowsPackagingStagesHoldNoSecrets(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	jobs := jobsOf(t, root)
	for _, name := range []string{"windows-package", "windows-verify-package", "windows-install-smoke"} {
		job := mappingValue(jobs, name)
		if mappingValue(job, "environment") != nil {
			t.Errorf("%s declares an environment", name)
		}
		if nodeContains(job, "secrets.") {
			t.Errorf("%s references a secret", name)
		}
		if nodeContains(job, "id-token") {
			t.Errorf("%s requests an id-token", name)
		}
	}
}

// The arm64 installer reads back through the runner's 7-Zip, which decodes
// filters the NSIS extractor cannot, so the filter that extractor reads is
// pinned on every job that packs one, and publish waits for an installer to run.
func TestArm64InstallerIsPackedWithTheFilterTheExtractorReads(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	jobs := jobsOf(t, root)
	pack := map[string]string{"build": "Package Windows from the bundle", "windows-package": "Package Windows from the signed bundle"}
	for job, step := range pack {
		env := mappingValue(stepNamed(t, mappingValue(jobs, job), step), "env")
		if got := mappingScalar(env, "ELECTRON_BUILDER_7Z_FILTER"); !strings.Contains(got, "'BCJ'") {
			t.Errorf("%s / %s: ELECTRON_BUILDER_7Z_FILTER = %q, want BCJ for arm64", job, step, got)
		}
	}
	needs := mappingValue(mappingValue(jobs, "publish"), "needs")
	var names []string
	for _, n := range needs.Content {
		names = append(names, n.Value)
	}
	if !slices.Contains(names, "windows-install-smoke") {
		t.Error("publish does not wait for windows-install-smoke")
	}
	if !strings.Contains(mappingScalar(mappingValue(jobs, "publish"), "if"), "needs.windows-install-smoke.result == 'success'") {
		t.Error("publish does not require windows-install-smoke to have succeeded")
	}
}

// Matrix legs share one output map and the last non-empty write wins, so each
// leg writes only its own architecture's key. A key that is not guarded by the
// leg's architecture would let one leg's value stand in for the other's.
func TestWindowsOutputsAreOnePerArchitecture(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	jobs := jobsOf(t, root)
	want := map[string][]string{
		"windows-sign-payload":   {"payload-digest"},
		"windows-verify-package": {"installer-sha256", "archive-sha256"},
		"windows-sign-installer": {"installer-sha256"},
	}
	for name, keys := range want {
		outputs := mappingValue(mappingValue(jobs, name), "outputs")
		if outputs == nil {
			t.Fatalf("%s has no outputs", name)
		}
		if len(outputs.Content)/2 != len(keys)*len(windowsArchitectures) {
			t.Errorf("%s declares %d outputs, want %d", name, len(outputs.Content)/2, len(keys)*len(windowsArchitectures))
		}
		for _, key := range keys {
			for _, arch := range windowsArchitectures {
				got := mappingScalar(outputs, key+"-"+arch)
				if !strings.Contains(got, "matrix.arch == '"+arch+"'") {
					t.Errorf("%s output %s-%s = %q, want it guarded by matrix.arch == '%s'", name, key, arch, got, arch)
				}
			}
		}
	}
}

// The install smoke gates publish, so it has to run exactly when a Windows
// package exists to run: after a signed installer, or after the build when
// signing is off, and never after a failed signing chain.
func TestInstallSmokeRunsOnlyWhereAPackageExists(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	jobs := jobsOf(t, root)
	smoke := mappingValue(jobs, "windows-install-smoke")
	cond := mappingScalar(smoke, "if")
	for _, want := range []string{
		"!cancelled()",
		"needs.build.result == 'success'",
		"needs.windows-sign-installer.result == 'success'",
		"vars.STUDIO_SIGNING_ENABLED != 'true'",
		"needs.windows-sign-installer.result == 'skipped'",
	} {
		if !strings.Contains(cond, want) {
			t.Errorf("windows-install-smoke if lacks %q: %s", want, cond)
		}
	}
	run := mappingScalar(stepNamed(t, smoke, "Install, check and uninstall"), "run")
	if !strings.Contains(run, "./scripts/install-smoke.ps1") {
		t.Error("the smoke job does not run scripts/install-smoke.ps1, the script pull requests run")
	}
	studio := parseWorkflowFile(t, "../../.github/workflows/studio.yml")
	pkg := mappingValue(jobsOf(t, studio), "package")
	if run := mappingScalar(stepNamed(t, pkg, "The package installs, runs and uninstalls"), "run"); !strings.Contains(run, "./scripts/install-smoke.ps1") {
		t.Error("the Studio package job does not run scripts/install-smoke.ps1, so pull requests would not exercise the release smoke")
	}
}

func TestPublishRefusesAPartialWindowsSet(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	publish := mappingValue(jobsOf(t, root), "publish")
	run := mappingScalar(stepNamed(t, publish, "Collect artifacts"), "run")
	if !strings.Contains(run, `"$count" -lt 9`) {
		t.Error("publish does not require at least 9 ReasonixStudio-* packages")
	}
	for _, name := range []string{
		"ReasonixStudio-windows-amd64-installer.exe", "ReasonixStudio-windows-arm64-installer.exe",
		"ReasonixStudio-windows-amd64.zip", "ReasonixStudio-windows-arm64.zip",
	} {
		if !strings.Contains(run, "'"+name+"'") {
			t.Errorf("publish does not require %s by name", name)
		}
	}
}
