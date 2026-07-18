package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"reasonix/internal/remote/lifecycle"
	"reasonix/internal/remote/protocol"
)

type lifecycleManagerStub struct {
	calls        []string
	actionResult lifecycle.ActionResult
	status       lifecycle.Status
	doctor       lifecycle.DoctorReport
	logs         lifecycle.LogsResult
	logsOptions  lifecycle.LogsOptions
	errors       map[string]error
}

func (manager *lifecycleManagerStub) record(command string) error {
	manager.calls = append(manager.calls, command)
	return manager.errors[command]
}

func (manager *lifecycleManagerStub) Install(context.Context) (lifecycle.ActionResult, error) {
	return manager.actionResult, manager.record("install")
}

func (manager *lifecycleManagerStub) Start(context.Context) (lifecycle.ActionResult, error) {
	return manager.actionResult, manager.record("start")
}

func (manager *lifecycleManagerStub) Stop(context.Context) (lifecycle.ActionResult, error) {
	return manager.actionResult, manager.record("stop")
}

func (manager *lifecycleManagerStub) Restart(context.Context) (lifecycle.ActionResult, error) {
	return manager.actionResult, manager.record("restart")
}

func (manager *lifecycleManagerStub) Status(context.Context) (lifecycle.Status, error) {
	return manager.status, manager.record("status")
}

func (manager *lifecycleManagerStub) Doctor(context.Context) (lifecycle.DoctorReport, error) {
	return manager.doctor, manager.record("doctor")
}

func (manager *lifecycleManagerStub) Logs(_ context.Context, options lifecycle.LogsOptions) (lifecycle.LogsResult, error) {
	manager.logsOptions = options
	return manager.logs, manager.record("logs")
}

func (manager *lifecycleManagerStub) Uninstall(context.Context) (lifecycle.ActionResult, error) {
	return manager.actionResult, manager.record("uninstall")
}

func lifecycleCLIStatus() lifecycle.Status {
	buildID := protocol.BuildID{
		ProductVersion:  "v9.8.7",
		SourceRevision:  remoteCLIRevision,
		ProtocolVersion: protocol.ProtocolVersion,
		SchemaHash:      protocol.SchemaHash(),
	}
	return lifecycle.Status{
		Platform:   "linux",
		Profile:    lifecycle.InstallProfile{ReasonixHome: "/home/test/.reasonix", ID: "profile"},
		CLIBuildID: buildID,
		Unit: lifecycle.UnitStatus{
			Enabled: true,
			Active:  true,
		},
		Socket:    lifecycle.FileStatus{Secure: true},
		Lingering: lifecycle.LingeringStatus{Known: true, Enabled: true},
	}
}

func lifecycleCLIStreams() (remoteCommandIO, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	return remoteCommandIO{
		stdin: io.NopCloser(strings.NewReader("")), stdout: &stdout, stderr: &stderr,
	}, &stdout, &stderr
}

func TestRemoteLifecycleRoutesEveryFrozenPublicCommand(t *testing.T) {
	isolateRemoteGlobals(t)
	tests := []struct {
		args       []string
		wantCall   string
		wantOutput string
		check      func(*testing.T, *lifecycleManagerStub)
	}{
		{args: []string{"install"}, wantCall: "install"},
		{args: []string{"start"}, wantCall: "start"},
		{args: []string{"stop"}, wantCall: "stop"},
		{args: []string{"restart"}, wantCall: "restart"},
		{args: []string{"status"}, wantCall: "status", wantOutput: "Reasonix Remote status\n"},
		{args: []string{"status", "--json"}, wantCall: "status", wantOutput: `"cliBuildId"`},
		{args: []string{"doctor"}, wantCall: "doctor"},
		{args: []string{"logs", "--lines", "25", "--since=2 hours ago"}, wantCall: "logs", wantOutput: "journal output\n", check: func(t *testing.T, manager *lifecycleManagerStub) {
			if manager.logsOptions.Lines != 25 || manager.logsOptions.Since != "2 hours ago" {
				t.Fatalf("logs options = %+v", manager.logsOptions)
			}
		}},
		{args: []string{"uninstall"}, wantCall: "uninstall"},
	}
	for _, test := range tests {
		t.Run(strings.Join(test.args, "_"), func(t *testing.T) {
			manager := &lifecycleManagerStub{
				actionResult: lifecycle.ActionResult{Changed: true},
				status:       lifecycleCLIStatus(),
				doctor: lifecycle.DoctorReport{
					Healthy: true,
					Checks:  []lifecycle.DoctorCheck{{Name: "unit", State: lifecycle.CheckPass, Detail: "ok"}},
				},
				logs:   lifecycle.LogsResult{Output: "journal output\n"},
				errors: make(map[string]error),
			}
			newRemoteLifecycleManager = func(version string) (lifecycle.Manager, error) {
				if version != "v9.8.7" {
					t.Fatalf("factory version = %q", version)
				}
				return manager, nil
			}
			streams, stdout, stderr := lifecycleCLIStreams()
			if rc := remoteCommandWithIO(test.args, "v9.8.7", streams); rc != 0 {
				t.Fatalf("args %v rc=%d stderr=%q", test.args, rc, stderr.String())
			}
			if len(manager.calls) != 1 || manager.calls[0] != test.wantCall {
				t.Fatalf("args %v calls=%v", test.args, manager.calls)
			}
			if test.wantOutput != "" && !strings.Contains(stdout.String(), test.wantOutput) {
				t.Fatalf("args %v stdout=%q, want %q", test.args, stdout.String(), test.wantOutput)
			}
			if test.wantOutput == "" && stdout.Len() != 0 {
				t.Fatalf("args %v unexpected stdout=%q", test.args, stdout.String())
			}
			if test.check != nil {
				test.check(t, manager)
			}
		})
	}
}

