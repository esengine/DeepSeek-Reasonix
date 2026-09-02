package main

import (
	"slices"
	"testing"
)

// TestRemoteTabCommandsForwardedToServe pins that every command binding
// reaches the right serve endpoint with the mapped body.
func TestRemoteTabCommandsForwardedToServe(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	const currentPath = "/sessions/current.jsonl"
	fs.newSessionPath = currentPath
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	var profileDrained []string

	steps := []struct {
		name string
		call func() error
		want string
	}{
		{"submit", func() error { return a.SubmitRemoteTab(meta.ID, "hello") }, `POST /submit {"input":"hello"}`},
		{"cancel", func() error { return a.CancelRemoteTab(meta.ID) }, "POST /cancel {}"},
		{"approve", func() error { return a.ApproveRemoteTab(meta.ID, "call-1", "allow") }, `POST /approve {"allow":true,"id":"call-1","persist":false,"session":false}`},
		{"approve-session", func() error { return a.ApproveRemoteTab(meta.ID, "call-2", "session") }, `POST /approve {"allow":true,"id":"call-2","persist":false,"session":true}`},
		{"approve-persist", func() error { return a.ApproveRemoteTab(meta.ID, "call-3", "persist") }, `POST /approve {"allow":true,"id":"call-3","persist":true,"session":true}`},
		{"approve-deny", func() error { return a.ApproveRemoteTab(meta.ID, "call-4", "deny") }, `POST /approve {"allow":false,"id":"call-4","persist":false,"session":false}`},
		{"plan-start", func() error { return a.ResolveRemoteTabPlanDecision(meta.ID, "plan-1", "start_execution", "") }, `POST /plan-decision {"action":"start_execution","feedback":"","id":"plan-1"}`},
		{"plan-revise", func() error {
			return a.ResolveRemoteTabPlanDecision(meta.ID, "plan-2", "revise_plan", "check the fallback")
		}, `POST /plan-decision {"action":"revise_plan","feedback":"check the fallback","id":"plan-2"}`},
		{"answer", func() error {
			return a.AnswerRemoteTab(meta.ID, "ask-1", []RemoteAskAnswer{{QuestionID: "question-1", Selected: []string{"yes"}}})
		}, `POST /answer {"answers":[{"QuestionID":"question-1","Selected":["yes"]}],"id":"ask-1"}`},
		{"rewind", func() error { return a.RewindRemoteTab(meta.ID, "3", "code") }, `POST /rewind {"scope":"code","turn":3}`},
		{"approval-mode", func() error { return a.SetRemoteTabToolApprovalMode(meta.ID, "auto") }, `POST /tool-approval-mode {"mode":"auto"}`},
		{"composer-profile", func() (err error) {
			profileDrained, err = a.SetRemoteTabComposerProfile(meta.ID, "plan", "auto", "")
			return
		}, `POST /composer-profile {"collaborationMode":"plan","goal":"","toolApprovalMode":"auto"}`},
		{"goal", func() error { return a.SetRemoteTabGoal(meta.ID, "ship it") }, `POST /goal {"goal":"ship it"}`},
		{"effort", func() error { return a.SetRemoteTabEffort(meta.ID, "high") }, `POST /effort {"level":"high"}`},
		{"quality-floor", func() error { return a.SetRemoteTabQualityFloor(meta.ID, "delivery") }, `POST /quality-floor {"floor":"delivery"}`},
		{"pause-goal", func() error { return a.PauseRemoteTabGoal(meta.ID) }, "POST /goal/pause {}"},
		{"resume-goal", func() error { return a.ResumeRemoteTabGoal(meta.ID) }, "POST /goal/resume {}"},
		{"cancel-jobs", func() error { return a.CancelRemoteTabJobs(meta.ID, []string{"job-1"}) }, `POST /jobs/cancel {"ids":["job-1"]}`},
		{"steer", func() error { return a.SteerRemoteTab(meta.ID, "keep it narrow") }, `POST /inbox/items {"input":"keep it narrow","intent":"steer"}`},
		{"plan-on", func() error { return a.SetRemoteTabPlanMode(meta.ID, true) }, `POST /plan {"on":true}`},
		{"compact", func() error { return a.CompactRemoteTab(meta.ID, "preserve tests") }, `POST /compact {"instructions":"preserve tests"}`},
		{"fork", func() error { return a.ForkRemoteTab(meta.ID, 2, "try-auth") }, `POST /fork {"name":"try-auth","turn":2}`},
		{"summarize", func() error { return a.SummarizeRemoteTab(meta.ID, 4, "upto") }, `POST /summarize {"mode":"upto","turn":4}`},
		{"forget", func() error { return a.ForgetRemoteTab(meta.ID, "api-key") }, `POST /forget {"name":"api-key"}`},
		{"clear", func() error { return a.ClearRemoteTabSession(meta.ID) }, "POST /clear {}"},
	}
	for _, step := range steps {
		if err := step.call(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
	}
	for i, got := range fs.recordedExpectedPaths() {
		if got != currentPath {
			t.Fatalf("command %d expected-session header = %q, want %q", i, got, currentPath)
		}
	}
	if !slices.Equal(profileDrained, []string{"approval-1"}) {
		t.Fatalf("composer profile drained ids = %v, want [approval-1]", profileDrained)
	}
	calls := fs.recorded()
	if slices.ContainsFunc(calls, func(call string) bool {
		return call == `POST /inbox/items {"idempotencyKey":"plan-revision:plan-2","input":"check the fallback","intent":"followup"}`
	}) {
		t.Fatalf("plan revision was split into a second request: %v", calls)
	}
	for _, step := range steps {
		if !slices.Contains(calls, step.want) {
			t.Fatalf("%s: serve saw %v, want %q", step.name, calls, step.want)
		}
	}
	if _, err := a.RemoteTabBranches(meta.ID); err != nil {
		t.Fatalf("branches: %v", err)
	}
	if _, err := a.RemoteTabSkills(meta.ID); err != nil {
		t.Fatalf("skills: %v", err)
	}
	if _, err := a.ReplayRemoteTabPrompts(meta.ID); err != nil {
		t.Fatalf("pending prompts: %v", err)
	}
	foundBranches, foundSkills, foundPrompts := false, false, false
	for _, c := range fs.recorded() {
		if c == "GET /branches " {
			foundBranches = true
		}
		if c == "GET /skills " {
			foundSkills = true
		}
		if c == "GET /pending-prompts " {
			foundPrompts = true
		}
	}
	if !foundBranches || !foundSkills || !foundPrompts {
		t.Fatalf("branches/skills/prompt reads missing: %v", fs.recorded())
	}
}
