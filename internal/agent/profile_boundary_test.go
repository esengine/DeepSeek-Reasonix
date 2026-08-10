package agent

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/skill"
)

// The criterion these guards enforce. It is repeated in every failure message
// because the whole point is that the next person adding a field reads it.
const profileBoundaryRule = `A field belongs to the member that DECIDES its value:
  ProfileDefinition / WorkerSpec — follows from the worker's identity (how it thinks, which model, its capability ceiling)
  TaskSpec                       — decided per call (objective, criteria, result contract)
  CapabilityGrant                — what this call may touch (ceiling ∩ request)
  ContextRequest                 — what the child starts from
  SchedulerPolicy                — when and how it runs
Fields like max_turns, write_paths, retry or verification policy are decided by
the task or the scheduler, never by the worker, so they must not enter a profile.`

func fieldNames(t *testing.T, v any) []string {
	t.Helper()
	rt := reflect.TypeOf(v)
	if rt.Kind() != reflect.Struct {
		t.Fatalf("%T is not a struct", v)
	}
	names := make([]string, 0, rt.NumField())
	for i := range rt.NumField() {
		names = append(names, rt.Field(i).Name)
	}
	slices.Sort(names)
	return names
}

func assertFieldSet(t *testing.T, what string, v any, want []string) {
	t.Helper()
	got := fieldNames(t, v)
	slices.Sort(want)
	if slices.Equal(got, want) {
		return
	}
	t.Fatalf("%s fields changed.\n  got:  %s\n  want: %s\n\n%s\n\nIf the new field really is decided by this member, add it to the guard in the same commit.",
		what, strings.Join(got, ", "), strings.Join(want, ", "), profileBoundaryRule)
}

// A profile describes a worker, not a run. Widening it is how a profile turns
// into a workflow definition language.
func TestProfileDefinitionStaysWorkerIdentityOnly(t *testing.T) {
	assertFieldSet(t, "ProfileDefinition", ProfileDefinition{}, []string{
		"Name", "Body", "AllowedTools", "Model", "Effort", "ReadOnly", "Invocation", "NamedBuiltin",
	})
}

func TestDelegationSpecMembersStaySeparate(t *testing.T) {
	assertFieldSet(t, "ProfileExecSpec", ProfileExecSpec{}, []string{
		"Task", "Worker", "Grant", "Context", "Sched",
	})
	assertFieldSet(t, "TaskSpec", TaskSpec{}, []string{"Objective", "Description"})
	assertFieldSet(t, "WorkerSpec", WorkerSpec{}, []string{
		"Kind", "Name", "Profile", "SystemPrompt", "UseProfilePrompt", "Model", "Effort",
	})
	assertFieldSet(t, "CapabilityGrant", CapabilityGrant{}, []string{
		"ReadOnly", "AllowNoTools", "CallTools", "ProfileTools", "WritePaths",
	})
	assertFieldSet(t, "ContextRequest", ContextRequest{}, []string{"ContinueFrom", "ForkFrom", "Ephemeral"})
	assertFieldSet(t, "SchedulerPolicy", SchedulerPolicy{}, []string{
		"MaxSteps", "RunInBackground", "BackgroundWriter", "Nested",
	})
}

// Routing metadata decides when a worker is picked, not how it thinks, so the
// projection must leave it in the Skill store.
func TestProfileFromSkillLeavesRoutingMetadataBehind(t *testing.T) {
	projected := fieldNames(t, ProfileDefinition{})
	for _, routing := range []string{"Triggers", "NegativeTriggers", "AutoUse", "NeedsFreshData", "Cost", "Requires", "Plugin", "Path", "SlashPrefix", "Color"} {
		if slices.Contains(projected, routing) {
			t.Errorf("Skill routing field %q reached ProfileDefinition.\n\n%s", routing, profileBoundaryRule)
		}
	}
}

// The opposite failure: a field that legitimately belongs to the worker is
// declared on both types but never wired through, so profiles silently lose it.
func TestProfileFromSkillPopulatesEveryIdentityField(t *testing.T) {
	got := ProfileFromSkill(skill.Skill{
		Name:         "reviewer",
		Body:         "you review code",
		AllowedTools: []string{"read_file"},
		Model:        "some-model",
		Effort:       "high",
		ReadOnly:     true,
		Invocation:   "manual",
	})
	rv := reflect.ValueOf(got)
	rt := rv.Type()
	for i := range rt.NumField() {
		if rt.Field(i).Name == "NamedBuiltin" {
			continue // derived from the name, not carried on the Skill
		}
		if rv.Field(i).IsZero() {
			t.Errorf("ProfileFromSkill left %s unset — the projection dropped a worker identity field", rt.Field(i).Name)
		}
	}
	if got.NamedBuiltin {
		t.Error("a custom profile must not be flagged as a named built-in")
	}
}
