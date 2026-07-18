package control

import (
	"fmt"
	"testing"
	"time"

	"reasonix/internal/diff"
)

func TestCheckpointSnapshotBeginIsOneVisibleTransition(t *testing.T) {
	var m checkpointManager
	m.rebind("", t.TempDir())
	entered := make(chan struct{})
	release := make(chan struct{})
	m.beforeStoreMutation = func(kind string) {
		if kind != "begin" {
			return
		}
		close(entered)
		<-release
	}

	beginDone := make(chan struct{})
	go func() {
		m.begin("first", 3)
		close(beginDone)
	}()
	<-entered // manager boundary exists; Store.Begin has not run yet

	captured := make(chan CheckpointSnapshot, 1)
	go func() { captured <- m.capture() }()
	select {
	case got := <-captured:
		t.Fatalf("capture escaped begin transition with split state: %+v", got)
	case <-time.After(75 * time.Millisecond):
	}
	close(release)
	<-beginDone
	got := <-captured
	if len(got.Metas) != 1 || got.Metas[0].Turn != 0 || got.Metas[0].Prompt != "first" {
		t.Fatalf("metas = %+v, want completed turn 0 begin", got.Metas)
	}
	if turn, ok := got.TurnsByMessageIndex[3]; !ok || turn != 0 {
		t.Fatalf("turn mapping = %v, want 3 -> 0", got.TurnsByMessageIndex)
	}
	if !got.ConversationAvailable[0] {
		t.Fatalf("conversation capability = %v, want turn 0 true", got.ConversationAvailable)
	}
}

func TestCheckpointSnapshotTruncateIsOneVisibleTransition(t *testing.T) {
	var m checkpointManager
	m.rebind("", t.TempDir())
	m.begin("first", 1)
	m.begin("second", 3)
	entered := make(chan struct{})
	release := make(chan struct{})
	m.beforeStoreMutation = func(kind string) {
		if kind != "truncate" {
			return
		}
		close(entered)
		<-release
	}

	truncateDone := make(chan struct{})
	go func() {
		m.truncateFrom(1)
		close(truncateDone)
	}()
	<-entered // manager boundary was removed; Store still has turn 1

	captured := make(chan CheckpointSnapshot, 1)
	go func() { captured <- m.capture() }()
	select {
	case got := <-captured:
		t.Fatalf("capture escaped truncate transition with split state: %+v", got)
	case <-time.After(75 * time.Millisecond):
	}
	close(release)
	<-truncateDone
	got := <-captured
	if len(got.Metas) != 1 || got.Metas[0].Turn != 0 {
		t.Fatalf("metas after truncate = %+v, want only turn 0", got.Metas)
	}
	if len(got.TurnsByMessageIndex) != 1 || got.TurnsByMessageIndex[1] != 0 {
		t.Fatalf("turn mapping after truncate = %v, want {1:0}", got.TurnsByMessageIndex)
	}
	if len(got.ConversationAvailable) != 1 || !got.ConversationAvailable[0] {
		t.Fatalf("conversation capability after truncate = %v", got.ConversationAvailable)
	}
}

func TestCheckpointSnapshotDeepCopyUnderConcurrentCapture(t *testing.T) {
	var m checkpointManager
	m.rebind("", t.TempDir())
	m.begin("initial", 0)
	m.snapshot(diff.Change{Path: "initial.txt", Kind: diff.Create})

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for i := 1; i <= 100; i++ {
			m.begin(fmt.Sprintf("prompt-%d", i), i*2)
			m.snapshot(diff.Change{Path: fmt.Sprintf("file-%d.txt", i), Kind: diff.Create})
		}
	}()

	for i := 0; i < 100; i++ {
		got := m.capture()
		if len(got.Metas) > 0 {
			got.Metas[0].Prompt = "mutated"
			if len(got.Metas[0].Paths) > 0 {
				got.Metas[0].Paths[0] = "mutated.txt"
			}
		}
		got.TurnsByMessageIndex[-1] = -1
		got.ConversationAvailable[-1] = true
	}
	<-writerDone

	got := m.capture()
	if len(got.Metas) != 101 {
		t.Fatalf("metas = %d, want 101", len(got.Metas))
	}
	if got.Metas[0].Prompt != "initial" || len(got.Metas[0].Paths) != 1 || got.Metas[0].Paths[0] != "initial.txt" {
		t.Fatalf("caller mutation aliased manager metadata: %+v", got.Metas[0])
	}
	if _, ok := got.TurnsByMessageIndex[-1]; ok {
		t.Fatalf("caller mutation aliased turn mapping: %v", got.TurnsByMessageIndex)
	}
	if _, ok := got.ConversationAvailable[-1]; ok {
		t.Fatalf("caller mutation aliased capability map: %v", got.ConversationAvailable)
	}
}
