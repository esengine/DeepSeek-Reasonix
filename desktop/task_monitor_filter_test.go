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
		{TaskID: "child", SessionID: "session-b", ParentTaskID: "a", ParentSessionID: "session-a"},
	}
	got := filterTasksBySession(tasks, "session-a")
	if len(got) != 3 || got[0].TaskID != "a" || got[1].TaskID != "c" || got[2].TaskID != "child" {
		t.Fatalf("filtered tasks = %#v, want a, c, and child", got)
	}
}