func TestRemoteLifecycleParseFailuresDoNotConstructManager(t *testing.T) {
	isolateRemoteGlobals(t)
	factoryCalls := 0
	newRemoteLifecycleManager = func(string) (lifecycle.Manager, error) {
		factoryCalls++
		return &lifecycleManagerStub{}, nil
	}
	tests := [][]string{
		nil,
		{"unknown"},
		{"install", "extra"},
		{"install", "--force"},
		{"uninstall", "--purge"},
		{"status", "--json=false"},
		{"status", "--force"},
		{"doctor", "repair"},
		{"logs", "--force"},
		{"logs", "--purge"},
		{"logs", "--lines"},
		{"logs", "--lines=-1"},
		{"logs", "--lines=1000001"},
		{"logs", "--lines=1", "--lines=2"},
		{"logs", "--since"},
		{"logs", "--since="},
		{"logs", "--since=now", "--since=later"},
		{"logs", "--since=bad\nvalue"},
	}
	for _, args := range tests {
		streams, stdout, stderr := lifecycleCLIStreams()
		if rc := remoteCommandWithIO(args, "dev", streams); rc != 2 {
			t.Errorf("args %v rc=%d, want 2; stderr=%q", args, rc, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Errorf("args %v wrote stdout %q", args, stdout.String())
		}
	}
	if factoryCalls != 0 {
		t.Fatalf("parse failures constructed manager %d times", factoryCalls)
	}
}

func TestRemoteLifecycleManagerErrorsAndProfileMismatchExitOne(t *testing.T) {
	isolateRemoteGlobals(t)
	tests := []struct {
		args    []string
		command string
		err     error
	}{
		{[]string{"install"}, "install", errors.New("install failed")},
		{[]string{"start"}, "start", errors.New("start failed")},
		{[]string{"stop"}, "stop", errors.New("stop failed")},
		{[]string{"restart"}, "restart", lifecycle.ErrProfileMismatch},
		{[]string{"status"}, "status", errors.New("status failed")},
		{[]string{"doctor"}, "doctor", errors.New("doctor failed")},
		{[]string{"logs"}, "logs", errors.New("logs failed")},
		{[]string{"uninstall"}, "uninstall", lifecycle.ErrProfileMismatch},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			manager := &lifecycleManagerStub{errors: map[string]error{test.command: test.err}}
			newRemoteLifecycleManager = func(string) (lifecycle.Manager, error) { return manager, nil }
			streams, stdout, stderr := lifecycleCLIStreams()
			if rc := remoteCommandWithIO(test.args, "dev", streams); rc != 1 {
				t.Fatalf("rc=%d, want 1", rc)
			}
			if stdout.Len() != 0 {
				t.Fatalf("manager error wrote stdout %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), test.err.Error()) {
				t.Fatalf("stderr=%q does not contain %q", stderr.String(), test.err)
			}
		})
	}
}

