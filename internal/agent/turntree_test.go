package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestBuildTurnTreeSkipsInheritedBranchPrefix(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "root.jsonl")
	childPath := filepath.Join(dir, "child.jsonl")

	root := NewSession("sys")
	root.Add(provider.Message{Role: provider.RoleUser, Content: "first prompt\nwith spacing"})
	root.Add(provider.Message{Role: provider.RoleAssistant, Content: "first answer"})
	root.Add(provider.Message{Role: provider.RoleUser, Content: "second prompt"})
	root.Add(provider.Message{Role: provider.RoleAssistant, Content: "second answer"})
	if err := root.Save(rootPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(rootPath, BranchMeta{ID: "root"}); err != nil {
		t.Fatal(err)
	}

	child := NewSession("sys")
	child.Add(provider.Message{Role: provider.RoleUser, Content: "first prompt\nwith spacing"})
	child.Add(provider.Message{Role: provider.RoleAssistant, Content: "first answer"})
	child.Add(provider.Message{Role: provider.RoleUser, Content: "alternate second prompt"})
	if err := child.Save(childPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(childPath, BranchMeta{
		ID:               "child",
		ParentID:         "root",
		ForkTurn:         0,
		ForkMessageIndex: 3,
	}); err != nil {
		t.Fatal(err)
	}

	tree, err := BuildTurnTree(dir, childPath)
	if err != nil {
		t.Fatal(err)
	}
	flat := tree.Flatten()
	if len(flat) != 3 {
		t.Fatalf("flat nodes = %d, want 3: %+v", len(flat), flat)
	}
	if flat[0].BranchID != "root" || flat[0].Turn != 0 || flat[0].Prompt != "first prompt with spacing" {
		t.Fatalf("root first node = %+v", flat[0])
	}
	if flat[0].Response != "first answer" {
		t.Fatalf("root first response = %q, want first answer", flat[0].Response)
	}
	if flat[1].BranchID != "child" || flat[1].Turn != 1 || flat[1].Depth != 1 {
		t.Fatalf("child node should appear directly under root turn 1: %+v", flat[1])
	}
	if flat[0].ParentKey != "" {
		t.Fatalf("root parent key = %q, want empty", flat[0].ParentKey)
	}
	if flat[1].ParentKey != NodeKey("root", 0) {
		t.Fatalf("child parent key = %q, want %q", flat[1].ParentKey, NodeKey("root", 0))
	}
	if flat[2].BranchID != "root" || flat[2].Turn != 1 || flat[2].Depth != 0 {
		t.Fatalf("root continuation should remain at depth 0: %+v", flat[2])
	}
	if flat[2].ParentKey != NodeKey("root", 0) {
		t.Fatalf("continuation parent key = %q, want %q", flat[2].ParentKey, NodeKey("root", 0))
	}
	firstPrefixChars := len([]rune("sys")) + len([]rune("first prompt\nwith spacing")) + len([]rune("first answer"))
	secondPrefixChars := firstPrefixChars + len([]rune("second prompt")) + len([]rune("second answer"))
	if flat[0].PrefixChars != firstPrefixChars {
		t.Fatalf("first prefix chars = %d", flat[0].PrefixChars)
	}
	if flat[2].PrefixChars != secondPrefixChars {
		t.Fatalf("second prefix chars = %d", flat[2].PrefixChars)
	}
	if !flat[1].IsCurrent || tree.CurrentKey != NodeKey("child", 1) {
		t.Fatalf("current = %q, flat = %+v", tree.CurrentKey, flat)
	}
}

func TestBuildTurnTreeSkipsMalformedSessionInsteadOfReturningPartialTurns(t *testing.T) {
	dir := t.TempDir()
	goodPath := filepath.Join(dir, "good.jsonl")
	badPath := filepath.Join(dir, "bad.jsonl")

	good := NewSession("sys")
	good.Add(provider.Message{Role: provider.RoleUser, Content: "good"})
	if err := good.Save(goodPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(goodPath, BranchMeta{ID: "good"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badPath, []byte("{\"role\":\"user\",\"content\":\"partial\"}\n{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(badPath, BranchMeta{ID: "bad"}); err != nil {
		t.Fatal(err)
	}

	tree, err := BuildTurnTree(dir, goodPath)
	if err != nil {
		t.Fatal(err)
	}
	flat := tree.Flatten()
	if len(flat) != 1 {
		t.Fatalf("flat nodes = %d, want only the good session: %+v", len(flat), flat)
	}
	if flat[0].BranchID != "good" {
		t.Fatalf("malformed session should be skipped, got %+v", flat)
	}
}

func TestBuildTurnTreeMarksParentWhenActiveBranchHasNoUniqueTurns(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "root.jsonl")
	childPath := filepath.Join(dir, "child.jsonl")

	root := NewSession("sys")
	root.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	root.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	if err := root.Save(rootPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(rootPath, BranchMeta{ID: "root"}); err != nil {
		t.Fatal(err)
	}

	child := NewSession("sys")
	child.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	child.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	if err := child.Save(childPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(childPath, BranchMeta{
		ID:               "child",
		ParentID:         "root",
		ForkTurn:         0,
		ForkMessageIndex: 3,
	}); err != nil {
		t.Fatal(err)
	}

	tree, err := BuildTurnTree(dir, childPath)
	if err != nil {
		t.Fatal(err)
	}
	flat := tree.Flatten()
	if len(flat) != 1 {
		t.Fatalf("flat nodes = %d, want 1: %+v", len(flat), flat)
	}
	if !flat[0].IsCurrent || tree.CurrentKey != NodeKey("root", 0) {
		t.Fatalf("current should be parent fork node, key=%q flat=%+v", tree.CurrentKey, flat)
	}
}

func TestBuildTurnTreeResolvesEmptyIntermediateBranchParent(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "root.jsonl")
	emptyPath := filepath.Join(dir, "empty.jsonl")
	grandchildPath := filepath.Join(dir, "grandchild.jsonl")

	root := NewSession("sys")
	root.Add(provider.Message{Role: provider.RoleUser, Content: "root prompt"})
	root.Add(provider.Message{Role: provider.RoleAssistant, Content: "root answer"})
	if err := root.Save(rootPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(rootPath, BranchMeta{ID: "root"}); err != nil {
		t.Fatal(err)
	}

	empty := NewSession("sys")
	empty.Add(provider.Message{Role: provider.RoleUser, Content: "root prompt"})
	empty.Add(provider.Message{Role: provider.RoleAssistant, Content: "root answer"})
	if err := empty.Save(emptyPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(emptyPath, BranchMeta{
		ID:               "empty",
		ParentID:         "root",
		ForkTurn:         0,
		ForkMessageIndex: 3,
	}); err != nil {
		t.Fatal(err)
	}

	grandchild := NewSession("sys")
	grandchild.Add(provider.Message{Role: provider.RoleUser, Content: "root prompt"})
	grandchild.Add(provider.Message{Role: provider.RoleAssistant, Content: "root answer"})
	grandchild.Add(provider.Message{Role: provider.RoleUser, Content: "grandchild prompt"})
	if err := grandchild.Save(grandchildPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(grandchildPath, BranchMeta{
		ID:               "grandchild",
		ParentID:         "empty",
		ForkTurn:         0,
		ForkMessageIndex: 3,
	}); err != nil {
		t.Fatal(err)
	}

	tree, err := BuildTurnTree(dir, grandchildPath)
	if err != nil {
		t.Fatal(err)
	}
	flat := tree.Flatten()
	if len(flat) != 2 {
		t.Fatalf("flat nodes = %d, want 2: %+v", len(flat), flat)
	}
	if flat[0].BranchID != "root" || flat[0].Turn != 0 || flat[0].Depth != 0 {
		t.Fatalf("root node = %+v", flat[0])
	}
	if flat[1].BranchID != "grandchild" || flat[1].Turn != 1 || flat[1].Depth != 1 {
		t.Fatalf("grandchild should be visible under root: %+v", flat[1])
	}
	if flat[1].ParentKey != NodeKey("root", 0) {
		t.Fatalf("grandchild parent key = %q, want %q", flat[1].ParentKey, NodeKey("root", 0))
	}
}

func TestFlattenSummarizesLongTurnText(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "root.jsonl")

	root := NewSession("sys")
	root.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("p", turnPromptSummaryRunes+20)})
	root.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("r", turnResponseSummaryRunes+20)})
	if err := root.Save(rootPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(rootPath, BranchMeta{ID: "root"}); err != nil {
		t.Fatal(err)
	}

	tree, err := BuildTurnTree(dir, rootPath)
	if err != nil {
		t.Fatal(err)
	}
	flat := tree.Flatten()
	if len(flat) != 1 {
		t.Fatalf("flat nodes = %d, want 1: %+v", len(flat), flat)
	}
	if got := len([]rune(flat[0].Prompt)); got > turnPromptSummaryRunes {
		t.Fatalf("prompt summary length = %d, want <= %d", got, turnPromptSummaryRunes)
	}
	if got := len([]rune(flat[0].Response)); got > turnResponseSummaryRunes {
		t.Fatalf("response summary length = %d, want <= %d", got, turnResponseSummaryRunes)
	}
	if !strings.HasSuffix(flat[0].Prompt, "...") || !strings.HasSuffix(flat[0].Response, "...") {
		t.Fatalf("summaries should end with ellipsis: %+v", flat[0])
	}
}

func TestFlattenCurrentRootFocusesActiveTree(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.jsonl")
	secondPath := filepath.Join(dir, "second.jsonl")

	first := NewSession("sys")
	first.Add(provider.Message{Role: provider.RoleUser, Content: "first root"})
	if err := first.Save(firstPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(firstPath, BranchMeta{ID: "first"}); err != nil {
		t.Fatal(err)
	}

	second := NewSession("sys")
	second.Add(provider.Message{Role: provider.RoleUser, Content: "second root"})
	if err := second.Save(secondPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(secondPath, BranchMeta{ID: "second"}); err != nil {
		t.Fatal(err)
	}

	tree, err := BuildTurnTree(dir, secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if flat := tree.Flatten(); len(flat) != 2 {
		t.Fatalf("full flatten nodes = %d, want 2: %+v", len(flat), flat)
	}
	focused := tree.FlattenCurrentRoot()
	if len(focused) != 1 {
		t.Fatalf("focused nodes = %d, want 1: %+v", len(focused), focused)
	}
	if focused[0].BranchID != "second" || !focused[0].IsCurrent {
		t.Fatalf("focused root should be current second root: %+v", focused)
	}
}
