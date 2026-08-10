package main

import (
	"testing"

	"reasonix/internal/taskmonitor"
)

func TestFilterTasksBySession(t *testing.T) {
	tasks := []taskmonitor.TaskSnapshot{
		{TaskID: "a", SessionID: "session-a"},
		{TaskID: "b", SessionID: "session-b"},
		{TaskID: "c", SessionID: "session-a"},
	}
	got := filterTasksBySession(tasks, "session-a")
	if len(got) != 2 || got[0].TaskID != "a" || got[1].TaskID != "c" {
		t.Fatalf("filtered tasks = %#v, want a and c", got)
	}
}