func TestRemoteLifecycleFactoryFailureAndNilManagerExitOne(t *testing.T) {
	isolateRemoteGlobals(t)
	for _, test := range []struct {
		name    string
		factory remoteLifecycleManagerFactory
		want    string
	}{
		{"error", func(string) (lifecycle.Manager, error) { return nil, errors.New("factory failed") }, "factory failed"},
		{"nil", func(string) (lifecycle.Manager, error) { return nil, nil }, "manager is unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			newRemoteLifecycleManager = test.factory
			streams, stdout, stderr := lifecycleCLIStreams()
			if rc := remoteCommandWithIO([]string{"status"}, "dev", streams); rc != 1 {
				t.Fatalf("rc=%d", rc)
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestRemoteStatusJSONAlwaysHasThreeTopLevelBuildIdentities(t *testing.T) {
	isolateRemoteGlobals(t)
	status := lifecycleCLIStatus()
	manager := &lifecycleManagerStub{status: status, errors: make(map[string]error)}
	newRemoteLifecycleManager = func(string) (lifecycle.Manager, error) { return manager, nil }
	streams, stdout, stderr := lifecycleCLIStreams()
	if rc := remoteCommandWithIO([]string{"status", "--json"}, "dev", streams); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"cliBuildId", "installedBuildId", "daemonBuildId"} {
		value, ok := object[key]
		if !ok {
			t.Errorf("status JSON omitted %s: %s", key, stdout.String())
			continue
		}
		if key != "cliBuildId" && string(value) != "null" {
			t.Errorf("%s = %s, want null", key, value)
		}
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		t.Fatalf("status stdout contains more than one JSON value: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("clean status wrote stderr %q", stderr.String())
	}
}

func TestRemoteStatusReportsDiagnosticsOnStderrWhileStdoutRemainsOneJSONObject(t *testing.T) {
	isolateRemoteGlobals(t)
	status := lifecycleCLIStatus()
	status.Diagnostics = []lifecycle.Diagnostic{{
		Severity: lifecycle.SeverityWarning, Message: "restart needed", Suggestion: "run restart",
	}}
	manager := &lifecycleManagerStub{status: status, errors: make(map[string]error)}
	newRemoteLifecycleManager = func(string) (lifecycle.Manager, error) { return manager, nil }
	streams, stdout, stderr := lifecycleCLIStreams()
	if rc := remoteCommandWithIO([]string{"status", "--json"}, "dev", streams); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &object); err != nil || object["cliBuildId"] == nil || !strings.Contains(stderr.String(), "restart needed") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRemoteDoctorIsReadOnlySurfaceAndUnhealthyExitOne(t *testing.T) {
	isolateRemoteGlobals(t)
	manager := &lifecycleManagerStub{
		doctor: lifecycle.DoctorReport{
			Healthy: false,
			Checks: []lifecycle.DoctorCheck{{
				Name: "unit-active", State: lifecycle.CheckFail, Detail: "inactive", Suggestion: "run start",
			}},
		},
		errors: make(map[string]error),
	}
	newRemoteLifecycleManager = func(string) (lifecycle.Manager, error) { return manager, nil }
	streams, stdout, stderr := lifecycleCLIStreams()
	if rc := remoteCommandWithIO([]string{"doctor"}, "dev", streams); rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
	if strings.Join(manager.calls, ",") != "doctor" {
		t.Fatalf("doctor invoked mutating methods: %v", manager.calls)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "unhealthy") || !strings.Contains(stderr.String(), "run start") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRemoteLifecycleNoopAndDiagnosticsAreSuccessfulAndStayOnStderr(t *testing.T) {
	isolateRemoteGlobals(t)
	manager := &lifecycleManagerStub{
		actionResult: lifecycle.ActionResult{
			Changed: false,
			Diagnostics: []lifecycle.Diagnostic{{
				Severity: lifecycle.SeverityInfo, Message: "already active",
			}},
		},
		errors: make(map[string]error),
	}
	newRemoteLifecycleManager = func(string) (lifecycle.Manager, error) { return manager, nil }
	streams, stdout, stderr := lifecycleCLIStreams()
	if rc := remoteCommandWithIO([]string{"start"}, "dev", streams); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "no changes") || !strings.Contains(stderr.String(), "already active") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRemoteUsageListsOnlyFrozenPublicAndInternalCommands(t *testing.T) {
	var output bytes.Buffer
	remoteUsage(&output)
	for _, command := range []string{
		"install", "start", "stop", "restart", "status", "doctor", "logs", "uninstall", "serve", "attach",
	} {
		if !strings.Contains(output.String(), "reasonix remote "+command) {
			t.Errorf("usage omitted %s:\n%s", command, output.String())
		}
	}
	for _, forbidden := range []string{"upgrade", "force", "purge"} {
		if strings.Contains(output.String(), forbidden) {
			t.Errorf("usage exposed deferred command/flag %q:\n%s", forbidden, output.String())
		}
	}
}
